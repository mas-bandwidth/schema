package tablewire

import (
	"errors"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// The same unit at two builds: the newer one appended the Extra variant.
const appendedFlagsNew = `package appended

flags Cap { Trace, Verbose, Extra }

fixed table Root
{
    cap Cap
}
`

const appendedFlagsOld = `package appended

flags Cap { Trace, Verbose }

fixed table Root
{
    cap Cap
}
`

// TestFlagBitsPastWidthRefuse holds a flags mask to its reader's width W on
// every table path (docs/SPEC-TABLES.md §4, A TABLE LOAD HOLDS A MASK TO ITS
// WIDTH; §3.3; §16.3). The newer reader reads old data, and never the other
// way: an older build that meets a newer build's variable-form file with the
// appended bit set refuses the whole load out_of_range, and the same file with
// only the older build's bits set reads. The fixed form refuses a stored bit
// past W the same way. Every writer refuses a mask past W, so a same-build
// round trip never writes what its own reader refuses. The old rule kept the
// bit and wrote it back; planting it back turns every refusal here green-read.
func TestFlagBitsPastWidthRefuse(t *testing.T) {
	t.Parallel()
	newer, older := covVariableTwin(t, appendedFlagsNew), covVariableTwin(t, appendedFlagsOld)
	for _, tc := range []struct {
		mask uint64
		read bool
	}{{0b011, true}, {0b101, false}, {0b100, false}} {
		inst := newer.New(newer.Lookup("Root"))
		inst.Fields[0].Cell.U = tc.mask
		file, err := Encode(newer, inst)
		if err != nil {
			t.Fatalf("%#b: the newer build's write: %v", tc.mask, err)
		}
		old := older.New(older.Lookup("Root"))
		var r tabletext.Report
		ok, err := Decode(older, old, file, &r)
		switch {
		case tc.read && (!ok || err != nil || r.Refused || old.Fields[0].Cell.U != tc.mask):
			t.Errorf("%#b: the older build read %v %v %+v %#b, want the mask read whole", tc.mask, ok, err, r, old.Fields[0].Cell.U)
		case !tc.read && (ok || RefusalReason(err) != ReasonOutOfRange || !r.Refused):
			t.Errorf("%#b: the older build read %v %v %+v, want the whole load refused out_of_range", tc.mask, ok, err, r)
		}
	}

	// THE FIXED FORM: a stored bit past W refuses the load out_of_range.
	fixed := covModel(t, appendedFlagsOld)
	image := func(mask uint64) []byte {
		inst := fixed.New(fixed.Lookup("Root"))
		inst.Fields[0].Cell.U = mask
		b, err := EncodeFixed(fixed, inst)
		if err != nil {
			t.Fatalf("fixed %#b: %v", mask, err)
		}
		return b
	}
	low, both := image(0b01), image(0b11)
	at := -1
	for i := range low {
		if low[i] != both[i] {
			at = i
		}
	}
	if at < 0 || len(low) != len(both) {
		t.Fatalf("fixed: no mask byte found between %x and %x", low, both)
	}
	forged := append([]byte(nil), low...)
	forged[at] |= 0b100
	var r tabletext.Report
	if ok, err := Decode(fixed, fixed.New(fixed.Lookup("Root")), forged, &r); ok || RefusalReason(err) != ReasonOutOfRange {
		t.Errorf("fixed: a stored bit past W read %v %v %+v, want out_of_range", ok, err, r)
	}

	// EVERY WRITER refuses a mask past W: the variable and fixed file forms, the
	// text form (a mask is its names) and the message form (a mask is W bits).
	for _, m := range []*tabletext.Model{older, fixed} {
		inst := m.New(m.Lookup("Root"))
		inst.Fields[0].Cell.U = 0b101
		var iv *tabletext.InvalidValue
		if out, err := Encode(m, inst); !errors.As(err, &iv) || out != nil {
			t.Errorf("the file writer answered %x %v for a bit past W, want invalid_value", out, err)
		}
		if out, err := m.Write(inst); !errors.As(err, &iv) || out != nil {
			t.Errorf("the text writer answered %q %v for a bit no name carries, want invalid_value", out, err)
		}
		if out, err := EncodeMessage(m, inst); !errors.As(err, &iv) || out != nil {
			t.Errorf("the message writer answered %x %v for a bit past W, want invalid_value", out, err)
		}
	}
	inst := fixed.New(fixed.Lookup("Root"))
	inst.Fields[0].Cell.U = 0b101
	var iv *tabletext.InvalidValue
	if out, err := EncodeFixed(fixed, inst); !errors.As(err, &iv) || out != nil {
		t.Errorf("the fixed writer answered %x %v for a bit past W, want invalid_value", out, err)
	}
}
