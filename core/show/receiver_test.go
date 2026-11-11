package show_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	nsConn    = "urn:x-cast:com.google.cast.tp.connection"
	nsBeat    = "urn:x-cast:com.google.cast.tp.heartbeat"
	nsReceive = "urn:x-cast:com.google.cast.receiver"
	nsMedia   = "urn:x-cast:com.google.cast.media"
	transport = "web-1"
)

type message struct {
	source, destination, namespace string
	payload                        map[string]any
}

type load struct {
	url, contentType string
	at               float64
}

type receiver struct {
	addr     string
	mu       sync.Mutex
	conns    []net.Conn
	dials    []time.Time
	refuse   bool
	fail     bool
	fetch    bool
	idle     bool
	rejects  string
	shown    []string
	media    int
	state    string
	time     float64
	loads    []load
	commands []string
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	if err != nil {
		t.Fatal(err)
	}
	r := &receiver{addr: ln.Addr().String()}
	t.Cleanup(func() {
		ln.Close()
		r.drop()
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			r.mu.Lock()
			r.dials = append(r.dials, time.Now())
			refuse := r.refuse
			if !refuse {
				r.conns = append(r.conns, c)
			}
			r.mu.Unlock()
			if refuse {
				c.Close()
				continue
			}
			go r.serve(c)
		}
	}()
	return r
}

func (r *receiver) serve(c net.Conn) {
	defer c.Close()
	for {
		m, err := readMessage(c)
		if err != nil {
			return
		}
		typ, _ := m.payload["type"].(string)
		id := m.payload["requestId"]
		var reply map[string]any
		switch {
		case m.namespace == nsBeat && typ == "PING":
			reply = map[string]any{"type": "PONG"}
		case m.namespace == nsReceive && typ == "LAUNCH":
			reply = map[string]any{"type": "RECEIVER_STATUS", "requestId": id, "status": map[string]any{"applications": []any{
				map[string]any{"appId": "CC1AD845", "sessionId": "session-1", "transportId": transport},
			}}}
		case m.namespace == nsMedia:
			reply = r.control(typ, id, m.payload)
		}
		if reply != nil {
			r.mu.Lock()
			writeMessage(c, message{m.destination, m.source, m.namespace, reply})
			r.mu.Unlock()
		}
	}
}

func (r *receiver) control(typ string, id any, p map[string]any) map[string]any {
	if typ == "LOAD" {
		return r.load(id, p)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, typ)
	if sid, _ := p["mediaSessionId"].(float64); r.media == 0 || int(sid) != r.media {
		return map[string]any{"type": "INVALID_REQUEST", "requestId": id, "reason": "INVALID_MEDIA_SESSION_ID"}
	}
	if r.state == "IDLE" && typ != "STOP" {
		return map[string]any{"type": "INVALID_REQUEST", "requestId": id, "reason": "INVALID_MEDIA_SESSION_ID"}
	}
	switch typ {
	case "PLAY":
		r.state = "PLAYING"
	case "PAUSE":
		r.state = "PAUSED"
	case "STOP":
		r.state = "IDLE"
		return r.status(id, "CANCELLED")
	}
	return r.status(id, "")
}

func (r *receiver) load(id any, p map[string]any) map[string]any {
	media, _ := p["media"].(map[string]any)
	url, _ := media["contentId"].(string)
	ct, _ := media["contentType"].(string)
	at, _ := p["currentTime"].(float64)
	r.mu.Lock()
	fetch := r.fetch
	r.mu.Unlock()
	body, fetched := "", true
	if fetch {
		body, fetched = download(url)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loads = append(r.loads, load{url, ct, at})
	if r.fail || !fetched {
		return map[string]any{"type": "LOAD_FAILED", "requestId": id, "detailedErrorCode": 905}
	}
	r.media++
	r.time = at
	if r.rejects != "" && strings.HasPrefix(ct, r.rejects) {
		r.state = "IDLE"
		return r.status(id, "ERROR")
	}
	if fetch {
		r.shown = append(r.shown, body)
	}
	if r.idle {
		r.state = "IDLE"
		return r.status(id, "ERROR")
	}
	r.state = "PLAYING"
	return r.status(id, "")
}

func download(url string) (string, bool) {
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return string(body), err == nil && resp.StatusCode == http.StatusOK
}

func (r *receiver) status(id any, idleReason string) map[string]any {
	st := map[string]any{"mediaSessionId": r.media, "playerState": r.state, "currentTime": r.time}
	if idleReason != "" {
		st["idleReason"] = idleReason
	}
	return map[string]any{"type": "MEDIA_STATUS", "requestId": id, "status": []any{st}}
}

func (r *receiver) push(state, idleReason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = state
	for _, c := range r.conns {
		writeMessage(c, message{transport, "*", nsMedia, r.status(0, idleReason)})
	}
}

func (r *receiver) progress(at float64) {
	r.mu.Lock()
	r.time = at
	r.mu.Unlock()
	r.push("PLAYING", "")
}

func (r *receiver) rejecting(contentType string) {
	r.mu.Lock()
	r.rejects = contentType
	r.mu.Unlock()
}

func (r *receiver) failLoads(fail bool) {
	r.mu.Lock()
	r.fail = fail
	r.mu.Unlock()
}

func (r *receiver) refusing(refuse bool) {
	r.mu.Lock()
	r.refuse = refuse
	r.mu.Unlock()
}

func (r *receiver) dialed() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.dials...)
}

func (r *receiver) fetching() {
	r.mu.Lock()
	r.fetch = true
	r.mu.Unlock()
}

func (r *receiver) failPlayback(idle bool) {
	r.mu.Lock()
	r.idle = idle
	r.mu.Unlock()
}

func (r *receiver) displayed() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.shown...)
}

func (r *receiver) drop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.conns {
		c.Close()
	}
	r.conns = nil
}

func (r *receiver) loaded() []load {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]load(nil), r.loads...)
}

func (r *receiver) commanded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.commands...)
}

func (r *receiver) waitLoad(t *testing.T, n int) load {
	t.Helper()
	eventually(t, "LOAD", func() bool { return len(r.loaded()) >= n })
	l := r.loaded()
	if len(l) != n {
		t.Fatalf("receiver got %d LOADs, want %d: %v", len(l), n, l)
	}
	return l[n-1]
}

func writeMessage(w io.Writer, m message) error {
	payload, err := json.Marshal(m.payload)
	if err != nil {
		return err
	}
	var b []byte
	b = binary.AppendUvarint(b, 1<<3|0)
	b = binary.AppendUvarint(b, 0)
	for i, s := range []string{m.source, m.destination, m.namespace} {
		b = binary.AppendUvarint(b, uint64(i+2)<<3|2)
		b = binary.AppendUvarint(b, uint64(len(s)))
		b = append(b, s...)
	}
	b = binary.AppendUvarint(b, 5<<3|0)
	b = binary.AppendUvarint(b, 0)
	b = binary.AppendUvarint(b, 6<<3|2)
	b = binary.AppendUvarint(b, uint64(len(payload)))
	b = append(b, payload...)
	_, err = w.Write(append(binary.BigEndian.AppendUint32(nil, uint32(len(b))), b...))
	return err
}

func readMessage(r io.Reader) (message, error) {
	var m message
	var n [4]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return m, err
	}
	b := make([]byte, binary.BigEndian.Uint32(n[:]))
	if _, err := io.ReadFull(r, b); err != nil {
		return m, err
	}
	fields := map[uint64][]byte{}
	for len(b) > 0 {
		key, k := binary.Uvarint(b)
		b = b[k:]
		v, k := binary.Uvarint(b)
		if key&7 == 2 {
			fields[key>>3] = b[k : k+int(v)]
			b = b[k+int(v):]
		} else {
			b = b[k:]
		}
	}
	m.source, m.destination, m.namespace = string(fields[2]), string(fields[3]), string(fields[4])
	err := json.Unmarshal(fields[6], &m.payload)
	return m, err
}
