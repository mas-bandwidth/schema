// W14 — THE PLAN'S DESTINATIONS ARE THIS LEG'S OWN OFFSETS AND SIZES
// (docs/FIXED-FORM-ALGORITHM.md §7, docs/SPEC-TABLES.md §3.4: "THE PLAN'S
// DESTINATIONS ARE ASSERTED AGAINST THE LANGUAGE'S OWN ABI ... every backend
// emits those offsets back as build-time assertions against its own compiler's
// `offsetof` and `sizeof`").
//
// JavaScript has no struct ABI to take an offsetof of (jstable.go's opening
// statement), so this leg's offsets are its own storage: the reader's storage
// IS the canonical body image — the packed wire positions — and its sizeof is
// the constant `<Name>FixedBodyBytes`. The plan's destinations are the
// five-lane `<Name>FixedDst` rows (dst, stride, aux, counted, arg), emitted
// beside `<Name>FixedLayout` and consumed by the plan compiler the lineage
// lays down at module load (`TableFixedLineagePlans`, which the generated
// module itself calls). The identity plan is one whole-body copy, so NO
// identity-path test can see a row that drifted: it would misplace every
// value a compiled (lineage) plan lands (the leg's own Go pin says exactly
// this — internal/codegen/jstable/fixedform_test.go, W14).
//
// This test holds the law against the GENERATED ARTIFACT, the same tree the
// conformance driver loads (`SCHEMA_JS_GENERATED`, default
// build/tables-generated-js), over every table the leg emits:
//
//   1. sizeof — the root layout entry states `<Name>FixedBodyBytes`;
//   2. the dst rows are one five-lane row per layout entry, and every dst
//      and aux offset they resolve to lies inside the body;
//   3. the plan TILES the record: the resolved spans reach the body end
//      exactly (the last field lands at N with W bytes and the body is B);
//   4. the identity plan is one whole-body copy of exactly the body;
//   5. THE OFFSETOF PIN, hand-derived from the declared widths (not copied
//      from the walk), for Keyed.schema's TeamConfig — int32 is four bytes
//      at the field's own byte, so spawn_count's row is {dst:0}; string(16)
//      is a four-byte length at the field's byte with its sixteen-unit
//      buffer behind it, so banner's row is {dst:4, aux:8}, utf8 flavour 1;
//      the body is 4 + 4 + 16 = 24;
//   6. THE LAW'S EFFECT, through the production path: a compiled lineage
//      plan for the artifact's own dst rows lands a writer's values exactly
//      where the emitted decode walk reads them. The writer is a permuted
//      THEIRS layout (banner before spawn_count), built by swapping the two
//      field entries of this build's own layout bytes — ids intact, because
//      the matcher matches by id (§5.2 PLAN) — so a drifted row misplaces
//      the value and the walk reads garbage.
//
// The identity round trip (7) stays on the identity plan and must stay green
// under a dst-row break — the locality that says the bite is the plan's
// destinations and not some other break.
//
// Run from the repository root:  node test/conformance/js/rows/W14.mjs
// Green prints one line per assertion and exits 0; any red prints FAIL and
// exits 1.

import { readdirSync, readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

// The generated tree is a PATH, not a static import, exactly as the driver
// resolves it (test/conformance/js/main.mjs): the harness's negative controls
// point the driver at a sabotaged copy, and this test must follow the same
// aim point.
const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const root = resolve(generated);

let failures = 0;
function check(ok, line) {
  if (ok) {
    console.log("ok " + line);
  } else {
    console.log("FAIL " + line);
    failures++;
  }
}

// ---- parsing the module-private plan rows out of the generated artifact ----

function withoutComments(text) {
  return text.replace(/\/\/[^\n]*/g, "");
}

// `const <Name>FixedDst = new Int32Array([ ... ]);` — the rows may span any
// number of lines; every integer in them is a lane.
function parseInt32ArrayDecl(text, name, suffix) {
  const at = text.indexOf(`const ${name}${suffix} = new Int32Array([`);
  if (at < 0) { return null; }
  const open = text.indexOf("[", at);
  const close = text.indexOf("]);", open);
  if (close < 0) { return null; }
  const body = withoutComments(text.slice(open + 1, close));
  const lanes = body.match(/-?\d+/g);
  return lanes === null ? [] : lanes.map(Number);
}

function loadModule(path) {
  return import(pathToFileURL(path).href);
}

// ---- resolving the five-lane rows through the layout into absolute offsets ----
//
// The layout is a u32 count then 17-byte entries (id u64, kind u8, size u32,
// children u32), pre-order: each row's dst and aux are measured from its
// PARENT, so the walk carries the parent's absolute offset down — the same
// resolution the plan compiler's TableFixedCompileEntry does with myAt.

function u32(view, at) {
  return view.getUint32(at, true);
}

function resolveRows(layout, dstRows) {
  const v = new DataView(layout.buffer, layout.byteOffset, layout.byteLength);
  const count = u32(v, 0);
  const rows = dstRows.length / 5;
  const offsets = new Set();
  let maxEnd = 0;
  let index = 0;
  function walk(parent) {
    if (index >= rows) { return; }
    const i = index++;
    const row = i * 5;
    const at = parent + dstRows[row];
    const entry = 4 + i * 17;
    const size = u32(v, entry + 9);
    const children = u32(v, entry + 13);
    offsets.add(at);
    if (dstRows[row + 2] !== 0) { offsets.add(parent + dstRows[row + 2]); }
    // An entry's span: a counted array or an optional wrapper carries the
    // count / present flag at its aux lane and the payload behind it, so the
    // span's head is the aux; everything else spans from dst.
    const counted = dstRows[row + 3] !== 0;
    const kind = layout[entry + 8];
    const head = counted || kind === 35 ? parent + dstRows[row + 2] : at;
    const end = head + size;
    if (end > maxEnd) { maxEnd = end; }
    for (let c = 0; c < children; c++) { walk(at); }
  }
  walk(0);
  return { count, rows, offsets, maxEnd };
}

// ---- 1..5: every table the leg emits, law per table ----

function listGenerated() {
  const out = [];
  const walk = (dir) => {
    for (const entry of readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) { walk(path); }
      else if (entry.name.endsWith(".js") && readFileSync(path, "utf8").includes("FixedDst = new Int32Array")) {
        out.push(path);
      }
    }
  };
  walk(root);
  return out;
}

const modules = listGenerated();
check(modules.length >= 10, `the generated tree carries the fixed tables (${modules.length} table modules)`);

for (const path of modules) {
  const text = readFileSync(path, "utf8");
  const mod = await loadModule(path).catch((e) => ({ __failed: String(e) }));
  if (mod.__failed !== undefined) {
    check(false, `${path} loads as the driver loads it (${mod.__failed})`);
    continue;
  }
  const rel = path.slice(root.length + 1);
  for (const match of text.matchAll(/const (\w+?)FixedDst = new Int32Array\(/g)) {
    const name = match[1];
    const dstRows = parseInt32ArrayDecl(text, name, "FixedDst");
    const identity = parseInt32ArrayDecl(text, name, "FixedIdentity");
    const body = mod[`${name}FixedBodyBytes`];
    const layout = mod[`${name}FixedLayout`];
    if (dstRows === null || identity === null || body === undefined || layout === undefined) {
      check(false, `${rel}/${name}: the plan rows and their exported facts are all present`);
      continue;
    }
    const { count, rows, offsets, maxEnd } = resolveRows(layout, dstRows);
    check(5 * count === dstRows.length,
      `sizeof/rows ${rel}/${name}: one five-lane dst row per layout entry (${count})`);
    check(u32(new DataView(layout.buffer, layout.byteOffset, layout.byteLength), 13) === body,
      `sizeof ${rel}/${name}: the root layout entry states the ${body}-byte body`);
    let inside = true;
    for (const o of offsets) { if (o < 0 || o >= body) { inside = false; } }
    check(inside, `dst ${rel}/${name}: every dst/aux offset the rows resolve to lies inside the body (${body})`);
    check(maxEnd === body,
      `tiling ${rel}/${name}: the resolved spans reach the body end exactly (${maxEnd} of ${body})`);
    check(identity.length === 9 && identity[0] === 0 && identity[1] === 0 && identity[2] === 0 &&
      identity[3] === body && identity[5] === -1,
      `identity ${rel}/${name}: one whole-body copy of exactly the body (${identity[3]})`);
  }
}

// ---- 5: the offsetof pin, hand-derived from the declared widths ----
//
// Keyed.schema (package tabledemo):
//   fixed table TeamConfig { spawn_count int32 = 4; banner string(16) }
// The canonical image packs the fields in declared order at their widths:
// spawn_count is four bytes at the field's own byte (dst row {0,0,0,0,0});
// banner is a four-byte length at the field's byte with its sixteen-unit
// buffer behind it (dst row {4,0,8,0,1}, utf8 flavour 1); the body is
// 4 + 4 + 16 = 24. THE ROWS ARE THE ARTIFACT'S, parsed above — the same
// numbers a compiled plan lands values through.

const keyed = await loadModule(join(root, "examples", "KeyedTable.js"));
const keyedText = withoutComments(readFileSync(join(root, "examples", "KeyedTable.js"), "utf8"));
const teamDst = parseInt32ArrayDecl(keyedText, "TeamConfig", "FixedDst");
check(JSON.stringify(teamDst.slice(0, 5)) === "[0,0,0,0,0]" &&
  JSON.stringify(teamDst.slice(5, 10)) === "[0,0,0,0,0]" &&
  JSON.stringify(teamDst.slice(10, 15)) === "[4,0,8,0,1]" &&
  keyed.TeamConfigFixedBodyBytes === 24,
  "offsetof pin KeyedTable/TeamConfig: spawn_count {dst:0}, banner {dst:4 aux:8 arg:1}, body 24 — the plan's rows are this build's own offsets");

// ---- 6: the law's EFFECT, through the production path ----
//
// The generated module's own load path: TableFixedLineagePlans(known,
// ownHash, <Name>FixedLayout, <Name>FixedDst, <Name>FixedBodyBytes). Here
// the lock's entry is a permuted THEIRS, so the compiler runs and the
// artifact's own dst rows decide where every value lands.

const runtime = await loadModule(join(root, "examples", "TabledemoTable.js"));
const {
  TableFixedKnownLayout, TableFixedLineagePlans, TableFixedReport, TableFixedResetReport, TableFixedRun,
  TeamConfig, TeamConfigFixedDecode,
} = runtime;

const ownLayout = keyed.TeamConfigFixedLayout;
const body = keyed.TeamConfigFixedBodyBytes;

// THEIRS: this build's own layout with the two FIELD entries swapped. The
// layout is a u32 count then 17-byte entries; entry 0 is the root, so bytes
// 21..38 (spawn_count) and 38..55 (banner) exchange places. Ids move with
// their entries — the matcher matches by id, and the plan compiler walks
// THEIR order, so a plan from this layout must land every value at MY row.
const theirs = new Uint8Array(ownLayout.length);
theirs.set(ownLayout, 0);
theirs.set(ownLayout.subarray(21, 38), 38);
theirs.set(ownLayout.subarray(38, 55), 21);
// The theirs positions follow from the declared widths alone: banner first
// (a four-byte length at 0, its buffer at 4..20), then spawn_count at 20.
// The body stays 24 bytes — a permutation moves nothing else.

const theirsLo = 0x1badb002, theirsHi = 0x0defaced;
const known = [new TableFixedKnownLayout(theirsLo, theirsHi, theirs, theirs.length, 8 + body)];
const plans = TableFixedLineagePlans(known, keyed.TeamConfigFixedHashLo, keyed.TeamConfigFixedHashHi,
  ownLayout, teamDst, body);
const plan = plans[0];
check(plan !== null && plan.ready && plan.why === 0 && plan.unknown === 0 && plan.kindMismatch === 0,
  "compiled plan TeamConfig: the permuted writer's lineage entry compiles with nothing unknown and no kind mismatch");

// THE THEIRS RECORD, derived from the declared widths under THEIRS: the
// length 3 at 0, "hey" at 4..7, the value 42 at 20..24 — inside the field's
// own declared bounds (spawn_count's is 0..64), so nothing clamps.
const src = new Uint8Array(body);
const srcView = new DataView(src.buffer);
srcView.setInt32(0, 3, true);
src.set([0x68, 0x65, 0x79], 4); // "hey"
srcView.setInt32(20, 42, true);

const report = new TableFixedReport();
TableFixedResetReport(report);
TableFixedRun(plan.entries, plan.count, src, srcView, 0, plan.image, plan.view, plan.remap, report);
const value = new TeamConfig();
TeamConfigFixedDecode(value, plan.view, 0, report);
check(value.SpawnCount === 42 && value.BannerLength === 3 &&
  value.Banner[0] === 0x68 && value.Banner[1] === 0x65 && value.Banner[2] === 0x79 &&
  report.clamped === 0 && report.unknown === 0 && report.kindMismatch === 0,
  "plan effect TeamConfig: the permuted writer's values land exactly where this build's decode walk reads them (42, \"hey\")");

// ---- 7: the identity round trip, which a dst-row break must NOT bite ----

const caller = keyed.TeamConfigFixedNewPlan(64, 64);
const out = new TeamConfig();
out.SpawnCount = 42;
out.BannerLength = 3;
out.Banner.set([0x68, 0x65, 0x79], 0); // "hey"
const file = new Uint8Array(keyed.TeamConfigFixedMeasure(1));
check(keyed.TeamConfigFixedSave([out], 1, file) === file.length,
  "identity round trip TeamConfig: the artifact's own writer writes its own record");
const back = new TeamConfig();
const readReport = new TableFixedReport();
TableFixedResetReport(readReport);
const n = keyed.TeamConfigFixedLoad([back], 1, file, file.length, caller, readReport);
check(n === 1 && back.SpawnCount === 42 && back.BannerLength === 3 &&
  back.Banner[0] === 0x68 && back.Banner[1] === 0x65 && back.Banner[2] === 0x79,
  "identity round trip TeamConfig: the artifact's own read lands its own writer's values");

if (failures > 0) {
  console.log(`FAILED: ${failures} red assertion(s)`);
  process.exit(1);
}
console.log("W14: the plan's destinations are this leg's own offsets and sizes");
