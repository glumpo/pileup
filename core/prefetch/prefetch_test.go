package prefetch_test

import (
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/prefetch"
)

var t0 = time.Unix(1_700_000_000, 0)

func id(i int) string { return fmt.Sprintf("a%03d", i) }

func ids(from, to int) []string {
	var out []string
	for i := from; i < to; i++ {
		out = append(out, id(i))
	}
	return out
}

func behind(cursor int) []string {
	var out []string
	for i := cursor - 1; i >= 0 && i >= cursor-5; i-- {
		out = append(out, id(i))
	}
	return out
}

func chronology(n int, src func(i int) string) []prefetch.Item {
	items := make([]prefetch.Item, n)
	for i := range items {
		items[i] = prefetch.Item{ID: id(i), TakenAt: int64(i) * 1000, Source: src(i)}
	}
	return items
}

func ownSource(i int) string { return fmt.Sprintf("s%d", i) }

func oneSource(int) string { return "ann" }

type cache struct {
	*prefetch.Cache
	sizes map[string]int64
}

func newCache(items []prefetch.Item) *cache {
	c := &cache{Cache: prefetch.New(), sizes: map[string]int64{}}
	c.SetItems(items)
	return c
}

func (c *cache) loaded(list ...string) {
	for _, x := range list {
		c.Loaded(x)
		c.sizes[x] = 1
	}
	c.SetCached(maps.Clone(c.sizes))
}

func (c *cache) dropped(list ...string) {
	for _, x := range list {
		delete(c.sizes, x)
	}
	c.SetCached(maps.Clone(c.sizes))
}

func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

func TestNoCursorNoLoads(t *testing.T) {
	c := newCache(chronology(30, ownSource))
	load := c.Plan(t0)
	equal(t, "load", load, nil)
}

func TestLoadsCursorAndNextTwentyThenFiveBeforeInOrder(t *testing.T) {
	c := newCache(chronology(60, ownSource))
	c.SetCursor(id(10))
	load := c.Plan(t0)
	equal(t, "load", load, slices.Concat(ids(10, 31), behind(10)))
	load = c.Plan(t0)
	equal(t, "load while in flight", load, nil)
}

func TestWindowStopsAtEndsOfChronology(t *testing.T) {
	c := newCache(chronology(15, ownSource))
	c.SetCursor(id(3))
	load := c.Plan(t0)
	equal(t, "load", load, slices.Concat(ids(3, 15), behind(3)))
}

func TestSkippedItemsAreNotLoadedOrCounted(t *testing.T) {
	items := chronology(40, ownSource)
	for _, i := range []int{3, 4, 7} {
		items[i].Skip = true
	}
	c := newCache(items)
	c.SetCursor(id(0))
	load := c.Plan(t0)
	want := slices.DeleteFunc(ids(0, 24), func(x string) bool { return x == id(3) || x == id(4) || x == id(7) })
	equal(t, "load", load, want)
}

func TestSkippedCursorItemIsStillLoaded(t *testing.T) {
	items := chronology(40, ownSource)
	items[2].Skip = true
	c := newCache(items)
	c.SetCursor(id(2))
	load := c.Plan(t0)
	equal(t, "load", load, slices.Concat(ids(2, 23), behind(2)))
}

func TestCursorStepLoadsOnlyTheNewItem(t *testing.T) {
	c := newCache(chronology(60, ownSource))
	c.SetCursor(id(0))
	load := c.Plan(t0)
	c.loaded(load...)
	c.SetCursor(id(1))
	load = c.Plan(t0)
	equal(t, "load", load, []string{id(21)})
}

func TestNewItemsInsideWindowAreLoaded(t *testing.T) {
	items := chronology(30, ownSource)
	c := newCache(items)
	c.SetCursor(id(0))
	load := c.Plan(t0)
	c.loaded(load...)
	late := prefetch.Item{ID: "late", TakenAt: 4500, Source: "late"}
	c.SetItems(slices.Insert(slices.Clone(items), 5, late))
	load = c.Plan(t0)
	equal(t, "load", load, []string{"late"})
}

func TestJumpForwardKeepsEarlierCache(t *testing.T) {
	c := newCache(chronology(200, ownSource))
	c.SetCursor(id(0))
	load := c.Plan(t0)
	c.loaded(load...)
	c.SetCursor(id(150))
	load = c.Plan(t0)
	equal(t, "load", load, slices.Concat(ids(150, 171), behind(150)))
	c.loaded(load...)
	c.SetCursor(id(0))
	load = c.Plan(t0)
	equal(t, "load after jumping back", load, nil)
}

func TestJumpBackwardLoadsNewWindow(t *testing.T) {
	c := newCache(chronology(200, ownSource))
	c.SetCursor(id(150))
	load := c.Plan(t0)
	c.loaded(load...)
	c.SetCursor(id(140))
	load = c.Plan(t0)
	equal(t, "load", load, slices.Concat(ids(140, 145), behind(140)))
	c.SetCursor(id(20))
	load = c.Plan(t0)
	equal(t, "load", load, slices.Concat(ids(20, 41), behind(20)))
}

func TestItemsBeforeCursorWaitForNextItemsOfTheirSource(t *testing.T) {
	c := newCache(chronology(100, oneSource))
	c.SetCursor(id(50))
	for _, want := range [][]string{ids(50, 55), ids(55, 60), ids(60, 65), ids(65, 70), {id(70), id(49), id(48), id(47), id(46)}, {id(45)}, nil} {
		load := c.Plan(t0)
		equal(t, "load", load, want)
		c.loaded(load...)
	}
}

func TestAtMostFiveLoadsPerSourceInChronologicalOrder(t *testing.T) {
	items := chronology(100, func(i int) string {
		if i%2 == 0 {
			return "ann"
		}
		return "bob"
	})
	c := newCache(items)
	c.SetCursor(id(0))
	load := c.Plan(t0)
	equal(t, "load", load, ids(0, 10))
	c.loaded(id(4))
	c.loaded(id(1))
	c.loaded(id(3))
	load = c.Plan(t0)
	equal(t, "load", load, []string{id(10), id(11), id(13)})
}

func TestFailedSourceWaitsWhileOthersLoad(t *testing.T) {
	items := chronology(100, func(i int) string {
		if i%2 == 0 {
			return "ann"
		}
		return "bob"
	})
	c := newCache(items)
	c.SetCursor(id(0))
	c.Plan(t0)
	c.Failed(id(0), t0)
	c.loaded(id(1))
	load := c.Plan(t0)
	equal(t, "load", load, []string{id(11)})
	equal(t, "waiting", c.Waiting(), []string{id(0), id(2), id(4), id(6), id(8), id(10), id(12), id(14), id(16), id(18), id(20)})
}

func TestRetryWithBackoffAndSingleProbe(t *testing.T) {
	c := newCache(chronology(100, oneSource))
	c.SetCursor(id(0))
	c.Plan(t0)
	for _, x := range ids(0, 5) {
		c.Failed(x, t0.Add(100*time.Millisecond))
	}
	now := t0.Add(100 * time.Millisecond)
	for i, delay := range []time.Duration{1, 2, 4, 8, 16, 30, 30} {
		delay *= time.Second
		load := c.Plan(now.Add(delay - time.Millisecond))
		equal(t, fmt.Sprintf("load before retry %d", i), load, nil)
		now = now.Add(delay)
		load = c.Plan(now)
		equal(t, fmt.Sprintf("probe %d", i), load, []string{id(0)})
		load = c.Plan(now)
		equal(t, fmt.Sprintf("load during probe %d", i), load, nil)
		c.Failed(id(0), now)
	}
	now = now.Add(prefetch.MaxBackoff)
	load := c.Plan(now)
	equal(t, "probe", load, []string{id(0)})
	c.loaded(id(0))
	equal(t, "waiting", c.Waiting(), nil)
	load = c.Plan(now)
	equal(t, "load after recovery", load, ids(1, 6))
}

func TestWaitingItemsAreInTimelineOrder(t *testing.T) {
	c := newCache(chronology(40, oneSource))
	c.SetOnline("ann", false)
	c.SetCursor(id(10))
	equal(t, "waiting", c.Waiting(), ids(5, 31))
}

func TestOfflineSourceWaitsUntilOnline(t *testing.T) {
	items := chronology(100, func(i int) string {
		if i < 3 {
			return "ann"
		}
		return "bob"
	})
	c := newCache(items)
	c.SetOnline("ann", false)
	c.SetCursor(id(0))
	load := c.Plan(t0)
	equal(t, "load", load, ids(3, 8))
	equal(t, "waiting", c.Waiting(), ids(0, 3))
	c.SetOnline("ann", true)
	equal(t, "waiting", c.Waiting(), nil)
	load = c.Plan(t0)
	equal(t, "load", load, ids(0, 3))
}

func TestComingOnlineCancelsBackoff(t *testing.T) {
	c := newCache(chronology(100, oneSource))
	c.SetCursor(id(0))
	c.Plan(t0)
	for _, x := range ids(0, 5) {
		c.Failed(x, t0)
	}
	c.SetOnline("ann", false)
	c.SetOnline("ann", true)
	load := c.Plan(t0)
	equal(t, "load", load, ids(0, 5))
}

func TestLoadFinishedAfterJumpIsKept(t *testing.T) {
	c := newCache(chronology(200, oneSource))
	c.SetCursor(id(0))
	c.Plan(t0)
	c.SetCursor(id(100))
	load := c.Plan(t0)
	equal(t, "load", load, nil)
	c.loaded(ids(0, 5)...)
	load = c.Plan(t0)
	equal(t, "load", load, ids(100, 105))
}

func pinAll(c *cache, list []string) {
	for _, x := range list {
		c.Pin(x)
	}
}

func TestPinnedItemsLoadAfterWindowFromCursor(t *testing.T) {
	c := newCache(chronology(60, ownSource))
	pinAll(c, ids(0, 10))
	pinAll(c, ids(55, 60))
	c.SetCursor(id(30))
	load := c.Plan(t0)
	equal(t, "load", load, slices.Concat(ids(30, 51), behind(30), ids(55, 60), ids(0, 10)))
}

func TestPinnedItemsLoadWithoutCursorInChronologicalOrder(t *testing.T) {
	c := newCache(chronology(30, ownSource))
	pinAll(c, []string{id(20), id(3), id(11)})
	load := c.Plan(t0)
	equal(t, "load", load, []string{id(3), id(11), id(20)})
}

func TestPinnedItemsShareFiveLoadsPerSource(t *testing.T) {
	c := newCache(chronology(100, oneSource))
	pinAll(c, ids(0, 100))
	c.SetCursor(id(90))
	load := c.Plan(t0)
	equal(t, "load", load, ids(90, 95))
	c.loaded(load...)
	load = c.Plan(t0)
	equal(t, "load", load, ids(95, 100))
	c.loaded(load...)
	load = c.Plan(t0)
	equal(t, "load before the cursor", load, behind(90))
	c.loaded(load...)
	load = c.Plan(t0)
	equal(t, "load after the end of chronology", load, ids(0, 5))
}

func TestPinnedItemsAreAllLoaded(t *testing.T) {
	c := newCache(chronology(100, oneSource))
	pinAll(c, ids(0, 100))
	for range 20 {
		c.loaded(c.Plan(t0)...)
	}
	if p := c.Progress()["ann"]; p != (prefetch.Progress{Total: 100, Done: 100}) {
		t.Fatalf("progress %+v", p)
	}
}

func TestWindowIsCursorTwentyNextAndFiveBeforeShownItems(t *testing.T) {
	items := chronology(40, ownSource)
	items[12].Skip = true
	items[7].Skip = true
	c := newCache(items)
	equal(t, "window without cursor", c.Window(), nil)
	c.SetCursor(id(10))
	want := slices.DeleteFunc(ids(10, 32), func(x string) bool { return x == id(12) })
	equal(t, "window", c.Window(), slices.Concat(want, []string{id(9), id(8), id(6), id(5), id(4)}))
}

func TestItemDroppedFromCacheIsLoadedAgainWhenWanted(t *testing.T) {
	c := newCache(chronology(60, ownSource))
	pinAll(c, []string{id(40)})
	c.SetCursor(id(0))
	c.loaded(c.Plan(t0)...)
	c.dropped(id(3), id(40), id(50))
	equal(t, "load", c.Plan(t0), []string{id(3), id(40)})
}

func TestBrokenItemDoesNotBlockItsSource(t *testing.T) {
	c := newCache(chronology(100, oneSource))
	c.SetCursor(id(0))
	load := c.Plan(t0)
	equal(t, "load", load, ids(0, 5))
	c.Broken(id(0), t0)
	load = c.Plan(t0)
	equal(t, "load after broken", load, []string{id(5)})
	c.Failed(id(1), t0)
	c.Broken(id(2), t0)
	equal(t, "waiting", c.Waiting(), nil)
	load = c.Plan(t0)
	equal(t, "load after the source answered", load, []string{id(1), id(6)})
	c.SetOnline("ann", false)
	equal(t, "waiting", c.Waiting(), slices.DeleteFunc(ids(1, 21), func(x string) bool { return x == id(2) }))
	c.SetOnline("ann", true)
	c.loaded(id(3), id(4), id(5))
	for range 10 {
		c.loaded(load...)
		load = c.Plan(t0)
		for _, x := range load {
			if x == id(0) || x == id(2) {
				t.Fatalf("broken %s is loaded again", x)
			}
		}
	}
}

func TestBrokenItemIsPlannedAgainAfterRecheck(t *testing.T) {
	c := newCache(chronology(10, oneSource))
	pinAll(c, ids(0, 10))
	equal(t, "load", c.Plan(t0), ids(0, 5))
	c.Broken(id(0), t0)
	c.loaded(ids(1, 5)...)
	c.loaded(c.Plan(t0)...)
	progress := func(want prefetch.Progress) {
		t.Helper()
		if p := c.Progress()["ann"]; p != want {
			t.Fatalf("progress %+v, want %+v", p, want)
		}
	}
	progress(prefetch.Progress{Total: 10, Done: 9, Failed: 1})
	equal(t, "load before the recheck", c.Plan(t0.Add(prefetch.Recheck-time.Second)), nil)
	at := t0.Add(prefetch.Recheck)
	equal(t, "load after the recheck", c.Plan(at), []string{id(0)})
	progress(prefetch.Progress{Total: 10, Done: 9})
	c.Broken(id(0), at)
	progress(prefetch.Progress{Total: 10, Done: 9, Failed: 1})
	equal(t, "load before the next recheck", c.Plan(at.Add(prefetch.Recheck-time.Second)), nil)
	at = at.Add(prefetch.Recheck)
	equal(t, "load after the next recheck", c.Plan(at), []string{id(0)})
	c.loaded(id(0))
	progress(prefetch.Progress{Total: 10, Done: 10})
}

func TestProgressCountsPinnedItemsPerSource(t *testing.T) {
	c := newCache(chronology(10, func(i int) string {
		if i < 4 {
			return "ann"
		}
		return "bob"
	}))
	pinAll(c, ids(0, 4))
	pinAll(c, []string{id(4), id(5)})
	c.loaded(id(9))
	c.Plan(t0)
	c.loaded(id(0))
	c.loaded(id(1))
	c.Broken(id(2), t0)
	c.loaded(id(4))
	c.Failed(id(5), t0)
	got := c.Progress()
	want := map[string]prefetch.Progress{"ann": {Total: 4, Done: 2, Failed: 1}, "bob": {Total: 2, Done: 1}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("progress %v, want %v", got, want)
	}
	if c.Unavailable("ann") || !c.Unavailable("bob") {
		t.Fatalf("unavailable: ann %v, bob %v", c.Unavailable("ann"), c.Unavailable("bob"))
	}
	if size, ok := c.Size(id(9)); !ok || size != 1 {
		t.Fatalf("size of a cached item: %d %v", size, ok)
	}
	if _, ok := c.Size(id(3)); ok {
		t.Fatal("size of a missing item")
	}
}
