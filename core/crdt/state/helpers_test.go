package state_test

import (
	"bytes"
	"crypto/ed25519"
	"testing"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/crdt/vv"
)

var (
	pile   = bytes.Repeat([]byte{0x50}, 32)
	secret = bytes.Repeat([]byte{0x53}, 32)
)

type node struct {
	key ed25519.PrivateKey
	*state.State
}

func newNode(seed byte) *node {
	return &node{
		key:   ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize)),
		State: state.New(pile, secret),
	}
}

func (n *node) author() op.Author {
	return op.Author(n.key.Public().(ed25519.PublicKey))
}

func (n *node) join(t testing.TB, name, platform string) op.Op {
	t.Helper()
	return n.create(t, op.Join{Name: name, Platform: platform, Proof: op.JoinProof(secret, n.author())})
}

func (n *node) create(t testing.TB, c op.Change) op.Op {
	t.Helper()
	o, err := n.Create(n.key, 1700000000000, c)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func (n *node) pull(t testing.TB, from *state.State) {
	t.Helper()
	for a, r := range vv.Missing(n.View().VV, from.View().VV) {
		for _, raw := range from.Ops(a, r.From) {
			if _, err := n.Apply(raw); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func asset(id string, name string) op.AssetMeta {
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
