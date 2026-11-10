package show_test

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/httpapi"
	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

func twoPhones(t *testing.T) (*phone, *phone, node.Pile) {
	t.Helper()
	a, b := newPhone(t, "ios", node.DefaultBudget), newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a, b)
	a.lib.photo("A1", 0)
	a.lib.photo("A2", 2)
	b.lib.photo("B1", 1)
	b.lib.photo("B2", 3)
	a.share(p)
	b.share(p)
	waitTimeline(t, p, 4, a, b)
	return a, b, p
}

func TestEitherPhoneHostsAShowOnCast(t *testing.T) {
	a, b, p := twoPhones(t)
	order := []string{"A1", "B1", "A2", "B2"}
	for _, host := range []*phone{a, b} {
		rx := newReceiver(t)
		st := must(t)(host.show.Start(p.ID, host.tv(rx), nil))
		if !bytes.Equal(st.Pile, p.ID) || st.Screen.Kind != "cast" || st.Screen.Name != "Гостиная" || st.Total != 4 {
			t.Fatalf("started show %+v", st)
		}
		var urls []string
		for i, local := range order {
			if i > 0 {
				st = must(t)(host.show.Next())
			}
			if st.Index != i || current(st) != local {
				t.Fatalf("step %d: index %d, current %s; want %d, %s", i, st.Index, current(st), i, local)
			}
			l := rx.waitLoad(t, i+1)
			if l.contentType != "image/jpeg" {
				t.Fatalf("%s loaded as %s", local, l.contentType)
			}
			resp, body := get(t, l.url)
			if resp.StatusCode != http.StatusOK || string(body) != display(local) || resp.Header.Get("Content-Type") != "image/jpeg" {
				t.Fatalf("%s: renderer got status %d, %q, %s", local, resp.StatusCode, body, resp.Header.Get("Content-Type"))
			}
			urls = append(urls, l.url)
		}
		if st := must(t)(host.show.Next()); st.Index != 3 || current(st) != "B2" {
			t.Fatalf("next after the last file: %+v", st)
		}
		if st := must(t)(host.show.Prev()); current(st) != "A2" {
			t.Fatalf("prev: %s", current(st))
		}
		if l := rx.waitLoad(t, 5); l.url != urls[2] {
			t.Fatalf("prev loaded %s, want %s", l.url, urls[2])
		}
		if st := must(t)(host.show.Goto(host.asset(p, "A1").Meta.AssetID)); st.Index != 0 || current(st) != "A1" {
			t.Fatalf("goto: %+v", st)
		}
		if l := rx.waitLoad(t, 6); l.url != urls[0] {
			t.Fatalf("goto loaded %s, want %s", l.url, urls[0])
		}
		if st := must(t)(host.show.Prev()); st.Index != 0 {
			t.Fatalf("prev before the first file: index %d", st.Index)
		}

		host.show.Stop()
		if cmds := rx.commanded(); len(cmds) == 0 || cmds[len(cmds)-1] != "STOP" {
			t.Fatalf("receiver commands after stop: %v", cmds)
		}
		if _, err := host.show.State(); !errors.Is(err, show.ErrNoShow) {
			t.Fatalf("state after stop: %v", err)
		}
		if _, err := http.Get(urls[0]); err == nil {
			t.Fatal("renderer media is still served after stop")
		}
	}
}

func TestShowStartsFromChosenFile(t *testing.T) {
	a, _, p := twoPhones(t)
	rx := newReceiver(t)
	st := must(t)(a.show.Start(p.ID, a.tv(rx), a.asset(p, "A2").Meta.AssetID))
	if st.Index != 2 || current(st) != "A2" {
		t.Fatalf("started at %d %s", st.Index, current(st))
	}
	if _, body := get(t, rx.waitLoad(t, 1).url); string(body) != display("A2") {
		t.Fatalf("renderer got %q", body)
	}
	if _, err := a.show.Start(p.ID, a.tv(rx), []byte("no such file")); !errors.Is(err, node.ErrUnknownAsset) {
		t.Fatalf("start from unknown file: %v", err)
	}
	if _, err := a.show.Start([]byte("no such pile"), a.tv(rx), nil); !errors.Is(err, node.ErrUnknownPile) {
		t.Fatalf("start in unknown pile: %v", err)
	}
	if _, err := a.show.Start(p.ID, "cast:nowhere", nil); !errors.Is(err, show.ErrUnknownScreen) {
		t.Fatalf("start on unknown screen: %v", err)
	}
	empty, err := a.Create("Пусто", "Аня")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.show.Start(empty.ID, a.tv(rx), nil); !errors.Is(err, show.ErrEmpty) {
		t.Fatalf("start in empty pile: %v", err)
	}
	if _, err := a.show.Goto(a.asset(p, "A1").Meta.AssetID); !errors.Is(err, show.ErrNoShow) {
		t.Fatalf("goto without show: %v", err)
	}
}

func TestRendererTokenOpensOnlyShowMedia(t *testing.T) {
	_, b, p := twoPhones(t)
	rx := newReceiver(t)
	must(t)(b.show.Start(p.ID, b.tv(rx), nil))
	photo, err := url.Parse(rx.waitLoad(t, 1).url)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(photo.Path, "/")
	if len(parts) != 6 || parts[1] != "show" {
		t.Fatalf("renderer url %s", photo)
	}
	token, asset, revision := parts[2], parts[3], parts[4]
	root := "http://" + photo.Host
	for name, path := range map[string]string{
		"wrong token":     "/show/" + strings.Repeat("A", len(token)) + "/" + asset + "/" + revision + "/2",
		"no token":        "/show/" + asset + "/" + revision + "/2",
		"photo original":  "/show/" + token + "/" + asset + "/" + revision + "/3",
		"thumbnail":       "/show/" + token + "/" + asset + "/" + revision + "/1",
		"other revision":  "/show/" + token + "/" + asset + "/00/2",
		"health":          "/health",
		"piles":           "/piles",
		"pile blob route": fmt.Sprintf("/piles/%x/blob/%s/%s/2", p.ID, asset, revision),
		"peer blob route": "/blob/" + asset + "/" + revision + "/2",
		"vv":              "/vv",
	} {
		for _, auth := range []string{"", "Bearer " + token} {
			resp, body := get(t, root+path, "Authorization", auth)
			if resp.StatusCode < 400 || resp.StatusCode >= 500 || bytes.Contains(body, []byte("T2 of")) {
				t.Errorf("%s (auth %q): status %d, body %q", name, auth, resp.StatusCode, body)
			}
		}
	}

	ui := httptest.NewServer(httpapi.WithNode("ui-secret", b.Node, b.show))
	t.Cleanup(ui.Close)
	for _, path := range []string{"/show", "/piles", "/screens", "/diag", fmt.Sprintf("/piles/%x/blob/%s/%s/2", p.ID, asset, revision)} {
		if resp, _ := get(t, ui.URL+path, "Authorization", "Bearer "+token); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("UI %s with renderer token: status %d", path, resp.StatusCode)
		}
	}

	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/blob/%s/%s/2?token=%s", peerAddr(t, b, p), asset, revision, token))
	if err == nil {
		resp.Body.Close()
		t.Fatalf("peer port answered plain HTTP with status %d", resp.StatusCode)
	}

	b.show.Stop()
	must(t)(b.show.Start(p.ID, b.tv(rx), nil))
	again, err := url.Parse(rx.waitLoad(t, 2).url)
	if err != nil {
		t.Fatal(err)
	}
	newToken := strings.Split(again.Path, "/")[2]
	if newToken == token {
		t.Fatal("new show reuses the renderer token")
	}
	if resp, _ := get(t, "http://"+again.Host+strings.Replace(again.Path, newToken, token, 1)); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("token of a stopped show: status %d", resp.StatusCode)
	}
	if resp, body := get(t, again.String()); resp.StatusCode != http.StatusOK || string(body) != display("A1") {
		t.Fatalf("new token: status %d, %q", resp.StatusCode, body)
	}
}

func peerAddr(t *testing.T, ph *phone, p node.Pile) string {
	t.Helper()
	s, err := ph.Invite(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := invite.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return inv.Addrs[0].String()
}

func randomBytes(n int, seed uint64) []byte {
	rng := rand.New(rand.NewPCG(seed, seed))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Uint32())
	}
	return b
}

func TestVideoStreamsFromSourceThroughHostWithRange(t *testing.T) {
	a, b := newPhone(t, "ios", node.DefaultBudget), newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a, b)
	theirs, own := randomBytes(1<<20+333, 1), randomBytes(200_000, 2)
	a.lib.add("VID_A", "video", "video/quicktime", 0, theirs)
	b.lib.photo("IMG_B", 1)
	b.lib.add("VID_B", "video", "video/mp4", 2, own)
	a.share(p)
	b.share(p)
	waitTimeline(t, p, 3, a, b)
	rx := newReceiver(t)
	must(t)(b.show.Start(p.ID, b.tv(rx), nil))

	check := func(l load, original []byte, mime string) {
		t.Helper()
		if l.contentType != mime {
			t.Fatalf("video loaded as %s, want %s", l.contentType, mime)
		}
		resp, body := get(t, l.url)
		if resp.StatusCode != http.StatusOK || !bytes.Equal(body, original) || resp.Header.Get("Content-Type") != mime {
			t.Fatalf("full video: status %d, %d bytes, %s", resp.StatusCode, len(body), resp.Header.Get("Content-Type"))
		}
		size := len(original)
		for _, c := range []struct {
			header     string
			start, end int
		}{{"bytes=0-0", 0, 1}, {"bytes=100000-150000", 100000, 150001}, {"bytes=-10", size - 10, size}} {
			resp, body := get(t, l.url, "Range", c.header)
			want := fmt.Sprintf("bytes %d-%d/%d", c.start, c.end-1, size)
			if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Range") != want || !bytes.Equal(body, original[c.start:c.end]) {
				t.Fatalf("%s: status %d, Content-Range %q, %d bytes", c.header, resp.StatusCode, resp.Header.Get("Content-Range"), len(body))
			}
		}
	}
	l := rx.waitLoad(t, 1)
	if u, _ := url.Parse(l.url); u.Host == peerAddr(t, a, p) {
		t.Fatalf("renderer url %s points at the source", l.url)
	}
	check(l, theirs, "video/quicktime")
	eventually(t, "playing", func() bool { return must(t)(b.show.State()).Playing })

	must(t)(b.show.Pause())
	eventually(t, "paused", func() bool { return !must(t)(b.show.State()).Playing })
	must(t)(b.show.Play())
	eventually(t, "playing again", func() bool { return must(t)(b.show.State()).Playing })
	if cmds := rx.commanded(); len(cmds) != 2 || cmds[0] != "PAUSE" || cmds[1] != "PLAY" {
		t.Fatalf("receiver commands %v", cmds)
	}

	must(t)(b.show.Next())
	if l := rx.waitLoad(t, 2); l.contentType != "image/jpeg" {
		t.Fatalf("photo loaded as %s", l.contentType)
	}
	if _, err := b.show.Pause(); !errors.Is(err, show.ErrNotVideo) {
		t.Fatalf("pause on a photo: %v", err)
	}
	if st := must(t)(b.show.State()); st.Playing {
		t.Fatal("photo is playing")
	}
	must(t)(b.show.Next())
	check(rx.waitLoad(t, 3), own, "video/mp4")
}

func TestNextWindowIsCachedBeforeRendererAsks(t *testing.T) {
	a, b := newPhone(t, "ios", node.DefaultBudget), newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a, b)
	var locals []string
	for i := range 30 {
		locals = append(locals, fmt.Sprintf("P%02d", i))
		a.lib.photo(locals[i], i)
	}
	a.share(p)
	waitTimeline(t, p, 30, b)
	rx := newReceiver(t)
	must(t)(b.show.Start(p.ID, b.tv(rx), nil))

	cachedUpTo := func(last int) func() bool {
		return func() bool {
			for _, l := range locals[:last+1] {
				if !b.cached(p, l) {
					return false
				}
			}
			return true
		}
	}
	eventually(t, "cursor and 20 next files cached", cachedUpTo(20))
	time.Sleep(1500 * time.Millisecond)
	for _, l := range locals[21:] {
		if b.cached(p, l) || a.lib.displayed(l) != 0 {
			t.Fatalf("%s beyond the window was loaded", l)
		}
	}

	must(t)(b.show.Next())
	eventually(t, "window follows the cursor", cachedUpTo(21))
	if _, body := get(t, rx.waitLoad(t, 2).url); string(body) != display("P01") {
		t.Fatalf("renderer got %q", body)
	}
	must(t)(b.show.Goto(b.asset(p, "P28").Meta.AssetID))
	eventually(t, "window after a jump", func() bool {
		for _, l := range locals[23:] {
			if !b.cached(p, l) {
				return false
			}
		}
		return true
	})
	if _, body := get(t, rx.waitLoad(t, 3).url); string(body) != display("P28") {
		t.Fatalf("renderer got %q", body)
	}
	for i, l := range locals {
		want := 1
		if i == 22 {
			want = 0
		}
		if n := a.lib.displayed(l); n != want {
			t.Fatalf("%s rendered on the source %d times, want %d", l, n, want)
		}
	}
}

func TestCacheEvictsEarliestFilesOverBudget(t *testing.T) {
	size := int64(len(display("P00")))
	a, b := newPhone(t, "ios", node.DefaultBudget), newPhone(t, "android", 25*size)
	p := pileOf(t, a, b)
	var locals []string
	for i := range 30 {
		locals = append(locals, fmt.Sprintf("P%02d", i))
		a.lib.photo(locals[i], i)
	}
	a.share(p)
	waitTimeline(t, p, 30, b)
	rx := newReceiver(t)
	must(t)(b.show.Start(p.ID, b.tv(rx), nil))
	cachedExactly := func(from, to int) func() bool {
		return func() bool {
			for i, l := range locals {
				if b.cached(p, l) != (i >= from && i <= to) {
					return false
				}
			}
			return true
		}
	}
	eventually(t, "first window cached", cachedExactly(0, 20))
	must(t)(b.show.Goto(b.asset(p, "P10").Meta.AssetID))
	eventually(t, "earliest files evicted down to the budget", cachedExactly(5, 29))
	for _, l := range locals[:5] {
		x := b.asset(p, l)
		if _, ok := b.Cached(p.ID, x.Meta.AssetID, x.Meta.Revision, node.T1); !ok {
			t.Fatalf("T1 of %s evicted", l)
		}
	}
}

func TestFiveFilesBeforeCursorAreCachedAndKeptOverBudget(t *testing.T) {
	size := int64(len(display("P00")))
	a, b := newPhone(t, "ios", node.DefaultBudget), newPhone(t, "android", 21*size)
	p := pileOf(t, a, b)
	var locals []string
	for i := range 40 {
		locals = append(locals, fmt.Sprintf("P%02d", i))
		a.lib.photo(locals[i], i)
	}
	a.share(p)
	waitTimeline(t, p, 40, b)
	rx := newReceiver(t)
	must(t)(b.show.Start(p.ID, b.tv(rx), b.asset(p, "P10").Meta.AssetID))
	window := func() bool {
		for i, l := range locals {
			if b.cached(p, l) != (i >= 5 && i <= 30) {
				return false
			}
		}
		return true
	}
	eventually(t, "cursor, 20 next and 5 previous files cached", window)
	time.Sleep(1500 * time.Millisecond)
	if !window() {
		t.Fatal("files of the window are evicted")
	}
	for i, l := range locals {
		want := 0
		if i >= 5 && i <= 30 {
			want = 1
		}
		if n := a.lib.displayed(l); n != want {
			t.Fatalf("%s rendered on the source %d times, want %d", l, n, want)
		}
	}
}

func TestFilesOfOfflineSourceWaitWhileOthersLoad(t *testing.T) {
	a, b, c := newPhone(t, "ios", node.DefaultBudget), newPhone(t, "android", node.DefaultBudget), newPhone(t, "ios", node.DefaultBudget)
	p := pileOf(t, a, b, c)
	var ofA, ofC []string
	for i := range 6 {
		ofA = append(ofA, fmt.Sprintf("A%d", i))
		ofC = append(ofC, fmt.Sprintf("C%d", i))
		a.lib.photo(ofA[i], 2*i)
		c.lib.photo(ofC[i], 2*i+1)
	}
	a.share(p)
	c.share(p)
	waitTimeline(t, p, 12, b)
	eventually(t, "b connects to c", func() bool { return b.Online(p.ID)[c.Author()] })
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	rx := newReceiver(t)
	must(t)(b.show.Start(p.ID, b.tv(rx), nil))
	waiting := func(want ...string) func() bool {
		return func() bool {
			st := must(t)(b.show.State())
			var got []string
			for _, w := range st.Waiting {
				got = append(got, w.Meta.OriginalName)
			}
			return fmt.Sprint(got) == fmt.Sprint(want)
		}
	}
	eventually(t, "files of c wait", waiting(ofC...))
	eventually(t, "files of a are cached", func() bool {
		for _, l := range ofA {
			if !b.cached(p, l) {
				return false
			}
		}
		return true
	})
	st := must(t)(b.show.Next())
	if current(st) != "C0" || len(st.Waiting) != len(ofC) {
		t.Fatalf("cursor on a waiting file: %+v", st)
	}

	openPhone(t, c.dir, "ios", c.lib, node.DefaultBudget)
	eventually(t, "files of c are cached after c is back", func() bool {
		for _, l := range ofC {
			if !b.cached(p, l) {
				return false
			}
		}
		return true
	})
	eventually(t, "nothing waits after c is back", waiting())
}

func TestCastErrorsAndDisconnectAreReported(t *testing.T) {
	a := newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a)
	for i := range 4 {
		a.lib.photo(fmt.Sprintf("P%d", i), i)
	}
	a.lib.add("V", "video", "video/mp4", 5, []byte("video"))
	a.share(p)
	ui := httptest.NewServer(httpapi.WithNode("ui", a.Node, a.show))
	t.Cleanup(ui.Close)
	logged := func(text string) func() bool {
		return func() bool {
			_, body := get(t, ui.URL+"/diag", "Authorization", "Bearer ui")
			return bytes.Contains(body, []byte(`"kind":"renderer"`)) && bytes.Contains(body, []byte(text))
		}
	}
	fault := func(code string) func() bool {
		return func() bool {
			st := must(t)(a.show.State())
			if code == "" {
				return st.Error == nil
			}
			return st.Error != nil && st.Error.Code == code && st.Error.Message != ""
		}
	}

	rx := newReceiver(t)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	rx.waitLoad(t, 1)
	eventually(t, "no error", fault(""))

	rx.failLoads(true)
	must(t)(a.show.Next())
	eventually(t, "LOAD_FAILED in state", fault("load_failed"))
	eventually(t, "LOAD_FAILED in diag", logged("LOAD_FAILED"))
	rx.failLoads(false)
	must(t)(a.show.Next())
	eventually(t, "error cleared by next load", fault(""))

	must(t)(a.show.Goto(a.asset(p, "V").Meta.AssetID))
	if l := rx.waitLoad(t, 4); l.contentType != "video/mp4" {
		t.Fatalf("video loaded as %s", l.contentType)
	}
	eventually(t, "video is playing", func() bool { return must(t)(a.show.State()).Playing })
	rx.push("IDLE", "ERROR")
	eventually(t, "playback error in state", fault("playback_failed"))
	eventually(t, "playback error in diag", logged("playback"))

	rx.drop()
	eventually(t, "disconnect in state", fault("disconnected"))
	eventually(t, "disconnect in diag", logged("connection"))
	if st := must(t)(a.show.Prev()); current(st) != "P3" || st.Error == nil || st.Error.Code != "disconnected" {
		t.Fatalf("after disconnect: %+v", st)
	}

	closed := httptest.NewServer(http.NotFoundHandler())
	closedAddr := strings.TrimPrefix(closed.URL, "http://")
	closed.Close()
	screen := a.found("_googlecast._tcp", "Кухня", closedAddr)
	if _, err := a.show.Start(p.ID, screen, nil); !errors.Is(err, show.ErrUnreachable) {
		t.Fatalf("start on unreachable screen: %v", err)
	}
	eventually(t, "connect failure in diag", logged("connect"))
}

func TestAppleTVNeedsIPhoneHost(t *testing.T) {
	a, b, p := twoPhones(t)
	screen := b.found("_airplay._tcp", "Спальня", "192.168.1.31:7000")
	var unavailable *show.UnavailableError
	if _, err := b.show.Start(p.ID, screen, nil); !errors.As(err, &unavailable) || unavailable.Reason != b.Screens()[0].Reason || unavailable.Reason == "" {
		t.Fatalf("android host on Apple TV: %v", err)
	}
	if _, err := b.show.State(); !errors.Is(err, show.ErrNoShow) {
		t.Fatalf("state after refused start: %v", err)
	}

	screen = a.found("_airplay._tcp", "Спальня", "192.168.1.31:7000")
	st := must(t)(a.show.Start(p.ID, screen, nil))
	if st.Screen.Kind != "airplay" || current(st) != "A1" {
		t.Fatalf("iPhone host on Apple TV: %+v", st)
	}
	if st := must(t)(a.show.Next()); current(st) != "B1" {
		t.Fatalf("next: %s", current(st))
	}
}

func TestFilesSharedDuringShowJoinIt(t *testing.T) {
	a, b, p := twoPhones(t)
	rx := newReceiver(t)
	must(t)(a.show.Start(p.ID, a.tv(rx), a.asset(p, "B2").Meta.AssetID))
	b.lib.photo("B0", -1)
	b.lib.photo("B3", 4)
	b.share(p)
	waitTimeline(t, p, 6, a)
	st := must(t)(a.show.Next())
	if st.Total != 6 || st.Index != 5 || current(st) != "B3" {
		t.Fatalf("next after new files: index %d of %d, current %s", st.Index, st.Total, current(st))
	}
	if _, body := get(t, rx.waitLoad(t, 2).url); string(body) != display("B3") {
		t.Fatalf("renderer got %q", body)
	}
}
