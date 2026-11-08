package op_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/crdt/op"
)

type envelope struct {
	_    struct{} `cbor:",toarray"`
	Body []byte
	Sig  []byte
}

func split(t *testing.T, raw []byte) envelope {
	t.Helper()
	var e envelope
	if err := cbor.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestNewProducesSignedEnvelope(t *testing.T) {
	k := key(1)
	first := mustDecode(t, op.New(k, pile, op.Head{}, 1, 1700000000000, join(k)))
	change := op.PileCreate{Name: "Грузия"}
	raw := op.New(k, pile, first.Head(), 7, 1700000000123, change)

	e := split(t, raw)
	if !ed25519.Verify(k.Public().(ed25519.PublicKey), e.Body, e.Sig) {
		t.Fatal("signature does not verify over body bytes")
	}
	o := mustDecode(t, raw)
	if o.ID != sha256.Sum256(e.Body) {
		t.Error("id is not SHA-256 of body")
	}
	if !bytes.Equal(o.Raw, raw) {
		t.Error("raw bytes changed")
	}
	if !bytes.Equal(o.PileID, pile) || o.Author != author(k) || o.Seq != 2 || o.Lamport != 7 ||
		o.WallTime != 1700000000123 || o.Schema != op.Schema || o.Type != op.TypePileCreate {
		t.Errorf("unexpected body %+v", o.Body)
	}
	if !bytes.Equal(o.Prev, first.ID[:]) {
		t.Error("prev is not id of previous op")
	}
	if !reflect.DeepEqual(o.Change, change) {
		t.Errorf("change = %#v, want %#v", o.Change, change)
	}
	if len(first.Prev) != 0 {
		t.Error("prev of seq 1 is not empty")
	}
}

func TestEveryChangeTypeRoundTrips(t *testing.T) {
	k := key(1)
	offset := int32(3 * 3600)
	duration := uint64(4200)
	platform := int64(1700000000000)
	changes := []op.Change{
		op.PileCreate{Name: "Грузия"},
		join(k),
		op.MemberSet{Name: "Боря"},
		op.AssetAdd{Assets: []op.AssetMeta{{
			AssetID:        []byte{1, 2, 3},
			Revision:       []byte{4},
			Kind:           op.KindVideo,
			TakenAt:        1700000000000,
			TakenAtSource:  op.SourceVideoUTC,
			LocalOffsetSec: &offset,
			RawTime:        op.RawTime{PlatformTime: &platform, ExifDateTime: "2023:11:14 22:13:20", ExifOffset: "+03:00"},
			Width:          1920,
			Height:         1080,
			DurationMs:     &duration,
			SizeBytes:      123456,
			MIME:           "video/mp4",
			Codec:          "hevc",
			OriginalName:   "IMG_0001.MOV",
			Thumbhash:      []byte{9, 9, 9},
		}}},
		op.AssetRemove{AssetIDs: [][]byte{{1, 2, 3}, {4}}},
	}
	for _, c := range changes {
		o := mustDecode(t, op.New(k, pile, op.Head{}, 1, 0, c))
		if o.Type != c.Type() || !reflect.DeepEqual(o.Change, c) {
			t.Errorf("type %d: got %#v, want %#v", c.Type(), o.Change, c)
		}
	}
}

func TestAnySingleByteChangeIsRejected(t *testing.T) {
	k := key(1)
	raw := op.New(k, pile, op.Head{}, 1, 1700000000000, join(k))
	for i := range raw {
		for _, delta := range []byte{1, 0x80} {
			mutated := bytes.Clone(raw)
			mutated[i] ^= delta
			_, err := op.Decode(mutated)
			if !errors.Is(err, op.ErrDecode) && !errors.Is(err, op.ErrSignature) {
				t.Fatalf("byte %d ^ %#x: err = %v", i, delta, err)
			}
		}
	}
}

type reversedBody struct {
	Payload  []byte  `cbor:"9,keyasint"`
	Type     op.Type `cbor:"8,keyasint"`
	Schema   uint64  `cbor:"7,keyasint"`
	WallTime int64   `cbor:"6,keyasint"`
	Lamport  uint64  `cbor:"5,keyasint"`
	Seq      uint64  `cbor:"3,keyasint"`
	Author   []byte  `cbor:"2,keyasint"`
	PileID   []byte  `cbor:"1,keyasint"`
	Future   string  `cbor:"100,keyasint"`
}

func TestSignatureIsVerifiedOverBodyAsReceived(t *testing.T) {
	k := key(1)
	a := author(k)
	payload, err := cbor.Marshal(op.PileCreate{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := cbor.Marshal(reversedBody{
		Payload: payload, Type: op.TypePileCreate, Schema: op.Schema, WallTime: 5,
		Lamport: 3, Seq: 1, Author: a[:], PileID: pile, Future: "field from a newer version",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := cbor.Marshal(envelope{Body: body, Sig: ed25519.Sign(k, body)})
	if err != nil {
		t.Fatal(err)
	}
	o := mustDecode(t, raw)
	if !bytes.Equal(o.Raw, raw) || o.ID != sha256.Sum256(body) {
		t.Error("op is not kept as received")
	}
	if o.Author != a || o.Seq != 1 || o.Lamport != 3 || o.WallTime != 5 || !reflect.DeepEqual(o.Change, op.PileCreate{Name: "x"}) {
		t.Errorf("unexpected op %+v", o)
	}
}

func TestSignatureFromAnotherKeyIsRejected(t *testing.T) {
	b := body(key(1), 1, nil, 1, op.TypeMemberSet, []byte{0xa0})
	if _, err := op.Decode(op.Sign(key(2), b)); !errors.Is(err, op.ErrSignature) {
		t.Fatalf("err = %v, want ErrSignature", err)
	}
}

func TestMalformedPayloadOfKnownTypeIsRejected(t *testing.T) {
	k := key(1)
	for _, typ := range []op.Type{op.TypePileCreate, op.TypeJoin, op.TypeMemberSet, op.TypeAssetAdd, op.TypeAssetRemove} {
		for _, payload := range [][]byte{nil, {0xff}, {0x61, 'x'}, {0xa1, 0x01, 0x01}, {0xa0, 0x00}} {
			_, err := op.Decode(op.Sign(k, body(k, 1, nil, 1, typ, payload)))
			if !errors.Is(err, op.ErrPayload) {
				t.Errorf("type %d payload %x: err = %v, want ErrPayload", typ, payload, err)
			}
		}
	}
}

func TestUnknownTypeIsDecodedWithoutChange(t *testing.T) {
	k := key(1)
	for _, b := range []op.Body{
		body(k, 1, nil, 1, 999, []byte{0xff, 0x00}),
		func() op.Body {
			b := body(k, 1, nil, 1, op.TypePileCreate, []byte{0xff})
			b.Schema = op.Schema + 1
			return b
		}(),
	} {
		raw := op.Sign(k, b)
		o := mustDecode(t, raw)
		if o.Change != nil || o.Type != b.Type || !bytes.Equal(o.Payload, b.Payload) || !bytes.Equal(o.Raw, raw) {
			t.Errorf("unexpected op %+v", o)
		}
	}
}

func TestMalformedEncodingIsRejected(t *testing.T) {
	k := key(1)
	valid := op.New(k, pile, op.Head{}, 1, 0, join(k))
	a := author(k)
	shortAuthor, err := cbor.Marshal(map[int]any{1: pile, 2: a[:31], 3: 1, 5: 1, 7: 1, 8: 2, 9: []byte{0xa0}})
	if err != nil {
		t.Fatal(err)
	}
	dupKey := []byte{0xa2, 0x03, 0x01, 0x03, 0x02}
	for name, raw := range map[string][]byte{
		"empty":          nil,
		"not an array":   {0xa0},
		"one element":    {0x81, 0x40},
		"three elements": {0x83, 0x40, 0x40, 0x40},
		"trailing bytes": append(bytes.Clone(valid), 0x00),
		"truncated":      valid[:len(valid)-1],
	} {
		if _, err := op.Decode(raw); !errors.Is(err, op.ErrDecode) {
			t.Errorf("%s: err = %v, want ErrDecode", name, err)
		}
	}
	for name, body := range map[string][]byte{"short author": shortAuthor, "duplicate key": dupKey, "not a map": {0x01}} {
		raw, err := cbor.Marshal(envelope{Body: body, Sig: ed25519.Sign(k, body)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := op.Decode(raw); !errors.Is(err, op.ErrDecode) {
			t.Errorf("%s: err = %v, want ErrDecode", name, err)
		}
	}
}
