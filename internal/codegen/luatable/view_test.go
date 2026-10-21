package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaViewDescriptorsMatchGo holds the Lua table module's descriptors to
// docs/SPEC-TABLES.md §8.1, with internal/viewlisting's listing of the same
// unit as the oracle for every identity column and the IR for the descriptor
// columns the listing does not print:
//
//   - every declared table's every field carries its text key, type spelling,
//     wire id and optional flag exactly as the listing states them;
//   - beside them it carries the 8.1 column set: kind and element kind,
//     element size, counted, the UnboundedContainer tell (array_bound 0 on a
//     map or list), Unbounded for unbounded text, array_bound, the key
//     vocabulary, and the `<field>_present` companion of a ?T;
//   - the registry names every table in the closure, map entries included
//     (§8.3).
//
// It generates every unit of the tables conformance manifest, runs a Lua probe
// under the pinned interpreter in a t.TempDir() and compares its dump line by
// line. A unit the Lua target refuses by name is listed, not failed: the
// refusal is the target's answer, and no descriptor exists to compare.
func TestLuaViewDescriptorsMatchGo(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	keys := make([]string, 0, len(units))
	for key := range units {
		keys = append(keys, key)
	}
	sortStrings(keys)
	loaded := 0
	var refused []string
	for _, key := range keys {
		var paths []string
		for _, p := range units[key] {
			paths = append(paths, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, paths...)
		if len(u.Tables) == 0 {
			continue
		}
		if len(ir.TableWideFields(u)) > 0 {
			refused = append(refused, fmt.Sprintf("%s: carries the wide kinds the listing does not spell yet", key))
			continue
		}
		files, err := Generate(u)
		if err != nil {
			if !strings.Contains(err.Error(), "Lua") {
				t.Errorf("unit %s: a refusal that does not name the Lua backend: %v", key, err)
			}
			refused = append(refused, fmt.Sprintf("%s: %v", key, err))
			continue
		}
		// The listing is the pin: table and field identity, the text key, the
		// wire id and optional all come from it (docs/SPEC-TABLES.md §8.3,
		// §8.7), so the dump cannot invent them.
		listing := viewlisting.Listing(u)
		want := viewLines(u, listing)
		if len(want) == 0 {
			t.Errorf("unit %s: the listing names no table field to compare", key)
			continue
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(viewProbe(ModuleName(u))), 0o644); err != nil {
			t.Fatal(err)
		}
		out := string(luarun.Run(t, dir, "main.lua", nil))
		got := map[string]string{}
		var order []string
		for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
			if line == "" {
				continue
			}
			id := viewLineID(line)
			got[id] = line
			order = append(order, id)
		}
		for _, id := range sortedKeys(want) {
			w, ok := got[id]
			if !ok {
				t.Errorf("unit %s: the Lua dump has no descriptor for %s", key, strings.Replace(id, "\x00", ".", 1))
				continue
			}
			if w != want[id] {
				t.Errorf("unit %s: descriptor differs\n  listing: %s\n  lua:     %s", key, want[id], w)
			}
		}
		// A declared table's row count is what the listing declared: an extra
		// row in the dump means the module writes a field the schema does not.
		for _, line := range order {
			if _, ok := want[line]; ok {
				continue
			}
			table := strings.Fields(line)[0]
			if _, declared := u.Tables[table]; declared && !u.Tables[table].IsMapEntry() {
				t.Errorf("unit %s: the Lua dump carries an undeclared row: %s", key, line)
			}
		}
		loaded++
	}
	for _, r := range refused {
		t.Logf("refused by name: %s", r)
	}
	if loaded == 0 {
		t.Fatalf("no manifest unit generated a table module (%d refused)", len(refused))
	}
	t.Logf("%d of %d manifest units compared", loaded, len(keys))
}

// viewLines builds each declared table field's expected dump line. The
// identity tokens are taken VERBATIM from internal/viewlisting's listing; the
// §8.1 columns beside them are derived from the IR, which is what the
// descriptor must agree with.
func viewLines(u *ir.Unit, listing string) map[string]string {
	want := map[string]string{}
	for line := range strings.SplitSeq(listing, "\n") {
		f := strings.Fields(line)
		if len(f) < 8 || f[0] != "table" || f[2] != "field" {
			continue
		}
		table, field := f[1], f[3]
		st, ok := u.Tables[table]
		if !ok || st.IsMapEntry() {
			continue
		}
		var fd *ir.Field
		for _, candidate := range st.Fields {
			if candidate.Name == field {
				fd = candidate
				break
			}
		}
		if fd == nil {
			continue
		}
		id := table + "\x00" + field
		want[id] = fmt.Sprintf("table %s field %s %s %s %s %s kind=%d elem_kind=%d elem_size=%d counted=%v UnboundedContainer=%v Unbounded=%v array_bound=%d key_type_name=%s keys=%s present=%s",
			table, field, f[4], f[5], f[6], f[7],
			ir.TableWireFieldKind(fd), ir.TableWireElemKind(fd), viewElemSize(u, fd), viewCounted(fd),
			fd.IsList() || fd.IsMap(), fd.Type.TextUnbounded(), viewArrayBound(fd),
			fd.KeyEnum, viewKeys(fd), viewPresent(fd))
	}
	return want
}

// viewLineID is a dump line's table and field, the key the comparison maps by.
func viewLineID(line string) string {
	f := strings.Fields(line)
	if len(f) < 4 {
		return line
	}
	return f[1] + "\x00" + f[3]
}

// viewElemSize is §8.1's element pitch: the element's storage size for an
// array or map, the scalar's wire width otherwise.
func viewElemSize(u *ir.Unit, f *ir.Field) int64 {
	switch {
	case f.IsMap():
		return ir.RecordLayout(u, f.MapEntry).Size
	case f.Array != ir.ArrayNone:
		size, _ := ir.ListElementLayout(u, f)
		return size
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString:
		return 1
	case f.Type.Kind == ir.TWString:
		return 2
	}
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok && !f.Type.Pointer {
			return ir.RecordLayout(u, r).Size
		}
	}
	return int64(ir.TableKindWidth(ir.TableWireFieldKind(f)))
}

// viewCounted is §8.1's counted column: a fixed array has no count companion;
// a counted array, list, map, string or byte buffer has one.
func viewCounted(f *ir.Field) bool {
	if f.IsList() || f.IsMap() {
		return true
	}
	return f.Array == ir.ArrayCounted ||
		(!f.Type.Pointer && !f.Type.TextUnbounded() &&
			(f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString))
}

// viewArrayBound is §8.1's array_bound, 0 on a container with no declared
// bound (§2.9).
func viewArrayBound(f *ir.Field) int64 {
	switch {
	case f.IsMap() || f.IsList():
		return 0
	case f.KeyEnum != "" && f.KeyEnumRef != nil:
		return f.KeyEnumRef.Max
	case f.Array != ir.ArrayNone:
		return f.ArrayBound
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString:
		return f.Type.Size
	}
	return 0
}

// viewKeys renders the enum-keyed array's key vocabulary: each variant's name
// and the wire id it rides under, in declared order (§2.4, §8.1).
func viewKeys(f *ir.Field) string {
	if f.KeyEnumRef == nil {
		return ""
	}
	parts := make([]string, len(f.KeyEnumRef.Variants))
	for i, v := range f.KeyEnumRef.Variants {
		parts[i] = fmt.Sprintf("%s=%016x", v, ir.TableWireId(f.KeyEnumRef.VariantWireName(i)))
	}
	return strings.Join(parts, ",")
}

// viewPresent is §8.1's presence companion, the `<field>_present` bool a ?T
// stores beside its value.
func viewPresent(f *ir.Field) string {
	if f.Type.Optional {
		return f.Name + "_present"
	}
	return ""
}

// viewProbe is the Lua half: one line per field of every table the registry
// names, in the listing's column set and the §8.1 names beside it.
func viewProbe(module string) string {
	return `local T = require("` + module + `")
local function keys(k)
  local parts = {}
  for _, v in ipairs(k) do
    parts[#parts + 1] = string.format("%s=%016x", v[1], v[2])
  end
  return table.concat(parts, ",")
end
for _, name in ipairs(T.Registry.tables) do
  local d = T.Tables[name]
  for _, f in ipairs(d.fields) do
    print(string.format("table %s field %s json=%s type=%s id=%016x optional=%s kind=%d elem_kind=%d elem_size=%d counted=%s UnboundedContainer=%s Unbounded=%s array_bound=%d key_type_name=%s keys=%s present=%s",
      name, f.name, f.key, f.type_name, f.id, tostring(f.optional), f.kind, f.elem_kind, f.elem_size,
      tostring(f.counted), tostring(f.UnboundedContainer), tostring(f.Unbounded), f.array_bound, f.key_type_name, keys(f.keys), f.present))
  end
end
`
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}
