package jstable

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED
// (docs/SPEC-TABLES.md §3.4; docs/SPEC-CONFORMANCE.md, "Counted-array slots
// after a lawful fixed load"). A load's result never depends on what the
// value held before: after every fixed-form load, by the identity door or a
// lineage plan, every element slot past a counted array's live count holds
// what a fresh value holds there. The fixture is test/tables/FixedSlack.schema
// and the recipe is its header's.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// TestJSFixedSlackEmitsTheReset is the FAST pin: SlackRootFixedDecode resets
// the slots past each count after its live loop, at both depths, a table slot
// through its own Init and a scalar slot filled with the element default.
func TestJSFixedSlackEmitsTheReset(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, jsReadSchema(t, "FixedSlack"))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var body string
	for _, b := range files {
		if s := jsFn(string(b), "SlackRootFixedDecode"); s != "" {
			body = s
			break
		}
	}
	if body == "" {
		t.Fatal("no SlackRootFixedDecode in the generated unit")
	}
	for _, want := range []string{
		"for (let i = value.cellsCount; i < 3; i++) { InitSlackCell(value.cells[i]); }",
		"for (let i1 = e1.subsCount; i1 < 2; i1++) { InitSlackSub(e1.subs[i1]); }",
		"value.nums.fill(0, value.numsCount, 4);",
	} {
		if strings.Count(body, want) != 1 {
			t.Errorf("SlackRootFixedDecode must reset the slots past the count once (§3.4, THE SLOTS PAST A COUNT AFTER ANY LOAD): want %q\n%s", want, body)
		}
	}
	// the reset follows the live loop: the live elements land first
	if i, j := strings.Index(body, "value.nums[i] = view.getInt32("), strings.Index(body, "value.nums.fill(0, value.numsCount, 4);"); i < 0 || j < i {
		t.Errorf("the nums reset must follow its live loop:\n%s", body)
	}
}

// slackWrite saves the recipe's files A and B with FixedSlack's own writer.
const slackWrite = `
const { writeFileSync } = await import("node:fs");
const a = new T(); InitT(a);
a.cellsCount = 3;
for (let i = 0; i < 3; i++) {
  a.cells[i].id = 77; a.cells[i].subsCount = 2;
  for (let j = 0; j < 2; j++) { a.cells[i].subs[j].v = 77; }
}
a.numsCount = 4; for (let i = 0; i < 4; i++) { a.nums[i] = 77; }
const b = new T(); InitT(b);
b.cellsCount = 1; b.cells[0].id = 77; b.cells[0].subsCount = 1; b.cells[0].subs[0].v = 77;
b.numsCount = 1; b.nums[0] = 77;
for (const [v, file] of [[a, %[1]q], [b, %[2]q]]) {
  const buf = new Uint8Array(TMeasure(1));
  if (TSave([v], 1, buf) !== buf.length) { fail("the writer must save one record for " + file); }
  writeFileSync(file, buf);
}
`

// slackRead loads A then B into ONE value and holds it to a fresh value loaded
// from B, slot by slot; then loads A again and a refused file (B with its
// cells count forged past the bound) into the same value and holds every slot
// to the declared defaults. %[3]q names the path: "identity" asserts the plan
// image was never touched (the in-place door), "lineage" that it was.
const slackRead = `
const A = readFileSync(%[1]q), B = readFileSync(%[2]q);
function load(v, data, label) {
  const r = new TableFixedReport();
  const plan = TNewPlan(4096, 4096);
  plan.image.fill(0x5A);
  const n = TLoad([v], 1, data, data.length, plan, r);
  const touched = plan.image.some((x) => x !== 0x5A);
  if (%[3]q === "identity" && touched) { fail(label + ": expected the in-place identity door, the plan image was written"); }
  if (%[3]q === "lineage" && !touched) { fail(label + ": expected the lineage plan's image, it was never written"); }
  return [n, r];
}
const fresh = new T(); InitT(fresh);
{ const [n, r] = load(fresh, B, "fresh<-B"); if (n !== 1 || r.refused !== 0) { fail("fresh<-B: n=" + n + " " + reason(r)); } }
const v = new T(); InitT(v);
{ const [n, r] = load(v, A, "v<-A"); if (n !== 1 || r.refused !== 0) { fail("v<-A: n=" + n + " " + reason(r)); } }
if (v.cells[2].id !== 77 || v.nums[3] !== 77) { fail("file A did not land its 77s: " + show(v)); }
{ const [n, r] = load(v, B, "v<-B"); if (n !== 1 || r.refused !== 0) { fail("v<-B: n=" + n + " " + reason(r)); } }
function slots(v, label) {
  if (v.cellsCount !== 1 || v.cells[0].id !== 77 || v.cells[0].subsCount !== 1 || v.cells[0].subs[0].v !== 77 || v.numsCount !== 1 || v.nums[0] !== 77) {
    fail(label + ": the live values are not B's: " + show(v));
  }
  for (let i = 1; i < 3; i++) {
    if (v.cells[i].id !== 7) { fail(label + ": cells[" + i + "].id = " + v.cells[i].id + ", want the declared 7"); }
    if (v.cells[i].subsCount !== 0) { fail(label + ": cells[" + i + "].subsCount = " + v.cells[i].subsCount + ", want 0"); }
    for (let j = 0; j < 2; j++) {
      if (v.cells[i].subs[j].v !== 3) { fail(label + ": cells[" + i + "].subs[" + j + "].v = " + v.cells[i].subs[j].v + ", want the declared 3"); }
    }
  }
  if (v.cells[0].subs[1].v !== 3) { fail(label + ": cells[0].subs[1].v = " + v.cells[0].subs[1].v + ", want the declared 3"); }
  for (let i = 1; i < 4; i++) {
    if (v.nums[i] !== 0) { fail(label + ": nums[" + i + "] = " + v.nums[i] + ", want 0"); }
  }
}
slots(v, "A then B");
slots(fresh, "fresh from B");
if (show(v) !== show(fresh)) { fail("A then B is not a fresh value loaded from B:\n  " + show(v) + "\n  " + show(fresh)); }

// THE REFUSED LOAD: B with its cells count forged to 9 (bound 3). The record
// is the file's last 60 bytes (cells 40, nums 20), its cells count first.
const bad = Uint8Array.from(B);
const dv = new DataView(bad.buffer);
const at = bad.length - 60;
if (dv.getInt32(at, true) !== 1) { fail("the forge did not find B's cells count at " + at + ": " + dv.getInt32(at, true)); }
dv.setInt32(at, 9, true);
{ const [n, r] = load(v, A, "v<-A again"); if (n !== 1) { fail("v<-A again: n=" + n + " " + reason(r)); } }
{
  const r = new TableFixedReport();
  const n = TLoad([v], 1, bad, bad.length, TNewPlan(4096, 4096), r);
  if (n !== -1 || r.refused === 0) { fail("the forged count must refuse: n=" + n + " " + reason(r)); }
}
const defaults = new T(); InitT(defaults);
if (show(v) !== show(defaults)) { fail("a refused load must leave the declared defaults:\n  " + show(v) + "\n  " + show(defaults)); }
if (v.cells[1].id !== 7 || v.cells[0].subs[1].v !== 3 || v.nums[3] !== 0) { fail("after the refusal a slot is off its default: " + show(v)); }
`

// TestJSFixedSlackPastCountHoldsDefaults runs the recipe on both load paths:
// the in-place identity door (FixedSlack reads its own file) and a lineage
// plan (a reader that appended one field reads the same files through the
// older entry, prefill + plan + projection from the image).
func TestJSFixedSlackPastCountHoldsDefaults(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (it runs the generated reader over the FixedSlack recipe)")
	node := jsNode(t)
	src := jsReadSchema(t, "FixedSlack")
	const tail = "    nums  [..4]int32\n}"
	if strings.Count(src, tail) != 1 {
		t.Fatalf("FixedSlack.schema's SlackRoot no longer ends %q", tail)
	}
	appended := strings.Replace(src, tail, "    nums  [..4]int32\n    tail  int32 = 5\n}", 1)
	dir := t.TempDir()
	fileA, fileB := filepath.Join(dir, "A.bin"), filepath.Join(dir, "B.bin")
	if out, err := jsRunVersionProbe(t, node, src, nil, 0, "SlackRoot", fmt.Sprintf(slackWrite, fileA, fileB)); err != nil {
		t.Fatalf("writer: %v\n%s", err, out)
	}
	for _, c := range []struct {
		name   string
		reader string
		older  []string
	}{
		{"identity", src, nil},
		{"lineage", appended, []string{src}},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := jsRunVersionProbe(t, node, c.reader, c.older, 0, "SlackRoot", fmt.Sprintf(slackRead, fileA, fileB, c.name))
			if err != nil || strings.Contains(string(out), "FAIL:") {
				t.Fatalf("%s: %v\n%s", c.name, err, out)
			}
		})
	}
}
