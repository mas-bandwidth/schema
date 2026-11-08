package squirreltable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const viewDocTagsSixTablesSchema = `package carrydoctags

/// One weapon's tuning. A designer edits this table and nothing else.
fixed table WeaponConfig | designer_facing
{
    ///  Damage per hit, before armor.
    ///
    /// The armor curve is written "hardness \ hit" in the design notes,
    /// and the backslash is part of the name.
    damage      float32 = 21.0  | ui_slider, hot, tuning
    speed       float32 = 500.0 | was = "velocity"
    penetration int32 = 1       | min = 0, max = 10
}

/// Starter stats and archetype tags for a hero class.
fixed table HeroTemplate | roster, balance
{
    /// Base hit points at level one.
    health  int32 = 100 | min = 1, max = 10000, core_stat
    stamina float32 = 50.0
}

/// Weighted drop table for an encounter tier.
fixed table LootTable | economy
{
    /// Drop rolls granted on clear.
    rolls int32 = 3 | min = 0, max = 16, reward
    bonus float32 = 1.0
}

/// Top-level match parameters shared by client and server.
fixed table GameConfig | runtime_config, authoritative
{
    /// Simulation tick rate in hertz.
    tick_rate int32 = 60 | min = 10, max = 240, net_tick
    weapons   [..4]WeaponConfig
    heroes    [..4]HeroTemplate
    loot      LootTable
}

fixed table PartFrame
{
    version uint64
    count   uint32 | frame_counter
}

fixed table ToolMessage
{
    sequence uint32
    flags    uint32
}
`

const carriedViewDocTagsProbe = `
local m = dofile("schema_module.nut", true).require(%q);
foreach (name in [%s]) {
  local info = m[name + "TableInfo"];
  if (info.doc == "" && info.doc != m.TableDocNone) print("BAD|" + name + ": empty table doc is not TableDocNone\n");
  if (info.tags.len() == 0 && info.tags != m.TableTagsNone) print("BAD|" + name + ": empty table tags is not TableTagsNone\n");
  local fs = m[name + "TableFields"];
  if (info.fields != fs) print("BAD|" + name + ": TableInfo.fields is not TableFields\n");
  for (local i = 0; i < fs.len(); i++) {
    local f = fs[i];
    if (f.doc == "" && f.doc != m.TableDocNone) print("BAD|" + name + "." + f.name + ": empty field doc is not TableDocNone\n");
    if (f.tags.len() == 0 && f.tags != m.TableTagsNone) print("BAD|" + name + "." + f.name + ": empty field tags is not TableTagsNone\n");
  }
  viewTable(%q, name, info);
}
`

func TestSquirrelViewDocAndTagsCarried(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}

	// Verify the corpus units (tables/examples, tables/blockhome, tables/messages)
	// and the six-table unit (WeaponConfig, HeroTemplate, LootTable, GameConfig,
	// PartFrame, ToolMessage) emit exact <T>TableInfo and <T>TableFields doc and
	// tags descriptors plus shared _M.TableDocNone and _M.TableTagsNone.
	for _, corpusPath := range []struct {
		dir   string
		table string
	}{
		{dir: "tables/examples", table: "WeaponConfig"},
		{dir: "tables/blockhome", table: "PartFrame"},
		{dir: "tables/messages", table: "ToolMessage"},
	} {
		cu := loadUnit(t, root, []string{corpusPath.dir})
		assertSquirrelUnitDocAndTags(t, corpusPath.dir, cu, []string{corpusPath.table})
	}

	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "CarryDocTags.schema")
	writeFile(t, schemaPath, []byte(viewDocTagsSixTablesSchema))
	u := loadUnit(t, dir, []string{"CarryDocTags.schema"})
	sixTables := []string{
		"WeaponConfig",
		"HeroTemplate",
		"LootTable",
		"GameConfig",
		"PartFrame",
		"ToolMessage",
	}
	files := assertSquirrelUnitDocAndTags(t, "carrydoctags", u, sixTables)

	// Verify the carried view_test.go pin and negative control, Makefile wiring,
	// and Squirrel packet value defaults emission are present on the branch.
	viewTestBytes, err := os.ReadFile(filepath.Join(root, "internal", "codegen", "squirreltable", "view_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	viewTestSrc := string(viewTestBytes)
	for _, sym := range []string{
		"func TestSquirrelViewDocAndTags(",
		"func TestSquirrelViewDocAndTagsNegativeControl(",
		"func buildSquirrelDocTagsUnit(",
		"const viewDocTagsBody =",
	} {
		if !strings.Contains(viewTestSrc, sym) {
			t.Fatalf("internal/codegen/squirreltable/view_test.go missing carried pin %q", sym)
		}
	}

	exUnit := loadUnit(t, root, []string{"examples"})
	exFiles, err := squirrel.Generate(exUnit)
	if err != nil {
		t.Fatalf("examples: squirrel.Generate refused: %v", err)
	}
	if !strings.Contains(string(exFiles["ArmDefaults.nut"]), "value.First = _M.DefaultArm();") {
		t.Fatalf("ArmDefaults.nut missing union arm default reset 'value.First = _M.DefaultArm();'")
	}

	for _, check := range []struct {
		rel    string
		needle string
	}{
		{rel: "compiler/target_squirrel_defaults.go", needle: "registerPacketValueDefaultCarrier(\"squirrel\")"},
		{rel: "make/squirrel.mk", needle: "TestSquirrelViewDocAndTags"},
		{rel: "make/squirrel.mk", needle: "shasum -a 256"},
		{rel: "make/luau.mk", needle: "shasum -a 256"},
		{rel: "make/squirrel-negative-controls.mk", needle: "tables-squirrel-view-doc-tags-negative-control"},
		{rel: "make/squirrel-packet-wide.mk", needle: "packet-utf8-squirrel:"},
		{rel: "make/squirrel-packet-wide.mk", needle: "packet-defaults-squirrel:"},
		{rel: "make/negative-controls.json", needle: "tables-squirrel-view-doc-tags-negative-control"},
		{rel: "make/negative-controls.json", needle: "packet-utf8-squirrel-negative-control"},
		{rel: "make/negative-controls.json", needle: "packet-defaults-squirrel-negative-control"},
	} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(check.rel)))
		if err != nil {
			t.Fatalf("%s missing on branch: %v", check.rel, err)
		}
		if !strings.Contains(string(data), check.needle) {
			t.Fatalf("%s missing carried entry %q", check.rel, check.needle)
		}
	}

	if slowtest.Enabled() {
		runDir := t.TempDir()
		for name, data := range files {
			writeFile(t, filepath.Join(runDir, name), data)
		}
		quoted := make([]string, len(sixTables))
		for i, name := range sixTables {
			quoted[i] = fmt.Sprintf("%q", name)
		}
		modName := squirreltable.ModuleName(u)
		probe := viewPrelude + fmt.Sprintf(carriedViewDocTagsProbe, modName, strings.Join(quoted, ", "), "carrydoctags")
		writeFile(t, filepath.Join(runDir, "main.nut"), []byte(probe))
		out := sqrun.Run(t, runDir, "main.nut", nil)
		var sqLines []string
		for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
			if strings.HasPrefix(line, "BAD|") {
				t.Fatalf("Squirrel view probe reported: %s", line)
			}
			if strings.HasPrefix(line, "INFO|") || strings.HasPrefix(line, "VIEW|") {
				sqLines = append(sqLines, line)
			}
		}
		goLines := runGoView(t, root, viewUnit{
			key:     "carrydoctags",
			unit:    u,
			names:   sixTables,
			dir:     runDir,
			modName: modName,
		})
		sort.Strings(sqLines)
		sort.Strings(goLines)
		if len(sqLines) != len(goLines) {
			t.Fatalf("Squirrel printed %d view rows, Go printed %d", len(sqLines), len(goLines))
		}
		for i := range sqLines {
			if sqLines[i] != goLines[i] {
				t.Fatalf("row %d differs\n squirrel: %s\n       go: %s", i, sqLines[i], goLines[i])
			}
		}
	}
}

func assertSquirrelUnitDocAndTags(t *testing.T, label string, u *ir.Unit, requiredTables []string) map[string][]byte {
	t.Helper()
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("%s: squirrel.Generate refused: %v", label, err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("%s: squirreltable.Generate refused: %v", label, err)
	}
	maps.Copy(files, tables)

	modFile := squirreltable.ModuleName(u) + ".nut"
	modSrc := string(files[modFile])
	if strings.Count(modSrc, `_M.TableDocNone <- "";`) != 1 {
		t.Fatalf("%s (%s): defines _M.TableDocNone %d times, want 1", label, modFile, strings.Count(modSrc, `_M.TableDocNone <- "";`))
	}
	if strings.Count(modSrc, `_M.TableTagsNone <- [];`) != 1 {
		t.Fatalf("%s (%s): defines _M.TableTagsNone %d times, want 1", label, modFile, strings.Count(modSrc, `_M.TableTagsNone <- [];`))
	}

	for _, name := range requiredTables {
		st := u.Tables[name]
		if st == nil {
			t.Fatalf("%s: missing table %s in IR", label, name)
		}
		wantTableDoc := "_M.TableDocNone"
		if st.Doc != "" {
			wantTableDoc = squirrel.StringLit([]byte(st.Doc))
		}
		wantTableNumTags := fmt.Sprintf("numTags = %d,", len(st.Tags))
		wantTableTags := fmt.Sprintf("tags = %s,", expectedSquirrelTagsLit(st.Tags))
		infoHeader := fmt.Sprintf("_M.%sTableInfo <- {", name)
		infoIdx := strings.Index(modSrc, infoHeader)
		if infoIdx < 0 {
			t.Fatalf("%s: missing %s in %s", label, infoHeader, modFile)
		}
		infoBlock := modSrc[infoIdx:]
		if end := strings.Index(infoBlock, "\n};\n"); end >= 0 {
			infoBlock = infoBlock[:end]
		}
		if !strings.Contains(infoBlock, "doc = "+wantTableDoc+",") {
			t.Fatalf("%s: %sTableInfo missing doc = %s in:\n%s", label, name, wantTableDoc, infoBlock)
		}
		if !strings.Contains(infoBlock, wantTableNumTags) || !strings.Contains(infoBlock, wantTableTags) {
			t.Fatalf("%s: %sTableInfo missing %s %s in:\n%s", label, name, wantTableNumTags, wantTableTags, infoBlock)
		}

		fieldsHeader := fmt.Sprintf("_M.%sTableFields <- [", name)
		fieldsIdx := strings.Index(modSrc, fieldsHeader)
		if fieldsIdx < 0 {
			t.Fatalf("%s: missing %s in %s", label, fieldsHeader, modFile)
		}
		fieldsBlock := modSrc[fieldsIdx:]
		if end := strings.Index(fieldsBlock, "\n];\n"); end >= 0 {
			fieldsBlock = fieldsBlock[:end]
		}
		for _, f := range st.Fields {
			wantFieldDoc := "_M.TableDocNone"
			if f.Doc != "" {
				wantFieldDoc = squirrel.StringLit([]byte(f.Doc))
			}
			wantFieldTags := fmt.Sprintf("numTags = %d, tags = %s", len(f.Tags), expectedSquirrelTagsLit(f.Tags))
			if !strings.Contains(fieldsBlock, "doc = "+wantFieldDoc) {
				t.Fatalf("%s: %s.%s missing doc = %s in %sTableFields", label, name, f.Name, wantFieldDoc, name)
			}
			if !strings.Contains(fieldsBlock, wantFieldTags) {
				t.Fatalf("%s: %s.%s missing %s in %sTableFields", label, name, f.Name, wantFieldTags, name)
			}
		}
	}
	return files
}

func expectedSquirrelTagsLit(tags []string) string {
	if len(tags) == 0 {
		return "_M.TableTagsNone"
	}
	parts := make([]string, len(tags))
	for i, tag := range tags {
		parts[i] = squirrel.StringLit([]byte(tag))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
