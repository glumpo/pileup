package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/glumpo/pileup/core/blob"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/peer"
	"github.com/glumpo/pileup/core/replica"
	"github.com/glumpo/pileup/core/store"
)

const (
	pileIDSize  = 16
	retry       = time.Second
	retries     = 100
	backoff     = 10 * time.Millisecond
	timeout     = 20 * time.Second
	dialTimeout = 5 * time.Second
)

var (
	ErrUnknownPile = errors.New("node: unknown pile")
	ErrJoined      = errors.New("node: already a member of this pile")
	ErrUnreachable = errors.New("node: no inviter address accepted the invite")
	ErrOffline     = errors.New("node: no network address to invite with")
)

type Config struct {
	Dir      string
	Keystore device.Keystore
	Platform string
	Addrs    func() []netip.Addr
	Media    MediaProvider

	AppVersion string
	Renditions RenditionMaker
	Discovery  DiscoveryProvider
}

type Pile struct {
	ID      []byte
	Name    string
	Members map[op.Author]state.Member
}

type entry struct {
	ID     []byte   `json:"id"`
	Secret []byte   `json:"secret"`
	Port   uint16   `json:"port"`
	Peers  []string `json:"peers"`
}

type pile struct {
	entry
	store   *store.Store
	replica *replica.Node
	owners  *http.Client
	stream  *http.Client
	local   map[string]string
	online  map[op.Author]int
	loops   map[string]*loop
}

type loop struct {
	cancel context.CancelFunc
}

type Node struct {
	cfg    Config
	key    ed25519.PrivateKey
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	blobs  *blob.Store
	diag   *diag.Log

	mu    sync.Mutex
	piles []*pile
	found map[string]map[string]service

	selecting sync.Mutex
	rendering sync.Mutex
}

func Open(cfg Config) (*Node, error) {
	key, err := device.Key(cfg.Keystore)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(cfg.Dir, "piles"), 0o700); err != nil {
		return nil, err
	}
	var entries []entry
	b, err := os.ReadFile(filepath.Join(cfg.Dir, "piles.json"))
	if err == nil {
		err = json.Unmarshal(b, &entries)
	} else if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	blobs, err := blob.Open(filepath.Join(cfg.Dir, "blobs"))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	n := &Node{cfg: cfg, key: key, ctx: ctx, cancel: cancel, blobs: blobs, found: map[string]map[string]service{}, diag: diag.New(diagSize)}
	n.mu.Lock()
	for _, e := range entries {
		var p *pile
		if p, err = n.start(e); err != nil {
			break
		}
		n.piles = append(n.piles, p)
	}
	if err == nil {
		err = n.save()
	}
	n.mu.Unlock()
	if err != nil {
		n.Close()
		return nil, err
	}
	n.discover()
	return n, nil
}

func (n *Node) Close() error {
	n.stopDiscovery()
	n.mu.Lock()
	n.cancel()
	n.mu.Unlock()
	n.wg.Wait()
	var errs []error
	for _, p := range n.piles {
		errs = append(errs, p.store.Close())
	}
	return errors.Join(errs...)
}

func (n *Node) Author() op.Author {
	return op.Author(n.key.Public().(ed25519.PublicKey))
}

func (n *Node) Create(name, member string) (Pile, error) {
	e := entry{ID: random(pileIDSize), Secret: random(invite.SecretSize)}
	return n.add(e, n.join(e.Secret, member), op.PileCreate{Name: name})
}

func (n *Node) Join(ctx context.Context, inv, member string) (Pile, error) {
	i, err := invite.Parse(inv)
	if err != nil {
		return Pile{}, err
	}
	if _, err := n.find(i.PileID); err == nil {
		return Pile{}, ErrJoined
	}
	addr, err := n.reach(ctx, i)
	if err != nil {
		return Pile{}, err
	}
	e := entry{ID: i.PileID, Secret: i.Secret, Peers: []string{addr}}
	return n.add(e, n.join(e.Secret, member))
}

func (n *Node) Piles() ([]Pile, error) {
	n.mu.Lock()
	ps := slices.Clone(n.piles)
	n.mu.Unlock()
	out := make([]Pile, 0, len(ps))
	for _, p := range ps {
		info, err := p.info()
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

func (n *Node) Invite(id []byte) (string, error) {
	p, err := n.find(id)
	if err != nil {
		return "", err
	}
	i := invite.Invite{PileID: p.ID, Secret: p.Secret}
	for _, a := range n.cfg.Addrs() {
		i.Addrs = append(i.Addrs, netip.AddrPortFrom(a, p.Port))
	}
	if len(i.Addrs) == 0 {
		return "", ErrOffline
	}
	return i.String(), nil
}

func (n *Node) find(id []byte) (*pile, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, p := range n.piles {
		if bytes.Equal(p.ID, id) {
			return p, nil
		}
	}
	return nil, ErrUnknownPile
}

func (n *Node) reach(ctx context.Context, i invite.Invite) (string, error) {
	addrs := n.discovered(i.PileID)
	for _, a := range i.Addrs {
		addrs = append(addrs, a.String())
	}
	var errs []error
	for _, a := range addrs {
		dctx, cancel := context.WithTimeout(ctx, dialTimeout)
		c, err := peer.Dial(dctx, a, n.key, i.PileID, i.Secret)
		cancel()
		if err == nil {
			c.Close()
			return a, nil
		}
		n.report(diag.Connection, i.PileID, a, "join", err)
		errs = append(errs, err)
	}
	return "", fmt.Errorf("%w: %w", ErrUnreachable, errors.Join(errs...))
}

func (n *Node) add(e entry, changes ...op.Change) (Pile, error) {
	p, err := n.start(e, changes...)
	if err != nil {
		return Pile{}, err
	}
	n.mu.Lock()
	n.piles = append(n.piles, p)
	err = n.save()
	n.mu.Unlock()
	if err != nil {
		return Pile{}, err
	}
	n.advertise(p)
	return p.info()
}

func (n *Node) start(e entry, changes ...op.Change) (*pile, error) {
	c, err := n.load(e.ID)
	if err != nil {
		return nil, err
	}
	s, err := store.Open(filepath.Join(n.cfg.Dir, "piles", hex.EncodeToString(e.ID)+".db"), e.ID, e.Secret)
	if err != nil {
		return nil, err
	}
	for _, c := range changes {
		if _, err := s.Create(n.key, now(), c); err != nil {
			s.Close()
			return nil, err
		}
	}
	ln, err := listen(e.Port)
	if err != nil {
		s.Close()
		return nil, err
	}
	e.Port = uint16(ln.Addr().(*net.TCPAddr).Port)
	n.diag.Add(diag.Connection, e.ID, "", fmt.Sprintf("listening on port %d", e.Port))
	p := &pile{entry: e, store: s, replica: replica.New(s, n.key, e.ID, e.Secret), local: c.Local, online: map[op.Author]int{}, loops: map[string]*loop{}}
	p.owners = n.owners(p)
	p.stream = &http.Client{Transport: p.owners.Transport}
	for _, a := range p.Peers {
		n.dial(p, a, p.replica.Hold())
	}
	n.serve(p, ln)
	n.thumbnails(p)
	n.thumbhashes(p)
	return p, nil
}

func listen(port uint16) (net.Listener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil && port != 0 {
		return net.Listen("tcp", ":0")
	}
	return ln, err
}

func (n *Node) serve(p *pile, ln net.Listener) {
	mux := http.NewServeMux()
	mux.Handle("/", p.replica.Handler())
	mux.HandleFunc("POST /announce", func(w http.ResponseWriter, r *http.Request) { n.announced(p, w, r) })
	mux.HandleFunc("GET /blob/{asset}/{revision}/{tier}", func(w http.ResponseWriter, r *http.Request) { n.serveBlob(p, w, r) })
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: timeout, IdleTimeout: timeout, ConnState: n.handshakes(p)}
	l := peer.NewListener(ln, n.key, func(id []byte) ([]byte, bool) {
		return p.Secret, bytes.Equal(id, p.ID)
	})
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		srv.Serve(l)
	}()
	context.AfterFunc(n.ctx, func() {
		srv.Close()
		ln.Close()
	})
}

func (n *Node) announced(p *pile, w http.ResponseWriter, r *http.Request) {
	port, err := strconv.ParseUint(r.URL.Query().Get("port"), 10, 16)
	from, ferr := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil || ferr != nil || port == 0 {
		http.Error(w, "bad announce", http.StatusBadRequest)
		return
	}
	addr := netip.AddrPortFrom(from.Addr().Unmap(), uint16(port)).String()
	n.diag.Add(diag.Connection, p.ID, addr, "announce received")
	if err := n.addPeer(p, addr); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	n.mu.Lock()
	n.dial(p, addr, func() {})
	others := slices.DeleteFunc(slices.Clone(p.Peers), func(a string) bool { return a == addr })
	n.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(others)
}

func (n *Node) addPeer(p *pile, addr string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ctx.Err() != nil || slices.Contains(p.Peers, addr) {
		return nil
	}
	p.Peers = append(p.Peers, addr)
	n.diag.Add(diag.Discovery, p.ID, addr, "new peer address")
	n.dial(p, addr, func() {})
	return n.save()
}

func (n *Node) dial(p *pile, addr string, release func()) {
	if n.ctx.Err() != nil || p.loops[addr] != nil {
		release()
		return
	}
	ctx, cancel := context.WithCancel(n.ctx)
	l := &loop{cancel: cancel}
	p.loops[addr] = l
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		defer cancel()
		n.connect(ctx, p, addr, release)
		n.mu.Lock()
		if p.loops[addr] == l {
			delete(p.loops, addr)
		}
		n.mu.Unlock()
	}()
}

func (n *Node) connect(ctx context.Context, p *pile, addr string, release func()) {
	for retry := 0; ; retry++ {
		if retry > 0 && pause(ctx, time.Duration(retry)*backoff) != nil {
			return
		}
		synced, err := n.attempt(ctx, p, addr, release)
		release()
		if synced {
			retry = 0
		}
		if ctx.Err() != nil || errors.Is(err, peer.ErrCertificate) || errors.Is(err, peer.ErrMembership) || retry == retries {
			return
		}
	}
}

func (n *Node) attempt(ctx context.Context, p *pile, addr string, release func()) (bool, error) {
	who, err := n.announce(ctx, p, addr)
	n.report(diag.Connection, p.ID, addr, "announce", err)
	if err != nil {
		return false, err
	}
	n.report(diag.Discovery, p.ID, addr, "add peer", n.addPeer(p, addr))
	n.connected(p, who, 1)
	defer n.connected(p, who, -1)
	synced, report := false, n.synced(p, addr)
	err = p.replica.Pull(ctx, addr, func(ops int) {
		synced = true
		release()
		report(ops)
	})
	n.report(diag.Sync, p.ID, addr, "sync", err)
	return synced, err
}

var pause = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (n *Node) announce(ctx context.Context, p *pile, addr string) (op.Author, error) {
	var mu sync.Mutex
	var who op.Author
	c := http.Client{Timeout: timeout, Transport: &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, a string) (net.Conn, error) {
			c, err := peer.Dial(ctx, a, n.key, p.ID, p.Secret)
			if err == nil {
				mu.Lock()
				who = op.Author(c.Peer())
				mu.Unlock()
			}
			return c, err
		},
	}}
	u := "http://" + addr + "/announce?port=" + strconv.Itoa(int(p.Port))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return op.Author{}, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return op.Author{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return op.Author{}, errors.New(resp.Status)
	}
	var others []string
	if err := json.NewDecoder(resp.Body).Decode(&others); err != nil {
		return op.Author{}, err
	}
	for _, a := range others {
		n.report(diag.Discovery, p.ID, a, "add peer", n.addPeer(p, a))
	}
	mu.Lock()
	defer mu.Unlock()
	return who, nil
}

func (n *Node) connected(p *pile, who op.Author, d int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	p.online[who] += d
	if p.online[who] == 0 {
		delete(p.online, who)
	}
}

func (n *Node) Online(pileID []byte) map[op.Author]bool {
	out := map[op.Author]bool{}
	p, err := n.find(pileID)
	if err != nil {
		return out
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for a := range p.online {
		out[a] = true
	}
	return out
}

func (n *Node) save() error {
	entries := make([]entry, len(n.piles))
	for i, p := range n.piles {
		entries[i] = p.entry
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return device.WriteFile(filepath.Join(n.cfg.Dir, "piles.json"), b)
}

func (n *Node) join(secret []byte, member string) op.Join {
	return op.Join{Name: member, Platform: n.cfg.Platform, AppVersion: n.cfg.AppVersion, Proof: op.JoinProof(secret, n.Author())}
}

func (p *pile) info() (Pile, error) {
	v, err := p.store.View()
	if err != nil {
		return Pile{}, err
	}
	return Pile{ID: p.ID, Name: v.PileName, Members: v.Members}, nil
}

func random(size int) []byte {
	b := make([]byte, size)
	rand.Read(b)
	return b
}

func now() int64 {
	return time.Now().UnixMilli()
}
