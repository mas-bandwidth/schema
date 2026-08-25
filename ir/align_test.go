// Tests for the static byte-alignment analysis behind the bulk-bytes
// emission path: a fixed [N]uint8 array is marked ONLY when its element
// bytes provably begin on a byte boundary — anything else must keep the
// per-byte loop, because the bulk path's internal align would put padding
// bits on the wire that the pinned encoding does not have.
package ir_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/parser"
	"github.com/mas-bandwidth/schema/ir"
)

// Each type carries one fixed byte array named data; the table below says
// whether the analysis may mark it. Positions worked by hand from the SPEC
// §4.3/§4.7 wire widths.
const alignCorpus = `package aligntest

type MarkedAfterAlign {
    lead bits(3)
    align
    data [17]uint8
}

type UnmarkedAtEntry {
    data [17]uint8
}

type MarkedAfterString {
    s    string(31)
    data [3]uint8
}

type MarkedAfterBytes {
    b    bytes(64)
    data [3]uint8
}

type UnmarkedOffBoundary {
    align
    flag bool
    data [4]uint8
}

type MarkedWholeBytesBetween {
    align
    a    uint16
    b    bits(24)
    data [2]uint8
}

type UnmarkedRanged {
    align
    data [4]uint8 | min = 0, max = 255
}

type UnmarkedCounted {
    align
    data [..8]uint8
}

type MarkedBranchAgree {
    cond bool
    align
    if cond {
        x bits(8)
    } else {
        y bits(8)
    }
    data [2]uint8
}

type UnmarkedBranchDisagree {
    cond bool
    align
    if cond {
        x bits(3)
    }
    data [2]uint8
}

type EndsAligned {
    s string(15)
}

type EndsOffBoundary {
    align
    x bits(3)
}

type EndsUnknown {
    x bits(3)
}

type MarkedAfterNestedAligned {
    inner EndsAligned
    data  [2]uint8
}

type UnmarkedAfterNestedOffBoundary {
    inner EndsOffBoundary
    data  [2]uint8
}

type UnmarkedAfterNestedUnknown {
    align
    inner EndsUnknown
    data  [2]uint8
}

type UnmarkedAfterCountedPrefix {
    align
    counted [..3]uint16
    data    [2]uint8
}

type MarkedAfterCountedBytePrefix {
    align
    counted [..255]uint16
    data    [2]uint8
}
`

// want: type name -> may the data field take the bulk path?
var want = map[string]bool{
	"MarkedAfterAlign":    true,  // align puts the elements on a boundary
	"UnmarkedAtEntry":     false, // entry offset is unknown — a type may embed at any bit position
	"MarkedAfterString":   true,  // string wire ends on a byte boundary (§4.7)
	"MarkedAfterBytes":    true,  // same shape as string
	"UnmarkedOffBoundary": false, // align + 1 bool bit = position 1 (mod 8)
	// align + 16 + 24 bits keeps the boundary
	"MarkedWholeBytesBetween": true,
	// ranged elements go through the ranged wire path, not the raw byte path
	"UnmarkedRanged": false,
	// counted arrays are position-tracked but not on the bulk path (yet)
	"UnmarkedCounted": false,
	// both branch sides add exactly 8 bits from a boundary
	"MarkedBranchAgree": true,
	// then-side adds 3 bits, the empty else adds 0 — position unknown after
	"UnmarkedBranchDisagree": false,
	// the nested struct ends aligned regardless of its entry offset
	"MarkedAfterNestedAligned": true,
	// the nested struct exits 3 bits past a boundary
	"UnmarkedAfterNestedOffBoundary": false,
	// the nested struct's exit depends on its (unknown) entry
	"UnmarkedAfterNestedUnknown": false,
	// count prefix [0, 3] is 2 bits — elements whole bytes, data at 2 (mod 8)
	"UnmarkedAfterCountedPrefix": false,
	// count prefix [0, 255] is 8 bits — data back on a boundary
	"MarkedAfterCountedBytePrefix": true,
	// the exit-shape helper types have no data field at all
	"EndsAligned":     false,
	"EndsOffBoundary": false,
	"EndsUnknown":     false,
}

func loadAlignCorpus(t *testing.T) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Align.schema", []byte(alignCorpus))
	if len(perrs) > 0 {
		t.Fatalf("test corpus does not parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  "Align.schema",
		Name:  "Align.schema",
		Base:  "Align",
		Bytes: []byte(alignCorpus),
		AST:   f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("test corpus does not check: %v", cerrs[0])
	}
	return u
}

func TestAlignedFixedByteArrays(t *testing.T) {
	u := loadAlignCorpus(t)
	seen := 0
	for name, st := range u.Structs {
		expect, ok := want[name]
		if !ok {
			t.Errorf("type %s missing from the expectation table", name)
			continue
		}
		seen++
		marked := ir.AlignedFixedByteArrays(st)
		var dataField *ir.Field
		for _, f := range st.Fields {
			if f.Name == "data" {
				dataField = f
			}
		}
		if dataField == nil {
			if expect {
				t.Errorf("%s: no data field found", name)
			}
			if len(marked) != 0 {
				t.Errorf("%s: no data field, yet %d fields marked", name, len(marked))
			}
			continue
		}
		if got := marked[dataField]; got != expect {
			t.Errorf("%s.data: marked = %v, want %v", name, got, expect)
		}
		// nothing but the data field may ever be marked
		for f := range marked {
			if f != dataField {
				t.Errorf("%s: unexpected marked field %s", name, f.Name)
			}
		}
	}
	if seen != len(want) {
		var missing []string
		for name := range want {
			if _, ok := u.Structs[name]; !ok {
				missing = append(missing, name)
			}
		}
		t.Fatalf("expected %d types, saw %d (missing: %s)", len(want), seen, strings.Join(missing, ", "))
	}
}
