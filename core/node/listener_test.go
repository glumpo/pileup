package node_test

import (
	"context"
	"crypto/ed25519"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/invite"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/peer"
)

type socket struct {
	net.Listener
	mu     sync.Mutex
	death  error
	closed bool
}

func (s *socket) Accept() (net.Conn, error) {
	c, err := s.Listener.Accept()
	s.mu.Lock()
	defer s.mu.Unlock()
	if (s.closed || s.death != nil) && c != nil {
		c.Close()
	}
	switch {
	case s.closed:
		return nil, net.ErrClosed
	case s.death != nil:
		return nil, s.death
	}
	return c, err
}

func (s *socket) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return s.Listener.Close()
}

func (s *socket) die(err error) {
	s.mu.Lock()
	s.death = err
	s.mu.Unlock()
	s.Listener.Close()
}

type sockets struct {
	mu   sync.Mutex
	all  []*socket
	addr []string
}

func listenOnSockets(t *testing.T) *sockets {
	ss := &sockets{}
	t.Cleanup(node.SetListen(func(network, addr string) (net.Listener, error) {
		ln, err := net.Listen(network, addr)
		ss.mu.Lock()
		defer ss.mu.Unlock()
		ss.addr = append(ss.addr, addr)
		if err != nil {
			return nil, err
		}
		s := &socket{Listener: ln}
		ss.all = append(ss.all, s)
		return s, nil
	}))
	return ss
}

func (ss *sockets) last() (*socket, int) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.all[len(ss.all)-1], len(ss.addr)
}

func accepts(t *testing.T, n *node.Node, p node.Pile) func() bool {
	t.Helper()
	i, err := invite.Parse(inviteTo(t, n, p))
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c, err := peer.Dial(ctx, i.Addrs[0].String(), key, i.PileID, i.Secret)
		if err == nil {
			c.Close()
		}
		return err == nil
	}
}

func acceptError(errno syscall.Errno) error {
	return &net.OpError{Op: "accept", Net: "tcp", Err: os.NewSyscallError("accept", errno)}
}

func TestPilePortAcceptsAgainAfterAcceptFails(t *testing.T) {
	ss := listenOnSockets(t)
	a := open(t, t.TempDir(), "ios")
	p := create(t, a, "Грузия", "Аня")
	ok := accepts(t, a, p)
	if !ok() {
		t.Fatal("pile port does not accept before the failure")
	}
	s, _ := ss.last()
	s.die(acceptError(syscall.EBADF))
	eventually(t, "the pile port accepts again on the same port", ok)
	eventually(t, "the failure is logged", logged(a, diag.Connection, p, "", "stopped accepting"))
}

func TestReconnectReopensPilePortStuckOnAcceptErrors(t *testing.T) {
	ss := listenOnSockets(t)
	a := open(t, t.TempDir(), "ios")
	p := create(t, a, "Грузия", "Аня")
	ok := accepts(t, a, p)
	s, _ := ss.last()
	s.die(acceptError(syscall.ECONNABORTED))
	time.Sleep(500 * time.Millisecond)
	if ok() {
		t.Fatal("dead socket still accepts")
	}
	a.Reconnect("app returned to foreground")
	eventually(t, "the pile port accepts again on the same port", ok)
	eventually(t, "the reopening is logged", logged(a, diag.Connection, p, "", "does not accept connections"))
}

func TestReconnectKeepsWorkingPilePort(t *testing.T) {
	ss := listenOnSockets(t)
	a := open(t, t.TempDir(), "ios")
	p := create(t, a, "Грузия", "Аня")
	ok := accepts(t, a, p)
	s, listens := ss.last()
	for range 3 {
		a.Reconnect("app returned to foreground")
	}
	time.Sleep(500 * time.Millisecond)
	if now, n := ss.last(); now != s || n != listens || !ok() {
		t.Fatalf("Reconnect reopened a working pile port: listens %d → %d, accepts %v", listens, n, ok())
	}
	if logged(a, diag.Connection, p, "", "accept")() {
		dump(t, a)
		t.Fatal("Reconnect logged a listener failure for a working pile port")
	}
}
