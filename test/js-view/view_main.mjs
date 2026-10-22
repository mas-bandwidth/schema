// THE EDITOR GATE, the JavaScript leg (docs/SPEC-TABLES.md §8.7). This program
// has the generated view module and NOTHING ELSE: no schema files, no compiler,
// no knowledge of a single declaration's name. It imports the unit's view,
// calls UnitView() and prints the whole build — every constant with its value,
// every enum, flags and union with its variants in order, every type and every
// table with every property — in the listing test/c-tables/view_main.c prints,
// and that listing is byte-compared against the one the compiler produces from
// its own IR (internal/viewlisting). If it cannot reach a declaration or a
// property of one, the view is incomplete and the gate says so.
//
// The unit is named by the command line, not by this source:
//   node test/js-view/view_main.mjs <path to the generated <Home>View.js>
//
// A MULTI-LINE doc prints as ONE LINE, each newline written \n and the
// escape's own backslash written \\, the rule both halves flatten by.

import { pathToFileURL } from "node:url";
import { resolve } from "node:path";

if (process.argv.length !== 3) {
  process.stderr.write("usage: view_main.mjs <generated View.js>\n");
  process.exit(2);
}
const { UnitView } = await import(pathToFileURL(resolve(process.argv[2])).href);
const unit = UnitView();
const out = [];

// flatten reads the doc column with NO absence test: doc is never null or
// undefined (§8.1), and a missing one prints "undefined" rather than a line
// that happens to look right.
function flatten(doc) {
  let s = "";
  for (const ch of doc) {
    if (ch === "\\") { s += "\\\\"; } else if (ch === "\n") { s += "\\n"; } else { s += ch; }
  }
  return s;
}

// annotation is a row's tags and its doc, the doc LAST because it is free-form
// text and everything after it on the line would be ambiguous.
const annotation = (r) => " tags=[" + r.tags.join(",") + "] doc=" + flatten(r.doc);
const hex = (n) => BigInt.asUintN(64, BigInt(n)).toString(16).padStart(16, "0");

function vocabulary(what, v, row) {
  out.push(what + " " + v.name + " file=" + v.file + " max=" + v.max + " bits=" + v.storageBits +
    " variants=" + v.variants.length + annotation(v));
  for (const r of v.variants) {
    out.push(what + " " + v.name + " " + row + " " + r.value + " " + r.name + " id=" + hex(r.id) + annotation(r));
  }
}

// the general arm's OVERLAY (§8.7): a generic walker reaches a union's arms
// through a HOLDER, the first registry entry declaring a field of the union's
// type, the tables in registry order and then the types. Every arm overlays
// the union's payload base, so the column is the arm's own offset less that
// base, read off the holder's field.
function holderOf(union) {
  for (const set of [unit.tables, unit.types]) {
    for (const entry of set) {
      for (const f of entry.type.fields) {
        if (f.vocabulary !== null && f.vocabulary.kind === "union" && f.vocabulary.name === union.name) { return f; }
      }
    }
  }
  return null;
}

function unionVocabulary(v) {
  out.push("union " + v.name + " file=" + v.file + " max=" + v.max + " bits=" + v.storageBits +
    " variants=" + v.variants.length + annotation(v));
  const holder = holderOf(v);
  for (const r of v.variants) {
    let line = "union " + v.name + " arm " + r.value + " " + r.name + " id=" + hex(r.id) +
      " payload=" + (r.payloadName === null ? "-" : r.payloadName) +
      " record=" + (r.payload !== null ? "yes" : "no") + " field=" + (r.field !== null ? "yes" : "no");
    if (r.field !== null) {
      line += " kind=" + r.field.kind + " bound=" + r.field.arrayBound;
      line += holder !== null ? " overlay=" + (r.field.offset - holder.vocabulary.payloadOffset) : " overlay=-";
    } else {
      line += " kind=- bound=- overlay=-";
    }
    out.push(line + annotation(r));
  }
}

function declaration(what, entry) {
  const type = entry.type;
  out.push(what + " " + entry.name + " file=" + entry.file + " fields=" + type.fields.length + annotation(entry));
  for (const f of type.fields) {
    out.push(what + " " + entry.name + " field " + f.name + " json=" + (f.json === null ? "-" : f.json) +
      " type=" + f.typeName + " id=" + hex(f.id) + " optional=" + f.optional + annotation(f));
  }
}

out.push("unit package=" + unit.package + " protocol=" + hex(unit.protocolId));
const bits = new DataView(new ArrayBuffer(8));
for (const c of unit.constants) {
  bits.setFloat64(0, c.floatValue, true);
  const real = c.isFloat ? bits.getBigUint64(0, true) : 0n;
  out.push("constant " + c.name + " file=" + c.file + " type=" + c.typeName + " float=" + c.isFloat +
    " int=" + BigInt.asIntN(64, c.intValue) + " real=" + hex(real) + annotation(c));
}
for (const e of unit.enums) { vocabulary("enum", e, "variant"); }
for (const f of unit.flags) { vocabulary("flags", f, "bit"); }
for (const u of unit.unions) { unionVocabulary(u); }
for (const t of unit.types) { declaration("type", t); }
for (const t of unit.tables) { declaration("table", t); }
process.stdout.write(out.join("\n") + "\n");
