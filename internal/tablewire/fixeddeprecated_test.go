package tablewire

// A DEPRECATED FIELD IN THE FIXED FORM IS CARRIED (docs/SPEC-TABLES.md §2.10,
// §3.4, §21.2, §22.2, §22.6; docs/rulings.json
// fixed-deprecated-field-is-carried). The engine reads the stored value and
// checks it as it checks any field of its kind; the writer stores what the
// value holds and refuses what any field of its kind would refuse. The
// declared default applies only where the record does not supply the field.

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const fixedDeprecatedUnit = `package fixeddep

type Inner
{
    kept    int16
    retired int32 = 5 | deprecated
}

fixed table Root
{
    dscalar int32 = 7 | deprecated
    dtext   string(8) = "xy" | deprecated
    dcounted [..3]uint16 | deprecated
    dopt    ?int32 | deprecated
    dnest   Inner | deprecated
    drange  uint8 = 3 | min = 1, max = 5, deprecated
    live    int32
}
`

func fixedDeprecatedField(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("no field %q", name)
	return nil
}

func fixedDeprecatedOffset(t *testing.T, st *ir.Struct, name string) int64 {
	t.Helper()
	off := int64(0)
	for _, f := range st.Fields {
		if f.Name == name {
			return off
		}
		off += ir.TableFixedFieldBytes(f)
	}
	t.Fatalf("no field %q", name)
	return 0
}

func TestFixedDeprecatedFieldIsCarried(t *testing.T) {
	t.Parallel()
	m := covModel(t, fixedDeprecatedUnit)
	src := covPlace(t, m, "Root", `{"dscalar": 99, "dtext": "hello", "dcounted": [11, 22, 33], "dopt": 1234, "dnest": {"kept": -3, "retired": 44}, "drange": 3, "live": 8}`)
	wire, err := Encode(m, src)
	if err != nil {
		t.Fatal(err)
	}
	if got := fixedDeprecatedField(t, src, "dscalar").Cell.I; got != 99 {
		t.Fatalf("the placed deprecated scalar holds %d, want 99", got)
	}

	back := m.New(m.Lookup("Root"))
	var report tabletext.Report
	ok, err := Decode(m, back, wire, &report)
	if !ok || err != nil || !report.Silent() {
		t.Fatalf("the read of a record whose deprecated fields are set: ok=%v err=%v report=%+v", ok, err, report)
	}

	scalar := fixedDeprecatedField(t, back, "dscalar")
	text := fixedDeprecatedField(t, back, "dtext")
	counted := fixedDeprecatedField(t, back, "dcounted")
	opt := fixedDeprecatedField(t, back, "dopt")
	nest := fixedDeprecatedField(t, back, "dnest")
	retired := fixedDeprecatedField(t, nest.Cell.Tab, "retired")
	live := fixedDeprecatedField(t, back, "live")
	if scalar.Cell.I != 99 {
		t.Errorf("the deprecated scalar landed %d, want 99", scalar.Cell.I)
	}
	if string(text.Cell.Str) != "hello" {
		t.Errorf("the deprecated text landed %q, want %q", text.Cell.Str, "hello")
	}
	if counted.Count != 3 || counted.Elems[0].U != 11 || counted.Elems[1].U != 22 || counted.Elems[2].U != 33 {
		t.Errorf("the deprecated counted array landed count=%d values=%v", counted.Count, counted.Elems)
	}
	if !opt.Present || opt.Cell.I != 1234 {
		t.Errorf("the deprecated optional landed present=%v value=%d", opt.Present, opt.Cell.I)
	}
	if retired.Cell.I != 44 {
		t.Errorf("the nested deprecated scalar landed %d, want 44", retired.Cell.I)
	}
	if got := fixedDeprecatedField(t, nest.Cell.Tab, "kept").Cell.I; got != -3 {
		t.Errorf("the nested live scalar landed %d, want -3", got)
	}
	if live.Cell.I != 8 {
		t.Errorf("the live field landed %d, want 8", live.Cell.I)
	}

	again, err := Encode(m, back)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire, again) {
		t.Fatalf("the record does not write back byte for byte:\n got %x\nwant %x", again, wire)
	}

	// THE READ CHECKS IT AS IT CHECKS ANY FIELD OF ITS KIND: a deprecated
	// uint8 declared [1, 5] holding 9 refuses the whole load.
	st := m.Lookup("Root")
	forged := append([]byte(nil), wire...)
	body := len(forged) - int(ir.TableFixedTypeBytes(st))
	forged[body+int(fixedDeprecatedOffset(t, st, "drange"))] = 9
	refused := m.New(st)
	var forgedReport tabletext.Report
	if ok, err := Decode(m, refused, forged, &forgedReport); ok || err == nil || !forgedReport.Refused {
		t.Fatalf("a deprecated field holding 9 against max = 5: ok=%v err=%v report=%+v, want one refusal", ok, err, forgedReport)
	}

	// AND THE WRITER REFUSES IT TOO: the same value held by the instance.
	forgedPlace := covPlace(t, m, "Root", `{"live": 8}`)
	fixedDeprecatedField(t, forgedPlace, "drange").Cell.U = 9
	if _, err := Encode(m, forgedPlace); err == nil {
		t.Fatalf("the writer accepted a deprecated field holding 9 against max = 5")
	}
}
