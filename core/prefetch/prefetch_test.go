package prefetch_test

import (
	"fmt"
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

func chronology(n int, src func(i int) string) []prefetch.Item {
	items := make([]prefetch.Item, n)
	for i := range items {
		items[i] = prefetch.Item{ID: id(i), TakenAt: int64(i) * 1000, Source: src(i)}
	}
	return items
}

func ownSource(i int) string { return fmt.Sprintf("s%d", i) }

func oneSource(int) string { return "ann" }

func newCache(budget int64, items []prefetch.Item) *prefetch.Cache {
	c := prefetch.New(budget)
	c.SetItems(items)
	return c
}

func loadAll(c *prefetch.Cache, list []string, size int64) {
	for _, x := range list {
		c.Loaded(x, size)
	}
}

func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

func TestNoCursorNoLoads(t *testing.T) {
	c := newCache(1000, chronology(30, ownSource))
	load, evict := c.Plan(t0)
	equal(t, "load", load, nil)
	equal(t, "evict", evict, nil)
}

func TestLoadsCursorAndNextTwentyInOrder(t *testing.T) {
	c := newCache(1000, chronology(60, ownSource))
	c.SetCursor(id(10))
	load, _ := c.Plan(t0)
	equal(t, "load", load, ids(10, 31))
	load, _ = c.Plan(t0)
	equal(t, "load while in flight", load, nil)
}

func TestWindowStopsAtEndOfChronology(t *testing.T) {
	c := newCache(1000, chronology(15, ownSource))
	c.SetCursor(id(5))
	load, _ := c.Plan(t0)
	equal(t, "load", load, ids(5, 15))
}

func TestSkippedItemsAreNotLoadedOrCounted(t *testing.T) {
	items := chronology(40, ownSource)
	for _, i := range []int{3, 4, 7} {
		items[i].Skip = true
	}
	c := newCache(1000, items)
	c.SetCursor(id(0))
	load, _ := c.Plan(t0)
	want := slices.DeleteFunc(ids(0, 24), func(x string) bool { return x == id(3) || x == id(4) || x == id(7) })
	equal(t, "load", load, want)
}

func TestSkippedCursorItemIsStillLoaded(t *testing.T) {
	items := chronology(40, ownSource)
	items[2].Skip = true
	c := newCache(1000, items)
	c.SetCursor(id(2))
	load, _ := c.Plan(t0)
	equal(t, "load", load, ids(2, 23))
}

func TestCursorStepLoadsOnlyTheNewItem(t *testing.T) {
	c := newCache(1000, chronology(60, ownSource))
	c.SetCursor(id(0))
	load, _ := c.Plan(t0)
	loadAll(c, load, 1)
	c.SetCursor(id(1))
	load, evict := c.Plan(t0)
	equal(t, "load", load, []string{id(21)})
	equal(t, "evict", evict, nil)
}

func TestNewItemsInsideWindowAreLoaded(t *testing.T) {
	items := chronology(30, ownSource)
	c := newCache(1000, items)
	c.SetCursor(id(0))
	load, _ := c.Plan(t0)
	loadAll(c, load, 1)
	late := prefetch.Item{ID: "late", TakenAt: 4500, Source: "late"}
	c.SetItems(slices.Insert(slices.Clone(items), 5, late))
	load, _ = c.Plan(t0)
	equal(t, "load", load, []string{"late"})
}

func TestJumpForwardKeepsEarlierCacheUnderBudget(t *testing.T) {
	c := newCache(1000, chronology(200, ownSource))
	c.SetCursor(id(0))
	load, _ := c.Plan(t0)
	loadAll(c, load, 1)
	c.SetCursor(id(150))
	load, evict := c.Plan(t0)
	equal(t, "load", load, ids(150, 171))
	equal(t, "evict", evict, nil)
	loadAll(c, load, 1)
	c.SetCursor(id(0))
	load, _ = c.Plan(t0)
	equal(t, "load after jumping back", load, nil)
}

func TestJumpBackwardLoadsNewWindow(t *testing.T) {
	c := newCache(1000, chronology(200, ownSource))
	c.SetCursor(id(150))
	load, _ := c.Plan(t0)
	loadAll(c, load, 1)
	c.SetCursor(id(140))
	load, evict := c.Plan(t0)
	equal(t, "load", load, ids(140, 150))
	equal(t, "evict", evict, nil)
	c.SetCursor(id(20))
	load, _ = c.Plan(t0)
	equal(t, "load", load, ids(20, 41))
}

func TestNoEvictionWithinBudget(t *testing.T) {
	c := newCache(42, chronology(200, ownSource))
	c.SetCursor(id(0))
	load, _ := c.Plan(t0)
	loadAll(c, load, 2)
	c.SetCursor(id(100))
	_, evict := c.Plan(t0)
	equal(t, "evict", evict, nil)
}

func TestEvictsEarliestTakenAtFirst(t *testing.T) {
	c := newCache(21*10+5*10, chronology(200, ownSource))
	c.SetCursor(id(100))
	load, _ := c.Plan(t0)
	loadAll(c, load, 10)
	c.SetCursor(id(50))
	load, _ = c.Plan(t0)
	loadAll(c, load, 10)
	_, evict := c.Plan(t0)
	equal(t, "evict", evict, ids(100, 116))
	c.SetCursor(id(150))
	load, _ = c.Plan(t0)
	loadAll(c, load, 10)
	_, evict = c.Plan(t0)
	equal(t, "evict", evict, ids(50, 71))
}

func TestEvictionUsesTakenAtNotInsertionOrder(t *testing.T) {
	c := newCache(20, chronology(200, ownSource))
	c.Loaded(id(90), 10)
	c.Loaded(id(10), 10)
	c.Loaded(id(50), 10)
	c.Loaded(id(30), 10)
	_, evict := c.Plan(t0)
	equal(t, "evict", evict, []string{id(10), id(30)})
}

func TestEvictsUnknownItemsBeforeChronology(t *testing.T) {
	c := newCache(10, chronology(10, ownSource))
	c.Loaded(id(1), 10)
	c.Loaded("removed", 10)
	_, evict := c.Plan(t0)
	equal(t, "evict", evict, []string{"removed"})
}

func TestNeverEvictsCursorWindowOrPinned(t *testing.T) {
	c := newCache(0, chronology(200, ownSource))
	c.Loaded(id(5), 10)
	c.Loaded(id(6), 10)
	c.Pin(id(5))
	c.SetCursor(id(100))
	load, evict := c.Plan(t0)
	equal(t, "evict", evict, []string{id(6)})
	loadAll(c, load, 10)
	load, evict = c.Plan(t0)
	equal(t, "load", load, nil)
	equal(t, "evict", evict, nil)
	c.Unpin(id(5))
	_, evict = c.Plan(t0)
	equal(t, "evict after unpin", evict, []string{id(5)})
}

func TestBudgetShrinkEvicts(t *testing.T) {
	c := newCache(1000, chronology(200, ownSource))
	for i := range 10 {
		c.Loaded(id(i), 10)
	}
	_, evict := c.Plan(t0)
	equal(t, "evict", evict, nil)
	c.SetBudget(35)
	_, evict = c.Plan(t0)
	equal(t, "evict", evict, ids(0, 7))
}

func TestWindowIsLoadedEvenOverBudget(t *testing.T) {
	c := newCache(5, chronology(200, ownSource))
	c.SetCursor(id(0))
	load, _ := c.Plan(t0)
	equal(t, "load", load, ids(0, 21))
	loadAll(c, load, 10)
	_, evict := c.Plan(t0)
	equal(t, "evict", evict, nil)
}

func TestAtMostFiveLoadsPerSourceInChronologicalOrder(t *testing.T) {
	items := chronology(100, func(i int) string {
		if i%2 == 0 {
			return "ann"
		}
		return "bob"
	})
	c := newCache(1000, items)
	c.SetCursor(id(0))
	load, _ := c.Plan(t0)
	equal(t, "load", load, ids(0, 10))
	c.Loaded(id(4), 1)
	c.Loaded(id(1), 1)
	c.Loaded(id(3), 1)
	load, _ = c.Plan(t0)
	equal(t, "load", load, []string{id(10), id(11), id(13)})
}

func TestFailedSourceWaitsWhileOthersLoad(t *testing.T) {
	items := chronology(100, func(i int) string {
		if i%2 == 0 {
			return "ann"
		}
		return "bob"
	})
	c := newCache(1000, items)
	c.SetCursor(id(0))
	c.Plan(t0)
	c.Failed(id(0), t0)
	c.Loaded(id(1), 1)
	load, _ := c.Plan(t0)
	equal(t, "load", load, []string{id(11)})
	equal(t, "waiting", c.Waiting(), []string{id(0), id(2), id(4), id(6), id(8), id(10), id(12), id(14), id(16), id(18), id(20)})
}

func TestRetryWithBackoffAndSingleProbe(t *testing.T) {
	c := newCache(1000, chronology(100, oneSource))
	c.SetCursor(id(0))
	c.Plan(t0)
	for _, x := range ids(0, 5) {
		c.Failed(x, t0.Add(100*time.Millisecond))
	}
	now := t0.Add(100 * time.Millisecond)
	for i, delay := range []time.Duration{1, 2, 4, 8, 16, 30, 30} {
		delay *= time.Second
		load, _ := c.Plan(now.Add(delay - time.Millisecond))
		equal(t, fmt.Sprintf("load before retry %d", i), load, nil)
		now = now.Add(delay)
		load, _ = c.Plan(now)
		equal(t, fmt.Sprintf("probe %d", i), load, []string{id(0)})
		load, _ = c.Plan(now)
		equal(t, fmt.Sprintf("load during probe %d", i), load, nil)
		c.Failed(id(0), now)
	}
	now = now.Add(prefetch.MaxBackoff)
	load, _ := c.Plan(now)
	equal(t, "probe", load, []string{id(0)})
	c.Loaded(id(0), 1)
	equal(t, "waiting", c.Waiting(), nil)
	load, _ = c.Plan(now)
	equal(t, "load after recovery", load, ids(1, 6))
}

func TestOfflineSourceWaitsUntilOnline(t *testing.T) {
	items := chronology(100, func(i int) string {
		if i < 3 {
			return "ann"
		}
		return "bob"
	})
	c := newCache(1000, items)
	c.SetOnline("ann", false)
	c.SetCursor(id(0))
	load, _ := c.Plan(t0)
	equal(t, "load", load, ids(3, 8))
	equal(t, "waiting", c.Waiting(), ids(0, 3))
	c.SetOnline("ann", true)
	equal(t, "waiting", c.Waiting(), nil)
	load, _ = c.Plan(t0)
	equal(t, "load", load, ids(0, 3))
}

func TestComingOnlineCancelsBackoff(t *testing.T) {
	c := newCache(1000, chronology(100, oneSource))
	c.SetCursor(id(0))
	c.Plan(t0)
	for _, x := range ids(0, 5) {
		c.Failed(x, t0)
	}
	c.SetOnline("ann", false)
	c.SetOnline("ann", true)
	load, _ := c.Plan(t0)
	equal(t, "load", load, ids(0, 5))
}

func TestLoadFinishedAfterJumpIsKept(t *testing.T) {
	c := newCache(1000, chronology(200, oneSource))
	c.SetCursor(id(0))
	c.Plan(t0)
	c.SetCursor(id(100))
	load, _ := c.Plan(t0)
	equal(t, "load", load, nil)
	loadAll(c, ids(0, 5), 1)
	load, evict := c.Plan(t0)
	equal(t, "load", load, ids(100, 105))
	equal(t, "evict", evict, nil)
}

func pinAll(c *prefetch.Cache, list []string) {
	for _, x := range list {
		c.Pin(x)
	}
}

func TestPinnedItemsLoadAfterWindowFromCursor(t *testing.T) {
	c := newCache(1000, chronology(60, ownSource))
	pinAll(c, ids(0, 10))
	pinAll(c, ids(55, 60))
	c.SetCursor(id(30))
	load, _ := c.Plan(t0)
	equal(t, "load", load, slices.Concat(ids(30, 51), ids(55, 60), ids(0, 10)))
}

func TestPinnedItemsLoadWithoutCursorInChronologicalOrder(t *testing.T) {
	c := newCache(1000, chronology(30, ownSource))
	pinAll(c, []string{id(20), id(3), id(11)})
	load, _ := c.Plan(t0)
	equal(t, "load", load, []string{id(3), id(11), id(20)})
}

func TestPinnedItemsShareFiveLoadsPerSource(t *testing.T) {
	c := newCache(1000, chronology(100, oneSource))
	pinAll(c, ids(0, 100))
	c.SetCursor(id(90))
	load, _ := c.Plan(t0)
	equal(t, "load", load, ids(90, 95))
	loadAll(c, load, 1)
	load, _ = c.Plan(t0)
	equal(t, "load", load, ids(95, 100))
	loadAll(c, load, 1)
	load, _ = c.Plan(t0)
	equal(t, "load after the end of chronology", load, ids(0, 5))
}

func TestPinnedItemsAreNotEvictedOverBudget(t *testing.T) {
	c := newCache(10, chronology(100, oneSource))
	pinAll(c, ids(0, 100))
	for range 20 {
		load, evict := c.Plan(t0)
		equal(t, "evict", evict, nil)
		loadAll(c, load, 10)
	}
	if p := c.Progress()["ann"]; p != (prefetch.Progress{Total: 100, Done: 100}) {
		t.Fatalf("progress %+v", p)
	}
	for _, x := range ids(0, 99) {
		c.Unpin(x)
	}
	_, evict := c.Plan(t0)
	equal(t, "evict after unpin", evict, ids(0, 99))
}

func TestBrokenItemDoesNotBlockItsSource(t *testing.T) {
	c := newCache(1000, chronology(100, oneSource))
	c.SetCursor(id(0))
	load, _ := c.Plan(t0)
	equal(t, "load", load, ids(0, 5))
	c.Broken(id(0))
	load, _ = c.Plan(t0)
	equal(t, "load after broken", load, []string{id(5)})
	c.Failed(id(1), t0)
	c.Broken(id(2))
	equal(t, "waiting", c.Waiting(), nil)
	load, _ = c.Plan(t0)
	equal(t, "load after the source answered", load, []string{id(1), id(6)})
	c.SetOnline("ann", false)
	equal(t, "waiting", c.Waiting(), slices.DeleteFunc(ids(1, 21), func(x string) bool { return x == id(2) }))
	c.SetOnline("ann", true)
	loadAll(c, []string{id(3), id(4), id(5)}, 1)
	for range 10 {
		loadAll(c, load, 1)
		load, _ = c.Plan(t0)
		for _, x := range load {
			if x == id(0) || x == id(2) {
				t.Fatalf("broken %s is loaded again", x)
			}
		}
	}
}

func TestProgressCountsPinnedItemsPerSource(t *testing.T) {
	c := newCache(1000, chronology(10, func(i int) string {
		if i < 4 {
			return "ann"
		}
		return "bob"
	}))
	pinAll(c, ids(0, 4))
	pinAll(c, []string{id(4), id(5)})
	c.Loaded(id(9), 1)
	c.Plan(t0)
	c.Loaded(id(0), 1)
	c.Loaded(id(1), 1)
	c.Broken(id(2))
	c.Loaded(id(4), 1)
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
