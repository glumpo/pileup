package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"
	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
)

func (s *Store) Asset(id []byte) (state.Asset, bool, error) {
	var owner, meta []byte
	var present bool
	err := s.db.QueryRow(`SELECT author, present, meta FROM claims WHERE asset_id = ?
		ORDER BY first_lamport, author LIMIT 1`, notNull(id)).Scan(&owner, &present, &meta)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !present {
		return state.Asset{}, false, nil
	}
	if err != nil {
		return state.Asset{}, false, err
	}
	a := state.Asset{Owner: op.Author(owner)}
	if err := cbor.Unmarshal(meta, &a.Meta); err != nil {
		return state.Asset{}, false, fmt.Errorf("store: asset meta: %w", err)
	}
	return a, true, nil
}
