package node_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/node"
)

func TestRenditionIsKeptWhenMemberGivesUp(t *testing.T) {
	m := newMedia()
	a := openSource(t, t.TempDir(), "ios", m)
	p := create(t, a, "Грузия", "Аня")
	m.add(photo("IMG_1", day), []byte("jpeg"))
	img := share(t, a, p)["IMG_1"]
	hold := make(chan chan struct{})
	m.mu.Lock()
	m.hold = hold
	m.mu.Unlock()
	b := memberOf(t, a, p)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+b.addr+blobPath(img, node.T2), nil)
	if err != nil {
		t.Fatal(err)
	}
	gaveUp := make(chan error, 1)
	go func() {
		resp, err := b.http.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		gaveUp <- err
	}()
	release := <-hold
	cancel()
	if err := <-gaveUp; !errors.Is(err, context.Canceled) {
		t.Fatalf("request that gave up: err = %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	m.mu.Lock()
	m.hold = nil
	m.mu.Unlock()
	close(release)

	eventually(t, "the owner caches T2 the member gave up on", func() bool { return a.Stored(p.ID, img.AssetID, img.Revision, node.T2) })
	resp, body := b.get(blobPath(img, node.T2))
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, rendition("IMG_1", node.T2)) {
		t.Fatalf("T2 asked again: status %d, %q", resp.StatusCode, body)
	}
	if r, _ := m.calls(); r[node.T2] != 1 {
		t.Fatalf("T2 rendered %d times, want once", r[node.T2])
	}
}

type turns struct {
	*media
	started chan string
	gate    chan struct{}
}

func (q *turns) Rendition(local string, tier int) ([]byte, error) {
	q.started <- fmt.Sprintf("T%d of %s", tier, local)
	<-q.gate
	return q.media.Rendition(local, tier)
}

func TestOwnerRendersWaitingDisplayBeforeWaitingThumbnails(t *testing.T) {
	q := &turns{media: newMedia(), started: make(chan string, 16), gate: make(chan struct{})}
	dir := t.TempDir()
	a, err := node.Open(node.Config{Dir: dir, Keystore: device.File(filepath.Join(dir, "device.key")), Platform: "ios", Addrs: loopback, Media: q, Renditions: q})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	p := create(t, a, "Грузия", "Аня")
	for i := range 4 {
		q.add(photo(fmt.Sprintf("IMG_%d", i), day), []byte("jpeg"))
	}
	shared := share(t, a, p)
	first := <-q.started
	b := memberOf(t, a, p)

	done := make(chan struct{})
	ask := func(path string) {
		go func() {
			resp, err := b.http.Get("http://" + b.addr + path)
			if err == nil {
				resp.Body.Close()
			}
			done <- struct{}{}
		}()
	}
	for _, m := range shared {
		ask(blobPath(m, node.T1))
	}
	time.Sleep(300 * time.Millisecond)
	ask(blobPath(shared["IMG_0"], node.T2))
	time.Sleep(300 * time.Millisecond)

	q.gate <- struct{}{}
	next := <-q.started
	close(q.gate)
	for range len(shared) + 1 {
		<-done
	}
	if next != "T2 of IMG_0" {
		t.Fatalf("after %s the owner rendered %s, want T2 of IMG_0 before the waiting thumbnails", first, next)
	}
}

type hanging struct {
	*media
	file    string
	release chan struct{}
	free    func()

	mu               sync.Mutex
	calls            map[string]int
	running, started int
}

func newHanging(t *testing.T, file string) *hanging {
	h := &hanging{media: newMedia(), file: file, release: make(chan struct{}), calls: map[string]int{}}
	h.free = sync.OnceFunc(func() { close(h.release) })
	t.Cleanup(h.free)
	return h
}

func (h *hanging) Rendition(local string, tier int) ([]byte, error) {
	h.mu.Lock()
	h.calls[local]++
	h.running++
	h.started++
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.running--
		h.mu.Unlock()
	}()
	if h.file == "" || h.file == local {
		<-h.release
	}
	return h.media.Rendition(local, tier)
}

func (h *hanging) count() (calls map[string]int, running, started int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return maps.Clone(h.calls), h.running, h.started
}

func openHanging(t *testing.T, h *hanging) *node.Node {
	t.Helper()
	dir := t.TempDir()
	n, err := node.Open(node.Config{Dir: dir, Keystore: device.File(filepath.Join(dir, "device.key")), Platform: "android", Addrs: loopback, Media: h, Renditions: h})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	t.Cleanup(h.free)
	return n
}

func TestStuckRenditionDoesNotBlockOtherFiles(t *testing.T) {
	t.Cleanup(node.SetRenderTimeout(300 * time.Millisecond))
	t.Cleanup(node.SetBrokenPause(500 * time.Millisecond))
	h := newHanging(t, "IMG_0")
	a := openHanging(t, h)
	p := create(t, a, "Грузия", "Аня")
	h.add(photo("IMG_0", day), []byte("broken video"))
	h.add(photo("IMG_1", day), []byte("jpeg"))
	shared := share(t, a, p)
	b := memberOf(t, a, p)
	eventually(t, "the broken file is being rendered", func() bool { c, _, _ := h.count(); return c["IMG_0"] == 1 })

	resp, body := b.get(blobPath(shared["IMG_1"], node.T2))
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, rendition("IMG_1", node.T2)) {
		t.Fatalf("T2 of another file while one rendering hangs: status %d, %q", resp.StatusCode, body)
	}
	eventually(t, "the stuck rendering is logged", logged(a, diag.Timeout, p, "", "render"))

	asked := func(what string) {
		t.Helper()
		if resp, _ := b.get(blobPath(shared["IMG_0"], node.T2)); resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("%s: T2 of the stuck file: status %d, want 500", what, resp.StatusCode)
		}
		if c, _, _ := h.count(); c["IMG_0"] != 1 {
			t.Fatalf("%s: the stuck file was rendered %d times, want no new native call while it hangs", what, c["IMG_0"])
		}
	}
	asked("right after the timeout")
	time.Sleep(time.Second)
	asked("after the pause, still hanging")

	h.free()
	eventually(t, "the stuck call returns", func() bool { _, running, _ := h.count(); return running == 0 })
	time.Sleep(time.Second)
	resp, body = b.get(blobPath(shared["IMG_0"], node.T2))
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, rendition("IMG_0", node.T2)) {
		t.Fatalf("T2 of the file after its call returned and the pause passed: status %d, %q", resp.StatusCode, body)
	}
}

func TestStuckRenditionsInFlightAreBounded(t *testing.T) {
	t.Cleanup(node.SetRenderTimeout(100 * time.Millisecond))
	h := newHanging(t, "")
	a := openHanging(t, h)
	p := create(t, a, "Грузия", "Аня")
	for i := range 8 {
		h.add(photo(fmt.Sprintf("IMG_%d", i), day), []byte("broken video"))
	}
	share(t, a, p)
	eventually(t, "renders go on past stuck ones", func() bool { _, running, _ := h.count(); return running == 3 })
	time.Sleep(time.Second)
	if _, running, started := h.count(); running != 3 || started != 3 {
		t.Fatalf("native renders in flight %d, started %d; want at most 3 while they hang", running, started)
	}
}

func closeWithin(t *testing.T, n *node.Node, d time.Duration) {
	t.Helper()
	closed := make(chan struct{})
	start := time.Now()
	go func() {
		n.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(d):
		t.Fatalf("Close still running after %v", d)
	}
	t.Logf("Close took %v", time.Since(start))
}

func TestCloseDoesNotWaitForQueuedRenders(t *testing.T) {
	q := &slowRenders{media: newMedia()}
	dir := t.TempDir()
	a, err := node.Open(node.Config{Dir: dir, Keystore: device.File(filepath.Join(dir, "device.key")), Platform: "android", Addrs: loopback, Media: q, Renditions: q})
	if err != nil {
		t.Fatal(err)
	}
	p := create(t, a, "Грузия", "Аня")
	for i := range 12 {
		q.add(photo(fmt.Sprintf("IMG_%d", i), day), []byte("jpeg"))
	}
	shared := share(t, a, p)
	b := memberOf(t, a, p)
	for _, m := range shared {
		go func() {
			resp, err := b.http.Get("http://" + b.addr + blobPath(m, node.T2))
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
	time.Sleep(500 * time.Millisecond)
	before := q.started.Load()
	closeWithin(t, a, time.Second)
	time.Sleep(time.Second)
	if started := q.started.Load() - before; started > 1 {
		t.Fatalf("%d renders started during and after Close, want at most the one already handed its turn", started)
	}
}

func TestCloseDoesNotWaitForStuckRender(t *testing.T) {
	h := newHanging(t, "IMG_0")
	a := openHanging(t, h)
	p := create(t, a, "Грузия", "Аня")
	h.add(photo("IMG_0", day), []byte("broken video"))
	share(t, a, p)
	eventually(t, "the broken file is being rendered", func() bool { _, running, _ := h.count(); return running == 1 })
	closeWithin(t, a, time.Second)
}

type slowRenders struct {
	*media
	started atomic.Int32
}

func (s *slowRenders) Rendition(local string, tier int) ([]byte, error) {
	s.started.Add(1)
	time.Sleep(300 * time.Millisecond)
	return s.media.Rendition(local, tier)
}
