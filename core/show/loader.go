package show

import (
	"bytes"
	"cmp"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/prefetch"
)

const (
	Unreachable = "unreachable"
	Timeout     = "timeout"
	typicalT2   = 400 << 10
)

type Hint struct {
	Member    op.Author
	Name      string
	Platform  string
	Reason    string
	LastError string
}

type Progress struct {
	Hint
	Total  int
	Done   int
	Failed int
}

type Estimate struct {
	Files      int
	Missing    int
	Bytes      int64
	Budget     int64
	Free       *int64
	OverBudget bool
}

type loader struct {
	n    *node.Node
	pile []byte
	self op.Author
	ctx  context.Context
	wg   *sync.WaitGroup

	mu      sync.Mutex
	members map[op.Author]state.Member
	items   []state.Asset
	index   map[string]int
	pins    map[string]bool
	pinned  map[string]bool
	online  map[op.Author]bool
	stalled map[string]bool
	cache   *prefetch.Cache
}

func source(a op.Author) string { return hex.EncodeToString(a[:]) }

func author(src string) op.Author {
	var a op.Author
	hex.Decode(a[:], []byte(src))
	return a
}

func (l *loader) setItems(p node.Pile, items []state.Asset) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.members = p.Members
	l.items = items
	l.index = make(map[string]int, len(items))
	planned := make([]prefetch.Item, len(items))
	for i, a := range items {
		k := key(a)
		l.index[k] = i
		planned[i] = prefetch.Item{ID: k, TakenAt: a.Meta.TakenAt, Source: source(a.Owner)}
	}
	l.cache.SetItems(planned)
	l.setOnline(l.n.Online(l.pile))
	l.syncPins()
	l.plan()
}

func (l *loader) setOnline(online map[op.Author]bool) {
	l.online = online
	for _, a := range l.items {
		if a.Owner != l.self {
			l.cache.SetOnline(source(a.Owner), online[a.Owner])
		}
	}
}

func (l *loader) setCursor(k string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache.SetCursor(k)
	l.plan()
}

func (l *loader) syncPins() {
	want := map[string]bool{}
	for _, a := range l.items {
		if l.pins[hex.EncodeToString(a.Meta.AssetID)] {
			want[key(a)] = true
		}
	}
	for k := range l.pinned {
		if !want[k] {
			l.cache.Unpin(k)
		}
	}
	for k := range want {
		l.cache.Pin(k)
	}
	l.pinned = want
}

func (l *loader) setPins(member []byte, pin bool) ([]Progress, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if member == nil && !pin {
		clear(l.pins)
	}
	for _, a := range l.items {
		if member != nil && !bytes.Equal(a.Owner[:], member) {
			continue
		}
		if pin {
			l.pins[hex.EncodeToString(a.Meta.AssetID)] = true
		} else {
			delete(l.pins, hex.EncodeToString(a.Meta.AssetID))
		}
	}
	err := l.n.SetPins(l.pile, slices.Collect(maps.Keys(l.pins)))
	l.syncPins()
	l.plan()
	return l.progress(), err
}

func (l *loader) pinProgress() []Progress {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.progress()
}

func (l *loader) progress() []Progress {
	out := []Progress{}
	for src, p := range l.cache.Progress() {
		h := l.hint(author(src))
		if p.Done+p.Failed == p.Total || !l.cache.Unavailable(src) {
			h.Reason = ""
		}
		out = append(out, Progress{Hint: h, Total: p.Total, Done: p.Done, Failed: p.Failed})
	}
	slices.SortFunc(out, func(a, b Progress) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), bytes.Compare(a.Member[:], b.Member[:]))
	})
	return out
}

func (l *loader) estimate(member []byte) Estimate {
	l.mu.Lock()
	defer l.mu.Unlock()
	var after, total int64
	var count int
	for _, a := range l.items {
		if size, ok := l.cache.Size(key(a)); ok {
			total += size
			count++
		}
	}
	typical := int64(typicalT2)
	if count > 0 {
		typical = total / int64(count)
	}
	e := Estimate{Budget: l.n.Budget()}
	for _, a := range l.items {
		inScope := member == nil || bytes.Equal(a.Owner[:], member)
		if !inScope && !l.pinned[key(a)] {
			continue
		}
		size, ok := l.cache.Size(key(a))
		if !ok {
			size = typical
		}
		after += size
		if !inScope {
			continue
		}
		e.Files++
		if !ok {
			e.Missing++
			e.Bytes += typical
		}
	}
	e.OverBudget = after > e.Budget
	if free, err := l.n.Free(); err == nil {
		e.Free = &free
	}
	return e
}

func (l *loader) hint(a op.Author) Hint {
	m := l.members[a]
	reason := Timeout
	if !l.online[a] {
		reason = Unreachable
	}
	return Hint{Member: a, Name: m.Name, Platform: m.Platform, Reason: reason, LastError: l.n.Presence(l.pile, a).LastError}
}

func (l *loader) status(extra ...op.Author) ([]state.Asset, []Hint) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var waiting []state.Asset
	var hints []Hint
	seen := map[op.Author]bool{}
	add := func(a op.Author) {
		if !seen[a] {
			seen[a] = true
			hints = append(hints, l.hint(a))
		}
	}
	for _, k := range l.cache.Waiting() {
		a := l.items[l.index[k]]
		waiting = append(waiting, a)
		add(a.Owner)
	}
	for _, a := range extra {
		add(a)
	}
	return waiting, hints
}

func (l *loader) reachable(a op.Author) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return a == l.self || !l.cache.Unavailable(source(a))
}

func (l *loader) plan() {
	if l.ctx.Err() != nil {
		return
	}
	var window []state.Asset
	for _, k := range l.cache.Window() {
		window = append(window, l.items[l.index[k]])
	}
	if err := l.n.Protect(l.pile, window); err != nil {
		l.n.Diag().Error(diag.Media, l.pile, "", fmt.Errorf("protect the window: %w", err))
	}
	displays, err := l.n.Displays(l.pile)
	if err != nil {
		l.n.Diag().Error(diag.Media, l.pile, "", fmt.Errorf("cached files: %w", err))
		return
	}
	sizes := map[string]int64{}
	for _, d := range displays {
		sizes[hex.EncodeToString(d.AssetID)+"/"+hex.EncodeToString(d.Revision)] = d.Size
	}
	l.cache.SetCached(sizes)
	for _, k := range l.cache.Plan(time.Now()) {
		l.wg.Add(1)
		go l.fetch(k, l.items[l.index[k]].Owner)
	}
}

func (l *loader) fetch(k string, owner op.Author) {
	defer l.wg.Done()
	assetID, revision := parseKey(k)
	f, _, err := l.n.Blob(l.ctx, l.pile, assetID, revision, node.T2)
	if err == nil {
		f.Close()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ctx.Err() != nil {
		return
	}
	switch {
	case err == nil:
		l.cache.Loaded(k)
	case owner == l.self || errors.Is(err, node.ErrUnknownAsset) || errors.Is(err, node.ErrRefused):
		l.cache.Broken(k, time.Now())
	default:
		l.cache.Failed(k, time.Now())
	}
	l.plan()
}

func (l *loader) tick() {
	defer l.wg.Done()
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-t.C:
		}
		online := l.n.Online(l.pile)
		l.mu.Lock()
		l.setOnline(online)
		l.plan()
		l.report()
		l.mu.Unlock()
	}
}

func (l *loader) report() {
	stalled := map[string]bool{}
	for _, k := range l.cache.Waiting() {
		stalled[source(l.items[l.index[k]].Owner)] = true
	}
	for src, p := range l.cache.Progress() {
		if p.Done+p.Failed < p.Total && l.cache.Unavailable(src) {
			stalled[src] = true
		}
	}
	for src := range stalled {
		if !l.stalled[src] {
			h := l.hint(author(src))
			l.n.Diag().Add(diag.Timeout, l.pile, src, fmt.Sprintf("no answer from %s (%s %s), files wait", h.Name, h.Platform, h.Reason))
		}
	}
	l.stalled = stalled
}
