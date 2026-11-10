package mobile_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/mobile"
)

type keystore struct {
	mu   sync.Mutex
	seed []byte
}

func (k *keystore) Load() ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return slices.Clone(k.seed), nil
}

func (k *keystore) Save(seed []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.seed = slices.Clone(seed)
	return nil
}

type library struct{ items string }

func (l library) Media(from, to int64) ([]byte, error) {
	return []byte(l.items), nil
}

func (l library) Rendition(localID string, tier int) ([]byte, error) {
	return []byte(fmt.Sprintf("T%d of %s", tier, localID)), nil
}

func (l library) OriginalSize(localID string) (int64, error) {
	return 0, errors.New("no originals")
}

func (l library) ReadOriginal(localID string, offset, length int64) ([]byte, error) {
	return nil, errors.New("no originals")
}

type network []string

func (n network) Addrs() (string, error) {
	b, err := json.Marshal([]string(n))
	return string(b), err
}

type lan struct {
	mu      sync.Mutex
	adverts []string
	stopped bool
}

func (l *lan) Advertise(service, name string, port int, txt []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.adverts = append(l.adverts, service)
	return nil
}

func (l *lan) Browse(service string, h mobile.DiscoveryHandler) error {
	if service == "_googlecast._tcp" {
		h.Found("Google-TV-4f1a9c", "192.168.1.30", 8009, append([]byte{byte(len("fn=Гостиная"))}, "fn=Гостиная"...))
	}
	return nil
}

func (l *lan) Stop() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopped = true
	return nil
}

type device struct {
	dir     string
	keys    *keystore
	media   library
	network network
	lan     *lan
}

func newDevice(t *testing.T) *device {
	return &device{dir: t.TempDir(), keys: &keystore{}, media: library{items: "[]"}, network: network{"192.168.1.20"}, lan: &lan{}}
}

func start(t *testing.T, d *device) {
	t.Helper()
	if err := mobile.Start(d.dir, "android", "1.4.0", d.keys, d.media, d.media, d.network, d.lan); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mobile.Stop() })
}

func call(t *testing.T, method, path, token string, body any) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://127.0.0.1:"+strconv.Itoa(mobile.Port())+path, r)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out
}

func health(t *testing.T, port int, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+"/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

type pile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func createPile(t *testing.T, name string) pile {
	t.Helper()
	code, body := call(t, http.MethodPost, "/piles", mobile.Token(), map[string]string{"name": name, "member": "Боря"})
	if code != http.StatusCreated {
		t.Fatalf("POST /piles: status = %d, body %s", code, body)
	}
	var p pile
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStartServesHealthOnLoopbackWithToken(t *testing.T) {
	start(t, newDevice(t))
	port, token := mobile.Port(), mobile.Token()
	if port <= 0 || token == "" {
		t.Fatalf("Port() = %d, Token() = %q, want running server", port, token)
	}
	if code := health(t, port, token); code != http.StatusOK {
		t.Fatalf("with token: status = %d, want 200", code)
	}
	if code := health(t, port, ""); code != http.StatusUnauthorized {
		t.Fatalf("without token: status = %d, want 401", code)
	}
}

func TestNodeAPIRequiresToken(t *testing.T) {
	start(t, newDevice(t))
	if code, _ := call(t, http.MethodGet, "/piles", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("GET /piles without token: status = %d, want 401", code)
	}
	if code, body := call(t, http.MethodGet, "/piles", mobile.Token(), nil); code != http.StatusOK || string(bytes.TrimSpace(body)) != "[]" {
		t.Fatalf("GET /piles: status = %d, body %s, want 200 []", code, body)
	}
}

func TestStopClosesPort(t *testing.T) {
	start(t, newDevice(t))
	port := mobile.Port()
	if err := mobile.Stop(); err != nil {
		t.Fatal(err)
	}
	if conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port)); err == nil {
		conn.Close()
		t.Fatalf("port %d still accepts connections after Stop", port)
	}
	if mobile.Port() != 0 || mobile.Token() != "" {
		t.Fatalf("after Stop: Port() = %d, Token() = %q, want 0 and empty", mobile.Port(), mobile.Token())
	}
}

func TestStopWithoutStartSucceeds(t *testing.T) {
	if err := mobile.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestStartAgainAfterStopUsesNewToken(t *testing.T) {
	d := newDevice(t)
	start(t, d)
	oldToken := mobile.Token()
	if err := mobile.Stop(); err != nil {
		t.Fatal(err)
	}
	start(t, d)
	port, token := mobile.Port(), mobile.Token()
	if token == oldToken {
		t.Fatal("token did not change after restart")
	}
	if code := health(t, port, token); code != http.StatusOK {
		t.Fatalf("new token: status = %d, want 200", code)
	}
	if code := health(t, port, oldToken); code != http.StatusUnauthorized {
		t.Fatalf("old token: status = %d, want 401", code)
	}
}

func TestStartWhileRunningKeepsServer(t *testing.T) {
	d := newDevice(t)
	start(t, d)
	port, token := mobile.Port(), mobile.Token()
	start(t, d)
	if mobile.Port() != port || mobile.Token() != token {
		t.Fatalf("second Start changed port/token: %d %q -> %d %q", port, token, mobile.Port(), mobile.Token())
	}
	if code := health(t, port, token); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
}

func TestDeviceKeyIsKeptInProvidedKeystore(t *testing.T) {
	d := newDevice(t)
	start(t, d)
	seed, _ := d.keys.Load()
	if len(seed) != 32 {
		t.Fatalf("keystore holds %d bytes after Start, want a 32-byte seed", len(seed))
	}
	if err := mobile.Stop(); err != nil {
		t.Fatal(err)
	}
	start(t, d)
	if again, _ := d.keys.Load(); !bytes.Equal(again, seed) {
		t.Fatal("restart replaced the device key")
	}
}

func TestPilesSurviveRestartOnSameDataDir(t *testing.T) {
	d := newDevice(t)
	start(t, d)
	p := createPile(t, "Поездка")
	if err := mobile.Stop(); err != nil {
		t.Fatal(err)
	}
	start(t, d)
	code, body := call(t, http.MethodGet, "/piles", mobile.Token(), nil)
	var ps []pile
	if err := json.Unmarshal(body, &ps); code != http.StatusOK || err != nil {
		t.Fatalf("GET /piles: status = %d, body %s", code, body)
	}
	if len(ps) != 1 || ps[0] != p {
		t.Fatalf("piles after restart = %+v, want [%+v]", ps, p)
	}
}

func TestInviteCarriesCurrentLANAddresses(t *testing.T) {
	d := newDevice(t)
	d.network = network{"127.0.0.1", "::1", "fe80::1%wlan0", "169.254.3.4", "bogus", "192.168.1.20", "::ffff:10.0.0.7", "2001:db8::5"}
	start(t, d)
	p := createPile(t, "Поездка")
	code, body := call(t, http.MethodGet, "/piles/"+p.ID+"/invite", mobile.Token(), nil)
	var resp struct{ Invite string }
	if err := json.Unmarshal(body, &resp); code != http.StatusOK || err != nil {
		t.Fatalf("GET invite: status = %d, body %s", code, body)
	}
	inv, err := invite.Parse(resp.Invite)
	if err != nil {
		t.Fatal(err)
	}
	var got []netip.Addr
	for _, a := range inv.Addrs {
		got = append(got, a.Addr())
	}
	want := []netip.Addr{netip.MustParseAddr("192.168.1.20"), netip.MustParseAddr("10.0.0.7"), netip.MustParseAddr("2001:db8::5")}
	if !slices.Equal(got, want) {
		t.Fatalf("invite addresses = %v, want %v", got, want)
	}
}

func TestInviteWithoutLANAddressIsOffline(t *testing.T) {
	d := newDevice(t)
	d.network = network{"127.0.0.1"}
	start(t, d)
	p := createPile(t, "Поездка")
	if code, body := call(t, http.MethodGet, "/piles/"+p.ID+"/invite", mobile.Token(), nil); code != http.StatusServiceUnavailable {
		t.Fatalf("GET invite: status = %d, body %s, want 503", code, body)
	}
}

func TestSelectionAndRenditionsComeFromNativeProviders(t *testing.T) {
	d := newDevice(t)
	taken := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC).UnixMilli()
	d.media = library{items: fmt.Sprintf(`[{"local_id":"image:42","kind":"photo","platform_time":%d,"width":4000,"height":3000,"size_bytes":10,"mime":"image/jpeg","original_name":"IMG_1.jpg","modified":1}]`, taken)}
	start(t, d)
	p := createPile(t, "Поездка")
	if code, body := call(t, http.MethodPut, "/piles/"+p.ID+"/selection", mobile.Token(), map[string]int64{"from": taken - 1000, "to": taken + 1000}); code != http.StatusNoContent {
		t.Fatalf("PUT selection: status = %d, body %s", code, body)
	}
	code, body := call(t, http.MethodGet, "/piles/"+p.ID+"/selection", mobile.Token(), nil)
	var sel struct {
		Assets []struct {
			ID       string `json:"id"`
			LocalID  string `json:"local_id"`
			Selected bool   `json:"selected"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &sel); code != http.StatusOK || err != nil {
		t.Fatalf("GET selection: status = %d, body %s", code, body)
	}
	if len(sel.Assets) != 1 || sel.Assets[0].LocalID != "image:42" || !sel.Assets[0].Selected {
		t.Fatalf("selection = %s, want image:42 selected", body)
	}
	rev := sha256.Sum256(binary.BigEndian.AppendUint64(nil, 1))
	code, body = call(t, http.MethodGet, "/piles/"+p.ID+"/blob/"+sel.Assets[0].ID+"/"+hex.EncodeToString(rev[:])+"/1", mobile.Token(), nil)
	if code != http.StatusOK || string(body) != "T1 of image:42" {
		t.Fatalf("GET T1: status = %d, body %q, want the native rendition", code, body)
	}
}

func TestDiscoveryGoesThroughNativeProvider(t *testing.T) {
	d := newDevice(t)
	start(t, d)
	createPile(t, "Поездка")
	code, body := call(t, http.MethodGet, "/screens", mobile.Token(), nil)
	var screens []struct {
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		Address   string `json:"address"`
		Available bool   `json:"available"`
	}
	if err := json.Unmarshal(body, &screens); code != http.StatusOK || err != nil {
		t.Fatalf("GET /screens: status = %d, body %s", code, body)
	}
	if len(screens) != 1 || screens[0].Name != "Гостиная" || screens[0].Kind != "cast" || screens[0].Address != "192.168.1.30:8009" || !screens[0].Available {
		t.Fatalf("screens = %s, want the Cast device found by the native provider", body)
	}
	if err := mobile.Stop(); err != nil {
		t.Fatal(err)
	}
	d.lan.mu.Lock()
	defer d.lan.mu.Unlock()
	if !slices.Equal(d.lan.adverts, []string{"_pileup._tcp"}) || !d.lan.stopped {
		t.Fatalf("advertised %v, stopped %v; want the pile advertised and discovery stopped", d.lan.adverts, d.lan.stopped)
	}
}

func TestMemberCarriesAppVersionFromNative(t *testing.T) {
	start(t, newDevice(t))
	createPile(t, "Поездка")
	code, body := call(t, http.MethodGet, "/piles", mobile.Token(), nil)
	var ps []struct {
		Members []struct {
			Name       string `json:"name"`
			Platform   string `json:"platform"`
			AppVersion string `json:"app_version"`
		} `json:"members"`
	}
	if err := json.Unmarshal(body, &ps); code != http.StatusOK || err != nil {
		t.Fatalf("GET /piles: status = %d, body %s", code, body)
	}
	if len(ps) != 1 || len(ps[0].Members) != 1 || ps[0].Members[0].AppVersion != "1.4.0" || ps[0].Members[0].Platform != "android" {
		t.Fatalf("piles = %s", body)
	}
}

type stalled struct {
	library
	once    *sync.Once
	entered chan struct{}
	release chan struct{}
}

func (s stalled) Media(from, to int64) ([]byte, error) {
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	return s.library.Media(from, to)
}

func TestStopLetsInFlightRequestFinishBeforeClosingData(t *testing.T) {
	d := newDevice(t)
	taken := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC).UnixMilli()
	photo := fmt.Sprintf(`{"local_id":"image:42","kind":"photo","platform_time":%d,"width":4000,"height":3000,"size_bytes":10,"mime":"image/jpeg","original_name":"IMG_1.jpg","modified":1}`, taken)
	older := strings.Repeat(`,{"local_id":"image:1","kind":"photo","platform_time":1,"mime":"image/jpeg","modified":1}`, 5000)
	media := stalled{
		library: library{items: "[" + photo + older + "]"},
		once:    &sync.Once{},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	if err := mobile.Start(d.dir, "android", "1.4.0", d.keys, media, media, d.network, d.lan); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mobile.Stop() })
	p := createPile(t, "Поездка")

	b, _ := json.Marshal(map[string]int64{"from": taken - 1000, "to": taken + 1000})
	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:"+strconv.Itoa(mobile.Port())+"/piles/"+p.ID+"/selection", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+mobile.Token())
	dropped := make(chan struct{})
	go func() {
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
		close(dropped)
	}()
	<-media.entered

	stopped := make(chan error, 1)
	go func() { stopped <- mobile.Stop() }()
	<-dropped
	close(media.release)
	if err := <-stopped; err != nil {
		t.Fatalf("Stop: %v", err)
	}

	start(t, d)
	code, body := call(t, http.MethodGet, "/piles/"+p.ID+"/selection", mobile.Token(), nil)
	var sel struct {
		From int64 `json:"from"`
		To   int64 `json:"to"`
	}
	if err := json.Unmarshal(body, &sel); code != http.StatusOK || err != nil {
		t.Fatalf("GET selection after restart: status = %d, body %s", code, body)
	}
	if sel.From != taken-1000 || sel.To != taken+1000 {
		t.Fatalf("selection after restart = %s, want the range set by the request in flight during Stop", body)
	}
}
