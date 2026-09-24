package jstable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// E7: A VARIANT AND AN ARM INSERTED IN THE MIDDLE ARE REMAPPED BY NAME
// (schema#876 item E7; docs/FIXED-FORM-ALGORITHM.md §7 item 6's V1/V2 fixture;
// the reference is test/tables/fixedform_main.cpp, `v_case`).
//
// V2 inserts `Silver` BETWEEN `Bronze` and `Gold`, so a stored `Gold` ordinal
// slides from 2 to 3; it inserts the `hex` arm BETWEEN `boost` and `ward`, so
// the stored `Ward` tag slides from 2 to 3. Under a POSITIONAL encoding every
// stored Gold reads back as Silver and every Ward as Hex. They ride as the hash
// of the variant's/arm's NAME, so they do not.
//
// THE CASE LIVES ON THE LINEAGE HARNESS AND NOT ON THE BYTE DRIVER
// (test/js-tables/fixedform.mjs). E7's read is NEW-READS-OLD across two
// generations, so on this leg it is a COMPILED plan selected through the lock
// (§5.2, §5.6): measured against the tree, V2 reading a V1 file with NO lineage
// handed in refuses `layout_newer` before any record — the byte driver has no
// lineage to hand and the §5.6 retirements name exactly this shape. Under §5
// the lock is played HERE, where the older unit's entry is handed to the newer
// reader, which is the same place `TestJSFixedVersioningUnionArmText` asserts
// the guard/flavour lanes.
//
// THE CASE NEEDS ONLY `node` AND THE TWO SCHEMAS. It does not read the
// reference's byte oracle, so it does not call jsFixedCorpus: it runs on a tree
// that never built the corpus, and under `make tables-js-versioning` it is one
// more `TestJSFixedVersioning*` case over the leg's own toolchain.
//
// It is the js twin of the ENUM half of `v_case` — the arm half's guard/flavour
// the UT1/UT2 case already holds — and it is the half the fixed-form byte driver
// could not carry after §5.6 retired its compiled reads.
func TestJSFixedVersioningVariantArmMidList(t *testing.T) {
	node := jsNode(t)
	v1 := jsReadSchema(t, "V1")
	v2 := jsReadSchema(t, "V2")
	table := jsFixedRootName(t, v1)
	file := filepath.Join(t.TempDir(), "v1_cfg.bin")

	// V1 WRITES A RECORD AT ITS OWN ORDINALS: Grade.Gold is 2 here and
	// Effect.Type Ward is 2, with the Ward arm's payload set so a landed tag
	// with a lost payload cannot pass.
	write := `
const { writeFileSync } = await import("node:fs");
const { Grade, EffectType, Slot } = await import("./Probe.js");
const v = new T(); InitT(v);
v.A = 42;
const NAME = "hello";
for (let i = 0; i < NAME.length; i++) { v.Name[i] = NAME.charCodeAt(i); }
v.NameLength = NAME.length;
v.Grade = Grade.Gold;
v.Effect.Type = EffectType.Ward;
v.Effect.Ward.Charge = 0.75;
v.Tokens[Slot.Alpha - 1] = 21;
v.Tokens[Slot.Delta - 1] = 24;
v.TierPresent = true;
v.Tier = 77;
const buf = new Uint8Array(TMeasure(1));
if (TSave([v], 1, buf) !== buf.length) { fail("E7: V1 wrote its record"); }
writeFileSync(%q, buf);
`
	if out, err := jsRunVersionProbe(t, node, v1, nil, 0, table, fmt.Sprintf(write, file)); err != nil {
		t.Fatalf("E7, V1's own save: %v\n%s", err, out)
	}

	// V2 READS IT THROUGH A PLAN COMPILED FROM V1's ENTRY. Gold must land
	// V2's Gold (ordinal 3) and Ward must land V2's Ward (ordinal 3), each by
	// NAME; a positional remap lands Silver and Hex.
	read := fmt.Sprintf(`
const { Grade, EnumNameGrade, EffectType, Slot } = await import("./Probe.js");
const data = readFileSync(%q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport(); const r = report;
const plan = TNewPlan(8192, 8192);
// THE POISON (§5.7, §5.9 #17): a prefill that ran and one that never did look
// alike over zeros.
plan.image.fill(0x5A);
const n = TLoad(back, back.length, data, data.length, plan, report);
if (!(n === 1 && !r.malformed && r.refused === 0)) {
  fail("E7: V2 read V1's record through a compiled plan: n=" + n + " " + reason(r));
}
const got = back[0];
if (got.Grade !== Grade.Gold) {
  fail("E7 ENUM: a variant inserted in the middle is remapped by NAME, not by ordinal — got " +
    EnumNameGrade(got.Grade) + " (" + got.Grade + "), want Gold");
}
if (got.Effect.Type !== EffectType.Ward) {
  fail("E7 UNION: an arm inserted in the middle is remapped by NAME, not by ordinal — got " +
    got.Effect.Type + ", want " + EffectType.Ward);
}
if (got.Effect.Ward.Charge !== 0.75) {
  fail("E7 UNION: the remapped arm's payload lands — got " + got.Effect.Ward.Charge);
}
if (!(got.Tokens[Slot.Delta - 1] === 24 && got.TierPresent === true && got.Tier === 77)) {
  fail("E7: the keyed slot and the optional beside the moved variants land: " + show(got));
}
`, file)
	if out, err := jsRunVersionProbe(t, node, v2, []string{v1}, 0, table, read); err != nil {
		t.Fatalf("E7, V2 over V1's file: %v\n%s", err, out)
	}
}
