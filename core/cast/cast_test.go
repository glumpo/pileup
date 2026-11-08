package cast_test

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/cast"
)

const heartbeat = 100 * time.Millisecond

func connect(t *testing.T, f *fakeReceiver) *cast.Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := cast.Connect(ctx, f.addr, heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func timeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func waitDone(t *testing.T, s *cast.Session, within time.Duration) error {
	t.Helper()
	select {
	case <-s.Done():
		return s.Err()
	case <-time.After(within):
		t.Fatal("session is still alive")
		return nil
	}
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestShowPhotoThenVideoWithControls(t *testing.T) {
	f := newFakeReceiver(t)
	s := connect(t, f)
	ctx := timeout(t)

	photo, err := s.Load(ctx, "http://192.168.1.20:8080/t2/1.jpg", "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if photo.State != "PLAYING" {
		t.Fatalf("photo state %q", photo.State)
	}
	video, err := s.Load(ctx, "http://192.168.1.20:8080/v/2.mp4", "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	if video.State != "PLAYING" || video.SessionID == photo.SessionID {
		t.Fatalf("video %+v after photo %+v", video, photo)
	}
	want := []load{
		{"http://192.168.1.20:8080/t2/1.jpg", "image/jpeg"},
		{"http://192.168.1.20:8080/v/2.mp4", "video/mp4"},
	}
	if got := f.loaded(); !reflect.DeepEqual(got, want) {
		t.Fatalf("receiver loaded %v, want %v", got, want)
	}

	for _, step := range []struct {
		do    func(context.Context) (cast.Media, error)
		state string
	}{
		{s.Pause, "PAUSED"},
		{s.Play, "PLAYING"},
		{s.Stop, "IDLE"},
	} {
		m, err := step.do(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if m.State != step.state || m.SessionID != video.SessionID {
			t.Fatalf("got %+v, want state %s of media %d", m, step.state, video.SessionID)
		}
	}
	if m := s.Media(); m.State != "IDLE" || m.IdleReason != "CANCELLED" {
		t.Fatalf("media after stop %+v", m)
	}
}

func TestLoadFailedIsReported(t *testing.T) {
	f := newFakeReceiver(t)
	s := connect(t, f)
	ctx := timeout(t)

	_, err := s.Load(ctx, "http://192.168.1.20:8080/broken.mp4", "video/mp4")
	var ce *cast.Error
	if !errors.As(err, &ce) || ce.Type != "LOAD_FAILED" || ce.Code != 905 {
		t.Fatalf("got %v, want LOAD_FAILED with code 905", err)
	}
	if _, err := s.Load(ctx, "http://192.168.1.20:8080/t2/3.jpg", "image/jpeg"); err != nil {
		t.Fatalf("session unusable after failed load: %v", err)
	}
}

func TestControlWithoutMediaIsReported(t *testing.T) {
	f := newFakeReceiver(t)
	s := connect(t, f)

	_, err := s.Pause(timeout(t))
	var ce *cast.Error
	if !errors.As(err, &ce) || ce.Type != "INVALID_REQUEST" || ce.Reason != "INVALID_MEDIA_SESSION_ID" {
		t.Fatalf("got %v, want INVALID_REQUEST", err)
	}
}

func TestLaunchErrorIsReported(t *testing.T) {
	f := newFakeReceiver(t)
	f.launchError = "NOT_ALLOWED"

	_, err := cast.Connect(timeout(t), f.addr, heartbeat)
	var ce *cast.Error
	if !errors.As(err, &ce) || ce.Type != "LAUNCH_ERROR" || ce.Reason != "NOT_ALLOWED" {
		t.Fatalf("got %v, want LAUNCH_ERROR NOT_ALLOWED", err)
	}
}

func TestConnectToUnreachableReceiverFails(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	if _, err := cast.Connect(timeout(t), addr, heartbeat); err == nil {
		t.Fatal("connected to a closed port")
	}
}

func TestHeartbeatKeepsSessionAlive(t *testing.T) {
	f := newFakeReceiver(t)
	s := connect(t, f)

	eventually(t, func() bool { return f.count(&f.pings) >= 5 })
	f.push("receiver-0", nsBeat, map[string]any{"type": "PING"})
	eventually(t, func() bool { return f.count(&f.pongs) >= 1 })
	select {
	case <-s.Done():
		t.Fatalf("session ended: %v", s.Err())
	default:
	}
	if _, err := s.Load(timeout(t), "http://192.168.1.20:8080/t2/1.jpg", "image/jpeg"); err != nil {
		t.Fatal(err)
	}
}

func TestSilentReceiverIsDetected(t *testing.T) {
	f := newFakeReceiver(t)
	s := connect(t, f)
	f.setMute()

	loaded := make(chan error, 1)
	go func() {
		_, err := s.Load(context.Background(), "http://192.168.1.20:8080/t2/1.jpg", "image/jpeg")
		loaded <- err
	}()
	if err := waitDone(t, s, 10*heartbeat); !errors.Is(err, cast.ErrNoHeartbeat) {
		t.Fatalf("got %v, want ErrNoHeartbeat", err)
	}
	if err := <-loaded; !errors.Is(err, cast.ErrNoHeartbeat) {
		t.Fatalf("pending load got %v, want ErrNoHeartbeat", err)
	}
}

func TestReconnectAfterReceiverDropsConnection(t *testing.T) {
	f := newFakeReceiver(t)
	s := connect(t, f)
	f.drop()

	if err := waitDone(t, s, time.Second); !errors.Is(err, cast.ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}
	if _, err := s.Load(timeout(t), "http://192.168.1.20:8080/t2/1.jpg", "image/jpeg"); !errors.Is(err, cast.ErrClosed) {
		t.Fatalf("load on dropped session got %v, want ErrClosed", err)
	}

	s = connect(t, f)
	if _, err := s.Load(timeout(t), "http://192.168.1.20:8080/t2/2.jpg", "image/jpeg"); err != nil {
		t.Fatal(err)
	}
}

func TestPlaybackChangesAreReported(t *testing.T) {
	f := newFakeReceiver(t)
	s := connect(t, f)
	m, err := s.Load(timeout(t), "http://192.168.1.20:8080/v/2.mp4", "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	<-s.Changed()

	f.push(transport, nsMedia, map[string]any{"type": "MEDIA_STATUS", "requestId": 0, "status": []any{
		map[string]any{"mediaSessionId": m.SessionID, "playerState": "IDLE", "idleReason": "FINISHED"},
	}})
	select {
	case <-s.Changed():
	case <-time.After(5 * time.Second):
		t.Fatal("no change reported")
	}
	if got := s.Media(); got.SessionID != m.SessionID || got.State != "IDLE" || got.IdleReason != "FINISHED" {
		t.Fatalf("media %+v", got)
	}
}

func TestStoppedMediaReceiverEndsSession(t *testing.T) {
	for name, msg := range map[string]castMessage{
		"app gone":        {"receiver-0", "*", nsReceive, map[string]any{"type": "RECEIVER_STATUS", "requestId": 0, "status": map[string]any{"applications": []any{}}}},
		"transport close": {transport, "*", nsConn, map[string]any{"type": "CLOSE"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeReceiver(t)
			s := connect(t, f)
			f.push(msg.source, msg.namespace, msg.payload)
			if err := waitDone(t, s, time.Second); !errors.Is(err, cast.ErrSessionEnded) {
				t.Fatalf("got %v, want ErrSessionEnded", err)
			}
		})
	}
}

func TestOtherReceiverStatusKeepsSession(t *testing.T) {
	f := newFakeReceiver(t)
	s := connect(t, f)
	f.mu.Lock()
	status := f.receiverStatus()
	f.mu.Unlock()
	f.push("receiver-0", nsReceive, map[string]any{"type": "RECEIVER_STATUS", "requestId": 0, "status": status})

	if _, err := s.Load(timeout(t), "http://192.168.1.20:8080/t2/1.jpg", "image/jpeg"); err != nil {
		t.Fatal(err)
	}
}
