package squirreltable_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideProbeSchema holds kind 33 at both sites a reader decodes it: a table's
// own field and a union arm's payload (docs/SPEC-TABLES.md §3). The field is
// f34 because its wire id's low byte is 0xDE: after a trailing high surrogate
// the terminator (00) and that id byte (DE) spell the valid low surrogate
// DE00, so a reader that ran past L would accept a pair the oracle refuses.
const wideProbeSchema = `package probewide

union Body
{
    wide   wstring(4)
    narrow string(4)
}

table Root
{
    f34  wstring(4)
    body Body
}
`

// wideCase is one hand-made form-1 file: a kind 33 payload at the field, or
// at the union's wide arm.
type wideCase struct {
	name    string
	arm     bool
	payload []byte
}

// wideCases is every refusal kind 33's reader owes (SPEC §4.12's content rule
// in this wire's idiom, wideValid and wideBoundary in internal/tablewire),
// at both sites, beside the well-formed payloads that must land: an astral
// pair, exactly the bound, and a clamp whose cut falls inside a pair.
var wideCases = []wideCase{
	{"field odd byte length", false, []byte{0x41}},
	{"field zero code unit", false, []byte{0x41, 0x00, 0x00, 0x00}},
	{"field lone high surrogate", false, []byte{0x00, 0xD8}},
	{"field lone low surrogate", false, []byte{0x00, 0xDC}},
	{"field high surrogate then a non-low unit", false, []byte{0x00, 0xD8, 0x41, 0x00}},
	{"field trailing high surrogate", false, []byte{0x41, 0x00, 0x00, 0xD8}},
	{"field astral pair", false, []byte{0x3D, 0xD8, 0x00, 0xDE}},
	{"field at the bound", false, []byte{0x41, 0, 0x42, 0, 0x43, 0, 0x44, 0}},
	{"field clamp inside a pair", false, []byte{0x41, 0, 0x42, 0, 0x43, 0, 0x3D, 0xD8, 0x00, 0xDE}},
	{"arm odd byte length", true, []byte{0x41, 0x00, 0x42}},
	{"arm zero code unit", true, []byte{0x00, 0x00}},
	{"arm lone low surrogate", true, []byte{0x41, 0x00, 0x00, 0xDC}},
	{"arm trailing high surrogate", true, []byte{0x41, 0x00, 0x00, 0xD8}},
	{"arm astral pair", true, []byte{0x3D, 0xD8, 0x00, 0xDE}},
	{"arm clamp inside a pair", true, []byte{0x41, 0, 0x42, 0, 0x43, 0, 0x3D, 0xD8, 0x00, 0xDE}},
}

// wideWire frames one case as a whole form-1 file: the field under kind 33,
// or the union under kind 15 naming its wide arm, then the terminator and the
// id table.
func wideWire(u *ir.Unit, c wideCase) []byte {
	root := u.Tables["Root"]
	var ids []uint64
	body := binary.AppendUvarint(nil, 1)
	if !c.arm {
		ids = append(ids, ir.TableFieldWireId(root.Fields[0]))
		body = append(body, byte(ir.TableKindWstring))
	} else {
		un := root.Fields[1].Type.Ref.(*ir.Union)
		ids = append(ids, ir.TableFieldWireId(root.Fields[1]), ir.TableWireId(un.Variants[0].WireName()))
		body = append(body, byte(ir.TableKindUnion))
		body = binary.AppendUvarint(body, 2)
		body = append(body, byte(ir.TableKindWstring))
	}
	body = binary.AppendUvarint(body, uint64(len(c.payload)))
	body = append(body, c.payload...)
	body = append(body, 0)
	wire := append([]byte{1}, body...)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

// wideLoadProbe prints, per case, the report's counters and the value the
// field or the arm landed: its code units in hex, or the arm tag when None.
const wideLoadProbe = `local inputs = dofile("inputs.nut", true);
local schema = dofile("probewide/schema_module.nut", true);
local T = schema.require("ProbewideTables");
local L = schema.require("schema_table_load");
local units = function(a) { local s = ""; foreach (u in a) s += format("%04x", u); return s == "" ? "-" : s; };
local run = function(key, arm) {
  local report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0, malformed = false, refused = false };
  local v = L.loadTolerant(T, "Root", inputs[key], report);
  local got = v == null ? "null" : (arm ? (v.Body.Type == 1 ? units(v.Body.Wide) : "none") : units(v.F34));
  print(key + "|" + report.unknown + "," + report.kindMismatch + "," + report.widened + "," + report.clamped + "," +
    report.duplicate + "," + (report.malformed ? "true" : "false") + "," + (report.refused ? "refused" : "read") + "|" + got + "\n");
};
`

// wideOracle is internal/tablewire's answer for one case, in the probe's
// shape: the counters, then the field's units or the arm's.
func wideOracle(t *testing.T, model *tabletext.Model, c wideCase, wire []byte) string {
	t.Helper()
	var report tabletext.Report
	v := model.New(model.Lookup("Root"))
	if _, err := tablewire.Decode(model, v, wire, &report); err != nil {
		t.Fatalf("%s: %v", c.name, err)
	}
	if report.Malformed {
		return "malformed"
	}
	got := ""
	if !c.arm {
		got = unitsHex(v.Fields[0].Cell.Units[:v.Fields[0].Count])
	} else {
		// the union's tag is the wide arm's ordinal, 1, when it reads that arm
		arm := v.Fields[1].Cell.Arm
		if v.Fields[1].Cell.U != 1 || arm == nil {
			got = "none"
		} else {
			got = unitsHex(arm.Cell.Units[:arm.Count])
		}
	}
	return reportOf(report) + "|" + got
}

func unitsHex(units []uint16) string {
	if len(units) == 0 {
		return "-"
	}
	var b strings.Builder
	for _, u := range units {
		fmt.Fprintf(&b, "%04x", u)
	}
	return b.String()
}

// runWideCases loads every case through the Squirrel reader, the emitted load
// runtime first passed through patch (nil leaves it as generated), and answers
// each case's probe line.
func runWideCases(t *testing.T, u *ir.Unit, wires map[string][]byte, patch func(string) string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	emitUnit(t, dir, "probewide", u)
	if patch != nil {
		path := filepath.Join(dir, "probewide", "schema_table_load.nut")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, []byte(patch(string(data))))
	}
	var p strings.Builder
	p.WriteString(wideLoadProbe)
	for _, c := range wideCases {
		fmt.Fprintf(&p, "run(%q, %t);\n", c.name, c.arm)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", wires)), "\n"), "\n") {
		name, rest, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("probe line %q has no case", line)
		}
		if strings.Contains(rest, ",true,") && strings.HasSuffix(rest, "|null") {
			rest = "malformed"
		}
		got[name] = rest
	}
	return got
}

// wideAnswers is the oracle's answer per case, and the wire per case.
func wideAnswers(t *testing.T) (*ir.Unit, map[string][]byte, map[string]string) {
	t.Helper()
	u := loadSource(t, wideProbeSchema)
	model := tabletext.NewModel(u)
	wires := map[string][]byte{}
	want := map[string]string{}
	for _, c := range wideCases {
		wires[c.name] = wideWire(u, c)
		want[c.name] = wideOracle(t, model, c, wires[c.name])
	}
	return u, wires, want
}

// TestSquirrelWideTextMatchesTheWire reads every kind 33 case through the
// Squirrel load and through internal/tablewire and requires the same counters
// and the same landed value: an odd L and ill-formed content are malformed
// with no returned value, a pair lands
// whole, and a clamp never splits one (docs/SPEC-TABLES.md §3, §4).
func TestSquirrelWideTextMatchesTheWire(t *testing.T) {
	t.Parallel()
	u, wires, want := wideAnswers(t)
	got := runWideCases(t, u, wires, nil)
	malformed := 0
	for _, c := range wideCases {
		if got[c.name] != want[c.name] {
			t.Errorf("%s: Squirrel answers %q, the wire oracle %q", c.name, got[c.name], want[c.name])
		}
		if want[c.name] == "malformed" {
			malformed++
		}
	}
	if malformed < 8 {
		t.Fatalf("the oracle calls %d cases malformed; the battery pins more than eight refusals", malformed)
	}
	t.Logf("%d kind 33 cases agree with the wire oracle, %d of them malformed", len(wideCases), malformed)
}

// TestSquirrelWideTextNegativeControl is the decode's sabotage control: each
// blade removes one content rule from the emitted wideDecode, and the battery
// must go red on EXACTLY the cases that rule decides, every other case staying
// green. Two blades, two rules: the zero unit and the low surrogate that
// comes first.
func TestSquirrelWideTextNegativeControl(t *testing.T) {
	t.Parallel()
	u, wires, want := wideAnswers(t)
	for _, blade := range []struct {
		name, anchor, replace string
		red                   []string
	}{
		{
			name:    "zero unit",
			anchor:  "    if (u == 0) return null;\n",
			replace: "",
			red:     []string{"arm zero code unit", "field zero code unit"},
		},
		{
			name:    "low surrogate first",
			anchor:  "      return null; // a low surrogate first\n",
			replace: "      // sabotaged\n",
			red:     []string{"arm lone low surrogate", "field lone low surrogate"},
		},
	} {
		t.Run(blade.name, func(t *testing.T) {
			t.Parallel()
			got := runWideCases(t, u, wires, func(src string) string {
				if strings.Count(src, blade.anchor) != 1 {
					t.Fatalf("the blade's anchor %q is not in the emitted load exactly once", blade.anchor)
				}
				return strings.Replace(src, blade.anchor, blade.replace, 1)
			})
			var red []string
			for _, c := range wideCases {
				if got[c.name] != want[c.name] {
					red = append(red, c.name)
				}
			}
			sort.Strings(red)
			if strings.Join(red, ";") != strings.Join(blade.red, ";") {
				t.Fatalf("the blade turned %v red; it decides exactly %v", red, blade.red)
			}
		})
	}
}
