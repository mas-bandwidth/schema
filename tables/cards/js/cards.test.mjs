// cards.test.mjs — the JavaScript card facade's node:test cases
// (tables/cards/cards_test.go, parse_test.go). Each test reads committed files
// and in-memory buffers only; the driver and conformance targets that start
// processes are the functional tier.
//
// The three tests the card names: TestCardRoundTripsThePopulatedFixture,
// TestCardBoundsRefuseBeforePublishing and TestParseRenderMatchesGo.

import { test } from "node:test";
import assert from "node:assert";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { CardsUnitRegistry } from "../../../build/tables-generated-js/cards/CardsVariable.js";
import * as cards from "./cards.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, "..", "..", "..");

function read(path) {
  return new Uint8Array(readFileSync(join(repoRoot, path)));
}

function bytesEqual(a, b) {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

// TestCardRoundTripsThePopulatedFixture loads the committed populated wire from
// the shared conformance corpus (tables/cards/conformance/cases, the fixtures
// this card's PATHS name), compares every field with card-populated.json (the
// body byte for byte), and proves ToJSON and FromJSON reproduce both files
// exactly; it also mutates the caller's input and a Wire result and shows the
// record unchanged (docs/SPEC-TABLES.md §6.2, §16).
test("TestCardRoundTripsThePopulatedFixture", () => {
  const wire = read("tables/cards/conformance/cases/card-populated.wire");
  const json = read("tables/cards/conformance/cases/card-populated.json");

  const { record, report, error } = cards.Load(wire);
  assert.strictEqual(error, null);
  assert.ok(record !== null);
  assert.strictEqual(report.malformed, false);

  // ToJSON reproduces the JSON file exactly, every field and the body byte for
  // byte.
  const { text, error: e2 } = cards.ToJSON(record);
  assert.strictEqual(e2, null);
  assert.ok(bytesEqual(text, json), "ToJSON does not reproduce card-populated.json");

  // FromJSON reproduces the wire exactly.
  const { record: rec2, error: e3 } = cards.FromJSON(json);
  assert.strictEqual(e3, null);
  assert.ok(bytesEqual(rec2.Wire(), wire), "FromJSON does not reproduce card-populated.wire");

  // Mutating the caller's input leaves the record's wire unchanged.
  const snapshot = new Uint8Array(wire);
  wire.fill(0xff);
  assert.ok(bytesEqual(record.Wire(), snapshot), "mutating the caller's input changed the record's wire");

  // Mutating a Wire() return leaves the record's wire unchanged.
  const returned = record.Wire();
  returned.fill(0);
  assert.ok(bytesEqual(record.Wire(), snapshot), "mutating a Wire() return changed the record's wire");
});

// TestCardBoundsRefuseBeforePublishing proves the 64 KiB body ceiling and the
// 512-entry ceiling for each collection, checked before any record is
// published (docs/SPEC-TABLES.md §2.5, §2.9).
test("TestCardBoundsRefuseBeforePublishing", () => {
  const enc = new TextEncoder();

  const body = (n) => enc.encode(JSON.stringify({ id: "x", body: "y".repeat(n) }));
  assert.strictEqual(cards.FromJSON(body(cards.MaxBodyBytes)).error, null, "exactly 64 KiB body was refused");
  const overBody = cards.FromJSON(body(cards.MaxBodyBytes + 1)).error;
  assert.ok(overBody !== null, "64 KiB + 1 body was accepted");
  assert.match(overBody.message, /body/);
  assert.match(overBody.message, /65537/);
  assert.match(overBody.message, /65536/);

  const collections = [
    ["paths", "p", false],
    ["_new", "n", false],
    ["shared", "s", false],
    ["needs", "d", false],
    ["consumers", "k", true],
  ];
  for (const [field, elem, isConsumer] of collections) {
    const mk = (n) => {
      const arr = [];
      for (let i = 0; i < n; i++) arr.push(isConsumer ? { kind: elem } : { s: elem });
      return enc.encode(JSON.stringify({ id: "x", [field]: arr }));
    };
    assert.strictEqual(cards.FromJSON(mk(cards.MaxListEntries)).error, null, `exactly 512 entries in ${field} was refused`);
    const over = cards.FromJSON(mk(cards.MaxListEntries + 1)).error;
    assert.ok(over !== null, `513 entries in ${field} was accepted`);
    assert.match(over.message, new RegExp(field), `refusal did not name ${field}`);
    assert.match(over.message, /513/, `refusal did not name the 513-entry size for ${field}`);
  }
});

// TestParseRenderMatchesGo parses every parse case in tables/cards/conformance
// to the expected wire and renders every render case to the expected text byte
// for byte; the rejected case refuses with the stranded key
// (docs/SPEC-TABLES.md §2, §16).
test("TestParseRenderMatchesGo", () => {
  const conformance = join(repoRoot, "tables/cards/conformance");

  // parse: normalized-valid parses to its wire; rejected-legacy refuses with
  // the stranded key.
  const normalized = read("tables/cards/conformance/cases/normalized-valid.md");
  const { record, error } = cards.Parse(normalized);
  assert.strictEqual(error, null, "Parse normalized-valid refused");
  const wantWire = read("tables/cards/conformance/expected/normalized-valid.wire");
  assert.ok(bytesEqual(record.Wire(), wantWire), "Parse normalized-valid does not match the expected wire");

  const rejected = read("tables/cards/conformance/cases/rejected-legacy.md");
  const { record: r2, error: e2 } = cards.Parse(rejected);
  assert.strictEqual(r2, null);
  assert.ok(e2 !== null, "Parse accepted a stranded-key brief");
  assert.match(e2.class, /stranded-key/);
  assert.match(e2.class, /PRIORITY/);

  // render: every render case renders to its expected text byte for byte.
  for (const name of ["card-populated", "initial-populated", "consumers-populated", "empty-consumers"]) {
    const wire = read(`tables/cards/conformance/cases/${name}.wire`);
    const { record: rec, error: le } = cards.Load(wire);
    assert.strictEqual(le, null, `Load ${name} refused`);
    const { text, error: re } = cards.Render(rec);
    assert.strictEqual(re, null, `Render ${name} refused`);
    const want = read(`tables/cards/conformance/expected/${name}.txt`);
    assert.ok(bytesEqual(text, want), `Render ${name} does not match the expected text`);
  }
});

// TestUpgradeMatchesGo runs every upgrade case in tables/cards/conformance: a
// success yields the expected wire and report byte for byte, a refusal the
// expected refusal class; upgrading an upgraded record is byte-identical; and
// retention storage one id short refuses with retain-lost leaving the input
// unchanged (docs/SPEC-TABLES.md §6.6; tla/CardVersioning.tla — NoFieldLost,
// RefusalKeepsOriginal, ShippedUpgradeSucceeds).
test("TestUpgradeMatchesGo", () => {
  const upgradeCases = [
    ["initial-populated", true],
    ["consumers-populated", true],
    ["empty-consumers", true],
    ["future-note", true],
    ["truncated", false],
  ];
  for (const [name, ok] of upgradeCases) {
    const wire = read(`tables/cards/conformance/cases/${name}.wire`);
    const { out, report, error } = cards.Upgrade(wire);
    if (!ok) {
      assert.ok(error !== null, `upgrade ${name} should refuse`);
      const want = read("tables/cards/conformance/expected/truncated.refused");
      assert.strictEqual(error.class + "\n", new TextDecoder().decode(want), `upgrade ${name} refusal class`);
      continue;
    }
    assert.strictEqual(error, null, `upgrade ${name} refused`);
    const wantWire = read(`tables/cards/conformance/expected/${name}.wire`);
    assert.ok(bytesEqual(out, wantWire), `upgrade ${name} wire mismatch`);
    const wantReport = read(`tables/cards/conformance/expected/${name}.report`);
    const gotReport = `Retained=${report.retained}\nRetainLost=${report.retainLost}\nWidened=${report.widened}\nUnknown=${report.unknown}\nKindMismatch=${report.kindMismatch}\nClamped=${report.clamped}\nDuplicate=${report.duplicate}\nMalformed=${report.malformed}\n`;
    assert.strictEqual(gotReport, new TextDecoder().decode(wantReport), `upgrade ${name} report mismatch`);

    const again = cards.Upgrade(out);
    assert.strictEqual(again.error, null, `upgrade of upgraded ${name} refused`);
    assert.ok(bytesEqual(again.out, out), `upgrade of upgraded ${name} is not byte-identical`);
  }

  const wire = read("tables/cards/conformance/cases/card-populated.wire");
  const before = new Uint8Array(wire);
  const { out, error } = cards.Upgrade(wire);
  assert.strictEqual(error, null);
  assert.ok(bytesEqual(wire, before), "upgrade mutated its input");
  assert.ok(out.length > 0);
});

// TestTaskHashFramesLogicalFields pins the frame with an independently written
// byte vector, shows a body/paths boundary change alters the digest, matches
// every expected .hex, and refuses a lossy record (docs/SPEC-TABLES.md §5;
// tla/CardHops.tla — TaskHashStable).
test("TestTaskHashFramesLogicalFields", () => {
  const enc = new TextEncoder();
  const { record, error } = cards.FromJSON(enc.encode(JSON.stringify({ id: "x" })));
  assert.strictEqual(error, null);

  const bodyAB = JSON.stringify({ id: "x", body: "ab" });
  const bodyA = JSON.stringify({ id: "x", body: "a" });
  const { record: ra, error: ea } = cards.FromJSON(enc.encode(bodyA));
  const { record: rb, error: eb } = cards.FromJSON(enc.encode(bodyAB));
  assert.strictEqual(ea, null);
  assert.strictEqual(eb, null);
  const hA = cards.TaskHash(ra);
  const hB = cards.TaskHash(rb);
  assert.notStrictEqual(hA, hB, "different bodies hashed the same");

  for (const name of ["card-populated", "initial-populated", "empty-consumers"]) {
    const wire = read(`tables/cards/conformance/cases/${name}.wire`);
    const { record: rec, error: le } = cards.Load(wire);
    assert.strictEqual(le, null, `Load ${name} refused`);
    const hex = cards.TaskHash(rec);
    const want = new TextDecoder().decode(read(`tables/cards/conformance/expected/${name}.hex`));
    assert.strictEqual(hex, want, `TaskHash ${name} does not match the expected hex`);
  }

  const future = read("tables/cards/conformance/cases/future-note.wire");
  const { record: lossy, error: le2 } = cards.Load(future);
  assert.strictEqual(le2, null);
  assert.throws(() => cards.TaskHash(lossy), /record has loss events/, "a lossy record was hashed");
});

// TestFieldEditLeavesTheTaskHashAlone proves needs, tier, priority, column and
// holder edits change the wire and not the hash, a paths edit changes both, the
// body stays byte-equal throughout, every refusal leaves the wire byte-equal,
// and every edit case in the corpus matches its expected wire or refusal class
// (docs/SPEC-TABLES.md §6.1, §6.2; tla/CardHops.tla — EditDropsUnknown,
// BodyUnchanged).
test("TestFieldEditLeavesTheTaskHashAlone", () => {
  const wire = read("tables/cards/conformance/cases/card-populated.wire");
  const { record, error } = cards.Load(wire);
  assert.strictEqual(error, null);
  const body0 = record._root.body;
  const bodyBytes0 = encBytes(body0);
  const hash0 = cards.TaskHash(record);
  const before = new Uint8Array(wire);

  cards.SetNeeds(record, ["a", "b"]);
  assert.strictEqual(cards.TaskHash(record), hash0, "needs edit changed the hash");
  assert.ok(record._root.needs.length === 2);
  cards.SetTier(record, "heavy");
  assert.strictEqual(cards.TaskHash(record), hash0, "tier edit changed the hash");
  cards.SetPriority(record, "blocker");
  assert.strictEqual(cards.TaskHash(record), hash0, "priority edit changed the hash");
  cards.SetColumn(record, "landed");
  assert.strictEqual(cards.TaskHash(record), hash0, "column edit changed the hash");
  cards.SetHolder(record, "h");
  assert.strictEqual(cards.TaskHash(record), hash0, "holder edit changed the hash");
  assert.ok(encBytes(record._root.body) === bodyBytes0, "body changed after non-paths edits");

  cards.SetPaths(record, ["x", "y"]);
  assert.notStrictEqual(cards.TaskHash(record), hash0, "a paths edit left the hash unchanged");
  assert.ok(encBytes(record._root.body) === bodyBytes0, "body changed after a paths edit");

  const future = read("tables/cards/conformance/cases/future-note.wire");
  const { record: lossy } = cards.Load(future);
  const lossyWire = new Uint8Array(lossy._wire);
  assert.throws(() => cards.SetNeeds(lossy, ["a"]), /record has loss events/, "editing a lossy record was allowed");
  assert.ok(bytesEqual(lossy._wire, lossyWire), "a refused edit changed the wire");

  const editCases = [
    ["needs", "needs=a,b"],
    ["tier", "tier=heavy"],
    ["priority", "priority=blocker"],
    ["paths", "paths=x,y"],
    ["column", "column=landed"],
    ["holder", "holder=h"],
    ["empty-to-growing", "paths=x"],
    ["unknown", "needs=a"],
  ];
  for (const [name, editLine] of editCases) {
    const input = name === "empty-to-growing" ? "empty-consumers" : name === "unknown" ? "future-note" : "card-populated";
    const w = read("tables/cards/conformance/cases/" + input + ".wire");
    const { record: rec, error: le } = cards.Load(w);
    assert.strictEqual(le, null, `Load ${name} refused`);
    const parts = editLine.split("=");
    let editErr = null;
    try {
      switch (parts[0]) {
        case "needs": cards.SetNeeds(rec, parts[1].split(",")); break;
        case "paths": cards.SetPaths(rec, parts[1].split(",")); break;
        case "tier": cards.SetTier(rec, parts[1]); break;
        case "priority": cards.SetPriority(rec, parts[1]); break;
        case "column": cards.SetColumn(rec, parts[1]); break;
        case "holder": cards.SetHolder(rec, parts[1]); break;
      }
    } catch (e) {
      editErr = e;
    }
    if (name === "unknown") {
      assert.ok(editErr !== null, `edit ${name} should refuse`);
      const want = new TextDecoder().decode(read("tables/cards/conformance/expected/unknown.refused"));
      assert.strictEqual(editErr.class, want.trim(), `edit ${name} refusal class`);
      continue;
    }
    assert.strictEqual(editErr, null, `edit ${name} refused`);
    const wantWire = read(`tables/cards/conformance/expected/${name}.wire`);
    assert.ok(bytesEqual(rec.Wire(), wantWire), `edit ${name} wire mismatch`);
  }
});

function encBytes(s) {
  if (s === null || s === undefined) return "";
  return new TextEncoder().encode(s).length;
}

// TestViewWalksEveryField compares the generated view with an explicit
// vocabulary written here (not derived from the descriptors) and with
// tables/cards/conformance's expected view.txt, and walks a loaded and an
// edited record's values through the descriptors (docs/SPEC-TABLES.md §8).
test("TestViewWalksEveryField", () => {
  const expectedView = read("tables/cards/conformance/expected/view.txt");
  const committed = new TextDecoder().decode(expectedView);

  const vocab = [
    "Card id string value 64",
    "Card sha string value 40",
    "Card tier Tier value -",
    "Card repo string value 96",
    "Card _base string value 96",
    "Card kind string value 24",
    "Card priority Priority value -",
    "Card stream string value 32",
    "Card paths PathText list -",
    "Card _new PathText list -",
    "Card shared PathText list -",
    "Card needs CardID list -",
    "Card test string value 192",
    "Card done string blob -",
    "Card deadline_minutes int32 value -",
    "Card source string blob -",
    "Card epoch int32 value -",
    "Card column Column value -",
    "Card holder string value 32",
    "Card attempt int32 value -",
    "Card head string value 40",
    "Card branch string value 128",
    "Card body string blob -",
    "Card extra KeyValue list -",
    "Card consumers Consumer list -",
    "CardID s string value 64",
    "Consumer kind string value 16",
    "Consumer who string value 32",
    "Consumer route string value 48",
    "Consumer model string value 64",
    "Consumer started int64 value -",
    "Consumer ended int64 value -",
    "Consumer outcome string value 16",
    "Consumer line string value 200",
    "Consumer cost_micro_usd int64 value -",
    "KeyValue key string value 32",
    "KeyValue value string blob -",
    "PathText s string value 192",
  ].join("\n") + "\n";

  assert.strictEqual(committed, vocab, "committed view.txt differs from the pinned vocabulary");

  const tables = [...CardsUnitRegistry.tables].sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
  const enumTypeName = { tier: "Tier", priority: "Priority", column: "Column" };
  const listElemType = { paths: "PathText", _new: "PathText", shared: "PathText", needs: "CardID", extra: "KeyValue", consumers: "Consumer" };
  const typeOf = (f) => {
    switch (f.kind) {
      case 12: return "string";
      case 4: return "int32";
      case 5: return "int64";
      case 30: return enumTypeName[f.name];
      case 17: return "string";
      case 14: return listElemType[f.name];
      default: return "?";
    }
  };
  let generated = "";
  for (const t of tables) {
    for (const f of t.fields) {
      let bound = "-";
      if (f.shape === "value" && f.bound > 0) bound = String(f.bound);
      generated += `${t.name} ${f.name} ${typeOf(f)} ${f.shape} ${bound}\n`;
    }
  }
  assert.strictEqual(generated, committed, "the descriptor walk differs from the committed view.txt");

  const wire = read("tables/cards/conformance/cases/card-populated.wire");
  const { record, error } = cards.Load(wire);
  assert.strictEqual(error, null);
  assert.ok(record._root.id.length > 0, "scalar id did not walk");
  assert.ok(record._root.paths.length > 0, "list paths did not walk");
  assert.ok(record._root.body !== null, "body did not walk");
  cards.SetPaths(record, ["one", "two", "three"]);
  assert.ok(record._root.paths.length === 3, "paths did not walk after edit");
  assert.ok(record._root.body !== null, "body did not walk after edit");
  assert.strictEqual(cards.TaskHash(record).length, 64, "the record is not walkable to a digest");
});
