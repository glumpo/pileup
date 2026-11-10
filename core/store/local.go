package store

import (
	"crypto/ed25519"
	"database/sql"
	"errors"

	"github.com/glumpo/pileup/core/crdt/op"
)

const localSchema = `
CREATE TABLE IF NOT EXISTS selection (
	range_from INTEGER NOT NULL,
	range_to   INTEGER NOT NULL
);
INSERT INTO selection SELECT 0, 0 WHERE NOT EXISTS (SELECT 1 FROM selection);
CREATE TABLE IF NOT EXISTS decisions (
	asset_id BLOB PRIMARY KEY,
	selected INTEGER NOT NULL
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS files (
	asset_id BLOB PRIMARY KEY,
	local_id TEXT NOT NULL
) WITHOUT ROWID;
`

type Selection struct {
	From, To int64
	Manual   map[string]bool
	Files    map[string]string
}

func (s *Store) Selection() (Selection, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Selection{}, err
	}
	defer tx.Rollback()
	sel := Selection{Manual: map[string]bool{}, Files: map[string]string{}}
	if err := tx.QueryRow(`SELECT range_from, range_to FROM selection`).Scan(&sel.From, &sel.To); err != nil {
		return Selection{}, err
	}
	err = each(tx, `SELECT asset_id, selected FROM decisions`, func(scan func(...any) error) error {
		var id []byte
		var selected bool
		err := scan(&id, &selected)
		sel.Manual[string(id)] = selected
		return err
	})
	if err != nil {
		return Selection{}, err
	}
	err = each(tx, `SELECT asset_id, local_id FROM files`, func(scan func(...any) error) error {
		var id []byte
		var local string
		err := scan(&id, &local)
		sel.Files[string(id)] = local
		return err
	})
	if err != nil {
		return Selection{}, err
	}
	return sel, nil
}

func (s *Store) LocalID(assetID []byte) (string, bool, error) {
	var local string
	err := s.db.QueryRow(`SELECT local_id FROM files WHERE asset_id = ?`, notNull(assetID)).Scan(&local)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return local, err == nil, err
}

func (s *Store) Select(key ed25519.PrivateKey, wallTime int64, sel Selection, changes ...op.Change) error {
	_, err := s.inTx(func(tx *sql.Tx) (op.Op, error) {
		if err := writeSelection(tx, sel); err != nil {
			return op.Op{}, err
		}
		for _, c := range changes {
			if _, err := s.create(tx, key, wallTime, c); err != nil {
				return op.Op{}, err
			}
		}
		return op.Op{}, nil
	})
	return err
}

func writeSelection(tx *sql.Tx, sel Selection) error {
	if _, err := tx.Exec(`UPDATE selection SET range_from = ?, range_to = ?`, sel.From, sel.To); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM decisions`); err != nil {
		return err
	}
	for id, selected := range sel.Manual {
		if _, err := tx.Exec(`INSERT INTO decisions (asset_id, selected) VALUES (?, ?)`, notNull([]byte(id)), selected); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM files`); err != nil {
		return err
	}
	for id, local := range sel.Files {
		if _, err := tx.Exec(`INSERT INTO files (asset_id, local_id) VALUES (?, ?)`, notNull([]byte(id)), local); err != nil {
			return err
		}
	}
	return nil
}
