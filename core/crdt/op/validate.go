package op

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
)

var (
	ErrPile    = errors.New("op: wrong pile")
	ErrSeq     = errors.New("op: seq is not next for author")
	ErrPrev    = errors.New("op: prev does not match author's previous op")
	ErrLamport = errors.New("op: lamport does not increase")
	ErrJoin    = errors.New("op: author log must start with a valid Join")
)

type Validator struct {
	pileID []byte
	kAuth  []byte
}

func NewValidator(pileID, secret []byte) Validator {
	return Validator{pileID: pileID, kAuth: authKey(secret)}
}

func authKey(secret []byte) []byte {
	k, err := hkdf.Key(sha256.New, secret, nil, "pileup-auth", sha256.Size)
	if err != nil {
		panic(err)
	}
	return k
}

func proof(kAuth []byte, a Author) []byte {
	m := hmac.New(sha256.New, kAuth)
	m.Write(a[:])
	return m.Sum(nil)
}

func JoinProof(secret []byte, a Author) []byte {
	return proof(authKey(secret), a)
}

func (v Validator) Check(o Op, head Head) error {
	if !bytes.Equal(o.PileID, v.pileID) {
		return ErrPile
	}
	if o.Seq != head.Seq+1 {
		return ErrSeq
	}
	if o.Seq == 1 && len(o.Prev) != 0 || o.Seq > 1 && !bytes.Equal(o.Prev, head.ID[:]) {
		return ErrPrev
	}
	if o.Lamport <= head.Lamport {
		return ErrLamport
	}
	j, isJoin := o.Change.(Join)
	if isJoin != (o.Seq == 1) || isJoin && !hmac.Equal(j.Proof, proof(v.kAuth, o.Author)) {
		return ErrJoin
	}
	return nil
}
