package rusttable

// A fixed-form file holds at least one record (docs/FIXED-FORM-ALGORITHM.md
// §5.3 step 9, docs/SPEC-TABLES.md §22.6). The vector under
// testdata/wire/fixed-empty-batch carries one header, layout and integrity
// trailer twice: once with a record, once without. The fixed load refuses the
// file with no record at every capacity, the one-record file still reads, and
// a save of zero records is refused by a debug assert (docs/rulings.json
// write-refusal). The verdict is the contract; this test compares no reason.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestRustFixedBatchHoldsARecord(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated fixed-form reader and writer)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatal(err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join(root, "test", "fixed-empty-batch", "Batch.schema"))
	if err != nil {
		t.Fatal(err)
	}
	vector := filepath.Join(root, "testdata", "wire", "fixed-empty-batch")
	none := filepath.Join(vector, "no-record.bin")
	one := filepath.Join(vector, "one-record.bin")
	for _, path := range []string{none, one} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("the vector has no %s: %v", filepath.Base(path), err)
		}
	}

	bin := compileRustFixedBatchProbe(t, unitFrom(t, string(schema)), none, one)

	out, err := runRustFixedBatchMode(t, bin, "read")
	if err != nil || !strings.Contains(out, "READ ok") {
		t.Fatalf("the empty-batch vector: %v\n%s", err, out)
	}

	// A SAVE OF ZERO RECORDS IS REFUSED. The debug assert is the write
	// contract, so a save that returns is not a refusal.
	out, err = runRustFixedBatchMode(t, bin, "save")
	if err == nil || strings.Contains(out, "SAVED") {
		t.Fatalf("a save of zero records was not refused: %v\n%s", err, out)
	}
}

// compileRustFixedBatchProbe builds the schema's fixed modules and a main that
// reads the vector or saves an empty batch. Debug assertions stay on: that is
// the build in which this leg's write contract is live.
func compileRustFixedBatchProbe(t *testing.T, u *ir.Unit, none, one string) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		keep := name == BuildVersionModule+".rs" || name == FixedRuntimeModule+".rs" || name == MessageRuntimeModule+".rs" ||
			name == VariableRuntimeModule+".rs" || name == TableEnumsModule+".rs" || name == integrityRuntimeModule+".rs" ||
			strings.HasSuffix(name, "_records.rs") || strings.HasSuffix(name, "_fixed.rs") || strings.HasSuffix(name, "_message.rs") ||
			strings.HasSuffix(name, "_table.rs") || strings.HasSuffix(name, "_variable.rs")
		if !keep {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, strings.TrimSuffix(name, ".rs"))
	}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports, unused_variables, unused_mut)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	fmt.Fprintf(&main, `
fn main() {
    let mode = std::env::args().nth(1).expect("mode");
    if mode == "read" {
        let none = std::fs::read(%q).expect("no-record.bin");
        let one = std::fs::read(%q).expect("one-record.bin");
        // The public fixed load is the read entry. Room for one value and room
        // for a batch are both that entry: a file with no record is refused
        // either way.
        for cap in [1usize, 8] {
            let mut values = vec![BatchRow::default(); cap];
            let mut plan = vec![TableFixedEntry::default(); 4096];
            let mut remap = vec![0u16; 4096];
            let mut report = TableFixedReport::default();
            let n = batch_fixed_load(&mut values, &none, &mut plan, &mut remap, &mut report);
            if n.is_some() || (!report.damaged && !report.refused) {
                println!("no-record at cap {cap}: read {n:?}, not a refusal");
                std::process::exit(1);
            }
        }
        let mut values = vec![BatchRow::default(); 8];
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut report = TableFixedReport::default();
        let n = batch_fixed_load(&mut values, &one, &mut plan, &mut remap, &mut report);
        let name_ok = values[0].name_length == 5 && values[0].name[..5] == *b"hello";
        if n != Some(1) || report.damaged || report.refused || values[0].a != 7 || !name_ok {
            println!("one-record: n={n:?} a={} length={}", values[0].a, values[0].name_length);
            std::process::exit(1);
        }
        println!("READ ok");
        return;
    }
    if mode == "save" {
        let mut buffer = vec![0u8; batch_fixed_measure(1)];
        let n = batch_fixed_save(&[], &mut buffer);
        println!("save of zero records returned {n:?}");
        println!("SAVED");
        return;
    }
    println!("unknown mode {mode}");
    std::process::exit(2);
}
`, none, one)
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	build := exec.Command("rustc", "--edition", "2024", "-C", "debug-assertions=on", "-A", "warnings", "-o", bin, "main.rs")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	return bin
}

func runRustFixedBatchMode(t *testing.T, bin, mode string) (string, error) {
	t.Helper()
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, mode)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s probe did not finish: %v\n%s", mode, ctx.Err(), out)
	}
	return string(out), err
}
