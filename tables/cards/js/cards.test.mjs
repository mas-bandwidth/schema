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
