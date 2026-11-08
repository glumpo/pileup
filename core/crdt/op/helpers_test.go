package op_test

import (
	"bytes"
	"crypto/ed25519"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/crdt/op"
)

var (
	pile   = bytes.Repeat([]byte{0x50}, 32)
	secret = bytes.Repeat([]byte{0x53}, 32)
)

func key(n byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{n}, ed25519.SeedSize))
}

func author(k ed25519.PrivateKey) op.Author {
	return op.Author(k.Public().(ed25519.PublicKey))
}

func join(k ed25519.PrivateKey) op.Join {
	return op.Join{Name: "Аня", Platform: "ios", Proof: op.JoinProof(secret, author(k))}
}

func mustDecode(t testing.TB, raw []byte) op.Op {
	t.Helper()
	o, err := op.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func body(k ed25519.PrivateKey, seq uint64, prev []byte, lamport uint64, typ op.Type, payload []byte) op.Body {
	return op.Body{
		PileID:   pile,
		Author:   author(k),
		Seq:      seq,
		Prev:     prev,
		Lamport:  lamport,
		WallTime: 1700000000000,
		Schema:   op.Schema,
		Type:     typ,
		Payload:  payload,
	}
}

func mustMarshal(t testing.TB, v any) []byte {
	t.Helper()
	b, err := cbor.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
