package media

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
)

type Time struct {
	TakenAt        int64
	TakenAtSource  op.TimeSource
	LocalOffsetSec *int32
}

type donor struct {
	local  int64
	offset int32
}

func TakenAt(device []op.RawTime) []Time {
	var donors []donor
	for _, r := range device {
		local, localOK := parseExif(r.ExifDateTime)
		offset, offsetOK := parseOffset(r.ExifOffset)
		if localOK && offsetOK {
			donors = append(donors, donor{local, offset})
		}
	}
	out := make([]Time, len(device))
	for i, r := range device {
		out[i] = takenAt(r, donors)
	}
	return out
}

func takenAt(r op.RawTime, donors []donor) Time {
	local, localOK := parseExif(r.ExifDateTime)
	offset, offsetOK := parseOffset(r.ExifOffset)
	var known *int32
	if offsetOK {
		known = &offset
	}
	switch {
	case positive(r.PlatformTime):
		return Time{*r.PlatformTime, op.SourcePlatform, known}
	case localOK && offsetOK:
		return Time{shift(local, offset), op.SourceExifOffset, known}
	case positive(r.VideoCreationTime):
		return Time{*r.VideoCreationTime, op.SourceVideoUTC, known}
	case localOK && len(donors) > 0:
		d := nearest(local, donors)
		return Time{shift(local, d.offset), op.SourceExifBorrowedOffset, &d.offset}
	case localOK:
		return Time{local, op.SourceExifNaive, nil}
	case r.FileTime != nil:
		return Time{*r.FileTime, op.SourceFileTime, known}
	}
	return Time{0, op.SourceFileTime, known}
}

func nearest(local int64, donors []donor) donor {
	return slices.MinFunc(donors, func(a, b donor) int {
		return cmp.Or(
			cmp.Compare(distance(a.local, local), distance(b.local, local)),
			cmp.Compare(a.local, b.local),
			cmp.Compare(a.offset, b.offset),
		)
	})
}

func distance(a, b int64) int64 {
	if a > b {
		return a - b
	}
	return b - a
}

func shift(local int64, offset int32) int64 {
	return local - int64(offset)*1000
}

func positive(t *int64) bool {
	return t != nil && *t > 0
}

func parseExif(s string) (int64, bool) {
	t, err := time.Parse("2006:01:02 15:04:05", s)
	if err != nil {
		return 0, false
	}
	return t.UnixMilli(), true
}

var offsetPattern = regexp.MustCompile(`^([+-])(\d\d):(\d\d)$`)

func parseOffset(s string) (int32, bool) {
	m := offsetPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	h, _ := strconv.Atoi(m[2])
	min, _ := strconv.Atoi(m[3])
	if h > 14 || min > 59 {
		return 0, false
	}
	sec := int32(h*3600 + min*60)
	if m[1] == "-" {
		sec = -sec
	}
	return sec, true
}
