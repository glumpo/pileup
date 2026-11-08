package op

const (
	TypePileCreate Type = iota + 1
	TypeJoin
	TypeMemberSet
	TypeAssetAdd
	TypeAssetRemove
)

type Change interface {
	Type() Type
}

type PileCreate struct {
	Name string `cbor:"1,keyasint"`
}

type Join struct {
	Name     string `cbor:"1,keyasint"`
	Platform string `cbor:"2,keyasint"`
	Proof    []byte `cbor:"3,keyasint"`
}

type MemberSet struct {
	Name string `cbor:"1,keyasint"`
}

type AssetAdd struct {
	Assets []AssetMeta `cbor:"1,keyasint"`
}

type AssetRemove struct {
	AssetIDs [][]byte `cbor:"1,keyasint"`
}

func (PileCreate) Type() Type  { return TypePileCreate }
func (Join) Type() Type        { return TypeJoin }
func (MemberSet) Type() Type   { return TypeMemberSet }
func (AssetAdd) Type() Type    { return TypeAssetAdd }
func (AssetRemove) Type() Type { return TypeAssetRemove }

type Kind uint64

const (
	KindPhoto Kind = iota + 1
	KindVideo
	KindLivePhoto
)

type TimeSource uint64

const (
	SourcePlatform TimeSource = iota + 1
	SourceExifOffset
	SourceVideoUTC
	SourceExifBorrowedOffset
	SourceExifNaive
	SourceFileTime
)

type AssetMeta struct {
	AssetID        []byte     `cbor:"1,keyasint"`
	Revision       []byte     `cbor:"2,keyasint"`
	Kind           Kind       `cbor:"3,keyasint"`
	TakenAt        int64      `cbor:"4,keyasint"`
	TakenAtSource  TimeSource `cbor:"5,keyasint"`
	LocalOffsetSec *int32     `cbor:"6,keyasint,omitempty"`
	RawTime        RawTime    `cbor:"7,keyasint"`
	Width          uint32     `cbor:"8,keyasint"`
	Height         uint32     `cbor:"9,keyasint"`
	DurationMs     *uint64    `cbor:"10,keyasint,omitempty"`
	SizeBytes      uint64     `cbor:"11,keyasint"`
	MIME           string     `cbor:"12,keyasint"`
	Codec          string     `cbor:"13,keyasint,omitempty"`
	OriginalName   string     `cbor:"14,keyasint"`
	Thumbhash      []byte     `cbor:"15,keyasint"`
}

type RawTime struct {
	PlatformTime      *int64 `cbor:"1,keyasint,omitempty"`
	ExifDateTime      string `cbor:"2,keyasint,omitempty"`
	ExifOffset        string `cbor:"3,keyasint,omitempty"`
	VideoCreationTime *int64 `cbor:"4,keyasint,omitempty"`
	FileTime          *int64 `cbor:"5,keyasint,omitempty"`
}

func decodeChange(b Body) (Change, error) {
	if b.Schema != Schema {
		return nil, nil
	}
	switch b.Type {
	case TypePileCreate:
		return decodeAs[PileCreate](b.Payload)
	case TypeJoin:
		return decodeAs[Join](b.Payload)
	case TypeMemberSet:
		return decodeAs[MemberSet](b.Payload)
	case TypeAssetAdd:
		return decodeAs[AssetAdd](b.Payload)
	case TypeAssetRemove:
		return decodeAs[AssetRemove](b.Payload)
	}
	return nil, nil
}

func decodeAs[T Change](payload []byte) (Change, error) {
	var c T
	if err := decMode.Unmarshal(payload, &c); err != nil {
		return nil, err
	}
	return c, nil
}
