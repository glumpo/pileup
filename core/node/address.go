package node

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/diag"
)

var (
	errOwnAddress = errors.New("node: this device answers at the address")
	errNoPeers    = errors.New("node: piles.json: pile without peers")
	errPeerKey    = errors.New("node: piles.json: peer key is not an ed25519 public key")
)

type listedAddr struct {
	Key  string `json:"key"`
	Addr string `json:"addr"`
}

func peersOf(e entry) (map[string]op.Author, error) {
	if e.Peers == nil {
		return nil, fmt.Errorf("%w %x", errNoPeers, e.ID)
	}
	peers := map[string]op.Author{}
	for a, k := range e.Peers {
		if len(k) != len(op.Author{}) {
			return nil, fmt.Errorf("%w: %s", errPeerKey, a)
		}
		peers[a] = op.Author(k)
	}
	return peers, nil
}

func (p *pile) listing(asker op.Author) []listedAddr {
	out := []listedAddr{}
	for a, k := range p.peers {
		if k != asker {
			out = append(out, listedAddr{Key: hex.EncodeToString(k[:]), Addr: a})
		}
	}
	slices.SortFunc(out, func(x, y listedAddr) int { return strings.Compare(x.Addr, y.Addr) })
	return out
}

func (p *pile) live(a string, who op.Author) bool {
	l := p.loops[a]
	return l != nil && l.live && p.peers[a] == who
}

func (p *pile) liveAt(who op.Author, except string) (string, bool) {
	if a := p.addrs[who]; a != except && p.live(a, who) {
		return a, true
	}
	var at []string
	for a := range p.loops {
		if a != except && p.live(a, who) {
			at = append(at, a)
		}
	}
	if len(at) == 0 {
		return "", false
	}
	return slices.Min(at), true
}

func (p *pile) stale(a string) (op.Author, string, bool) {
	who, ok := p.peers[a]
	if !ok {
		return op.Author{}, "", false
	}
	if l := p.loops[a]; l != nil && (l.live || !l.failing) {
		return op.Author{}, "", false
	}
	at, live := p.liveAt(who, a)
	return who, at, live
}

func (p *pile) drop(a string) {
	if l := p.loops[a]; l != nil {
		l.cancel()
		delete(p.loops, a)
	}
	delete(p.peers, a)
	delete(p.faults, a)
}

func (p *pile) supersede(who op.Author) []string {
	var out []string
	for a, k := range p.peers {
		if k != who {
			continue
		}
		if _, _, ok := p.stale(a); ok {
			p.drop(a)
			out = append(out, a)
		}
	}
	slices.Sort(out)
	return out
}

func (n *Node) failed(p *pile, l *loop, addr string) bool {
	n.mu.Lock()
	l.failing = true
	who, at, ok := p.stale(addr)
	moved := ok && p.addrs[who] == addr
	var err error
	if ok {
		p.drop(addr)
		if moved {
			p.addrs[who] = at
		}
		err = n.save()
	}
	n.mu.Unlock()
	if !ok {
		return false
	}
	n.report(diag.Discovery, p.ID, addr, "save addresses", err)
	n.diag.Add(diag.Discovery, p.ID, addr, fmt.Sprintf("dropped address of %s: it answers at %s", n.member(p, who), at))
	if moved {
		p.owners.Transport.(*http.Transport).CloseIdleConnections()
		n.fetching(p, who, at, addr)
	}
	return true
}

func (n *Node) dropOwn(p *pile, addr string) {
	n.mu.Lock()
	p.drop(addr)
	err := n.save()
	n.mu.Unlock()
	n.report(diag.Discovery, p.ID, addr, "save addresses", err)
	n.diag.Add(diag.Discovery, p.ID, addr, "dropped address: this device answers here")
}

func (n *Node) listed(p *pile, from string, others []listedAddr) {
	self := n.Author()
	for _, e := range others {
		k, kerr := hex.DecodeString(e.Key)
		a, aerr := netip.ParseAddrPort(e.Addr)
		if kerr != nil || aerr != nil || len(k) != len(op.Author{}) {
			continue
		}
		who, addr := op.Author(k), a.String()
		if who == self {
			n.diag.Add(diag.Discovery, p.ID, addr, "ignored own address listed by "+from)
			continue
		}
		n.mu.Lock()
		owner := who
		if c, ok := p.peers[addr]; ok {
			owner = c
		}
		at, live := "", false
		if !p.live(addr, owner) {
			at, live = p.liveAt(owner, addr)
		}
		n.mu.Unlock()
		if live {
			n.diag.Add(diag.Discovery, p.ID, addr, fmt.Sprintf("ignored address of %s listed by %s: it answers at %s", n.member(p, owner), from, at))
			continue
		}
		n.report(diag.Discovery, p.ID, addr, "add peer", n.addPeer(p, addr, who, "listed by "+from))
	}
}

func (n *Node) member(p *pile, who op.Author) string {
	short := hex.EncodeToString(who[:4])
	if m, ok, err := p.store.Member(who); err == nil && ok && m.Name != "" {
		return m.Name + " (" + short + ")"
	}
	return short
}

func (n *Node) fetching(p *pile, who op.Author, addr, instead string) {
	msg := "files of " + n.member(p, who) + " are fetched from this address"
	if instead != "" {
		msg += " instead of " + instead
	}
	n.diag.Add(diag.Discovery, p.ID, addr, msg)
}
