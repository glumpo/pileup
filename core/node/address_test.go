package node_test

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/peer"
)

type listed struct {
	Key  string `json:"key"`
	Addr string `json:"addr"`
}

func keyOf(a op.Author) string { return hex.EncodeToString(a[:]) }

func named(name string, a op.Author) string { return name + " (" + keyOf(a)[:8] + ")" }

func nodeAddr(t *testing.T, n *node.Node, id []byte) string {
	t.Helper()
	return inviterAddr(t, inviteTo(t, n, node.Pile{ID: id}))
}

func newKey(t *testing.T) (ed25519.PrivateKey, op.Author) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return key, op.Author(key.Public().(ed25519.PublicKey))
}

func announceAs(t *testing.T, key ed25519.PrivateKey, id, secret []byte, to string, port int) []listed {
	t.Helper()
	tr := &http.Transport{DialContext: func(ctx context.Context, _, a string) (net.Conn, error) {
		return peer.Dial(ctx, a, key, id, secret)
	}}
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	resp, err := c.Post("http://"+to+"/announce?port="+strconv.Itoa(port), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("announce: %s", resp.Status)
	}
	var out []listed
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("announce answer: %v", err)
	}
	return out
}

func listing(gate <-chan struct{}, entries func() []listed) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		json.NewEncoder(w).Encode(entries())
	}
}

func (s saved) listenOn(t *testing.T, port int) {
	t.Helper()
	path := filepath.Join(s.dir, "piles.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatal(err)
	}
	entries[0]["port"] = port
	if b, err = json.Marshal(entries); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func addressesAre(n *node.Node, id []byte, want ...string) func() bool {
	return func() bool {
		slices.Sort(want)
		peers := n.Peers(id)
		slices.Sort(peers)
		return slices.Equal(peers, want) && slices.Equal(n.Dialing(id), want)
	}
}

func syncedAt(t *testing.T, n *node.Node, id []byte, addrs ...string) {
	t.Helper()
	for _, a := range addrs {
		eventually(t, "sync with "+a, logged(n, diag.Sync, node.Pile{ID: id}, a, "sync established"))
	}
}

func TestMovedMemberOldAddressIsDropped(t *testing.T) {
	s := newSaved(t)
	o := newOwner(t, s.id, s.secret)
	old := o.serve(t, nil)
	s.write(t, s.history(t, 1), old.addr())
	n := s.open(t)
	syncedWith(t, n, s.id, o)
	pile := node.Pile{ID: s.id}

	old.stop()
	moved := o.serve(t, nil)
	o.announce(t, nodeAddr(t, n, s.id), portOf(moved.addr()))
	eventually(t, "only the new address is kept", addressesAre(n, s.id, moved.addr()))
	eventually(t, "the drop is logged", logged(n, diag.Discovery, pile, old.addr(), "dropped address of "+named("Вера", o.author())))
	eventually(t, "the fetch address is logged", logged(n, diag.Discovery, pile, moved.addr(), "files of "+named("Вера", o.author())+" are fetched from this address"))

	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = s.open(t)
	if got := n.Dialing(s.id); slices.Contains(got, old.addr()) {
		t.Errorf("after restart dialing %v, the dropped address %s is back", got, old.addr())
	}
}

func TestUndialableAnnouncedAddressOfConnectedMemberIsDropped(t *testing.T) {
	s := newSaved(t)
	o := newOwner(t, s.id, s.secret)
	at := o.serve(t, nil)
	s.write(t, s.history(t, 1), at.addr())
	n := s.open(t)
	syncedWith(t, n, s.id, o)

	closed := closedAddr(t)
	o.announce(t, nodeAddr(t, n, s.id), int(closed.Port()))
	eventually(t, "the undialable address is dropped", logged(n, diag.Discovery, node.Pile{ID: s.id}, closed.String(), "dropped address of "+named("Вера", o.author())))
	eventually(t, "the working address stays alone", addressesAre(n, s.id, at.addr()))
}

func TestWorkingAddressesOfOneMemberAreKept(t *testing.T) {
	s := newSaved(t)
	o := newOwner(t, s.id, s.secret)
	first, second := o.serve(t, nil), o.serve(t, nil)
	s.write(t, s.history(t, 1), first.addr(), second.addr())
	n := s.open(t)
	syncedAt(t, n, s.id, first.addr(), second.addr())

	o.announce(t, nodeAddr(t, n, s.id), portOf(second.addr()))
	syncedWith(t, n, s.id, o)
	if !addressesAre(n, s.id, first.addr(), second.addr())() {
		t.Errorf("peers %v, dialing %v; want both working addresses", n.Peers(s.id), n.Dialing(s.id))
	}
}

func TestSavedOldAddressIsDroppedAfterRestart(t *testing.T) {
	s := newSaved(t)
	o := newOwner(t, s.id, s.secret)
	first, second := o.serve(t, nil), o.serve(t, nil)
	s.write(t, s.history(t, 1), first.addr(), second.addr())
	n := s.open(t)
	syncedAt(t, n, s.id, first.addr(), second.addr())
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}

	first.stop()
	n = s.open(t)
	eventually(t, "the saved address that stopped answering is dropped", addressesAre(n, s.id, second.addr()))
	eventually(t, "the drop is logged", logged(n, diag.Discovery, node.Pile{ID: s.id}, first.addr(), "dropped address of "+named("Вера", o.author())))
}

func TestFilesMoveToWorkingAddressWhenFetchAddressIsDropped(t *testing.T) {
	s := newSaved(t)
	o := newOwner(t, s.id, s.secret)
	first, second := o.serve(t, nil), o.serve(t, nil)
	s.write(t, s.history(t, 1), first.addr())
	n := s.open(t)
	syncedWith(t, n, s.id, o)
	pile := node.Pile{ID: s.id}
	o.announce(t, nodeAddr(t, n, s.id), portOf(second.addr()))
	syncedAt(t, n, s.id, second.addr())

	second.stop()
	eventually(t, "the stopped address is dropped", logged(n, diag.Discovery, pile, second.addr(), "dropped address of "+named("Вера", o.author())))
	eventually(t, "the move of downloads is logged", logged(n, diag.Discovery, pile, first.addr(), "files of "+named("Вера", o.author())+" are fetched from this address instead of "+second.addr()))
	got, err := fetchWithin(n, s.id, o.photo, node.T2, 3*time.Second)
	if err != nil || string(got) != string(ownerBytes("2")) {
		t.Fatalf("T2 after the fetch address was dropped: %q, %v", got, err)
	}
	if f, sc := first.displays.Load(), second.displays.Load(); f != 1 || sc != 0 {
		t.Errorf("T2 requests: %d to the working address, %d to the dropped one; want 1 and 0", f, sc)
	}
}

func TestAnnounceAnswerListsOtherMembersWithKeys(t *testing.T) {
	s := newSaved(t)
	keyO, o := newKey(t)
	keyD, d := newKey(t)
	keyQ, q := newKey(t)
	atO := serveAs(t, keyO, s.id, s.secret, nil, nil)
	atD := serveAs(t, keyD, s.id, s.secret, nil, nil)
	atQ := serveAs(t, keyQ, s.id, s.secret, nil, nil)
	s.write(t, s.history(t, 1), atO.addr(), atD.addr(), atQ.addr())
	n := s.open(t)
	syncedAt(t, n, s.id, atO.addr(), atD.addr(), atQ.addr())

	got := announceAs(t, keyQ, s.id, s.secret, nodeAddr(t, n, s.id), portOf(atQ.addr()))
	want := []listed{{Key: keyOf(o), Addr: atO.addr()}, {Key: keyOf(d), Addr: atD.addr()}}
	order := func(a, b listed) int { return cmp.Compare(a.Key+a.Addr, b.Key+b.Addr) }
	slices.SortFunc(got, order)
	slices.SortFunc(want, order)
	if !slices.Equal(got, want) {
		t.Errorf("announce answer %v, want %v without the asker %s", got, want, keyOf(q))
	}
}

func TestListedAddressOfConnectedMemberIsNotAdded(t *testing.T) {
	s := newSaved(t)
	o := newOwner(t, s.id, s.secret)
	at := o.serve(t, nil)
	stale := closedAddr(t).String()
	gate := make(chan struct{})
	keyF, _ := newKey(t)
	f := serveAs(t, keyF, s.id, s.secret, nil, map[string]http.HandlerFunc{
		"POST /announce": listing(gate, func() []listed { return []listed{{Key: keyOf(o.author()), Addr: stale}} }),
	})
	s.write(t, append(s.history(t, 1), o.raws...), at.addr(), f.addr())
	n := s.open(t)
	syncedWith(t, n, s.id, o)

	close(gate)
	eventually(t, "the listed address is ignored", logged(n, diag.Discovery, node.Pile{ID: s.id}, stale, "ignored address of "+named("Вера", o.author())))
	syncedAt(t, n, s.id, f.addr())
	if peers, dialing := n.Peers(s.id), n.Dialing(s.id); slices.Contains(peers, stale) || slices.Contains(dialing, stale) {
		t.Errorf("peers %v, dialing %v; want no address %s of the connected member", peers, dialing, stale)
	}
}

func TestOwnAddressIsNotDialed(t *testing.T) {
	t.Run("listed by a member", func(t *testing.T) {
		s := newSaved(t)
		gate := make(chan struct{})
		var mine listed
		keyF, _ := newKey(t)
		f := serveAs(t, keyF, s.id, s.secret, nil, map[string]http.HandlerFunc{
			"POST /announce": listing(gate, func() []listed { return []listed{mine} }),
		})
		s.write(t, s.history(t, 1), f.addr())
		n := s.open(t)
		mine = listed{Key: keyOf(n.Author()), Addr: nodeAddr(t, n, s.id)}
		close(gate)

		eventually(t, "the own address is ignored", logged(n, diag.Discovery, node.Pile{ID: s.id}, mine.Addr, "ignored own address"))
		syncedAt(t, n, s.id, f.addr())
		if peers, dialing := n.Peers(s.id), n.Dialing(s.id); slices.Contains(peers, mine.Addr) || slices.Contains(dialing, mine.Addr) {
			t.Errorf("peers %v, dialing %v; want no own address %s", peers, dialing, mine.Addr)
		}
	})
	t.Run("saved", func(t *testing.T) {
		s := newSaved(t)
		port := int(closedAddr(t).Port())
		self := "127.0.0.1:" + strconv.Itoa(port)
		s.write(t, s.history(t, 1), self)
		s.listenOn(t, port)
		n := s.open(t)

		eventually(t, "the own address is dropped", logged(n, diag.Discovery, node.Pile{ID: s.id}, self, "this device answers here"))
		eventually(t, "no addresses are left", addressesAre(n, s.id))
	})
}

func listenAt(t *testing.T, addr string) *counting {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	c := &counting{Listener: ln}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestListedAddressOfUnknownMemberIsAdded(t *testing.T) {
	s := newSaved(t)
	o := newOwner(t, s.id, s.secret)
	at := o.serve(t, nil)
	keyF, _ := newKey(t)
	f := serveAs(t, keyF, s.id, s.secret, nil, map[string]http.HandlerFunc{
		"POST /announce": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode([]listed{{Key: keyOf(o.author()), Addr: at.addr()}})
		},
	})
	s.write(t, s.history(t, 1), f.addr())
	n := s.open(t)

	eventually(t, "the listed address is added with its source", logged(n, diag.Discovery, node.Pile{ID: s.id}, at.addr(), "new address of "+keyOf(o.author())[:8]+", listed by "+f.addr()))
	syncedWith(t, n, s.id, o)
	got, err := fetchWithin(n, s.id, o.photo, node.T2, 3*time.Second)
	if err != nil || string(got) != string(ownerBytes("2")) {
		t.Fatalf("T2 from the listed address: %q, %v", got, err)
	}
}

func TestAddressTakenOverByAnotherMemberIsReattributed(t *testing.T) {
	s := newSaved(t)
	first, second := newOwner(t, s.id, s.secret), newOwner(t, s.id, s.secret)
	old := first.serve(t, nil)
	addr := old.addr()
	s.write(t, s.history(t, 1), addr)
	n := s.open(t)
	syncedWith(t, n, s.id, first)

	old.stop()
	serveOn(t, listenAt(t, addr), second.key, s.id, s.secret, second.raws, nil)
	eventually(t, "the new owner of the address is logged", logged(n, diag.Discovery, node.Pile{ID: s.id}, addr,
		"address now answers as "+keyOf(second.author())[:8]+" instead of "+named("Вера", first.author())))
	eventually(t, "the new owner is online", func() bool { return n.Online(s.id)[second.author()] })
	if got := n.Peers(s.id); !slices.Equal(got, []string{addr}) {
		t.Errorf("peers %v, want the address kept for its new owner", got)
	}
}

func TestUnreadablePilesFileIsSetAside(t *testing.T) {
	for _, c := range []struct {
		name  string
		peers any
		raw   string
	}{
		{name: "not json", raw: "{broken"},
		{name: "no peers field"},
		{name: "list of addresses", peers: []string{"127.0.0.1:1"}},
		{name: "short key", peers: map[string][]byte{"127.0.0.1:1": {1, 2}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newSaved(t)
			s.write(t, s.history(t, 1))
			path := filepath.Join(s.dir, "piles.json")
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var entries []map[string]any
			if err := json.Unmarshal(b, &entries); err != nil {
				t.Fatal(err)
			}
			delete(entries[0], "peers")
			if c.peers != nil {
				entries[0]["peers"] = c.peers
			}
			if b, err = json.Marshal(entries); err != nil {
				t.Fatal(err)
			}
			if c.raw != "" {
				b = []byte(c.raw)
			}
			if err := os.WriteFile(path, b, 0o600); err != nil {
				t.Fatal(err)
			}

			n, err := node.Open(node.Config{Dir: s.dir, Keystore: device.File(filepath.Join(s.dir, "device.key")), Platform: "android", Addrs: loopback})
			if err != nil {
				t.Fatalf("node with an unreadable piles.json: %v", err)
			}
			t.Cleanup(func() { n.Close() })
			if ps, err := n.Piles(); err != nil || len(ps) != 0 {
				t.Errorf("piles %v, %v; want none", ps, err)
			}
			aside, _ := filepath.Glob(path + ".broken-*")
			if len(aside) != 1 {
				t.Fatalf("set aside %v, want one copy", aside)
			}
			if got, err := os.ReadFile(aside[0]); err != nil || string(got) != string(b) {
				t.Errorf("set aside copy %q, %v; want the unreadable file", got, err)
			}
			eventually(t, "the set aside file is logged", logged(n, diag.Sync, node.Pile{}, "", filepath.Base(aside[0])))
		})
	}
}
