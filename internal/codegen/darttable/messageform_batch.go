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
	usesFloat32, usesFloat64 := false, false
	for _, entry := range ir.TableVocabulary(g.unit) {
		usesFloat32 = usesFloat32 || entry.Kind == ir.TableKindF32
		usesFloat64 = usesFloat64 || entry.Kind == ir.TableKindF64
		for shape := &entry.Shape; shape != nil; shape = shape.Inner {
			usesFloat32 = usesFloat32 || shape.Elem == ir.TableKindF32
			usesFloat64 = usesFloat64 || shape.Elem == ir.TableKindF64
		}
	}
	// each width's views only where the unit carries that width: the analyzer
	// refuses an unused field
	cells := ""
	if usesFloat32 {
		cells += strings.TrimLeft(dartMessageFloat32Runtime, "\n")
	}
	if usesFloat64 {
		cells += strings.TrimLeft(dartMessageFloat64Runtime, "\n")
	}
	if cells != "" {
		runtime = strings.Replace(runtime, "class TableBitWriter {\n", "class TableBitWriter {\n"+cells, 1)
	}
	g.pf("%s", runtime)
	g.pf("const int tableMessageRefBits = %d;\n", ir.TableMessageRefBits(len(ir.TableVocabulary(g.unit))))
	g.pf("const int tableMessageBatchMax = 256;\n\n")
	// THE THREE IDS THE LANGUAGE HOLDS BACK (docs/SPEC-TABLES.md §3.1, §3.3,
	// §5) are malformed anywhere but their own transport, and the rule
	// outranks the wrong-sort rule: the Go reference's `reserved`
	// (internal/tablewire/messagedecode.go), which its `name` and `arm`
	// resolutions consult. The ids are 64-bit and the Dart int is signed, so
	// each is written as its two's-complement value.
	reserved := []uint64{ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId, ir.TableNodeWireId}
	g.pf("bool tableMessageReservedId(int id) =>\n    id == %d || id == %d || id == %d;\n\n",
		int64(reserved[0]), int64(reserved[1]), int64(reserved[2]))
}

// emitMessageBody emits <T>SaveMessageBody and <T>LoadMessageBody for one
// struct of the closure, in the home beside the fixed write/decode bodies. A
// variable table gets none: the message form is fixed tables only
// (docs/SPEC-TABLES.md §3.3), even where its fields would fit a fixed layout.
func (g *fixedModule) emitMessageBody(st *ir.Struct) {
	if st.IsMapEntry() || (st.IsTable && !st.FixedDeclared) {
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
		lower+"SaveMessageBody", lower+"LoadMessageBody", "init"+capitalize(st.Name), st.Name)
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
	g.pf("    if (!%sLoadMessageBody(r, values[i])) {\n", lower)
	// A NON-FINITE FLOAT REFUSES THE WHOLE BATCH, the tolerant read included
	// (docs/SPEC-TABLES.md §3.3): every body this call wrote goes back to its
	// defaults and the read answers no body at all.
	g.pf("      if (report.nonFiniteFloat.isEmpty) return i;\n")
	g.pf("      for (var j = 0; j <= i; j++) {\n        init%s(values[j]);\n      }\n", capitalize(st.Name))
	g.pf("      return 0;\n    }\n")
	g.pf("  }\n")
	g.pf("  if (!tableMessageBatchClose(r)) return count;\n")
	g.pf("  return count;\n}\n\n")
}

// emitSaveMessageBody writes the bitpacked body of one table: each riding
// field's reference and payload, then the zero reference that ends the body
// (docs/SPEC-TABLES.md §3.3).
func (g *fixedModule) emitSaveMessageBody(st *ir.Struct) {
	lower := lowerFirst(st.Name)
	g.gen.cur = st
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
	g.gen.cur = st
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
	// AN UNKNOWN SLOT IS TOLERANT (docs/SPEC-TABLES.md §3.3): a reference
	// that names an announced entry this root does not declare counts one
	// `unknown`, its payload is stepped over by the announcement's own kind and
	// shape, and the body keeps reading, exactly as the Go reference's body
	// walk does (internal/tablewire/messagedecode.go: `Unknown++`, `skip(entry)`,
	// `continue`). A reference ABOVE the announced vocabulary names no entry
	// and is damage, which is terminal for the batch.
	g.pf("        if (ref > r.vocabulary.count) {\n")
	g.pf("          r.report.Damage();\n          return false;\n        }\n")
	// A RESERVED ID IN A BODY IS MALFORMED (docs/SPEC-TABLES.md §3.1, §3.3),
	// and the rule outranks the unknown-slot rule: the Go reference body walk
	// (internal/tablewire/messagedecode.go body, the `case
	// ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId,
	// ir.TableNodeWireId` arm) sets `Malformed` and stops BEFORE it counts an
	// unknown, so a reference naming the node table (or either trailer id) is
	// damage here and counts nothing.
	g.pf("        final named = r.vocabulary.entries[ref - 1].id;\n")
	g.pf("        if (named == TableVocabulary.buildVersionWireId ||\n")
	g.pf("            named == TableVocabulary.vocabularyWireId ||\n")
	g.pf("            named == TableVocabulary.nodeWireId) {\n")
	g.pf("          r.report.Damage();\n          return false;\n        }\n")
	g.pf("        r.report.unknown++;\n")
	g.pf("        if (!TableMessageShape.skipEntry(r, r.vocabulary.entries[ref - 1])) {\n")
	g.pf("          r.report.Damage();\n          return false;\n        }\n")
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
	if f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64 {
		// ELISION COMPARES BITS (docs/SPEC-TABLES.md §3.3): a -0 under a +0
		// default rides, as on the variable form
		return floatRides(f, expr, g.gen.fixedZeroFor(f))
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
	if f.Type.Kind == ir.TNamed {
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
	// A NaN OR AN INFINITY IS REFUSED, never quantized, clamped or written
	// (docs/SPEC-TABLES.md §3.3, both special-values rules): the writer answers
	// false before the quantizer runs, so measure and save both return -1.
	// Every finite double narrows to float32 through the ordinary conversion.
	if kind == ir.TableKindF32 {
		// A COMPRESSED FLOAT CROSSES AS ITS STEP INDEX (docs/SPEC-TABLES.md
		// §3.3, packing 2, ir.TableMessageQuantized): the writer clamps
		// `(value - min) / delta` to [0, 1], multiplies by `count`, rounds the
		// product to float32, adds 0.5 and floors, and clamps the integer to
		// `count` — the oracle's order in float32
		// (internal/tablewire/messageencode.go, ir.TableMessageQuantize). A raw
		// f32 keeps the IEEE-754 path below.
		if shape.Packing == ir.TableMessageQuantized {
			count, delta, ok := ir.TableMessageQuantization(shape)
			if ok {
				countLit := fmt.Sprintf("%d", count)
				g.pf("%s{\n", ind)
				g.pf("%s  final f = %s;\n", ind, expr)
				g.pf("%s  if (!f.isFinite) return false;\n", ind)
				g.pf("%s  TableBitWriter._float32Cell[0] = f;\n", ind)
				g.pf("%s  final f32 = TableBitWriter._float32Cell[0];\n", ind)
				g.pf("%s  TableBitWriter._float32Cell[0] = f32 - %s;\n", ind, fixedFloatLit(float64(shape.QMin)))
				g.pf("%s  final shifted = TableBitWriter._float32Cell[0];\n", ind)
				g.pf("%s  TableBitWriter._float32Cell[0] = shifted / %s;\n", ind, fixedFloatLit(float64(delta)))
				g.pf("%s  var ratio = TableBitWriter._float32Cell[0];\n", ind)
				g.pf("%s  if (!(ratio >= 0)) { ratio = 0.0; } else if (!(ratio <= 1)) { ratio = 1.0; }\n", ind)
				g.pf("%s  TableBitWriter._float32Cell[0] = %s.0;\n", ind, countLit)
				g.pf("%s  final fc = TableBitWriter._float32Cell[0];\n", ind)
				g.pf("%s  TableBitWriter._float32Cell[0] = ratio * fc;\n", ind)
				g.pf("%s  final scaled = TableBitWriter._float32Cell[0];\n", ind)
				g.pf("%s  TableBitWriter._float32Cell[0] = scaled + 0.5;\n", ind)
				g.pf("%s  var index = TableBitWriter._float32Cell[0].floor();\n", ind)
				g.pf("%s  if (index > %s) { index = %s; }\n", ind, countLit, countLit)
				g.pf("%s  w.put(index, %d);\n", ind, shape.Bits)
				g.pf("%s}\n", ind)
				return
			}
		}
		// The narrowing is AT THE ASSIGNMENT (issue 404, PORTING M3): under
		// the JIT a double crossing an out-of-line conversion call is boxed.
		g.pf("%s{\n", ind)
		g.pf("%s  final f = %s;\n", ind, expr)
		g.pf("%s  if (!f.isFinite) return false;\n", ind)
		g.pf("%s  TableBitWriter._float32Cell[0] = f;\n", ind)
		g.pf("%s  w.put(TableBitWriter._float32Words[0], 32);\n", ind)
		g.pf("%s}\n", ind)
		return
	}
	if kind == ir.TableKindF64 {
		// the same refusal, and the bits through the writer's own cell, at
		// the assignment (issue 404): no ByteData is made per value
		g.pf("%s{\n", ind)
		g.pf("%s  final f = %s;\n", ind, expr)
		g.pf("%s  if (!f.isFinite) return false;\n", ind)
		g.pf("%s  TableBitWriter._float64Cell[0] = f;\n", ind)
		g.pf("%s  w.put(TableBitWriter._float64Words[0], 64);\n", ind)
		g.pf("%s}\n", ind)
		return
	}
	if ir.TableKindWidth(int(kind)) == 16 {
		// A 128-BIT SCALAR RIDES AS TWO 64-BIT HALVES, THE LOW FIRST
		// (docs/SPEC-TABLES.md §3.3, the message form's scalar payload): the
		// low half at the width the vocabulary entry states, then the high half
		// at the rest, exactly the Go oracle's tableMessageWriteScalar
		// (internal/codegen/gotable/message.go:182).
		g.emitMessageWriteScalar128(f, entry, expr, ind)
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

// emitMessageWriteScalar128 writes a 128-bit scalar as its two 64-bit halves,
// the low first, at the width the vocabulary entry states (docs/SPEC-TABLES.md
// §3.3, the message form's scalar payload), mirroring the Go oracle's
// tableMessageWriteScalar (internal/codegen/gotable/message.go:182). A ranged
// shape subtracts its base from the halves first, with the borrow the low half
// passes to the high, exactly the runtime's UInt128 pair.
func (g *fixedModule) emitMessageWriteScalar128(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	kind, shape := entry.Kind, entry.Shape
	bits := ir.TableMessageValueBits(kind, shape)
	lowBits := min(bits, 64)
	highBits := bits - lowBits
	g.pf("%svar lo = %s.lo;\n", ind, expr)
	g.pf("%svar hi = %s.hi;\n", ind, expr)
	if shape.Packing == ir.TableMessageRanged {
		loBase, hiBase := fixed128Halves(shape.Base)
		loLit := fixedHexLit(loBase)
		g.pf("%svar dlo = lo - %s;\n", ind, loLit)
		g.pf("%svar borrow = (lo ^ 0x8000000000000000) < (%s ^ 0x8000000000000000) ? 1 : 0;\n", ind, loLit)
		g.pf("%slo = dlo;\n", ind)
		g.pf("%shi = hi - %s - borrow;\n", ind, fixedHexLit(hiBase))
	}
	g.pf("%sw.put(lo, %d);\n", ind, lowBits)
	if highBits > 0 {
		g.pf("%sw.put(hi, %d);\n", ind, highBits)
	}
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
		g.pf("%s  final buf = List<int>.filled(n, 0);\n", ind)
		g.pf("%s  for (var i = 0; i < n; i++) { buf[i] = r.bits.get(16); }\n", ind)
		g.pf("%s  if (r.bits.failed) { %s; }\n", ind, bad)
		g.pf("%s  for (var i = 0; i < kept; i++) { %s[i] = buf[i]; }\n", ind, expr)
		g.pf("%s  %sLength = kept;\n", ind, expr)
		g.pf("%s}\n", ind)
	case f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted:
		count := fmt.Sprintf("%d", f.ArrayBound)
		aind := ind
		if f.Array == ir.ArrayCounted {
			// A COUNTED ARRAY'S CLAMP IS A BLOCK: its `kept` is a local, so a
			// second counted array in the same body does not redeclare it.
			g.pf("%s{\n", ind)
			aind = ind + "  "
			count = expr + "Count"
			if ir.TableMessageCountBits(shape) > 0 {
				// THE COUNT IS A PAYLOAD TOO: read it into a local and store it
				// only after the read succeeded, so a body that runs out keeps
				// the count it had landed (internal/tablewire/messagedecode.go,
				// `array`; docs/SPEC-TABLES.md §3.3).
				g.pf("%s{\n", aind)
				g.pf("%s  final v = r.bits.get(%d) + %d;\n", aind, ir.TableMessageCountBits(shape), shape.Min)
				g.pf("%s  if (r.bits.failed) { %s; }\n", aind, bad)
				g.pf("%s  %s = v;\n", aind, count)
				g.pf("%s}\n", aind)
			}
			// A COUNT ABOVE THIS READER'S OWN BOUND KEEPS THE BOUND and counts
			// one `clamped`, exactly as the oracle's array read does
			// (internal/tablewire/messagedecode.go, `array`; docs/SPEC-TABLES.md
			// §3.3, §4). The elements past the bound are still read and dropped,
			// as Go's array does.
			g.pf("%s  final kept = %s < %d ? %s : %d;\n", aind, count, f.ArrayBound, count, f.ArrayBound)
			g.pf("%s  if (kept != %s) { r.report.clamped++; }\n", aind, count)
		} else if ir.TableMessageCountBits(shape) > 0 {
			g.pf("%sr.bits.get(%d);\n", aind, ir.TableMessageCountBits(shape))
			g.pf("%sif (r.bits.failed) { %s; }\n", aind, bad)
		}
		if ir.TableMessageAligns(kind, shape) {
			g.pf("%sif (!r.bits.align()) { %s; }\n", aind, bad)
		}
		elem := messageElementField(f)
		if f.Array == ir.ArrayCounted {
			if run := messageElementRunBits(kind, shape); run >= 0 {
				// a FIXED-WIDTH element: keep the bound and skip the surplus in
				// one arithmetic step, as the oracle's skipRun does (§3.3)
				g.pf("%s  for (var i = 0; i < kept; i++) {\n", aind)
				g.emitMessageReadElement(elem, entry, expr+"[i]", aind+"  ")
				g.pf("%s  }\n", aind)
				g.pf("%s  if (kept != %s) {\n", aind, count)
				g.pf("%s    if (!r.bits.skip((%s - kept) * %d)) { %s; }\n", aind, count, run, bad)
				g.pf("%s  }\n", aind)
			} else {
				// a COMPLEX element has no fixed width: walk them all and read
				// the surplus into a dropped sink, exactly as the oracle's array
				// walk does (§3.3)
				g.pf("%s  for (var i = 0; i < %s; i++) {\n", aind, count)
				g.pf("%s    if (i < kept) {\n", aind)
				g.emitMessageReadElement(elem, entry, expr+"[i]", aind+"      ")
				g.pf("%s    } else {\n", aind)
				g.emitMessageDropElement(elem, entry, aind+"      ")
				g.pf("%s    }\n", aind)
				g.pf("%s  }\n", aind)
			}
			// THE READER'S OWN COUNT BECOMES THE KEPT COUNT, as Go's array does
			// (`fv.Count = int(kept)`), so a save writes the bound and the
			// elements it holds, not the wire's larger count.
			g.pf("%s  %s = kept;\n", aind, count)
			g.pf("%s}\n", ind)
		} else {
			g.pf("%sfor (var i = 0; i < %s; i++) {\n", aind, count)
			g.emitMessageReadElement(elem, entry, expr+"[i]", aind+"  ")
			g.pf("%s}\n", aind)
		}
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
		// READ INTO A LOCAL, CHECK, THEN STORE: a body that runs out of bits
		// keeps the value it had landed (internal/tablewire/messagedecode.go,
		// `scalar`, the bool arm; docs/SPEC-TABLES.md §3.3).
		g.pf("%s{\n", ind)
		g.pf("%s  final v = r.bits.get(1) != 0;\n", ind)
		g.pf("%s  if (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
		g.pf("%s  %s = v;\n", ind, expr)
		g.pf("%s}\n", ind)
		return
	}
	// A FLOAT IS READ BACK FROM ITS IEEE-754 BITS (docs/SPEC-TABLES.md §3.3,
	// the message form's scalar payload): float32 from its 32 bits and float64
	// from its 64. A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS, before the
	// bits become a double and before the field is written: the batch refuses
	// as non_finite_float, naming the field, and lands nothing.
	refuse := fmt.Sprintf("r.report.RefuseNonFiniteFloat('%s')", g.gen.floatOwner(f))
	if kind == ir.TableKindF32 {
		// A COMPRESSED FLOAT RIDES AS ITS STEP INDEX (docs/SPEC-TABLES.md §3.3,
		// packing 2, ir.TableMessageQuantized): the width is the announced
		// shape's, an index above `count` is DAMAGE and is never reconstructed,
		// and the float is `min + (index / count) * delta` with TWO float32
		// roundings on each side, in the oracle's order
		// (internal/tablewire/messagedecode.go, `float32` and
		// ir.TableMessageDequantize). A raw f32 keeps the IEEE-754 path below.
		if shape.Packing == ir.TableMessageQuantized {
			count, delta, ok := ir.TableMessageQuantization(shape)
			if ok {
				countLit := fmt.Sprintf("%d", count)
				g.pf("%s{\n", ind)
				g.pf("%s  final index = r.bits.get(%d);\n", ind, shape.Bits)
				g.pf("%s  if (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
				g.pf("%s  if (index > %s) { r.report.Damage(); return false; }\n", ind, countLit)
				// float32(index) / float32(count), the oracle's operands.
				g.pf("%s  TableBitWriter._float32Cell[0] = index.toDouble();\n", ind)
				g.pf("%s  final fi = TableBitWriter._float32Cell[0];\n", ind)
				g.pf("%s  TableBitWriter._float32Cell[0] = %s.0;\n", ind, countLit)
				g.pf("%s  final fc = TableBitWriter._float32Cell[0];\n", ind)
				g.pf("%s  TableBitWriter._float32Cell[0] = fi / fc;\n", ind)
				g.pf("%s  final normalized = TableBitWriter._float32Cell[0];\n", ind)
				g.pf("%s  TableBitWriter._float32Cell[0] = normalized * %s;\n", ind, fixedFloatLit(float64(delta)))
				g.pf("%s  final scaled = TableBitWriter._float32Cell[0];\n", ind)
				g.pf("%s  TableBitWriter._float32Cell[0] = scaled + %s;\n", ind, fixedFloatLit(float64(shape.QMin)))
				g.pf("%s  %s = TableBitWriter._float32Cell[0];\n", ind, expr)
				g.pf("%s}\n", ind)
				if f.HasFloatRange && f.Type.Kind == ir.TFloat32 {
					// THE READER'S OWN RANGED FLOAT CLAMPS AND COUNTS, exactly
					// as the oracle's float32 read does
					// (internal/tablewire/messagedecode.go, `float32` and
					// `clampFloat`; docs/SPEC-TABLES.md §3.3, §4) and as the
					// file form's emitClamp does (fixeddart.go). The index check
					// above is the damage rule and runs first.
					lo, hi, _ := fixedFloatClampEnds(f)
					g.pf("%sif (%s < %s) { %s = %s; r.report.clamped++; } else if (%s > %s) { %s = %s; r.report.clamped++; }\n",
						ind, expr, lo, expr, lo, expr, hi, expr, hi)
				}
				return
			}
		}
		// The widening is AT THE ASSIGNMENT (issue 404, PORTING M3), and the
		// bits are a NaN or an infinity exactly when the exponent is all ones.
		g.pf("%s{\n", ind)
		g.pf("%s  final b = r.bits.get(32);\n", ind)
		g.pf("%s  if (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
		g.pf("%s  if ((b & 0x7f800000) == 0x7f800000) return %s;\n", ind, refuse)
		g.pf("%s  TableBitWriter._float32Words[0] = b;\n", ind)
		g.pf("%s  %s = TableBitWriter._float32Cell[0];\n", ind, expr)
		g.pf("%s}\n", ind)
		g.pf("%sif (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
		if f.HasFloatRange && f.Type.Kind == ir.TFloat32 {
			// THE READER'S OWN RANGED FLOAT CLAMPS AND COUNTS, exactly as the
			// oracle's float32 read does (internal/tablewire/messagedecode.go,
			// `float32` and `clampFloat`; docs/SPEC-TABLES.md §3.3, §4) and as
			// the file form's emitClamp does (fixeddart.go). A float64 field
			// reads raw and clamps nothing, as Go's scalar case for F64.
			// A -0 lands as +0, uncounted (§3.3, A COMPRESSED FLOAT'S SPECIAL
			// VALUES), as the oracle's clampFloat lands it.
			lo, hi, _ := fixedFloatClampEnds(f)
			g.pf("%sif (%s < %s) { %s = %s; r.report.clamped++; } else if (%s > %s) { %s = %s; r.report.clamped++; } else if (%s == 0) { %s = 0.0; }\n",
				ind, expr, lo, expr, lo, expr, hi, expr, hi, expr, expr)
		}
		return
	}
	if kind == ir.TableKindF64 {
		// READ INTO A LOCAL, CHECK, THEN STORE (internal/tablewire/messagedecode.go,
		// `scalar`, the f64 arm; docs/SPEC-TABLES.md §3.3).
		g.pf("%s{\n", ind)
		g.pf("%s  final b = r.bits.get(64);\n", ind)
		g.pf("%s  if (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
		g.pf("%s  if ((b & 0x7ff0000000000000) == 0x7ff0000000000000) return %s;\n", ind, refuse)
		g.pf("%s  TableBitWriter._float64Words[0] = b;\n", ind)
		g.pf("%s  %s = TableBitWriter._float64Cell[0];\n", ind, expr)
		g.pf("%s}\n", ind)
		return
	}
	if ir.TableKindWidth(int(kind)) == 16 {
		// A 128-BIT SCALAR IS READ BACK AS ITS TWO 64-BIT HALVES, THE LOW
		// FIRST (docs/SPEC-TABLES.md §3.3, the message form's scalar payload),
		// at the width the vocabulary entry states, the Go oracle's
		// tableMessageReadScalar (internal/codegen/gotable/message.go:180).
		g.emitMessageReadScalar128(f, entry, expr, ind)
		return
	}
	bits := ir.TableMessageValueBits(kind, shape)
	// READ INTO A LOCAL, CHECK, THEN STORE: a body that runs out of bits keeps
	// every value it had landed, and a ranged field keeps its landed value
	// rather than raw 0 with no base added
	// (internal/tablewire/messagedecode.go, `scalar`; docs/SPEC-TABLES.md §3.3).
	g.pf("%s{\n", ind)
	g.pf("%s  final v = r.bits.get(%d);\n", ind, bits)
	g.pf("%s  if (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
	g.pf("%s  %s = v;\n", ind, expr)
	if shape.Packing == ir.TableMessageRanged {
		base := shape.Base
		baseLit := "0"
		if base != nil {
			baseLit = fixedIntLit(base, 8)
		}
		g.pf("%s  %s += %s;\n", ind, expr, baseLit)
	}
	// THE VALUE IS RECONSTRUCTED AT ITS STORAGE WIDTH (the reference's
	// tableMessageReadScalar writes the lane into the field's own bytes): a
	// signed int8/int16/int32 read back from its bits is that width's two's
	// complement, so -7 crosses as 0xfffffff9 and comes back as -7, never as
	// 4294967289 in a 64-bit Dart int.
	switch kind {
	case ir.TableKindI8, ir.TableKindI16, ir.TableKindI32:
		g.pf("%s  %s = %s.toSigned(%d);\n", ind, expr, expr, 8*ir.TableKindWidth(int(kind)))
	}
	g.pf("%s}\n", ind)
	// THE VALUE IS THEN CLAMPED TO THIS READER'S OWN DECLARATION and counted,
	// in the oracle's order (internal/tablewire/messagedecode.go, `scalar`;
	// docs/SPEC-TABLES.md §3.3, §4): the integer range first — signed against
	// the signed bounds, unsigned against the whole unsigned bounds — then the
	// `bits(N)` width clamp, each counting one `clamped`.
	// The comparisons and assignment literals are the fixed form's own
	// (fixedClampTerms, fixeddart.go): a uint64 bound at 2^63 or above rides
	// as its bit pattern XOR-flipped into the unsigned domain, so an unsigned
	// range is ordered whole (a decoded value past 2^63 reads back negative in
	// the Dart int and must not be mistaken for below-min), and an int64 bound
	// at -2^63 is written as the exhaustive literal Dart accepts (fixedIntLit).
	if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
		loLHS, loCmp, loAssign := g.gen.fixedClampTerms(f.Type, expr, f.IntMin)
		hiLHS, hiCmp, hiAssign := g.gen.fixedClampTerms(f.Type, expr, f.IntMax)
		g.pf("%sif (%s < %s) { %s = %s; r.report.clamped++; } else if (%s > %s) { %s = %s; r.report.clamped++; }\n",
			ind, loLHS, loCmp, expr, loAssign, hiLHS, hiCmp, expr, hiAssign)
	}
	if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
		maxBits := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		lhs, cmp, assign := g.gen.fixedClampTerms(f.Type, expr, maxBits)
		g.pf("%sif (%s > %s) { %s = %s; r.report.clamped++; }\n", ind, lhs, cmp, expr, assign)
	}
}

// emitMessageReadScalar128 reads a 128-bit scalar back as its two 64-bit
// halves, the low first, at the width the vocabulary entry states
// (docs/SPEC-TABLES.md §3.3, the message form's scalar payload), mirroring the
// Go oracle's tableMessageReadScalar (internal/codegen/gotable/message.go:180)
// and reconstructing the storage class with the carry a ranged shape's base
// adds to the high half, exactly the runtime's UInt128 pair.
func (g *fixedModule) emitMessageReadScalar128(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	kind, shape := entry.Kind, entry.Shape
	bits := ir.TableMessageValueBits(kind, shape)
	lowBits := min(bits, 64)
	highBits := bits - lowBits
	bad := "r.report.Damage(); return false"
	cls := "UInt128"
	if fixedSigned(f.Type) {
		cls = "Int128"
	}
	g.needClass(cls)
	g.pf("%svar lo = r.bits.get(%d);\n", ind, lowBits)
	g.pf("%sif (r.bits.failed) { %s; }\n", ind, bad)
	if highBits > 0 {
		g.pf("%svar hi = r.bits.get(%d);\n", ind, highBits)
		g.pf("%sif (r.bits.failed) { %s; }\n", ind, bad)
	} else {
		g.pf("%svar hi = 0;\n", ind)
	}
	if shape.Packing == ir.TableMessageRanged {
		loBase, hiBase := fixed128Halves(shape.Base)
		loLit := fixedHexLit(loBase)
		g.pf("%svar sumLo = lo + %s;\n", ind, loLit)
		g.pf("%svar carry = (sumLo ^ 0x8000000000000000) < (lo ^ 0x8000000000000000) ? 1 : 0;\n", ind)
		g.pf("%shi = hi + %s + carry;\n", ind, fixedHexLit(hiBase))
		g.pf("%slo = sumLo;\n", ind)
	}
	g.pf("%s%s = %s(hi, lo);\n", ind, expr, cls)
	if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
		// A 128-BIT SCALAR CLAMPS AGAINST THIS READER'S OWN DECLARATION and
		// counts, exactly as the oracle's wide read does
		// (internal/tablewire/messagedecode.go, `scalar`, the 128-bit
		// payload via tabletext.WideClamp; docs/SPEC-TABLES.md §3.3, §4).
		// The comparison literals come from the fixed form's own
		// fixedClampTerms (fixeddart.go), which spell an Int128/UInt128
		// bound as its high and low hex halves.
		loLHS, loCmp, loAssign := g.gen.fixedClampTerms(f.Type, expr, f.IntMin)
		hiLHS, hiCmp, hiAssign := g.gen.fixedClampTerms(f.Type, expr, f.IntMax)
		g.pf("%sif (%s < %s) { %s = %s; r.report.clamped++; } else if (%s > %s) { %s = %s; r.report.clamped++; }\n",
			ind, loLHS, loCmp, expr, loAssign, hiLHS, hiCmp, expr, hiAssign)
	}
}

func (g *fixedModule) emitMessageReadEnum(e *ir.Enum, expr, ind string) {
	g.pf("%s{\n", ind)
	g.pf("%s  final ref = r.bits.get(tableMessageRefBits);\n", ind)
	g.pf("%s  if (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
	g.gen.need(e.Name)
	g.pf("%s  if (ref == 0) { %s = %s.none; } else {\n", ind, expr, e.Name)
	g.pf("%s    switch (ref) {\n", ind)
	for i, v := range e.Variants {
		slot := g.messageNameSlot(ir.TableWireId(e.VariantWireName(i)))
		g.pf("%s      case %d: %s = %s.%s; break;\n", ind, slot, expr, e.Name, dartName(v))
	}
	g.pf("%s      default:\n", ind)
	// A reference that names no kind-0 entry — one above the announcement's
	// count, one naming a reserved id (the node table's entry is the one an
	// announcement may carry), or an entry that carries a payload — is
	// damage, exactly as the reference's bitDecoder.name resolves it
	// (internal/tablewire/messagedecode.go, `name` and `reserved`;
	// docs/SPEC-TABLES.md §3.1, §3.3). Only a kind-0 entry that is not one
	// of this enum's variants counts an `unknown`.
	g.pf("%s        if (ref > r.vocabulary.count || tableMessageReservedId(r.vocabulary.entries[ref - 1].id) || r.vocabulary.entries[ref - 1].kind != 0) {\n", ind)
	g.pf("%s          r.report.Damage();\n", ind)
	g.pf("%s          return false;\n", ind)
	g.pf("%s        }\n", ind)
	g.pf("%s        %s = %s.none; r.report.unknown++;\n", ind, expr, e.Name)
	g.pf("%s        break;\n", ind)
	g.pf("%s    }\n%s  }\n%s}\n", ind, ind, ind)
}

func (g *fixedModule) emitMessageReadUnion(un *ir.Union, expr, ind string) {
	g.pf("%s{\n", ind)
	g.pf("%s  final ref = r.bits.get(tableMessageRefBits);\n", ind)
	g.pf("%s  if (r.bits.failed) { r.report.Damage(); return false; }\n", ind)
	tagType, tagNone := g.gen.unionTagType(un)
	g.pf("%s  if (ref == 0) { %s.type = %s; } else {\n", ind, expr, tagNone)
	g.pf("%s    switch (ref) {\n", ind)
	for i, v := range un.Variants {
		slot := g.messageSlot(ir.TableArmEntry(v))
		arm := fmt.Sprint(i + 1)
		if tagType != "" {
			arm = tagType + "." + dartName(ir.GoExportName(v.Name))
		}
		g.pf("%s      case %d:\n", ind, slot)
		g.pf("%s        %s.type = %s;\n", ind, expr, arm)
		if !v.Void() {
			g.emitMessageReadValue(v.F, ir.TableArmEntry(v), expr+"."+dartName(v.Name), ind+"        ")
		}
		g.pf("%s        break;\n", ind)
	}
	g.pf("%s      default:\n", ind)
	// A reference that names no union arm's kind — one above the
	// announcement's count, one naming a reserved id, or a kind-0 entry that
	// frames nothing — is damage, exactly as the reference's bitDecoder.arm
	// resolves it (internal/tablewire/messagedecode.go, `arm` and
	// `reserved`; docs/SPEC-TABLES.md §3.1, §3.3). Only a payload-bearing
	// entry that is not one of this union's arms counts an `unknown`.
	g.pf("%s        if (ref > r.vocabulary.count || tableMessageReservedId(r.vocabulary.entries[ref - 1].id) || r.vocabulary.entries[ref - 1].kind == 0) {\n", ind)
	g.pf("%s          r.report.Damage();\n", ind)
	g.pf("%s          return false;\n", ind)
	g.pf("%s        }\n", ind)
	// AND ITS PAYLOAD IS STEPPED OVER by the announcement's own kind and
	// shape, as the reference does (internal/tablewire/messagedecode.go
	// union: `Unknown++`, `return d.skip(entry)`; docs/SPEC-TABLES.md §3.3).
	g.pf("%s        %s.type = %s; r.report.unknown++;\n", ind, expr, tagNone)
	g.pf("%s        if (!TableMessageShape.skipEntry(r, r.vocabulary.entries[ref - 1])) {\n", ind)
	g.pf("%s          r.report.Damage();\n", ind)
	g.pf("%s          return false;\n", ind)
	g.pf("%s        }\n", ind)
	g.pf("%s        break;\n", ind)
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

// messageElementRunBits is the bits ONE element of a message array occupies
// where that is a fixed number the reader can multiply, and -1 where the
// element has to be stepped one at a time — the message-form mirror of the
// oracle's elementRunBits (internal/tablewire/messagedecode.go).
func messageElementRunBits(kind uint8, shape ir.TableMessageShape) int64 {
	if int(kind) != ir.TableKindArray {
		return -1
	}
	switch int(shape.Elem) {
	case ir.TableKindTable, ir.TableKindUnion, ir.TableKindEnum, ir.TableKindPointer:
		return -1
	}
	inner := ir.TableMessageShape{}
	if shape.Inner != nil {
		inner = *shape.Inner
	}
	return ir.TableMessageValueBits(shape.Elem, inner)
}

// emitMessageDropElement reads one array element past the reader's own bound
// into a throwaway sink, consuming the same bits the oracle's element walk
// consumes so the fields that follow stay aligned; the value is discarded
// (internal/tablewire/messagedecode.go, `array`; docs/SPEC-TABLES.md §3.3).
func (g *fixedModule) emitMessageDropElement(f *ir.Field, entry ir.TableVocabularyEntry, ind string) {
	elem := messageElementField(f)
	switch t := elem.Type.Ref.(type) {
	case *ir.Struct:
		g.pf("%s    final t = %s();\n", ind, t.Name)
		g.emitMessageReadElement(elem, entry, "t", ind+"  ")
		return
	case *ir.Union:
		g.pf("%s    final t = %s();\n", ind, t.Name)
		g.emitMessageReadElement(elem, entry, "t", ind+"  ")
		return
	}
	// the scalar sink is written and never read: the bits are what is kept
	g.pf("%s    // ignore: unused_local_variable\n", ind)
	g.pf("%s    Object? t;\n", ind)
	g.emitMessageReadElement(elem, entry, "t", ind+"  ")
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
  report.nonFiniteFloat = ''; // this read's own refusal (§3.3)
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

/// ONE ANNOUNCED ENTRY'S SHAPE, decoded from its signature bytes so an
/// unknown entry can be stepped over (docs/SPEC-TABLES.md §3.3). The decode
/// and the skip are its static members, so the runtime spells no private
/// library-scope name (compiler/tablesdart_test.go).
final class TableMessageShape {
  int packing = 0;
  int bits = 0;
  int min = 0;
  int max = 0;
  int elem = 0;
  TableMessageShape? inner;


  /// Decodes ONE shape at at in an announced entry's signature, mirroring
  /// ir.DecodeShape. Answers the shape and the offset past it.
  static (TableMessageShape, int) decode(Uint8List sig, int at, int kind) {
    final s = TableMessageShape();
    if ((kind >= 2 && kind <= 10) || (kind >= 18 && kind <= 29)) {
      s.packing = sig[at];
      at += 1;
      if (s.packing == 1 && kind != 10) {
        final n = tableMessageLeb(sig, at, sig.length);
        at = n.$2;
        s.bits = n.$1;
        if (kind >= 18) {
          at += 16;
        } else {
          final b = tableMessageLeb(sig, at, sig.length);
          at = b.$2;
        }
      } else if (s.packing == 2 && kind == 10) {
        s.bits = quantizedBits(sig, at);
        at += 12;
      }
    } else if (kind == 12 || kind == 33) {
      final n = tableMessageLeb(sig, at, sig.length);
      s.max = n.$1;
      at = n.$2;
    } else if (kind == 14 || kind == 16) {
      if (kind == 14) {
        final lo = tableMessageLeb(sig, at, sig.length);
        s.min = lo.$1;
        at = lo.$2;
      }
      final hi = tableMessageLeb(sig, at, sig.length);
      s.max = hi.$1;
      at = hi.$2;
      s.elem = sig[at];
      at += 1;
      final inner = decode(sig, at, s.elem);
      s.inner = inner.$1;
      at = inner.$2;
    }
    return (s, at);
  }

  /// The width of a quantized f32 (ir.DecodeShape, ir.TableMessageQuantization):
  /// bits.Len32 of ceil(float32((max - min) / res)), clamped to [1, 2^32 - 256].
  static int quantizedBits(Uint8List sig, int at) {
    if (at + 12 > sig.length) return 0;
    final view = ByteData.sublistView(sig, at, at + 12);
    final f = Float32List(3);
    f[0] = view.getFloat32(0, Endian.little);
    f[1] = view.getFloat32(4, Endian.little);
    f[2] = view.getFloat32(8, Endian.little);
    f[1] = f[1] - f[0];
    f[2] = f[1] / f[2];
    var values = f[2];
    if (!(values >= 1)) {
      values = 1;
    } else if (values > 4294967040.0) {
      values = 4294967040.0;
    }
    var count = values.ceil();
    var width = 0;
    while (count != 0) {
      width++;
      count >>= 1;
    }
    return width;
  }

  /// bits_required(min, max): the width of max - min, and zero where the two
  /// are equal (ir.TableMessageBitsRequired). The difference is taken in the
  /// 64-bit domain Go's int64 subtraction wraps in, and a span near 2^64 reads
  /// back negative in a Dart int: its uint64 bit length is 64, exactly what
  /// bits.Len64 answers after the same wrap.
  static int bitsRequired(int min, int max) {
    if (max <= min) {
      return 0;
    }
    final value = max - min;
    if (value < 0) {
      return 64;
    }
    var width = 0;
    var v = value;
    while (v != 0) {
      width++;
      v >>= 1;
    }
    return width;
  }

  /// ONE value's width in bits under a shape (ir.TableMessageValueBits), or -1
  /// where the kind has no fixed-width payload.
  static int valueBits(int kind, TableMessageShape s) {
    if (kind == 1) {
      return 1;
    }
    if (kind == 11) {
      return 64;
    }
    if (kind == 10) {
      return s.packing == 2 ? s.bits : 32;
    }
    if ((kind >= 2 && kind <= 9) || (kind >= 18 && kind <= 29)) {
      if (s.packing == 1) {
        return s.bits;
      }
      return tableMessageKindWidth(kind) * 8;
    }
    return -1;
  }

  /// Steps over ONE announced entry's payload without decoding it. The
  /// announcement's kind and shape say where the next reference begins.
  /// [depth] is the unknown-skip walk (docs/SPEC-TABLES.md message_skip_depth):
  /// callers that omit it start at 0, and a nested body, a union arm or a
  /// walked element passes depth + 1. depth > 128 is malformed; 128 still skips.
  static bool skipEntry(TableMessageReader r, TableMessageEntry e, [int depth = 0]) {
    if (depth > 128) return false;
    final shape = decode(e.signature, 9, e.kind);
    return skip(r, e.kind, shape.$1, depth);
  }

  /// One body at [depth]. Fields of the body are skipped at this same depth.
  static bool skipBody(TableMessageReader r, int depth) {
    if (depth > 128) return false;
    while (true) {
      final ref = r.bits.get(r.vocabulary.refBits);
      if (r.bits.failed) return false;
      if (ref == 0) return true;
      if (ref > r.vocabulary.count) return false;
      if (!skipEntry(r, r.vocabulary.entries[ref - 1], depth)) return false;
    }
  }

  static bool skip(TableMessageReader r, int kind, TableMessageShape s, [int depth = 0]) {
    if (depth > 128) return false;
    switch (kind) {
      case 0:
      case 32:
        return true;
      case 30:
        final ref = r.bits.get(r.vocabulary.refBits);
        if (r.bits.failed) return false;
        if (ref == 0) return true;
        if (ref > r.vocabulary.count) return false;
        // bitDecoder.name: a reserved id or a payload-bearing entry is damage
        final named = r.vocabulary.entries[ref - 1];
        return !tableMessageReservedId(named.id) && named.kind == 0;
      case 15:
        final ref = r.bits.get(r.vocabulary.refBits);
        if (r.bits.failed) return false;
        if (ref == 0) return true;
        if (ref > r.vocabulary.count) return false;
        final arm = r.vocabulary.entries[ref - 1];
        // bitDecoder.arm: a reserved id or a kind-0 entry is damage
        if (tableMessageReservedId(arm.id) || arm.kind == 0) return false;
        return skipEntry(r, arm, depth + 1);
      case 13:
        return skipBody(r, depth + 1);
      case 17:
        // A FIXED ROOT NUMBERS NO NODE, so there is no index width to step
        // (internal/tablewire/messagedecode.go root: indexBits = 0).
        return false;
      case 12:
        final n = r.bits.get(bitsRequired(0, s.max));
        if (r.bits.failed || !r.bits.align()) return false;
        return r.bits.skip(n * 8);
      case 33:
        final n = r.bits.get(bitsRequired(0, s.max));
        if (r.bits.failed) return false;
        return r.bits.skip(n * 16);
      case 31:
        if (!r.bits.align()) return false;
        final n = r.bits.get(32);
        if (r.bits.failed) return false;
        return r.bits.skip(n * 8);
      case 14:
      case 16:
        var n = s.min;
        if (kind == 16) {
          n = 0;
        }
        final width = kind == 16
            ? bitsRequired(0, s.max)
            : bitsRequired(s.min, s.max);
        if (width > 0) {
          final raw = r.bits.get(width);
          if (r.bits.failed) return false;
          n = raw;
          if (kind == 14) {
            n += s.min;
          }
        }
        if (kind == 14 && s.elem == 6 && !r.bits.align()) return false;
        final inner = s.inner ?? TableMessageShape();
        // elementRunBits + skipRun (internal/codegen/gotable/message.go): a
        // fixed-width element of an array or keyed entry is stepped
        // arithmetically; only a walked element recurses at depth + 1.
        final elem = s.elem;
        if (elem != 13 && elem != 15 && elem != 17 && elem != 30) {
          var run = (elem == 0 || elem == 32) ? 0 : valueBits(elem, inner);
          if (run >= 0) {
            if (kind == 16) {
              run += r.vocabulary.refBits;
            }
            if (run == 0) return true;
            if (n > 0x7fffffffffffffff ~/ run) return false;
            return r.bits.skip(n * run);
          }
        }
        for (var i = 0; i < n; i++) {
          if (kind == 16) {
            r.bits.get(r.vocabulary.refBits);
            if (r.bits.failed) return false;
          }
          if (!skip(r, s.elem, inner, depth + 1)) return false;
        }
        return true;
    }
    final width = valueBits(kind, s);
    if (width < 0) return false;
    return r.bits.skip(width);
  }
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

// The views the inline float conversions use, spelled at each assignment
// (issue 404; a Dart library cannot reach another library's private views,
// SPEC-TABLES §3). A table float is finite (§3.3), so each is the ordinary
// conversion through one cell.
const dartMessageFloat32Runtime = `
  static final Float32List _float32Cell = Float32List(1);
  static final Uint32List _float32Words = _float32Cell.buffer.asUint32List();

`

const dartMessageFloat64Runtime = `
  static final Float64List _float64Cell = Float64List(1);
  static final Uint64List _float64Words = _float64Cell.buffer.asUint64List();

`
