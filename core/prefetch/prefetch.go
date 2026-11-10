package prefetch

import (
	"slices"
	"time"
)

const (
	WindowSize = 20
	Behind     = 5
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

type Progress struct {
	Total  int
	Done   int
	Failed int
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
	items   []Item
	index   map[string]int
	cursor  string
	cached  map[string]int64
	pinned  map[string]bool
	broken  map[string]bool
	loading map[string]string
	sources map[string]*source
}

func New() *Cache {
	return &Cache{
		index:   map[string]int{},
		cached:  map[string]int64{},
		pinned:  map[string]bool{},
		broken:  map[string]bool{},
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

func (c *Cache) SetCached(sizes map[string]int64) { c.cached = sizes }

func (c *Cache) Loaded(id string) { c.answered(id) }

func (c *Cache) Broken(id string) {
	c.answered(id)
	c.broken[id] = true
}

func (c *Cache) answered(id string) {
	if src, ok := c.loading[id]; ok {
		delete(c.loading, id)
		s := c.sources[src]
		s.loading--
		s.failures = 0
		s.retryAt = time.Time{}
	}
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

func (c *Cache) Plan(now time.Time) (load []string) {
	for _, it := range slices.Concat(c.window(), c.wanted()) {
		if c.has(it.ID) || c.broken[it.ID] {
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
	return load
}

func (c *Cache) Window() []string {
	var out []string
	for _, it := range c.window() {
		out = append(out, it.ID)
	}
	return out
}

func (c *Cache) Waiting() []string {
	var out []string
	for _, it := range c.window() {
		if _, ok := c.cached[it.ID]; !ok && !c.broken[it.ID] && c.source(it.Source).unavailable() {
			out = append(out, it.ID)
		}
	}
	return out
}

func (c *Cache) Progress() map[string]Progress {
	out := map[string]Progress{}
	for _, it := range c.items {
		if !c.pinned[it.ID] {
			continue
		}
		p := out[it.Source]
		p.Total++
		if _, ok := c.cached[it.ID]; ok {
			p.Done++
		} else if c.broken[it.ID] {
			p.Failed++
		}
		out[it.Source] = p
	}
	return out
}

func (c *Cache) Size(id string) (int64, bool) {
	size, ok := c.cached[id]
	return size, ok
}

func (c *Cache) Unavailable(src string) bool { return c.source(src).unavailable() }

func (c *Cache) wanted() []Item {
	if len(c.pinned) == 0 {
		return nil
	}
	start := c.index[c.cursor]
	var out []Item
	for _, it := range slices.Concat(c.items[start:], c.items[:start]) {
		if c.pinned[it.ID] {
			out = append(out, it)
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
