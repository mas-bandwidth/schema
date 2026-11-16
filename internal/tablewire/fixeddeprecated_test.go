package tablewire

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A DEPRECATED FIELD IN THE FIXED FORM IS CARRIED (docs/SPEC-TABLES.md §3.4,
// §21.2, §22.6; docs/rulings.json fixed-deprecated-field-is-carried): it reads
// and writes like any other field, and its declared default applies only where
// the record does not supply it.
const carriedUnit = `package carried

type Child
{
    n uint16 = 9
    tag string(4) = "ab"
}

fixed table Root
{
    lead   int32 = 1
    scalar int32 = 4 | deprecated
    text   string(4) = "xy" | deprecated
    counted [..3]uint8 | deprecated
    opt    ?uint16 | deprecated
    kid    Child | deprecated
    ranged uint8 = 3 | min = 1, max = 5, deprecated
    trail  int32 = 2
}
`

func carriedField(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("no field %s", name)
	return nil
}

func carriedValues(t *testing.T, inst *tabletext.Instance) {
	t.Helper()
	if f := carriedField(t, inst, "scalar"); f.Cell.U != 99 {
		t.Fatalf("deprecated scalar read %d, want 99", f.Cell.U)
	}
	if f := carriedField(t, inst, "text"); string(f.Cell.Str) != "wxyz" {
		t.Fatalf("deprecated text read %q, want wxyz", f.Cell.Str)
	}
	if f := carriedField(t, inst, "counted"); f.Count != 3 || f.Elems[0].U != 0x11 || f.Elems[1].U != 0x22 || f.Elems[2].U != 0x33 {
		t.Fatalf("deprecated counted array read count=%d values=%v", f.Count, f.Elems)
	}
	if f := carriedField(t, inst, "opt"); !f.Present || f.Cell.U != 0xbeef {
		t.Fatalf("deprecated optional read present=%v value=%d", f.Present, f.Cell.U)
	}
	if f := carriedField(t, inst, "kid"); f.Cell.Tab == nil {
		t.Fatal("deprecated nested record read as no value")
	} else if n := carriedField(t, f.Cell.Tab, "n"); n.Cell.U != 0x4567 {
		t.Fatalf("deprecated nested record n=%d, want 0x4567", n.Cell.U)
	} else if tag := carriedField(t, f.Cell.Tab, "tag"); string(tag.Cell.Str) != "wxyz" {
		t.Fatalf("deprecated nested record tag=%q, want wxyz", tag.Cell.Str)
	}
	if f := carriedField(t, inst, "ranged"); f.Cell.U != 5 {
		t.Fatalf("deprecated ranged field read %d, want 5", f.Cell.U)
	}
}

func TestFixedDeprecatedFieldIsCarried(t *testing.T) {
	t.Parallel()
	m := covModel(t, carriedUnit)
	inst := covPlace(t, m, "Root", `{
		"lead": 7,
		"scalar": 99,
		"text": "wxyz",
		"counted": [17, 34, 51],
		"opt": 48879,
		"kid": {"n": 17767, "tag": "wxyz"},
		"ranged": 5,
		"trail": 8
	}`)
	wire, err := EncodeFixed(m, inst)
	if err != nil {
		t.Fatalf("fixed save of a value holding deprecated fields: %v", err)
	}
	back := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if ok, err := Decode(m, back, wire, &report); !ok || err != nil || report.Refused {
		t.Fatalf("fixed read of a record with deprecated fields: ok=%v err=%v report=%+v", ok, err, report)
	}
	carriedValues(t, back)
	if f := carriedField(t, back, "lead"); f.Cell.U != 7 {
		t.Fatalf("live lead read %d, want 7", f.Cell.U)
	}
	if f := carriedField(t, back, "trail"); f.Cell.U != 8 {
		t.Fatalf("live trail read %d, want 8", f.Cell.U)
	}
	again, err := EncodeFixed(m, back)
	if err != nil {
		t.Fatalf("fixed save of the value read back: %v", err)
	}
	if !bytes.Equal(wire, again) {
		t.Fatalf("the deprecated fields did not write back byte for byte:\nfirst  %x\nsecond %x", wire, again)
	}

	// A RECORD THAT DOES NOT HOLD THE DEPRECATED VALUES STILL DIFFERS: the
	// writer stores what the value holds rather than the declared default.
	defaults := m.New(m.Lookup("Root"))
	defaultWire, err := EncodeFixed(m, defaults)
	if err != nil {
		t.Fatalf("fixed save of a reset value: %v", err)
	}
	if bytes.Equal(wire, defaultWire) {
		t.Fatal("the writer stored the declared defaults instead of the held deprecated values")
	}
}

func TestFixedDeprecatedFieldIsCarriedRefusesLikeAnyField(t *testing.T) {
	t.Parallel()
	m := covModel(t, carriedUnit)
	inst := m.New(m.Lookup("Root"))
	carriedField(t, inst, "ranged").Cell.U = 9
	if _, err := EncodeFixed(m, inst); err == nil {
		t.Fatal("a deprecated field holding a value outside its declared range was saved; it must be refused like any field")
	}
}

// THE READ CHECKS A DEPRECATED FIELD LIKE ANY FIELD: a stored value outside
// the field's declared range refuses the whole load (the verdict only). The
// field's byte is found as the one byte two in-range saves differ in.
func TestFixedDeprecatedFieldIsCarriedReadRefusesLikeAnyField(t *testing.T) {
	t.Parallel()
	m := covModel(t, carriedUnit)
	inst := m.New(m.Lookup("Root"))
	carriedField(t, inst, "ranged").Cell.U = 5
	wire, err := EncodeFixed(m, inst)
	if err != nil {
		t.Fatalf("fixed save: %v", err)
	}
	carriedField(t, inst, "ranged").Cell.U = 4
	other, err := EncodeFixed(m, inst)
	if err != nil {
		t.Fatalf("fixed save: %v", err)
	}
	if len(wire) != len(other) {
		t.Fatalf("the two saves differ in length: %d and %d", len(wire), len(other))
	}
	at := -1
	for i := range wire {
		if wire[i] != other[i] {
			if at >= 0 {
				t.Fatalf("the two saves differ at %d and %d; want one byte", at, i)
			}
			at = i
		}
	}
	if at < 0 || wire[at] != 5 {
		t.Fatalf("no single byte holds the deprecated ranged field (at=%d)", at)
	}
	wire[at] = 9
	back := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if ok, err := Decode(m, back, wire, &report); ok && err == nil && !report.Refused {
		t.Fatal("a stored deprecated value outside its declared range was read; it must be refused like any field")
	}
}
