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
	"math/big"
	"strings"

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
	runtime := dartMessageBatchRuntime
	for _, entry := range ir.TableVocabulary(g.unit) {
		shape := &entry.Shape
		usesFloat32 := entry.Kind == ir.TableKindF32
		for shape != nil {
			usesFloat32 = usesFloat32 || shape.Elem == ir.TableKindF32
			shape = shape.Inner
		}
		if usesFloat32 {
			runtime = strings.Replace(runtime, "class TableBitWriter {\n", "class TableBitWriter {\n"+strings.TrimLeft(dartMessageFloat32Runtime, "\n"), 1)
			break
		}
	}
	g.pf("%s", runtime)
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
		"tableMessageBatchOpen", "tableMessageBatchClose", "tableMessageBatchMax",
		lower+"SaveMessageBody", lower+"LoadMessageBody", st.Name)
	g.pf("// ---- %s, THE MESSAGE FORM's BATCH (docs/SPEC-TABLES.md §3.3) ----\n", st.Name)
	g.pf("/// MeasureMessages sizes the buffer for a batch of %s bodies, or -1 with\n", st.Name)
	g.pf("/// the reason on the report (docs/SPEC-TABLES.md §3.3).\n")
	g.emitFn("int", lower+"MeasureMessages", []string{"List<" + st.Name + "> values", "TableReport report"})
	g.pf("  if (values.length < 1 || values.length > tableMessageBatchMax) {\n")
	g.pf("    report.Refuse('batch_too_large');\n    return -1;\n  }\n")
	g.pf("  final w = TableBitWriter(Uint8List(0))..measuring = true;\n")
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
		expr := "value." + dartName(f.Name)
		rides := g.messageRides(f, expr, "  ")
		if rides == "" {
			// A field that always rides (a struct array, position is
			// identity there) is written WITHOUT an `if (true)`: the guard
			// is not a suppression of a lint, it is no guard at all.
			g.pf("  w.put(%d, tableMessageRefBits);\n", slot)
			g.emitMessageWriteValue(f, entry, expr, "  ")
			continue
		}
		g.pf("  if (%s) {\n", rides)
		g.pf("    w.put(%d, tableMessageRefBits);\n", slot)
		g.emitMessageWriteValue(f, entry, expr, "    ")
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
	// AN ANNOUNCED ENTRY THIS ROOT DOES NOT NAME IS `unknown` AND IS STEPPED
	// PAST, continuing the body; only a reference that names no announced
	// entry, or one of the reserved ids, is damage (§3.3). That is the
	// reference's `body` walk (internal/tablewire/messagedecode.go), which
	// counts the entry and calls its `skip`.
	g.pf("      default:\n")
	g.pf("        if (ref > r.vocabulary.count) { r.report.Damage(); return false; }\n")
	g.pf("        final entry = r.vocabulary.entries[ref - 1];\n")
	g.pf("        if (r.isReserved(entry.id)) { r.report.Damage(); return false; }\n")
	g.pf("        r.report.unknown++;\n")
	g.pf("        if (!r.skipEntry(entry)) { r.report.Damage(); return false; }\n")
	g.pf("        break;\n")
	g.pf("    }\n  }\n}\n\n")
}

// messageRides answers the Dart condition under which one field rides the wire
// (§3): a field at its declared default, an absent optional, an empty
// string/bytes/wstring and an empty counted array all elide.
func (g *fixedModule) messageRides(f *ir.Field, expr, ind string) string {
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
		// A fixed array always rides here, position being its identity (§3.3,
		// "fixed arrays of tables keep their elements"). It has no guard, so
		// the writer emits the reference and payload unconditionally rather
		// than an `if (true)` the lint set would (rightly) call constant.
		return ""
	}
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			// A nested table/type elides when it is all-default (§3.3).
			// Probe the body in measuring mode exactly as the Go reference
			// does (internal/codegen/gotable/messagewrite.go): the all-default
			// body is its terminator alone, anything else costs more. Comparing
			// the record to an int was a REAL emitter bug the strict analyzer's
			// unrelated_type_equality_checks caught.
			probe := "probe" + capitalize(dartName(f.Name))
			g.pf("%sfinal %s = TableBitWriter(Uint8List(0))..measuring = true;\n", ind, probe)
			g.pf("%sif (!%sSaveMessageBody(%s, %s)) return false;\n", ind, lowerFirst(r.Name), probe, expr)
			return probe + ".bits > tableMessageRefBits"
		case *ir.Union:
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
	// A FLOAT CROSSES THE BIT LANE AS ITS IEEE-754 BITS (docs/SPEC-TABLES.md
	// §3.3, the message form's scalar payload): float32 as its 32 bits and
	// float64 as its 64, exactly the bits math.Float32bits/math.Float64bits
	// hand the Go reference (internal/codegen/gotable/messagewrite.go:146).
	// Never widened: kind 10's message width is 32 (ir.TableMessageValueBits).
	// Float32 NaNs narrow in software so the signalling bit survives; the
	// bit width remains kind 10's own. Float64 keeps its original bit view.
	if kind == ir.TableKindF32 {
		// The narrowing is AT THE ASSIGNMENT (issue 404, PORTING M3): under
		// the JIT a double crossing an out-of-line conversion call is boxed.
		g.pf("%s{\n", ind)
		g.pf("%s  final f = %s;\n", ind, expr)
		g.pf("%s  if (!f.isNaN) {\n", ind)
		g.pf("%s    TableBitWriter._float32Cell[0] = f;\n", ind)
		g.pf("%s    w.put(TableBitWriter._float32Words[0], 32);\n", ind)
		g.pf("%s  } else {\n", ind)
		g.pf("%s    TableBitWriter._float64Cell[0] = f;\n", ind)
		g.pf("%s    final b = TableBitWriter._float64Words[0];\n", ind)
		g.pf("%s    var m = (b >>> 29) & 0x7fffff;\n", ind)
		g.pf("%s    if (m == 0) {\n", ind)
		g.pf("%s      m = 0x400000;\n", ind)
		g.pf("%s    }\n", ind)
		g.pf("%s    w.put(((b >>> 63) << 31) | 0x7f800000 | m, 32);\n", ind)
		g.pf("%s  }\n", ind)
		g.pf("%s}\n", ind)
		return
	}
	if kind == ir.TableKindF64 {
		g.pf("%sw.put((ByteData(8)..setFloat64(0, %s, Endian.little)).getUint64(0, Endian.little), 64);\n", ind, expr)
		return
	}
	bits := ir.TableMessageValueBits(kind, shape)
	if ir.TableKindWidth(int(kind)) == 16 {
		// A 128-BIT VALUE CROSSES THE BIT LANE AS ITS LOW SIXTY-FOUR BITS AND
		// THEN THE REST (the Go reference's tableMessageWriteScalar lo/hi
		// pair): the ranged offset is taken at 128-bit width first, so a base
		// past int64 subtracts exactly.
		cls := g.message128Class(kind)
		value := expr
		if shape.Packing == ir.TableMessageRanged && shape.Base != nil && shape.Base.Sign() != 0 {
			value = "(" + expr + " - " + message128Lit(cls, shape.Base) + ")"
		}
		g.pf("%s{\n%s  final v = %s;\n", ind, ind, value)
		g.pf("%s  w.put(v.lo, %d);\n", ind, min(bits, 64))
		if bits > 64 {
			g.pf("%s  w.put(v.hi, %d);\n", ind, bits-64)
		}
		g.pf("%s}\n", ind)
		return
	}
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

// message128Class names the Dart class a 128-bit kind's value is stored in.
func (g *fixedModule) message128Class(kind uint8) string {
	cls := "UInt128"
	if ir.TableKindSigned(int(kind)) {
		cls = "Int128"
	}
	return cls
}

// message128Lit spells a 128-bit constant as its class's (hi, lo) pair.
func message128Lit(cls string, v *big.Int) string {
	lo, hi := fixed128Halves(v)
	return fmt.Sprintf("%s(%s, %s)", cls, fixedHexLit(hi), fixedHexLit(lo))
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
		if f.Type.Kind == ir.TString {
			g.pf("%s  if (!tableTextValid(r.bits.buffer, start, n)) { %s; }\n", ind, bad)
		}
		g.pf("%s  var kept = n;\n", ind)
		g.pf("%s  if (kept > %d) { kept = %d; r.report.clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%s  %sLength = kept;\n", ind, expr)
		g.pf("%s  %s.setRange(0, kept, r.bits.buffer, start);\n", ind, expr)
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TWString:
		g.pf("%s{\n", ind)
		g.pf("%s  final n = r.bits.get(%d);\n", ind, ir.TableMessageCountBits(shape))
		g.pf("%s  if (r.bits.failed) { %s; }\n", ind, bad)
		g.pf("%s  var kept = n;\n", ind)
		g.pf("%s  if (kept > %d) { kept = %d; r.report.clamped++; }\n", ind, f.Type.Size, f.Type.Size)
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
			g.pf("%sr.bits.get(%d);\n", ind, ir.TableMessageCountBits(shape))
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
	// A FLOAT IS READ BACK FROM ITS IEEE-754 BITS (docs/SPEC-TABLES.md §3.3,
	// the message form's scalar payload): float32 from its 32 bits and float64
	// from its 64. Float32 NaNs widen in software; a hardware conversion would
	// quiet a signalling NaN before a consumer could send it again.
	if kind == ir.TableKindF32 {
		// The widening is AT THE ASSIGNMENT (issue 404, PORTING M3); the bits
		// are a NaN exactly when their magnitude exceeds infinity's.
		g.pf("%s{\n", ind)
		g.pf("%s  final b = r.bits.get(32);\n", ind)
		g.pf("%s  if ((b & 0x7fffffff) <= 0x7f800000) {\n", ind)
		g.pf("%s    TableBitWriter._float32Words[0] = b;\n", ind)
		g.pf("%s    %s = TableBitWriter._float32Cell[0];\n", ind, expr)
		g.pf("%s  } else {\n", ind)
		g.pf("%s    final w64 = ((b >>> 31) << 63) | ((b & 0x7fffff) << 29);\n", ind)
		g.pf("%s    TableBitWriter._float64Words[0] = w64 | 0x7ff0000000000000;\n", ind)
		g.pf("%s    %s = TableBitWriter._float64Cell[0];\n", ind, expr)
		g.pf("%s  }\n", ind)
		g.pf("%s}\n", ind)
		g.pf("%sif (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
		return
	}
	if kind == ir.TableKindF64 {
		g.pf("%s%s = (ByteData(8)..setUint64(0, r.bits.get(64), Endian.little)).getFloat64(0, Endian.little);\n", ind, expr)
		g.pf("%sif (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
		return
	}
	bits := ir.TableMessageValueBits(kind, shape)
	if ir.TableKindWidth(int(kind)) == 16 {
		// THE WRITER'S MIRROR: the low sixty-four bits, then the rest, and the
		// ranged base added back at 128-bit width.
		cls := g.message128Class(kind)
		g.pf("%s{\n%s  final lo = r.bits.get(%d);\n", ind, ind, min(bits, 64))
		hi := "0"
		if bits > 64 {
			g.pf("%s  final hi = r.bits.get(%d);\n", ind, bits-64)
			hi = "hi"
		}
		g.pf("%s  if (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
		value := cls + "(" + hi + ", lo)"
		if shape.Packing == ir.TableMessageRanged && shape.Base != nil && shape.Base.Sign() != 0 {
			value += " + " + message128Lit(cls, shape.Base)
		}
		g.pf("%s  %s = %s;\n%s}\n", ind, expr, value, ind)
		return
	}
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
	// THE VALUE IS RECONSTRUCTED AT ITS STORAGE WIDTH (the reference's
	// tableMessageReadScalar writes the lane into the field's own bytes): a
	// signed int8/int16/int32 read back from its bits is that width's two's
	// complement, so -7 crosses as 0xfffffff9 and comes back as -7, never as
	// 4294967289 in a 64-bit Dart int.
	switch kind {
	case ir.TableKindI8, ir.TableKindI16, ir.TableKindI32:
		g.pf("%s%s = %s.toSigned(%d);\n", ind, expr, expr, 8*ir.TableKindWidth(int(kind)))
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

  /// isReserved reports one of the three ids the language holds back
  /// (docs/SPEC-TABLES.md §3.1, §3.3, §5), the reference's reserved.
  bool isReserved(int id) =>
      id == TableVocabulary.buildVersionWireId ||
      id == TableVocabulary.vocabularyWireId ||
      id == TableVocabulary.nodeWireId;

  /// skipEntry steps past ONE announced entry's payload using its signature
  /// alone, the reference's skip (internal/tablewire/messagedecode.go).
  bool skipEntry(TableMessageEntry entry) =>
      skipKind(entry.signature, 9, entry.kind);

  /// bitsRequired is bits_required(min, max) (§3.3): zero where the two are
  /// equal, otherwise the bit length of their difference.
  int bitsRequired(int min, int max) {
    if (max <= min) {
      return 0;
    }
    var v = max - min;
    var n = 0;
    while (v > 0) {
      n++;
      v >>= 1;
    }
    return n;
  }

  /// payloadWidth is one scalar entry's width in bits under its announced
  /// shape, or -1 where the kind is not a fixed-width scalar. It is the shape
  /// half of the reference's TableMessageValueBits over the signature's own
  /// bytes (ir/tablemessage.go).
  int payloadWidth(Uint8List b, int at, int kind) {
    if (kind == 1) {
      return 1; // bool
    }
    if (kind == 11) {
      return 64; // f64 carries no shape byte
    }
    final width = tableMessageKindWidth(kind);
    if (width == 0 || at >= b.length) {
      return -1;
    }
    final packing = b[at];
    if (packing == 0) {
      return width * 8;
    }
    if (packing == 1 && kind != 10) {
      final n = tableMessageLeb(b, at + 1, b.length);
      return n.$1 < 0 ? -1 : n.$1;
    }
    if (packing == 2 && kind == 10) {
      if (at + 13 > b.length) {
        return -1;
      }
      final view = ByteData.sublistView(b);
      final qMin = view.getFloat32(at + 1, Endian.little);
      final qMax = view.getFloat32(at + 5, Endian.little);
      final qRes = view.getFloat32(at + 9, Endian.little);
      if (!(qMin < qMax) || !(qRes > 0)) {
        return -1;
      }
      final cell = Float32List(2);
      cell[0] = qMax - qMin;
      final delta = cell[0];
      cell[0] = delta / qRes;
      var values = cell[0];
      if (values != values) {
        return -1;
      }
      if (!(values >= 1)) {
        values = 1;
      } else if (values > 4294967040.0) {
        values = 4294967040.0;
      }
      var count = values.floor();
      if (count.toDouble() != values) {
        count += 1;
      }
      var n = count;
      var bits = 0;
      while (n > 0) {
        bits++;
        n >>= 1;
      }
      return bits;
    }
    return -1;
  }

  /// skipName resolves a reference used as a VALUE: an enum's variant or a
  /// keyed array's slot key, which must name a kind-0 entry (the reference's
  /// name, §3.3).
  bool skipName() {
    final ref = bits.get(tableMessageRefBits);
    if (bits.failed) {
      return false;
    }
    if (ref == 0) {
      return true;
    }
    if (ref > vocabulary.count) {
      return false;
    }
    final entry = vocabulary.entries[ref - 1];
    return !isReserved(entry.id) && entry.kind == 0;
  }

  /// skipArm resolves a union's arm reference, which must name an entry that
  /// carries a payload (the reference's arm, §3.3), and steps the arm.
  bool skipArm() {
    final ref = bits.get(tableMessageRefBits);
    if (bits.failed) {
      return false;
    }
    if (ref == 0) {
      return true;
    }
    if (ref > vocabulary.count) {
      return false;
    }
    final entry = vocabulary.entries[ref - 1];
    if (isReserved(entry.id) || entry.kind == 0) {
      return false;
    }
    return skipEntry(entry);
  }

  /// skipBody walks a nested body it cannot name, field by field, against the
  /// same announced vocabulary the parent resolves through (the reference's
  /// skipBody).
  bool skipBody() {
    while (true) {
      final ref = bits.get(tableMessageRefBits);
      if (bits.failed) {
        return false;
      }
      if (ref == 0) {
        return true;
      }
      if (ref > vocabulary.count) {
        return false;
      }
      if (!skipEntry(vocabulary.entries[ref - 1])) {
        return false;
      }
    }
  }

  /// skipArray steps an announced array or keyed array by its shape and its
  /// element kind (§3.3), the reference's skip array arm.
  bool skipArray(Uint8List b, int at, int kind) {
    var p = at;
    var min = 0;
    if (kind == 14) {
      final lo = tableMessageLeb(b, p, b.length);
      if (lo.$1 < 0) {
        return false;
      }
      min = lo.$1;
      p = lo.$2;
    }
    final hi = tableMessageLeb(b, p, b.length);
    if (hi.$1 < 0) {
      return false;
    }
    final max = hi.$1;
    p = hi.$2;
    if (p >= b.length) {
      return false;
    }
    final elem = b[p];
    p += 1;
    final countBits = kind == 16 ? bitsRequired(0, max) : bitsRequired(min, max);
    var n = min;
    if (countBits > 0) {
      final raw = bits.get(countBits);
      if (bits.failed) {
        return false;
      }
      n = kind == 14 ? raw + min : raw;
    }
    if (kind == 14 && elem == 6) {
      bits.align();
      if (bits.failed) {
        return false;
      }
    }
    // A RUN OF FIXED-WIDTH ELEMENTS IS ARITHMETIC, as in the reference: a
    // zero-width element under a huge count must not buy a loop (§3.3).
    final run = kind == 14 &&
            elem != 13 &&
            elem != 15 &&
            elem != 30 &&
            elem != 17
        ? payloadWidth(b, p, elem)
        : -1;
    if (run >= 0) {
      if (run == 0) {
        return true;
      }
      if (n > 9223372036854775807 ~/ run) {
        return false;
      }
      return bits.skip(n * run);
    }
    for (var i = 0; i < n; i++) {
      if (kind == 16) {
        final key = bits.get(tableMessageRefBits);
        if (bits.failed) {
          return false;
        }
      }
      if (!skipKind(b, p, elem)) {
        return false;
      }
    }
    return true;
  }

  /// skipKind parses ONE announced kind's shape from its signature and steps
  /// its payload off the bit stream, the reference's skip switch.
  bool skipKind(Uint8List b, int at, int kind) {
    switch (kind) {
      case 0:
      case 32:
        return true;
      case 30:
        return skipName();
      case 15:
        return skipArm();
      case 13:
        return skipBody();
      case 17:
        // A fixed root numbers no node, so an unknown pointer index cannot be
        // stepped over and is damage (§3.1, §3.3).
        return false;
      case 12:
        final n = tableMessageLeb(b, at, b.length);
        if (n.$1 < 0) {
          return false;
        }
        final width = bitsRequired(0, n.$1);
        var len = 0;
        if (width > 0) {
          len = bits.get(width);
          if (bits.failed) {
            return false;
          }
        }
        bits.align();
        if (bits.failed) {
          return false;
        }
        return bits.skip(len * 8);
      case 33:
        final n = tableMessageLeb(b, at, b.length);
        if (n.$1 < 0) {
          return false;
        }
        final width = bitsRequired(0, n.$1);
        var len = 0;
        if (width > 0) {
          len = bits.get(width);
          if (bits.failed) {
            return false;
          }
        }
        return bits.skip(len * 16);
      case 31:
        bits.align();
        if (bits.failed) {
          return false;
        }
        final n = bits.get(32);
        if (bits.failed) {
          return false;
        }
        return bits.skip(n * 8);
      case 14:
      case 16:
        return skipArray(b, at, kind);
    }
    final width = payloadWidth(b, at, kind);
    if (width < 0) {
      return false;
    }
    if (width == 0) {
      return true;
    }
    return bits.skip(width);
  }
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

/// A string(N) payload must be well-formed UTF-8 with no zero byte among its
/// bytes (docs/SPEC-TABLES.md §3.3, §3): a payload that is not text is not
/// text at whatever length the reader would have kept. Mirrors the Go
/// reference's textValid (internal/tablewire/text.go).
bool tableTextValid(Uint8List bytes, int start, int length) {
  final end = start + length;
  if (start < 0 || end > bytes.length) return false;
  var index = start;
  while (index < end) {
    final lead = bytes[index++];
    if (lead == 0) return false;
    if (lead < 0x80) continue;
    int remaining, point;
    if ((lead & 0xE0) == 0xC0) {
      remaining = 1;
      point = lead & 0x1F;
    } else if ((lead & 0xF0) == 0xE0) {
      remaining = 2;
      point = lead & 0x0F;
    } else if ((lead & 0xF8) == 0xF0) {
      remaining = 3;
      point = lead & 0x07;
    } else {
      return false;
    }
    if (remaining > end - index) return false;
    for (var part = 0; part < remaining; part++) {
      final byte = bytes[index++];
      if ((byte & 0xC0) != 0x80) return false;
      point = (point << 6) | (byte & 0x3F);
    }
    if (remaining == 1 && point < 0x80) return false;
    if (remaining == 2 &&
        (point < 0x800 || (point >= 0xD800 && point <= 0xDFFF))) {
      return false;
    }
    if (remaining == 3 && (point < 0x10000 || point > 0x10FFFF)) {
      return false;
    }
  }
  return true;
}
`

// The views the inline float32 conversion uses (the packet emitter's software
// rule, spelled at each assignment — issue 404; a Dart library cannot reach
// another library's private views, SPEC-TABLES §3).
const dartMessageFloat32Runtime = `
  static final Float32List _float32Cell = Float32List(2);
  static final Uint32List _float32Words = _float32Cell.buffer.asUint32List();
  static final Float64List _float64Cell = _float32Cell.buffer.asFloat64List();
  static final Uint64List _float64Words = _float32Cell.buffer.asUint64List();

`
