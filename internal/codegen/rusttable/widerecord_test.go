package rusttable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/rust"
	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideRecordDir holds a record past 4 GiB (docs/SPEC-TABLES.md §11): Huge is
// three 2 GiB parts and an int32 whose offset is past 4 GiB.
const wideRecordDir = "../../../testdata/wire/tables/storage-cap/wide"

// TestWideRecordCompilesRust: the whole generated crate for the wide unit,
// block and cook included, compiles, and every stored size and offset it
// carries equals the layout model's: the record's size_of and offset_of, the
// cook descriptor, the block descriptor and the unit view. Nothing builds a
// Huge.
func TestWideRecordCompilesRust(t *testing.T) {
	slowtest.Gate(t, "cargo (the generated wide-record crate)")
	memceiling.Guard(t, memceiling.Default)
	out, err := runWideRecordRust(t, "")
	if err != nil {
		t.Fatalf("wide-record probe: %v\n%s", err, out)
	}
}

// TestWideRecordNegativeControlRust narrows the cook descriptor's size back to
// 32 bits, and the crate must stop compiling: a record past 4 GiB does not fit.
func TestWideRecordNegativeControlRust(t *testing.T) {
	slowtest.Gate(t, "cargo (the generated wide-record crate)")
	memceiling.Guard(t, memceiling.Default)
	out, err := runWideRecordRust(t, "    pub size: i64,\n    pub align: i64,\n")
	if err == nil {
		t.Fatalf("the crate compiled with the cook size narrowed to i32:\n%s", out)
	}
	if !strings.Contains(out, "literal out of range for `i32`") {
		t.Fatalf("the narrowed crate failed for another reason:\n%s", out)
	}
}

// runWideRecordRust generates the wide unit, writes a probe binary over the
// crate and runs it. narrow, when set, is a line of the cook runtime put back
// at 32 bits before the build.
func runWideRecordRust(t *testing.T, narrow string) (string, error) {
	t.Helper()
	schema, err := os.ReadFile(filepath.Join(wideRecordDir, "Wide.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	huge := ir.RecordLayout(u, u.Tables["Huge"])
	tail := huge.FieldByName("tail")
	blocks := ir.Blocks(u)
	var proj *ir.BlockLayout
	for _, bl := range blocks.Tables {
		if bl.Table.Name == "Huge" {
			proj = bl
		}
	}
	if proj == nil {
		t.Fatal("Huge has no block form")
	}
	projTail := proj.Projection.FieldByName("tail")
	if huge.Size <= 1<<32 || tail.Offset <= 1<<32 {
		t.Fatalf("Huge is %d bytes with tail at %d; the unit no longer reaches past 4 GiB", huge.Size, tail.Offset)
	}
	// the whole crate, assembled as compiler/target_rust.go assembles it
	files, err := rust.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	shadowed := rust.ShadowedTypes(files, tables)
	maps.Copy(files, tables)
	lib, err := rust.Lib(u, Modules(tables), shadowed)
	if err != nil {
		t.Fatal(err)
	}
	files["lib.rs"] = lib
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	serialize := filepath.Join(repo, "..", "serialize.rs")
	if _, err := os.Stat(serialize); err != nil {
		t.Fatalf("the Rust runtime is not beside this tree at %s: %v", serialize, err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if narrow != "" && name == "cook_runtime.rs" {
			if !strings.Contains(string(data), narrow) {
				t.Fatalf("the negative control matched nothing in cook_runtime.rs: %q", narrow)
			}
			data = []byte(strings.Replace(string(data), narrow, strings.ReplaceAll(narrow, "i64", "i32"), 1))
		}
		if err := os.WriteFile(filepath.Join(src, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := fmt.Sprintf(`[package]
name = "wideprobe"
version = "0.0.0"
edition = "2024"

[features]
default = ["block", "cook"]
block = []
cook = []

[dependencies]
serialize = { package = "serialize-official", path = %q }

[profile.dev]
debug = 0
`, serialize)
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf(`#![allow(dead_code)]
use wideprobe::*;

fn main() {
    let mut failures = 0;
    macro_rules! check {
        ($ok:expr, $name:expr) => { if !$ok { eprintln!("FAIL: {}", $name); failures += 1; } };
    }
    const SIZE: u64 = %[1]d;
    const TAIL: u64 = %[2]d;
    const PROJECTION: u64 = %[3]d;
    const PROJECTION_TAIL: u64 = %[4]d;
    check!(core::mem::size_of::<HugeRow>() as u64 == SIZE, "HugeRow size_of");
    check!(core::mem::offset_of!(HugeRow, tail) as u64 == TAIL, "HugeRow.tail offset_of");
    let cook = huge_cook_info();
    check!(cook.size == SIZE as i64, format!("cook size {}", cook.size));
    check!(cook.fields[1].offset == TAIL as i64, format!("cook tail offset {}", cook.fields[1].offset));
    let block = HugeBlock::type_info();
    check!(block.size == PROJECTION, format!("block size {}", block.size));
    check!(block.fields[1].offset == PROJECTION_TAIL, format!("block tail offset {}", block.fields[1].offset));
    let view = unit_view();
    let huge = view.types.iter().chain(view.tables.iter()).find(|t| t.name == "Huge").expect("Huge in the unit view");
    check!(huge.type_info.size == SIZE, format!("view size {}", huge.type_info.size));
    check!(huge.type_info.fields[1].offset == TAIL, format!("view tail offset {}", huge.type_info.fields[1].offset));
    if failures != 0 { std::process::exit(1); }
    println!("wide record ok");
}
`, huge.Size, tail.Offset, proj.Projection.Size, projTail.Offset)
	if err := os.WriteFile(filepath.Join(src, "main.rs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cargo", "run", "--quiet", "-j", "2")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-widerecord-target"))
	out, err := cmd.CombinedOutput()
	if err == nil && !strings.Contains(string(out), "wide record ok") {
		return string(out), fmt.Errorf("the probe did not report")
	}
	return string(out), err
}
