package node_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"path/filepath"
	"sync"
	"testing"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/thumbhash"
)

type gallery struct {
	mu      sync.Mutex
	items   []node.MediaItem
	colors  map[string]color.RGBA
	broken  map[string]bool
	renders map[string]int
}

func newGallery() *gallery {
	return &gallery{colors: map[string]color.RGBA{}, broken: map[string]bool{}, renders: map[string]int{}}
}

func (g *gallery) put(item node.MediaItem, c color.RGBA) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, it := range g.items {
		if it.LocalID == item.LocalID {
			g.items = append(g.items[:i], g.items[i+1:]...)
			break
		}
	}
	g.items = append(g.items, item)
	g.colors[item.LocalID] = c
}

func (g *gallery) breakFile(local string, broken bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.broken[local] = broken
}

func (g *gallery) rendered(local string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.renders[local]
}

func (g *gallery) Media(from, to int64) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return json.Marshal(g.items)
}

func (g *gallery) Rendition(local string, tier int) ([]byte, error) {
	g.mu.Lock()
	c, ok := g.colors[local]
	broken := g.broken[local]
	g.renders[local]++
	g.mu.Unlock()
	if !ok || broken {
		return nil, errors.New("cannot render")
	}
	img := image.NewRGBA(image.Rect(0, 0, 128, 96))
	for y := range 96 {
		for x := range 128 {
			img.SetRGBA(x, y, color.RGBA{c.R, c.G + uint8(y/2), c.B + uint8(x/2), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (g *gallery) OriginalSize(local string) (int64, error) {
	return 0, errors.New("no originals")
}

func (g *gallery) ReadOriginal(local string, offset, length int64) ([]byte, error) {
	return nil, errors.New("no originals")
}

func openGallery(t *testing.T, dir string, g *gallery) *node.Node {
	t.Helper()
	n, err := node.Open(node.Config{
		Dir:        dir,
		Keystore:   device.File(filepath.Join(dir, "device.key")),
		Platform:   "ios",
		Addrs:      loopback,
		Media:      g,
		Renditions: g,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	return n
}

func tint(i int) color.RGBA {
	return color.RGBA{uint8(40 + i*7), uint8(200 - i*3), uint8(i * 11), 255}
}

func hashOfT1(t *testing.T, n *node.Node, p node.Pile, m op.AssetMeta) []byte {
	t.Helper()
	f, _, err := n.Blob(context.Background(), p.ID, m.AssetID, m.Revision, node.T1)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return thumbhash.Encode(img)
}

func hashed(t *testing.T, n *node.Node, p node.Pile, metas ...op.AssetMeta) func() bool {
	return func() bool {
		as := assetsOn(t, n, p.ID)
		for _, m := range metas {
			a, ok := as[string(m.AssetID)]
			if !ok || !bytes.Equal(a.Meta.Revision, m.Revision) || len(a.Meta.Thumbhash) == 0 {
				return false
			}
		}
		return true
	}
}

func TestThumbhashOfEveryOwnAssetReachesMembersInBoundedBatches(t *testing.T) {
	g := newGallery()
	a := openGallery(t, t.TempDir(), g)
	dirB := t.TempDir()
	b := open(t, dirB, "android")
	p := create(t, a, "Грузия", "Аня")
	join(t, b, inviteTo(t, a, p), "Боря")
	total := node.ThumbhashBatch + 7
	for i := range total - 1 {
		g.put(photo(fmt.Sprintf("IMG_%d", i), day), tint(i))
	}
	g.put(video("VID_1", day, 9000), tint(total))
	shared := share(t, a, p)
	var metas []op.AssetMeta
	for _, m := range shared {
		metas = append(metas, m)
	}
	if len(metas) != total {
		t.Fatalf("selected %d files, want %d", len(metas), total)
	}

	eventually(t, "b sees a thumbhash of every asset of a", hashed(t, b, p, metas...))
	onB := assetsOn(t, b, p.ID)
	for _, m := range metas {
		got := onB[string(m.AssetID)].Meta.Thumbhash
		if want := hashOfT1(t, b, p, m); !bytes.Equal(got, want) {
			t.Fatalf("%s: thumbhash %x, want ThumbHash of its T1 %x", m.OriginalName, got, want)
		}
	}
	for _, m := range metas {
		want := m
		want.Thumbhash = onB[string(m.AssetID)].Meta.Thumbhash
		if !sameMeta(want, onB[string(m.AssetID)].Meta) {
			t.Fatalf("%s: metadata on b %+v, want the selected one with a thumbhash", m.OriginalName, onB[string(m.AssetID)].Meta)
		}
	}

	secret := secretOf(t, b, p)
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	changes := changesBy(t, logOf(t, dirB, p, secret), a.Author())
	first, ok := changes[0].(op.AssetAdd)
	if !ok || len(first.Assets) != total {
		t.Fatalf("selection published %+v, want one AssetAdd with all %d files", changes[0], total)
	}
	for _, m := range first.Assets {
		if len(m.Thumbhash) != 0 {
			t.Fatalf("selection waited for the thumbhash of %s", m.OriginalName)
		}
	}
	updated := 0
	for _, c := range changes[1:] {
		add, ok := c.(op.AssetAdd)
		if !ok || len(add.Assets) == 0 || len(add.Assets) > node.ThumbhashBatch {
			t.Fatalf("thumbhash update %+v, want an AssetAdd of 1..%d assets", c, node.ThumbhashBatch)
		}
		updated += len(add.Assets)
	}
	if updated != total {
		t.Fatalf("thumbhash updates carried %d assets, want each of %d once", updated, total)
	}
}

func sameMeta(a, b op.AssetMeta) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func TestReselectingKeepsThumbhashes(t *testing.T) {
	g := newGallery()
	a := openGallery(t, t.TempDir(), g)
	p := create(t, a, "Грузия", "Аня")
	g.put(photo("IMG_1", day), tint(1))
	g.put(photo("IMG_2", day), tint(2))
	var metas []op.AssetMeta
	for _, m := range share(t, a, p) {
		metas = append(metas, m)
	}
	eventually(t, "thumbhashes are published", hashed(t, a, p, metas...))
	before := assetsOn(t, a, p.ID)

	choose(t, a, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")
	if after := assetsOn(t, a, p.ID); !sameAssets(after, before) {
		t.Fatalf("after selecting the same range again: %+v, want %+v", after, before)
	}
}

func sameAssets(a, b map[string]state.Asset) bool {
	if len(a) != len(b) {
		return false
	}
	for id, x := range a {
		if y, ok := b[id]; !ok || x.Owner != y.Owner || !sameMeta(x.Meta, y.Meta) {
			return false
		}
	}
	return true
}

func TestEditedFileGetsNewThumbhash(t *testing.T) {
	g := newGallery()
	a := openGallery(t, t.TempDir(), g)
	b := open(t, t.TempDir(), "android")
	p := create(t, a, "Грузия", "Аня")
	join(t, b, inviteTo(t, a, p), "Боря")
	original := photo("IMG_1", day)
	g.put(original, tint(1))
	old := share(t, a, p)["IMG_1"]
	eventually(t, "b sees the thumbhash", hashed(t, b, p, old))
	oldHash := assetsOn(t, b, p.ID)[string(old.AssetID)].Meta.Thumbhash

	edited := original
	edited.Modified += 60_000
	g.put(edited, color.RGBA{10, 20, 230, 255})
	cur := share(t, a, p)["IMG_1"]
	if bytes.Equal(cur.Revision, old.Revision) {
		t.Fatal("edit kept the revision")
	}
	eventually(t, "b sees the thumbhash of the new revision", hashed(t, b, p, cur))
	got := assetsOn(t, b, p.ID)[string(cur.AssetID)].Meta.Thumbhash
	if want := hashOfT1(t, b, p, cur); !bytes.Equal(got, want) || bytes.Equal(got, oldHash) {
		t.Fatalf("thumbhash after edit %x, want ThumbHash of the new T1 %x (before edit %x)", got, want, oldHash)
	}
}

func TestFailingRenditionDoesNotBlockOthersAndIsRetriedAfterRestart(t *testing.T) {
	g := newGallery()
	dir := t.TempDir()
	a := openGallery(t, dir, g)
	p := create(t, a, "Грузия", "Аня")
	for i := range 3 {
		g.put(photo(fmt.Sprintf("IMG_%d", i), day), tint(i))
	}
	g.breakFile("IMG_1", true)
	shared := share(t, a, p)
	healthy := []op.AssetMeta{shared["IMG_0"], shared["IMG_2"]}
	broken := shared["IMG_1"]
	eventually(t, "healthy files get thumbhashes", hashed(t, a, p, healthy...))
	if hashed(t, a, p, broken)() {
		t.Fatal("a file that cannot be rendered got a thumbhash")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	g.breakFile("IMG_1", false)
	a = openGallery(t, dir, g)
	eventually(t, "the file that failed gets a thumbhash after restart", hashed(t, a, p, broken))
	for _, local := range []string{"IMG_0", "IMG_2"} {
		if r := g.rendered(local); r != 1 {
			t.Fatalf("%s rendered %d times across the restart, want once", local, r)
		}
	}
}
