package darttable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestUnitViewDescriptorsDart pins the declared vocabulary, including
// annotations, outside the card's wire closure (SPEC-TABLES §8.1–8.3).
func TestUnitViewDescriptorsDart(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	makefile, err := os.ReadFile(filepath.Join(repo, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus string
	collecting := false
	for _, line := range strings.Split(string(makefile), "\n") {
		if strings.HasPrefix(line, "VIEW_CORPUS :=") {
			collecting = true
			line = strings.TrimPrefix(line, "VIEW_CORPUS :=")
		}
		if !collecting {
			continue
		}
		line = strings.TrimSpace(line)
		more := strings.HasSuffix(line, "\\")
		corpus += " " + strings.TrimSpace(strings.TrimSuffix(line, "\\"))
		if !more {
			break
		}
	}
	if corpus == "" {
		t.Fatal("Makefile has no VIEW_CORPUS")
	}
	for _, entry := range strings.Fields(corpus) {
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			t.Fatalf("VIEW_CORPUS entry %q has no package", entry)
		}
		dir := filepath.Join(repo, "tables", parts[0])
		if parts[0] == "wide" {
			dir = filepath.Join(repo, "examples-wide")
		}
		schemas, err := filepath.Glob(filepath.Join(dir, "*.schema"))
		if err != nil || len(schemas) == 0 {
			t.Fatalf("VIEW_CORPUS %s: %v, %d schema files", entry, err, len(schemas))
		}
		unit := loadUnit(t, schemas...)
		generated, err := Generate(unit)
		if err != nil {
			t.Fatalf("VIEW_CORPUS %s: %v", entry, err)
		}
		view := string(generated[runtimeHome(unit)+"View.dart"])
		if view == "" {
			t.Errorf("VIEW_CORPUS %s has no UnitView", entry)
			continue
		}
		for _, name := range sortedKeys(unit.Tables) {
			st := unit.Tables[name]
			if st.IsMapEntry() {
				continue
			}
			if !strings.Contains(view, "// table "+name) {
				t.Errorf("VIEW_CORPUS %s lacks table %s", entry, name)
			}
			for _, field := range st.Fields {
				kind := ir.TableWireScalarKind(field)
				if field.IsMap() {
					kind = ir.TableKindTable
				} else if field.Type.Kind == ir.TBytes && !field.Type.Blob() {
					kind = ir.TableKindU8
				}
				want := fmt.Sprintf("ViewField(%s,%s,%s,%s,%t,%d,", viewDartString(field.Name),
					viewDartString(ir.TableFieldJsonKey(field)), viewDartString(ir.TableTypeSpelling(field)),
					dartU64Literal(ir.TableFieldWireId(field)), field.Type.Optional, kind)
				if !strings.Contains(view, want) {
					t.Errorf("VIEW_CORPUS %s lacks %s.%s kind %d", entry, name, field.Name, kind)
				}
			}
		}
		for _, group := range []struct {
			label string
			names []string
		}{{"enum", sortedKeys(unit.Enums)}, {"flags", sortedKeys(unit.Flags)},
			{"union", sortedKeys(unit.Unions)}, {"constant", sortedKeys(unit.Consts)}} {
			for _, name := range group.names {
				if !strings.Contains(view, "// "+group.label+" "+name) {
					t.Errorf("VIEW_CORPUS %s lacks %s %s", entry, group.label, name)
				}
			}
		}
		for _, name := range sortedKeys(unit.Enums) {
			for i, variant := range unit.Enums[name].Variants {
				want := fmt.Sprintf("ViewVariant(%d,%s,", i+1, viewDartString(variant))
				if !strings.Contains(view, want) {
					t.Errorf("VIEW_CORPUS %s lacks enum %s variant %s", entry, name, variant)
				}
			}
		}
		for _, name := range sortedKeys(unit.Flags) {
			for i, bit := range unit.Flags[name].Variants {
				want := fmt.Sprintf("ViewVariant(%d,%s,", i, viewDartString(bit))
				if !strings.Contains(view, want) {
					t.Errorf("VIEW_CORPUS %s lacks flags %s bit %s", entry, name, bit)
				}
			}
		}
		for _, unions := range []map[string]*ir.Union{unit.Unions, unit.TableUnions} {
			for _, name := range sortedKeys(unions) {
				for i, arm := range unions[name].Variants {
					want := fmt.Sprintf("ViewVariant(%d,%s,", i+1, viewDartString(arm.Name))
					if !strings.Contains(view, want) {
						t.Errorf("VIEW_CORPUS %s lacks union %s arm %s", entry, name, arm.Name)
					}
				}
			}
		}
		for _, name := range sortedKeys(unit.Consts) {
			c := unit.Consts[name]
			want := fmt.Sprintf("ViewConstant(%s,%s,%s,%t,", viewDartString(name),
				viewDartString(viewDartFile(unit, name)), viewDartString(c.Storage), c.IsFloat)
			if !strings.Contains(view, want) {
				t.Errorf("VIEW_CORPUS %s lacks constant %s and its type", entry, name)
			}
		}
	}
	paths, err := filepath.Glob(filepath.Join(repo, "tables", "examples", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("VIEW_CORPUS examples: %v, %d schema files", err, len(paths))
	}
	u := loadUnit(t, paths...)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	name := runtimeHome(u) + "View.dart"
	view := string(files[name])
	if view == "" {
		t.Fatalf("no %s unit view: the card-only descriptors omit the unit registry", name)
	}
	for _, set := range []struct {
		kind  string
		names []string
	}{
		{"table", sortedKeys(u.Tables)},
		{"enum", sortedKeys(u.Enums)},
		{"flags", sortedKeys(u.Flags)},
		{"union", sortedKeys(u.Unions)},
		{"constant", sortedKeys(u.Consts)},
	} {
		for _, n := range set.names {
			if set.kind == "table" && u.Tables[n].IsMapEntry() {
				continue
			}
			want := fmt.Sprintf("%s %s", set.kind, n)
			if !strings.Contains(view, want) {
				t.Errorf("%s missing from %s", want, name)
			}
		}
	}
	for _, row := range []string{"WeaponConfig", "Grade", "Effect", "MaxLoadoutSlots", "designer_facing", "ui_slider"} {
		if !strings.Contains(view, row) {
			t.Errorf("declared name or tag %q missing from %s", row, name)
		}
	}
	// §8.1 exposes the element kind of arrays (a map's generated entry is a
	// table), so a generic walker does not mistake the wire wrapper for a value.
	for _, st := range u.Tables {
		if st.IsMapEntry() {
			continue
		}
		for _, f := range st.Fields {
			kind := ir.TableWireScalarKind(f)
			if f.IsMap() {
				kind = ir.TableKindTable
			} else if f.Type.Kind == ir.TBytes && !f.Type.Blob() {
				kind = ir.TableKindU8
			}
			want := fmt.Sprintf("ViewField(%s,%s,%s,%s,%t,%d,", viewDartString(f.Name),
				viewDartString(ir.TableFieldJsonKey(f)), viewDartString(ir.TableTypeSpelling(f)),
				dartU64Literal(ir.TableFieldWireId(f)), f.Type.Optional, kind)
			if !strings.Contains(view, want) {
				t.Errorf("%s.%s lacks element kind %d in the unit view", st.Name, f.Name, kind)
			}
		}
	}
	// §16's top-level text and wire entry points are needed even for a table
	// held by another table: the manifest may name RootConfig directly.
	wire := string(files[runtimeHome(u)+"Wire.dart"])
	for _, name := range sortedKeys(u.Tables) {
		st := u.Tables[name]
		if st.IsMapEntry() || !cardWireTable(st) {
			continue
		}
		fn := dartName(name)
		for _, suffix := range []string{"FromJson(", "ToJson(", "WireSave(", "WireLoad("} {
			if !strings.Contains(wire, fn+suffix) {
				t.Errorf("table %s lacks top-level %s%s", name, fn, suffix)
			}
		}
	}
}
