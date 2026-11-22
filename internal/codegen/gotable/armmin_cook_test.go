package gotable

import "testing"

// R10 (docs/SPEC-TABLES.md section 3.3, "an array arm's declared MINIMUM"): a
// counted [A..B] holds A wherever the field sits. In Go the cook writer and the
// fixed form's file load both refuse a count below A on a plain field and in a
// union arm alike, and still take A through B. The conformance framing rows
// read a fixed root's file through the variable unit in the Go leg, so this is
// the Go check that reaches the fixed load. No real time, no sockets: the
// generated unit is built in a temporary directory and runs its own test.
func TestGoCountedMinimumHoldsInAUnionArm(t *testing.T) {
	t.Parallel()
	const schema = `package armmin

union Pick
{
    few   [2..4]uint32
    other int32
}

fixed table ArmFix { pick Pick }

fixed table PlainFix { few [2..4]uint32 }
`
	runGeneratedTables(t, schema, `package armmin

import (
	"bytes"
	"testing"
)

func TestCookMinimum(t *testing.T) {
	buf := make([]byte, 4096)
	for _, c := range []struct {
		count int32
		ok    bool
	}{{0, false}, {1, false}, {2, true}, {4, true}, {5, false}} {
		var a ArmFix
		a.Pick.Type = PickTypeFew
		a.Pick.FewCount = c.count
		n := ArmFixCookMeasure(&a)
		if got := ArmFixCookFrom(&a, buf[:n], TableByteOrderLittle); got != c.ok {
			t.Errorf("arm cook count %d: wrote %v, want %v", c.count, got, c.ok)
		}
		var p PlainFix
		p.FewCount = c.count
		n = PlainFixCookMeasure(&p)
		if got := PlainFixCookFrom(&p, buf[:n], TableByteOrderLittle); got != c.ok {
			t.Errorf("plain cook count %d: wrote %v, want %v", c.count, got, c.ok)
		}
	}
}

// the fixed file form: save a lawful record, set the count the way a forged
// file would, zero the dead slots behind it, and load it.
func TestFixedLoadMinimum(t *testing.T) {
	values := []byte{7, 0, 0, 0, 9, 0, 0, 0}
	for _, c := range []struct {
		count byte
		ok    bool
	}{{0, false}, {1, false}, {2, true}} {
		var a ArmFix
		a.Pick.Type = PickTypeFew
		a.Pick.FewCount = 2
		a.Pick.Few[0], a.Pick.Few[1] = 7, 9
		buf := make([]byte, 4096)
		n := ArmFixSave(&a, buf)
		file := append([]byte{}, buf[:n]...)
		i := bytes.LastIndex(file, values)
		if i < 5 {
			t.Fatalf("the arm's values are not in the file")
		}
		file[i-4] = c.count
		for k := int(c.count) * 4; k < 8; k++ {
			file[i+k] = 0
		}
		var out ArmFix
		var r TableReport
		if got := ArmFixLoad(&out, file, &r); got != c.ok {
			t.Errorf("arm fixed load count %d: read %v, want %v (refused %v)", c.count, got, c.ok, r.Refused())
		}
	}
}
`)
}
