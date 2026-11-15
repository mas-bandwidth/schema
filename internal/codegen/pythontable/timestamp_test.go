package pythontable

// TestPythonVtTimestamp holds the form-1 table wire's `timestamp` kind
// (docs/SPEC-TABLES.md §2.11) to the Go engine: a variable table's timestamp
// field rides kind 36 as an eight-byte two's-complement little-endian count,
// is an `int` in int64's range on write, and reads back to the same count it
// was written from. The engine's own wires are the oracle and every accepted
// case must save byte for byte; a kind-mismatched or shortened wire is refused
// with no value. Both pinned interpreters run the probe, and the negative
// control plants a wrong width in a copy of the runtime so the gate goes red by
// name. The text forms belong to py-json-2 and py-handtext-2, the fixed form
// to py-fixed-form-2.

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const pythonTimestampSchema = `package stamp

table Event
{
    at timestamp
    id uint32
}
`

// pythonTimestampRow is one case: the engine's variable wire, the value the
// read must land, or a refusal.
type pythonTimestampRow struct {
	name   string
	at     int64
	id     uint32
	wire   []byte
	refuse bool
}

// pythonTimestampRows builds the engine's wires for the counts §2.11 names:
// the zero time, both signs, the min and max of the int64 cell, and a wire
// respelled int64 (kind 5) plus a shortened wire, each refused.
func pythonTimestampRows(t *testing.T) []pythonTimestampRow {
	t.Helper()
	u := loadUnit(t, pythonTimestampSchema)
	m := tabletext.NewModel(u)
	st := m.Lookup("Event")
	if st == nil {
		t.Fatal("the unit has no table Event")
	}
	encode := func(at int64, id uint32) []byte {
		inst := m.New(st)
		atField, ok := inst.FieldByKey("at")
		if !ok {
			t.Fatal("Event has no field at")
		}
		atField.Cell.I = at
		atField.Cell.U = uint64(at)
		idField, ok := inst.FieldByKey("id")
		if !ok {
			t.Fatal("Event has no field id")
		}
		idField.Cell.U = uint64(id)
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("the engine refused a timestamp: %v", err)
		}
		return wire
	}

	rows := []pythonTimestampRow{
		{name: "zero", at: 0, id: 0, wire: encode(0, 0)},
		{name: "positive", at: 1729296000123456789, id: 42, wire: encode(1729296000123456789, 42)},
		{name: "negative", at: -86400000000000, id: 7, wire: encode(-86400000000000, 7)},
		{name: "minimum", at: math.MinInt64, id: 1, wire: encode(math.MinInt64, 1)},
		{name: "maximum", at: math.MaxInt64, id: 2, wire: encode(math.MaxInt64, 2)},
	}
	positive := rows[1].wire
	if len(positive) < 3 || positive[2] != 36 {
		t.Fatalf("the engine's timestamp wire does not open with kind 36: %x", positive)
	}
	mismatch := append([]byte(nil), positive...)
	mismatch[2] = 5 // the int64 kind: §2.11 refuses the respelling by name
	rows = append(rows, pythonTimestampRow{name: "kind_int64", wire: mismatch, refuse: true})
	short := append([]byte(nil), positive[:len(positive)-4]...)
	rows = append(rows, pythonTimestampRow{name: "shortened", wire: short, refuse: true})
	return rows
}

// pythonTimestampProbe drives Event.load/save over the cases file and the wire
// files beside it. Every mismatch breaks with a line that names the case.
const pythonTimestampProbe = `import sys
from stamp import Event, InvalidValue, Refused

with open("cases.txt", "r", encoding="utf-8") as f:
    cases = [line.rstrip("\n").split("\t") for line in f if line.strip()]

for case in cases:
    name, verdict = case[0], case[1]
    with open("case_" + name + ".bin", "rb") as f:
        data = f.read()
    try:
        value = Event.load(data)
    except Refused as err:
        if verdict != "refused":
            raise SystemExit("vt_timestamp " + name + ": refused " + err.reason + ", want accepted")
        continue
    if verdict != "ok":
        raise SystemExit("vt_timestamp " + name + ": accepted, want refused")
    at, ident, want = int(case[2]), int(case[3]), case[4]
    if value.at != at:
        raise SystemExit("vt_timestamp " + name + ": at " + str(value.at) + ", want " + str(at))
    if value.id != ident:
        raise SystemExit("vt_timestamp " + name + ": id " + str(value.id) + ", want " + str(ident))
    got = value.save().hex()
    if got != want:
        raise SystemExit("vt_timestamp " + name + ": wrote " + got + ", want " + want)

for bad in (1 << 63, -(1 << 63) - 1):
    value = Event()
    value.at = bad
    try:
        value.save()
    except InvalidValue:
        pass
    else:
        raise SystemExit("vt_timestamp write_range: " + str(bad) + " was written")

value = Event()
value.at = -(1 << 63)
value.id = (1 << 32) - 1
if value.save() != value.save():
    raise SystemExit("vt_timestamp write_range: two saves of one value differ")

print("OK")
`

func pythonTimestampInputs(t *testing.T, rows []pythonTimestampRow) map[string][]byte {
	t.Helper()
	inputs := map[string][]byte{}
	var cases strings.Builder
	for _, row := range rows {
		inputs["case_"+row.name+".bin"] = row.wire
		if row.refuse {
			fmt.Fprintf(&cases, "%s\trefused\n", row.name)
			continue
		}
		fmt.Fprintf(&cases, "%s\tok\t%d\t%d\t%s\n", row.name, row.at, row.id, hex.EncodeToString(row.wire))
	}
	inputs["cases.txt"] = []byte(cases.String())
	return inputs
}

func TestPythonVtTimestamp(t *testing.T) {
	t.Parallel()
	rows := pythonTimestampRows(t)
	files, err := Generate(loadUnit(t, pythonTimestampSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)

	inputs := pythonTimestampInputs(t, rows)
	inputs["main.py"] = []byte(pyrun.ImportPrelude + pythonTimestampProbe)
	current := string(pyrun.Run(t, dir, "main.py", inputs))
	floor := string(pyrun.RunFloor(t, dir, "main.py", inputs))
	if current != floor {
		t.Fatalf("the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", current, floor)
	}
	if !strings.Contains(current, "OK\n") {
		t.Fatalf("the probe did not finish:\n%s", current)
	}
	if out, err := pyrun.Analyze(t, dir, "stamp"); err != nil {
		t.Fatalf("mypy --strict over the generated package:\n%s", out)
	}
	if out, err := pyrun.Analyze(t, dir, "stamp/_schema"); err != nil {
		t.Fatalf("mypy --strict over the runtime:\n%s", out)
	}
}

// TestPythonVtTimestampNegativeControl plants a four-byte timestamp width in
// a COPY of the runtime's kind_int.py: every eight-byte count the wire carries
// is then read short, so the probe must go red by case name.
func TestPythonVtTimestampNegativeControl(t *testing.T) {
	t.Parallel()
	rows := pythonTimestampRows(t)
	files, err := Generate(loadUnit(t, pythonTimestampSchema))
	if err != nil {
		t.Fatal(err)
	}
	sabotaged := make(map[string][]byte, len(files))
	for name, data := range files {
		sabotaged[name] = data
	}
	const path = "stamp/_schema/kind_int.py"
	const anchor = "    36: 8,"
	text := string(sabotaged[path])
	if !strings.Contains(text, anchor) {
		t.Fatalf("the negative control's anchor %q is absent from %s", anchor, path)
	}
	sabotaged[path] = []byte(strings.Replace(text, anchor, "    36: 4,", 1))
	dir := t.TempDir()
	writeTree(t, dir, sabotaged)

	inputs := pythonTimestampInputs(t, rows)
	inputs["main.py"] = []byte(pyrun.ImportPrelude + pythonTimestampProbe)
	_, stderr, runErr := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", inputs)
	if runErr == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: a four-byte timestamp width left the probe green")
	}
	if !strings.Contains(stderr, "vt_timestamp positive:") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not naming the positive case:\n%s", stderr)
	}
}
