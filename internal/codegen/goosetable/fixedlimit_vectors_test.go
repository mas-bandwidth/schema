package goosetable

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedlimit"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE FIXED FORM'S HARD LIMITS (docs/SPEC-TABLES.md §3.4's fixed_max_depth and
// fixed_record_bound), the shared vectors every leg reads. The NEW reader holds
// the OLD layout as a lineage entry, so the plan it reads the AT-limit file
// with is compiled from a layout that passed the bound: the file reads whole,
// and a reader whose bound is one short refuses it. The PAST-limit file's
// layout is forged under the OLD hash and refused by name before any record is
// touched. Every answer is limits.txt's, which the C++ reference wrote from its
// own reader. This leg's probe reports no duplicate counter and runs no layout
// parse of its own, so the file's verdict, reason and other counters are what
// it asserts.

type gooseLimitValue struct{ path, expr string }

func gooseLimitRun(t *testing.T, row, at, over string, values []gooseLimitValue) {
	t.Helper()
	slowtest.Gate(t, "goose (it runs the generated NEW reader over the limit vectors)")
	t.Parallel()
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
	older := evolutionLoad(t, "test/tables/VOLD_"+row+".schema")
	newer := evolutionLoad(t, "test/tables/VNEW_"+row+".schema")
	root := a.Root
	var print []string
	for _, v := range values {
		print = append(print, v.expr)
	}
	got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: root,
		capacity: 1, data: gooseCorpusFile(t, corpus, at), print: print})
	got.reads(t, 1)
	gooseLimitCounters(t, at, a, got)
	if len(got.printed) != len(values) {
		t.Fatalf("%s: printed %v, want %d values", at, got.printed, len(values))
	}
	for k, v := range values {
		w, err := a.Int(v.path)
		if err != nil {
			t.Fatal(err)
		}
		if got.printed[k] != strconv.FormatInt(w, 10) {
			t.Errorf("%s: %s landed %s, want %d", at, v.path, got.printed[k], w)
		}
	}
	bad := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: root,
		capacity: 1, data: gooseCorpusFile(t, corpus, over)})
	if bad.n >= 0 || bad.reason != o.Reason {
		t.Fatalf("%s: n=%d reason=%s, want refused %s: %s", over, bad.n, bad.reason, o.Reason, bad.raw)
	}
	gooseLimitCounters(t, over, o, bad)
}

func gooseLimitCounters(t *testing.T, file string, v fixedlimit.Vector, got gooseVersionResult) {
	t.Helper()
	malformed := int64(0)
	if got.malformed {
		malformed = 1
	}
	for _, c := range []struct {
		name string
		got  int64
	}{{"unknown", got.unknown}, {"kind_mismatch", got.kindMismatch}, {"widened", got.widened}, {"clamped", got.clamped}, {"malformed", malformed}} {
		if want := v.Counters[c.name]; c.got != want {
			t.Errorf("%s: %s=%d, want %d: %s", file, c.name, c.got, want, got.raw)
		}
	}
}

func TestGooseFixedLimitDepth(t *testing.T) {
	gooseLimitRun(t, "limit_depth", fixedlimit.DepthAt, fixedlimit.DepthOver, []gooseLimitValue{
		{"r0.lead", "values[0].Lead"},
		{fixedlimit.DeepPath(), "values[0]" + strings.Repeat(".D", 63) + ".V"},
		{"r0.trail", "values[0].Trail"},
	})
}

func TestGooseFixedLimitRecord(t *testing.T) {
	gooseLimitRun(t, "limit_record", fixedlimit.RecordAt, fixedlimit.RecordOver, []gooseLimitValue{
		{"r0.lead", "values[0].Lead"},
		{"r0.v", "values[0].V"},
		{"r0.text_length", "values[0].TextLength"},
		{"r0.text[0]", "values[0].Text[0]"},
		{"r0.text[32761]", "values[0].Text[32761]"},
		{"r0.text[65519]", "values[0].Text[65519]"},
		{"r0.trail", "values[0].Trail"},
	})
}
