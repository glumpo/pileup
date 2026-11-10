package op_test

import (
	"bytes"
	"encoding/hex"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/crdt/op"
)

var update = flag.Bool("update", false, "rewrite golden vectors in testdata")

type vector struct {
	name   string
	typ    op.Type
	change op.Change
}

func goldenVectors() []vector {
	offset := int32(4 * 3600)
	duration := uint64(3500)
	platform := int64(1700000000000)
	created := int64(1699999990000)
	return []vector{
		{"join", op.TypeJoin, join(key(1))},
		{"pile_create", op.TypePileCreate, op.PileCreate{Name: "Грузия 2026"}},
		{"member_set", op.TypeMemberSet, op.MemberSet{Name: "Анна"}},
		{"asset_add", op.TypeAssetAdd, op.AssetAdd{Assets: []op.AssetMeta{
			{
				AssetID:       bytes.Repeat([]byte{0xa1}, 32),
				Revision:      bytes.Repeat([]byte{0xb1}, 32),
				Kind:          op.KindPhoto,
				TakenAt:       1700000000000,
				TakenAtSource: op.SourceExifOffset,
				RawTime:       op.RawTime{ExifDateTime: "2023:11:15 02:13:20", ExifOffset: "+04:00"},
				Width:         4032,
				Height:        3024,
				SizeBytes:     2500000,
				MIME:          "image/heic",
				OriginalName:  "IMG_0001.HEIC",
				Thumbhash:     []byte{0x1f, 0x2e, 0x3d},
			},
			{
				AssetID:        bytes.Repeat([]byte{0xa2}, 32),
				Revision:       bytes.Repeat([]byte{0xb2}, 32),
				Kind:           op.KindVideo,
				TakenAt:        1700000005000,
				TakenAtSource:  op.SourcePlatform,
				LocalOffsetSec: &offset,
				RawTime:        op.RawTime{PlatformTime: &platform, VideoCreationTime: &created, FileTime: &created},
				Width:          1920,
				Height:         1080,
				DurationMs:     &duration,
				SizeBytes:      9000000,
				MIME:           "video/mp4",
				Codec:          "hvc1",
				OriginalName:   "VID_0002.MP4",
				Thumbhash:      []byte{0x4c},
			},
		}}},
		{"asset_remove", op.TypeAssetRemove, op.AssetRemove{AssetIDs: [][]byte{bytes.Repeat([]byte{0xa1}, 32)}}},
		{"unknown_type", 999, nil},
	}
}

func TestGoldenVectors(t *testing.T) {
	k := key(1)
	v := op.NewValidator(pile, secret)
	var head op.Head
	for i, vec := range goldenVectors() {
		seq, lamport, wall := uint64(i+1), uint64(10*i+3), int64(1700000000000+i)
		var built []byte
		if vec.change != nil {
			built = op.New(k, pile, head, lamport, wall, vec.change)
		} else {
			built = op.Sign(k, op.Body{
				PileID: pile, Author: author(k), Seq: seq, Prev: head.ID[:], Lamport: lamport,
				WallTime: wall, Type: vec.typ, Payload: []byte{0xa1, 0x01, 0x63, 'n', 'e', 'w'},
			})
		}

		path := filepath.Join("testdata", vec.name+".hex")
		if *update {
			if err := os.WriteFile(path, []byte(hex.EncodeToString(built)+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := hex.DecodeString(strings.TrimSpace(string(text)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(built, stored) {
			t.Errorf("%s: freshly built op differs from golden bytes", vec.name)
		}

		o := mustDecode(t, stored)
		if !bytes.Equal(o.PileID, pile) || o.Author != author(k) || o.Seq != seq || o.Lamport != lamport ||
			o.WallTime != wall || o.Type != vec.typ {
			t.Errorf("%s: body %+v", vec.name, o.Body)
		}
		if seq == 1 && len(o.Prev) != 0 || seq > 1 && !bytes.Equal(o.Prev, head.ID[:]) {
			t.Errorf("%s: prev %x, want id of previous vector", vec.name, o.Prev)
		}
		if !reflect.DeepEqual(o.Change, vec.change) {
			t.Errorf("%s: change %#v, want %#v", vec.name, o.Change, vec.change)
		}
		if err := v.Check(o, head); err != nil {
			t.Errorf("%s: %v", vec.name, err)
		}
		head = o.Head()
	}
}

func TestLegacyVectorsWithSchemaStillApply(t *testing.T) {
	k := key(1)
	v := op.NewValidator(pile, secret)
	var head op.Head
	for i, vec := range goldenVectors() {
		seq, lamport, wall := uint64(i+1), uint64(10*i+3), int64(1700000000000+i)
		text, err := os.ReadFile(filepath.Join("testdata", "schema1", vec.name+".hex"))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := hex.DecodeString(strings.TrimSpace(string(text)))
		if err != nil {
			t.Fatal(err)
		}
		var fields map[int]cbor.RawMessage
		if err := cbor.Unmarshal(split(t, raw).Body, &fields); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fields[7], []byte{0x01}) {
			t.Fatalf("%s: not a legacy op with schema 1", vec.name)
		}

		o := mustDecode(t, raw)
		want := vec.change
		if j, ok := want.(op.Join); ok {
			j.AppVersion = ""
			want = j
		}
		if !bytes.Equal(o.PileID, pile) || o.Author != author(k) || o.Seq != seq || o.Lamport != lamport ||
			o.WallTime != wall || o.Type != vec.typ || !bytes.Equal(o.Raw, raw) {
			t.Errorf("%s: body %+v", vec.name, o.Body)
		}
		if !reflect.DeepEqual(o.Change, want) {
			t.Errorf("%s: change %#v, want %#v", vec.name, o.Change, want)
		}
		if err := v.Check(o, head); err != nil {
			t.Errorf("%s: %v", vec.name, err)
		}
		head = o.Head()
	}
}
