package show_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/httpapi"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

func videoLoads(r *receiver) []load {
	var out []load
	for _, l := range r.loaded() {
		if strings.HasPrefix(l.contentType, "video/") {
			out = append(out, l)
		}
	}
	return out
}

func posterShown(t *testing.T, r *receiver, local string, n int) {
	t.Helper()
	eventually(t, "poster on the screen", func() bool {
		shown := r.displayed()
		return len(shown) >= n && shown[n-1] == display(local)
	})
}

func logged(ph *phone, text string) bool {
	return slices.ContainsFunc(ph.Diag().Events(), func(e diag.Event) bool { return strings.Contains(e.Message, text) })
}

func uiState(t *testing.T, ph *phone) string {
	t.Helper()
	ui := httptest.NewServer(httpapi.WithNode("ui", ph.Node, ph.show))
	t.Cleanup(ui.Close)
	_, body := get(t, ui.URL+"/show", "Authorization", "Bearer ui")
	return string(body)
}

func TestVideoTheScreenCannotPlayGetsPosterAfterOneRetry(t *testing.T) {
	a, b := newPhone(t, "android", node.DefaultBudget), newPhone(t, "ios", node.DefaultBudget)
	p := pileOf(t, a, b)
	b.lib.add("V", "video", "video/quicktime", 0, randomBytes(1<<20, 7))
	b.lib.codec("V", "hvc1")
	b.share(p)
	waitTimeline(t, p, 1, a)
	rx := newReceiver(t)
	rx.fetching()
	rx.rejecting("video/")
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))

	posterShown(t, rx, "V", 1)
	noMoreLoads(t, rx)
	if n := len(videoLoads(rx)); n != 2 {
		t.Fatalf("%d LOADs of the video, want 2", n)
	}
	st := must(t)(a.show.State())
	if !st.VideoUnavailable || st.VideoReason != show.Unsupported || st.Playing || st.Error != nil || len(st.Hints) != 0 {
		t.Fatalf("state after the screen failed the video twice: %+v", st)
	}
	if st.Current.Meta.Codec != "hvc1" {
		t.Fatalf("codec in the show state: %q", st.Current.Meta.Codec)
	}
	if body := uiState(t, a); !strings.Contains(body, `"video_reason":"unsupported"`) || !strings.Contains(body, `"codec":"hvc1"`) {
		t.Fatalf("show state in the UI API: %s", body)
	}

	must(t)(a.show.Play())
	posterShown(t, rx, "V", 2)
	noMoreLoads(t, rx)
	if n := len(videoLoads(rx)); n != 4 {
		t.Fatalf("%d LOADs of the video after play, want 4", n)
	}
	if st := must(t)(a.show.State()); st.VideoReason != show.Unsupported {
		t.Fatalf("state after a manual retry: %+v", st)
	}
}

func TestVideoTheOwnerCannotReadGetsPosterAndReason(t *testing.T) {
	for _, c := range []struct {
		name, native, reason string
		own                  bool
	}{
		{"member, only in iCloud", "not_local: the original is in iCloud", show.NotLocal, false},
		{"member, unreadable", "permission denied for video:42", show.Unreadable, false},
		{"host, only in iCloud", "not_local: the original is in iCloud", show.NotLocal, true},
		{"host, unreadable", "permission denied for video:42", show.Unreadable, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, b := newPhone(t, "android", node.DefaultBudget), newPhone(t, "ios", node.DefaultBudget)
			p := pileOf(t, a, b)
			owner := b
			if c.own {
				owner = a
			}
			owner.lib.add("V", "video", "video/quicktime", 0, randomBytes(1<<20, 7))
			owner.lib.lose("V", c.native)
			owner.share(p)
			waitTimeline(t, p, 1, a)
			rx := newReceiver(t)
			rx.fetching()
			must(t)(a.show.Start(p.ID, a.tv(rx), nil))

			posterShown(t, rx, "V", 1)
			noMoreLoads(t, rx)
			if n := len(videoLoads(rx)); n != 1 {
				t.Fatalf("%d LOADs of the video, want 1", n)
			}
			st := must(t)(a.show.State())
			if !st.VideoUnavailable || st.VideoReason != c.reason || st.Playing || st.Error != nil || len(st.Hints) != 0 {
				t.Fatalf("state: %+v", st)
			}
			if !logged(a, c.native) {
				t.Fatalf("the owner's reason is not in the host journal: %+v", a.Diag().Events())
			}
			if body := uiState(t, a); !strings.Contains(body, `"video_reason":"`+c.reason+`"`) {
				t.Fatalf("show state in the UI API: %s", body)
			}

			must(t)(a.show.Play())
			posterShown(t, rx, "V", 2)
			noMoreLoads(t, rx)
			if n := len(videoLoads(rx)); n != 2 {
				t.Fatalf("%d LOADs of the video after play, want 2", n)
			}
		})
	}
}

func resumedAt(t *testing.T, l load, from float64, since time.Time) {
	t.Helper()
	if !strings.HasPrefix(l.contentType, "video/") || l.at < from || l.at > from+time.Since(since).Seconds() {
		t.Fatalf("LOAD %+v, want the video from %v s", l, from)
	}
}

func TestVideoResumesAfterReconnectAndRetry(t *testing.T) {
	a := newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a)
	a.lib.add("V", "video", "video/mp4", 0, randomBytes(100_000, 5))
	a.share(p)
	rx := newReceiver(t)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	if l := rx.waitLoad(t, 1); l.at != 0 {
		t.Fatalf("first LOAD from %v s", l.at)
	}

	at := time.Now()
	rx.progress(95)
	rx.drop()
	resumedAt(t, rx.waitLoad(t, 2), 95, at)
	eventually(t, "reconnected", func() bool { return rendererError(t, a.show) == "" })

	at = time.Now()
	rx.seek(40)
	rx.push("IDLE", "ERROR")
	resumedAt(t, rx.waitLoad(t, 3), 40, at)
	eventually(t, "error cleared", func() bool { return rendererError(t, a.show) == "" })
}

func TestVideoResumesAfterManualPlay(t *testing.T) {
	a := newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a)
	a.lib.add("V", "video", "video/mp4", 0, randomBytes(100_000, 5))
	a.share(p)
	rx := newReceiver(t)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	video := rx.waitLoad(t, 1)

	at := time.Now()
	rx.seek(30)
	rx.push("PAUSED", "")
	eventually(t, "paused", func() bool { return !must(t)(a.show.State()).Playing })
	a.lib.lose("V", "not_local: the original is in iCloud")
	if resp, _ := get(t, video.url); resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("video the owner cannot read: status %d", resp.StatusCode)
	}
	if l := rx.waitLoad(t, 2); l.contentType != "image/jpeg" {
		t.Fatalf("screen got %+v instead of the poster", l)
	}
	a.lib.lose("V", "")
	must(t)(a.show.Play())
	resumedAt(t, rx.waitLoad(t, 3), 30, at)
}

func TestVideoIsRetriedOnceWhenItsOwnerIsBack(t *testing.T) {
	a, b := newPhone(t, "android", node.DefaultBudget), newPhone(t, "ios", node.DefaultBudget)
	p := pileOf(t, a, b)
	b.lib.add("V", "video", "video/mp4", 0, randomBytes(100_000, 5))
	b.share(p)
	waitTimeline(t, p, 1, a)
	eventually(t, "a connects to b", func() bool { return a.Online(p.ID)[b.Author()] })
	rx := newReceiver(t)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	video := rx.waitLoad(t, 1)

	at := time.Now()
	rx.progress(20)
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "b is offline", func() bool { return !a.Online(p.ID)[b.Author()] })
	time.Sleep(1500 * time.Millisecond)
	if resp, _ := get(t, video.url); resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("video of a closed owner: status %d", resp.StatusCode)
	}
	if l := rx.waitLoad(t, 2); l.contentType != "image/jpeg" {
		t.Fatalf("screen got %+v instead of the poster", l)
	}
	if st := must(t)(a.show.State()); st.VideoReason != show.Unreachable || len(st.Hints) != 1 {
		t.Fatalf("state while the owner is away: %+v", st)
	}

	openPhone(t, b.dir, "ios", b.lib, node.DefaultBudget)
	resumedAt(t, rx.waitLoad(t, 3), 20, at)
	if cmds := rx.commanded(); len(cmds) != 0 {
		t.Fatalf("receiver commands %v", cmds)
	}
}

func TestFinishedVideoShowsPosterAndPlaysFromStart(t *testing.T) {
	a := newPhone(t, "android", node.DefaultBudget)
	p := pileOf(t, a)
	a.lib.add("V", "video", "video/mp4", 0, randomBytes(100_000, 5))
	a.share(p)
	rx := newReceiver(t)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	rx.waitLoad(t, 1)

	rx.progress(12)
	rx.push("IDLE", "FINISHED")
	poster := rx.waitLoad(t, 2)
	if _, body := get(t, poster.url); poster.contentType != "image/jpeg" || string(body) != display("V") {
		t.Fatalf("screen got %+v with %q after the video ended", poster, body)
	}
	if st := must(t)(a.show.State()); st.Playing || st.VideoUnavailable || st.Error != nil {
		t.Fatalf("state after the video ended: %+v", st)
	}

	must(t)(a.show.Play())
	if l := rx.waitLoad(t, 3); l.contentType != "video/mp4" || l.at != 0 {
		t.Fatalf("play after the end loaded %+v", l)
	}
	eventually(t, "playing again", func() bool { return must(t)(a.show.State()).Playing })
	if code := rendererError(t, a.show); code != "" {
		t.Fatalf("renderer error %s", code)
	}
	if cmds := rx.commanded(); len(cmds) != 0 {
		t.Fatalf("receiver commands %v", cmds)
	}
}

func TestOwnerOfVideoOnTheScreenReportsServing(t *testing.T) {
	a, b := newPhone(t, "android", node.DefaultBudget), newPhone(t, "ios", node.DefaultBudget)
	p := pileOf(t, a, b)
	b.lib.add("V", "video", "video/mp4", 0, randomBytes(100_000, 5))
	b.share(p)
	waitTimeline(t, p, 1, a)
	eventually(t, "a connects to b", func() bool { return a.Online(p.ID)[b.Author()] })
	serving := func(ph *phone) bool {
		t.Helper()
		ui := httptest.NewServer(httpapi.WithNode("ui", ph.Node, ph.show))
		defer ui.Close()
		resp, body := get(t, ui.URL+"/serving", "Authorization", "Bearer ui")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /serving: status %d", resp.StatusCode)
		}
		return string(body) == `{"serving":true}`+"\n"
	}
	if serving(a) || serving(b) {
		t.Fatal("serving before the show")
	}

	rx := newReceiver(t)
	rx.fetching()
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	eventually(t, "owner of the video on the screen is serving", func() bool { return serving(b) })
	if serving(a) {
		t.Fatal("host that relays the video is serving")
	}
	eventually(t, "owner stops serving after the screen read the video", func() bool { return !serving(b) })
}
