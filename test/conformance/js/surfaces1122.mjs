// THE 2026-11-22 SURFACES (test/conformance/README.md, "The 2026-11-22
// surfaces ask what no surface asked before"; docs/SPEC-CONFORMANCE.md §2):
// `announce`, `read-into` and `json-write-into`. Each calls the generated
// runtime and writes what it did; nothing here judges a value itself.
//
// What this leg's runtime does not offer is answered as exactly that:
//
// - read-into `variable` and `json`: the variable form's <Root>Load and
//   <Root>FromJson take no caller value; each returns a new value or null. No
//   call is handed a value it could leave at its defaults, so those rows are
//   `<case>.absent`, by that reason.
// - read-into `fixed-nocap`: <Root>FixedLoad takes the batch's capacity as a
//   positional argument beside the caller's values, so no capacity can be
//   omitted, and the row answers `unrepresentable` (FL-17).
// - json-write-into: <Root>ToJson returns a string and takes no caller
//   buffer, so there is no byte past the text for it to write. The
//   comma-decimal locale IS this process's: the driver script exports
//   LC_ALL from SCHEMA_COMMA_LOCALE before node starts, which is the one
//   place a Node process takes its locale, and the surface refuses to run
//   (exit 1) unless Intl confirms a comma decimal.

import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { toVar } from "./fixed.mjs";

// The modules of each unit a 2026-11-22 row names: the home module (the
// vocabulary, the announcement writer and reader, the fixed classes), the
// table module (the fixed load and the message verbs), the variable module
// (ToJson) and the view (the fixed bridge's descriptors).
const units = {
  tblsurf: { home: "surfaces/SurfacesTable.js", table: "surfaces/SurfacesTable.js", variable: "surfaces/SurfacesVariable.js", view: "surfaces/SurfacesView.js" },
  tblann4096: { home: "announce4096/Announce4096Table.js" },
  tblann4097: { home: "announce4097/Announce4097Table.js" },
  messagedemo: { home: "messages/MessagedemoTable.js" },
  backenddemo: { home: "backend/BackenddemoTable.js", table: "backend/BackendTable.js", variable: "backend/BackenddemoVariable.js", view: "backend/BackenddemoView.js" },
};

function unitOf(name, what, row) {
  const u = units[name];
  if (u === undefined || u[what] === undefined) { throw new Error(row + ": this driver binds no " + what + " module for unit " + name); }
  return u;
}

function need(module, name, row) {
  const v = module[name];
  if (v === undefined) { throw new Error(row + ": the generated module exports no " + name); }
  return v;
}

// a fresh vocabulary with room for the protocol's 4096 entries, as the message
// surface makes one (main.mjs)
function vocabularyOf(home) {
  const v = new home.TableVocabulary();
  v.Init(new Array(Math.max(home.TableMessageEntriesHere, 4096)));
  return v;
}

// ---- announce <case> <unit> <root> <mode> <input>
export async function surfaceAnnounce(lines, outDir, load, messageVerbs, fixedValueOf) {
  for (const f of lines) {
    if (f[0] !== "announce") { continue; }
    const [, name, unitName, root, mode, input] = f;
    let answer;
    switch (mode) {
      case "write": {
        // AnnounceMeasure, then Announce: the bytes, or the writer's refusal
        const home = await load(unitOf(unitName, "home", name).home);
        const size = need(home, "AnnounceMeasure", name)();
        if (size < 0) { answer = "refused\n"; break; }
        const buffer = new Uint8Array(size);
        const n = need(home, "Announce", name)(buffer);
        if (n >= 0 && n !== size) { throw new Error(name + ": Announce wrote " + n + " bytes, AnnounceMeasure said " + size); }
        answer = n < 0 ? "refused\n" : buffer;
        break;
      }
      case "strict":
      case "lenient": {
        // a fresh vocabulary of the unit, its Strict flag set or left at the
        // default, reads the announcement file
        const home = await load(unitOf(unitName, "home", name).home);
        const v = vocabularyOf(home);
        if (mode === "strict") { v.Strict = true; }
        answer = need(home, "AnnounceRead", name)(v, new Uint8Array(readFileSync(input)), {}) ? "read\n" : "refused\n";
        break;
      }
      case "send": {
        // SaveMessages of one body with NO announcement read or written on
        // this connection (FL-18)
        const verbs = await messageVerbs(unitName, root);
        if (verbs === null) {
          writeFileSync(join(outDir, name + ".absent"), "the generated unit " + unitName + " carries no " + root + "SaveMessages: docs/SPEC-TABLES.md §3.3\n");
          continue;
        }
        const x = await fixedValueOf(unitName, root, input);
        if (x === null) { throw new Error(name + ": no fixed class for " + unitName + "." + root); }
        const size = verbs.measure([x], {});
        if (size < 0) { answer = "refused\n"; break; }
        const buffer = new Uint8Array(size);
        const n = verbs.save([x], buffer, {});
        if (n >= 0 && n !== size) { throw new Error(name + ": SaveMessages wrote " + n + " bytes, MeasureMessages said " + size); }
        answer = n < 0 ? "refused\n" : buffer;
        break;
      }
      default:
        throw new Error(name + ": " + mode + " is not an announce mode");
    }
    writeFileSync(join(outDir, name), answer);
  }
}

// ---- read-into <case> <unit> <root> <form> <lawful> <damaged> <count> <announcement>
export async function surfaceReadInto(lines, outDir, load) {
  for (const f of lines) {
    if (f[0] !== "read-into") { continue; }
    const [, name, unitName, root, form, lawful, damaged, countText, announcement] = f;
    if (form === "variable" || form === "json") {
      writeFileSync(join(outDir, name + ".absent"),
        "the JavaScript " + (form === "json" ? "<Root>FromJson" : "<Root>Load") + " takes no caller value: it returns a new value or null\n");
      continue;
    }
    if (form === "fixed-nocap") {
      // <Root>FixedLoad(values, capacity, bytes, byteLength, plan, report):
      // the capacity is the caller's, beside the caller's values, always
      writeFileSync(join(outDir, name), "unrepresentable\n");
      continue;
    }
    const u = unitOf(unitName, "table", name);
    const home = await load(u.home), table = await load(u.table), variable = await load(u.variable), view = await load(u.view);
    const t = view.TableUnitView.descriptors[root];
    if (t === undefined) { throw new Error(name + ": the view has no descriptor for " + root); }
    const Class = need(home, root, name), init = need(home, "Init" + root, name), toJson = need(variable, root + "ToJson", name);
    const count = Number(countText);
    if (!Number.isInteger(count) || count < 1) { throw new Error(name + ": " + countText + " is not a count of values"); }
    // <count> fresh values and ONE report, handed to every read below
    const values = Array.from({ length: count }, () => { const x = new Class(); init(x); return x; });
    let read;
    if (form === "fixed") {
      const fixedLoad = need(table, root + "FixedLoad", name);
      const plan = need(table, root + "FixedNewPlan", name)();
      const report = new home.TableFixedReport();
      read = (path) => {
        const bytes = new Uint8Array(readFileSync(path));
        const n = fixedLoad(values, count, bytes, bytes.length, plan, report);
        return n >= 1 && report.refused === 0 && !report.damaged;
      };
    } else if (form === "message") {
      const vocabulary = vocabularyOf(home);
      if (!need(home, "AnnounceRead", name)(vocabulary, new Uint8Array(readFileSync(announcement)), {})) {
        throw new Error(name + ": the connection's announcement was refused: " + announcement);
      }
      const loadMessages = need(table, root + "LoadMessages", name);
      const report = {};
      read = (path) => {
        const n = loadMessages(values, vocabulary, new Uint8Array(readFileSync(path)), report);
        return n >= 1 && report.Verdict === 0 && !report.Damaged;
      };
    } else {
      throw new Error(name + ": " + form + " is not a read-into form");
    }
    // A READ REFUSES THROUGH ITS REPORT AND NEVER BY THROWING (FL-29): the
    // driver catches only to say `threw`
    const step = (path) => {
      try {
        return read(path) ? "read\n" : "refused\n";
      } catch (e) {
        process.stderr.write(name + ": the read of " + path + " threw: " + (e && e.stack ? e.stack : String(e)) + "\n");
        return "threw\n";
      }
    };
    let out = step(lawful) + step(damaged);
    // the text of every value the call was handed, in order, through the
    // fixed bridge and the unit's generated ToJson
    for (const x of values) {
      const text = toJson(toVar(t, x, root));
      if (text === null) { throw new Error(name + ": ToJson refused a value the read left"); }
      out += text;
    }
    out += step(lawful);
    writeFileSync(join(outDir, name), out);
  }
}

// ---- json-write-into <case> <unit> <root> <wire>
export async function surfaceJsonWriteInto(lines, outDir, textCodec, readOf) {
  const locale = process.env.SCHEMA_COMMA_LOCALE;
  if (locale === undefined || locale === "") {
    throw new Error("json-write-into: SCHEMA_COMMA_LOCALE is not set (the harness names the comma-decimal locale)");
  }
  const sample = new Intl.NumberFormat().format(1.5);
  if (sample !== "1,5") {
    throw new Error("json-write-into: the process locale is not comma-decimal: Intl formats 1.5 as " + JSON.stringify(sample) +
      " (LC_ALL=" + process.env.LC_ALL + ", SCHEMA_COMMA_LOCALE=" + locale + ")");
  }
  for (const f of lines) {
    if (f[0] !== "json-write-into") { continue; }
    const [, name, unit, root, wirePath] = f;
    const codec = await textCodec(unit, root);
    if (codec === null) { writeFileSync(join(outDir, name + ".absent"), "no generated unit carries " + unit + "." + root + "\n"); continue; }
    const v = readOf(codec.load, new Uint8Array(readFileSync(wirePath)), codec.report());
    if (v === null) { throw new Error(name + ": load refused " + wirePath); }
    const text = codec.toJson(v);
    writeFileSync(join(outDir, name), text === null ? "refused\n" : text);
  }
}
