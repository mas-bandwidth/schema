// Every public schema name a generated Rust crate carries is reachable from
// outside the crate: the packet type at the crate root, and the table layer's
// own type for that name at crate::tables (docs/SPEC-TABLES.md, the Rust
// backend's notes). A Rust table test in its own file, as the other ports keep
// theirs (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// rustNameUnits are table units whose names collide on purpose: a packet type
// and a table type under one schema name (all of them), a table enum reached
// from two files (mapdemo's Slot, blockdemo's Team), and a schema file that
// lowers to the table surface's own module name (tabledemo's Tables.schema).
var rustNameUnits = []struct{ crate, path string }{
	{"tabledemo", "../tables/examples"},
	{"mapdemo", "../tables/maps"},
	{"blockdemo", "../tables/block"},
	{"messagedemo", "../tables/messages"},
	{"backenddemo", "../tables/backend"},
	{"tblfe1", "../test/tables/FE1.schema"},
	{"tblv1", "../test/tables/V1.schema"},
	{"widedemo", "../examples-wide/Caption.schema"},
}

var rustTableTypeDecl = regexp.MustCompile(`(?m)^pub (?:struct|enum) ([A-Za-z_][A-Za-z0-9_]*)\b`)

// TestRustEverySchemaNameIsReachable generates each unit, writes an
// integration test that names every public schema name from outside the
// crate, and builds and runs it: a name that is ambiguous at the root, or
// missing from crate::tables, does not compile.
func TestRustEverySchemaNameIsReachable(t *testing.T) {
	slowtest.Gate(t, "the Rust toolchain (it builds each generated crate and a test naming its schema names)")
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	serialize := filepath.Join(filepath.Dir(repo), "serialize.rs")
	if _, err := os.Stat(filepath.Join(serialize, "Cargo.toml")); err != nil {
		t.Fatalf("the sibling serialize.rs checkout is absent at %s", serialize)
	}
	root := t.TempDir()
	var members []string
	c := New()
	for _, unit := range rustNameUnits {
		paths := []string{unit.path}
		if !strings.HasSuffix(unit.path, ".schema") {
			paths, err = filepath.Glob(filepath.Join(unit.path, "*.schema"))
			if err != nil || len(paths) == 0 {
				t.Fatalf("no schemas under %s: %v", unit.path, err)
			}
		}
		u, err := c.Load(paths)
		if err != nil {
			t.Fatalf("%s: load: %v", unit.crate, err)
		}
		files, err := c.Generate(u, "rust", Options{})
		if err != nil {
			t.Fatalf("%s: generate: %v", unit.crate, err)
		}
		dir := filepath.Join(root, unit.crate)
		for name, data := range files {
			writeFile(t, filepath.Join(dir, "src", name), data)
		}
		manifest := fmt.Sprintf("[package]\nname = %q\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[features]\ndefault = [\"block\", \"cook\"]\nblock = []\ncook = []\n\n[dependencies]\nserialize = { package = \"serialize-official\", path = %q }\n", unit.crate, serialize)
		writeFile(t, filepath.Join(dir, "Cargo.toml"), []byte(manifest))
		writeFile(t, filepath.Join(dir, "tests", "names.rs"), []byte(rustNamesTest(unit.crate, u, files)))
		members = append(members, fmt.Sprintf("%q", unit.crate))
	}
	ws := fmt.Sprintf("[workspace]\nresolver = \"3\"\nmembers = [%s]\n\n[profile.dev]\ndebug = 0\n", strings.Join(members, ", "))
	writeFile(t, filepath.Join(root, "Cargo.toml"), []byte(ws))

	cmd := exec.Command("cargo", "test", "--workspace", "--test", "names", "--quiet")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-names-target"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("a schema name is not reachable where it is documented: %v\n%s", err, out)
	}
}

// rustNamesTest is the integration test for one crate: each packet
// declaration by the spelling only the packet type has, each table at the root
// and at crate::tables, and each name both layers declare at both paths as
// two distinct types.
func rustNamesTest(crate string, u *ir.Unit, files map[string][]byte) string {
	tableTypes := map[string]bool{}
	for name, src := range files {
		if strings.HasSuffix(name, ".rs") && name != "lib.rs" && isRustTableModule(u, name) {
			for _, m := range rustTableTypeDecl.FindAllStringSubmatch(string(src), -1) {
				tableTypes[m[1]] = true
			}
		}
	}
	var lines []string
	add := func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	both := map[string]bool{}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Enum:
				add("    let _ = %s::%s::NONE; // the packet newtype keeps the root name", crate, d.Name)
				both[d.Name] = tableTypes[d.Name]
			case *ir.Struct:
				add("    let _: fn(&mut serialize::WriteStream<'_>, &%s::%s) -> %s::Result = %s::write_%s;", crate, d.Name, crate, crate, ir.RustSnake(d.Name))
				both[d.Name] = tableTypes[d.Name]
			case *ir.Union:
				add("    let _: fn(&mut serialize::WriteStream<'_>, &%s::%s) -> %s::Result = %s::write_%s;", crate, d.Name, crate, crate, ir.RustSnake(d.Name))
				both[d.Name] = tableTypes[d.Name]
			case *ir.Flags:
				add("    let _: %s::%s = 0;", crate, d.Name)
			case *ir.Const:
				add("    let _ = %s::%s;", crate, ir.RustConstName(d.Name))
			}
		}
	}
	tables := make([]string, 0, len(u.Tables))
	for name := range u.Tables {
		tables = append(tables, name)
	}
	sort.Strings(tables)
	for _, name := range tables {
		if !tableTypes[name] || both[name] {
			continue // a table the Rust leg does not carry names nothing
		}
		add("    let _: Option<%s::%s> = None;", crate, name)
		add("    let _: Option<%s::tables::%s> = None;", crate, name)
	}
	shared := make([]string, 0, len(both))
	for name, table := range both {
		if table {
			shared = append(shared, name)
		}
	}
	sort.Strings(shared)
	for _, name := range shared {
		add("    assert_ne!(TypeId::of::<%s::%s>(), TypeId::of::<%s::tables::%s>(), \"%s: one type at both paths\");", crate, name, crate, name, name)
	}
	return fmt.Sprintf(`//! Every public schema name of %s, named from outside the crate.

use std::any::TypeId;

#[test]
fn every_schema_name_is_reachable() {
%s
}
`, crate, strings.Join(lines, "\n"))
}

// isRustTableModule reports a generated module of the table layer: anything
// that is not one schema file's packet module.
func isRustTableModule(u *ir.Unit, file string) bool {
	mod := strings.TrimSuffix(file, ".rs")
	for _, f := range u.Files {
		if strings.ToLower(f.Base) == mod {
			return false
		}
	}
	return true
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
