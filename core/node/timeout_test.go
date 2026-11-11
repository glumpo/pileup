package node_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/node"
)

func silentOwner(t *testing.T, tier string, serve http.HandlerFunc) (saved, owner, *node.Node) {
	t.Helper()
	t.Cleanup(node.SetFetchTimeouts(300*time.Millisecond, 300*time.Millisecond))
	s := newSaved(t)
	o := newOwner(t, s.id, s.secret)
	at := o.serve(t, map[string]http.HandlerFunc{
		"GET /blob/{asset}/{revision}/{tier}": func(w http.ResponseWriter, r *http.Request) {
			if r.PathValue("tier") != tier {
				w.Write(ownerBytes(r.PathValue("tier")))
				return
			}
			serve(w, r)
		},
	})
	s.write(t, s.history(t, 1), at.addr())
	n := s.open(t)
	syncedWith(t, n, s.id, o)
	return s, o, n
}

func stalls(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Length", "100")
	w.Write([]byte("part"))
	w.(http.Flusher).Flush()
	<-r.Context().Done()
}

func TestFetchTimeoutsAreLogged(t *testing.T) {
	for name, serve := range map[string]http.HandlerFunc{
		"no headers":   func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() },
		"stalled body": stalls,
	} {
		t.Run(name, func(t *testing.T) {
			s, o, n := silentOwner(t, "2", serve)
			fetchWithin(n, s.id, o.photo, node.T2, 5*time.Second)
			eventually(t, "the timeout is logged", logged(n, diag.Timeout, node.Pile{ID: s.id}, keyOf(o.author()), "owner went silent"))
		})
	}
}

func TestRelayTimeoutIsLogged(t *testing.T) {
	s, o, n := silentOwner(t, "3", stalls)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.ServeOriginal(w, r, s.id, o.photo.AssetID, o.photo.Revision)
	}))
	t.Cleanup(srv.Close)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(srv.URL)
	if err == nil {
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}
	eventually(t, "the timeout is logged", logged(n, diag.Timeout, node.Pile{ID: s.id}, keyOf(o.author()), "owner went silent"))
}
