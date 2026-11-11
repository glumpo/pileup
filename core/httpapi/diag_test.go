package httpapi_test

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
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
	"github.com/glumpo/pileup/core/invite"
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
		Dir:        dir,
		Keystore:   device.File(filepath.Join(dir, "device.key")),
		Platform:   "ios",
		AppVersion: "1.4.0",
		Addrs:      func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	srv := httptest.NewServer(httpapi.WithNode("secret", n, show.New(n)))
	t.Cleanup(srv.Close)
	return client{t: t, url: srv.URL}, n.Diag()
}

func TestDiagListsEventsNewestFirst(t *testing.T) {
	c, l := startDiag(t)
	var fresh []event
	c.do("GET", "/diag", nil, http.StatusOK, &fresh)
	if len(fresh) != 1 || fresh[0].Kind != "app" || !strings.Contains(fresh[0].Message, "1.4.0") {
		t.Fatalf("fresh log: %v, want only the start of the core", fresh)
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
	if len(got) != 3 || !reflect.DeepEqual(got[:2], want) {
		t.Fatalf("events %+v, want %+v and the start", got, want)
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
	if !strings.HasSuffix(string(body), want.String()) || !strings.Contains(want.String(), "cast lost") {
		t.Fatalf("export\n%s\nwant\n%s", body, want.String())
	}
}

func TestDiagRequiresToken(t *testing.T) {
	c, _ := startDiag(t)
	for _, path := range []string{"/diag", "/diag/export"} {
		c.call("GET", path, "wrong", nil, http.StatusUnauthorized, nil)
	}
}

func exportOf(c client) string {
	c.t.Helper()
	req, _ := http.NewRequest("GET", c.url+"/diag/export", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func TestDiagAcceptsErrorsOfTheApp(t *testing.T) {
	c, _ := startDiag(t)
	c.do("POST", "/diag", map[string]string{"kind": "app", "message": "TypeError: undefined is not a function"}, http.StatusNoContent, nil)
	c.do("POST", "/diag", map[string]string{"kind": "discovery", "message": "nsd: discovery of _pileup._tcp failed, error 0"}, http.StatusNoContent, nil)
	for _, bad := range []any{
		map[string]string{"kind": "bogus", "message": "x"},
		map[string]string{"kind": "app", "message": ""},
		"not an object",
	} {
		c.do("POST", "/diag", bad, http.StatusBadRequest, nil)
	}
	c.call("POST", "/diag", "wrong", map[string]string{"kind": "app", "message": "x"}, http.StatusUnauthorized, nil)

	var got []event
	c.do("GET", "/diag", nil, http.StatusOK, &got)
	if len(got) != 3 || got[0].Kind != "discovery" || got[0].Message != "nsd: discovery of _pileup._tcp failed, error 0" ||
		got[1].Kind != "app" || got[1].Message != "TypeError: undefined is not a function" {
		t.Fatalf("events %+v, want the two reported errors above the start", got)
	}
	if !strings.Contains(exportOf(c), "app TypeError: undefined is not a function") {
		t.Fatal("reported error is not in the export")
	}
}

func TestDiagLongReportIsShortened(t *testing.T) {
	c, _ := startDiag(t)
	c.do("POST", "/diag", map[string]string{"kind": "app", "message": "Error: x\n" + strings.Repeat("at f (index.bundle:1:2)\n", 10000)}, http.StatusNoContent, nil)
	var got []event
	c.do("GET", "/diag", nil, http.StatusOK, &got)
	if !strings.HasPrefix(got[0].Message, "Error: x\n") || len(got[0].Message) > 8<<10 {
		t.Fatalf("report of %d bytes kept as %d bytes", 10000*24, len(got[0].Message))
	}
}

func TestDiagHidesSecrets(t *testing.T) {
	dir := t.TempDir()
	n, err := node.Open(node.Config{
		Dir:      dir,
		Keystore: device.File(filepath.Join(dir, "device.key")),
		Platform: "android",
		Addrs:    func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	const token = "Qx7uiTokenZ"
	srv := httptest.NewServer(httpapi.WithNode(token, n, show.New(n)))
	t.Cleanup(srv.Close)
	c := client{t: t, url: srv.URL}

	var p pile
	c.call("POST", "/piles", token, map[string]string{"name": "Грузия", "member": "Аня"}, http.StatusCreated, &p)
	var out struct{ Invite string }
	c.call("GET", "/piles/"+p.ID+"/invite", token, nil, http.StatusOK, &out)
	inv, err := invite.Parse(out.Invite)
	if err != nil {
		t.Fatal(err)
	}
	c.call("POST", "/diag", token, map[string]string{"kind": "app", "message": "fetch with Bearer " + token + " failed; scanned " + out.Invite}, http.StatusNoContent, nil)
	c.call("POST", "/piles/join", token, map[string]string{"invite": out.Invite + "A", "member": "Боря"}, http.StatusBadRequest, nil)

	var events []event
	c.call("GET", "/diag", token, nil, http.StatusOK, &events)
	req, _ := http.NewRequest("GET", srv.URL+"/diag/export", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	export, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	all := fmt.Sprint(events) + string(export)
	if !strings.Contains(all, "fetch with Bearer") {
		t.Fatalf("reported error is missing: %s", all)
	}
	for _, secret := range []string{token, strings.TrimPrefix(out.Invite, "PILEUP:")[:16], hex.EncodeToString(inv.Secret), base64.StdEncoding.EncodeToString(inv.Secret)} {
		if strings.Contains(all, secret) {
			t.Fatalf("log contains secret %q:\n%s", secret, all)
		}
	}
}

func TestDiagExportStartsWithContext(t *testing.T) {
	c, l := startDiag(t)
	p := c.create("Грузия", "Аня")
	var ps []struct {
		ID      string
		Members []struct{ Key string }
	}
	c.do("GET", "/piles", nil, http.StatusOK, &ps)
	key := ps[0].Members[0].Key
	id, _ := hex.DecodeString(p.ID)
	l.Add(diag.Media, id, key, "fetch: connection refused")

	lines := strings.Split(exportOf(c), "\n")
	if !strings.HasPrefix(lines[0], "Pileup 1.4.0 ios") || !strings.Contains(lines[0], key[:8]) {
		t.Fatalf("first line %q, want version, platform and own key", lines[0])
	}
	var pileLine, eventLine string
	for _, line := range lines {
		if strings.Contains(line, "Грузия") && pileLine == "" {
			pileLine = line
		}
		if strings.Contains(line, "fetch: connection refused") {
			eventLine = line
		}
	}
	if !strings.Contains(pileLine, p.ID[:8]) || !strings.Contains(pileLine, "Аня ("+key[:8]+") ios 1.4.0 online") {
		t.Fatalf("pile line %q, want its id and members with their state", pileLine)
	}
	if !strings.Contains(eventLine, "peer=Аня ("+key[:8]+")") {
		t.Fatalf("event line %q, want the member name next to the key", eventLine)
	}

	var got []event
	c.do("GET", "/diag", nil, http.StatusOK, &got)
	if got[0].Peer != "Аня ("+key[:8]+")" {
		t.Fatalf("peer %q, want the member name next to the key", got[0].Peer)
	}
}
