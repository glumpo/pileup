package node

import (
	"bytes"
	"cmp"
	"slices"

	"github.com/glumpo/pileup/core/crdt/state"
)

type timeline struct {
	changed <-chan struct{}
	pile    Pile
	items   []state.Asset
}

func (n *Node) Timeline(id []byte) (Pile, []state.Asset, error) {
	p, err := n.find(id)
	if err != nil {
		return Pile{}, nil, err
	}
	changed := p.replica.Changed()
	n.mu.Lock()
	c := p.cached
	n.mu.Unlock()
	if c.changed == changed {
		return c.pile, c.items, nil
	}
	v, err := p.store.View()
	if err != nil {
		return Pile{}, nil, err
	}
	out := make([]state.Asset, 0, len(v.Assets))
	for _, a := range v.Assets {
		out = append(out, a)
	}
	slices.SortFunc(out, func(x, y state.Asset) int {
		return cmp.Or(
			cmp.Compare(x.Meta.TakenAt, y.Meta.TakenAt),
			bytes.Compare(x.Owner[:], y.Owner[:]),
			bytes.Compare(x.Meta.AssetID, y.Meta.AssetID),
		)
	})
	c = timeline{changed: changed, pile: Pile{ID: p.ID, Name: v.PileName, Members: v.Members}, items: out}
	n.mu.Lock()
	p.cached = c
	n.mu.Unlock()
	return c.pile, c.items, nil
}

func (n *Node) Changed(id []byte) (<-chan struct{}, error) {
	p, err := n.find(id)
	if err != nil {
		return nil, err
	}
	return p.replica.Changed(), nil
}
