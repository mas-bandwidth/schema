package pythontable

// THE EVOLUTION CONSTRUCTS on the Python table leg (ROADMAP.md, Variable
// tables: "string, bytes and flags defaults, and renaming a table" and
// "renaming variants, arms and type fields"; docs/SPEC-TABLES.md §3, §5).
//
// Every case is a Go-engine construct: the engine's own form-1 wire of a JSON
// text, read by the generated Python package and written back. A value at a
// declared string, bytes or flags default is elided as a scalar is, so the
// empty wire is the canonical wire of an all-default table; a value off its
// default rides and must come back byte for byte. A table, a table field and
// an enum variant renamed under `was` keep the old name's hash, so an old
// writer's bytes read in silence and save back unchanged.
//
// The closure is union-free (a union arm's identity is the unions card's), and
// the kinds are this card's own (string, bytes, flags, an enum's variant) and
// the base's (bool, int, by-value tables, a single pointer).

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// vtEvolutionCase is one Go-engine construct: schema (the NEW side, the unit
// the Python package is generated from), oldSchema (the writer's unit, empty
// when it is the same declaration), root and the JSON text the engine writes.
// check is the probe lines that hold the loaded value.
type vtEvolutionCase struct {
	name      string
	oldSchema string
	schema    string
	root      string
	json      string
	check     string
}

const vtDefaultsSchema = `package probe

flags Caps { Jump, Crouch, Fly }

table D
{
    s    string(8) = "hi"
    b    bytes(4)  = "ab"
    caps Caps      = { Jump, Fly }
    n    int32     = 5 | min = 0, max = 9
}
`

const vtRenameOld = `package probe

flags Caps { Jump, Crouch, Fly }

table Vessel
{
    name string(32) = "untitled"
    tag  bytes(4)  = "ab"
    caps Caps      = { Jump, Fly }
    hull int32     = 100 | min = 0, max = 1000
}

table Fleet
{
    title    string(16) = "fleet"
    flagship *Vessel
    home     Vessel
}
`

const vtRenameNew = `package probe

flags Caps { Jump, Crouch, Fly }

table Ship | was = "Vessel"
{
    name string(32) = "untitled"
    tag  bytes(4)  = "ab"
    caps Caps      = { Jump, Fly }
    hull int32     = 100 | min = 0, max = 1000
}

table Fleet
{
    title    string(16) = "fleet"
    flagship *Ship
    home     Ship
}
`

const vtRenameFieldOld = `package probe

table Record
{
    past int32 | min = 0, max = 100
}
`

const vtRenameFieldNew = `package probe

table Record
{
    moved int32 | min = 0, max = 100, was = "past"
}
`

const vtRenameVariantOld = `package probe

enum Grade { Bronze, Silver, Gold }

table Cfg
{
    grade Grade = Bronze
    tally int32 = 3 | min = 0, max = 9
}
`

const vtRenameVariantNew = `package probe

enum Grade
{
    Bronze
    Argent | was = "Silver"
    Gold
}

table Cfg
{
    grade Grade = Bronze
    tally int32 = 3 | min = 0, max = 9
}
`

var vtEvolutionCases = []vtEvolutionCase{
	{"defaults_at", "", vtDefaultsSchema, "D", `{"s":"hi","b":"YWI=","caps":["Jump","Fly"],"n":5}`,
		`print("defaults", value.s == "hi", value.b == b"ab", value.caps == 5, value.n == 5)`},
	{"defaults_off", "", vtDefaultsSchema, "D", `{"s":"","b":"","caps":[],"n":0}`,
		`print("off", value.s == "", value.b == b"", value.caps == 0, value.n == 0)`},
	{"rename_table", vtRenameOld, vtRenameNew, "Fleet",
		`{"title":"armada","flagship":{"name":"Aurora","tag":"enpx","caps":["Crouch"],"hull":250},"home":{}}`,
		`print("renamed", value.title, value.flagship.name, value.home.name, value.flagship.caps, value.home.caps)`},
	{"rename_field", vtRenameFieldOld, vtRenameFieldNew, "Record", `{"past":7}`,
		`print("field", value.moved)`},
	{"rename_variant", vtRenameVariantOld, vtRenameVariantNew, "Cfg", `{"grade":"Silver","tally":7}`,
		`print("variant", value.grade, Cfg().grade, value.tally)`},
}

// vtEngineWire is the Go engine's form-1 wire of the JSON text at root.
func vtEngineWire(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	inst := m.New(st)
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || rep.Refused || rep.Unknown > 0 {
		t.Fatalf("the Go engine does not read the JSON cleanly: %+v", rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine does not write it: %v", err)
	}
	return wire
}

// vtEvolutionProbe is the program the pinned interpreters run: load the wire,
// save it back, print the verdict and then the case's own value checks.
func vtEvolutionProbe(root, check string) string {
	return pyrun.ImportPrelude + `from probe import ` + root + `

with open("wire", "rb") as fh:
    data = fh.read()
try:
    value = ` + root + `.load(data)
except Exception as err:
    print("load_refused", type(err).__name__, getattr(err, "reason", ""))
    raise SystemExit(0)
else:
    out = value.save()
    print("roundtrip", len(out), out == data, value.measure() == len(out))
` + check + `
`
}

// vtEvolutionRun writes the generator's tree (edited by mutate when set),
// writes the engine's wire and answers the probe's stdout.
func vtEvolutionRun(t *testing.T, c vtEvolutionCase, mutate func(*testing.T, map[string][]byte)) string {
	t.Helper()
	writerSchema := c.schema
	if c.oldSchema != "" {
		writerSchema = c.oldSchema
	}
	writer := loadUnit(t, writerSchema)
	wire := vtEngineWire(t, writer, c.root, c.json)

	u := loadUnit(t, c.schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("%s: the Python backend refuses it: %v", c.name, err)
	}
	files = maps.Clone(files)
	if mutate != nil {
		mutate(t, files)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := vtEvolutionProbe(c.root, c.check)
	return string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire}))
}

// vtEvolutionFailures is every line of a case's probe that did not hold.
func vtEvolutionFailures(t *testing.T, c vtEvolutionCase, mutate func(*testing.T, map[string][]byte)) []string {
	t.Helper()
	got := vtEvolutionRun(t, c, mutate)
	var fails []string
	wantWire := fmt.Sprintf("roundtrip %d True True", len(vtEvolutionWire(t, c)))
	if !strings.Contains(got, wantWire+"\n") {
		fails = append(fails, fmt.Sprintf("%s: the save is not the wire, want %q:\n%s", c.name, wantWire, got))
	}
	for _, line := range vtEvolutionWant[c.name] {
		if !strings.Contains(got, line+"\n") {
			fails = append(fails, fmt.Sprintf("%s: the probe lacks %q:\n%s", c.name, line, got))
		}
	}
	return fails
}

// vtEvolutionWire is the engine wire the case is held to.
func vtEvolutionWire(t *testing.T, c vtEvolutionCase) []byte {
	t.Helper()
	writerSchema := c.schema
	if c.oldSchema != "" {
		writerSchema = c.oldSchema
	}
	writer := loadUnit(t, writerSchema)
	return vtEngineWire(t, writer, c.root, c.json)
}

var vtEvolutionWant = map[string][]string{
	"defaults_at":    {"defaults True True True True"},
	"defaults_off":   {"off True True True True"},
	"rename_table":   {"renamed armada Aurora untitled 2 5"},
	"rename_field":   {"field 7"},
	"rename_variant": {"variant 2 1 7"},
}

// Functional (the pinned interpreters and analyzer): run with SCHEMA_SLOW=1.
func TestPythonVtEvolution(t *testing.T) {
	t.Parallel()
	for _, c := range vtEvolutionCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, f := range vtEvolutionFailures(t, c, nil) {
				t.Error(f)
			}
			u := loadUnit(t, c.schema)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			if out, err := pyrun.Analyze(t, dir, u.Package); err != nil {
				t.Fatalf("%s: mypy --strict over the generated package:\n%s", c.name, out)
			}
			if out, err := pyrun.Analyze(t, dir, u.Package+"/_schema"); err != nil {
				t.Fatalf("%s: mypy --strict over the runtime:\n%s", c.name, out)
			}
		})
	}
}

// TestPythonVtEvolutionNegativeControl plants a defect in the runtime's string
// write, in a copy of the generated package, and requires the rename_table
// construct (whose nodes carry strings off their defaults) to go red: a gate is
// only watching the codec if a broken codec fails it.
//
// Functional (the pinned interpreters): run with SCHEMA_SLOW=1.
func TestPythonVtEvolutionNegativeControl(t *testing.T) {
	t.Parallel()
	const path = "probe/_schema/kind_string.py"
	mutate := func(t *testing.T, files map[string][]byte) {
		text := string(files[path])
		const anchor = "w.raw(encoded)"
		if strings.Count(text, anchor) != 1 {
			t.Fatalf("the negative control's anchor %q rides %d times in %s, want 1", anchor, strings.Count(text, anchor), path)
		}
		files[path] = []byte(strings.Replace(text, anchor, "w.raw(encoded[:-1])", 1))
	}
	var c vtEvolutionCase
	for _, candidate := range vtEvolutionCases {
		if candidate.name == "rename_table" {
			c = candidate
		}
	}
	if c.name == "" {
		t.Fatal("the rename_table construct is absent")
	}
	fails := vtEvolutionFailures(t, c, mutate)
	if len(fails) == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: a truncated string write left the construct green")
	}
	if !strings.Contains(fails[0], "rename_table") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the construct went red, but not by name:\n%s", fails[0])
	}
}
