package rusttable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// §5.8 row 11, unknown_census: the NEW reader (Item.drop REMOVED) is handed the
// OLD writer's layout as a lineage entry. §5.1 refuses a removal, so a lawful
// older writer never carries a field the reader lacks: the entry is UNLAWFUL,
// COMPILE lays no plan for it and the load refuses layout_malformed with every
// counter at zero — unknown included, because there is no compile census
// (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH, §5.4, §5.9 #30). Item.drop sits in
// every element of a [4]Item, so the rule is held in an array of tables, not
// only at the root. After the refusal every row handed in — poisoned first — is
// back at its declared defaults (SPEC-TABLES §6.5, ruling R4), and a second load
// refuses the same way.

// rustUnlawfulProbe is the probe every unlawful-entry row shares. It loads
// `file` through a NEW build of `reader` with the `older` units' entries handed
// in as its lineage, TWICE, each time over rows `poison` set to non-default
// values, and asserts each load refuses layout_malformed, is not damage, moves
// no counter, and leaves every row handed in where `check` (a Rust boolean over
// `v`, one row) says its declared defaults are.
func rustUnlawfulProbe(t *testing.T, row, file, reader, older, poison, check string) {
	t.Helper()

	corpus := versionCorpus(t)
	path := filepath.Join(corpus, file)
	// THE CORPUS IS HERE, SO THIS ROW'S FILE MUST BE: a corpus built before the
	// row was added is a stale oracle, and a probe that cannot read its file
	// fails by name rather than reporting the row as run.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s: the reference's %s is not in the built corpus (rebuild it: make tables-fixedform-corpus): %v", row, file, err)
	}

	fnName := "v_" + row + "_unlawful_entry_refuses"
	tests := fmt.Sprintf(`
/// The UNLAWFUL lineage entry (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH and EMIT,
/// §5.4, §5.9 #30): no plan is laid, nothing is counted, and every load of a
/// file under that hash refuses layout_malformed with every row handed in at
/// its declared defaults (SPEC-TABLES §6.5, ruling R4) — the second load too.
#[test]
fn %[1]s() {
    let data = std::fs::read(%[2]q).expect("the reference's %[3]s");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    for pass in 0..2 {
        // THE ROWS ARE POISONED, NOT RESET: a refusal that left them alone
        // would pass a check against a fresh default.
        for v in values.iter_mut() {
            %[4]s
        }
        let mut report = TableFixedReport::default();
        let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
        assert!(n.is_none(), "%[5]s pass {pass}: the unlawful entry LOADED {:?} records: {:?}", n, report);
        assert!(report.refused, "%[5]s pass {pass}: a refusal by name sets refused: {:?}", report);
        assert_eq!(
            report.reason.name(), "layout_malformed",
            "%[5]s pass {pass}: the refusal is layout_malformed: {:?}", report
        );
        assert!(!report.damaged, "%[5]s pass {pass}: a refusal by name is not damage: {:?}", report);
        assert!(
            report.unknown == 0 && report.widened == 0,
            "%[5]s pass {pass}: a refusal moved a counter: unknown={} widened={}", report.unknown, report.widened
        );
        for (k, v) in values.iter().enumerate() {
            assert!(%[6]s, "%[5]s pass {pass}: row {k} is not at its declared defaults");
        }
    }
}
`, fnName, path, file, poison, row, check)

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	serialize, err := filepath.Abs(filepath.Join(repo, "..", "serialize.rs"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(serialize); err != nil {
		t.Fatalf("the Rust runtime is not beside this tree at %s: %v", serialize, err)
	}

	root := t.TempDir()
	name := "probe_" + row
	versionWriteCrate(t, root, serialize, versionProbe{
		name:   name,
		reader: reader,
		older:  []string{older},
		src:    versionProbeSource(tests),
	})

	cmd := testproc.Command(t, "cargo", "test")
	cmd.Dir = filepath.Join(root, name)
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", row, err, out)
	}
	versionAssert(t, string(out), fnName)
}

// The Census rows' poison and declared defaults: lead = 1, trail = 2, and each
// Item.a its zero default.
const (
	censusPoison = `v.lead = 99; v.trail = 88; for i in 0..4 { v.items[i].a = 5 + i as i32; }`
	censusCheck  = `v.lead == 1 && v.trail == 2 && v.items.iter().all(|it| it.a == 0)`
)

func TestFixedVersioningUnknownCensus(t *testing.T) {
	rustUnlawfulProbe(t, "unknown_census", "old_unknown_census.bin",
		"VNEW_unknown_census", "VOLD_unknown_census", censusPoison, censusCheck)
}
