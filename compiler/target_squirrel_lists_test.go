// TestSquirrelCarriesUnboundedArrays proves Squirrel carries the unbounded
// array (docs/SPEC-TABLES.md §2.9): the construct's probe unit generates, the
// emitted module reads and writes it under the pinned interpreter, and its
// saved bytes are internal/tablewire's bytes for the same value (§3, §16.2).
package compiler

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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
// its bytes, loads them back and prints the value again.
const squirrelListPrelude = `local inputs = dofile("inputs.nut", true);
local schema = dofile("schema_module.nut", true);
local T = schema.require(%q);
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");
local report = {
  unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
  duplicate = 0, malformed = false, refused = false
};
local member = T.Descriptors.L.fields[0].member;
local values = function(v) {
  local s = "";
  foreach (i, x in v[member]) s += (i > 0 ? "," : "") + x;
  return s;
};
local status = function() {
  return format("%%d,%%d,%%d,%%d,%%d", report.unknown, report.kindMismatch,
    report.widened, report.clamped, report.duplicate) + "," +
    report.malformed.tostring() + "," + report.refused.tostring();
};
local read = L.loadTolerant(T, "L", inputs.want, report);
print("loaded=" + values(read) + "\n");
print("loaded-report=" + status() + "\n");
::hexline(S.save(T, "L", read));

report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
  duplicate = 0, malformed = false, refused = false };
local fresh = T.Descriptors.L.make();
fresh[member] = [10, -20, 30];
local saved = S.save(T, "L", fresh);
::hexline(saved);
local again = L.loadTolerant(T, "L", saved, report);
print("round=" + values(again) + "\n");
print("round-report=" + status() + "\n");
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
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := fmt.Sprintf(squirrelListPrelude, squirreltable.ModuleName(u))
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if got := rows["loaded"]; got != "10,-20,30" {
		t.Errorf("the oracle's bytes read back as %q, want %q", got, "10,-20,30")
	}
	for _, key := range []string{"loaded-report", "round-report"} {
		if got := rows[key]; got != "0,0,0,0,0,false,false" {
			t.Errorf("%s is %q, want a silent read", key, got)
		}
	}
	if got := rows["round"]; got != "10,-20,30" {
		t.Errorf("the Squirrel value saved and loaded back as %q, want %q", got, "10,-20,30")
	}
	if len(hexes) != 2 {
		t.Fatalf("the probe printed %d hex lines, want 2:\n%s", len(hexes), out)
	}
	oracle := hex.EncodeToString(want)
	for i, line := range hexes {
		if line != oracle {
			t.Errorf("probe saved bytes %d are %s, the Go oracle %s", i, line, oracle)
		}
	}
}
