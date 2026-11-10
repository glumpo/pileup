package node

func (n *Node) Stored(pileID, assetID, revision []byte, tier int) bool {
	_, ok := n.Cached(pileID, assetID, revision, tier)
	return ok
}
