package invite_test

import (
	"bytes"
	"encoding/base32"
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/invite"
)

const qrAlphanumeric = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ $%*+-./:"

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func sample() invite.Invite {
	return invite.Invite{
		PileID: bytes.Repeat([]byte{0x50}, 16),
		Secret: bytes.Repeat([]byte{0x53}, 32),
		Addrs: []netip.AddrPort{
			netip.MustParseAddrPort("192.168.1.23:54321"),
			netip.MustParseAddrPort("10.0.0.5:54321"),
			netip.MustParseAddrPort("[fd00::1:2]:54321"),
		},
	}
}

func encode(version byte, v any) string {
	b, err := cbor.Marshal(v)
	if err != nil {
		panic(err)
	}
	return "PILEUP:" + b32.EncodeToString(append([]byte{version}, b...))
}

func addr(s string) []byte {
	b, _ := netip.MustParseAddrPort(s).MarshalBinary()
	return b
}

func TestRoundTrip(t *testing.T) {
	in := sample()
	got, err := invite.Parse(in.String())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("got %+v, want %+v", got, in)
	}
}

func TestStringFitsQRAlphanumericMode(t *testing.T) {
	s := sample().String()
	if strings.Trim(s, qrAlphanumeric) != "" {
		t.Fatalf("%q has characters outside the QR alphanumeric set", s)
	}
	if len(s) > 200 {
		t.Fatalf("invite is %d characters long", len(s))
	}
}

func TestParseRejectsUnknownVersion(t *testing.T) {
	in := sample()
	s := encode(2, []any{in.PileID, in.Secret, [][]byte{addr("192.168.1.23:54321")}})
	if _, err := invite.Parse(s); !errors.Is(err, invite.ErrVersion) {
		t.Fatalf("err = %v, want ErrVersion", err)
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	in := sample()
	valid := in.String()
	good := addr("192.168.1.23:54321")
	cases := map[string]string{
		"empty":            "",
		"no prefix":        strings.TrimPrefix(valid, "PILEUP:"),
		"other prefix":     "HTTPS://" + strings.TrimPrefix(valid, "PILEUP:"),
		"lowercase":        strings.ToLower(valid),
		"not base32":       "PILEUP:0189!",
		"only prefix":      "PILEUP:",
		"truncated":        valid[:len(valid)-8],
		"trailing data":    "PILEUP:" + b32.EncodeToString(append(mustDecode(valid), 0)),
		"short secret":     encode(1, []any{in.PileID, in.Secret[:31], [][]byte{good}}),
		"empty pile id":    encode(1, []any{[]byte{}, in.Secret, [][]byte{good}}),
		"long pile id":     encode(1, []any{make([]byte, 256), in.Secret, [][]byte{good}}),
		"no addresses":     encode(1, []any{in.PileID, in.Secret, [][]byte{}}),
		"bad address":      encode(1, []any{in.PileID, in.Secret, [][]byte{{1, 2, 3}}}),
		"missing field":    encode(1, []any{in.PileID, in.Secret}),
		"extra field":      encode(1, []any{in.PileID, in.Secret, [][]byte{good}, 1}),
		"wrong field type": encode(1, []any{"pile", in.Secret, [][]byte{good}}),
	}
	for name, s := range cases {
		if _, err := invite.Parse(s); !errors.Is(err, invite.ErrFormat) {
			t.Errorf("%s: err = %v, want ErrFormat", name, err)
		}
	}
}

func mustDecode(s string) []byte {
	b, err := b32.DecodeString(strings.TrimPrefix(s, "PILEUP:"))
	if err != nil {
		panic(err)
	}
	return b
}
