package node_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/peer"
	"github.com/glumpo/pileup/core/replica"
	"github.com/glumpo/pileup/core/store"
)

type owner struct {
	key        ed25519.PrivateKey
	id, secret []byte
	raws       [][]byte
	photo      op.AssetMeta
}

func newOwner(t *testing.T, id, secret []byte) owner {
	t.Helper()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	o := owner{key: key, id: id, secret: secret}
	o.photo = op.AssetMeta{
		AssetID:       bytes.Repeat([]byte{1}, 32),
		Revision:      bytes.Repeat([]byte{2}, 32),
		Kind:          op.KindPhoto,
		TakenAt:       ms(day),
		TakenAtSource: op.SourcePlatform,
		MIME:          "image/jpeg",
		OriginalName:  "IMG_1.JPG",
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "owner.db"), id, secret)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, c := range []op.Change{
		op.Join{Name: "Вера", Platform: "ios", Proof: op.JoinProof(secret, o.author())},
		op.AssetAdd{Assets: []op.AssetMeta{o.photo}},
	} {
		if _, err := st.Create(key, 1700000000000, c); err != nil {
			t.Fatal(err)
		}
	}
	if o.raws, err = st.Ops(o.author(), 1); err != nil {
		t.Fatal(err)
	}
	return o
}

func (o owner) author() op.Author { return op.Author(o.key.Public().(ed25519.PublicKey)) }

func ownerBytes(tier string) []byte { return []byte("T" + tier + " from the owner") }

type site struct {
	*counting
	displays atomic.Int32
	srv      *http.Server
}

func (s *site) addr() string { return s.Addr().String() }

func (s *site) stop() { s.srv.Close() }

func serveAs(t *testing.T, key ed25519.PrivateKey, id, secret []byte, raws [][]byte, routes map[string]http.HandlerFunc) *site {
	t.Helper()
	return serveOn(t, listenCounting(t), key, id, secret, raws, routes)
}

func serveOn(t *testing.T, ln *counting, key ed25519.PrivateKey, id, secret []byte, raws [][]byte, routes map[string]http.HandlerFunc) *site {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "site.db"), id, secret)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, raw := range raws {
		if _, err := st.Apply(raw); err != nil {
			t.Fatal(err)
		}
	}
	s := &site{counting: ln}
	h := map[string]http.HandlerFunc{
		"POST /announce": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("[]")) },
		"GET /blob/{asset}/{revision}/{tier}": func(w http.ResponseWriter, r *http.Request) {
			w.Write(ownerBytes(r.PathValue("tier")))
		},
	}
	maps.Copy(h, routes)
	mux := http.NewServeMux()
	mux.Handle("/", replica.New(st, key, id, secret).Handler())
	for pattern, f := range h {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if r.PathValue("tier") == "2" {
				s.displays.Add(1)
			}
			f(w, r)
		})
	}
	s.srv = &http.Server{Handler: mux}
	go s.srv.Serve(peer.NewListener(s, key, func(got []byte) ([]byte, bool) { return secret, bytes.Equal(got, id) }))
	t.Cleanup(func() { s.srv.Close() })
	return s
}

func (o owner) serve(t *testing.T, routes map[string]http.HandlerFunc) *site {
	t.Helper()
	return serveAs(t, o.key, o.id, o.secret, o.raws, routes)
}

func (o owner) announce(t *testing.T, to string, port int) {
	t.Helper()
	tr := &http.Transport{DialContext: func(ctx context.Context, _, a string) (net.Conn, error) {
		return peer.Dial(ctx, a, o.key, o.id, o.secret)
	}}
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	resp, err := c.Post("http://"+to+"/announce?port="+strconv.Itoa(port), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("announce: %s", resp.Status)
	}
}

func listenSilent(t *testing.T) *counting {
	t.Helper()
	c := listenCounting(t)
	go func() {
		for {
			conn, err := c.Accept()
			if err != nil {
				return
			}
			go func() {
				io.Copy(io.Discard, conn)
				conn.Close()
			}()
		}
	}()
	return c
}

func fetchWithin(n *node.Node, id []byte, m op.AssetMeta, tier int, d time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	f, _, err := n.Blob(ctx, id, m.AssetID, m.Revision, tier)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func syncedWith(t *testing.T, n *node.Node, id []byte, o owner) {
	t.Helper()
	eventually(t, "sync with the owner", func() bool { return n.Online(id)[o.author()] && len(assetsOn(t, n, id)) == 1 })
}

func TestFetchGoesOnlyToOwnerAddress(t *testing.T) {
	endlessPauses(t)
	s := newSaved(t)
	o := newOwner(t, s.id, s.secret)
	at := o.serve(t, nil)
	silent := []*counting{listenSilent(t), listenSilent(t), listenSilent(t)}
	peers := []string{closedAddr(t).String(), closedAddr(t).String()}
	for _, c := range silent {
		peers = append(peers, c.Addr().String())
	}
	s.write(t, s.history(t, 1), append(peers, at.addr())...)
	n := s.open(t)
	syncedWith(t, n, s.id, o)
	for i, c := range silent {
		eventually(t, fmt.Sprintf("sync attempt to silent address %d", i), func() bool { return c.accepted.Load() == 1 })
	}

	got, err := fetchWithin(n, s.id, o.photo, node.T2, 3*time.Second)
	if err != nil || !bytes.Equal(got, ownerBytes("2")) {
		t.Fatalf("T2 of the owner: %q, %v", got, err)
	}
	for i, c := range silent {
		if got := c.accepted.Load(); got != 1 {
			t.Errorf("silent address %d accepted %d connections, want only the sync attempt", i, got)
		}
	}
}

func TestFetchFromOwnerWithUnknownAddressFailsWithoutConnecting(t *testing.T) {
	endlessPauses(t)
	s := newSaved(t)
	o := newOwner(t, s.id, s.secret)
	silent := []*counting{listenSilent(t), listenSilent(t)}
	s.write(t, append(s.history(t, 1), o.raws...), silent[0].Addr().String(), silent[1].Addr().String())
	n := s.open(t)
	for i, c := range silent {
		eventually(t, fmt.Sprintf("sync attempt to silent address %d", i), func() bool { return c.accepted.Load() == 1 })
	}

	if _, err := fetchWithin(n, s.id, o.photo, node.T2, 3*time.Second); !errors.Is(err, node.ErrUnavailable) {
		t.Fatalf("T2 of an owner with no known address: err = %v, want ErrUnavailable", err)
	}
	for i, c := range silent {
		if got := c.accepted.Load(); got != 1 {
			t.Errorf("silent address %d accepted %d connections, want only the sync attempt", i, got)
		}
	}
}

func TestFetchFollowsOwnerToNewAddress(t *testing.T) {
	s := newSaved(t)
	o := newOwner(t, s.id, s.secret)
	old := o.serve(t, nil)
	s.write(t, s.history(t, 1), old.addr())
	n := s.open(t)
	syncedWith(t, n, s.id, o)
	if got, err := fetchWithin(n, s.id, o.photo, node.T1, 3*time.Second); err != nil || !bytes.Equal(got, ownerBytes("1")) {
		t.Fatalf("T1 from the old address: %q, %v", got, err)
	}

	moved := o.serve(t, nil)
	o.announce(t, inviterAddr(t, inviteTo(t, n, node.Pile{ID: s.id})), portOf(moved.addr()))
	eventually(t, "sync with the new address", logged(n, diag.Sync, node.Pile{ID: s.id}, moved.addr(), "sync established"))
	got, err := fetchWithin(n, s.id, o.photo, node.T2, 3*time.Second)
	if err != nil || !bytes.Equal(got, ownerBytes("2")) {
		t.Fatalf("T2 after the move: %q, %v", got, err)
	}
	if o, m := old.displays.Load(), moved.displays.Load(); o != 0 || m != 1 {
		t.Errorf("T2 requests: %d to the old address, %d to the new one; want 0 and 1", o, m)
	}
}

func TestIncomingAnnounceAloneDoesNotSetOwnerAddress(t *testing.T) {
	t.Run("undialable address keeps the working one", func(t *testing.T) {
		s := newSaved(t)
		o := newOwner(t, s.id, s.secret)
		at := o.serve(t, nil)
		s.write(t, s.history(t, 1), at.addr())
		n := s.open(t)
		syncedWith(t, n, s.id, o)
		if got, err := fetchWithin(n, s.id, o.photo, node.T1, 3*time.Second); err != nil || !bytes.Equal(got, ownerBytes("1")) {
			t.Fatalf("T1 from the working address: %q, %v", got, err)
		}

		o.announce(t, inviterAddr(t, inviteTo(t, n, node.Pile{ID: s.id})), int(closedAddr(t).Port()))
		got, err := fetchWithin(n, s.id, o.photo, node.T2, 3*time.Second)
		if err != nil || !bytes.Equal(got, ownerBytes("2")) {
			t.Fatalf("T2 after an announce from an undialable address: %q, %v", got, err)
		}
		if d := at.displays.Load(); d != 1 {
			t.Errorf("%d T2 requests to the working address, want 1", d)
		}
	})
	t.Run("incoming contact becomes fetchable after our announce", func(t *testing.T) {
		s := newSaved(t)
		o := newOwner(t, s.id, s.secret)
		asked, gate := make(chan struct{}, 1), make(chan struct{})
		at := o.serve(t, map[string]http.HandlerFunc{
			"POST /announce": func(w http.ResponseWriter, r *http.Request) {
				signal(asked)
				select {
				case <-gate:
					w.Write([]byte("[]"))
				case <-r.Context().Done():
				}
			},
		})
		s.write(t, append(s.history(t, 1), o.raws...))
		n := s.open(t)

		o.announce(t, inviterAddr(t, inviteTo(t, n, node.Pile{ID: s.id})), portOf(at.addr()))
		received(t, asked, "our announce to the announced address")
		if _, err := fetchWithin(n, s.id, o.photo, node.T2, 3*time.Second); !errors.Is(err, node.ErrUnavailable) {
			t.Fatalf("T2 before our announce succeeded: err = %v, want ErrUnavailable", err)
		}
		if d := at.displays.Load(); d != 0 {
			t.Fatalf("%d T2 requests before our announce succeeded, want 0", d)
		}

		close(gate)
		eventually(t, "sync with the announced address", logged(n, diag.Sync, node.Pile{ID: s.id}, at.addr(), "sync established"))
		got, err := fetchWithin(n, s.id, o.photo, node.T2, 3*time.Second)
		if err != nil || !bytes.Equal(got, ownerBytes("2")) {
			t.Fatalf("T2 after our announce: %q, %v", got, err)
		}
	})
}

func TestBodyIsLimitedByReadIdleTimeoutNotByTotalTime(t *testing.T) {
	const idle = 500 * time.Millisecond
	t.Cleanup(node.SetFetchTimeouts(time.Second, idle))
	chunk := bytes.Repeat([]byte{7}, 1000)
	for _, c := range []struct {
		name  string
		serve http.HandlerFunc
		check func(t *testing.T, got []byte, err error)
	}{
		{
			name: "steady",
			serve: func(w http.ResponseWriter, r *http.Request) {
				for range 40 {
					w.Write(chunk)
					w.(http.Flusher).Flush()
					time.Sleep(idle / 10)
				}
			},
			check: func(t *testing.T, got []byte, err error) {
				if err != nil || !bytes.Equal(got, bytes.Repeat(chunk, 40)) {
					t.Fatalf("steady body for twice the header limit: %d bytes, %v", len(got), err)
				}
			},
		},
		{
			name: "stalled",
			serve: func(w http.ResponseWriter, r *http.Request) {
				w.Write(chunk)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			},
			check: func(t *testing.T, got []byte, err error) {
				if !errors.Is(err, node.ErrUnavailable) {
					t.Fatalf("stalled body: %d bytes, err = %v, want ErrUnavailable", len(got), err)
				}
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newSaved(t)
			o := newOwner(t, s.id, s.secret)
			at := o.serve(t, map[string]http.HandlerFunc{
				"GET /blob/{asset}/{revision}/{tier}": func(w http.ResponseWriter, r *http.Request) {
					if r.PathValue("tier") != "2" {
						w.Write(ownerBytes(r.PathValue("tier")))
						return
					}
					c.serve(w, r)
				},
			})
			s.write(t, s.history(t, 1), at.addr())
			n := s.open(t)
			syncedWith(t, n, s.id, o)
			got, err := fetchWithin(n, s.id, o.photo, node.T2, 10*time.Second)
			c.check(t, got, err)
		})
	}
}

func TestInviterFilesAreFetchedFromInviterRightAfterJoin(t *testing.T) {
	id, secret := bytes.Repeat([]byte{5}, 16), bytes.Repeat([]byte{6}, 32)
	o := newOwner(t, id, secret)
	inviter := o.serve(t, map[string]http.HandlerFunc{
		"POST /announce": func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() },
	})
	_, relayKey, _ := ed25519.GenerateKey(nil)
	relay := serveAs(t, relayKey, id, secret, o.raws, nil)
	b := &bus{}
	n := openOn(t, t.TempDir(), "android", b.join("127.0.0.1"))
	inv := invite.Invite{PileID: id, Secret: secret, Addrs: []netip.AddrPort{netip.MustParseAddrPort(inviter.addr())}}
	join(t, n, inv.String(), "Боря")
	b.join("127.0.0.1").Advertise("_pileup._tcp", "pileup-relay", portOf(relay.addr()), txt("pile="+pileHash(id)))
	eventually(t, "the inviter's photo arrives through the relay", func() bool { return len(assetsOn(t, n, id)) == 1 })

	got, err := fetchWithin(n, id, o.photo, node.T2, 3*time.Second)
	if err != nil || !bytes.Equal(got, ownerBytes("2")) {
		t.Fatalf("T2 of the inviter: %q, %v", got, err)
	}
	if i, r := inviter.displays.Load(), relay.displays.Load(); i != 1 || r != 0 {
		t.Errorf("T2 requests: %d to the inviter, %d to the relay; want 1 and 0", i, r)
	}
}
