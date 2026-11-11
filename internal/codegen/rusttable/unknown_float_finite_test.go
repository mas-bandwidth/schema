package rusttable

// THE FINITE RULE REACHES UNKNOWN FIELDS TOO (docs/SPEC-TABLES.md §3.3, §5):
// a strict read refuses a NaN or an infinity wherever the wire carries one,
// in a field or record this reader declares and in one it does not. The same
// walk serves <root>_load and <root>_load_retain, so both answer one verdict,
// and a retained unknown is never kept to be written back out.
//
// The wires are built in Go from a WRITER declaration that names the fields,
// then read by a READER declaration that does not: every probe field is an
// unknown of the Rust reader, whose verdict is compared with the engine's on
// the same bytes. The six shapes are a top-level float32, a top-level float64,
// an array element, a keyed entry's value, a union arm and a nested body; a
// finite control reads through both loads.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const unknownFloatWriterSchema = `package probe

enum Key { A, B }

union Pick
{
    pf float32
    pi int32
}

type Inner
{
    nf float32
}

table Root
{
    keep       float32
    extra32    float32
    extra64    float64
    extraarr   [..4]float32
    extrakey   [Key]float32
    extraunion Pick
    extranested Inner
}
`

const unknownFloatReaderSchema = `package probe

table Root
{
    keep float32
}
`

// unknownFloatWire reads one finite writer value through the text form and
// writes its wire: the writer refuses a non-finite float on its own, so the
// probes below replace a finite marker's raw bits after the encode.
func unknownFloatWire(t *testing.T, m *tabletext.Model, text string) []byte {
	t.Helper()
	value := m.New(m.Unit.Tables["Root"])
	var report tabletext.Report
	if !m.Read(value, []byte(text), &report) {
		t.Fatalf("the writer value did not read: %s", report.Reason)
	}
	wire, err := tablewire.Encode(m, value)
	if err != nil {
		t.Fatalf("the writer refused its finite value: %v", err)
	}
	return wire
}

// rewriteFloat replaces one finite marker's bytes with the non-finite pattern,
// requiring the marker to occur exactly once so a probe cannot patch the wrong
// field or patch nothing.
func rewriteFloat(t *testing.T, wire, from, to []byte) []byte {
	t.Helper()
	if n := bytes.Count(wire, from); n != 1 {
		t.Fatalf("the finite marker % x occurs %d times in the wire, want once", from, n)
	}
	return bytes.Replace(wire, from, to, 1)
}

func TestRustUnknownFieldFloatIsFinite(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (the generated Rust reads unknown fields)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the unknown-field finite rule cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}

	writer := unitFrom(t, unknownFloatWriterSchema)
	reader := unitFrom(t, unknownFloatReaderSchema)
	wm := tabletext.NewModel(writer)
	rm := tabletext.NewModel(reader)

	const writerValues = `{
    "keep": 1.0,
    "extra32": 1.5,
    "extra64": 6.5,
    "extraarr": [2.5],
    "extrakey": {"A": 3.5},
    "extraunion": {"pf": 4.5},
    "extranested": {"nf": 5.5}
}`
	const controlValues = `{
    "keep": 1.0,
    "extra32": 1.0,
    "extra64": 1.0,
    "extraarr": [1.0],
    "extrakey": {"A": 1.0},
    "extraunion": {"pf": 1.0},
    "extranested": {"nf": 1.0}
}`

	base := unknownFloatWire(t, wm, writerValues)
	nan32 := []byte{0x00, 0x00, 0xC0, 0x7F}
	inf64 := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x7F}

	for _, c := range []struct {
		name string
		from []byte
		to   []byte
	}{
		{"unknown-float32", []byte{0x00, 0x00, 0xC0, 0x3F}, nan32},
		{"unknown-float64", []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x1A, 0x40}, inf64},
		{"unknown-array-element", []byte{0x00, 0x00, 0x20, 0x40}, nan32},
		{"unknown-keyed-entry", []byte{0x00, 0x00, 0x60, 0x40}, nan32},
		{"unknown-union-arm", []byte{0x00, 0x00, 0x90, 0x40}, nan32},
		{"unknown-nested-body", []byte{0x00, 0x00, 0xB0, 0x40}, nan32},
	} {
		wire := rewriteFloat(t, base, c.from, c.to)

		value := rm.New(reader.Tables["Root"])
		var report tabletext.Report
		ok, err := tablewire.DecodeRegion(rm, value, wire, &report)
		if ok {
			t.Fatalf("%s: the engine accepted the non-finite unknown float", c.name)
		}
		var nf *tablewire.NonFiniteRefusal
		if !errors.As(err, &nf) {
			t.Fatalf("%s: the engine refused %v, want the non-finite float verdict", c.name, err)
		}

		body := fmt.Sprintf(`
for retaining in [false, true] {
    let wire = vec!%s;
    let mut report = TableReport::default();
    let mut store = TableRetain { bytes: Vec::with_capacity(4096), ids: Vec::with_capacity(4096) };
    let got = if retaining { root_load_retain(&wire, &mut store, &mut report) } else { root_load(&wire, &mut report) };
    assert!(got.is_none() && report.refused && report.reason == TableReason::NonFiniteFloat, "%s retaining={} accepted a non-finite unknown float: {:?}", retaining, report);
}
`, rustByteVector(wire), c.name)
		if out, err := rustConstructProbe(t, reader, body); err != nil {
			t.Fatalf("%s: %v\n%s", c.name, err, out)
		}
	}

	control := unknownFloatWire(t, wm, controlValues)
	value := rm.New(reader.Tables["Root"])
	var report tabletext.Report
	if ok, err := tablewire.DecodeRegion(rm, value, control, &report); !ok || err != nil {
		t.Fatalf("the engine refused the finite control: %v %+v", err, report)
	}
	body := fmt.Sprintf(`
for retaining in [false, true] {
    let wire = vec!%s;
    let mut report = TableReport::default();
    let mut store = TableRetain { bytes: Vec::with_capacity(4096), ids: Vec::with_capacity(4096) };
    let got = if retaining { root_load_retain(&wire, &mut store, &mut report) } else { root_load(&wire, &mut report) };
    assert!(got.is_some() && !report.refused, "finite-control retaining={} refused: {:?}", retaining, report);
}
`, rustByteVector(control))
	if out, err := rustConstructProbe(t, reader, body); err != nil {
		t.Fatalf("finite control: %v\n%s", err, out)
	}
}
