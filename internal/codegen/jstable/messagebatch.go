package jstable

// THE MESSAGE FORM'S BATCH BODY VERBS (docs/SPEC-TABLES.md §3.3): the plural
// surface `MeasureMessages`, `SaveMessages` and `LoadMessages`, which the
// announcement card left to this one. A batch is M bodies of ONE fixed root
// under the connection's vocabulary, written as one continuous bit stream.
//
// THE WRITE IS A MEASURE THEN A SAVE over the same bit writer, and the READ is
// one loop over one bit reader, both in the language's own shape (§6.1):
// storage is the fixed form's own classes (the message form and the fixed form
// are two encodings of one value), a refusal is a report field and never a
// throw, and the codec allocates nothing.
//
// The runtime is the home's; the per-root verbs are emitted beside the fixed
// form in each root module and import the shared symbols.

import (
	"fmt"
	"math/big"
	"strconv"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// messageBatchRuntime is the emitted JavaScript: the bit writer and reader, the
// scalar reconstruction and the skip walk, ported whole from the Go reference
// (gotable/message.go) and the C# leg.
func messageBatchRuntime() string { return jsBatchRuntime }

const jsBatchRuntime = `
// ---- the batch body codec (docs/SPEC-TABLES.md §3.3) ----
//
// The bit stream is the packet wire's: bit i is bit i%8 of byte i/8, low bit
// first, so a value written here and one written by the packet writer are the
// same bits in the same places (SPEC.md §4.3). Every multi-byte fact rides a
// BigInt, and every width is bounded before it is used.
export class TableBitWriter {
  constructor(buffer, measuring) {
    this.Buffer = buffer;
    this.Bits = 0;
    this.Measuring = measuring === true;
    this.Overflow = false;
  }
  Put(value, n) {
    if (n < 0 || n > 64) { this.Overflow = true; return; }
    if (n === 0) { return; }
    const at = this.Bits;
    this.Bits += n;
    if (this.Measuring) { return; }
    if (((this.Bits + 7) >> 3) > this.Buffer.length) { this.Overflow = true; return; }
    const v = BigInt.asUintN(n, value);
    let shift = at & 7;
    let byteIdx = at >> 3;
    for (let i = 0; i < n; i++) {
      if ((v & (1n << BigInt(i))) !== 0n) { this.Buffer[byteIdx] |= (1 << shift); }
      else { this.Buffer[byteIdx] &= ~(1 << shift); }
      shift++;
      if (shift === 8) { shift = 0; byteIdx++; }
    }
  }
  Align() { this.Put(0n, (-this.Bits) & 7); }
  Raw(data) {
    this.Align();
    if (this.Measuring) { this.Bits += data.length * 8; return; }
    const at = this.Bits >> 3;
    this.Bits += data.length * 8;
    if (at > this.Buffer.length - data.length) { this.Overflow = true; return; }
    this.Buffer.set(data, at);
  }
}
export class TableBitReader {
  constructor(buffer) { this.Buffer = buffer; this.Offset = 0; }
  Get(n) {
    if (n < 0 || n > 64 || this.Offset < 0 || n > this.Buffer.length * 8 - this.Offset) { return [0n, false]; }
    if (n === 0) { return [0n, true]; }
    let v = 0n;
    let at = this.Offset >> 3;
    let shift = this.Offset & 7;
    for (let i = 0; i < n; i++) {
      const bit = (this.Buffer[at] >> shift) & 1;
      if (bit !== 0) { v |= (1n << BigInt(i)); }
      shift++;
      if (shift === 8) { shift = 0; at++; }
    }
    this.Offset += n;
    return [v, true];
  }
  Skip(n) {
    if (n < 0 || n > this.Buffer.length * 8 - this.Offset) { return false; }
    this.Offset += n;
    return true;
  }
  Align() {
    const [v, ok] = this.Get((-this.Offset) & 7);
    return ok && v === 0n;
  }
}
export class TableMessageBatchReader {
  constructor(buffer, vocabulary, report) {
    this.Bits = new TableBitReader(buffer);
    this.Vocabulary = vocabulary;
    this.RefBits = vocabulary.RefBits;
    this.IndexBits = 0;
    this.Report = report;
  }
}
export function TableMessageResetReport(report) {
  report.Malformed = false;
  report.Verdict = 0;
  report.Reason = "";
  report.NonFiniteFloat = "";
  report.Unknown = 0;
  report.Clamped = 0;
  report.KindMismatch = 0;
  report.Widened = 0;
}
function tableMessageCountBits(s) {
  let n = s.Max - s.Min;
  let bits = 0;
  while (n !== 0) { bits++; n = Math.floor(n / 2); }
  return bits;
}
export function tableMessageCount(r, s) {
  const bits = tableMessageCountBits(s);
  const [raw, ok] = r.Get(bits);
  return [raw + BigInt(s.Min), ok];
}
export function tableMessageEntry(v, ref) {
  if (ref === 0n || ref > BigInt(v.Count)) { return null; }
  return v.Entries[Number(ref) - 1];
}
// THE SKIP WALK: a reader that cannot name a field still steps over it exactly,
// by the announced shape, so the position after it is known (§3.3).
export function tableMessageSkip(r, v, indexBits, e, depth) {
  if (depth > 128) { return false; }
  const s = e.Shape;
  switch (s.Kind) {
    case 0: case 32: return true;
    case 30: {
      const [ref, ok] = r.Get(v.RefBits);
      if (!ok || ref === 0n) { return ok; }
      const en = tableMessageEntry(v, ref);
      return en !== null && en.Shape.Kind === 0 && en.Id < 0xfffffffffffffffdn;
    }
    case 13: return tableMessageSkipBody(r, v, indexBits, depth + 1);
    case 17: return indexBits > 0 && r.Skip(indexBits);
    case 15: {
      const [ref, ok] = r.Get(v.RefBits);
      if (!ok || ref === 0n) { return ok; }
      const arm = tableMessageEntry(v, ref);
      return arm !== null && arm.Id < 0xfffffffffffffffdn && arm.Shape.Kind !== 0 &&
        tableMessageSkip(r, v, indexBits, arm, depth + 1);
    }
    case 12: case 33: {
      const [n, ok] = tableMessageCount(r, s);
      if (!ok) { return false; }
      const width = s.Kind === 12 ? 8 : 16;
      if (s.Kind === 12 && !r.Align()) { return false; }
      return r.Skip(Number(n) * width);
    }
    case 31: {
      if (!r.Align()) { return false; }
      const [n, ok] = r.Get(32);
      return ok && r.Skip(Number(n) * 8);
    }
    case 14: case 16: {
      const [n, ok] = tableMessageCount(r, s);
      if (!ok) { return false; }
      if (s.Kind === 14 && e.Element.Kind === 6 && !r.Align()) { return false; }
      if (e.Element.Bits >= 0 || e.Element.Kind === 0 || e.Element.Kind === 32) {
        let width = Math.max(0, e.Element.Bits);
        if (s.Kind === 16) { width += v.RefBits; }
        return r.Skip(Number(n) * width);
      }
      for (let i = 0n; i < n; i++) {
        const before = r.Offset;
        if (s.Kind === 16 && !r.Skip(v.RefBits)) { return false; }
        if (!tableMessageSkip(r, v, indexBits, { Shape: e.Element, Element: { Kind: 0, Bits: 0 } }, depth + 1) || r.Offset === before) { return false; }
      }
      return true;
    }
  }
  return s.Bits >= 0 && r.Skip(s.Bits);
}
export function tableMessageSkipBody(r, v, indexBits, depth) {
  if (depth > 128) { return false; }
  for (;;) {
    const [ref, ok] = r.Get(v.RefBits);
    if (!ok || ref === 0n) { return ok; }
    const e = tableMessageEntry(v, ref);
    if (e === null || !tableMessageSkip(r, v, indexBits, e, depth)) { return false; }
  }
}
// A scalar reconstructs at the announced storage width. A ranged value is the
// offset plus the base, in unsigned 64-bit arithmetic so a negative base wraps
// as it does in the reference; the caller then narrows to its own declared
// width and clamps.
export function tableMessageReadScalar(r, s) {
  const bits = Math.min(s.Bits, 64);
  let [lo, ok] = r.Get(bits);
  if (!ok) { return [null, false]; }
  let hi = 0n;
  if (s.Bits > 64) { [hi, ok] = r.Get(s.Bits - 64); if (!ok) { return [null, false]; } }
  if (s.Packing === 1) {
    const sum = lo + s.Base[0];
    const carry = (sum < lo) ? 1n : 0n;
    lo = sum & 0xffffffffffffffffn;
    hi = (hi + s.Base[1] + carry) & 0xffffffffffffffffn;
  } else if (s.Packing === 2) {
    let index = Number(lo & 0xffffffffn);
    if (index > s.QCount) { index = s.QCount; }
    const ratio = Math.fround(index / s.QCount);
    const scaled = Math.fround(ratio * s.QDelta);
    lo = BigInt(Math.fround(scaled + s.QMin) === 0 ? 0 : TableMessageFloat32Bits(Math.fround(scaled + s.QMin)));
  }
  return [[lo, hi], true];
}
export function tableMessageWriteScalar(w, s, lo, hi) {
  if (s.Packing === 1) {
    const borrow = (lo < s.Base[0]) ? 1n : 0n;
    lo = (lo - s.Base[0]) & 0xffffffffffffffffn;
    hi = (hi - s.Base[1] - borrow) & 0xffffffffffffffffn;
  } else if (s.Packing === 2) {
    const value = TableMessageFloat32FromBits(Number(lo & 0xffffffffn));
    let norm = (value - s.QMin) / s.QDelta;
    if (!(norm >= 0)) { norm = 0; } else if (!(norm <= 1)) { norm = 1; }
    const scaled = Math.fround(norm * s.QCount);
    let index = Math.floor(Math.fround(scaled + 0.5));
    if (index > s.QCount) { index = s.QCount; }
    lo = BigInt(index);
  }
  w.Put(lo, Math.min(s.Bits, 64));
  if (s.Bits > 64) { w.Put(hi, s.Bits - 64); }
}
export function tableMessageBatchOpen(v, data, report) {
  if (data.length === 0) { report.Malformed = true; return [null, -1]; }
  if (data[0] !== 2) { TableMessageRefuse(report, "newer_form"); return [null, -1]; }
  if (v === null || v === undefined || !v.Announced) { TableMessageRefuse(report, "no_vocabulary"); return [null, -1]; }
  if (data.length < 2) { report.Malformed = true; return [null, -1]; }
  return [new TableMessageBatchReader(data.subarray(2), v, report), data[1] + 1];
}
export function tableMessageBatchClose(r) {
  if (!r.Bits.Align() || r.Bits.Offset !== r.Bits.Buffer.length * 8) { r.Report.Malformed = true; return false; }
  return true;
}
export function TableMessageFloat32Bits(v) { TableMessageFloat[0] = v; return BigInt(TableMessageWords[0] >>> 0); }
const TableMessageFloat64 = new Float64Array(1);
const TableMessageDoubleWords = new BigUint64Array(TableMessageFloat64.buffer);
export function TableMessageFloat64Bits(v) { TableMessageFloat64[0] = v; return TableMessageDoubleWords[0]; }
export function TableMessageFloat64FromBits(bits) { TableMessageDoubleWords[0] = bits; return TableMessageFloat64[0]; }
`

// messageSlot is the 1-based slot an entry names in this unit's own
// vocabulary, a compile-time constant on the write path (§3.3).
func messageSlot(u *ir.Unit, entry ir.TableVocabularyEntry) int {
	for i, v := range ir.TableVocabulary(u) {
		if entry.Key() == v.Key() {
			return i + 1
		}
	}
	panic("message entry outside vocabulary: " + fmt.Sprintf("%016x", entry.Id))
}

// messageNameSlot is the 1-based slot of a NAME entry (kind 0) with a given id,
// the enum-variant slot lookup on the write path.
func messageNameSlot(u *ir.Unit, id uint64) int {
	for i, v := range ir.TableVocabulary(u) {
		if v.Id == id && v.Kind == 0 {
			return i + 1
		}
	}
	return 0
}

func isStructRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	_, ok := t.Ref.(*ir.Struct)
	return ok
}

func isUnionRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	_, ok := t.Ref.(*ir.Union)
	return ok
}

func enumRefOf(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

// messageElementField synthesizes one array element's field: the array and
// keyed framing is gone, and a bytes element becomes a bare u8 (§3.3).
func messageElementField(f *ir.Field) *ir.Field {
	e := *f
	e.Array = ir.ArrayNone
	e.KeyEnum = ""
	e.KeyEnumRef = nil
	e.Type.Optional = false
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		e.Type = ir.FieldType{Kind: ir.TInt, Width: 8, Signed: false}
	}
	return &e
}

func messageScalarBig(f *ir.Field, mem string) string {
	if fixedIsBig(f.Type) {
		return mem
	}
	return "BigInt(" + mem + ")"
}

func baseBigLit(base *big.Int) string {
	if base == nil {
		return "0n"
	}
	return base.String() + "n"
}

// emitMessageBody emits one closure member's message body verbs: the save and
// load halves of a body. Every member of the closure gets them — a root's body
// may hold a `type` by value, whose own body verbs ride beside it — and only
// the three batch verbs are gated to the fixed table roots (docs/SPEC-TABLES.md
// §3.3; gotable/gotable.go emits the body verbs for every closure member and
// gates only the batch verbs on FixedDeclared).
func (g *fixedModule) emitMessageBody(st *ir.Struct) {
	g.needHome("TableMessageRefBitsHere", "tableMessageEntry",
		"tableMessageCount", "tableMessageSkip", "tableMessageSkipBody",
		"tableMessageReadScalar", "tableMessageWriteScalar",
		"TableMessageFloat32Bits", "TableMessageFloat32FromBits",
		"TableMessageFloat64Bits", "TableMessageFloat64FromBits")

	g.pf("// ---- %s, THE MESSAGE FORM'S BODY (docs/SPEC-TABLES.md §3.3) ----\n", st.Name)
	g.pf("//\n")
	g.pf("// A body is a sequence of references and payloads ending at its own zero\n")
	g.pf("// reference. The write measures then saves over one bit writer; the read\n")
	g.pf("// fills the caller's storage.\n\n")

	g.emitMessageSaveBody(st)
	g.emitMessageLoadBody(st)
}

// emitMessageBatch emits one fixed table root's three batch verbs: a batch is
// a root's, so the plural surface is gated to the roots alone (docs/SPEC-TABLES.md
// §3.3), while the body verbs above ride every closure member.
func (g *fixedModule) emitMessageBatch(st *ir.Struct) {
	g.needHome("TableMessageBatchMax", "TableBitWriter",
		"TableMessageResetReport", "TableMessageRefuse",
		"tableMessageBatchOpen", "tableMessageBatchClose",
		st.Name+"SaveMessageBody", st.Name+"LoadMessageBody")

	g.emitMessageVerbs(st)
}

func (g *fixedModule) emitMessageSaveBody(st *ir.Struct) {
	g.pf("export function %sSaveMessageBody(w, value) {\n", st.Name)
	for _, f := range st.Fields {
		g.emitMessageWriteField(f, "  ")
	}
	g.pf("  w.Put(0n, TableMessageRefBitsHere);\n  return !w.Overflow;\n}\n\n")
}

// messageRides answers the elision condition: a field at its declared default
// is not written, an empty string/array is not written, and presence rather
// than content decides `?T` (§3.3, §4).
func (g *fixedModule) messageRides(f *ir.Field, mem, ind string) string {
	if f.Type.Optional {
		return mem + "Present"
	}
	// THE KEYED CHECK COMES FIRST (§3.2): an enum-keyed array has Array ==
	// ir.ArrayFixed and KeyEnum != "" (check.go:1621-1622), so the fixed-array
	// arm would swallow it and answer a bound where the wire wants the number
	// of present slots. The reference asks KeyEnum first (gotable/messagewrite.go).
	if f.KeyEnum != "" {
		g.emitMessageKeyedCount(f, mem, ind)
		return ir.GoExportName(f.Name) + "Pairs > 0"
	}
	switch f.Type.Kind {
	case ir.TString, ir.TBytes:
		if len(f.DefBytes) == 0 {
			return mem + "Length > 0"
		}
		return fmt.Sprintf("%sLength !== %d", mem, len(f.DefBytes))
	case ir.TWString:
		return mem + "Length > 0"
	}
	if f.Array == ir.ArrayCounted {
		return mem + "Count > 0"
	}
	if f.Array == ir.ArrayFixed {
		if isStructRef(f.Type) && !f.Type.Pointer {
			return "true"
		}
		lhs := mem + "[i]"
		def := fixedElementZero(f)
		if isUnionRef(f.Type) {
			lhs += ".type"
			def = "0"
		}
		v := ir.GoExportName(f.Name) + "AllDefault"
		g.pf("%slet %s = true;\n", ind, v)
		g.pf("%sfor (let i = 0; i < %d; i++) { if (%s) { %s = false; break; } }\n", ind, f.ArrayBound, messageDiffers(f, lhs, def), v)
		return "!" + v
	}
	if isUnionRef(f.Type) {
		return mem + ".type !== 0"
	}
	return messageDiffers(f, mem, fixedZeroFor(f))
}

// messageDiffers is the elision compare of one value against its default. A
// float compares its BIT PATTERN (docs/SPEC-TABLES.md §3.3, the 10-15 rule):
// Object.is is the bit compare for two finite values, so a -0 under a +0
// default rides, and a NaN never elides and reaches the writer's refusal.
func messageDiffers(f *ir.Field, lhs, def string) string {
	if fixedIsFloat(f.Type) {
		return "!Object.is(" + lhs + ", " + def + ")"
	}
	return lhs + " !== " + def
}

func (g *fixedModule) emitMessageKeyedCount(f *ir.Field, mem, ind string) {
	e := messageElementField(f)
	v := ir.GoExportName(f.Name) + "Pairs"
	g.pf("%slet %s = 0;\n", ind, v)
	g.pf("%sfor (let i = 0; i < %d; i++) {\n", ind, f.KeyEnumRef.Max)
	cond := g.messageRides(e, mem+"[i]", ind+"  ")
	g.pf("%s  if (%s) { %s++; }\n", ind, cond, v)
	g.pf("%s}\n", ind)
}

func (g *fixedModule) emitMessageWriteField(f *ir.Field, ind string) {
	name := js.MemberName(f.Name)
	mem := "value." + name
	entry := ir.TableFieldEntry(f)
	slot := messageSlot(g.unit, entry)
	ride := g.messageRides(f, mem, ind)
	if ride == "true" {
		// a field that always rides is a plain block: `if (true)` is a
		// constant condition (eslint no-constant-condition)
		g.pf("%s{\n", ind)
	} else {
		g.pf("%sif (%s) {\n", ind, ride)
	}
	g.pf("%s  w.Put(%dn, TableMessageRefBitsHere);\n", ind, slot)
	g.emitMessageWriteValue(f, entry, mem, ind+"  ")
	g.pf("%s}\n", ind)
}

func (g *fixedModule) emitMessageWriteValue(f *ir.Field, entry ir.TableVocabularyEntry, mem, ind string) {
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("%sw.Put(BigInt(%sLength), %d);\n", ind, mem, ir.TableMessageCountBits(entry.Shape))
		g.pf("%sw.Raw(%s.subarray(0, %sLength));\n", ind, mem, mem)
	case f.Type.Kind == ir.TWString:
		g.pf("%sw.Put(BigInt(%sLength), %d);\n", ind, mem, ir.TableMessageCountBits(entry.Shape))
		g.pf("%sfor (let i = 0; i < %sLength; i++) { w.Put(BigInt(%s[i]), 16); }\n", ind, mem, mem)
	case f.KeyEnum != "" || f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		g.emitMessageWriteArray(f, entry, mem, ind)
	case isStructRef(f.Type):
		g.pf("%sif (!%sSaveMessageBody(w, %s)) { return false; }\n", ind, f.Type.Name, mem)
	case isUnionRef(f.Type):
		g.emitMessageWriteUnion(f, mem, ind)
	case enumRefOf(f) != nil:
		g.emitMessageWriteEnum(f, mem, ind)
	default:
		g.emitMessageWriteScalar(f, entry, mem, ind)
	}
}

func (g *fixedModule) emitMessageWriteArray(f *ir.Field, entry ir.TableVocabularyEntry, mem, ind string) {
	countBits := ir.TableMessageCountBits(entry.Shape)
	keyed := f.KeyEnum != ""
	var countExpr, loopExpr string
	switch {
	case f.Type.Kind == ir.TBytes:
		countExpr = mem + "Length"
		loopExpr = mem + "Length"
	case keyed:
		// A KEYED BODY WRITES THE NUMBER OF PRESENT SLOTS AS ITS COUNT, then a
		// key reference before each present element (§3.2): "the body carries
		// `(variant id, element)` pairs". The reader consumes one key reference
		// per element (emitMessageReadArray), so the writer must write one.
		g.emitMessageKeyedCount(f, mem, ind)
		countExpr = ir.GoExportName(f.Name) + "Pairs"
		loopExpr = strconv.FormatInt(f.ArrayBound, 10)
	case f.Array == ir.ArrayFixed:
		countExpr = strconv.FormatInt(f.ArrayBound, 10)
		loopExpr = strconv.FormatInt(f.ArrayBound, 10)
	default:
		countExpr = mem + "Count"
		loopExpr = mem + "Count"
	}
	g.pf("%sw.Put(BigInt(%s) - %dn, %d);\n", ind, countExpr, entry.Shape.Min, countBits)
	if ir.TableMessageAligns(entry.Kind, entry.Shape) {
		g.pf("%sw.Align();\n", ind)
	}
	e := messageElementField(f)
	elemEntry := ir.TableVocabularyEntry{Kind: entry.Shape.Elem, Shape: *entry.Shape.Inner}
	g.pf("%sfor (let i = 0; i < %s; i++) {\n", ind, loopExpr)
	if keyed {
		cond := g.messageRides(e, mem+"[i]", ind+"  ")
		g.pf("%s  if (!(%s)) { continue; }\n", ind, cond)
		g.pf("%s  switch (i) {\n", ind)
		for i := range f.KeyEnumRef.Variants {
			slot := messageNameSlot(g.unit, ir.TableWireId(f.KeyEnumRef.VariantWireName(i)))
			g.pf("%s    case %d: w.Put(%dn, TableMessageRefBitsHere); break;\n", ind, i, slot)
		}
		g.pf("%s  }\n", ind)
	}
	g.emitMessageWriteElement(e, elemEntry, mem+"[i]", ind+"  ")
	g.pf("%s}\n", ind)
}

func (g *fixedModule) emitMessageWriteElement(f *ir.Field, entry ir.TableVocabularyEntry, mem, ind string) {
	if isStructRef(f.Type) && entry.Kind == ir.TableKindTable {
		g.pf("%sif (!%sSaveMessageBody(w, %s)) { return false; }\n", ind, f.Type.Name, mem)
		return
	}
	g.emitMessageWriteValue(f, entry, mem, ind)
}

func (g *fixedModule) emitMessageWriteScalar(f *ir.Field, entry ir.TableVocabularyEntry, mem, ind string) {
	kind := int(entry.Kind)
	s := entry.Shape
	big := messageScalarBig(f, mem)
	switch kind {
	case ir.TableKindBool:
		g.pf("%sw.Put(%s ? 1n : 0n, 1);\n", ind, mem)
		return
	case ir.TableKindF32:
		// A NaN OR AN INFINITY IS REFUSED BEFORE IT IS WRITTEN OR QUANTIZED
		// (docs/SPEC-TABLES.md §3.3): no form carries one, so the save refuses
		g.pf("%sif (!Number.isFinite(Math.fround(%s))) { return false; }\n", ind, mem)
		if s.Packing == ir.TableMessageQuantized {
			count, delta, _ := ir.TableMessageQuantization(s)
			g.pf("%s{ const b = TableMessageFloat32Bits(%s); tableMessageWriteScalar(w, {Packing:2, Bits:%d, Base:[0n,0n], QMin:%s, QDelta:%s, QCount:%d}, b, 0n); }\n",
				ind, mem, s.Bits, jsFloatLit(float64(s.QMin)), jsFloatLit(float64(delta)), count)
		} else {
			g.pf("%sw.Put(TableMessageFloat32Bits(%s), 32);\n", ind, mem)
		}
		return
	case ir.TableKindF64:
		g.pf("%sif (!Number.isFinite(%s)) { return false; }\n", ind, mem)
		g.pf("%sw.Put(TableMessageFloat64Bits(%s), 64);\n", ind, mem)
		return
	}
	if s.Packing == ir.TableMessageRanged {
		g.pf("%sw.Put(%s - %s, %d);\n", ind, big, baseBigLit(s.Base), s.Bits)
		return
	}
	width := ir.TableMessageValueBits(uint8(kind), s)
	if width > 64 {
		g.pf("%s{ const v = %s; w.Put(BigInt.asUintN(64, v), 64); w.Put(BigInt.asUintN(64, v >> 64n), %d); }\n", ind, big, width-64)
		return
	}
	g.pf("%sw.Put(%s, %d);\n", ind, big, width)
}

func (g *fixedModule) emitMessageWriteEnum(f *ir.Field, mem, ind string) {
	e := enumRefOf(f)
	g.pf("%sif (%s === 0) { w.Put(0n, TableMessageRefBitsHere); } else {\n", ind, mem)
	g.pf("%s  switch (%s) {\n", ind, mem)
	for i, v := range e.Variants {
		g.pf("%s    case %d: w.Put(%dn, TableMessageRefBitsHere); break;\n", ind, i+1, messageNameSlot(g.unit, ir.TableWireId(e.VariantWireName(i))))
		_ = v
	}
	g.pf("%s    default: return false;\n%s  }\n%s}\n", ind, ind, ind)
}

func (g *fixedModule) emitMessageWriteUnion(f *ir.Field, mem, ind string) {
	un := f.Type.Ref.(*ir.Union)
	g.pf("%sswitch (%s.type) {\n", ind, mem)
	g.pf("%s  case 0: w.Put(0n, TableMessageRefBitsHere); break;\n", ind)
	for i, v := range un.Variants {
		g.pf("%s  case %d: {\n", ind, i+1)
		g.pf("%s    w.Put(%dn, TableMessageRefBitsHere);\n", ind, messageSlot(g.unit, ir.TableArmEntry(v)))
		if !v.Void() {
			arm := ir.TableArmEntry(v)
			g.emitMessageWriteValue(v.F, arm, mem+"."+js.MemberName(v.Name), ind+"    ")
		}
		g.pf("%s    break;\n%s  }\n", ind, ind)
	}
	g.pf("%s  default: return false;\n%s}\n", ind, ind)
}

func (g *fixedModule) emitMessageLoadBody(st *ir.Struct) {
	g.pf("export function %sLoadMessageBody(r, value) {\n", st.Name)
	g.pf("  for (;;) {\n")
	g.pf("    const [ref, ok] = r.Bits.Get(r.RefBits);\n")
	g.pf("    if (!ok) { r.Report.Malformed = true; return false; }\n")
	g.pf("    if (ref === 0n) { return true; }\n")
	g.pf("    const entry = tableMessageEntry(r.Vocabulary, ref);\n")
	g.pf("    if (entry === null || entry.Id >= 0xfffffffffffffffdn) { r.Report.Malformed = true; return false; }\n")
	g.pf("    switch (entry.Id) {\n")
	for _, f := range st.Fields {
		mine := ir.TableFieldEntry(f)
		g.pf("      case 0x%016xn: {\n", mine.Id)
		g.emitMessageReadField(f, mine, "        ")
		g.pf("        break;\n      }\n")
	}
	g.pf("      default: { r.Report.Unknown++; if (!tableMessageSkip(r.Bits, r.Vocabulary, r.IndexBits, entry, 0)) { r.Report.Malformed = true; return false; } }\n")
	g.pf("    }\n  }\n}\n\n")
}

func (g *fixedModule) emitMessageReadField(f *ir.Field, mine ir.TableVocabularyEntry, ind string) {
	mem := "value." + js.MemberName(f.Name)
	g.pf("%sif (entry.Shape.Kind !== %d) { r.Report.KindMismatch++; if (!tableMessageSkip(r.Bits, r.Vocabulary, r.IndexBits, entry, 0)) { r.Report.Malformed = true; return false; } break; }\n", ind, mine.Kind)
	g.emitMessageReadValue(f, mem, ind, "entry.Shape")
	if f.Type.Optional {
		g.pf("%svalue.%sPresent = true;\n", ind, js.MemberName(f.Name))
	}
}

// shape is the JS expression naming the SHAPE this value rides at: a top-level
// field reads at `entry.Shape`, an array element at `entry.Element` and a union
// arm at `arm.Shape`, so a scalar element is read at its own width and never at
// the array's count width (§3.2).
func (g *fixedModule) emitMessageReadValue(f *ir.Field, mem, ind, shape string) {
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("%s{ const [n, ok] = tableMessageCount(r.Bits, %s); if (!ok || !r.Bits.Align()) { r.Report.Malformed = true; return false; }\n", ind, shape)
		g.pf("%s  const start = r.Bits.Offset >> 3;\n", ind)
		g.pf("%s  if (!r.Bits.Skip(Number(n) * 8)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%s  let kept = Number(n);\n", ind)
		g.pf("%s  if (kept > %d) { kept = %d; r.Report.Clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%s  %s.fill(0);\n", ind, mem)
		g.pf("%s  for (let i = 0; i < kept; i++) { %s[i] = r.Bits.Buffer[start + i]; }\n", ind, mem)
		g.pf("%s  %sLength = kept;\n%s}\n", ind, mem, ind)
	case f.Type.Kind == ir.TBytes:
		g.pf("%s{ const [n, ok] = tableMessageCount(r.Bits, %s); if (!ok || !r.Bits.Align()) { r.Report.Malformed = true; return false; }\n", ind, shape)
		g.pf("%s  const start = r.Bits.Offset >> 3;\n", ind)
		g.pf("%s  if (!r.Bits.Skip(Number(n) * 8)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%s  let kept = Number(n);\n", ind)
		g.pf("%s  if (kept > %d) { kept = %d; r.Report.Clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%s  %s.fill(0);\n", ind, mem)
		g.pf("%s  for (let i = 0; i < kept; i++) { %s[i] = r.Bits.Buffer[start + i]; }\n", ind, mem)
		g.pf("%s  %sLength = kept;\n%s}\n", ind, mem, ind)
	case f.Type.Kind == ir.TWString:
		g.pf("%s{ const [n, ok] = tableMessageCount(r.Bits, %s); if (!ok) { r.Report.Malformed = true; return false; }\n", ind, shape)
		g.pf("%s  let kept = Number(n);\n", ind)
		g.pf("%s  if (kept > %d) { kept = %d; r.Report.Clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%s  %s.fill(0);\n", ind, mem)
		g.pf("%s  for (let i = 0; i < Number(n); i++) { const [u, uok] = r.Bits.Get(16); if (!uok) { r.Report.Malformed = true; return false; } if (i < kept) { %s[i] = Number(u); } }\n", ind, mem)
		g.pf("%s  %sLength = kept;\n%s}\n", ind, mem, ind)
	case f.KeyEnum != "" || f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		g.emitMessageReadArray(f, mem, ind)
	case isStructRef(f.Type):
		g.pf("%sif (!%sLoadMessageBody(r, %s)) { return false; }\n", ind, f.Type.Name, mem)
	case isUnionRef(f.Type):
		g.emitMessageReadUnion(f, mem, ind)
	case enumRefOf(f) != nil:
		g.emitMessageReadEnum(f, mem, ind)
	default:
		g.emitMessageReadScalar(f, mem, ind, shape)
	}
}

func (g *fixedModule) emitMessageReadArray(f *ir.Field, mem, ind string) {
	bad := "r.Report.Malformed = true; return false"
	bound := f.ArrayBound
	counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes
	companion := mem + "Count"
	if f.Type.Kind == ir.TBytes {
		bound = f.Type.Size
		companion = mem + "Length"
	}
	g.pf("%s{ const [n, ok] = tableMessageCount(r.Bits, entry.Shape); if (!ok) { %s; }\n", ind, bad)
	if f.KeyEnum == "" {
		g.pf("%s  if (entry.Element.Kind === 6 && !r.Bits.Align()) { %s; }\n", ind, bad)
	}
	g.pf("%s  let keep = Number(n);\n", ind)
	g.pf("%s  if (keep > %d) { keep = %d; r.Report.Clamped++; }\n", ind, bound, bound)
	e := messageElementField(f)
	if f.KeyEnum != "" {
		g.pf("%s  for (let i = 0n; i < n; i++) {\n", ind)
		g.pf("%s    const [kref, kok] = r.Bits.Get(r.RefBits); if (!kok) { %s; }\n", ind, bad)
		g.pf("%s    const ken = tableMessageEntry(r.Vocabulary, kref);\n", ind)
		g.pf("%s    if (ken === null || ken.Shape.Kind !== 0 || ken.Id >= 0xfffffffffffffffdn) { %s; }\n", ind, bad)
		g.pf("%s    let key = 0;\n", ind)
		g.pf("%s    switch (ken.Id) {\n", ind)
		ke := f.KeyEnumRef
		for i := range ke.Variants {
			g.pf("%s      case 0x%016xn: key = %d; break;\n", ind, ir.TableWireId(ke.VariantWireName(i)), i+1)
		}
		g.pf("%s      default: r.Report.Unknown++;\n%s    }\n", ind, ind)
		g.pf("%s    if (key === 0) { if (!tableMessageSkip(r.Bits, r.Vocabulary, r.IndexBits, { Shape: entry.Element }, 0)) { %s; } }\n", ind, bad)
		g.pf("%s    else {\n", ind)
		g.emitMessageReadElement(e, mem+"[key - 1]", ind+"      ", "entry.Element")
		g.pf("%s    }\n", ind)
		g.pf("%s  }\n", ind)
		if counted {
			g.pf("%s  %s = 0;\n", ind, companion)
		}
	} else {
		g.pf("%s  for (let i = 0n; i < n; i++) {\n", ind)
		g.pf("%s    const p = i < BigInt(keep) ? %s[Number(i)] : null;\n", ind, mem)
		g.pf("%s    if (p === null) {\n", ind)
		g.pf("%s      if (entry.Element.Bits >= 0 || entry.Element.Kind === 0 || entry.Element.Kind === 32) {\n", ind)
		g.pf("%s        if (!r.Bits.Skip(Number(n - i) * Math.max(0, entry.Element.Bits))) { %s; }\n", ind, bad)
		g.pf("%s        break;\n", ind)
		g.pf("%s      }\n", ind)
		g.pf("%s      const before = r.Bits.Offset;\n", ind)
		g.pf("%s      if (!tableMessageSkip(r.Bits, r.Vocabulary, r.IndexBits, { Shape: entry.Element }, 0) || r.Bits.Offset === before) { %s; }\n", ind, bad)
		g.pf("%s    } else {\n", ind)
		g.emitMessageReadElement(e, mem+"[Number(i)]", ind+"      ", "entry.Element")
		g.pf("%s    }\n", ind)
		g.pf("%s  }\n", ind)
		if counted {
			g.pf("%s  %s = keep;\n", ind, companion)
		}
	}
	g.pf("%s}\n", ind)
}

func (g *fixedModule) emitMessageReadElement(f *ir.Field, mem, ind, shape string) {
	if isStructRef(f.Type) {
		g.pf("%sif (!%sLoadMessageBody(r, %s)) { return false; }\n", ind, f.Type.Name, mem)
		return
	}
	g.emitMessageReadValue(f, mem, ind, shape)
}

func (g *fixedModule) emitMessageReadScalar(f *ir.Field, mem, ind, shape string) {
	big := fixedIsBig(f.Type)
	signed := fixedSigned(f.Type)
	width := f.Type.Width
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%s{ const [v, ok] = r.Bits.Get(1); if (!ok) { r.Report.Malformed = true; return false; } %s = v !== 0n; }\n", ind, mem)
		return
	case ir.TFloat32, ir.TFloat64:
		// A compressed float reconstructs by the packet wire's rule, two
		// roundings each side; a raw float is its own bits.
		//
		// A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS (docs/SPEC-TABLES.md
		// §3.3), before the bits become a value and before the field is
		// written: the read names the field and the batch refuses whole as
		// non_finite_float. A quantized float reconstructs from its index and
		// is finite by construction, so only the raw shape is tested.
		refuse := fmt.Sprintf("{ if (!r.Report.NonFiniteFloat) { r.Report.NonFiniteFloat = %s; } return false; }", jsStr(jsFloatOwner(g.unit, f)))
		if f.Type.Kind == ir.TFloat32 {
			g.pf("%s{ const [v, ok] = tableMessageReadScalar(r.Bits, %s); if (!ok) { r.Report.Malformed = true; return false; } const b = Number(v[0] & 0xffffffffn);\n", ind, shape)
			g.pf("%s  if (%s.Packing !== 2 && (b & 0x7f800000) === 0x7f800000) %s\n", ind, shape, refuse)
			g.pf("%s  let x = TableMessageFloat32FromBits(b);\n", ind)
		} else {
			g.pf("%s{ const [v, ok] = tableMessageReadScalar(r.Bits, %s); if (!ok) { r.Report.Malformed = true; return false; }\n", ind, shape)
			g.pf("%s  if ((v[0] & 0x7ff0000000000000n) === 0x7ff0000000000000n) %s\n", ind, refuse)
			g.pf("%s  let x = TableMessageFloat64FromBits(v[0]);\n", ind)
		}
		g.emitMessageFloatClamp(f, "x", ind+"  ")
		g.pf("%s  %s = x;\n%s}\n", ind, mem, ind)
		return
	case ir.TBits:
		g.pf("%s{ const [v, ok] = tableMessageReadScalar(r.Bits, %s); if (!ok) { r.Report.Malformed = true; return false; } let x = v[0];\n", ind, shape)
		g.pf("%s  x = BigInt.asUintN(%d, x);\n", ind, width)
		g.pf("%s  if (x > %dn) { x = %dn; r.Report.Clamped++; }\n", ind, (uint64(1)<<uint(f.Type.Width))-1, (uint64(1)<<uint(f.Type.Width))-1)
		g.pf("%s  %s = %s;\n%s}\n", ind, mem, numOrBig("x", big), ind)
		return
	}
	g.pf("%s{ const [v, ok] = tableMessageReadScalar(r.Bits, %s); if (!ok) { r.Report.Malformed = true; return false; } let x = v[0];\n", ind, shape)
	if width == 128 {
		g.pf("%s  x = x | (v[1] << 64n);\n", ind)
	}
	g.pf("%s  x = %s;\n", ind, narrowInt("x", signed, int64(width)))
	g.emitMessageIntClamp(f, "x", ind+"  ")
	g.pf("%s  %s = %s;\n%s}\n", ind, mem, numOrBig("x", big), ind)
}

func narrowInt(x string, signed bool, width int64) string {
	if signed {
		return fmt.Sprintf("BigInt.asIntN(%d, %s)", width, x)
	}
	return fmt.Sprintf("BigInt.asUintN(%d, %s)", width, x)
}

func numOrBig(x string, big bool) string {
	if big {
		return x
	}
	return "Number(" + x + ")"
}

func (g *fixedModule) emitMessageIntClamp(f *ir.Field, x, ind string) {
	rlo, rhi, ok := ir.TableRawRange(f)
	if !ok {
		return
	}
	asBig := fixedIsBig(f.Type)
	lo, hi := jsIntLit(rlo, asBig), jsIntLit(rhi, asBig)
	g.pf("%sif (%s < %s) { %s = %s; r.Report.Clamped++; } else if (%s > %s) { %s = %s; r.Report.Clamped++; }\n", ind, x, lo, x, lo, x, hi, x, hi)
}

func (g *fixedModule) emitMessageFloatClamp(f *ir.Field, x, ind string) {
	lo, hi, ok := fixedFloatClampEnds(f)
	if !ok {
		return
	}
	g.pf("%sif (%s < %s) { %s = %s; r.Report.Clamped++; } else if (%s > %s) { %s = %s; r.Report.Clamped++; }\n", ind, x, lo, x, lo, x, hi, x, hi)
}

func (g *fixedModule) emitMessageReadEnum(f *ir.Field, mem, ind string) {
	e := enumRefOf(f)
	g.pf("%s{ const [ref, ok] = r.Bits.Get(r.RefBits); if (!ok) { r.Report.Malformed = true; return false; }\n", ind)
	g.pf("%s  if (ref === 0n) { %s = 0; }\n", ind, mem)
	g.pf("%s  else {\n", ind)
	g.pf("%s    const en = tableMessageEntry(r.Vocabulary, ref);\n", ind)
	g.pf("%s    if (en === null || en.Shape.Kind !== 0 || en.Id >= 0xfffffffffffffffdn) { r.Report.Malformed = true; return false; }\n", ind)
	g.pf("%s    switch (en.Id) {\n", ind)
	for i := range e.Variants {
		g.pf("%s      case 0x%016xn: %s = %d; break;\n", ind, ir.TableWireId(e.VariantWireName(i)), mem, i+1)
	}
	g.pf("%s      default: %s = 0; r.Report.Unknown++;\n", ind, mem)
	g.pf("%s    }\n", ind)
	g.pf("%s  }\n%s}\n", ind, ind)
}

func (g *fixedModule) emitMessageReadUnion(f *ir.Field, mem, ind string) {
	un := f.Type.Ref.(*ir.Union)
	g.pf("%s{ const [ref, ok] = r.Bits.Get(r.RefBits); if (!ok) { r.Report.Malformed = true; return false; }\n", ind)
	g.pf("%s  if (ref === 0n) { %s.type = 0; }\n", ind, mem)
	g.pf("%s  else {\n", ind)
	g.pf("%s    const arm = tableMessageEntry(r.Vocabulary, ref);\n", ind)
	g.pf("%s    if (arm === null || arm.Id >= 0xfffffffffffffffdn || arm.Shape.Kind === 0) { r.Report.Malformed = true; return false; }\n", ind)
	g.pf("%s    switch (arm.Id) {\n", ind)
	for i, v := range un.Variants {
		arm := ir.TableArmEntry(v)
		g.pf("%s      case 0x%016xn: {\n", ind, arm.Id)
		g.pf("%s        %s.type = %d;\n", ind, mem, i+1)
		if !v.Void() {
			g.emitMessageReadValue(v.F, mem+"."+js.MemberName(v.Name), ind+"        ", "arm.Shape")
		}
		g.pf("%s        break;\n%s      }\n", ind, ind)
	}
	g.pf("%s      default: { %s.type = 0; r.Report.Unknown++; if (!tableMessageSkip(r.Bits, r.Vocabulary, r.IndexBits, arm, 0)) { r.Report.Malformed = true; return false; } }\n", ind, mem)
	g.pf("%s    }\n", ind)
	g.pf("%s  }\n%s}\n", ind, ind)
}

func (g *fixedModule) emitMessageVerbs(st *ir.Struct) {
	n := st.Name
	g.pf("// THE THREE BATCH VERBS (§3.3): the primitive is a batch and a single\n")
	g.pf("// message is the batch of one. Measure sizes the buffer, Save writes one\n")
	g.pf("// buffer, Load fills the caller's storage and reports how many it read.\n")
	g.pf("export function %sMeasureMessages(values, report) {\n", n)
	g.pf("  if (report === undefined || report === null) { report = {}; }\n")
	g.pf("  TableMessageResetReport(report);\n")
	g.pf("  if (values.length < 1 || values.length > TableMessageBatchMax) { TableMessageRefuse(report, \"batch_too_large\"); return -1; }\n")
	g.pf("  const w = new TableBitWriter(null, true);\n")
	g.pf("  for (let i = 0; i < values.length; i++) {\n")
	g.pf("    if (values[i] === null || values[i] === undefined || !%sSaveMessageBody(w, values[i])) { return -1; }\n", n)
	g.pf("  }\n")
	g.pf("  w.Align();\n")
	g.pf("  if (w.Overflow) { return -1; }\n")
	g.pf("  return 2 + (w.Bits >> 3);\n}\n\n")

	g.pf("export function %sSaveMessages(values, buffer, report) {\n", n)
	g.pf("  if (report === undefined || report === null) { report = {}; }\n")
	g.pf("  TableMessageResetReport(report);\n")
	g.pf("  if (values.length < 1 || values.length > TableMessageBatchMax) { TableMessageRefuse(report, \"batch_too_large\"); return -1; }\n")
	g.pf("  if (buffer.length < 2) { return -1; }\n")
	g.pf("  buffer[0] = 2;\n")
	g.pf("  buffer[1] = values.length - 1;\n")
	g.pf("  const w = new TableBitWriter(buffer.subarray(2), false);\n")
	g.pf("  for (let i = 0; i < values.length; i++) {\n")
	g.pf("    if (values[i] === null || values[i] === undefined || !%sSaveMessageBody(w, values[i])) { return -1; }\n", n)
	g.pf("  }\n")
	g.pf("  w.Align();\n")
	g.pf("  if (w.Overflow) { return -1; }\n")
	g.pf("  return 2 + (w.Bits >> 3);\n}\n\n")

	g.pf("export function %sLoadMessages(values, vocabulary, data, report) {\n", n)
	g.pf("  if (report === undefined || report === null) { report = {}; }\n")
	g.pf("  TableMessageResetReport(report);\n")
	g.pf("  const [r, count] = tableMessageBatchOpen(vocabulary, data, report);\n")
	g.pf("  if (count < 0) { return -1; }\n")
	g.pf("  if (count > values.length) { TableMessageRefuse(report, \"batch_too_large\"); return count; }\n")
	g.pf("  for (let i = 0; i < count; i++) {\n")
	g.pf("    if (!%sLoadMessageBody(r, values[i])) {\n", n)
	g.pf("      // A NON-FINITE FLOAT IN ANY BODY REFUSES THE BATCH (docs/SPEC-TABLES.md\n")
	g.pf("      // §3.3), the tolerant read included: no body of it is delivered.\n")
	g.pf("      if (report.NonFiniteFloat) {\n")
	g.pf("        for (let j = 0; j <= i; j++) { Init%s(values[j]); }\n", n)
	g.pf("        TableMessageRefuse(report, \"non_finite_float: \" + report.NonFiniteFloat);\n")
	g.pf("        return -1;\n")
	g.pf("      }\n")
	g.pf("      // DAMAGE ANYWHERE IN THE BATCH DELIVERS NO BODY (docs/SPEC-TABLES.md §4):\n")
	g.pf("      // the count says 0 and every body the walk touched goes back to its\n")
	g.pf("      // declared defaults.\n")
	g.pf("      for (let j = 0; j <= i; j++) { Init%s(values[j]); }\n", n)
	g.pf("      return 0;\n")
	g.pf("    }\n")
	g.pf("  }\n")
	g.pf("  // damage after the last body discards the batch too (§4)\n")
	g.pf("  if (!tableMessageBatchClose(r)) { for (let j = 0; j < count; j++) { Init%s(values[j]); } return 0; }\n", n)
	g.pf("  return count;\n}\n\n")
}
