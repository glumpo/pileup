package show_test

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

func (ph *phone) shows(l load, p node.Pile, local string) bool {
	return strings.Contains(l.url, hex.EncodeToString(ph.asset(p, local).Meta.AssetID))
}

func TestNextReachesScreenWhileLoadHangs(t *testing.T) {
	a, _, p := twoPhones(t)
	rx := newReceiver(t)
	rx.holding(true)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	rx.received(t, 1, 5*time.Second)

	for range 3 {
		must(t)(a.show.Next())
	}
	if l := rx.received(t, 2, time.Second); !a.shows(l, p, "B2") {
		t.Fatalf("second LOAD %s, want B2", l.url)
	}
	time.Sleep(time.Second)
	if n := len(rx.loaded()); n != 2 {
		t.Fatalf("screen got %d LOADs for a series of presses, want the first and the last: %v", n, rx.loaded())
	}

	rx.holding(false)
	rx.waitLoad(t, 2)
	time.Sleep(100 * time.Millisecond)
	if st := must(t)(a.show.State()); st.Error != nil || current(st) != "B2" {
		t.Fatalf("state after the screen loaded the last file: %+v", st)
	}
	if logged(a, "LOAD_CANCELLED") {
		t.Fatalf("cancelled LOAD of a previous file is reported: %+v", a.Diag().Events())
	}
}

func TestStaleLoadDoesNotCountAgainstNextVideo(t *testing.T) {
	a := newPhone(t, "android", node.DefaultBudget)
	p, err := a.Create("Грузия", "Аня")
	if err != nil {
		t.Fatal(err)
	}
	a.lib.add("V1", "video", "video/mp4", 0, randomBytes(1<<16, 1))
	a.lib.add("V2", "video", "video/mp4", 1, randomBytes(1<<16, 2))
	a.share(p)
	waitTimeline(t, p, 2, a)
	rx := newReceiver(t)
	rx.holding(true)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	rx.received(t, 1, 5*time.Second)

	must(t)(a.show.Next())
	if l := rx.received(t, 2, time.Second); !a.shows(l, p, "V2") || l.at != 0 {
		t.Fatalf("LOAD after next: %+v", l)
	}
	rx.holding(false)
	rx.waitLoad(t, 2)
	time.Sleep(100 * time.Millisecond)
	if st := must(t)(a.show.State()); st.Error != nil || st.VideoUnavailable || !st.Playing {
		t.Fatalf("state after the second video loaded: %+v", st)
	}
	if logged(a, "LOAD_CANCELLED") {
		t.Fatalf("cancelled LOAD of the first video is reported: %+v", a.Diag().Events())
	}

	since := time.Now()
	rx.push("IDLE", "ERROR")
	l := rx.waitLoad(t, 3)
	if !a.shows(l, p, "V2") {
		t.Fatalf("retry after the first error of V2: %+v", l)
	}
	resumedAt(t, l, 0, since)
	if !logged(a, `(video/mp4, codec "")`) {
		t.Fatalf("playback error without the format of the file: %+v", a.Diag().Events())
	}
	time.Sleep(100 * time.Millisecond)
	if st := must(t)(a.show.State()); st.VideoUnavailable {
		t.Fatalf("one error of V2 gave the poster: %+v", st)
	}
}

func TestRestartWhileScreenIsAwayKeepsShow(t *testing.T) {
	show.Pauses(t, time.Hour, time.Hour)
	a, _, p := twoPhones(t)
	rx := newReceiver(t)
	screen := a.tv(rx)
	st := must(t)(a.show.Start(p.ID, screen, nil))
	rx.waitLoad(t, 1)
	must(t)(a.show.Next())
	rx.waitLoad(t, 2)

	rx.refusing(true)
	rx.drop()
	eventually(t, "reconnecting", func() bool { return rendererError(t, a.show) == "reconnecting" })
	a.lan.mu.Lock()
	h := a.lan.browsing["_googlecast._tcp"]
	a.lan.mu.Unlock()
	h.Lost("Гостиная")

	if st, err := a.show.Start(p.ID, screen, st.Current.Meta.AssetID); err != nil || current(st) != "B1" {
		t.Fatalf("restart while the screen is away: %+v, %v", st, err)
	}
	if st := must(t)(a.show.State()); current(st) != "B1" || st.Error == nil || st.Error.Code != "reconnecting" {
		t.Fatalf("show after the restart attempt: %+v", st)
	}

	moved := newReceiver(t)
	if a.found("_googlecast._tcp", "Гостиная", moved.addr) != screen {
		t.Fatal("the screen at a new address got another id")
	}
	must(t)(a.show.Start(p.ID, screen, nil))
	if l := moved.received(t, 1, 3*time.Second); !a.shows(l, p, "B1") {
		t.Fatalf("LOAD after the restart: %+v", l)
	}
	moved.waitLoad(t, 1)
	eventually(t, "error cleared", func() bool { return rendererError(t, a.show) == "" })
}

func TestStartOnUnreachableScreenKeepsShow(t *testing.T) {
	a, _, p := twoPhones(t)
	rx := newReceiver(t)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	rx.waitLoad(t, 1)
	must(t)(a.show.Next())
	rx.waitLoad(t, 2)

	other := newReceiver(t)
	other.refusing(true)
	bedroom := a.found("_googlecast._tcp", "Спальня", other.addr)
	if _, err := a.show.Start(p.ID, bedroom, nil); !errors.Is(err, show.ErrUnreachable) {
		t.Fatalf("start on an unreachable screen: %v", err)
	}
	st := must(t)(a.show.State())
	if st.Screen.Name != "Гостиная" || current(st) != "B1" || st.Error != nil {
		t.Fatalf("show after a failed start on another screen: %+v", st)
	}
	must(t)(a.show.Next())
	if l := rx.waitLoad(t, 3); !a.shows(l, p, "A2") {
		t.Fatalf("the show stopped driving its screen: %+v", l)
	}
	if cmds := rx.commanded(); len(cmds) > 0 && cmds[len(cmds)-1] == "STOP" {
		t.Fatalf("the screen got STOP: %v", cmds)
	}
}

func TestBufferingIsShownAndExplained(t *testing.T) {
	show.Stalls(t, 300*time.Millisecond)
	a := newPhone(t, "android", node.DefaultBudget)
	p, err := a.Create("Грузия", "Аня")
	if err != nil {
		t.Fatal(err)
	}
	a.lib.add("V", "video", "video/mp4", 0, randomBytes(1<<20, 1))
	a.lib.duration("V", 1000)
	a.share(p)
	waitTimeline(t, p, 1, a)
	rx := newReceiver(t)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	l := rx.waitLoad(t, 1)
	if resp, _ := get(t, l.url); resp.StatusCode != 200 {
		t.Fatalf("video for the screen: %d", resp.StatusCode)
	}

	rx.push("BUFFERING", "")
	eventually(t, "buffering", func() bool { return must(t)(a.show.State()).Buffering })
	if st := must(t)(a.show.State()); !st.Playing || st.Stalled {
		t.Fatalf("state right after buffering began: %+v", st)
	}
	if body := uiState(t, a); !strings.Contains(body, `"buffering":true`) {
		t.Fatalf("show state in the UI API: %s", body)
	}
	eventually(t, "stalled", func() bool { return must(t)(a.show.State()).Stalled })
	st := must(t)(a.show.State())
	if st.NeedBitrate != 8<<20 || st.Bitrate <= 0 {
		t.Fatalf("bitrates of a stalled video: need %v, streaming %v", st.NeedBitrate, st.Bitrate)
	}
	if !logged(a, "buffers") {
		t.Fatalf("long buffering is not in the journal: %+v", a.Diag().Events())
	}
	if body := uiState(t, a); !strings.Contains(body, `"stalled":true`) || !strings.Contains(body, `"need_bps":8388608`) || !strings.Contains(body, `"rate_bps":`) {
		t.Fatalf("show state in the UI API: %s", body)
	}

	rx.push("PLAYING", "")
	eventually(t, "playing", func() bool { st := must(t)(a.show.State()); return !st.Buffering && !st.Stalled })
	rx.push("BUFFERING", "")
	eventually(t, "buffering again", func() bool { return must(t)(a.show.State()).Buffering })
	if st := must(t)(a.show.State()); !st.Stalled {
		t.Fatalf("repeated buffering is not explained: %+v", st)
	}
}

func TestBufferingWithoutStreamHasNoEstimate(t *testing.T) {
	show.Stalls(t, 300*time.Millisecond)
	a := newPhone(t, "android", node.DefaultBudget)
	p, err := a.Create("Грузия", "Аня")
	if err != nil {
		t.Fatal(err)
	}
	a.lib.add("V", "video", "video/mp4", 0, randomBytes(1<<16, 1))
	a.share(p)
	waitTimeline(t, p, 1, a)
	rx := newReceiver(t)
	must(t)(a.show.Start(p.ID, a.tv(rx), nil))
	rx.waitLoad(t, 1)
	rx.push("BUFFERING", "")
	eventually(t, "stalled", func() bool { return must(t)(a.show.State()).Stalled })
	if st := must(t)(a.show.State()); st.NeedBitrate != 0 || st.Bitrate != 0 {
		t.Fatalf("estimate without duration and without streamed bytes: %+v", st)
	}
}
