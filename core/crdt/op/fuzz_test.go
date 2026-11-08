package op_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/crdt/op"
)

func goldenOps(f *testing.F) [][]byte {
	paths, err := filepath.Glob(filepath.Join("testdata", "*.hex"))
	if err != nil {
		f.Fatal(err)
	}
	var raws [][]byte
	for _, p := range paths {
		text, err := os.ReadFile(p)
		if err != nil {
			f.Fatal(err)
		}
		raw, err := hex.DecodeString(strings.TrimSpace(string(text)))
		if err != nil {
			f.Fatal(err)
		}
		raws = append(raws, raw)
	}
	return raws
}

func FuzzDecode(f *testing.F) {
	for _, raw := range goldenOps(f) {
		f.Add(raw)
	}
	f.Add([]byte{})
	f.Add([]byte{0x82, 0x40, 0x40})
	v := op.NewValidator(pile, secret)
	f.Fuzz(func(t *testing.T, raw []byte) {
		o, err := op.Decode(raw)
		if err != nil {
			return
		}
		if !bytes.Equal(o.Raw, raw) {
			t.Fatal("decoded op does not keep received bytes")
		}
		v.Check(o, op.Head{})
		v.Check(o, op.Head{Seq: o.Seq - 1, Lamport: o.Lamport - 1})
	})
}

func FuzzSignedBody(f *testing.F) {
	for _, raw := range goldenOps(f) {
		var e envelope
		if err := cbor.Unmarshal(raw, &e); err != nil {
			f.Fatal(err)
		}
		f.Add(e.Body)
	}
	k := key(1)
	v := op.NewValidator(pile, secret)
	f.Fuzz(func(t *testing.T, body []byte) {
		raw := mustMarshal(t, envelope{Body: body, Sig: ed25519.Sign(k, body)})
		o, err := op.Decode(raw)
		if err != nil {
			return
		}
		v.Check(o, op.Head{})
		v.Check(o, op.Head{Seq: o.Seq - 1, Lamport: o.Lamport - 1})
	})
}

func FuzzPayload(f *testing.F) {
	for _, raw := range goldenOps(f) {
		o := mustDecode(f, raw)
		f.Add(uint64(o.Type), o.Payload)
	}
	k := key(1)
	f.Fuzz(func(t *testing.T, typ uint64, payload []byte) {
		o, err := op.Decode(op.Sign(k, body(k, 1, nil, 1, op.Type(typ), payload)))
		if err != nil && !errors.Is(err, op.ErrPayload) {
			t.Fatalf("err = %v, want nil or ErrPayload", err)
		}
		if err == nil && (o.Change == nil) != (typ == 0 || typ > uint64(op.TypeAssetRemove)) {
			t.Fatalf("type %d decoded as %#v", typ, o.Change)
		}
		if err != nil && (typ == 0 || typ > uint64(op.TypeAssetRemove)) {
			t.Fatalf("unknown type %d rejected: %v", typ, err)
		}
	})
}
