package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMapsAndUnionArmsOnTheTableWireRust pins the Rust leg's carry of the
// maps and union-arm variable-class constructs (docs/SPEC-TABLES.md §2.3,
// §2.6, §2.8, §3), every case held BYTE FOR BYTE against the Go oracle
// (internal/tablewire, internal/tabletext):
//
//   - the map corpus (tables/maps): map[K]V with scalar, enum, nested-type and
//     array values, empty and full;
//   - the union-arm corpora (tables/arms, tables/messages): unions whose arms
//     are tables, pointers, scalars, strings, enums, arrays and nested unions,
//     arrays of unions, and optional arrays.
//
// Each wire is loaded and saved back (the bytes must not move), written as text
// (the text must be the oracle's), and read back from the text and saved (the
// bytes must be the wire again). A corpus wire is first held to the oracle
// itself: the oracle decodes it and re-encodes the same bytes, and writes the
// committed text.
func TestMapsAndUnionArmsOnTheTableWireRust(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	type maCase struct {
		name, root string
		wire, text []byte
	}
	corpus := func(name, root string, text bool) maCase {
		wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		c := maCase{name: name, root: root, wire: wire}
		if text {
			c.text, err = os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "json", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
		}
		return c
	}
	units := []struct {
		module string
		unit   *ir.Unit
		cases  []maCase
	}{
		{"mapdemo", vcLoadUnit(t, repo, "tables/maps"), []maCase{
			corpus("map_cells", "Cells", true),
			corpus("map_chunks", "Chunks", true),
		}},
		{"armdemo", vcLoadUnit(t, repo, "tables/arms"), []maCase{
			corpus("arms_gate", "Gate", false),
			corpus("arms_holder", "Holder", false),
		}},
	}

	var mods, main strings.Builder
	main.WriteString(variableClassProbeHead)
	var want []string
	for _, u := range units {
		modules, err := Generate(u.unit)
		if err != nil {
			t.Fatalf("%s: the maps/union-arm class was refused: %v", u.module, err)
		}
		dir := filepath.Join(work, u.module)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		var names []string
		for name, data := range modules {
			if name == VariableRuntimeModule+".rs" || strings.HasSuffix(name, "_table.rs") {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
				names = append(names, strings.TrimSuffix(name, ".rs"))
			}
		}
		sort.Strings(names)
		fmt.Fprintf(&mods, "#[allow(dead_code)]\nmod %s {\n", u.module)
		for _, mod := range names {
			fmt.Fprintf(&mods, "    #[path = %q]\n    pub mod %s;\n", filepath.Join(dir, mod+".rs"), mod)
		}
		mods.WriteString("}\n")
		for _, c := range u.cases {
			table := ""
			for _, f := range u.unit.Files {
				for _, st := range f.Tables {
					if st.Name == c.root {
						table = strings.ToLower(f.Base) + "_table"
					}
				}
			}
			if table == "" {
				t.Fatalf("%s: no file declares %s", c.name, c.root)
			}
			wirePath := filepath.Join(work, c.name+".wire")
			if err := os.WriteFile(wirePath, c.wire, 0o600); err != nil {
				t.Fatal(err)
			}
			jsonPath := "None"
			want = append(want, "WIRE "+c.name+" OK")
			if c.text != nil {
				p := filepath.Join(work, c.name+".json")
				if err := os.WriteFile(p, c.text, 0o600); err != nil {
					t.Fatal(err)
				}
				jsonPath = fmt.Sprintf("Some(%q)", p)
				want = append(want, "JSONW "+c.name+" OK", "JSONR "+c.name+" OK")
			}
			snake := ir.RustSnake(c.root)
			fmt.Fprintf(&main, "    run_case!(%q, %q, %s, crate::%s::%s, %s_load, %s_save, %s_to_json_checked, %s_from_json);\n",
				c.name, wirePath, jsonPath, u.module, table, snake, snake, snake, snake)
		}
	}
	main.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(work, "main.rs"), []byte(mods.String()+main.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(work, "probe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = work
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the maps/union-arm codec did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = work
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the maps/union-arm probe did not run: %v\n%s", err, out)
	}
	got := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		got[strings.TrimSpace(line)] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("the probe did not print %q", w)
		}
	}
	t.Logf("probe output (%d expected lines):\n%s", len(want), out)
}
