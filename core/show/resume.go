package show

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/node"
)

var remember = 2 * time.Second

type Interrupted struct {
	Pile   []byte
	Screen node.Screen
	Asset  []byte
}

type record struct {
	Pile    []byte `json:"pile"`
	Screen  string `json:"screen"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Addr    string `json:"addr"`
	Asset   []byte `json:"asset"`
	TakenAt int64  `json:"taken_at"`
}

func (r record) screen() node.Screen {
	addr, _ := netip.ParseAddrPort(r.Addr)
	return node.Screen{ID: r.Screen, Name: r.Name, Kind: r.Kind, Addr: addr, Available: true}
}

func recordPath(n *node.Node) string {
	return filepath.Join(n.Dir(), "show.json")
}

func (h *Host) saved() (record, bool) {
	b, err := os.ReadFile(recordPath(h.n))
	var r record
	if err != nil || json.Unmarshal(b, &r) != nil {
		return record{}, false
	}
	return r, true
}

func (h *Host) Interrupted() (Interrupted, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.saved()
	if !ok || h.cur != nil {
		return Interrupted{}, ErrNoShow
	}
	_, items, err := h.n.Timeline(r.Pile)
	if errors.Is(err, node.ErrUnknownPile) {
		os.Remove(recordPath(h.n))
		return Interrupted{}, ErrNoShow
	}
	if err != nil {
		return Interrupted{}, err
	}
	out := Interrupted{Pile: r.Pile, Screen: r.screen()}
	i := slices.IndexFunc(items, func(a state.Asset) bool { return bytes.Equal(a.Meta.AssetID, r.Asset) })
	if i < 0 {
		i = slices.IndexFunc(items, func(a state.Asset) bool { return a.Meta.TakenAt >= r.TakenAt })
	}
	if i < 0 {
		i = len(items) - 1
	}
	if i >= 0 {
		out.Asset = items[i].Meta.AssetID
	}
	return out, nil
}

func (h *Host) Forget() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cur == nil {
		os.Remove(recordPath(h.n))
	}
}

func (s *session) keep() {
	defer s.wg.Done()
	for {
		select {
		case <-s.moved:
			s.save()
		case <-s.ctx.Done():
		}
		select {
		case <-s.ctx.Done():
			select {
			case <-s.moved:
				s.save()
			default:
			}
			return
		case <-time.After(remember):
		}
	}
}

func (s *session) save() {
	s.mu.Lock()
	if s.cursor < 0 {
		s.mu.Unlock()
		return
	}
	a := s.items[s.cursor]
	r := record{Pile: s.pile, Screen: s.screen.ID, Name: s.screen.Name, Kind: s.screen.Kind, Addr: s.screen.Addr.String(), Asset: a.Meta.AssetID, TakenAt: a.Meta.TakenAt}
	s.mu.Unlock()
	b, err := json.Marshal(r)
	if err == nil {
		err = device.WriteFile(recordPath(s.n), b)
	}
	if err != nil {
		s.n.Diag().Error(diag.Renderer, s.pile, s.screen.Name, fmt.Errorf("remember show: %w", err))
	}
}
