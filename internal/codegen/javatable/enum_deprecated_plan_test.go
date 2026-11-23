package javatable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A DEPRECATED ENUM VARIANT READS AS THE DEFAULT ON THE COMPILED PLAN TOO
// (docs/SPEC-TABLES.md section 2.15). The writer (testdata/enum-deprecated-plan/old.schema)
// holds Old as a live variant and writes it in a scalar and in an array element;
// the reader (new.schema) has retired Old in place, so the fixed read runs the
// COMPILED plan (the writer's layout hash is not the reader's). The scalar reads
// as the field's declared default (Fierce, ordinal 3) and the array element as
// the element default None (0), and nothing is counted unknown: the variant is
// known, only retired. The ordinal op used to land the retired variant by name.
func TestJavaCompiledPlanDeprecatedVariantReadsDefault(t *testing.T) {
	const dir = "testdata/enum-deprecated-plan"
	oldUnit := versioningSchemaUnit(t, dir+"/old.schema")
	wm := tabletext.NewModel(oldUnit)
	inst := wm.New(wm.Lookup("Lineage"))
	var rep tabletext.Report
	if !wm.Read(inst, []byte(`{"one":"Old","row":["Calm","Old","Fierce"]}`), &rep) || !rep.Silent() {
		t.Fatalf("oracle read: %+v", rep)
	}
	wire, err := tablewire.EncodeFixed(wm, inst)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "old_enum_dep.bin")
	if err := os.WriteFile(file, wire, 0o644); err != nil {
		t.Fatal(err)
	}
	_, classes := buildRow(t, t.TempDir(), "enum_dep", []sideSpec{
		{key: "reads", schema: dir + "/new.schema", older: []string{dir + "/old.schema"}},
	})
	got := runProbe(t, classes, "Probe_reads", file)
	if got.n != 1 || got.refused || got.damaged || got.unknown != 0 {
		t.Fatalf("read: n=%d refused=%v reason=%s damaged=%v unknown=%d, want one clean record", got.n, got.refused, got.reason, got.damaged, got.unknown)
	}
	if one := got.value["one"]; one != "3" {
		t.Errorf("one = %q, want 3 (the field's declared default Fierce, not the retired Old)", one)
	}
	if row := got.value["row"]; row != "bytes:010003" {
		t.Errorf("row = %q, want bytes:010003 (the retired Old reads None as an element)", row)
	}
}
