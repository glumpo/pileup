package show_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/httpapi"
	"github.com/glumpo/pileup/core/show"
)

func TestHintNamesMemberWhoseFilesWait(t *testing.T) {
	a, b, c := newPhone(t, "android", show.DefaultBudget), newPhone(t, "android", show.DefaultBudget), newPhone(t, "ios", show.DefaultBudget)
	p := pileOf(t, a, b, c)
	for i := range 3 {
		a.lib.photo(fmt.Sprintf("A%d", i), 2*i)
		c.lib.photo(fmt.Sprintf("C%d", i), 2*i+1)
	}
	a.share(p)
	c.share(p)
	waitTimeline(t, p, 6, b)
	eventually(t, "b connects to c", func() bool { return b.Online(p.ID)[c.Author()] })
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	rx := newReceiver(t)
	must(t)(b.show.Start(p.ID, b.tv(rx), nil))
	want := []show.Hint{{Member: c.Author(), Name: "участник 1", Platform: "ios", Reason: show.Unreachable}}
	eventually(t, "hint names c", func() bool { return reflect.DeepEqual(must(t)(b.show.State()).Hints, want) })
	ui := httptest.NewServer(httpapi.WithNode("ui", b.Node, b.show))
	t.Cleanup(ui.Close)
	_, body := get(t, ui.URL+"/show", "Authorization", "Bearer ui")
	var st struct{ Hints []map[string]string }
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatal(err)
	}
	wantJSON := []map[string]string{{"author": fmt.Sprintf("%x", c.Author()), "name": "участник 1", "platform": "ios", "reason": "unreachable"}}
	if !reflect.DeepEqual(st.Hints, wantJSON) {
		t.Fatalf("hints in the UI API: %s", body)
	}
	eventually(t, "timeout in diag", func() bool {
		for _, e := range b.Diag().Events() {
			if e.Kind == diag.Timeout && e.Peer == fmt.Sprintf("%x", c.Author()) && strings.Contains(e.Message, "участник 1") {
				return true
			}
		}
		return false
	})

	openPhone(t, c.dir, "ios", c.lib, show.DefaultBudget)
	eventually(t, "no hints after c is back", func() bool { return len(must(t)(b.show.State()).Hints) == 0 })
}

func progressOf(t *testing.T, ph *phone, pileID []byte) map[string]show.Progress {
	t.Helper()
	list, err := ph.show.Pins(pileID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]show.Progress{}
	for _, x := range list {
		out[x.Name] = x
	}
	return out
}

func counts(p show.Progress) [3]int { return [3]int{p.Total, p.Done, p.Failed} }

func TestLoadAllFromMemberThenEverythingWithoutVideos(t *testing.T) {
	a, b, c := newPhone(t, "ios", show.DefaultBudget), newPhone(t, "android", show.DefaultBudget), newPhone(t, "android", show.DefaultBudget)
	p := pileOf(t, a, b, c)
	var ofA, ofC []string
	for i := range 6 {
		ofA = append(ofA, fmt.Sprintf("A%d", i))
		ofC = append(ofC, fmt.Sprintf("C%d", i))
		a.lib.photo(ofA[i], i)
		c.lib.photo(ofC[i], 10+i)
	}
	a.lib.add("VA", "video", "video/mp4", 7, randomBytes(100_000, 3))
	ofA = append(ofA, "VA")
	a.share(p)
	c.share(p)
	waitTimeline(t, p, 13, b)
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	a.lib.mu.Lock()
	a.lib.gate = gate
	a.lib.mu.Unlock()

	est, err := b.show.Estimate(p.ID, member(a))
	if err != nil {
		t.Fatal(err)
	}
	if est.Files != 7 || est.Missing != 7 || est.Bytes <= 0 || est.Budget != show.DefaultBudget || est.Free == nil || *est.Free <= 0 || est.OverBudget {
		t.Fatalf("estimate before loading from a: %+v", est)
	}
	if est, err := b.show.Estimate(p.ID, nil); err != nil || est.Files != 13 || est.Missing != 13 {
		t.Fatalf("estimate before loading everything: %+v, %v", est, err)
	}
	if est, err := (newPhone(t, "ios", 1).show.Estimate(p.ID, nil)); err == nil {
		t.Fatalf("estimate for a pile of another device: %+v", est)
	}

	list, err := b.show.Pin(p.ID, member(a))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Member != a.Author() || list[0].Name != "Аня" || list[0].Platform != "ios" || counts(list[0]) != [3]int{7, 0, 0} {
		t.Fatalf("progress right after pinning: %+v", list)
	}
	gate <- struct{}{}
	gate <- struct{}{}
	eventually(t, "two files of a loaded", func() bool { return counts(progressOf(t, b, p.ID)["Аня"]) == [3]int{7, 2, 0} })
	release()
	eventually(t, "all files of a loaded", func() bool { return counts(progressOf(t, b, p.ID)["Аня"]) == [3]int{7, 7, 0} })
	for _, l := range ofA {
		if !b.cached(p, l) {
			t.Fatalf("T2 of %s is not loaded", l)
		}
	}
	for _, l := range ofC {
		if b.cached(p, l) {
			t.Fatalf("T2 of %s is loaded by «load all from a»", l)
		}
	}
	if est, err := b.show.Estimate(p.ID, member(a)); err != nil || est.Files != 7 || est.Missing != 0 || est.Bytes != 0 {
		t.Fatalf("estimate after loading from a: %+v, %v", est, err)
	}

	if _, err := b.show.Pin(p.ID, nil); err != nil {
		t.Fatal(err)
	}
	eventually(t, "everything loaded", func() bool {
		got := progressOf(t, b, p.ID)
		return len(got) == 2 && counts(got["Аня"]) == [3]int{7, 7, 0} && counts(got["участник 1"]) == [3]int{6, 6, 0}
	})
	for _, l := range ofC {
		if !b.cached(p, l) {
			t.Fatalf("T2 of %s is not loaded", l)
		}
	}
	if n := a.lib.read("VA"); n != 0 {
		t.Fatalf("original of the video read %d times", n)
	}
}

func TestPinnedFilesAreNotEvictedUntilUnpinned(t *testing.T) {
	size := int64(len(display("A00")))
	a, c := newPhone(t, "ios", show.DefaultBudget), newPhone(t, "ios", show.DefaultBudget)
	dir, lib := t.TempDir(), newLibrary()
	b := openPhone(t, dir, "android", lib, 5*size)
	p := pileOf(t, a, b, c)
	var ofA, ofC []string
	for i := range 10 {
		ofA = append(ofA, fmt.Sprintf("A%02d", i))
		a.lib.photo(ofA[i], i)
	}
	for i := range 30 {
		ofC = append(ofC, fmt.Sprintf("C%02d", i))
		c.lib.photo(ofC[i], 10+i)
	}
	a.share(p)
	c.share(p)
	waitTimeline(t, p, 40, b)

	if est, err := b.show.Estimate(p.ID, member(a)); err != nil || !est.OverBudget {
		t.Fatalf("estimate does not warn about the budget: %+v, %v", est, err)
	}
	if _, err := b.show.Pin(p.ID, member(a)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "files of a loaded", func() bool { return counts(progressOf(t, b, p.ID)["Аня"]) == [3]int{10, 10, 0} })

	b.show.Close()
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	b = openPhone(t, dir, "android", lib, 5*size)
	eventually(t, "b connects to c", func() bool { return b.Online(p.ID)[c.Author()] })
	rx := newReceiver(t)
	must(t)(b.show.Start(p.ID, b.tv(rx), b.asset(p, "C00").Meta.AssetID))
	cached := func(want map[string]bool) func() bool {
		return func() bool {
			for l, w := range want {
				if b.cached(p, l) != w {
					return false
				}
			}
			return true
		}
	}
	want := map[string]bool{}
	for _, l := range ofA {
		want[l] = true
	}
	for i, l := range ofC {
		want[l] = i <= 20
	}
	eventually(t, "window cached, pinned kept", cached(want))
	must(t)(b.show.Goto(b.asset(p, "C25").Meta.AssetID))
	for i, l := range ofC {
		want[l] = i >= 25
	}
	eventually(t, "unpinned files evicted, pinned kept over budget", cached(want))

	list, err := b.show.Unpin(p.ID, member(a))
	if err != nil || len(list) != 0 {
		t.Fatalf("progress after unpinning: %+v, %v", list, err)
	}
	for _, l := range ofA {
		want[l] = false
	}
	eventually(t, "files of a evicted after unpinning", cached(want))
}

func TestPermanentlyFailingFileDoesNotBlockItsSource(t *testing.T) {
	a, b := newPhone(t, "ios", show.DefaultBudget), newPhone(t, "android", show.DefaultBudget)
	p := pileOf(t, a, b)
	var locals []string
	for i := range 30 {
		locals = append(locals, fmt.Sprintf("A%02d", i))
		a.lib.photo(locals[i], i)
	}
	a.lib.broken["A05"] = true
	a.share(p)
	waitTimeline(t, p, 30, b)
	cachedAll := func(list ...string) func() bool {
		return func() bool {
			for _, l := range list {
				if l != "A05" && !b.cached(p, l) {
					return false
				}
			}
			return true
		}
	}

	rx := newReceiver(t)
	must(t)(b.show.Start(p.ID, b.tv(rx), nil))
	eventually(t, "window cached except the failing file", cachedAll(locals[:21]...))
	must(t)(b.show.Goto(b.asset(p, "A03").Meta.AssetID))
	eventually(t, "new files of the window cached", cachedAll(locals[21:24]...))
	if st := must(t)(b.show.State()); len(st.Waiting) != 0 || len(st.Hints) != 0 {
		t.Fatalf("a working source is reported: waiting %d, hints %+v", len(st.Waiting), st.Hints)
	}
	if _, err := b.show.Pin(p.ID, nil); err != nil {
		t.Fatal(err)
	}
	eventually(t, "everything else loaded, failing file counted", func() bool {
		got := progressOf(t, b, p.ID)["Аня"]
		return counts(got) == [3]int{30, 29, 1} && got.Reason == ""
	})
}

func TestVideoOfUnavailableSourceShowsPosterAndShowGoesOn(t *testing.T) {
	a, b := newPhone(t, "ios", show.DefaultBudget), newPhone(t, "android", show.DefaultBudget)
	p := pileOf(t, a, b)
	original := randomBytes(50_000, 4)
	a.lib.photo("A0", 0)
	a.lib.add("V", "video", "video/mp4", 1, original)
	a.lib.photo("A2", 2)
	a.share(p)
	waitTimeline(t, p, 3, b)
	rx := newReceiver(t)
	must(t)(b.show.Start(p.ID, b.tv(rx), b.asset(p, "V").Meta.AssetID))
	video := rx.waitLoad(t, 1)
	if video.contentType != "video/mp4" || !strings.HasSuffix(video.url, "/3") {
		t.Fatalf("video loaded as %+v", video)
	}
	eventually(t, "poster and next file cached", func() bool { return b.cached(p, "V") && b.cached(p, "A2") })

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if resp, _ := get(t, video.url); resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("video of a closed source: status %d", resp.StatusCode)
	}
	hint := []show.Hint{{Member: a.Author(), Name: "Аня", Platform: "ios", Reason: show.Unreachable}}
	unavailable := func() bool {
		st := must(t)(b.show.State())
		return st.VideoUnavailable && !st.Playing && current(st) == "V" && reflect.DeepEqual(st.Hints, hint)
	}
	eventually(t, "video unavailable with a hint", unavailable)
	poster := func(n int) {
		t.Helper()
		l := rx.waitLoad(t, n)
		if _, body := get(t, l.url); l.contentType != "image/jpeg" || string(body) != display("V") {
			t.Fatalf("screen got %+v with %q instead of the poster", l, body)
		}
	}
	poster(2)
	ui := httptest.NewServer(httpapi.WithNode("ui", b.Node, b.show))
	t.Cleanup(ui.Close)
	if _, body := get(t, ui.URL+"/show", "Authorization", "Bearer ui"); !strings.Contains(string(body), `"video_unavailable":true`) {
		t.Fatalf("show state in the UI API: %s", body)
	}

	st := must(t)(b.show.Next())
	if current(st) != "A2" || st.VideoUnavailable {
		t.Fatalf("next after an unavailable video: %+v", st)
	}
	if _, body := get(t, rx.waitLoad(t, 3).url); string(body) != display("A2") {
		t.Fatalf("screen got %q", body)
	}
	must(t)(b.show.Prev())
	if !unavailable() {
		t.Fatalf("back on the video of an offline source: %+v", must(t)(b.show.State()))
	}
	poster(4)
	if st := must(t)(b.show.Play()); !st.VideoUnavailable {
		t.Fatalf("play while the source is offline: %+v", st)
	}

	openPhone(t, a.dir, "ios", a.lib, show.DefaultBudget)
	eventually(t, "play after the source is back", func() bool { return !must(t)(b.show.Play()).VideoUnavailable })
	again := rx.waitLoad(t, 5)
	if resp, body := get(t, again.url); again.url != video.url || resp.StatusCode != http.StatusOK || string(body) != string(original) {
		t.Fatalf("video after the source is back: %s, status %d, %d bytes", again.url, resp.StatusCode, len(body))
	}
}

func member(ph *phone) []byte {
	a := ph.Author()
	return a[:]
}
