package rusttable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// rustUnlawfulAnswer is the Rust leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful): a poison that sets one row
// `v` of the NEW type to values that are not its declared defaults, and a Rust
// boolean over `v` that holds only when the row is back at its declared
// defaults (lead = 1, trail = 2, every unlisted field its type's zero).
type rustUnlawfulAnswer struct {
	poison string
	check  string
}

var rustUnlawfulAnswers = map[string]rustUnlawfulAnswer{
	"unknown_census":      {poison: censusPoison, check: censusCheck},
	"unknown_census_many": {poison: censusPoison, check: censusCheck},
	"kind_moved": {
		poison: `v.lead = 99; v.v = 5.5; v.trail = 88;`,
		check:  `v.lead == 1 && v.v == 0.0 && v.trail == 2`,
	},
	"arm_removed": {
		poison: `v.lead = 99; v.pick.set_gamma(GammaRow { p: 77 }); v.trail = 88;`,
		check:  `v.lead == 1 && v.pick.tag() == 0 && unsafe { v.pick.arms.gamma.p } == 0 && v.trail == 2`,
	},
	"variant_removed": {
		poison: `v.lead = 99; v.tier = Tier::GOLD.0; v.trail = 88;`,
		check:  `v.lead == 1 && v.tier == 0 && v.trail == 2`,
	},
	"key_removed": {
		poison: `v.lead = 99; for j in 0..v.slots.len() { v.slots[j] = 70 + j as i32; } v.trail = 88;`,
		check:  `v.lead == 1 && v.slots.iter().all(|&s| s == 0) && v.trail == 2`,
	},
	"array_shrunk": {
		poison: `v.lead = 99; for j in 0..v.vals.len() { v.vals[j] = 70 + j as i32; } v.trail = 88;`,
		check:  `v.lead == 1 && v.vals.iter().all(|&s| s == 0) && v.trail == 2`,
	},
	"counted_shrunk": {
		poison: `v.lead = 99; v.vals_count = 2; v.vals[0] = 70; v.vals[1] = 71; v.trail = 88;`,
		check:  `v.lead == 1 && v.vals_count == 0 && v.vals.iter().all(|&s| s == 0) && v.trail == 2`,
	},
	"text_narrowed": {
		poison: `v.lead = 99; v.text[0] = b'z'; v.text[1] = b'z'; v.text_length = 2; v.trail = 88;`,
		check:  `v.lead == 1 && v.text_length == 0 && v.text.iter().all(|&b| b == 0) && v.trail == 2`,
	},
	"bytes_narrowed": {
		poison: `v.lead = 99; v.blob[0] = 0x5A; v.blob_length = 1; v.trail = 88;`,
		check:  `v.lead == 1 && v.blob_length == 0 && v.blob.iter().all(|&b| b == 0) && v.trail == 2`,
	},
	// The LAWFUL control: check is a Rust expression over `v` (row 0) that
	// formats the loaded record as the case's Values line.
	"lawful_control": {
		poison: `v.lead = 99; v.v = 5; v.trail = 88;`,
		check:  `format!("lead={} v={} trail={}", v.lead, v.v, v.trail)`,
	},
}

// TestRustFixedUnlawfulCorpus is the Rust leg's driver for the shared
// unlawful-lineage corpus. The Rust probe writes its verdict to a file and the
// driver answers what the probe wrote: the LAWFUL control's loaded values, or
// "refused" once every check held. Per UNLAWFUL case, the NEW build handed the
// OLD layout as a lineage entry refuses the OLD file before any record, moves
// no counter, and leaves every poisoned row at its declared defaults, on a
// first load and a second (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT, §5.4,
// §5.9 #30; SPEC-TABLES §6.5). The shared harness compares verdicts only; the
// Rust refusal's NAME, layout_malformed, is pinned here and is never compared
// across legs (refusal-verdict-only).
//
// The cases run one after another: every probe builds into the one shared
// cargo target dir (build/rust-versioning-target), whose lock would serialize
// them anyway.
func TestRustFixedUnlawfulCorpus(t *testing.T) {
	fixedunlawful.Run(t, "rusttable", rustUnlawfulAnswers, rustCorpusVerdict)
}

// rustCorpusVerdict builds and runs the Rust probe for one case and returns
// the verdict the probe itself wrote.
func rustCorpusVerdict(t *testing.T, c fixedunlawful.Case, a rustUnlawfulAnswer) string {
	t.Helper()

	path := filepath.Join(versionCorpus(t), c.File)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s: the reference's %s is not in the built corpus (rebuild it: make tables-fixedform-corpus): %v", c.Name, c.File, err)
	}
	verdict := filepath.Join(t.TempDir(), "verdict.txt")

	fnName := "v_" + c.Name + "_corpus_verdict"
	var tests string
	if c.Lawful {
		tests = fmt.Sprintf(`
#[test]
fn %[1]s() {
    let data = std::fs::read(%[2]q).expect("the reference's %[3]s");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    // THE ROWS ARE POISONED, NOT RESET: a load that left them alone is seen.
    for v in values.iter_mut() {
        %[4]s
    }
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert!(!report.refused, "%[5]s: the LAWFUL control refused: n={:?} {:?}", n, report);
    let n = n.expect("%[5]s: the LAWFUL control must load");
    let v = &values[0];
    std::fs::write(%[6]q, format!("loaded {}: {}", n, %[7]s)).expect("write the verdict");
}
`, fnName, path, c.File, a.poison, c.Name, verdict, a.check)
	} else {
		tests = fmt.Sprintf(`
#[test]
fn %[1]s() {
    let data = std::fs::read(%[2]q).expect("the reference's %[3]s");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    for pass in 0..2 {
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
        assert!(
            report.unknown == 0 && report.widened == 0,
            "%[5]s pass {pass}: a refusal moved a counter: unknown={} widened={}", report.unknown, report.widened
        );
        for (k, v) in values.iter().enumerate() {
            assert!(%[7]s, "%[5]s pass {pass}: row {k} is not at its declared defaults");
        }
    }
    std::fs::write(%[6]q, "refused").expect("write the verdict");
}
`, fnName, path, c.File, a.poison, c.Name, verdict, a.check)
	}

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
	name := "probe_corpus_" + c.Name
	versionWriteCrate(t, root, serialize, versionProbe{
		name:   name,
		reader: c.New,
		older:  []string{c.Old},
		src:    versionProbeSource(tests),
	})

	cmd := testproc.Command(t, "cargo", "test")
	cmd.Dir = filepath.Join(root, name)
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", c.Name, err, out)
	}
	versionAssert(t, string(out), fnName)

	got, err := os.ReadFile(verdict)
	if err != nil {
		t.Fatalf("%s: the probe wrote no verdict: %v", c.Name, err)
	}
	return string(got)
}
