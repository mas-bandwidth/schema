package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// THE COOK HOLDS THE WRITE CONTRACT (docs/SPEC.md §5.1 rows 3, 4, 5 and 6;
// docs/SPEC-TABLES.md §7.6 and §2.15): Lobster's table writers refuse in every
// build, so <T>CookMeasure answers -1 and <T>Cook answers "" for a value its
// declaration does not admit, at the root and at a node a pointer reaches,
// rather than cutting a string(N) to N bytes, writing a count past its bound
// into the next field's bytes or writing a tag with no arm. The variable save
// refuses a deprecated enum variant (scalar and array element) as
// invalid_value, as the fixed, message and JSON writers do. Every refused case
// has a lawful twin that is written.

const cookContractSchema = `package wcprobe

enum Grade
{
    Bronze
    Silver | deprecated
    Gold
}

table Buff { v int32 }

union Effect
{
    buff  Buff
    level int32
}

table Inner { s string(4) }

table Probe
{
    s    string(4)
    y    bytes(4)
    n    [..2]int8
    g    Grade
    gs   [..2]Grade
    e    Effect
    next *Inner
}
`

// cookContractCases set fields of the root `rec`; `want` is the line the
// program prints: the measure, the cook and the variable save's reason.
var cookContractCases = []struct{ name, set, want string }{
	{"clean", ``, "measure ok cook ok save ok"},
	{"string_long", `slot(rec, "s").cell.s = "abcdef"`, "measure -1 cook refused save invalid_value"},
	{"string_ok", `slot(rec, "s").cell.s = "abcd"`, "measure ok cook ok save ok"},
	{"string_long_multibyte", `slot(rec, "s").cell.s = "ab\xc3\xa9\xc3\xa9"`, "measure -1 cook refused save invalid_value"},
	{"bytes_long", `slot(rec, "y").cell.s = "\x01\x02\x03\x04\x05"`, "measure -1 cook refused save invalid_value"},
	{"bytes_ok", `slot(rec, "y").cell.s = "\x01\x02\x03\x04"`, "measure ok cook ok save ok"},
	{"count_past", "let n = slot(rec, \"n\")\n    while length(n.elems) < 3: push(n.elems, schema_rt.Value { i: 1 })\n    n.count = 3", "measure -1 cook refused save invalid_value"},
	{"count_ok", "let n = slot(rec, \"n\")\n    while length(n.elems) < 2: push(n.elems, schema_rt.Value { i: 1 })\n    n.count = 2", "measure ok cook ok save ok"},
	{"tag_past", `slot(rec, "e").cell.i = 3`, "measure -1 cook refused save invalid_value"},
	{"tag_ok", `wc_level(rec, 5)`, "measure ok cook ok save ok"},
	{"enum_deprecated", `slot(rec, "g").cell.i = 2`, "measure -1 cook refused save invalid_value"},
	{"enum_live_low", `slot(rec, "g").cell.i = 1`, "measure ok cook ok save ok"},
	{"enum_live_high", `slot(rec, "g").cell.i = 3`, "measure ok cook ok save ok"},
	{"element_deprecated", `wc_grades(rec, 2)`, "measure -1 cook refused save invalid_value"},
	{"element_live", `wc_grades(rec, 3)`, "measure ok cook ok save ok"},
	{"pointed_string_long", `wc_next(rec, "abcdef")`, "measure -1 cook refused save invalid_value"},
	{"pointed_string_ok", `wc_next(rec, "abcd")`, "measure ok cook ok save ok"},
}

const cookContractHelpers = `def slot(rec:schema_rt.Record, name:string) -> schema_rt.Slot:
    return schema_rt.slot_named(rec, name)
def wc_level(rec:schema_rt.Record, v:int):
    let un = schema_rt.lookup_union("Effect")
    assert un
    let arm = schema_rt.new_slot(un.arms[1])
    arm.cell.i = v
    let sl = slot(rec, "e")
    sl.cell.i = 2
    sl.cell.arm = arm
def wc_grades(rec:schema_rt.Record, v:int):
    let sl = slot(rec, "gs")
    while length(sl.elems) < 2: push(sl.elems, schema_rt.Value { i: 1 })
    sl.elems[1].i = v
    sl.count = 2
def wc_next(rec:schema_rt.Record, s:string):
    let inner = schema_rt.new_record(schema_rt.table_of("Inner"))
    slot(inner, "s").cell.s = s
    slot(rec, "next").cell.node = inner
def report(name:string, rec:schema_rt.Record):
    let m = wcprobe.ProbeCookMeasure(rec)
    let c = wcprobe.ProbeCook(rec, false)
    let out, why = schema_rt.save_reasoned(rec)
    print(name + " measure " + (if m < 0: "-1" else: "ok") + " cook " + (if length(c) == 0: "refused" else: "ok") + " save " + (if why == "" and length(out) > 0: "ok" else: why))
`

func cookContractProgram(g *loadUnit) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .schema_save\n")
	b.WriteString(cookContractHelpers)
	for _, c := range cookContractCases {
		fmt.Fprintf(&b, "def case_%s():\n    let rec = schema_rt.new_record(schema_rt.table_of(\"Probe\"))\n", c.name)
		if c.set != "" {
			fmt.Fprintf(&b, "    %s\n", c.set)
		}
		fmt.Fprintf(&b, "    report(%q, rec)\ncase_%s()\n", c.name, c.name)
	}
	return b.String()
}

// TestLobsterCookHoldsTheWriteContract runs every case in one process: each
// off-contract value makes the measure -1, the cook "" and the save
// invalid_value, and each lawful twin is measured, cooked and saved.
func TestLobsterCookHoldsTheWriteContract(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(cookContractSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the surface refuses the cook-contract probe: %v", err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(cookContractProgram(g)), 0o644); err != nil {
		t.Fatal(err)
	}
	// RunExit: a case the writer does not hold may stop the VM; the lines
	// printed before it still name every case that ran
	stdout, code := lobsterrun.RunExit(t, g.dir, "main.lobster", nil)
	got := strings.Split(strings.TrimRight(string(stdout), "\n"), "\n")
	var bad []string
	for i, c := range cookContractCases {
		want := c.name + " " + c.want
		line := "(no line: the program stopped)"
		if i < len(got) {
			line = got[i]
		}
		if line != want {
			bad = append(bad, fmt.Sprintf("%s: got %q, want %q", c.name, line, want))
		}
	}
	if code != 0 {
		bad = append(bad, fmt.Sprintf("the program exited %d", code))
	}
	if len(bad) > 0 {
		t.Fatalf("the cook and the variable save do not hold the write contract:\n%s", strings.Join(bad, "\n"))
	}
}
