package node

import (
	"context"
	"net"
	"slices"
	"time"
)

func SetPause(f func(ctx context.Context, d time.Duration) error) (restore func()) {
	old := pause
	pause = f
	return func() { pause = old }
}

func SetNetworkCheck(d time.Duration) (restore func()) {
	old := networkCheck
	networkCheck = d
	return func() { networkCheck = old }
}

func SetAnnounceTimeout(d time.Duration) (restore func()) {
	old := announceTimeout
	announceTimeout = d
	return func() { announceTimeout = old }
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

func SetListen(f func(network, addr string) (net.Listener, error)) (restore func()) {
	old := netListen
	netListen = f
	return func() { netListen = old }
}
