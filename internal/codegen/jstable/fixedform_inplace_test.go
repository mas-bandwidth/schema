package jstable

// THE IN-PLACE IDENTITY READ (docs/SPEC-TABLES.md §3.4): a language MAY read
// its identity plan in place, with no intermediate copy, when that is necessary
// for performance in an extreme case, and the spec LISTS the languages that do.
// This file holds the JavaScript side of that rule:
//
//   - the list is read from the spec, never from this file, and the JS door is
//     allowed only while the list names javascript (jsIdentityDoor);
//   - the list names no language whose door has no guard here: a new entry
//     comes with its own package's narrowing, or this goes red;
//   - the in-place path and the shared path (prefill, plan loop, projection
//     from the image) give VALUE- and BYTE-identical results on the same
//     files, clean and damaged.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
)

// specInPlaceMarker is the one line of docs/SPEC-TABLES.md §3.4 that lists the
// languages with an in-place identity read, comma separated.
const specInPlaceMarker = "in-place identity readers:"

// specInPlaceLanguages reads the list from the spec. Exactly one marker line.
func specInPlaceLanguages(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile("../../../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatalf("read the spec: %v", err)
	}
	var lines []string
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), specInPlaceMarker) {
			lines = append(lines, strings.TrimSpace(ln))
		}
	}
	if len(lines) != 1 {
		t.Fatalf("docs/SPEC-TABLES.md §3.4 must carry exactly one %q line, found %d", specInPlaceMarker, len(lines))
	}
	set := map[string]bool{}
	for _, l := range strings.Split(strings.TrimPrefix(lines[0], specInPlaceMarker), ",") {
		if l = strings.TrimSpace(l); l != "" {
			set[l] = true
		}
	}
	return set
}

// jsIdentityDoor holds the record loop of <root>FixedLoad to §3.4's rule. When
// the spec lists javascript, the load has EXACTLY ONE door, of exactly this
// shape: it is chosen by the identity plan with an empty hole list, projects
// from the FILE's view at the record body, and refuses the way the shared path
// does; the shared path (TableFixedRun into the image, then the projection from
// the image) is still there for every compiled plan. When the spec does not
// list javascript, there is no door at all. Any other second reader is refused.
func jsIdentityDoor(t *testing.T, load, root string) {
	t.Helper()
	allowed := specInPlaceLanguages(t)["javascript"]
	decl := fmt.Sprintf("const identity = entries === %sFixedIdentity && holeN === 0;", root)
	door := fmt.Sprintf("if (identity) {\n"+
		"      %sFixedDecode(values[k], srcView, at + 8, stage);\n"+
		"      if (stage.refused !== 0) { report.refused = stage.refused; report.malformed = stage.malformed; return -1; }\n"+
		"      at += recordBytes;\n"+
		"      continue;\n"+
		"    }", root)
	if !allowed {
		if strings.Contains(load, "if (identity") {
			t.Error("the load grew a second reader and docs/SPEC-TABLES.md §3.4 does not list javascript")
		}
		return
	}
	if n := strings.Count(load, decl); n != 1 {
		t.Errorf("the in-place door must be chosen exactly once, by the identity plan with no holes: %q appears %d times", decl, n)
	}
	if n := strings.Count(load, door); n != 1 {
		t.Errorf("the in-place door must be exactly §3.4's shape (project from srcView at the body, refuse as the shared path does); found %d", n)
	}
	// NOTHING ELSE BRANCHES ON IDENTITY: the declaration and the door are the
	// only two mentions in the load's code (its comments may say why).
	if n := strings.Count(jsCode(load), "identity"); n != 2 {
		t.Errorf("identity is named %d times in the load; the declaration and the one door are all §3.4 allows", n)
	}
	// AND THE SHARED PATH STAYS, for every compiled plan.
	if !strings.Contains(load, "TableFixedRun(entries, entryCount, bytes, srcView, at + 8, image, imageView, remap, stage);") {
		t.Error("the shared path's plan loop is gone; compiled plans keep it")
	}
	if !strings.Contains(load, root+"FixedDecode(values[k], imageView, 0, stage)") {
		t.Error("the shared path's projection from the image is gone; compiled plans keep it")
	}
}

// jsCode is generated source with its line comments removed.
func jsCode(src string) string {
	var b strings.Builder
	for _, ln := range strings.Split(src, "\n") {
		if i := strings.Index(ln, "//"); i >= 0 {
			ln = ln[:i]
		}
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	return b.String()
}

// TestSpecInPlaceListHasAGuard: every language §3.4 lists has its door guarded.
// JavaScript's is jsIdentityDoor; a language added to the list without its own
// package's narrowing of its no-second-reader guard is refused here by name.
func TestSpecInPlaceListHasAGuard(t *testing.T) {
	t.Parallel()
	guarded := map[string]bool{"javascript": true}
	for lang := range specInPlaceLanguages(t) {
		if !guarded[lang] {
			t.Errorf("docs/SPEC-TABLES.md §3.4 lists %q with an in-place identity read, and no guard here allows it: "+
				"narrow that language's own no-second-reader guard to the list and add it to this set", lang)
		}
	}
}

// TestJSFixedInPlaceEqualsSharedPath holds §3.4's equality rule. One generated
// module tree is the shipped reader; its twin is the same tree with the door
// closed (`const identity = false;`), which is the shared path: prefill, the
// identity plan's one copy into the image, and the projection from the image.
// Both read the same files: a clean 64-record file, and 2000 single-record files
// each with a few (or up to 96) body bytes overwritten by a seeded generator (no clock, no
// randomness from outside), which lands clamps, counts past their bound, bad
// text and refusals. Every read must agree on n, on every report field, on the
// value, and on the bytes the value saves back to.
func TestJSFixedInPlaceEqualsSharedPath(t *testing.T) {
	t.Parallel()
	if !specInPlaceLanguages(t)["javascript"] {
		t.Skip("docs/SPEC-TABLES.md §3.4 does not list javascript: there is no in-place path to compare")
	}
	node := jsNode(t)
	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	files, err := js.Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate tables: %v", err)
	}
	maps.Copy(files, tables)
	const root = "FixedTable"
	decl := "const identity = entries === " + root + "FixedIdentity && holeN === 0;"
	dir := t.TempDir()
	closed := 0
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		for _, side := range []string{"inplace", "shared"} {
			body := string(data)
			if side == "shared" && strings.Contains(body, decl) {
				body = strings.Replace(body, decl, "const identity = false;", 1)
				closed++
			}
			if err := os.MkdirAll(filepath.Join(dir, side), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, side, name), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if closed != 1 {
		var names []string
		for name := range files {
			names = append(names, name)
		}
		t.Fatalf("the door's declaration was closed in %d generated files, want exactly 1 (files %v)", closed, names)
	}
	home := runtimeHome(u)
	probe := strings.NewReplacer("ROOT", root, "HOME", home).Replace(`
import * as A from "./inplace/ROOTTable.js";
import * as B from "./shared/ROOTTable.js";
import * as AT from "./inplace/HOMETable.js";
import * as BT from "./shared/HOMETable.js";

let failures = 0;
function fail(why) { if (failures < 10) { console.error("FAIL: " + why); } failures++; }
// a VALUE comparison: a BigInt and a non-finite float are spelled, never dropped
function show(v) {
  return JSON.stringify(v, (k, x) => typeof x === "bigint" ? "n" + x.toString()
    : (typeof x === "number" && !Number.isFinite(x)) ? "f" + String(x) : x);
}
function rep(r) {
  return [r.refused, r.malformed, r.unknown, r.kindMismatch, r.clamped, r.widened, r.duplicate, r.layoutHash].map(String).join(",");
}
function same(a, b) { if (a.length !== b.length) return false; for (let i = 0; i < a.length; i++) { if (a[i] !== b[i]) return false; } return true; }
// a seeded xorshift32: the same files on every run
let s = 0x9e3779b9;
function rnd() { s ^= s << 13; s >>>= 0; s ^= s >>> 17; s ^= s << 5; s >>>= 0; return s; }
function fresh(T, n) { const v = []; for (let k = 0; k < n; k++) { const x = new T.ROOT(); T.InitROOT(x); v.push(x); } return v; }

const N = 64;
const need = A.ROOTFixedMeasure(N);
const clean = new Uint8Array(need);
if (A.ROOTFixedSave(fresh(AT, N), N, clean) !== need) { fail("the clean file did not save"); }
const recordBytes = A.ROOTFixedRecordBytes;
const bodyAt = need - N * recordBytes;

const planA = A.ROOTFixedNewPlan(), planB = B.ROOTFixedNewPlan();
const outcomes = { ok: 0, refused: 0, clamped: 0 };
function compare(label, bytes, n) {
  const va = fresh(AT, n), vb = fresh(BT, n);
  const ra = new AT.TableFixedReport(), rb = new BT.TableFixedReport();
  const na = A.ROOTFixedLoad(va, n, bytes, bytes.length, planA, ra);
  const nb = B.ROOTFixedLoad(vb, n, bytes, bytes.length, planB, rb);
  if (na !== nb) { fail(label + ": n " + na + " in place, " + nb + " shared"); return; }
  if (rep(ra) !== rep(rb)) { fail(label + ": report " + rep(ra) + " in place, " + rep(rb) + " shared"); return; }
  for (let k = 0; k < n; k++) {
    if (show(va[k]) !== show(vb[k])) { fail(label + ": record " + k + " differs by value"); return; }
  }
  if (na < 0) { outcomes.refused++; return; }
  outcomes.ok++;
  if (ra.clamped > 0) { outcomes.clamped++; }
  const sa = new Uint8Array(A.ROOTFixedMeasure(na)), sb = new Uint8Array(B.ROOTFixedMeasure(nb));
  if (A.ROOTFixedSave(va, na, sa) !== sa.length || B.ROOTFixedSave(vb, nb, sb) !== sb.length) { fail(label + ": a loaded value did not save"); return; }
  if (!same(sa, sb)) { fail(label + ": the two loads save back to different bytes"); }
}

compare("clean 64", clean, N);
const one = clean.slice(0, bodyAt + recordBytes);
for (let c = 0; c < 2000; c++) {
  const f = new Uint8Array(one);
  const hits = 1 + (rnd() % ((c & 1) ? 96 : 8)); // a few bytes, or many
  for (let h = 0; h < hits; h++) {
    f[bodyAt + 8 + (rnd() % (recordBytes - 8))] = rnd() & 0xff;
  }
  compare("case " + c, f, 1);
}
if (outcomes.ok < 100 || outcomes.refused < 100 || outcomes.clamped < 10) {
  fail("the damaged files did not cover both outcomes: " + JSON.stringify(outcomes));
}
if (failures !== 0) { process.exit(1); }
console.log("ok " + JSON.stringify(outcomes));
`)
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("in place and shared path disagree: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
