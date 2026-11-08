package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5), the
// shared cross-version vectors every leg reads (fixedvector.WriterRange): an
// OLD writer's file with one ranged value forged past that writer's range, read
// by the NEW reader whose own range is wider. The WRITER's range is the bound:
// a scalar, one element of a ranged fixed array, and a uint64 at or above 2^63
// (compared unsigned). Under strict-only (docs/plans/strict-only.md) a vector
// whose manifest counts a clamp is an out_of_range refusal of the whole batch,
// where a reader bounding by its own wider range would read it; a vector that
// counts none reads and lands the manifest's `expect=` values.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "cargo (it runs the generated NEW reader over the forged file)")
	corpus := versionCorpus(t)
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
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			leaves, err := want.Leaves()
			if err != nil {
				t.Fatal(err)
			}
			var checks strings.Builder
			for _, l := range leaves {
				expr := "values[0]." + l.Field
				if l.Index >= 0 {
					expr += fmt.Sprintf("[%d]", l.Index)
				}
				fmt.Fprintf(&checks, "    assert_eq!(%[1]s.to_string(), %[2]q, \"%[3]s: the in-range value lands\");\n",
					expr, fmt.Sprint(l.Value), l.Path)
			}
			fn := "v_" + vec.Name + "_hostile"
			verdict := fmt.Sprintf(`    let n = n.unwrap_or_else(|| panic!("the NEW reader REFUSED the forged file: {} {:?}", report.reason.name(), report));
    assert_eq!(n, 1, "the forged file carries one record, not {n}");
    assert!(!report.refused && !report.damaged, "an in-range read is not a refusal: {:?}", report);
%s`, checks.String())
			if want.Clamped > 0 {
				verdict = `    assert!(n.is_none(), "a value past the WRITER's range was read, not refused: {:?}", report);
    assert!(
        report.refused && report.reason == TableFixedReason::OutOfRange,
        "a value past the WRITER's range is an out_of_range refusal: {:?}", report
    );
    assert!(!report.damaged, "a forged value is a refusal, not damage: {:?}", report);
`
			}
			tests := fmt.Sprintf(`
#[test]
fn %[1]s() {
    let data = std::fs::read(%[2]q).expect("the reference's %[3]s");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
%[4]s    assert!(
        report.unknown == 0 && report.widened == 0,
        "a counter that must not move did: {:?}", report
    );
}
`, fn, filepath.Join(corpus, vec.File), vec.File, verdict)
			root := t.TempDir()
			name := "probe_" + vec.Name + "_hostile"
			versionWriteCrate(t, root, serialize, versionProbe{
				name:   name,
				reader: "VNEW_" + vec.Row,
				older:  []string{"VOLD_" + vec.Row},
				src:    versionProbeSource(tests),
			})
			cmd := exec.Command("cargo", "test", "--", "--nocapture")
			cmd.Dir = filepath.Join(root, name)
			cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			versionAssert(t, string(out), fn)
			t.Logf("%s", out)
		})
	}
}
