package jstable

// A DEPRECATED VARIANT IS NEVER WRITTEN (docs/SPEC-TABLES.md §2.15 WRITE;
// docs/SPEC.md §5.1: JavaScript refuses in every build). The shared rows
// wref_enum_*deprecated and fref_enum_*deprecated (testdata/conformance/
// tables/MANIFEST.txt) reach this leg's FIXED save only: their roots are
// fixed tables, whose own form is the fixed form. This gate holds the
// VARIABLE save (TableVariableEnumRef) to the same rule, on a variable root
// and on a fixed root's variable form, an element and a scalar each, beside
// the fixed save, and saves every lawful twin. Unit-test rules: no real
// time, no socket, no network.

import (
	"strings"
	"testing"
)

const jsDeprecatedWriteSchema = `package probe

enum Mood
{
    Calm
    Old | deprecated
    Fierce
}

table VarMoods
{
    row []Mood
    mood Mood = Fierce
}

fixed table FixMoods
{
    row [..4]Mood
    mood Mood = Fierce
}
`

const jsDeprecatedWriteProbe = `import * as m from "./ProbeVariable.js";
import * as f from "./ProbeTable.js";

let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }

// the variable save answers bytes, or null when it refuses
function variable(save, v) {
  try { return save(v) instanceof Uint8Array ? "wrote" : "refused"; } catch (e) { return "threw " + e.name; }
}

// the fixed save answers -1 when it refuses, writing no byte
function fixed(mutate) {
  const v = new f.FixMoods();
  f.InitFixMoods(v);
  v.rowCount = 1;
  mutate(v);
  const bytes = new Uint8Array(f.FixMoodsFixedMeasure(1) + 16);
  bytes.fill(0xa5);
  let got;
  try { got = f.FixMoodsFixedSave([v], 1, bytes); } catch (e) { return "threw " + e.name; }
  if (got === -1) {
    for (let i = 0; i < bytes.length; i++) { if (bytes[i] !== 0xa5) { return "refused but changed byte " + i; } }
    return "refused";
  }
  return got >= 0 ? "wrote" : "answered " + got;
}

function want(label, got, expect) { if (got !== expect) { fail(label + ": " + got + ", want " + expect); } }

for (const [name, expect] of [["Old", "refused"], ["Fierce", "wrote"], ["Calm", "wrote"]]) {
  { const v = new m.VarMoods(); v.row = [name]; want("variable root element " + name, variable(m.VarMoodsSave, v), expect); }
  { const v = new m.VarMoods(); v.mood = name; want("variable root scalar " + name, variable(m.VarMoodsSave, v), expect); }
  { const v = new m.FixMoods(); v.row = [name]; want("fixed root variable form element " + name, variable(m.FixMoodsSave, v), expect); }
  { const v = new m.FixMoods(); v.mood = name; want("fixed root variable form scalar " + name, variable(m.FixMoodsSave, v), expect); }
}
for (const [ord, expect] of [[2, "refused"], [3, "wrote"], [1, "wrote"]]) {
  want("fixed save element ordinal " + ord, fixed((v) => { v.row[0] = ord; }), expect);
  want("fixed save scalar ordinal " + ord, fixed((v) => { v.mood = ord; }), expect);
}

if (failures !== 0) { process.exit(1); }
console.log("ok");
`

func TestJsDeprecatedVariantNeverWritten(t *testing.T) {
	t.Parallel()
	out, err := jsRunMessageProbe(t, jsNode(t), unitFrom(t, jsDeprecatedWriteSchema), nil, jsDeprecatedWriteProbe)
	if err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("a JavaScript writer wrote a deprecated variant: %v\n%s", err, out)
	}
}
