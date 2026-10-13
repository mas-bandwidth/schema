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
import * as cards from "./cards.mjs";

const surfaces = ["load", "save", "render", "parse", "bounds"];

// CARDS_JS_SABOTAGE_RENDER is the test-only environment switch the negative
// control sets: it changes one rendered byte so the check must go red on
// render while the other four surfaces stay green.
const sabotageRender = process.env.CARDS_JS_SABOTAGE_RENDER === "1";

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
  }
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
