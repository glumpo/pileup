package replica

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/vv"
	"github.com/glumpo/pileup/core/peer"
	"github.com/glumpo/pileup/core/store"
)

const (
	pollWait = 20 * time.Second
	timeout  = 20 * time.Second
)

var (
	ErrFork        = errors.New("replica: author log forked")
	ErrPeerChanged = errors.New("replica: another device answers at the address")
)

type Node struct {
	store  *store.Store
	key    ed25519.PrivateKey
	pileID []byte
	secret []byte

	mu       sync.Mutex
	changed  chan struct{}
	pending  int
	caughtUp bool
}

func New(s *store.Store, key ed25519.PrivateKey, pileID, secret []byte) *Node {
	return &Node{store: s, key: key, pileID: pileID, secret: secret, changed: make(chan struct{})}
}

func (n *Node) watch() <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.changed
}

func (n *Node) Changed() <-chan struct{} {
	return n.watch()
}

func (n *Node) notify() {
	n.mu.Lock()
	defer n.mu.Unlock()
	close(n.changed)
	n.changed = make(chan struct{})
}

func (n *Node) track(d int) {
	n.mu.Lock()
	n.pending += d
	n.mu.Unlock()
	n.notify()
}

func (n *Node) Hold() func() {
	n.track(1)
	var once sync.Once
	return func() { once.Do(func() { n.track(-1) }) }
}

func (n *Node) ready(ctx context.Context) error {
	for {
		n.mu.Lock()
		if n.pending == 0 {
			n.caughtUp = true
		}
		ready, ch := n.caughtUp, n.changed
		n.mu.Unlock()
		if ready {
			return nil
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (n *Node) Create(ctx context.Context, wallTime int64, c op.Change) (op.Op, error) {
	if err := n.ready(ctx); err != nil {
		return op.Op{}, err
	}
	o, err := n.store.Create(n.key, wallTime, c)
	if err == nil {
		n.notify()
	}
	return o, err
}

func (n *Node) Select(ctx context.Context, wallTime int64, sel store.Selection, changes ...op.Change) error {
	if err := n.ready(ctx); err != nil {
		return err
	}
	err := n.store.Select(n.key, wallTime, sel, changes...)
	if err == nil {
		n.notify()
	}
	return err
}

func (n *Node) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /vv", n.serveVV)
	mux.HandleFunc("POST /vv", n.servePoll)
	mux.HandleFunc("GET /ops", n.serveOps)
	return mux
}

func (n *Node) Serve(ctx context.Context, inner net.Listener) error {
	l := peer.NewListener(inner, n.key, func(id []byte) ([]byte, bool) {
		return n.secret, bytes.Equal(id, n.pileID)
	})
	srv := &http.Server{Handler: n.Handler(), ReadHeaderTimeout: timeout, IdleTimeout: timeout}
	defer context.AfterFunc(ctx, func() { srv.Close() })()
	return srv.Serve(l)
}

func (n *Node) serveVV(w http.ResponseWriter, r *http.Request) {
	v, err := n.store.VV()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeVV(w, v)
}

func (n *Node) servePoll(w http.ResponseWriter, r *http.Request) {
	var known vv.VV
	if err := cbor.NewDecoder(r.Body).Decode(&known); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t := time.NewTimer(pollWait)
	defer t.Stop()
	for {
		ch := n.watch()
		v, err := n.store.VV()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(vv.Missing(known, v)) > 0 {
			writeVV(w, v)
			return
		}
		select {
		case <-ch:
		case <-t.C:
			writeVV(w, v)
			return
		case <-r.Context().Done():
			return
		}
	}
}

func writeVV(w http.ResponseWriter, v vv.VV) {
	w.Header().Set("Content-Type", "application/cbor")
	cbor.NewEncoder(w).Encode(v)
}

func (n *Node) serveOps(w http.ResponseWriter, r *http.Request) {
	var a op.Author
	b, err := hex.DecodeString(r.URL.Query().Get("author"))
	if err == nil {
		err = a.UnmarshalBinary(b)
	}
	from, ferr := strconv.ParseUint(r.URL.Query().Get("from"), 10, 64)
	if err = errors.Join(err, ferr); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	raws, err := n.store.Ops(a, from)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/cbor-seq")
	enc := cbor.NewEncoder(w)
	for _, raw := range raws {
		if enc.Encode(raw) != nil {
			return
		}
	}
}

type remote struct {
	http *http.Client
	base string
}

func (p remote) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("replica: %s %s: %s", method, path, resp.Status)
	}
	return resp, nil
}

func (p remote) vv(ctx context.Context, mine vv.VV) (vv.VV, error) {
	method, body := http.MethodGet, []byte(nil)
	if mine != nil {
		b, err := cbor.Marshal(mine)
		if err != nil {
			return nil, err
		}
		method, body = http.MethodPost, b
	}
	resp, err := p.do(ctx, method, "/vv", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var v vv.VV
	return v, cbor.NewDecoder(resp.Body).Decode(&v)
}

func (n *Node) Pull(ctx context.Context, addr string, synced func(ops int)) error {
	var mu sync.Mutex
	var from ed25519.PublicKey
	tr := &http.Transport{DialContext: func(ctx context.Context, _, a string) (net.Conn, error) {
		c, err := peer.Dial(ctx, a, n.key, n.pileID, n.secret)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		defer mu.Unlock()
		if from == nil {
			from = c.Peer()
		} else if !from.Equal(c.Peer()) {
			c.Close()
			return nil, ErrPeerChanged
		}
		return c, nil
	}}
	defer tr.CloseIdleConnections()
	p := remote{http: &http.Client{Transport: tr, Timeout: pollWait + timeout}, base: "http://" + addr}
	n.track(1)
	first := true
	defer func() {
		if first {
			n.track(-1)
		}
	}()
	theirs, err := p.vv(ctx, nil)
	for err == nil {
		var ops int
		if ops, err = n.round(ctx, p, theirs); err != nil {
			break
		}
		synced(ops)
		if first {
			first = false
			n.track(-1)
		}
		var mine vv.VV
		if mine, err = n.store.VV(); err == nil {
			theirs, err = p.vv(ctx, mine)
		}
	}
	return err
}

func (n *Node) round(ctx context.Context, p remote, theirs vv.VV) (int, error) {
	mine, err := n.store.VV()
	if err != nil {
		return 0, err
	}
	total := 0
	var errs []error
	for a, r := range vv.Missing(mine, theirs) {
		ops, err := n.fetch(ctx, p, a, r.From)
		total += ops
		errs = append(errs, err)
	}
	return total, errors.Join(errs...)
}

func (n *Node) fetch(ctx context.Context, p remote, a op.Author, from uint64) (int, error) {
	q := url.Values{"author": {hex.EncodeToString(a[:])}, "from": {strconv.FormatUint(from, 10)}}
	resp, err := p.do(ctx, http.MethodGet, "/ops?"+q.Encode(), nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	defer n.notify()
	dec := cbor.NewDecoder(resp.Body)
	for ops := 0; ; ops++ {
		var raw []byte
		if err := dec.Decode(&raw); err == io.EOF {
			return ops, nil
		} else if err != nil {
			return ops, err
		}
		if err := n.apply(raw); err != nil {
			return ops, err
		}
	}
}

func (n *Node) apply(raw []byte) error {
	o, err := op.Decode(raw)
	if err != nil {
		return err
	}
	_, err = n.store.Apply(raw)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, op.ErrPrev) && o.Seq > 1:
		return fork(o)
	case errors.Is(err, op.ErrSeq):
		id, ok, qerr := n.store.ID(o.Author, o.Seq)
		switch {
		case qerr != nil:
			return qerr
		case !ok:
			return err
		case id != o.ID:
			return fork(o)
		}
		return nil
	}
	return err
}

func fork(o op.Op) error {
	return fmt.Errorf("%w: author %x, seq %d", ErrFork, o.Author[:], o.Seq)
}
