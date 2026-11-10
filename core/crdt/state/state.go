package state

import (
	"bytes"
	"crypto/ed25519"

	"github.com/glumpo/pileup/core/crdt/clock"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/vv"
)

type State struct {
	pileID    []byte
	validator op.Validator
	clock     clock.Clock
	logs      map[op.Author][]op.Op
	name      register
	members   map[op.Author]Member
	claims    map[string]map[op.Author]*claim
}

type register struct {
	lamport uint64
	author  op.Author
	value   string
}

type claim struct {
	firstLamport uint64
	present      bool
	meta         op.AssetMeta
}

type View struct {
	PileName string
	Members  map[op.Author]Member
	Assets   map[string]Asset
	VV       vv.VV
	Lamport  uint64
}

type Member struct {
	Name       string
	Platform   string
	AppVersion string
}

type Asset struct {
	Owner op.Author
	Meta  op.AssetMeta
}

func New(pileID, secret []byte) *State {
	return &State{
		pileID:    pileID,
		validator: op.NewValidator(pileID, secret),
		logs:      map[op.Author][]op.Op{},
		members:   map[op.Author]Member{},
		claims:    map[string]map[op.Author]*claim{},
	}
}

func (s *State) head(a op.Author) op.Head {
	log := s.logs[a]
	if len(log) == 0 {
		return op.Head{}
	}
	return log[len(log)-1].Head()
}

func (s *State) Create(key ed25519.PrivateKey, wallTime int64, c op.Change) (op.Op, error) {
	a := op.Author(key.Public().(ed25519.PublicKey))
	return s.Apply(op.New(key, s.pileID, s.head(a), s.clock.Next(), wallTime, c))
}

func (s *State) Apply(raw []byte) (op.Op, error) {
	o, err := op.Decode(raw)
	if err != nil {
		return op.Op{}, err
	}
	if err := s.validator.Check(o, s.head(o.Author)); err != nil {
		return op.Op{}, err
	}
	s.logs[o.Author] = append(s.logs[o.Author], o)
	s.clock.Observe(o.Lamport)
	s.reduce(o)
	return o, nil
}

func (s *State) reduce(o op.Op) {
	switch c := o.Change.(type) {
	case op.PileCreate:
		if o.Lamport > s.name.lamport || o.Lamport == s.name.lamport && bytes.Compare(o.Author[:], s.name.author[:]) > 0 {
			s.name = register{lamport: o.Lamport, author: o.Author, value: c.Name}
		}
	case op.Join:
		s.members[o.Author] = Member{Name: c.Name, Platform: c.Platform, AppVersion: c.AppVersion}
	case op.MemberSet:
		m := s.members[o.Author]
		m.Name = c.Name
		s.members[o.Author] = m
	case op.AssetAdd:
		for _, meta := range c.Assets {
			id := string(meta.AssetID)
			if s.claims[id] == nil {
				s.claims[id] = map[op.Author]*claim{}
			}
			cl := s.claims[id][o.Author]
			if cl == nil {
				cl = &claim{firstLamport: o.Lamport}
				s.claims[id][o.Author] = cl
			}
			cl.present = true
			cl.meta = meta
		}
	case op.AssetRemove:
		for _, id := range c.AssetIDs {
			if cl := s.claims[string(id)][o.Author]; cl != nil {
				cl.present = false
			}
		}
	}
}

func (s *State) Ops(a op.Author, from uint64) [][]byte {
	var raws [][]byte
	for _, o := range s.logs[a][min(max(from, 1)-1, uint64(len(s.logs[a]))):] {
		raws = append(raws, o.Raw)
	}
	return raws
}

func (s *State) View() View {
	v := View{
		PileName: s.name.value,
		Members:  map[op.Author]Member{},
		Assets:   map[string]Asset{},
		VV:       vv.VV{},
		Lamport:  s.clock.Now(),
	}
	for a, m := range s.members {
		v.Members[a] = m
	}
	for a, log := range s.logs {
		v.VV[a] = uint64(len(log))
	}
	for id, claims := range s.claims {
		var owner op.Author
		var first *claim
		for a, cl := range claims {
			if first == nil || cl.firstLamport < first.firstLamport ||
				cl.firstLamport == first.firstLamport && bytes.Compare(a[:], owner[:]) < 0 {
				owner, first = a, cl
			}
		}
		if first.present {
			v.Assets[id] = Asset{Owner: owner, Meta: first.meta}
		}
	}
	return v
}
