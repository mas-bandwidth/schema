// Squirrel carries a string, bytes or flags DEFAULT on the FORM-1 table wire
// (docs/SPEC-TABLES.md §4, §16.2; SPEC §4.2): the writer elides a field whose
// value is the declared default and the reader restores it on an absent field.
// The probe unit is the one the squirrel refusal test names for this construct.
package compiler

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestSquirrelCarriesTableWireDefaults generates the probe unit the squirrel
// refusal test names for a value default, runs the emitted module under the
// pinned interpreter, and holds Save and Load to the Go oracle byte for byte
// (docs/SPEC-TABLES.md §4, §16.2; SPEC §4.2). A field at its declared default
// is elided, and an absent field reads back at that default.
func TestSquirrelCarriesTableWireDefaults(t *testing.T) {
	t.Parallel()
	const src = "package probe\n\ntable L\n{\n    s string(8) = \"hi\"\n}\n"
	u := unitFromSource(t, src)
	files, err := New().Generate(u, "squirrel", Options{})
	if err != nil {
		t.Fatalf("squirrel refused the probe unit: %v", err)
	}

	cases := []struct{ name, json string }{
		{"default", `{"s":"hi"}`}, // the declared default, elided on the wire
		{"set", `{"s":"hello"}`},  // a value that is not the default
	}

	m := tabletext.NewModel(u)
	st := m.Lookup("L")
	if st == nil {
		t.Fatal("the probe unit declares no table L")
	}
	inputs := map[string][]byte{}
	want := map[string][]byte{}
	for _, c := range cases {
		inst := m.New(st)
		var report tabletext.Report
		if !m.Read(inst, []byte(c.json), &report) {
			t.Fatalf("%s: tabletext refused %s: %+v", c.name, c.json, report)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: tablewire encode: %v", c.name, err)
		}
		want[c.name] = wire
		inputs[c.name+".json"] = []byte(c.json)
	}

	probe := squirrelVerdict + `local inputs = dofile("inputs.nut", true);
local schema = dofile("probe/schema_module.nut", true);
local T = schema.require("ProbeTables");
local S = schema.require("schema_table_save");
local L = schema.require("schema_table_load");
local J = schema.require("schema_table_json");
foreach (key in ["default", "set"]) {
  local r = {};
  local v = J.FromJson(T, "L", inputs[key + ".json"], r);
  if (::verdict(v, r) != "accepted") { print(key + " json-refused\n"); continue; }
  local b = S.save(T, "L", v);
  hexline(b);
  local lr = {};
  local loaded = L.load(T, "L", b, lr);
  if (::verdict(loaded, lr) != "accepted") { print(key + " load-refused\n"); continue; }
  hexline(S.save(T, "L", loaded));
  print(key + " " + (v.S == loaded.S ? "same" : "different") + "\n");
}
`
	// check runs the probe over one set of emitted files and answers every
	// way it differs from the Go oracle.
	check := func(files map[string][]byte) []string {
		dir := t.TempDir()
		out := filepath.Join(dir, "probe")
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(out, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(probe), 0o600); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n")
		if len(lines) != len(cases)*3 {
			return []string{fmt.Sprintf("the probe printed %d lines for %d cases: %q", len(lines), len(cases)*3, lines)}
		}
		var bad []string
		for i, c := range cases {
			for phase := range 2 {
				got, err := hex.DecodeString(lines[3*i+phase])
				if err != nil {
					bad = append(bad, fmt.Sprintf("%s phase %d is not hex: %q", c.name, phase, lines[3*i+phase]))
					continue
				}
				if string(got) != string(want[c.name]) {
					bad = append(bad, fmt.Sprintf("%s phase %d differs from tablewire\n got %x\nwant %x", c.name, phase, got, want[c.name]))
				}
			}
			if verdict := lines[3*i+2]; verdict != c.name+" same" {
				bad = append(bad, fmt.Sprintf("%s: saved and loaded values differ: %q", c.name, verdict))
			}
		}
		return bad
	}
	for _, e := range check(files) {
		t.Error(e)
	}

	// NEGATIVE CONTROL: the emitted descriptor loses its declared default, so
	// the writer no longer elides "hi"; the probe must go red against the
	// oracle's elided wire.
	const defAnchor = `def = "hi"`
	module := string(files["ProbeTables.nut"])
	if n := strings.Count(module, defAnchor); n != 1 {
		t.Fatalf("the default's anchor %q is in the emitted module %d times, want 1", defAnchor, n)
	}
	bladed := maps.Clone(files)
	bladed["ProbeTables.nut"] = []byte(strings.Replace(module, defAnchor, `def = null`, 1))
	if bad := check(bladed); len(bad) == 0 {
		t.Error("negative control: with the declared default dropped from the descriptor the probe still agrees with the oracle")
	}

	// THE STRICT READ REFUSES what the root cannot hold, with its negative
	// control (docs/SPEC-TABLES.md §6.5).
	squirrelStrictRefusal(t, u, files, "L", "package probe\n\ntable L\n{\n    s int32\n}\n", `{"s":5}`)
}
