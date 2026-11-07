package gotable

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

func goLimitAnswers(t *testing.T, corpus, at, over string) (fixedlimit.Vector, fixedlimit.Vector) {
	t.Helper()
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
	return a, o
}

// goLimitProbe is the body both limits share: the at-limit file through the
// tolerant load and the parser, then the over-limit file the same two ways.
// CHECKS is the at-limit file's landed values.
const goLimitProbe = `package probe

import ("os"; "testing")

func load(t *testing.T, path string) ([]ROOT, int64, TableReport, []byte) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]ROOT, 1)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := ROOTFixedLoadTolerant(back, data, plan, &r)
	return back, n, r, data
}

func layout(t *testing.T, data []byte) string {
	n := int(tableFixedGet32(data[16:]))
	_, why := tableFixedParseLayout(data[20 : 20+n])
	if why == "" {
		return "ok"
	}
	return why
}

func counters(r TableReport) string {
	m := 0
	if r.Malformed {
		m = 1
	}
	return fmt.Sprintf("unknown=%d,kind_mismatch=%d,widened=%d,clamped=%d,duplicate=%d,malformed=%d", r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, m)
}

func TestAtLimit(t *testing.T) {
	back, n, r, data := load(t, AT)
	if n != 1 || r.Reason != "" || r.Verdict == TableOpenRefused {
		t.Fatalf("the at-limit file reads one record: n=%d reason=%q %+v", n, r.Reason, r)
	}
	if got := counters(r); got != AT_COUNTERS {
		t.Errorf("counters %s, want %s", got, AT_COUNTERS)
	}
	CHECKS
	if got := layout(t, data); got != AT_PARSE {
		t.Errorf("the at-limit layout parses as %s, want %s", got, AT_PARSE)
	}
}

func TestOverLimit(t *testing.T) {
	_, n, r, data := load(t, OVER)
	if n >= 0 || r.Verdict != TableOpenRefused || r.Reason != OVER_REASON {
		t.Fatalf("the past-limit file: n=%d verdict=%v reason=%q, want refused %s", n, r.Verdict, r.Reason, OVER_REASON)
	}
	if got := counters(r); got != OVER_COUNTERS {
		t.Errorf("a refusal moves no counter: %s, want %s", got, OVER_COUNTERS)
	}
	if got := layout(t, data); got != OVER_PARSE {
		t.Errorf("the past-limit layout parses as %s, want %s", got, OVER_PARSE)
	}
}
`

func goLimitCounters(v fixedlimit.Vector) string {
	var parts []string
	for _, k := range []string{"unknown", "kind_mismatch", "widened", "clamped", "duplicate", "malformed"} {
		parts = append(parts, fmt.Sprintf("%s=%d", k, v.Counters[k]))
	}
	return strings.Join(parts, ",")
}

func goLimitRun(t *testing.T, row, at, over string, checks func(fixedlimit.Vector) string) {
	t.Helper()
	slowtest.Gate(t, "go test (a generated probe package) (it runs the generated NEW reader over the limit vectors)")
	corpus := fixedCorpus(t)
	a, o := goLimitAnswers(t, corpus, at, over)
	older := readSchema(t, "VOLD_"+row)
	newer := readSchema(t, "VNEW_"+row)
	src := strings.NewReplacer(
		"ROOT", fixedRootName(t, older),
		"OVER_COUNTERS", fmt.Sprintf("%q", goLimitCounters(o)),
		"AT_COUNTERS", fmt.Sprintf("%q", goLimitCounters(a)),
		"OVER_REASON", fmt.Sprintf("%q", o.Reason),
		"OVER_PARSE", fmt.Sprintf("%q", o.Parse),
		"AT_PARSE", fmt.Sprintf("%q", a.Parse),
		"OVER", fmt.Sprintf("%q", filepath.Join(corpus, over)),
		"AT", fmt.Sprintf("%q", filepath.Join(corpus, at)),
		"CHECKS", checks(a),
		`import ("os"; "testing")`, `import ("fmt"; "os"; "testing")`,
	).Replace(goLimitProbe)
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("limit_%s: %v\n%s", row, err, out)
	}
}

func goLimitCheck(t *testing.T, a fixedlimit.Vector, path, expr string) string {
	t.Helper()
	want, err := a.Int(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("if int64(%[1]s) != %[2]d { t.Errorf(\"%[3]s landed %%d, want %[2]d\", %[1]s) }\n\t", expr, want, path)
}

func TestFixedLimitDepth(t *testing.T) {
	goLimitRun(t, "limit_depth", fixedlimit.DepthAt, fixedlimit.DepthOver, func(a fixedlimit.Vector) string {
		return goLimitCheck(t, a, "r0.lead", "back[0].Lead") +
			goLimitCheck(t, a, fixedlimit.DeepPath(), "back[0]"+strings.Repeat(".D", 63)+".V") +
			goLimitCheck(t, a, "r0.trail", "back[0].Trail")
	})
}

func TestFixedLimitRecord(t *testing.T) {
	goLimitRun(t, "limit_record", fixedlimit.RecordAt, fixedlimit.RecordOver, func(a fixedlimit.Vector) string {
		return goLimitCheck(t, a, "r0.lead", "back[0].Lead") +
			goLimitCheck(t, a, "r0.v", "back[0].V") +
			goLimitCheck(t, a, "r0.text_length", "back[0].TextLength") +
			goLimitCheck(t, a, "r0.text[0]", "back[0].Text[0]") +
			goLimitCheck(t, a, "r0.text[32761]", "back[0].Text[32761]") +
			goLimitCheck(t, a, "r0.text[65519]", "back[0].Text[65519]") +
			goLimitCheck(t, a, "r0.trail", "back[0].Trail")
	})
}
