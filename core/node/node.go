package node

import (
	"bytes"
	"cmp"
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

var announceTimeout = timeout

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
	Budget   int64

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
	ID     []byte            `json:"id"`
	Secret []byte            `json:"secret"`
	Port   uint16            `json:"port"`
	Peers  map[string][]byte `json:"peers"`
}

type pile struct {
	entry
	store   *store.Store
	replica *replica.Node
	owners  *http.Client
	stream  *http.Client
	online  map[op.Author]int
	seen    map[op.Author]time.Time
	addrs   map[op.Author]string
	peers   map[string]op.Author
	faults  map[string]string
	loops   map[string]*loop
}

type loop struct {
	cancel  context.CancelFunc
	kick    context.CancelFunc
	live    bool
	failing bool
}

type connKey struct{}

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
	rendering turns
}

func Open(cfg Config) (*Node, error) {
	cfg.Budget = cmp.Or(cfg.Budget, DefaultBudget)
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
	blobs, err := blob.Open(filepath.Join(cfg.Dir, "blobs"), cfg.Budget)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	n := &Node{cfg: cfg, key: key, ctx: ctx, cancel: cancel, blobs: blobs, found: map[string]map[string]service{}, diag: diag.New(diagSize)}
	n.mu.Lock()
	for _, e := range entries {
		var p *pile
		if p, err = n.start(e, map[op.Author]string{}); err != nil {
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
	n.watchNetwork()
	return n, nil
}

func (n *Node) Close() error {
	n.stopDiscovery()
	n.mu.Lock()
	n.cancel()
	n.mu.Unlock()
	n.wg.Wait()
	errs := []error{n.blobs.Close()}
	for _, p := range n.piles {
		errs = append(errs, p.store.Close())
	}
	return errors.Join(errs...)
}

func (n *Node) Author() op.Author {
	return op.Author(n.key.Public().(ed25519.PublicKey))
}

func (n *Node) Create(name, member string) (Pile, error) {
	e := entry{ID: random(pileIDSize), Secret: random(invite.SecretSize), Peers: map[string][]byte{}}
	return n.add(e, map[op.Author]string{}, n.join(e.Secret, member), op.PileCreate{Name: name})
}

func (n *Node) Join(ctx context.Context, inv, member string) (Pile, error) {
	i, err := invite.Parse(inv)
	if err != nil {
		return Pile{}, err
	}
	if _, err := n.find(i.PileID); err == nil {
		return Pile{}, ErrJoined
	}
	addr, who, err := n.reach(ctx, i)
	if err != nil {
		return Pile{}, err
	}
	e := entry{ID: i.PileID, Secret: i.Secret, Peers: map[string][]byte{addr: who[:]}}
	joined, err := n.add(e, map[op.Author]string{who: addr}, n.join(e.Secret, member))
	if p, ferr := n.find(i.PileID); err == nil && ferr == nil {
		n.fetching(p, who, addr, "")
	}
	return joined, err
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

func (n *Node) reach(ctx context.Context, i invite.Invite) (string, op.Author, error) {
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
			return a, op.Author(c.Peer()), nil
		}
		n.report(diag.Connection, i.PileID, a, "join", err)
		errs = append(errs, err)
	}
	return "", op.Author{}, fmt.Errorf("%w: %w", ErrUnreachable, errors.Join(errs...))
}

func (n *Node) add(e entry, addrs map[op.Author]string, changes ...op.Change) (Pile, error) {
	p, err := n.start(e, addrs, changes...)
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

func (n *Node) start(e entry, addrs map[op.Author]string, changes ...op.Change) (*pile, error) {
	peers, err := peersOf(e)
	if err != nil {
		return nil, err
	}
	if err := n.importPins(e.ID); err != nil {
		return nil, err
	}
	s, err := store.Open(filepath.Join(n.cfg.Dir, "piles", hex.EncodeToString(e.ID)+".db"), e.ID, e.Secret)
	if err != nil {
		return nil, err
	}
	if err := n.importSelection(e.ID, s); err != nil {
		s.Close()
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
	e.Peers = nil
	p := &pile{entry: e, store: s, replica: replica.New(s, n.key, e.ID, e.Secret), online: map[op.Author]int{}, seen: map[op.Author]time.Time{}, addrs: addrs, peers: peers, faults: map[string]string{}, loops: map[string]*loop{}}
	p.owners = n.owners(p)
	p.stream = &http.Client{Transport: p.owners.Transport}
	for a := range p.peers {
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
	srv := &http.Server{
		Handler:           n.tracked(mux),
		ReadHeaderTimeout: timeout,
		IdleTimeout:       timeout,
		ConnState:         n.handshakes(p),
		BaseContext:       func(net.Listener) context.Context { return n.ctx },
		ConnContext:       func(ctx context.Context, c net.Conn) context.Context { return context.WithValue(ctx, connKey{}, c) },
	}
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

func (n *Node) tracked(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.mu.Lock()
		if n.ctx.Err() != nil {
			n.mu.Unlock()
			panic(http.ErrAbortHandler)
		}
		n.wg.Add(1)
		n.mu.Unlock()
		defer n.wg.Done()
		h.ServeHTTP(w, r)
	})
}

func (n *Node) announced(p *pile, w http.ResponseWriter, r *http.Request) {
	port, err := strconv.ParseUint(r.URL.Query().Get("port"), 10, 16)
	from, ferr := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil || ferr != nil || port == 0 {
		http.Error(w, "bad announce", http.StatusBadRequest)
		return
	}
	asker := op.Author(r.Context().Value(connKey{}).(*peer.Conn).Peer())
	w.Header().Set("Content-Type", "application/json")
	if asker == n.Author() {
		w.Write([]byte("[]"))
		return
	}
	addr := netip.AddrPortFrom(from.Addr().Unmap(), uint16(port)).String()
	n.diag.Add(diag.Connection, p.ID, addr, "announce received from "+n.member(p, asker))
	n.mu.Lock()
	fresh, err := n.admit(p, addr, asker)
	var others []listedAddr
	if err == nil {
		n.hurry(p, addr)
		others = p.listing(asker)
	}
	n.mu.Unlock()
	if fresh {
		n.added(p, addr, asker, "announced by the member")
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(others)
}

func (n *Node) learn(p *pile, l *loop, who op.Author, addr string) {
	n.mu.Lock()
	was, known := p.peers[addr]
	p.peers[addr] = who
	delete(p.faults, addr)
	l.live, l.failing = true, false
	p.online[who]++
	prev := p.addrs[who]
	p.addrs[who] = addr
	dropped := p.supersede(who)
	var err error
	if !known || was != who || len(dropped) > 0 {
		err = n.save()
	}
	n.mu.Unlock()
	n.report(diag.Discovery, p.ID, addr, "save addresses", err)
	if known && was != who {
		n.diag.Add(diag.Discovery, p.ID, addr, fmt.Sprintf("address now answers as %s instead of %s", n.member(p, who), n.member(p, was)))
	}
	if prev != addr {
		p.owners.Transport.(*http.Transport).CloseIdleConnections()
		n.fetching(p, who, addr, prev)
	}
	for _, d := range dropped {
		n.diag.Add(diag.Discovery, p.ID, d, fmt.Sprintf("dropped address of %s: it answers at %s", n.member(p, who), addr))
	}
}

func (n *Node) left(p *pile, l *loop, who op.Author) {
	n.mu.Lock()
	defer n.mu.Unlock()
	l.live = false
	p.seen[who] = time.Now()
	p.online[who]--
	if p.online[who] == 0 {
		delete(p.online, who)
	}
}

func (n *Node) addPeer(p *pile, addr string, who op.Author, from string) error {
	n.mu.Lock()
	fresh, err := n.admit(p, addr, who)
	n.mu.Unlock()
	if fresh {
		n.added(p, addr, who, from)
	}
	return err
}

func (n *Node) admit(p *pile, addr string, who op.Author) (bool, error) {
	if n.ctx.Err() != nil {
		return false, nil
	}
	if _, known := p.peers[addr]; known {
		return false, nil
	}
	p.peers[addr] = who
	n.dial(p, addr, func() {})
	return true, n.save()
}

func (n *Node) added(p *pile, addr string, who op.Author, from string) {
	n.diag.Add(diag.Discovery, p.ID, addr, fmt.Sprintf("new address of %s, %s", n.member(p, who), from))
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
		n.connect(ctx, p, l, addr, release)
		n.mu.Lock()
		if p.loops[addr] == l {
			delete(p.loops, addr)
		}
		n.mu.Unlock()
	}()
}

func (n *Node) connect(ctx context.Context, p *pile, l *loop, addr string, release func()) {
	for retry := 0; ; retry++ {
		if retry > 0 {
			kicked, err := n.rest(ctx, l, delay(retry))
			if err != nil {
				return
			}
			if kicked {
				retry = 0
			}
		}
		synced, err := n.attempt(ctx, p, l, addr, release)
		release()
		if synced {
			retry = 0
		}
		if ctx.Err() != nil {
			return
		}
		n.fault(p, addr, err)
		if errors.Is(err, peer.ErrCertificate) || errors.Is(err, peer.ErrMembership) || errors.Is(err, errOwnAddress) {
			return
		}
		if n.failed(p, l, addr) {
			return
		}
	}
}

func (n *Node) attempt(ctx context.Context, p *pile, l *loop, addr string, release func()) (bool, error) {
	who, others, err := n.announce(ctx, p, addr)
	n.report(diag.Connection, p.ID, addr, "announce", err)
	if err != nil {
		return false, err
	}
	if who == n.Author() {
		n.dropOwn(p, addr)
		return false, errOwnAddress
	}
	n.report(diag.Discovery, p.ID, addr, "add peer", n.addPeer(p, addr, who, "confirmed by announce"))
	n.learn(p, l, who, addr)
	defer n.left(p, l, who)
	n.listed(p, addr, others)
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

func (n *Node) announce(ctx context.Context, p *pile, addr string) (op.Author, []listedAddr, error) {
	var mu sync.Mutex
	var who op.Author
	c := http.Client{Timeout: announceTimeout, Transport: &http.Transport{
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
		return op.Author{}, nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return op.Author{}, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return op.Author{}, nil, errors.New(resp.Status)
	}
	var others []listedAddr
	if err := json.NewDecoder(resp.Body).Decode(&others); err != nil {
		return op.Author{}, nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	return who, others, nil
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
		e := p.entry
		e.Peers = map[string][]byte{}
		for a, k := range p.peers {
			e.Peers[a] = k[:]
		}
		entries[i] = e
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
