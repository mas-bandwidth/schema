package pythontable

// TestPythonVtTimestamp holds the form-1 table wire's `timestamp` kind
// (docs/SPEC-TABLES.md §2.11) to the Go engine: a variable table's timestamp
// field rides kind 36 as an eight-byte two's-complement little-endian count,
// is an `int` in int64's range on write, and reads back to the same count it
// was written from. The engine's own wires are the oracle: every accepted case
// must save byte for byte to the engine's wire, both after the read and from a
// value built fresh, and every refused case must be refused by the engine and
// by Python with the same reason and no value. The cases cover both widening
// directions §2.11 closes (a timestamp wire read into an int64 field, and an
// int64 wire read into a timestamp field), a wire cut inside the timestamp's
// own eight bytes, and the write refusals' path and rule. Both pinned
// interpreters run the probe, and the negative controls plant one fault per
// check in a copy of the runtime so the gate goes red by case name. The text
// forms belong to py-json-2 and py-handtext-2, the fixed form to
// py-fixed-form-2.

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// pythonTimestampSchema declares the timestamp table and its int64 twin. A
// field id is the hash of the field's name (§5), so each table's wire names
// the other's fields, and reading one's wire as the other is exactly the
// respelling between `int64` and `timestamp` §2.11 refuses in both directions.
const pythonTimestampSchema = `package stamp

table Event
{
    at timestamp
    id uint32
}

table Count
{
    at int64
    id uint32
}
`

// pythonTimestampRow is one case: the root it is read as, the engine's
// variable wire, and the value the read must land or the reason it must be
// refused with.
type pythonTimestampRow struct {
	name   string
	root   string
	at     int64
	id     uint32
	wire   []byte
	reason string // "" for an accepted case
}

// pythonTimestampRows builds the engine's wires for the counts §2.11 names
// (the zero time, both signs, the min and max of the int64 cell), the int64
// twin's own wire, and the refused cases, then holds every case to the Go
// engine's own read: an accepted case lands its value, a refused one is
// refused with the case's reason.
func pythonTimestampRows(t *testing.T) []pythonTimestampRow {
	t.Helper()
	m := tabletext.NewModel(loadUnit(t, pythonTimestampSchema))
	lookup := func(root string) *ir.Struct {
		st := m.Lookup(root)
		if st == nil {
			t.Fatalf("the unit has no table %s", root)
		}
		return st
	}
	field := func(inst *tabletext.Instance, key string) *tabletext.Field {
		fv, ok := inst.FieldByKey(key)
		if !ok {
			t.Fatalf("the table has no field %s", key)
		}
		return fv
	}
	encode := func(root string, at int64, id uint32) []byte {
		inst := m.New(lookup(root))
		atField := field(inst, "at")
		atField.Cell.I = at
		atField.Cell.U = uint64(at)
		field(inst, "id").Cell.U = uint64(id)
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("the engine refused %s{at: %d, id: %d}: %v", root, at, id, err)
		}
		return wire
	}

	rows := []pythonTimestampRow{
		{name: "zero", root: "Event", at: 0, id: 0},
		{name: "positive", root: "Event", at: 1729296000123456789, id: 42},
		{name: "negative", root: "Event", at: -86400000000000, id: 7},
		{name: "minimum", root: "Event", at: math.MinInt64, id: 1},
		{name: "maximum", root: "Event", at: math.MaxInt64, id: math.MaxUint32},
		{name: "count_positive", root: "Count", at: 1729296000123456789, id: 42},
	}
	for i := range rows {
		rows[i].wire = encode(rows[i].root, rows[i].at, rows[i].id)
	}
	positive := rows[1].wire
	if len(positive) < 3 || positive[2] != 36 {
		t.Fatalf("the engine's timestamp wire does not open with kind 36: %x", positive)
	}
	count := rows[5].wire
	if len(count) < 3 || count[2] != 5 {
		t.Fatalf("the engine's int64 wire does not open with kind 5: %x", count)
	}

	// The timestamp wire with its kind byte respelled int64 (kind 5).
	mismatch := append([]byte(nil), positive...)
	mismatch[2] = 5
	// The id table's trailer cut by four bytes: the file's framing is damaged.
	short := append([]byte(nil), positive[:len(positive)-4]...)
	// The timestamp's own eight bytes cut after three, with the id table kept
	// whole behind them: the root body ends inside the count.
	ids := int(binary.LittleEndian.Uint64(positive[len(positive)-8:]))
	trailer := positive[len(positive)-8-8*ids:]
	cut := append(append([]byte(nil), positive[:3+3]...), trailer...)

	rows = append(rows,
		pythonTimestampRow{name: "kind_int64", root: "Event", wire: mismatch, reason: tablewire.ReasonKindMismatch},
		pythonTimestampRow{name: "int64_wire_as_timestamp", root: "Event", wire: count, reason: tablewire.ReasonKindMismatch},
		pythonTimestampRow{name: "timestamp_wire_as_int64", root: "Count", wire: positive, reason: tablewire.ReasonKindMismatch},
		pythonTimestampRow{name: "shortened", root: "Event", wire: short, reason: tablewire.ReasonWireDamaged},
		pythonTimestampRow{name: "payload_cut", root: "Event", wire: cut, reason: tablewire.ReasonWireDamaged},
	)

	for _, row := range rows {
		inst := m.New(lookup(row.root))
		var report tabletext.Report
		ok, err := tablewire.Decode(m, inst, row.wire, &report)
		if row.reason != "" {
			if ok {
				t.Fatalf("the engine accepted %s, want refused %s", row.name, row.reason)
			}
			if got := tablewire.RefusalReason(err); got != row.reason {
				t.Fatalf("the engine refused %s as %q (%v), want %s", row.name, got, err, row.reason)
			}
			continue
		}
		if !ok {
			t.Fatalf("the engine refused %s: %v", row.name, err)
		}
		if got := field(inst, "at").Cell.I; got != row.at {
			t.Fatalf("the engine read %s at %d, want %d", row.name, got, row.at)
		}
		if got := field(inst, "id").Cell.U; got != uint64(row.id) {
			t.Fatalf("the engine read %s id %d, want %d", row.name, got, row.id)
		}
	}
	return rows
}

// pythonTimestampProbe drives load/save over the cases file and the wire files
// beside it, then the write refusals. Every mismatch breaks with a line that
// names the case.
const pythonTimestampProbe = `import sys
from stamp import Count, Event, InvalidValue, Refused

roots = {"Count": Count, "Event": Event}

with open("cases.txt", "r", encoding="utf-8") as f:
    cases = [line.rstrip("\n").split("\t") for line in f if line.strip()]

for case in cases:
    name, root, verdict = case[0], roots[case[1]], case[2]
    with open("case_" + name + ".bin", "rb") as f:
        data = f.read()
    try:
        value = root.load(data)
    except Refused as err:
        if verdict == "ok":
            raise SystemExit("vt_timestamp " + name + ": refused " + err.reason + ", want accepted")
        if err.reason != verdict:
            raise SystemExit("vt_timestamp " + name + ": refused " + err.reason + ", want " + verdict)
        continue
    if verdict != "ok":
        raise SystemExit("vt_timestamp " + name + ": accepted, want refused " + verdict)
    at, ident, want = int(case[3]), int(case[4]), case[5]
    if value.at != at:
        raise SystemExit("vt_timestamp " + name + ": at " + str(value.at) + ", want " + str(at))
    if value.id != ident:
        raise SystemExit("vt_timestamp " + name + ": id " + str(value.id) + ", want " + str(ident))
    got = value.save().hex()
    if got != want:
        raise SystemExit("vt_timestamp " + name + ": wrote " + got + ", want " + want)
    fresh = root()
    fresh.at = at
    fresh.id = ident
    got = fresh.save().hex()
    if got != want:
        raise SystemExit("vt_timestamp " + name + ": built and wrote " + got + ", want " + want)


def refuse_write(label: str, bad: object, rule: str) -> None:
    value = Event()
    value.at = bad  # type: ignore[assignment]
    try:
        value.save()
    except InvalidValue as err:
        if err.path != "at" or err.rule != rule:
            raise SystemExit("vt_timestamp " + label + ": refused " + err.path + " (" + err.rule + "), want at (" + rule + ")")
        return
    except Exception as err:
        raise SystemExit("vt_timestamp " + label + ": raised " + type(err).__name__ + ", want InvalidValue")
    raise SystemExit("vt_timestamp " + label + ": " + repr(bad) + " was written")


span = "[-9223372036854775808, 9223372036854775807]"
refuse_write("write_range_max", 1 << 63, str(1 << 63) + " is outside " + span)
refuse_write("write_range_min", -(1 << 63) - 1, str(-(1 << 63) - 1) + " is outside " + span)
refuse_write("write_type_bool", True, "not an int")
refuse_write("write_type_float", 1.5, "not an int")
refuse_write("write_type_none", None, "not an int")
refuse_write("write_type_str", "1", "not an int")

print("OK")
`

func pythonTimestampInputs(t *testing.T, rows []pythonTimestampRow) map[string][]byte {
	t.Helper()
	inputs := map[string][]byte{}
	var cases strings.Builder
	for _, row := range rows {
		inputs["case_"+row.name+".bin"] = row.wire
		if row.reason != "" {
			fmt.Fprintf(&cases, "%s\t%s\t%s\n", row.name, row.root, row.reason)
			continue
		}
		fmt.Fprintf(&cases, "%s\t%s\tok\t%d\t%d\t%s\n", row.name, row.root, row.at, row.id, hex.EncodeToString(row.wire))
	}
	inputs["cases.txt"] = []byte(cases.String())
	inputs["main.py"] = []byte(pyrun.ImportPrelude + pythonTimestampProbe)
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

// TestPythonVtTimestampNegativeControl plants one fault per check of the
// probe in a COPY of the generated package (its runtime, or the generated
// module whose straight-line loaders carry the kind check), and each must turn
// the probe red naming the case that check covers. A control's path "" is the
// generated module holding the loaders; sites is how many times its anchor
// stands there, every one of them planted.
func TestPythonVtTimestampNegativeControl(t *testing.T) {
	t.Parallel()
	controls := []struct {
		name    string
		path    string
		sites   int
		anchor  string
		planted string
		red     string
	}{
		{
			// Every eight-byte count is read four bytes short.
			name:    "width",
			path:    "stamp/_schema/kind_int.py",
			sites:   1,
			anchor:  "    36: 8,",
			planted: "    36: 4,",
			red:     "vt_timestamp positive:",
		},
		{
			// A kind mismatch is refused under the wrong reason.
			name:    "reason",
			path:    "",
			sites:   4, // two fields in each of the two loaders
			anchor:  "raise Refused(\"kind_mismatch\", ",
			planted: "raise Refused(\"out_of_range\", ",
			red:     "vt_timestamp kind_int64: refused out_of_range, want kind_mismatch",
		},
		{
			// An int64 field takes a timestamp wire: the second widening
			// direction §2.11 closes is opened, the first stays closed.
			name:    "widen_reverse",
			path:    "",
			sites:   2, // the at field in each of the two loaders
			anchor:  "if kind != fields[0].kind:\n",
			planted: "if kind != fields[0].kind and not (kind == 36 and fields[0].kind == 5):\n",
			red:     "vt_timestamp timestamp_wire_as_int64: accepted, want refused kind_mismatch",
		},
		{
			// A payload cut inside its body is refused under the wrong reason.
			name:    "payload_cut",
			path:    "stamp/_schema/load.py",
			sites:   1,
			anchor:  "raise Refused(\"wire_damaged\", \"a length runs past its body\")",
			planted: "raise Refused(\"past_end\", \"a length runs past its body\")",
			red:     "vt_timestamp payload_cut: refused past_end, want wire_damaged",
		},
		{
			// A bool passes the int check.
			name:    "write_type",
			path:    "stamp/_schema/kind_int.py",
			sites:   1,
			anchor:  "        if type(value) is not int:\n",
			planted: "        if not isinstance(value, int):\n",
			red:     "vt_timestamp write_type_bool: True was written",
		},
		{
			// The range check lets one count past the top through.
			name:    "write_range",
			path:    "stamp/_schema/kind_int.py",
			sites:   1,
			anchor:  "        if value < fd.lo or value > fd.hi:\n",
			planted: "        if value < fd.lo or value > fd.hi + 1:\n",
			red:     "vt_timestamp write_range_max:",
		},
	}

	rows := pythonTimestampRows(t)
	files, err := Generate(loadUnit(t, pythonTimestampSchema))
	if err != nil {
		t.Fatal(err)
	}
	module := ""
	for name, data := range files {
		if strings.Contains(string(data), "def _s_load_Event(") {
			module = name
		}
	}
	if module == "" {
		t.Fatal("no generated module holds Event's loader")
	}
	for _, control := range controls {
		t.Run(control.name, func(t *testing.T) {
			t.Parallel()
			sabotaged := make(map[string][]byte, len(files))
			maps.Copy(sabotaged, files)
			path := control.path
			if path == "" {
				path = module
			}
			text := string(sabotaged[path])
			if got := strings.Count(text, control.anchor); got != control.sites {
				t.Fatalf("the negative control's anchor %q stands %d times in %s, want %d", control.anchor, got, path, control.sites)
			}
			sabotaged[path] = []byte(strings.ReplaceAll(text, control.anchor, control.planted))
			dir := t.TempDir()
			writeTree(t, dir, sabotaged)

			inputs := pythonTimestampInputs(t, rows)
			_, stderr, runErr := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", inputs)
			if runErr == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s fault left the probe green", control.name)
			}
			if !strings.Contains(stderr, control.red) {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s fault went red, but not with %q:\n%s", control.name, control.red, stderr)
			}
		})
	}
}
