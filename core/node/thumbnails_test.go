package node_test

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/node"
)

func fetchAttempts(n *node.Node, id []byte, o owner) int {
	total := 0
	for _, e := range n.Diag().Events() {
		if bytes.Equal(e.Pile, id) && e.Peer == keyOf(o.author()) && strings.HasPrefix(e.Message, "fetch ") {
			total += e.Count
		}
	}
	return total
}

func TestUnavailableOwnerIsAskedForThumbnailsWhenItConnects(t *testing.T) {
	t.Run("never connected", func(t *testing.T) {
		s := newSaved(t)
		o := newOwner(t, s.id, s.secret)
		s.write(t, append(s.history(t, 1), o.raws...))
		n := s.open(t)
		time.Sleep(3 * time.Second)
		if got := fetchAttempts(n, s.id, o); got > 1 {
			t.Fatalf("%d thumbnail requests to an owner that never connected, want at most 1", got)
		}

		at := o.serve(t, nil)
		o.announce(t, nodeAddr(t, n, s.id), portOf(at.addr()))
		eventually(t, "T1 of the owner after it connects", func() bool { return n.Stored(s.id, o.photo.AssetID, o.photo.Revision, node.T1) })
	})
	t.Run("gone asleep", func(t *testing.T) {
		pausing := endlessPauses(t)
		s := newSaved(t)
		o := newOwner(t, s.id, s.secret)
		at := o.serve(t, map[string]http.HandlerFunc{
			"GET /blob/{asset}/{revision}/{tier}": func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) },
		})
		s.write(t, s.history(t, 1), at.addr())
		n := s.open(t)
		syncedWith(t, n, s.id, o)
		eventually(t, "a thumbnail request that broke off", func() bool { return fetchAttempts(n, s.id, o) > 0 })
		at.stop()
		received(t, pausing, "pause after the connection was lost")
		before := fetchAttempts(n, s.id, o)
		time.Sleep(3 * time.Second)
		if got := fetchAttempts(n, s.id, o) - before; got > 1 {
			t.Fatalf("%d thumbnail requests while the owner is away, want at most 1", got)
		}

		serveOn(t, listenAt(t, at.addr()), o.key, s.id, s.secret, o.raws, nil)
		o.announce(t, nodeAddr(t, n, s.id), portOf(at.addr()))
		eventually(t, "T1 of the owner after it is back", func() bool { return n.Stored(s.id, o.photo.AssetID, o.photo.Revision, node.T1) })
	})
}

func TestThumbnailOwnerDoesNotServeIsAskedAgainOnlyAfterPause(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
	}{{"not found", http.StatusNotFound}, {"render error", http.StatusInternalServerError}} {
		t.Run(c.name, func(t *testing.T) {
			const pause = 3 * time.Second
			t.Cleanup(node.SetBrokenPause(pause))
			s := newSaved(t)
			o := newOwner(t, s.id, s.secret)
			var mu sync.Mutex
			var asked []time.Time
			at := o.serve(t, map[string]http.HandlerFunc{
				"GET /blob/{asset}/{revision}/{tier}": func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					asked = append(asked, time.Now())
					mu.Unlock()
					http.Error(w, "no thumbnail", c.status)
				},
			})
			requests := func() []time.Time {
				mu.Lock()
				defer mu.Unlock()
				return append([]time.Time{}, asked...)
			}
			s.write(t, s.history(t, 1), at.addr())
			n := s.open(t)
			eventually(t, "the first thumbnail request", func() bool { return len(requests()) >= 1 })
			if err := s.choose(context.Background(), n); err != nil {
				t.Fatal(err)
			}

			eventually(t, "the thumbnail is asked again", func() bool { return len(requests()) >= 2 })
			got := requests()
			if gap := got[1].Sub(got[0]); gap < pause {
				t.Fatalf("thumbnail asked again %v after the owner answered %d, want not before the pause of %v", gap, c.status, pause)
			}
		})
	}
}

func TestPassWithEveryThumbnailCachedTouchesNeitherOwnersNorCache(t *testing.T) {
	s := newSaved(t)
	o := newOwner(t, s.id, s.secret)
	var asked atomic.Int32
	at := o.serve(t, map[string]http.HandlerFunc{
		"GET /blob/{asset}/{revision}/{tier}": func(w http.ResponseWriter, r *http.Request) {
			asked.Add(1)
			w.Write(ownerBytes(r.PathValue("tier")))
		},
	})
	s.write(t, s.history(t, 1), at.addr())
	n := s.open(t)
	eventually(t, "T1 of the owner", func() bool { return n.Stored(s.id, o.photo.AssetID, o.photo.Revision, node.T1) })
	db, err := sql.Open("sqlite", filepath.Join(s.dir, "blobs", "cache.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE touches (n INTEGER); CREATE TRIGGER touched BEFORE INSERT ON holds BEGIN INSERT INTO touches VALUES (1); END`); err != nil {
		t.Fatal(err)
	}

	if err := s.choose(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	var touches int
	if err := db.QueryRow(`SELECT count(*) FROM touches`).Scan(&touches); err != nil {
		t.Fatal(err)
	}
	if touches != 0 || asked.Load() != 1 {
		t.Fatalf("pass after a pile change with every T1 cached: %d cache holds taken, %d requests to the owner; want none and the first one", touches, asked.Load())
	}
}
