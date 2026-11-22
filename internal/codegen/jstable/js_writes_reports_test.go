package jstable

// The JavaScript leg refuses bad writes in every build and reports each read on
// its own (docs/SPEC.md §5.1, docs/SPEC-TABLES.md §4, §16). Each test runs the
// generated module under node and goes red when the matching fix is planted back.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// runJsVariableProbe writes the variable module for src and runs body against it.
func runJsVariableProbe(t *testing.T, src, body string) {
	t.Helper()
	slowtest.Gate(t, "node")
	node := jsNode(t)
	u := unitFrom(t, src)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	probe := `import { RootSave, RootLoad, RootToJson, RootFromJson, TableVariableReport } from "./ProbeVariable.js";
let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }
` + body + `
if (failures !== 0) { process.exit(1); }
console.log("ok");
`
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("probe: %v\n%s", err, out)
	}
}

const jsWritesReportsVariableSchema = `package probe

enum Mood { calm, angry }
flags Perms { Read, Write, Exec }

table Root
{
    value int32 = 7
    mood  Mood
    perms Perms
    tag   string(32)
}
`

// TestJsFixedWriterRefusesBareIntOffWidth: a bare intN/uintN past its width is
// refused (SPEC.md §5.1 row 11), not written as its low N bits.
func TestJsFixedWriterRefusesBareIntOffWidth(t *testing.T) {
	slowtest.Gate(t, "node")
	t.Parallel()
	node := jsNode(t)
	u := unitFrom(t, `package probe

fixed table Row
{
    a int8
    b uint8
    c int16
    d uint16
    e int32
    f uint32
    g int64
    h uint64
}
`)
	files := generatedTree(u)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const probe = `import { Row, InitRow, RowFixedSave, RowFixedMeasure } from "./ProbeTable.js";
let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }
function save(mutate) {
  const v = new Row();
  InitRow(v);
  mutate(v);
  const bytes = new Uint8Array(RowFixedMeasure(1));
  return RowFixedSave([v], 1, bytes);
}
const need = RowFixedMeasure(1);
const bad = {
  "int8 300": (v) => { v.a = 300; }, "int8 128": (v) => { v.a = 128; }, "int8 -129": (v) => { v.a = -129; },
  "uint8 256": (v) => { v.b = 256; }, "uint8 -1": (v) => { v.b = -1; },
  "int16 32768": (v) => { v.c = 32768; }, "uint16 65536": (v) => { v.d = 65536; },
  "int32 2^31": (v) => { v.e = 2147483648; }, "uint32 2^32": (v) => { v.f = 4294967296; },
  "int64 2^63": (v) => { v.g = 9223372036854775808n; }, "int64 -2^63-1": (v) => { v.g = -9223372036854775809n; },
  "uint64 -1": (v) => { v.h = -1n; }, "uint64 2^64": (v) => { v.h = 18446744073709551616n; },
};
for (const [label, mutate] of Object.entries(bad)) {
  const got = save(mutate);
  if (got !== -1) { fail(label + " was written (" + got + ")"); }
}
const good = {
  "int8 ends": (v) => { v.a = 127; }, "int8 low": (v) => { v.a = -128; }, "uint8 top": (v) => { v.b = 255; },
  "int64 ends": (v) => { v.g = -9223372036854775808n; }, "uint64 top": (v) => { v.h = 18446744073709551615n; },
};
for (const [label, mutate] of Object.entries(good)) {
  const got = save(mutate);
  if (got !== need) { fail(label + " was refused (" + got + ")"); }
}
if (failures !== 0) { process.exit(1); }
console.log("ok");
`
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("probe: %v\n%s", err, out)
	}
}

// TestJsVariableReadZeroesTheReport: each read starts from a zeroed report, so
// damage and counters a previous read left never colour the next one.
func TestJsVariableReadZeroesTheReport(t *testing.T) {
	t.Parallel()
	runJsVariableProbe(t, jsWritesReportsVariableSchema, `
const report = new TableVariableReport();
if (RootFromJson("{", report) !== null || !report.damaged) { fail("a broken text was not damaged"); }
const a = RootFromJson("{}", report);
if (a === null || report.damaged || report.reason !== "" || report.refused !== 0) { fail("text read after damage: " + JSON.stringify(report)); }
RootFromJson("{\"bogus\":1,\"value\":\"x\"}", report);
const b = RootFromJson("{}", report);
if (b === null || report.unknown !== 0 || report.mismatch !== 0 || report.oor !== 0) { fail("counters carried into a clean text read: " + JSON.stringify(report)); }
const wire = RootSave(b);
const wr = new TableVariableReport();
wr.damaged = true; wr.unknown = 3; wr.mismatch = 2; wr.oor = 1; wr.widened = 4;
const c = RootLoad(wire, wr);
if (c === null || wr.damaged || wr.unknown !== 0 || wr.mismatch !== 0 || wr.oor !== 0 || wr.widened !== 0) { fail("wire read kept the old report: " + JSON.stringify(wr)); }
`)
}

// TestJsEnumNoneSpellingsWriteTheSameBytes: null and "None" are one value, so
// the default elision compare cannot tell them apart.
func TestJsEnumNoneSpellingsWriteTheSameBytes(t *testing.T) {
	t.Parallel()
	runJsVariableProbe(t, jsWritesReportsVariableSchema, `
const a = RootFromJson("{}", new TableVariableReport());
const b = RootFromJson("{}", new TableVariableReport());
a.mood = null;
b.mood = "None";
const x = Buffer.from(RootSave(a)).toString("hex"), y = Buffer.from(RootSave(b)).toString("hex");
if (x !== y) { fail("None as null " + x + " differs from None as the string " + y); }
b.mood = "angry";
if (Buffer.from(RootSave(b)).toString("hex") === x) { fail("a set variant elided"); }
`)
}

// TestJsJsonFlagsWriterRefusesNonMasks: the JSON writer refuses what the save
// refuses, a value that is not a BigInt mask inside the unsigned width.
func TestJsJsonFlagsWriterRefusesNonMasks(t *testing.T) {
	t.Parallel()
	runJsVariableProbe(t, jsWritesReportsVariableSchema, `
const v = RootFromJson("{}", new TableVariableReport());
for (const [label, bad] of [["Number mask", 5], ["negative BigInt", -1n], ["fractional Number", 1.5], ["string", "1"], ["past 64 bits", 1n << 64n]]) {
  v.perms = bad;
  let got;
  try { got = RootToJson(v); } catch (e) { fail(label + " threw " + e.name); continue; }
  if (got !== null) { fail(label + " was written: " + JSON.stringify(got)); }
}
v.perms = 5n;
const ok = RootToJson(v);
if (ok === null || !ok.includes("Read") || !ok.includes("Exec")) { fail("a valid mask was refused: " + ok); }
`)
}

// TestJsJsonStringEntryRefusesLoneSurrogateInComment: a lone surrogate half in a
// comment is ill-formed text, as it is in a string (docs/SPEC-TABLES.md §16.3).
func TestJsJsonStringEntryRefusesLoneSurrogateInComment(t *testing.T) {
	t.Parallel()
	runJsVariableProbe(t, jsWritesReportsVariableSchema, `
for (const [label, text] of [
  ["high half in a line comment", "{} // a\ud800 b\n"],
  ["low half in a line comment", "{} // a\udc00 b\n"],
  ["high half in a block comment", "{ /* \ud800 */ }"],
  ["low half in a block comment", "{ /* \udc00 */ }"],
  ["high half at the end of a comment", "{ /* a\ud800*/ }"],
]) {
  const report = new TableVariableReport();
  if (RootFromJson(text, report) !== null) { fail(label + " was accepted"); }
}
for (const [label, text] of [
  ["a pair in a line comment", "{} // 😀\n"],
  ["a pair in a block comment", "{ /* 😀 */ }"],
]) {
  if (RootFromJson(text, new TableVariableReport()) === null) { fail(label + " was refused"); }
}
`)
}

// TestJsAnnounceReadOverCapacityTouchesNoEntry: a vocabulary past the entry
// capacity refuses before one slot of the caller's storage is written.
func TestJsAnnounceReadOverCapacityTouchesNoEntry(t *testing.T) {
	slowtest.Gate(t, "node")
	t.Parallel()
	node := jsNode(t)
	u := loadUnit(t, "../../../tables/backend/Backend.schema")
	announcementOf := manifestAnnouncements(t)
	probe := `
const data = new Uint8Array(readFileSync("./ann.bin"));
const sentinel = { sentinel: true };
const storage = [sentinel];
const entries = new TableVocabulary();
entries.Init(storage);
const report = {};
if (AnnounceRead(entries, data, report) || report.Reason !== "vocabulary_too_large" || entries.Announced) {
  fail("not refused by name: " + JSON.stringify(report));
}
if (storage[0] !== sentinel || storage.length !== 1) { fail("the refusal overwrote the caller's entries"); }
`
	out, err := jsRunAnnounceProbe(t, node, u, map[string][]byte{"ann.bin": announcementOf["backenddemo"]}, probe)
	if err != nil {
		t.Fatalf("over capacity: %v\n%s", err, out)
	}
}
