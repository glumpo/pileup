package prefetch_test

import (
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/prefetch"
	"pgregory.net/rapid"
)

var sources = []string{"ann", "bob", "cat"}

type model struct {
	c        *prefetch.Cache
	items    []prefetch.Item
	cursor   string
	budget   int64
	now      time.Time
	cached   map[string]int64
	loading  map[string]string
	pinned   map[string]bool
	offline  map[string]bool
	failed   map[string]bool
	lastFail map[string]time.Time
}

func drawItems(t *rapid.T) []prefetch.Item {
	n := rapid.IntRange(0, 60).Draw(t, "items")
	items := make([]prefetch.Item, n)
	var at int64
	for i := range items {
		at += rapid.Int64Range(0, 3).Draw(t, "gap")
		items[i] = prefetch.Item{
			ID:      id(i),
			TakenAt: at,
			Source:  rapid.SampledFrom(sources).Draw(t, "source"),
			Skip:    rapid.IntRange(0, 4).Draw(t, "skip") == 0,
		}
	}
	return items
}

func (m *model) window() []prefetch.Item {
	i := slices.IndexFunc(m.items, func(it prefetch.Item) bool { return it.ID == m.cursor })
	if i < 0 {
		return nil
	}
	w := []prefetch.Item{m.items[i]}
	for _, it := range m.items[i+1:] {
		if !it.Skip && len(w) <= prefetch.WindowSize {
			w = append(w, it)
		}
	}
	return w
}

func (m *model) inFlight(src string) int {
	n := 0
	for _, s := range m.loading {
		if s == src {
			n++
		}
	}
	return n
}

func (m *model) known(x string) (int64, bool) {
	for _, it := range m.items {
		if it.ID == x {
			return it.TakenAt, true
		}
	}
	return 0, false
}

func (m *model) before(a, b string) bool {
	ta, oka := m.known(a)
	tb, okb := m.known(b)
	if oka != okb {
		return okb
	}
	return ta < tb
}

func (m *model) total() int64 {
	var sum int64
	for _, size := range m.cached {
		sum += size
	}
	return sum
}

func (m *model) unavailable(src string) bool { return m.offline[src] || m.failed[src] }

func (m *model) pickLoading(t *rapid.T) (string, bool) {
	if len(m.loading) == 0 {
		return "", false
	}
	return rapid.SampledFrom(slices.Sorted(maps.Keys(m.loading))).Draw(t, "in flight"), true
}

func (m *model) step(t *rapid.T) {
	switch rapid.IntRange(0, 8).Draw(t, "action") {
	case 0, 1:
		if len(m.items) > 0 {
			m.cursor = rapid.SampledFrom(m.items).Draw(t, "cursor").ID
			m.c.SetCursor(m.cursor)
		}
	case 2:
		if x, ok := m.pickLoading(t); ok {
			size := rapid.Int64Range(1, 50).Draw(t, "size")
			m.c.Loaded(x, size)
			m.cached[x] = size
			m.failed[m.loading[x]] = false
			delete(m.loading, x)
		}
	case 3:
		if x, ok := m.pickLoading(t); ok {
			m.c.Failed(x, m.now)
			src := m.loading[x]
			m.failed[src] = true
			m.lastFail[src] = m.now
			delete(m.loading, x)
		}
	case 4:
		m.budget = rapid.Int64Range(0, 600).Draw(t, "budget")
		m.c.SetBudget(m.budget)
	case 5:
		if len(m.items) > 0 {
			x := rapid.SampledFrom(m.items).Draw(t, "pin").ID
			if m.pinned[x] {
				m.c.Unpin(x)
				delete(m.pinned, x)
			} else {
				m.c.Pin(x)
				m.pinned[x] = true
			}
		}
	case 6:
		src := rapid.SampledFrom(sources).Draw(t, "source")
		online := rapid.Bool().Draw(t, "online")
		m.c.SetOnline(src, online)
		if online && m.offline[src] {
			m.failed[src] = false
		}
		m.offline[src] = !online
	case 7:
		m.now = m.now.Add(time.Duration(rapid.Int64Range(0, 40_000).Draw(t, "ms")) * time.Millisecond)
	case 8:
		var kept []prefetch.Item
		for _, it := range drawItems(t) {
			if rapid.IntRange(0, 9).Draw(t, "drop") > 0 {
				kept = append(kept, it)
			}
		}
		m.items = kept
		m.c.SetItems(kept)
	}
}

func (m *model) plan(t *rapid.T) {
	window := m.window()
	inWindow := map[string]int{}
	for i, it := range window {
		inWindow[it.ID] = i
	}
	before := map[string]int{}
	for _, src := range sources {
		before[src] = m.inFlight(src)
	}
	totalBefore := m.total()

	load, evict := m.c.Plan(m.now)

	last := -1
	dispatched := map[string]int{}
	for _, x := range load {
		i, ok := inWindow[x]
		if !ok {
			t.Fatalf("loads %s outside the window", x)
		}
		if i <= last {
			t.Fatalf("loads %v out of chronological order", load)
		}
		last = i
		if _, ok := m.cached[x]; ok {
			t.Fatalf("loads cached %s", x)
		}
		if _, ok := m.loading[x]; ok {
			t.Fatalf("loads %s twice", x)
		}
		src := window[i].Source
		m.loading[x] = src
		dispatched[src]++
	}

	for _, src := range sources {
		n := m.inFlight(src)
		if n > prefetch.PerSource {
			t.Fatalf("%d loads in flight from %s", n, src)
		}
		if m.offline[src] && dispatched[src] > 0 {
			t.Fatalf("loads from offline %s", src)
		}
		if m.failed[src] && dispatched[src] > 0 && (dispatched[src] > 1 || before[src] > 0) {
			t.Fatalf("more than one probe to failed %s: %d new, %d before", src, dispatched[src], before[src])
		}
		var missing []string
		for _, it := range window {
			_, cached := m.cached[it.ID]
			_, loading := m.loading[it.ID]
			if it.Source == src && !cached && !loading {
				missing = append(missing, it.ID)
			}
		}
		for _, x := range missing {
			for _, y := range load {
				if m.loading[y] == src && inWindow[y] > inWindow[x] {
					t.Fatalf("loads %s before earlier %s from %s", y, x, src)
				}
			}
		}
		if len(missing) > 0 && !m.unavailable(src) && n < prefetch.PerSource {
			t.Fatalf("%s idle with %d loads while %v missing", src, n, missing)
		}
		retry := m.failed[src] && !m.offline[src] && !m.now.Before(m.lastFail[src].Add(prefetch.MaxBackoff))
		if retry && len(missing) > 0 && n == 0 {
			t.Fatalf("no retry of %s %v after %v", src, missing, m.now.Sub(m.lastFail[src]))
		}
	}

	if len(evict) > 0 && totalBefore <= m.budget {
		t.Fatalf("evicts %v within budget %d (total %d)", evict, m.budget, totalBefore)
	}
	for i, x := range evict {
		if _, ok := m.cached[x]; !ok {
			t.Fatalf("evicts %s that is not cached", x)
		}
		if _, ok := inWindow[x]; ok || m.pinned[x] {
			t.Fatalf("evicts protected %s", x)
		}
		if i > 0 && m.before(x, evict[i-1]) {
			t.Fatalf("evicts %v not in taken_at order", evict)
		}
		if i == len(evict)-1 && m.total() <= m.budget {
			t.Fatalf("evicts %s already within budget", x)
		}
		delete(m.cached, x)
	}
	var rest []string
	for x := range m.cached {
		if _, ok := inWindow[x]; !ok && !m.pinned[x] {
			rest = append(rest, x)
		}
	}
	if m.total() > m.budget && len(rest) > 0 {
		t.Fatalf("over budget %d with %d while %v evictable", m.budget, m.total(), rest)
	}
	for _, x := range evict {
		for _, y := range rest {
			if m.before(y, x) {
				t.Fatalf("evicts %s but keeps earlier %s", x, y)
			}
		}
	}

	var waiting []string
	for _, it := range window {
		if _, ok := m.cached[it.ID]; !ok && m.unavailable(it.Source) {
			waiting = append(waiting, it.ID)
		}
	}
	if got := m.c.Waiting(); !slices.Equal(got, waiting) {
		t.Fatalf("waiting %v, want %v", got, waiting)
	}
}

func TestInvariantsOverRandomEvents(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		items := drawItems(t)
		budget := rapid.Int64Range(0, 600).Draw(t, "budget")
		m := &model{
			c:        prefetch.New(budget),
			items:    items,
			budget:   budget,
			now:      t0,
			cached:   map[string]int64{},
			loading:  map[string]string{},
			pinned:   map[string]bool{},
			offline:  map[string]bool{},
			failed:   map[string]bool{},
			lastFail: map[string]time.Time{},
		}
		m.c.SetItems(items)
		for range rapid.IntRange(1, 200).Draw(t, "steps") {
			m.step(t)
			m.plan(t)
		}
	})
}
