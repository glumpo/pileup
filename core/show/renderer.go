package show

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
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
)

func (s *session) connect() error {
	ctx, cancel := context.WithTimeout(s.ctx, connectTimeout)
	defer cancel()
	cs, err := cast.Connect(ctx, s.screen.Addr.String(), heartbeat)
	if err != nil {
		s.n.Diag().Error(diag.Renderer, s.pile, s.screen.Name, fmt.Errorf("connect: %w", err))
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	local := cs.LocalAddr().(*net.TCPAddr).AddrPort().Addr().Unmap()
	ln, err := net.Listen("tcp", netip.AddrPortFrom(local, 0).String())
	if err != nil {
		cs.Close()
		return err
	}
	s.cast = cs
	s.base = "http://" + ln.Addr().String() + "/show/" + s.token
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
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		s.n.ServeOriginal(sw, r, s.pile, a.Meta.AssetID, a.Meta.Revision)
		if sw.status == http.StatusBadGateway && a.Owner != s.self {
			s.unavailable(a)
		}
	default:
		http.NotFound(w, r)
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
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

func (s *session) media(a state.Asset) (string, string) {
	u := fmt.Sprintf("%s/%x/%x/", s.base, a.Meta.AssetID, a.Meta.Revision)
	if a.Meta.Kind == op.KindVideo && !s.videoDown {
		return u + "3", a.Meta.MIME
	}
	return u + "2", "image/jpeg"
}

func (s *session) drive() {
	defer s.wg.Done()
	loaded := ""
	var retry <-chan time.Time
	for {
		s.mu.Lock()
		var cur state.Asset
		url, contentType := "", ""
		if s.cursor >= 0 {
			cur = s.items[s.cursor]
			url, contentType = s.media(cur)
		}
		command := ""
		if url == loaded {
			command, s.command = s.command, ""
		}
		s.mu.Unlock()
		switch {
		case url != loaded && url == "":
			loaded, retry = "", nil
			s.request("command_failed", "stop", s.cast.Stop)
		case url != loaded:
			loaded, retry = url, nil
			if !s.request("load_failed", fmt.Sprintf("load %x", cur.Meta.AssetID), func(ctx context.Context) (cast.Media, error) {
				return s.cast.Load(ctx, url, contentType)
			}) {
				retry = time.After(reload)
			}
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
			return
		case <-s.wake:
		case <-retry:
			loaded, retry = "", nil
		case <-s.cast.Changed():
			if !s.status(s.cast.Media()) {
				retry = time.After(reload)
			}
		case <-s.cast.Done():
			s.failed("disconnected", "connection", s.cast.Err())
			return
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

func (s *session) status(m cast.Media) bool {
	s.mu.Lock()
	s.player = m.State
	s.mu.Unlock()
	if m.State == "IDLE" && m.IdleReason == "ERROR" {
		s.failed("playback_failed", "playback", errors.New("media receiver failed to play the file"))
		return false
	}
	return true
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
