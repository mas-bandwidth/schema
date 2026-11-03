package mootable_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The text-form half of the constructs compiler/carries_moo.go registers
// (docs/SPEC-TABLES.md §2.3 required, §2.13.1 unbounded text, §16.10 the
// array root), each against the Go reference engine's own reading and writing
// of the same text. The wire half is construct_test.go's.

// jsonProbe reads each named input through <root>FromJson and prints one line
// per input: the verdict and report, then the text <root>ToJson writes back,
// in hex.
func jsonProbe(g *generatedUnit, root string, names []string) string {
	var b strings.Builder
	b.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal P = require(\"./ProbeTable\")\n")
	for _, n := range names {
		fmt.Fprintf(&b, `do
	local v = P.New%[1]s()
	local r: any = {}
	local ok = P.%[1]sFromJson(v, inputs.%[2]s, r)
	local text = "-"
	if ok then
		local size = P.%[1]sToJsonMeasure(v)
		if size >= 0 then
			local out = buffer.create(size)
			P.%[1]sToJson(v, out)
			local parts = table.create(size)
			for i = 0, size - 1 do
				parts[i + 1] = string.format("%%02x", buffer.readu8(out, i))
			end
			text = table.concat(parts)
		end
	end
	print(string.format("%%s %%s %%d %%s %%s", if ok then "read" else "refused", tostring(r.malformed), r.missingRequired, tostring(r.reason), text))
end
`, root, n)
	}
	return b.String()
}

// engineText is the Go engine's verdict and canonical text for one input, in
// jsonProbe's line format.
func engineText(t *testing.T, src, root, text string) string {
	t.Helper()
	u := unitFromSource(t, src)
	m := tabletext.NewModel(u)
	inst := m.New(u.Tables[root])
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) {
		return fmt.Sprintf("refused %v %d %s -", rep.Malformed, rep.MissingRequired, reasonOrNil(rep.Reason))
	}
	out, err := m.Write(inst)
	if err != nil {
		t.Fatalf("the Go engine does not write what it read: %v", err)
	}
	// ToJson's text ends in exactly one newline (§16.1); the engine's Write
	// leaves it to its caller
	return fmt.Sprintf("read %v %d %s %x", rep.Malformed, rep.MissingRequired, reasonOrNil(rep.Reason), append(out, '\n'))
}

func reasonOrNil(r string) string {
	if r == "" {
		return "nil"
	}
	return r
}

type textCase struct {
	name, text string
}

// runTextCases runs every case through moo and the Go engine and requires the
// two to answer alike, line for line; wantRefused names the cases the engine
// itself must refuse, so the comparison cannot pass by both accepting.
func runTextCases(t *testing.T, src, root string, cases []textCase, wantRefused map[string]bool, plant func(map[string][]byte)) []string {
	t.Helper()
	u := unitFromSource(t, src)
	files, err := generate(u)
	if err != nil {
		t.Fatalf("moo refuses the unit: %v", err)
	}
	if plant != nil {
		plant(files)
	}
	g := writeUnit(t, u, files)
	inputs := map[string][]byte{}
	var names []string
	for _, c := range cases {
		inputs[c.name] = []byte(c.text)
		names = append(names, c.name)
	}
	got := strings.Split(g.run(t, jsonProbe(g, root, names), inputs), "\n")
	if len(got) != len(cases) {
		t.Fatalf("the probe printed %d lines for %d cases:\n%s", len(got), len(cases), strings.Join(got, "\n"))
	}
	var diffs []string
	for i, c := range cases {
		want := engineText(t, src, root, c.text)
		if wantRefused[c.name] != strings.HasPrefix(want, "refused") {
			t.Fatalf("%s: the Go engine answers %s", c.name, want)
		}
		if got[i] != want {
			diffs = append(diffs, fmt.Sprintf("%s:\nmoo: %s\ngo:  %s", c.name, got[i], want))
		}
	}
	return diffs
}

const requiredText = `package probe
table Inner
{
    k int32 | required
}
table Player
{
    name string(8) | required
    n int32 | required
    score int32
    inner []Inner
    next *Inner
}
`

var requiredCases = []textCase{
	{"whole", `{"name":"","n":0}`},
	{"set", `{"name":"ann","n":-3,"score":9,"inner":[{"k":0}],"next":{"k":1}}`},
	{"lacks_n", `{"name":"ann","score":9}`},
	{"lacks_both", `{}`},
	{"element_lacks_k", `{"name":"a","n":1,"inner":[{"k":1},{}]}`},
	{"pointee_empty", `{"name":"a","n":1,"next":{}}`},
	{"kind_mismatch_is_named", `{"name":"a","n":"one"}`},
}

var requiredRefused = map[string]bool{"lacks_n": true, "lacks_both": true, "element_lacks_k": true, "pointee_empty": true}

// TestMooJsonRequired: a JSON object that leaves out a required key refuses
// the read by name, missing_required, never as damage, at any depth (§2.3);
// a key named with a mismatched kind is still named.
func TestMooJsonRequired(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted text form)")
	t.Parallel()
	for _, d := range runTextCases(t, requiredText, "Player", requiredCases, requiredRefused, nil) {
		t.Error(d)
	}
}

// TestMooJsonRequiredNegativeControl turns the object's required check off in
// the emitted json.moo and requires the comparison to go red.
func TestMooJsonRequiredNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted text form)")
	t.Parallel()
	const anchor = "if fd.required == true and not seen[i] and guardHolds(fd, rec) then"
	diffs := runTextCases(t, requiredText, "Player", requiredCases, requiredRefused, func(files map[string][]byte) {
		src := string(files["json.moo"])
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("the required-check anchor is not in json.moo exactly once")
		}
		files["json.moo"] = []byte(strings.Replace(src, anchor, "if false then", 1))
	})
	if len(diffs) == 0 {
		t.Fatal("json.moo with no required check still answers as the Go engine does")
	}
}

// TestMooWireRequiredRefusesAbsence: a wire written before a field was
// required (the field elided at zero) refuses the strict read by name; the
// same wire with the field written is read. The wires are the Go engine's.
func TestMooWireRequiredRefusesAbsence(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	t.Parallel()
	const required = "package probe\ntable Player\n{\n    name string(8)\n    n int32 | required\n}\n"
	older := unitFromSource(t, strings.Replace(required, " | required", "", 1))
	lacks := goOracleWire(t, older, "Player", `{"name":"a","n":0}`)
	holds := goOracleWire(t, older, "Player", `{"name":"a","n":4}`)
	u := unitFromSource(t, required)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	if out := g.run(t, strictRoundTrip(g, "Player"), map[string][]byte{"wire": lacks}); out != "REFUSED missing_required: n n" {
		t.Errorf("the wire lacking n: %s", out)
	}
	if out := g.run(t, strictRoundTrip(g, "Player"), map[string][]byte{"wire": holds}); out != fmt.Sprintf("ok %d %x", len(holds), holds) {
		t.Errorf("the control wire holding n: %s", out)
	}
	// the engine refuses the absence alike
	m := tabletext.NewModel(u)
	var rep tabletext.Report
	if ok, _ := tablewire.Decode(m, m.New(u.Tables["Player"]), lacks, &rep); ok || rep.MissingRequired != 1 {
		t.Errorf("the Go engine reads the wire lacking n: %+v", rep)
	}
}

const unboundedText = `package probe
table Where
{
    receipts []string
    running []?string
    recent [..2]string
    rows map[string]int32
}
`

var unboundedCases = []textCase{
	{"long", `{"receipts":["` + strings.Repeat("receipt line ", 40) + `",""],"running":[null,"` + strings.Repeat("r", 300) + `"],"recent":["` + strings.Repeat("x", 100) + `"],"rows":{"` + strings.Repeat("k", 70) + `":1,"b":2}}`},
	{"count_clamps", `{"recent":["a","b","c"]}`},
	{"zero_refuses", `{"receipts":["a\u0000b"]}`},
}

// TestMooJsonUnboundedText: an unbounded element or key is never clamped for
// its length; a [..N] list's count still clamps; a zero refuses (§2.13.1).
func TestMooJsonUnboundedText(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted text form)")
	t.Parallel()
	for _, d := range runTextCases(t, unboundedText, "Where", unboundedCases, map[string]bool{"zero_refuses": true}, nil) {
		t.Error(d)
	}
}

// TestMooJsonUnboundedTextNegativeControl gives the unbounded element a
// capacity of eight bytes in the emitted module and requires the comparison
// to go red.
func TestMooJsonUnboundedTextNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted text form)")
	t.Parallel()
	const anchor = "{ tl = { opt = false, bound = -1, rec = \"WhereReceiptsText\" } }"
	diffs := runTextCases(t, unboundedText, "Where", unboundedCases, map[string]bool{"zero_refuses": true}, func(files map[string][]byte) {
		src := string(files["ProbeTable.moo"])
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("the unbounded element anchor is not in ProbeTable.moo exactly once")
		}
		files["ProbeTable.moo"] = []byte(strings.Replace(src, anchor, strings.Replace(anchor, "-1", "8", 1), 1))
	})
	if len(diffs) == 0 {
		t.Fatal("an eight-byte capacity on the unbounded element still answers as the Go engine does")
	}
}

const arrayRootText = `package probe
table Goal
{
    x int32
}
table GoalList | json = "array"
{
    goals []Goal
}
`

var arrayRootCases = []textCase{
	{"array", `[{"x":1},{"x":-2},{}]`},
	{"empty", `[]`},
	{"object_refuses", `{"goals":[]}`},
}

// TestMooJsonArrayRoot: a json = "array" table's text is its one field's
// array, read and written; an object at its root refuses (§16.10).
func TestMooJsonArrayRoot(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted text form)")
	t.Parallel()
	for _, d := range runTextCases(t, arrayRootText, "GoalList", arrayRootCases, map[string]bool{"object_refuses": true}, nil) {
		t.Error(d)
	}
}

// TestMooJsonArrayRootNegativeControl drops the array-root mark from the
// emitted descriptor and requires the comparison to go red.
func TestMooJsonArrayRootNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted text form)")
	t.Parallel()
	const anchor = "jsonArray = true"
	diffs := runTextCases(t, arrayRootText, "GoalList", arrayRootCases, map[string]bool{"object_refuses": true}, func(files map[string][]byte) {
		src := string(files["ProbeTable.moo"])
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("the array-root anchor is not in ProbeTable.moo exactly once")
		}
		files["ProbeTable.moo"] = []byte(strings.Replace(src, anchor, "jsonArray = false", 1))
	})
	if len(diffs) == 0 {
		t.Fatal("a descriptor with no array-root mark still answers as the Go engine does")
	}
}

// TestMooFormsGateTheTextForm: a table whose forms declaration leaves json out
// gets no text-form entry points, and a unit where no table carries json
// emits no json.moo; the sibling that carries json is the control (§2.12).
func TestMooFormsGateTheTextForm(t *testing.T) {
	t.Parallel()
	const src = "package probe\ntable Pong | forms = \"wire\"\n{\n    x int32\n}\ntable Ping | forms = \"wire,json\"\n{\n    x int32\n}\n"
	files, err := generate(unitFromSource(t, src))
	if err != nil {
		t.Fatal(err)
	}
	mod := string(files["ProbeTable.moo"])
	if !strings.Contains(mod, "function M.PingFromJson(") || !strings.Contains(mod, "forms = { wire = true, json = true, }") {
		t.Fatal("the control: Ping, which carries json, has no text form")
	}
	if strings.Contains(mod, "PongFromJson") || strings.Contains(mod, "PongToJson") {
		t.Error("Pong, which leaves json out, has a text form")
	}
	only, err := generate(unitFromSource(t, strings.Replace(src, "\"wire,json\"", "\"wire\"", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := only["json.moo"]; ok {
		t.Error("a unit where no table carries json emits json.moo")
	}
}

// TestMooJsonRequiredRangeRefusesTheWrite: a required integer whose range
// excludes zero has no text while it holds zero, so a fresh record refuses
// ToJson as the Go engine's Write does; the same record set in range is the
// control (§2.3).
func TestMooJsonRequiredRangeRefusesTheWrite(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted text form)")
	t.Parallel()
	const src = "package probe\ntable R\n{\n    n int32 | min = 1, max = 9, required\n}\n"
	u := unitFromSource(t, src)
	m := tabletext.NewModel(u)
	if _, err := m.Write(m.New(u.Tables["R"])); err == nil {
		t.Fatal("the Go engine writes a fresh R whose required n is out of range")
	}
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	out := g.run(t, "--!strict\nlocal P = require(\"./ProbeTable\")\nlocal v = P.NewR()\nprint(P.RToJsonMeasure(v))\nv.N = 5\nprint(P.RToJsonMeasure(v) > 0)\n", nil)
	if out != "-1\ntrue" {
		t.Fatalf("moo's ToJson over R at 0 and at 5: %q", out)
	}
}
