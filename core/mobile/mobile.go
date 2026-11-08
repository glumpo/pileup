package mobile

import (
	"crypto/rand"
	"errors"
	"net"
	"net/http"
	"sync"

	"github.com/glumpo/pileup/core/httpapi"
)

var (
	mu       sync.Mutex
	listener net.Listener
	server   *http.Server
	port     int
	token    string
)

func Start() error {
	mu.Lock()
	defer mu.Unlock()
	if server != nil {
		return nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	t := rand.Text()
	listener = ln
	server = &http.Server{Handler: httpapi.New(t)}
	port = ln.Addr().(*net.TCPAddr).Port
	token = t
	go server.Serve(ln)
	return nil
}

func Stop() error {
	mu.Lock()
	defer mu.Unlock()
	if server == nil {
		return nil
	}
	listener.Close()
	err := server.Close()
	listener, server, port, token = nil, nil, 0, ""
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func Port() int {
	mu.Lock()
	defer mu.Unlock()
	return port
}

func Token() string {
	mu.Lock()
	defer mu.Unlock()
	return token
}
