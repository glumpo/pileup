package node_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/node"
)

type ad struct {
	from          *lan
	service, name string
	port          int
	txt           []byte
}

type watch struct {
	from    *lan
	service string
	handler node.DiscoveryHandler
}

type bus struct {
	mu      sync.Mutex
	ads     []ad
	watches []watch
}

type lan struct {
	bus  *bus
	host string
}

func (b *bus) join(host string) *lan { return &lan{bus: b, host: host} }

func (l *lan) Advertise(service, name string, port int, txt []byte) error {
	b := l.bus
	b.mu.Lock()
	a := ad{from: l, service: service, name: name, port: port, txt: slices.Clone(txt)}
	b.ads = append(b.ads, a)
	ws := slices.Clone(b.watches)
	b.mu.Unlock()
	for _, w := range ws {
		if w.service == service {
			w.handler.Found(name, l.host, port, slices.Clone(txt))
		}
	}
	return nil
}

func (l *lan) Browse(service string, h node.DiscoveryHandler) error {
	b := l.bus
	b.mu.Lock()
	b.watches = append(b.watches, watch{from: l, service: service, handler: h})
	as := slices.Clone(b.ads)
	b.mu.Unlock()
	for _, a := range as {
		if a.service == service {
			h.Found(a.name, a.from.host, a.port, slices.Clone(a.txt))
		}
	}
	return nil
}

func (l *lan) Stop() error {
	b := l.bus
	b.mu.Lock()
	gone := slices.DeleteFunc(slices.Clone(b.ads), func(a ad) bool { return a.from != l })
	b.ads = slices.DeleteFunc(b.ads, func(a ad) bool { return a.from == l })
	b.watches = slices.DeleteFunc(b.watches, func(w watch) bool { return w.from == l })
	ws := slices.Clone(b.watches)
	b.mu.Unlock()
	for _, a := range gone {
		for _, w := range ws {
			if w.service == a.service {
				w.handler.Lost(a.name)
			}
		}
	}
	return nil
}

func (b *bus) advertised(l *lan) []ad {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []ad
	for _, a := range b.ads {
		if a.from == l {
			out = append(out, a)
		}
	}
	return out
}

func openOn(t *testing.T, dir, platform string, l *lan) *node.Node {
	t.Helper()
	n, err := node.Open(node.Config{
		Dir:       dir,
		Keystore:  device.File(filepath.Join(dir, "device.key")),
		Platform:  platform,
		Addrs:     loopback,
		Discovery: l,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	return n
}

func txt(entries ...string) []byte {
	var b []byte
	for _, e := range entries {
		b = append(b, byte(len(e)))
		b = append(b, e...)
	}
	return b
}

func pileHash(id []byte) string {
	h := sha256.Sum256(id)
	return hex.EncodeToString(h[:16])
}

func closedAddr(t *testing.T) netip.AddrPort {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := netip.MustParseAddrPort(ln.Addr().String())
	ln.Close()
	return a
}

func staleInvite(t *testing.T, n *node.Node, p node.Pile) string {
	t.Helper()
	i, err := invite.Parse(inviteTo(t, n, p))
	if err != nil {
		t.Fatal(err)
	}
	i.Addrs = []netip.AddrPort{closedAddr(t)}
	return i.String()
}

func addrOf(t *testing.T, b *bus, l *lan) string {
	t.Helper()
	as := b.advertised(l)
	if len(as) != 1 {
		t.Fatalf("%d advertisements, want 1", len(as))
	}
	return l.host + ":" + strconv.Itoa(as[0].port)
}

func TestMembersFindEachOtherWhenInviteAddressIsStale(t *testing.T) {
	b := &bus{}
	la, lb := b.join("127.0.0.1"), b.join("127.0.0.1")
	anya := openOn(t, t.TempDir(), "android", la)
	borya := openOn(t, t.TempDir(), "ios", lb)
	p := create(t, anya, "Грузия", "Аня")

	join(t, borya, staleInvite(t, anya, p), "Боря")
	waitConverged(t, p.ID, node.Pile{ID: p.ID, Name: "Грузия", Members: map[op.Author]state.Member{
		anya.Author():  {Name: "Аня", Platform: "android"},
		borya.Author(): {Name: "Боря", Platform: "ios"},
	}}, anya, borya)
}

func TestOnlyOtherMembersOfThePileBecomePeers(t *testing.T) {
	b := &bus{}
	la, lb, lc := b.join("127.0.0.1"), b.join("127.0.0.1"), b.join("127.0.0.1")
	anya := openOn(t, t.TempDir(), "android", la)
	vera := openOn(t, t.TempDir(), "android", lc)
	p := create(t, anya, "Грузия", "Аня")
	q := create(t, vera, "Армения", "Вера")
	borya := openOn(t, t.TempDir(), "ios", lb)
	join(t, borya, staleInvite(t, anya, p), "Боря")
	waitConverged(t, p.ID, node.Pile{ID: p.ID, Name: "Грузия", Members: map[op.Author]state.Member{
		anya.Author():  {Name: "Аня", Platform: "android"},
		borya.Author(): {Name: "Боря", Platform: "ios"},
	}}, anya, borya)

	if got, want := anya.Peers(p.ID), []string{addrOf(t, b, lb)}; !slices.Equal(got, want) {
		t.Errorf("Аня's peers = %v, want only Боря %v", got, want)
	}
	if got, want := borya.Peers(p.ID), []string{addrOf(t, b, la)}; !slices.Equal(got, want) {
		t.Errorf("Боря's peers = %v, want only Аня %v", got, want)
	}
	if got := vera.Peers(q.ID); len(got) != 0 {
		t.Errorf("Вера's peers in another pile = %v, want none", got)
	}
}

func TestEachPileIsAdvertisedWithOnlyItsHash(t *testing.T) {
	b := &bus{}
	l := b.join("127.0.0.1")
	dir := t.TempDir()
	n := openOn(t, dir, "android", l)
	p := create(t, n, "Грузия", "Аня")
	q := create(t, n, "Армения", "Аня")
	secrets := map[string][]byte{}
	want := map[int]string{}
	for _, x := range []node.Pile{p, q} {
		i, err := invite.Parse(inviteTo(t, n, x))
		if err != nil {
			t.Fatal(err)
		}
		secrets[string(x.ID)] = i.Secret
		want[int(i.Addrs[0].Port())] = string(txt("pile=" + pileHash(x.ID)))
	}

	check := func() {
		t.Helper()
		got := map[int]string{}
		for _, a := range b.advertised(l) {
			if a.service != "_pileup._tcp" {
				t.Errorf("advertised service %q", a.service)
			}
			for id, s := range secrets {
				for _, leak := range []string{id, hex.EncodeToString([]byte(id)), string(s), hex.EncodeToString(s)} {
					if strings.Contains(a.name, leak) || strings.Contains(string(a.txt), leak) {
						t.Errorf("advertisement %q %q leaks the pile id or secret", a.name, a.txt)
					}
				}
			}
			got[a.port] = string(a.txt)
		}
		if len(got) != len(want) {
			t.Fatalf("advertised ports %v, want %v", got, want)
		}
		for port, r := range want {
			if got[port] != r {
				t.Errorf("port %d: TXT %q, want %q", port, got[port], r)
			}
		}
	}
	check()

	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	if as := b.advertised(l); len(as) != 0 {
		t.Fatalf("still advertised after Close: %v", as)
	}
	openOn(t, dir, "android", l)
	check()
}

func hold(t *testing.T, addr string) net.Listener {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

func TestMembersReconnectByDiscoveryAfterBothAddressesChange(t *testing.T) {
	b := &bus{}
	la, lb, lc := b.join("127.0.0.1"), b.join("127.0.0.1"), b.join("127.0.0.1")
	dirA, dirB := t.TempDir(), t.TempDir()
	anya := openOn(t, dirA, "android", la)
	borya := openOn(t, dirB, "ios", lb)
	p := create(t, anya, "Грузия", "Аня")
	join(t, borya, inviteTo(t, anya, p), "Боря")
	waitConverged(t, p.ID, node.Pile{ID: p.ID, Name: "Грузия", Members: map[op.Author]state.Member{
		anya.Author():  {Name: "Аня", Platform: "android"},
		borya.Author(): {Name: "Боря", Platform: "ios"},
	}}, anya, borya)
	oldA, oldB := addrOf(t, b, la), addrOf(t, b, lb)
	if err := errors.Join(anya.Close(), borya.Close()); err != nil {
		t.Fatal(err)
	}

	ha, hb := hold(t, oldA), hold(t, oldB)
	anya = openOn(t, dirA, "android", la)
	borya = openOn(t, dirB, "ios", lb)
	ha.Close()
	hb.Close()
	if addrOf(t, b, la) == oldA || addrOf(t, b, lb) == oldB {
		t.Fatal("addresses did not change after restart")
	}

	vera := openOn(t, t.TempDir(), "android", lc)
	join(t, vera, inviteTo(t, anya, p), "Вера")
	waitConverged(t, p.ID, node.Pile{ID: p.ID, Name: "Грузия", Members: map[op.Author]state.Member{
		anya.Author():  {Name: "Аня", Platform: "android"},
		borya.Author(): {Name: "Боря", Platform: "ios"},
		vera.Author():  {Name: "Вера", Platform: "android"},
	}}, anya, borya, vera)
}

func TestOwnAdvertisementCachedFromPreviousRunIsIgnored(t *testing.T) {
	b := &bus{}
	l := b.join("127.0.0.1")
	dir := t.TempDir()
	n := openOn(t, dir, "android", l)
	p := create(t, n, "Грузия", "Аня")
	before := b.advertised(l)
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	cache := b.join("127.0.0.1")
	for _, a := range before {
		cache.Advertise(a.service, a.name, a.port, a.txt)
	}

	n = openOn(t, dir, "android", l)
	if len(b.advertised(l)) != 1 {
		t.Fatal("pile is not advertised after restart")
	}
	if got := n.Peers(p.ID); len(got) != 0 {
		t.Fatalf("peers = %v, want none: the node must not peer with itself", got)
	}
}
