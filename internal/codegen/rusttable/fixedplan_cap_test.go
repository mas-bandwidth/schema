package rusttable

// THE LINEAGE'S PLAN STORAGE IS SIZED FROM THE PLAN, NOT FROM THE RECORD.
//
// A fixed reader lays one plan per locked layout down at build time. The plan an
// older entry needs is a walk of THAT entry's layout, and it can carry far more
// entries than the record has bytes: a union under an [..] array contributes a
// constant per arm and one entry per arm slot, so an array of a type carrying a
// union multiplies the arm count by the array bound while the record itself
// stays one tag byte an element.
//
// The reader below is handed an older layout whose element union has fewer arms
// than its own. The old writer's file is valid and the lineage holds its hash,
// so the load must land it; a plan capacity guessed from the record's byte size
// refuses it `plan_too_large` instead.
//
// A FUNCTIONAL TEST: it compiles and runs the generated Rust with rustc, so it
// is gated by slowtest and runs under SCHEMA_SLOW=1.

import (
	"encoding/binary"
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

const planCapOld = `package plancapold

union Pick
{
    a
    b
    c
    d
    e
}

type SmallU
{
    pick Pick
}

fixed table T
{
    units [200]SmallU
}
`

const planCapNew = `package plancapnew

union Pick
{
    a
    b
    c
    d
    e
    f
}

type SmallU
{
    pick Pick
}

fixed table T
{
    units [200]SmallU
}
`

func TestFixedLineagePlanCapCoversAnEvolvedUnionArrayNative(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated reader)")
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Fatalf("rustc is not on PATH, so the fixed reader cannot be built: %v", err)
	}

	oldU := unitFrom(t, planCapOld)
	newU := unitFrom(t, planCapNew)
	older, ok := FixedLineageOf(oldU, "T")
	if !ok {
		t.Fatal("the older unit has no lineage entry for T")
	}

	dir := t.TempDir()
	file := planCapWriteOldFile(t, dir, older)

	files, err := GenerateLineage(newU, map[string][]FixedLineageEntry{"T": {older}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	var mods []string
	for name, data := range files {
		mod := strings.TrimSuffix(name, ".rs")
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".rs") || mod == "lib" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, mod)
	}
	sort.Strings(mods)

	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports)]\n")
	for _, mod := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", mod, mod)
	}
	fmt.Fprintf(&main, `
fn main() {
    let data = std::fs::read(%q).expect("the older writer's file");
    let mut values = vec![TRow::default(); 1];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = t_fixed_load(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert_eq!(
        n,
        Some(1),
        "the newer reader refused the older writer's file: {} {:?}",
        report.reason.name(),
        report
    );
    assert!(!report.refused && !report.damaged, "a clean backward read: {:?}", report);
}
`, file)

	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "plan_cap")
	build := exec.Command("rustc", "--edition", "2024", "-o", bin, "main.rs")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the generated reader did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = dir
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("the newer reader did not load the older writer's file: %v\n%s", err, out)
	}
}

// planCapWriteOldFile writes the file the OLDER reader's own writer would: the
// pinned header, the older layout, then one all-default record stamped with the
// older hash (docs/SPEC-TABLES.md §3.4).
func planCapWriteOldFile(t *testing.T, dir string, older FixedLineageEntry) string {
	t.Helper()
	file := make([]byte, 0, int(ir.TableFixedHeaderBytes)+4+len(older.Layout)+int(older.Record))
	file = append(file, 3, 0, 0, 0, 0, 0, 0, 0) // the form byte and seven reserved zeros
	file = binary.LittleEndian.AppendUint64(file, older.Wire)
	file = binary.LittleEndian.AppendUint32(file, uint32(len(older.Layout)))
	file = append(file, older.Layout...)
	record := make([]byte, int(older.Record))
	binary.LittleEndian.PutUint64(record, older.Wire)
	file = append(file, record...)
	path := filepath.Join(dir, "old.bin")
	if err := os.WriteFile(path, file, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestFixedLineagePlanCapIsTheExactPlanMaximum pins the baked capacity to the
// walk's own number for the fixture above: the older [200]SmallU array's union
// has five arms, so each element contributes the None constant, five arm
// constants and five payload runs — 200 * 11 entries. The record is 200 bytes,
// which is where the old size-from-the-body guess landed, and the two numbers
// are what the defect is about.
func TestFixedLineagePlanCapIsTheExactPlanMaximum(t *testing.T) {
	t.Parallel()
	oldU := unitFrom(t, planCapOld)
	newU := unitFrom(t, planCapNew)
	older, ok := FixedLineageOf(oldU, "T")
	if !ok {
		t.Fatal("the older unit has no lineage entry for T")
	}
	own, ok := FixedLineageOf(newU, "T")
	if !ok {
		t.Fatal("the newer unit has no lineage entry for T")
	}
	st := newU.Tables["T"]
	if st == nil {
		t.Fatal("the newer unit declares no T")
	}
	entries := ir.TableFixedWalkRoot(st)
	block := ir.TableFixedLayoutBytes(entries)
	lineage := []FixedLineageEntry{older, own}

	const want = 200 * 11
	if got := fixedLineagePlanCap(block, entries, lineage, len(lineage)-1); got != want {
		t.Fatalf("the lineage plan maximum is %d entries, want the walk's exact %d", got, want)
	}

	files, err := GenerateLineage(newU, map[string][]FixedLineageEntry{"T": {older}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	wantConst := fmt.Sprintf("T_FIXED_PLAN_CAP: usize = %d;", want)
	found := false
	for _, data := range files {
		if strings.Contains(string(data), wantConst) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the emitted reader does not bake %q", wantConst)
	}
}
