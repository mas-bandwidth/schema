// The Squirrel table probes read through THE STRICT READ (docs/SPEC-TABLES.md
// §6.5): the plain L.load / <T>Load, which answers the value or null with
// report.reason naming the refusal. This file holds what every
// compiler/target_squirrel_*_test.go probe shares for that: the verdict line a
// probe prints, the strict-read refusal each probe runs beside its round trip,
// and the negative control that turns the strict read back into the walk load
// and requires the probe to go red.
package compiler

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// squirrelVerdict is the Squirrel a probe prints a strict read's answer with:
// "accepted" for a value and no reason, "refused" otherwise. Reason names are
// never compared across languages under strict-only; the verdict is.
const squirrelVerdict = `::verdict <- function(v, r) {
  return (v != null && (!("reason" in r) || r.reason == null)) ? "accepted" : "refused";
};
`

// squirrelStrictAnchor is the strict read's one line in the emitted
// schema_table_load.nut (runtime/load.nut, _M.load), and squirrelWalkBlade is
// the negative control's sabotage of it: the plain name answers the walk load
// (the value with its counters, no refusal), the read the probes left.
const (
	squirrelStrictAnchor = "  return R.ReadStrict(T, rootName, bytes, report);\n"
	squirrelWalkBlade    = "  return _M.loadWalk(T, rootName, bytes, _M.report(report));\n"
)

// squirrelWalkBladed is files with the strict read sabotaged into the walk
// load; it fails the test when the anchor is not in the emitted load exactly
// once, so the control cannot pass by missing its target.
func squirrelWalkBladed(t *testing.T, files map[string][]byte) map[string][]byte {
	t.Helper()
	return squirrelBladed(t, files, "schema_table_load.nut", squirrelStrictAnchor, squirrelWalkBlade)
}

// squirrelWriteFiles writes the emitted files and main.nut into dir.
func squirrelWriteFiles(t *testing.T, dir string, files map[string][]byte, main string) {
	t.Helper()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
}

// squirrelGoVerdict is the Go engine's verdict on wire over u's root.
func squirrelGoVerdict(u *ir.Unit, root string, wire []byte) string {
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(root))
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, inst, wire, &report); !ok || err != nil {
		return "refused"
	}
	return "accepted"
}

// squirrelOracleWire is the Go engine's bytes for json over src's root.
func squirrelOracleWire(t *testing.T, src, root, json string) []byte {
	t.Helper()
	u := unitFromSource(t, src)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(root))
	var report tabletext.Report
	if !m.Read(inst, []byte(json), &report) || !report.Silent() {
		t.Fatalf("the Go engine did not read %s over %q: %+v", json, src, report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine did not encode %s: %v", json, err)
	}
	return wire
}

// squirrelStrictVerdictProbe reads one wire through the strict read and
// prints its verdict.
func squirrelStrictVerdictProbe(module, root string) string {
	return fmt.Sprintf(`local inputs = dofile("inputs.nut", true);
local schema = dofile("schema_module.nut", true);
local T = schema.require(%q);
local L = schema.require("schema_table_load");
`+squirrelVerdict+`local r = {};
local v = L.load(T, %q, inputs.wire, r);
print(::verdict(v, r) + "\n");
`, module, root)
}

// squirrelStrictRefusal is the strict-read half every probe runs beside its
// round trip: a wire the probe's root cannot hold (written by wrongSrc, the
// same root and field names at another kind, from wrongJSON) is refused by the
// Go engine over u and by the emitted strict read. Its NEGATIVE CONTROL runs
// the same wire with the strict read bladed back into the walk load and
// requires the verdict to turn: the probe's verdict is the strict read's.
func squirrelStrictRefusal(t *testing.T, u *ir.Unit, files map[string][]byte, root, wrongSrc, wrongJSON string) {
	t.Helper()
	wire := squirrelOracleWire(t, wrongSrc, root, wrongJSON)
	squirrelRefusalControl(t, u, files, root, wire, squirrelWalkBladed(t, files))
}

// squirrelRefusalControl requires the Go engine and the emitted strict read to
// refuse wire at root, and the same read over bladed (files with the check
// that refuses it sabotaged) to accept it: the negative control.
func squirrelRefusalControl(t *testing.T, u *ir.Unit, files map[string][]byte, root string, wire []byte, bladed map[string][]byte) {
	t.Helper()
	if got := squirrelGoVerdict(u, root, wire); got != "refused" {
		t.Fatalf("the Go engine %s the wire %x; the refusal case is not one", got, wire)
	}
	main := squirrelStrictVerdictProbe(squirreltable.ModuleName(u), root)
	run := func(files map[string][]byte) string {
		dir := t.TempDir()
		squirrelWriteFiles(t, dir, files, main)
		return strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", map[string][]byte{"wire": wire})))
	}
	if got := run(files); got != "refused" {
		t.Errorf("the Squirrel strict read %s the wire %x the Go engine refuses", got, wire)
	}
	if got := run(bladed); got != "accepted" {
		t.Errorf("negative control: with the refusing check bladed the wire %x is still %s; the probe does not decide by the check", wire, got)
	}
}

// squirrelBladed is files with one emitted file's anchor replaced; it fails
// the test unless the anchor is in that file exactly once.
func squirrelBladed(t *testing.T, files map[string][]byte, file, anchor, replace string) map[string][]byte {
	t.Helper()
	src, ok := files[file]
	if !ok {
		t.Fatalf("the emitted files carry no %s", file)
	}
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("the blade's anchor %q is in %s %d times, want 1", anchor, file, n)
	}
	out := maps.Clone(files)
	out[file] = []byte(strings.Replace(string(src), anchor, replace, 1))
	return out
}
