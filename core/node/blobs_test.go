package node_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/vv"
	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/peer"
)

type media struct {
	mu         sync.Mutex
	items      []node.MediaItem
	originals  map[string][]byte
	renditions map[int]int
	reads      int
	hold       chan chan struct{}
}

func newMedia() *media {
	return &media{originals: map[string][]byte{}, renditions: map[int]int{}}
}

func (m *media) add(item node.MediaItem, original []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item.SizeBytes = uint64(len(original))
	m.items = append(m.items, item)
	m.originals[item.LocalID] = original
}

func (m *media) Media(from, to int64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return json.Marshal(m.items)
}

func rendition(local string, tier int) []byte {
	return []byte(fmt.Sprintf("T%d of %s", tier, local))
}

func (m *media) Rendition(local string, tier int) ([]byte, error) {
	m.mu.Lock()
	hold := m.hold
	m.mu.Unlock()
	if hold != nil && tier == node.T2 {
		release := make(chan struct{})
		hold <- release
		<-release
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.originals[local]; !ok {
		return nil, errors.New("no such file")
	}
	m.renditions[tier]++
	return rendition(local, tier), nil
}

func (m *media) OriginalSize(local string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads++
	return int64(len(m.originals[local])), nil
}

func (m *media) ReadOriginal(local string, offset, length int64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads++
	b := m.originals[local]
	return b[offset:min(offset+length, int64(len(b)))], nil
}

func (m *media) calls() (renditions map[int]int, reads int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[int]int{}
	for k, v := range m.renditions {
		out[k] = v
	}
	return out, m.reads
}

func openSource(t *testing.T, dir, platform string, m *media) *node.Node {
	t.Helper()
	n, err := node.Open(node.Config{
		Dir:        dir,
		Keystore:   device.File(filepath.Join(dir, "device.key")),
		Platform:   platform,
		Addrs:      loopback,
		Media:      m,
		Renditions: m,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	return n
}

const day = "2026-07-02T10:00:00Z"

func share(t *testing.T, n *node.Node, p node.Pile) map[string]op.AssetMeta {
	t.Helper()
	choose(t, n, p.ID, "2026-07-01T00:00:00Z", "2026-07-10T00:00:00Z")
	out := map[string]op.AssetMeta{}
	for _, c := range selection(t, n, p.ID).Items {
		out[c.LocalID] = c.Meta
	}
	return out
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: timed out", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func blobPath(m op.AssetMeta, tier int) string {
	return fmt.Sprintf("/blob/%x/%x/%d", m.AssetID, m.Revision, tier)
}

type member struct {
	t    *testing.T
	addr string
	http *http.Client
}

func memberOf(t *testing.T, n *node.Node, p node.Pile) member {
	t.Helper()
	i, err := invite.Parse(inviteTo(t, n, p))
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	tr := &http.Transport{DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
		return peer.Dial(ctx, addr, key, i.PileID, i.Secret)
	}}
	t.Cleanup(tr.CloseIdleConnections)
	return member{t: t, addr: i.Addrs[0].String(), http: &http.Client{Transport: tr, Timeout: 10 * time.Second}}
}

func (m member) get(path string, header ...string) (*http.Response, []byte) {
	m.t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+m.addr+path, nil)
	if err != nil {
		m.t.Fatal(err)
	}
	for i := 0; i < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := m.http.Do(req)
	if err != nil {
		m.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		m.t.Fatal(err)
	}
	return resp, body
}

func contentETag(b []byte) string {
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func TestRenditionsServedImmutableWithContentHashETag(t *testing.T) {
	m := newMedia()
	a := openSource(t, t.TempDir(), "ios", m)
	p := create(t, a, "Грузия", "Аня")
	m.add(photo("IMG_1", day), []byte("jpeg original"))
	img := share(t, a, p)["IMG_1"]
	b := memberOf(t, a, p)
	if r, _ := m.calls(); r[node.T2] != 0 {
		t.Fatalf("T2 made before any request: %v", r)
	}

	for _, tier := range []int{node.T1, node.T2} {
		want := rendition("IMG_1", tier)
		for range 2 {
			resp, body := b.get(blobPath(img, tier))
			if resp.StatusCode != http.StatusOK || !bytes.Equal(body, want) {
				t.Fatalf("T%d: status %d, body %q; want 200, %q", tier, resp.StatusCode, body, want)
			}
			if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
				t.Fatalf("T%d: Cache-Control = %q", tier, cc)
			}
			if etag := resp.Header.Get("ETag"); etag != contentETag(want) {
				t.Fatalf("T%d: ETag = %s, want SHA-256 of content %s", tier, etag, contentETag(want))
			}
		}
		resp, body := b.get(blobPath(img, tier), "If-None-Match", contentETag(want))
		if resp.StatusCode != http.StatusNotModified || len(body) != 0 {
			t.Fatalf("T%d with matching If-None-Match: status %d, %d bytes", tier, resp.StatusCode, len(body))
		}
	}
	if r, _ := m.calls(); r[node.T1] != 1 || r[node.T2] != 1 || len(r) != 2 {
		t.Fatalf("renditions made %v, want each tier once", r)
	}
}

func TestRangeRequestsIncludingVideoOriginal(t *testing.T) {
	m := newMedia()
	a := openSource(t, t.TempDir(), "android", m)
	p := create(t, a, "Грузия", "Аня")
	original := make([]byte, 3<<20+12345)
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range original {
		original[i] = byte(rng.Uint32())
	}
	clip := video("VID_1", day, 15_000)
	clip.MIME = "video/quicktime"
	m.add(clip, original)
	m.add(photo("IMG_1", day), []byte("jpeg"))
	shared := share(t, a, p)
	movie, img := shared["VID_1"], shared["IMG_1"]
	b := memberOf(t, a, p)

	resp, body := b.get(blobPath(movie, node.T3))
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, original) {
		t.Fatalf("full original: status %d, %d bytes, equal %v", resp.StatusCode, len(body), bytes.Equal(body, original))
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" || resp.Header.Get("Content-Type") != "video/quicktime" ||
		!strings.Contains(resp.Header.Get("Cache-Control"), "immutable") || resp.Header.Get("ETag") == "" {
		t.Fatalf("full original headers %v", resp.Header)
	}
	etag := resp.Header.Get("ETag")

	size := len(original)
	for _, c := range []struct {
		header     string
		start, end int
	}{
		{"bytes=0-0", 0, 1},
		{"bytes=100-199", 100, 200},
		{"bytes=1048000-1049000", 1048000, 1049001},
		{"bytes=2000000-", 2000000, size},
		{"bytes=-4096", size - 4096, size},
		{fmt.Sprintf("bytes=%d-%d", size-10, size+1000), size - 10, size},
	} {
		resp, body := b.get(blobPath(movie, node.T3), "Range", c.header)
		wantRange := fmt.Sprintf("bytes %d-%d/%d", c.start, c.end-1, size)
		if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Range") != wantRange || !bytes.Equal(body, original[c.start:c.end]) {
			t.Fatalf("%s: status %d, Content-Range %q (want %q), %d bytes, equal %v",
				c.header, resp.StatusCode, resp.Header.Get("Content-Range"), wantRange, len(body), bytes.Equal(body, original[c.start:c.end]))
		}
	}

	resp, body = b.get(blobPath(movie, node.T3), "Range", "bytes=10-19,3000000-3000009")
	mt, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusPartialContent || err != nil || mt != "multipart/byteranges" {
		t.Fatalf("multiple ranges: status %d, Content-Type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	parts := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for _, want := range [][]byte{original[10:20], original[3000000:3000010]} {
		part, err := parts.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(part)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("multiple ranges: part %x, want %x", got, want)
		}
	}

	resp, body = b.get(blobPath(movie, node.T3), "Range", "bytes=100-199", "If-Range", etag)
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, original[100:200]) {
		t.Fatalf("If-Range with current ETag: status %d", resp.StatusCode)
	}
	if resp, _ := b.get(blobPath(movie, node.T3), "Range", fmt.Sprintf("bytes=%d-", size)); resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("range past the end: status %d", resp.StatusCode)
	}

	thumb := rendition("IMG_1", node.T1)
	resp, body = b.get(blobPath(img, node.T1), "Range", "bytes=3-")
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, thumb[3:]) {
		t.Fatalf("T1 range: status %d, body %q", resp.StatusCode, body)
	}
}

func TestOnlyOwnPublishedAssetsAreServed(t *testing.T) {
	ma, mb := newMedia(), newMedia()
	a, bn := openSource(t, t.TempDir(), "ios", ma), openSource(t, t.TempDir(), "android", mb)
	p := create(t, a, "Грузия", "Аня")
	join(t, bn, inviteTo(t, a, p), "Боря")
	ma.add(photo("IMG_1", day), []byte("jpeg"))
	ma.add(photo("IMG_2", day), []byte("jpeg"))
	shared := share(t, a, p)
	mine, removed := shared["IMG_1"], shared["IMG_2"]
	decide(t, a, p.ID, removed.AssetID, false)
	mb.add(photo("PXL_1", day), []byte("jpeg"))
	theirs := share(t, bn, p)["PXL_1"]
	eventually(t, "a learns the asset of b", func() bool { return a.Stored(p.ID, theirs.AssetID, theirs.Revision, node.T1) })

	c := memberOf(t, a, p)
	if resp, _ := c.get(blobPath(mine, node.T1)); resp.StatusCode != http.StatusOK {
		t.Fatalf("own asset: status %d", resp.StatusCode)
	}
	oldRevision := mine
	oldRevision.Revision = []byte("rev:before edit")
	unknown := mine
	unknown.AssetID = []byte("never published")
	for name, path := range map[string]string{
		"unknown asset":        blobPath(unknown, node.T1),
		"other revision":       blobPath(oldRevision, node.T1),
		"removed asset":        blobPath(removed, node.T1),
		"asset of other owner": blobPath(theirs, node.T1),
		"tier 0":               blobPath(mine, 0),
		"tier 4":               blobPath(mine, 4),
		"tier name":            fmt.Sprintf("/blob/%x/%x/T1", mine.AssetID, mine.Revision),
		"asset not hex":        fmt.Sprintf("/blob/zz/%x/1", mine.Revision),
		"revision not hex":     fmt.Sprintf("/blob/%x/zz/1", mine.AssetID),
	} {
		if resp, _ := c.get(path); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", name, resp.StatusCode)
		}
	}
}

func TestMembersFetchThumbnailsInBackground(t *testing.T) {
	ma, mb := newMedia(), newMedia()
	a, b := openSource(t, t.TempDir(), "ios", ma), openSource(t, t.TempDir(), "android", mb)
	p := create(t, a, "Грузия", "Аня")
	for i := range 12 {
		ma.add(photo(fmt.Sprintf("IMG_%d", i), day), []byte("jpeg"))
	}
	var ofA []op.AssetMeta
	for _, m := range share(t, a, p) {
		ofA = append(ofA, m)
	}
	join(t, b, inviteTo(t, a, p), "Боря")
	mb.add(photo("PXL_1", day), []byte("jpeg"))
	ofB := share(t, b, p)["PXL_1"]

	stored := func(n *node.Node, assets ...op.AssetMeta) func() bool {
		return func() bool {
			for _, m := range assets {
				if !n.Stored(p.ID, m.AssetID, m.Revision, node.T1) {
					return false
				}
			}
			return true
		}
	}
	eventually(t, "b stores T1 of all assets of a", stored(b, ofA...))
	eventually(t, "a stores T1 of the asset of b", stored(a, ofB))

	ma.add(photo("IMG_later", day), []byte("jpeg"))
	later := share(t, a, p)["IMG_later"]
	eventually(t, "b stores T1 of an asset added later", stored(b, later))

	if r, reads := ma.calls(); r[node.T1] != len(ofA)+1 || len(r) != 1 || reads != 0 {
		t.Fatalf("a made renditions %v and read originals %d times; want only T1 of each asset once", r, reads)
	}
	for _, m := range append(ofA, later) {
		if b.Stored(p.ID, m.AssetID, m.Revision, node.T2) {
			t.Fatalf("b stored T2 of %s without a request", m.OriginalName)
		}
	}

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	f, _, err := b.Blob(context.Background(), p.ID, later.AssetID, later.Revision, node.T1)
	if err != nil {
		t.Fatalf("T1 with the owner offline: %v", err)
	}
	defer f.Close()
	if got, err := io.ReadAll(f); err != nil || !bytes.Equal(got, rendition("IMG_later", node.T1)) {
		t.Fatalf("T1 with the owner offline: %q, %v", got, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, _, err := b.Blob(ctx, p.ID, later.AssetID, later.Revision, node.T2); !errors.Is(err, node.ErrUnavailable) {
		t.Fatalf("T2 with the owner offline: err = %v, want ErrUnavailable", err)
	}
}

func TestBlobForUIFromLocalStoreOrOwner(t *testing.T) {
	ma := newMedia()
	a, b := openSource(t, t.TempDir(), "ios", ma), open(t, t.TempDir(), "android")
	p := create(t, a, "Грузия", "Аня")
	ma.add(photo("IMG_1", day), []byte("jpeg"))
	img := share(t, a, p)["IMG_1"]
	join(t, b, inviteTo(t, a, p), "Боря")
	eventually(t, "b learns the asset", func() bool { return b.Stored(p.ID, img.AssetID, img.Revision, node.T1) })

	read := func(n *node.Node, tier int) {
		t.Helper()
		f, h, err := n.Blob(context.Background(), p.ID, img.AssetID, img.Revision, tier)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		got, err := io.ReadAll(f)
		want := rendition("IMG_1", tier)
		if err != nil || !bytes.Equal(got, want) || node.ETag(h) != contentETag(want) {
			t.Fatalf("T%d: %q, %v, ETag %s", tier, got, err, node.ETag(h))
		}
	}
	for range 2 {
		read(b, node.T2)
		read(b, node.T1)
		read(a, node.T2)
	}
	if r, _ := ma.calls(); r[node.T1] != 1 || r[node.T2] != 1 {
		t.Fatalf("a made renditions %v, want each tier once", r)
	}

	unknown := img
	unknown.Revision = []byte("other revision")
	for name, c := range map[string]struct {
		pile, asset, revision []byte
		tier                  int
		want                  error
	}{
		"unknown pile":     {[]byte("no such pile"), img.AssetID, img.Revision, node.T1, node.ErrUnknownPile},
		"unknown revision": {p.ID, unknown.AssetID, unknown.Revision, node.T1, node.ErrUnknownAsset},
		"original":         {p.ID, img.AssetID, img.Revision, node.T3, node.ErrUnknownAsset},
	} {
		if _, _, err := b.Blob(context.Background(), c.pile, c.asset, c.revision, c.tier); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
}

type recorder struct {
	mu       sync.Mutex
	requests []string
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.requests = append(r.requests, req.Method+" "+req.URL.Path+" "+strconv.Itoa(len(body)))
	r.mu.Unlock()
	if req.Method == http.MethodPost && req.URL.Path == "/vv" {
		<-req.Context().Done()
		return
	}
	if req.URL.Path == "/vv" {
		b, _ := cbor.Marshal(vv.VV{})
		w.Write(b)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *recorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.requests...)
}

func TestSourceSendsBlobsOnlyInResponseToRequests(t *testing.T) {
	m := newMedia()
	a := openSource(t, t.TempDir(), "ios", m)
	p := create(t, a, "Грузия", "Аня")
	for i := range 5 {
		m.add(photo(fmt.Sprintf("IMG_%d", i), day), []byte("jpeg"))
	}
	first := share(t, a, p)["IMG_0"]

	i, err := invite.Parse(inviteTo(t, a, p))
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	srv := &http.Server{Handler: rec}
	go srv.Serve(peer.NewListener(ln, key, func(id []byte) ([]byte, bool) { return i.Secret, bytes.Equal(id, i.PileID) }))
	t.Cleanup(func() { srv.Close() })

	c := memberOf(t, a, p)
	port := netip.MustParseAddrPort(ln.Addr().String()).Port()
	req, err := http.NewRequest(http.MethodPost, "http://"+c.addr+"/announce?port="+strconv.Itoa(int(port)), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	eventually(t, "a pulls from the announced member", func() bool { return len(rec.seen()) >= 2 })
	time.Sleep(3 * time.Second)

	for _, r := range rec.seen() {
		if !strings.HasPrefix(r, "GET /vv ") && !strings.HasPrefix(r, "POST /vv ") && !strings.HasPrefix(r, "POST /announce 0") {
			t.Errorf("a sent an unexpected request to the member: %s", r)
		}
	}
	if r, reads := m.calls(); r[node.T2] != 0 || reads != 0 {
		t.Fatalf("a made renditions %v and read originals %d times while nobody asked; want no T2 and no originals", r, reads)
	}

	resp2, body := c.get(blobPath(first, node.T1))
	if resp2.StatusCode != http.StatusOK || !bytes.Equal(body, rendition("IMG_0", node.T1)) {
		t.Fatalf("requested T1: status %d, %q", resp2.StatusCode, body)
	}
	if r, _ := m.calls(); r[node.T1] != 5 || len(r) != 1 {
		t.Fatalf("a made renditions %v after one request, want T1 of each asset once", r)
	}
}

func TestOwnAssetsAreServedAfterRestart(t *testing.T) {
	m := newMedia()
	dir := t.TempDir()
	a := openSource(t, dir, "ios", m)
	p := create(t, a, "Грузия", "Аня")
	m.add(photo("IMG_1", day), []byte("jpeg"))
	img := share(t, a, p)["IMG_1"]
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	a = openSource(t, dir, "ios", m)
	resp, body := memberOf(t, a, p).get(blobPath(img, node.T1))
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, rendition("IMG_1", node.T1)) {
		t.Fatalf("T1 after restart: status %d, body %q", resp.StatusCode, body)
	}
}

func TestOwnAssetWithoutRenditionMakerIsUnavailable(t *testing.T) {
	a := openWith(t, t.TempDir(), "ios", shelf(photo("IMG_1", day)))
	b := open(t, t.TempDir(), "android")
	p := create(t, a, "Грузия", "Аня")
	img := share(t, a, p)["IMG_1"]
	join(t, b, inviteTo(t, a, p), "Боря")
	waitAssets(t, p.ID, idSet(img.AssetID), b)

	if _, _, err := a.Blob(context.Background(), p.ID, img.AssetID, img.Revision, node.T1); !errors.Is(err, node.ErrUnavailable) {
		t.Fatalf("own T1 without a rendition maker: err = %v, want ErrUnavailable", err)
	}
	if resp, _ := memberOf(t, a, p).get(blobPath(img, node.T1)); resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		t.Fatalf("member asked for T1: status %d, want a server error", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, _, err := b.Blob(ctx, p.ID, img.AssetID, img.Revision, node.T1); !errors.Is(err, node.ErrUnavailable) {
		t.Fatalf("T1 from an owner without a rendition maker: err = %v, want ErrUnavailable", err)
	}
}

func TestMembersJoinedThroughOneInviterFetchEachOthersThumbnails(t *testing.T) {
	mb, mc := newMedia(), newMedia()
	dirA := t.TempDir()
	a := open(t, dirA, "ios")
	b, c := openSource(t, t.TempDir(), "android", mb), openSource(t, t.TempDir(), "ios", mc)
	p := create(t, a, "Грузия", "Аня")
	inv := inviteTo(t, a, p)
	join(t, b, inv, "Боря")
	join(t, c, inv, "Вера")
	mb.add(photo("PXL_B", day), []byte("jpeg"))
	mc.add(photo("IMG_C", day), []byte("jpeg"))
	ofB, ofC := share(t, b, p)["PXL_B"], share(t, c, p)["IMG_C"]

	eventually(t, "c stores T1 of the asset of b", func() bool { return c.Stored(p.ID, ofB.AssetID, ofB.Revision, node.T1) })
	eventually(t, "b stores T1 of the asset of c", func() bool { return b.Stored(p.ID, ofC.AssetID, ofC.Revision, node.T1) })

	var addrs [][]byte
	for _, n := range []*node.Node{b, c} {
		i, err := invite.Parse(inviteTo(t, n, p))
		if err != nil {
			t.Fatal(err)
		}
		addrs = append(addrs, []byte(i.Addrs[0].String()))
	}
	secret := secretOf(t, a, p)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	for _, raw := range logOf(t, dirA, p, secret) {
		for _, addr := range addrs {
			if bytes.Contains(raw, addr) {
				t.Fatalf("member address %s leaked into an op", addr)
			}
		}
	}
}

func TestDisplayCachedOnMemberIsEvictedOverBudgetAndFetchedAgain(t *testing.T) {
	ma := newMedia()
	a := openSource(t, t.TempDir(), "ios", ma)
	dir := t.TempDir()
	want := rendition("IMG_1", node.T2)
	b, err := node.Open(node.Config{
		Dir:      dir,
		Keystore: device.File(filepath.Join(dir, "device.key")),
		Platform: "android",
		Addrs:    loopback,
		Budget:   int64(len(want)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	p := create(t, a, "Грузия", "Аня")
	ma.add(photo("IMG_1", day), []byte("jpeg"))
	ma.add(photo("IMG_2", "2026-07-03T10:00:00Z"), []byte("jpeg"))
	shared := share(t, a, p)
	img, later := shared["IMG_1"], shared["IMG_2"]
	join(t, b, inviteTo(t, a, p), "Боря")
	eventually(t, "b stores T1", func() bool { return b.Stored(p.ID, img.AssetID, img.Revision, node.T1) })

	if _, ok := b.Cached(p.ID, img.AssetID, img.Revision, node.T2); ok {
		t.Fatal("T2 cached before any request")
	}
	read := func(m op.AssetMeta) []byte {
		t.Helper()
		f, _, err := b.Blob(context.Background(), p.ID, m.AssetID, m.Revision, node.T2)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		got, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	read(img)
	if size, ok := b.Cached(p.ID, img.AssetID, img.Revision, node.T2); !ok || size != int64(len(want)) {
		t.Fatalf("cached T2: %d, %v; want %d bytes", size, ok, len(want))
	}

	read(later)
	if _, ok := b.Cached(p.ID, img.AssetID, img.Revision, node.T2); ok {
		t.Fatal("earlier T2 cached over the budget")
	}
	if _, ok := b.Cached(p.ID, later.AssetID, later.Revision, node.T2); !ok {
		t.Fatal("later T2 is not cached")
	}
	if _, ok := b.Cached(p.ID, img.AssetID, img.Revision, node.T1); !ok {
		t.Fatal("T1 evicted together with T2")
	}
	if got := read(img); !bytes.Equal(got, want) {
		t.Fatalf("T2 after eviction: %q", got)
	}
	if _, ok := b.Cached(p.ID, later.AssetID, later.Revision, node.T2); !ok {
		t.Fatal("later T2 evicted by an earlier one")
	}
}

func TestOriginalIsStreamedFromOwnerWithRanges(t *testing.T) {
	ma := newMedia()
	a, b := openSource(t, t.TempDir(), "ios", ma), open(t, t.TempDir(), "android")
	p := create(t, a, "Грузия", "Аня")
	original := make([]byte, 2<<20+777)
	rng := rand.New(rand.NewPCG(3, 4))
	for i := range original {
		original[i] = byte(rng.Uint32())
	}
	clip := video("VID_1", day, 15_000)
	clip.MIME = "video/quicktime"
	ma.add(clip, original)
	movie := share(t, a, p)["VID_1"]
	join(t, b, inviteTo(t, a, p), "Боря")
	waitAssets(t, p.ID, idSet(movie.AssetID), b)

	serve := func(n *node.Node, assetID, revision []byte) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n.ServeOriginal(w, r, p.ID, assetID, revision)
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	get := func(url string, header ...string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
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

	for name, url := range map[string]string{
		"member": serve(b, movie.AssetID, movie.Revision),
		"owner":  serve(a, movie.AssetID, movie.Revision),
	} {
		resp, body := get(url)
		if resp.StatusCode != http.StatusOK || !bytes.Equal(body, original) || resp.Header.Get("Content-Type") != "video/quicktime" ||
			resp.Header.Get("Accept-Ranges") != "bytes" || resp.Header.Get("ETag") == "" {
			t.Fatalf("%s: full original: status %d, %d bytes, headers %v", name, resp.StatusCode, len(body), resp.Header)
		}
		size := len(original)
		for _, c := range []struct {
			header     string
			start, end int
		}{
			{"bytes=0-1", 0, 2},
			{"bytes=1048570-1048580", 1048570, 1048581},
			{"bytes=-100", size - 100, size},
		} {
			resp, body := get(url, "Range", c.header)
			wantRange := fmt.Sprintf("bytes %d-%d/%d", c.start, c.end-1, size)
			if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Range") != wantRange || !bytes.Equal(body, original[c.start:c.end]) {
				t.Fatalf("%s %s: status %d, Content-Range %q, %d bytes", name, c.header, resp.StatusCode, resp.Header.Get("Content-Range"), len(body))
			}
		}
		if resp, _ := get(url, "Range", fmt.Sprintf("bytes=%d-", size)); resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
			t.Fatalf("%s: range past the end: status %d", name, resp.StatusCode)
		}
	}

	other := movie
	other.Revision = []byte("other revision")
	if resp, _ := get(serve(b, other.AssetID, other.Revision)); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown revision: status %d", resp.StatusCode)
	}
	url := serve(b, movie.AssetID, movie.Revision)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if resp, _ := get(url, "Range", "bytes=0-1"); resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("owner offline: status %d", resp.StatusCode)
	}
}
