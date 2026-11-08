package cast

import (
	"encoding/binary"
	"errors"
	"io"
)

const maxFrame = 64 << 10

var errFrame = errors.New("cast: malformed message")

type frame struct {
	source, destination, namespace string
	payload                        []byte
}

func writeFrame(w io.Writer, f frame) error {
	b := make([]byte, 4, 64+len(f.namespace)+len(f.payload))
	b = append(b, 0x08, 0)
	b = appendBytes(b, 2, []byte(f.source))
	b = appendBytes(b, 3, []byte(f.destination))
	b = appendBytes(b, 4, []byte(f.namespace))
	b = append(b, 0x28, 0)
	b = appendBytes(b, 6, f.payload)
	binary.BigEndian.PutUint32(b, uint32(len(b)-4))
	_, err := w.Write(b)
	return err
}

func appendBytes(b []byte, field uint64, v []byte) []byte {
	b = binary.AppendUvarint(b, field<<3|2)
	b = binary.AppendUvarint(b, uint64(len(v)))
	return append(b, v...)
}

func readFrame(r io.Reader) (frame, error) {
	var f frame
	var n [4]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return f, err
	}
	size := binary.BigEndian.Uint32(n[:])
	if size > maxFrame {
		return f, errFrame
	}
	b := make([]byte, size)
	if _, err := io.ReadFull(r, b); err != nil {
		return f, err
	}
	for len(b) > 0 {
		key, k := binary.Uvarint(b)
		if k <= 0 {
			return f, errFrame
		}
		b = b[k:]
		switch key & 7 {
		case 0:
			if _, k = binary.Uvarint(b); k <= 0 {
				return f, errFrame
			}
			b = b[k:]
		case 2:
			l, k := binary.Uvarint(b)
			if k <= 0 || l > uint64(len(b)-k) {
				return f, errFrame
			}
			v := b[k : k+int(l)]
			b = b[k+int(l):]
			switch key >> 3 {
			case 2:
				f.source = string(v)
			case 3:
				f.destination = string(v)
			case 4:
				f.namespace = string(v)
			case 6:
				f.payload = v
			}
		default:
			return f, errFrame
		}
	}
	return f, nil
}
