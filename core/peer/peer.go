package peer

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
)

const exporterLabel = "EXPORTER-Channel-Binding"

var (
	ErrCertificate = errors.New("peer: certificate is not self-signed ed25519")
	ErrMembership  = errors.New("peer: membership not proven")
	ErrPileID      = errors.New("peer: pile id longer than 255 bytes")
)

type Secrets func(pileID []byte) (secret []byte, ok bool)

type Conn struct {
	net.Conn
	tls     *tls.Conn
	self    ed25519.PublicKey
	member  func(c *Conn, exporter []byte) error
	secret  []byte
	secrets Secrets
	pileID  []byte
	peer    ed25519.PublicKey
	once    sync.Once
	err     error
}

func (c *Conn) Peer() ed25519.PublicKey { return c.peer }

func (c *Conn) PileID() []byte { return c.pileID }

func (c *Conn) Handshake() error {
	c.once.Do(func() {
		if c.err = c.handshake(); c.err != nil {
			c.tls.Close()
		}
	})
	return c.err
}

func (c *Conn) Read(b []byte) (int, error) {
	if err := c.Handshake(); err != nil {
		return 0, err
	}
	return c.tls.Read(b)
}

func (c *Conn) Write(b []byte) (int, error) {
	if err := c.Handshake(); err != nil {
		return 0, err
	}
	return c.tls.Write(b)
}

func (c *Conn) handshake() error {
	if err := c.tls.Handshake(); err != nil {
		return err
	}
	st := c.tls.ConnectionState()
	c.peer = st.PeerCertificates[0].PublicKey.(ed25519.PublicKey)
	exporter, err := st.ExportKeyingMaterial(exporterLabel, nil, 32)
	if err != nil {
		return err
	}
	return c.member(c, exporter)
}

func (c *Conn) client(exporter []byte) error {
	msg := append([]byte{byte(len(c.pileID))}, c.pileID...)
	if _, err := c.tls.Write(append(msg, proof(c.secret, exporter, c.self)...)); err != nil {
		return err
	}
	got := make([]byte, sha256.Size)
	if _, err := io.ReadFull(c.tls, got); err != nil {
		return err
	}
	if !hmac.Equal(got, proof(c.secret, exporter, c.peer)) {
		return ErrMembership
	}
	return nil
}

func (c *Conn) server(exporter []byte) error {
	n := make([]byte, 1)
	if _, err := io.ReadFull(c.tls, n); err != nil {
		return err
	}
	msg := make([]byte, int(n[0])+sha256.Size)
	if _, err := io.ReadFull(c.tls, msg); err != nil {
		return err
	}
	pileID, got := msg[:n[0]], msg[n[0]:]
	secret, ok := c.secrets(pileID)
	if !ok || !hmac.Equal(got, proof(secret, exporter, c.peer)) {
		return ErrMembership
	}
	c.pileID = pileID
	_, err := c.tls.Write(proof(secret, exporter, c.self))
	return err
}

func proof(secret, exporter []byte, pub ed25519.PublicKey) []byte {
	m := hmac.New(sha256.New, op.AuthKey(secret))
	m.Write(exporter)
	m.Write(pub)
	return m.Sum(nil)
}

func Dial(ctx context.Context, addr string, key ed25519.PrivateKey, pileID, secret []byte) (*Conn, error) {
	if len(pileID) > 255 {
		return nil, ErrPileID
	}
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if d, ok := ctx.Deadline(); ok {
		raw.SetDeadline(d)
	}
	c := newConn(tls.Client(raw, config(key)), key, (*Conn).client)
	c.pileID, c.secret = pileID, secret
	if err := c.Handshake(); err != nil {
		return nil, err
	}
	raw.SetDeadline(time.Time{})
	return c, nil
}

type listener struct {
	net.Listener
	key     ed25519.PrivateKey
	config  *tls.Config
	secrets Secrets
}

func NewListener(inner net.Listener, key ed25519.PrivateKey, secrets Secrets) net.Listener {
	return &listener{Listener: inner, key: key, config: config(key), secrets: secrets}
}

func (l *listener) Accept() (net.Conn, error) {
	raw, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	c := newConn(tls.Server(raw, l.config), l.key, (*Conn).server)
	c.secrets = l.secrets
	return c, nil
}

func newConn(t *tls.Conn, key ed25519.PrivateKey, member func(*Conn, []byte) error) *Conn {
	return &Conn{Conn: t, tls: t, self: key.Public().(ed25519.PublicKey), member: member}
}

func config(key ed25519.PrivateKey) *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{certificate(key)},
		ClientAuth:         tls.RequireAnyClientCert,
		InsecureSkipVerify: true,
		VerifyConnection:   verify,
	}
}

func certificate(key ed25519.PrivateKey) tls.Certificate {
	tmpl := &x509.Certificate{
		NotBefore: time.Unix(0, 0),
		NotAfter:  time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		panic(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func verify(st tls.ConnectionState) error {
	if len(st.PeerCertificates) != 1 {
		return ErrCertificate
	}
	cert := st.PeerCertificates[0]
	if _, ok := cert.PublicKey.(ed25519.PublicKey); !ok {
		return ErrCertificate
	}
	if cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) != nil {
		return ErrCertificate
	}
	return nil
}
