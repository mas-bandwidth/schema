package jstable

import (
	"fmt"
	"path/filepath"
	"strings"
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
// V1 ITSELF IS NOT A LAWFUL OLDER WRITER OF V2. V2 moved `a` from int32 to
// float32 (off every ladder) and removed `b`, so V1's entry handed to V2 is
// UNLAWFUL (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT, §5.4, §5.9 #30): the
// load refuses layout_malformed with every counter at zero and the rows back at
// V2's declared defaults, twice — the first half below. The remap is the
// COMPILED PLAN's, so the second half holds it over a LAWFUL older writer: V2
// with only the mid-list inserts taken out (Silver, the hex arm, Omega), which
// leaves Gold, Ward and the keyed Beta/Delta sliding exactly as V1's do.
//
// THE CASE LIVES ON THE LINEAGE HARNESS AND NOT ON THE BYTE DRIVER
// (test/js-tables/fixedform.mjs). E7's read is NEW-READS-OLD across two
// generations, so on this leg it is a COMPILED plan selected through the lock
// (§5.2, §5.6): the older unit's entry is handed to the newer reader here.
//
// THE CASE NEEDS ONLY `node` AND THE SCHEMAS. It does not read the reference's
// byte oracle, so it does not call jsFixedCorpus.
func TestJSFixedVersioningVariantArmMidList(t *testing.T) {
	node := jsNode(t)
	v1 := jsReadSchema(t, "V1")
	v2 := jsReadSchema(t, "V2")
	table := jsFixedRootName(t, v1)

	// THE LAWFUL OLDER WRITER: V2 minus its three mid-list inserts.
	older := v2
	for _, edit := range [][2]string{
		{"enum Grade { Bronze, Silver, Gold }", "enum Grade { Bronze, Gold }"},
		{"enum Slot { Alpha, Omega, Beta, Delta, Sigma }", "enum Slot { Alpha, Beta, Delta, Sigma }"},
		{"    hex   Hex\n", ""},
	} {
		if !strings.Contains(older, edit[0]) {
			t.Fatalf("V2.schema no longer holds %q: the lawful older writer is derived by taking it out", edit[0])
		}
		older = strings.Replace(older, edit[0], edit[1], 1)
	}

	// HALF ONE: V1's entry is unlawful to V2, and the load refuses by name.
	v1File := filepath.Join(t.TempDir(), "v1_cfg.bin")
	writeV1 := `
const { writeFileSync } = await import("node:fs");
const { Grade, EffectType } = await import("./Probe.js");
const v = new T(); InitT(v);
v.a = 42;
v.grade = Grade.Gold;
v.effect.type = EffectType.Ward;
v.effect.ward.charge = 0.75;
const buf = new Uint8Array(TMeasure(1));
if (TSave([v], 1, buf) !== buf.length) { fail("E7: V1 wrote its record"); }
writeFileSync(%q, buf);
`
	if out, err := jsRunVersionProbe(t, node, v1, nil, 0, table, fmt.Sprintf(writeV1, v1File)); err != nil {
		t.Fatalf("E7, V1's own save: %v\n%s", err, out)
	}
	refuse := fmt.Sprintf(`
const { Grade, EffectType } = await import("./Probe.js");
const data = readFileSync(%q);
const back = []; for (let k = 0; k < 4; k++) { back.push(new T()); InitT(back[k]); }
const want = new T(); InitT(want);
const plan = TNewPlan(8192, 8192);
for (let pass = 0; pass < 2; pass++) {
  // The poison stays on state a reset owns: Init does not reach an unselected
  // arm's payload, so the Ward payload is not poisoned here.
  for (const v of back) { v.a = 9.5; v.grade = Grade.Silver; v.effect.type = EffectType.Ward; v.tier = 99; v.tierPresent = true; v.titleLength = 3; }
  plan.image.fill(0x5A);
  const r = new TableFixedReport();
  const n = TLoad(back, back.length, data, data.length, plan, r);
  if (n !== -1 || r.refused !== TableFixedRefusal.LayoutMalformed || r.damaged) {
    fail("E7 pass " + pass + ": V1's entry is UNLAWFUL to V2 (a moved kind, b removed) and refuses layout_malformed: n=" + n + " " + reason(r));
  }
  if (r.unknown || r.mismatch || r.widened || r.oor || r.repeated) {
    fail("E7 pass " + pass + ": a refusal moves no counter and there is no compile census: " + reason(r));
  }
  for (let k = 0; k < back.length; k++) {
    if (show(back[k]) !== show(want)) { fail("E7 pass " + pass + ": row " + k + " is not at V2's declared defaults: " + show(back[k])); }
  }
}
`, v1File)
	if out, err := jsRunVersionProbe(t, node, v2, []string{v1}, 0, table, refuse); err != nil {
		t.Fatalf("E7, V2 over V1's file: %v\n%s", err, out)
	}

	// HALF TWO: THE LAWFUL OLDER WRITER WRITES A RECORD AT ITS OWN ORDINALS:
	// Grade.Gold is 2 here and Effect.Type Ward is 2, with the Ward arm's payload
	// set so a landed tag with a lost payload cannot pass.
	file := filepath.Join(t.TempDir(), "older_cfg.bin")
	write := `
const { writeFileSync } = await import("node:fs");
const { Grade, EffectType, Slot } = await import("./Probe.js");
const v = new T(); InitT(v);
v.a = 42;
const NAME = "hello";
for (let i = 0; i < NAME.length; i++) { v.title[i] = NAME.charCodeAt(i); }
v.titleLength = NAME.length;
v.grade = Grade.Gold;
v.effect.type = EffectType.Ward;
v.effect.ward.charge = 0.75;
v.tokens[Slot.Alpha - 1] = 21;
v.tokens[Slot.Beta - 1] = 22;
v.tokens[Slot.Delta - 1] = 24;
v.tierPresent = true;
v.tier = 77;
if (Grade.Gold !== 2 || EffectType.Ward !== 2 || Slot.Beta !== 2) { fail("E7: the lawful older writer's ordinals are not V1's"); }
const buf = new Uint8Array(TMeasure(1));
if (TSave([v], 1, buf) !== buf.length) { fail("E7: the older writer wrote its record"); }
writeFileSync(%q, buf);
`
	if out, err := jsRunVersionProbe(t, node, older, nil, 0, table, fmt.Sprintf(write, file)); err != nil {
		t.Fatalf("E7, the lawful older writer's own save: %v\n%s", err, out)
	}

	// V2 READS IT THROUGH A PLAN COMPILED FROM THE OLDER ENTRY. Gold must land
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
if (!(n === 1 && !r.damaged && r.refused === 0)) {
  fail("E7: V2 read the older record through a compiled plan: n=" + n + " " + reason(r));
}
if (r.unknown !== 0 || r.mismatch !== 0) { fail("E7: a lawful entry counts nothing: " + reason(r)); }
const got = back[0];
if (got.grade !== Grade.Gold) {
  fail("E7 ENUM: a variant inserted in the middle is remapped by NAME, not by ordinal — got " +
    EnumNameGrade(got.grade) + " (" + got.grade + "), want Gold");
}
if (got.effect.type !== EffectType.Ward) {
  fail("E7 UNION: an arm inserted in the middle is remapped by NAME, not by ordinal — got " +
    got.effect.type + ", want " + EffectType.Ward);
}
if (got.effect.ward.charge !== 0.75) {
  fail("E7 UNION: the remapped arm's payload lands — got " + got.effect.ward.charge);
}
if (got.a !== 42 || got.titleLength !== 5 || String.fromCharCode(...got.title.slice(0, 5)) !== "hello") {
  fail("E7: the fields beside the moved variants land: " + show(got));
}
if (!(got.tokens[Slot.Alpha - 1] === 21 && got.tokens[Slot.Beta - 1] === 22 && got.tokens[Slot.Delta - 1] === 24 &&
      got.tokens[Slot.Omega - 1] === 0 && got.tierPresent === true && got.tier === 77)) {
  fail("E7: the keyed slots (Beta slid, Omega the writer never named) and the optional land: " + show(got));
}
`, file)
	if out, err := jsRunVersionProbe(t, node, v2, []string{older}, 0, table, read); err != nil {
		t.Fatalf("E7, V2 over the lawful older file: %v\n%s", err, out)
	}
}
