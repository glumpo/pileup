package diag

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
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
	App        Kind = "app"
	Crash      Kind = "crash"
)

const (
	fileName   = "diag.log"
	fileLimit  = 256 << 10
	resave     = time.Minute
	maxMessage = 4 << 10
)

var invites = regexp.MustCompile(`(?i)pileup:[a-z2-7]+`)

type Event struct {
	Time    time.Time
	Kind    Kind
	Pile    []byte
	Peer    string
	Message string
	Count   int

	saved   int
	savedAt time.Time
}

func (e Event) same(o Event) bool {
	return e.Kind == o.Kind && bytes.Equal(e.Pile, o.Pile) && e.Peer == o.Peer && e.Message == o.Message
}

type Log struct {
	mu      sync.Mutex
	size    int
	events  []Event
	path    string
	file    *os.File
	written int64
	failed  time.Time
	closed  bool
}

type record struct {
	Time    int64  `json:"time"`
	Kind    Kind   `json:"kind"`
	Pile    []byte `json:"pile,omitempty"`
	Peer    string `json:"peer,omitempty"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}

func New(size int) *Log {
	return &Log{size: size}
}

func Open(dir string, size int) *Log {
	l := &Log{size: size, path: filepath.Join(dir, fileName)}
	l.load()
	l.open()
	return l
}

func (l *Log) load() {
	var lines [][]byte
	for _, p := range []string{l.path + ".1", l.path} {
		if f, err := os.Open(p); err == nil {
			b, _ := io.ReadAll(io.LimitReader(f, 2*fileLimit))
			f.Close()
			lines = append(lines, bytes.Split(b, []byte("\n"))...)
		}
	}
	seen := map[string]bool{}
	for _, line := range slices.Backward(lines) {
		var r record
		if json.Unmarshal(line, &r) != nil || r.Count < 1 {
			continue
		}
		key := fmt.Sprintf("%s\x00%x\x00%s\x00%s", r.Kind, r.Pile, r.Peer, r.Message)
		if seen[key] {
			continue
		}
		seen[key] = true
		t := time.UnixMilli(r.Time)
		l.events = append(l.events, Event{Time: t, Kind: r.Kind, Pile: r.Pile, Peer: r.Peer, Message: r.Message, Count: r.Count, saved: r.Count, savedAt: t})
	}
	slices.Reverse(l.events)
	slices.SortStableFunc(l.events, func(a, b Event) int { return a.Time.Compare(b.Time) })
	l.events = l.events[max(0, len(l.events)-l.size):]
}

func (l *Log) open() bool {
	err := os.MkdirAll(filepath.Dir(l.path), 0o700)
	var f *os.File
	if err == nil {
		f, err = os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	}
	var info os.FileInfo
	if err == nil {
		info, err = f.Stat()
	}
	if err == nil && info.Size() > 0 {
		last := make([]byte, 1)
		if _, err = f.ReadAt(last, info.Size()-1); err == nil && last[0] != '\n' {
			_, err = f.Write([]byte("\n"))
		}
	}
	if err != nil {
		if f != nil {
			f.Close()
		}
		l.fail(err)
		return false
	}
	l.file, l.written = f, info.Size()
	return true
}

func (l *Log) fail(err error) {
	l.failed = time.Now()
	l.keep(Event{Time: l.failed, Kind: App, Message: "diagnostics are not saved to disk: " + err.Error(), Count: 1})
}

func (l *Log) save(e *Event) {
	if l.file == nil && (l.closed || l.path == "" || time.Since(l.failed) < resave || !l.open()) {
		return
	}
	line, _ := json.Marshal(record{Time: e.Time.UnixMilli(), Kind: e.Kind, Pile: e.Pile, Peer: e.Peer, Message: e.Message, Count: e.Count})
	n, err := l.file.Write(append(line, '\n'))
	l.written += int64(n)
	if err == nil {
		e.saved, e.savedAt = e.Count, e.Time
	}
	if err == nil && l.written >= fileLimit {
		err = errors.Join(l.file.Close(), os.Rename(l.path, l.path+".1"))
		l.file = nil
		if err == nil {
			l.open()
			return
		}
	}
	if err != nil {
		if l.file != nil {
			l.file.Close()
			l.file = nil
		}
		l.fail(err)
	}
}

func (l *Log) keep(e Event) *Event {
	if i := slices.IndexFunc(l.events, e.same); i >= 0 {
		l.events = slices.Delete(l.events, i, i+1)
	} else if len(l.events) == l.size {
		l.events = slices.Delete(l.events, 0, 1)
	}
	l.events = append(l.events, e)
	return &l.events[len(l.events)-1]
}

func (l *Log) Add(k Kind, pile []byte, peer, message string) {
	message = invites.ReplaceAllString(message, "PILEUP:…")
	if len(message) > maxMessage {
		message = strings.ToValidUTF8(message[:maxMessage], "") + "…"
	}
	e := Event{Time: time.Now(), Kind: k, Pile: bytes.Clone(pile), Peer: peer, Message: message, Count: 1}
	l.mu.Lock()
	defer l.mu.Unlock()
	if i := slices.IndexFunc(l.events, e.same); i >= 0 {
		e.Count += l.events[i].Count
		e.saved, e.savedAt = l.events[i].saved, l.events[i].savedAt
	}
	if last := l.keep(e); last.Time.Sub(last.savedAt) >= resave {
		l.save(last)
	}
}

func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.events {
		if l.events[i].saved < l.events[i].Count {
			l.save(&l.events[i])
		}
	}
	l.closed = true
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
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
		if _, err := fmt.Fprintln(w, e); err != nil {
			return err
		}
	}
	return nil
}

func (e Event) String() string {
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
	return line + " " + e.Message
}
