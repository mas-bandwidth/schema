package tablecook_test

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
)

// TABLETEXT-7b: SplitAttribution cuts where the header says the attribution
// begins, and a recorded attribution length that would cut inside the header
// is refused.
func TestSplitAttributionRefusesALengthInsideTheHeader(t *testing.T) {
	t.Parallel()
	fx := newCookFixture(t)
	ord := binary.LittleEndian

	headerOnly := make([]byte, tablecook.HeaderBytes)
	ord.PutUint64(headerOnly[0:], tablecook.Magic)
	ord.PutUint64(headerOnly[32:], uint64(tablecook.HeaderBytes))

	inHeader := append([]byte(nil), fx.valid...)
	ord.PutUint64(inHeader[32:], uint64(len(inHeader))-32)

	wholeFile := append([]byte(nil), fx.valid...)
	ord.PutUint64(wholeFile[32:], uint64(len(wholeFile)))

	offByOne := append([]byte(nil), fx.valid...)
	ord.PutUint64(offByOne[32:], uint64(fx.h.AttribLength)+8)

	for _, tc := range []struct {
		name string
		file []byte
	}{
		{"a 64-byte file whose attribution length is the whole file", headerOnly},
		{"an attribution length that cuts at byte 32", inHeader},
		{"an attribution length that is the whole cook", wholeFile},
		{"an attribution length that cuts inside the data part", offByOne},
	} {
		refusedWithoutPanic(t, "SplitAttribution: "+tc.name, func() error {
			_, _, err := tablecook.SplitAttribution(tc.file)
			return err
		})
		refusedWithoutPanic(t, "compiler.CookSplitAttribution: "+tc.name, func() error {
			_, _, err := compiler.CookSplitAttribution(tc.file)
			return err
		})
	}

	// the lawful twin: the valid cook still splits at its own attribution offset
	data, side, err := tablecook.SplitAttribution(fx.valid)
	if err != nil {
		t.Fatalf("the valid cook does not split: %v", err)
	}
	if int64(len(data)) != fx.h.AttribOffset() || int64(len(side)) != fx.h.AttribLength {
		t.Fatalf("the split cut at %d (%d attribution bytes), and the header says %d (%d)", len(data), len(side), fx.h.AttribOffset(), fx.h.AttribLength)
	}
}
