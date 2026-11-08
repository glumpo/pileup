package store_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/glumpo/pileup/core/crdt/op"
)

func TestStateSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pile.db")
	a, b := key(1), key(2)
	s := open(t, path)
	create(t, s, a, join(a, "Аня"))
	create(t, s, a, op.PileCreate{Name: "Грузия"})
	create(t, s, a, op.AssetAdd{Assets: []op.AssetMeta{asset("x", "a.jpg"), asset("y", "b.jpg")}})
	create(t, s, a, op.AssetRemove{AssetIDs: [][]byte{[]byte("y")}})
	if _, err := s.Apply(op.New(b, pile, op.Head{}, 10, wallTime, join(b, "Боря"))); err != nil {
		t.Fatal(err)
	}
	before := view(t, s)
	journal := [][][]byte{ops(t, s, author(a), 1), ops(t, s, author(b), 1)}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s = open(t, path)
	defer s.Close()
	if got := view(t, s); !reflect.DeepEqual(got, before) {
		t.Fatalf("view after reopen:\n got %+v\nwant %+v", got, before)
	}
	if got := [][][]byte{ops(t, s, author(a), 1), ops(t, s, author(b), 1)}; !reflect.DeepEqual(got, journal) {
		t.Fatal("journal changed after reopen")
	}
	o := create(t, s, a, op.MemberSet{Name: "А"})
	if o.Seq != 5 || o.Lamport != 11 {
		t.Fatalf("op after reopen: seq %d lamport %d, want 5 and 11", o.Seq, o.Lamport)
	}
}
