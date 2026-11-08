package node

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/peer"
)

const diagSize = 1000

func (n *Node) Diag() *diag.Log {
	return n.diag
}

func (n *Node) report(k diag.Kind, pileID []byte, peer, what string, err error) {
	if err != nil && n.ctx.Err() == nil && !errors.Is(err, context.Canceled) {
		n.diag.Error(k, pileID, peer, fmt.Errorf("%s: %w", what, err))
	}
}

func (n *Node) synced(p *pile, addr string) func(int) {
	first := true
	return func(ops int) {
		if first {
			first = false
			n.diag.Add(diag.Sync, p.ID, addr, "sync established")
		}
		if ops > 0 {
			n.diag.Add(diag.Sync, p.ID, addr, fmt.Sprintf("received %d ops", ops))
		}
	}
}

func (n *Node) handshakes(p *pile) func(net.Conn, http.ConnState) {
	return func(c net.Conn, s http.ConnState) {
		if s == http.StateClosed {
			host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
			n.report(diag.Connection, p.ID, host, "incoming handshake", c.(*peer.Conn).Handshake())
		}
	}
}
