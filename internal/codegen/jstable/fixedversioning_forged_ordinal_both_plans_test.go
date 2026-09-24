package jstable

// forged_ordinal_both_plans is §5.8 row 12 of the fixed-form versioning law:
// VOLD_/VNEW_enum_append read the SAME forged bytes, once through the NEW
// build's COMPILED plan (selected by the lineage) and once through the OLD
// build's IDENTITY plan (its own hash). The forge turns old_enum_append.bin's
// r0.tier from 3 (Gold) into 4, an ordinal past the writer's three variants.
// `clamped` is asserted EXACTLY `== 1` on both plans, never `>= 1`: a leg that
// counts in the `ordinal` op as well lands 2 and passes a looser read — the
// row exists to catch that double count.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestJSFixedVersioningForgedOrdinalBothPlans(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_enum_append")
	older := jsReadSchema(t, "VOLD_enum_append")
	table := jsFixedRootName(t, older)

	old := filepath.Join(corpus, "old_enum_append.bin")
	data, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	// The manifest says old_enum_append.bin holds r0.tier=3,r0.seq=9: the enum
	// ordinal (one byte) followed by its neighbour, the int32 scalar, LE.
	needle := []byte{3, 9, 0, 0, 0}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatalf("tier=3 followed by seq=9 is not in %s", old)
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatalf("tier=3 followed by seq=9 occurs more than once in %s", old)
	}
	data[at] = 4 // the forge
	forged := filepath.Join(t.TempDir(), "hostile_enum_append.bin")
	if err := os.WriteFile(forged, data, 0o600); err != nil {
		t.Fatal(err)
	}

	body := fmt.Sprintf(`
const data = readFileSync(%q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport(); const r = report;
const plan = TNewPlan(4096, 4096);
const n = TLoad(back, back.length, data, data.length, plan, report);
if (n !== 1) { fail("the forged file carries one record: n=" + n + " " + reason(r)); }
if (r.refused !== 0 || r.malformed) { fail("a forged ordinal is not a refusal and not malformed: " + reason(r)); }
if (back[0].Tier !== 0) { fail("a forged ordinal lands None: " + show(back[0])); }
if (back[0].Tier === 4) { fail("a forged ordinal never lands Platinum: " + show(back[0])); }
if (back[0].Seq !== 9) { fail("the scalar after the enum is untouched: " + show(back[0])); }
if (r.clamped !== 1) { fail("the forged ordinal is counted exactly once: " + reason(r)); }
if (r.unknown !== 0 || r.kindMismatch !== 0 || r.widened !== 0 || r.duplicate !== 0) {
  fail("no other counter moved: " + reason(r));
}
`, forged)

	out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, body)
	if err != nil {
		t.Fatalf("the COMPILED plan read of the forged ordinal: %v\n%s", err, out)
	}
	out, err = jsRunVersionProbe(t, node, older, nil, 0, table, body)
	if err != nil {
		t.Fatalf("the IDENTITY plan read of the forged ordinal: %v\n%s", err, out)
	}
}
