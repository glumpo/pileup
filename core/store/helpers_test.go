package store_test

import (
	"bytes"
	"crypto/ed25519"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/store"
)

var (
	pile   = bytes.Repeat([]byte{0x50}, 32)
	secret = bytes.Repeat([]byte{0x53}, 32)
)

const wallTime = 1700000000000

type tb interface {
	Helper()
	Fatal(...any)
	Fatalf(string, ...any)
}

func key(n byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{n}, ed25519.SeedSize))
}

func author(k ed25519.PrivateKey) op.Author {
	return op.Author(k.Public().(ed25519.PublicKey))
}

func join(k ed25519.PrivateKey, name string) op.Join {
	return op.Join{Name: name, Platform: "ios", Proof: op.JoinProof(secret, author(k))}
}

func open(t tb, path string) *store.Store {
	t.Helper()
	s, err := store.Open(path, pile, secret)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func view(t tb, s *store.Store) state.View {
	t.Helper()
	v, err := s.View()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func ops(t tb, s *store.Store, a op.Author, from uint64) [][]byte {
	t.Helper()
	raws, err := s.Ops(a, from)
	if err != nil {
		t.Fatal(err)
	}
	return raws
}

func create(t tb, s *store.Store, k ed25519.PrivateKey, c op.Change) op.Op {
	t.Helper()
	o, err := s.Create(k, wallTime, c)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func rebuild(t tb, s *store.Store) *state.State {
	t.Helper()
	ref := state.New(pile, secret)
	for a := range view(t, s).VV {
		for _, raw := range ops(t, s, a, 1) {
			if _, err := ref.Apply(raw); err != nil {
				t.Fatalf("stored journal of %x does not replay: %v", a[:4], err)
			}
		}
	}
	return ref
}

func asset(id, name string) op.AssetMeta {
	return op.AssetMeta{
		AssetID:      []byte(id),
		Revision:     []byte(name),
		Kind:         op.KindPhoto,
		MIME:         "image/jpeg",
		OriginalName: name,
		Thumbhash:    []byte{1, 2, 3},
	}
}

type unknown struct {
	X string `cbor:"1,keyasint"`
}

func (unknown) Type() op.Type { return 999 }
