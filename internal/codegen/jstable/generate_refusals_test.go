package jstable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// generate_refusals_test.go holds the refusals Generate makes BEFORE it emits a
// line, through Generate itself rather than the helper each refusal lives in: a
// helper that answers right is no proof the entry point still asks it.

// loadFiles writes each schema source under its own basename in one temporary
// directory and checks them as one unit.
func loadFiles(t *testing.T, files map[string]string) []string {
	t.Helper()
	dir := t.TempDir()
	var paths []string
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

// TestGenerateRefusesModuleCollisions: a schema file whose basename is a module
// this emitter writes for the unit is refused by name, never written over
// (docs/SPEC-TABLES.md §8.5, §11). Two collisions are held: the unit's VIEW
// module, <home>View.js, and a file's table surface, <base>Table.js. The
// controls are the same units with the colliding file renamed, which generate.
func TestGenerateRefusesModuleCollisions(t *testing.T) {
	t.Parallel()
	const table = "table Item { n int32 }\n"
	for _, tc := range []struct {
		name    string
		files   map[string]string
		control map[string]string
		want    string
	}{
		{
			// the runtime home is the file named for the package, `game`, so the
			// view is gameView.js: the checker's view rule names GameView, and
			// only this emitter's own refusal stands between gameView.schema and
			// a second writer of gameView.js
			name:    "view",
			files:   map[string]string{"game.schema": "package game\n" + table, "gameView.schema": "package game\ntable Other { m int32 }\n"},
			control: map[string]string{"game.schema": "package game\n" + table, "gameViews.schema": "package game\ntable Other { m int32 }\n"},
			want:    "the unit's view",
		},
		{
			name:    "table surface",
			files:   map[string]string{"shop.schema": "package shop\n" + table, "shopTable.schema": "package shop\ntable Other { m int32 }\n"},
			control: map[string]string{"shop.schema": "package shop\n" + table, "shopTables.schema": "package shop\ntable Other { m int32 }\n"},
			want:    "shopTable.js",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Generate(loadUnit(t, loadFiles(t, tc.control)...)); err != nil {
				t.Fatalf("control: the unit with no colliding file was refused: %v", err)
			}
			_, err := Generate(loadUnit(t, loadFiles(t, tc.files)...))
			if err == nil {
				t.Fatalf("a schema file named for a module this emitter writes was accepted: one of the two writers loses its file")
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "collide") {
				t.Fatalf("the refusal does not name the collision %q: %v", tc.want, err)
			}
		})
	}
}

// TestGenerateRefusesUncarriedVariableConstruct: a unit the variable form owns
// (no fixed table in its closure) that holds a construct this leg does not
// carry is refused by GENERATE, by table and field (docs/SPEC-TABLES.md §11).
// TestOtherVariableConstructsAreRefusedByName holds the refusal at
// generateVariableFiles and says the boundary is the backend's; this holds that
// Generate passes the refusal on rather than emitting the unit without its
// variable module. The checker refuses every such construct in source, so the
// unit is built as that test builds it; the control is the same unit with a
// field the leg carries.
func TestGenerateRefusesUncarriedVariableConstruct(t *testing.T) {
	t.Parallel()
	control := &ir.Struct{Name: "Probe", IsTable: true, Fields: []*ir.Field{
		{Name: "word", Type: ir.FieldType{Kind: ir.TString, Size: 8}},
	}}
	if _, err := Generate(unitWith(control)); err != nil {
		t.Fatalf("control: a carried string(8) field was refused: %v", err)
	}
	st := &ir.Struct{Name: "Probe", IsTable: true, Fields: []*ir.Field{
		{Name: "words", Type: ir.FieldType{Kind: ir.TString, Size: 8}, Array: ir.ArrayList},
	}}
	files, err := Generate(unitWith(st))
	if err == nil {
		t.Fatalf("an array of strings, which this leg does not carry on the variable wire, generated %d modules instead of a refusal", len(files))
	}
	if !strings.Contains(err.Error(), "Probe.words") || !strings.Contains(err.Error(), "an array of strings") {
		t.Fatalf("the refusal does not name Probe.words and the construct: %v", err)
	}
}

// TestGenerateLineageRefusesRecordNoLargerThanItsHash: a lineage entry whose
// record size is not larger than the 8-byte hash is not a record, and the
// build fails naming exactly that (docs/FIXED-FORM-ALGORITHM.md §5.9 #26),
// never with the generic "its own layout accounts for" arithmetic. The layout
// is the table's own, so only the record size is wrong; the control is the
// table's own entry, which generates.
func TestGenerateLineageRefusesRecordNoLargerThanItsHash(t *testing.T) {
	t.Parallel()
	u := jsUnitOf(t, "package probe\nfixed table Small\n{\n    keep uint32 = 0\n}\n")
	own, ok := FixedLineageOf(u, "Small")
	if !ok {
		t.Fatal("no lineage entry for Small")
	}
	if _, err := GenerateLineage(u, map[string][]FixedLineageEntry{"Small": {own}}); err != nil {
		t.Fatalf("control: the table's own lineage entry was refused: %v", err)
	}
	for _, record := range []int64{0, fixedHashBytes} {
		bad := own
		bad.Record = record
		_, err := GenerateLineage(u, map[string][]FixedLineageEntry{"Small": {bad}})
		if err == nil {
			t.Fatalf("record size %d was accepted", record)
		}
		if !strings.Contains(err.Error(), "which is not a record") {
			t.Fatalf("record size %d: the refusal does not say it is not a record (§5.9 #26): %v", record, err)
		}
	}
}
