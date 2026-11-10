package node_test

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/store"
)

func loopback() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} }

func open(t *testing.T, dir, platform string) *node.Node {
	t.Helper()
	n, err := node.Open(node.Config{
		Dir:      dir,
		Keystore: device.File(filepath.Join(dir, "device.key")),
		Platform: platform,
		Addrs:    loopback,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	return n
}

func create(t *testing.T, n *node.Node, name, member string) node.Pile {
	t.Helper()
	p, err := n.Create(name, member)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func inviteTo(t *testing.T, n *node.Node, p node.Pile) string {
	t.Helper()
	s, err := n.Invite(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func join(t *testing.T, n *node.Node, inv, member string) node.Pile {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := n.Join(ctx, inv, member)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func pileOn(t *testing.T, n *node.Node, id []byte) node.Pile {
	t.Helper()
	ps, err := n.Piles()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if string(p.ID) == string(id) {
			return p
		}
	}
	t.Fatalf("pile %x is not listed", id)
	return node.Pile{}
}

func waitConverged(t *testing.T, id []byte, want node.Pile, nodes ...*node.Node) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		same := true
		for _, n := range nodes {
			if !reflect.DeepEqual(pileOn(t, n, id), want) {
				same = false
			}
		}
		if same {
			return
		}
		if time.Now().After(deadline) {
			for i, n := range nodes {
				t.Logf("node %d: %+v", i, pileOn(t, n, id))
			}
			t.Fatalf("piles did not converge to %+v", want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCreatedPileIsListed(t *testing.T) {
	a := open(t, t.TempDir(), "android")
	p := create(t, a, "Грузия", "Аня")
	want := node.Pile{ID: p.ID, Name: "Грузия", Members: map[op.Author]state.Member{
		a.Author(): {Name: "Аня", Platform: "android"},
	}}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("created %+v, want %+v", p, want)
	}
	if got := pileOn(t, a, p.ID); !reflect.DeepEqual(got, want) {
		t.Fatalf("listed %+v, want %+v", got, want)
	}
	other := create(t, a, "Армения", "Аня")
	if string(other.ID) == string(p.ID) {
		t.Fatal("two piles got the same id")
	}
}

func TestInviteCarriesPileAndInviterAddress(t *testing.T) {
	a := open(t, t.TempDir(), "android")
	p := create(t, a, "Грузия", "Аня")
	i, err := invite.Parse(inviteTo(t, a, p))
	if err != nil {
		t.Fatal(err)
	}
	if string(i.PileID) != string(p.ID) || len(i.Secret) != 32 || len(i.Addrs) != 1 || i.Addrs[0].Addr() != loopback()[0] || i.Addrs[0].Port() == 0 {
		t.Fatalf("invite %+v", i)
	}
	other, err := invite.Parse(inviteTo(t, a, create(t, a, "Армения", "Аня")))
	if err != nil {
		t.Fatal(err)
	}
	if string(other.Secret) == string(i.Secret) {
		t.Fatal("two piles got the same secret")
	}
}

func TestMembersJoinedByInviteSeeEachOther(t *testing.T) {
	a := open(t, t.TempDir(), "android")
	b := open(t, t.TempDir(), "ios")
	c := open(t, t.TempDir(), "android")
	p := create(t, a, "Грузия", "Аня")
	inv := inviteTo(t, a, p)

	join(t, b, inv, "Боря")
	two := node.Pile{ID: p.ID, Name: "Грузия", Members: map[op.Author]state.Member{
		a.Author(): {Name: "Аня", Platform: "android"},
		b.Author(): {Name: "Боря", Platform: "ios"},
	}}
	waitConverged(t, p.ID, two, a, b)

	join(t, c, inv, "Вера")
	three := node.Pile{ID: p.ID, Name: "Грузия", Members: map[op.Author]state.Member{
		a.Author(): {Name: "Аня", Platform: "android"},
		b.Author(): {Name: "Боря", Platform: "ios"},
		c.Author(): {Name: "Вера", Platform: "android"},
	}}
	waitConverged(t, p.ID, three, a, b, c)
}

func TestInviteWithWrongSecretFailsToJoin(t *testing.T) {
	a := open(t, t.TempDir(), "android")
	b := open(t, t.TempDir(), "ios")
	p := create(t, a, "Грузия", "Аня")
	i, err := invite.Parse(inviteTo(t, a, p))
	if err != nil {
		t.Fatal(err)
	}
	i.Secret = make([]byte, 32)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := b.Join(ctx, i.String(), "Боря"); !errors.Is(err, node.ErrUnreachable) {
		t.Fatalf("join with wrong secret: err = %v, want ErrUnreachable", err)
	}
	if ps, err := b.Piles(); err != nil || len(ps) != 0 {
		t.Fatalf("joiner lists %v, %v", ps, err)
	}
	if got := pileOn(t, a, p.ID); len(got.Members) != 1 {
		t.Fatalf("inviter sees %+v", got)
	}
}

func TestMalformedInviteIsRejected(t *testing.T) {
	b := open(t, t.TempDir(), "ios")
	if _, err := b.Join(context.Background(), "PILEUP:XYZ", "Боря"); !errors.Is(err, invite.ErrFormat) {
		t.Fatalf("err = %v, want ErrFormat", err)
	}
}

func TestJoiningKnownPileIsRejected(t *testing.T) {
	a := open(t, t.TempDir(), "android")
	b := open(t, t.TempDir(), "ios")
	p := create(t, a, "Грузия", "Аня")
	inv := inviteTo(t, a, p)
	join(t, b, inv, "Боря")
	for name, n := range map[string]*node.Node{"joiner": b, "creator": a} {
		if _, err := n.Join(context.Background(), inv, "ещё раз"); !errors.Is(err, node.ErrJoined) {
			t.Errorf("%s: err = %v, want ErrJoined", name, err)
		}
	}
}

func TestEitherSideCanInvite(t *testing.T) {
	a := open(t, t.TempDir(), "ios")
	b := open(t, t.TempDir(), "android")
	byA := create(t, a, "От iPhone", "Аня")
	byB := create(t, b, "От Android", "Боря")
	join(t, b, inviteTo(t, a, byA), "Боря")
	join(t, a, inviteTo(t, b, byB), "Аня")
	members := map[op.Author]state.Member{
		a.Author(): {Name: "Аня", Platform: "ios"},
		b.Author(): {Name: "Боря", Platform: "android"},
	}
	waitConverged(t, byA.ID, node.Pile{ID: byA.ID, Name: "От iPhone", Members: members}, a, b)
	waitConverged(t, byB.ID, node.Pile{ID: byB.ID, Name: "От Android", Members: members}, a, b)
}

func TestPilesSurviveRestart(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	a := open(t, dirA, "android")
	b := open(t, dirB, "ios")
	p := create(t, a, "Грузия", "Аня")
	before, err := invite.Parse(inviteTo(t, a, p))
	if err != nil {
		t.Fatal(err)
	}
	join(t, b, inviteTo(t, a, p), "Боря")
	two := node.Pile{ID: p.ID, Name: "Грузия", Members: map[op.Author]state.Member{
		a.Author(): {Name: "Аня", Platform: "android"},
		b.Author(): {Name: "Боря", Platform: "ios"},
	}}
	waitConverged(t, p.ID, two, a, b)
	authorA, authorB := a.Author(), b.Author()
	if err := errors.Join(a.Close(), b.Close()); err != nil {
		t.Fatal(err)
	}

	a, b = open(t, dirA, "android"), open(t, dirB, "ios")
	if a.Author() != authorA || b.Author() != authorB {
		t.Fatal("device keys changed after restart")
	}
	after, err := invite.Parse(inviteTo(t, a, p))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("invite after restart %+v, before %+v", after, before)
	}
	c := open(t, t.TempDir(), "android")
	join(t, c, inviteTo(t, a, p), "Вера")
	three := node.Pile{ID: p.ID, Name: "Грузия", Members: map[op.Author]state.Member{
		authorA:    {Name: "Аня", Platform: "android"},
		authorB:    {Name: "Боря", Platform: "ios"},
		c.Author(): {Name: "Вера", Platform: "android"},
	}}
	waitConverged(t, p.ID, three, a, b, c)
}

func TestMemberIsOnlineWhileConnected(t *testing.T) {
	a, b := open(t, t.TempDir(), "ios"), open(t, t.TempDir(), "android")
	p := create(t, a, "Грузия", "Аня")
	join(t, b, inviteTo(t, a, p), "Боря")
	online := func(n *node.Node, who op.Author, want bool) func() bool {
		return func() bool { return n.Online(p.ID)[who] == want }
	}
	eventually(t, "a sees b online", online(a, b.Author(), true))
	eventually(t, "b sees a online", online(b, a.Author(), true))
	if b.Online(p.ID)[b.Author()] {
		t.Fatal("node reports itself as a connected member")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a sees b offline", online(a, b.Author(), false))
	if len(a.Online([]byte("no such pile"))) != 0 {
		t.Fatal("members online in an unknown pile")
	}
}

func TestClosedNodeLeavesNoRequestsRunning(t *testing.T) {
	for round := range 20 {
		dir := t.TempDir()
		n := open(t, dir, "android")
		p := create(t, n, "Грузия", "Аня")
		m := memberOf(t, n, p)
		busy := make(chan struct{}, 8)
		var wg sync.WaitGroup
		for range cap(busy) {
			wg.Go(func() {
				for {
					req, _ := http.NewRequest(http.MethodGet, "http://"+m.addr+"/vv", nil)
					resp, err := m.http.Do(req)
					if err != nil {
						return
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					select {
					case busy <- struct{}{}:
					default:
					}
				}
			})
		}
		for range cap(busy) {
			<-busy
		}
		if err := n.Close(); err != nil {
			t.Fatal(err)
		}
		s, err := store.Open(filepath.Join(dir, "piles", hex.EncodeToString(p.ID)+".db"), p.ID, secretOf(t, n, p))
		if err != nil {
			t.Fatalf("round %d: store of a closed node: %v", round, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		wg.Wait()
	}
}
