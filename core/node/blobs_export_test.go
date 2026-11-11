package node

import "time"

func (n *Node) Stored(pileID, assetID, revision []byte, tier int) bool {
	_, ok := n.Cached(pileID, assetID, revision, tier)
	return ok
}

func SetFetchTimeouts(headers, idle time.Duration) (restore func()) {
	oldHeaders, oldIdle := headerTimeout, readIdle
	headerTimeout, readIdle = headers, idle
	return func() { headerTimeout, readIdle = oldHeaders, oldIdle }
}

func SetBrokenPause(d time.Duration) (restore func()) {
	old := brokenPause
	brokenPause = d
	return func() { brokenPause = old }
}

func SetRenderTimeout(d time.Duration) (restore func()) {
	old := renderTimeout
	renderTimeout = d
	return func() { renderTimeout = old }
}

func SetOwnerPause(d time.Duration) (restore func()) {
	old := ownerPause
	ownerPause = d
	return func() { ownerPause = old }
}
