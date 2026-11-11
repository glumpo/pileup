package show

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"sync"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/prefetch"
)

var (
	ErrNoShow        = errors.New("show: no active show")
	ErrUnknownScreen = errors.New("show: unknown screen")
	ErrEmpty         = errors.New("show: pile has no files")
	ErrNotVideo      = errors.New("show: current file is not a video")
	ErrUnreachable   = errors.New("show: screen is unreachable")
	ErrUnknownMember = errors.New("show: unknown member")
)

const (
	Unsupported = "unsupported"
	NotLocal    = "not_local"
	Unreadable  = "unreadable"
)

type UnavailableError struct{ Reason string }

func (e *UnavailableError) Error() string { return "show: screen is unavailable: " + e.Reason }

type RendererError struct {
	Code    string
	Message string
}

type State struct {
	Pile             []byte
	Screen           node.Screen
	Index            int
	Total            int
	Current          state.Asset
	Playing          bool
	VideoUnavailable bool
	VideoReason      string
	Buffering        bool
	Stalled          bool
	NeedBitrate      float64
	Bitrate          float64
	Error            *RendererError
	Waiting          []state.Asset
	Hints            []Hint
}

type Host struct {
	n       *node.Node
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
	cur     *session
	loaders map[string]*loader
}

func New(n *node.Node) *Host {
	ctx, cancel := context.WithCancel(context.Background())
	return &Host{n: n, ctx: ctx, cancel: cancel, loaders: map[string]*loader{}}
}

func (h *Host) loader(pileID []byte) (*loader, error) {
	if l, ok := h.loaders[string(pileID)]; ok {
		return l, nil
	}
	pins, err := h.n.Pins(pileID)
	if err != nil {
		return nil, err
	}
	l := &loader{
		n:     h.n,
		pile:  pileID,
		self:  h.n.Author(),
		ctx:   h.ctx,
		wg:    &h.wg,
		pins:  map[string]bool{},
		cache: prefetch.New(),
	}
	for _, a := range pins {
		l.pins[a] = true
	}
	h.loaders[string(pileID)] = l
	h.wg.Add(1)
	go l.tick()
	return l, nil
}

func (h *Host) pile(pileID, member []byte) (*loader, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, items, err := h.n.Timeline(pileID)
	if err != nil {
		return nil, err
	}
	if member != nil && !isMember(p, member) {
		return nil, ErrUnknownMember
	}
	l, err := h.loader(pileID)
	if err != nil {
		return nil, err
	}
	l.setItems(p, items)
	return l, nil
}

func isMember(p node.Pile, member []byte) bool {
	var a op.Author
	if len(member) != len(a) {
		return false
	}
	copy(a[:], member)
	_, ok := p.Members[a]
	return ok
}

func (h *Host) Pin(pileID, member []byte) ([]Progress, error) {
	l, err := h.pile(pileID, member)
	if err != nil {
		return nil, err
	}
	return l.setPins(member, true)
}

func (h *Host) Unpin(pileID, member []byte) ([]Progress, error) {
	l, err := h.pile(pileID, member)
	if err != nil {
		return nil, err
	}
	return l.setPins(member, false)
}

func (h *Host) Pins(pileID []byte) ([]Progress, error) {
	l, err := h.pile(pileID, nil)
	if err != nil {
		return nil, err
	}
	return l.pinProgress(), nil
}

func (h *Host) Estimate(pileID, member []byte) (Estimate, error) {
	l, err := h.pile(pileID, member)
	if err != nil {
		return Estimate{}, err
	}
	return l.estimate(member), nil
}

func (h *Host) Close() {
	h.mu.Lock()
	h.stop()
	h.cancel()
	for _, l := range h.loaders {
		l.mu.Lock()
		l.mu.Unlock()
	}
	h.mu.Unlock()
	h.wg.Wait()
}

func (h *Host) Start(pileID []byte, screenID string, assetID []byte) (State, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stop()
	screen, err := h.screen(screenID)
	if err != nil {
		return State{}, err
	}
	changed, err := h.n.Changed(pileID)
	if err != nil {
		return State{}, err
	}
	p, items, err := h.n.Timeline(pileID)
	if err != nil {
		return State{}, err
	}
	if len(items) == 0 {
		return State{}, ErrEmpty
	}
	cursor := 0
	if assetID != nil {
		cursor = slices.IndexFunc(items, func(a state.Asset) bool { return bytes.Equal(a.Meta.AssetID, assetID) })
		if cursor < 0 {
			return State{}, node.ErrUnknownAsset
		}
	}
	l, err := h.loader(pileID)
	if err != nil {
		return State{}, err
	}
	ctx, cancel := context.WithCancel(h.ctx)
	s := &session{
		n:      h.n,
		pile:   pileID,
		self:   h.n.Author(),
		screen: screen,
		token:  rand.Text(),
		ctx:    ctx,
		cancel: cancel,
		wake:   make(chan struct{}, 1),
		l:      l,
	}
	if screen.Kind == "cast" {
		if err := s.connect(); err != nil {
			cancel()
			return State{}, err
		}
	}
	s.begin(p, items, cursor, changed)
	h.cur = s
	return s.state(), nil
}

func (h *Host) screen(id string) (node.Screen, error) {
	for _, s := range h.n.Screens() {
		if s.ID != id {
			continue
		}
		if !s.Available {
			return node.Screen{}, &UnavailableError{Reason: s.Reason}
		}
		return s, nil
	}
	return node.Screen{}, ErrUnknownScreen
}

func (h *Host) State() (State, error) {
	return h.do(func(s *session) error { return nil })
}

func (h *Host) Next() (State, error) {
	return h.do(func(s *session) error {
		i, kept := s.refresh()
		if kept {
			i++
		}
		return s.seek(i)
	})
}

func (h *Host) Prev() (State, error) {
	return h.do(func(s *session) error {
		i, _ := s.refresh()
		return s.seek(i - 1)
	})
}

func (h *Host) Goto(assetID []byte) (State, error) {
	return h.do(func(s *session) error {
		at, _ := s.refresh()
		i := slices.IndexFunc(s.items, func(a state.Asset) bool { return bytes.Equal(a.Meta.AssetID, assetID) })
		if i < 0 {
			if err := s.seek(at); err != nil {
				return err
			}
			return node.ErrUnknownAsset
		}
		return s.seek(i)
	})
}

func (h *Host) Play() (State, error) {
	return h.do(func(s *session) error { return s.control(playing) })
}

func (h *Host) Pause() (State, error) {
	return h.do(func(s *session) error { return s.control(paused) })
}

func (h *Host) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stop()
}

func (h *Host) stop() {
	if h.cur != nil {
		h.cur.stop()
		h.cur = nil
	}
}

func (h *Host) do(f func(s *session) error) (State, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.cur
	if s == nil {
		return State{}, ErrNoShow
	}
	if err := s.locked(f); err != nil {
		return State{}, err
	}
	return s.state(), nil
}

func (s *session) locked(f func(s *session) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return f(s)
}
