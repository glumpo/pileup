package show_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

func (ph *phone) remove(p node.Pile, local string) {
	ph.t.Helper()
	if err := ph.Decide(context.Background(), p.ID, ph.asset(p, local).Meta.AssetID, false); err != nil {
		ph.t.Fatal(err)
	}
}

func (r *receiver) stopped() bool {
	cmds := r.commanded()
	return len(cmds) > 0 && cmds[len(cmds)-1] == "STOP"
}

func checkEmpty(t *testing.T, h *show.Host, st show.State) {
	t.Helper()
	if st.Total != 0 || st.Index != 0 || st.Current.Meta.AssetID != nil || st.Playing || st.VideoUnavailable {
		t.Fatalf("empty show: %+v", st)
	}
	for name, cmd := range map[string]func() (show.State, error){"next": h.Next, "prev": h.Prev, "play": h.Play, "pause": h.Pause} {
		if _, err := cmd(); !errors.Is(err, show.ErrEmpty) {
			t.Fatalf("%s in an empty show: %v", name, err)
		}
	}
}

func TestRemovedCurrentFileIsNotSkipped(t *testing.T) {
	show.Settle(t, time.Hour)
	a := newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a)
	for i, local := range []string{"A", "B", "C", "D"} {
		a.lib.photo(local, i)
	}
	a.share(p)
	rx := newReceiver(t)
	if st := must(t)(a.show.Start(p.ID, a.tv(rx), a.asset(p, "B").Meta.AssetID)); current(st) != "B" {
		t.Fatalf("started at %s", current(st))
	}
	rx.waitLoad(t, 1)

	c := a.asset(p, "C")
	a.remove(p, "B")
	st := must(t)(a.show.Next())
	if string(st.Current.Meta.AssetID) != string(c.Meta.AssetID) || st.Index != 1 || st.Total != 3 {
		t.Fatalf("next after removing the current file: %s, index %d of %d", current(st), st.Index, st.Total)
	}
	if _, body := get(t, rx.waitLoad(t, 2).url); string(body) != display("C") {
		t.Fatalf("renderer got %q", body)
	}

	must(t)(a.show.Goto(a.asset(p, "D").Meta.AssetID))
	rx.waitLoad(t, 3)
	a.remove(p, "D")
	st = must(t)(a.show.Prev())
	if string(st.Current.Meta.AssetID) != string(c.Meta.AssetID) || st.Index != 1 || st.Total != 2 {
		t.Fatalf("prev after removing the current last file: %s, index %d of %d", current(st), st.Index, st.Total)
	}
	last := rx.waitLoad(t, 4).url

	a.remove(p, "A")
	a.remove(p, "C")
	if _, err := a.show.Next(); !errors.Is(err, show.ErrEmpty) {
		t.Fatalf("next after removing all files: %v", err)
	}
	checkEmpty(t, a.show, must(t)(a.show.State()))
	if _, err := a.show.Goto(c.Meta.AssetID); !errors.Is(err, show.ErrEmpty) {
		t.Fatalf("goto in an empty show: %v", err)
	}
	eventually(t, "renderer stopped", rx.stopped)
	if resp, _ := get(t, last); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("removed file is still served: status %d", resp.StatusCode)
	}
	if n := len(rx.loaded()); n != 4 {
		t.Fatalf("receiver got %d LOADs after the show became empty", n)
	}
}

func TestAppliedRemovalUpdatesShow(t *testing.T) {
	a, b, p := twoPhones(t)
	rx := newReceiver(t)
	rx.fetching()
	must(t)(b.show.Start(p.ID, b.tv(rx), nil))
	first := rx.waitLoad(t, 1).url
	follows := func(want string, index, total int) func() bool {
		return func() bool {
			st := must(t)(b.show.State())
			return current(st) == want && st.Index == index && st.Total == total
		}
	}

	a.remove(p, "A2")
	eventually(t, "other file removed", follows("A1", 0, 3))
	if n := len(rx.loaded()); n != 1 {
		t.Fatalf("receiver got %d LOADs, the current file did not change", n)
	}

	a.remove(p, "A1")
	eventually(t, "current file removed", follows("B1", 0, 2))
	eventually(t, "receiver shows the next file", func() bool {
		shown := rx.displayed()
		return len(shown) == 2 && shown[1] == display("B1")
	})
	if resp, _ := get(t, first); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("removed file is still served: status %d", resp.StatusCode)
	}
	if st := must(t)(b.show.Next()); current(st) != "B2" || st.Index != 1 || st.Total != 2 {
		t.Fatalf("next after the update: %s, index %d of %d", current(st), st.Index, st.Total)
	}
	rx.waitLoad(t, 3)

	b.remove(p, "B2")
	eventually(t, "current last file removed", follows("B1", 0, 1))
	rx.waitLoad(t, 4)
	b.remove(p, "B1")
	eventually(t, "all files removed", func() bool { return must(t)(b.show.State()).Total == 0 })
	checkEmpty(t, b.show, must(t)(b.show.State()))
	eventually(t, "renderer stopped", rx.stopped)
	if n := len(rx.loaded()); n != 4 {
		t.Fatalf("receiver got %d LOADs, want 4", n)
	}
}

func TestAppliedRemovalMovesLoadWindow(t *testing.T) {
	a, b := newPhone(t, "ios", node.DefaultBudget), newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a, b)
	var locals []string
	for i := range 25 {
		locals = append(locals, fmt.Sprintf("P%02d", i))
		a.lib.photo(locals[i], i)
	}
	a.share(p)
	waitTimeline(t, p, 25, b)
	rx := newReceiver(t)
	must(t)(b.show.Start(p.ID, b.tv(rx), nil))
	eventually(t, "first window cached", func() bool {
		for _, l := range locals[:21] {
			if !b.cached(p, l) {
				return false
			}
		}
		return true
	})
	if a.lib.displayed("P21") != 0 {
		t.Fatal("P21 beyond the window was loaded")
	}

	a.remove(p, "P05")
	eventually(t, "window extends past the removed file", func() bool { return b.cached(p, "P21") })
	if st := must(t)(b.show.State()); st.Total != 24 || current(st) != "P00" {
		t.Fatalf("state after removal: %s of %d", current(st), st.Total)
	}
	if a.lib.displayed("P22") != 0 {
		t.Fatal("P22 beyond the window was loaded")
	}
}
