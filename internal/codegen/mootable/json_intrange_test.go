package mootable_test

// THE DECLARED RANGE IS COMPARED EXACTLY (docs/SPEC-TABLES.md §16.2, the
// "integers" row): a JSON integer token's value is compared with the field's
// declared min and max as the integers they are, so a value one past a bound
// refuses at every magnitude and no wide integer passes through a double. The
// Go engine (internal/tabletext, compareInteger) is the oracle: every text is
// read by it and by moo, and both must answer the same verdict, with the same
// value where they read one.
//
// The first two fields carry bounds at 2^53, where a rounded comparison merges
// adjacent integers; the same texts at small bounds are the control that the
// rule holds where no rounding could hide it.

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const intRangeSchema = `package probe

table Big
{
    x uint64 | min = 0, max = 9007199254740992
    w int64 | min = -9007199254740992, max = 9007199254740992
}

table Small
{
    x uint64 | min = 0, max = 7
    w int64 | min = -7, max = 7
}
`

type intRangeCase struct{ name, root, text string }

var intRangeCases = []intRangeCase{
	{"big_x_at_max", "Big", `{"x":9007199254740992}`},
	{"big_x_past_max", "Big", `{"x":9007199254740993}`},
	{"big_x_at_min", "Big", `{"x":0}`},
	{"big_x_past_min", "Big", `{"x":-1}`},
	{"big_w_at_max", "Big", `{"w":9007199254740992}`},
	{"big_w_past_max", "Big", `{"w":9007199254740993}`},
	{"big_w_at_min", "Big", `{"w":-9007199254740992}`},
	{"big_w_past_min", "Big", `{"w":-9007199254740993}`},
	{"small_x_at_max", "Small", `{"x":7}`},
	{"small_x_past_max", "Small", `{"x":8}`},
	{"small_x_at_min", "Small", `{"x":0}`},
	{"small_x_past_min", "Small", `{"x":-1}`},
	{"small_w_at_max", "Small", `{"w":7}`},
	{"small_w_past_max", "Small", `{"w":8}`},
	{"small_w_at_min", "Small", `{"w":-7}`},
	{"small_w_past_min", "Small", `{"w":-8}`},
}

// intRangeAnswer is the engine's answer for one case: the verdict, and for a
// text it reads the two fields' exact values.
func intRangeAnswer(t *testing.T, u *ir.Unit, c intRangeCase) string {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(c.root))
	var rep tabletext.Report
	if !m.Read(inst, []byte(c.text), &rep) {
		return c.name + " refused"
	}
	x, w := "0", "0"
	for i := range inst.Fields {
		switch inst.Fields[i].Def.Name {
		case "x":
			x = fmt.Sprint(inst.Fields[i].Cell.U)
		case "w":
			w = fmt.Sprint(inst.Fields[i].Cell.I)
		}
	}
	return c.name + " read " + x + " " + w
}

// intRangeRun reads every text with moo and with the Go engine, returning the
// cases whose answers differ. sabotage, when set, edits the emitted json.moo
// first, which is how the negative control restores the rounded comparison.
func intRangeRun(t *testing.T, sabotage func(string) string) []string {
	t.Helper()
	g := generateFiles(t, map[string]string{"Probe.schema": intRangeSchema})
	if sabotage != nil {
		files := maps.Clone(g.files)
		src := string(files["json.moo"])
		broken := sabotage(src)
		if broken == src {
			t.Fatal("the control's site moved: json.moo no longer holds the exact range comparison")
		}
		files["json.moo"] = []byte(broken)
		g = writeUnit(t, g.unit, files)
	}
	inputs := map[string][]byte{}
	var main strings.Builder
	main.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal P = require(\"./ProbeTable\")\n")
	main.WriteString(`local function probe(root: string, input: string)
	local v = P["New" .. root]()
	local report: any = {}
	local ok = P[root .. "FromJson"](v, inputs[input], report)
	if not ok then
		print(input .. " refused")
		return
	end
	print(input .. " read " .. tostring(v.X) .. " " .. tostring(v.W))
end
`)
	for _, c := range intRangeCases {
		inputs[c.name] = []byte(c.text)
		fmt.Fprintf(&main, "probe(%q, %q)\n", c.root, c.name)
	}
	out := g.run(t, main.String(), inputs)
	lines := strings.Split(out, "\n")
	if len(lines) != len(intRangeCases) {
		t.Fatalf("the probe printed %d lines for %d cases:\n%s", len(lines), len(intRangeCases), out)
	}
	var bad []string
	for i, c := range intRangeCases {
		want := intRangeAnswer(t, g.unit, c)
		if lines[i] != want {
			bad = append(bad, fmt.Sprintf("%s: moo %s, the Go engine %s", c.name, lines[i], want))
		}
	}
	return bad
}

// intRangeExactCompare is the exact range test in json.moo's placeInteger.
const intRangeExactCompare = `		if cmpSigned(negative, mag, ln, lm) < 0 then
			bn, bm = ln and #lm > 0, lm
			r.report.outOfRange += 1
		elseif cmpSigned(negative, mag, hn, hm) > 0 then
			bn, bm = hn and #hm > 0, hm
			r.report.outOfRange += 1
		end
`

// intRangeRoundedCompare compares the same values after rounding them to the
// nearest double, the comparison the negative control plants back in.
const intRangeRoundedCompare = `		local function dblRound(a: Big): Big
			if bnBitLen(a) <= 53 then
				return a
			end
			local qh, ql, e = roundBinary(a, { 1 }, 0, 53, -1074)
			local q = bnShl(bnSmall(qh), 24)
			bnAddSmall(q, ql)
			return bnShl(q, e)
		end
		local scale = dblRound(mag)
		local dlo, dhi = dblRound(lm), dblRound(hm)
		if cmpSigned(negative, scale, ln, dlo) < 0 then
			bn, bm = ln and #dlo > 0, dlo
			r.report.outOfRange += 1
		elseif cmpSigned(negative, scale, hn, dhi) > 0 then
			bn, bm = hn and #dhi > 0, dhi
			r.report.outOfRange += 1
		end
`

func intRangeRounded(src string) string {
	if strings.Count(src, intRangeExactCompare) != 1 {
		return src
	}
	return strings.Replace(src, intRangeExactCompare, intRangeRoundedCompare, 1)
}

// TestMooJsonIntegerRangeIsExact: at every bound the value reads exactly, and
// one past it refuses, at 2^53 and at small bounds alike, as the Go engine
// answers.
func TestMooJsonIntegerRangeIsExact(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated text form)")
	t.Parallel()
	for _, b := range intRangeRun(t, nil) {
		t.Error(b)
	}
}

// TestMooJsonIntegerRangeIsExactNegativeControl restores the rounded
// comparison and requires the one-past rows at 2^53 to go red, which is what
// says the gate can see a rounded range test.
func TestMooJsonIntegerRangeIsExactNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a sabotaged json.moo)")
	t.Parallel()
	bad := intRangeRun(t, intRangeRounded)
	red := map[string]bool{}
	for _, b := range bad {
		name, _, _ := strings.Cut(b, ":")
		red[name] = true
	}
	for _, name := range []string{"big_x_past_max", "big_w_past_max", "big_w_past_min"} {
		if !red[name] {
			t.Errorf("with the rounded comparison restored, %s stayed green: the test cannot see the rounding", name)
		}
	}
}
