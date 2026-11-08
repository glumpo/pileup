package peer_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/peer"
)

type accepted struct {
	conn *peer.Conn
	err  error
}

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func pub(k ed25519.PrivateKey) ed25519.PublicKey { return k.Public().(ed25519.PublicKey) }

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func piles(pileID, secret []byte) peer.Secrets {
	return func(id []byte) ([]byte, bool) {
		if bytes.Equal(id, pileID) {
			return secret, true
		}
		return nil, false
	}
}

func tcpListen(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func serve(t *testing.T, key ed25519.PrivateKey, secrets peer.Secrets) (string, <-chan accepted) {
	t.Helper()
	l := peer.NewListener(tcpListen(t), key, secrets)
	out := make(chan accepted, 1)
	conns := make(chan net.Conn, 1)
	t.Cleanup(func() {
		select {
		case c := <-conns:
			c.Close()
		default:
		}
	})
	go func() {
		c, err := l.Accept()
		if err != nil {
			out <- accepted{err: err}
			return
		}
		conns <- c
		c.SetDeadline(time.Now().Add(5 * time.Second))
		pc := c.(*peer.Conn)
		out <- accepted{conn: pc, err: pc.Handshake()}
	}()
	return l.Addr().String(), out
}

func dial(t *testing.T, addr string, key ed25519.PrivateKey, pileID, secret []byte) (*peer.Conn, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := peer.Dial(ctx, addr, key, pileID, secret)
	if err == nil {
		t.Cleanup(func() { c.Close() })
	}
	return c, err
}

func cert(t *testing.T, key, signer ed25519.PrivateKey) tls.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), signer)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func rawConfig(c tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates:       []tls.Certificate{c},
		InsecureSkipVerify: true,
		ClientAuth:         tls.RequireAnyClientCert,
	}
}

func TestConnect(t *testing.T) {
	a, b := newKey(t), newKey(t)
	pileID, secret := randBytes(t, 16), randBytes(t, 32)
	addr, srv := serve(t, b, piles(pileID, secret))

	ca, err := dial(t, addr, a, pileID, secret)
	if err != nil {
		t.Fatal(err)
	}
	s := <-srv
	if s.err != nil {
		t.Fatal(s.err)
	}
	if !ca.Peer().Equal(pub(b)) || !s.conn.Peer().Equal(pub(a)) {
		t.Fatal("peers learned wrong public keys")
	}
	if !bytes.Equal(ca.PileID(), pileID) || !bytes.Equal(s.conn.PileID(), pileID) {
		t.Fatal("peers learned wrong pile id")
	}

	if _, err := ca.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(s.conn, got); err != nil || string(got) != "ping" {
		t.Fatalf("server read %q, %v", got, err)
	}
	if _, err := s.conn.Write([]byte("pong")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(ca, got); err != nil || string(got) != "pong" {
		t.Fatalf("client read %q, %v", got, err)
	}
}

func TestHTTPOverConnection(t *testing.T) {
	a, b := newKey(t), newKey(t)
	pileID, secret := randBytes(t, 16), randBytes(t, 32)
	l := peer.NewListener(tcpListen(t), b, piles(pileID, secret))
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	})}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return peer.Dial(ctx, addr, a, pileID, secret)
		},
	}, Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	resp, err := client.Get("http://" + l.Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "hello" {
		t.Fatalf("body %q, %v", body, err)
	}
}

func TestMembershipRejected(t *testing.T) {
	pileID, secret := randBytes(t, 16), randBytes(t, 32)
	other := randBytes(t, 32)
	cases := []struct {
		name          string
		dialerSecret  []byte
		listenerPiles peer.Secrets
	}{
		{"dialer does not know secret", other, piles(pileID, secret)},
		{"listener does not know secret", secret, piles(pileID, other)},
		{"listener does not have pile", secret, piles(randBytes(t, 16), secret)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr, srv := serve(t, newKey(t), tc.listenerPiles)
			if _, err := dial(t, addr, newKey(t), pileID, tc.dialerSecret); err == nil {
				t.Fatal("dialer accepted the connection")
			}
			if s := <-srv; s.err == nil {
				t.Fatal("listener accepted the connection")
			}
		})
	}
}

func relay(t *testing.T, toDialer, toListener tls.Certificate, target string) string {
	t.Helper()
	l := tcpListen(t)
	go func() {
		raw, err := l.Accept()
		if err != nil {
			return
		}
		down := tls.Server(raw, rawConfig(toDialer))
		defer down.Close()
		up, err := tls.Dial("tcp", target, rawConfig(toListener))
		if err != nil {
			return
		}
		defer up.Close()
		done := make(chan struct{}, 2)
		go func() { io.Copy(up, down); done <- struct{}{} }()
		go func() { io.Copy(down, up); done <- struct{}{} }()
		<-done
	}()
	return l.Addr().String()
}

func TestRelayThroughMiddleman(t *testing.T) {
	a, b, m := newKey(t), newKey(t), newKey(t)
	cases := []struct {
		name       string
		toDialer   tls.Certificate
		toListener tls.Certificate
	}{
		{"middleman uses own key", cert(t, m, m), cert(t, m, m)},
		{"middleman uses stolen dialer key", cert(t, m, m), cert(t, a, a)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pileID, secret := randBytes(t, 16), randBytes(t, 32)
			addr, srv := serve(t, b, piles(pileID, secret))
			mitm := relay(t, tc.toDialer, tc.toListener, addr)
			if _, err := dial(t, mitm, a, pileID, secret); err == nil {
				t.Fatal("dialer accepted the relayed connection")
			}
			if s := <-srv; s.err == nil {
				t.Fatal("listener accepted the relayed connection")
			}
		})
	}
}

type rawResult struct {
	state tls.ConnectionState
	err   error
}

func rawServe(t *testing.T, cfg *tls.Config, then func(*tls.Conn)) (string, <-chan rawResult) {
	t.Helper()
	l := tcpListen(t)
	out := make(chan rawResult, 1)
	go func() {
		raw, err := l.Accept()
		if err != nil {
			return
		}
		c := tls.Server(raw, cfg)
		defer c.Close()
		c.SetDeadline(time.Now().Add(5 * time.Second))
		err = c.Handshake()
		if err == nil && then != nil {
			then(c)
		}
		out <- rawResult{c.ConnectionState(), err}
	}()
	return l.Addr().String(), out
}

func rawDial(t *testing.T, addr string, cfg *tls.Config) (*tls.Conn, error) {
	t.Helper()
	c, err := tls.Dial("tcp", addr, cfg)
	if err == nil {
		t.Cleanup(func() { c.Close() })
	}
	return c, err
}

func assertSelfSigned(t *testing.T, st tls.ConnectionState, key ed25519.PrivateKey) {
	t.Helper()
	if st.Version != tls.VersionTLS13 {
		t.Fatalf("version %x", st.Version)
	}
	if len(st.PeerCertificates) != 1 {
		t.Fatalf("%d certificates", len(st.PeerCertificates))
	}
	c := st.PeerCertificates[0]
	if k, ok := c.PublicKey.(ed25519.PublicKey); !ok || !k.Equal(pub(key)) {
		t.Fatal("certificate is not on the device key")
	}
	if err := c.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature); err != nil {
		t.Fatal(err)
	}
}

func TestCertificateIsSelfSignedDeviceKey(t *testing.T) {
	t.Run("listener", func(t *testing.T) {
		b := newKey(t)
		addr, _ := serve(t, b, piles(nil, nil))
		c, err := rawDial(t, addr, rawConfig(cert(t, newKey(t), newKey(t))))
		if err != nil {
			t.Fatal(err)
		}
		assertSelfSigned(t, c.ConnectionState(), b)
	})
	t.Run("dialer", func(t *testing.T) {
		m, a := newKey(t), newKey(t)
		addr, srv := rawServe(t, rawConfig(cert(t, m, m)), nil)
		dial(t, addr, a, randBytes(t, 16), randBytes(t, 32))
		assertSelfSigned(t, (<-srv).state, a)
	})
}

func TestForgedCertificateRejected(t *testing.T) {
	a, m := newKey(t), newKey(t)
	forged := cert(t, a, m)
	t.Run("from dialer", func(t *testing.T) {
		addr, srv := serve(t, newKey(t), piles(nil, nil))
		rawDial(t, addr, rawConfig(forged))
		if s := <-srv; !errors.Is(s.err, peer.ErrCertificate) {
			t.Fatalf("listener: %v", s.err)
		}
	})
	t.Run("from listener", func(t *testing.T) {
		addr, _ := rawServe(t, rawConfig(forged), nil)
		if _, err := dial(t, addr, newKey(t), randBytes(t, 16), randBytes(t, 32)); !errors.Is(err, peer.ErrCertificate) {
			t.Fatalf("dialer: %v", err)
		}
	})
}

func TestOnlyTLS13(t *testing.T) {
	m := newKey(t)
	cfg := rawConfig(cert(t, m, m))
	cfg.MaxVersion = tls.VersionTLS12
	t.Run("dialer offers TLS 1.2", func(t *testing.T) {
		addr, srv := serve(t, newKey(t), piles(nil, nil))
		if _, err := rawDial(t, addr, cfg); err == nil {
			t.Fatal("TLS 1.2 negotiated with listener")
		}
		if s := <-srv; s.err == nil {
			t.Fatal("listener accepted TLS 1.2")
		}
	})
	t.Run("listener offers TLS 1.2", func(t *testing.T) {
		addr, srv := rawServe(t, cfg, nil)
		dial(t, addr, newKey(t), randBytes(t, 16), randBytes(t, 32))
		if s := <-srv; s.err == nil {
			t.Fatalf("dialer negotiated version %x", s.state.Version)
		}
	})
}

func TestListenerReflectingDialerProofRejected(t *testing.T) {
	m := newKey(t)
	reflect := func(c *tls.Conn) {
		n := make([]byte, 1)
		io.ReadFull(c, n)
		msg := make([]byte, int(n[0])+32)
		io.ReadFull(c, msg)
		c.Write(msg[n[0]:])
	}
	addr, _ := rawServe(t, rawConfig(cert(t, m, m)), reflect)
	if _, err := dial(t, addr, newKey(t), randBytes(t, 16), randBytes(t, 32)); !errors.Is(err, peer.ErrMembership) {
		t.Fatalf("dialer: %v", err)
	}
}

func TestLongPileIDRejected(t *testing.T) {
	pileID, secret := randBytes(t, 256), randBytes(t, 32)
	addr, _ := serve(t, newKey(t), piles(pileID, secret))
	if _, err := dial(t, addr, newKey(t), pileID, secret); !errors.Is(err, peer.ErrPileID) {
		t.Fatalf("dialer: %v", err)
	}
}
