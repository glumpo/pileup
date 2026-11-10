package node_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/crdt/vv"
	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/peer"
)

type pauses struct {
	mu  sync.Mutex
	got []time.Duration
}

func (p *pauses) list() []time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.got)
}

func instantPauses(t *testing.T) *pauses {
	t.Helper()
	p := &pauses{}
	t.Cleanup(node.SetPause(func(ctx context.Context, d time.Duration) error {
		p.mu.Lock()
		p.got = append(p.got, d)
		p.mu.Unlock()
		return ctx.Err()
	}))
	return p
}

func endlessPauses(t *testing.T) <-chan struct{} {
	t.Helper()
	pausing := make(chan struct{}, 1)
	t.Cleanup(node.SetPause(func(ctx context.Context, d time.Duration) error {
		signal(pausing)
		<-ctx.Done()
		return ctx.Err()
	}))
	return pausing
}

func linear(from, to int) []time.Duration {
	var out []time.Duration
	for i := from; i <= to; i++ {
		out = append(out, time.Duration(i)*10*time.Millisecond)
	}
	return out
}

type counting struct {
	net.Listener
	accepted atomic.Int32
}

func (c *counting) Accept() (net.Conn, error) {
	conn, err := c.Listener.Accept()
	if err == nil {
		c.accepted.Add(1)
	}
	return conn, err
}

func listenCounting(t *testing.T) *counting {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := &counting{Listener: ln}
	t.Cleanup(func() { c.Close() })
	return c
}

func serveRefusing(t *testing.T) *counting {
	t.Helper()
	c := listenCounting(t)
	_, key, _ := ed25519.GenerateKey(nil)
	srv := &http.Server{Handler: http.NotFoundHandler()}
	go srv.Serve(peer.NewListener(c, key, func([]byte) ([]byte, bool) { return []byte("another secret"), true }))
	t.Cleanup(func() { srv.Close() })
	return c
}

func selfSigned(t *testing.T, key crypto.Signer) tls.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Unix(0, 0), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func serveImpostor(t *testing.T, cert tls.Certificate) *counting {
	t.Helper()
	c := listenCounting(t)
	cfg := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequireAnyClientCert}
	go func() {
		for {
			conn, err := c.Accept()
			if err != nil {
				return
			}
			go func() {
				tc := tls.Server(conn, cfg)
				defer tc.Close()
				size := make([]byte, 1)
				if _, err := io.ReadFull(tc, size); err != nil {
					return
				}
				if _, err := io.ReadFull(tc, make([]byte, int(size[0])+32)); err != nil {
					return
				}
				tc.Write(make([]byte, 32))
				io.Copy(io.Discard, tc)
			}()
		}
	}()
	return c
}

func stopped(t *testing.T, n *node.Node, id []byte) {
	t.Helper()
	eventually(t, "connection loops end", func() bool { return len(n.Dialing(id)) == 0 })
}

func received(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: timed out", what)
	}
}

func portOf(addr string) int { return int(netip.MustParseAddrPort(addr).Port()) }

func TestAmbiguousFailuresAreRetriedHundredTimesWithLinearPauses(t *testing.T) {
	ps := instantPauses(t)
	refusing := serveRefusing(t)
	s := newSaved(t)
	s.write(t, s.history(t, 1), refusing.Addr().String())
	n := s.open(t)

	stopped(t, n, s.id)
	if got := refusing.accepted.Load(); got != 101 {
		t.Errorf("%d connection attempts, want the first one and 100 retries", got)
	}
	if got := ps.list(); !slices.Equal(got, linear(1, 100)) {
		t.Errorf("pauses %v, want 10ms, 20ms … 1s", got)
	}
}

func TestAuthenticityErrorStopsSeriesWithoutPull(t *testing.T) {
	_, edKey, _ := ed25519.GenerateKey(nil)
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		key  crypto.Signer
		want error
	}{
		{"certificate", ecKey, peer.ErrCertificate},
		{"membership", edKey, peer.ErrMembership},
	} {
		t.Run(c.name, func(t *testing.T) {
			ps := instantPauses(t)
			impostor := serveImpostor(t, selfSigned(t, c.key))
			addr := impostor.Addr().String()
			s := newSaved(t)
			s.write(t, s.history(t, 1), addr)
			n := s.open(t)

			stopped(t, n, s.id)
			eventually(t, "the error is logged", logged(n, diag.Connection, node.Pile{ID: s.id}, addr, c.want.Error()))
			if got := impostor.accepted.Load(); got != 1 {
				t.Errorf("%d connections, want only the failed announce", got)
			}
			if got := ps.list(); len(got) != 0 {
				t.Errorf("pauses %v, want none", got)
			}
		})
	}
}

func TestSeriesRestartsOnlyAfterSuccessfulSync(t *testing.T) {
	ps := instantPauses(t)
	s := newSaved(t)
	var announces, asks atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /announce", func(w http.ResponseWriter, r *http.Request) {
		announces.Add(1)
		w.Write([]byte("[]"))
	})
	mux.HandleFunc("GET /vv", func(w http.ResponseWriter, r *http.Request) {
		if asks.Add(1) != 51 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		b, _ := cbor.Marshal(vv.VV{})
		w.Write(b)
	})
	mux.HandleFunc("POST /vv", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusServiceUnavailable)
	})
	addr := servePeer(t, s.id, s.secret, mux)
	s.write(t, s.history(t, 1), addr)
	n := s.open(t)

	stopped(t, n, s.id)
	if got := announces.Load(); got != 151 {
		t.Errorf("%d attempts, want 51 until the successful round and 100 retries after it", got)
	}
	if got, want := ps.list(), append(linear(1, 50), linear(1, 100)...); !slices.Equal(got, want) {
		t.Errorf("pauses %v, want %v", got, want)
	}
}

type candidate struct {
	n    *node.Node
	pile node.Pile
	lan  *lan
}

func discover(t *testing.T, addr func(id, secret []byte) string) candidate {
	t.Helper()
	b := &bus{}
	n := openOn(t, t.TempDir(), "android", b.join("127.0.0.1"))
	p := create(t, n, "Грузия", "Аня")
	c := candidate{n: n, pile: p, lan: b.join("127.0.0.1")}
	a := addr(p.ID, secretOf(t, n, p))
	c.lan.Advertise("_pileup._tcp", "pileup-candidate", portOf(a), txt("pile="+pileHash(p.ID)))
	return c
}

func TestLostCancelsUnconfirmedCandidate(t *testing.T) {
	t.Run("request", func(t *testing.T) {
		var sp slowPeer
		c := discover(t, func(id, secret []byte) string {
			sp = serveSlowPeer(t, id, secret, nil)
			return sp.addr
		})
		received(t, sp.asked, "announce to the candidate")
		c.lan.Stop()
		received(t, sp.gone, "announce cancelled")
		stopped(t, c.n, c.pile.ID)
		if got := c.n.Peers(c.pile.ID); slices.Contains(got, sp.addr) {
			t.Errorf("peers %v keep the unconfirmed candidate", got)
		}
	})
	t.Run("pause", func(t *testing.T) {
		pausing := endlessPauses(t)
		var refusing *counting
		c := discover(t, func(id, secret []byte) string {
			refusing = serveRefusing(t)
			return refusing.Addr().String()
		})
		received(t, pausing, "pause after the failed attempt")
		c.lan.Stop()
		stopped(t, c.n, c.pile.ID)
		if got := refusing.accepted.Load(); got != 1 {
			t.Errorf("%d attempts, want 1", got)
		}
	})
}

func TestRepeatedFoundStartsNoParallelLoops(t *testing.T) {
	var sp slowPeer
	c := discover(t, func(id, secret []byte) string {
		sp = serveSlowPeer(t, id, secret, nil)
		return sp.addr
	})
	received(t, sp.asked, "announce to the candidate")
	for range 2 {
		c.lan.Advertise("_pileup._tcp", "pileup-candidate", portOf(sp.addr), txt("pile="+pileHash(c.pile.ID)))
	}
	if got := c.n.Dialing(c.pile.ID); !slices.Equal(got, []string{sp.addr}) {
		t.Errorf("dialing %v, want one loop to %s", got, sp.addr)
	}
	c.lan.Stop()
	received(t, sp.gone, "announce cancelled")
	stopped(t, c.n, c.pile.ID)
	if got := sp.asks.Load(); got != 1 {
		t.Errorf("%d announces, want 1", got)
	}
}

func TestOnlyNewDiscoveryStartsNewSeries(t *testing.T) {
	instantPauses(t)
	var refusing *counting
	c := discover(t, func(id, secret []byte) string {
		refusing = serveRefusing(t)
		return refusing.Addr().String()
	})
	addr := refusing.Addr().String()
	advertise := func() {
		c.lan.Advertise("_pileup._tcp", "pileup-candidate", portOf(addr), txt("pile="+pileHash(c.pile.ID)))
	}
	stopped(t, c.n, c.pile.ID)
	if got := refusing.accepted.Load(); got != 101 {
		t.Fatalf("%d attempts in the first series, want 101", got)
	}

	advertise()
	if got := c.n.Dialing(c.pile.ID); len(got) != 0 {
		t.Fatalf("repeated Found started %v", got)
	}

	c.lan.Stop()
	advertise()
	eventually(t, "second series ends", func() bool {
		return refusing.accepted.Load() == 202 && len(c.n.Dialing(c.pile.ID)) == 0
	})
}

func openFull(t *testing.T, dir, platform string, lib node.MediaProvider, l *lan) *node.Node {
	t.Helper()
	n, err := node.Open(node.Config{
		Dir:       dir,
		Keystore:  device.File(filepath.Join(dir, "device.key")),
		Platform:  platform,
		Addrs:     loopback,
		Media:     lib,
		Discovery: l,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	return n
}

func TestLostKeepsConnectionToConfirmedMember(t *testing.T) {
	b := &bus{}
	la, lb := b.join("127.0.0.1"), b.join("127.0.0.1")
	anya := openOn(t, t.TempDir(), "android", la)
	borya := openFull(t, t.TempDir(), "ios", shelf(photo("IMG_1", "2026-07-02T10:00:00Z")), lb)
	p := create(t, anya, "Грузия", "Аня")
	join(t, borya, inviteTo(t, anya, p), "Боря")
	waitConverged(t, p.ID, node.Pile{ID: p.ID, Name: "Грузия", Members: map[op.Author]state.Member{
		anya.Author():  {Name: "Аня", Platform: "android"},
		borya.Author(): {Name: "Боря", Platform: "ios"},
	}}, anya, borya)
	eventually(t, "Аня sees Боря online", func() bool { return anya.Online(p.ID)[borya.Author()] })

	lb.Stop()
	choose(t, borya, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")
	eventually(t, "Аня receives the file of Боря", func() bool { return len(assetsOn(t, anya, p.ID)) == 1 })
	if !anya.Online(p.ID)[borya.Author()] {
		t.Error("Боря went offline for Аня after Lost")
	}
}

func TestReturningMemberIsSyncedAfterSeriesEnded(t *testing.T) {
	instantPauses(t)
	dirB := t.TempDir()
	lib := shelf(photo("IMG_1", "2026-07-02T10:00:00Z"))
	anya := open(t, t.TempDir(), "android")
	borya := openWith(t, dirB, "ios", lib)
	p := create(t, anya, "Грузия", "Аня")
	join(t, borya, inviteTo(t, anya, p), "Боря")
	waitConverged(t, p.ID, node.Pile{ID: p.ID, Name: "Грузия", Members: map[op.Author]state.Member{
		anya.Author():  {Name: "Аня", Platform: "android"},
		borya.Author(): {Name: "Боря", Platform: "ios"},
	}}, anya, borya)
	if err := borya.Close(); err != nil {
		t.Fatal(err)
	}
	stopped(t, anya, p.ID)

	borya = openWith(t, dirB, "ios", lib)
	choose(t, borya, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")
	eventually(t, "Аня receives the file of Боря", func() bool { return len(assetsOn(t, anya, p.ID)) == 1 })
}

func TestCloseCancelsConnectionLoops(t *testing.T) {
	pausing := endlessPauses(t)
	s := newSaved(t)
	sp := serveSlowPeer(t, s.id, s.secret, nil)
	refusing := serveRefusing(t)
	s.write(t, s.history(t, 1), sp.addr, refusing.Addr().String())
	n := s.open(t)
	received(t, sp.asked, "announce")
	received(t, pausing, "pause")

	closed := make(chan struct{})
	go func() {
		n.Close()
		close(closed)
	}()
	received(t, closed, "Close")
	received(t, sp.gone, "announce cancelled")
}
