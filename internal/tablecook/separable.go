// THE ATTRIBUTION IS SEPARABLE, and this is what that costs to say in bytes
// (SPEC-TABLES.md §6.3, §7).
//
// Nothing that READS a structure touches the directory: a deref is one add on a
// self-relative offset, in a locked region and a loaded one alike. `LoadMeasure`
// reports the data bytes and the attribution bytes separately so a caller may
// place them together or apart and may release the attribution once `Load`
// returns, and `Cook` writes them as two parts for the same reason — so a build
// that ships no tooling need not carry the directory at all: the header records
// its length as zero and the file is just data.
//
// Neither direction reads a declaration. They are header arithmetic, which is
// why a tool can split a cook it cannot open and rejoin one whose unit it has
// not loaded yet.
package tablecook

import (
	"encoding/binary"
	"fmt"
)

// magicOrder reads the magic BYTEWISE, before anything else, and answers the
// byte order every other header field is written in.
func magicOrder(file []byte) (order, error) {
	if int64(len(file)) < HeaderBytes {
		return nil, fmt.Errorf("not a cook: %d bytes is shorter than the %d-byte header", len(file), HeaderBytes)
	}
	switch {
	case binary.LittleEndian.Uint64(file) == Magic:
		return binary.LittleEndian, nil
	case binary.BigEndian.Uint64(file) == Magic:
		return binary.BigEndian, nil
	}
	return nil, fmt.Errorf("not a cook: the magic is 0x%016x, and a cook's is 0x%016x in one order or the other", binary.LittleEndian.Uint64(file), Magic)
}

// SplitAttribution takes a cook carrying both parts and returns the file with
// the attribution part removed — its length word zeroed — and the attribution
// part on its own.
func SplitAttribution(file []byte) (data, attribution []byte, err error) {
	ord, err := magicOrder(file)
	if err != nil {
		return nil, nil, err
	}
	n := int64(ord.Uint64(file[offAttribLen:]))
	if n == 0 {
		return nil, nil, fmt.Errorf("this cook already carries data alone: its attribution length is zero")
	}
	if n < 0 || n > int64(len(file)) {
		return nil, nil, fmt.Errorf("the attribution length %d does not fit the file's %d bytes", n, len(file))
	}
	cut := int64(len(file)) - n
	data = append([]byte(nil), file[:cut]...)
	ord.PutUint64(data[offAttribLen:], 0)
	return data, append([]byte(nil), file[cut:]...), nil
}

// JoinAttribution is the inverse: a cook carrying data alone, plus the
// attribution part that was written beside it, back into the two-part file the
// tool reads. The rejoined bytes are byte-identical to the cook the writer
// produced, because a split moves nothing but the length word.
func JoinAttribution(file, attribution []byte) ([]byte, error) {
	ord, err := magicOrder(file)
	if err != nil {
		return nil, err
	}
	if n := int64(ord.Uint64(file[offAttribLen:])); n != 0 {
		return nil, fmt.Errorf("this cook already carries an attribution part of %d bytes: there is nothing to join", n)
	}
	out := append([]byte(nil), file...)
	ord.PutUint64(out[offAttribLen:], uint64(len(attribution)))
	return append(out, attribution...), nil
}
