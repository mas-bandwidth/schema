package rusttable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestUnitViewDescriptorsRust pins the Rust leg's VIEW (docs/SPEC-TABLES.md
// §8.1, §8.2, §8.3): the emitted unit_view.rs carries a descriptor for EVERY
// table and every type the unit holds — not only the card tables — with the
// field's text key, type spelling, wire id, record-layout offset, doc and
// tags; the enum, flags and union vocabulary with each variant's id and
// annotation; the constants; and the unit registry unit_view() returns.
//
// The rows below are WRITTEN IN THE TEST from the schema sources, not derived
// from the emitter, so a column the emitter drops or misspells goes red here.
// The completeness half (every declaration has its row) is held over the whole
// VIEW_CORPUS by make tables-rust-view's listing gate, which compares the
// listing a Rust program prints from unit_view() with the compiler's own.
//
// A unit test: it reads the generator's output and nothing else, with no
// real time, no sockets, no network and no outside service.
func TestUnitViewDescriptorsRust(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		dir  string
		pins []string
	}{
		{"examples", []string{
			// the registry and its entry point (§8.3)
			`package: "tabledemo",`,
			`pub fn unit_view() -> &'static TableUnitView {`,
			// a constant with its doc and tag, and one with neither (§8.1:
			// absence has a spelling)
			`TableViewConstant { name: "MaxLoadoutSlots", file: "Tables.schema", type_name: "int64", is_float: false, int_value: 0x0000000000000008_u64 as i64, float_value: f64::from_bits(0x0000000000000000), doc: "The slots a loadout admits. Raising it is a free edit on the wire: a\nbound sizes storage, and it is not an id.", num_tags: 1, tags: &["tuning"] },`,
			`TableViewConstant { name: "MaxProfileName", file: "Tables.schema", type_name: "int64", is_float: false, int_value: 0x0000000000000020_u64 as i64, float_value: f64::from_bits(0x0000000000000000), doc: TABLE_DOC_NONE, num_tags: 0, tags: &[] },`,
			// an enum's head row and a variant carrying three tags and a doc
			`TableViewVocabulary { name: "Grade", file: "Tables.schema", max: 3, storage_bits: 8, num_variants: 4, doc: "A profile's standing, ranked low to high.", num_tags: 1, tags: &["progression"], variants: &[`,
			`name: "Gold", id: 0x`,
			`doc: "The top rank. A profile that reaches it keeps it.", num_tags: 3, tags: &["celebrate", "rare", "announce"] },`,
			// a flags mask, its bits by index with no wire id (§4, §8.3)
			`TableViewVocabulary { name: "Perks", file: "Tables.schema", max: 2, storage_bits: 8, num_variants: 3,`,
			`TableViewVariant { value: 0, name: "Shielded", id: 0, payload_name: None, payload: None, field: None,`,
			// a union and a record-naming arm with its doc and tag
			`TableViewVocabulary { name: "Effect", file: "Tables.schema",`,
			`name: "buff", id: 0x`,
			`payload_name: Some("Buff"), payload: Some(&VIEW_T_`,
			`doc: "A multiplier that rides while the effect is live.", num_tags: 1, tags: &["positive"] },`,
			// a fixed table (not a card table) and its renamed text key (§16.4)
			`TableViewType { name: "ShipEntry", file: "Pack.schema", table: true, type_info: &VIEW_T_`,
			`TableFieldInfo { name: "display_name", json: Some("name"), type_name: "string", id: 0x`,
			`TableFieldInfo { name: "gunner", json: Some("gunner"), type_name: "GunnerSettings", id: 0x`,
			// a type, beside the tables (§8.2)
			`TableViewType { name: "Attachment", file: "Tables.schema", table: false, type_info: &VIEW_T_`,
		}},
		{"pointers", []string{
			`package: "graphdemo",`,
			// the pointered tables of the variable class (§2.5)
			`TableViewType { name: "ListNode", file: "Graph.schema", table: true, type_info: &VIEW_T_`,
			`TableViewType { name: "TreeNode", file: "Graph.schema", table: true, type_info: &VIEW_T_`,
			`TableViewType { name: "Scene", file: "Graph.schema", table: true, type_info: &VIEW_T_`,
			`TableViewConstant { name: "MaxLayers", file: "Graph.schema", type_name: "int64", is_float: false, int_value: 0x0000000000000004_u64 as i64,`,
			// a type, which no table closure reaches by declaration alone
			`TableViewType { name: "Colour", file: "Parts.schema", table: false, type_info: &VIEW_T_`,
			`TableViewVocabulary { name: "Tier", file: "Graph.schema", max: 2, storage_bits: 8, num_variants: 3,`,
		}},
	} {
		t.Run(c.dir, func(t *testing.T) {
			t.Parallel()
			u := loadViewUnit(t, filepath.Join("..", "..", "..", "tables", c.dir))
			out, err := Generate(u)
			if err != nil {
				t.Fatalf("tables/%s: %v", c.dir, err)
			}
			view, ok := out[UnitViewModule+".rs"]
			if !ok {
				t.Fatalf("tables/%s: the Rust leg emits no %s.rs; the unit's tables and types have no descriptors and the unit no registry (docs/SPEC-TABLES.md §8.1, §8.3)", c.dir, UnitViewModule)
			}
			src := string(view)
			for _, pin := range c.pins {
				if !strings.Contains(src, pin) {
					t.Errorf("tables/%s: %s.rs does not carry\n  %s", c.dir, UnitViewModule, pin)
				}
			}
			// EVERY declaration has its row, every field of every record
			// its descriptor with its own wire id, in name order (§8.3).
			assertViewComplete(t, u, src)
		})
	}
}

// assertViewComplete holds the registry's count and order against the IR:
// one TableViewType row per declared type and table in declaration-name
// order, a map's generated entry table excluded (§2.8), and one field row per
// field of each.
func assertViewComplete(t *testing.T, u *ir.Unit, src string) {
	t.Helper()
	rows := regexp.MustCompile(`TableViewType \{ name: "([A-Za-z0-9_]+)", file: "[^"]*", table: (true|false),`).FindAllStringSubmatch(src, -1)
	var want []string
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && !st.IsTable {
				want = append(want, "false:"+st.Name)
			}
		}
	}
	var tables []string
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.MapEntryOf == "" {
				tables = append(tables, "true:"+st.Name)
			}
		}
	}
	sortStrings(want)
	sortStrings(tables)
	want = append(want, tables...)
	got := make([]string, 0, len(rows))
	for _, r := range rows {
		got = append(got, r[2]+":"+r[1])
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("the registry rows are not the unit's declarations in name order:\n  got  %v\n  want %v", got, want)
	}
	for _, st := range u.Tables {
		for _, f := range st.Fields {
			row := `TableFieldInfo { name: "` + f.Name + `", json: Some(` + viewStr(ir.TableFieldJsonKey(f)) + `), type_name: ` + viewStr(ir.TableTypeSpelling(f)) + `, id: 0x`
			if !strings.Contains(src, row) {
				t.Errorf("table %s field %s has no descriptor row %s", st.Name, f.Name, row)
			}
		}
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func loadViewUnit(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("%s: no schema files (%v)", dir, err)
	}
	var files []check.SourceFile
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(p)
		f, perrs := parser.Parse(name, src)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", p, perrs[0])
		}
		files = append(files, check.SourceFile{Path: name, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: src, AST: f})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", dir, cerrs[0])
	}
	return u
}
