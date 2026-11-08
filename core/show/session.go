package show

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/glumpo/pileup/core/cast"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/prefetch"
)

const (
	playing = "PLAYING"
	paused  = "PAUSED"
	tick    = time.Second
)

type session struct {
	n      *node.Node
	pile   []byte
	self   op.Author
	screen node.Screen
	token  string
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	cast   *cast.Session
	srv    *http.Server
	base   string
	wake   chan struct{}

	mu      sync.Mutex
	items   []state.Asset
	index   map[string]int
	cursor  int
	checked map[string]bool
	cache   *prefetch.Cache
	player  string
	command string
	fault   *RendererError
}

func key(a state.Asset) string {
	return hex.EncodeToString(a.Meta.AssetID) + "/" + hex.EncodeToString(a.Meta.Revision)
}

func parseKey(k string) (assetID, revision []byte) {
	a, r, _ := strings.Cut(k, "/")
	assetID, _ = hex.DecodeString(a)
	revision, _ = hex.DecodeString(r)
	return assetID, revision
}

func (s *session) begin(items []state.Asset, cursor int) {
	online := s.n.Online(s.pile)
	s.mu.Lock()
	s.setItems(items)
	s.setOnline(online)
	s.cursor = -1
	s.move(cursor)
	s.mu.Unlock()
	s.n.Diag().Add(diag.Renderer, s.pile, s.screen.Name, "show started")
	s.wg.Add(1)
	go s.tick()
	if s.cast != nil {
		s.wg.Add(1)
		go s.drive()
	}
}

func (s *session) setItems(items []state.Asset) {
	s.items = items
	s.index = make(map[string]int, len(items))
	planned := make([]prefetch.Item, len(items))
	for i, a := range items {
		k := key(a)
		s.index[k] = i
		planned[i] = prefetch.Item{ID: k, TakenAt: a.Meta.TakenAt, Source: hex.EncodeToString(a.Owner[:])}
		if !s.checked[k] {
			s.checked[k] = true
			if size, ok := s.n.Cached(s.pile, a.Meta.AssetID, a.Meta.Revision, node.T2); ok {
				s.cache.Loaded(k, size)
			}
		}
	}
	s.cache.SetItems(planned)
}

func (s *session) setOnline(online map[op.Author]bool) {
	for _, a := range s.items {
		if a.Owner != s.self {
			s.cache.SetOnline(hex.EncodeToString(a.Owner[:]), online[a.Owner])
		}
	}
}

func (s *session) refresh() {
	_, items, err := s.n.Timeline(s.pile)
	if err != nil || len(items) == 0 {
		return
	}
	current := key(s.items[s.cursor])
	s.setItems(items)
	if i, ok := s.index[current]; ok {
		s.cursor = i
		return
	}
	i := min(s.cursor, len(items)-1)
	s.cursor = -1
	s.move(i)
}

func (s *session) move(i int) {
	if i != s.cursor {
		s.cursor = i
		s.command = ""
		s.player = ""
		if s.cast == nil {
			s.player = playing
		}
		s.signal()
	}
	s.cache.SetCursor(key(s.items[i]))
	s.plan()
}

func (s *session) control(command string) error {
	if s.items[s.cursor].Meta.Kind != op.KindVideo {
		return ErrNotVideo
	}
	if s.cast == nil {
		s.player = command
		return nil
	}
	s.command = command
	s.signal()
	return nil
}

func (s *session) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *session) plan() {
	load, evict := s.cache.Plan(time.Now())
	for _, k := range evict {
		assetID, revision := parseKey(k)
		if err := s.n.Evict(s.pile, assetID, revision, node.T2); err != nil {
			s.n.Diag().Error(diag.Media, s.pile, "", fmt.Errorf("evict %x T2: %w", assetID, err))
		}
	}
	for _, k := range load {
		s.wg.Add(1)
		go s.fetch(k)
	}
}

func (s *session) fetch(k string) {
	defer s.wg.Done()
	assetID, revision := parseKey(k)
	var size int64
	f, _, err := s.n.Blob(s.ctx, s.pile, assetID, revision, node.T2)
	if err == nil {
		info, serr := f.Stat()
		f.Close()
		if err = serr; err == nil {
			size = info.Size()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	if err != nil {
		s.cache.Failed(k, time.Now())
	} else {
		s.cache.Loaded(k, size)
	}
	s.plan()
}

func (s *session) tick() {
	defer s.wg.Done()
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
		online := s.n.Online(s.pile)
		s.mu.Lock()
		s.setOnline(online)
		s.plan()
		s.mu.Unlock()
	}
}

func (s *session) state() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.items[s.cursor]
	st := State{
		Pile:    s.pile,
		Screen:  s.screen,
		Index:   s.cursor,
		Total:   len(s.items),
		Current: cur,
		Playing: cur.Meta.Kind == op.KindVideo && (s.player == playing || s.player == "BUFFERING"),
	}
	if s.fault != nil {
		f := *s.fault
		st.Error = &f
	}
	for _, k := range s.cache.Waiting() {
		st.Waiting = append(st.Waiting, s.items[s.index[k]])
	}
	return st
}

func (s *session) stop() {
	s.cancel()
	if s.srv != nil {
		s.srv.Close()
	}
	s.wg.Wait()
	s.n.Diag().Add(diag.Renderer, s.pile, s.screen.Name, "show stopped")
}
