package rusttable

import (
	"bytes"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `forged_ordinal_both_plans`, §5.8 row 12 of docs/FIXED-FORM-VERSIONING-TESTS.md:
// the pair `VOLD_/VNEW_enum_append` as they stand, plus a forged `hostile_enum_append.bin`
// — `old_enum_append.bin` with `r0.tier` set to 4, past the WRITER's three variants.
// The SAME bytes are read TWICE: the NEW build's COMPILED plan and the OLD build's
// IDENTITY plan. Under strict-only both refuse the batch as out_of_range; a plan
// that lands None silently reads the file, which this row exists to catch.

func TestFixedVersioningForgedOrdinalBothPlans(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	corpus := versionCorpus(t)
	oldFile := filepath.Join(corpus, "old_enum_append.bin")
	raw, err := os.ReadFile(oldFile)
	if err != nil {
		t.Fatalf("read %s: %v", oldFile, err)
	}

	// THE FORGE: locate r0.tier by its declared value 3 (Gold) and its neighbour
	// r0.seq = 9 (the manifest's lawful values for the writer), assert the needle
	// occurs exactly once, and overwrite the tier ordinal with 4.
	needle := []byte{0x03, 0x09, 0x00, 0x00, 0x00}
	at := bytes.Index(raw, needle)
	if at < 0 {
		t.Fatalf("the forge needle % x is not in %s", needle, oldFile)
	}
	if bytes.Contains(raw[at+1:], needle) {
		t.Fatalf("the forge needle % x occurs more than once in %s", needle, oldFile)
	}
	raw[at] = 0x04 // the forge
	forged := filepath.Join(t.TempDir(), "hostile_enum_append.bin")
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

	body := fmt.Sprintf(`    let data = std::fs::read(%q).expect("the forged hostile_enum_append.bin");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    // STRICT-ONLY (docs/plans/strict-only.md): a value past its bound refuses
    // the whole batch as out_of_range; nothing lands and no counter moves.
    assert!(n.is_none(), "the forged file was read, not refused: {:?}", report);
    assert!(
        report.refused && report.reason == TableFixedReason::OutOfRange,
        "a forged value past its bound is an out_of_range refusal: {:?}", report
    );
    assert!(!report.damaged, "a forged value is not damage: {:?}", report);
    assert!(report.unknown == 0 && report.widened == 0, "a refusal moved a counter: {:?}", report);
`, forged)

	probes := []versionProbe{
		{
			name:   "probe_forged_ordinal_compiled",
			reader: "VNEW_enum_append",
			older:  []string{"VOLD_enum_append"},
			src: versionProbeSource(`/// forged_ordinal_both_plans, the COMPILED plan (§5.8 row 12): the NEW build
/// reads the forged bytes through the plan compiled from the OLD writer's
/// lineage entry, so the plan's variant count is the WRITER's three.
#[test]
fn v_forged_ordinal_compiled() {
` + body + `}
`),
		},
		{
			name:   "probe_forged_ordinal_identity",
			reader: "VOLD_enum_append",
			src: versionProbeSource(`/// forged_ordinal_both_plans, the IDENTITY plan (§5.8 row 12): the OLD build
/// reads the forged bytes through its own hash, so the variant count is again
/// the WRITER's three and refuses exactly as the compiled plan does.
#[test]
fn v_forged_ordinal_identity() {
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
		t.Fatalf("forged_ordinal_both_plans produced no cargo output: %v", runErr)
	}
	for _, name := range []string{"v_forged_ordinal_compiled", "v_forged_ordinal_identity"} {
		versionAssert(t, outStr, name)
	}
}
