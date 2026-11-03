package jstable

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

func jsLimitCounters(v fixedlimit.Vector) string {
	return fmt.Sprintf("r.unknown !== %d || r.kindMismatch !== %d || r.widened !== %d || r.clamped !== %d || r.duplicate !== %d || (r.malformed ? 1 : 0) !== %d",
		v.Counters["unknown"], v.Counters["kind_mismatch"], v.Counters["widened"], v.Counters["clamped"], v.Counters["duplicate"], v.Counters["malformed"])
}

func jsLimitRun(t *testing.T, row, at, over string, checks func(fixedlimit.Vector) string) {
	t.Helper()
	slowtest.Gate(t, "node (it runs the generated NEW reader over the limit vectors)")
	corpus := jsFixedCorpus(t)
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
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_"+row)
	older := jsReadSchema(t, "VOLD_"+row)
	table := jsFixedRootName(t, older)
	home := runtimeHome(jsUnitOf(t, newer))
	src := fmt.Sprintf(`
const { TableFixedParseLayout, TableFixedLayoutView } = await import("./%[1]sTable.js");
function parse(data) {
  const view = new TableFixedLayoutView();
  const n = data[16] | data[17] << 8 | data[18] << 16 | data[19] << 24;
  return TableFixedParseLayout(data, 20, n >>> 0, view) ? "ok" : TableFixedRefusalName(view.refusal);
}
function load(path) {
  const data = readFileSync(path);
  const back = [new T()]; InitT(back[0]);
  const r = new TableFixedReport();
  const plan = TNewPlan(4096, 4096);
  const n = TLoad(back, back.length, data, data.length, plan, r);
  return { data, back, r, n };
}
{
  const { data, back, r, n } = load(%[2]q);
  if (n !== 1 || r.malformed || r.refused !== 0) { fail("%[3]s: the at-limit file reads one record: n=" + n + " " + reason(r)); }
  if (%[4]s) { fail("%[3]s: a counter moved: " + reason(r)); }
%[5]s  if (parse(data) !== %[6]q) { fail("%[3]s: the layout parses as " + parse(data) + ", want %[6]s"); }
}
{
  const { data, r, n } = load(%[7]q);
  if (n >= 0 || TableFixedRefusalName(r.refused) !== %[9]q) { fail("%[8]s: n=" + n + " " + reason(r) + ", want refused %[9]s"); }
  if (%[10]s) { fail("%[8]s: a refusal moved a counter: " + reason(r)); }
  if (parse(data) !== %[11]q) { fail("%[8]s: the layout parses as " + parse(data) + ", want %[11]s"); }
}
`, home, filepath.Join(corpus, at), at, jsLimitCounters(a), checks(a), a.Parse,
		filepath.Join(corpus, over), over, o.Reason, jsLimitCounters(o), o.Parse)
	out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, src)
	if err != nil {
		t.Fatalf("limit_%s: %v\n%s", row, err, out)
	}
	t.Logf("%s", out)
}

func jsLimitCheck(t *testing.T, a fixedlimit.Vector, path, expr string) string {
	t.Helper()
	want, err := a.Int(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("  if (Number(%[1]s) !== %[2]d) { fail(%[3]q + \" landed \" + %[1]s + \", want %[2]d\"); }\n", expr, want, path)
}

func TestFixedLimitDepth(t *testing.T) {
	jsLimitRun(t, "limit_depth", fixedlimit.DepthAt, fixedlimit.DepthOver, func(a fixedlimit.Vector) string {
		return jsLimitCheck(t, a, "r0.lead", "back[0].lead") +
			jsLimitCheck(t, a, fixedlimit.DeepPath(), "back[0]"+strings.Repeat(".d", 63)+".v") +
			jsLimitCheck(t, a, "r0.trail", "back[0].trail")
	})
}

func TestFixedLimitRecord(t *testing.T) {
	jsLimitRun(t, "limit_record", fixedlimit.RecordAt, fixedlimit.RecordOver, func(a fixedlimit.Vector) string {
		return jsLimitCheck(t, a, "r0.lead", "back[0].lead") +
			jsLimitCheck(t, a, "r0.v", "back[0].v") +
			jsLimitCheck(t, a, "r0.text_length", "back[0].textLength") +
			jsLimitCheck(t, a, "r0.text[0]", "back[0].text[0]") +
			jsLimitCheck(t, a, "r0.text[32761]", "back[0].text[32761]") +
			jsLimitCheck(t, a, "r0.text[65519]", "back[0].text[65519]") +
			jsLimitCheck(t, a, "r0.trail", "back[0].trail")
	})
}
