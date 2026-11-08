package replica_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/vv"
	"github.com/glumpo/pileup/core/replica"
)

func TestTwoNodesConverge(t *testing.T) {
	ka, kb := key(1), key(2)
	a, b := newNode(t, ka, "Аня"), newNode(t, kb, "Боря")
	a.create(t, op.PileCreate{Name: "Грузия"})
	a.create(t, assets("x", "y"))
	b.create(t, assets("z"))

	addrA, addrB := a.serve(t, nil), b.serve(t, nil)
	live := []*session{a.pull(t, addrB), b.pull(t, addrA)}
	waitFor(t, "initial sync", converged(t, vv.VV{author(ka): 3, author(kb): 2}, a, b), live...)

	a.create(t, op.AssetRemove{AssetIDs: [][]byte{[]byte("x")}})
	waitFor(t, "op of a created while connected", converged(t, vv.VV{author(ka): 4, author(kb): 2}, a, b), live...)
	b.create(t, op.MemberSet{Name: "Борис"})
	waitFor(t, "op of b created while connected", converged(t, vv.VV{author(ka): 4, author(kb): 3}, a, b), live...)

	v, err := b.store.View()
	if err != nil {
		t.Fatal(err)
	}
	if v.PileName != "Грузия" || v.Members[author(ka)].Name != "Аня" || len(v.Assets) != 2 {
		t.Fatalf("b sees %+v", v)
	}
}

func forge(t *testing.T, raw []byte) []byte {
	var env struct {
		_    struct{} `cbor:",toarray"`
		Body []byte
		Sig  []byte
	}
	var body op.Body
	if err := cbor.Unmarshal(raw, &env); err != nil {
		t.Error(err)
	}
	if err := cbor.Unmarshal(env.Body, &body); err != nil {
		t.Error(err)
	}
	body.WallTime++
	var err error
	if env.Body, err = cbor.Marshal(body); err != nil {
		t.Error(err)
	}
	out, err := cbor.Marshal(env)
	if err != nil {
		t.Error(err)
	}
	return out
}

func TestOpsRelayedByThirdNode(t *testing.T) {
	ka, kc := key(1), key(3)
	a, c := newNode(t, ka, "Аня"), newNode(t, kc, "Вера")
	a.create(t, op.PileCreate{Name: "Грузия"})
	a.create(t, assets("x"))
	toA := c.pull(t, a.serve(t, nil))
	waitFor(t, "relay got a's journal", func() bool { return c.vv(t)[author(ka)] == 3 }, toA)
	toA.stop(t)

	t.Run("honest relay", func(t *testing.T) {
		kb := key(2)
		b := newNode(t, kb, "Боря")
		s := b.pull(t, c.serve(t, nil))
		waitFor(t, "b got a's ops via c", func() bool {
			return reflect.DeepEqual(b.vv(t), vv.VV{author(ka): 3, author(kc): 1, author(kb): 1})
		}, s)
		v, err := b.store.View()
		if err != nil {
			t.Fatal(err)
		}
		if v.PileName != "Грузия" || v.Assets["x"].Owner != author(ka) {
			t.Fatalf("b sees %+v", v)
		}
	})

	t.Run("relay tampering with bytes", func(t *testing.T) {
		kb := key(2)
		b := newNode(t, kb, "Боря")
		tamper := func(h http.Handler) http.Handler {
			return rewriteOps(t, h, func(_ *http.Request, raws [][]byte) [][]byte {
				var out [][]byte
				for _, raw := range raws {
					out = append(out, forge(t, raw))
				}
				return out
			})
		}
		err := b.pull(t, c.serve(t, tamper)).wait(t)
		if !errors.Is(err, op.ErrSignature) {
			t.Fatalf("pull from tampering relay: %v", err)
		}
		if got := b.vv(t); !reflect.DeepEqual(got, vv.VV{author(kb): 1}) {
			t.Fatalf("b accepted forged ops: %v", got)
		}
	})
}

func TestReconnectTransfersOnlyMissing(t *testing.T) {
	ka, kb, kc := key(1), key(2), key(3)
	a, b := newNode(t, ka, "Аня"), newNode(t, kb, "Боря")
	for i := range 19 {
		a.create(t, assets(string(rune('a'+i))))
	}
	a.apply(t, history(t, kc, 2)...)
	var log served
	addrA := a.serve(t, log.wrap(t))

	s := b.pull(t, addrA)
	want := vv.VV{author(ka): 20, author(kb): 1, author(kc): 2}
	waitFor(t, "first sync", func() bool { return reflect.DeepEqual(b.vv(t), want) }, s)
	s.stop(t)
	first := log.take()
	if len(first[author(ka)]) != 20 || len(first[author(kc)]) != 2 || len(first) != 2 {
		t.Fatalf("first sync transferred %v", first)
	}

	for _, id := range []string{"x", "y", "z"} {
		a.create(t, assets(id))
	}
	b.create(t, op.MemberSet{Name: "Борис"})
	s = b.pull(t, addrA)
	want[author(ka)], want[author(kb)] = 23, 2
	waitFor(t, "sync after reconnect", func() bool { return reflect.DeepEqual(b.vv(t), want) }, s)
	s.stop(t)
	if got := log.take(); !reflect.DeepEqual(got, map[op.Author][]uint64{author(ka): {21, 22, 23}}) {
		t.Fatalf("reconnect transferred %v, want only seq 21..23 of a", got)
	}
}

func TestRestoredNodeCatchesUpBeforeFirstWrite(t *testing.T) {
	ka, kb := key(1), key(2)
	journal := history(t, ka, 4)
	b := newNode(t, kb, "Боря")
	b.apply(t, journal...)
	var once sync.Once
	asked := make(chan struct{})
	addrB := b.serve(t, func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			once.Do(func() { close(asked) })
			h.ServeHTTP(w, r)
		})
	})

	restored := nodeOn(openStore(t), ka)
	restored.apply(t, journal[:2]...)
	s := restored.pull(t, addrB)
	<-asked
	o := restored.create(t, op.MemberSet{Name: "Анна"})
	if head := id(t, journal[3]); o.Seq != 5 || !bytes.Equal(o.Prev, head[:]) {
		t.Fatalf("first write after restore: seq %d, prev %x", o.Seq, o.Prev)
	}

	back := b.pull(t, restored.serve(t, nil))
	waitFor(t, "b accepted the new op", func() bool { return b.vv(t)[author(ka)] == 5 }, s, back)
}

func closedAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	return l.Addr().String()
}

func TestFirstWriteWithoutReachablePeers(t *testing.T) {
	ka, kb := key(1), key(2)
	journal := history(t, ka, 4)
	b := newNode(t, kb, "Боря")
	b.apply(t, journal...)

	for _, tc := range []struct {
		name  string
		peers int
	}{{"no peers", 0}, {"peer unreachable", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			restored := nodeOn(openStore(t), ka)
			restored.apply(t, journal[:2]...)
			for range tc.peers {
				restored.pull(t, closedAddr(t))
			}
			if o := restored.create(t, op.MemberSet{Name: "Анна"}); o.Seq != 3 {
				t.Fatalf("offline write got seq %d, want 3", o.Seq)
			}
			err := restored.pull(t, b.serve(t, nil)).wait(t)
			if !errors.Is(err, replica.ErrFork) {
				t.Fatalf("pull of the longer own log: %v", err)
			}
		})
	}
}

func resend(t *testing.T, a op.Author) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return rewriteOps(t, h, func(r *http.Request, raws [][]byte) [][]byte {
			again := r.Clone(r.Context())
			again.URL.RawQuery = url.Values{"author": {hex.EncodeToString(a[:])}, "from": {"1"}}.Encode()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, again)
			old, err := frames(rec.Body.Bytes())
			if err != nil {
				t.Error(err)
			}
			return append(old, raws...)
		})
	}
}

func TestForkOfferedAtKnownSeq(t *testing.T) {
	ka, kb := key(1), key(2)
	journal := history(t, ka, 3)
	b := newNode(t, kb, "Боря")
	b.apply(t, journal...)

	restored := nodeOn(openStore(t), ka)
	restored.apply(t, journal[:2]...)
	own := restored.create(t, op.MemberSet{Name: "Анна"})

	err := restored.pull(t, b.serve(t, resend(t, author(ka)))).wait(t)
	if !errors.Is(err, replica.ErrFork) {
		t.Fatalf("pull from peer offering a different op at seq 3: %v", err)
	}
	if got, ok, err := restored.store.ID(author(ka), 3); err != nil || !ok || got != own.ID {
		t.Fatalf("own op at seq 3 replaced: %v %v", ok, err)
	}
}

func TestResentOpsAreIgnored(t *testing.T) {
	ka, kb := key(1), key(2)
	a, b := newNode(t, ka, "Аня"), newNode(t, kb, "Боря")
	a.create(t, assets("x"))
	s := b.pull(t, a.serve(t, resend(t, author(ka))))
	waitFor(t, "first sync", converged(t, vv.VV{author(ka): 2, author(kb): 1}, b), s)
	a.create(t, assets("y"))
	waitFor(t, "sync of a new op", func() bool { return b.vv(t)[author(ka)] == 3 }, s)
}
