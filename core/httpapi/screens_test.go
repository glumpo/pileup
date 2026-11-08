package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/httpapi"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

type screen struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	Address   string  `json:"address"`
	Available bool    `json:"available"`
	Reason    *string `json:"reason"`
}

type room struct {
	mu       sync.Mutex
	browsing map[string]node.DiscoveryHandler
}

func (r *room) Advertise(service, name string, port int, txt []byte) error { return nil }

func (r *room) Browse(service string, h node.DiscoveryHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.browsing[service] = h
	return nil
}

func (r *room) Stop() error { return nil }

func (r *room) handler(t *testing.T, service string) node.DiscoveryHandler {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.browsing[service]
	if h == nil {
		t.Fatalf("%s is not browsed", service)
	}
	return h
}

func txt(entries ...string) []byte {
	var b []byte
	for _, e := range entries {
		b = append(b, byte(len(e)))
		b = append(b, e...)
	}
	return b
}

func startIn(t *testing.T, platform string) (client, *room) {
	t.Helper()
	dir := t.TempDir()
	r := &room{browsing: map[string]node.DiscoveryHandler{}}
	n, err := node.Open(node.Config{
		Dir:       dir,
		Keystore:  device.File(filepath.Join(dir, "device.key")),
		Platform:  platform,
		Addrs:     func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("127.0.0.1")} },
		Discovery: r,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	srv := httptest.NewServer(httpapi.WithNode("secret", n, show.New(n, show.DefaultBudget)))
	t.Cleanup(srv.Close)
	return client{t: t, url: srv.URL}, r
}

func (c client) screens() []screen {
	c.t.Helper()
	var out []screen
	c.do("GET", "/screens", nil, http.StatusOK, &out)
	return out
}

func livingRoom(t *testing.T, r *room) {
	t.Helper()
	r.handler(t, "_googlecast._tcp").Found("Google-TV-4f1a9c", "192.168.1.30", 8009, txt("id=4f1a9c", "md=Google TV", "fn=Гостиная"))
	r.handler(t, "_airplay._tcp").Found("Спальня", "192.168.1.31", 7000, txt("model=AppleTV11,1"))
}

func withoutIDs(t *testing.T, ss []screen) []screen {
	t.Helper()
	seen := map[string]bool{}
	out := []screen{}
	for _, s := range ss {
		if s.ID == "" || seen[s.ID] {
			t.Fatalf("screen ids must be unique and non-empty: %+v", ss)
		}
		seen[s.ID] = true
		s.ID = ""
		out = append(out, s)
	}
	return out
}

func reason(s string) *string { return &s }

func TestScreensRequireToken(t *testing.T) {
	c, _ := startIn(t, "android")
	c.call("GET", "/screens", "wrong", nil, http.StatusUnauthorized, nil)
}

func TestNoScreensIsAnEmptyList(t *testing.T) {
	c, _ := startIn(t, "android")
	if got := c.screens(); got == nil || len(got) != 0 {
		t.Fatalf("screens = %#v, want []", got)
	}
}

func TestAndroidHostListsAppleTVWithExplanation(t *testing.T) {
	c, r := startIn(t, "android")
	livingRoom(t, r)
	want := []screen{
		{Name: "Гостиная", Kind: "cast", Address: "192.168.1.30:8009", Available: true},
		{Name: "Спальня", Kind: "airplay", Address: "192.168.1.31:7000", Available: false,
			Reason: reason("Показ на Apple TV доступен только с iPhone. Запустите показ с iPhone участника.")},
	}
	if got := withoutIDs(t, c.screens()); !reflect.DeepEqual(got, want) {
		t.Fatalf("screens = %+v, want %+v", got, want)
	}
}

func TestIPhoneHostCanShowOnAppleTV(t *testing.T) {
	c, r := startIn(t, "ios")
	livingRoom(t, r)
	want := []screen{
		{Name: "Гостиная", Kind: "cast", Address: "192.168.1.30:8009", Available: true},
		{Name: "Спальня", Kind: "airplay", Address: "192.168.1.31:7000", Available: true},
	}
	if got := withoutIDs(t, c.screens()); !reflect.DeepEqual(got, want) {
		t.Fatalf("screens = %+v, want %+v", got, want)
	}
}

func TestScreenThatLeftIsNotListed(t *testing.T) {
	c, r := startIn(t, "ios")
	livingRoom(t, r)
	r.handler(t, "_googlecast._tcp").Found("Chromecast-77", "192.168.1.32", 8009, nil)
	r.handler(t, "_googlecast._tcp").Lost("Google-TV-4f1a9c")
	r.handler(t, "_airplay._tcp").Lost("Спальня")
	want := []screen{{Name: "Chromecast-77", Kind: "cast", Address: "192.168.1.32:8009", Available: true}}
	if got := withoutIDs(t, c.screens()); !reflect.DeepEqual(got, want) {
		t.Fatalf("screens = %+v, want %+v", got, want)
	}
}
