package store_test

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/crdt/vv"
	"pgregory.net/rapid"
)

var (
	names    = rapid.SampledFrom([]string{"", "Аня", "Боря", "Грузия", "x"})
	assetIDs = rapid.SampledFrom([]string{"a", "b", "c", "d"})
)

func drawMeta(t *rapid.T, id string) op.AssetMeta {
	m := asset(id, names.Draw(t, "file name"))
	m.TakenAt = rapid.Int64().Draw(t, "taken at")
	m.Thumbhash = rapid.SliceOfN(rapid.Byte(), 0, 3).Draw(t, "thumbhash")
	m.Codec = names.Draw(t, "codec")
	m.LocalOffsetSec = rapid.Ptr(rapid.Int32(), true).Draw(t, "offset")
	m.DurationMs = rapid.Ptr(rapid.Uint64(), true).Draw(t, "duration")
	m.RawTime.ExifDateTime = names.Draw(t, "exif time")
	m.RawTime.FileTime = rapid.Ptr(rapid.Int64(), true).Draw(t, "file time")
	return m
}

func drawChange(t *rapid.T, k ed25519.PrivateKey, joined bool) op.Change {
	if !joined || rapid.IntRange(0, 20).Draw(t, "rejoin") == 0 {
		return join(k, names.Draw(t, "join name"))
	}
	switch rapid.IntRange(0, 4).Draw(t, "change") {
	case 0:
		return op.PileCreate{Name: names.Draw(t, "pile name")}
	case 1:
		return op.MemberSet{Name: names.Draw(t, "member name")}
	case 2:
		var metas []op.AssetMeta
		for _, id := range rapid.SliceOfN(assetIDs, 1, 3).Draw(t, "add") {
			metas = append(metas, drawMeta(t, id))
		}
		return op.AssetAdd{Assets: metas}
	case 3:
		var ids [][]byte
		for _, id := range rapid.SliceOfN(assetIDs, 1, 3).Draw(t, "remove") {
			ids = append(ids, []byte(id))
		}
		return op.AssetRemove{AssetIDs: ids}
	}
	return unknown{X: names.Draw(t, "unknown")}
}

func sameErr(a, b error) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Error() == b.Error()
}

type peer struct {
	key ed25519.PrivateKey
	*state.State
}

func TestStoreBehavesLikeReferenceState(t *testing.T) {
	dir := t.TempDir()
	cases := 0
	rapid.Check(t, func(t *rapid.T) {
		cases++
		path := filepath.Join(dir, fmt.Sprintf("%d.db", cases))
		s := open(t, path)
		defer func() { s.Close() }()
		ref := state.New(pile, secret)
		local := []ed25519.PrivateKey{key(1), key(2)}
		peers := []peer{{key(3), state.New(pile, secret)}, {key(4), state.New(pile, secret)}}
		pool := [][]byte{{1, 2, 3}, op.New(key(1), []byte("other pile"), op.Head{}, 1, 0, join(key(1), ""))}

		for range rapid.IntRange(0, 50).Draw(t, "steps") {
			switch rapid.IntRange(0, 5).Draw(t, "action") {
			case 0:
				k := rapid.SampledFrom(local).Draw(t, "local author")
				c := drawChange(t, k, ref.View().VV[author(k)] > 0)
				got, gotErr := s.Create(k, wallTime, c)
				want, wantErr := ref.Create(k, wallTime, c)
				if !sameErr(gotErr, wantErr) || !bytes.Equal(got.Raw, want.Raw) {
					t.Fatalf("Create: err %v, want %v; same bytes %v", gotErr, wantErr, bytes.Equal(got.Raw, want.Raw))
				}
				if wantErr == nil {
					pool = append(pool, want.Raw)
				}
			case 1:
				p := rapid.SampledFrom(peers).Draw(t, "peer")
				o, err := p.Create(p.key, wallTime, drawChange(t, p.key, p.View().VV[author(p.key)] > 0))
				if err == nil {
					pool = append(pool, o.Raw)
				}
			case 2:
				p := rapid.SampledFrom(peers).Draw(t, "peer")
				for a, r := range vv.Missing(p.View().VV, ref.View().VV) {
					for _, raw := range ref.Ops(a, r.From) {
						if _, err := p.Apply(raw); err != nil {
							t.Fatal(err)
						}
					}
				}
			case 3:
				p := rapid.SampledFrom(peers).Draw(t, "peer")
				for a, r := range vv.Missing(ref.View().VV, p.View().VV) {
					raw := p.Ops(a, r.From)[0]
					if _, err := s.Apply(raw); err != nil {
						t.Fatal(err)
					}
					if _, err := ref.Apply(raw); err != nil {
						t.Fatal(err)
					}
				}
			case 4:
				raw := rapid.SampledFrom(pool).Draw(t, "any op")
				_, gotErr := s.Apply(raw)
				_, wantErr := ref.Apply(raw)
				if !sameErr(gotErr, wantErr) {
					t.Fatalf("Apply: err %v, want %v", gotErr, wantErr)
				}
			case 5:
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				s = open(t, path)
			}
			if got, want := view(t, s), ref.View(); !reflect.DeepEqual(got, want) {
				t.Fatalf("view differs from reference:\n got %+v\nwant %+v", got, want)
			}
		}
		for a := range ref.View().VV {
			for from := uint64(0); from <= ref.View().VV[a]+1; from++ {
				if got, want := ops(t, s, a, from), ref.Ops(a, from); !reflect.DeepEqual(got, want) {
					t.Fatalf("Ops(%x, %d): %d ops, want %d", a[:4], from, len(got), len(want))
				}
			}
		}
	})
}
