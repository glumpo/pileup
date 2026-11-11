package node_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/glumpo/pileup/core/device"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/node"
)

type unreadable struct {
	*media
	reason string
}

func (u unreadable) OriginalSize(string) (int64, error) { return 0, errors.New(u.reason) }

func TestUnreadableOriginalIsNamedAndNotStreamed(t *testing.T) {
	for _, c := range []struct {
		native   string
		notLocal bool
	}{
		{"not_local: the original is in iCloud", true},
		{"permission denied for video:42", false},
	} {
		ma := newMedia()
		dir := t.TempDir()
		a, err := node.Open(node.Config{
			Dir:        dir,
			Keystore:   device.File(filepath.Join(dir, "device.key")),
			Platform:   "ios",
			Addrs:      loopback,
			Media:      ma,
			Renditions: unreadable{ma, c.native},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { a.Close() })
		b := open(t, t.TempDir(), "android")
		p := create(t, a, "Грузия", "Аня")
		clip := video("VID_1", day, 15_000)
		clip.MIME = "video/quicktime"
		ma.add(clip, []byte("movie"))
		movie := share(t, a, p)["VID_1"]
		join(t, b, inviteTo(t, a, p), "Боря")
		waitAssets(t, p.ID, idSet(movie.AssetID), b)

		for name, n := range map[string]*node.Node{"owner": a, "member": b} {
			w := httptest.NewRecorder()
			err := n.ServeOriginal(w, httptest.NewRequest(http.MethodGet, "/", nil), p.ID, movie.AssetID, movie.Revision)
			if w.Code != http.StatusBadGateway || errors.Is(err, node.ErrNotLocal) != c.notLocal || !strings.Contains(err.Error(), c.native) {
				t.Fatalf("%s, %q: status %d, error %v", name, c.native, w.Code, err)
			}
			if !slices.ContainsFunc(n.Diag().Events(), func(e diag.Event) bool { return strings.Contains(e.Message, c.native) }) {
				t.Fatalf("%s, %q: reason is not in the journal: %+v", name, c.native, n.Diag().Events())
			}
		}
	}
}
