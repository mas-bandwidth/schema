package tablewire

import (
	"bytes"
	"errors"
	"strings"
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

// TestAppendedFlagBitsReadThenWrite holds a flags mask's appended bits on
// every path (docs/SPEC-TABLES.md §4, A TABLE LOAD DOES NOT JUDGE A MASK
// AGAINST A RANGE; §3.3; §16.3). An older build reads the newer build's
// variable-form file, keeps the bit it cannot name, and writes the file back
// byte for byte. The fixed form never reads a newer writer (§21), so its half
// is the same build: a mask bit past W reads and writes back whole, as the
// u64 it rides as. The writers that cannot carry that bit, the text form (a
// mask is its names) and the message form (a mask is its declared W bits),
// refuse invalid_value rather than drop it.
func TestAppendedFlagBitsReadThenWrite(t *testing.T) {
	t.Parallel()
	for _, form := range []struct {
		name     string
		newer    *tabletext.Model
		older    *tabletext.Model
		readBack func(m *tabletext.Model, inst *tabletext.Instance, data []byte, r *tabletext.Report) (bool, error)
	}{
		{"variable", covVariableTwin(t, appendedFlagsNew), covVariableTwin(t, appendedFlagsOld), Decode},
		{"fixed", covModel(t, appendedFlagsOld), covModel(t, appendedFlagsOld), Decode},
	} {
		inst := form.newer.New(form.newer.Lookup("Root"))
		inst.Fields[0].Cell.U = 0b101 // Trace and the appended Extra
		file, err := Encode(form.newer, inst)
		if err != nil {
			t.Fatalf("%s: the newer build's write: %v", form.name, err)
		}
		old := form.older.New(form.older.Lookup("Root"))
		var r tabletext.Report
		ok, err := form.readBack(form.older, old, file, &r)
		if !ok || err != nil {
			t.Errorf("%s: the older build refused the newer build's file: %v %+v", form.name, err, r)
			continue
		}
		if got := old.Fields[0].Cell.U; got != 0b101 {
			t.Errorf("%s: the older build read the mask as %#b, want 0b101 (the appended bit kept)", form.name, got)
		}
		again, err := Encode(form.older, old)
		if err != nil || !bytes.Equal(again, file) {
			t.Errorf("%s: the older build's write-back is %x %v, want the file it read, %x", form.name, again, err, file)
		}
		if form.name == "fixed" {
			if fixed, err := EncodeFixed(form.older, old); err != nil || !bytes.Equal(fixed, file) {
				t.Errorf("fixed: EncodeFixed wrote %x %v, want the file it read, %x", fixed, err, file)
			}
		}
		var iv *tabletext.InvalidValue
		if out, err := form.older.Write(old); !errors.As(err, &iv) || out != nil {
			t.Errorf("%s: the text writer answered %q %v for a bit no name carries, want invalid_value", form.name, out, err)
		}
		if out, err := EncodeMessage(form.older, old); !errors.As(err, &iv) || out != nil {
			t.Errorf("%s: the message writer answered %x %v for a bit past W, want invalid_value", form.name, out, err)
		} else if !strings.Contains(err.Error(), "name no variant") {
			t.Errorf("%s: the message writer's refusal is %v", form.name, err)
		}
	}
}
