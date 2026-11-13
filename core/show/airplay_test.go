package show_test

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

const bedroom = "192.168.1.31:7000"

func appleTVShow(t *testing.T) (*phone, *phone, node.Pile, string) {
	t.Helper()
	a, b, p := twoPhones(t)
	b.lib.add("V", "video", "video/quicktime", 4, []byte("original of V"))
	b.share(p)
	waitTimeline(t, p, 5, a, b)
	return a, b, p, a.found("_airplay._tcp", "Спальня", bedroom)
}

func body(t *testing.T, u string) string {
	t.Helper()
	resp, b := get(t, u)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET slide: %d %s", resp.StatusCode, b)
	}
	return string(b)
}

func bodies(t *testing.T, urls []string) []string {
	t.Helper()
	var out []string
	for _, u := range urls {
		out = append(out, body(t, u))
	}
	slices.Sort(out)
	return out
}

func ownAddress(t *testing.T, a netip.Addr) bool {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range addrs {
		if p, err := netip.ParsePrefix(x.String()); err == nil && p.Addr().Unmap() == a {
			return true
		}
	}
	return false
}

func TestAppleTVGetsSlidesFromTheShowPortOnTheLANAddress(t *testing.T) {
	a, _, p, tv := appleTVShow(t)
	st := must(t)(a.show.Start(p.ID, tv, nil))
	if st.Slide == nil {
		t.Fatalf("show on Apple TV has no slide: %+v", st)
	}
	u, err := url.Parse(st.Slide.Image)
	if err != nil {
		t.Fatal(err)
	}
	host, err := netip.ParseAddr(u.Hostname())
	if err != nil || host.IsLoopback() || !ownAddress(t, host) {
		t.Fatalf("slide is served on %q, want a LAN address of this phone", u.Host)
	}
	if got := body(t, st.Slide.Image); got != display("A1") {
		t.Fatalf("slide is %q", got)
	}
	if st.Slide.Video != "" {
		t.Fatalf("photo has a video URL %q", st.Slide.Video)
	}
	if got := bodies(t, st.Slide.Around); !slices.Equal(got, []string{display("B1")}) {
		t.Fatalf("around the first file: %q", got)
	}

	st = must(t)(a.show.Next())
	if got := body(t, st.Slide.Image); got != display("B1") {
		t.Fatalf("slide after next is %q", got)
	}
	if got := bodies(t, st.Slide.Around); !slices.Equal(got, []string{display("A1"), display("A2")}) {
		t.Fatalf("around the second file: %q", got)
	}

	parts := strings.Split(u.Path, "/")
	parts[2] = "wrong"
	u.Path = strings.Join(parts, "/")
	if resp, _ := get(t, u.String()); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("slide with a wrong token: %d", resp.StatusCode)
	}

	a.show.Stop()
	if resp, err := http.Get(st.Slide.Image); err == nil {
		resp.Body.Close()
		t.Fatalf("show port answers after stop: %d", resp.StatusCode)
	}
}

func TestShowOnCastHasNoSlide(t *testing.T) {
	a, _, p := twoPhones(t)
	if st := must(t)(a.show.Start(p.ID, a.tv(newReceiver(t)), nil)); st.Slide != nil {
		t.Fatalf("cast show has a slide %+v", st.Slide)
	}
}

func TestAppleTVPlaysVideoFromTheShowPort(t *testing.T) {
	a, _, p, tv := appleTVShow(t)
	v := a.asset(p, "V").Meta.AssetID
	st := must(t)(a.show.Start(p.ID, tv, v))
	if !st.Playing || st.Slide == nil || st.Slide.Video == "" {
		t.Fatalf("video on Apple TV: %+v", st)
	}
	if got := body(t, st.Slide.Image); got != display("V") {
		t.Fatalf("poster is %q", got)
	}
	resp, b := get(t, st.Slide.Video, "Range", "bytes=0-7")
	if resp.StatusCode != http.StatusPartialContent || string(b) != "original" {
		t.Fatalf("video range: %d %q", resp.StatusCode, b)
	}
	st = must(t)(a.show.Pause())
	if st.Playing || st.Slide.Video == "" {
		t.Fatalf("paused video: playing %v, video %q", st.Playing, st.Slide.Video)
	}
	if st := must(t)(a.show.Play()); !st.Playing {
		t.Fatal("resumed video is not playing")
	}
}

func TestAppleTVPlayerStateReachesTheRemote(t *testing.T) {
	show.Stalls(t, time.Hour)
	a, _, p, tv := appleTVShow(t)
	v := a.asset(p, "V").Meta.AssetID
	must(t)(a.show.Start(p.ID, tv, v))

	if st := must(t)(a.show.Player(v, "buffering", "")); !st.Buffering || !st.Playing || st.Stalled {
		t.Fatalf("buffering: %+v", st)
	}
	if st := must(t)(a.show.Player(v, "playing", "")); st.Buffering || !st.Playing {
		t.Fatalf("playing after buffering: %+v", st)
	}
	if st := must(t)(a.show.Player(v, "buffering", "")); !st.Stalled {
		t.Fatalf("second buffering is not stalled: %+v", st)
	}
	must(t)(a.show.Player(v, "playing", ""))

	must(t)(a.show.Pause())
	if st := must(t)(a.show.Player(v, "buffering", "")); st.Buffering || st.Playing {
		t.Fatalf("paused video reported buffering: %+v", st)
	}
	if st := must(t)(a.show.Player(v, "playing", "")); st.Playing {
		t.Fatal("late report from the player resumed a paused video")
	}
	must(t)(a.show.Play())

	other := a.asset(p, "A1").Meta.AssetID
	if st := must(t)(a.show.Player(other, "ended", "")); !st.Playing || st.Slide.Video == "" {
		t.Fatalf("report about another file changed the video: %+v", st)
	}

	st := must(t)(a.show.Player(v, "ended", ""))
	if st.Playing || st.Slide.Video != "" || body(t, st.Slide.Image) != display("V") {
		t.Fatalf("finished video: %+v", st)
	}
	if st := must(t)(a.show.Play()); !st.Playing || st.Slide.Video == "" {
		t.Fatalf("play after the end: %+v", st)
	}

	st = must(t)(a.show.Player(v, "failed", "Cannot Open"))
	if !st.VideoUnavailable || st.VideoReason != show.Unsupported || st.Playing || st.Slide.Video != "" {
		t.Fatalf("failed video: %+v", st)
	}
	eventually(t, "player failure in diag", func() bool { return logged(a, "Cannot Open") })

	if _, err := a.show.Player(v, "dancing", ""); !errors.Is(err, show.ErrPlayerState) {
		t.Fatalf("unknown player state: %v", err)
	}
	a.show.Stop()
	if _, err := a.show.Player(v, "playing", ""); !errors.Is(err, show.ErrNoShow) {
		t.Fatalf("player report without a show: %v", err)
	}
}

func TestInterruptedAppleTVShowContinuesBeforeTheScreenIsFoundAgain(t *testing.T) {
	a, _, p, tv := appleTVShow(t)
	must(t)(a.show.Start(p.ID, tv, a.asset(p, "B1").Meta.AssetID))
	a.show.Close()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a = openPhone(t, a.dir, "ios", a.lib, node.DefaultBudget)

	in, err := a.show.Interrupted()
	if err != nil {
		t.Fatal(err)
	}
	st := must(t)(a.show.Start(in.Pile, in.Screen.ID, in.Asset))
	if st.Screen.Kind != "airplay" || current(st) != "B1" || st.Slide == nil || body(t, st.Slide.Image) != display("B1") {
		t.Fatalf("continued show on Apple TV: %+v", st)
	}
}
