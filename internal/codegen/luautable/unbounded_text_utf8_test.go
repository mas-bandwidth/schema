package luautable_test

// UNBOUNDED TEXT IS VALIDATED AS ANY TEXT IS (docs/SPEC-TABLES.md §3, §2.13.1):
// a kind 12 payload is well-formed UTF-8 with no zero byte at every bound, the
// unbounded one included. An optional unbounded element ([..N]?string) is an
// element record whose one field is unbounded text, read through the field
// path; that path once copied the bytes unchecked. Each row below plants
// ill-formed bytes in one unbounded text payload of an engine-written wire,
// and the Luau load must report what internal/tablewire reports for it.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const unboundedUTF8Probe = `local inputs = require("./inputs")
local load = require("./schema_load")
MODSfor _, name in NAMES do
	local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
	load.loadTolerant("Where", inputs[name], report)
	print(name .. ":" .. tostring(report.malformed))
end
`

type unboundedUTF8Row struct {
	name  string
	field string // the field whose payload carries the marker
	bad   []byte // what the 4-byte marker becomes
}

var unboundedUTF8Rows = []unboundedUTF8Row{
	{"optional_element_overlong", "first", []byte{0xC0, 0xAF, 'Q', 'Q'}},
	{"optional_element_zero", "first", []byte{'Q', 0x00, 'Q', 'Q'}},
	{"optional_element_surrogate", "first", []byte{0xED, 0xA0, 0x80, 'Q'}},
	{"element_overlong", "rules", []byte{0xC0, 0xAF, 'Q', 'Q'}},
	{"key_truncated", "byname", []byte{'Q', 'Q', 'Q', 0xE2}},
}

// unboundedUTF8Answers runs the probe over every row's mutant and returns
// Luau's malformed answer per row, beside the engine's.
func unboundedUTF8Answers(t *testing.T, patch func(string) string) (luau, engine map[string]string) {
	t.Helper()
	u := luauUnboundedTextUnit(t)
	m := tabletext.NewModel(u)
	g := luauUnboundedTextGenerated(t, u)
	if patch != nil {
		g.files["schema_load.luau"] = []byte(patch(string(g.files["schema_load.luau"])))
	}
	const marker = "QQQQ"
	inputs := map[string][]byte{}
	engine = map[string]string{}
	var names []string
	for _, r := range unboundedUTF8Rows {
		wire := luauUnboundedTextWire(t, m, "Where", func(inst *tabletext.Instance) {
			if r.field == "byname" {
				luauUnboundedTextMap(t, m, inst, "byname", []string{marker}, []int32{1})
				return
			}
			luauUnboundedTextList(t, m, inst, r.field, []*string{&[]string{marker}[0]})
		})
		if bytes.Count(wire, []byte(marker)) != 1 {
			t.Fatalf("%s: the marker is not in the engine's wire exactly once", r.name)
		}
		wire = bytes.Replace(wire, []byte(marker), r.bad, 1)
		inputs[r.name] = wire
		names = append(names, `"`+r.name+`"`)
		var rep tabletext.Report
		inst := m.New(m.Lookup("Where"))
		ok, err := tablewire.Decode(m, inst, wire, &rep)
		if tablewire.Refused(err) {
			t.Fatalf("%s: the engine refused the root", r.name)
		}
		engine[r.name] = r.name + ":" + map[bool]string{true: "true", false: "false"}[rep.Malformed || !ok]
	}
	var mods strings.Builder
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods.WriteString("require(\"./" + strings.TrimSuffix(name, ".luau") + "\")\n")
		}
	}
	probe := strings.Replace(unboundedUTF8Probe, "NAMES", "{ "+strings.Join(names, ", ")+" }", 1)
	probe = strings.Replace(probe, "MODS", mods.String(), 1)
	out := runProbe(t, g, probe, inputs)
	luau = map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if i := strings.IndexByte(line, ':'); i > 0 {
			luau[line[:i]] = line
		}
	}
	return luau, engine
}

func TestLuauUnboundedTextIsValidatedAsText(t *testing.T) {
	t.Parallel()
	got, want := unboundedUTF8Answers(t, nil)
	for _, r := range unboundedUTF8Rows {
		if want[r.name] != r.name+":true" {
			t.Errorf("%s: the engine does not count the ill-formed payload malformed (%s), so the row proves nothing", r.name, want[r.name])
		}
		if got[r.name] != want[r.name] {
			t.Errorf("%s: Luau says %q, the engine says %q", r.name, got[r.name], want[r.name])
		}
	}
}

// TestLuauUnboundedTextUTF8NegativeControl removes the field path's check from
// the emitted load and requires exactly the optional-element rows to turn: the
// list element and key paths keep their own checks.
func TestLuauUnboundedTextUTF8NegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "\t\tif not utf8_ok(r.b, r.at, n) then\n\t\t\t-- ILL-FORMED TEXT IS DAMAGE at any bound"
	got, want := unboundedUTF8Answers(t, func(s string) string {
		if strings.Count(s, anchor) != 1 {
			t.Fatalf("the unbounded text check is not in the emitted load exactly once")
		}
		return strings.Replace(s, anchor, "\t\tif false then\n\t\t\t-- ILL-FORMED TEXT IS DAMAGE at any bound", 1)
	})
	var red []string
	for _, r := range unboundedUTF8Rows {
		if got[r.name] != want[r.name] {
			red = append(red, r.name)
		}
	}
	wantRed := "optional_element_overlong optional_element_zero optional_element_surrogate"
	if strings.Join(red, " ") != wantRed {
		t.Fatalf("with the check removed the rows that turned are %v, want exactly [%s]", red, wantRed)
	}
	t.Logf("with the check removed exactly %v turn", red)
}
