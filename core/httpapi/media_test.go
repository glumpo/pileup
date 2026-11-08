package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/httpapi"
	"github.com/glumpo/pileup/core/node"
)

type library string

func (l library) Media(from, to int64) ([]byte, error) { return []byte(l), nil }

type asset struct {
	ID           string `json:"id"`
	LocalID      string `json:"local_id"`
	Kind         string `json:"kind"`
	TakenAt      int64  `json:"taken_at"`
	OriginalName string `json:"original_name"`
	Screenshot   bool   `json:"screenshot"`
	Hidden       bool   `json:"hidden"`
	Selected     bool   `json:"selected"`
}

type selection struct {
	From   int64   `json:"from"`
	To     int64   `json:"to"`
	Assets []asset `json:"assets"`
}

const photos library = `[
	{"local_id": "plain", "kind": "photo", "platform_time": 1782986400000, "width": 4032, "height": 3024,
	 "size_bytes": 2500000, "mime": "image/heic", "original_name": "IMG_0001.HEIC", "modified": 1,
	 "latitude": 41.715137, "longitude": 44.827096},
	{"local_id": "shot", "kind": "photo", "platform_time": 1782990000000, "width": 1179, "height": 2556,
	 "size_bytes": 900000, "mime": "image/png", "original_name": "IMG_0002.PNG", "modified": 1, "screenshot": true},
	{"local_id": "later", "kind": "video", "platform_time": 1785664800000, "width": 1920, "height": 1080,
	 "duration_ms": 15000, "size_bytes": 80000000, "mime": "video/mp4", "original_name": "IMG_0003.MOV", "modified": 1}
]`

const (
	july1  = 1782864000000
	july10 = 1783641600000
)

func startWith(t *testing.T, lib node.MediaProvider) client {
	t.Helper()
	dir := t.TempDir()
	n, err := node.Open(node.Config{
		Dir:      dir,
		Keystore: device.File(filepath.Join(dir, "device.key")),
		Platform: "ios",
		Addrs:    func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} },
		Media:    lib,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	srv := httptest.NewServer(httpapi.WithNode("secret", n))
	t.Cleanup(srv.Close)
	return client{t: t, url: srv.URL}
}

func (c client) selection(p pile) selection {
	c.t.Helper()
	var s selection
	c.do("GET", "/piles/"+p.ID+"/selection", nil, http.StatusOK, &s)
	return s
}

func (c client) fails(method, path string, body any, status int, code string) {
	c.t.Helper()
	var out struct{ Error string }
	c.do(method, path, body, status, &out)
	if out.Error != code {
		c.t.Fatalf("%s %s: error = %q, want %q", method, path, out.Error, code)
	}
}

func TestSelectionEndpointsRequireToken(t *testing.T) {
	a := startWith(t, photos)
	p := a.create("Грузия", "Аня")
	for _, r := range []struct{ method, path string }{
		{"GET", "/piles/" + p.ID + "/selection"},
		{"PUT", "/piles/" + p.ID + "/selection"},
		{"PUT", "/piles/" + p.ID + "/selection/00"},
	} {
		a.call(r.method, r.path, "wrong", nil, http.StatusUnauthorized, nil)
	}
}

func TestSelectRangeListAndExclude(t *testing.T) {
	a := startWith(t, photos)
	p := a.create("Грузия", "Аня")
	if s := a.selection(p); len(s.Assets) != 0 {
		t.Fatalf("selection before choosing a range %+v", s)
	}
	a.do("PUT", "/piles/"+p.ID+"/selection", map[string]int64{"from": july1, "to": july10}, http.StatusNoContent, nil)

	s := a.selection(p)
	if s.From != july1 || s.To != july10 || len(s.Assets) != 2 {
		t.Fatalf("selection %+v", s)
	}
	plain, shot := s.Assets[0], s.Assets[1]
	if plain.LocalID != "plain" {
		plain, shot = shot, plain
	}
	if plain.ID == "" || plain.LocalID != "plain" || plain.Kind != "photo" || plain.TakenAt != 1782986400000 ||
		plain.OriginalName != "IMG_0001.HEIC" || plain.Screenshot || plain.Hidden || !plain.Selected {
		t.Fatalf("plain photo %+v", plain)
	}
	if shot.LocalID != "shot" || !shot.Screenshot || shot.Selected {
		t.Fatalf("screenshot %+v", shot)
	}

	a.do("PUT", "/piles/"+p.ID+"/selection/"+plain.ID, map[string]bool{"selected": false}, http.StatusNoContent, nil)
	a.do("PUT", "/piles/"+p.ID+"/selection/"+shot.ID, map[string]bool{"selected": true}, http.StatusNoContent, nil)
	selected := map[string]bool{}
	for _, x := range a.selection(p).Assets {
		selected[x.LocalID] = x.Selected
	}
	if !selected["shot"] || selected["plain"] || len(selected) != 2 {
		t.Fatalf("after manual decisions %v", selected)
	}
}

func TestSelectionErrors(t *testing.T) {
	a := startWith(t, photos)
	p := a.create("Грузия", "Аня")
	base := "/piles/" + p.ID + "/selection"
	for _, id := range []string{"00112233445566778899aabbccddeeff", "zz"} {
		a.fails("GET", "/piles/"+id+"/selection", nil, http.StatusNotFound, "unknown_pile")
		a.fails("PUT", "/piles/"+id+"/selection", map[string]int64{"from": 0, "to": 1}, http.StatusNotFound, "unknown_pile")
	}
	a.fails("PUT", base, nil, http.StatusBadRequest, "bad_request")
	a.fails("PUT", base, map[string]int64{"from": july1}, http.StatusBadRequest, "bad_request")
	a.do("PUT", base, map[string]int64{"from": july1, "to": july10}, http.StatusNoContent, nil)
	id := a.selection(p).Assets[0].ID
	a.fails("PUT", base+"/"+id, map[string]string{}, http.StatusBadRequest, "bad_request")
	for _, asset := range []string{"00112233445566778899aabbccddeeff", "zz"} {
		a.fails("PUT", base+"/"+asset, map[string]bool{"selected": false}, http.StatusNotFound, "unknown_asset")
	}

	b := start(t, "android")
	q := b.create("Грузия", "Боря")
	b.fails("GET", "/piles/"+q.ID+"/selection", nil, http.StatusNotImplemented, "no_media")
	b.fails("PUT", "/piles/"+q.ID+"/selection", map[string]int64{"from": july1, "to": july10}, http.StatusNotImplemented, "no_media")
}
