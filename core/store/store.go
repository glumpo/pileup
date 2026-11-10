package store

import (
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
	"github.com/glumpo/pileup/core/crdt/vv"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS ops (
	author  BLOB NOT NULL,
	seq     INTEGER NOT NULL,
	id      BLOB NOT NULL,
	lamport INTEGER NOT NULL,
	raw     BLOB NOT NULL,
	PRIMARY KEY (author, seq)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS pile (
	l            INTEGER NOT NULL,
	name         TEXT NOT NULL,
	name_lamport INTEGER NOT NULL,
	name_author  BLOB NOT NULL
);
INSERT INTO pile SELECT 0, '', 0, x'' WHERE NOT EXISTS (SELECT 1 FROM pile);
CREATE TABLE IF NOT EXISTS members (
	author   BLOB PRIMARY KEY,
	name     TEXT NOT NULL,
	platform TEXT NOT NULL
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS claims (
	asset_id      BLOB NOT NULL,
	author        BLOB NOT NULL,
	first_lamport INTEGER NOT NULL,
	present       INTEGER NOT NULL,
	meta          BLOB NOT NULL,
	PRIMARY KEY (asset_id, author)
) WITHOUT ROWID;
`

type Store struct {
	db        *sql.DB
	pileID    []byte
	validator op.Validator
}

func Open(path string, pileID, secret []byte) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema + localSchema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, pileID: pileID, validator: op.NewValidator(pileID, secret)}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Apply(raw []byte) (op.Op, error) {
	return s.inTx(func(tx *sql.Tx) (op.Op, error) { return s.apply(tx, raw) })
}

func (s *Store) Create(key ed25519.PrivateKey, wallTime int64, c op.Change) (op.Op, error) {
	return s.inTx(func(tx *sql.Tx) (op.Op, error) { return s.create(tx, key, wallTime, c) })
}

func (s *Store) create(tx *sql.Tx, key ed25519.PrivateKey, wallTime int64, c op.Change) (op.Op, error) {
	head, err := headOf(tx, op.Author(key.Public().(ed25519.PublicKey)))
	if err != nil {
		return op.Op{}, err
	}
	var l uint64
	if err := tx.QueryRow(`SELECT l FROM pile`).Scan(&l); err != nil {
		return op.Op{}, err
	}
	return s.apply(tx, op.New(key, s.pileID, head, l+1, wallTime, c))
}

func (s *Store) inTx(f func(*sql.Tx) (op.Op, error)) (op.Op, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return op.Op{}, err
	}
	o, err := f(tx)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		tx.Rollback()
		return op.Op{}, err
	}
	return o, nil
}

func headOf(tx *sql.Tx, a op.Author) (op.Head, error) {
	var h op.Head
	var id []byte
	err := tx.QueryRow(`SELECT seq, id, lamport FROM ops WHERE author = ? ORDER BY seq DESC LIMIT 1`, a[:]).Scan(&h.Seq, &id, &h.Lamport)
	if errors.Is(err, sql.ErrNoRows) {
		return op.Head{}, nil
	}
	copy(h.ID[:], id)
	return h, err
}

func (s *Store) apply(tx *sql.Tx, raw []byte) (op.Op, error) {
	o, err := op.Decode(raw)
	if err != nil {
		return op.Op{}, err
	}
	head, err := headOf(tx, o.Author)
	if err != nil {
		return op.Op{}, err
	}
	if err := s.validator.Check(o, head); err != nil {
		return op.Op{}, err
	}
	if _, err := tx.Exec(`INSERT INTO ops (author, seq, id, lamport, raw) VALUES (?, ?, ?, ?, ?)`,
		o.Author[:], o.Seq, o.ID[:], o.Lamport, raw); err != nil {
		return op.Op{}, err
	}
	if _, err := tx.Exec(`UPDATE pile SET l = max(l, ?)`, o.Lamport); err != nil {
		return op.Op{}, err
	}
	return o, reduce(tx, o)
}

func reduce(tx *sql.Tx, o op.Op) error {
	a := o.Author[:]
	switch c := o.Change.(type) {
	case op.PileCreate:
		_, err := tx.Exec(`UPDATE pile SET name = ?1, name_lamport = ?2, name_author = ?3
			WHERE ?2 > name_lamport OR ?2 = name_lamport AND ?3 > name_author`, c.Name, o.Lamport, a)
		return err
	case op.Join:
		_, err := tx.Exec(`INSERT OR REPLACE INTO members (author, name, platform) VALUES (?, ?, ?)`, a, c.Name, c.Platform)
		return err
	case op.MemberSet:
		_, err := tx.Exec(`INSERT INTO members (author, name, platform) VALUES (?, ?, '')
			ON CONFLICT (author) DO UPDATE SET name = excluded.name`, a, c.Name)
		return err
	case op.AssetAdd:
		for _, m := range c.Assets {
			meta, err := cbor.Marshal(m)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO claims (asset_id, author, first_lamport, present, meta) VALUES (?, ?, ?, 1, ?)
				ON CONFLICT (asset_id, author) DO UPDATE SET present = 1, meta = excluded.meta`,
				notNull(m.AssetID), a, o.Lamport, meta); err != nil {
				return err
			}
		}
	case op.AssetRemove:
		for _, id := range c.AssetIDs {
			if _, err := tx.Exec(`UPDATE claims SET present = 0 WHERE asset_id = ? AND author = ?`, notNull(id), a); err != nil {
				return err
			}
		}
	}
	return nil
}

func notNull(b []byte) []byte {
	return append([]byte{}, b...)
}

func (s *Store) Ops(a op.Author, from uint64) ([][]byte, error) {
	rows, err := s.db.Query(`SELECT raw FROM ops WHERE author = ? AND seq >= ? ORDER BY seq`, a[:], from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var raws [][]byte
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		raws = append(raws, raw)
	}
	return raws, rows.Err()
}

func (s *Store) VV() (vv.VV, error) {
	rows, err := s.db.Query(`SELECT author, max(seq) FROM ops GROUP BY author`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	v := vv.VV{}
	for rows.Next() {
		var a []byte
		var n uint64
		if err := rows.Scan(&a, &n); err != nil {
			return nil, err
		}
		v[op.Author(a)] = n
	}
	return v, rows.Err()
}

func (s *Store) ID(a op.Author, seq uint64) (op.ID, bool, error) {
	var id op.ID
	var b []byte
	err := s.db.QueryRow(`SELECT id FROM ops WHERE author = ? AND seq = ?`, a[:], seq).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return id, false, nil
	}
	copy(id[:], b)
	return id, err == nil, err
}

func (s *Store) View() (state.View, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return state.View{}, err
	}
	defer tx.Rollback()
	v := state.View{
		Members: map[op.Author]state.Member{},
		Assets:  map[string]state.Asset{},
		VV:      vv.VV{},
	}
	if err := tx.QueryRow(`SELECT l, name FROM pile`).Scan(&v.Lamport, &v.PileName); err != nil {
		return state.View{}, err
	}
	err = each(tx, `SELECT author, name, platform FROM members`, func(scan func(...any) error) error {
		var a []byte
		var m state.Member
		if err := scan(&a, &m.Name, &m.Platform); err != nil {
			return err
		}
		v.Members[op.Author(a)] = m
		return nil
	})
	if err != nil {
		return state.View{}, err
	}
	err = each(tx, `SELECT author, max(seq) FROM ops GROUP BY author`, func(scan func(...any) error) error {
		var a []byte
		var n uint64
		if err := scan(&a, &n); err != nil {
			return err
		}
		v.VV[op.Author(a)] = n
		return nil
	})
	if err != nil {
		return state.View{}, err
	}
	err = each(tx, `SELECT asset_id, author, meta FROM (
			SELECT asset_id, author, present, meta,
				row_number() OVER (PARTITION BY asset_id ORDER BY first_lamport, author) AS rank
			FROM claims
		) WHERE rank = 1 AND present`, func(scan func(...any) error) error {
		var id, owner, meta []byte
		if err := scan(&id, &owner, &meta); err != nil {
			return err
		}
		asset := state.Asset{Owner: op.Author(owner)}
		if err := cbor.Unmarshal(meta, &asset.Meta); err != nil {
			return fmt.Errorf("store: asset meta: %w", err)
		}
		v.Assets[string(id)] = asset
		return nil
	})
	if err != nil {
		return state.View{}, err
	}
	return v, nil
}

func each(tx *sql.Tx, query string, f func(scan func(...any) error) error) error {
	rows, err := tx.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := f(rows.Scan); err != nil {
			return err
		}
	}
	return rows.Err()
}
