package node

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/glumpo/pileup/core/diag"
)

const longPause = 30 * time.Second

var networkCheck = 5 * time.Second

func delay(retry int) time.Duration {
	if retry > retries {
		return longPause
	}
	return time.Duration(retry) * backoff
}

func (n *Node) rest(ctx context.Context, l *loop, d time.Duration) (bool, error) {
	pctx, kick := context.WithCancel(ctx)
	defer kick()
	n.mu.Lock()
	l.kick = kick
	n.mu.Unlock()
	err := pause(pctx, d)
	n.mu.Lock()
	l.kick = nil
	n.mu.Unlock()
	switch {
	case ctx.Err() != nil:
		return false, ctx.Err()
	case pctx.Err() != nil:
		return true, nil
	}
	return false, err
}

func (n *Node) hurry(p *pile, addr string) {
	if l := p.loops[addr]; l == nil {
		n.dial(p, addr, func() {})
	} else if l.kick != nil {
		l.kick()
	}
}

func (n *Node) Reconnect(reason string) {
	n.mu.Lock()
	if n.ctx.Err() != nil {
		n.mu.Unlock()
		return
	}
	var now, started int
	for _, p := range n.piles {
		for _, l := range p.loops {
			if l.kick != nil {
				l.kick()
				now++
			}
		}
		for a := range p.peers {
			if p.loops[a] == nil {
				n.dial(p, a, func() {})
				started++
			}
		}
		n.wg.Add(1)
		go n.check(p, p.ln)
	}
	n.mu.Unlock()
	n.diag.Add(diag.Connection, nil, "", fmt.Sprintf("reconnect, %s: %d waiting loops retried now, %d loops started", reason, now, started))
}

func (n *Node) watchNetwork() {
	if n.cfg.Addrs == nil {
		return
	}
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		t := time.NewTicker(networkCheck)
		defer t.Stop()
		last := sortedAddrs(n.cfg.Addrs())
		for {
			select {
			case <-n.ctx.Done():
				return
			case <-t.C:
			}
			now := sortedAddrs(n.cfg.Addrs())
			if !slices.Equal(now, last) {
				n.diag.Add(diag.Connection, nil, "", fmt.Sprintf("network addresses changed from %v to %v", last, now))
				last = now
				n.Reconnect("network addresses changed")
			}
		}
	}()
}

func sortedAddrs(addrs []netip.Addr) []netip.Addr {
	out := slices.Clone(addrs)
	slices.SortFunc(out, func(a, b netip.Addr) int { return a.Compare(b) })
	return out
}
