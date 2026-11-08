package op_test

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/glumpo/pileup/core/crdt/op"
)

func TestJoinProofIsHMACOfPubkeyUnderDerivedKey(t *testing.T) {
	a := author(key(1))
	kAuth, err := hkdf.Key(sha256.New, secret, nil, "pileup-auth", 32)
	if err != nil {
		t.Fatal(err)
	}
	m := hmac.New(sha256.New, kAuth)
	m.Write(a[:])
	if !bytes.Equal(op.JoinProof(secret, a), m.Sum(nil)) {
		t.Fatal("proof differs from HMAC-SHA256(HKDF-SHA256(S, \"pileup-auth\"), pubkey)")
	}
}

func TestCheck(t *testing.T) {
	v := op.NewValidator(pile, secret)
	k := key(1)
	first := mustDecode(t, op.New(k, pile, op.Head{}, 5, 0, join(k)))
	second := mustDecode(t, op.New(k, pile, first.Head(), 6, 0, op.MemberSet{Name: "x"}))
	signed := func(b op.Body) op.Op { return mustDecode(t, op.Sign(k, b)) }
	joinPayload := mustMarshal(t, join(k))

	cases := []struct {
		name string
		op   op.Op
		head op.Head
		want error
	}{
		{"join at seq 1", first, op.Head{}, nil},
		{"next op", second, first.Head(), nil},
		{"unknown type after join", signed(body(k, 2, first.ID[:], 6, 999, nil)), first.Head(), nil},
		{"lamport may jump", mustDecode(t, op.New(k, pile, first.Head(), 1000, 0, op.MemberSet{})), first.Head(), nil},
		{"other pile", mustDecode(t, op.New(k, []byte("other"), op.Head{}, 1, 0, join(k))), op.Head{}, op.ErrPile},
		{"duplicate", first, first.Head(), op.ErrSeq},
		{"gap", second, op.Head{}, op.ErrSeq},
		{"seq 0", signed(body(k, 0, nil, 1, op.TypeJoin, joinPayload)), op.Head{}, op.ErrSeq},
		{"wrong prev", mustDecode(t, op.New(k, pile, op.Head{Seq: 1, ID: op.ID{1}, Lamport: 5}, 6, 0, op.MemberSet{})), first.Head(), op.ErrPrev},
		{"missing prev", signed(body(k, 2, nil, 6, op.TypeMemberSet, []byte{0xa0})), first.Head(), op.ErrPrev},
		{"prev at seq 1", signed(body(k, 1, make([]byte, 32), 1, op.TypeJoin, joinPayload)), op.Head{}, op.ErrPrev},
		{"equal lamport", mustDecode(t, op.New(k, pile, first.Head(), 5, 0, op.MemberSet{})), first.Head(), op.ErrLamport},
		{"lower lamport", mustDecode(t, op.New(k, pile, first.Head(), 4, 0, op.MemberSet{})), first.Head(), op.ErrLamport},
		{"zero lamport at seq 1", mustDecode(t, op.New(k, pile, op.Head{}, 0, 0, join(k))), op.Head{}, op.ErrLamport},
		{"seq 1 is not join", mustDecode(t, op.New(k, pile, op.Head{}, 1, 0, op.PileCreate{Name: "x"})), op.Head{}, op.ErrJoin},
		{"seq 1 of unknown type", signed(body(k, 1, nil, 1, 999, nil)), op.Head{}, op.ErrJoin},
		{"proof from other secret", mustDecode(t, op.New(k, pile, op.Head{}, 1, 0, op.Join{Proof: op.JoinProof([]byte("other"), author(k))})), op.Head{}, op.ErrJoin},
		{"proof of other author", mustDecode(t, op.New(k, pile, op.Head{}, 1, 0, join(key(2)))), op.Head{}, op.ErrJoin},
		{"no proof", mustDecode(t, op.New(k, pile, op.Head{}, 1, 0, op.Join{Name: "x"})), op.Head{}, op.ErrJoin},
		{"join at seq 2", mustDecode(t, op.New(k, pile, first.Head(), 6, 0, join(k))), first.Head(), op.ErrJoin},
	}
	for _, c := range cases {
		if err := v.Check(c.op, c.head); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}
