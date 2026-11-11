package node_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/node"
)

func timelineOf(t *testing.T, n *node.Node, id []byte) (node.Pile, map[string]state.Asset) {
	t.Helper()
	p, items, err := n.Timeline(id)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]state.Asset{}
	for _, a := range items {
		out[string(a.Meta.AssetID)] = a
	}
	return p, out
}

func hasMember(p node.Pile, name string) bool {
	for _, m := range p.Members {
		if m.Name == name {
			return true
		}
	}
	return false
}

func TestTimelineAndPileListShowEveryAppliedChange(t *testing.T) {
	ga, gb := newGallery(), newGallery()
	a := openGallery(t, t.TempDir(), ga)
	b := openGallery(t, t.TempDir(), gb)
	p := create(t, a, "Грузия", "Аня")
	timelineOf(t, a, p.ID)
	pileOn(t, a, p.ID)

	ga.put(photo("A1", day), tint(1))
	mine := share(t, a, p)["A1"]
	if _, as := timelineOf(t, a, p.ID); len(as) != 1 || as[string(mine.AssetID)].Owner != a.Author() {
		t.Fatalf("own file is not in the next timeline: %v", as)
	}
	eventually(t, "own thumbhash in the timeline", func() bool {
		_, as := timelineOf(t, a, p.ID)
		return len(as[string(mine.AssetID)].Meta.Thumbhash) > 0
	})

	join(t, b, inviteTo(t, a, p), "Боря")
	eventually(t, "joined member is named in the pile list and the timeline", func() bool {
		tp, _ := timelineOf(t, a, p.ID)
		return hasMember(pileOn(t, a, p.ID), "Боря") && hasMember(tp, "Боря")
	})

	original := photo("B1", "2026-07-02T11:00:00Z")
	gb.put(original, tint(2))
	theirs := share(t, b, p)["B1"]
	eventually(t, "peer file with its thumbhash in the timeline", func() bool {
		_, as := timelineOf(t, a, p.ID)
		got := as[string(theirs.AssetID)]
		return got.Owner == b.Author() && bytes.Equal(got.Meta.Revision, theirs.Revision) && len(got.Meta.Thumbhash) > 0
	})

	edited := original
	edited.Modified += 60_000
	gb.put(edited, tint(3))
	revised := share(t, b, p)["B1"]
	eventually(t, "new revision of the peer file in the timeline", func() bool {
		_, as := timelineOf(t, a, p.ID)
		return bytes.Equal(as[string(revised.AssetID)].Meta.Revision, revised.Revision)
	})

	decide(t, b, p.ID, revised.AssetID, false)
	eventually(t, "removed peer file leaves the timeline", func() bool {
		_, as := timelineOf(t, a, p.ID)
		_, ok := as[string(revised.AssetID)]
		return !ok
	})

	decide(t, a, p.ID, mine.AssetID, false)
	if _, as := timelineOf(t, a, p.ID); len(as) != 0 {
		t.Fatalf("own removed file is still in the next timeline: %v", as)
	}
}

func manyPhotos(n int) *library {
	items := make([]any, n)
	start := ms(day)
	for i := range items {
		it := photo(fmt.Sprintf("P%05d", i), day)
		v := start + int64(i)*1000
		it.PlatformTime = &v
		items[i] = it
	}
	return shelf(items...)
}

func TestRepeatedTimelineAndPileListDoNotReadEveryFile(t *testing.T) {
	const files = 1000
	a := openWith(t, t.TempDir(), "ios", manyPhotos(files))
	p := create(t, a, "Грузия", "Аня")
	choose(t, a, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")
	if _, as := timelineOf(t, a, p.ID); len(as) != files {
		t.Fatalf("timeline has %d files, want %d", len(as), files)
	}
	timeline := testing.AllocsPerRun(20, func() { a.Timeline(p.ID) })
	piles := testing.AllocsPerRun(20, func() { a.Piles() })
	if timeline >= files || piles >= files {
		t.Fatalf("with %d files a repeated timeline costs %.0f allocations and the pile list %.0f", files, timeline, piles)
	}
}
