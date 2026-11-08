package httpapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/httpapi"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

type event struct {
	Time    int64  `json:"time"`
	Kind    string `json:"kind"`
	Pile    string `json:"pile"`
	Peer    string `json:"peer"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}

func startDiag(t *testing.T) (client, *diag.Log) {
	t.Helper()
	dir := t.TempDir()
	n, err := node.Open(node.Config{
		Dir:      dir,
		Keystore: device.File(filepath.Join(dir, "device.key")),
		Platform: "ios",
		Addrs:    func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	srv := httptest.NewServer(httpapi.WithNode("secret", n, show.New(n, show.DefaultBudget)))
	t.Cleanup(srv.Close)
	return client{t: t, url: srv.URL}, n.Diag()
}

func TestDiagListsEventsNewestFirst(t *testing.T) {
	c, l := startDiag(t)
	var none []event
	c.do("GET", "/diag", nil, http.StatusOK, &none)
	if none == nil || len(none) != 0 {
		t.Fatalf("fresh log: %v, want an empty list", none)
	}

	before := time.Now().UnixMilli()
	l.Add(diag.Sync, []byte{0xab, 0x01}, "10.0.0.2:4000", "sync established")
	l.Add(diag.Renderer, nil, "", "cast lost")
	l.Add(diag.Renderer, nil, "", "cast lost")
	after := time.Now().UnixMilli()
	var got []event
	c.do("GET", "/diag", nil, http.StatusOK, &got)
	for i, e := range got {
		if e.Time < before || e.Time > after {
			t.Fatalf("event %d time %d not in [%d, %d]", i, e.Time, before, after)
		}
		got[i].Time = 0
	}
	want := []event{
		{Kind: "renderer", Message: "cast lost", Count: 2},
		{Kind: "sync", Pile: "ab01", Peer: "10.0.0.2:4000", Message: "sync established", Count: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events %+v, want %+v", got, want)
	}
}

func TestDiagExportIsATextAttachment(t *testing.T) {
	c, l := startDiag(t)
	l.Add(diag.Connection, []byte{0xab, 0x01}, "10.0.0.2:4000", "announce: connection refused")
	l.Add(diag.Renderer, nil, "", "cast lost")
	req, _ := http.NewRequest("GET", c.url+"/diag/export", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment; filename=") || !strings.Contains(cd, ".txt") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	var want strings.Builder
	l.Export(&want)
	if string(body) != want.String() || !strings.Contains(want.String(), "cast lost") {
		t.Fatalf("export\n%s\nwant\n%s", body, want.String())
	}
}

func TestDiagRequiresToken(t *testing.T) {
	c, _ := startDiag(t)
	for _, path := range []string{"/diag", "/diag/export"} {
		c.call("GET", path, "wrong", nil, http.StatusUnauthorized, nil)
	}
}
