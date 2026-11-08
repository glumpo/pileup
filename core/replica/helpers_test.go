package replica_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/vv"
	"github.com/glumpo/pileup/core/peer"
	"github.com/glumpo/pileup/core/replica"
	"github.com/glumpo/pileup/core/store"
)

var (
	pile   = bytes.Repeat([]byte{0x50}, 32)
	secret = bytes.Repeat([]byte{0x53}, 32)
)

const wallTime = 1700000000000

func key(n byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{n}, ed25519.SeedSize))
}

func author(k ed25519.PrivateKey) op.Author {
	return op.Author(k.Public().(ed25519.PublicKey))
}

func join(k ed25519.PrivateKey, name string) op.Join {
	return op.Join{Name: name, Platform: "android", Proof: op.JoinProof(secret, author(k))}
}

func assets(ids ...string) op.AssetAdd {
	var c op.AssetAdd
	for _, id := range ids {
		c.Assets = append(c.Assets, op.AssetMeta{
			AssetID:      []byte(id),
			Revision:     []byte("r1"),
			Kind:         op.KindPhoto,
			MIME:         "image/jpeg",
			OriginalName: id + ".jpg",
		})
	}
	return c
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "pile.db"), pile, secret)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

type node struct {
	*replica.Node
	store *store.Store
	key   ed25519.PrivateKey
}

func nodeOn(s *store.Store, k ed25519.PrivateKey) *node {
	return &node{Node: replica.New(s, k, pile, secret), store: s, key: k}
}

func newNode(t *testing.T, k ed25519.PrivateKey, name string) *node {
	t.Helper()
	n := nodeOn(openStore(t), k)
	n.create(t, join(k, name))
	return n
}

func (n *node) create(t *testing.T, c op.Change) op.Op {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	o, err := n.Create(ctx, wallTime, c)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func (n *node) apply(t *testing.T, raws ...[]byte) {
	t.Helper()
	for _, raw := range raws {
		if _, err := n.store.Apply(raw); err != nil {
			t.Fatal(err)
		}
	}
}

func (n *node) vv(t *testing.T) vv.VV {
	t.Helper()
	v, err := n.store.VV()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (n *node) serve(t *testing.T, wrap func(http.Handler) http.Handler) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if wrap == nil {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { n.Serve(ctx, l); close(done) }()
		t.Cleanup(func() { cancel(); <-done })
		return l.Addr().String()
	}
	secrets := func(id []byte) ([]byte, bool) { return secret, bytes.Equal(id, pile) }
	srv := &http.Server{Handler: wrap(n.Handler())}
	go srv.Serve(peer.NewListener(l, n.key, secrets))
	t.Cleanup(func() { srv.Close() })
	return l.Addr().String()
}

type session struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func (n *node) pull(t *testing.T, addr string) *session {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &session{cancel: cancel, done: make(chan struct{})}
	go func() { s.err = n.Pull(ctx, addr); close(s.done) }()
	t.Cleanup(func() { cancel(); <-s.done })
	return s
}

func (s *session) stop(t *testing.T) {
	t.Helper()
	s.cancel()
	<-s.done
	if !errors.Is(s.err, context.Canceled) {
		t.Fatalf("pull ended with %v", s.err)
	}
}

func (s *session) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-s.done:
		return s.err
	case <-time.After(10 * time.Second):
		t.Fatal("pull did not end")
		return nil
	}
}

func waitFor(t *testing.T, what string, cond func() bool, live ...*session) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		for _, s := range live {
			select {
			case <-s.done:
				t.Fatalf("%s: pull ended with %v", what, s.err)
			default:
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: timed out", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func converged(t *testing.T, want vv.VV, nodes ...*node) func() bool {
	return func() bool {
		first, err := nodes[0].store.View()
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range nodes {
			v, err := n.store.View()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(v.VV, want) || !reflect.DeepEqual(v, first) {
				return false
			}
		}
		return true
	}
}

func frames(body []byte) ([][]byte, error) {
	var out [][]byte
	dec := cbor.NewDecoder(bytes.NewReader(body))
	for {
		var raw []byte
		if err := dec.Decode(&raw); err == io.EOF {
			return out, nil
		} else if err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
}

func rewriteOps(t *testing.T, h http.Handler, f func(r *http.Request, raws [][]byte) [][]byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		body := rec.Body.Bytes()
		if r.URL.Path == "/ops" && rec.Code == http.StatusOK {
			raws, err := frames(body)
			if err != nil {
				t.Error(err)
			}
			var b bytes.Buffer
			enc := cbor.NewEncoder(&b)
			for _, raw := range f(r, raws) {
				enc.Encode(raw)
			}
			body = b.Bytes()
		}
		w.WriteHeader(rec.Code)
		w.Write(body)
	})
}

type served struct {
	mu  sync.Mutex
	ops map[op.Author][]uint64
}

func (s *served) wrap(t *testing.T) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return rewriteOps(t, h, func(_ *http.Request, raws [][]byte) [][]byte {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.ops == nil {
				s.ops = map[op.Author][]uint64{}
			}
			for _, raw := range raws {
				o, err := op.Decode(raw)
				if err != nil {
					t.Error(err)
					continue
				}
				s.ops[o.Author] = append(s.ops[o.Author], o.Seq)
			}
			return raws
		})
	}
}

func (s *served) take() map[op.Author][]uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	got := s.ops
	s.ops = map[op.Author][]uint64{}
	return got
}

func history(t *testing.T, k ed25519.PrivateKey, n int) [][]byte {
	t.Helper()
	s := openStore(t)
	if _, err := s.Create(k, wallTime, join(k, "Аня")); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < n; i++ {
		if _, err := s.Create(k, wallTime, op.MemberSet{Name: string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	raws, err := s.Ops(author(k), 1)
	if err != nil {
		t.Fatal(err)
	}
	return raws
}

func id(t *testing.T, raw []byte) op.ID {
	t.Helper()
	o, err := op.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return o.ID
}
