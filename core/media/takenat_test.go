package media_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/glumpo/pileup/core/crdt/op"
	"github.com/glumpo/pileup/core/media"
)

func ms(rfc3339 string) int64 {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		panic(err)
	}
	return t.UnixMilli()
}

func ptr[T any](v T) *T { return &v }

func hours(h float64) *int32 { return ptr(int32(h * 3600)) }

var (
	platform = ptr(ms("2023-11-14T11:00:01Z"))
	video    = ptr(ms("2023-11-14T11:05:00Z"))
	file     = ptr(ms("2023-11-20T18:00:00Z"))
)

type takenAtCase struct {
	name   string
	device []op.RawTime
	want   []media.Time
}

var takenAtCases = []takenAtCase{
	{
		name: "1: platform_time wins over every raw field",
		device: []op.RawTime{{
			PlatformTime: platform, ExifDateTime: "2023:11:14 14:00:00", ExifOffset: "+03:00",
			VideoCreationTime: video, FileTime: file,
		}},
		want: []media.Time{{TakenAt: *platform, TakenAtSource: op.SourcePlatform, LocalOffsetSec: hours(3)}},
	},
	{
		name:   "1: platform_time without EXIF offset has no local offset",
		device: []op.RawTime{{PlatformTime: platform, ExifDateTime: "2023:11:14 14:00:00", FileTime: file}},
		want:   []media.Time{{TakenAt: *platform, TakenAtSource: op.SourcePlatform}},
	},
	{
		name:   "1: zero platform_time is unavailable",
		device: []op.RawTime{{PlatformTime: ptr(int64(0)), VideoCreationTime: video}},
		want:   []media.Time{{TakenAt: *video, TakenAtSource: op.SourceVideoUTC}},
	},
	{
		name: "2: EXIF local time with offset wins over video and file time",
		device: []op.RawTime{{
			ExifDateTime: "2023:11:14 14:00:00", ExifOffset: "+03:00", VideoCreationTime: video, FileTime: file,
		}},
		want: []media.Time{{TakenAt: ms("2023-11-14T11:00:00Z"), TakenAtSource: op.SourceExifOffset, LocalOffsetSec: hours(3)}},
	},
	{
		name:   "2: negative offset with minutes",
		device: []op.RawTime{{ExifDateTime: "2023:11:14 08:00:00", ExifOffset: "-05:30"}},
		want:   []media.Time{{TakenAt: ms("2023-11-14T13:30:00Z"), TakenAtSource: op.SourceExifOffset, LocalOffsetSec: hours(-5.5)}},
	},
	{
		name:   "2: offset moves the moment to the previous UTC day",
		device: []op.RawTime{{ExifDateTime: "2023:11:15 03:00:00", ExifOffset: "+04:00"}},
		want:   []media.Time{{TakenAt: ms("2023-11-14T23:00:00Z"), TakenAtSource: op.SourceExifOffset, LocalOffsetSec: hours(4)}},
	},
	{
		name:   "3: video creation_time is taken as UTC",
		device: []op.RawTime{{VideoCreationTime: video, FileTime: file}},
		want:   []media.Time{{TakenAt: *video, TakenAtSource: op.SourceVideoUTC}},
	},
	{
		name:   "3: malformed EXIF offset falls through to video",
		device: []op.RawTime{{ExifDateTime: "2023:11:14 14:00:00", ExifOffset: "+3:00", VideoCreationTime: video}},
		want:   []media.Time{{TakenAt: *video, TakenAtSource: op.SourceVideoUTC}},
	},
	{
		name:   "3: malformed EXIF time falls through to video, the known offset is kept",
		device: []op.RawTime{{ExifDateTime: "14:00", ExifOffset: "+03:00", VideoCreationTime: video}},
		want:   []media.Time{{TakenAt: *video, TakenAtSource: op.SourceVideoUTC, LocalOffsetSec: hours(3)}},
	},
	{
		name: "3: video with EXIF time but no offset does not borrow",
		device: []op.RawTime{
			{ExifDateTime: "2023:11:14 14:00:00", ExifOffset: "+03:00"},
			{ExifDateTime: "2023:11:14 14:10:00", VideoCreationTime: video},
		},
		want: []media.Time{
			{TakenAt: ms("2023-11-14T11:00:00Z"), TakenAtSource: op.SourceExifOffset, LocalOffsetSec: hours(3)},
			{TakenAt: *video, TakenAtSource: op.SourceVideoUTC},
		},
	},
	{
		name: "4: offset borrowed from the only neighbour with an offset",
		device: []op.RawTime{
			{ExifDateTime: "2023:11:14 14:00:00", ExifOffset: "+03:00"},
			{ExifDateTime: "2023:11:14 15:00:00", FileTime: file},
		},
		want: []media.Time{
			{TakenAt: ms("2023-11-14T11:00:00Z"), TakenAtSource: op.SourceExifOffset, LocalOffsetSec: hours(3)},
			{TakenAt: ms("2023-11-14T12:00:00Z"), TakenAtSource: op.SourceExifBorrowedOffset, LocalOffsetSec: hours(3)},
		},
	},
	{
		name: "4: nearest neighbour is earlier",
		device: []op.RawTime{
			{ExifDateTime: "2023:11:14 12:00:00"},
			{ExifDateTime: "2023:11:14 10:00:00", ExifOffset: "+03:00"},
			{ExifDateTime: "2023:11:15 10:00:00", ExifOffset: "+04:00"},
		},
		want: []media.Time{
			{TakenAt: ms("2023-11-14T09:00:00Z"), TakenAtSource: op.SourceExifBorrowedOffset, LocalOffsetSec: hours(3)},
			{TakenAt: ms("2023-11-14T07:00:00Z"), TakenAtSource: op.SourceExifOffset, LocalOffsetSec: hours(3)},
			{TakenAt: ms("2023-11-15T06:00:00Z"), TakenAtSource: op.SourceExifOffset, LocalOffsetSec: hours(4)},
		},
	},
	{
		name: "4: nearest neighbour is later",
		device: []op.RawTime{
			{ExifDateTime: "2023:11:15 08:00:00"},
			{ExifDateTime: "2023:11:14 10:00:00", ExifOffset: "+03:00"},
			{ExifDateTime: "2023:11:15 10:00:00", ExifOffset: "+04:00"},
		},
		want: []media.Time{
			{TakenAt: ms("2023-11-15T04:00:00Z"), TakenAtSource: op.SourceExifBorrowedOffset, LocalOffsetSec: hours(4)},
			{TakenAt: ms("2023-11-14T07:00:00Z"), TakenAtSource: op.SourceExifOffset, LocalOffsetSec: hours(3)},
			{TakenAt: ms("2023-11-15T06:00:00Z"), TakenAtSource: op.SourceExifOffset, LocalOffsetSec: hours(4)},
		},
	},
	{
		name: "4: neighbour with a malformed offset does not lend it",
		device: []op.RawTime{
			{ExifDateTime: "2023:11:14 14:00:00", ExifOffset: "+03:00"},
			{ExifDateTime: "2023:11:14 18:00:00", ExifOffset: "+25:00"},
			{ExifDateTime: "2023:11:14 18:01:00"},
		},
		want: []media.Time{
			{TakenAt: ms("2023-11-14T11:00:00Z"), TakenAtSource: op.SourceExifOffset, LocalOffsetSec: hours(3)},
			{TakenAt: ms("2023-11-14T15:00:00Z"), TakenAtSource: op.SourceExifBorrowedOffset, LocalOffsetSec: hours(3)},
			{TakenAt: ms("2023-11-14T15:01:00Z"), TakenAtSource: op.SourceExifBorrowedOffset, LocalOffsetSec: hours(3)},
		},
	},
	{
		name: "4: neighbour whose time came from the platform lends its EXIF offset",
		device: []op.RawTime{
			{PlatformTime: platform, ExifDateTime: "2023:11:14 14:00:01", ExifOffset: "+03:00"},
			{ExifDateTime: "2023:11:14 14:30:00"},
		},
		want: []media.Time{
			{TakenAt: *platform, TakenAtSource: op.SourcePlatform, LocalOffsetSec: hours(3)},
			{TakenAt: ms("2023-11-14T11:30:00Z"), TakenAtSource: op.SourceExifBorrowedOffset, LocalOffsetSec: hours(3)},
		},
	},
	{
		name: "5: without neighbours with an offset EXIF time is taken as UTC",
		device: []op.RawTime{
			{ExifDateTime: "2023:11:14 14:00:00", FileTime: file},
			{ExifDateTime: "2023:11:14 15:00:00", ExifOffset: ""},
		},
		want: []media.Time{
			{TakenAt: ms("2023-11-14T14:00:00Z"), TakenAtSource: op.SourceExifNaive},
			{TakenAt: ms("2023-11-14T15:00:00Z"), TakenAtSource: op.SourceExifNaive},
		},
	},
	{
		name: "5: neighbour with an offset but without EXIF time does not lend it",
		device: []op.RawTime{
			{ExifOffset: "+05:00", VideoCreationTime: video},
			{ExifDateTime: "2023:11:14 14:00:00"},
		},
		want: []media.Time{
			{TakenAt: *video, TakenAtSource: op.SourceVideoUTC, LocalOffsetSec: hours(5)},
			{TakenAt: ms("2023-11-14T14:00:00Z"), TakenAtSource: op.SourceExifNaive},
		},
	},
	{
		name:   "6: file time when nothing else is available",
		device: []op.RawTime{{FileTime: file}},
		want:   []media.Time{{TakenAt: *file, TakenAtSource: op.SourceFileTime}},
	},
	{
		name:   "6: zero EXIF time is unavailable",
		device: []op.RawTime{{ExifDateTime: "0000:00:00 00:00:00", FileTime: file}},
		want:   []media.Time{{TakenAt: *file, TakenAtSource: op.SourceFileTime}},
	},
	{
		name:   "6: blank EXIF fields are unavailable",
		device: []op.RawTime{{ExifDateTime: "    :  :     :  :  ", ExifOffset: "   :  ", FileTime: file}},
		want:   []media.Time{{TakenAt: *file, TakenAtSource: op.SourceFileTime}},
	},
	{
		name:   "6: impossible EXIF date is unavailable",
		device: []op.RawTime{{ExifDateTime: "2023:02:30 10:00:00", FileTime: file}},
		want:   []media.Time{{TakenAt: *file, TakenAtSource: op.SourceFileTime}},
	},
	{
		name:   "6: EXIF time with trailing text is unavailable",
		device: []op.RawTime{{ExifDateTime: "2023:11:14 14:00:00Z", FileTime: file}},
		want:   []media.Time{{TakenAt: *file, TakenAtSource: op.SourceFileTime}},
	},
	{
		name:   "6: zero platform and video time fall through to file time",
		device: []op.RawTime{{PlatformTime: ptr(int64(0)), VideoCreationTime: ptr(int64(-2082844800000)), FileTime: file}},
		want:   []media.Time{{TakenAt: *file, TakenAtSource: op.SourceFileTime}},
	},
	{
		name:   "6: no raw time at all gives zero file time",
		device: []op.RawTime{{}},
		want:   []media.Time{{TakenAt: 0, TakenAtSource: op.SourceFileTime}},
	},
}

func TestTakenAt(t *testing.T) {
	for _, c := range takenAtCases {
		t.Run(c.name, func(t *testing.T) {
			if got := media.TakenAt(c.device); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got  %s\nwant %s", show(got), show(c.want))
			}
		})
	}
}

func TestDeviceTimeZoneIsNotUsed(t *testing.T) {
	saved := time.Local
	t.Cleanup(func() { time.Local = saved })
	zones := []*time.Location{
		time.UTC,
		time.FixedZone("UTC+3", 3*3600),
		time.FixedZone("UTC+4", 4*3600),
		time.FixedZone("UTC+14", 14*3600),
		time.FixedZone("UTC-12", -12*3600),
		time.FixedZone("UTC+5:45", 5*3600+45*60),
	}
	for _, zone := range zones {
		time.Local = zone
		for _, c := range takenAtCases {
			if got := media.TakenAt(c.device); !reflect.DeepEqual(got, c.want) {
				t.Errorf("zone %s, %s:\ngot  %s\nwant %s", zone, c.name, show(got), show(c.want))
			}
		}
	}
}

func TestBorrowedOffsetDoesNotDependOnInputOrder(t *testing.T) {
	device := []op.RawTime{
		{ExifDateTime: "2023:11:14 10:00:00", ExifOffset: "+03:00"},
		{ExifDateTime: "2023:11:14 10:00:00", ExifOffset: "+05:00"},
		{ExifDateTime: "2023:11:14 14:00:00", ExifOffset: "+04:00"},
		{ExifDateTime: "2023:11:14 12:00:00"},
	}
	want := media.TakenAt(device)
	for _, order := range permutations(len(device)) {
		shuffled := make([]op.RawTime, len(device))
		for i, j := range order {
			shuffled[i] = device[j]
		}
		got := media.TakenAt(shuffled)
		for i, j := range order {
			if !reflect.DeepEqual(got[i], want[j]) {
				t.Fatalf("order %v: asset %d got %s, want %s", order, j, show(got[i:i+1]), show(want[j:j+1]))
			}
		}
	}
}

func FuzzTakenAt(f *testing.F) {
	f.Add("2023:11:14 14:00:00", "+03:00", "2023:11:14 15:00:00", "", int64(0), int64(0), int64(0))
	f.Add("0000:00:00 00:00:00", "   :  ", "9999:12:31 23:59:59", "-14:00", int64(-1), int64(1), int64(1))
	f.Fuzz(func(t *testing.T, exif1, offset1, exif2, offset2 string, platform, video, file int64) {
		device := []op.RawTime{
			{ExifDateTime: exif1, ExifOffset: offset1, PlatformTime: &platform, FileTime: &file},
			{ExifDateTime: exif2, ExifOffset: offset2, VideoCreationTime: &video},
		}
		got := media.TakenAt(device)
		if len(got) != len(device) {
			t.Fatalf("got %d results for %d assets", len(got), len(device))
		}
		for _, r := range got {
			if r.TakenAtSource < op.SourcePlatform || r.TakenAtSource > op.SourceFileTime {
				t.Fatalf("unknown source %d", r.TakenAtSource)
			}
		}
	})
}

func show(ts []media.Time) string {
	var b strings.Builder
	for _, r := range ts {
		offset := "none"
		if r.LocalOffsetSec != nil {
			offset = (time.Duration(*r.LocalOffsetSec) * time.Second).String()
		}
		fmt.Fprintf(&b, "{%s source=%d offset=%s} ", time.UnixMilli(r.TakenAt).UTC().Format(time.RFC3339), r.TakenAtSource, offset)
	}
	return b.String()
}

func permutations(n int) [][]int {
	if n == 0 {
		return [][]int{{}}
	}
	var out [][]int
	for _, p := range permutations(n - 1) {
		for i := 0; i <= len(p); i++ {
			q := append(append(append([]int{}, p[:i]...), n-1), p[i:]...)
			out = append(out, q)
		}
	}
	return out
}
