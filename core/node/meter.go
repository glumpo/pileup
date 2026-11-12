package node

import (
	"net/http"
	"sync"
	"time"
)

const window = 5

type meter struct {
	mu    sync.Mutex
	total int64
	bytes [window]int64
	secs  [window]int64
}

func (m *meter) add(k int) {
	now := time.Now().Unix()
	m.mu.Lock()
	defer m.mu.Unlock()
	i := now % window
	if m.secs[i] != now {
		m.secs[i], m.bytes[i] = now, 0
	}
	m.bytes[i] += int64(k)
	m.total += int64(k)
}

type counted struct {
	http.ResponseWriter
	m *meter
}

func (w counted) Write(b []byte) (int, error) {
	k, err := w.ResponseWriter.Write(b)
	w.m.add(k)
	return k, err
}

func (n *Node) Streamed() (total int64, rate float64) {
	m := &n.streamed
	now := time.Now().Unix()
	m.mu.Lock()
	defer m.mu.Unlock()
	var sum int64
	for i, s := range m.secs {
		if s > now-window {
			sum += m.bytes[i]
		}
	}
	return m.total, float64(sum) / window
}

func (n *Node) Serving() bool { return false }
