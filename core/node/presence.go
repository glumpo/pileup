package node

import (
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
)

const (
	NoAddress   = "no_address"
	Refused     = "refused"
	TimedOut    = "timeout"
	NoRoute     = "no_route"
	OtherDevice = "other_device"
	Failed      = "error"
)

type Presence struct {
	Online    bool
	LastSeen  time.Time
	LastError string
}

func (n *Node) Presence(pileID []byte, who op.Author) Presence {
	return Presence{}
}
