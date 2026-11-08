package diag

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"time"
)

type Kind string

const (
	Discovery  Kind = "discovery"
	Connection Kind = "connection"
	Sync       Kind = "sync"
	Timeout    Kind = "timeout"
	Renderer   Kind = "renderer"
	Media      Kind = "media"
)

type Event struct {
	Time    time.Time
	Kind    Kind
	Pile    []byte
	Peer    string
	Message string
	Count   int
}

func (e Event) same(o Event) bool {
	return e.Kind == o.Kind && bytes.Equal(e.Pile, o.Pile) && e.Peer == o.Peer && e.Message == o.Message
}

type Log struct {
	mu     sync.Mutex
	size   int
	events []Event
}

func New(size int) *Log {
	return &Log{size: size}
}

func (l *Log) Add(k Kind, pile []byte, peer, message string) {
	e := Event{Time: time.Now(), Kind: k, Pile: bytes.Clone(pile), Peer: peer, Message: message, Count: 1}
	l.mu.Lock()
	defer l.mu.Unlock()
	if i := slices.IndexFunc(l.events, e.same); i >= 0 {
		e.Count += l.events[i].Count
		l.events = slices.Delete(l.events, i, i+1)
	} else if len(l.events) == l.size {
		l.events = slices.Delete(l.events, 0, 1)
	}
	l.events = append(l.events, e)
}

func (l *Log) Error(k Kind, pile []byte, peer string, err error) {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout() {
		k = Timeout
	}
	l.Add(k, pile, peer, err.Error())
}

func (l *Log) Events() []Event {
	l.mu.Lock()
	out := slices.Clone(l.events)
	l.mu.Unlock()
	slices.Reverse(out)
	return out
}

func (l *Log) Export(w io.Writer) error {
	events := l.Events()
	slices.Reverse(events)
	for _, e := range events {
		line := e.Time.UTC().Format("2006-01-02T15:04:05.000Z") + " " + string(e.Kind)
		if len(e.Pile) > 0 {
			line += fmt.Sprintf(" pile=%x", e.Pile)
		}
		if e.Peer != "" {
			line += " peer=" + e.Peer
		}
		if e.Count > 1 {
			line += fmt.Sprintf(" x%d", e.Count)
		}
		if _, err := fmt.Fprintln(w, line+" "+e.Message); err != nil {
			return err
		}
	}
	return nil
}
