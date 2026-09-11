package jstable

// THE VERSIONING FIXTURES ON THE JAVASCRIPT LEG (docs/FIXED-FORM-VERSIONING-TESTS.md,
// the procedures in docs/FIXED-FORM-ALGORITHM.md §5). Every row of that page is
// one definition change and owes TWO read columns:
//
//   NEW-READS-OLD    the widened reader reads the older writer's file: every
//                    old value lands exactly, the reader's tail is its declared
//                    default, and the counters are §5.4's — for an APPEND, all
//                    of them zero, because an append the reader knows is not an
//                    event.
//   OLD-REFUSES-NEW  the older reader given the widened writer's file refuses
//                    `layout_newer` BEFORE any record, reporting THE FILE'S
//                    HASH and nothing else, with no counter moved and
//                    `malformed` false (§5.3's joint answer).
//
// The bytes are the C++ reference's: `make tables-fixedform-corpus` writes
// `build/fixedform-corpus/old_<row>.bin` and `new_<row>.bin`, and the rows that
// are not a pair have the dump's own names (§5.9 #28) — `old_floor.bin`,
// `mid_floor.bin`, `new_floor.bin`, and `old_`/`a_`/`b_`/`new_lineage_merge.bin`.
//
// THE PROBE UNIT IS THE COLUMN (§5.9 #18): two generations of ONE table name
// have no spelling inside one ES module, so each row's each column is its own
// generated module tree, run with the leg's own toolchain — $(NODE), the same
// binary make/js.mk uses.
//
// THE LINEAGE IS DATA THE BUILD HANDS THE BACKEND (§5.2, COMPILE(lock, T)): here
// the test plays the lock, handing the newer unit the older unit's locked entry
// — the wire hash, the layout bytes verbatim and the record size. A reader NEVER
// parses the layout a file carries; it matches the header's hash against the
// lineage and compares the bytes it already holds.
//
// THE 0x5A POISON (§5.7, §5.9 #17) IS LAID THROUGH plan.image. A JavaScript
// value is an object graph and has no byte view, so the byte destination this
// leg's load writes — and the one the prefill answers — is the plan's record
// image. Poisoning it is the only instrument that can tell a prefill that ran
// from one that never did: a zero-fill looks exactly like a zero default.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- the rows ---------------------------------------------------------------

type jsVersionRow struct {
	row string
	// sameHash is a row whose edit moves no layout byte and no digest byte
	// (`rename_without_was`, `field_deprecate`, `field_undeprecate`): the two
	// hashes are equal, both sides take the identity plan and the file READS in
	// BOTH directions — which is the test that the hash, and only the hash, is
	// the version (§5.7).
	sameHash bool
	// widens is a row that grows a WIDTH, so `widened` moves at least once;
	// every other row is an APPEND and owes every counter at zero (§5.9 #10).
	widens bool
	// unported is a row whose schema this backend refuses to generate at all —
	// kind 33, `wstring(N)`, is still refused in a JavaScript table
	// (compiler/target_javascript.go, refuseWideText). It is NOT a §5 red: the
	// row is named here, skipped by name, and owed the day the kind lands.
	unported string
	// check is JavaScript asserting the landed values, over `back[0]`.
	check string
}

var jsVersionRows = []jsVersionRow{
	{row: "array_bounded_grow"},
	{row: "array_elem_widen", widens: true},
	{row: "array_fixed_grow"},
	{row: "bits_grow", widens: true},
	{row: "bytes_grow"},
	{row: "constant_grow"},
	{row: "enum_append"},
	{row: "enum_width", widens: true},
	{row: "field_append", check: `
  if (back[0].X !== 11 || back[0].Y !== 22 || back[0].Z !== 33) {
    fail("the old writer's values did not land: " + show(back[0]));
  }
  if (back[0].W !== 77) {
    fail("the appended field is not its declared default: " + show(back[0]));
  }`},
	{row: "field_deprecate", sameHash: true},
	{row: "field_undeprecate", sameHash: true},
	{row: "fixed_I_grow", widens: true},
	{row: "flags_append"},
	{row: "float_widen", widens: true},
	{row: "int_widen", widens: true, check: `
  if (n !== 3) { fail("the int_widen file carries three records, not " + n); }
  const want = [-1, -32768, 32767];
  for (let k = 0; k < want.length; k++) {
    if (Number(back[k].V) !== want[k]) { fail("record " + k + " widened wrong: " + show(back[k])); }
    if (back[k].Lead !== 0xAAAAAAAA || back[k].Trail !== 0xBBBBBBBB) {
      fail("record " + k + " moved a neighbour: " + show(back[k]));
    }
  }`},
	{row: "keyed_array_enum_append"},
	{row: "nested_append"},
	{row: "optional_add"},
	{row: "range_widen"},
	{row: "rename_without_was", sameHash: true},
	{row: "string_grow"},
	{row: "uint_widen", widens: true},
	{row: "union_append"},
	{row: "union_arm_payload_widen"},
	{row: "wstring_grow", unported: "kind 33, wstring(N), is refused in a JavaScript table (schema#366 / refuseWideText)"},
}

// ---- NEW-READS-OLD ----------------------------------------------------------

func TestJSFixedVersioningNewReadsOld(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	for _, r := range jsVersionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			if r.unported != "" {
				t.Skipf("NOT PORTED, not a §5 red: %s", r.unported)
			}
			newer := jsReadSchema(t, "VNEW_"+r.row)
			older := jsReadSchema(t, "VOLD_"+r.row)
			table := jsFixedRootName(t, older)
			counters := `if (r.widened !== 0) { fail("widened " + r.widened + ", and an append is not an event"); }`
			if r.widens {
				counters = `if (r.widened === 0) { fail("a widening row moved no widened counter"); }`
			}
			src := fmt.Sprintf(`
const data = readFileSync(%q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport();
const plan = TNewPlan(4096, 4096);
// THE POISON, through the byte destination this leg writes (§5.7, §5.9 #17).
plan.image.fill(0x5A);
const n = TLoad(back, back.length, data, data.length, plan, report);
const r = report;
if (n < 1) { fail("the newer reader refused the older writer's file: n=" + n + " " + reason(r)); }
if (r.refused !== 0 || r.malformed) { fail("a clean NEW-READS-OLD is not a refusal: " + reason(r)); }
if (r.unknown !== 0 || r.kindMismatch !== 0 || r.clamped !== 0 || r.duplicate !== 0) {
  fail("counters moved on a clean backward read: " + reason(r));
}
%s
%s
`, filepath.Join(corpus, "old_"+r.row+".bin"), counters, r.check)
			// The newer unit is handed the OLDER unit's locked lineage entry.
			out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, src)
			if err != nil {
				t.Fatalf("NEW-READS-OLD %s: %v\n%s", r.row, err, out)
			}
		})
	}
}

// ---- OLD-REFUSES-NEW --------------------------------------------------------

func TestJSFixedVersioningOldRefusesNew(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	for _, r := range jsVersionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			if r.unported != "" {
				t.Skipf("NOT PORTED, not a §5 red: %s", r.unported)
			}
			older := jsReadSchema(t, "VOLD_"+r.row)
			table := jsFixedRootName(t, older)
			file := filepath.Join(corpus, "new_"+r.row+".bin")
			var body string
			if r.sameHash {
				// A row whose edit moves no layout byte and no digest byte has
				// NO second column: the hashes are equal and the file reads in
				// both directions (§5.7).
				body = `
if (n < 1) { fail("a row that moved no layout byte must READ in both directions: n=" + n + " " + reason(r)); }
if (r.refused !== 0 || r.malformed) { fail("an equal hash is not a version: " + reason(r)); }`
			} else {
				body = `
if (n !== -1) { fail("the older reader did not refuse the newer writer's file: n=" + n + " " + reason(r)); }
if (r.refused !== TableFixedRefusal.LayoutNewer) {
  fail("a hash in no lineage entry owes layout_newer, not " + TableFixedRefusalName(r.refused));
}
if (r.layoutHash !== want) {
  fail("layout_newer reports THE FILE'S hash: " + want.toString(16) + ", not " + r.layoutHash.toString(16));
}
if (r.malformed) { fail("a refusal by name never sets malformed too (§5.3, the joint answer)"); }
if (r.widened !== 0 || r.unknown !== 0 || r.kindMismatch !== 0 || r.clamped !== 0 || r.duplicate !== 0) {
  fail("REFUSE is total: no counter moves: " + reason(r));
}
// THE DESTINATION IS STILL EVERY FIELD OF A FRESH VALUE — VALUES, never a
// struct's slack (§5.9 #17).
if (show(back[0]) !== show(fresh)) {
  fail("REFUSE wrote destination bytes: " + show(back[0]));
}`
			}
			src := fmt.Sprintf(`
const data = readFileSync(%q);
const view = new DataView(data.buffer, data.byteOffset, data.length);
const want = view.getBigUint64(8, true);
const fresh = new T(); InitT(fresh);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport();
const plan = TNewPlan(4096, 4096);
const n = TLoad(back, back.length, data, data.length, plan, report);
const r = report;
%s
`, file, body)
			out, err := jsRunVersionProbe(t, node, older, nil, 0, table, src)
			if err != nil {
				t.Fatalf("OLD-REFUSES-NEW %s: %v\n%s", r.row, err, out)
			}
		})
	}
}

// ---- the floor --------------------------------------------------------------
//
// The floor is ONE NUMBER and the lineage is ONE ARRAY, so "retired" is an index
// cut and the operator's two answers stay distinct: below the floor is
// `layout_unsupported` (upgrade the client), outside the lineage is
// `layout_newer` (ship the reader). §5.2, §5.7's three floor rows.

func TestJSFixedVersioningFloor(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	older := jsReadSchema(t, "VOLD_floor")
	mid := jsReadSchema(t, "VMID_floor")
	newer := jsReadSchema(t, "VNEW_floor")
	table := jsFixedRootName(t, older)

	probe := func(t *testing.T, retire int, body string) {
		t.Helper()
		src := fmt.Sprintf(`
const oldFile = readFileSync(%q);
const midFile = readFileSync(%q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport();
const plan = TNewPlan(4096, 4096);
const r = report;
%s
`, filepath.Join(corpus, "old_floor.bin"), filepath.Join(corpus, "mid_floor.bin"), body)
		out, err := jsRunVersionProbe(t, node, newer, []string{older, mid}, retire, table, src)
		if err != nil {
			t.Fatalf("floor: %v\n%s", err, out)
		}
	}

	t.Run("floor_at", func(t *testing.T) {
		t.Parallel()
		// Nothing retired: the OLDEST file is at the floor and it reads.
		probe(t, 0, `
const n = TLoad(back, back.length, oldFile, oldFile.length, plan, report);
if (n < 1 || r.refused !== 0 || r.malformed) { fail("a file AT the floor must read: n=" + n + " " + reason(r)); }`)
	})
	t.Run("floor_below", func(t *testing.T) {
		t.Parallel()
		// Entry 0 retired: the floor is 1, and the file one below it refuses
		// layout_unsupported — nothing decoded, no counter moved.
		probe(t, 1, `
const n = TLoad(back, back.length, oldFile, oldFile.length, plan, report);
if (n !== -1 || r.refused !== TableFixedRefusal.LayoutUnsupported) {
  fail("a file below the floor owes layout_unsupported: n=" + n + " " + reason(r));
}
const viewOld = new DataView(oldFile.buffer, oldFile.byteOffset, oldFile.length);
if (r.layoutHash !== viewOld.getBigUint64(8, true)) {
  fail("layout_unsupported reports the file's hash too (§5.9 #7): " + r.layoutHash.toString(16));
}
if (r.malformed || r.widened !== 0 || r.unknown !== 0 || r.clamped !== 0) { fail("REFUSE is total: " + reason(r)); }
TableFixedResetReport(report);
const m = TLoad(back, back.length, midFile, midFile.length, plan, report);
if (m < 1) { fail("the file AT the raised floor must still read: " + m + " " + reason(r)); }`)
	})
	t.Run("floor_raise_live", func(t *testing.T) {
		t.Parallel()
		// The floor raised by one: the file that read yesterday refuses today.
		probe(t, 2, `
const n = TLoad(back, back.length, midFile, midFile.length, plan, report);
if (n !== -1 || r.refused !== TableFixedRefusal.LayoutUnsupported) {
  fail("the floor raised by one: yesterday's file must refuse: n=" + n + " " + reason(r));
}`)
	})
}

// ---- the hash ---------------------------------------------------------------

func TestJSFixedVersioningHash(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	newer := jsReadSchema(t, "VNEW_field_append")
	older := jsReadSchema(t, "VOLD_field_append")
	table := jsFixedRootName(t, older)
	old := filepath.Join(corpus, "old_field_append.bin")
	fresh := filepath.Join(corpus, "new_field_append.bin")

	cases := []struct {
		name string
		src  string
	}{
		// hash_unknown: a hash in no lineage refuses layout_newer.
		{"hash_unknown", fmt.Sprintf(`
const data = readFileSync(%q);
const view = new DataView(data.buffer, data.byteOffset, data.length);
view.setBigUint64(8, 0xDEADBEEFCAFEF00Dn, true);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport(); const r = report;
const n = TLoad(back, back.length, data, data.length, TNewPlan(4096, 4096), report);
if (n !== -1 || r.refused !== TableFixedRefusal.LayoutNewer || r.layoutHash !== 0xDEADBEEFCAFEF00Dn || r.malformed) {
  fail("hash_unknown: n=" + n + " " + reason(r) + " hash=" + r.layoutHash.toString(16));
}`, old)},
		// hash_known_bytes_differ: a KNOWN hash whose layout bytes differ from
		// the lock's is ONE name, layout_malformed — "a lie about a known
		// version". The seven §1.1 malformations under a known hash all land
		// here, never in a runtime walk (§5.3).
		{"hash_known_bytes_differ", fmt.Sprintf(`
const data = readFileSync(%q);
data[20] ^= 0xFF;
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport(); const r = report;
const n = TLoad(back, back.length, data, data.length, TNewPlan(4096, 4096), report);
if (n !== -1 || r.refused !== TableFixedRefusal.LayoutMalformed || r.malformed) {
  fail("hash_known_bytes_differ: n=" + n + " " + reason(r));
}`, old)},
		// hash_identity: the reader's own hash selects the identity plan.
		{"hash_identity", fmt.Sprintf(`
const data = readFileSync(%q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport(); const r = report;
const n = TLoad(back, back.length, data, data.length, TNewPlan(4096, 4096), report);
if (n < 1 || r.refused !== 0 || r.malformed) { fail("hash_identity: n=" + n + " " + reason(r)); }
if (back[0].W !== 777) { fail("the identity plan lost a value: " + show(back[0])); }
if (r.layoutHash !== 0n) { fail("layout_hash is zero on every path but the two layout refusals (§5.9 #15)"); }`, fresh)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, c.src)
			if err != nil {
				t.Fatalf("%s: %v\n%s", c.name, err, out)
			}
		})
	}
}

// ---- lineage_merge ----------------------------------------------------------
//
// Two branches append different fields; after the merge BOTH pre-merge files
// read on the merged build (the name-subset rule, bill §8a.1).

func TestJSFixedVersioningLineageMerge(t *testing.T) {
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	merged := jsReadSchema(t, "VNEW_lineage_merge")
	a := jsReadSchema(t, "VBRA_lineage_merge")
	b := jsReadSchema(t, "VBRB_lineage_merge")
	base := jsReadSchema(t, "VOLD_lineage_merge")
	table := jsFixedRootName(t, base)
	src := fmt.Sprintf(`
for (const name of [%q, %q]) {
  const data = readFileSync(name);
  const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
  const report = new TableFixedReport(); const r = report;
  const n = TLoad(back, back.length, data, data.length, TNewPlan(4096, 4096), report);
  if (n < 1 || r.refused !== 0 || r.malformed) {
    fail(name + ": both pre-merge files read on the merged build: n=" + n + " " + reason(r));
  }
}`, filepath.Join(corpus, "a_lineage_merge.bin"), filepath.Join(corpus, "b_lineage_merge.bin"))
	out, err := jsRunVersionProbe(t, node, merged, []string{base, a, b}, 0, table, src)
	if err != nil {
		t.Fatalf("lineage_merge: %v\n%s", err, out)
	}
}

// ---- union_arm_text ---------------------------------------------------------
//
// A TEXT FIELD UNDER A UNION'S ARM, ACROSS TWO GENERATIONS (test/tables/UT1.schema,
// UT2.schema; docs/SPEC-TABLES.md §3.4). A plan entry under a union arm carries
// TWO facts that are not the same fact: THE VALUE THE GUARD BYTE MUST HOLD for
// the entry to run — the WRITER's ordinal — and the argument the entry's own op
// takes, which for a text entry is ITS FLAVOUR. They had one lane between them.
//
// The case belongs HERE and not on the byte driver (test/js-tables/fixedform.mjs,
// where it was `unionArmText`) because BOTH of its directions are a stranger's
// layout: UT2 inserts `mid` IN THE MIDDLE of the union, so the arm carrying the
// `string(8)` moves from ordinal 2 to ordinal 3 and neither generation can read
// the other's file without the other's locked entry. The driver has no lineage to
// hand in; this harness is where the lock is played (§5.2, COMPILE(lock, T)).
//
// So it is two probes. UT1 writes its own arm-b record — an identity save, no
// lineage — and UT2 reads that file with UT1's entry handed in: the guard must
// hold THEIR ordinal (2) while the tag this reader stores is MINE (3), and the
// text entry's flavour is neither number. Share the two lanes and the entry runs
// under the wrong arm or with the wrong flavour, and a byte string becomes a WIDE
// one — which is why `make tables-js-union-arm-text-negative-control` sabotages
// exactly that pair and asserts this case reds.
func TestJSFixedVersioningUnionArmText(t *testing.T) {
	jsFixedCorpus(t) // the leg's gate promises the oracle; this case needs only node
	node := jsNode(t)
	ut1 := jsReadSchema(t, "UT1")
	ut2 := jsReadSchema(t, "UT2")
	table := jsFixedRootName(t, ut1)
	file := filepath.Join(t.TempDir(), "ut1_arm_b.bin")

	// UT1 WRITES ARM `b` AT ORDINAL 2 — its own layout, its own plan.
	write := fmt.Sprintf(`
const { writeFileSync } = await import("node:fs");
const { PickType } = await import("./Probe.js");
const v = new T(); InitT(v);
v.Head = 81;
v.Pick.Type = PickType.B;
const LABEL = "back";
for (let i = 0; i < LABEL.length; i++) { v.Pick.B.Label[i] = LABEL.charCodeAt(i); }
v.Pick.B.LabelLength = LABEL.length;
v.Pick.B.M = -12;
v.Tail = 88;
const buf = new Uint8Array(TMeasure(1));
if (TSave([v], 1, buf) !== buf.length) { fail("union-arm text: UT1 wrote its arm-b record"); }
writeFileSync(%q, buf);
`, file)
	if out, err := jsRunVersionProbe(t, node, ut1, nil, 0, table, write); err != nil {
		t.Fatalf("union_arm_text, UT1's own save: %v\n%s", err, out)
	}

	// AND UT2 READS IT AT ORDINAL 3, through a plan compiled from UT1's entry.
	read := fmt.Sprintf(`
const { PickType } = await import("./Probe.js");
const data = readFileSync(%q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const report = new TableFixedReport(); const r = report;
const plan = TNewPlan(4096, 4096);
// THE POISON (§5.7, §5.9 #17): a prefill that ran and one that never did look
// alike over zeros, and this record has a string's slack behind a used length.
plan.image.fill(0x5A);
const n = TLoad(back, back.length, data, data.length, plan, report);
if (!(n === 1 && !r.malformed)) {
  fail("union-arm text: UT2 read UT1's record through a compiled plan: n=" + n + " " + reason(r));
}
const got = back[0];
let label = "";
for (let i = 0; i < got.Pick.B.LabelLength; i++) { label += String.fromCharCode(got.Pick.B.Label[i]); }
if (!(got.Pick.Type === PickType.B && label === "back" && got.Pick.B.M === -12)) {
  fail("union-arm text: the arm's string and scalar land the other way round too — tag " +
    got.Pick.Type + " " + JSON.stringify(label) + " " + got.Pick.B.M);
}
if (!(got.Head === 81 && got.Tail === 88)) {
  fail("union-arm text: the fields either side of the union are 81/88, got " + got.Head + "/" + got.Tail);
}
`, file)
	if out, err := jsRunVersionProbe(t, node, ut2, []string{ut1}, 0, table, read); err != nil {
		t.Fatalf("union_arm_text, UT2 over UT1's file: %v\n%s", err, out)
	}
}

// ---- the harness ------------------------------------------------------------

func jsFixedCorpus(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../../build/fixedform-corpus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old_field_append.bin")); err != nil {
		// THE SKIP IS A FAILURE WHEN SOMETHING PROMISED THE CORPUS. A bare
		// `go test ./...` on a tree that never built the oracle has nothing to
		// read and says so; but `make tables-js-versioning` builds the corpus
		// first and sets SCHEMA_REQUIRE_CORPUS=1, so under that target a missing
		// file means the build did not do what the target says it did — and a
		// suite that skips itself there would report green over §5 having never
		// run, which is the whole reason this gate exists.
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the reference's byte oracle is not in %s: %v", dir, err)
		}
		t.Skip("the reference's byte oracle is not built: make tables-fixedform-corpus")
	}
	return dir
}

// jsNode is THE LEG'S OWN TOOLCHAIN (§5.9 #18): $NODE, which make/js.mk exports
// as dist/node-v26.7.0-<host>/bin/node, and the dist symlink when it is not set.
func jsNode(t *testing.T) string {
	t.Helper()
	if n := os.Getenv("NODE"); n != "" {
		if _, err := os.Stat(n); err == nil {
			return n
		}
	}
	matches, _ := filepath.Glob("../../../dist/node-*/bin/node")
	for _, m := range matches {
		if abs, err := filepath.Abs(m); err == nil {
			return abs
		}
	}
	if n, err := exec.LookPath("node"); err == nil {
		return n
	}
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatal("SCHEMA_REQUIRE_CORPUS is set and no node is on this bench: make dist, or NODE=node")
	}
	t.Skip("no node toolchain: see make/js.mk's NODE")
	return ""
}

func jsReadSchema(t *testing.T, base string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "test", "tables", base+".schema")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func jsUnitOf(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// jsFixedRootName is §5.9 #9's rule: every declared `fixed table` is a root, and
// the file's root is THE ONE NO OTHER FIXED TABLE NAMES BY VALUE — the outer
// table the dump wrote the row from.
func jsFixedRootName(t *testing.T, src string) string {
	t.Helper()
	u := jsUnitOf(t, src)
	roots := jsFixedUnitRoots(u)
	if len(roots) == 0 {
		t.Fatal("a versioning row declares a fixed root")
	}
	named := map[string]bool{}
	for _, st := range roots {
		for _, f := range st.Fields {
			named[f.Type.Name] = true
		}
	}
	for _, st := range roots {
		if !named[st.Name] {
			return st.Name
		}
	}
	return roots[len(roots)-1].Name
}

// jsRunVersionProbe generates the READER's unit with the OLDER units' locked
// entries handed to it as its lineage — oldest first, the reader's own layout
// last — marks the first `retire` entries retired, which is what moves the
// floor, and runs one generated probe module with the leg's own node.
//
// `T` in the probe source is the row's root table: the probe is written once per
// column and the names are substituted here, because two generations of one
// table name have no spelling inside one module (§5.9 #18).
func jsRunVersionProbe(t *testing.T, node, reader string, older []string, retire int, table, probe string) ([]byte, error) {
	t.Helper()
	u := jsUnitOf(t, reader)
	lineage := map[string][]FixedLineageEntry{}
	for i, src := range older {
		o := jsUnitOf(t, src)
		for _, st := range jsFixedUnitRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			e.Retired = i < retire
			if e.Retired {
				e.Reason = "retired by the test's lock"
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	files, err := js.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	home := runtimeHome(u)
	body := strings.NewReplacer(
		"TLoad", table+"FixedLoad",
		"TNewPlan", table+"FixedNewPlan",
		"TMeasure", table+"FixedMeasure",
		"TSave", table+"FixedSave",
		"InitT", "Init"+table,
		"new T(", "new "+table+"(",
	).Replace(probe)
	src := fmt.Sprintf(`// one generated probe, one column (docs/FIXED-FORM-ALGORITHM.md §5.9 #18).
import { readFileSync } from "node:fs";
// The WRITE half is here for the one case that makes its own file: two
// generations of one table have no spelling in one module, so a case whose file
// is not the reference's writes it with ONE probe and reads it with another.
import { %[1]sFixedLoad, %[1]sFixedNewPlan, %[1]sFixedSave, %[1]sFixedMeasure } from "./ProbeTable.js";
// THE VALUE'S CLASS AND ITS RESET COME FROM THE UNIT'S RUNTIME HOME: a fixed
// table's storage class is emitted there beside the shared runtime, because an
// ES module is file-scoped (fixedmodule.go's home rule).
import { %[1]s, Init%[1]s, TableFixedReport, TableFixedRefusal, TableFixedRefusalName, TableFixedResetReport } from "./%[2]sTable.js";

let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }
// show is a VALUE comparison and a value printing, never a struct's slack
// (§5.9 #17). A flags field rides as a BigInt in this language and
// JSON.stringify refuses one, so the replacer spells it.
function show(v) { return JSON.stringify(v, (k, x) => typeof x === "bigint" ? x.toString() : x); }
function reason(r) {
  return "refused=" + TableFixedRefusalName(r.refused) + " malformed=" + r.malformed +
    " unknown=" + r.unknown + " kindMismatch=" + r.kindMismatch + " clamped=" + r.clamped +
    " widened=" + r.widened + " layoutHash=0x" + r.layoutHash.toString(16);
}
%[3]s
if (failures !== 0) { process.exit(1); }
console.log("ok");
`, table, home, body)
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

var _ = binary.LittleEndian

// TestJSFixedLineageLockBugFailsTheBuild holds §5.9 #8 and #26's FIRST half, the
// half the emitted runtime's own comment already claimed the generator held.
//
// A handed lineage entry whose LAYOUT does not parse, or whose RECORD SIZE is the
// hash alone or less or not what its own layout accounts for, is a BUG IN THE
// LOCK: GENERATE fails, naming the table and the entry's hash, and no module is
// written. It is never a wire event — a reader that routed it into LOAD would
// report a lock bug as a refusal on the first file that matched that hash, and a
// reader that routed the record size into §5.3 step 9 would report it as
// `malformed` arithmetic over a file's tail, with no name at all.
//
// The run-time `layout_malformed` lane stays and is NOT this path: a host that
// hands entries in after the build cannot fail a build that already ran, which
// is #8's second half and why `TableFixedLineagePlans` still carries the name.
func TestJSFixedLineageLockBugFailsTheBuild(t *testing.T) {
	const src = `package probe
table Row {
    keep uint32 = 0
    label string(8) = "fx"
}
`
	u := jsUnitOf(t, src)
	roots := jsFixedUnitRoots(u)
	if len(roots) != 1 {
		t.Fatalf("the probe declares one fixed root, got %d", len(roots))
	}
	name := roots[0].Name
	own, ok := FixedLineageOf(u, name)
	if !ok {
		t.Fatalf("no lineage entry for %s", name)
	}

	// THE CONTROL FIRST: the entry this build computes for itself is lawful, so
	// a failure below is the edit's and never the fixture's.
	if _, err := GenerateLineage(u, map[string][]FixedLineageEntry{name: {own}}); err != nil {
		t.Fatalf("the build's own entry must generate: %v", err)
	}
	if got, want := own.Record, fixedHashBytes+fixedTypeBytes(roots[0]); got != want {
		t.Fatalf("the fixture's record size is %d, want %d", got, want)
	}

	bend := func(f func(e *FixedLineageEntry)) FixedLineageEntry {
		e := own
		e.Layout = append([]byte(nil), own.Layout...)
		f(&e)
		return e
	}

	for _, row := range []struct {
		what string
		e    FixedLineageEntry
	}{
		// ---- §5.9 #8: the LAYOUT is not a layout
		{"a layout shorter than the header", bend(func(e *FixedLineageEntry) { e.Layout = e.Layout[:3] })},
		{"no entries at all", bend(func(e *FixedLineageEntry) { e.Layout = e.Layout[:4] })},
		{"an entry count that does not fit the length", bend(func(e *FixedLineageEntry) {
			binary.LittleEndian.PutUint32(e.Layout, binary.LittleEndian.Uint32(e.Layout)+1)
		})},
		{"a tail past the walk", bend(func(e *FixedLineageEntry) {
			e.Layout = append(e.Layout, make([]byte, fixedEntryBytes)...)
			binary.LittleEndian.PutUint32(e.Layout, binary.LittleEndian.Uint32(e.Layout)+1)
		})},
		{"a root that is not a TABLE", bend(func(e *FixedLineageEntry) { e.Layout[4+8] = 4 })},
		{"a root whose size is not its fields' sum", bend(func(e *FixedLineageEntry) {
			binary.LittleEndian.PutUint32(e.Layout[4+9:], 7)
		})},
		{"a kind outside the closed set", bend(func(e *FixedLineageEntry) { e.Layout[4+fixedEntryBytes+8] = 31 })},
		{"a leaf at a size its kind does not admit", bend(func(e *FixedLineageEntry) {
			// `keep` is a u32, kind 8, and four is the only size it takes; the
			// root's sum moves with it so ONLY the leaf rule can fire.
			root := int(binary.LittleEndian.Uint32(e.Layout[4+9:]))
			binary.LittleEndian.PutUint32(e.Layout[4+fixedEntryBytes+9:], 3)
			binary.LittleEndian.PutUint32(e.Layout[4+9:], uint32(root-1))
		})},
		{"a child count no subtree closes", bend(func(e *FixedLineageEntry) {
			binary.LittleEndian.PutUint32(e.Layout[4+13:], 9)
		})},

		// ---- §5.9 #26: the RECORD SIZE is a lie about a layout that parses
		{"a record size of zero", bend(func(e *FixedLineageEntry) { e.Record = 0 })},
		{"a record size of the hash alone", bend(func(e *FixedLineageEntry) { e.Record = fixedHashBytes })},
		{"a record size one byte short of its layout", bend(func(e *FixedLineageEntry) { e.Record-- })},
		{"a record size one byte long", bend(func(e *FixedLineageEntry) { e.Record++ })},
		{"a negative record size", bend(func(e *FixedLineageEntry) { e.Record = -1 })},
	} {
		t.Run(row.what, func(t *testing.T) {
			files, err := GenerateLineage(u, map[string][]FixedLineageEntry{name: {row.e}})
			if err == nil {
				t.Fatalf("GENERATE must FAIL on %s: a lock bug is never a wire event (§5.9 #8, #26)", row.what)
			}
			if len(files) != 0 {
				t.Fatalf("a failed generate writes no module, got %d", len(files))
			}
			// THE ERROR NAMES THE TABLE AND THE HASH, which is what the operator
			// fixes the lock with: `schema lock` addresses an entry by T@0xhash.
			if msg := err.Error(); !strings.Contains(msg, name) ||
				!strings.Contains(msg, fmt.Sprintf("0x%016x", row.e.Wire)) {
				t.Fatalf("the error names the table and the entry's hash, got %q", msg)
			}
		})
	}

	// AND A RETIRED ENTRY IS STILL CHECKED: the floor stops a FILE from selecting
	// it, it does not make the recorded bytes lawful, and an entry nobody may
	// read is still a lock an operator has to fix.
	dead := bend(func(e *FixedLineageEntry) {
		e.Retired, e.Reason, e.Record = true, "retired by the test's lock", fixedHashBytes
	})
	if _, err := GenerateLineage(u, map[string][]FixedLineageEntry{name: {dead, own}}); err == nil {
		t.Fatal("a RETIRED entry with a bad record size still fails the build (§5.9 #26)")
	}
}
