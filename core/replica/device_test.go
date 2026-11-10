package replica_test

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/glumpo/pileup/core/replica"
)

func forward(t *testing.T, to *atomic.Value) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				up, err := net.Dial("tcp", to.Load().(string))
				if err != nil {
					return
				}
				defer up.Close()
				go io.Copy(up, c)
				io.Copy(c, up)
			}()
		}
	}()
	return l.Addr().String()
}

func briefly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close")
		if r.Method == http.MethodPost && r.URL.Path == "/vv" {
			now := httptest.NewRequestWithContext(r.Context(), http.MethodGet, "/vv", nil)
			h.ServeHTTP(w, now)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func TestPullEndsWhenAnotherDeviceAnswersAtTheAddress(t *testing.T) {
	a := newNode(t, key(1), "Аня")
	b := newNode(t, key(2), "Боря")
	c := newNode(t, key(3), "Вера")
	var to atomic.Value
	to.Store(b.serve(t, briefly))
	addr := forward(t, &to)
	s := a.pull(t, addr)
	waitFor(t, "a pulls from b", func() bool { return len(s.synced()) > 0 }, s)

	to.Store(c.serve(t, briefly))
	if err := s.wait(t); !errors.Is(err, replica.ErrPeerChanged) {
		t.Fatalf("pull after another device took the address ended with %v, want ErrPeerChanged", err)
	}
	if _, ok := a.vv(t)[author(c.key)]; ok {
		t.Error("ops of the other device were pulled under the first one")
	}
}
