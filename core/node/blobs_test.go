package node_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
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
	originals  map[string][]byte
	renditions map[int]int
	reads      int
}

func newMedia() *media {
	return &media{originals: map[string][]byte{}, renditions: map[int]int{}}
}

func (m *media) add(local string, original []byte) op.AssetMeta {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.originals[local] = original
	return op.AssetMeta{
		AssetID:      []byte("asset:" + local),
		Revision:     []byte("rev:" + local),
		Kind:         op.KindPhoto,
		MIME:         "image/jpeg",
		OriginalName: local + ".jpg",
		SizeBytes:    uint64(len(original)),
	}
}

func rendition(local string, tier int) []byte {
	return []byte(fmt.Sprintf("T%d of %s", tier, local))
}

func (m *media) Rendition(local string, tier int) ([]byte, error) {
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

func (m *media) LocalID(assetID []byte) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	local, ok := strings.CutPrefix(string(assetID), "asset:")
	_, known := m.originals[local]
	return local, ok && known
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

func openSource(t *testing.T, platform string, m *media) *node.Node {
	t.Helper()
	dir := t.TempDir()
	n, err := node.Open(node.Config{
		Dir:        dir,
		Keystore:   device.File(filepath.Join(dir, "device.key")),
		Platform:   platform,
		Addrs:      loopback,
		Renditions: m,
		LocalID:    m.LocalID,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	return n
}

func publish(t *testing.T, n *node.Node, p node.Pile, c op.Change) {
	t.Helper()
	if err := n.Publish(p.ID, c); err != nil {
		t.Fatal(err)
	}
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
	a := openSource(t, "ios", m)
	p := create(t, a, "Грузия", "Аня")
	photo := m.add("IMG_1", []byte("jpeg original"))
	publish(t, a, p, op.AssetAdd{Assets: []op.AssetMeta{photo}})
	b := memberOf(t, a, p)
	if r, _ := m.calls(); len(r) != 0 {
		t.Fatalf("renditions made before any request: %v", r)
	}

	for _, tier := range []int{node.T1, node.T2} {
		want := rendition("IMG_1", tier)
		for range 2 {
			resp, body := b.get(blobPath(photo, tier))
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
		resp, body := b.get(blobPath(photo, tier), "If-None-Match", contentETag(want))
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
	a := openSource(t, "android", m)
	p := create(t, a, "Грузия", "Аня")
	original := make([]byte, 3<<20+12345)
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range original {
		original[i] = byte(rng.Uint32())
	}
	video := m.add("VID_1", original)
	video.Kind, video.MIME = op.KindVideo, "video/quicktime"
	photo := m.add("IMG_1", []byte("jpeg"))
	publish(t, a, p, op.AssetAdd{Assets: []op.AssetMeta{video, photo}})
	b := memberOf(t, a, p)

	resp, body := b.get(blobPath(video, node.T3))
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
		resp, body := b.get(blobPath(video, node.T3), "Range", c.header)
		wantRange := fmt.Sprintf("bytes %d-%d/%d", c.start, c.end-1, size)
		if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Range") != wantRange || !bytes.Equal(body, original[c.start:c.end]) {
			t.Fatalf("%s: status %d, Content-Range %q (want %q), %d bytes, equal %v",
				c.header, resp.StatusCode, resp.Header.Get("Content-Range"), wantRange, len(body), bytes.Equal(body, original[c.start:c.end]))
		}
	}

	resp, body = b.get(blobPath(video, node.T3), "Range", "bytes=10-19,3000000-3000009")
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

	resp, body = b.get(blobPath(video, node.T3), "Range", "bytes=100-199", "If-Range", etag)
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, original[100:200]) {
		t.Fatalf("If-Range with current ETag: status %d", resp.StatusCode)
	}
	if resp, _ := b.get(blobPath(video, node.T3), "Range", fmt.Sprintf("bytes=%d-", size)); resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("range past the end: status %d", resp.StatusCode)
	}

	thumb := rendition("IMG_1", node.T1)
	resp, body = b.get(blobPath(photo, node.T1), "Range", "bytes=3-")
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, thumb[3:]) {
		t.Fatalf("T1 range: status %d, body %q", resp.StatusCode, body)
	}
}

func TestOnlyOwnPublishedAssetsAreServed(t *testing.T) {
	ma, mb := newMedia(), newMedia()
	a, bn := openSource(t, "ios", ma), openSource(t, "android", mb)
	p := create(t, a, "Грузия", "Аня")
	join(t, bn, inviteTo(t, a, p), "Боря")
	mine := ma.add("IMG_1", []byte("jpeg"))
	removed := ma.add("IMG_2", []byte("jpeg"))
	forgotten := mine
	forgotten.AssetID = []byte("asset:deleted from the phone")
	publish(t, a, p, op.AssetAdd{Assets: []op.AssetMeta{mine, removed, forgotten}})
	publish(t, a, p, op.AssetRemove{AssetIDs: [][]byte{removed.AssetID}})
	theirs := mb.add("PXL_1", []byte("jpeg"))
	publish(t, bn, p, op.AssetAdd{Assets: []op.AssetMeta{theirs}})
	eventually(t, "a learns the asset of b", func() bool { return a.Stored(p.ID, theirs.AssetID, theirs.Revision, node.T1) })

	c := memberOf(t, a, p)
	if resp, _ := c.get(blobPath(mine, node.T1)); resp.StatusCode != http.StatusOK {
		t.Fatalf("own asset: status %d", resp.StatusCode)
	}
	oldRevision := mine
	oldRevision.Revision = []byte("rev:before edit")
	unknown := mine
	unknown.AssetID = []byte("asset:never published")
	for name, path := range map[string]string{
		"unknown asset":        blobPath(unknown, node.T1),
		"other revision":       blobPath(oldRevision, node.T1),
		"removed asset":        blobPath(removed, node.T1),
		"asset of other owner": blobPath(theirs, node.T1),
		"no local file":        blobPath(forgotten, node.T1),
		"no local original":    blobPath(forgotten, node.T3),
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
	a, b := openSource(t, "ios", ma), openSource(t, "android", mb)
	p := create(t, a, "Грузия", "Аня")
	var ofA []op.AssetMeta
	for i := range 12 {
		ofA = append(ofA, ma.add(fmt.Sprintf("IMG_%d", i), []byte("jpeg")))
	}
	publish(t, a, p, op.AssetAdd{Assets: ofA})
	join(t, b, inviteTo(t, a, p), "Боря")
	ofB := mb.add("PXL_1", []byte("jpeg"))
	publish(t, b, p, op.AssetAdd{Assets: []op.AssetMeta{ofB}})

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

	later := ma.add("IMG_later", []byte("jpeg"))
	publish(t, a, p, op.AssetAdd{Assets: []op.AssetMeta{later}})
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
	a, b := openSource(t, "ios", ma), open(t, t.TempDir(), "android")
	p := create(t, a, "Грузия", "Аня")
	photo := ma.add("IMG_1", []byte("jpeg"))
	publish(t, a, p, op.AssetAdd{Assets: []op.AssetMeta{photo}})
	join(t, b, inviteTo(t, a, p), "Боря")
	eventually(t, "b learns the asset", func() bool { return b.Stored(p.ID, photo.AssetID, photo.Revision, node.T1) })

	read := func(n *node.Node, tier int) {
		t.Helper()
		f, h, err := n.Blob(context.Background(), p.ID, photo.AssetID, photo.Revision, tier)
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

	unknown := photo
	unknown.Revision = []byte("rev:other")
	for name, c := range map[string]struct {
		pile, asset, revision []byte
		tier                  int
		want                  error
	}{
		"unknown pile":     {[]byte("no such pile"), photo.AssetID, photo.Revision, node.T1, node.ErrUnknownPile},
		"unknown revision": {p.ID, unknown.AssetID, unknown.Revision, node.T1, node.ErrUnknownAsset},
		"original":         {p.ID, photo.AssetID, photo.Revision, node.T3, node.ErrUnknownAsset},
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
	a := openSource(t, "ios", m)
	p := create(t, a, "Грузия", "Аня")
	var assets []op.AssetMeta
	for i := range 5 {
		assets = append(assets, m.add(fmt.Sprintf("IMG_%d", i), []byte("jpeg")))
	}
	publish(t, a, p, op.AssetAdd{Assets: assets})

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
	if r, reads := m.calls(); len(r) != 0 || reads != 0 {
		t.Fatalf("a made renditions %v and read originals %d times while nobody asked", r, reads)
	}

	resp2, body := c.get(blobPath(assets[0], node.T1))
	if resp2.StatusCode != http.StatusOK || !bytes.Equal(body, rendition("IMG_0", node.T1)) {
		t.Fatalf("requested T1: status %d, %q", resp2.StatusCode, body)
	}
	if r, _ := m.calls(); r[node.T1] != 1 || len(r) != 1 {
		t.Fatalf("a made renditions %v after one request, want one T1", r)
	}
}
