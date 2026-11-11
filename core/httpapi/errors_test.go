package httpapi_test

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/httpapi"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

type gallery struct {
	list      string
	listErr   error
	renderErr map[int]error
}

func (g gallery) Media(from, to int64) ([]byte, error) { return []byte(g.list), g.listErr }

func (g gallery) Rendition(local string, tier int) ([]byte, error) {
	if err := g.renderErr[tier]; err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("T%d of %s", tier, local)), nil
}

func (gallery) OriginalSize(string) (int64, error) { return 0, errors.New("not used") }

func (gallery) ReadOriginal(string, int64, int64) ([]byte, error) { return nil, errors.New("not used") }

func serve(t *testing.T, dir string, g *gallery) (*node.Node, client) {
	t.Helper()
	cfg := node.Config{
		Dir:      dir,
		Keystore: device.File(filepath.Join(dir, "device.key")),
		Platform: "android",
		Addrs:    func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} },
	}
	if g != nil {
		cfg.Media, cfg.Renditions = g, g
	}
	n, err := node.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	srv := httptest.NewServer(httpapi.WithNode("secret", n, show.New(n)))
	t.Cleanup(srv.Close)
	return n, client{t: t, url: srv.URL}
}

func sharedPhoto(t *testing.T, n *node.Node) (node.Pile, op.AssetMeta) {
	t.Helper()
	p, err := n.Create("Грузия", "Аня")
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Select(context.Background(), p.ID, july1, july10); err != nil {
		t.Fatal(err)
	}
	s, err := n.Selection(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range s.Items {
		if c.LocalID == "plain" {
			return p, c.Meta
		}
	}
	t.Fatalf("selection %+v has no photo", s)
	return node.Pile{}, op.AssetMeta{}
}

func journaled(t *testing.T, log *diag.Log, pile []byte, parts ...string) {
	t.Helper()
	for _, e := range log.Events() {
		if string(e.Pile) != string(pile) {
			continue
		}
		found := true
		for _, s := range parts {
			found = found && strings.Contains(e.Message, s)
		}
		if found {
			return
		}
	}
	t.Fatalf("no diag event with %q in %+v", parts, log.Events())
}

func pileID(t *testing.T, p pile) []byte {
	t.Helper()
	id, err := hex.DecodeString(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestUnexpectedCoreErrorIsJournaledWithRouteAndText(t *testing.T) {
	n, c := serve(t, t.TempDir(), &gallery{list: "not json"})
	p := c.create("Грузия", "Аня")

	c.fails("PUT", "/piles/"+p.ID+"/selection", map[string]int64{"from": july1, "to": july10}, http.StatusInternalServerError, "internal")

	journaled(t, n.Diag(), pileID(t, p), "PUT /piles/{id}/selection", "invalid character")
}

func TestMediaLibraryFailureHasItsOwnCode(t *testing.T) {
	n, c := serve(t, t.TempDir(), &gallery{listErr: errors.New("java.lang.SecurityException: Permission Denial")})
	p := c.create("Грузия", "Аня")

	c.fails("GET", "/piles/"+p.ID+"/selection", nil, http.StatusInternalServerError, "media_failed")
	c.fails("PUT", "/piles/"+p.ID+"/selection", map[string]int64{"from": july1, "to": july10}, http.StatusInternalServerError, "media_failed")
	journaled(t, n.Diag(), pileID(t, p), "GET /piles/{id}/selection", "Permission Denial")
}

func TestOwnRenderFailureHasItsOwnCode(t *testing.T) {
	g := &gallery{list: string(photos), renderErr: map[int]error{node.T2: errors.New("java.io.FileNotFoundException: no such file")}}
	n, c := serve(t, t.TempDir(), g)
	p, photo := sharedPhoto(t, n)

	resp, body := getBlob(t, c.url, blobPath(p, photo, "2"), "secret")
	if resp.StatusCode != http.StatusInternalServerError || errorCode(t, body) != "render_failed" {
		t.Fatalf("own T2 that cannot be rendered: status %d, body %s", resp.StatusCode, body)
	}
	journaled(t, n.Diag(), p.ID, "GET /piles/{id}/blob/", "no such file")
}

func TestOwnerFailureIsNotAnAsleepOwner(t *testing.T) {
	g := &gallery{list: string(photos), renderErr: map[int]error{node.T2: errors.New("java.io.FileNotFoundException: no such file")}}
	a, _ := serve(t, t.TempDir(), g)
	p, photo := sharedPhoto(t, a)
	inv, err := a.Invite(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, bc := serve(t, t.TempDir(), nil)
	var joined pile
	bc.do("POST", "/piles/join", map[string]string{"invite": inv, "member": "Боря"}, http.StatusCreated, &joined)
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, body := getBlob(t, bc.url, blobPath(p, photo, "1"), "secret")
		if resp.StatusCode == http.StatusOK && string(body) == "T1 of plain" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("T1 from the owner: status %d, body %q", resp.StatusCode, body)
		}
		time.Sleep(10 * time.Millisecond)
	}

	resp, body := getBlob(t, bc.url, blobPath(p, photo, "2"), "secret")
	if resp.StatusCode != http.StatusBadGateway || errorCode(t, body) != "source_failed" {
		t.Fatalf("T2 the owner failed to render: status %d, body %s", resp.StatusCode, body)
	}
	journaled(t, b.Diag(), p.ID, "fetch", "no such file")
}
