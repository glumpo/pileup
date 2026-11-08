package node

import (
	"bytes"
	"fmt"
	"image/jpeg"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/thumbhash"
)

const ThumbhashBatch = 32

func (n *Node) thumbhashes(p *pile) {
	if n.cfg.Renditions == nil {
		return
	}
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		failed := map[string]bool{}
		for {
			changed := p.replica.Changed()
			for n.ctx.Err() == nil && n.thumbhashBatch(p, failed) {
			}
			select {
			case <-n.ctx.Done():
				return
			case <-changed:
			}
		}
	}()
}

func (n *Node) thumbhashBatch(p *pile, failed map[string]bool) bool {
	mine, err := n.published(p)
	if err != nil {
		return false
	}
	var todo []op.AssetMeta
	for _, m := range mine {
		if len(m.Thumbhash) == 0 && !failed[string(m.AssetID)+string(m.Revision)] {
			todo = append(todo, m)
		}
	}
	more := len(todo) > ThumbhashBatch
	var done []op.AssetMeta
	for _, m := range todo[:min(len(todo), ThumbhashBatch)] {
		h, err := n.thumbhash(p, m)
		if err != nil {
			n.report(diag.Media, p.ID, "", fmt.Sprintf("thumbhash %x", m.AssetID), err)
			failed[string(m.AssetID)+string(m.Revision)] = true
			continue
		}
		m.Thumbhash = h
		done = append(done, m)
	}
	return n.publishThumbhashes(p, done) == nil && more
}

func (n *Node) thumbhash(p *pile, m op.AssetMeta) ([]byte, error) {
	f, _, err := n.open(n.ctx, p, m.AssetID, m.Revision, T1)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		return nil, err
	}
	return thumbhash.Encode(img), nil
}

func (n *Node) publishThumbhashes(p *pile, done []op.AssetMeta) error {
	n.selecting.Lock()
	defer n.selecting.Unlock()
	mine, err := n.published(p)
	if err != nil {
		return err
	}
	var add op.AssetAdd
	for _, d := range done {
		m, ok := mine[string(d.AssetID)]
		if ok && bytes.Equal(m.Revision, d.Revision) {
			m.Thumbhash = d.Thumbhash
			add.Assets = append(add.Assets, m)
		}
	}
	if len(add.Assets) == 0 {
		return nil
	}
	_, err = p.replica.Create(n.ctx, now(), add)
	return err
}
