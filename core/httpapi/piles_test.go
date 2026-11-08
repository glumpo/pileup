package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"reflect"
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
	Name     string `json:"name"`
	Platform string `json:"platform"`
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
		Dir:      dir,
		Keystore: device.File(filepath.Join(dir, "device.key")),
		Platform: platform,
		Addrs:    func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	srv := httptest.NewServer(httpapi.WithNode("secret", n, show.New(n, show.DefaultBudget)))
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
	want := pile{ID: p.ID, Name: "Грузия", Members: []member{{"Аня", "android"}}}
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
