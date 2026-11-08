package prefetch

import (
	"cmp"
	"slices"
	"time"
)

const (
	WindowSize = 20
	PerSource  = 5
	MinBackoff = time.Second
	MaxBackoff = 30 * time.Second
)

type Item struct {
	ID      string
	TakenAt int64
	Source  string
	Skip    bool
}

type source struct {
	offline  bool
	failures int
	retryAt  time.Time
	loading  int
}

func (s *source) unavailable() bool { return s.offline || s.failures > 0 }

func (s *source) limit(now time.Time) int {
	switch {
	case s.offline || now.Before(s.retryAt):
		return 0
	case s.failures > 0:
		return 1
	}
	return PerSource
}

type Cache struct {
	budget  int64
	items   []Item
	index   map[string]int
	cursor  string
	cached  map[string]int64
	pinned  map[string]bool
	loading map[string]string
	sources map[string]*source
}

func New(budget int64) *Cache {
	return &Cache{
		budget:  budget,
		index:   map[string]int{},
		cached:  map[string]int64{},
		pinned:  map[string]bool{},
		loading: map[string]string{},
		sources: map[string]*source{},
	}
}

func (c *Cache) SetItems(items []Item) {
	c.items = items
	c.index = make(map[string]int, len(items))
	for i, it := range items {
		c.index[it.ID] = i
	}
}

func (c *Cache) SetCursor(id string) { c.cursor = id }

func (c *Cache) SetBudget(budget int64) { c.budget = budget }

func (c *Cache) Pin(id string) { c.pinned[id] = true }

func (c *Cache) Unpin(id string) { delete(c.pinned, id) }

func (c *Cache) SetOnline(src string, online bool) {
	s := c.source(src)
	if online && s.offline {
		s.failures = 0
		s.retryAt = time.Time{}
	}
	s.offline = !online
}

func (c *Cache) Loaded(id string, size int64) {
	if src, ok := c.loading[id]; ok {
		delete(c.loading, id)
		s := c.sources[src]
		s.loading--
		s.failures = 0
		s.retryAt = time.Time{}
	}
	c.cached[id] = size
}

func (c *Cache) Failed(id string, now time.Time) {
	s := c.sources[c.loading[id]]
	delete(c.loading, id)
	s.loading--
	if now.Before(s.retryAt) {
		return
	}
	s.failures++
	s.retryAt = now.Add(backoff(s.failures))
}

func (c *Cache) Plan(now time.Time) (load, evict []string) {
	window := c.window()
	for _, it := range window {
		if c.has(it.ID) {
			continue
		}
		s := c.source(it.Source)
		if s.loading >= s.limit(now) {
			continue
		}
		c.loading[it.ID] = it.Source
		s.loading++
		load = append(load, it.ID)
	}
	return load, c.evict(window)
}

func (c *Cache) Waiting() []string {
	var out []string
	for _, it := range c.window() {
		if _, ok := c.cached[it.ID]; !ok && c.source(it.Source).unavailable() {
			out = append(out, it.ID)
		}
	}
	return out
}

func (c *Cache) window() []Item {
	i, ok := c.index[c.cursor]
	if !ok {
		return nil
	}
	w := []Item{c.items[i]}
	for _, it := range c.items[i+1:] {
		if len(w) == WindowSize+1 {
			break
		}
		if !it.Skip {
			w = append(w, it)
		}
	}
	return w
}

func (c *Cache) evict(window []Item) []string {
	var total int64
	for _, size := range c.cached {
		total += size
	}
	if total <= c.budget {
		return nil
	}
	keep := map[string]bool{}
	for _, it := range window {
		keep[it.ID] = true
	}
	var victims []string
	for id := range c.cached {
		if !keep[id] && !c.pinned[id] {
			victims = append(victims, id)
		}
	}
	slices.SortFunc(victims, c.earlier)
	var out []string
	for _, id := range victims {
		if total <= c.budget {
			break
		}
		total -= c.cached[id]
		delete(c.cached, id)
		out = append(out, id)
	}
	return out
}

func (c *Cache) earlier(a, b string) int {
	ia, oka := c.index[a]
	ib, okb := c.index[b]
	switch {
	case !oka && !okb:
		return cmp.Compare(a, b)
	case !oka:
		return -1
	case !okb:
		return 1
	}
	return cmp.Or(cmp.Compare(c.items[ia].TakenAt, c.items[ib].TakenAt), cmp.Compare(ia, ib))
}

func (c *Cache) has(id string) bool {
	_, cached := c.cached[id]
	_, loading := c.loading[id]
	return cached || loading
}

func (c *Cache) source(src string) *source {
	s, ok := c.sources[src]
	if !ok {
		s = &source{}
		c.sources[src] = s
	}
	return s
}

func backoff(failures int) time.Duration {
	d := MinBackoff
	for i := 1; i < failures && d < MaxBackoff; i++ {
		d *= 2
	}
	return min(d, MaxBackoff)
}
