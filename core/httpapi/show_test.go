package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/httpapi"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

type showItem struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Author   string `json:"author"`
	Kind     string `json:"kind"`
	TakenAt  int64  `json:"taken_at"`
}

type hint struct {
	Author   string `json:"author"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Reason   string `json:"reason"`
}

type showState struct {
	Pile             string          `json:"pile"`
	Screen           screen          `json:"screen"`
	Index            int             `json:"index"`
	Total            int             `json:"total"`
	Current          showItem        `json:"current"`
	Playing          bool            `json:"playing"`
	VideoUnavailable bool            `json:"video_unavailable"`
	RendererError    *map[string]any `json:"renderer_error"`
	Waiting          []showItem      `json:"waiting"`
	Hints            []hint          `json:"hints"`
}

func hostIn(t *testing.T, platform string) (client, *room, pile, []item) {
	t.Helper()
	dir := t.TempDir()
	r := &room{browsing: map[string]node.DiscoveryHandler{}}
	n, err := node.Open(node.Config{
		Dir:        dir,
		Keystore:   device.File(filepath.Join(dir, "device.key")),
		Platform:   platform,
		Addrs:      func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} },
		Media:      phone{anya},
		Renditions: phone{anya},
		Discovery:  r,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := show.New(n)
	t.Cleanup(func() {
		h.Close()
		n.Close()
	})
	srv := httptest.NewServer(httpapi.WithNode("secret", n, h))
	t.Cleanup(srv.Close)
	c := client{t: t, url: srv.URL}
	p := c.create("Грузия", "Аня")
	c.do("PUT", "/piles/"+p.ID+"/selection", map[string]int64{"from": july1, "to": july10}, http.StatusNoContent, nil)
	return c, r, p, c.timeline(p)
}

func appleTV(t *testing.T, c client, r *room) string {
	t.Helper()
	r.handler(t, "_airplay._tcp").Found("Спальня", "192.168.1.31", 7000, nil)
	return c.screens()[0].ID
}

func (c client) show(method, path string, body any) showState {
	c.t.Helper()
	var st showState
	want := http.StatusOK
	if method == "POST" && path == "/show" {
		want = http.StatusCreated
	}
	c.do(method, path, body, want, &st)
	return st
}

func asShowItem(i item) showItem {
	return showItem{ID: i.ID, Revision: i.Revision, Author: i.Author, Kind: i.Kind, TakenAt: i.TakenAt}
}

func TestShowEndpointsRequireToken(t *testing.T) {
	c, _, _, _ := hostIn(t, "ios")
	for _, r := range []struct{ method, path string }{
		{"POST", "/show"}, {"GET", "/show"}, {"DELETE", "/show"}, {"POST", "/show/next"},
		{"POST", "/show/prev"}, {"POST", "/show/goto"}, {"POST", "/show/play"}, {"POST", "/show/pause"},
	} {
		c.call(r.method, r.path, "wrong", nil, http.StatusUnauthorized, nil)
	}
}

func TestShowOnIPhoneHostIsControlledThroughAPI(t *testing.T) {
	c, r, p, items := hostIn(t, "ios")
	if len(items) != 3 || items[1].Kind != "video" {
		t.Fatalf("timeline %+v", items)
	}
	tv := appleTV(t, c, r)
	st := c.show("POST", "/show", map[string]string{"pile": p.ID, "screen": tv})
	want := showState{
		Pile:    p.ID,
		Screen:  screen{ID: tv, Name: "Спальня", Kind: "airplay", Address: "192.168.1.31:7000", Available: true},
		Index:   0,
		Total:   3,
		Current: asShowItem(items[0]),
		Waiting: []showItem{},
		Hints:   []hint{},
	}
	if !reflect.DeepEqual(st, want) {
		t.Fatalf("started show\n%+v\nwant\n%+v", st, want)
	}
	if got := c.show("GET", "/show", nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("state %+v", got)
	}
	c.fails("POST", "/show/play", nil, http.StatusConflict, "not_video")

	st = c.show("POST", "/show/next", nil)
	if st.Index != 1 || st.Current != asShowItem(items[1]) || !st.Playing {
		t.Fatalf("next to video: %+v", st)
	}
	if st := c.show("POST", "/show/pause", nil); st.Playing {
		t.Fatal("paused video is playing")
	}
	if st := c.show("POST", "/show/play", nil); !st.Playing {
		t.Fatal("resumed video is not playing")
	}
	if st := c.show("POST", "/show/next", nil); st.Index != 2 || st.Playing {
		t.Fatalf("next to photo: %+v", st)
	}
	if st := c.show("POST", "/show/next", nil); st.Index != 2 {
		t.Fatalf("next after the last file: index %d", st.Index)
	}
	if st := c.show("POST", "/show/prev", nil); st.Index != 1 {
		t.Fatalf("prev: index %d", st.Index)
	}
	if st := c.show("POST", "/show/goto", map[string]string{"asset": items[0].ID}); st.Index != 0 {
		t.Fatalf("goto: index %d", st.Index)
	}
	if st := c.show("POST", "/show/prev", nil); st.Index != 0 {
		t.Fatalf("prev before the first file: index %d", st.Index)
	}
	c.fails("POST", "/show/goto", map[string]string{"asset": "00"}, http.StatusNotFound, "unknown_asset")
	c.fails("POST", "/show/goto", map[string]string{"asset": "zz"}, http.StatusNotFound, "unknown_asset")
	c.fails("POST", "/show/goto", "not an object", http.StatusBadRequest, "bad_request")

	if st := c.show("POST", "/show", map[string]string{"pile": p.ID, "screen": tv, "asset": items[2].ID}); st.Index != 2 {
		t.Fatalf("start from a file: index %d", st.Index)
	}

	for range 2 {
		c.do("DELETE", "/show", nil, http.StatusNoContent, nil)
	}
	c.fails("GET", "/show", nil, http.StatusNotFound, "no_show")
	for _, path := range []string{"/show/next", "/show/prev", "/show/play", "/show/pause"} {
		c.fails("POST", path, nil, http.StatusNotFound, "no_show")
	}
	c.fails("POST", "/show/goto", map[string]string{"asset": items[0].ID}, http.StatusNotFound, "no_show")
}

func TestShowStartErrors(t *testing.T) {
	c, r, p, _ := hostIn(t, "android")
	tv := appleTV(t, c, r)
	var refused struct{ Error, Reason string }
	c.do("POST", "/show", map[string]string{"pile": p.ID, "screen": tv}, http.StatusConflict, &refused)
	if refused.Error != "screen_unavailable" || refused.Reason != "Показ на Apple TV доступен только с iPhone. Запустите показ с iPhone участника." {
		t.Fatalf("android host on Apple TV: %+v", refused)
	}

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	ap := netip.MustParseAddrPort(strings.TrimPrefix(closed.URL, "http://"))
	r.handler(t, "_googlecast._tcp").Found("Chromecast", ap.Addr().String(), int(ap.Port()), nil)
	var cast string
	for _, s := range c.screens() {
		if s.Kind == "cast" {
			cast = s.ID
		}
	}
	empty := c.create("Пусто", "Аня")
	for name, e := range map[string]struct {
		body   any
		status int
		code   string
	}{
		"unknown screen":     {map[string]string{"pile": p.ID, "screen": "cast:nowhere"}, http.StatusNotFound, "unknown_screen"},
		"unknown pile":       {map[string]string{"pile": "00", "screen": cast}, http.StatusNotFound, "unknown_pile"},
		"pile not hex":       {map[string]string{"pile": "zz", "screen": cast}, http.StatusNotFound, "unknown_pile"},
		"unknown asset":      {map[string]string{"pile": p.ID, "screen": cast, "asset": "00"}, http.StatusNotFound, "unknown_asset"},
		"asset not hex":      {map[string]string{"pile": p.ID, "screen": cast, "asset": "zz"}, http.StatusNotFound, "unknown_asset"},
		"empty pile":         {map[string]string{"pile": empty.ID, "screen": cast}, http.StatusConflict, "empty_pile"},
		"unreachable screen": {map[string]string{"pile": p.ID, "screen": cast}, http.StatusBadGateway, "screen_unreachable"},
		"bad body":           {"not an object", http.StatusBadRequest, "bad_request"},
	} {
		var out struct{ Error string }
		c.do("POST", "/show", e.body, e.status, &out)
		if out.Error != e.code {
			t.Errorf("%s: error %q, want %q", name, out.Error, e.code)
		}
	}
	c.fails("GET", "/show", nil, http.StatusNotFound, "no_show")
}

func TestAppleTVSlideAndPlayerStateThroughAPI(t *testing.T) {
	c, r, p, items := hostIn(t, "ios")
	tv := appleTV(t, c, r)
	var st struct {
		Playing   bool `json:"playing"`
		Buffering bool `json:"buffering"`
		Slide     *struct {
			Image  string   `json:"image"`
			Video  string   `json:"video"`
			Around []string `json:"around"`
		} `json:"slide"`
	}
	c.do("POST", "/show", map[string]string{"pile": p.ID, "screen": tv, "asset": items[1].ID}, http.StatusCreated, &st)
	if st.Slide == nil || st.Slide.Image == "" || st.Slide.Video == "" || len(st.Slide.Around) != 2 {
		t.Fatalf("slide of a video on Apple TV: %+v", st.Slide)
	}
	c.do("POST", "/show/player", map[string]string{"asset": items[1].ID, "state": "buffering"}, http.StatusOK, &st)
	if !st.Buffering || !st.Playing {
		t.Fatalf("buffering reported by the player: %+v", st)
	}
	c.fails("POST", "/show/player", map[string]string{"asset": items[1].ID, "state": "dancing"}, http.StatusBadRequest, "bad_request")
	c.fails("POST", "/show/player", map[string]string{"asset": "zz", "state": "playing"}, http.StatusNotFound, "unknown_asset")
	c.fails("POST", "/show/player", "not an object", http.StatusBadRequest, "bad_request")
	c.call("POST", "/show/player", "wrong", map[string]string{"asset": items[1].ID, "state": "playing"}, http.StatusUnauthorized, nil)
	c.do("DELETE", "/show", nil, http.StatusNoContent, nil)
	c.fails("POST", "/show/player", map[string]string{"asset": items[1].ID, "state": "playing"}, http.StatusNotFound, "no_show")
}
