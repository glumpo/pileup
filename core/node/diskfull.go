package node

import (
	"errors"
	"syscall"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func DiskFull(err error) bool {
	var e *sqlite.Error
	return errors.Is(err, syscall.ENOSPC) || errors.As(err, &e) && e.Code()&0xff == sqlite3.SQLITE_FULL
}
