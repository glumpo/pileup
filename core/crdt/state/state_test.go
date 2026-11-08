package state_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/crdt/vv"
)

func TestCreateTakesNextSeqPrevAndLamport(t *testing.T) {
	a, b := newNode(1), newNode(2)
	first := a.join(t, "Аня", "ios")
	if first.Seq != 1 || first.Lamport != 1 || len(first.Prev) != 0 {
		t.Fatalf("first op: seq %d lamport %d prev %x", first.Seq, first.Lamport, first.Prev)
	}
	b.join(t, "Боря", "android")
	b.create(t, op.MemberSet{Name: "Б"})
	b.create(t, op.MemberSet{Name: "Борис"})

	a.pull(t, b.State)
	if l := a.View().Lamport; l != 3 {
		t.Fatalf("after receiving lamport 3: L = %d", l)
	}
	second := a.create(t, op.PileCreate{Name: "Грузия"})
	if second.Seq != 2 || second.Lamport != 4 || !bytes.Equal(second.Prev, first.ID[:]) {
		t.Fatalf("second op: seq %d lamport %d prev %x", second.Seq, second.Lamport, second.Prev)
	}

	b.pull(t, a.State)
	if l := b.View().Lamport; l != 4 {
		t.Fatalf("b after receiving lamport 4: L = %d", l)
	}
	low := newNode(3)
	low.join(t, "Вера", "desktop")
	b.pull(t, low.State)
	if l := b.View().Lamport; l != 4 {
		t.Fatalf("b after receiving lamport 1: L = %d, want 4", l)
	}
}

func TestRejectedOpLeavesStateUnchanged(t *testing.T) {
	a, b := newNode(1), newNode(2)
	a.join(t, "Аня", "ios")
	second := a.create(t, op.MemberSet{Name: "А"})
	third := a.create(t, op.PileCreate{Name: "x"})
	b.join(t, "Боря", "android")
	before := b.View()

	rejected := map[string]struct {
		raw  []byte
		want error
	}{
		"gap":        {second.Raw, op.ErrSeq},
		"later gap":  {third.Raw, op.ErrSeq},
		"other pile": {op.New(a.key, []byte("other"), op.Head{}, 1, 0, op.Join{}), op.ErrPile},
		"garbage":    {[]byte{1, 2, 3}, op.ErrDecode},
		"own dup":    {b.Ops(b.author(), 1)[0], op.ErrSeq},
	}
	for name, c := range rejected {
		if _, err := b.Apply(c.raw); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
		if !reflect.DeepEqual(b.View(), before) {
			t.Errorf("%s: state changed", name)
		}
	}
	if got := b.Ops(a.author(), 1); len(got) != 0 {
		t.Errorf("rejected ops were stored: %d", len(got))
	}
}

func TestJoinNeedsPileSecret(t *testing.T) {
	a := newNode(1)
	raw := op.New(a.key, pile, op.Head{}, 1, 0, op.Join{Name: "x", Proof: op.JoinProof([]byte("other secret"), a.author())})
	if _, err := state.New(pile, secret).Apply(raw); !errors.Is(err, op.ErrJoin) {
		t.Fatalf("err = %v, want ErrJoin", err)
	}
	if _, err := state.New(pile, []byte("other secret")).Apply(raw); err != nil {
		t.Fatalf("join with matching secret: %v", err)
	}
}

func TestLamportMustGrowWithinAuthorLog(t *testing.T) {
	a := newNode(1)
	first := a.join(t, "Аня", "ios")
	s := state.New(pile, secret)
	if _, err := s.Apply(first.Raw); err != nil {
		t.Fatal(err)
	}
	for _, l := range []uint64{0, first.Lamport} {
		if _, err := s.Apply(op.New(a.key, pile, first.Head(), l, 0, op.MemberSet{})); !errors.Is(err, op.ErrLamport) {
			t.Errorf("lamport %d after %d: err = %v, want ErrLamport", l, first.Lamport, err)
		}
	}
}

func TestUnknownTypeIsStoredAndForwardedWithoutEffect(t *testing.T) {
	a, b, c := newNode(1), newNode(2), newNode(3)
	b.join(t, "Боря", "android")
	a.pull(t, b.State)
	before := a.View()

	u := b.create(t, unknown{X: "from the future"})
	after := b.create(t, op.MemberSet{Name: "Борис"})

	if _, err := a.Apply(u.Raw); err != nil {
		t.Fatalf("unknown type rejected: %v", err)
	}
	got := a.View()
	if !reflect.DeepEqual(got.Members, before.Members) || !reflect.DeepEqual(got.Assets, before.Assets) || got.PileName != before.PileName {
		t.Error("unknown type changed state")
	}
	if got.VV[b.author()] != 2 {
		t.Errorf("unknown op not stored: vv %d", got.VV[b.author()])
	}
	if fwd := a.Ops(b.author(), 2); len(fwd) != 1 || !bytes.Equal(fwd[0], u.Raw) {
		t.Fatal("unknown op is not forwarded byte for byte")
	}
	if _, err := a.Apply(after.Raw); err != nil {
		t.Fatalf("op after unknown type rejected: %v", err)
	}

	c.pull(t, a.State)
	if !reflect.DeepEqual(c.View(), b.View()) {
		t.Error("state received through forwarding differs from author's")
	}
}

func TestMembers(t *testing.T) {
	a, b := newNode(1), newNode(2)
	a.join(t, "Аня", "ios")
	b.join(t, "Боря", "android")
	b.create(t, op.MemberSet{Name: "Борис"})
	a.pull(t, b.State)
	want := map[op.Author]state.Member{
		a.author(): {Name: "Аня", Platform: "ios"},
		b.author(): {Name: "Борис", Platform: "android"},
	}
	if got := a.View().Members; !reflect.DeepEqual(got, want) {
		t.Errorf("members = %v, want %v", got, want)
	}
}

func TestPileNameIsLastWriterWins(t *testing.T) {
	a, b := newNode(1), newNode(2)
	a.join(t, "Аня", "ios")
	b.join(t, "Боря", "android")
	a.create(t, op.PileCreate{Name: "от Ани"})
	b.create(t, op.PileCreate{Name: "от Бори"})

	greater := "от Ани"
	if aa, ba := a.author(), b.author(); bytes.Compare(ba[:], aa[:]) > 0 {
		greater = "от Бори"
	}
	a.pull(t, b.State)
	b.pull(t, a.State)
	if a.View().PileName != greater || b.View().PileName != greater {
		t.Fatalf("concurrent names: a %q b %q, want %q", a.View().PileName, b.View().PileName, greater)
	}

	loser := a
	if greater == "от Ани" {
		loser = b
	}
	loser.create(t, op.PileCreate{Name: "позже"})
	a.pull(t, b.State)
	b.pull(t, a.State)
	if a.View().PileName != "позже" || b.View().PileName != "позже" {
		t.Fatalf("later name lost: a %q b %q", a.View().PileName, b.View().PileName)
	}
}

func TestAssetsChangeOnlyByOwner(t *testing.T) {
	a, b := newNode(1), newNode(2)
	a.join(t, "Аня", "ios")
	b.join(t, "Боря", "android")
	a.create(t, op.AssetAdd{Assets: []op.AssetMeta{asset("x", "a.jpg")}})
	b.pull(t, a.State)
	b.create(t, op.AssetAdd{Assets: []op.AssetMeta{asset("x", "stolen.jpg"), asset("y", "b.jpg")}})
	b.create(t, op.AssetRemove{AssetIDs: [][]byte{[]byte("x")}})
	a.pull(t, b.State)
	a.create(t, op.AssetRemove{AssetIDs: [][]byte{[]byte("y")}})
	b.pull(t, a.State)

	want := map[string]state.Asset{
		"x": {Owner: a.author(), Meta: asset("x", "a.jpg")},
		"y": {Owner: b.author(), Meta: asset("y", "b.jpg")},
	}
	for _, n := range []*node{a, b} {
		if got := n.View().Assets; !reflect.DeepEqual(got, want) {
			t.Errorf("assets = %v, want %v", got, want)
		}
	}
}

func TestOwnerCanRemoveAndAddAgain(t *testing.T) {
	a := newNode(1)
	a.join(t, "Аня", "ios")
	a.create(t, op.AssetAdd{Assets: []op.AssetMeta{asset("x", "1.jpg")}})
	a.create(t, op.AssetRemove{AssetIDs: [][]byte{[]byte("x")}})
	if got := a.View().Assets; len(got) != 0 {
		t.Fatalf("removed asset still present: %v", got)
	}
	a.create(t, op.AssetAdd{Assets: []op.AssetMeta{asset("x", "2.jpg")}})
	want := map[string]state.Asset{"x": {Owner: a.author(), Meta: asset("x", "2.jpg")}}
	if got := a.View().Assets; !reflect.DeepEqual(got, want) {
		t.Fatalf("assets = %v, want %v", got, want)
	}
}

func TestConcurrentAddOfSameAssetHasOneOwnerEverywhere(t *testing.T) {
	a, b := newNode(1), newNode(2)
	a.join(t, "Аня", "ios")
	a.create(t, op.MemberSet{Name: "А"})
	a.create(t, op.AssetAdd{Assets: []op.AssetMeta{asset("x", "a.jpg")}})
	b.join(t, "Боря", "android")
	b.create(t, op.AssetAdd{Assets: []op.AssetMeta{asset("x", "b.jpg")}})

	ab, ba := newNode(3), newNode(4)
	ab.pull(t, a.State)
	ab.pull(t, b.State)
	ba.pull(t, b.State)
	ba.pull(t, a.State)
	want := map[string]state.Asset{"x": {Owner: b.author(), Meta: asset("x", "b.jpg")}}
	for _, n := range []*node{ab, ba} {
		if got := n.View().Assets; !reflect.DeepEqual(got, want) {
			t.Errorf("assets = %v, want %v", got, want)
		}
	}
}

func TestOpsFrom(t *testing.T) {
	a := newNode(1)
	var raws [][]byte
	raws = append(raws, a.join(t, "Аня", "ios").Raw)
	for range 3 {
		raws = append(raws, a.create(t, op.MemberSet{Name: "x"}).Raw)
	}
	for from := uint64(1); from <= 5; from++ {
		got := a.Ops(a.author(), from)
		if want := raws[from-1:]; len(got) != len(want) || len(want) > 0 && !reflect.DeepEqual(got, want) {
			t.Errorf("Ops from %d: %d ops, want %d", from, len(got), len(want))
		}
	}
	if got := a.View().VV; !reflect.DeepEqual(got, vv.VV{a.author(): 4}) {
		t.Errorf("vv = %v", got)
	}
}
