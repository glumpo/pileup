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
	"github.com/glumpo/pileup/core/show"
)

type interrupted struct {
	Pile   string `json:"pile"`
	Screen screen `json:"screen"`
	Asset  string `json:"asset"`
}

func TestInterruptedShowIsOfferedThroughAPI(t *testing.T) {
	dir := t.TempDir()
	r := &room{browsing: map[string]node.DiscoveryHandler{}}
	open := func() (client, func()) {
		n, err := node.Open(node.Config{
			Dir:        dir,
			Keystore:   device.File(filepath.Join(dir, "device.key")),
			Platform:   "ios",
			Addrs:      func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} },
			Media:      phone{anya},
			Renditions: phone{anya},
			Discovery:  r,
		})
		if err != nil {
			t.Fatal(err)
		}
		h := show.New(n)
		srv := httptest.NewServer(httpapi.WithNode("secret", n, h))
		closed := false
		stop := func() {
			if !closed {
				closed = true
				srv.Close()
				h.Close()
				n.Close()
			}
		}
		t.Cleanup(stop)
		return client{t: t, url: srv.URL}, stop
	}
	c, stop := open()
	p := c.create("Грузия", "Аня")
	c.do("PUT", "/piles/"+p.ID+"/selection", map[string]int64{"from": july1, "to": july10}, http.StatusNoContent, nil)
	items := c.timeline(p)
	tv := appleTV(t, c, r)
	c.show("POST", "/show", map[string]string{"pile": p.ID, "screen": tv, "asset": items[1].ID})
	c.do("GET", "/show/interrupted", nil, http.StatusNotFound, nil)
	stop()

	c, _ = open()
	var in interrupted
	c.do("GET", "/show/interrupted", nil, http.StatusOK, &in)
	if in.Pile != p.ID || in.Screen.ID != tv || in.Screen.Name != "Спальня" || in.Screen.Kind != "airplay" || in.Asset != items[1].ID {
		t.Fatalf("interrupted show %+v, want pile %s, screen %s, file %s", in, p.ID, tv, items[1].ID)
	}
	c.do("DELETE", "/show/interrupted", nil, http.StatusNoContent, nil)
	c.do("GET", "/show/interrupted", nil, http.StatusNotFound, nil)
	c.call("GET", "/show/interrupted", "wrong", nil, http.StatusUnauthorized, nil)
}
