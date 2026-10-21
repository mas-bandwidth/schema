package squirreltable_test

import (
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// strictCountRefusalPrelude is the strict-read probe for this card: load a
// wire file through ReadStrict and print the named reason, exactly as the
// refusal corpus does (§6.5, §11).
const strictCountRefusalPrelude = `local inputs = dofile("inputs.nut", true);
local run = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local R = schema.require("schema_table_refusal");
  local report = {};
  local v = R.ReadStrict(T, root, inputs[key], report);
  if ((v == null) != (report.reason != null))
    throw "ReadStrict answered a value and a reason together, or neither";
  print(key + " " + (report.reason == null ? "ok" : report.reason) + "\n");
};
`

// TestSquirrelWireCountRefusalNamesCountRefusal pins the hostile-corpus row
// wire-count-refusal (schema-squirrel-runs-hostile-corpus-t-r): the form-1
// probe body whose map count is past the int32 storage cap must refuse the
// whole value with the reason count_refusal — the Go engine's answer is
// *tablewire.CountRefusal on the very same bytes — never read as malformed.
func TestSquirrelWireCountRefusalNamesCountRefusal(t *testing.T) {
	t.Parallel()
	unit := loadSource(t, "package probe\n\ntable Root\n{\n    entries map[int32]int32\n}\n")
	dir := t.TempDir()
	mod := emitUnit(t, dir, "probe", unit)
	raw, err := hex.DecodeString("01010e080d808080800801000053a245082ca7b2c50100000000000000")
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(unit)
	var report tabletext.Report
	if _, err := tablewire.Decode(model, model.New(model.Lookup("Root")), raw, &report); err == nil {
		t.Fatal("the wire oracle read a count past the cap without refusing")
	} else {
		if _, ok := errors.AsType[*tablewire.CountRefusal](err); !ok {
			t.Fatalf("the wire oracle refuses %T, want *tablewire.CountRefusal", err)
		}
	}
	var p strings.Builder
	p.WriteString(strictCountRefusalPrelude)
	fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", "probe", mod, "Root", "case")
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	got := reportLine(t, sqrun.Run(t, dir, "main.nut", map[string][]byte{"case": raw}))
	if got != "count_refusal" {
		t.Errorf("wire-count-refusal: Squirrel reports reason %q, want count_refusal", got)
	}
}
