package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

func TestLobsterUnitViewIncludesUnreachedFiles(t *testing.T) {
	for _, tables := range []bool{false, true} {
		t.Run(fmt.Sprintf("tables_%v", tables), func(t *testing.T) {
			dir := t.TempDir()
			sources := map[string]string{
				"Orphan.schema":     "package probe\ntype Orphan { choice Choice }\n",
				"Payload.schema":    "package probe\ntype Payload { n int32 }\n",
				"Vocabulary.schema": "package probe\nenum Mode { One, Two }\nflags Options { Fast, Safe }\nunion Choice { one Payload }\n",
				"Constants.schema":  "package probe\nconst Answer = 42\nconst Floor = -9223372036854775808\n",
			}
			if tables {
				sources["Root.schema"] = "package probe\ntable Root { n int32 }\n"
			}
			var paths []string
			// Keep input ordering stable so this also checks that the entry point
			// imports every file, rather than accidentally depending on root order.
			for _, name := range []string{"Orphan.schema", "Vocabulary.schema", "Payload.schema", "Constants.schema", "Root.schema"} {
				if src, ok := sources[name]; ok {
					p := filepath.Join(dir, name)
					if err := os.WriteFile(p, []byte(src), 0644); err != nil {
						t.Fatal(err)
					}
					paths = append(paths, p)
				}
			}
			g, err := generateLoadUnit(t, paths)
			if err != nil {
				t.Fatal(err)
			}
			tableCount := 0
			if tables {
				tableCount = 1
			}
			program := fmt.Sprintf(`import .probeView
let v = probe.UnitView()
assert v.package == "probe" and v.protocol_id != 0
assert length(v.types) == 2
assert schema_rt.lookup_table("Orphan") != nil and schema_rt.lookup_table("Payload") != nil
assert schema_rt.lookup_union("Choice") != nil
assert length(v.tables) == %d
assert length(v.constants) == 2 and v.constants[0].name == "Answer" and v.constants[0].int_value == 42
assert v.constants[1].name == "Floor" and v.constants[1].int_value == 0x8000000000000000
assert length(v.enums) == 1 and v.enums[0].name == "Mode"
assert length(v.flags) == 1 and v.flags[0].name == "Options"
assert length(v.unions) == 1 and v.unions[0].name == "Choice"
print("whole unit passed")
`, tableCount)
			if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program), 0644); err != nil {
				t.Fatal(err)
			}
			if out := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", nil))); out != "whole unit passed" {
				t.Fatal(out)
			}
			// Remove a declaration-only module from the entry point. The
			// otherwise valid unit must expose the missing constant, proving
			// that the entry point imports it rather than relying on codecs.
			entry := filepath.Join(g.dir, "probeView.lobster")
			raw, err := os.ReadFile(entry)
			if err != nil {
				t.Fatal(err)
			}
			const anchor = "import .ConstantsTable\n"
			if strings.Count(string(raw), anchor) != 1 {
				t.Fatal("constant module import must occur once")
			}
			if err := os.WriteFile(entry, []byte(strings.Replace(string(raw), anchor, "", 1)), 0644); err != nil {
				t.Fatal(err)
			}
			control := `import .probeView
let v = probe.UnitView()
assert length(v.types) == 2 and length(v.enums) == 1 and length(v.flags) == 1 and length(v.unions) == 1
assert length(v.constants) == 0
print("constant omission detected")
`
			if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(control), 0644); err != nil {
				t.Fatal(err)
			}
			if out := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", nil))); out != "constant omission detected" {
				t.Fatal(out)
			}

		})
	}
}

// Compare names, source files and declaration kinds against the compiler IR;
// totals alone can miss a duplicated registration replacing a missing one.
func TestLobsterUnitViewCorpus(t *testing.T) {
	for _, source := range []string{"examples", "bench/corpus/BenchTable.schema"} {
		t.Run(source, func(t *testing.T) {
			paths := []string{filepath.Join(root, source)}
			if source == "examples" {
				var err error
				paths, err = filepath.Glob(filepath.Join(root, source, "*.schema"))
				if err != nil || len(paths) == 0 {
					t.Fatalf("example paths: %v (%d files)", err, len(paths))
				}
			}
			g, err := generateLoadUnit(t, paths)
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			add := func(kind, name string) { want = append(want, kind+" "+name+" "+g.u.DeclFile[name]) }
			for name := range g.u.Structs {
				add("types", name)
			}
			for name, d := range g.u.Tables {
				if !d.IsMapEntry() {
					add("tables", name)
				}
			}
			for name := range g.u.Enums {
				add("enums", name)
			}
			for name := range g.u.Flags {
				add("flags", name)
			}
			for name := range g.u.Unions {
				add("unions", name)
			}
			for name := range g.u.TableUnions {
				add("unions", name)
			}
			for name := range g.u.Consts {
				add("constants", name)
			}
			var program strings.Builder
			fmt.Fprintf(&program, "import .%sView\nlet v = %s.UnitView()\n", g.u.Package, g.u.Package)
			for _, kind := range []string{"constants", "enums", "flags", "tables", "types", "unions"} {
				fmt.Fprintf(&program, "for(v.%s) d:\n    print(\"%s \" + d.name + \" \" + d.file)\n", kind, kind)
			}
			if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0644); err != nil {
				t.Fatal(err)
			}
			got := strings.Split(strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", nil))), "\n")
			sort.Strings(want)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("registry differs from IR:\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
			t.Logf("%d declarations match IR by kind, name and source file", len(want))
			if source == "examples" {
				runtimePath := filepath.Join(g.dir, "descriptors.lobster")
				raw, err := os.ReadFile(runtimePath)
				if err != nil {
					t.Fatal(err)
				}
				const sorting = "types: view_sorted_types(view_types)"
				if strings.Count(string(raw), sorting) != 1 {
					t.Fatal("type sorting anchor must occur once")
				}
				patched := strings.Replace(string(raw), sorting, "types: view_types", 1)
				if err := os.WriteFile(runtimePath, []byte(patched), 0644); err != nil {
					t.Fatal(err)
				}
				control := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", nil)))
				if control == strings.Join(want, "\n") {
					t.Fatal("removing declaration-name sorting did not break the ordered registry check")
				}
			}

		})
	}
}
