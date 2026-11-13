package show

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/glumpo/pileup/core/cast"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/node"
)

const (
	heartbeat      = 5 * time.Second
	connectTimeout = 10 * time.Second
	requestTimeout = 30 * time.Second
	stopTimeout    = 2 * time.Second
	reload         = time.Second
	burst          = 300 * time.Millisecond
	again          = 2 * time.Second
	misses         = 2
)

var firstPause, maxPause = time.Second, 30 * time.Second

var stall = 2 * time.Second

func (s *session) connect() error {
	ctx, cancel := context.WithTimeout(s.ctx, connectTimeout)
	defer cancel()
	s.mu.Lock()
	addr := s.screen.Addr.String()
	s.mu.Unlock()
	cs, err := cast.Connect(ctx, addr, heartbeat)
	if err != nil {
		s.n.Diag().Error(diag.Renderer, s.pile, s.screen.Name, fmt.Errorf("connect: %w", err))
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	if err := s.open(cs.LocalAddr().(*net.TCPAddr).AddrPort().Addr()); err != nil {
		cs.Close()
		return err
	}
	s.mu.Lock()
	s.cast = cs
	s.mu.Unlock()
	return nil
}

func (s *session) route() error {
	c, err := net.Dial("udp", s.screen.Addr.String())
	if err != nil {
		s.n.Diag().Error(diag.Renderer, s.pile, s.screen.Name, fmt.Errorf("route: %w", err))
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	local := c.LocalAddr().(*net.UDPAddr).AddrPort().Addr()
	c.Close()
	return s.open(local)
}

func (s *session) open(local netip.Addr) error {
	ln, err := net.Listen("tcp", netip.AddrPortFrom(local.Unmap(), 0).String())
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.base = "http://" + ln.Addr().String() + "/show/" + s.token
	s.mu.Unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /show/{token}/{asset}/{revision}/{tier}", s.serve)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: requestTimeout}
	go s.srv.Serve(ln)
	return nil
}

func (s *session) serve(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.PathValue("token")), []byte(s.token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.mu.Lock()
	i, ok := s.index[r.PathValue("asset")+"/"+r.PathValue("revision")]
	var a state.Asset
	if ok {
		a = s.items[i]
	}
	s.mu.Unlock()
	switch tier := r.PathValue("tier"); {
	case ok && tier == "2":
		s.display(w, r, a)
	case ok && tier == "3" && a.Meta.Kind == op.KindVideo:
		if err := s.n.ServeOriginal(castWriter{w}, r, s.pile, a.Meta.AssetID, a.Meta.Revision); err != nil {
			s.unavailable(a, why(err))
		}
	default:
		http.NotFound(w, r)
	}
}

func why(err error) string {
	switch {
	case errors.Is(err, node.ErrNotLocal):
		return NotLocal
	case errors.Is(err, node.ErrUnavailable) && !errors.Is(err, node.ErrRefused):
		return Unreachable
	}
	return Unreadable
}

type castWriter struct{ http.ResponseWriter }

func (w castWriter) WriteHeader(code int) {
	w.Header().Set("Content-Type", castType(w.Header().Get("Content-Type")))
	w.ResponseWriter.WriteHeader(code)
}

func castType(mime string) string {
	if mime == "video/quicktime" {
		return "video/mp4"
	}
	return mime
}

func (s *session) display(w http.ResponseWriter, r *http.Request, a state.Asset) {
	f, h, err := s.n.Blob(r.Context(), s.pile, a.Meta.AssetID, a.Meta.Revision, node.T2)
	switch {
	case errors.Is(err, node.ErrUnknownAsset):
		http.NotFound(w, r)
		return
	case errors.Is(err, node.ErrUnavailable):
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("ETag", node.ETag(h))
	w.Header().Set("Cache-Control", node.Immutable)
	http.ServeContent(w, r, "", time.Time{}, f)
}

func (s *session) url(a state.Asset, tier int) string {
	return fmt.Sprintf("%s/%x/%x/%d", s.base, a.Meta.AssetID, a.Meta.Revision, tier)
}

func (s *session) media(a state.Asset) (string, string) {
	if a.Meta.Kind == op.KindVideo && !s.videoDown && !s.ended {
		return s.url(a, 3), castType(a.Meta.MIME)
	}
	return s.url(a, 2), "image/jpeg"
}

type answer struct {
	n     int
	what  string
	media cast.Media
	err   error
}

func (s *session) load(n int, what, url, contentType string, at float64) {
	c := s.cast
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(s.ctx, requestTimeout)
		m, err := c.Load(ctx, url, contentType, at)
		cancel()
		select {
		case s.answers <- answer{n, what, m, err}:
		case <-s.ctx.Done():
		}
	}()
}

func (s *session) drive() {
	defer s.wg.Done()
	loaded, missed, want := "", false, ""
	sent, media, loading, due := 0, 0, false, false
	var retry, revive, gap, stalled <-chan time.Time
	for {
		s.mu.Lock()
		var cur state.Asset
		url, contentType := "", ""
		if s.cursor >= 0 {
			cur = s.items[s.cursor]
			url, contentType = s.media(cur)
		}
		command := ""
		if url == loaded && !loading {
			command, s.command = s.command, ""
		}
		switch {
		case !s.reviving():
			revive = nil
		case revive == nil:
			revive = time.After(again)
		}
		switch wait := s.stallIn(); {
		case wait < 0:
			stalled = nil
		case stalled == nil:
			stalled = time.After(wait)
		}
		s.l.setVideo(s.video())
		s.mu.Unlock()
		switch {
		case url != loaded && url == "":
			loaded, retry, loading = "", nil, false
			s.request("command_failed", "stop", s.cast.Stop)
		case url != loaded && loading && !due:
			if url != want {
				want, gap = url, time.After(burst)
			}
		case url != loaded:
			loaded, retry, missed, loading, due, want, gap = url, nil, false, true, false, "", nil
			sent++
			s.load(sent, fmt.Sprintf("load %x", cur.Meta.AssetID), url, contentType, s.position(url))
		case command == playing:
			s.request("command_failed", "play", s.cast.Play)
		case command == paused:
			s.request("command_failed", "pause", s.cast.Pause)
		}
		select {
		case <-s.ctx.Done():
			ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
			s.cast.Stop(ctx)
			cancel()
			s.cast.Close()
			s.srv.Close()
			return
		case <-s.wake:
		case <-gap:
			gap, due = nil, true
		case <-retry:
			loaded, retry = "", nil
		case <-revive:
			revive = nil
			s.revive()
		case <-stalled:
			stalled = nil
			s.note()
		case a := <-s.answers:
			if a.n != sent || !loading {
				break
			}
			loading = false
			if a.err != nil {
				s.failed("load_failed", a.what, a.err)
				retry, missed = s.miss(loaded, missed), true
				break
			}
			select {
			case <-s.cast.Changed():
			default:
			}
			media = a.media.SessionID
			m := s.cast.Media()
			if m.SessionID != media {
				m = a.media
			}
			s.mu.Lock()
			s.fault = nil
			s.mu.Unlock()
			if !s.status(m, loaded) {
				retry, missed = s.miss(loaded, missed), true
			}
		case <-s.cast.Changed():
			if m := s.cast.Media(); !loading && m.SessionID == media && !s.status(m, loaded) {
				retry, missed = s.miss(loaded, missed), true
			}
		case <-s.cast.Done():
			select {
			case <-s.cast.Changed():
				if m := s.cast.Media(); !loading && m.SessionID == media {
					s.status(m, loaded)
				}
			default:
			}
			s.position("")
			s.failed("reconnecting", "connection", s.cast.Err())
			s.srv.Close()
			if !s.reconnect() {
				return
			}
			loaded, retry, loading, media = "", nil, false, 0
		}
	}
}

func (s *session) reconnect() bool {
	for pause := firstPause; ; pause = min(2*pause, maxPause) {
		select {
		case <-s.ctx.Done():
			return false
		case <-time.After(pause):
		case <-s.kick:
		}
		if s.connect() == nil {
			s.n.Diag().Add(diag.Renderer, s.pile, s.screen.Name, "reconnected to the screen")
			return true
		}
	}
}

func (s *session) request(code, what string, f func(context.Context) (cast.Media, error)) bool {
	ctx, cancel := context.WithTimeout(s.ctx, requestTimeout)
	m, err := f(ctx)
	cancel()
	if err != nil {
		s.failed(code, what, err)
		return false
	}
	s.mu.Lock()
	s.fault = nil
	s.player = m.State
	s.mu.Unlock()
	return true
}

func (s *session) status(m cast.Media, loaded string) bool {
	s.mu.Lock()
	was := s.player
	s.player = m.State
	if s.playingVideo(loaded) {
		if m.State == buffering && was != buffering {
			s.episodes++
			s.buffered = time.Now()
		}
		switch {
		case m.State == "IDLE" && m.IdleReason == "FINISHED":
			s.ended, s.offset, s.since = true, 0, time.Time{}
			s.signal()
		case m.State == "IDLE" && m.CurrentTime == 0:
			s.hold()
		default:
			s.offset, s.since = m.CurrentTime, time.Time{}
			if m.State == playing {
				s.since = time.Now()
			}
		}
	}
	var cur op.AssetMeta
	if s.cursor >= 0 {
		cur = s.items[s.cursor].Meta
	}
	s.mu.Unlock()
	if m.State == "IDLE" && m.IdleReason == "ERROR" {
		s.failed("playback_failed", fmt.Sprintf("playback %x (%s, codec %q)", cur.AssetID, cur.MIME, cur.Codec), errors.New("media receiver failed to play the file"))
		return false
	}
	return true
}

func (s *session) playingVideo(url string) bool {
	if s.cursor < 0 || s.items[s.cursor].Meta.Kind != op.KindVideo {
		return false
	}
	u, _ := s.media(s.items[s.cursor])
	return u == url && strings.HasSuffix(u, "/3")
}

func (s *session) position(url string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hold()
	if !s.playingVideo(url) {
		return 0
	}
	return s.offset
}

func (s *session) hold() {
	if !s.since.IsZero() {
		s.offset += time.Since(s.since).Seconds()
		s.since = time.Time{}
	}
}

func (s *session) miss(url string, missed bool) <-chan time.Time {
	s.mu.Lock()
	if !s.playingVideo(url) {
		s.mu.Unlock()
		return time.After(reload)
	}
	if !missed {
		s.misses++
	}
	if s.misses < misses {
		s.mu.Unlock()
		return time.After(reload)
	}
	cur := s.items[s.cursor]
	s.videoDown, s.why = true, Unsupported
	s.signal()
	s.mu.Unlock()
	s.posterOnly(cur, Unsupported)
	return nil
}

func (s *session) reviving() bool {
	return s.cursor >= 0 && s.videoDown && s.why == Unreachable && !s.revived
}

func (s *session) revive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reviving() && s.l.reachable(s.items[s.cursor].Owner) {
		s.revived = true
		s.videoDown, s.why = false, ""
		s.signal()
	}
}

func (s *session) failed(code, what string, err error) {
	if s.ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	s.fault = &RendererError{Code: code, Message: err.Error()}
	s.mu.Unlock()
	s.n.Diag().Error(diag.Renderer, s.pile, s.screen.Name, fmt.Errorf("%s: %w", what, err))
}
