// TestSquirrelCarriesUnboundedArrays proves Squirrel carries the unbounded
// array (docs/SPEC-TABLES.md §2.9): the construct's probe unit generates, the
// emitted module reads and writes it under the pinned interpreter, and its
// saved bytes are internal/tablewire's bytes for the same value (§3, §16.2).
package compiler

import (
	"encoding/hex"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// squirrelListProbe is the refusal test's probe unit for this construct, so
// the construct this card lifts is the construct that is run.
const squirrelListProbe = "package probe\n\ntable L\n{\n    repos []int32\n}\n"

// squirrelListPrelude prints the value the oracle's bytes read back as, the
// saved bytes, and the report; then saves a value built in Squirrel, prints
// its bytes, loads them back and prints the value again; every read is the
// strict read (docs/SPEC-TABLES.md §6.5).
const squirrelListPrelude = squirrelVerdict + `local inputs = dofile("inputs.nut", true);
local schema = dofile("schema_module.nut", true);
local T = schema.require(%q);
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");
local report = {};
local member = T.Descriptors.L.fields[0].member;
local values = function(v) {
  if (v == null) return "null";
  local s = "";
  foreach (i, x in v[member]) s += (i > 0 ? "," : "") + x;
  return s;
};
local read = L.load(T, "L", inputs.want, report);
print("loaded-report=" + ::verdict(read, report) + "\n");
print("loaded=" + values(read) + "\n");
::hexline(S.save(T, "L", read));

report = {};
local fresh = T.Descriptors.L.make();
fresh[member] = [10, -20, 30];
local saved = S.save(T, "L", fresh);
::hexline(saved);
local again = L.load(T, "L", saved, report);
print("round-report=" + ::verdict(again, report) + "\n");
print("round=" + values(again) + "\n");
`

func TestSquirrelCarriesUnboundedArrays(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, squirrelListProbe)

	// THE ORACLE: the same value, encoded by the Go engine (docs/SPEC-TABLES.md
	// §3). The count is the data's, not the declaration's (§2.9).
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["L"])
	repos := &value.Fields[0]
	for _, n := range []int64{10, -20, 30} {
		repos.Elems = append(repos.Elems, tabletext.Cell{I: n, U: uint64(n)})
	}
	repos.Count = len(repos.Elems)
	want, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatalf("the oracle refused the probe value: %v", err)
	}

	// the probe unit generates for squirrel with no error
	files, err := New().Generate(u, "squirrel", Options{})
	if err != nil {
		t.Fatalf("squirrel refused the unbounded array the card lifts: %v", err)
	}
	main := fmt.Sprintf(squirrelListPrelude, squirreltable.ModuleName(u))

	// check runs the probe over one set of emitted files and answers every
	// way it differs from the Go oracle.
	check := func(files map[string][]byte) []string {
		dir := t.TempDir()
		squirrelWriteFiles(t, dir, files, main)
		out := sqrun.Run(t, dir, "main.nut", map[string][]byte{"want": want})
		rows := map[string]string{}
		var hexes []string
		for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
			if k, v, ok := strings.Cut(line, "="); ok {
				rows[k] = v
				continue
			}
			hexes = append(hexes, line)
		}
		var bad []string
		if got := rows["loaded"]; got != "10,-20,30" {
			bad = append(bad, fmt.Sprintf("the oracle's bytes read back as %q, want %q", got, "10,-20,30"))
		}
		for _, key := range []string{"loaded-report", "round-report"} {
			if got := rows[key]; got != "accepted" {
				bad = append(bad, fmt.Sprintf("%s is %q, want the strict read to accept", key, got))
			}
		}
		if got := rows["round"]; got != "10,-20,30" {
			bad = append(bad, fmt.Sprintf("the Squirrel value saved and loaded back as %q, want %q", got, "10,-20,30"))
		}
		if len(hexes) != 2 {
			return append(bad, fmt.Sprintf("the probe printed %d hex lines, want 2:\n%s", len(hexes), out))
		}
		oracle := hex.EncodeToString(want)
		for i, line := range hexes {
			if line != oracle {
				bad = append(bad, fmt.Sprintf("probe saved bytes %d are %s, the Go oracle %s", i, line, oracle))
			}
		}
		return bad
	}
	for _, e := range check(files) {
		t.Error(e)
	}

	// NEGATIVE CONTROL: the emitted descriptor's element kind moved off int32,
	// so the array's elements are written at another kind; the probe must go
	// red against the oracle's bytes.
	const elemAnchor = `elem = { kind = 4, shape = "int"`
	module := string(files["ProbeTables.nut"])
	if n := strings.Count(module, elemAnchor); n != 1 {
		t.Fatalf("the element's anchor %q is in the emitted module %d times, want 1", elemAnchor, n)
	}
	bladed := maps.Clone(files)
	bladed["ProbeTables.nut"] = []byte(strings.Replace(module, elemAnchor, `elem = { kind = 5, shape = "int"`, 1))
	if bad := check(bladed); len(bad) == 0 {
		t.Error("negative control: with the element kind moved the probe still agrees with the oracle")
	}

	// THE STRICT READ REFUSES what the root cannot hold, with its negative
	// control (docs/SPEC-TABLES.md §6.5).
	squirrelStrictRefusal(t, u, files, "L", "package probe\n\ntable L\n{\n    repos string(8)\n}\n", `{"repos":"x"}`)
}
