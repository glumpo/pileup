package cast

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	appID     = "CC1AD845"
	sender    = "sender-0"
	platform  = "receiver-0"
	nsConn    = "urn:x-cast:com.google.cast.tp.connection"
	nsBeat    = "urn:x-cast:com.google.cast.tp.heartbeat"
	nsReceive = "urn:x-cast:com.google.cast.receiver"
	nsMedia   = "urn:x-cast:com.google.cast.media"
)

var (
	ErrClosed       = errors.New("cast: connection closed")
	ErrNoHeartbeat  = errors.New("cast: receiver stopped responding")
	ErrNotLaunched  = errors.New("cast: receiver did not start the media receiver")
	ErrSessionEnded = errors.New("cast: media receiver session ended")
)

type Error struct {
	Type   string
	Reason string
	Code   int
}

func (e *Error) Error() string {
	return fmt.Sprintf("cast: %s reason=%q code=%d", e.Type, e.Reason, e.Code)
}

type Media struct {
	SessionID   int
	State       string
	IdleReason  string
	CurrentTime float64
}

type reply struct {
	Type      string          `json:"type"`
	RequestID int             `json:"requestId"`
	Reason    string          `json:"reason"`
	Code      int             `json:"detailedErrorCode"`
	Status    json.RawMessage `json:"status"`
}

type mediaStatus struct {
	SessionID   int     `json:"mediaSessionId"`
	State       string  `json:"playerState"`
	IdleReason  string  `json:"idleReason"`
	CurrentTime float64 `json:"currentTime"`
}

type receiverStatus struct {
	Applications []struct {
		AppID       string `json:"appId"`
		SessionID   string `json:"sessionId"`
		TransportID string `json:"transportId"`
	} `json:"applications"`
}

type Session struct {
	conn      net.Conn
	heartbeat time.Duration
	wmu       sync.Mutex
	mu        sync.Mutex
	next      int
	pending   map[int]chan reply
	app       string
	transport string
	media     Media
	changed   chan struct{}
	done      chan struct{}
	err       error
}

func Connect(ctx context.Context, addr string, heartbeat time.Duration) (*Session, error) {
	d := tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true}}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Session{
		conn:      conn,
		heartbeat: heartbeat,
		pending:   map[int]chan reply{},
		changed:   make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
	go s.read()
	go s.ping()
	if err := s.launch(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Session) launch(ctx context.Context) error {
	if err := s.send(platform, nsConn, map[string]any{"type": "CONNECT"}); err != nil {
		return err
	}
	r, err := s.request(ctx, platform, nsReceive, map[string]any{"type": "LAUNCH", "appId": appID})
	if err != nil {
		return err
	}
	var st receiverStatus
	json.Unmarshal(r.Status, &st)
	for _, a := range st.Applications {
		if a.AppID == appID && a.TransportID != "" {
			s.mu.Lock()
			s.app, s.transport = a.SessionID, a.TransportID
			s.mu.Unlock()
			return s.send(a.TransportID, nsConn, map[string]any{"type": "CONNECT"})
		}
	}
	return ErrNotLaunched
}

func (s *Session) Load(ctx context.Context, url, contentType string, currentTime float64) (Media, error) {
	body := map[string]any{
		"type":     "LOAD",
		"autoplay": true,
		"media": map[string]any{
			"contentId":   url,
			"contentType": contentType,
			"streamType":  "BUFFERED",
		},
	}
	if currentTime > 0 {
		body["currentTime"] = currentTime
	}
	return s.control(ctx, body)
}

func (s *Session) Play(ctx context.Context) (Media, error) { return s.command(ctx, "PLAY") }

func (s *Session) Pause(ctx context.Context) (Media, error) { return s.command(ctx, "PAUSE") }

func (s *Session) Stop(ctx context.Context) (Media, error) { return s.command(ctx, "STOP") }

func (s *Session) Media() Media {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.media
}

func (s *Session) Changed() <-chan struct{} { return s.changed }

func (s *Session) LocalAddr() net.Addr { return s.conn.LocalAddr() }

func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *Session) Close() error {
	s.fail(ErrClosed)
	return nil
}

func (s *Session) command(ctx context.Context, typ string) (Media, error) {
	return s.control(ctx, map[string]any{"type": typ, "mediaSessionId": s.Media().SessionID})
}

func (s *Session) control(ctx context.Context, body map[string]any) (Media, error) {
	s.mu.Lock()
	transport := s.transport
	s.mu.Unlock()
	r, err := s.request(ctx, transport, nsMedia, body)
	if err != nil {
		return Media{}, err
	}
	return firstMedia(r.Status), nil
}

func (s *Session) request(ctx context.Context, dst, ns string, body map[string]any) (reply, error) {
	ch := make(chan reply, 1)
	s.mu.Lock()
	s.next++
	id := s.next
	s.pending[id] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()
	body["requestId"] = id
	if err := s.send(dst, ns, body); err != nil {
		return reply{}, err
	}
	select {
	case r := <-ch:
		if r.Type != "RECEIVER_STATUS" && r.Type != "MEDIA_STATUS" {
			return r, &Error{Type: r.Type, Reason: r.Reason, Code: r.Code}
		}
		return r, nil
	case <-s.done:
		return reply{}, s.Err()
	case <-ctx.Done():
		return reply{}, ctx.Err()
	}
}

func (s *Session) send(dst, ns string, body map[string]any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.conn.SetWriteDeadline(time.Now().Add(3 * s.heartbeat))
	if err := writeFrame(s.conn, frame{sender, dst, ns, payload}); err != nil {
		s.fail(fmt.Errorf("%w: %v", ErrClosed, err))
		return s.Err()
	}
	return nil
}

func (s *Session) ping() {
	t := time.NewTicker(s.heartbeat)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.send(platform, nsBeat, map[string]any{"type": "PING"})
		case <-s.done:
			return
		}
	}
}

func (s *Session) read() {
	for {
		s.conn.SetReadDeadline(time.Now().Add(3 * s.heartbeat))
		f, err := readFrame(s.conn)
		var ne net.Error
		switch {
		case errors.As(err, &ne) && ne.Timeout():
			s.fail(ErrNoHeartbeat)
			return
		case err != nil:
			s.fail(fmt.Errorf("%w: %v", ErrClosed, err))
			return
		}
		s.handle(f)
	}
}

func (s *Session) handle(f frame) {
	var r reply
	if json.Unmarshal(f.payload, &r) != nil {
		return
	}
	switch {
	case f.namespace == nsBeat && r.Type == "PING":
		s.send(f.source, nsBeat, map[string]any{"type": "PONG"})
	case f.namespace == nsConn && r.Type == "CLOSE":
		s.fail(ErrSessionEnded)
	case f.namespace == nsReceive && r.Type == "RECEIVER_STATUS" && r.RequestID == 0:
		s.checkApp(r.Status)
	case f.namespace == nsMedia && r.Type == "MEDIA_STATUS":
		s.mu.Lock()
		s.media = firstMedia(r.Status)
		s.mu.Unlock()
		select {
		case s.changed <- struct{}{}:
		default:
		}
	}
	s.mu.Lock()
	ch := s.pending[r.RequestID]
	delete(s.pending, r.RequestID)
	s.mu.Unlock()
	if ch != nil {
		ch <- r
	}
}

func (s *Session) checkApp(status json.RawMessage) {
	s.mu.Lock()
	app := s.app
	s.mu.Unlock()
	if app == "" {
		return
	}
	var st receiverStatus
	json.Unmarshal(status, &st)
	for _, a := range st.Applications {
		if a.SessionID == app {
			return
		}
	}
	s.fail(ErrSessionEnded)
}

func (s *Session) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return
	}
	s.err = err
	close(s.done)
	s.conn.Close()
}

func firstMedia(status json.RawMessage) Media {
	var st []mediaStatus
	json.Unmarshal(status, &st)
	if len(st) == 0 {
		return Media{}
	}
	return Media{SessionID: st[0].SessionID, State: st[0].State, IdleReason: st[0].IdleReason, CurrentTime: st[0].CurrentTime}
}
