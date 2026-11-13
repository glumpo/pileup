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
)

const (
	playing   = "PLAYING"
	paused    = "PAUSED"
	buffering = "BUFFERING"
	tick      = time.Second
)

var settle = 200 * time.Millisecond

type session struct {
	n       *node.Node
	pile    []byte
	self    op.Author
	screen  node.Screen
	token   string
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	cast    *cast.Session
	srv     *http.Server
	base    string
	wake    chan struct{}
	kick    chan struct{}
	moved   chan struct{}
	answers chan answer

	l *loader

	mu        sync.Mutex
	version   <-chan struct{}
	items     []state.Asset
	index     map[string]int
	cursor    int
	player    string
	command   string
	fault     *RendererError
	videoDown bool
	why       string
	revived   bool
	misses    int
	ended     bool
	offset    float64
	since     time.Time
	episodes  int
	buffered  time.Time
	noted     bool
	streamed  int64
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
	s.version = changed
	s.setItems(p, items)
	s.cursor = -1
	s.move(cursor)
	s.mu.Unlock()
	s.n.Diag().Add(diag.Renderer, s.pile, s.screen.Name, "show started")
	s.wg.Add(2)
	go s.follow(changed)
	go s.keep()
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
	changed, err := s.n.Changed(s.pile)
	if err == nil && changed == s.version {
		return max(s.cursor, 0), s.cursor >= 0
	}
	p, items, err := s.n.Timeline(s.pile)
	if err != nil {
		return s.cursor, true
	}
	s.version = changed
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
		s.why, s.revived, s.misses, s.ended, s.offset, s.since = "", s.videoDown, 0, false, 0, time.Time{}
		s.episodes, s.buffered, s.noted = 0, time.Time{}, false
		s.streamed, _ = s.n.Streamed()
		select {
		case s.moved <- struct{}{}:
		default:
		}
		if s.videoDown {
			s.why = Unreachable
			s.posterOnly(cur, Unreachable)
		}
		s.signal()
	}
	s.l.setCursor(key(s.items[i]))
	s.l.setVideo(s.video())
}

func (s *session) control(command string) error {
	if s.cursor < 0 {
		return ErrEmpty
	}
	cur := s.items[s.cursor]
	if cur.Meta.Kind != op.KindVideo {
		return ErrNotVideo
	}
	if s.ended {
		if command == playing {
			s.ended = false
			s.signal()
		}
		return nil
	}
	if s.videoDown {
		if command != playing || !s.l.reachable(cur.Owner) {
			return nil
		}
		s.videoDown, s.why, s.misses = false, "", 0
		if s.cast != nil {
			s.signal()
			return nil
		}
	}
	if s.cast == nil {
		s.player = command
		s.l.setVideo(s.video())
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

func (s *session) unavailable(a state.Asset, why string) {
	s.mu.Lock()
	down := s.cursor >= 0 && key(s.items[s.cursor]) == key(a) && !s.videoDown
	if down {
		s.videoDown, s.why = true, why
		s.l.setVideo(s.video())
		s.signal()
	}
	s.mu.Unlock()
	if down {
		s.posterOnly(a, why)
	}
}

func (s *session) posterOnly(a state.Asset, why string) {
	switch why {
	case Unreachable:
		s.n.Diag().Add(diag.Media, s.pile, source(a.Owner), "video source is unavailable, showing the poster")
	case Unsupported:
		s.n.Diag().Add(diag.Renderer, s.pile, s.screen.Name, fmt.Sprintf("screen cannot play video %x (%s, codec %q), showing the poster", a.Meta.AssetID, a.Meta.MIME, a.Meta.Codec))
	default:
		s.n.Diag().Add(diag.Media, s.pile, source(a.Owner), fmt.Sprintf("video original cannot be read (%s), showing the poster", why))
	}
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
		st.Playing = s.video()
		st.Buffering = s.buffers()
		if s.stalled() {
			st.Stalled = true
			st.NeedBitrate, st.Bitrate = s.rates()
		}
		st.VideoUnavailable = s.videoDown
		st.VideoReason = s.why
		if s.why == Unreachable {
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

func (s *session) video() bool {
	return s.cursor >= 0 && s.items[s.cursor].Meta.Kind == op.KindVideo && !s.videoDown && !s.ended && (s.player == playing || s.player == buffering)
}

func (s *session) buffers() bool {
	return s.cursor >= 0 && s.items[s.cursor].Meta.Kind == op.KindVideo && !s.videoDown && !s.ended && s.player == buffering
}

func (s *session) stalled() bool {
	return s.buffers() && (s.episodes > 1 || time.Since(s.buffered) >= stall)
}

func (s *session) stallIn() time.Duration {
	switch {
	case s.noted || !s.buffers():
		return -1
	case s.episodes > 1:
		return 0
	}
	return max(0, stall-time.Since(s.buffered))
}

func (s *session) rates() (need, rate float64) {
	m := s.items[s.cursor].Meta
	if m.DurationMs != nil && *m.DurationMs > 0 {
		need = float64(m.SizeBytes) * 8000 / float64(*m.DurationMs)
	}
	if total, r := s.n.Streamed(); total > s.streamed {
		rate = r * 8
	}
	return need, rate
}

func (s *session) note() {
	s.mu.Lock()
	if s.noted || !s.stalled() {
		s.mu.Unlock()
		return
	}
	s.noted = true
	cur := s.items[s.cursor]
	need, rate := s.rates()
	s.mu.Unlock()
	msg := fmt.Sprintf("video %x buffers on the screen", cur.Meta.AssetID)
	if need > 0 {
		msg += fmt.Sprintf(", needs ~%.1f Mbit/s", need/1e6)
	}
	if rate > 0 {
		msg += fmt.Sprintf(", streaming ~%.1f Mbit/s", rate/1e6)
	}
	s.n.Diag().Add(diag.Renderer, s.pile, s.screen.Name, msg)
}

func (s *session) retry(screen node.Screen, found bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fault == nil || s.fault.Code != "reconnecting" {
		return false
	}
	if found {
		s.screen.Addr = screen.Addr
	}
	select {
	case s.kick <- struct{}{}:
	default:
	}
	return true
}

func (s *session) stop() {
	s.cancel()
	s.wg.Wait()
	s.l.setCursor("")
	s.l.setVideo(false)
	s.n.Diag().Add(diag.Renderer, s.pile, s.screen.Name, "show stopped")
}
