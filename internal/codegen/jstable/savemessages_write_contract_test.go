package jstable

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJsSaveMessagesRefusesOffContract holds the JavaScript message-form writer
// to SPEC §5.1 rows 1 to 8: <T>SaveMessages and <T>MeasureMessages refuse a
// batch holding a value its declaration does not admit, answering -1 with the
// report's Verdict set and never throwing; the boundary values are written and
// round-trip through LoadMessages. Each off-contract value is checked alone and
// as the second body of a two-body batch.
func TestJsSaveMessagesRefusesOffContract(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, jsSaveMessagesContractSchema)
	st := findTable(t, u, "Doc")
	node := jsNode(t)
	probe := fmt.Sprintf(jsSaveMessagesContractProbe, messageRootBase(u, st), messageHomeBase(u))
	out, err := jsRunMessageProbe(t, node, u, map[string][]byte{
		"ann.bin": tablewire.Announce(u),
	}, probe)
	if err != nil {
		t.Fatalf("the off-contract probe failed: %v\n%s", err, out)
	}
}

const jsSaveMessagesContractSchema = `package probe

enum Kind { Red, Blue }

union Choice
{
    number int32 | min = 0, max = 10
    text string(4)
}

fixed table Doc
{
    score int32 | min = -5, max = 5
    mask bits(5)
    kind Kind
    choice Choice
    values [..3]int32 | min = -2, max = 2
    name string(6)
    wide int64
}
`

const jsSaveMessagesContractProbe = `import { readFileSync } from "node:fs";
import { Doc, DocSaveMessages, DocLoadMessages, DocMeasureMessages } from "./%[1]s.js";
import { TableVocabulary, AnnounceRead, TableMessageEntriesHere } from "./%[2]s.js";

let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }

const vocabulary = new TableVocabulary();
vocabulary.Init(new Array(TableMessageEntriesHere));
if (!AnnounceRead(vocabulary, new Uint8Array(readFileSync("./ann.bin")), {})) { fail("AnnounceRead"); }

function lawful() { return new Doc(); }

function bytesEq(a, b) {
  if (a.length !== b.length) { return false; }
  for (let i = 0; i < a.length; i++) { if (a[i] !== b[i]) { return false; } }
  return true;
}

function refused(what, bad, second) {
  const values = second ? [lawful(), bad] : [bad];
  let size;
  try {
    size = DocMeasureMessages(values, {});
  } catch (e) {
    fail(what + (second ? " (second body)" : "") + ": MeasureMessages threw " + e);
    return;
  }
  if (size !== -1) { fail(what + (second ? " (second body)" : "") + ": MeasureMessages answered " + size + ", want -1"); }
  const report = {};
  let n;
  try {
    n = DocSaveMessages(values, new Uint8Array(512), report);
  } catch (e) {
    fail(what + (second ? " (second body)" : "") + ": SaveMessages threw " + e);
    return;
  }
  if (n !== -1) { fail(what + (second ? " (second body)" : "") + ": SaveMessages answered " + n + ", want -1"); }
  if (!report.Verdict) { fail(what + (second ? " (second body)" : "") + ": SaveMessages left Verdict unset"); }
}

function both(what, bad) {
  refused(what, bad, false);
  refused(what, bad, true);
}

function roundTrip(what, value, check) {
  const buffer = new Uint8Array(512);
  let n;
  try {
    n = DocSaveMessages([value], buffer, {});
  } catch (e) {
    fail(what + ": SaveMessages threw " + e);
    return;
  }
  if (n < 1) { fail(what + ": SaveMessages answered " + n); return; }
  const storage = [new Doc()];
  const report = {};
  let got;
  try {
    got = DocLoadMessages(storage, vocabulary, buffer.subarray(0, n), report);
  } catch (e) {
    fail(what + ": LoadMessages threw " + e);
    return;
  }
  if (got !== 1) { fail(what + ": LoadMessages answered " + got + " " + JSON.stringify(report)); return; }
  check(storage[0]);
}

// row 1: a ranged integer off either end
for (const score of [-6, 6]) {
  const d = lawful(); d.score = score;
  both("score=" + score, d);
}
// row 2: a bits(N) value wider than N
for (const mask of [-1, 32, 64]) {
  const d = lawful(); d.mask = mask;
  both("mask=" + mask, d);
}
// row 3: a counted array's count past Max, below Min, or past storage
for (const count of [-1, 4]) {
  const d = lawful(); d.valuesCount = count;
  both("valuesCount=" + count, d);
}
// row 4: a string length past N, or negative
for (const length of [-1, 7]) {
  const d = lawful(); d.nameLength = length;
  both("nameLength=" + length, d);
}
// row 5: an enum ordinal outside its variant set
for (const kind of [3, 4]) {
  const d = lawful(); d.kind = kind;
  both("kind=" + kind, d);
}
// row 6: a union tag outside its arm set
for (const tag of [3, 4]) {
  const d = lawful(); d.choice.type = tag;
  both("choice.type=" + tag, d);
}
// row 8: ill-formed text, a zero byte, and a non-integer length
{
  const ill = lawful(); ill.nameLength = 3; ill.name.set([0x61, 0xff, 0x62]);
  both("ill-formed UTF-8", ill);
  const zero = lawful(); zero.nameLength = 3; zero.name.set([0x61, 0x00, 0x62]);
  both("a zero byte", zero);
  const frac = lawful(); frac.nameLength = 1.5;
  both("a non-integer string length", frac);
}
// the wrong JavaScript type for an integer field, refused and never thrown
{
  const frac = lawful(); frac.score = 1.5;
  both("a non-integer score", frac);
  const number = lawful(); number.wide = 1;
  both("a Number where a BigInt is stored", number);
}

// the boundary values are written and round-trip through LoadMessages
roundTrip("the high boundaries", (() => {
  const d = lawful();
  d.score = 5;
  d.mask = 31;
  d.kind = 2;
  d.choice.type = 1; d.choice.number = 10;
  d.valuesCount = 3; d.values[0] = -2; d.values[1] = 0; d.values[2] = 2;
  d.nameLength = 6; d.name.set([0x61, 0x62, 0x63, 0x64, 0x65, 0x66]);
  d.wide = 9223372036854775807n;
  return d;
})(), (back) => {
  if (back.score !== 5) { fail("round trip moved score=" + back.score); }
  if (back.mask !== 31) { fail("round trip moved mask=" + back.mask); }
  if (back.kind !== 2) { fail("round trip moved kind=" + back.kind); }
  if (back.choice.type !== 1 || back.choice.number !== 10) { fail("round trip moved the union number arm"); }
  if (back.valuesCount !== 3 || back.values[0] !== -2 || back.values[1] !== 0 || back.values[2] !== 2) { fail("round trip moved the counted array"); }
  if (back.nameLength !== 6 || !bytesEq(back.name.subarray(0, 6), Uint8Array.from([0x61, 0x62, 0x63, 0x64, 0x65, 0x66]))) { fail("round trip moved name"); }
  if (back.wide !== 9223372036854775807n) { fail("round trip moved wide=" + back.wide); }
});

roundTrip("the low boundaries", (() => {
  const d = lawful();
  d.score = -5;
  d.mask = 0;
  d.kind = 1;
  d.choice.type = 0;
  d.valuesCount = 0;
  d.nameLength = 0;
  d.wide = -9223372036854775808n;
  return d;
})(), (back) => {
  if (back.score !== -5) { fail("round trip moved score=" + back.score); }
  if (back.mask !== 0) { fail("round trip moved mask=" + back.mask); }
  if (back.kind !== 1) { fail("round trip moved kind=" + back.kind); }
  if (back.choice.type !== 0) { fail("round trip moved the union none tag"); }
  if (back.valuesCount !== 0) { fail("round trip moved valuesCount=" + back.valuesCount); }
  if (back.nameLength !== 0) { fail("round trip moved nameLength=" + back.nameLength); }
  if (back.wide !== -9223372036854775808n) { fail("round trip moved wide=" + back.wide); }
});

roundTrip("the union text arm at its bound", (() => {
  const d = lawful();
  d.choice.type = 2;
  d.choice.textLength = 4;
  d.choice.text.set([0x61, 0x62, 0x63, 0x64]);
  return d;
})(), (back) => {
  if (back.choice.type !== 2 || back.choice.textLength !== 4 || !bytesEq(back.choice.text.subarray(0, 4), Uint8Array.from([0x61, 0x62, 0x63, 0x64]))) {
    fail("round trip moved the union text arm");
  }
});

if (failures !== 0) { process.exit(1); }
console.log("ok");
`
