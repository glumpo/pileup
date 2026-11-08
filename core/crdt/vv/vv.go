package vv

import "github.com/glumpo/pileup/core/crdt/op"

type VV map[op.Author]uint64

type Range struct {
	From, To uint64
}

func Missing(peer, have VV) map[op.Author]Range {
	m := map[op.Author]Range{}
	for a, n := range have {
		if peer[a] < n {
			m[a] = Range{From: peer[a] + 1, To: n}
		}
	}
	return m
}
