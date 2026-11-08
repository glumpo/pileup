package state_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"pgregory.net/rapid"
)

var (
	names    = rapid.SampledFrom([]string{"", "Аня", "Боря", "Грузия", "x"})
	assetIDs = rapid.SampledFrom([]string{"a", "b", "c", "d"})
)

func drawChange(t *rapid.T) op.Change {
	switch rapid.IntRange(0, 4).Draw(t, "change") {
	case 0:
		return op.PileCreate{Name: names.Draw(t, "pile name")}
	case 1:
		return op.MemberSet{Name: names.Draw(t, "member name")}
	case 2:
		var metas []op.AssetMeta
		for _, id := range rapid.SliceOfN(assetIDs, 1, 3).Draw(t, "add") {
			metas = append(metas, asset(id, names.Draw(t, "file name")))
		}
		return op.AssetAdd{Assets: metas}
	case 3:
		var ids [][]byte
		for _, id := range rapid.SliceOfN(assetIDs, 1, 3).Draw(t, "remove") {
			ids = append(ids, []byte(id))
		}
		return op.AssetRemove{AssetIDs: ids}
	}
	return unknown{X: names.Draw(t, "unknown")}
}

func history(t *rapid.T) ([]*node, [][][]byte) {
	nodes := make([]*node, rapid.IntRange(1, 4).Draw(t, "authors"))
	for i := range nodes {
		nodes[i] = newNode(byte(i + 1))
		nodes[i].join(t, names.Draw(t, "join name"), "ios")
	}
	pick := rapid.IntRange(0, len(nodes)-1)
	for range rapid.IntRange(0, 40).Draw(t, "steps") {
		n := nodes[pick.Draw(t, "node")]
		if rapid.IntRange(0, 2).Draw(t, "sync") == 0 {
			n.pull(t, nodes[pick.Draw(t, "peer")].State)
			continue
		}
		before := n.View().Lamport
		o := n.create(t, drawChange(t))
		if o.Lamport != before+1 || n.View().Lamport != o.Lamport {
			t.Fatalf("new op lamport %d, L before %d after %d", o.Lamport, before, n.View().Lamport)
		}
	}
	logs := make([][][]byte, len(nodes))
	for i, n := range nodes {
		logs[i] = n.Ops(n.author(), 1)
	}
	return nodes, logs
}

func inOrder(t *rapid.T, logs [][][]byte) state.View {
	s := state.New(pile, secret)
	for _, log := range logs {
		for _, raw := range log {
			if _, err := s.Apply(raw); err != nil {
				t.Fatal(err)
			}
		}
	}
	return s.View()
}

func TestAnyDeliveryOrderGivesSameState(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		_, logs := history(t)
		want := inOrder(t, logs)

		s := state.New(pile, secret)
		next := make([]int, len(logs))
		var delivered [][]byte
		rejectedWithoutEffect := func(raw []byte) {
			before := s.View()
			if _, err := s.Apply(raw); !errors.Is(err, op.ErrSeq) {
				t.Fatalf("out of order delivery: err = %v, want ErrSeq", err)
			}
			if !reflect.DeepEqual(s.View(), before) {
				t.Fatal("rejected op changed state")
			}
		}
		for {
			var pending, ahead []int
			for i, log := range logs {
				if next[i] < len(log) {
					pending = append(pending, i)
				}
				if next[i]+1 < len(log) {
					ahead = append(ahead, i)
				}
			}
			if len(pending) == 0 {
				break
			}
			switch rapid.IntRange(0, 3).Draw(t, "delivery") {
			case 0:
				if len(delivered) > 0 {
					rejectedWithoutEffect(rapid.SampledFrom(delivered).Draw(t, "duplicate"))
				}
			case 1:
				if len(ahead) > 0 {
					i := rapid.SampledFrom(ahead).Draw(t, "ahead author")
					rejectedWithoutEffect(logs[i][rapid.IntRange(next[i]+1, len(logs[i])-1).Draw(t, "ahead seq")])
				}
			default:
				i := rapid.SampledFrom(pending).Draw(t, "author")
				raw := logs[i][next[i]]
				if _, err := s.Apply(raw); err != nil {
					t.Fatal(err)
				}
				delivered = append(delivered, raw)
				next[i]++
			}
		}
		if got := s.View(); !reflect.DeepEqual(got, want) {
			t.Fatalf("state differs from in-order delivery:\n got %+v\nwant %+v", got, want)
		}
	})
}

func TestNodesConvergeAfterSync(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nodes, logs := history(t)
		want := inOrder(t, logs)
		for _, n := range nodes {
			for _, peer := range nodes {
				n.pull(t, peer.State)
			}
		}
		for i, n := range nodes {
			if got := n.View(); !reflect.DeepEqual(got, want) {
				t.Fatalf("node %d differs:\n got %+v\nwant %+v", i, got, want)
			}
		}
	})
}
