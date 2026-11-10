package node

import (
	"errors"
	"net"
	"slices"
	"syscall"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/peer"
	"github.com/glumpo/pileup/core/replica"
)

const (
	NoAddress   = "no_address"
	Refused     = "refused"
	TimedOut    = "timeout"
	NoRoute     = "no_route"
	OtherDevice = "other_device"
	Failed      = "error"
)

var hopeful = []string{Refused, TimedOut, NoRoute, OtherDevice, Failed, "", NoAddress}

type Presence struct {
	Online    bool
	LastSeen  time.Time
	LastError string
}

func (n *Node) Presence(pileID []byte, who op.Author) Presence {
	p, err := n.find(pileID)
	if err != nil {
		return Presence{}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if who == n.Author() || p.online[who] > 0 {
		return Presence{Online: true, LastSeen: time.Now()}
	}
	out := Presence{LastSeen: p.seen[who], LastError: NoAddress}
	for a, k := range p.peers {
		if k == who && slices.Index(hopeful, p.faults[a]) < slices.Index(hopeful, out.LastError) {
			out.LastError = p.faults[a]
		}
	}
	return out
}

func (n *Node) fault(p *pile, addr string, err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := p.peers[addr]; ok {
		p.faults[addr] = class(err)
	}
}

func class(err error) string {
	var ne net.Error
	switch {
	case errors.Is(err, peer.ErrMembership), errors.Is(err, peer.ErrCertificate), errors.Is(err, replica.ErrPeerChanged):
		return OtherDevice
	case errors.Is(err, syscall.ECONNREFUSED):
		return Refused
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		return NoRoute
	case errors.As(err, &ne) && ne.Timeout():
		return TimedOut
	}
	return Failed
}
