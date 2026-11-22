// THE WRITE-REFUSAL SURFACES (docs/SPEC.md §5.1, test/conformance/README.md).
//
// Each row loads one instance, sets ONE field to the value the row names, and
// writes it through the writer its surface names: the root's own form's save
// (`write-refusal`; a fixed root's own form is the fixed form, as the Go leg
// writes it), the fixed form's FixedSave (`write-refusal-fixed`), the message
// form's SaveMessages against the connection's unit (`write-refusal-message`),
// the text writer (`write-refusal-json`), or the cook writer
// (`write-refusal-cook`: CookMeasure, then CookFrom little-endian, over the
// variable value). The answer is what that writer did: `refused` when it
// answered its refusal, `wrote` when it completed, and `threw` when it raised
// instead of answering (the row is then red, and the throw is on stderr).
// `unrepresentable` is answered only where the storage the writer reads cannot
// hold the value at all (a JavaScript Map holds a key once; a typed array has
// one length).
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

// resize answers `arr` at `n` elements, more being copies of element 0 and
// fewer dropping the tail (the `elems` kind), or false where the storage has
// one length (a typed array).
function resize(arr, n, path) {
  if (ArrayBuffer.isView(arr)) { return false; }
  if (!Array.isArray(arr)) { throw new Error(path + ": elems names no array"); }
  if (!Number.isInteger(n) || n < 0) { throw new Error(path + ": " + n + " is not an element count"); }
  while (arr.length < n) { arr.push(structuredClone(arr[0])); }
  arr.length = n;
  return true;
}

// setFixed sets the one field a row names in the generated fixed class; it
// answers false where that class cannot hold the value.
export function setFixed(value, path, kind, text) {
  const { owner, last } = walk(value, path, camel);
  const p = own(owner, camel(last.name), path);
  const set = (v) => { if (last.index < 0) { owner[p] = v; } else { owner[p][last.index] = v; } };
  const get = () => (last.index < 0 ? owner[p] : owner[p][last.index]);
  switch (kind) {
    case "count":
      owner[own(owner, p + "Count", path)] = Number(text);
      return true;
    case "elems":
      return resize(get(), Number(text), path);
    case "mask":
      // a flags field's raw bits, in decimal (FL-06)
      set(typeof get() === "bigint" ? BigInt(text) : Number(text));
      return true;
    case "length": {
      // the used length the row names, over storage filled with `a` as far
      // as it reaches: a length past the storage is the row's point
      const n = Number(text);
      const storage = owner[p];
      for (let i = 0; i < n && i < storage.length; i++) { storage[i] = 0x61; }
      owner[own(owner, p + "Length", path)] = n;
      return true;
    }
    case "bytes": {
      const b = hexBytes(text);
      const storage = owner[p];
      for (let i = 0; i < b.length && i < storage.length; i++) { storage[i] = b[i]; }
      owner[own(owner, p + "Length", path)] = b.length;
      return true;
    }
    case "int":
      set(typeof get() === "bigint" ? BigInt(text) : Number(text));
      return true;
    case "ordinal":
      set(Number(text));
      return true;
    case "tag": {
      const u = get();
      if (u === null || typeof u !== "object" || !("type" in u)) { throw new Error(path + ": carries no union tag"); }
      u.type = Number(text);
      return true;
    }
    case "float":
      set(floatOf(text));
      return true;
    default:
      throw new Error(path + ": kind " + kind + " has no spelling in this leg's fixed storage");
  }
}

// setVariable sets the one field a row names in the variable value, where the
// text writer reads it: a text is a string, so `bytes` must be UTF-8 this leg's
// string can hold, and bytes it cannot are `unrepresentable`.
function decodeVariableLeaf(cur, kind, text) {
  switch (kind) {
    case "bytes": {
      const raw = hexBytes(text);
      if (cur instanceof Uint8Array) { return { ok: true, v: raw }; }
      try { return { ok: true, v: utf8.decode(raw) }; } catch { return { ok: false, v: null }; }
    }
    case "length": {
      const n = Number(text);
      if (cur instanceof Uint8Array) { return { ok: true, v: new Uint8Array(n).fill(0x61) }; }
      return { ok: true, v: "a".repeat(n) };
    }
    case "int":
      return { ok: true, v: typeof cur === "bigint" ? BigInt(text) : Number(text) };
    case "float":
      return { ok: true, v: floatOf(text) };
    case "mask":
      return { ok: true, v: typeof cur === "bigint" ? BigInt(text) : Number(text) };
    case "ordinal": {
      const n = Number(text);
      return { ok: true, v: n === 0 ? null : "__invalid_ordinal_" + text };
    }
    case "tag": {
      const n = Number(text);
      return { ok: true, v: n === 0 ? null : { arm: "__invalid_tag_" + text, value: null } };
    }
    case "count": {
      const n = Number(text);
      // A NEGATIVE COUNT IS A VALUE THAT IS NO LIST (SF-28): the variable
      // value's array is a native Array, and a dynamically typed field can
      // hold the row's number where the list was.
      if (n < 0) { return { ok: true, v: n }; }
      const arr = Array.isArray(cur) ? cur.slice(0, n) : [];
      while (arr.length < n) { arr.push(structuredClone(cur[0])); }
      return { ok: true, v: arr };
    }
    default:
      throw new Error("kind " + kind + " has no spelling in this leg's variable value");
  }
}

// variableSlot answers the getter and setter of the field `path` names in the
// variable value, where a field is held under its declared name.
function variableSlot(value, path) {
  const segments = path.split(".");
  let cur = value;
  for (const s of segments.slice(0, -1)) {
    const { name, index } = segmentOf(s);
    if (cur === null || typeof cur !== "object" || !(name in cur)) { throw new Error(path + ": no field " + name); }
    cur = index < 0 ? cur[name] : cur[name][index];
  }
  const last = segmentOf(segments[segments.length - 1]);
  if (cur === null || typeof cur !== "object" || !(last.name in cur)) { throw new Error(path + ": no field " + last.name); }
  const owner = cur;
  return {
    get: () => (last.index < 0 ? owner[last.name] : owner[last.name][last.index]),
    set: (v) => { if (last.index < 0) { owner[last.name] = v; } else { owner[last.name][last.index] = v; } },
  };
}

// setStructural sets the 2026-11-22 kinds that change a value's shape rather
// than one scalar: `elems` (a fixed or keyed array's element count), `dupkey`
// (a map's first entry held twice) and `point` (a pointer aimed at the node
// another pointer reaches, a cycle when that node encloses it).
function setStructural(value, path, kind, text) {
  const slot = variableSlot(value, path);
  switch (kind) {
    case "elems":
      return resize(slot.get(), Number(text), path);
    case "dupkey": {
      const map = slot.get();
      if (text === "0") { return true; }
      if (text !== "1") { throw new Error(path + ": dupkey takes 0 or 1, not " + text); }
      // a Map holds each key once: the duplicate has no spelling here
      if (map instanceof Map) { return false; }
      throw new Error(path + ": dupkey names no map");
    }
    case "point": {
      const node = variableSlot(value, text).get();
      if (node === null || typeof node !== "object") { throw new Error(text + ": points at no node"); }
      slot.set(node);
      return true;
    }
    default:
      throw new Error(path + ": " + kind + " is not a structural kind");
  }
}

export function setVariable(value, path, kind, text) {
  if (kind === "elems" || kind === "dupkey" || kind === "point") { return setStructural(value, path, kind, text); }
  const segments = path.split(".");
  let cur = value;
  for (let i = 0; i < segments.length - 1; i++) {
    const { name, index } = segmentOf(segments[i]);
    if (cur === null || typeof cur !== "object" || !(name in cur)) { throw new Error(path + ": no field " + name); }
    const next = cur[name];
    if (next instanceof Map && index >= 0 && i === segments.length - 2) {
      const entries = Array.from(next.entries()).sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0));
      const which = segments[segments.length - 1];
      const leaf = decodeVariableLeaf(which === "key" ? entries[index][0] : entries[index][1], kind, text);
      if (!leaf.ok) { return false; }
      if (which === "key") { entries[index][0] = leaf.v; } else { entries[index][1] = leaf.v; }
      next.clear();
      for (const [k, v] of entries) { next.set(k, v); }
      return true;
    }
    cur = index < 0 ? next : next[index];
  }
  if (cur === null || typeof cur !== "object") { throw new Error(path + ": no owner"); }
  const last = segmentOf(segments[segments.length - 1]);
  if ("arm" in cur && "value" in cur && !(last.name in cur)) {
    const leaf = decodeVariableLeaf(cur.value, kind, text);
    if (!leaf.ok) { return false; }
    cur.arm = last.name;
    cur.value = leaf.v;
    return true;
  }
  const p = own(cur, last.name, path);
  const slot = last.index < 0 ? cur[p] : cur[p][last.index];
  const isTextListBox = last.index >= 0 && slot !== null && typeof slot === "object" && !(slot instanceof Uint8Array) && "value" in slot && Object.keys(slot).length === 1;
  const leaf = decodeVariableLeaf(isTextListBox ? slot.value : slot, kind, text);
  if (!leaf.ok) { return false; }
  if (isTextListBox) {
    slot.value = leaf.v;
  } else if (last.index < 0) {
    cur[p] = leaf.v;
  } else {
    cur[p][last.index] = leaf.v;
  }
  return true;
}

function connectionUnit(lines, connection) {
  const c = lines.find((f) => f[0] === "connection" && f[1] === connection);
  return c === undefined ? null : c[2];
}

function absent(outDir, name, why) {
  writeFileSync(join(outDir, name + ".absent"), why + "\n");
}

// writes answers what one writer did with the value, catching ONLY to say it
// threw: a writer refuses by its answer, never by raising, so a throw is
// answered `threw` (red against either verdict) and named on stderr.
function writes(name, write) {
  try {
    return write() ? "wrote" : "refused";
  } catch (e) {
    process.stderr.write(name + ": the writer threw: " + (e && e.stack ? e.stack : String(e)) + "\n");
    return "threw";
  }
}

// surfaceWriteRefusal answers one of the five surfaces. `codecFor(unit, root)`
// is main.mjs's text codec (the fixed form bound in for a fixed root, and the
// root's cook writer beside it), `messageUnit(unit, root)` the root's message
// verbs, and `fixedValueOf(unit, root, wire)` the generated fixed class loaded
// from the wire, or fresh where the wire is `-`.
export async function surfaceWriteRefusal(surface, lines, outDir, codecFor, messageUnit, fixedValueOf) {
  for (const f of lines) {
    if (f[0] !== surface) { continue; }
    // <surface> <case> <unit> <root> <wire> <path> <kind> <value>
    const [, name, unitName, root, wirePath, path, kind, text] = f;
    const unit = surface === "write-refusal-message" ? connectionUnit(lines, unitName) : unitName;
    if (unit === null) { throw new Error(name + ": connection " + unitName + " names no unit"); }
    let answer;
    if (surface === "write-refusal-message") {
      // the message writer takes the generated fixed class, as a batch of one
      const m = await messageUnit(unit, root);
      if (m === null) { absent(outDir, name, "the generated unit " + unit + " carries no " + root + "SaveMessages: docs/SPEC-TABLES.md §3.3, §3.4"); continue; }
      const x = await fixedValueOf(unit, root, wirePath);
      if (x === null) { absent(outDir, name, "no fixed class for " + unit + "." + root + " on this leg"); continue; }
      answer = !setFixed(x, path, kind, text) ? "unrepresentable" : writes(name, () => {
        const size = m.measure([x], {});
        return size >= 0 && m.save([x], new Uint8Array(size), {}) === size;
      });
      writeFileSync(join(outDir, name), answer + "\n");
      continue;
    }
    const codec = await codecFor(unit, root);
    if (codec === null) { absent(outDir, name, "no generated unit carries " + unit + "." + root + ": docs/SPEC-TABLES.md §11"); continue; }
    if (wirePath === "-") { throw new Error(name + ": a fresh-value row (wire -) on " + surface + ": this leg starts only the message writer from the fresh value"); }
    const wire = new Uint8Array(readFileSync(wirePath));
    const variable = surface === "write-refusal-json" || surface === "write-refusal-cook" || (surface === "write-refusal" && codec.fixedValue === undefined);
    if (variable) {
      // the variable value: the text writer's and the cook writer's input, and
      // a variable root's own save. A fixed root's read here is its fixed load,
      // bridged to the variable value (fixed.mjs).
      if (surface === "write-refusal-cook" && codec.cook === null) { absent(outDir, name, unit + "." + root + " has no cook writer on this leg: docs/SPEC-TABLES.md §7.6"); continue; }
      const v = codec.load(wire, codec.report());
      if (v === null || v === undefined) { throw new Error(name + ": load refused " + wirePath); }
      if (!setVariable(v, path, kind, text)) {
        answer = "unrepresentable";
      } else if (surface === "write-refusal-json") {
        answer = writes(name, () => codec.toJson(v) !== null);
      } else if (surface === "write-refusal-cook") {
        // CookMeasure, then CookFrom in little-endian order (1); `wrote` when both complete
        answer = writes(name, () => {
          const size = codec.cook.measure(v);
          return typeof size === "number" && size >= 1 && codec.cook.from(v, new Uint8Array(size), 1) === true;
        });
      } else {
        answer = writes(name, () => codec.save(v) !== null);
      }
    } else {
      if (codec.fixedValue === undefined) { absent(outDir, name, unit + "." + root + " is not a fixed root"); continue; }
      const x = codec.fixedValue.load(wire);
      if (x === null) { throw new Error(name + ": load refused " + wirePath); }
      answer = !setFixed(x, path, kind, text) ? "unrepresentable" : writes(name, () => codec.fixedValue.save(x) >= 0);
    }
    writeFileSync(join(outDir, name), answer + "\n");
  }
}
