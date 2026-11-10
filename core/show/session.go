package show

import (
	"context"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/glumpo/pileup/core/cast"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/node"
)

const (
	playing = "PLAYING"
	paused  = "PAUSED"
	tick    = time.Second
)

var settle = 200 * time.Millisecond

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

	l *loader

	mu        sync.Mutex
	items     []state.Asset
	index     map[string]int
	cursor    int
	player    string
	command   string
	fault     *RendererError
	videoDown bool
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

func (s *session) begin(p node.Pile, items []state.Asset, cursor int, changed <-chan struct{}) {
	s.mu.Lock()
	s.setItems(p, items)
	s.cursor = -1
	s.move(cursor)
	s.mu.Unlock()
	s.n.Diag().Add(diag.Renderer, s.pile, s.screen.Name, "show started")
	s.wg.Add(1)
	go s.follow(changed)
	if s.cast != nil {
		s.wg.Add(1)
		go s.drive()
	}
}

func (s *session) setItems(p node.Pile, items []state.Asset) {
	s.items = items
	s.index = make(map[string]int, len(items))
	for i, a := range items {
		s.index[key(a)] = i
	}
	s.l.setItems(p, items)
}

func (s *session) follow(changed <-chan struct{}) {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-changed:
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(settle):
		}
		var err error
		if changed, err = s.n.Changed(s.pile); err != nil {
			return
		}
		s.mu.Lock()
		i, _ := s.refresh()
		s.seek(i)
		s.mu.Unlock()
	}
}

func (s *session) refresh() (int, bool) {
	p, items, err := s.n.Timeline(s.pile)
	if err != nil {
		return s.cursor, true
	}
	old, cur := s.items, s.cursor
	s.setItems(p, items)
	pos := make(map[string]int, len(items))
	for i, a := range items {
		pos[string(a.Meta.AssetID)] = i
	}
	if cur < 0 {
		return 0, false
	}
	if i, ok := pos[string(old[cur].Meta.AssetID)]; ok {
		s.cursor = i
		return i, true
	}
	s.cursor = -1
	for _, a := range old[cur+1:] {
		if i, ok := pos[string(a.Meta.AssetID)]; ok {
			return i, false
		}
	}
	return len(items), false
}

func (s *session) seek(i int) error {
	if len(s.items) == 0 {
		s.l.setCursor("")
		s.signal()
		return ErrEmpty
	}
	s.move(min(max(i, 0), len(s.items)-1))
	return nil
}

func (s *session) move(i int) {
	if i != s.cursor {
		s.cursor = i
		s.command = ""
		s.player = ""
		if s.cast == nil {
			s.player = playing
		}
		cur := s.items[i]
		s.videoDown = cur.Meta.Kind == op.KindVideo && !s.l.reachable(cur.Owner)
		if s.videoDown {
			s.posterOnly(cur)
		}
		s.signal()
	}
	s.l.setCursor(key(s.items[i]))
}

func (s *session) control(command string) error {
	if s.cursor < 0 {
		return ErrEmpty
	}
	cur := s.items[s.cursor]
	if cur.Meta.Kind != op.KindVideo {
		return ErrNotVideo
	}
	if s.videoDown {
		if command != playing || !s.l.reachable(cur.Owner) {
			return nil
		}
		s.videoDown = false
		if s.cast != nil {
			s.signal()
			return nil
		}
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

func (s *session) unavailable(a state.Asset) {
	s.mu.Lock()
	down := s.cursor >= 0 && key(s.items[s.cursor]) == key(a) && !s.videoDown
	if down {
		s.videoDown = true
		s.signal()
	}
	s.mu.Unlock()
	if down {
		s.posterOnly(a)
	}
}

func (s *session) posterOnly(a state.Asset) {
	s.n.Diag().Add(diag.Media, s.pile, source(a.Owner), "video source is unavailable, showing the poster")
}

func (s *session) state() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := State{Pile: s.pile, Screen: s.screen, Total: len(s.items)}
	var down []op.Author
	if s.cursor >= 0 {
		cur := s.items[s.cursor]
		st.Index = s.cursor
		st.Current = cur
		st.Playing = cur.Meta.Kind == op.KindVideo && !s.videoDown && (s.player == playing || s.player == "BUFFERING")
		st.VideoUnavailable = s.videoDown
		if s.videoDown {
			down = append(down, cur.Owner)
		}
	}
	if s.fault != nil {
		f := *s.fault
		st.Error = &f
	}
	st.Waiting, st.Hints = s.l.status(down...)
	return st
}

func (s *session) stop() {
	s.cancel()
	s.wg.Wait()
	s.l.setCursor("")
	s.n.Diag().Add(diag.Renderer, s.pile, s.screen.Name, "show stopped")
}
