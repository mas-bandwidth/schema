package darttable

// THE MESSAGE FORM'S BATCH ON THE DART LEG (docs/SPEC-TABLES.md §3.3).
//
// "A BATCH OF BODIES IS THE UNIT THIS FORM READS AND WRITES, and a single
// message is the batch of one" (§3.3): `SaveMessages` takes an array of bodies
// of ONE root and writes ONE buffer, `LoadMessages` reads one buffer into the
// caller's storage and allocates nothing, and `MeasureMessages` sizes the
// buffer. The announcement (messageform_announce.go) landed on this leg
// already; this file owes the batch surface itself, over the vocabulary the
// announcement card emitted.
//
// The bytes are the C++ reference's, pinned by testdata/conformance/tables/
// MANIFEST.txt's `message` rows. The bit codec and the refusals follow the Go
// leg (internal/codegen/gotable/message.go, messageread.go, messagewrite.go)
// and the C# leg (internal/codegen/cstable/message.go), spelled in this
// language's naming convention (docs/SPEC-TABLES.md §6.1): `<root>MeasureMessages`,
// `<root>SaveMessages` and `<root>LoadMessages` beside `<root>SaveMessageBody`
// and `<root>LoadMessageBody`.

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// messageSlot answers the 1-based vocabulary slot an entry rides under, the
// writer's half of the reference a body spends on a field (§3.3).
func (g *fixedModule) messageSlot(e ir.TableVocabularyEntry) int {
	for i, v := range ir.TableVocabulary(g.unit) {
		if e.Key() == v.Key() {
			return i + 1
		}
	}
	panic("message entry outside vocabulary")
}

// messageNameSlot answers the slot of a NAME entry — a kind-0 entry under an
// enum variant's or a reserved id's wire name (§3.3: a variant rides by NAME).
func (g *fixedModule) messageNameSlot(id uint64) int {
	for i, v := range ir.TableVocabulary(g.unit) {
		if v.Id == id && v.Kind == 0 {
			return i + 1
		}
	}
	return 0
}

// emitMessageBatchRuntime writes the shared bit codec and the batch's framing,
// once, in the unit's runtime home.
func (g *fixedModule) emitMessageBatchRuntime() {
	g.pf("%s", dartMessageBatchRuntime)
	g.pf("const int tableMessageRefBits = %d;\n", ir.TableMessageRefBits(len(ir.TableVocabulary(g.unit))))
	g.pf("const int tableMessageBatchMax = 256;\n\n")
}

// emitMessageBody emits <T>SaveMessageBody and <T>LoadMessageBody for one
// struct of the closure, in the home beside the fixed write/decode bodies.
func (g *fixedModule) emitMessageBody(st *ir.Struct) {
	if st.IsMapEntry() {
		return
	}
	// A `type`'s class and its in-place reset live in the packet emitter's own
	// library, imported rather than emitted twice.
	g.needClass(st.Name, "init"+capitalize(st.Name))
	g.emitSaveMessageBody(st)
	g.emitLoadMessageBody(st)
}

// emitMessageBatch emits <T>MeasureMessages, <T>SaveMessages and
// <T>LoadMessages for one fixed root (docs/SPEC-TABLES.md §3.3: the form is a
// fixed table's, so only a `fixed table` root gets the surface).
func (g *fixedModule) emitMessageBatch(st *ir.Struct) {
	if !st.IsTable || !st.FixedDeclared || st.IsMapEntry() {
		return
	}
	lower := lowerFirst(st.Name)
	g.needHome("TableBitWriter", "TableMessageReader", "TableVocabulary", "TableReport",
		"tableMessageBatchOpen", "tableMessageBatchClose",
		lower+"SaveMessageBody", lower+"LoadMessageBody", st.Name, "init"+capitalize(st.Name))
	g.pf("// ---- %s, THE MESSAGE FORM's BATCH (docs/SPEC-TABLES.md §3.3) ----\n", st.Name)
	g.pf("/// MeasureMessages sizes the buffer for a batch of %s bodies, or -1 with\n", st.Name)
	g.pf("/// the reason on the report (docs/SPEC-TABLES.md §3.3).\n")
	g.emitFn("int", lower+"MeasureMessages", []string{"List<" + st.Name + "> values", "TableReport report"})
	g.pf("  if (values.length < 1 || values.length > tableMessageBatchMax) {\n")
	g.pf("    report.Refuse('batch_too_large');\n    return -1;\n  }\n")
	g.pf("  final w = TableBitWriter(Uint8List(0))..measuring = true;\n")
	g.pf("  w.put(2, 8);\n  w.put(values.length - 1, 8);\n")
	g.pf("  for (final value in values) {\n")
	g.pf("    if (!%sSaveMessageBody(w, value)) return -1;\n", lower)
	g.pf("  }\n  w.align();\n")
	g.pf("  if (w.overflow) return -1;\n")
	g.pf("  return 2 + (w.bits >> 3);\n}\n\n")

	g.pf("/// SaveMessages writes one buffer for a batch of %s bodies, or -1 with\n", st.Name)
	g.pf("/// the reason on the report (docs/SPEC-TABLES.md §3.3).\n")
	g.emitFn("int", lower+"SaveMessages",
		[]string{"List<" + st.Name + "> values", "Uint8List buffer", "TableReport report"})
	g.pf("  if (values.length < 1 || values.length > tableMessageBatchMax) {\n")
	g.pf("    report.Refuse('batch_too_large');\n    return -1;\n  }\n")
	g.pf("  if (buffer.length < 2) return -1;\n")
	g.pf("  buffer[0] = 2;\n  buffer[1] = values.length - 1;\n")
	g.pf("  final w = TableBitWriter(Uint8List.sublistView(buffer, 2));\n")
	g.pf("  for (final value in values) {\n")
	g.pf("    if (!%sSaveMessageBody(w, value)) return -1;\n", lower)
	g.pf("  }\n  w.align();\n")
	g.pf("  if (w.overflow) return -1;\n")
	g.pf("  return 2 + (w.bits >> 3);\n}\n\n")

	g.pf("/// LoadMessages reads one buffer into the caller's storage, allocating\n")
	g.pf("/// nothing, and answers the bodies read (docs/SPEC-TABLES.md §3.3).\n")
	g.emitFn("int", lower+"LoadMessages",
		[]string{"List<" + st.Name + "> values", "Uint8List bytes",
			"TableVocabulary vocabulary", "TableReport report"})
	g.pf("  final r = TableMessageReader();\n")
	g.pf("  final count = tableMessageBatchOpen(vocabulary, bytes, report, r);\n")
	g.pf("  if (count < 0) return -1;\n")
	g.pf("  if (count > values.length) {\n")
	g.pf("    report.Refuse('batch_too_large');\n    return count;\n  }\n")
	g.pf("  for (var i = 0; i < count; i++) {\n")
	g.pf("    if (!%sLoadMessageBody(r, values[i])) return i;\n", lower)
	g.pf("  }\n")
	g.pf("  if (!tableMessageBatchClose(r)) return count;\n")
	g.pf("  return count;\n}\n\n")
}

// emitSaveMessageBody writes the bitpacked body of one table: each riding
// field's reference and payload, then the zero reference that ends the body
// (docs/SPEC-TABLES.md §3.3).
func (g *fixedModule) emitSaveMessageBody(st *ir.Struct) {
	lower := lowerFirst(st.Name)
	g.emitFn("bool", lower+"SaveMessageBody", []string{"TableBitWriter w", st.Name + " value"})
	for _, f := range st.Fields {
		entry := ir.TableFieldEntry(f)
		slot := g.messageSlot(entry)
		rides := g.messageRides(f, "value."+dartName(f.Name))
		g.pf("  if (%s) {\n", rides)
		g.pf("    w.put(%d, tableMessageRefBits);\n", slot)
		g.emitMessageWriteValue(f, entry, "value."+dartName(f.Name), "    ")
		g.pf("  }\n")
	}
	g.pf("  w.put(0, tableMessageRefBits);\n  return !w.overflow;\n}\n\n")
}

// emitLoadMessageBody writes the bitpacked read of one table: the reset, then
// each reference dispatched to its field, the zero reference ending the body.
func (g *fixedModule) emitLoadMessageBody(st *ir.Struct) {
	lower := lowerFirst(st.Name)
	g.emitFn("bool", lower+"LoadMessageBody", []string{"TableMessageReader r", st.Name + " value"})
	g.pf("  init%s(value);\n", capitalize(st.Name))
	g.pf("  while (true) {\n")
	g.pf("    final ref = r.bits.get(tableMessageRefBits);\n")
	g.pf("    if (r.bits.failed) { r.report.Damage(); return false; }\n")
	g.pf("    if (ref == 0) return true;\n")
	g.pf("    switch (ref) {\n")
	for _, f := range st.Fields {
		entry := ir.TableFieldEntry(f)
		slot := g.messageSlot(entry)
		g.pf("      case %d:\n", slot)
		g.emitMessageReadValue(f, entry, "value."+dartName(f.Name), "        ")
		g.pf("        break;\n")
	}
	g.pf("      default:\n")
	g.pf("        r.report.unknown++;\n")
	g.pf("        r.report.Damage();\n        return false;\n")
	g.pf("    }\n  }\n}\n\n")
}

// messageRides answers the Dart condition under which one field rides the wire
// (§3): a field at its declared default, an absent optional, an empty
// string/bytes/wstring and an empty counted array all elide.
func (g *fixedModule) messageRides(f *ir.Field, expr string) string {
	if f.IsList() || f.IsMap() {
		return expr + ".length > 0"
	}
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		return expr + " != 0"
	}
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
		if len(f.DefBytes) == 0 {
			return expr + "Length > 0"
		}
		return fmt.Sprintf("%sLength != %d", expr, len(f.DefBytes))
	}
	if f.Type.Kind == ir.TWString {
		return expr + "Length > 0"
	}
	if f.Array == ir.ArrayCounted {
		return expr + "Count > 0"
	}
	if f.Array == ir.ArrayFixed {
		if f.Type.Kind == ir.TNamed {
			if _, ok := f.Type.Ref.(*ir.Struct); ok {
				return "true"
			}
		}
		return "true"
	}
	if f.Type.Kind == ir.TNamed {
		if _, ok := f.Type.Ref.(*ir.Union); ok {
			return expr + ".type != 0"
		}
	}
	return expr + " != " + g.gen.fixedZeroFor(f)
}

// emitMessageWriteValue writes one field's payload after its reference.
func (g *fixedModule) emitMessageWriteValue(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	kind, shape := entry.Kind, entry.Shape
	switch {
	case f.Type.Kind == ir.TString || (f.Type.Kind == ir.TBytes && !f.Type.Pointer):
		g.pf("%sw.put(%sLength, %d);\n", ind, expr, ir.TableMessageCountBits(shape))
		g.pf("%sw.align();\n", ind)
		g.pf("%sw.raw(Uint8List.sublistView(%s, 0, %sLength));\n", ind, expr, expr)
	case f.Type.Kind == ir.TWString:
		g.pf("%sw.put(%sLength, %d);\n", ind, expr, ir.TableMessageCountBits(shape))
		g.pf("%sfor (var i = 0; i < %sLength; i++) { w.put(%s[i], 16); }\n", ind, expr, expr)
	case f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted:
		count := fmt.Sprintf("%d", f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			count = expr + "Count"
		}
		if ir.TableMessageCountBits(shape) > 0 {
			g.pf("%sw.put(%s - %d, %d);\n", ind, count, shape.Min, ir.TableMessageCountBits(shape))
		}
		if ir.TableMessageAligns(kind, shape) {
			g.pf("%sw.align();\n", ind)
		}
		elem := messageElementField(f)
		elemEntry := ir.TableVocabularyEntry{Kind: shape.Elem}
		if shape.Inner != nil {
			elemEntry.Shape = *shape.Inner
		}
		g.pf("%sfor (var i = 0; i < %s; i++) {\n", ind, count)
		g.emitMessageWriteElement(elem, elemEntry, expr+"[i]", ind+"  ")
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%sif (!%sSaveMessageBody(w, %s)) return false;\n", ind, lowerFirst(r.Name), expr)
		case *ir.Union:
			g.emitMessageWriteUnion(r, expr, ind)
		case *ir.Enum:
			g.emitMessageWriteEnum(r, expr, ind)
		}
	default:
		g.emitMessageWriteScalar(f, entry, expr, ind)
	}
}

func (g *fixedModule) emitMessageWriteElement(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	switch {
	case f.Type.Kind == ir.TNamed:
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			g.pf("%sif (!%sSaveMessageBody(w, %s)) return false;\n", ind, lowerFirst(r.Name), expr)
			return
		}
	}
	g.emitMessageWriteValue(f, entry, expr, ind)
}

func (g *fixedModule) emitMessageWriteScalar(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	kind, shape := entry.Kind, entry.Shape
	if kind == ir.TableKindBool {
		g.pf("%sw.put(%s ? 1 : 0, 1);\n", ind, expr)
		return
	}
	bits := ir.TableMessageValueBits(kind, shape)
	if shape.Packing == ir.TableMessageRanged {
		base := shape.Base
		baseLit := "0"
		if base != nil {
			baseLit = fixedIntLit(base, 8)
		}
		g.pf("%sw.put(%s - %s, %d);\n", ind, expr, baseLit, bits)
		return
	}
	g.pf("%sw.put(%s, %d);\n", ind, expr, bits)
}

func (g *fixedModule) emitMessageWriteEnum(e *ir.Enum, expr, ind string) {
	g.pf("%sif (%s == 0) {\n", ind, expr)
	g.pf("%s  w.put(0, tableMessageRefBits);\n%s} else {\n", ind, ind)
	g.pf("%s  switch (%s) {\n", ind, expr)
	for i := range e.Variants {
		slot := g.messageNameSlot(ir.TableWireId(e.VariantWireName(i)))
		g.pf("%s    case %d: w.put(%d, tableMessageRefBits); break;\n", ind, i+1, slot)
	}
	g.pf("%s    default: w.put(0, tableMessageRefBits);\n", ind)
	g.pf("%s  }\n%s}\n", ind, ind)
}

func (g *fixedModule) emitMessageWriteUnion(un *ir.Union, expr, ind string) {
	g.pf("%sswitch (%s.type) {\n", ind, expr)
	g.pf("%s  case 0: w.put(0, tableMessageRefBits); break;\n", ind)
	for i, v := range un.Variants {
		slot := g.messageSlot(ir.TableArmEntry(v))
		g.pf("%s  case %d:\n", ind, i+1)
		g.pf("%s    w.put(%d, tableMessageRefBits);\n", ind, slot)
		if !v.Void() {
			g.emitMessageWriteValue(v.F, ir.TableArmEntry(v), expr+"."+dartName(v.Name), ind+"    ")
		}
		g.pf("%s    break;\n", ind)
	}
	g.pf("%s  default: w.overflow = true;\n%s}\n", ind, ind)
}

// emitMessageReadValue reads one field's payload after its reference.
func (g *fixedModule) emitMessageReadValue(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	kind, shape := entry.Kind, entry.Shape
	bad := "r.report.Damage(); return false"
	switch {
	case f.Type.Kind == ir.TString || (f.Type.Kind == ir.TBytes && !f.Type.Pointer):
		g.pf("%s{\n", ind)
		g.pf("%s  final n = r.bits.get(%d);\n", ind, ir.TableMessageCountBits(shape))
		g.pf("%s  if (r.bits.failed || !r.bits.align()) { %s; }\n", ind, bad)
		g.pf("%s  final start = r.bits.offset >> 3;\n", ind)
		g.pf("%s  if (!r.bits.skip(n * 8)) { %s; }\n", ind, bad)
		g.pf("%s  var kept = n;\n", ind)
		g.pf("%s  if (kept > %d) { kept = %d;  }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%s  %sLength = kept;\n", ind, expr)
		g.pf("%s  %s.setRange(0, kept, r.bits.buffer, start);\n", ind, expr)
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TWString:
		g.pf("%s{\n", ind)
		g.pf("%s  final n = r.bits.get(%d);\n", ind, ir.TableMessageCountBits(shape))
		g.pf("%s  if (r.bits.failed) { %s; }\n", ind, bad)
		g.pf("%s  var kept = n;\n", ind)
		g.pf("%s  if (kept > %d) { kept = %d;  }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%s  for (var i = 0; i < n; i++) { final v = r.bits.get(16); if (i < kept) %s[i] = v; }\n", ind, expr)
		g.pf("%s  if (r.bits.failed) { %s; }\n", ind, bad)
		g.pf("%s  %sLength = kept;\n", ind, expr)
		g.pf("%s}\n", ind)
	case f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted:
		count := fmt.Sprintf("%d", f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			count = expr + "Count"
			if ir.TableMessageCountBits(shape) > 0 {
				g.pf("%s%s = r.bits.get(%d) + %d;\n", ind, count, ir.TableMessageCountBits(shape), shape.Min)
				g.pf("%sif (r.bits.failed) { %s; }\n", ind, bad)
			}
		} else if ir.TableMessageCountBits(shape) > 0 {
			g.pf("%sfinal n = r.bits.get(%d);\n", ind, ir.TableMessageCountBits(shape))
			g.pf("%sif (r.bits.failed) { %s; }\n", ind, bad)
		}
		if ir.TableMessageAligns(kind, shape) {
			g.pf("%sif (!r.bits.align()) { %s; }\n", ind, bad)
		}
		elem := messageElementField(f)
		g.pf("%sfor (var i = 0; i < %s; i++) {\n", ind, count)
		g.emitMessageReadElement(elem, entry, expr+"[i]", ind+"  ")
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%sif (!%sLoadMessageBody(r, %s)) return false;\n", ind, lowerFirst(r.Name), expr)
		case *ir.Union:
			g.emitMessageReadUnion(r, expr, ind)
		case *ir.Enum:
			g.emitMessageReadEnum(r, expr, ind)
		}
	default:
		g.emitMessageReadScalar(f, entry, expr, ind)
	}
}

func (g *fixedModule) emitMessageReadElement(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	if f.Type.Kind == ir.TNamed {
		if r, ok := f.Type.Ref.(*ir.Struct); ok {
			g.pf("%sif (!%sLoadMessageBody(r, %s)) return false;\n", ind, lowerFirst(r.Name), expr)
			return
		}
	}
	elemEntry := ir.TableVocabularyEntry{Kind: entry.Shape.Elem}
	if entry.Shape.Inner != nil {
		elemEntry.Shape = *entry.Shape.Inner
	}
	g.emitMessageReadValue(f, elemEntry, expr, ind)
}

func (g *fixedModule) emitMessageReadScalar(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	kind, shape := entry.Kind, entry.Shape
	if kind == ir.TableKindBool {
		g.pf("%s%s = r.bits.get(1) != 0;\n", ind, expr)
		g.pf("%sif (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
		return
	}
	bits := ir.TableMessageValueBits(kind, shape)
	g.pf("%s%s = r.bits.get(%d);\n", ind, expr, bits)
	g.pf("%sif (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
	if shape.Packing == ir.TableMessageRanged {
		base := shape.Base
		baseLit := "0"
		if base != nil {
			baseLit = fixedIntLit(base, 8)
		}
		g.pf("%s%s += %s;\n", ind, expr, baseLit)
	}
}

func (g *fixedModule) emitMessageReadEnum(e *ir.Enum, expr, ind string) {
	g.pf("%s{\n", ind)
	g.pf("%s  final ref = r.bits.get(tableMessageRefBits);\n", ind)
	g.pf("%s  if (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
	g.pf("%s  if (ref == 0) { %s = 0; } else {\n", ind, expr)
	g.pf("%s    switch (ref) {\n", ind)
	for i := range e.Variants {
		slot := g.messageNameSlot(ir.TableWireId(e.VariantWireName(i)))
		g.pf("%s      case %d: %s = %d; break;\n", ind, slot, expr, i+1)
	}
	g.pf("%s      default: %s = 0; r.report.unknown++; break;\n", ind, expr)
	g.pf("%s    }\n%s  }\n%s}\n", ind, ind, ind)
}

func (g *fixedModule) emitMessageReadUnion(un *ir.Union, expr, ind string) {
	g.pf("%s{\n", ind)
	g.pf("%s  final ref = r.bits.get(tableMessageRefBits);\n", ind)
	g.pf("%s  if (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
	g.pf("%s  if (ref == 0) { %s.type = 0; } else {\n", ind, expr)
	g.pf("%s    switch (ref) {\n", ind)
	for i, v := range un.Variants {
		slot := g.messageSlot(ir.TableArmEntry(v))
		g.pf("%s      case %d:\n", ind, slot)
		g.pf("%s        %s.type = %d;\n", ind, expr, i+1)
		if !v.Void() {
			g.emitMessageReadValue(v.F, ir.TableArmEntry(v), expr+"."+dartName(v.Name), ind+"        ")
		}
		g.pf("%s        break;\n", ind)
	}
	g.pf("%s      default: %s.type = 0; r.report.unknown++; break;\n", ind, expr)
	g.pf("%s    }\n%s  }\n%s}\n", ind, ind, ind)
}

// messageElementField is a field's element as a scalar field, the shape the
// array's element rides under (§3.3): the array and keyed shapes are gone.
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

const dartMessageBatchRuntime = `
// ---------------------------------------------------------------------------
// THE MESSAGE FORM'S BATCH, form byte 2 (docs/SPEC-TABLES.md §3.3)
// ---------------------------------------------------------------------------

/// The batch's bit writer: low-bit-first, the packet wire's order (§3.3).
/// Measuring walks the identical stream without touching a buffer.
class TableBitWriter {
  Uint8List buffer;
  int bits = 0;
  bool measuring = false;
  bool overflow = false;
  TableBitWriter(this.buffer);

  void put(int value, int n) {
    if (n < 0 || n > 64) { overflow = true; return; }
    if (n == 0) return;
    if (n < 64) { value &= (1 << n) - 1; }
    final at = bits;
    bits += n;
    if (measuring) return;
    if ((bits + 7) >> 3 > buffer.length) { overflow = true; return; }
    for (var i = 0; i < n; i++) {
      if (((value >> i) & 1) != 0) {
        buffer[(at + i) >> 3] |= 1 << ((at + i) & 7);
      }
    }
  }

  void align() { put(0, (-bits) & 7); }

  void raw(Uint8List bytes) {
    align();
    if (measuring) { bits += bytes.length * 8; return; }
    final at = bits >> 3;
    bits += bytes.length * 8;
    if (at + bytes.length > buffer.length) { overflow = true; return; }
    buffer.setRange(at, at + bytes.length, bytes);
  }
}

/// The batch's bit reader, the writer's mirror.
class TableBitReader {
  Uint8List buffer;
  int offset = 0;
  bool failed = false;
  TableBitReader(this.buffer);

  int get(int n) {
    if (n < 0 || n > 64 || offset < 0 || n > buffer.length * 8 - offset) {
      failed = true;
      return 0;
    }
    if (n == 0) return 0;
    var value = 0;
    for (var i = 0; i < n; i++) {
      final at = offset + i;
      if (((buffer[at >> 3] >> (at & 7)) & 1) != 0) {
        value |= 1 << i;
      }
    }
    offset += n;
    return value;
  }

  bool skip(int n) {
    if (n < 0 || n > buffer.length * 8 - offset) { failed = true; return false; }
    offset += n;
    return true;
  }

  bool align() {
    final pad = (-offset) & 7;
    return get(pad) == 0;
  }
}

/// ONE BATCH READ, the reader's cursor over the buffer and the announced
/// vocabulary (docs/SPEC-TABLES.md §3.3).
class TableMessageReader {
  TableBitReader bits = TableBitReader(Uint8List(0));
  TableVocabulary vocabulary = TableVocabulary(entries: <TableMessageEntry>[]);
  TableReport report = TableReport();
}

/// Opens one form-2 batch: the form byte first, then the count, then the
/// bodies as one continuous bit stream. Answers the body count, or -1 with the
/// refusal on the report (§3.3).
int tableMessageBatchOpen(
  TableVocabulary vocabulary,
  Uint8List data,
  TableReport report,
  TableMessageReader r,
) {
  if (data.length == 0) { report.Damage(); return -1; }
  if (data[0] != 2) { report.Refuse('newer_form'); return -1; }
  if (!vocabulary.announced) { report.Refuse('no_vocabulary'); return -1; }
  if (data.length < 2) { report.Damage(); return -1; }
  r.bits = TableBitReader(Uint8List.sublistView(data, 2));
  r.vocabulary = vocabulary;
  r.report = report;
  return data[1] + 1;
}

/// Closes a batch: the pad is verified zero and no byte may ride past it
/// (docs/SPEC-TABLES.md §3.3: "BYTES AFTER THE PAD ARE MALFORMED").
bool tableMessageBatchClose(TableMessageReader r) {
  if (!r.bits.align() || r.bits.offset != r.bits.buffer.length * 8) {
    r.report.Damage();
    return false;
  }
  return true;
}
`
