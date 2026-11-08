package mobile_test

import (
	"net"
	"net/http"
	"strconv"
	"testing"

	"github.com/glumpo/pileup/core/mobile"
)

func start(t *testing.T) {
	t.Helper()
	if err := mobile.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mobile.Stop() })
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

func TestStartServesHealthOnLoopbackWithToken(t *testing.T) {
	start(t)
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

func TestStopClosesPort(t *testing.T) {
	start(t)
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
	start(t)
	oldToken := mobile.Token()
	if err := mobile.Stop(); err != nil {
		t.Fatal(err)
	}
	start(t)
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
	start(t)
	port, token := mobile.Port(), mobile.Token()
	start(t)
	if mobile.Port() != port || mobile.Token() != token {
		t.Fatalf("second Start changed port/token: %d %q -> %d %q", port, token, mobile.Port(), mobile.Token())
	}
	if code := health(t, port, token); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
}
