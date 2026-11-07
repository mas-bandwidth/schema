package tablewire

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A FIXED ROOT IS FORM 3 IN THE ENGINE, BOTH WAYS (docs/SPEC-TABLES.md "The
// three wires", §3.4; docs/rulings.json fixed-root-refuses-form1). The rule,
// dated 2026-11-03: "If we expect a fixed table, but it is not, refuse."
//
// The pin is the goose refusal pin's own unit and value
// (internal/codegen/goosetable/testdata/load_refusal_reasons.tsv), and the two
// files are the C++ reference's: fixedRootForm3 is RootFixedSave of one record,
// and fixedRootForm1 is the same value under the variable form's writer, the
// bytes every plain Save wrote at a fixed root before the ruling.
const fixedRootUnit = `package fixedpin

fixed table Root
{
    a     int32
    flag  bool
    small int8 | max = 5
    f     float32
}
`

const (
	fixedRootForm3 = "030000000000000009cb5e63a5a41b685900000005000000a5b251ef2bfe7cca0d0a000000040000008cec01864cdc63af040400000000000000170b8c0879d0f2d50101000000000000008c9998e072e16c7d020100000000000000d9ea01864cdb63af0a040000000000000009cb5e63a5a41b680700000001030000c03f"
	fixedRootForm1 = "01010407000000020101030203040a0000c03f008cec01864cdc63af170b8c0879d0f2d58c9998e072e16c7dd9ea01864cdb63af0400000000000000"
)

func fixedRootValue(t *testing.T, m *tabletext.Model) *tabletext.Instance {
	t.Helper()
	return covPlace(t, m, "Root", `{"a": 7, "flag": true, "small": 3, "f": 1.5}`)
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFixedRootWritesTheReferenceForm3(t *testing.T) {
	t.Parallel()
	m := covModel(t, fixedRootUnit)
	got, err := Encode(m, fixedRootValue(t, m))
	if err != nil {
		t.Fatal(err)
	}
	if want := mustHex(t, fixedRootForm3); !bytes.Equal(got, want) {
		t.Fatalf("Encode at a fixed root:\n got %x\nwant %x (the reference's RootFixedSave)", got, want)
	}
}

func TestFixedRootReadsForm3(t *testing.T) {
	t.Parallel()
	m := covModel(t, fixedRootUnit)
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	ok, err := Decode(m, inst, mustHex(t, fixedRootForm3), &report)
	if err != nil || !ok || !report.Silent() || report.Refused {
		t.Fatalf("Decode of the reference's form 3: ok=%v err=%v report=%+v", ok, err, report)
	}
	back, err := Encode(m, inst)
	if err != nil || !bytes.Equal(back, mustHex(t, fixedRootForm3)) {
		t.Fatalf("the value read back does not write the same bytes: %x, %v", back, err)
	}
}

func TestFixedRootRefusesForm1AsPreviousForm(t *testing.T) {
	t.Parallel()
	m := covModel(t, fixedRootUnit)
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	ok, err := Decode(m, inst, mustHex(t, fixedRootForm1), &report)
	var refusal *FixedRefusal
	if ok || !errors.As(err, &refusal) || refusal.Reason != FixedReasonPreviousForm {
		t.Fatalf("a form-1 file at a fixed root: ok=%v err=%v, want the previous_form refusal", ok, err)
	}
	if !Refused(err) || !report.Refused || report.Reason != "previous_form" {
		t.Errorf("the refusal is not the report's verdict: %+v", report)
	}
	if report.Unknown != 0 || report.Widened != 0 {
		t.Errorf("a refusal moves no counter: %+v", report)
	}
	if inst.Fields[0].Cell.I != 0 {
		t.Errorf("a refused read decoded a value: a=%d", inst.Fields[0].Cell.I)
	}
}

// THE FIXED READER'S OTHER NAMES, each on a file this reader did not write.
func TestFixedRootRefusalNames(t *testing.T) {
	t.Parallel()
	m := covModel(t, fixedRootUnit)
	good := mustHex(t, fixedRootForm3)
	mutate := func(f func(b []byte) []byte) []byte { return f(append([]byte(nil), good...)) }
	rows := []struct {
		name      string
		wire      []byte
		reason    string
		malformed bool
	}{
		{"message form", mutate(func(b []byte) []byte { b[0] = 2; return b }), "", false},
		{"newer form", mutate(func(b []byte) []byte { b[0] = 9; return b }), "", false},
		{"reserved byte", mutate(func(b []byte) []byte { b[3] = 1; return b }), FixedReasonReservedNotZero, true},
		{"foreign hash", mutate(func(b []byte) []byte { b[8] ^= 1; return b }), FixedReasonLayoutUnknown, false},
		{"layout byte", mutate(func(b []byte) []byte { b[21] ^= 1; return b }), FixedReasonLayoutMalformed, false},
		{"cut record", good[:len(good)-1], FixedReasonTruncated, true},
		{"two records", append(append([]byte(nil), good...), good[len(good)-18:]...), FixedReasonBatchTooLarge, false},
		{"empty", nil, FixedReasonTruncated, true},
	}
	for _, r := range rows {
		inst := m.New(m.Lookup("Root"))
		var report tabletext.Report
		ok, err := Decode(m, inst, r.wire, &report)
		if ok {
			t.Errorf("%s: read, want a refusal", r.name)
			continue
		}
		if r.malformed {
			if !report.Refused || report.Reason != r.reason {
				t.Errorf("%s: report %+v, want malformed %s", r.name, report, r.reason)
			}
			continue
		}
		if !Refused(err) || !report.Refused {
			t.Errorf("%s: err %v report %+v, want a refusal", r.name, err, report)
			continue
		}
		if r.reason != "" && report.Reason != r.reason {
			t.Errorf("%s: reason %q, want %q", r.name, report.Reason, r.reason)
		}
	}
}

// A VARIABLE ROOT IS UNTOUCHED: the same fields under a plain `table` still
// write and read form 1.
func TestVariableRootKeepsForm1(t *testing.T) {
	t.Parallel()
	m := covModel(t, `package varpin

table Root
{
    a int32
}
`)
	inst := covPlace(t, m, "Root", `{"a": 7}`)
	wire, err := Encode(m, inst)
	if err != nil || len(wire) == 0 || wire[0] != 1 {
		t.Fatalf("a variable root wrote %x, %v; want form 1", wire, err)
	}
}

// The declaration still expects form 3 when the body exceeds the emitter's
// bound. The writer must refuse that layout, never silently choose form 1.
func TestFixedRootBeyondEmissionBoundRefusesForm1(t *testing.T) {
	m := covModel(t, `package oversized
fixed table Huge { payload bytes(65536) }
`)
	st := m.Lookup("Huge")
	if ir.TableFixedEmitted(m.Unit, st) {
		t.Fatal("fixture still fits the fixed form")
	}
	inst := m.New(st)
	// A complete empty form-1 file: form, root terminator, zero ID count.
	wire := []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	var report tabletext.Report
	ok, err := Decode(m, inst, wire, &report)
	var refusal *FixedRefusal
	if ok || !errors.As(err, &refusal) || refusal.Reason != FixedReasonPreviousForm {
		t.Fatalf("oversized fixed root accepted form 1: ok=%v err=%v report=%+v", ok, err, report)
	}
	if out, err := Encode(m, inst); err == nil || len(out) != 0 {
		t.Fatalf("unsupported fixed layout wrote %x, %v", out, err)
	}
	if ok, err := Decode(m, inst, []byte{3}, &report); ok || err == nil {
		t.Fatalf("unsupported fixed layout read form 3: ok=%v err=%v", ok, err)
	}
}

func TestFixedRootAtEmissionBoundStillWritesForm3(t *testing.T) {
	m := covModel(t, fmt.Sprintf("package boundary\nfixed table Huge { payload bytes(%d) }\n", ir.TableFixedRecordMaxBytes-4))
	st := m.Lookup("Huge")
	if !ir.TableFixedEmitted(m.Unit, st) {
		t.Fatal("boundary fixture is not emitted")
	}
	wire, err := Encode(m, m.New(st))
	if err != nil || len(wire) == 0 || wire[0] != 3 {
		t.Fatalf("boundary write: %x, %v", wire, err)
	}
	var report tabletext.Report
	if ok, err := Decode(m, m.New(st), wire, &report); !ok || err != nil {
		t.Fatalf("boundary read: %v, %v", ok, err)
	}
}
