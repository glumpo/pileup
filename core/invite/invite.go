package invite

import (
	"encoding/base32"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/fxamacker/cbor/v2"
)

const (
	prefix     = "PILEUP:"
	version    = 1
	SecretSize = 32
)

var (
	ErrFormat  = errors.New("invite: malformed")
	ErrVersion = errors.New("invite: unknown version")
)

var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

type Invite struct {
	PileID []byte
	Secret []byte
	Addrs  []netip.AddrPort
}

type wire struct {
	_      struct{} `cbor:",toarray"`
	PileID []byte
	Secret []byte
	Addrs  [][]byte
}

func (i Invite) String() string {
	w := wire{PileID: i.PileID, Secret: i.Secret}
	for _, a := range i.Addrs {
		b, _ := a.MarshalBinary()
		w.Addrs = append(w.Addrs, b)
	}
	b, err := cbor.Marshal(w)
	if err != nil {
		panic(err)
	}
	return prefix + encoding.EncodeToString(append([]byte{version}, b...))
}

func Parse(s string) (Invite, error) {
	rest, ok := strings.CutPrefix(s, prefix)
	if !ok {
		return Invite{}, ErrFormat
	}
	b, err := encoding.DecodeString(rest)
	if err != nil || len(b) == 0 {
		return Invite{}, ErrFormat
	}
	if b[0] != version {
		return Invite{}, ErrVersion
	}
	var w wire
	if err := cbor.Unmarshal(b[1:], &w); err != nil {
		return Invite{}, fmt.Errorf("%w: %v", ErrFormat, err)
	}
	if len(w.PileID) == 0 || len(w.PileID) > 255 || len(w.Secret) != SecretSize || len(w.Addrs) == 0 {
		return Invite{}, ErrFormat
	}
	i := Invite{PileID: w.PileID, Secret: w.Secret}
	for _, b := range w.Addrs {
		var a netip.AddrPort
		if err := a.UnmarshalBinary(b); err != nil || !a.IsValid() {
			return Invite{}, ErrFormat
		}
		i.Addrs = append(i.Addrs, a)
	}
	return i, nil
}
