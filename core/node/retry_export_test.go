package node

import (
	"context"
	"slices"
	"time"
)

func SetPause(f func(ctx context.Context, d time.Duration) error) (restore func()) {
	old := pause
	pause = f
	return func() { pause = old }
}

func (n *Node) Dialing(pileID []byte) []string {
	p, err := n.find(pileID)
	if err != nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []string
	for a := range p.loops {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}
