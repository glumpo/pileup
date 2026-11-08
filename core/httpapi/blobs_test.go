package httpapi_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/httpapi"
	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/store"
)

type thumbnails struct{}

func (thumbnails) Rendition(local string, tier int) ([]byte, error) {
	return []byte(fmt.Sprintf("T%d of %s", tier, local)), nil
}

func (thumbnails) OriginalSize(string) (int64, error) { return 0, errors.New("not used") }

func (thumbnails) ReadOriginal(string, int64, int64) ([]byte, error) {
	return nil, errors.New("not used")
}

func localID(assetID []byte) (string, bool) {
	return strings.CutPrefix(string(assetID), "asset:")
}

var photo = op.AssetMeta{
	AssetID:      []byte("asset:IMG_1"),
	Revision:     []byte("rev:1"),
	Kind:         op.KindPhoto,
	MIME:         "image/jpeg",
	OriginalName: "IMG_1.jpg",
}

func sourceWithPhoto(t *testing.T) (*node.Node, node.Pile) {
	t.Helper()
	dir := t.TempDir()
	cfg := node.Config{
		Dir:        dir,
		Keystore:   device.File(filepath.Join(dir, "device.key")),
		Platform:   "ios",
		Addrs:      func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} },
		Renditions: thumbnails{},
		LocalID:    localID,
	}
	n, err := node.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p, err := n.Create("Грузия", "Аня")
	if err != nil {
		t.Fatal(err)
	}
	inv, err := n.Invite(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	i, err := invite.Parse(inv)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	key, err := device.Key(cfg.Keystore)
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(dir, "piles", hex.EncodeToString(p.ID)+".db"), p.ID, i.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(key, time.Now().UnixMilli(), op.AssetAdd{Assets: []op.AssetMeta{photo}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if n, err = node.Open(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	return n, p
}

func getBlob(t *testing.T, url, path, token string, header ...string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for i := 0; i < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

func blobPath(p node.Pile, m op.AssetMeta, tier string) string {
	return fmt.Sprintf("/piles/%x/blob/%x/%x/%s", p.ID, m.AssetID, m.Revision, tier)
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var out struct{ Error string }
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("error body %q: %v", body, err)
	}
	return out.Error
}

func TestThumbnailAndDisplayForUI(t *testing.T) {
	n, p := sourceWithPhoto(t)
	srv := httptest.NewServer(httpapi.WithNode("secret", n))
	t.Cleanup(srv.Close)

	for _, tier := range []string{"1", "2"} {
		want := []byte("T" + tier + " of IMG_1")
		sum := sha256.Sum256(want)
		etag := `"` + hex.EncodeToString(sum[:]) + `"`
		resp, body := getBlob(t, srv.URL, blobPath(p, photo, tier), "secret")
		if resp.StatusCode != http.StatusOK || !bytes.Equal(body, want) {
			t.Fatalf("T%s: status %d, body %q", tier, resp.StatusCode, body)
		}
		if resp.Header.Get("ETag") != etag || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
			t.Fatalf("T%s: ETag %s (want %s), Cache-Control %q", tier, resp.Header.Get("ETag"), etag, resp.Header.Get("Cache-Control"))
		}
		if resp, _ := getBlob(t, srv.URL, blobPath(p, photo, tier), "secret", "If-None-Match", etag); resp.StatusCode != http.StatusNotModified {
			t.Fatalf("T%s with matching If-None-Match: status %d", tier, resp.StatusCode)
		}
		if resp, body := getBlob(t, srv.URL, blobPath(p, photo, tier), "secret", "Range", "bytes=0-1"); resp.StatusCode != http.StatusPartialContent || string(body) != "T"+tier {
			t.Fatalf("T%s range: status %d, body %q", tier, resp.StatusCode, body)
		}
	}
	if resp, _ := getBlob(t, srv.URL, blobPath(p, photo, "1"), "wrong"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: status %d", resp.StatusCode)
	}

	other := photo
	other.Revision = []byte("rev:2")
	for name, c := range map[string]struct {
		path   string
		status int
		code   string
	}{
		"unknown pile":     {blobPath(node.Pile{ID: []byte("no such pile")}, photo, "1"), http.StatusNotFound, "unknown_pile"},
		"pile not hex":     {fmt.Sprintf("/piles/zz/blob/%x/%x/1", photo.AssetID, photo.Revision), http.StatusNotFound, "unknown_pile"},
		"unknown revision": {blobPath(p, other, "1"), http.StatusNotFound, "unknown_asset"},
		"original":         {blobPath(p, photo, "3"), http.StatusNotFound, "unknown_asset"},
		"tier name":        {blobPath(p, photo, "T1"), http.StatusNotFound, "unknown_asset"},
		"asset not hex":    {fmt.Sprintf("/piles/%x/blob/zz/%x/1", p.ID, photo.Revision), http.StatusNotFound, "unknown_asset"},
	} {
		resp, body := getBlob(t, srv.URL, c.path, "secret")
		if resp.StatusCode != c.status || errorCode(t, body) != c.code {
			t.Errorf("%s: status %d, body %s; want %d %s", name, resp.StatusCode, body, c.status, c.code)
		}
	}
}

func TestDisplayOfOfflineOwnerIsUnavailable(t *testing.T) {
	a, p := sourceWithPhoto(t)
	inv, err := a.Invite(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	b := start(t, "android")
	var joined pile
	b.do("POST", "/piles/join", map[string]string{"invite": inv, "member": "Боря"}, http.StatusCreated, &joined)

	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, body := getBlob(t, b.url, blobPath(p, photo, "1"), "secret")
		if resp.StatusCode == http.StatusOK && string(body) == "T1 of IMG_1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("T1 from the owner: status %d, body %q", resp.StatusCode, body)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	resp, body := getBlob(t, b.url, blobPath(p, photo, "2"), "secret")
	if resp.StatusCode != http.StatusBadGateway || errorCode(t, body) != "source_unavailable" {
		t.Fatalf("T2 of offline owner: status %d, body %s", resp.StatusCode, body)
	}
}
