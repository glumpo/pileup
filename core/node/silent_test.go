package node_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/node"
)

type sleepy struct {
	s      saved
	o      owner
	n      *node.Node
	at     *site
	photos []op.AssetMeta
	asleep atomic.Bool
	asked  atomic.Int32
}

func sleepyOwner(t *testing.T, photos int, silentTier string, routes map[string]http.HandlerFunc) *sleepy {
	t.Helper()
	t.Cleanup(node.SetFetchTimeouts(300*time.Millisecond, 300*time.Millisecond))
	o := &sleepy{s: newSaved(t)}
	o.o, o.photos = newOwner(t, o.s.id, o.s.secret).withPhotos(t, photos)
	o.photos = append(o.photos, o.o.photo)
	h := map[string]http.HandlerFunc{
		"GET /blob/{asset}/{revision}/{tier}": func(w http.ResponseWriter, r *http.Request) {
			if r.PathValue("tier") == silentTier {
				o.asked.Add(1)
				if o.asleep.Load() {
					<-r.Context().Done()
					return
				}
			}
			w.Write(ownerBytes(r.PathValue("tier")))
		},
	}
	for k, v := range routes {
		h[k] = v
	}
	o.asleep.Store(silentTier == "1")
	o.at = o.o.serve(t, h)
	o.s.write(t, o.s.history(t, 1), o.at.addr())
	o.n = o.s.open(t)
	eventually(t, "sync with the owner", func() bool {
		return o.n.Online(o.s.id)[o.o.author()] && len(assetsOn(t, o.n, o.s.id)) == len(o.photos)
	})
	if silentTier != "1" {
		eventually(t, "T1 of the owner", func() bool {
			for _, m := range o.photos {
				if !o.n.Stored(o.s.id, m.AssetID, m.Revision, node.T1) {
					return false
				}
			}
			return true
		})
	}
	o.asleep.Store(true)
	return o
}

func (o *sleepy) fetch(m op.AssetMeta) ([]byte, error, time.Duration) {
	t0 := time.Now()
	got, err := fetchWithin(o.n, o.s.id, m, node.T2, 5*time.Second)
	return got, err, time.Since(t0)
}

func (o *sleepy) timedOut(t *testing.T) {
	t.Helper()
	if _, err, took := o.fetch(o.photos[0]); !errors.Is(err, node.ErrUnavailable) || took < 250*time.Millisecond {
		t.Fatalf("first request to the silent owner: %v after %v, want ErrUnavailable after the header timeout", err, took)
	}
}

func (o *sleepy) answers(t *testing.T, what string) {
	t.Helper()
	got, err, took := o.fetch(o.photos[1])
	if err != nil || !bytes.Equal(got, ownerBytes("2")) {
		t.Fatalf("%s: T2 %q, %v after %v; want the file of the owner right away", what, got, err, took)
	}
}

func TestSilentOwnerIsNotWaitedForAgain(t *testing.T) {
	o := sleepyOwner(t, 5, "2", nil)
	o.timedOut(t)

	var wg sync.WaitGroup
	for _, m := range o.photos[1:] {
		wg.Go(func() {
			if _, err, took := o.fetch(m); !errors.Is(err, node.ErrUnavailable) || took > 100*time.Millisecond {
				t.Errorf("next request to the owner that just went silent: %v after %v, want ErrUnavailable right away", err, took)
			}
		})
	}
	wg.Wait()
	if got := o.asked.Load(); got != 1 {
		t.Errorf("the silent owner was asked %d times, want once", got)
	}
	for _, m := range o.photos {
		f, _, err := o.n.Blob(t.Context(), o.s.id, m.AssetID, m.Revision, node.T1)
		if err != nil {
			t.Fatalf("cached T1 while the owner is silent: %v", err)
		}
		f.Close()
	}
}

func TestSilentOwnerIsAskedAgainOneRequestAtATimeAfterPause(t *testing.T) {
	t.Cleanup(node.SetOwnerPause(500 * time.Millisecond))
	o := sleepyOwner(t, 3, "2", nil)
	o.timedOut(t)
	time.Sleep(600 * time.Millisecond)

	var fast atomic.Int32
	var wg sync.WaitGroup
	for _, m := range o.photos[1:] {
		wg.Go(func() {
			if _, err, took := o.fetch(m); errors.Is(err, node.ErrUnavailable) && took < 100*time.Millisecond {
				fast.Add(1)
			}
		})
	}
	wg.Wait()
	if asked, answered := o.asked.Load(), fast.Load(); asked != 2 || answered != 2 {
		t.Fatalf("after the pause the owner was asked %d times in total and %d requests failed right away; want one more request and the other two right away", asked, answered)
	}
}

func TestOwnerKnownToBeAwayIsNotWaitedFor(t *testing.T) {
	t.Cleanup(node.SetAnnounceTimeout(300 * time.Millisecond))
	o := sleepyOwner(t, 0, "2", nil)
	o.asleep.Store(false)
	o.at.stop()
	listenSilentAt(t, o.at.addr())
	eventually(t, "the owner is away after a timeout", func() bool {
		return o.n.Presence(o.s.id, o.o.author()).LastError == node.TimedOut
	})

	_, err, took := o.fetch(o.photos[0])
	if !errors.Is(err, node.ErrUnavailable) || took > 100*time.Millisecond || node.Class(err) != node.TimedOut {
		t.Fatalf("file of the owner that is away: %v (class %q) after %v; want ErrUnavailable of class %q right away", err, node.Class(err), took, node.TimedOut)
	}
}

func listenSilentAt(t *testing.T, addr string) {
	t.Helper()
	c := listenAt(t, addr)
	go func() {
		for {
			conn, err := c.Accept()
			if err != nil {
				return
			}
			go func() {
				io.Copy(io.Discard, conn)
				conn.Close()
			}()
		}
	}()
}

func TestSilentOwnerIsClassedAsTimedOut(t *testing.T) {
	o := sleepyOwner(t, 1, "2", nil)
	o.timedOut(t)
	if _, err, _ := o.fetch(o.photos[1]); node.Class(err) != node.TimedOut {
		t.Fatalf("class of the silent owner: %q (%v), want %q", node.Class(err), err, node.TimedOut)
	}
}

func TestOwnerBackIsAskedRightAway(t *testing.T) {
	t.Run("announce from the owner", func(t *testing.T) {
		t.Cleanup(node.SetOwnerPause(time.Minute))
		o := sleepyOwner(t, 1, "2", nil)
		o.timedOut(t)
		o.asleep.Store(false)
		o.o.announce(t, nodeAddr(t, o.n, o.s.id), portOf(o.at.addr()))
		o.answers(t, "after the announce from the owner")
	})
	t.Run("reconnect", func(t *testing.T) {
		t.Cleanup(node.SetOwnerPause(time.Minute))
		o := sleepyOwner(t, 1, "2", nil)
		o.timedOut(t)
		o.asleep.Store(false)
		o.n.Reconnect("test")
		o.answers(t, "after Reconnect")
	})
	t.Run("sync round", func(t *testing.T) {
		t.Cleanup(node.SetOwnerPause(time.Minute))
		polls := make(chan struct{})
		o := sleepyOwner(t, 1, "2", map[string]http.HandlerFunc{
			"POST /vv": func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-polls:
					w.Header().Set("Content-Type", "application/cbor")
					w.Write([]byte{0xa0})
				case <-r.Context().Done():
				}
			},
		})
		o.timedOut(t)
		o.asleep.Store(false)
		polls <- struct{}{}
		eventually(t, "the file of the owner after a sync round", func() bool {
			_, err, _ := o.fetch(o.photos[1])
			return err == nil
		})
	})
	t.Run("back at its address", func(t *testing.T) {
		t.Cleanup(node.SetOwnerPause(time.Minute))
		o := sleepyOwner(t, 1, "2", nil)
		o.timedOut(t)
		o.at.stop()
		serveOn(t, listenAt(t, o.at.addr()), o.o.key, o.s.id, o.s.secret, o.o.raws, nil)
		eventually(t, "the file of the owner after it is back", func() bool {
			_, err, _ := o.fetch(o.photos[1])
			return err == nil
		})
	})
}

func TestSilentOwnerIsNotAskedForThumbnailsOverAndOver(t *testing.T) {
	o := sleepyOwner(t, 0, "1", nil)
	eventually(t, "a thumbnail request", func() bool { return o.asked.Load() > 0 })
	time.Sleep(3 * time.Second)
	if got := o.asked.Load(); got > 1 {
		t.Fatalf("the silent owner was asked for thumbnails %d times in 3 s, want once", got)
	}
}
