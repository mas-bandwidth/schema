// driver.mjs — the JavaScript card conformance driver
// (tables/cards/conformance/FORMAT.md, test/conformance/README.md "The shape").
//
// It answers the driver contract — `<driver> <manifest> list` and
// `<driver> <manifest> <surface> <outdir>` — over the five surfaces this leg
// implements (load, save, render, parse, bounds) through the facade in
// cards.mjs. upgrade, edit, taskhash and view are the follow-up card's
// (a-js-card-upgrades-edits-and-walks-the-shared-corpus) and answer exit 2.
//
// The working directory is the repository root, which is the contract: every
// path in the manifest is repo-relative, so this driver never resolves one.

import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { CardsUnitRegistry } from "../../../build/tables-generated-js/cards/CardsVariable.js";
import * as cards from "./cards.mjs";

const surfaces = ["load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view"];

// CARDS_JS_SABOTAGE_RENDER is the test-only environment switch the negative
// control sets: it changes one rendered byte so the check must go red on
// render while the other four surfaces stay green.
const sabotageRender = process.env.CARDS_JS_SABOTAGE_RENDER === "1";

// CARDS_JS_SABOTAGE_UPGRADE drops one retained byte so upgrade alone goes red;
// CARDS_JS_SABOTAGE_HASH adds needs to the hash frame so taskhash alone does.
const sabotageUpgrade = process.env.CARDS_JS_SABOTAGE_UPGRADE === "1";
const sabotageHash = process.env.CARDS_JS_SABOTAGE_HASH === "1";

function readManifest(path) {
  const lines = [];
  for (const raw of readFileSync(path, "utf8").split("\n")) {
    const text = raw.trim();
    if (text.length === 0 || text[0] === "#") continue;
    lines.push(text.split(/\s+/));
  }
  return lines;
}

function fail(message) {
  process.stderr.write("cards-driver: " + message + "\n");
  process.exit(1);
}

// reportText writes the read report, one Name=value line per counter of the
// generated report in declaration order (tables/cards/conformance/FORMAT.md).
function reportText(report) {
  return (
    `Retained=${report.retained}\n` +
    `RetainLost=${report.retainLost}\n` +
    `Widened=${report.widened}\n` +
    `Unknown=${report.unknown}\n` +
    `KindMismatch=${report.kindMismatch}\n` +
    `Clamped=${report.clamped}\n` +
    `Duplicate=${report.duplicate}\n` +
    `Malformed=${report.malformed}\n`
  );
}

// refuse writes outdir/<name>.refused holding the one-line reason class.
function refuse(outdir, name, cls) {
  writeFileSync(join(outdir, name + ".refused"), cls + "\n");
}

function one(surface, c, outdir) {
  const name = c[1];
  const input = c[2];
  switch (surface) {
    case "load": {
      const wire = new Uint8Array(readFileSync(input));
      const { record, report, error } = cards.Load(wire);
      if (error) fail(error.message);
      const { text, error: e2 } = cards.ToJSON(record);
      if (e2) fail(e2.message);
      writeFileSync(join(outdir, name + ".json"), text);
      writeFileSync(join(outdir, name + ".report"), reportText(report));
      break;
    }
    case "save":
    case "bounds": {
      const text = new Uint8Array(readFileSync(input));
      const { record, error } = cards.FromJSON(text);
      if (error) {
        refuse(outdir, name, error.class);
        break;
      }
      writeFileSync(join(outdir, name + ".wire"), record.Wire());
      break;
    }
    case "render": {
      const wire = new Uint8Array(readFileSync(input));
      const { record, error } = cards.Load(wire);
      if (error) fail(error.message);
      const { text, error: e2 } = cards.Render(record);
      if (e2) fail(e2.message);
      if (sabotageRender) {
        text[0] = 0x00;
      }
      writeFileSync(join(outdir, name + ".txt"), text);
      break;
    }
    case "parse": {
      const text = new Uint8Array(readFileSync(input));
      const { record, error } = cards.Parse(text);
      if (error) {
        refuse(outdir, name, error.class);
        break;
      }
      writeFileSync(join(outdir, name + ".wire"), record.Wire());
      break;
    }
    case "upgrade": {
      const wire = new Uint8Array(readFileSync(input));
      const { out, report, error } = cards.Upgrade(wire);
      if (error) {
        refuse(outdir, name, error.class);
        break;
      }
      if (sabotageUpgrade) out[0] ^= 0xff;
      writeFileSync(join(outdir, name + ".wire"), out);
      writeFileSync(join(outdir, name + ".report"), reportText(report));
      break;
    }
    case "edit": {
      const wire = new Uint8Array(readFileSync(input));
      const { record, error } = cards.Load(wire);
      if (error) fail(error.message);
      try {
        applyEdit(record, c.slice(3));
      } catch (e) {
        refuse(outdir, name, refusalClass(e));
        break;
      }
      writeFileSync(join(outdir, name + ".wire"), record.Wire());
      break;
    }
    case "taskhash": {
      const wire = new Uint8Array(readFileSync(input));
      const { record, error } = cards.Load(wire);
      if (error) fail(error.message);
      let hex;
      try {
        hex = cards.TaskHash(record);
      } catch (e) {
        fail(e.message);
      }
      if (sabotageHash) hex = "0".repeat(64);
      writeFileSync(join(outdir, name + ".hex"), hex);
      break;
    }
    case "view": {
      writeFileSync(join(outdir, name + ".txt"), view());
      break;
    }
  }
}

// refusalClass maps a facade error to one class of the closed list FORMAT.md
// fixes, mirroring the Go reference driver's refusalClass.
function refusalClass(err) {
  const msg = err.message;
  if (msg.includes("past the 65536-byte ceiling")) return "body-bound 65536";
  if (msg.includes("past the 512-entry ceiling")) return "list-bound " + fieldName(msg) + " 512";
  if (msg.includes("stranded header key")) return "stranded-key " + quotedWord(msg);
  if (msg.includes("retained field(s) lost")) return "retain-lost";
  if (msg.includes("record has loss events")) return "unknown-present";
  return "malformed";
}

function fieldName(msg) {
  const rest = msg.replace(/^cards: /, "");
  return rest.split(" has ")[0];
}

function quotedWord(msg) {
  const i = msg.indexOf('"');
  if (i < 0) return "";
  const j = msg.indexOf('"', i + 1);
  if (j < 0) return "";
  return msg.slice(i + 1, j);
}

function applyEdit(record, edit) {
  if (edit.length !== 1) throw new cards.CardError("cards-driver: an edit line carries exactly one edit", "malformed");
  const [key, value] = edit[0].split("=");
  if (value === undefined) throw new cards.CardError(`cards-driver: edit ${edit[0]} has no =`, "malformed");
  switch (key) {
    case "needs": return cards.SetNeeds(record, splitList(value));
    case "paths": return cards.SetPaths(record, splitList(value));
    case "tier": return cards.SetTier(record, value);
    case "priority": return cards.SetPriority(record, value);
    case "column": return cards.SetColumn(record, value);
    case "holder": return cards.SetHolder(record, value);
  }
  throw new cards.CardError(`cards-driver: unknown edit ${key}`, "malformed");
}

function splitList(v) {
  if (v === "") return [];
  return v.split(",");
}

// The enum/list type names the generated view spells, keyed by the declared
// field name (docs/SPEC-TABLES.md §8); the bounded value and blob kinds are
// spelled from the kind number itself.
const enumTypeName = { tier: "Tier", priority: "Priority", column: "Column" };
const listElemType = {
  paths: "PathText", _new: "PathText", shared: "PathText",
  needs: "CardID", extra: "KeyValue", consumers: "Consumer",
};

function typeOf(f) {
  switch (f.kind) {
    case 12: return "string";
    case 4: return "int32";
    case 5: return "int64";
    case 30: return enumTypeName[f.name];
    case 17: return "string";
    case 14: return listElemType[f.name];
    default: return "?";
  }
}

// view renders the generated view: every field of every table in the unit, in
// table-name order then declaration order, one line each (docs/SPEC-TABLES.md
// §8; FORMAT.md "The view"), byte-identical to the Go reference leg.
function view() {
  const tables = [...CardsUnitRegistry.tables].sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
  let out = "";
  for (const t of tables) {
    for (const f of t.fields) {
      const shape = f.shape;
      let bound = "-";
      if (shape === "value" && f.bound > 0) bound = String(f.bound);
      out += `${t.name} ${f.name} ${typeOf(f)} ${shape} ${bound}\n`;
    }
  }
  const b = new Uint8Array(Buffer.from(out, "ascii"));
  return b;
}

function answer(surface, cases, outdir) {
  for (const c of cases) {
    if (c[0] !== surface) continue;
    one(surface, c, outdir);
  }
}

function main() {
  const args = process.argv.slice(2);
  if (args.length < 2) {
    process.stderr.write("usage: driver <manifest> list\n       driver <manifest> <surface> <outdir>\n");
    process.exit(2);
  }
  const manifest = args[0];
  const verb = args[1];
  if (verb === "list") {
    for (const s of surfaces) process.stdout.write(s + "\n");
    process.exit(0);
  }
  if (args.length < 3) {
    process.stderr.write("usage: driver <manifest> <surface> <outdir>\n");
    process.exit(2);
  }
  const surface = verb;
  const outdir = args[2];
  if (!surfaces.includes(surface)) {
    process.exit(2);
  }
  const cases = readManifest(manifest);
  mkdirSync(outdir, { recursive: true });
  answer(surface, cases, outdir);
}

main();
