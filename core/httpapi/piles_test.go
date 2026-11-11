package httpapi_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/httpapi"
	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

type member struct {
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
}

type pile struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Members []member `json:"members"`
}

type client struct {
	t   *testing.T
	url string
}

func start(t *testing.T, platform string) client {
	t.Helper()
	dir := t.TempDir()
	n, err := node.Open(node.Config{
		Dir:        dir,
		Keystore:   device.File(filepath.Join(dir, "device.key")),
		Platform:   platform,
		AppVersion: "1.4." + platform,
		Addrs:      func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	srv := httptest.NewServer(httpapi.WithNode("secret", n, show.New(n)))
	t.Cleanup(srv.Close)
	return client{t: t, url: srv.URL}
}

func (c client) call(method, path, token string, body any, want int, out any) {
	c.t.Helper()
	var b []byte
	if body != nil {
		var err error
		if b, err = json.Marshal(body); err != nil {
			c.t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, c.url+path, bytes.NewReader(b))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s: status = %d, want %d", method, path, resp.StatusCode, want)
	}
	if out == nil {
		return
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		c.t.Fatalf("%s %s: Content-Type = %q", method, path, ct)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		c.t.Fatal(err)
	}
}

func (c client) do(method, path string, body any, want int, out any) {
	c.t.Helper()
	c.call(method, path, "secret", body, want, out)
}

func (c client) create(name, member string) pile {
	c.t.Helper()
	var p pile
	c.do("POST", "/piles", map[string]string{"name": name, "member": member}, http.StatusCreated, &p)
	return p
}

func (c client) invite(p pile) string {
	c.t.Helper()
	var out struct{ Invite string }
	c.do("GET", "/piles/"+p.ID+"/invite", nil, http.StatusOK, &out)
	return out.Invite
}

func (c client) piles() []pile {
	c.t.Helper()
	var ps []pile
	c.do("GET", "/piles", nil, http.StatusOK, &ps)
	return ps
}

func (c client) joinFails(inv string, status int, code string) {
	c.t.Helper()
	var out struct{ Error string }
	c.do("POST", "/piles/join", map[string]string{"invite": inv, "member": "Боря"}, status, &out)
	if out.Error != code {
		c.t.Fatalf("join %q: error = %q, want %q", inv, out.Error, code)
	}
}

func (c client) joinReason(inv string) string {
	c.t.Helper()
	var out struct{ Error, Reason string }
	c.do("POST", "/piles/join", map[string]string{"invite": inv, "member": "Боря"}, http.StatusBadGateway, &out)
	return out.Reason
}

func TestPileEndpointsRequireToken(t *testing.T) {
	a := start(t, "android")
	p := a.create("Грузия", "Аня")
	for _, r := range []struct{ method, path string }{
		{"GET", "/piles"},
		{"POST", "/piles"},
		{"GET", "/piles/" + p.ID + "/invite"},
		{"POST", "/piles/join"},
	} {
		a.call(r.method, r.path, "wrong", nil, http.StatusUnauthorized, nil)
	}
}

func TestCreateListAndJoinByInvite(t *testing.T) {
	a, b := start(t, "android"), start(t, "ios")
	p := a.create("Грузия", "Аня")
	want := pile{ID: p.ID, Name: "Грузия", Members: []member{{"Аня", "android", "1.4.android"}}}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("created %+v, want %+v", p, want)
	}
	if got := a.piles(); !reflect.DeepEqual(got, []pile{want}) {
		t.Fatalf("listed %+v", got)
	}

	inv := a.invite(p)
	if !strings.HasPrefix(inv, "PILEUP:") {
		t.Fatalf("invite = %q", inv)
	}
	var joined pile
	b.do("POST", "/piles/join", map[string]string{"invite": inv, "member": "Боря"}, http.StatusCreated, &joined)
	if joined.ID != p.ID {
		t.Fatalf("joined %+v", joined)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		pa, pb := a.piles(), b.piles()
		if len(pa) == 1 && len(pa[0].Members) == 2 && reflect.DeepEqual(pa, pb) && pb[0].Name == "Грузия" {
			if !slices.Contains(pa[0].Members, member{"Боря", "ios", "1.4.ios"}) {
				t.Fatalf("joiner listed as %+v", pa[0].Members)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("inviter lists %+v, joiner lists %+v", pa, pb)
		}
		time.Sleep(10 * time.Millisecond)
	}
	b.joinFails(inv, http.StatusConflict, "already_joined")
}

func TestJoinErrors(t *testing.T) {
	a, b := start(t, "android"), start(t, "ios")
	p := a.create("Грузия", "Аня")
	i, err := invite.Parse(a.invite(p))
	if err != nil {
		t.Fatal(err)
	}
	b.joinFails("not an invite", http.StatusBadRequest, "bad_invite")
	b.joinFails("PILEUP:AI", http.StatusBadRequest, "unknown_invite_version")
	i.Secret = make([]byte, 32)
	b.joinFails(i.String(), http.StatusBadGateway, "inviter_unreachable")
	if got := b.joinReason(i.String()); got != "other_device" {
		t.Fatalf("wrong secret: reason = %q, want other_device", got)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := netip.MustParseAddrPort(l.Addr().String())
	l.Close()
	i.Addrs = []netip.AddrPort{closed}
	if got := b.joinReason(i.String()); got != "refused" {
		t.Fatalf("closed port: reason = %q, want refused", got)
	}

	var out struct{ Error string }
	b.do("POST", "/piles/join", nil, http.StatusBadRequest, &out)
	if out.Error != "bad_request" {
		t.Fatalf("empty body: error = %q", out.Error)
	}
}

func TestInviteForUnknownPile(t *testing.T) {
	a := start(t, "android")
	for _, id := range []string{"00112233445566778899aabbccddeeff", "zz"} {
		var out struct{ Error string }
		a.do("GET", "/piles/"+id+"/invite", nil, http.StatusNotFound, &out)
		if out.Error != "unknown_pile" {
			t.Fatalf("%s: error = %q", id, out.Error)
		}
	}
}

type presence struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Online    bool   `json:"online"`
	LastSeen  *int64 `json:"last_seen"`
	LastError string `json:"last_error"`
}

func (c client) presence(id string) map[string]presence {
	c.t.Helper()
	var ps []struct {
		ID      string     `json:"id"`
		Members []presence `json:"members"`
	}
	c.do("GET", "/piles", nil, http.StatusOK, &ps)
	out := map[string]presence{}
	for _, p := range ps {
		for _, m := range p.Members {
			if p.ID == id {
				out[m.Name] = m
			}
		}
	}
	return out
}

func TestPileListsWhetherMembersAreConnected(t *testing.T) {
	a, p, _ := sourceWithPhoto(t)
	inv, err := a.Invite(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	b := start(t, "android")
	b.do("POST", "/piles/join", map[string]string{"invite": inv, "member": "Боря"}, http.StatusCreated, nil)
	id := hex.EncodeToString(p.ID)
	author := a.Author()
	key := hex.EncodeToString(author[:])

	deadline := time.Now().Add(10 * time.Second)
	for {
		m := b.presence(id)
		if anya := m["Аня"]; anya.Online && anya.Key == key && anya.LastSeen != nil && anya.LastError == "" && m["Боря"].Online {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("members %+v, want Аня with key %s and both online", m, key)
		}
		time.Sleep(10 * time.Millisecond)
	}

	before := time.Now().UnixMilli()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		anya := b.presence(id)["Аня"]
		if !anya.Online && anya.LastError == "refused" {
			if anya.LastSeen == nil || *anya.LastSeen < before || *anya.LastSeen > time.Now().UnixMilli() {
				t.Fatalf("Аня last seen at %v, want the end of the connection after %d", anya.LastSeen, before)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Аня after her Pileup closed: %+v", anya)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
