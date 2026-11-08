package store_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
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

func TestPathIsUsedVerbatim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Application Support", "pile 100%20.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	s := open(t, path)
	defer s.Close()
	create(t, s, key(1), join(key(1), "Аня"))
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestOpsFromOtherEncodersAreReducedLikeReference(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "pile.db"))
	defer s.Close()
	ref := state.New(pile, secret)
	k := key(1)
	payloads := map[op.Type]any{
		op.TypeAssetAdd:    map[int]any{1: []any{map[int]any{2: []byte("r"), 3: 1}}},
		op.TypeAssetRemove: map[int]any{1: []any{nil}},
	}
	head := op.Head{}
	raws := [][]byte{op.New(k, pile, head, 1, wallTime, join(k, "Аня"))}
	for i, typ := range []op.Type{op.TypeAssetAdd, op.TypeAssetRemove, op.TypeAssetAdd} {
		o, err := op.Decode(raws[len(raws)-1])
		if err != nil {
			t.Fatal(err)
		}
		head = o.Head()
		payload, err := cbor.Marshal(payloads[typ])
		if err != nil {
			t.Fatal(err)
		}
		raws = append(raws, op.Sign(k, op.Body{
			PileID: pile, Author: author(k), Seq: head.Seq + 1, Prev: head.ID[:], Lamport: uint64(i + 2),
			Schema: op.Schema, Type: typ, Payload: payload,
		}))
	}
	for i, raw := range raws {
		_, gotErr := s.Apply(raw)
		_, wantErr := ref.Apply(raw)
		if !sameErr(gotErr, wantErr) {
			t.Fatalf("op %d: err %v, want %v", i, gotErr, wantErr)
		}
		if got, want := view(t, s), ref.View(); !reflect.DeepEqual(got, want) {
			t.Fatalf("op %d: view differs from reference:\n got %+v\nwant %+v", i, got, want)
		}
	}
}
