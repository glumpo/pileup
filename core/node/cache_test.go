package node_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/glumpo/pileup/core/node"
)

func onDisk(t *testing.T, dir string, content []byte) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		found = found || bytes.Equal(b, content)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestAssetRemovedWhileLoadingDoesNotReappear(t *testing.T) {
	m := newMedia()
	dirA, dirB := t.TempDir(), t.TempDir()
	a, b := openSource(t, dirA, "ios", m), open(t, dirB, "android")
	p := create(t, a, "Грузия", "Аня")
	m.add(photo("IMG_1", day), []byte("jpeg"))
	m.add(photo("IMG_2", day), []byte("jpeg"))
	shared := share(t, a, p)
	img, other := shared["IMG_1"], shared["IMG_2"]
	join(t, b, inviteTo(t, a, p), "Боря")
	eventually(t, "b learns the asset", func() bool { return b.Stored(p.ID, img.AssetID, img.Revision, node.T1) })
	hold := make(chan chan struct{})
	m.mu.Lock()
	m.hold = hold
	m.mu.Unlock()

	result := make(chan error)
	go func() {
		f, _, err := b.Blob(context.Background(), p.ID, img.AssetID, img.Revision, node.T2)
		if err == nil {
			f.Close()
		}
		result <- err
	}()
	release := <-hold
	decide(t, a, p.ID, img.AssetID, false)
	waitAssets(t, p.ID, idSet(other.AssetID), a, b)
	close(release)
	if err := <-result; !errors.Is(err, node.ErrUnknownAsset) {
		t.Fatalf("T2 of an asset removed while it was loading: err = %v, want ErrUnknownAsset", err)
	}
	display := rendition("IMG_1", node.T2)
	for name, dir := range map[string]string{"owner": dirA, "member": dirB} {
		if onDisk(t, dir, display) {
			t.Fatalf("T2 of the removed asset is stored on the %s", name)
		}
	}
}

func TestRemovedAssetAndOldRevisionLeaveNothingOnDisk(t *testing.T) {
	g := newGallery()
	dirA, dirB := t.TempDir(), t.TempDir()
	a, b := openGallery(t, dirA, g), open(t, dirB, "android")
	p := create(t, a, "Грузия", "Аня")
	join(t, b, inviteTo(t, a, p), "Боря")
	edited := photo("IMG_1", day)
	g.put(photo("IMG_0", day), tint(0))
	g.put(edited, tint(1))
	shared := share(t, a, p)
	removed, old := shared["IMG_0"], shared["IMG_1"]
	eventually(t, "b stores T1 of both", func() bool {
		return b.Stored(p.ID, removed.AssetID, removed.Revision, node.T1) && b.Stored(p.ID, old.AssetID, old.Revision, node.T1)
	})
	for _, x := range []struct {
		n *node.Node
		m []byte
		r []byte
	}{{a, removed.AssetID, removed.Revision}, {a, old.AssetID, old.Revision}, {b, removed.AssetID, removed.Revision}, {b, old.AssetID, old.Revision}} {
		f, _, err := x.n.Blob(context.Background(), p.ID, x.m, x.r, node.T2)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	removedBytes, err := g.Rendition("IMG_0", node.T1)
	if err != nil {
		t.Fatal(err)
	}
	oldBytes, err := g.Rendition("IMG_1", node.T1)
	if err != nil {
		t.Fatal(err)
	}

	decide(t, a, p.ID, removed.AssetID, false)
	edited.Modified += 60_000
	g.put(edited, tint(7))
	cur := share(t, a, p)["IMG_1"]
	waitAssets(t, p.ID, idSet(cur.AssetID), a, b)
	eventually(t, "b stores T1 of the new revision", func() bool { return b.Stored(p.ID, cur.AssetID, cur.Revision, node.T1) })
	for name, dir := range map[string]string{"owner": dirA, "member": dirB} {
		eventually(t, "renditions of the removed asset and the old revision are gone on the "+name, func() bool {
			return !onDisk(t, dir, removedBytes) && !onDisk(t, dir, oldBytes)
		})
	}
}
