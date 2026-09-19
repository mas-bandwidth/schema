package rusttable

// union_tag_both_plans: a forged union TAG is a different emitter site from a
// forged ENUM ORDINAL on this leg, and the identity plan's half of it was
// guarded by NOTHING. The forge sets the ONE-BYTE `pick` tag of
// `old_union_append.bin`'s single record to 9 — past the OLD writer's two arms
// and the NEW reader's three — located by the nine-byte needle
// 01 07 00 00 00 0F 00 00 00, asserted to occur EXACTLY ONCE; a search that is
// not a locator is not a forge. Both plans read the same forged bytes: the
// compiled plan (VNEW reader, VOLD in its lineage) and the identity plan (VOLD
// reader on its own hash).
//
// `clamped` is `== 1` and never `>= 1`: a leg that counts the tag in the plan's
// op AND again in the decode bounds lands 2, and the looser read is exactly what
// this row exists to catch.
//
// MEASURED 2026-09-19 on the gate rig at 380f1cad, one leg at a time, each edit
// counted to exactly 1 and each file restored and proved restored: the union tag
// bound was deleted from the emitter of ALL NINE LEGS and not one landed
// fixed-table test went red. §5.8 row 12 (forged_ordinal_both_plans) forges an
// ENUM ordinal, a separate site; dart's compiled-plan test reads only the
// compiled half. The identity half is what nothing guarded.
//
// BOTH HALVES ASSERT `clampedOnce` NOW, AND THAT IS schema#1254 CLOSED ON THIS
// LEG. Over these same forged bytes the compiled plan used to count ZERO where
// the identity plan counted ONE: the compiled plan's arms are guarded consts, a
// forged 9 matched no guard, and the tag lane was left to the PREFILL over a
// hole — the right value, written by something that looked at nothing.
//
// THE FIX IS NOT cpptable's EIGHT LINES, because this leg had no unguarded None
// const to hang them on. The union case pushes one — at the OUTER argw, with the
// WRITER'S ARM COUNT in `dstsize` — and `TableFixedOp::Const` counts a raw tag
// past that set. `Compiler::push` on this leg takes no record bound at all, so
// that one entry carries its own, through `src.get(..)`: a short record is a
// malformed verdict and never a panic.
//
// TWO NEGATIVE CONTROLS, each edit counted to exactly 1 and the file restored
// and proved byte-for-byte: the counter disabled, and the arm-count lane forced
// to 0, each leave 71 leaves with EXACTLY this row red.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixedVersioningUnionTagBothPlans(t *testing.T) {
	corpus := versionCorpus(t)
	oldFile := filepath.Join(corpus, "old_union_append.bin")
	raw, err := os.ReadFile(oldFile)
	if err != nil {
		t.Fatalf("read %s: %v", oldFile, err)
	}

	// THE FORGE: locate the single record body by its declared values — the tag
	// pick=alpha(1), pick.alpha.m = 7, seq = 15 — assert the needle occurs
	// exactly once, and overwrite the ONE-BYTE tag with 9. Nine is past the OLD
	// writer's two arms AND past the NEW reader's three, which is what makes it
	// a tag no arm lands rather than an arm the reader happens to know.
	needle := []byte{0x01, 0x07, 0x00, 0x00, 0x00, 0x0f, 0x00, 0x00, 0x00}
	at := bytes.Index(raw, needle)
	if at < 0 {
		t.Fatalf("the forge needle % x is not in %s", needle, oldFile)
	}
	if bytes.Contains(raw[at+1:], needle) {
		t.Fatalf("the forge needle % x occurs more than once in %s", needle, oldFile)
	}
	raw[at] = 0x09 // the forge: the tag is ONE byte, not four
	forged := filepath.Join(t.TempDir(), "hostile_union_append.bin")
	if err := os.WriteFile(forged, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	serialize, err := filepath.Abs(filepath.Join(repo, "..", "serialize.rs"))
	if err != nil {
		t.Fatal(err)
	}

	// clampedOnce is `== 1` and never `>= 1`: a leg that counts the tag in the
	// plan's op AND again in the decode bounds lands 2, and the looser read is
	// exactly what this row exists to catch.
	clampedOnce := `    assert_eq!(
        report.clamped, 1,
        "the bounds pass counts the forged tag EXACTLY once, never twice: {:?}", report
    );`

	body := func(clamped string) string {
		return fmt.Sprintf(`    let data = std::fs::read(%[1]q).expect("the forged hostile_union_append.bin");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    let n = n.unwrap_or_else(|| panic!(
        "the forged tag is not a refusal: {} {:?}", report.reason.name(), report
    ));
    assert!(n == 1, "the forged file reads one record, not {n}");
    assert!(
        !report.refused && report.reason == TableFixedReason::None,
        "a forged tag is not a refusal: {:?}", report
    );
    assert!(!report.malformed, "a forged tag is not damage: {:?}", report);
    assert!(
        values[0].pick.tag() == 0,
        "a tag past the arm set lands None (0), not {}", values[0].pick.tag()
    );
    assert!(
        values[0].pick.tag() != 9,
        "a tag past the arm set never lands the forged 9: {}", values[0].pick.tag()
    );
    assert!(
        values[0].pick.tag() != 1,
        "a tag past the arm set never lands the writer's 1: {}", values[0].pick.tag()
    );
    assert!(values[0].seq == 15, "the scalar after the union is untouched: {}", values[0].seq);
%[2]s
    assert!(
        report.unknown == 0 && report.kind_mismatch == 0 && report.widened == 0,
        "no other counter moved: {:?}", report
    );
`, forged, clamped)
	}

	probes := []versionProbe{
		{
			name:   "probe_union_tag_both_plans_compiled",
			reader: "VNEW_union_append",
			older:  []string{"VOLD_union_append"},
			src: versionProbeSource(`/// union_tag_both_plans, the COMPILED plan: the NEW build reads the forged
/// bytes through the plan compiled from the OLD writer's lineage entry. The
/// plan's own remap lands the hostile tag as None before the decode bound, so
/// this half asserts the LANDING and not the count (schema#1254, see header).
#[test]
fn v_union_tag_both_plans_compiled() {
` + body(clampedOnce) + `}
`),
		},
		{
			name:   "probe_union_tag_both_plans_identity",
			reader: "VOLD_union_append",
			src: versionProbeSource(`/// union_tag_both_plans, the IDENTITY plan: the OLD build reads the forged
/// bytes through its own hash. This is the half nothing guarded: delete the
/// emitter's union tag bound and this probe, and only this probe, goes red.
#[test]
fn v_union_tag_both_plans_identity() {
` + body(clampedOnce) + `}
`),
		},
	}

	root := t.TempDir()
	members := make([]string, 0, len(probes))
	for _, p := range probes {
		versionWriteCrate(t, root, serialize, p)
		members = append(members, fmt.Sprintf("%q", p.name))
	}
	ws := fmt.Sprintf(`[workspace]
resolver = "3"
members = [%s]

[profile.dev]
debug = 0
`, strings.Join(members, ", "))
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte(ws), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("cargo", "test", "--workspace", "--no-fail-fast")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, runErr := cmd.CombinedOutput()
	outStr := string(out)
	if outStr == "" {
		t.Fatalf("union_tag_both_plans produced no cargo output: %v", runErr)
	}
	for _, name := range []string{"v_union_tag_both_plans_compiled", "v_union_tag_both_plans_identity"} {
		versionAssert(t, outStr, name)
	}
}
