package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedlimit"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE FIXED FORM'S HARD LIMITS (docs/SPEC-TABLES.md §3.4's fixed_max_depth and
// fixed_record_bound), the shared vectors every leg reads. The NEW reader holds
// the OLD layout as a lineage entry, so the plan it reads the AT-limit file
// with is compiled from a layout that passed the bound: the file reads whole,
// and a reader whose bound is one short refuses it. The PAST-limit file's
// layout is forged under the OLD hash and refused by name before any record is
// touched; the layout bytes on their own name the bound itself. Every answer is
// limits.txt's, which the C++ reference wrote from its own reader. This
// report carries no duplicate counter, so the other five are asserted.

func rustLimitCounters(v fixedlimit.Vector) string {
	return fmt.Sprintf("(report.unknown, report.kind_mismatch, report.widened, report.clamped, report.malformed as i64), (%d, %d, %d, %d, %d)",
		v.Counters["unknown"], v.Counters["kind_mismatch"], v.Counters["widened"], v.Counters["clamped"], v.Counters["malformed"])
}

const rustLimitHelpers = `
fn limit_load(path: &str) -> (Vec<ROWTYPE>, Option<usize>, TableFixedReport, Vec<u8>) {
    let data = std::fs::read(path).expect("the reference's limit vector");
    let mut values = vec![ROWTYPE::default(); 1];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    (values, n, report, data)
}

fn limit_parse(data: &[u8]) -> &'static str {
    let n = u32::from_le_bytes([data[16], data[17], data[18], data[19]]) as usize;
    match TableFixedBlock::parse(&data[20..20 + n]) {
        Ok(_) => "ok",
        Err(why) => why.name(),
    }
}
`

func rustLimitRun(t *testing.T, row, at, over string, checks func(fixedlimit.Vector) string) {
	t.Helper()
	slowtest.Gate(t, "cargo (it runs the generated NEW reader over the limit vectors)")
	corpus := versionCorpus(t)
	a, err := fixedlimit.Read(corpus, at)
	if err != nil {
		t.Fatal(err)
	}
	o, err := fixedlimit.Read(corpus, over)
	if err != nil {
		t.Fatal(err)
	}
	if a.Refused() || !o.Refused() {
		t.Fatalf("limits.txt: %s must read and %s must refuse: %+v %+v", at, over, a, o)
	}
	tests := rustLimitHelpers + fmt.Sprintf(`
#[test]
fn limit_at() {
    let (values, n, report, data) = limit_load(%[1]q);
    let n = n.unwrap_or_else(|| panic!("%[2]s: the NEW reader REFUSED the at-limit file: {} {:?}", report.reason.name(), report));
    assert_eq!(n, 1, "%[2]s carries one record, not {n}");
    assert!(!report.refused, "%[2]s: a read is not a refusal: {:?}", report);
    assert_eq!(%[3]s, "%[2]s: a counter moved");
%[4]s    assert_eq!(limit_parse(&data), %[5]q, "%[2]s: the layout parse");
}

#[test]
fn limit_over() {
    let (_values, n, report, data) = limit_load(%[6]q);
    assert!(n.is_none() && report.refused, "%[7]s: the past-limit file read: {:?} {:?}", n, report);
    assert_eq!(report.reason.name(), %[8]q, "%[7]s: the refusal's name");
    assert_eq!(%[9]s, "%[7]s: a refusal moved a counter");
    assert_eq!(limit_parse(&data), %[10]q, "%[7]s: the layout parse");
}
`, filepath.Join(corpus, at), at, rustLimitCounters(a), checks(a), a.Parse,
		filepath.Join(corpus, over), over, o.Reason, rustLimitCounters(o), o.Parse)

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
		reader: "VNEW_" + row,
		older:  []string{"VOLD_" + row},
		src:    versionProbeSource(tests),
	})
	cmd := exec.Command("cargo", "test", "--", "--nocapture")
	cmd.Dir = filepath.Join(root, name)
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", row, err, out)
	}
	versionAssert(t, string(out), "limit_at")
	versionAssert(t, string(out), "limit_over")
}

func rustLimitCheck(t *testing.T, a fixedlimit.Vector, path, expr string) string {
	t.Helper()
	want, err := a.Int(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("    assert_eq!(%[1]s as i64, %[2]d, %[3]q);\n", expr, want, path+" landed")
}

func TestFixedLimitDepth(t *testing.T) {
	rustLimitRun(t, "limit_depth", fixedlimit.DepthAt, fixedlimit.DepthOver, func(a fixedlimit.Vector) string {
		return rustLimitCheck(t, a, "r0.lead", "values[0].lead") +
			rustLimitCheck(t, a, fixedlimit.DeepPath(), "values[0]"+strings.Repeat(".d", 63)+".v") +
			rustLimitCheck(t, a, "r0.trail", "values[0].trail")
	})
}

func TestFixedLimitRecord(t *testing.T) {
	rustLimitRun(t, "limit_record", fixedlimit.RecordAt, fixedlimit.RecordOver, func(a fixedlimit.Vector) string {
		return rustLimitCheck(t, a, "r0.lead", "values[0].lead") +
			rustLimitCheck(t, a, "r0.v", "values[0].v") +
			rustLimitCheck(t, a, "r0.text_length", "values[0].text_length") +
			rustLimitCheck(t, a, "r0.text[0]", "values[0].text[0]") +
			rustLimitCheck(t, a, "r0.text[32761]", "values[0].text[32761]") +
			rustLimitCheck(t, a, "r0.text[65519]", "values[0].text[65519]") +
			rustLimitCheck(t, a, "r0.trail", "values[0].trail")
	})
}
