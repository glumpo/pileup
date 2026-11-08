package show

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"sync"

	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/prefetch"
)

const DefaultBudget = 1 << 30

var (
	ErrNoShow        = errors.New("show: no active show")
	ErrUnknownScreen = errors.New("show: unknown screen")
	ErrEmpty         = errors.New("show: pile has no files")
	ErrNotVideo      = errors.New("show: current file is not a video")
	ErrUnreachable   = errors.New("show: screen is unreachable")
)

type UnavailableError struct{ Reason string }

func (e *UnavailableError) Error() string { return "show: screen is unavailable: " + e.Reason }

type RendererError struct {
	Code    string
	Message string
}

type State struct {
	Pile    []byte
	Screen  node.Screen
	Index   int
	Total   int
	Current state.Asset
	Playing bool
	Error   *RendererError
	Waiting []state.Asset
}

type Host struct {
	n      *node.Node
	budget int64
	mu     sync.Mutex
	cur    *session
}

func New(n *node.Node, budget int64) *Host {
	return &Host{n: n, budget: budget}
}

func (h *Host) Start(pileID []byte, screenID string, assetID []byte) (State, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stop()
	screen, err := h.screen(screenID)
	if err != nil {
		return State{}, err
	}
	_, items, err := h.n.Timeline(pileID)
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
	ctx, cancel := context.WithCancel(context.Background())
	s := &session{
		n:       h.n,
		pile:    pileID,
		self:    h.n.Author(),
		screen:  screen,
		token:   rand.Text(),
		ctx:     ctx,
		cancel:  cancel,
		wake:    make(chan struct{}, 1),
		checked: map[string]bool{},
		cache:   prefetch.New(h.budget),
	}
	if screen.Kind == "cast" {
		if err := s.connect(); err != nil {
			cancel()
			return State{}, err
		}
	}
	s.begin(items, cursor)
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
		s.refresh()
		s.move(min(s.cursor+1, len(s.items)-1))
		return nil
	})
}

func (h *Host) Prev() (State, error) {
	return h.do(func(s *session) error {
		s.refresh()
		s.move(max(s.cursor-1, 0))
		return nil
	})
}

func (h *Host) Goto(assetID []byte) (State, error) {
	return h.do(func(s *session) error {
		s.refresh()
		i := slices.IndexFunc(s.items, func(a state.Asset) bool { return bytes.Equal(a.Meta.AssetID, assetID) })
		if i < 0 {
			return node.ErrUnknownAsset
		}
		s.move(i)
		return nil
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
	s.mu.Lock()
	err := f(s)
	s.mu.Unlock()
	if err != nil {
		return State{}, err
	}
	return s.state(), nil
}
