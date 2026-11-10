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
	return op.Join{Name: "Аня", Platform: "ios", AppVersion: "1.4.0", Proof: op.JoinProof(secret, author(k))}
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

type wireBody struct {
	PileID   []byte  `cbor:"1,keyasint"`
	Author   []byte  `cbor:"2,keyasint"`
	Seq      uint64  `cbor:"3,keyasint"`
	Prev     []byte  `cbor:"4,keyasint,omitempty"`
	Lamport  uint64  `cbor:"5,keyasint"`
	WallTime int64   `cbor:"6,keyasint"`
	Schema   *uint64 `cbor:"7,keyasint,omitempty"`
	Type     op.Type `cbor:"8,keyasint"`
	Payload  []byte  `cbor:"9,keyasint"`
}

func signWire(t testing.TB, k ed25519.PrivateKey, b wireBody) []byte {
	t.Helper()
	a := author(k)
	b.PileID, b.Author = pile, a[:]
	body := mustMarshal(t, b)
	return mustMarshal(t, envelope{Body: body, Sig: ed25519.Sign(k, body)})
}
