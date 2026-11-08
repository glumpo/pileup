package cast_test

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
	transport = "web-7"
)

type castMessage struct {
	source, destination, namespace string
	payload                        map[string]any
}

type load struct{ url, contentType string }

type fakeReceiver struct {
	t           *testing.T
	addr        string
	mu          sync.Mutex
	conns       []net.Conn
	launchError string
	mute        bool
	pings       int
	pongs       int
	session     string
	media       int
	state       string
	loads       []load
	received    []castMessage
}

func newFakeReceiver(t *testing.T) *fakeReceiver {
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
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeReceiver{t: t, addr: ln.Addr().String()}
	t.Cleanup(func() {
		ln.Close()
		f.drop()
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.conns = append(f.conns, c)
			f.mu.Unlock()
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeReceiver) serve(c net.Conn) {
	defer c.Close()
	connected := map[string]bool{}
	send := func(src, ns string, payload map[string]any) {
		f.mu.Lock()
		mute := f.mute
		f.mu.Unlock()
		if mute {
			return
		}
		writeMessage(c, castMessage{src, "sender-0", ns, payload})
	}
	for {
		m, err := readMessage(c)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.received = append(f.received, m)
		f.mu.Unlock()
		typ, _ := m.payload["type"].(string)
		id := m.payload["requestId"]
		switch {
		case m.namespace == nsConn && typ == "CONNECT":
			connected[m.destination] = true
		case m.namespace == nsBeat && typ == "PING":
			f.mu.Lock()
			f.pings++
			f.mu.Unlock()
			send(m.destination, nsBeat, map[string]any{"type": "PONG"})
		case m.namespace == nsBeat && typ == "PONG":
			f.mu.Lock()
			f.pongs++
			f.mu.Unlock()
		case m.namespace == nsReceive && m.destination == "receiver-0" && connected["receiver-0"] && typ == "LAUNCH":
			f.mu.Lock()
			reason := f.launchError
			if m.payload["appId"] != "CC1AD845" {
				reason = "NOT_FOUND"
			}
			if reason == "" && f.session == "" {
				f.session = "session-1"
			}
			status := f.receiverStatus()
			f.mu.Unlock()
			if reason != "" {
				send("receiver-0", nsReceive, map[string]any{"type": "LAUNCH_ERROR", "requestId": id, "reason": reason})
				continue
			}
			send("receiver-0", nsReceive, map[string]any{"type": "RECEIVER_STATUS", "requestId": id, "status": status})
		case m.namespace == nsMedia && m.destination == transport && connected[transport]:
			send(transport, nsMedia, f.mediaCommand(typ, id, m.payload))
		}
	}
}

func (f *fakeReceiver) receiverStatus() map[string]any {
	apps := []any{}
	if f.session != "" {
		apps = append(apps, map[string]any{
			"appId":       "CC1AD845",
			"displayName": "Default Media Receiver",
			"sessionId":   f.session,
			"transportId": transport,
		})
	}
	return map[string]any{"applications": apps}
}

func (f *fakeReceiver) mediaCommand(typ string, id any, p map[string]any) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if typ == "LOAD" {
		media, _ := p["media"].(map[string]any)
		url, _ := media["contentId"].(string)
		ct, _ := media["contentType"].(string)
		if strings.Contains(url, "broken") {
			return map[string]any{"type": "LOAD_FAILED", "requestId": id, "detailedErrorCode": 905}
		}
		f.loads = append(f.loads, load{url, ct})
		f.media++
		f.state = "PLAYING"
		return f.mediaStatus(id, "")
	}
	if sid, _ := p["mediaSessionId"].(float64); f.media == 0 || int(sid) != f.media {
		return map[string]any{"type": "INVALID_REQUEST", "requestId": id, "reason": "INVALID_MEDIA_SESSION_ID"}
	}
	switch typ {
	case "PLAY":
		f.state = "PLAYING"
	case "PAUSE":
		f.state = "PAUSED"
	case "STOP":
		f.state = "IDLE"
		return f.mediaStatus(id, "CANCELLED")
	}
	return f.mediaStatus(id, "")
}

func (f *fakeReceiver) mediaStatus(id any, idleReason string) map[string]any {
	st := map[string]any{"mediaSessionId": f.media, "playerState": f.state, "playbackRate": 1, "currentTime": 0}
	if idleReason != "" {
		st["idleReason"] = idleReason
	}
	return map[string]any{"type": "MEDIA_STATUS", "requestId": id, "status": []any{st}}
}

func (f *fakeReceiver) push(src, ns string, payload map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.conns {
		writeMessage(c, castMessage{src, "*", ns, payload})
	}
}

func (f *fakeReceiver) drop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.conns {
		c.Close()
	}
	f.conns = nil
}

func (f *fakeReceiver) setMute() {
	f.mu.Lock()
	f.mute = true
	f.mu.Unlock()
}

func (f *fakeReceiver) count(n *int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *n
}

func (f *fakeReceiver) loaded() []load {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]load(nil), f.loads...)
}

func writeMessage(w io.Writer, m castMessage) error {
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

func readMessage(r io.Reader) (castMessage, error) {
	var m castMessage
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
			fields[key>>3] = nil
			b = b[k:]
		}
	}
	m.source, m.destination, m.namespace = string(fields[2]), string(fields[3]), string(fields[4])
	err := json.Unmarshal(fields[6], &m.payload)
	return m, err
}
