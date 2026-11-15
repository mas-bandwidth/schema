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
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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
	for ln := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), specInPlaceMarker) {
			lines = append(lines, strings.TrimSpace(ln))
		}
	}
	if len(lines) != 1 {
		t.Fatalf("docs/SPEC-TABLES.md §3.4 must carry exactly one %q line, found %d", specInPlaceMarker, len(lines))
	}
	set := map[string]bool{}
	for l := range strings.SplitSeq(strings.TrimPrefix(lines[0], specInPlaceMarker), ",") {
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
		"      if (stage.refused !== 0) { return %sFixedRefuseBatch(values, k, stage, report); }\n"+
		"      at += recordBytes;\n"+
		"      continue;\n"+
		"    }", root, root)
	// A ROOT WITH DEAD BYTES ALSO CHECKS THEM ON THE FILE BEFORE THE PROJECTION
	// (docs/FIXED-FORM-ALGORITHM.md §4.5, §5.3 step 11).
	dead := fmt.Sprintf("if (identity) {\n"+
		"      %sFixedCheckDead(srcView, at + 8, stage);\n"+
		"      if (stage.refused !== 0) { return %sFixedRefuseBatch(values, k, stage, report); }\n", root, root) +
		door[len("if (identity) {\n"):]
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
		if d := strings.Count(load, dead); d != 1 {
			t.Errorf("the in-place door must be exactly §3.4's shape (project from srcView at the body, refuse as the shared path does); found %d without dead bytes and %d with them", n, d)
		}
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
	for ln := range strings.SplitSeq(src, "\n") {
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
// randomness from outside), which lands counts past their bound, bad text and
// named refusals. Every read must agree on n, on every report field, on the
// value, and on the bytes the value saves back to.
func TestJSFixedInPlaceEqualsSharedPath(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

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
// THE VERDICT, never the refusal's name (docs/rulings.json
// refusal-verdict-only): a record can be dead-byte damage and out of range at
// once, and the first condition each path meets names it.
function rep(r) {
  return [r.damaged, r.unknown, r.mismatch, r.oor, r.widened, r.repeated, r.layoutHash].map(String).join(",");
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
const outcomes = { ok: 0, refused: 0 };
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
  const sa = new Uint8Array(A.ROOTFixedMeasure(na)), sb = new Uint8Array(B.ROOTFixedMeasure(nb));
  if (A.ROOTFixedSave(va, na, sa) !== sa.length || B.ROOTFixedSave(vb, nb, sb) !== sb.length) { fail(label + ": a loaded value did not save"); return; }
  if (!same(sa, sb)) { fail(label + ": the two loads save back to different bytes"); }
}

compare("clean 64", clean, N);
// ONE POPULATED RECORD: every live field carries a value, so its non-zero
// bytes are the live ones and its dead bytes are still the writer's zeros
// (docs/FIXED-FORM-ALGORITHM.md §3.1).
function populate(v) {
  v.sequence = 0x1234;
  v.ackSequence = 4321;
  v.ackBits = 0xDEADBEEF;
  v.sessionId = 0x0123456789ABCDEFn;
  v.clientId = 0x11223344;
  v.nonce = 0x00FEDCBA98765432n;
  v.worldTime = 123456789n;
  v.frameTick = 0x123456789ABCn;
  v.serverTime = 1234;
  v.entitiesCount = 8;
  for (let i = 0; i < 8; i++) {
    const e = v.entities[i];
    e.entityId = i + 1;
    e.posX = i + 1; e.posY = i + 2; e.posZ = i + 3;
    e.yaw = i + 1; e.pitch = i + 2;
    e.velX = i + 1; e.velY = i + 2; e.velZ = i + 3;
    e.health = i + 10;
    e.weapon = i & 15;
    e.damage = BigInt(i + 1);
    e.moving = (i & 1) === 0;
    e.firing = (i & 2) === 0;
  }
  v.statsCount = 80;
  for (let i = 0; i < 80; i++) { v.stats[i].statId = i & 0xff; v.stats[i].delta = i - 40; }
  v.gameEvent.type = 1;
  v.gameEvent.hit.targetId = 7;
  v.gameEvent.hit.damage = 11;
  v.gameEvent.hit.hitKind = 3;
  v.gameEvent.hit.crit = true;
  v.loadout[0] = 1; v.loadout[1] = 2; v.loadout[2] = 3; v.loadout[3] = 4;
  for (let i = 0; i < 8; i++) { v.playerName[i] = 97 + i; }
  v.playerNameLength = 8;
  for (let i = 0; i < 8; i++) { v.payload[i] = 1 + i; }
  v.payloadLength = 8;
  v.aimX = 0.5; v.aimY = -0.5; v.aimZ = 0.25;
  v.recoil = 1.5; v.drift = 2.5;
  v.wideKey = 0x0123456789ABCDEF0123456789ABCDEFn;
  v.flux = 123456789n;
  v.ping = 123; v.crcHint = 0x123456;
  v.hasExtra = true; v.extra = 7; v.idleTicks = 3;
}
const oneValue = fresh(AT, 1);
populate(oneValue[0].value);
const one = new Uint8Array(A.ROOTFixedMeasure(1));
if (A.ROOTFixedSave(oneValue, 1, one) !== one.length) { fail("the populated one-record file did not save"); }
for (let c = 0; c < 2000; c++) {
  const f = new Uint8Array(one);
  const hits = 1 + (rnd() % ((c & 1) ? 96 : 8)); // a few bytes, or many
  // THE DAMAGE TOUCHES LIVE BYTES ONLY: every dead byte in the populated image
  // is zero (docs/FIXED-FORM-ALGORITHM.md §3.1), so a mutation that lands on a
  // non-zero byte leaves every dead region zero and the read can still succeed;
  // a mutation of a dead byte is refused by the dead-byte rule and would never
  // exercise the two paths' values. A one-bit change keeps most live values in
  // their declared range, so the damaged set still covers both verdicts.
  for (let h = 0, tries = 0; h < hits && tries < 4096; tries++) {
    const at = bodyAt + 8 + (rnd() % (recordBytes - 8));
    if (one[at] === 0) { continue; }
    f[at] = one[at] ^ 1;
    h++;
  }
  compare("case " + c, f, 1);
}
if (outcomes.ok < 100 || outcomes.refused < 100) {
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
