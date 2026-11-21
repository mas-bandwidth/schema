package jstable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// writerchecks_test.go runs two checks of the generated JavaScript that
// no conformance row reaches: the cook writer's refusal of a value its
// declaration does not hold, inside a holder's node extent, and the block
// form's layout self-check. Each fails with the check it holds removed and
// passes with it in place, beside a control that must be accepted.

// runJSProbe generates u into a fresh directory, runs script there under the
// leg's own node, and answers its trimmed output; the probe prints "ok" last
// or calls fail.
func runJSProbe(t *testing.T, files map[string][]byte, script string) string {
	t.Helper()
	node := jsNode(t)
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "probe.mjs")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, node, path)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestCookRefusesAListElementOutOfItsKindJavaScript: a list's elements are
// laid in the holder's NODE EXTENT, after the record's own storage, by the
// extent walk (carveRecord over each field). An element its kind cannot hold —
// 300 in a []int8 — is a value the declaration does not hold, so the cook
// refuses it: CookMeasure answers -1 with invalid_value and CookFrom answers
// false and leaves the caller's buffer as it was (docs/SPEC-TABLES.md §7.6,
// §2.9). A field AFTER the list (tail) keeps the walk honest: a walk that
// stopped at the list and reported success would cook the record short. The
// control is the same record with every element in range, which cooks.
func TestCookRefusesAListElementOutOfItsKindJavaScript(t *testing.T) {
	t.Parallel()
	u := loadSchemaString(t, "carve.schema", "package carve\ntable Root\n{\n    stamp int32\n    vals []int8\n    tail int32\n}\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, ok := files["carveVariable.js"]; !ok {
		t.Fatal("no carveVariable.js: the variable form's cook is not where this test looks")
	}
	out := runJSProbe(t, files, `
import { Root, RootCookMeasure, RootCookMeasureReason, RootCookFrom } from "./carveVariable.js";
function fail(why) { console.log("FAIL " + why); process.exit(1); }
function make(vals) { const r = new Root(); r.stamp = 1; r.vals = vals; r.tail = 7; return r; }
const good = make([1, -128, 127]);
const m = RootCookMeasure(good);
if (!(m > 0)) fail("control: an in-range list did not measure: " + m);
if (!RootCookFrom(good, new Uint8Array(m), 1)) fail("control: an in-range list did not cook");
for (const vals of [[1, 300, 3], [128], [1, 2, -129]]) {
  const bad = make(vals);
  const why = RootCookMeasureReason(bad);
  if (RootCookMeasure(bad) !== -1 || why[0] !== -1 || why[1] !== "invalid_value") {
    fail("[]int8 holding " + JSON.stringify(vals) + " measured " + JSON.stringify(why));
  }
  const buf = new Uint8Array(4096).fill(0xa5);
  if (RootCookFrom(bad, buf, 1) !== false) fail("[]int8 holding " + JSON.stringify(vals) + " cooked");
  if (!buf.every((b) => b === 0xa5)) fail("a refused cook wrote into the caller's buffer");
}
console.log("ok");
`)
	if out != "ok" {
		t.Fatalf("probe:\n%s", out)
	}
}

// TestBlockLayoutVerifyRefusesEachBrokenRuleJavaScript holds
// TableBlockLayout.verify (docs/SPEC-TABLES.md §19.3), the block form's check
// that the generated constants agree with each other before any Open returns a
// handle. JavaScript has no layout of its own to measure them against, so the
// check is held here on descriptors built to its shape: one valid projection
// (an inline field and an out-of-line array whose element record carries a
// presence byte), which verifies, and one copy per rule with that rule alone
// broken, each of which must not verify. Every rule the function states is
// broken at least once, so no rule is dead weight a deletion would not show.
func TestBlockLayoutVerifyRefusesEachBrokenRuleJavaScript(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, "testdata/blockbuild/probe.schema")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(string(files["probeBlock.js"]), "export const TableBlockLayout") {
		t.Fatal("probeBlock.js does not export TableBlockLayout: the block home is not where this test looks")
	}
	out := runJSProbe(t, files, `
import { TableBlockLayout } from "./probeBlock.js";
function fail(why) { console.log("FAIL " + why); process.exit(1); }
function field(o) {
  return Object.assign({ Offset: 0, Size: 0, OutOfLine: false, OffsetOfOffset: -1, CountOffset: -1,
    StrideOffset: -1, PresentOffset: -1, ElementRef: null }, o);
}
// a valid layout: the projection's fields past the 24-byte prologue, an
// out-of-line array (offset_of, count, stride) and its 16-byte element
function valid() {
  const row = { Size: 16, Align: 8, Fields: [field({ Offset: 0, Size: 8 }), field({ Offset: 8, Size: 1, PresentOffset: 9 })] };
  const projection = { Size: 48, Align: 8, Fields: [
    field({ Offset: 24, Size: 8 }),
    field({ Offset: 32, Size: 16, OutOfLine: true, OffsetOfOffset: 32, CountOffset: 40, StrideOffset: 44, ElementRef: () => row }),
  ] };
  return { projection, row, strides: [{ Stride: 16, SizeOf: () => 16 }] };
}
const base = valid();
if (TableBlockLayout.verify(base.projection, base.strides) !== true) fail("control: the valid layout did not verify");
const rules = {
  "size not positive": (l) => { l.projection.Size = 0; },
  "align not positive": (l) => { l.projection.Align = 0; },
  "align not a power of two": (l) => { l.projection.Align = 12; },
  "size not a multiple of align": (l) => { l.projection.Size = 52; },
  "negative field offset": (l) => { l.row.Fields[0].Offset = -8; },
  "negative field size": (l) => { l.row.Fields[0].Size = -1; },
  "projection field inside the prologue": (l) => { l.projection.Fields[0].Offset = 16; },
  "field past the record": (l) => { l.projection.Fields[0].Size = 25; },
  "inline count past the record": (l) => { l.projection.Fields[0].CountOffset = 45; },
  "presence byte past the record": (l) => { l.projection.Fields[0].PresentOffset = 48; },
  "negative offset_of": (l) => { l.projection.Fields[1].OffsetOfOffset = -1; },
  "offset_of triple past the record": (l) => { l.projection.Size = 56; l.projection.Fields[0].Size = 8;
    Object.assign(l.projection.Fields[1], { Offset: 32, Size: 16, OffsetOfOffset: 41, CountOffset: 49, StrideOffset: 53 }); },
  "count not offset_of + 8": (l) => { l.projection.Fields[1].CountOffset = 36; },
  "stride not offset_of + 12": (l) => { l.projection.Fields[1].StrideOffset = 40; },
  "element record broken": (l) => { l.row.Align = 3; },
  "pitch not the row's size": (l) => { l.strides[0].Stride = 24; },
};
for (const [name, breakIt] of Object.entries(rules)) {
  const l = valid();
  breakIt(l);
  if (TableBlockLayout.verify(l.projection, l.strides) !== false) fail("verify accepted a layout with " + name);
}
console.log("ok");
`)
	if out != "ok" {
		t.Fatalf("probe:\n%s", out)
	}
}
