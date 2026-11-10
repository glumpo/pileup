package store

import (
	"database/sql"
	"errors"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/crdt/state"
)

func (s *Store) Member(a op.Author) (state.Member, bool, error) {
	var m state.Member
	err := s.db.QueryRow(`SELECT name, platform, app_version FROM members WHERE author = ?`, a[:]).Scan(&m.Name, &m.Platform, &m.AppVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return state.Member{}, false, nil
	}
	return m, err == nil, err
}
