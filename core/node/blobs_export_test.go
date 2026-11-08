package node

import (
	"context"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
)

func (n *Node) Publish(pileID []byte, c op.Change) error {
	p, err := n.find(pileID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
	defer cancel()
	_, err = p.replica.Create(ctx, now(), c)
	return err
}

func (n *Node) Stored(pileID, assetID, revision []byte, tier int) bool {
	p, err := n.find(pileID)
	if err != nil {
		return false
	}
	a, err := p.asset(assetID, revision)
	if err != nil {
		return false
	}
	h, ok, err := n.blobs.Ref(ref(a.Owner, assetID, revision, tier))
	if !ok || err != nil {
		return false
	}
	ok, err = n.blobs.Has(h)
	return ok && err == nil
}
