package blob

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

const display = 2

const schema = `
CREATE TABLE IF NOT EXISTS objects (
	owner    BLOB NOT NULL,
	asset    BLOB NOT NULL,
	revision BLOB NOT NULL,
	tier     INTEGER NOT NULL,
	hash     BLOB NOT NULL,
	size     INTEGER NOT NULL,
	PRIMARY KEY (owner, asset, revision, tier)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS objects_hash ON objects (hash);
CREATE TABLE IF NOT EXISTS holds (
	pile     BLOB NOT NULL,
	owner    BLOB NOT NULL,
	asset    BLOB NOT NULL,
	revision BLOB NOT NULL,
	tier     INTEGER NOT NULL,
	taken_at INTEGER NOT NULL,
	PRIMARY KEY (pile, owner, asset, revision, tier)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS holds_key ON holds (owner, asset, revision, tier);
CREATE TABLE IF NOT EXISTS pins (
	pile  BLOB NOT NULL,
	asset BLOB NOT NULL,
	PRIMARY KEY (pile, asset)
) WITHOUT ROWID;
`

var ErrClosed = errors.New("blob: store is closed")

type Hash [sha256.Size]byte

func (h Hash) String() string { return hex.EncodeToString(h[:]) }

type Key struct {
	Owner    string
	Asset    string
	Revision string
	Tier     int
}

type Load func(ctx context.Context, w io.Writer) error

type Request struct {
	Pile    string
	Key     Key
	TakenAt int64
	Valid   func() error
	Load    Load
}

type Held struct {
	Key  Key
	Size int64
}

type Store struct {
	dir    string
	db     *sql.DB
	budget int64
	wg     sync.WaitGroup

	mu        sync.Mutex
	closed    bool
	loads     map[Key]*attempt
	protected map[string]map[Key]bool
}

type attempt struct {
	cancel    context.CancelFunc
	done      chan struct{}
	waiters   map[*waiter]bool
	cancelled bool
}

type waiter struct {
	pile    string
	takenAt int64
	valid   func() error
	f       *os.File
	h       Hash
	err     error
}

func Open(dir string, budget int64) (*Store, error) {
	tmp := filepath.Join(dir, "tmp")
	if err := errors.Join(os.RemoveAll(tmp), os.RemoveAll(filepath.Join(dir, "refs"))); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "cache.db")+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{dir: dir, db: db, budget: budget, loads: map[Key]*attempt{}, protected: map[string]map[Key]bool{}}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.reconcile(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) reconcile() error {
	var missing [][]byte
	used := map[Hash]bool{}
	err := s.query(`SELECT DISTINCT hash FROM objects`, func(rows *sql.Rows) error {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return err
		}
		used[Hash(b)] = true
		_, err := os.Stat(s.path(Hash(b)))
		if errors.Is(err, fs.ErrNotExist) {
			missing, err = append(missing, b), nil
		}
		return err
	})
	if err != nil {
		return err
	}
	err = s.tx(func(tx *sql.Tx) error {
		for _, h := range missing {
			if _, err := tx.Exec(`DELETE FROM holds WHERE (owner, asset, revision, tier) IN (SELECT owner, asset, revision, tier FROM objects WHERE hash = ?)`, h); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM objects WHERE hash = ?`, h); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		b, err := hex.DecodeString(e.Name())
		if err != nil || len(b) != sha256.Size || used[Hash(b)] {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	for _, a := range s.loads {
		a.cancelled = true
		a.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
	return s.db.Close()
}

func (s *Store) path(h Hash) string { return filepath.Join(s.dir, h.String()) }

func (s *Store) Get(ctx context.Context, r Request) (*os.File, Hash, error) {
	for {
		s.mu.Lock()
		f, h, a, w, err := s.request(r)
		s.mu.Unlock()
		if a == nil {
			return f, h, err
		}
		select {
		case <-a.done:
		case <-ctx.Done():
			s.leave(a, w)
			return nil, Hash{}, ctx.Err()
		}
		if w != nil {
			return w.f, w.h, w.err
		}
	}
}

func (s *Store) request(r Request) (*os.File, Hash, *attempt, *waiter, error) {
	if s.closed {
		return nil, Hash{}, nil, nil, ErrClosed
	}
	if err := r.Valid(); err != nil {
		return nil, Hash{}, nil, nil, err
	}
	a := s.loads[r.Key]
	if a != nil && a.cancelled {
		return nil, Hash{}, a, nil, nil
	}
	if a == nil {
		var b []byte
		err := s.db.QueryRow(`SELECT hash FROM objects WHERE owner = ? AND asset = ? AND revision = ? AND tier = ?`, args(r.Key)...).Scan(&b)
		if err == nil {
			f, err := s.hold(r, Hash(b))
			return f, Hash(b), nil, nil, err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, Hash{}, nil, nil, err
		}
		ctx, cancel := context.WithCancel(context.Background())
		a = &attempt{cancel: cancel, done: make(chan struct{}), waiters: map[*waiter]bool{}}
		s.loads[r.Key] = a
		s.wg.Add(1)
		go s.run(ctx, r.Key, a, r.Load)
	}
	w := &waiter{pile: r.Pile, takenAt: r.TakenAt, valid: r.Valid}
	a.waiters[w] = true
	return nil, Hash{}, a, w, nil
}

func (s *Store) hold(r Request, h Hash) (*os.File, error) {
	f, err := os.Open(s.path(h))
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(holdSQL, holdArgs(r.Pile, r.Key, r.TakenAt)...); err != nil {
		f.Close()
		return nil, err
	}
	if r.Key.Tier == display {
		s.evict(r.Pile)
	}
	return f, nil
}

func (s *Store) leave(a *attempt, w *waiter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if w == nil {
		return
	}
	select {
	case <-a.done:
		if w.f != nil {
			w.f.Close()
		}
		return
	default:
	}
	delete(a.waiters, w)
	if len(a.waiters) == 0 {
		a.cancelled = true
		a.cancel()
	}
}

func (s *Store) run(ctx context.Context, k Key, a *attempt, load Load) {
	defer s.wg.Done()
	tmp, h, size, err := s.write(ctx, load)
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case a.cancelled:
		err = ErrClosed
	case err == nil:
		err = s.publish(k, a, tmp, h, size)
	}
	if tmp != "" {
		os.Remove(tmp)
	}
	for w := range a.waiters {
		if w.f == nil && w.err == nil {
			w.err = err
		}
	}
	delete(s.loads, k)
	a.cancel()
	close(a.done)
}

func (s *Store) write(ctx context.Context, load Load) (string, Hash, int64, error) {
	f, err := os.CreateTemp(filepath.Join(s.dir, "tmp"), "")
	if err != nil {
		return "", Hash{}, 0, err
	}
	sum := sha256.New()
	err = load(ctx, io.MultiWriter(f, sum))
	var size int64
	if err == nil {
		size, err = f.Seek(0, io.SeekCurrent)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", Hash{}, 0, err
	}
	return f.Name(), Hash(sum.Sum(nil)), size, nil
}

func (s *Store) publish(k Key, a *attempt, tmp string, h Hash, size int64) error {
	var valid []*waiter
	for w := range a.waiters {
		if w.err = w.valid(); w.err == nil {
			valid = append(valid, w)
		}
	}
	if len(valid) == 0 {
		return nil
	}
	if err := os.Rename(tmp, s.path(h)); err != nil {
		return err
	}
	err := s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO objects (owner, asset, revision, tier, hash, size) VALUES (?, ?, ?, ?, ?, ?)`,
			append(args(k), h[:], size)...); err != nil {
			return err
		}
		for _, w := range valid {
			if _, err := tx.Exec(holdSQL, holdArgs(w.pile, k, w.takenAt)...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return errors.Join(err, s.unlink([]Hash{h}))
	}
	for _, w := range valid {
		w.f, w.err = os.Open(s.path(h))
		w.h = h
	}
	if k.Tier == display {
		for _, w := range valid {
			s.evict(w.pile)
		}
	}
	return nil
}

func (s *Store) Held(pile string, tier int) ([]Held, error) {
	var out []Held
	err := s.query(`SELECT h.owner, h.asset, h.revision, o.size FROM holds h JOIN objects o USING (owner, asset, revision, tier)
		WHERE h.pile = ? AND h.tier = ?`, func(rows *sql.Rows) error {
		x := Held{Key: Key{Tier: tier}}
		err := rows.Scan(&x.Key.Owner, &x.Key.Asset, &x.Key.Revision, &x.Size)
		out = append(out, x)
		return err
	}, []byte(pile), tier)
	return out, err
}

func (s *Store) Usage(pile string) (int64, error) {
	all, err := s.displays(pile)
	var total int64
	seen := map[Hash]bool{}
	for _, c := range all {
		if !seen[c.hash] {
			seen[c.hash] = true
			total += c.size
		}
	}
	return total, err
}

func (s *Store) Retain(pile string, keep func(Key) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var drop []Key
	err := s.query(`SELECT owner, asset, revision, tier FROM holds WHERE pile = ?`, func(rows *sql.Rows) error {
		var k Key
		err := rows.Scan(&k.Owner, &k.Asset, &k.Revision, &k.Tier)
		if err == nil && !keep(k) {
			drop = append(drop, k)
		}
		return err
	}, []byte(pile))
	if err != nil {
		return err
	}
	return s.release(pile, drop)
}

func (s *Store) Protect(pile string, keys []Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := map[Key]bool{}
	for _, k := range keys {
		set[k] = true
	}
	s.protected[pile] = set
	return s.evict(pile)
}

func (s *Store) Pins(pile string) ([]string, error) {
	var out []string
	err := s.query(`SELECT asset FROM pins WHERE pile = ? ORDER BY asset`, func(rows *sql.Rows) error {
		var a string
		err := rows.Scan(&a)
		out = append(out, a)
		return err
	}, []byte(pile))
	return out, err
}

func (s *Store) SetPins(pile string, assets []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM pins WHERE pile = ?`, []byte(pile)); err != nil {
			return err
		}
		for _, a := range assets {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO pins (pile, asset) VALUES (?, ?)`, []byte(pile), []byte(a)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.evict(pile)
}

type cached struct {
	key  Key
	hash Hash
	size int64
}

func (s *Store) displays(pile string) ([]cached, error) {
	var out []cached
	err := s.query(`SELECT h.owner, h.asset, h.revision, o.hash, o.size FROM holds h JOIN objects o USING (owner, asset, revision, tier)
		WHERE h.pile = ? AND h.tier = ? ORDER BY h.taken_at, h.owner, h.asset, h.revision`, func(rows *sql.Rows) error {
		c := cached{key: Key{Tier: display}}
		var h []byte
		err := rows.Scan(&c.key.Owner, &c.key.Asset, &c.key.Revision, &h, &c.size)
		if err == nil {
			c.hash = Hash(h)
			out = append(out, c)
		}
		return err
	}, []byte(pile), display)
	return out, err
}

func (s *Store) evict(pile string) error {
	all, err := s.displays(pile)
	if err != nil {
		return err
	}
	pinned := map[string]bool{}
	err = s.query(`SELECT asset FROM pins WHERE pile = ?`, func(rows *sql.Rows) error {
		var a string
		err := rows.Scan(&a)
		pinned[a] = true
		return err
	}, []byte(pile))
	if err != nil {
		return err
	}
	var total int64
	users := map[Hash]int{}
	for _, c := range all {
		if users[c.hash]++; users[c.hash] == 1 {
			total += c.size
		}
	}
	var victims []Key
	for _, c := range all {
		if total <= s.budget {
			break
		}
		if s.protected[pile][c.key] || pinned[c.key.Asset] {
			continue
		}
		victims = append(victims, c.key)
		if users[c.hash]--; users[c.hash] == 0 {
			total -= c.size
		}
	}
	return s.release(pile, victims)
}

func (s *Store) release(pile string, keys []Key) error {
	if len(keys) == 0 {
		return nil
	}
	var unused []Hash
	err := s.tx(func(tx *sql.Tx) error {
		for _, k := range keys {
			if _, err := tx.Exec(`DELETE FROM holds WHERE pile = ? AND owner = ? AND asset = ? AND revision = ? AND tier = ?`,
				append([]any{[]byte(pile)}, args(k)...)...); err != nil {
				return err
			}
			var h []byte
			err := tx.QueryRow(`DELETE FROM objects WHERE owner = ? AND asset = ? AND revision = ? AND tier = ?
				AND NOT EXISTS (SELECT 1 FROM holds WHERE owner = ? AND asset = ? AND revision = ? AND tier = ?) RETURNING hash`,
				append(args(k), args(k)...)...).Scan(&h)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			unused = append(unused, Hash(h))
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.unlink(unused)
}

func (s *Store) unlink(hashes []Hash) error {
	var errs []error
	for _, h := range hashes {
		var used bool
		if err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM objects WHERE hash = ?)`, h[:]).Scan(&used); err != nil || used {
			errs = append(errs, err)
			continue
		}
		if err := os.Remove(s.path(h)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Store) tx(f func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := f(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) query(q string, row func(*sql.Rows) error, params ...any) error {
	rows, err := s.db.Query(q, params...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := row(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

const holdSQL = `INSERT OR IGNORE INTO holds (pile, owner, asset, revision, tier, taken_at) VALUES (?, ?, ?, ?, ?, ?)`

func holdArgs(pile string, k Key, takenAt int64) []any {
	return append(append([]any{[]byte(pile)}, args(k)...), takenAt)
}

func args(k Key) []any {
	return []any{[]byte(k.Owner), []byte(k.Asset), []byte(k.Revision), k.Tier}
}
