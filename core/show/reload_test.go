package show_test

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

func rendererError(t *testing.T, h *show.Host) string {
	t.Helper()
	st := must(t)(h.State())
	if st.Error == nil {
		return ""
	}
	return st.Error.Code
}

func urls(r *receiver) []string {
	var out []string
	for _, l := range r.loaded() {
		out = append(out, l.url)
	}
	return out
}

func noMoreLoads(t *testing.T, r *receiver) {
	t.Helper()
	before := urls(r)
	time.Sleep(3 * show.Reload)
	if after := urls(r); len(after) != len(before) {
		t.Fatalf("LOAD repeated: %v after %v", after[len(before):], before)
	}
}

func TestFailedLoadIsRepeatedWhenSourceIsBack(t *testing.T) {
	a, b := newPhone(t, "ios", node.DefaultBudget), newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a, b)
	a.lib.photo("A1", 0)
	a.share(p)
	waitTimeline(t, p, 1, b)
	eventually(t, "b connects to a", func() bool { return b.Online(p.ID)[a.Author()] })
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	rx := newReceiver(t)
	rx.fetching()
	must(t)(b.show.Start(p.ID, b.tv(rx), nil))
	eventually(t, "LOAD failed", func() bool { return rendererError(t, b.show) == "load_failed" })
	if shown := rx.displayed(); len(shown) != 0 {
		t.Fatalf("receiver showed %q while the source is offline", shown)
	}
	url := urls(rx)[0]

	openPhone(t, a.dir, "ios", a.lib, node.DefaultBudget)
	eventually(t, "receiver shows the file", func() bool {
		shown := rx.displayed()
		return len(shown) == 1 && shown[0] == display("A1")
	})
	eventually(t, "error cleared", func() bool { return rendererError(t, b.show) == "" })
	for _, u := range urls(rx) {
		if u != url {
			t.Fatalf("LOAD of %s, want only %s", u, url)
		}
	}
	noMoreLoads(t, rx)
	if shown := rx.displayed(); len(shown) != 1 {
		t.Fatalf("receiver showed %q", shown)
	}
}

func TestLoadEndingInPlaybackErrorIsRepeated(t *testing.T) {
	a := newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a)
	a.lib.photo("P0", 0)
	a.lib.photo("P1", 1)
	a.share(p)
	rx := newReceiver(t)
	rx.fetching()
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	first := rx.waitLoad(t, 1).url

	rx.push("IDLE", "ERROR")
	eventually(t, "LOAD repeated after a late error status", func() bool { return len(urls(rx)) >= 2 })
	eventually(t, "error cleared", func() bool { return rendererError(t, a.show) == "" })
	if got := urls(rx); fmt.Sprint(got) != fmt.Sprint([]string{first, first}) {
		t.Fatalf("LOADs %v, want the first file twice", got)
	}
	noMoreLoads(t, rx)

	rx.failPlayback(true)
	must(t)(a.show.Next())
	eventually(t, "error status in the LOAD answer", func() bool { return rendererError(t, a.show) != "" })
	second := urls(rx)[2]
	rx.failPlayback(false)
	eventually(t, "error cleared after a repeated LOAD", func() bool { return rendererError(t, a.show) == "" })
	got := urls(rx)
	if len(got) < 4 {
		t.Fatalf("LOADs %v: the second file is not repeated", got)
	}
	for _, u := range got[2:] {
		if u != second {
			t.Fatalf("LOADs %v, want only the second file after next", got)
		}
	}
	noMoreLoads(t, rx)
	if shown := rx.displayed(); shown[len(shown)-1] != display("P1") {
		t.Fatalf("receiver shows %q", shown)
	}
}

func TestLoadRetryFollowsCursorAndEndsWithShow(t *testing.T) {
	a := newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a)
	a.lib.photo("P0", 0)
	a.lib.photo("P1", 1)
	a.share(p)
	rx := newReceiver(t)
	rx.failLoads(true)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	eventually(t, "first LOAD failed", func() bool { return rendererError(t, a.show) == "load_failed" })

	must(t)(a.show.Next())
	eventually(t, "LOAD of the new file repeated", func() bool { return len(urls(rx)) >= 3 })
	rx.failLoads(false)
	eventually(t, "error cleared", func() bool { return rendererError(t, a.show) == "" })
	got := urls(rx)
	for _, u := range got[1:] {
		if u != got[1] || u == got[0] {
			t.Fatalf("LOADs %v: retry does not follow the cursor", got)
		}
	}
	noMoreLoads(t, rx)

	rx.failLoads(true)
	n := len(urls(rx))
	must(t)(a.show.Prev())
	eventually(t, "failed LOAD repeated", func() bool { return len(urls(rx)) >= n+2 })
	a.show.Stop()
	noMoreLoads(t, rx)
}

func TestShowReconnectsToScreenWithGrowingPauses(t *testing.T) {
	a := newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a)
	a.lib.photo("P0", 0)
	a.lib.photo("P1", 1)
	a.share(p)
	rx := newReceiver(t)
	rx.fetching()
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	first := rx.waitLoad(t, 1).url

	rx.refusing(true)
	dropped := time.Now()
	rx.drop()
	eventually(t, "reconnecting in state", func() bool { return rendererError(t, a.show) == "reconnecting" })
	if st := must(t)(a.show.Next()); current(st) != "P1" || st.Error == nil || st.Error.Code != "reconnecting" {
		t.Fatalf("next while reconnecting: %+v", st)
	}
	if resp, err := http.Get(first); err == nil {
		resp.Body.Close()
		t.Fatalf("media is served while the screen is disconnected: status %d", resp.StatusCode)
	}
	eventually(t, "two refused attempts", func() bool { return len(rx.dialed()) == 3 })
	rx.refusing(false)
	eventually(t, "current file shown after reconnecting", func() bool {
		shown := rx.displayed()
		return len(shown) == 2 && shown[1] == display("P1")
	})
	eventually(t, "error cleared", func() bool { return rendererError(t, a.show) == "" })
	dials := rx.dialed()
	if len(dials) != 4 {
		t.Fatalf("%d connections to the screen, want 4", len(dials))
	}
	prev := dropped
	for i, pause := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		if gap := dials[i+1].Sub(prev); gap < pause || gap > pause+time.Second {
			t.Fatalf("attempt %d after %v, want %v", i+1, gap, pause)
		}
		prev = dials[i+1]
	}
	if !slices.ContainsFunc(a.Diag().Events(), func(e diag.Event) bool {
		return e.Kind == diag.Renderer && e.Message == "reconnected to the screen"
	}) {
		t.Fatal("reconnect is not in diag")
	}
}

func TestStoppedShowStopsReconnecting(t *testing.T) {
	a := newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a)
	a.lib.photo("P0", 0)
	a.share(p)
	rx := newReceiver(t)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	rx.waitLoad(t, 1)

	rx.refusing(true)
	rx.drop()
	eventually(t, "first attempt to reconnect", func() bool { return len(rx.dialed()) == 2 })
	stopped := time.Now()
	a.show.Stop()
	if d := time.Since(stopped); d > time.Second {
		t.Fatalf("stop took %v", d)
	}
	time.Sleep(3 * time.Second)
	if n := len(rx.dialed()); n != 2 {
		t.Fatalf("%d connections to the screen after stop, want 2", n)
	}
}

func TestReconnectPauseStopsGrowingAtMaximum(t *testing.T) {
	show.Pauses(t, 100*time.Millisecond, 400*time.Millisecond)
	a := newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a)
	a.lib.photo("P0", 0)
	a.share(p)
	rx := newReceiver(t)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	rx.waitLoad(t, 1)

	rx.refusing(true)
	dropped := time.Now()
	rx.drop()
	eventually(t, "six refused attempts", func() bool { return len(rx.dialed()) == 7 })
	dials, prev := rx.dialed(), dropped
	for i, pause := range []time.Duration{100, 200, 400, 400, 400, 400} {
		pause *= time.Millisecond
		if gap := dials[i+1].Sub(prev); gap < pause || gap >= pause+300*time.Millisecond {
			t.Fatalf("attempt %d after %v, want %v", i+1, gap, pause)
		}
		prev = dials[i+1]
	}
}
