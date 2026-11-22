package gotable

import "testing"

// R10 (docs/SPEC-TABLES.md section 3.3, "an array arm's declared MINIMUM"): a
// counted [A..B] holds A wherever the field sits. The Go cook writer refuses a
// used count below A on a plain field and in a union arm alike, and still takes
// A through B. No real time, no sockets: the generated unit is built in a
// temporary directory and runs its own test.
func TestGoCookWriterRefusesACountBelowTheMinimum(t *testing.T) {
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

import "testing"

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
`)
}
