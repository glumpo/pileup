package mobile

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/glumpo/pileup/core/diag"
	"github.com/glumpo/pileup/core/httpapi"
	"github.com/glumpo/pileup/core/node"
	"github.com/glumpo/pileup/core/show"
)

type Keystore interface {
	Load() ([]byte, error)
	Save(seed []byte) error
}

type MediaProvider interface {
	Media(from, to int64) ([]byte, error)
}

type RenditionMaker interface {
	Rendition(localID string, tier int) ([]byte, error)
	OriginalSize(localID string) (int64, error)
	ReadOriginal(localID string, offset, length int64) ([]byte, error)
}

type NetworkInfo interface {
	Addrs() (string, error)
}

type DiscoveryProvider interface {
	Advertise(service, name string, port int, txt []byte) error
	Browse(service string, handler DiscoveryHandler) error
	Stop() error
}

type DiscoveryHandler interface {
	Found(name, host string, port int, txt []byte)
	Lost(name string)
}

type nativeDiscovery struct{ DiscoveryProvider }

func (d nativeDiscovery) Browse(service string, handler node.DiscoveryHandler) error {
	return d.DiscoveryProvider.Browse(service, handler)
}

var (
	mu       sync.Mutex
	core     *node.Node
	shows    *show.Host
	listener net.Listener
	server   *http.Server
	drain    func()
	port     int
	token    string

	reports sync.Mutex
	journal *diag.Log
	hidden  string
	pending []diag.Event
)

const maxPending = 100

func Report(kind, message string) {
	reports.Lock()
	defer reports.Unlock()
	if hidden != "" {
		message = strings.ReplaceAll(message, hidden, "…")
	}
	if journal != nil {
		journal.Add(diag.Kind(kind), nil, "", message)
	} else if len(pending) < maxPending {
		pending = append(pending, diag.Event{Kind: diag.Kind(kind), Message: message})
	}
}

func Start(dir, platform, appVersion string, keystore Keystore, media MediaProvider, renditions RenditionMaker, network NetworkInfo, discovery DiscoveryProvider) error {
	mu.Lock()
	defer mu.Unlock()
	if server != nil {
		return nil
	}
	os.MkdirAll(dir, 0o700)
	crash, crashErr := os.OpenFile(filepath.Join(dir, "crash.txt"), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	var crashed []byte
	var crashedAt time.Time
	if crashErr == nil {
		defer crash.Close()
		crashed, _ = io.ReadAll(io.LimitReader(crash, 4<<10))
		if info, err := crash.Stat(); err == nil {
			crashedAt = info.ModTime()
		}
		crashErr = debug.SetCrashOutput(crash, debug.CrashOptions{})
	}
	n, err := node.Open(node.Config{
		Dir:        dir,
		Keystore:   keystore,
		Platform:   platform,
		AppVersion: appVersion,
		Addrs:      addrs(network),
		Media:      media,
		Renditions: renditions,
		Discovery:  nativeDiscovery{discovery},
	})
	if err != nil {
		return err
	}
	if crashErr != nil {
		n.Diag().Error(diag.App, nil, "", fmt.Errorf("crash output: %w", crashErr))
	}
	if len(crashed) > 0 {
		n.Diag().Add(diag.Crash, nil, "", fmt.Sprintf("previous run crashed at %s: %s", crashedAt.UTC().Format(time.RFC3339), crashed))
		crash.Truncate(0)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		n.Close()
		return err
	}
	t := rand.Text()
	ctx, cancel := context.WithCancel(context.Background())
	var running sync.RWMutex
	core = n
	shows = show.New(n)
	listener = ln
	api := httpapi.WithNode(t, n, shows)
	server = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !running.TryRLock() {
				panic(http.ErrAbortHandler)
			}
			defer running.RUnlock()
			api.ServeHTTP(w, r)
		}),
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	drain = func() {
		cancel()
		running.Lock()
	}
	port = ln.Addr().(*net.TCPAddr).Port
	token = t
	go server.Serve(ln)
	reports.Lock()
	journal, hidden = n.Diag(), t
	for _, e := range pending {
		journal.Add(e.Kind, nil, "", e.Message)
	}
	pending = nil
	reports.Unlock()
	return nil
}

func addrs(network NetworkInfo) func() []netip.Addr {
	return func() []netip.Addr {
		s, err := network.Addrs()
		var list []string
		if err != nil || json.Unmarshal([]byte(s), &list) != nil {
			return nil
		}
		var out []netip.Addr
		for _, x := range list {
			a, err := netip.ParseAddr(x)
			a = a.Unmap().WithZone("")
			if err == nil && a.IsGlobalUnicast() {
				out = append(out, a)
			}
		}
		return out
	}
}

func Stop() error {
	mu.Lock()
	defer mu.Unlock()
	if server == nil {
		return nil
	}
	listener.Close()
	err := server.Close()
	if errors.Is(err, net.ErrClosed) {
		err = nil
	}
	drain()
	reports.Lock()
	journal = nil
	reports.Unlock()
	shows.Close()
	err = errors.Join(err, core.Close())
	core, shows, listener, server, drain, port, token = nil, nil, nil, nil, nil, 0, ""
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
