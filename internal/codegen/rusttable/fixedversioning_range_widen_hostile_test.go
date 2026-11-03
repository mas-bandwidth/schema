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
// by the NEW reader whose own range is wider. The reader clamps to the WRITER's
// range and counts `clamped` once: a scalar, one element of a ranged fixed
// array (held the same as a scalar), and a uint64 at or above 2^63 (compared
// unsigned, so it lands the max). The answer is the manifest's `expect=`, which
// is what the C++ reference's own NEW reader landed from the same bytes.
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
				fmt.Fprintf(&checks, "    assert_eq!(%[1]s.to_string(), %[2]q, \"%[3]s: the forged value clamps to the WRITER's range\");\n",
					expr, fmt.Sprint(l.Value), l.Path)
			}
			fn := "v_" + vec.Name + "_hostile"
			tests := fmt.Sprintf(`
#[test]
fn %[1]s() {
    let data = std::fs::read(%[2]q).expect("the reference's %[3]s");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    let n = n.unwrap_or_else(|| panic!("the NEW reader REFUSED the forged file: {} {:?}", report.reason.name(), report));
    assert_eq!(n, 1, "the forged file carries one record, not {n}");
    assert!(!report.refused && !report.malformed, "a clamped read is not a refusal: {:?}", report);
%[4]s    assert_eq!(report.clamped, %[5]d, "one forged value, one clamp");
    assert!(
        report.unknown == 0 && report.kind_mismatch == 0 && report.widened == 0,
        "a counter that must not move did: {:?}", report
    );
}
`, fn, filepath.Join(corpus, vec.File), vec.File, checks.String(), want.Clamped)
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
