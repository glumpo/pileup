package mobile

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"sync"

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
	port     int
	token    string
)

func Start(dir, platform, appVersion string, keystore Keystore, media MediaProvider, renditions RenditionMaker, network NetworkInfo, discovery DiscoveryProvider) error {
	mu.Lock()
	defer mu.Unlock()
	if server != nil {
		return nil
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
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		n.Close()
		return err
	}
	t := rand.Text()
	core = n
	shows = show.New(n, show.DefaultBudget)
	listener = ln
	server = &http.Server{Handler: httpapi.WithNode(t, n, shows)}
	port = ln.Addr().(*net.TCPAddr).Port
	token = t
	go server.Serve(ln)
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
	shows.Close()
	err = errors.Join(err, core.Close())
	core, shows, listener, server, port, token = nil, nil, nil, nil, 0, ""
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
