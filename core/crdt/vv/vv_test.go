package vv_test

import (
	"reflect"
	"testing"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/vv"
)

func TestMissing(t *testing.T) {
	a, b, c, d := op.Author{1}, op.Author{2}, op.Author{3}, op.Author{4}
	have := vv.VV{a: 5, b: 3, c: 2}
	peer := vv.VV{a: 2, b: 3, c: 7, d: 1}
	got := vv.Missing(peer, have)
	if !reflect.DeepEqual(got, map[op.Author]vv.Range{a: {From: 3, To: 5}}) {
		t.Errorf("Missing(peer, have) = %v", got)
	}
	got = vv.Missing(have, peer)
	if !reflect.DeepEqual(got, map[op.Author]vv.Range{c: {From: 3, To: 7}, d: {From: 1, To: 1}}) {
		t.Errorf("Missing(have, peer) = %v", got)
	}
	if got := vv.Missing(have, have); len(got) != 0 {
		t.Errorf("Missing(have, have) = %v", got)
	}
}
