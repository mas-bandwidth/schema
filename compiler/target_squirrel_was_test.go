// Squirrel carries a `was` on an enum variant, on a union arm and on a field
// of a type a table reaches (docs/SPEC-TABLES.md §5): the three ride under the
// OLD name's id, so a value saved under the old spelling loads into the
// renamed field in silence. compiler/target_squirrel_was.go registers the
// target; this test drives the emitted module under the pinned interpreter and
// holds its bytes to the compiler-side engine (internal/tablewire).
package compiler

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// squirrelWasVariantProbe is the `was`-on-a-variant probe (docs/SPEC-TABLES.md
// §5). It is a variable-class table so the emitted module can also be driven
// through internal/tablewire's form 1.
const squirrelWasVariantProbe = `package probe

enum Grade
{
    Bronze,
    Argent | was = "Silver"
}

table L
{
    grade Grade
}
`

// squirrelWasTypeProbe is the third row on a unit squirrel carries whole: a
// `was` on a field of a type a table reaches.
const squirrelWasTypeProbe = `package probe

type Buff
{
    mult float32 = 1.0 | was = "multiplier"
}

table L
{
    buff Buff
}
`

// squirrelWasOldProbe is the probe unit BEFORE the variant's rename: the same
// table under the old variant name, which the wire still carries.
const squirrelWasOldProbe = `package probe

enum Grade
{
    Bronze,
    Silver
}

table L
{
    grade Grade
}
`

// squirrelWasProbe is the run under the pinned interpreter: save the renamed
// value, read it back, then read the old-name wire into the renamed field and
// save it again. Every answer is a line; the test owns the comparison.
const squirrelWasProbe = `local inputs = dofile("inputs.nut", true);
local schema = dofile("schema_module.nut", true);
local T = schema.require("ProbeTables");
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");

local hex = function(b) {
  if (b == null) return "null";
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%02x", b[i] & 0xff);
  return s;
};
local report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
                 duplicate = 0, malformed = false, refused = false };
local v = T.L();
v.Grade = "Argent";
local saved = S.save(T, "L", v);
print("SAVE:" + hex(saved) + "\n");

local back = L.loadTolerant(T, "L", saved, report);
print("LOADED:" + back.Grade + ":" + report.malformed + ":" + report.refused + "\n");
print("RESAVE:" + hex(S.save(T, "L", back)) + "\n");

local old = L.loadTolerant(T, "L", inputs["old"], report);
print("OLDLOADED:" + old.Grade + ":" + report.malformed + ":" + report.refused + "\n");
print("OLDRESAVE:" + hex(S.save(T, "L", old)) + "\n");
`

func TestSquirrelCarriesWasRows(t *testing.T) {
	t.Parallel()
	c := New()

	// THE PROBE THE REFUSAL TEST USES: the tagged target now generates it
	// (docs/SPEC-TABLES.md §5).
	probe := unitFromSource(t, squirrelWasVariantProbe)
	files, err := c.Generate(probe, "squirrel", Options{})
	if err != nil {
		t.Fatalf("squirrel refused the was-on-a-variant probe: %v (docs/SPEC-TABLES.md §5)", err)
	}

	// THE REGISTER: squirrel is a carrier of all three rows, so the refuser
	// that names them accepts it and still refuses an unregistered target
	// (docs/SPEC-TABLES.md §5). `wasRowsUnit` carries the variant, the arm and
	// the type field together.
	all := unitFromSource(t, wasRowsUnit)
	if err := refuseWasRows(all, "squirrel"); err != nil {
		t.Fatalf("the was-row refuser still refuses squirrel: %v (docs/SPEC-TABLES.md §5)", err)
	}
	if err := refuseWasRows(all, "zig"); err == nil {
		t.Fatal("the was-row refuser accepted the unregistered target zig")
	}

	// THE OTHER TWO ROWS' IDS: a type field a table reaches generates under
	// its old name's id (docs/SPEC-TABLES.md §5), beside the variant probe's.
	typed := unitFromSource(t, squirrelWasTypeProbe)
	typedFiles, err := c.Generate(typed, "squirrel", Options{})
	if err != nil {
		t.Fatalf("squirrel refused the was-on-a-type-field probe: %v (docs/SPEC-TABLES.md §5)", err)
	}
	var emitted strings.Builder
	for _, data := range typedFiles {
		emitted.Write(data)
	}
	for _, old := range []string{"multiplier"} {
		want := fmt.Sprintf("0x%016x", ir.TableWireId(old))
		if !strings.Contains(emitted.String(), want) {
			t.Errorf("the emitted module lacks the id of the old name %q, %s (docs/SPEC-TABLES.md §5)", old, want)
		}
	}

	// THE SAME VALUE ON THE COMPILER-SIDE ENGINE, with the renamed name and
	// with the old one. docs/SPEC-TABLES.md §5 makes their bytes identical.
	newModel := tabletext.NewModel(probe)
	newValue := newModel.New(newModel.Lookup("L"))
	var report tabletext.Report
	if newModel.Read(newValue, []byte(`{"grade":"Argent"}`), &report); !report.Silent() {
		t.Fatalf("the renamed probe value did not read clean: %+v", report)
	}
	newWire, err := tablewire.Encode(newModel, newValue)
	if err != nil {
		t.Fatal(err)
	}
	oldModel := tabletext.NewModel(unitFromSource(t, squirrelWasOldProbe))
	oldValue := oldModel.New(oldModel.Lookup("L"))
	report = tabletext.Report{}
	if oldModel.Read(oldValue, []byte(`{"grade":"Silver"}`), &report); !report.Silent() {
		t.Fatalf("the old-name probe value did not read clean: %+v", report)
	}
	oldWire, err := tablewire.Encode(oldModel, oldValue)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(newWire, oldWire) {
		t.Fatalf("the renamed and old-name wires differ: %x against %x", newWire, oldWire)
	}

	// AN OLD-NAME WIRE READS INTO THE RENAMED FIELD, and Load then Save is the
	// same bytes (docs/SPEC-TABLES.md §5, §3).
	backModel := tabletext.NewModel(probe)
	back := backModel.New(backModel.Lookup("L"))
	report = tabletext.Report{}
	if ok, err := tablewire.Decode(backModel, back, oldWire, &report); !ok || err != nil || !report.Silent() {
		t.Fatalf("the old-name wire did not load into the renamed schema: ok=%v err=%v report=%+v", ok, err, report)
	}
	want, _ := newValue.FieldByKey("grade")
	got, _ := back.FieldByKey("grade")
	if got == nil || want == nil || got.Cell.U != want.Cell.U {
		t.Fatalf("the old-name wire did not land in the renamed field: got %+v want %+v", got, want)
	}
	saved, err := tablewire.Encode(backModel, back)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, oldWire) {
		t.Fatalf("internal/tablewire Load then Save moved the bytes: %x against %x", saved, oldWire)
	}

	// THE EMITTED MODULE, under the pinned interpreter in a directory the test
	// owns.
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(squirrelWasProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := sqrun.Run(t, dir, "main.nut", map[string][]byte{"old": oldWire})
	lines := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("probe line %q has no label", line)
		}
		lines[key] = value
	}
	if got, want := lines["SAVE"], hex.EncodeToString(newWire); got != want {
		t.Errorf("Squirrel saved %s, internal/tablewire saves %s", got, want)
	}
	if got, want := lines["RESAVE"], lines["SAVE"]; got != want {
		t.Errorf("Squirrel save-load-save moved the bytes: %s against %s", got, want)
	}
	if got, want := lines["LOADED"], "Argent:false:false"; got != want {
		t.Errorf("the saved value did not load back as Argent: %s, want %s", got, want)
	}
	if got, want := lines["OLDLOADED"], "Argent:false:false"; got != want {
		t.Errorf("the old-name wire did not load into the renamed field: %s, want %s", got, want)
	}
	if got, want := lines["OLDRESAVE"], hex.EncodeToString(oldWire); got != want {
		t.Errorf("the old-name wire did not save back byte for byte: %s against %s", got, want)
	}
}
