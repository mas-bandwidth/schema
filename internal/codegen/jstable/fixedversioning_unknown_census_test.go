package jstable

// §5.8 row 11, `unknown_census`, NEW-READS-OLD on the JavaScript leg. The OLD
// build wrote `old_unknown_census.bin` — one `Census`, `lead = 1`, `trail = 2`,
// four `items` with `a = 10 + i` and `drop = 900 + i` — and the NEW build removes
// `Item.drop`. §5.1 refuses a removal, so a lawful older writer never carries a
// field the reader lacks: the handed-in lineage entry is UNLAWFUL. COMPILE lays
// no plan for it and the load refuses `layout_malformed` with every counter at
// zero — `unknown` included, because there is no compile census
// (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH, §5.4, §5.9 #30). The removed field
// sits inside every element of an array of tables, so the rule is held in that
// slot too. After the refusal every row handed in is back at its DECLARED
// DEFAULTS (SPEC-TABLES §6.5) — not untouched, not poisoned — and a second load
// refuses the same way.

import (
	"fmt"
	"path/filepath"
	"testing"
)

// jsUnlawfulProbe is the probe body every unlawful-entry row shares: two loads
// over POISONED rows, each refusing `layout_malformed` by name with every
// counter at zero, each leaving every row the caller handed in at its declared
// defaults. `poison` sets non-default values in back[k]; `atDefaults` is a JS
// expression true when back[k] holds its declared defaults.
func jsUnlawfulProbe(file, row, poison, atDefaults string) string {
	return fmt.Sprintf(`
const data = readFileSync(%[1]q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const plan = TNewPlan(4096, 4096);
for (let pass = 0; pass < 2; pass++) {
  for (let k = 0; k < back.length; k++) { %[3]s }
  // THE POISON (§5.7, §5.9 #17): a prefill that ran and one that never did look
  // alike over zeros.
  plan.image.fill(0x5A);
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, data, data.length, plan, r);
  if (n !== -1) { fail(%[2]q + " pass " + pass + ": the UNLAWFUL entry loaded n=" + n + " " + reason(r)); }
  if (r.refused !== TableFixedRefusal.LayoutMalformed || r.damaged) {
    fail(%[2]q + " pass " + pass + ": an unlawful lineage entry refuses layout_malformed by name: " + reason(r));
  }
  if (r.unknown !== 0 || r.mismatch !== 0 || r.widened !== 0 || r.oor !== 0 || r.repeated !== 0) {
    fail(%[2]q + " pass " + pass + ": a refusal moves no counter, and there is no compile census: " + reason(r));
  }
  for (let k = 0; k < back.length; k++) {
    if (!(%[4]s)) {
      fail(%[2]q + " pass " + pass + ": row " + k + " is not at its declared defaults after the refusal: " + show(back[k]));
    }
  }
}
`, file, row, poison, atDefaults)
}

func TestJSFixedVersioningUnknownCensus(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_unknown_census")
	older := jsReadSchema(t, "VOLD_unknown_census")
	table := jsFixedRootName(t, older)
	src := jsUnlawfulProbe(filepath.Join(corpus, "old_unknown_census.bin"), "unknown_census",
		`back[k].lead = 99; back[k].trail = 88; for (let i = 0; i < 4; i++) { back[k].items[i].a = 5 + i; }`,
		`back[k].lead === 1 && back[k].trail === 2 && back[k].items.every((it) => it.a === 0)`)
	out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, src)
	if err != nil {
		t.Fatalf("NEW-READS-OLD unknown_census: %v\n%s", err, out)
	}
}
