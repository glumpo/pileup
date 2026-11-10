package node

import (
	"maps"
	"slices"
)

func (n *Node) Peers(pileID []byte) []string {
	p, err := n.find(pileID)
	if err != nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Sorted(maps.Keys(p.peers))
}
