// cards.mjs — the JavaScript card facade (tables/cards/cards.go, parse.go,
// render.go), the owning layer a customer imports.
//
// The generated variable-form codecs (CardLoad/CardLoadMeasure/CardSave,
// CardToJson/CardFromJson) answer the wire and text forms; this facade is the
// thin owning layer: a Record that keeps its own defensive copy of the wire,
// checked writes that refuse before publishing, and the two customer ceilings
// the schema deliberately leaves unbounded (docs/SPEC-TABLES.md §2.5, §2.9).
// It is the Go leg's facade in JavaScript's own spelling: Load, FromJSON,
// ToJSON, Wire, Parse and Render, each matching the Go leg byte for byte on the
// shared corpus (tla/CardHops.tla — ImplementationsAgree, HopOrderIrrelevant).

import { createHash } from "node:crypto";
import {
  Card,
  CardLoad,
  CardLoadMeasure,
  CardLoadRetain,
  CardMeasureRetain,
  CardSave,
  CardSaveRetain,
  CardToJson,
  CardFromJson,
  TableVariableReport,
  TableVariableRetain,
} from "../../../build/tables-generated-js/cards/CardsVariable.js";

// MaxBodyBytes is the customer ceiling on one card's opaque body. The schema
// declares body *string with no bound (docs/SPEC-TABLES.md §2.5); this facade
// refuses a larger body before any bytes are published.
export const MaxBodyBytes = 64 << 10;

// MaxListEntries is the customer ceiling on each of a card's collection fields
// (paths/new/shared/needs/extra/consumers), which the schema leaves unbounded
// (docs/SPEC-TABLES.md §2.9).
export const MaxListEntries = 512;

// CardError is a facade refusal. `class` is one of the closed refusal classes
// tables/cards/conformance/FORMAT.md fixes (body-bound, list-bound,
// stranded-key, malformed), never free text, so every language spells the same
// refusal.
export class CardError extends Error {
  constructor(message, cls) {
    super(message);
    this.class = cls;
  }
}

// Record owns a private defensive copy of the wire and the decoded Card. No
// shallow copy of the caller's bytes escapes (docs/SPEC-TABLES.md §6.2): the
// wire field is the caller's bytes copied, and Wire() returns a fresh copy.
export class Record {
  constructor(wire, root, report) {
    this._wire = wire;
    this._root = root;
    this._report = report;
  }

  // Wire returns a defensive copy of the record's wire bytes.
  Wire() {
    const w = new Uint8Array(this._wire.length);
    w.set(this._wire);
    return w;
  }
}

// ---- UTF-8 helpers (docs/SPEC-TABLES.md §2.5) ------------------------------

const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();
const fatalDecoder = new TextDecoder("utf-8", { fatal: true });

function utf8Bytes(s) {
  return textEncoder.encode(s);
}

function isValidUtf8(bytes) {
  try {
    fatalDecoder.decode(bytes);
    return true;
  } catch (e) {
    return false;
  }
}

function utf8Length(s) {
  return textEncoder.encode(s).length;
}

function decodeUtf8(bytes) {
  return textDecoder.decode(bytes);
}

// isWellFormedUtf8 rejects a lone surrogate, which the generated encoder would
// spell as an invalid UTF-8 sequence (docs/SPEC-TABLES.md §2.5).
function isWellFormedUtf8(s) {
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c >= 0xd800 && c <= 0xdbff) {
      if (i + 1 >= s.length) return false;
      const c2 = s.charCodeAt(i + 1);
      if (c2 < 0xdc00 || c2 > 0xdfff) return false;
      i++;
    } else if (c >= 0xdc00 && c <= 0xdfff) {
      return false;
    }
  }
  return true;
}

function hasNul(s) {
  return s.indexOf("\u0000") >= 0;
}

// ---- the owning facade (tables/cards/cards.go) ------------------------------

// Load reads wire into a Record using the generated load (docs/SPEC-TABLES.md
// §6.2). Failures and malformed wire are errors; tolerant events ride in the
// returned report. The ORIGINAL wire is copied, never rewritten, so an old
// input is preserved byte for byte.
export function Load(wire) {
  const report = new TableVariableReport();
  const n = CardLoadMeasure(wire);
  if (n < 0) {
    CardLoad(wire, report);
    return { record: null, report, error: new CardError("cards: wire is not a card", "malformed") };
  }
  const root = CardLoad(wire, report);
  if (root === null) {
    return { record: null, report, error: new CardError("cards: wire is malformed", "malformed") };
  }
  const err = checkCard(root);
  if (err !== null) {
    return { record: null, report, error: err };
  }
  const w = new Uint8Array(wire.length);
  w.set(wire);
  return { record: new Record(w, root, report), report, error: null };
}

// FromJSON builds a Record from JSON text through the generated CardFromJson
// and CardSave (docs/SPEC-TABLES.md §16). Unknown, mismatched, clamped and
// malformed inputs are refused, as are the customer bounds and any body or
// element that is not well-formed UTF-8 without NUL. Bytes are published only
// on complete success; a refused write leaves no destination and mutates
// nothing.
export function FromJSON(text) {
  const report = new TableVariableReport();
  const root = CardFromJson(decodeUtf8(text), report);
  normalizeEnums(root);
  if (report.malformed) {
    return { record: null, error: new CardError("cards: JSON is malformed", "malformed") };
  }
  if (report.unknown > 0 || report.kindMismatch > 0 || report.clamped > 0) {
    return {
      record: null,
      error: new CardError(
        `cards: JSON has loss events (unknown=${report.unknown} kindMismatch=${report.kindMismatch} clamped=${report.clamped}): refused, never truncated`,
        "malformed",
      ),
    };
  }
  const err = checkCard(root);
  if (err !== null) {
    return { record: null, error: err };
  }
  const wire = CardSave(root);
  if (wire === null) {
    return { record: null, error: new CardError("cards: save did not fill the measured wire", "malformed") };
  }
  return { record: new Record(wire, root, report), error: null };
}

// ToJSON writes the record's known-field JSON text and returns the record's
// load report. It is the known-field view, not a lossless archival rewrite
// (docs/SPEC-TABLES.md §16).
export function ToJSON(record) {
  if (record === null || record._root === null) {
    return { text: null, report: null, error: new CardError("cards: nil record", "malformed") };
  }
  const text = CardToJson(record._root);
  return { text: utf8Bytes(text), report: record._report, error: null };
}

// normalizeEnums maps the JSON spelling "None" of an enum's zero value back to
// null, the JavaScript spelling the generated writer refuses to serialize as a
// named variant (docs/SPEC-TABLES.md §16). The generated CardFromJson assigns
// the string verbatim; the Go leg's CardFromJson resolves "None" to the zero
// value, so this facade does the same resolution before any write.
function normalizeEnums(root) {
  if (root.tier === "None") root.tier = null;
  if (root.priority === "None") root.priority = null;
  if (root.column === "None") root.column = null;
}

// ---- the checked-write helper (tables/cards/cards.go checkCard) -------------

// checkCard enforces the customer bounds and the well-formedness of the opaque
// body and every collection element before bytes are published
// (docs/SPEC-TABLES.md §2.5, §2.9).
function checkCard(root) {
  if (root === null) {
    return new CardError("cards: nil card", "malformed");
  }
  if (root.body !== null) {
    const n = utf8Length(root.body);
    if (n > MaxBodyBytes) {
      return new CardError(`cards: body is ${n} bytes, past the ${MaxBodyBytes}-byte ceiling`, "body-bound 65536");
    }
    if (!isWellFormedUtf8(root.body) || hasNul(root.body)) {
      return new CardError("cards: body is not well-formed UTF-8 without NUL", "malformed");
    }
  }
  for (const [list, name] of [
    [root.paths, "paths"],
    [root._new, "_new"],
    [root.shared, "shared"],
  ]) {
    const err = checkPathList(list, name);
    if (err !== null) return err;
  }
  const errNeeds = checkIDList(root.needs, "needs");
  if (errNeeds !== null) return errNeeds;
  const errExtra = checkExtras(root.extra);
  if (errExtra !== null) return errExtra;
  const errConsumers = checkConsumers(root.consumers);
  if (errConsumers !== null) return errConsumers;
  return null;
}

function checkPathList(list, name) {
  if (list.length > MaxListEntries) {
    return new CardError(
      `cards: ${name} has ${list.length} entries, past the ${MaxListEntries}-entry ceiling (docs/SPEC-TABLES.md §2.9)`,
      `list-bound ${name} 512`,
    );
  }
  for (const e of list) {
    if (!isWellFormedUtf8(e.s) || hasNul(e.s)) {
      return new CardError(`cards: ${name} has an element that is not well-formed UTF-8 without NUL`, "malformed");
    }
  }
  return null;
}

function checkIDList(list, name) {
  if (list.length > MaxListEntries) {
    return new CardError(
      `cards: ${name} has ${list.length} entries, past the ${MaxListEntries}-entry ceiling (docs/SPEC-TABLES.md §2.9)`,
      `list-bound ${name} 512`,
    );
  }
  for (const e of list) {
    if (!isWellFormedUtf8(e.s) || hasNul(e.s)) {
      return new CardError(`cards: ${name} has an element that is not well-formed UTF-8 without NUL`, "malformed");
    }
  }
  return null;
}

// checkExtras enforces the extras rules before any facade write publishes: the
// 512-entry ceiling, the admitted header key grammar, no CR or LF in a key or
// value, well-formed UTF-8 keys and values without NUL, and no key duplicating
// a mapped header name (HEADER MAPPING, docs/SPEC-TABLES.md §2.9).
function checkExtras(list) {
  if (list.length > MaxListEntries) {
    return new CardError(
      `cards: extra has ${list.length} entries, past the ${MaxListEntries}-entry ceiling (docs/SPEC-TABLES.md §2.9)`,
      "list-bound extra 512",
    );
  }
  for (const e of list) {
    if (!isWellFormedUtf8(e.key) || hasNul(e.key)) {
      return new CardError("cards: extra has a key that is not well-formed UTF-8 without NUL", "malformed");
    }
    if (!headerKeyRE.test(e.key)) {
      return new CardError(`cards: extra key ${JSON.stringify(e.key)} is not an admitted header key`, "malformed");
    }
    if (e.key.indexOf("\r") >= 0 || e.key.indexOf("\n") >= 0) {
      return new CardError("cards: extra has a key with a CR or LF", "malformed");
    }
    if (reservedHeaderKeys.has(e.key)) {
      return new CardError(`cards: extra key ${JSON.stringify(e.key)} duplicates a mapped header key`, "malformed");
    }
    if (e.value !== null) {
      if (!isWellFormedUtf8(e.value) || hasNul(e.value)) {
        return new CardError("cards: extra has a value that is not well-formed UTF-8 without NUL", "malformed");
      }
      if (e.value.indexOf("\r") >= 0 || e.value.indexOf("\n") >= 0) {
        return new CardError("cards: extra has a value with a CR or LF", "malformed");
      }
    }
  }
  return null;
}

function checkConsumers(list) {
  if (list.length > MaxListEntries) {
    return new CardError(
      `cards: consumers has ${list.length} entries, past the ${MaxListEntries}-entry ceiling (docs/SPEC-TABLES.md §2.9)`,
      "list-bound consumers 512",
    );
  }
  for (const e of list) {
    for (const [name, s] of [
      ["kind", e.kind],
      ["who", e.who],
      ["route", e.route],
      ["model", e.model],
      ["outcome", e.outcome],
      ["line", e.line],
    ]) {
      if (!isWellFormedUtf8(s) || hasNul(s)) {
        return new CardError(`cards: consumers has a ${name} that is not well-formed UTF-8 without NUL`, "malformed");
      }
    }
  }
  return null;
}

// ---- parse (tables/cards/parse.go) -----------------------------------------

// headerLineRE is the admitted header grammar: a key of one letter then
// letters/digits/hyphens, a colon, optional whitespace, and the rest as the
// value. Anchored at the start, so leading whitespace is not a key.
const headerLineRE = /^([A-Za-z][A-Za-z0-9-]*):\s*(.*)$/;

// headerKeyRE is the admitted key grammar alone (TEXT GRAMMAR).
const headerKeyRE = /^[A-Za-z][A-Za-z0-9-]*$/;

// deadlineRE is the customer spelling DEADLINE accepts.
const deadlineRE = /^finish within ([0-9]+) minutes\.$/;

// strandedKeys is the customer's exact case-sensitive set refused once prose
// has begun (nova-sprint pkg/swarm/lintheader.go cardHeaderKeys).
const strandedKeys = new Set([
  "KIND", "PATHS", "TEST", "LEGS", "SOURCE", "START", "STOP", "DEPENDS-ON",
  "PRIORITY", "NEW", "SHARED", "REPO", "BASE", "tier", "RESULT",
]);

// reservedHeaderKeys is the mapped header vocabulary, including RESULT and the
// agreeing tier line.
const reservedHeaderKeys = new Set([
  "RESULT", "tier", "REPO", "BASE", "KIND", "PRIORITY", "PATHS", "NEW",
  "SHARED", "DEPENDS-ON", "TEST", "DONE-WHEN", "DEADLINE", "SOURCE",
]);

// Parse reads admitted brief text and publishes a typed Record only on complete
// checked success (docs/SPEC-TABLES.md §2, §6.2). The body is the exact suffix
// immediately after the last header line's terminator; header layout may
// canonicalize, body bytes never do.
export function Parse(text) {
  if (!isValidUtf8(text)) {
    return { record: null, error: new CardError("cards: brief is not valid UTF-8", "malformed") };
  }
  if (text.indexOf(0) >= 0) {
    return { record: null, error: new CardError("cards: brief contains a NUL byte", "malformed") };
  }

  const lines = splitLines(text);
  if (lines.length === 0) {
    return { record: null, error: new CardError("cards: brief has no RESULT line", "malformed") };
  }

  const first = matchHeader(lines[0].content);
  if (!first.ok || first.key !== "RESULT") {
    return { record: null, error: new CardError("cards: line 1 is not a RESULT line", "malformed") };
  }
  const p = {
    id: "", sha: "", tier: null, repo: "", base: "", kind: "", priority: null,
    stream: "", paths: [], news: [], shared: [], needs: [], test: "",
    doneWhen: "", source: "", deadline: 0, extras: [],
  };
  const errResult = applyResult(p, first.value);
  if (errResult !== null) return { record: null, error: errResult };

  let lastHeaderEnd = lines[0].termEnd;
  const seen = new Map([["RESULT", 1]]);
  let prose = false;
  for (const ln of lines.slice(1)) {
    if (isBlank(ln.content)) continue;
    const m = matchHeader(ln.content);
    if (!m.ok) {
      prose = true;
      continue;
    }
    if (prose) {
      if (strandedKeys.has(m.key)) {
        return { record: null, error: new CardError(`cards: stranded header key ${JSON.stringify(m.key)} on line ${ln.num}`, `stranded-key ${m.key}`) };
      }
      continue;
    }
    const err = applyHeader(p, m.key, m.value, ln.num, seen);
    if (err !== null) return { record: null, error: err };
    lastHeaderEnd = ln.termEnd;
  }

  const body = text.subarray(lastHeaderEnd);
  return build(p, body);
}

// splitLines returns every physical line with its content and its terminator
// end offset. A trailing LF does not invent a final empty line.
function splitLines(text) {
  const refs = [];
  let i = 0;
  let n = 1;
  while (i < text.length) {
    const start = i;
    let j = text.indexOf(0x0a, start);
    let lineEnd = text.length;
    let termEnd = text.length;
    if (j >= 0) {
      lineEnd = j;
      termEnd = lineEnd + 1;
    }
    let content = text.subarray(start, lineEnd);
    if (content.length > 0 && content[content.length - 1] === 0x0d) {
      content = content.subarray(0, content.length - 1);
    }
    refs.push({ content, termEnd, num: n });
    n++;
    i = termEnd;
  }
  return refs;
}

// matchHeader recognizes a header key line; ok is false for prose, blank or any
// line with leading whitespace.
function matchHeader(content) {
  const m = headerLineRE.exec(decodeUtf8(content));
  if (m === null) return { key: "", value: "", ok: false };
  return { key: m[1], value: m[2], ok: true };
}

// applyResult parses the RESULT value "id sha=<sha> tier: <tier>"
// (docs/SPEC-TABLES.md §2, §4).
function applyResult(p, value) {
  const i = value.lastIndexOf(" tier:");
  if (i < 0) return new CardError('cards: RESULT line has no " tier:"', "malformed");
  const tierStr = value.slice(i + " tier:".length).trim();
  const rest = value.slice(0, i);
  const j = rest.lastIndexOf(" sha=");
  if (j < 0) return new CardError('cards: RESULT line has no " sha="', "malformed");
  const idStr = rest.slice(0, j).trim();
  const shaStr = rest.slice(j + " sha=".length);
  const errId = checkStrBound("id", idStr, 64);
  if (errId !== null) return errId;
  if (!isHex(shaStr) || (shaStr.length !== 12 && shaStr.length !== 40)) {
    return new CardError(`cards: RESULT sha ${JSON.stringify(shaStr)} is not 12 or 40 hex digits`, "malformed");
  }
  const tier = parseTier(tierStr);
  if (tier === null) return new CardError(`cards: RESULT tier: unknown tier ${JSON.stringify(tierStr)}`, "malformed");
  p.id = idStr;
  p.sha = shaStr;
  p.tier = tier;
  return null;
}

// applyHeader dispatches one header line to its field, an agreeing tier, or an
// extra, refusing duplicates and CR/LF inside any single-line value.
function applyHeader(p, key, value, num, seen) {
  if (value.indexOf("\r") >= 0 || value.indexOf("\n") >= 0) {
    return new CardError(`cards: ${key} on line ${num} has a CR or LF in its value`, "malformed");
  }
  if (key === "tier") {
    if (seen.has("tier")) {
      return new CardError(`cards: duplicate header key ${JSON.stringify(key)} on lines ${seen.get("tier")} and ${num}`, "malformed");
    }
    seen.set("tier", num);
    const t = parseTier(value.trim());
    if (t === null) return new CardError(`cards: tier on line ${num}: unknown tier ${JSON.stringify(value.trim())}`, "malformed");
    if (t !== p.tier) return new CardError(`cards: tier on line ${num} disagrees with RESULT`, "malformed");
    return null;
  }
  if (key === "RESULT") {
    return new CardError(`cards: duplicate header key ${JSON.stringify(key)} on lines ${seen.get("RESULT")} and ${num}`, "malformed");
  }
  if (reservedHeaderKeys.has(key)) {
    if (seen.has(key)) {
      return new CardError(`cards: duplicate header key ${JSON.stringify(key)} on lines ${seen.get(key)} and ${num}`, "malformed");
    }
    seen.set(key, num);
    return assignMapped(p, key, value, num);
  }
  const errKey = checkStrBound("extra key", key, 32);
  if (errKey !== null) return new CardError(`cards: line ${num}: ${errKey.message}`, "malformed");
  p.extras.push({ key, value });
  return null;
}

// assignMapped fills one mapped header field with its checked value.
function assignMapped(p, key, value, num) {
  switch (key) {
    case "REPO": return assignStr(p, "repo", value, 96, "REPO", num);
    case "BASE": return assignStr(p, "base", value, 96, "BASE", num);
    case "KIND": return assignStr(p, "kind", value, 24, "KIND", num);
    case "PRIORITY": {
      const pr = parsePriority(value.trim());
      if (pr === null) return new CardError(`cards: PRIORITY on line ${num}: unknown priority ${JSON.stringify(value.trim())}`, "malformed");
      p.priority = pr;
      return null;
    }
    case "PATHS": return splitListChecked(p, "paths", value, 192, true, "PATHS", num);
    case "NEW": return splitListChecked(p, "news", value, 192, false, "NEW", num);
    case "SHARED": return splitListChecked(p, "shared", value, 192, false, "SHARED", num);
    case "DEPENDS-ON": return splitListChecked(p, "needs", value, 64, false, "DEPENDS-ON", num);
    case "TEST": return assignStr(p, "test", value, 192, "TEST", num);
    case "DONE-WHEN": p.doneWhen = value; return null;
    case "DEADLINE": {
      const n = parseDeadline(value);
      if (n === null) return new CardError(`cards: DEADLINE on line ${num}: not of the form "finish within N minutes."`, "malformed");
      p.deadline = n;
      return null;
    }
    case "SOURCE": p.source = value; return null;
  }
  return null;
}

function assignStr(p, field, value, bound, name, num) {
  const err = checkStrBound(name, value, bound);
  if (err !== null) return new CardError(`cards: ${name} on line ${num}: ${err.message}`, "malformed");
  p[field] = value;
  return null;
}

function checkStrBound(name, value, bound) {
  if (utf8Length(value) > bound) {
    return new CardError(`cards: ${name} is ${utf8Length(value)} bytes, past the ${bound}-byte bound`, "malformed");
  }
  return null;
}

// splitListChecked splits a comma-separated list, trims elements, drops empty
// ones, and enforces the element bound and the entry ceiling
// (docs/SPEC-TABLES.md §2.9). A whole value of "-" or empty is an empty list;
// PATHS alone also takes the customer sentinel "none".
function splitListChecked(p, field, value, bound, noneSentinel, name, num) {
  const v = value.trim();
  if (v === "" || v === "-") {
    p[field] = [];
    return null;
  }
  if (noneSentinel && v === "none") {
    p[field] = [];
    return null;
  }
  const parts = v.split(",");
  const out = [];
  for (let part of parts) {
    part = part.trim();
    if (part === "") continue;
    const err = checkStrBound(name + " element", part, bound);
    if (err !== null) return new CardError(`cards: ${name} on line ${num}: ${err.message}`, "malformed");
    out.push(part);
  }
  if (out.length > MaxListEntries) {
    return new CardError(`cards: ${name} on line ${num} has ${out.length} entries, past the ${MaxListEntries}-entry ceiling`, "malformed");
  }
  p[field] = out;
  return null;
}

function parseTier(s) {
  switch (s) {
    case "flash": return "flash";
    case "pro": return "pro";
    case "heavy": return "heavy";
    case "frontier": return "frontier";
  }
  return null;
}

function parsePriority(s) {
  switch (s) {
    case "normal": return "normal";
    case "high": return "high";
    case "critical": return "critical";
    case "blocker": return "blocker";
  }
  return null;
}

function parseDeadline(s) {
  const m = deadlineRE.exec(s.trim());
  if (m === null) return null;
  const n = parseInt(m[1], 10);
  if (isNaN(n) || n < 0 || n > 100000) return null;
  return n;
}

function isHex(s) {
  if (s === "") return false;
  for (const c of s) {
    if (!((c >= "0" && c <= "9") || (c >= "a" && c <= "f") || (c >= "A" && c <= "F"))) return false;
  }
  return true;
}

function isBlank(content) {
  for (const b of content) {
    if (b !== 0x20 && b !== 0x09 && b !== 0x0d) return false;
  }
  return true;
}

// build publishes the parsed card through the generated save and checked write
// (docs/SPEC-TABLES.md §6.2). Bytes are published only on complete success.
function build(p, body) {
  const card = new Card();
  card.id = p.id;
  card.sha = p.sha;
  card.tier = p.tier;
  card.repo = p.repo;
  card._base = p.base;
  card.kind = p.kind;
  card.priority = p.priority;
  card.stream = p.stream;
  card.paths = p.paths.map((s) => ({ s }));
  card._new = p.news.map((s) => ({ s }));
  card.shared = p.shared.map((s) => ({ s }));
  card.needs = p.needs.map((s) => ({ s }));
  card.test = p.test;
  card.done = p.doneWhen !== "" ? p.doneWhen : null;
  card.deadline_minutes = p.deadline;
  card.source = p.source !== "" ? p.source : null;
  card.extra = p.extras.map((kv) => ({ key: kv.key, value: kv.value }));
  card.body = body.length > 0 ? decodeUtf8(body) : null;
  const err = checkCard(card);
  if (err !== null) return { record: null, error: err };
  const wire = CardSave(card);
  if (wire === null) {
    return { record: null, error: new CardError("cards: save did not fill the measured wire", "malformed") };
  }
  return { record: new Record(wire, card, new TableVariableReport()), error: null };
}

// ---- render (tables/cards/render.go) ---------------------------------------

// Render reads the record's wire into a fresh load and returns its
// informational text plus the load report (docs/SPEC-TABLES.md §6.2, §16).
export function Render(record) {
  if (record === null || record._wire === null) {
    return { text: null, report: null, error: new CardError("cards: nil record", "malformed") };
  }
  const report = new TableVariableReport();
  const n = CardLoadMeasure(record._wire);
  if (n < 0) {
    CardLoad(record._wire, report);
    return { text: null, report, error: new CardError("cards: wire is not a card", "malformed") };
  }
  const root = CardLoad(record._wire, report);
  if (root === null) {
    return { text: null, report, error: new CardError("cards: wire is malformed", "malformed") };
  }
  const text = renderCard(root);
  if (text === null) {
    return { text: null, report, error: new CardError("cards: the card has no text spelling", "malformed") };
  }
  return { text: utf8Bytes(text), report, error: null };
}

function renderCard(root) {
  const tierName = tierString(root.tier);
  if (tierName === null) return null;
  let out = "RESULT: " + root.id + " sha=" + root.sha + " tier: " + tierName + "\n";
  if (root.repo !== "") out += "REPO: " + root.repo + "\n";
  if (root._base !== "") out += "BASE: " + root._base + "\n";
  if (root.kind !== "") out += "KIND: " + root.kind + "\n";
  const prio = priorityString(root.priority);
  if (prio === null) return null;
  if (prio !== "") out += "PRIORITY: " + prio + "\n";
  if (root.paths.length > 0) out += "PATHS: " + root.paths.map((e) => e.s).join(", ") + "\n";
  if (root._new.length > 0) out += "NEW: " + root._new.map((e) => e.s).join(", ") + "\n";
  if (root.shared.length > 0) out += "SHARED: " + root.shared.map((e) => e.s).join(", ") + "\n";
  if (root.needs.length > 0) out += "DEPENDS-ON: " + root.needs.map((e) => e.s).join(", ") + "\n";
  if (root.test !== "") out += "TEST: " + root.test + "\n";
  if (root.done !== null) out += "DONE-WHEN: " + root.done + "\n";
  if (root.deadline_minutes !== 0) out += "DEADLINE: finish within " + root.deadline_minutes + " minutes.\n";
  if (root.source !== null) out += "SOURCE: " + root.source + "\n";
  for (const e of root.extra) {
    out += e.key + ": ";
    if (e.value !== null) out += e.value;
    out += "\n";
  }
  if (root.body !== null) out += root.body;
  return out;
}

function tierString(t) {
  switch (t) {
    case "flash": return "flash";
    case "pro": return "pro";
    case "heavy": return "heavy";
    case "frontier": return "frontier";
  }
  return null;
}

function priorityString(p) {
  switch (p) {
    case null: return "";
    case "normal": return "normal";
    case "high": return "high";
    case "critical": return "critical";
    case "blocker": return "blocker";
  }
  return null;
}

// ---- upgrade and strict read (tables/cards/upgrade.go) -----------------------

// MaxRetainBytes is the customer ceiling on the retained-record byte store and
// MaxRetainIDs on the retained-id entry store; neither ever grows on demand
// (docs/SPEC-TABLES.md §6.6).
export const MaxRetainBytes = 1 << 20;
export const MaxRetainIDs = 4096;

// Upgrade rewrites wire through the generated retaining region path so every
// field the current declaration cannot name is preserved at the end of its own
// body (§6.6). It returns nil output and a named error when any value would be
// lost — RetainLost, KindMismatch, Clamped or Malformed — or when the wire is
// refused; the input wire is never mutated (tla/CardVersioning.tla:
// NoFieldLost, BodyUnchanged, RefusalKeepsOriginal, ShippedUpgradeSucceeds).
export function Upgrade(wire) {
  return upgrade(wire, MaxRetainBytes, MaxRetainIDs);
}

function upgrade(wire, retainBytes, retainIDs) {
  const report = new TableVariableReport();
  const n = CardLoadMeasure(wire);
  if (n < 0) {
    CardLoad(wire, report);
    return { out: null, report, error: new CardError("cards: upgrade: wire is not a card", "malformed") };
  }
  const retain = new TableVariableRetain(new Uint8Array(retainBytes), new Array(retainIDs));
  const root = CardLoadRetain(wire, retain, report);
  if (root === null) {
    return { out: null, report, error: new CardError("cards: upgrade: wire is malformed", "malformed") };
  }
  const err = checkCard(root);
  if (err !== null) return { out: null, report, error: err };
  const size = CardMeasureRetain(root, retain);
  if (size < 0) {
    return { out: null, report, error: new CardError("cards: upgrade: measure refused the retained card", "malformed") };
  }
  const out = new Uint8Array(size);
  const saved = CardSaveRetain(root, retain, out, report);
  if (saved === null || saved.length !== size) {
    return { out: null, report, error: new CardError("cards: upgrade: save did not fill the measured wire", "malformed") };
  }
  const lerr = lossErr(report);
  if (lerr !== null) return { out: null, report, error: lerr };
  return { out: saved, report, error: null };
}

// lossErr names the report events that make a rewrite unsafe (§6.6); Unknown,
// Retained and Widened alone are not loss (§4).
function lossErr(report) {
  if (report.retainLost > 0) {
    return new CardError(`cards: upgrade: ${report.retainLost} retained field(s) lost, refusing to replace`, "retain-lost");
  }
  if (report.kindMismatch > 0) {
    return new CardError(`cards: upgrade: ${report.kindMismatch} kind mismatch(es), refusing to replace`, "malformed");
  }
  if (report.clamped > 0) {
    return new CardError(`cards: upgrade: ${report.clamped} clamped value(s), refusing to replace`, "malformed");
  }
  if (report.malformed) {
    return new CardError("cards: upgrade: malformed wire, refusing to replace", "malformed");
  }
  return null;
}

// ReadStrict reads wire and refuses any data the current declaration cannot
// support: Unknown, KindMismatch, Clamped, Malformed and every load failure
// (docs/SPEC-TABLES.md §4).
export function ReadStrict(wire) {
  const { record, report, error } = Load(wire);
  if (error !== null) return { record: null, report, error };
  if (report.unknown > 0) return { record: null, report, error: new CardError(`cards: strict: ${report.unknown} unknown field(s), unsupported data`, "unknown-present") };
  if (report.kindMismatch > 0) return { record: null, report, error: new CardError(`cards: strict: ${report.kindMismatch} kind mismatch(es), unsupported data`, "malformed") };
  if (report.clamped > 0) return { record: null, report, error: new CardError(`cards: strict: ${report.clamped} clamped value(s), unsupported data`, "malformed") };
  if (report.malformed) return { record: null, report, error: new CardError("cards: strict: malformed wire, unsupported data", "malformed") };
  return { record, report, error: null };
}

// ---- the task hash and field edits (tables/cards/edit.go) --------------------

// refuseLossy names the read events that make a record unsafe to hash or
// rewrite (docs/SPEC-TABLES.md §4, §6.6).
function refuseLossy(report) {
  if (report.unknown > 0 || report.kindMismatch > 0 || report.clamped > 0 || report.retainLost > 0 || report.malformed) {
    throw new CardError(
      `cards: record has loss events (unknown=${report.unknown} kindMismatch=${report.kindMismatch} clamped=${report.clamped} retainLost=${report.retainLost} malformed=${report.malformed}): refused, never hashed or rewritten`,
      "unknown-present",
    );
  }
}

function hashU64(h, v) {
  const b = new DataView(new ArrayBuffer(8));
  b.setBigUint64(0, BigInt(v), true);
  h.update(new Uint8Array(b.buffer));
}

function hashText(h, text) {
  hashU64(h, text.length);
  h.update(text);
}

function hashPathList(h, list, name) {
  if (list === null || list === undefined) throw new CardError(`cards: ${name} list is malformed`, "malformed");
  hashU64(h, list.length);
  for (const e of list) hashText(h, utf8Bytes(e.s));
}

// TaskHash returns the hex digest of a record's TASK — body, paths, new,
// shared, test and done in that order, each framed as an unsigned 64-bit
// little-endian byte length then its bytes, each list as a count then its
// length-prefixed elements; needs, tier, priority and machine fields are
// excluded (docs/SPEC-TABLES.md §5; tla/CardHops.tla — TaskHashStable). A nil,
// failed, malformed or lossy record is refused with an error, never hashed.
export function TaskHash(record) {
  if (record === null || record._root === null) throw new CardError("cards: nil record", "malformed");
  refuseLossy(record._report);
  const h = createHash("sha256");
  hashText(h, utf8Bytes(record._root.body ?? ""));
  hashPathList(h, record._root.paths, "paths");
  hashPathList(h, record._root._new, "_new");
  hashPathList(h, record._root.shared, "shared");
  hashText(h, utf8Bytes(record._root.test ?? ""));
  hashText(h, utf8Bytes(record._root.done ?? ""));
  return h.digest("hex");
}

function validateStrings(name, values, bound) {
  for (const s of values) {
    if (utf8Length(s) > bound) {
      throw new CardError(`cards: ${name} element is ${utf8Length(s)} bytes, past its ${bound}-byte bound`, "malformed");
    }
    if (!isWellFormedUtf8(s) || hasNul(s)) {
      throw new CardError(`cards: ${name} has an element that is not well-formed UTF-8 without NUL`, "malformed");
    }
  }
}

function checkListCeiling(name, n) {
  if (n > MaxListEntries) {
    throw new CardError(`cards: ${name} has ${n} entries, past the ${MaxListEntries}-entry ceiling (docs/SPEC-TABLES.md §2.9)`, `list-bound ${name} 512`);
  }
}

// edit is the shared atomic rewrite: check the current record is safe, reload
// it into a fresh independent decode, apply mutate to the root, validate the
// whole rebuilt card, pack it, and only on complete success swap the record's
// owned wire (docs/SPEC-TABLES.md §6.2). Any refusal leaves the original wire
// untouched (tla/CardHops.tla — EditDropsUnknown, BodyUnchanged).
function edit(record, mutate) {
  if (record === null || record._root === null) throw new CardError("cards: nil record", "malformed");
  refuseLossy(record._report);
  const report = new TableVariableReport();
  const root = CardLoad(record._wire, report);
  if (root === null) throw new CardError("cards: reload into the fresh decode failed", "malformed");
  refuseLossy(report);
  mutate(root);
  const err = checkCard(root);
  if (err !== null) throw err;
  const wire = CardSave(root);
  if (wire === null) throw new CardError("cards: save did not fill the measured wire", "malformed");
  record._wire = wire;
  record._root = root;
  record._report = report;
}

export function SetNeeds(record, needs) {
  validateStrings("needs", needs, 64);
  checkListCeiling("needs", needs.length);
  edit(record, (root) => {
    root.needs = needs.map((s) => ({ s }));
  });
}

export function SetPaths(record, paths) {
  validateStrings("paths", paths, 192);
  checkListCeiling("paths", paths.length);
  edit(record, (root) => {
    root.paths = paths.map((s) => ({ s }));
  });
}

export function SetTier(record, tier) {
  if (!["flash", "pro", "heavy", "frontier"].includes(tier)) {
    throw new CardError(`cards: tier ${tier} is not a declared variant (docs/SPEC-TABLES.md §8)`, "malformed");
  }
  edit(record, (root) => { root.tier = tier; });
}

export function SetPriority(record, priority) {
  if (!["normal", "high", "critical", "blocker"].includes(priority)) {
    throw new CardError(`cards: priority ${priority} is not a declared variant (docs/SPEC-TABLES.md §8)`, "malformed");
  }
  edit(record, (root) => { root.priority = priority; });
}

export function SetColumn(record, column) {
  if (!["waiting", "ready", "working", "review", "merging", "landed", "plan", "check"].includes(column)) {
    throw new CardError(`cards: column ${column} is not a declared variant (docs/SPEC-TABLES.md §8)`, "malformed");
  }
  edit(record, (root) => { root.column = column; });
}

export function SetHolder(record, holder) {
  validateStrings("holder", [holder], 32);
  edit(record, (root) => { root.holder = holder; });
}
