package jstable

// THE MESSAGE FORM'S ANNOUNCEMENT SURFACE (docs/SPEC-TABLES.md §3.3), form
// byte 2, which is a fixed table's. This card emits the announcement half:
// `TableVocabulary`, `Announce`, `AnnounceMeasure` and `AnnounceRead`. The
// batch body verbs (`MeasureMessages`, `SaveMessages`, `LoadMessages`) are the
// next card and are not here.
//
// §3.3 is a form-1 FILE the unit's compiler settles whole: "THE WHOLE
// ANNOUNCEMENT IS A COMPILE-TIME CONSTANT OF THE UNIT ... a backend emits
// `Announce` and `AnnounceMeasure` as a constant byte array and its length
// rather than as a walk." The byte array is `ir.TableAnnouncement`, the same
// bytes every other backend emits and the manifest pins.
//
// THE READER IS EMITTED (docs/SPEC-TABLES.md §3.3): `AnnounceRead` resolves an
// announcement "once", keeps the build version while it reads, and refuses a
// second announcement by name. Its semantics are the reference's `-1` lane and
// the Go and C# legs' reader, ported whole into the home module the same way
// `fixedruntime.go` ports the fixed runtime. Nothing in a generated module
// calls a function the leg does not emit.
import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func messageAnnouncementRuntime(u *ir.Unit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nexport const TableMessageBatchMax = 256;\n")
	fmt.Fprintf(&b, "export const TableMessageRefBitsHere = %d;\n", ir.TableMessageRefBits(len(ir.TableVocabulary(u))))
	fmt.Fprintf(&b, "export const TableMessageEntriesHere = %d;\n", len(ir.TableVocabulary(u)))
	fmt.Fprintf(&b, "export const TableMessageAnnouncement = new Uint8Array([%s]);\n", jsByteLiterals(ir.TableAnnouncement(u)))
	b.WriteString(jsAnnounceRuntime)
	return b.String()
}

func jsByteLiterals(data []byte) string {
	var s strings.Builder
	for _, v := range data {
		fmt.Fprintf(&s, "0x%02x,", v)
	}
	return s.String()
}

// jsAnnounceRuntime is the emitted JavaScript. It is the language's own shape
// over the reference's semantics (docs/SPEC-TABLES.md §6.1): data members
// UpperCamelCase, methods lowerCamelCase, and a refusal is a report field
// rather than an exception (the reference's `-1`).
const jsAnnounceRuntime = `
export const TableMessageOpenOk = 0;
export const TableMessageOpenRefused = 1;
export const TableMessageOpenDamaged = 2;
export class TableVocabulary {
  constructor() { this.Reset(); }
  Reset() {
    this.Entries = [];
    this.Count = 0;
    this.RefBits = 1;
    this.BuildVersion = 0n;
    this.Announced = false;
    this.Refused = false;
    this.MaxBytes = 64 * 1024;
    this.Strict = false;
  }
  Init(storage) {
    this.Entries = storage;
    this.Count = 0;
    this.RefBits = 1;
    this.BuildVersion = 0n;
    this.Announced = false;
    this.Refused = false;
    this.MaxBytes = 64 * 1024;
    this.Strict = false;
  }
}
export function AnnounceMeasure() { return TableMessageAnnouncement.length; }
export function Announce(buffer) {
  if (buffer.length < TableMessageAnnouncement.length) { return -1; }
  buffer.set(TableMessageAnnouncement);
  return TableMessageAnnouncement.length;
}
// The announcement is an ordinary form-1 file, so its reader is the form-1
// reader (docs/SPEC-TABLES.md §3): a bounded view of the body, the id trailer,
// and the framing pre-walk that refuses a body ending after its terminator.
const TableMessageVersionId = BigInt("0xfffffffffffffffe");
const TableMessageVocabularyId = BigInt("0xfffffffffffffffd");
const TableMessageNodeId = BigInt("0xffffffffffffffff");
function TableMessageRead64(bytes, at) {
  let v = 0n;
  for (let i = 7; i >= 0; i--) { v = (v << 8n) | BigInt(bytes[at + i]); }
  return v;
}
class TableMessageReader {
  constructor(buffer, ids) { this.Buffer = buffer; this.Ids = ids; this.Offset = 0; }
  Clone() { const r = new TableMessageReader(this.Buffer, this.Ids); r.Offset = this.Offset; return r; }
  Has(n) { return n >= 0 && this.Offset >= 0 && this.Offset <= this.Buffer.length && n <= this.Buffer.length - this.Offset; }
  Room(n) { return n <= BigInt(this.Buffer.length - this.Offset); }
  Get8() { return this.Buffer[this.Offset++]; }
  Get32() {
    const v = this.Buffer[this.Offset] | (this.Buffer[this.Offset + 1] << 8) |
      (this.Buffer[this.Offset + 2] << 16) | (this.Buffer[this.Offset + 3] << 24);
    this.Offset += 4;
    return v >>> 0;
  }
  Get64() { const v = TableMessageRead64(this.Buffer, this.Offset); this.Offset += 8; return v; }
  LebPair() {
    const at = this.Offset;
    if (at < this.Buffer.length) {
      const b = this.Buffer[at];
      if (b < 128) { this.Offset = at + 1; return [BigInt(b), true]; }
    }
    return [0n, false];
  }
  LebWide() {
    const at = this.Offset;
    let v = 0n;
    for (let i = 0; i < 10; i++) {
      if (!this.Has(1)) { break; }
      const b = this.Get8();
      if (i === 9 && b > 1) { break; }
      v |= BigInt(b & 127) << BigInt(7 * i);
      if (b < 128) {
        if (i > 0 && b === 0) { break; }
        return [v, true];
      }
    }
    this.Offset = at;
    return [0n, false];
  }
  Leb() { const [v, ok] = this.LebPair(); if (ok) { return [v, true]; } return this.LebWide(); }
  Resolve(ref) {
    if (ref === 0n || ref > BigInt(Math.floor(this.Ids.length / 8))) { return [0n, false]; }
    return [TableMessageRead64(this.Ids, Number((ref - 1n) * 8n)), true];
  }
  Body() {
    const [n, ok] = this.Leb();
    if (!ok || !this.Room(n)) { return [null, false]; }
    const end = this.Offset + Number(n);
    const sub = new TableMessageReader(this.Buffer.subarray(this.Offset, end), this.Ids);
    this.Offset = end;
    return [sub, true];
  }
  Skip(kind) {
    const fixed = TableMessageKindBytes(kind);
    if (fixed !== 0) { if (!this.Has(fixed)) { return false; } this.Offset += fixed; return true; }
    switch (kind) {
      case 17: case 30: { const [, ok] = this.Leb(); return ok; }
      case 12: case 13: case 14: case 16: case 31: case 32: case 33: { const [, ok] = this.Body(); return ok; }
      case 15: {
        const [ref, ok] = this.Leb();
        if (!ok) { return false; }
        if (ref === 0n) { return true; }
        if (!this.Has(1)) { return false; }
        this.Offset++;
        const [, bok] = this.Body();
        return bok;
      }
    }
    return false;
  }
  EndsEarly() {
    const r = this.Clone();
    for (;;) {
      const [ref, ok] = r.Leb();
      if (!ok) { return false; }
      if (ref === 0n) { return r.Offset !== r.Buffer.length; }
      const [, rok] = r.Resolve(ref);
      if (!rok || !r.Has(1)) { return false; }
      if (!r.Skip(r.Get8())) { return false; }
    }
  }
}
function TableMessageKindBytes(kind) {
  switch (kind) {
    case 1: case 2: case 6: case 20: case 25: return 1;
    case 3: case 7: case 21: case 26: return 2;
    case 4: case 8: case 10: case 22: case 27: return 4;
    case 5: case 9: case 11: case 23: case 28: return 8;
    case 18: case 19: case 24: case 29: return 16;
  }
  return 0;
}
function TableMessageOpen(data) {
  if (data.length === 0) { return [null, TableMessageOpenDamaged]; }
  if (data[0] !== 1) { return [null, TableMessageOpenRefused]; }
  if (data.length < 9) { return [null, TableMessageOpenDamaged]; }
  const n = TableMessageRead64(data, data.length - 8);
  if (n > BigInt(Math.floor((data.length - 9) / 8))) { return [null, TableMessageOpenDamaged]; }
  const start = data.length - 8 - Number(n) * 8;
  return [new TableMessageReader(data.subarray(1, start), data.subarray(start, data.length - 8)), TableMessageOpenOk];
}
function TableMessageOpenFramed(data) {
  const [r, verdict] = TableMessageOpen(data);
  if (verdict === TableMessageOpenOk && r.EndsEarly()) { return [r, TableMessageOpenDamaged]; }
  return [r, verdict];
}
function TableMessageBits(n) { let count = 0; while (n !== 0n) { count++; n >>= 1n; } return count; }
function TableMessageKnownKind(k) { return k <= 33; }
function TableMessageKindBits(k) {
  if (k === 0 || k === 32) { return 0; }
  if (k === 1) { return 1; }
  switch (k) {
    case 2: case 6: case 20: case 25: return 8;
    case 3: case 7: case 21: case 26: return 16;
    case 4: case 8: case 10: case 22: case 27: return 32;
    case 5: case 9: case 11: case 23: case 28: return 64;
    case 18: case 19: case 24: case 29: return 128;
  }
  return -1;
}
const TableMessageFloat = new Float32Array(1);
const TableMessageWords = new Uint32Array(TableMessageFloat.buffer);
export function TableMessageFloat32FromBits(bits) { TableMessageWords[0] = bits >>> 0; return TableMessageFloat[0]; }
function TableMessageShape() {
  return { Kind: 0, Packing: 0, Bits: 0, Base: [0n, 0n], Min: 0, Max: 0, QMin: 0, QDelta: 0, QCount: 0 };
}
function TableMessageShapeRead(r, k) {
  const s = TableMessageShape();
  s.Kind = k;
  s.Bits = TableMessageKindBits(k);
  if ((k >= 2 && k <= 10) || (k >= 18 && k <= 29)) {
    if (!r.Has(1)) { return [s, false]; }
    s.Packing = r.Get8();
    if (s.Packing === 0) { return [s, true]; }
    if (s.Packing === 1 && k !== 10) {
      const [n, ok] = r.Leb();
      if (!ok || n > BigInt(s.Bits)) { return [s, false]; }
      s.Bits = Number(n);
      if (k >= 18) {
        if (!r.Has(16)) { return [s, false]; }
        s.Base = [r.Get64(), r.Get64()];
        return [s, true];
      }
      const [raw, bok] = r.Leb();
      if (!bok) { return [s, false]; }
      let base = raw;
      if (k >= 2 && k <= 5) { base = BigInt.asUintN(64, (raw >> 1n) ^ (-(raw & 1n))); }
      s.Base = [base, 0n];
      return [s, true];
    }
    if (s.Packing === 2 && k === 10) {
      if (!r.Has(12)) { return [s, false]; }
      const lo = TableMessageFloat32FromBits(r.Get32());
      const hi = TableMessageFloat32FromBits(r.Get32());
      const res = TableMessageFloat32FromBits(r.Get32());
      if (!(lo < hi) || !(res > 0)) { return [s, false]; }
      const delta = Math.fround(hi - lo);
      let values = Math.fround(delta / res);
      if (delta - delta !== 0 || values - values !== 0) { return [s, false]; }
      if (!(values >= 1)) { values = 1; } else if (values > 4294967040.0) { values = 4294967040.0; }
      s.QMin = lo;
      s.QDelta = delta;
      s.QCount = Math.ceil(values) >>> 0;
      s.Bits = TableMessageBits(BigInt(s.QCount));
      return [s, true];
    }
    return [s, false];
  }
  if (k === 12 || k === 33) {
    const [n, ok] = r.Leb();
    if (!ok || n > 2147483647n) { return [s, false]; }
    s.Max = Number(n);
  } else if (k === 14 || k === 16) {
    if (k === 14) {
      const [n, ok] = r.Leb();
      if (!ok || n > 4294967295n) { return [s, false]; }
      s.Min = Number(n);
    }
    const [n, ok] = r.Leb();
    if (!ok || n > 4294967295n || Number(n) < s.Min) { return [s, false]; }
    s.Max = Number(n);
  }
  return [s, true];
}
function TableMessageEntryRead(r) {
  const e = { Id: 0n, Shape: TableMessageShape(), Element: TableMessageShape() };
  if (!r.Has(9)) { return [e, false]; }
  e.Id = r.Get64();
  const kind = r.Get8();
  if (!TableMessageKnownKind(kind)) { return [e, false]; }
  const [shape, ok] = TableMessageShapeRead(r, kind);
  e.Shape = shape;
  if (!ok) { return [e, false]; }
  if (kind === 14 || kind === 16) {
    if (!r.Has(1)) { return [e, false]; }
    const elem = r.Get8();
    if (!TableMessageKnownKind(elem) || elem === 12 || elem === 33) { return [e, false]; }
    const [es, eok] = TableMessageShapeRead(r, elem);
    e.Element = es;
    if (!eok) { return [e, false]; }
    if (elem === 14 || elem === 16) {
      if (!r.Has(1)) { return [e, false]; }
      const inner = r.Get8();
      if (!TableMessageKnownKind(inner) || inner === 12 || inner === 33) { return [e, false]; }
    }
  }
  return [e, true];
}
function TableMessageShapeEqual(words, a, b) {
  const len = a.Max - a.Min;
  if (len !== b.Max - b.Min) { return false; }
  for (let i = 0; i < len; i++) {
    if (words[a.Min + i] !== words[b.Min + i]) { return false; }
  }
  return true;
}
function TableMessageEntryEqual(words, a, b) {
  return a !== null && b !== null && TableMessageShapeEqual(words, a.Shape, b.Shape);
}
export function TableMessageRefuse(report, reason) {
  report.Verdict = TableMessageOpenRefused;
  report.Reason = reason;
  return false;
}
// TableAnnounceRead is §3.3's reader: "the receiver resolves once. It reads the
// announcement, resolves every entry against its own descriptors ... THE
// ANNOUNCEMENT READS TOLERANTLY ... The vocabulary is a pure function of the
// build version." A refused announcement is TERMINAL.
function TableAnnounceRead(v, data, report) {
  const [r, verdict] = TableMessageOpenFramed(data);
  if (verdict !== TableMessageOpenOk) {
    if (verdict === TableMessageOpenRefused) {
      let reason = "newer_form";
      if (data.length > 0 && data[0] === 2) { reason = "message_form_as_file"; }
      return TableMessageRefuse(report, reason);
    }
    report.Malformed = true;
    return false;
  }
  let versionCount = 0, wordsCount = 0, words = null;
  for (;;) {
    const [ref, ok] = r.Leb();
    if (!ok) { report.Malformed = true; return false; }
    if (ref === 0n) { break; }
    const [id, rok] = r.Resolve(ref);
    if (!rok || !r.Has(1)) { report.Malformed = true; return false; }
    const kind = r.Get8();
    if (id === TableMessageVersionId) {
      if (kind !== 9 || !r.Has(8)) { report.Malformed = true; return false; }
      v.BuildVersion = r.Get64();
      versionCount++;
    } else if (id === TableMessageVocabularyId) {
      if (kind !== 14) { report.Malformed = true; return false; }
      const [sub, bok] = r.Body();
      if (!bok || !sub.Has(1) || sub.Get8() !== 6) { report.Malformed = true; return false; }
      const [n, nok] = sub.Leb();
      if (!nok || n > BigInt(sub.Buffer.length) || n !== BigInt(sub.Buffer.length - sub.Offset)) { report.Malformed = true; return false; }
      let limit = v.MaxBytes;
      if (!limit) { limit = 64 * 1024; }
      if (n > BigInt(limit)) { return TableMessageRefuse(report, "vocabulary_too_large"); }
      words = sub.Buffer.subarray(sub.Offset);
      wordsCount++;
    } else {
      report.Unknown++;
      if (!r.Skip(kind)) { report.Malformed = true; return false; }
    }
  }
  if (versionCount !== 1 || wordsCount !== 1) { report.Malformed = true; return false; }
  if (v.Strict && v.BuildVersion !== v.BuildVersionOwn) {
    report.BuildVersionPeer = v.BuildVersion;
    report.BuildVersionOwn = v.BuildVersionOwn;
    return TableMessageRefuse(report, "build_version_mismatch");
  }
  const src = new TableMessageReader(words, null);
  let count = 0, nodeSeen = false;
  while (src.Offset < words.length) {
    if (count >= v.Entries.length) { return TableMessageRefuse(report, "vocabulary_too_large"); }
    const began = src.Offset;
    const [e, eok] = TableMessageEntryRead(src);
    if (!eok || e.Id === TableMessageVocabularyId || e.Id === TableMessageVersionId || (e.Id === TableMessageNodeId && nodeSeen)) {
      report.Malformed = true;
      return false;
    }
    if (e.Id === TableMessageNodeId) { nodeSeen = true; }
    e.Shape.Min = began;
    e.Shape.Max = src.Offset;
    for (let i = 0; i < count; i++) {
      if (TableMessageEntryEqual(words, v.Entries[i], e)) { report.Malformed = true; return false; }
    }
    v.Entries[count] = e;
    count++;
  }
  src.Offset = 0;
  for (let i = 0; i < count; i++) {
    const [e] = TableMessageEntryRead(src);
    v.Entries[i] = e;
  }
  v.Count = count;
  v.RefBits = TableMessageBits(BigInt(count));
  if (v.RefBits === 0) { v.RefBits = 1; }
  v.Announced = true;
  return true;
}
export function AnnounceRead(v, data, report) {
  if (report === undefined || report === null) { report = {}; }
  if (v.Announced || v.Refused) {
    report.Verdict = TableMessageOpenRefused;
    report.Reason = "second_announcement";
    return false;
  }
  const ok = TableAnnounceRead(v, data, report);
  if (!ok) { v.Refused = true; }
  return ok;
}
`
