package pythontable

// THE UNKNOWN COUNT OVER BLOB RECORDS on the Python leg (docs/SPEC-TABLES.md
// §2.5, §3.1, §6.5). A newer writer's wire is read by an older declaration,
// and the Python report's unknown count must be the Go engine's exactly:
// a blob record is counted only when its kind is one no pointer below the
// reader's root can place (a record of a placeable kind that no known slot
// names is the skipped field's, counted once with it), and a *bytes or
// *string slot that names a record of a type this reader does not declare
// reads None, with the record counted once.

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// blobUnknownReader is the older declaration every case is read by.
const blobUnknownReader = `package probe

table Root
{
    keep *bytes
    n    int32
}
`

type blobUnknownCase struct {
	name, writer, json string
}

var blobUnknownCases = []blobUnknownCase{
	// a skipped *bytes field beside a known one: the record is of a kind the
	// root places, so only the field counts
	{"skipped_bytes_beside_bytes", `package probe

table Root
{
    keep  *bytes
    extra *bytes
    n     int32
}
`, `{"keep":"AQI=","extra":"AwQ=","n":1}`},
	// the same with the known slot null: the root still places *bytes
	{"skipped_bytes_keep_null", `package probe

table Root
{
    keep  *bytes
    extra *bytes
}
`, `{"extra":"AwQ="}`},
	// a skipped *string field: no pointer below the root places *string, so
	// the field and its record each count
	{"skipped_string", `package probe

table Root
{
    keep  *bytes
    extra *string
}
`, `{"keep":"AQI=","extra":"hi"}`},
	// both kinds skipped beside the known *bytes
	{"skipped_both_kinds", `package probe

table Root
{
    keep  *bytes
    extra *bytes
    label *string
}
`, `{"keep":"AQI=","extra":"AwQ=","label":"hi"}`},
	// the known *bytes slot names a record of a table this reader does not
	// declare: the slot reads None and the record counts once
	{"bytes_slot_names_undeclared_table", `package probe

table Extra
{
    v int32
}

table Root
{
    keep *Extra
    n    int32
}
`, `{"keep":{"v":5},"n":2}`},
}

const blobUnknownProbe = `from probe import LoadReport, Refused, Root
for name in open("cases").read().split():
    report = LoadReport()
    try:
        value = Root.load(open(name, "rb").read(), report=report)
    except Refused as err:
        print("case", name, "refused", err.reason)
        continue
    print("case", name, "read", report.unknown, "keep", "None" if value.keep is None else value.keep.hex())
`

// blobUnknownEngine is the Go engine's read of wire by the reader unit:
// "read <unknown> keep <hex|None>" or "refused <reason>".
func blobUnknownEngine(t *testing.T, reader *ir.Unit, wire []byte) string {
	t.Helper()
	m := tabletext.NewModel(reader)
	inst := m.New(reader.Tables["Root"])
	var rep tabletext.Report
	ok, err := tablewire.Decode(m, inst, wire, &rep)
	if !ok {
		return fmt.Sprintf("refused %v", err)
	}
	field, found := inst.FieldByKey("keep")
	if !found {
		t.Fatal("the reader's Root has no keep field")
	}
	keep := "None"
	if field.Cell.Blob != nil {
		keep = hex.EncodeToString(field.Cell.Blob.Data)
	}
	return fmt.Sprintf("read %d keep %s", rep.Unknown, keep)
}

// blobUnknownFailures reads every case's wire with the reader unit, in Go
// and in Python under both pins (mutate, when set, edits the generated
// files first), and answers each case whose Python read is not the engine's.
func blobUnknownFailures(t *testing.T, mutate func(*testing.T, map[string][]byte)) []string {
	t.Helper()
	reader := loadUnit(t, blobUnknownReader)
	inputs := map[string][]byte{}
	names := make([]string, 0, len(blobUnknownCases))
	engine := map[string]string{}
	for _, c := range blobUnknownCases {
		writer := loadUnit(t, c.writer)
		wire := stringIdentityReferenceAt(t, writer, "Root", c.json)
		inputs[c.name] = wire
		names = append(names, c.name)
		engine[c.name] = blobUnknownEngine(t, reader, wire)
	}
	inputs["cases"] = []byte(strings.Join(names, "\n"))
	got := pyKeyedRun(t, reader, blobUnknownProbe, inputs, mutate)
	var fails []string
	for _, name := range names {
		verdict := engine[name]
		if !strings.HasPrefix(verdict, "read ") {
			t.Fatalf("%s: the Go engine answers %s, not a read", name, verdict)
		}
		if want := "case " + name + " " + verdict + "\n"; !strings.Contains(got, want) {
			fails = append(fails, fmt.Sprintf("%s: the Python read differs from the Go engine's %q:\n%s", name, verdict, got))
		}
	}
	return fails
}

// Functional (python pins): run with SCHEMA_SLOW=1.
func TestPythonVtBlobsUnknownCount(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python (it runs the emitted table runtime)")
	for _, f := range blobUnknownFailures(t, nil) {
		t.Error(f)
	}
}

// TestPythonVtBlobsUnknownCountNegativeControl plants, one at a time, in a
// COPY of the runtime:
//
//   - NOT PLACEABLE: the root no longer places the *bytes records its own
//     slot can name, so a skipped *bytes field's record counts a second time;
//     skipped_bytes_beside_bytes must go red;
//   - UNDECLARED REFUSED: a *bytes slot that names a record of a type this
//     reader does not declare refuses instead of reading None;
//     bytes_slot_names_undeclared_table must go red.
//
// Functional (python pins): run with SCHEMA_SLOW=1.
func TestPythonVtBlobsUnknownCountNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python (it runs the emitted table runtime)")
	const file = "probe/_schema/load.py"
	for _, control := range []struct{ name, from, to, row string }{
		{"not-placeable", "                out.add(_BYTES_WIRE_TYPE_ID)\n", "                pass\n", "skipped_bytes_beside_bytes:"},
		{"undeclared-refused", "            if descriptor.by_id(ctx.types[index - 2]) is not None:\n                raise Refused(\"wire_damaged\", \"a byte buffer slot names a table record\")\n", "            raise Refused(\"wire_damaged\", \"a byte buffer slot names a table record\")\n", "bytes_slot_names_undeclared_table:"},
	} {
		t.Run(control.name, func(t *testing.T) {
			t.Parallel()
			mutate := func(t *testing.T, files map[string][]byte) {
				src := string(files[file])
				if strings.Count(src, control.from) != 1 {
					t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", file, control.from, strings.Count(src, control.from))
				}
				files[file] = []byte(strings.Replace(src, control.from, control.to, 1))
			}
			fails := blobUnknownFailures(t, mutate)
			named := false
			for _, f := range fails {
				if strings.HasPrefix(f, control.row) {
					named = true
				}
			}
			if !named {
				t.Fatalf("NEGATIVE CONTROL FAILED: %s did not turn %s red:\n%s", control.name, strings.TrimSuffix(control.row, ":"), strings.Join(fails, "\n"))
			}
		})
	}
}
