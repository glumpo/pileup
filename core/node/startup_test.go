package node_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/peer"
	"github.com/glumpo/pileup/core/replica"
	"github.com/glumpo/pileup/core/store"
)

type saved struct {
	dir    string
	id     []byte
	secret []byte
	key    ed25519.PrivateKey
}

func newSaved(t *testing.T) saved {
	t.Helper()
	dir := t.TempDir()
	key, err := device.Key(device.File(filepath.Join(dir, "device.key")))
	if err != nil {
		t.Fatal(err)
	}
	return saved{dir: dir, id: bytes.Repeat([]byte{7}, 16), secret: bytes.Repeat([]byte{9}, 32), key: key}
}

func (s saved) author() op.Author { return op.Author(s.key.Public().(ed25519.PublicKey)) }

func (s saved) openStore(t *testing.T, path string) *store.Store {
	t.Helper()
	st, err := store.Open(path, s.id, s.secret)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func (s saved) history(t *testing.T, n int) [][]byte {
	t.Helper()
	st := s.openStore(t, filepath.Join(t.TempDir(), "history.db"))
	defer st.Close()
	changes := []op.Change{op.Join{Name: "Аня", Platform: "android", Proof: op.JoinProof(s.secret, s.author())}}
	for i := 2; i <= n; i++ {
		changes = append(changes, op.MemberSet{Name: fmt.Sprintf("Аня %d", i)})
	}
	for _, c := range changes {
		if _, err := st.Create(s.key, 1700000000000, c); err != nil {
			t.Fatal(err)
		}
	}
	raws, err := st.Ops(s.author(), 1)
	if err != nil {
		t.Fatal(err)
	}
	return raws
}

func (s saved) dbPath() string {
	return filepath.Join(s.dir, "piles", hex.EncodeToString(s.id)+".db")
}

func (s saved) write(t *testing.T, raws [][]byte, peers ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(s.dir, "piles"), 0o700); err != nil {
		t.Fatal(err)
	}
	st := s.openStore(t, s.dbPath())
	for _, raw := range raws {
		if _, err := st.Apply(raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal([]map[string]any{{"id": s.id, "secret": s.secret, "port": 0, "peers": peers}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "piles.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (s saved) own(t *testing.T) [][]byte {
	t.Helper()
	st := s.openStore(t, s.dbPath())
	defer st.Close()
	raws, err := st.Ops(s.author(), 1)
	if err != nil {
		t.Fatal(err)
	}
	return raws
}

func (s saved) open(t *testing.T) *node.Node {
	t.Helper()
	return openWith(t, s.dir, "android", shelf(photo("IMG_1", "2026-07-02T10:00:00Z")))
}

func (s saved) choose(ctx context.Context, n *node.Node) error {
	return n.Select(ctx, s.id, ms("2026-07-01T00:00:00Z"), ms("2026-07-10T00:00:00Z"))
}

func servePeer(t *testing.T, s saved, h http.Handler) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, key, _ := ed25519.GenerateKey(nil)
	srv := &http.Server{Handler: h}
	go srv.Serve(peer.NewListener(ln, key, func(id []byte) ([]byte, bool) { return s.secret, bytes.Equal(id, s.id) }))
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().String()
}

type slowPeer struct {
	addr  string
	asked chan struct{}
	gate  chan struct{}
}

func serveSlowPeer(t *testing.T, s saved, raws [][]byte) slowPeer {
	t.Helper()
	st := s.openStore(t, filepath.Join(t.TempDir(), "peer.db"))
	t.Cleanup(func() { st.Close() })
	for _, raw := range raws {
		if _, err := st.Apply(raw); err != nil {
			t.Fatal(err)
		}
	}
	_, key, _ := ed25519.GenerateKey(nil)
	sp := slowPeer{asked: make(chan struct{}, 1), gate: make(chan struct{})}
	mux := http.NewServeMux()
	mux.Handle("/", replica.New(st, key, s.id, s.secret).Handler())
	mux.HandleFunc("POST /announce", func(w http.ResponseWriter, r *http.Request) {
		select {
		case sp.asked <- struct{}{}:
		default:
		}
		select {
		case <-sp.gate:
			w.Write([]byte("[]"))
		case <-r.Context().Done():
		}
	})
	sp.addr = servePeer(t, s, mux)
	return sp
}

func result(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("local write did not finish")
		return nil
	}
}

func checkContinues(t *testing.T, s saved, history [][]byte) {
	t.Helper()
	got := s.own(t)
	if len(got) != len(history)+1 {
		t.Fatalf("own log has %d ops, want %d", len(got), len(history)+1)
	}
	for i, raw := range history {
		if !bytes.Equal(got[i], raw) {
			t.Fatalf("own op %d differs from the one kept by the peer", i+1)
		}
	}
	last, err := op.Decode(got[len(history)])
	if err != nil {
		t.Fatal(err)
	}
	head, err := op.Decode(history[len(history)-1])
	if err != nil {
		t.Fatal(err)
	}
	if last.Seq != head.Seq+1 || !bytes.Equal(last.Prev, head.ID[:]) || last.Type != op.TypeAssetAdd {
		t.Fatalf("new op: seq %d, prev %x, type %d; want seq %d after %x", last.Seq, last.Prev, last.Type, head.Seq+1, head.ID)
	}
}

func TestFirstWriteAfterRestartWaitsForKnownPeer(t *testing.T) {
	s := newSaved(t)
	history := s.history(t, 12)
	p := serveSlowPeer(t, s, history)
	s.write(t, history[:10], p.addr)
	n := s.open(t)

	<-p.asked
	done := make(chan error, 1)
	go func() { done <- s.choose(context.Background(), n) }()
	select {
	case err := <-done:
		t.Fatalf("local write finished before the start check of the known peer: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(p.gate)
	if err := result(t, done); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	checkContinues(t, s, history)
}

func TestWriteWithoutPeersIsImmediate(t *testing.T) {
	s := newSaved(t)
	history := s.history(t, 10)
	s.write(t, history)
	n := s.open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.choose(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	checkContinues(t, s, history)
}

func TestUnreachablePeerDoesNotBlockWrites(t *testing.T) {
	s := newSaved(t)
	history := s.history(t, 10)
	s.write(t, history, closedAddr(t).String())
	n := s.open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.choose(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	checkContinues(t, s, history)
}

func TestCancelledWriteLeavesStartCheckForOthers(t *testing.T) {
	s := newSaved(t)
	history := s.history(t, 12)
	p := serveSlowPeer(t, s, history)
	s.write(t, history[:10], p.addr)
	n := s.open(t)

	<-p.asked
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { first <- s.choose(ctx, n) }()
	cancel()
	if err := result(t, first); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write: err = %v, want context.Canceled", err)
	}
	second := make(chan error, 1)
	go func() { second <- s.choose(context.Background(), n) }()
	close(p.gate)
	if err := result(t, second); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	checkContinues(t, s, history)
}
