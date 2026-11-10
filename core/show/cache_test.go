package show_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/httpapi"
	"github.com/glumpo/pileup/core/node"
)

func (ph *phone) read(p node.Pile, local string, tier int) string {
	ph.t.Helper()
	a := ph.asset(p, local)
	f, _, err := ph.Blob(context.Background(), p.ID, a.Meta.AssetID, a.Meta.Revision, tier)
	if err != nil {
		ph.t.Fatalf("T%d of %s: %v", tier, local, err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		ph.t.Fatal(err)
	}
	return string(b)
}

func (ph *phone) thumbnail(p node.Pile, local string) bool {
	a := ph.asset(p, local)
	_, ok := ph.Cached(p.ID, a.Meta.AssetID, a.Meta.Revision, node.T1)
	return ok
}

func (ph *phone) evictFirst(p node.Pile, first, second string) {
	ph.t.Helper()
	rx := newReceiver(ph.t)
	must(ph.t)(ph.show.Start(p.ID, ph.tv(rx), ph.asset(p, first).Meta.AssetID))
	eventually(ph.t, "window cached", func() bool { return ph.cached(p, first) && ph.cached(p, second) })
	ph.show.Stop()
	eventually(ph.t, first+" evicted", func() bool { return !ph.cached(p, first) })
}

func TestEvictionInOnePileKeepsDisplayPinnedInAnother(t *testing.T) {
	a, b := newPhone(t, "ios", node.DefaultBudget), newPhone(t, "android", int64(len(display("X"))))
	p, q := pileOf(t, a, b), pileOf(t, a, b)
	a.lib.photo("X", 0)
	a.lib.photo("Y", 1)
	a.share(p)
	a.share(q)
	waitTimeline(t, p, 2, b)
	waitTimeline(t, q, 2, b)
	if string(a.asset(p, "X").Meta.AssetID) != string(a.asset(q, "X").Meta.AssetID) {
		t.Fatal("one file of the owner has different ids in two piles")
	}
	if _, err := b.show.Pin(p.ID, member(a)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "pinned files loaded", func() bool { return counts(progressOf(t, b, p.ID)["Аня"]) == [3]int{2, 2, 0} })

	b.evictFirst(q, "X", "Y")
	if !b.cached(p, "X") {
		t.Fatal("eviction in one pile dropped the file pinned in another")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if got := b.read(p, "X", node.T2); got != display("X") {
		t.Fatalf("pinned T2 after eviction in another pile: %q", got)
	}
}

func TestEvictedDisplayKeepsThumbnailWithSameBytes(t *testing.T) {
	a, b := newPhone(t, "ios", node.DefaultBudget), newPhone(t, "android", int64(len(display("Y"))))
	p := pileOf(t, a, b)
	a.lib.content["X"] = "tiny X"
	a.lib.photo("X", 0)
	a.lib.photo("Y", 1)
	a.share(p)
	waitTimeline(t, p, 2, b)
	eventually(t, "thumbnail of X stored", func() bool { return b.thumbnail(p, "X") })

	b.evictFirst(p, "X", "Y")
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if got := b.read(p, "X", node.T1); got != "tiny X" {
		t.Fatalf("T1 after eviction of T2 with the same bytes: %q", got)
	}
}

func TestEvictedFileKeepsOtherFileWithSameBytes(t *testing.T) {
	a, b := newPhone(t, "ios", node.DefaultBudget), newPhone(t, "android", int64(len("same bytes")))
	p := pileOf(t, a, b)
	a.lib.content["X"] = "same bytes"
	a.lib.content["Z"] = "same bytes"
	a.lib.photo("X", 0)
	a.lib.photo("Y", 1)
	a.lib.photo("Z", 2)
	a.share(p)
	waitTimeline(t, p, 3, b)

	b.evictFirst(p, "X", "Z")
	if !b.cached(p, "Z") {
		t.Fatal("file in the window is not cached")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if got := b.read(p, "Z", node.T2); got != "same bytes" {
		t.Fatalf("T2 of the other file with the same bytes: %q", got)
	}
}

func (ph *phone) view(ui string, p node.Pile, a state.Asset) string {
	ph.t.Helper()
	resp, body := get(ph.t, fmt.Sprintf("%s/piles/%x/blob/%x/%x/2", ui, p.ID, a.Meta.AssetID, a.Meta.Revision), "Authorization", "Bearer ui")
	if resp.StatusCode != http.StatusOK {
		ph.t.Fatalf("viewer got status %d for %s", resp.StatusCode, a.Meta.OriginalName)
	}
	return string(body)
}

func TestDisplaysOpenedInViewerAreKeptWithinBudget(t *testing.T) {
	a, b := newPhone(t, "ios", node.DefaultBudget), newPhone(t, "android", 2*int64(len(display("P0"))))
	p := pileOf(t, a, b)
	var locals []string
	for i := range 5 {
		locals = append(locals, fmt.Sprintf("P%d", i))
		a.lib.photo(locals[i], i)
	}
	a.share(p)
	waitTimeline(t, p, 5, b)
	ui := httptest.NewServer(httpapi.WithNode("ui", b.Node, b.show))
	t.Cleanup(ui.Close)

	for _, l := range locals {
		if got := b.view(ui.URL, p, b.asset(p, l)); got != display(l) {
			t.Fatalf("viewer got %q for %s", got, l)
		}
	}
	eventually(t, "only the latest files within the budget stay", func() bool {
		for i, l := range locals {
			if b.cached(p, l) != (i >= 3) {
				return false
			}
		}
		return true
	})
}

func TestViewerShowAndPinsShareOneAccounting(t *testing.T) {
	a, b, c := newPhone(t, "ios", node.DefaultBudget), newPhone(t, "android", 9*int64(len(display("A0")))), newPhone(t, "ios", node.DefaultBudget)
	p := pileOf(t, a, b, c)
	a.lib.photo("A0", 0)
	a.lib.photo("A1", 1)
	c.lib.photo("C0", 2)
	want := map[string]bool{"A0": true, "A1": false, "C0": true, "A2": true, "A3": true}
	for i := range 5 {
		l := fmt.Sprintf("F%d", i)
		a.lib.photo(l, 3+i)
		want[l] = true
	}
	a.lib.photo("A2", 8)
	a.lib.photo("A3", 9)
	a.share(p)
	c.share(p)
	waitTimeline(t, p, 10, b)
	ui := httptest.NewServer(httpapi.WithNode("ui", b.Node, b.show))
	t.Cleanup(ui.Close)

	if _, err := b.show.Pin(p.ID, member(c)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "pinned file loaded", func() bool { return counts(progressOf(t, b, p.ID)["участник 1"]) == [3]int{1, 1, 0} })
	b.view(ui.URL, p, b.asset(p, "A0"))
	b.view(ui.URL, p, b.asset(p, "C0"))
	rx := newReceiver(t)
	must(t)(b.show.Start(p.ID, b.tv(rx), b.asset(p, "A2").Meta.AssetID))
	eventually(t, "window cached", func() bool {
		for l, w := range want {
			if w && !b.cached(p, l) {
				return false
			}
		}
		return true
	})
	b.view(ui.URL, p, b.asset(p, "A2"))

	for l, w := range want {
		if b.cached(p, l) != w {
			t.Fatalf("%s cached %v, want %v", l, !w, w)
		}
	}
	if n, m := a.lib.displayed("A2"), c.lib.displayed("C0"); n != 1 || m != 1 {
		t.Fatalf("sources rendered A2 %d and C0 %d times, want once each", n, m)
	}
}

func TestViewerCacheAccountingSurvivesRestart(t *testing.T) {
	size := int64(len(display("P0")))
	a := newPhone(t, "ios", node.DefaultBudget)
	dir, lib := t.TempDir(), newLibrary()
	b := openPhone(t, dir, "android", lib, 2*size)
	p := pileOf(t, a, b)
	for i := range 3 {
		a.lib.photo(fmt.Sprintf("P%d", i), i)
	}
	a.share(p)
	waitTimeline(t, p, 3, b)
	ui := httptest.NewServer(httpapi.WithNode("ui", b.Node, b.show))
	b.view(ui.URL, p, b.asset(p, "P0"))
	b.view(ui.URL, p, b.asset(p, "P1"))
	ui.Close()
	b.show.Close()
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	b = openPhone(t, dir, "android", lib, 2*size)
	ui = httptest.NewServer(httpapi.WithNode("ui", b.Node, b.show))
	t.Cleanup(ui.Close)
	b.view(ui.URL, p, b.asset(p, "P2"))
	for i, want := range []bool{false, true, true} {
		if l := fmt.Sprintf("P%d", i); b.cached(p, l) != want {
			t.Fatalf("%s cached %v after restart, want %v", l, !want, want)
		}
	}
}
