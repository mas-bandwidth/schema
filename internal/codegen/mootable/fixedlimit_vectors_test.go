package mootable_test

import (
	"fmt"
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
// own reader. This leg's probe prints the counters a read moves (unknown,
// kind_mismatch, clamped, widened) and the refusal's name and malformed flag,
// and a string as its quoted text, so a byte of text is read out of the quotes.

func mooLimitRun(t *testing.T, row, at, over string, paths []string) {
	t.Helper()
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
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
	older, newer := readTestSchema(t, "VOLD_"+row), readTestSchema(t, "VNEW_"+row)
	g := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	mod := "VNEW_" + row + "Table"
	got := runFixedLoad(t, g, mod, a.Root, corpusFile(t, corpus, at))
	if head := fmt.Sprintf("ok 1 u%d k%d c%d w%d", a.Counters["unknown"], a.Counters["kind_mismatch"], a.Counters["clamped"], a.Counters["widened"]); got.head != head {
		t.Fatalf("%s: head %s, want %s", at, got.head, head)
	}
	for _, path := range paths {
		want, err := a.Int(path)
		if err != nil {
			t.Fatal(err)
		}
		text, ok := got.dump[path]
		if base, index, cut := strings.Cut(path, "["); cut && !ok {
			// a byte of text: the dump prints the string whole, quoted
			k, _ := strconv.Atoi(strings.TrimSuffix(index, "]"))
			s := strings.TrimSuffix(strings.TrimPrefix(got.dump[base], `"`), `"`)
			if k >= len(s) {
				t.Fatalf("%s: %s is %d bytes, too short for %s", at, base, len(s), path)
			}
			text, ok = strconv.Itoa(int(s[k])), true
		}
		if !ok || text != strconv.FormatInt(want, 10) {
			t.Errorf("%s: %s landed %q, want %d", at, path, text, want)
		}
	}
	bad := runFixedLoad(t, g, mod, o.Root, corpusFile(t, corpus, over))
	malformed := o.Counters["malformed"] != 0
	if !strings.HasPrefix(bad.head, fmt.Sprintf("refused %s nil %v ", o.Reason, malformed)) {
		t.Fatalf("%s: head %s, want refused %s, malformed %v", over, bad.head, o.Reason, malformed)
	}
}

func TestMooFixedLimitDepth(t *testing.T) {
	mooLimitRun(t, "limit_depth", fixedlimit.DepthAt, fixedlimit.DepthOver,
		[]string{"r0.lead", fixedlimit.DeepPath(), "r0.trail"})
}

func TestMooFixedLimitRecord(t *testing.T) {
	mooLimitRun(t, "limit_record", fixedlimit.RecordAt, fixedlimit.RecordOver,
		[]string{"r0.lead", "r0.v", "r0.text_length", "r0.text[0]", "r0.text[32761]", "r0.text[65519]", "r0.trail"})
}
