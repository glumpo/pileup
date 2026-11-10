package node

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"net/netip"
	"slices"
	"strings"
)

const (
	pileService    = "_pileup._tcp"
	castService    = "_googlecast._tcp"
	airplayService = "_airplay._tcp"

	airplayReason = "Показ на Apple TV доступен только с iPhone. Запустите показ с iPhone участника."
)

type DiscoveryProvider interface {
	Advertise(service, name string, port int, txt []byte) error
	Browse(service string, handler DiscoveryHandler) error
	Stop() error
}

type DiscoveryHandler interface {
	Found(name, host string, port int, txt []byte)
	Lost(name string)
}

type Screen struct {
	ID        string
	Name      string
	Kind      string
	Addr      netip.AddrPort
	Available bool
	Reason    string
}

type service struct {
	addr netip.AddrPort
	txt  map[string]string
}

type browser struct {
	n       *Node
	service string
}

func pileTag(id []byte) string {
	h := sha256.Sum256(id)
	return hex.EncodeToString(h[:16])
}

func (n *Node) discover() {
	if n.cfg.Discovery == nil {
		return
	}
	n.mu.Lock()
	ps := slices.Clone(n.piles)
	n.mu.Unlock()
	for _, s := range []string{pileService, castService, airplayService} {
		n.cfg.Discovery.Browse(s, browser{n: n, service: s})
	}
	for _, p := range ps {
		n.advertise(p)
	}
}

func (n *Node) advertise(p *pile) {
	if n.cfg.Discovery == nil {
		return
	}
	n.cfg.Discovery.Advertise(pileService, n.advertName(p.ID), int(p.Port), txtRecord("pile="+pileTag(p.ID)))
}

func (n *Node) advertName(id []byte) string {
	a := n.Author()
	h := sha256.Sum256(append(a[:], id...))
	return "pileup-" + hex.EncodeToString(h[:8])
}

func (n *Node) stopDiscovery() {
	if n.cfg.Discovery != nil {
		n.cfg.Discovery.Stop()
	}
}

func (n *Node) discovered(id []byte) []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []string
	for _, s := range n.found[pileService] {
		if s.txt["pile"] == pileTag(id) {
			out = append(out, s.addr.String())
		}
	}
	slices.Sort(out)
	return out
}

func (b browser) Found(name, host string, port int, txt []byte) {
	a, err := netip.ParseAddr(host)
	if err != nil || port <= 0 || port > 0xffff {
		return
	}
	s := service{addr: netip.AddrPortFrom(a.Unmap(), uint16(port)), txt: parseTXT(txt)}
	n := b.n
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.found[b.service] == nil {
		n.found[b.service] = map[string]service{}
	}
	old, seen := n.found[b.service][name]
	n.found[b.service][name] = s
	if b.service != pileService || seen && old.addr == s.addr && maps.Equal(old.txt, s.txt) {
		return
	}
	for _, p := range n.piles {
		if s.txt["pile"] == pileTag(p.ID) && name != n.advertName(p.ID) {
			n.dial(p, s.addr.String(), func() {})
		}
	}
}

func (b browser) Lost(name string) {
	n := b.n
	n.mu.Lock()
	defer n.mu.Unlock()
	s, ok := n.found[b.service][name]
	delete(n.found[b.service], name)
	if !ok || b.service != pileService {
		return
	}
	addr := s.addr.String()
	for _, p := range n.piles {
		if l := p.loops[addr]; l != nil && s.txt["pile"] == pileTag(p.ID) && !slices.Contains(p.Peers, addr) && !n.advertised(p, addr) {
			l.cancel()
			delete(p.loops, addr)
		}
	}
}

func (n *Node) advertised(p *pile, addr string) bool {
	for _, s := range n.found[pileService] {
		if s.addr.String() == addr && s.txt["pile"] == pileTag(p.ID) {
			return true
		}
	}
	return false
}

func (n *Node) Screens() []Screen {
	n.mu.Lock()
	defer n.mu.Unlock()
	ios := n.cfg.Platform == "ios"
	var out []Screen
	for name, s := range n.found[castService] {
		out = append(out, Screen{ID: "cast:" + name, Name: cmp.Or(s.txt["fn"], name), Kind: "cast", Addr: s.addr, Available: true})
	}
	for name, s := range n.found[airplayService] {
		sc := Screen{ID: "airplay:" + name, Name: name, Kind: "airplay", Addr: s.addr, Available: ios}
		if !ios {
			sc.Reason = airplayReason
		}
		out = append(out, sc)
	}
	slices.SortFunc(out, func(x, y Screen) int { return cmp.Or(cmp.Compare(x.Name, y.Name), cmp.Compare(x.ID, y.ID)) })
	return out
}

func txtRecord(entries ...string) []byte {
	var b []byte
	for _, e := range entries {
		b = append(b, byte(len(e)))
		b = append(b, e...)
	}
	return b
}

func parseTXT(b []byte) map[string]string {
	out := map[string]string{}
	for len(b) > 0 {
		l := int(b[0])
		if 1+l > len(b) {
			break
		}
		k, v, _ := strings.Cut(string(b[1:1+l]), "=")
		k = strings.ToLower(k)
		if _, ok := out[k]; !ok && k != "" {
			out[k] = v
		}
		b = b[1+l:]
	}
	return out
}
