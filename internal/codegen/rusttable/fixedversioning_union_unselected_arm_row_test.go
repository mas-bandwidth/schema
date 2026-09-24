package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// union_unselected_arm (docs/FIXED-FORM-VERSIONING-TESTS.md, schema#1157,
// 2026-09-19, LAWFUL AS IS): after a read that RETURNS, an arm the landed tag
// did not select is UNDEFINED — one word, for every target (Glenn, 2026-09-19:
// "as designed it is 'undefined'"). A union read DEFINES the tag and the
// SELECTED arm and nothing else. THE ROW'S CONTENT IS THE ASSERTION IT REFUSES
// TO MAKE: it does not compare pick.beta or pick.gamma. That the Rust leg
// happens to land the declared default in every unselected arm is an OBSERVATION
// AND NOT A GUARANTEE, and nothing may be relied on it. A
// future reader who "completes" this test by asserting pick.gamma.p == 0 has
// reversed a ruling and should read the issue first. The poison is carried for
// uniformity with the other legs and cannot be observed on this leg's value
// surface. (Named *_arm_row_test.go, not *_arm_test.go: Go reads a trailing
// _arm before _test.go as a GOARCH build constraint and files it under
// IgnoredGoFiles instead of running it.)

func TestFixedVersioningUnionUnselectedArm(t *testing.T) {
	corpus := versionCorpus(t)
	file := filepath.Join(corpus, "old_union_append.bin")

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	serialize, err := filepath.Abs(filepath.Join(repo, "..", "serialize.rs"))
	if err != nil {
		t.Fatal(err)
	}

	body := fmt.Sprintf(`    let data = std::fs::read(%q).expect("the reference's old_union_append.bin");
    // THE POISON (§5.7): fill every destination byte with 0x5A before the load,
    // so a landed writer's value proves the scatter ran and not that the default
    // happened to be there already. On this leg the unselected arms land the
    // declared default either way, so the poison cannot be observed.
    let mut values = vec![ROWTYPE::default(); 8];
    unsafe {
        core::ptr::write_bytes(
            values.as_mut_ptr() as *mut u8,
            0x5A,
            core::mem::size_of::<ROWTYPE>() * values.len(),
        );
    }
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    let n = n.unwrap_or_else(|| panic!(
        "the reader REFUSED the older writer's file: {} {:?}", report.reason.name(), report
    ));
    assert_eq!(n, 1, "the file carries exactly one record, not {n}");
    assert_eq!(
        values[0].pick.tag(), 1,
        "pick's tag is the writer's alpha arm (1), not {}", values[0].pick.tag()
    );
    let alpha = values[0]
        .pick
        .alpha()
        .unwrap_or_else(|| panic!("the writer's record does not select the alpha arm: tag={}", values[0].pick.tag()));
    assert_eq!(alpha.m, 7, "pick.alpha.m is the writer's 7, not {}", alpha.m);
    assert_eq!(values[0].seq, 15, "seq is the writer's 15, not {}", values[0].seq);
    assert!(
        !report.refused && report.reason == TableFixedReason::None,
        "a clean backward read is not a refusal: {:?}", report
    );
    assert!(!report.malformed, "a clean backward read is not damage: {:?}", report);
    assert!(
        report.unknown == 0 && report.kind_mismatch == 0 && report.widened == 0 && report.clamped == 0,
        "a clean backward read moved a counter: {:?}", report
    );
`, file)

	probes := []versionProbe{
		{
			name:   "probe_union_unselected_arm_compiled",
			reader: "VNEW_union_append",
			older:  []string{"VOLD_union_append"},
			src: versionProbeSource(`/// union_unselected_arm, the COMPILED column: the NEW build reads the OLD
/// writer's file through the plan compiled from its lineage entry.
#[test]
fn v_union_unselected_arm_compiled() {
` + body + `}
`),
		},
		{
			name:   "probe_union_unselected_arm_identity",
			reader: "VOLD_union_append",
			src: versionProbeSource(`/// union_unselected_arm, the IDENTITY column: the OLD build reads the same
/// file through its own hash and its own plan.
#[test]
fn v_union_unselected_arm_identity() {
` + body + `}
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
		t.Fatalf("union_unselected_arm produced no cargo output: %v", runErr)
	}
	for _, name := range []string{"v_union_unselected_arm_compiled", "v_union_unselected_arm_identity"} {
		versionAssert(t, outStr, name)
	}
}
