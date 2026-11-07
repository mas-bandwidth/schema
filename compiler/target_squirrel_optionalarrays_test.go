// The Squirrel target CARRIES the optional array (docs/SPEC-TABLES.md §2.3):
// the probe unit of the refusal test generates, the emitted module saves and
// loads an absent, a present-empty and a present-full `?[..4]int32`, the value
// comes back equal to itself, and the saved bytes equal tablewire's save of the
// same value — presence included, because a present empty array always rides.
package compiler

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The refusal test's optional array case now rides in the append hook rather
// than the shared row literal: the tagged build carries it, the untagged build
// still refuses it by name (compiler/target_squirrel_optionalarrays_rows_test.go).
func init() {
	extraRefusalRows = append(extraRefusalRows, refusalRow{
		refuser: "refuseOptionalArrays",
		name:    "optional array",
		src:     "package probe\n\ntable L\n{\n    maybe ?[..4]int32\n}\n",
		field:   "L.maybe",
		carried: true,
	})
}

// squirrelOptionalArrayProbe is the probe unit of the refusal test's optional
// array case (compiler/tablessquirrel_test.go).
const squirrelOptionalArrayProbe = `package probe

table L
{
    maybe ?[..4]int32
}
`

// squirrelOptionalArrayMain drives the emitted save and load runtimes for the
// three presences. A failed assertion prints FAIL and the test reads it as a
// failure; a clean case prints OK, the case and the saved bytes as hex.
const squirrelOptionalArrayMain = `local schema = dofile("schema_module.nut", true);
local T = schema.require("ProbeTables");
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");
local report = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false };
};
local check = function(key, v, present, want) {
  local b = S.save(T, "L", v);
  local r = report();
  local back = L.loadTolerant(T, "L", b, r);
  if (r.malformed || r.refused) { print("FAIL " + key + " load\n"); return; }
  if (back.MaybePresent != present) { print("FAIL " + key + " present\n"); return; }
  if (present) {
    if (back.Maybe.len() != want.len()) { print("FAIL " + key + " len\n"); return; }
    for (local i = 0; i < want.len(); i++) {
      if (back.Maybe[i] != want[i]) { print("FAIL " + key + " value\n"); return; }
    }
  }
  local h = "";
  for (local i = 0; i < b.len(); i++) h += format("%02x", b[i] & 0xff);
  print("OK " + key + " " + h + "\n");
};
local a = T.L();
check("absent", a, false, []);
local e = T.L();
e.Maybe = [];
e.MaybePresent = true;
check("empty", e, true, []);
local f = T.L();
f.Maybe = [10, 20, 30];
f.MaybePresent = true;
check("full", f, true, [10, 20, 30]);
`

func TestSquirrelCarriesOptionalArrays(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, squirrelOptionalArrayProbe)
	files, err := New().Generate(u, "squirrel", Options{})
	if err != nil {
		t.Fatalf("squirrel refused an optional array in a table closure: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(squirrelOptionalArrayMain), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := sqrun.Run(t, dir, "main.nut", nil)

	got := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimRight(string(stdout), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "OK" {
			t.Fatalf("probe line %q is not OK, a case and saved bytes", line)
		}
		b, err := hex.DecodeString(fields[2])
		if err != nil {
			t.Fatalf("%s: saved bytes are not hex: %v", fields[1], err)
		}
		got[fields[1]] = b
	}

	model := tabletext.NewModel(u)
	cases := []struct {
		name, text string
	}{
		{"absent", `{}`},
		{"empty", `{"maybe": []}`},
		{"full", `{"maybe": [10, 20, 30]}`},
	}
	for _, tc := range cases {
		value := model.New(u.Tables["L"])
		var report tabletext.Report
		if !model.Read(value, []byte(tc.text), &report) {
			t.Fatalf("%s: the text form refused %s: %+v", tc.name, tc.text, report)
		}
		if report.Malformed || report.Refused || report.KindMismatch != 0 {
			t.Fatalf("%s: the text form did not read cleanly: %+v", tc.name, report)
		}
		want, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatalf("%s: tablewire save: %v", tc.name, err)
		}
		have, ok := got[tc.name]
		if !ok {
			t.Fatalf("%s: the emitted module saved no case by that name", tc.name)
		}
		if !bytes.Equal(have, want) {
			t.Errorf("%s: the emitted module saved %x, tablewire saves %x", tc.name, have, want)
		}
	}
}
