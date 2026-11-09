package rusttable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE STRICT READ'S ONE RULE (docs/plans/strict-only.md): a count, a length,
// an ordinal, a tag or a value past its bound sets held, and LOAD refuses the
// WHOLE batch OUT OF RANGE by name — it never lands the clamped value. These
// tests hold that rule in the REAL ARTIFACT — the exact Rust source the schema
// compiler emits for VNEW_range_widen with VOLD_range_widen in its lineage —
// and, when rustc is available under SCHEMA_SLOW=1 (functional test), compile
// and execute the generated Rust reader across R29 (writer's range on numeric
// evolution), C7 (scalar bounds), and P3 (hostile-input mutation sweep).

func rangeWidenLineageFiles(t *testing.T) (oldFiles, newFiles map[string][]byte, fixedName string) {
	t.Helper()
	u := versionUnit(t, versionSchema(t, "VNEW_range_widen"))
	old := versionUnit(t, versionSchema(t, "VOLD_range_widen"))
	var err error
	oldFiles, err = Generate(old)
	if err != nil {
		t.Fatalf("Generate(VOLD_range_widen): %v", err)
	}
	lineage := map[string][]FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(old) {
		e, ok := FixedLineageOf(old, st.Name)
		if !ok {
			t.Fatalf("no fixed lineage entry for %s in VOLD_range_widen", st.Name)
		}
		lineage[st.Name] = append(lineage[st.Name], e)
	}
	newFiles, err = GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("GenerateLineage(VNEW_range_widen): %v", err)
	}
	for name := range newFiles {
		if strings.HasSuffix(name, "_fixed.rs") {
			fixedName = name
			break
		}
	}
	if fixedName == "" {
		t.Fatal("GenerateLineage emitted no fixed module for VNEW_range_widen")
	}
	return oldFiles, newFiles, fixedName
}

// strictReadKeepsTheRule reads the rule off the emitted artifact. It returns
// nil when the strict read holds, and an error naming the first missing piece
// when it does not.
func strictReadKeepsTheRule(fixedSrc string) error {
	if !strings.Contains(fixedSrc, "report.held != 0") {
		return fmt.Errorf("the artefact has no held!=0 guard")
	}
	if !strings.Contains(fixedSrc, "return report.refuse(TableFixedReason::OutOfRange);") {
		return fmt.Errorf("the artefact does not refuse the whole batch out_of_range by name")
	}
	// the reader's own bounds count INTO held through the clamp pass, so the
	// refusal above can fire on a ranged scalar out of its declared range
	if !strings.Contains(fixedSrc, "fixed_clamp_body") || !strings.Contains(fixedSrc, "*oor +=") {
		return fmt.Errorf("the artefact has no clamp pass that counts a ranged scalar into held")
	}
	// under a lineage read the WRITER's range is the bound (§4.5): its pass
	// must be present so a value the OLD writer could not have written refuses
	if !strings.Contains(fixedSrc, "fixed_clamp_known_") {
		return fmt.Errorf("the artefact does not hold a lineage read to the writer's ranges")
	}
	return nil
}

func writeFlatSubmodule(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var mods []string
	for name, data := range files {
		keep := name == BuildVersionModule+".rs" || name == FixedRuntimeModule+".rs" ||
			name == TableEnumsModule+".rs" ||
			strings.HasSuffix(name, "_records.rs") || strings.HasSuffix(name, "_fixed.rs")
		if !keep {
			continue
		}
		rewritten := strings.ReplaceAll(string(data), "use crate::", "use super::")
		if err := os.WriteFile(filepath.Join(dir, name), []byte(rewritten), 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, strings.TrimSuffix(name, ".rs"))
	}
	sort.Strings(mods)
	var modRS strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&modRS, "pub mod %s;\npub use %s::*;\n", m, m)
	}
	if err := os.WriteFile(filepath.Join(dir, "mod.rs"), []byte(modRS.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// runRangeWidenStrictProbe is a functional test helper that compiles the real
// emitted VOLD and VNEW fixed modules with rustc and executes the R29, C7, and
// P3 strict-read checks at runtime.
func runRangeWidenStrictProbe(t *testing.T, oldFiles, newFiles map[string][]byte) (string, error) {
	t.Helper()
	dir := t.TempDir()
	writeFlatSubmodule(t, filepath.Join(dir, "vold"), oldFiles)
	writeFlatSubmodule(t, filepath.Join(dir, "vnew"), newFiles)
	mainSrc := `#![allow(dead_code, unused_imports, unused_variables, unused_mut)]
mod vold;
mod vnew;

fn is_declared_default_row(r: &vnew::RangeWidenRow) -> bool {
    r.lead == 1 && r.v == 0 && r.vals == [0, 0, 0, 0] && r.u == 10 && r.trail == 2
}

fn main() {
    let mut plan = vec![vnew::TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];

    // 1. R29: VOLD record saved in-range reads cleanly under VNEW's lineage read;
    //    forging v, vals[i], or u past VOLD's range (even inside VNEW's wider
    //    range, or u >= 2^63) refuses the whole batch as out_of_range, never
    //    returning a clamped record.
    let mut old_row = vold::RangeWidenRow::default();
    old_row.lead = 11;
    old_row.v = 50;
    old_row.vals = [10, 20, 30, 40];
    old_row.u = 500;
    old_row.trail = 22;
    let mut old_valid = vec![0u8; vold::range_widen_fixed_measure(1)];
    assert!(vold::range_widen_fixed_save(&[old_row], &mut old_valid).is_some());
    let old_block = u32::from_le_bytes(old_valid[16..20].try_into().unwrap()) as usize;
    let old_body_at = 16 + 4 + old_block + 8;

    {
        let mut values = vec![vnew::RangeWidenRow::default(); 1];
        let mut report = vnew::TableFixedReport::default();
        let got = vnew::range_widen_fixed_load(&mut values, &old_valid, &mut plan, &mut remap, &mut report);
        assert_eq!(got, Some(1), "clean VOLD must read under VNEW: {:?}", report);
        assert!(!report.damaged && !report.refused);
        assert_eq!(values[0].v, 50);
        assert_eq!(values[0].vals, [10, 20, 30, 40]);
        assert_eq!(values[0].u, 500);
    }

    // Body layout of RangeWiden: lead u32 (0..4), v i32 (4..8), vals [4]i32 (8..24), u u64 (24..32), trail u32 (32..36).
    for (label, off, width, raw) in [
        ("r29-scalar-past-writer", 4usize, 4usize, 150i64 as u64),
        ("r29-array-past-writer", 16usize, 4usize, 150i64 as u64),
        ("r29-u64-past-writer", 24usize, 8usize, 1500u64),
        ("r29-u64-high-bit", 24usize, 8usize, 1u64 << 63),
    ] {
        let mut forged = old_valid.clone();
        if width == 4 {
            forged[old_body_at + off..old_body_at + off + 4].copy_from_slice(&(raw as u32).to_le_bytes());
        } else {
            forged[old_body_at + off..old_body_at + off + 8].copy_from_slice(&raw.to_le_bytes());
        }
        let mut values = vec![vnew::RangeWidenRow::default(); 1];
        let mut report = vnew::TableFixedReport::default();
        let got = vnew::range_widen_fixed_load(&mut values, &forged, &mut plan, &mut remap, &mut report);
        assert!(got.is_none(), "{label}: expected None, got {:?}", got);
        assert!(report.refused && !report.damaged && report.reason == vnew::TableFixedReason::OutOfRange, "{label}: bad report {:?}", report);
        assert!(report.unknown == 0 && report.widened == 0 && report.held == 0, "{label}: counters must be zeroed on refusal: {:?}", report);
    }

    // 2. C7: Identity read scalar bounds — forging v or u outside VNEW's own
    //    declared range (0..200, 0..2000) refuses out_of_range whole.
    let mut new_row = vnew::RangeWidenRow::default();
    new_row.lead = 7;
    new_row.v = 120;
    new_row.vals = [5, 15, 25, 35];
    new_row.u = 1200;
    new_row.trail = 9;
    let mut new_valid = vec![0u8; vnew::range_widen_fixed_measure(1)];
    assert!(vnew::range_widen_fixed_save(&[new_row], &mut new_valid).is_some());
    let new_block = u32::from_le_bytes(new_valid[16..20].try_into().unwrap()) as usize;
    let new_body_at = 16 + 4 + new_block + 8;

    for (label, off, width, raw) in [
        ("c7-scalar-above-max", 4usize, 4usize, 250i64 as u64),
        ("c7-scalar-below-min", 4usize, 4usize, (-5i32 as u32) as u64),
        ("c7-u64-above-max", 24usize, 8usize, 2500u64),
    ] {
        let mut forged = new_valid.clone();
        if width == 4 {
            forged[new_body_at + off..new_body_at + off + 4].copy_from_slice(&(raw as u32).to_le_bytes());
        } else {
            forged[new_body_at + off..new_body_at + off + 8].copy_from_slice(&raw.to_le_bytes());
        }
        let mut values = vec![vnew::RangeWidenRow::default(); 1];
        let mut report = vnew::TableFixedReport::default();
        let got = vnew::range_widen_fixed_load(&mut values, &forged, &mut plan, &mut remap, &mut report);
        assert!(got.is_none(), "{label}: expected None, got {:?}", got);
        assert!(report.refused && !report.damaged && report.reason == vnew::TableFixedReason::OutOfRange, "{label}: bad report {:?}", report);
        assert!(report.unknown == 0 && report.widened == 0 && report.held == 0, "{label}: counters must be zeroed on refusal: {:?}", report);
    }

    // 3. P3: Hostile byte-mutation sweep — every mutation across the image
    //    produces one of the three strict verdicts (landed, damaged, or refused),
    //    never a fourth, and every damaged read resets caller rows to declared defaults.
    let mut landed = 0usize;
    let mut damaged = 0usize;
    let mut refused = 0usize;
    for at in 0..new_valid.len() {
        for b in [0x00u8, 0x01, 0x02, 0x7f, 0x80, 0xff] {
            if new_valid[at] == b {
                continue;
            }
            let mut mutated = new_valid.clone();
            mutated[at] = b;
            let mut values = vec![new_row];
            let mut report = vnew::TableFixedReport::default();
            let got = vnew::range_widen_fixed_load(&mut values, &mutated, &mut plan, &mut remap, &mut report);
            match (got, report.damaged, report.refused) {
                (Some(1), false, false) => {
                    assert_eq!(report.reason, vnew::TableFixedReason::None);
                    landed += 1;
                }
                (None, true, false) => {
                    assert!(is_declared_default_row(&values[0]));
                    damaged += 1;
                }
                (None, false, true) => {
                    assert_ne!(report.reason, vnew::TableFixedReason::None);
                    refused += 1;
                }
                other => panic!("P3 fourth answer at byte {at} (0x{b:02x}): {:?} {:?}", other, report),
            }
        }
    }
    assert!(landed > 0 && damaged > 0 && refused > 0, "P3 sweep must witness all three strict answers: landed={landed} damaged={damaged} refused={refused}");
    println!("RUST_FIXED_REMAINING_STRICT ok landed={landed} damaged={damaged} refused={refused}");
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(mainSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	build := exec.Command("rustc", "--edition", "2024", "-O", "-A", "warnings", "-o", bin, "main.rs")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("rustc build failed: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	return string(out), err
}

// TestRustFixedRemainingRowsStrict holds the strict read's one rule in the
// real artifact and (when rustc is available under SCHEMA_SLOW=1, as a
// functional test) at runtime: the emitted fixed read for the writer's-range
// row refuses a value past its bound whole, out_of_range, never landing the
// clamp, and hostile byte mutations answer only the three strict verdicts.
// This is the evidence for the Rust leg's numeric-evolution (R29),
// scalar-bounds (C7) and hostile-input (P3) cells.
func TestRustFixedRemainingRowsStrict(t *testing.T) {
	oldFiles, newFiles, fixedName := rangeWidenLineageFiles(t)
	if err := strictReadKeepsTheRule(string(newFiles[fixedName])); err != nil {
		t.Fatalf("the strict read does not hold the rule: %v", err)
	}
	t.Run("FunctionalRustc", func(t *testing.T) {
		slowtest.Gate(t, "functional test: rustc builds and runs the generated R29/C7/P3 fixed-load probe")
		if _, err := exec.LookPath("rustc"); err != nil {
			t.Skipf("rustc not on PATH: %v", err)
		}
		out, err := runRangeWidenStrictProbe(t, oldFiles, newFiles)
		if err != nil || !strings.Contains(out, "RUST_FIXED_REMAINING_STRICT ok") {
			t.Fatalf("functional probe failed: %v\n%s", err, out)
		}
	})
}

// TestRustFixedRemainingRowsStrictNegativeControl plants the pre-strict
// tolerant fault — drop the OutOfRange batch refusal so clamped values land —
// in the SAME real artifact, and requires both the artifact check and (when
// rustc is available under SCHEMA_SLOW=1) the functional probe to go red on it.
func TestRustFixedRemainingRowsStrictNegativeControl(t *testing.T) {
	oldFiles, newFiles, fixedName := rangeWidenLineageFiles(t)
	src := string(newFiles[fixedName])
	const reject = "return report.refuse(TableFixedReason::OutOfRange);"
	if !strings.Contains(src, reject) {
		t.Fatalf("negative control: the strict refusal line is not in the artefact")
	}
	planted := strings.Replace(src, reject, "{} // tolerant: the clamp landed, nothing is refused", 1)
	if planted == src {
		t.Fatal("negative control: the fault was not planted")
	}
	if err := strictReadKeepsTheRule(planted); err == nil {
		t.Fatal("negative control: the strict-read check passed on a faulted artefact")
	}
	t.Run("FunctionalRustc", func(t *testing.T) {
		slowtest.Gate(t, "functional test: rustc builds and runs the fault-planted R29/C7/P3 fixed-load probe")
		if _, err := exec.LookPath("rustc"); err != nil {
			t.Skipf("rustc not on PATH: %v", err)
		}
		sabotagedFiles := make(map[string][]byte, len(newFiles))
		maps.Copy(sabotagedFiles, newFiles)
		sabotagedFiles[fixedName] = []byte(planted)
		out, err := runRangeWidenStrictProbe(t, oldFiles, sabotagedFiles)
		if err == nil {
			t.Fatalf("negative control: functional probe passed on a fault-planted artefact:\n%s", out)
		}
	})
}
