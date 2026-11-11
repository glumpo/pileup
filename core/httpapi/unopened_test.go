package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/httpapi"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

func openDir(t *testing.T, dir string) (*node.Node, client) {
	t.Helper()
	n, err := node.Open(node.Config{
		Dir:      dir,
		Keystore: device.File(filepath.Join(dir, "device.key")),
		Platform: "android",
		Addrs:    func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} },
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpapi.WithNode("secret", n, show.New(n)))
	t.Cleanup(func() {
		srv.Close()
		n.Close()
	})
	return n, client{t: t, url: srv.URL}
}

func TestEveryRouteOfUnopenedPileAnswersPileUnavailable(t *testing.T) {
	dir := t.TempDir()
	n, c := openDir(t, dir)
	p := c.create("Армения", "Аня")
	inv := c.invite(p)
	n.Close()
	if err := os.WriteFile(filepath.Join(dir, "piles", p.ID+".db"), bytes.Repeat([]byte("not a database "), 512), 0o600); err != nil {
		t.Fatal(err)
	}
	_, c = openDir(t, dir)

	var listed []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Broken string `json:"broken"`
	}
	c.do("GET", "/piles", nil, http.StatusOK, &listed)
	if len(listed) != 1 || listed[0].ID != p.ID || listed[0].Name != "Армения" || listed[0].Broken != "corrupt" {
		t.Fatalf("GET /piles = %+v, want the pile with its name and reason corrupt", listed)
	}

	base := "/piles/" + p.ID
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{"GET", base + "/invite", nil},
		{"GET", base + "/selection", nil},
		{"PUT", base + "/selection", map[string]int64{"from": 0, "to": 1}},
		{"PUT", base + "/selection/00", map[string]bool{"selected": true}},
		{"GET", base + "/timeline", nil},
		{"GET", base + "/blob/00/00/1", nil},
		{"GET", base + "/pins", nil},
		{"POST", base + "/pins", map[string]string{}},
		{"DELETE", base + "/pins", nil},
		{"GET", base + "/pins/estimate", nil},
		{"POST", "/piles/join", map[string]string{"invite": inv, "member": "Аня"}},
	} {
		var out struct{ Error string }
		c.do(r.method, r.path, r.body, http.StatusServiceUnavailable, &out)
		if out.Error != "pile_unavailable" {
			b, _ := json.Marshal(out)
			t.Errorf("%s %s: %s, want pile_unavailable", r.method, r.path, b)
		}
	}
}
