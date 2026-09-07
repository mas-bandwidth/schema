package tablecook_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// pinnedWire is one of the table wire goldens the C++ reference writes.
func pinnedWire(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "wire", "tables", name+".bin"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A FLOAT RIDES AS ITS BIT PATTERN THROUGH THE COOK TOO (docs/SPEC-TABLES.md
// §3, §7.2, SPEC.md §4.3, schema#480).
//
// The cook is the wire's own bytes laid out for a mapped read, so what §3 says
// about a float32 field it says about the float32 SLOT: the four bytes are the
// pattern, and nothing on either side of the region converts. The tool holds a
// float32 in a float64 cell — the same shape the deliverable is about — so the
// narrowing at the writer and the widening at the reader are bit surgery, not
// a conversion, exactly as internal/tablewire's are.
//
// The instrument is the pin the C++ reference wrote,
// testdata/wire/tables/floats_nan.bin: it is cooked, checked, uncooked, and the
// wire has to come back byte for byte in BOTH byte orders.
func TestACookCarriesAFloatBitPattern(t *testing.T) {
	u := unit(t, "../../test/tables/F1.schema")
	m := tabletext.NewModel(u)
	root := m.Lookup("Floats")
	if root == nil {
		t.Fatal("F1.schema declares no Floats")
	}
	wire := pinnedWire(t, "floats_nan")

	for _, big := range []bool{false, true} {
		inst := decode(t, m, "Floats", wire)
		cooked, err := tablecook.Cook(m, inst, tablecook.Options{Big: big})
		if err != nil {
			t.Fatalf("big=%v: %v", big, err)
		}
		if _, err := tablecook.Check(m, cooked); err != nil {
			t.Fatalf("big=%v: the tool refused its own cook: %v", big, err)
		}

		// THE SLOT ITSELF, read out of the region: the pattern is in the four
		// bytes, in the cook's own byte order, and a re-encode that agreed
		// while the slot did not would be two errors cancelling.
		h, err := tablecook.ReadHeader(cooked, ir.BuildVersion(u))
		if err != nil {
			t.Fatal(err)
		}
		ord := binary.ByteOrder(binary.LittleEndian)
		if big {
			ord = binary.BigEndian
		}
		data := h.Data(cooked)
		ml := ir.RecordLayout(u, root)
		for _, want := range []struct {
			field string
			bits  uint32
		}{
			{"signalling", 0x7F800001},
			{"payload", 0x7FC0DEAD},
			{"negative", 0xFFA5A5A5},
		} {
			at := int64(-1)
			for i := range ml.Fields {
				if ml.Fields[i].Field.Name == want.field {
					at = ml.Fields[i].Offset
				}
			}
			if at < 0 {
				t.Fatalf("Floats declares no field %q", want.field)
			}
			if got := ord.Uint32(data[at:]); got != want.bits {
				t.Errorf("big=%v: the %s slot holds 0x%08X and the pattern is 0x%08X: the cook converted it",
					big, want.field, got, want.bits)
			}
		}

		back, err := tablecook.Uncook(m, root, cooked)
		if err != nil {
			t.Fatalf("big=%v: %v", big, err)
		}
		again, err := tablewire.Encode(m, back)
		if err != nil {
			t.Fatalf("big=%v: %v", big, err)
		}
		if !bytes.Equal(wire, again) {
			t.Fatalf("big=%v: the wire did not survive the cook: %d bytes back, %d pinned",
				big, len(again), len(wire))
		}
	}
}

// TestNarrowingAFloatIsBitSurgeryAndNotAConversion is the property under the
// row above, stated on its own so a reader can see what the cook depends on: a
// float64 cell holding a widened float32 NaN narrows back to the pattern it
// came from, where the hardware conversion would quiet it.
func TestNarrowingAFloatIsBitSurgeryAndNotAConversion(t *testing.T) {
	for _, bits := range []uint32{0x7F800001, 0x7FC0DEAD, 0xFFA5A5A5, 0x7F812345, 0xFF800042} {
		wide := tablewire.WidenF32(bits)
		if got := tablewire.NarrowF32(wide); got != bits {
			t.Errorf("0x%08X widened and narrowed back to 0x%08X", bits, got)
		}
		// what a hardware conversion does to the same value, so the row says
		// what it is refusing rather than only what it wants. A SIGNALLING one
		// is what discriminates: the conversion sets the quiet bit, and a
		// pattern that survives it proves nothing.
		if bits&0x00400000 != 0 {
			continue
		}
		if hardware := math.Float32bits(float32(wide)); hardware == bits {
			t.Errorf("the signalling 0x%08X survives the hardware conversion, so it discriminates nothing", bits)
		}
	}
	// and an ordinary value goes through untouched, by either route
	if got := tablewire.NarrowF32(1.5); got != math.Float32bits(1.5) {
		t.Errorf("1.5 narrowed to 0x%08X", got)
	}
}
