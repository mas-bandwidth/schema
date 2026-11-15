// THE WRITE-REFUSAL SURFACES (docs/SPEC.md §5.1, test/conformance/README.md).
//
// Each row loads one instance, sets ONE field to the value the row names, and
// writes it through the writer its surface names: the root's own form's save
// (`write-refusal`; a fixed root's own form is the fixed form, as the Go leg
// writes it), the fixed form's FixedSave (`write-refusal-fixed`), the message
// form's SaveMessages against the connection's unit (`write-refusal-message`),
// or the text writer (`write-refusal-json`). The answer is what that writer
// did: `refused` when it answered its refusal, `wrote` when it completed.
//
// JavaScript refuses in every build (docs/rulings.json `write-refusal`), so
// this leg answers from its ordinary build and the generated writer is the
// only judge: the driver sets the field and reports, and never checks a value
// itself.
//
// THE VALUE IS SET IN THE STORAGE THE WRITER READS. The binary writers take
// the generated fixed class (a text is its bytes and a used length, a counted
// array its slots and a count, a union its tag), so those rows set that class
// directly: a value its declaration does not admit has no spelling in the
// variable value the fixed bridge (fixed.mjs) places. The text writer takes
// the variable value, where a text is a string.

import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const segmentPattern = /^([A-Za-z_][A-Za-z0-9_]*)(?:\[([0-9]+)\])?$/;
const camel = (name) => name.replace(/_([a-z0-9])/g, (_, c) => c.toUpperCase());
const utf8 = new TextDecoder("utf-8", { fatal: true });

function segmentOf(segment) {
  const m = segmentPattern.exec(segment);
  if (m === null) { throw new Error(JSON.stringify(segment) + " is not a field path segment"); }
  return { name: m[1], index: m[2] === undefined ? -1 : Number(m[2]) };
}

function hexBytes(text) {
  if (text.length % 2 !== 0 || !/^[0-9A-Fa-f]*$/.test(text)) { throw new Error(JSON.stringify(text) + " is not hex"); }
  const out = new Uint8Array(text.length / 2);
  for (let i = 0; i < out.length; i++) { out[i] = parseInt(text.slice(2 * i, 2 * i + 2), 16); }
  return out;
}

function floatOf(text) {
  switch (text) {
    case "nan": return NaN;
    case "inf": return Infinity;
    case "-inf": return -Infinity;
    default: return Number(text);
  }
}

// walk answers the object that owns the path's last field and that field's
// segment; `key` maps a declared field name to the property the value holds.
function walk(value, path, key) {
  const segments = path.split(".");
  let cur = value;
  for (const s of segments.slice(0, -1)) {
    const { name, index } = segmentOf(s);
    const p = key(name);
    if (cur === null || typeof cur !== "object" || !(p in cur)) { throw new Error(path + ": no field " + name); }
    cur = index < 0 ? cur[p] : cur[p][index];
  }
  if (cur === null || typeof cur !== "object") { throw new Error(path + ": no owner"); }
  return { owner: cur, last: segmentOf(segments[segments.length - 1]) };
}

function own(owner, p, path) {
  if (!(p in owner)) { throw new Error(path + ": the value has no property " + p); }
  return p;
}

// setFixed sets the one field a row names in the generated fixed class.
export function setFixed(value, path, kind, text) {
  const { owner, last } = walk(value, path, camel);
  const p = own(owner, camel(last.name), path);
  const set = (v) => { if (last.index < 0) { owner[p] = v; } else { owner[p][last.index] = v; } };
  const get = () => (last.index < 0 ? owner[p] : owner[p][last.index]);
  switch (kind) {
    case "count":
      owner[own(owner, p + "Count", path)] = Number(text);
      return;
    case "length": {
      // the used length the row names, over storage filled with `a` as far
      // as it reaches: a length past the storage is the row's point
      const n = Number(text);
      const storage = owner[p];
      for (let i = 0; i < n && i < storage.length; i++) { storage[i] = 0x61; }
      owner[own(owner, p + "Length", path)] = n;
      return;
    }
    case "bytes": {
      const b = hexBytes(text);
      const storage = owner[p];
      for (let i = 0; i < b.length && i < storage.length; i++) { storage[i] = b[i]; }
      owner[own(owner, p + "Length", path)] = b.length;
      return;
    }
    case "int":
      set(typeof get() === "bigint" ? BigInt(text) : Number(text));
      return;
    case "ordinal":
      set(Number(text));
      return;
    case "tag": {
      const u = get();
      if (u === null || typeof u !== "object" || !("type" in u)) { throw new Error(path + ": carries no union tag"); }
      u.type = Number(text);
      return;
    }
    case "float":
      set(floatOf(text));
      return;
    default:
      throw new Error(path + ": kind " + kind + " has no spelling in this leg's fixed storage");
  }
}

// setVariable sets the one field a row names in the variable value, where the
// text writer reads it: a text is a string, so `bytes` must be UTF-8 this leg's
// string can hold, and bytes it cannot are `unrepresentable`.
export function setVariable(value, path, kind, text) {
  const { owner, last } = walk(value, path, (name) => name);
  const p = own(owner, last.name, path);
  const set = (v) => { if (last.index < 0) { owner[p] = v; } else { owner[p][last.index] = v; } };
  switch (kind) {
    case "bytes": {
      let s;
      try { s = utf8.decode(hexBytes(text)); } catch { return false; }
      set(s);
      return true;
    }
    case "length":
      set("a".repeat(Number(text)));
      return true;
    case "int":
      set(typeof (last.index < 0 ? owner[p] : owner[p][last.index]) === "bigint" ? BigInt(text) : Number(text));
      return true;
    case "float":
      set(floatOf(text));
      return true;
    default:
      throw new Error(path + ": kind " + kind + " has no spelling in this leg's variable value");
  }
}

function connectionUnit(lines, connection) {
  const c = lines.find((f) => f[0] === "connection" && f[1] === connection);
  return c === undefined ? null : c[2];
}

function absent(outDir, name, why) {
  writeFileSync(join(outDir, name + ".absent"), why + "\n");
}

// surfaceWriteRefusal answers one of the four surfaces. `codecFor(unit, root)`
// is main.mjs's text codec (the fixed form bound in for a fixed root), and
// `messageUnit(unit, root)` the module carrying the root's message verbs.
export async function surfaceWriteRefusal(surface, lines, outDir, codecFor, messageUnit) {
  for (const f of lines) {
    if (f[0] !== surface) { continue; }
    // <surface> <case> <unit> <root> <wire> <path> <kind> <value>
    const [, name, unitName, root, wirePath, path, kind, text] = f;
    const unit = surface === "write-refusal-message" ? connectionUnit(lines, unitName) : unitName;
    if (unit === null) { throw new Error(name + ": connection " + unitName + " names no unit"); }
    const codec = await codecFor(unit, root);
    if (codec === null) { absent(outDir, name, "no generated unit carries " + unit + "." + root + ": docs/SPEC-TABLES.md §11"); continue; }
    const wire = new Uint8Array(readFileSync(wirePath));
    let answer;
    if (surface === "write-refusal-json") {
      const v = codec.load(wire, codec.report());
      if (v === null || v === undefined) { throw new Error(name + ": load refused " + wirePath); }
      if (!setVariable(v, path, kind, text)) {
        answer = "unrepresentable";
      } else {
        answer = codec.toJson(v) === null ? "refused" : "wrote";
      }
    } else {
      if (codec.fixedValue === undefined) {
        absent(outDir, name, unit + "." + root + " is not a fixed root: this leg's binary write-refusal rows set the fixed class");
        continue;
      }
      const x = codec.fixedValue.load(wire);
      if (x === null) { throw new Error(name + ": load refused " + wirePath); }
      setFixed(x, path, kind, text);
      if (surface === "write-refusal-message") {
        const m = await messageUnit(unit, root);
        if (m === null) { absent(outDir, name, "no message form for " + unit + "." + root + ": docs/SPEC-TABLES.md §3.3"); continue; }
        const size = m.measure([x], {});
        answer = size < 0 || m.save([x], new Uint8Array(size), {}) !== size ? "refused" : "wrote";
      } else {
        answer = codec.fixedValue.save(x) < 0 ? "refused" : "wrote";
      }
    }
    writeFileSync(join(outDir, name), answer + "\n");
  }
}
