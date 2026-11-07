package darttable

import (
	"fmt"
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
// limits.txt's, which the C++ reference wrote from its own reader.

func dartLimitCounters(v fixedlimit.Vector) string {
	return fmt.Sprintf("report.unknown == %d && report.kindMismatch == %d && report.widened == %d && report.clamped == %d && report.duplicate == %d && (report.malformed ? 1 : 0) == %d",
		v.Counters["unknown"], v.Counters["kind_mismatch"], v.Counters["widened"], v.Counters["clamped"], v.Counters["duplicate"], v.Counters["malformed"])
}

// dartLimitHead reads the corpus file through the NEW reader and its layout
// through the reader's own parser; the checks after it see n and parsed.
const dartLimitHead = `  final data = File(FILE).readAsBytesSync();
  final n = LOAD(values, 1, data, data.length, plan, report);
  final length = ByteData.sublistView(data).getUint32(16, Endian.little);
  final layout = t.TableFixedLayout();
  final parsed = layout.parse(ByteData.sublistView(data), 20, length) ? 'ok' : name(layout.refusal);
`

func dartLimitRun(t *testing.T, row, at, over string, checks func(fixedlimit.Vector) string) {
	t.Helper()
	slowtest.Gate(t, "dart (it runs the generated NEW reader over the limit vectors)")
	corpus := fixedCorpus(t)
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
	dartBin := dartBinary(t)
	atBody := dartLimitHead + fmt.Sprintf(`  check(n == 1 && report.refused == 0 && !report.malformed, '%[1]s: the at-limit file reads one record: n=$n ${why(report)}');
  check(%[2]s, '%[1]s: a counter moved: ${why(report)}');
%[3]s  check(parsed == '%[4]s', '%[1]s: the layout parses as $parsed, want %[4]s');`, at, dartLimitCounters(a), checks(a), a.Parse)
	overBody := dartLimitHead + fmt.Sprintf(`  check(n < 0 && name(report.refused) == '%[2]s', '%[1]s: n=$n ${why(report)}, want refused %[2]s');
  check(%[3]s, '%[1]s: a refusal moved a counter: ${why(report)}');
  check(parsed == '%[4]s', '%[1]s: the layout parses as $parsed, want %[4]s');`, over, o.Reason, dartLimitCounters(o), o.Parse)
	for _, c := range []struct{ file, body string }{{at, atBody}, {over, overBody}} {
		out, err := runVersionProbe(t, dartBin, "VNEW_"+row, []string{"VOLD_" + row}, 0, filepath.Join(corpus, c.file), c.body)
		if err != nil {
			t.Fatalf("%s: %v\n%s", c.file, err, out)
		}
		t.Logf("%s: %s", c.file, out)
	}
}

func dartLimitCheck(t *testing.T, a fixedlimit.Vector, path, expr string) string {
	t.Helper()
	want, err := a.Int(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("  check(%[1]s == %[2]d, '%[3]s landed ${%[1]s}, want %[2]d');\n", expr, want, path)
}

func TestFixedLimitDepth(t *testing.T) {
	dartLimitRun(t, "limit_depth", fixedlimit.DepthAt, fixedlimit.DepthOver, func(a fixedlimit.Vector) string {
		return dartLimitCheck(t, a, "r0.lead", "values[0].lead") +
			dartLimitCheck(t, a, fixedlimit.DeepPath(), "values[0]"+strings.Repeat(".d", 63)+".v") +
			dartLimitCheck(t, a, "r0.trail", "values[0].trail")
	})
}

func TestFixedLimitRecord(t *testing.T) {
	dartLimitRun(t, "limit_record", fixedlimit.RecordAt, fixedlimit.RecordOver, func(a fixedlimit.Vector) string {
		return dartLimitCheck(t, a, "r0.lead", "values[0].lead") +
			dartLimitCheck(t, a, "r0.v", "values[0].v") +
			dartLimitCheck(t, a, "r0.text_length", "values[0].textLength") +
			dartLimitCheck(t, a, "r0.text[0]", "values[0].text[0]") +
			dartLimitCheck(t, a, "r0.text[32761]", "values[0].text[32761]") +
			dartLimitCheck(t, a, "r0.text[65519]", "values[0].text[65519]") +
			dartLimitCheck(t, a, "r0.trail", "values[0].trail")
	})
}
