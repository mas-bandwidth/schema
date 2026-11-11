package rusttable

// THE FIXED-POINT LADDER'S WIDEN INTO 128-BIT STORAGE
// (docs/FIXED-FORM-ALGORITHM.md §4, the `widen` row: "raw := LE(size,
// record+src), sign-extended from size*8 bits when sign, then PUT(dstsize,
// out+dst, raw)", and "both are exact by construction").
//
// A fixed-point kind grows into a wider storage width at the SAME scale
// (§5.1), so every lower rung — kinds 20..23 and 25..28 — can widen into the
// 128-bit kinds 24 and 29. The widened value is the writer's raw scaled
// integer, sign-extended for the signed ladder, into all sixteen destination
// bytes. This test writes the older layout with the older build, reads it with
// the newer build, and holds the landed value to the writer's in both a
// release build and a debug build with overflow checks on.
//
// THE LINEAGE IS THE LOCK'S: the two schemas are written and locked in the
// test's own directory with the repository's lock tooling, then the newer
// unit is generated from the lock's lineage. No cargo and no sibling checkout
// is involved — the fixed runtime is self-contained.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const widen128OldSchema = `package probe

fixed table Wide
{
    lead  uint32 = 1
    s8    fixed(1, 7)  | min = -1, max = 0
    s16   fixed(9, 7)  | min = -1, max = 0
    s32   fixed(25, 7) | min = -1, max = 0
    s64   fixed(57, 7) | min = -1, max = 0
    u8    ufixed(1, 7)  | min = 0, max = 1
    u16   ufixed(9, 7)  | min = 0, max = 1
    u32   ufixed(25, 7) | min = 0, max = 1
    u64   ufixed(57, 7) | min = 0, max = 1
    trail uint32 = 2
}
`

const widen128NewSchema = `package probe

fixed table Wide
{
    lead  uint32 = 1
    s8    fixed(121, 7)  | min = -1, max = 0
    s16   fixed(121, 7)  | min = -1, max = 0
    s32   fixed(121, 7)  | min = -1, max = 0
    s64   fixed(121, 7)  | min = -1, max = 0
    u8    ufixed(121, 7)  | min = 0, max = 1
    u16   ufixed(121, 7)  | min = 0, max = 1
    u32   ufixed(121, 7)  | min = 0, max = 1
    u64   ufixed(121, 7)  | min = 0, max = 1
    trail uint32 = 2
}
`

// widen128Unit parses and checks one generation of the fixture at its real
// path, so the lock tooling sees the unit it is asked to record.
func widen128Unit(t *testing.T, path, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse(path, []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: path, Name: filepath.Base(path), Base: base, Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// widen128Lineage writes the two generations, locks the first and then the
// second beside it, and reads the two-entry lineage back through the lock.
func widen128Lineage(t *testing.T) (oldU, newU *ir.Unit, lineage map[string][]FixedLineageEntry) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Wide.schema")
	if err := os.WriteFile(path, []byte(widen128OldSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	oldU = widen128Unit(t, path, widen128OldSchema)
	if _, _, err := lockfile.Update(oldU, []string{path}); err != nil {
		t.Fatalf("lock the older generation: %v", err)
	}
	if err := os.WriteFile(path, []byte(widen128NewSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	newU = widen128Unit(t, path, widen128NewSchema)
	if _, rewrote, err := lockfile.Update(newU, []string{path}); err != nil || !rewrote {
		t.Fatalf("lock the newer generation: rewrote=%v err=%v", rewrote, err)
	}
	lock, ok, err := lockfile.Open([]string{path})
	if err != nil || !ok {
		t.Fatalf("open the lock: ok=%v err=%v", ok, err)
	}
	entries := lockfile.Lineage(lock, "Wide")
	if len(entries) != 2 {
		t.Fatalf("the widened table's lineage carries %d entries, want 2", len(entries))
	}
	out := make([]FixedLineageEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, FixedLineageEntry{
			Wire:    e.Wire,
			Layout:  e.Layout,
			Record:  e.Record + 8,
			Retired: e.Retired,
			Reason:  e.Reason,
		})
	}
	return oldU, newU, map[string][]FixedLineageEntry{"Wide": out}
}

// widen128Writer is the OLDER build's probe: one row whose signed fixed-point
// fields carry a negative raw value at every rung and whose unsigned fields
// carry a positive one, saved in the old layout.
func widen128Writer(path string) string {
	return fmt.Sprintf(`mod build_version;
mod fixed_runtime;
mod wide_fixed;
mod wide_records;

pub use build_version::*;
pub use fixed_runtime::*;
pub use wide_fixed::*;
pub use wide_records::*;

fn main() {
    let row = WideRow {
        lead: 0xAAAA_AAAA,
        s8: -3,
        s16: -3,
        s32: -3,
        s64: -3,
        u8: 5,
        u16: 5,
        u32: 5,
        u64: 5,
        trail: 0xBBBB_BBBB,
    };
    let mut buffer = vec![0u8; wide_fixed_measure(1)];
    let n = wide_fixed_save(&[row], &mut buffer).expect("the older build saves");
    std::fs::write(%q, &buffer[..n]).expect("write the older file");
}
`, path)
}

// widen128Reader is the NEWER build's probe: it reads the older file and
// requires every field's raw scaled value, sign-extended into the sixteen
// bytes of the 128-bit kinds.
func widen128Reader(path string) string {
	return fmt.Sprintf(`mod build_version;
mod fixed_runtime;
mod wide_fixed;
mod wide_records;

pub use build_version::*;
pub use fixed_runtime::*;
pub use wide_fixed::*;
pub use wide_records::*;

fn main() {
    let data = std::fs::read(%q).expect("read the older file");
    let mut values = vec![WideRow::default(); 1];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = wide_fixed_load(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert_eq!(
        n, Some(1),
        "the newer build did not land the older writer's record: {:?} {:?}", n, report
    );
    assert!(!report.refused && !report.damaged, "a clean backward read is not a refusal: {:?}", report);
    let v = &values[0];
    assert!(v.lead == 0xAAAA_AAAA && v.trail == 0xBBBB_BBBB, "a neighbour moved: lead={:#x} trail={:#x}", v.lead, v.trail);
    assert!(
        v.s8.0 == -3 && v.s16.0 == -3 && v.s32.0 == -3 && v.s64.0 == -3,
        "a signed rung did not sign-extend: s8={:?} s16={:?} s32={:?} s64={:?}", v.s8, v.s16, v.s32, v.s64
    );
    assert!(
        v.u8.0 == 5 && v.u16.0 == 5 && v.u32.0 == 5 && v.u64.0 == 5,
        "an unsigned rung did not widen exactly: u8={:?} u16={:?} u32={:?} u64={:?}", v.u8, v.u16, v.u32, v.u64
    );
    println!("WIDEN128 ok");
}
`, path)
}

// widen128BuildAndRun writes one module set and its probe, builds it with the
// profile's flags and runs it.
func widen128BuildAndRun(t *testing.T, files map[string][]byte, mainSrc, name string, flags []string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	for file, data := range files {
		if strings.Contains(file, "/") || !strings.HasSuffix(file, ".rs") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, file), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(mainSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, name)
	args := append([]string{"--edition", "2024"}, flags...)
	// NAME THE SYSTEM LINKER ABSOLUTELY when one is present, so the build
	// does not depend on how the toolchain searches PATH for `cc`.
	if cc, err := exec.LookPath("cc"); err == nil {
		if abs, err := filepath.Abs(cc); err == nil {
			args = append(args, "-C", "linker="+abs)
		}
	}
	args = append(args, "-o", bin, "main.rs")
	build := exec.Command("rustc", args...)
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		return string(out), fmt.Errorf("rustc %s: %w", name, err)
	}
	run := exec.Command(bin)
	run.Dir = dir
	out, err := run.CombinedOutput()
	return string(out), err
}

// widen128Probe writes with the older build and reads with the newer one.
func widen128Probe(t *testing.T, oldU, newU *ir.Unit, lineage map[string][]FixedLineageEntry, flags []string, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	oldFiles, err := Generate(oldU)
	if err != nil {
		t.Fatalf("generate the older build: %v", err)
	}
	newFiles, err := GenerateLineage(newU, lineage)
	if err != nil {
		t.Fatalf("generate the newer build: %v", err)
	}
	if edit != nil {
		edit(newFiles)
	}
	wire := filepath.Join(t.TempDir(), "older.bin")
	if out, err := widen128BuildAndRun(t, oldFiles, widen128Writer(wire), "widen128_old", flags); err != nil {
		t.Fatalf("the older build did not write its file: %v\n%s", err, out)
	}
	return widen128BuildAndRun(t, newFiles, widen128Reader(wire), "widen128_new", flags)
}

// TestRustFixedWidenTo128IsExact holds the `widen` op to its rule for every
// fixed-point rung into kinds 24 and 29, in a release build and in a debug
// build whose overflow checks are on.
func TestRustFixedWidenTo128IsExact(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated fixed runtime)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the widened fixed read cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}
	oldU, newU, lineage := widen128Lineage(t)
	for _, profile := range []struct {
		name  string
		flags []string
	}{
		{name: "release", flags: []string{"-O"}},
		{name: "debug", flags: []string{"-C", "overflow-checks=on"}},
	} {
		t.Run(profile.name, func(t *testing.T) {
			t.Parallel()
			out, err := widen128Probe(t, oldU, newU, lineage, profile.flags, nil)
			if err != nil {
				t.Fatalf("the widened read failed in %s: %v\n%s", profile.name, err, out)
			}
			if !strings.Contains(out, "WIDEN128 ok") {
				t.Fatalf("the widened read in %s did not report success:\n%s", profile.name, out)
			}
		})
	}
}

// TestRustFixedWidenTo128NegativeControl plants the 64-bit store back into the
// generated runtime and requires the same read to go red: the value no longer
// fits the carried type, so the control proves the test watches the rule.
func TestRustFixedWidenTo128IsExactNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the fault-planted generated fixed runtime)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the negative control cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}
	oldU, newU, lineage := widen128Lineage(t)
	for _, profile := range []struct {
		name  string
		flags []string
	}{
		{name: "release", flags: []string{"-O"}},
		{name: "debug", flags: []string{"-C", "overflow-checks=on"}},
	} {
		t.Run(profile.name, func(t *testing.T) {
			t.Parallel()
			planted := false
			out, err := widen128Probe(t, oldU, newU, lineage, profile.flags, func(files map[string][]byte) {
				for name, data := range files {
					if !strings.HasSuffix(name, ".rs") {
						continue
					}
					edited := strings.Replace(string(data), widen128Exact, widen128U64Store, 1)
					if edited != string(data) {
						files[name] = []byte(edited)
						planted = true
					}
				}
			})
			if !planted {
				t.Fatal("the negative control did not find the exact widened store to replace")
			}
			if err == nil {
				t.Fatalf("the negative control stayed green in %s with the 64-bit store planted:\n%s", profile.name, out)
			}
		})
	}
}

// The exact store the emitter carries for a widened value, and the 64-bit
// store it replaces in the negative control.
const widen128Exact = `                let mut raw: u128 = 0;
                for i in 0..n {
                    raw |= u128::from(src[s + i]) << (8 * i);
                }
                if p.sign != 0 {
                    // TWO'S COMPLEMENT WIDENS BY ITS SIGN BIT, which is the
                    // whole reason a widen is an op and not a short copy.
                    let top = 1u128 << (n * 8 - 1);
                    if raw & top != 0 {
                        raw |= !((top << 1).wrapping_sub(1));
                    }
                }
                for i in 0..w {
                    image[d + i] = (raw >> (8 * i)) as u8;
                }`

const widen128U64Store = `                let mut raw: u64 = 0;
                for i in 0..n {
                    raw |= u64::from(src[s + i]) << (8 * i);
                }
                if p.sign != 0 {
                    // TWO'S COMPLEMENT WIDENS BY ITS SIGN BIT, which is the
                    // whole reason a widen is an op and not a short copy.
                    let top = 1u64 << (n * 8 - 1);
                    if raw & top != 0 {
                        raw |= !((top << 1).wrapping_sub(1));
                    }
                }
                for i in 0..w {
                    image[d + i] = (raw >> (8 * i)) as u8;
                }`
