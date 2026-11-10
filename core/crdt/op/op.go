package op

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

type Type uint64

type Author [ed25519.PublicKeySize]byte

func (a Author) MarshalBinary() ([]byte, error) { return a[:], nil }

func (a *Author) UnmarshalBinary(b []byte) error {
	if len(b) != len(a) {
		return fmt.Errorf("author: %d bytes", len(b))
	}
	copy(a[:], b)
	return nil
}

type ID [sha256.Size]byte

type Body struct {
	PileID   []byte `cbor:"1,keyasint"`
	Author   Author `cbor:"2,keyasint"`
	Seq      uint64 `cbor:"3,keyasint"`
	Prev     []byte `cbor:"4,keyasint,omitempty"`
	Lamport  uint64 `cbor:"5,keyasint"`
	WallTime int64  `cbor:"6,keyasint"`
	Type     Type   `cbor:"8,keyasint"`
	Payload  []byte `cbor:"9,keyasint"`
}

type Op struct {
	Body
	ID     ID
	Raw    []byte
	Change Change
}

type Head struct {
	Seq     uint64
	ID      ID
	Lamport uint64
}

func (o Op) Head() Head { return Head{Seq: o.Seq, ID: o.ID, Lamport: o.Lamport} }

type envelope struct {
	_    struct{} `cbor:",toarray"`
	Body []byte
	Sig  []byte
}

var (
	ErrDecode    = errors.New("op: malformed encoding")
	ErrSignature = errors.New("op: bad signature")
	ErrPayload   = errors.New("op: malformed payload")
)

var (
	encMode = mustEncMode()
	decMode = mustDecMode()
)

func mustEncMode() cbor.EncMode {
	opts := cbor.CoreDetEncOptions()
	opts.NilContainers = cbor.NilContainerAsEmpty
	m, err := opts.EncMode()
	if err != nil {
		panic(err)
	}
	return m
}

func mustDecMode() cbor.DecMode {
	m, err := cbor.DecOptions{DupMapKey: cbor.DupMapKeyEnforcedAPF}.DecMode()
	if err != nil {
		panic(err)
	}
	return m
}

func marshal(v any) []byte {
	b, err := encMode.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func Sign(key ed25519.PrivateKey, b Body) []byte {
	body := marshal(b)
	return marshal(envelope{Body: body, Sig: ed25519.Sign(key, body)})
}

func New(key ed25519.PrivateKey, pileID []byte, head Head, lamport uint64, wallTime int64, c Change) []byte {
	b := Body{
		PileID:   pileID,
		Seq:      head.Seq + 1,
		Lamport:  lamport,
		WallTime: wallTime,
		Type:     c.Type(),
		Payload:  marshal(c),
	}
	copy(b.Author[:], key.Public().(ed25519.PublicKey))
	if head.Seq > 0 {
		b.Prev = head.ID[:]
	}
	return Sign(key, b)
}

func Decode(raw []byte) (Op, error) {
	var e envelope
	if err := decMode.Unmarshal(raw, &e); err != nil {
		return Op{}, fmt.Errorf("%w: %v", ErrDecode, err)
	}
	var b Body
	if err := decMode.Unmarshal(e.Body, &b); err != nil {
		return Op{}, fmt.Errorf("%w: %v", ErrDecode, err)
	}
	if !ed25519.Verify(b.Author[:], e.Body, e.Sig) {
		return Op{}, ErrSignature
	}
	c, err := decodeChange(b)
	if err != nil {
		return Op{}, fmt.Errorf("%w: %v", ErrPayload, err)
	}
	return Op{Body: b, ID: sha256.Sum256(e.Body), Raw: raw, Change: c}, nil
}
