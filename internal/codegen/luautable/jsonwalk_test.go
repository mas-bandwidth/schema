package luautable_test

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// M9, ONE TEXT WALK FOR THE WHOLE UNIT (docs/PORTING.md; docs/SPEC-TABLES.md
// §16): the text form is one generic walk over the descriptors, identical
// across every unit, never a codec per table. Luau carries the walk as a
// runtime module, schema_json.luau, emitted beside each unit's modules, so the
// gate the C++, C, Go and C# legs hold over their generated walk markers is,
// here: every manifest unit that carries the text form emits the walk byte
// for byte as the embedded runtime spells it (everything before `adapted`,
// the one entry a §2.12 forms declaration gates), and no generated table or
// view module carries a walk of its own.

// luauJSONWalkEnd is where the walk ends and the per-unit entry begins.
const luauJSONWalkEnd = "local function adapted("

// luauJSONWalkMarkers are definitions only the walk holds; a generated module
// that defines one has grown a codec of its own.
var luauJSONWalkMarkers = []string{"local function scan_string(", "local function read_keys(", "local function core_from("}

// luauJSONWalk is the walk region of one schema_json.luau.
func luauJSONWalk(src []byte) ([]byte, bool) {
	before, _, ok := bytes.Cut(src, []byte(luauJSONWalkEnd))
	if !ok {
		return nil, false
	}
	return before, true
}

// luauJSONWalkFailures holds the gate over every unit's generated files and
// returns one line per unit that breaks it, and how many units carried the
// walk.
func luauJSONWalkFailures(t *testing.T, units map[string]map[string][]byte) ([]string, int) {
	t.Helper()
	want, ok := luauJSONWalk(luautable.Runtime()["schema_json.luau"])
	if !ok {
		t.Fatalf("the embedded schema_json.luau has no %q", luauJSONWalkEnd)
	}
	for _, m := range luauJSONWalkMarkers {
		if !bytes.Contains(want, []byte(m)) {
			t.Fatalf("the embedded walk does not define %q", m)
		}
	}
	var bad []string
	carried := 0
	for _, unit := range slices.Sorted(maps.Keys(units)) {
		files := units[unit]
		if src, ok := files["schema_json.luau"]; ok {
			carried++
			got, ok := luauJSONWalk(src)
			if !ok || !bytes.Equal(got, want) {
				bad = append(bad, fmt.Sprintf("%s: schema_json.luau's walk is not the runtime's walk", unit))
			}
		}
		for _, name := range slices.Sorted(maps.Keys(files)) {
			if strings.HasPrefix(name, "schema_") || strings.HasPrefix(name, "serialize") {
				continue
			}
			for _, m := range luauJSONWalkMarkers {
				if bytes.Contains(files[name], []byte(m)) {
					bad = append(bad, fmt.Sprintf("%s: %s carries a walk of its own (%s)", unit, name, m))
				}
			}
		}
	}
	return bad, carried
}

// luauJSONWalkUnits generates every manifest unit the target accepts.
func luauJSONWalkUnits(t *testing.T) map[string]map[string][]byte {
	t.Helper()
	units, _ := readManifest(t)
	out := map[string]map[string][]byte{}
	for _, key := range slices.Sorted(maps.Keys(units)) {
		g := generateUnit(t, units, key)
		if g.err != nil {
			continue
		}
		out[key] = g.files
	}
	return out
}

// TestLuauJsonWalkIsOneRuntime is the gate.
func TestLuauJsonWalkIsOneRuntime(t *testing.T) {
	t.Parallel()
	units := luauJSONWalkUnits(t)
	bad, carried := luauJSONWalkFailures(t, units)
	for _, b := range bad {
		t.Error(b)
	}
	if carried < 2 {
		t.Fatalf("only %d units carry the text form; the gate compares nothing", carried)
	}
	t.Logf("one walk, byte-identical to the runtime, in %d of %d generated units", carried, len(units))
}

// TestLuauJsonWalkNegativeControl plants the two regressions the gate exists
// for in a copy of the generated output: one unit's walk edited, and a second
// unit's table module given a walk of its own. The gate must name exactly
// those two units.
func TestLuauJsonWalkNegativeControl(t *testing.T) {
	t.Parallel()
	units := luauJSONWalkUnits(t)
	var withJSON []string
	for _, key := range slices.Sorted(maps.Keys(units)) {
		if _, ok := units[key]["schema_json.luau"]; ok {
			withJSON = append(withJSON, key)
		}
	}
	if len(withJSON) < 2 {
		t.Fatalf("only %d units carry the text form", len(withJSON))
	}
	edited, grown := withJSON[0], withJSON[len(withJSON)-1]
	src := units[edited]["schema_json.luau"]
	units[edited] = maps.Clone(units[edited])
	units[edited]["schema_json.luau"] = bytes.Replace(src, []byte("local MAX_DEPTH = 128"), []byte("local MAX_DEPTH = 127"), 1)
	if bytes.Equal(units[edited]["schema_json.luau"], src) {
		t.Fatal("NEGATIVE CONTROL FAILED: the walk edit changed nothing")
	}
	units[grown] = maps.Clone(units[grown])
	var table string
	for _, name := range slices.Sorted(maps.Keys(units[grown])) {
		if strings.HasSuffix(name, "Table.luau") {
			table = name
			break
		}
	}
	if table == "" {
		t.Fatalf("NEGATIVE CONTROL FAILED: unit %s has no table module", grown)
	}
	units[grown][table] = append(slices.Clone(units[grown][table]), []byte("\nlocal function read_keys() end\n")...)
	bad, _ := luauJSONWalkFailures(t, units)
	want := []string{
		fmt.Sprintf("%s: schema_json.luau's walk is not the runtime's walk", edited),
		fmt.Sprintf("%s: %s carries a walk of its own (local function read_keys()", grown, table),
	}
	if !slices.Equal(bad, want) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate answered %q, want exactly %q", bad, want)
	}
	t.Logf("the gate named exactly the planted units: %q", bad)
}
