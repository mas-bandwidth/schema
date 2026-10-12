package javatable

// THE MESSAGE FORM's BATCH SURFACE (docs/SPEC-TABLES.md §3.3), form byte 2:
// the batch verbs MeasureMessages, SaveMessages and LoadMessages over one
// announced vocabulary, emitted for `fixed table` roots. A message is a
// bitpacked body under a vocabulary announced once (§3.3), so the shape a
// reader steps through is one the declaration fixes.
//
// The Go backend (internal/codegen/gotable) and the C# backend
// (internal/codegen/cstable) are the references; the wire bytes are the C++
// reference's, pinned by testdata/conformance/tables/MANIFEST.txt. The bit
// order is the packet wire's: bit i of the stream is bit i%8 of byte i/8, low
// bit first (SPEC.md §4.3).
//
// The vocabulary a LoadMessages reads with is the ANNOUNCEMENT surface's
// resolved TableVocabulary (messageform.go), filled once by announceRead; the
// writer's reference slots are the unit's own compile-time vocabulary, so they
// ride as constants. The shared batch runtime is ONE FILE (TableMessage.java)
// with nested static types, because a nested class needs no file of its own
// name, where a package-scope type would (javatable.go, "THE UNIT'S NAMESPACE
// IS THE PACKAGE"). Nothing here allocates on the read path: the caller owns
// every buffer.
import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// messageClass is the class one fixed root's batch surface lands in:
// `<Root>Message`, beside the fixed form's `<Root>Fixed`.
func messageClass(name string) string { return ir.GoExportName(name) + "Message" }

// messageSlot returns a field or arm's slot number, counted from 1, in the
// unit's own vocabulary (§3.3: the writer's slot numbers are compile-time
// constants).
func messageSlot(u *ir.Unit, e ir.TableVocabularyEntry) int {
	for i, v := range ir.TableVocabulary(u) {
		if e.Key() == v.Key() {
			return i + 1
		}
	}
	panic("message entry outside vocabulary")
}

// messageNameSlot returns the slot a NAME entry (kind 0, the enum variant's or
// the keyed array's name) holds in the unit's own vocabulary, or 0 where it is
// absent — a name the unit does not carry cannot ride (§3.3).
func messageNameSlot(u *ir.Unit, id uint64) int {
	for i, e := range ir.TableVocabulary(u) {
		if e.Id == id && e.Kind == 0 {
			return i + 1
		}
	}
	return 0
}

func tableMessageRuntime(u *ir.Unit) string {
	var b strings.Builder
	b.WriteString("public final class TableMessage {\n    private TableMessage() {}\n\n")
	fmt.Fprintf(&b, "    public static final int batchMax = 256;\n")
	fmt.Fprintf(&b, "    public static final int refBits = %d;\n", ir.TableMessageRefBits(len(ir.TableVocabulary(u))))
	b.WriteString(messageRuntimeBody)
	b.WriteString("}\n")
	return b.String()
}

// generateMessageBatchFiles emits the batch surface for a unit with a fixed
// root: the shared runtime and one `<Root>Message` class per fixed root's
// closure. Without a fixed root there is no message form to read (§3.3) and no
// file is written.
func generateMessageBatchFiles(u *ir.Unit) (map[string][]byte, error) {
	roots := fixedRoots(u)
	if len(roots) == 0 {
		return map[string][]byte{}, nil
	}
	seen := map[string]bool{}
	var order []*ir.Struct
	for _, st := range roots {
		fixedCollectTypes(st, seen, &order)
	}
	isRoot := map[string]bool{}
	for _, st := range roots {
		isRoot[st.Name] = true
	}
	out := map[string][]byte{
		"TableMessage.java": javaFile(u, "the MESSAGE FORM's batch runtime (docs/SPEC-TABLES.md §3.3), form byte 2", tableMessageRuntime(u)),
	}
	for _, st := range order {
		g := &messageGen{unit: u}
		g.emitType(st, isRoot[st.Name])
		out[messageClass(st.Name)+".java"] = javaFile(u,
			fmt.Sprintf("%s under the message form (docs/SPEC-TABLES.md §3.3)", st.Name), g.b.String())
	}
	return out, nil
}

type messageGen struct {
	unit *ir.Unit
	b    strings.Builder
}

func (g *messageGen) pf(format string, args ...any) { fmt.Fprintf(&g.b, format, args...) }

func (g *messageGen) emitType(st *ir.Struct, root bool) {
	n := messageClass(st.Name)
	g.pf("public final class %s {\n    private %s() {}\n\n", n, n)
	g.emitReset(st)
	g.emitWriteBody(st)
	g.emitReadBody(st)
	if root {
		g.emitBatchVerbs(st)
	}
	g.pf("}\n")
}

// emitReset returns a value to its DECLARED DEFAULTS before a body is read,
// the same values the fixed form's Value constructor holds (fixedform.go,
// emitStorageField), because "a field at its declared default is not written"
// (docs/SPEC-TABLES.md §3.3): a field the body does not name reads as its
// declared default, never as zero.
func (g *messageGen) emitReset(st *ir.Struct) {
	g.pf("    static void reset(%s.Value v) {\n", fixedClass(st.Name))
	for _, f := range st.Fields {
		name := member(f)
		if f.Type.Optional {
			g.pf("        v.%sPresent = false;\n", name)
		}
		switch {
		case f.Type.Kind == ir.TString || (f.Type.Kind == ir.TBytes && !f.Type.Pointer):
			g.pf("        v.%sLength = %d;\n", name, len(f.DefBytes))
			if len(f.DefBytes) > 0 {
				g.pf("        %s;\n", fixedByteDefaultExpr("v."+name, f.DefBytes))
			}
		case f.Type.Kind == ir.TWString:
			g.pf("        v.%sLength = 0;\n", name)
		case f.Array != ir.ArrayNone || f.KeyEnum != "":
			for i := 0; i < int(f.ArrayBound); i++ {
				g.resetElement(messageElementField(f), fmt.Sprintf("v.%s[%d]", name, i))
			}
			if f.Array == ir.ArrayCounted {
				g.pf("        v.%sCount = %d;\n", name, f.BornCount())
			}
		case isStructRef(f.Type):
			g.pf("        %s.reset(v.%s);\n", messageClass(f.Type.Name), name)
		case isUnionRef(f.Type):
			g.pf("        v.%s.type = 0;\n", name)
		default:
			g.pf("        v.%s = %s;\n", name, messageDefault(f))
		}
	}
	g.pf("    }\n")
}

// resetElement returns one array or keyed slot to its declared default:
// "EVERY SLOT TAKES THE DECLARED DEFAULT" (fixedform.go, emitStorageField).
func (g *messageGen) resetElement(f *ir.Field, expr string) {
	switch {
	case isStructRef(f.Type):
		g.pf("        %s.reset(%s);\n", messageClass(f.Type.Name), expr)
	case isUnionRef(f.Type):
		g.pf("        %s.type = 0;\n", expr)
	default:
		g.pf("        %s = %s;\n", expr, messageDefault(f))
	}
}

// messageDefault is the Java literal of a leaf's declared default, the one the
// fixed form's Value constructor assigns (fixedScalarDefault), and zero or
// false where none is declared. The writer elides against it and the reader
// resets to it (docs/SPEC-TABLES.md §3.3, "Elision, defaults, and what a
// reader reports").
func messageDefault(f *ir.Field) string {
	if d := fixedScalarDefault(f); d != "" {
		return d
	}
	if f.Type.Kind == ir.TBool {
		return "false"
	}
	return "0"
}

func (g *messageGen) emitBatchVerbs(st *ir.Struct) {
	v := fixedClass(st.Name)
	g.pf("    public static long measureMessages(%s.Value[] values, TableMessage.Report report) {\n", v)
	g.pf("        if (values.length < 1 || values.length > TableMessage.batchMax) {\n            if (report != null) TableMessage.refuse(report, \"batch_too_large\");\n            return -1;\n        }\n")
	g.pf("        final TableMessage.Writer w = new TableMessage.Writer(null, 0, true);\n")
	g.pf("        for (%s.Value v : values) { if (v == null || !writeBody(w, v)) return -1; }\n", v)
	g.pf("        w.align();\n        if (w.overflow) return -1;\n        return 2 + w.bits / 8;\n    }\n\n")
	g.pf("    public static long saveMessages(%s.Value[] values, byte[] buffer, TableMessage.Report report) {\n", v)
	g.pf("        if (values.length < 1 || values.length > TableMessage.batchMax) {\n            if (report != null) TableMessage.refuse(report, \"batch_too_large\");\n            return -1;\n        }\n")
	g.pf("        if (buffer.length < 2) return -1;\n        buffer[0] = 2;\n        buffer[1] = (byte) (values.length - 1);\n")
	g.pf("        final TableMessage.Writer w = new TableMessage.Writer(buffer, 2, false);\n")
	g.pf("        for (%s.Value v : values) { if (v == null || !writeBody(w, v)) return -1; }\n", v)
	g.pf("        w.align();\n        if (w.overflow) return -1;\n        return w.bits / 8;\n    }\n\n")
	g.pf("    public static long loadMessages(%s.Value[] values, byte[] data, TableVocabulary vocabulary, TableMessage.Report report) {\n", v)
	g.pf("        final long count = TableMessage.batchOpen(values, data, vocabulary, report);\n        if (count < 0) return -1;\n")
	g.pf("        if (report != null && report.refused) return count;\n")
	g.pf("        final TableMessage.Reader r = TableMessage.reader(data, 2, vocabulary, report);\n")
	g.pf("        for (int i = 0; i < count; i++) { if (!readBody(r, vocabulary, values[i])) { return i; } }\n")
	g.pf("        if (!TableMessage.batchClose(r)) return count - 1;\n        return count;\n    }\n")
}

func (g *messageGen) emitWriteBody(st *ir.Struct) {
	g.pf("    static boolean writeBody(TableMessage.Writer w, %s.Value v) {\n", fixedClass(st.Name))
	for _, f := range st.Fields {
		expr := "v." + member(f)
		entry := ir.TableFieldEntry(f)
		g.pf("        {\n")
		if f.Type.Optional {
			g.pf("            if (v.%sPresent) {\n", member(f))
		}
		if isElidableStructField(f) {
			// "an all-default nested table is not written" (§3.3), so the
			// writer measures the nested body into a zeroed probe and rides
			// only when it exceeds the reference it would cost, mirroring
			// gotable/messagewrite.go's probe.
			g.pf("            final TableMessage.Writer probe = new TableMessage.Writer(null, 0, true);\n")
			g.pf("            if (!%s.writeBody(probe, %s)) return false;\n", messageClass(f.Type.Name), expr)
			g.pf("            if (probe.bits > TableMessage.refBits) { w.put(%d, TableMessage.refBits); %s }\n", messageSlot(g.unit, entry), g.value(f, expr))
		} else {
			// "presence rather than content decides ?T" (§3.3): a present
			// optional rides whatever it holds.
			pre, cond := "", "true"
			if !f.Type.Optional {
				pre, cond = g.fieldRides(f, expr)
			}
			g.pf("            %sif (%s) { w.put(%d, TableMessage.refBits); %s }\n", pre, cond, messageSlot(g.unit, entry), g.value(f, expr))
		}
		if f.Type.Optional {
			g.pf("            }\n")
		}
		g.pf("        }\n")
	}
	g.pf("        w.put(0, TableMessage.refBits);\n        return !w.overflow;\n    }\n")
}

// fieldRides answers whether a field rides: "A field at its declared default is
// not written, an empty string, array, union or all-default nested table is
// not written" (docs/SPEC-TABLES.md §3.3). It mirrors the Go reference's
// emitFieldRides (gotable/write.go): `pre` is a statement the condition needs
// (a fixed array's all-default scan), emitted inside the field's own block.
func (g *messageGen) fieldRides(f *ir.Field, expr string) (pre, cond string) {
	if f.Array == ir.ArrayFixed && f.KeyEnum == "" && f.Type.Kind != ir.TString && f.Type.Kind != ir.TBytes {
		if isStructRef(f.Type) {
			return "", "true"
		}
		el := messageElementField(f)
		return fmt.Sprintf("boolean allDefault = true; for (int i = 0; i < %s.length; i++) { if (%s) { allDefault = false; break; } } ", expr, g.rides(el, expr+"[i]")), "!allDefault"
	}
	return "", g.rides(f, expr)
}

// rides is one value's elision test against its declared default (§3.3); a
// counted array rides when it holds an element, text when it differs from its
// declared default, a union when an arm is selected.
func (g *messageGen) rides(f *ir.Field, expr string) string {
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		if len(f.DefBytes) == 0 {
			return expr + "Length != 0"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "(%sLength != %d", expr, len(f.DefBytes))
		for i, c := range f.DefBytes {
			fmt.Fprintf(&b, " || %s[%d] != (byte) %d", expr, i, int8(c))
		}
		b.WriteString(")")
		return b.String()
	case f.Type.Kind == ir.TWString:
		return expr + "Length != 0"
	case f.Array == ir.ArrayCounted:
		return expr + "Count != 0"
	case isStructRef(f.Type):
		return "true"
	case isUnionRef(f.Type):
		return expr + ".type != 0"
	}
	return expr + " != " + messageDefault(f)
}

func (g *messageGen) value(f *ir.Field, expr string) string {
	var b strings.Builder
	entry := ir.TableFieldEntry(f)
	shape := entry.Shape
	switch {
	case f.Type.Kind == ir.TBool:
		fmt.Fprintf(&b, "w.put(%s ? 1 : 0, 1);", expr)
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(&b, "w.put(%sLength, %d); w.align(); w.raw(%s, %sLength);", expr, ir.TableMessageCountBits(shape), expr, expr)
	case f.Type.Kind == ir.TBytes && !f.Type.Pointer:
		fmt.Fprintf(&b, "w.put(%sLength, %d); w.align(); w.raw(%s, %sLength);", expr, ir.TableMessageCountBits(shape), expr, expr)
	case f.Type.Kind == ir.TWString:
		fmt.Fprintf(&b, "w.put(%sLength, %d); for (int i = 0; i < %sLength; i++) w.put(%s[i], 16);", expr, ir.TableMessageCountBits(shape), expr, expr)
	case f.KeyEnum != "":
		g.keyedValue(f, expr, &b)
	case f.Array != ir.ArrayNone:
		g.arrayValue(f, expr, &b)
	case isStructRef(f.Type):
		fmt.Fprintf(&b, "if (!%s.writeBody(w, %s)) return false;", messageClass(f.Type.Name), expr)
	case isUnionRef(f.Type):
		g.unionValue(f, expr, &b)
	case enumRef(f) != nil:
		fmt.Fprintf(&b, "if (%s == 0) { w.put(0, TableMessage.refBits); } else { long slot = 0; switch (%s) {", expr, expr)
		for i, v := range enumRef(f).Variants {
			fmt.Fprintf(&b, " case %d: slot = %d; break;", i+1, messageNameSlot(g.unit, ir.TableWireId(v)))
		}
		b.WriteString(" } if (slot == 0) return false; w.put(slot, TableMessage.refBits); }")
	default:
		g.scalarValue(f, expr, &b, entry.Kind, shape)
	}
	return b.String()
}

func (g *messageGen) scalarValue(f *ir.Field, expr string, b *strings.Builder, kind uint8, shape ir.TableMessageShape) {
	lo, hi := "0L", "0L"
	switch kind {
	case 10:
		lo = "java.lang.Float.floatToRawIntBits(" + expr + ")"
	case 11:
		lo = "java.lang.Double.doubleToRawLongBits(" + expr + ")"
	default:
		if ir.TableKindWidth(int(kind)) == 16 {
			lo, hi = expr+"Lo", expr+"Hi"
		} else {
			lo = "((" + expr + ") & 0xFFFFFFFFFFFFFFFFL)"
		}
	}
	fmt.Fprintf(b, "w.putScalar(%s, %s, %d, %d, 0x%xL, 0x%xL);",
		lo, hi, shape.Packing, int(ir.TableMessageValueBits(kind, shape)),
		shapeBaseLo(shape), shapeBaseHi(shape))
}

func shapeBaseLo(s ir.TableMessageShape) uint64 {
	if s.Base == nil {
		return 0
	}
	v := new(big.Int).Set(s.Base)
	v.Mod(v, new(big.Int).Lsh(big.NewInt(1), 128))
	return v.Uint64()
}
func shapeBaseHi(s ir.TableMessageShape) uint64 {
	if s.Base == nil {
		return 0
	}
	v := new(big.Int).Set(s.Base)
	v.Mod(v, new(big.Int).Lsh(big.NewInt(1), 128))
	return new(big.Int).Rsh(v, 64).Uint64()
}

func (g *messageGen) arrayValue(f *ir.Field, expr string, b *strings.Builder) {
	entry := ir.TableFieldEntry(f)
	shape := entry.Shape
	count := fmt.Sprintf("%d", f.ArrayBound)
	if f.Array == ir.ArrayCounted {
		count = expr + "Count"
	}
	fmt.Fprintf(b, "w.put(%s - %dL, %d);", count, shape.Min, ir.TableMessageCountBits(shape))
	if ir.TableMessageAligns(entry.Kind, shape) {
		b.WriteString(" w.align();")
	}
	el := messageElementField(f)
	fmt.Fprintf(b, " for (int i = 0; i < %s; i++) { %s }", count, g.value(el, expr+"[i]"))
}

func (g *messageGen) keyedValue(f *ir.Field, expr string, b *strings.Builder) {
	entry := ir.TableFieldEntry(f)
	shape := entry.Shape
	el := messageElementField(f)
	fmt.Fprintf(b, "{ long present = 0; for (int i = 0; i < %d; i++) { if (%s) present++; } w.put(present - %dL, %d);",
		f.ArrayBound, g.rides(el, expr+"[i]"), shape.Min, ir.TableMessageCountBits(shape))
	fmt.Fprintf(b, " for (int i = 0; i < %d; i++) { if (%s) { long slot = %d; if (slot == 0) return false; w.put(slot, TableMessage.refBits); %s } } }",
		f.ArrayBound, g.rides(el, expr+"[i]"), messageNameSlot(g.unit, ir.TableWireId(f.KeyEnumRef.Variants[0])), g.value(el, expr+"[i]"))
}

func (g *messageGen) unionValue(f *ir.Field, expr string, b *strings.Builder) {
	un := f.Type.Ref.(*ir.Union)
	fmt.Fprintf(b, "switch (%s.type) {\n", expr)
	fmt.Fprintf(b, "            case 0: w.put(0, TableMessage.refBits); break;\n")
	for i, v := range un.Variants {
		arm := ir.TableArmEntry(v)
		fmt.Fprintf(b, "            case %d: w.put(%d, TableMessage.refBits);", i+1, messageSlot(g.unit, arm))
		if !v.Void() {
			b.WriteString(" " + g.value(v.F, g.unionArmExpr(un, v, expr)) + " break;")
		} else {
			b.WriteString(" break;")
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "            default: return false;\n        }")
}

func (g *messageGen) unionArmExpr(un *ir.Union, v ir.UnionVariant, expr string) string {
	return expr + "." + javaName(v.Name)
}

func messageElementField(f *ir.Field) *ir.Field {
	e := *f
	e.Array = ir.ArrayNone
	e.KeyEnum = ""
	e.KeyEnumRef = nil
	e.Type.Optional = false
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		e.Type = ir.FieldType{Kind: ir.TInt, Width: 8}
	}
	return &e
}

func (g *messageGen) emitReadBody(st *ir.Struct) {
	g.pf("    static boolean readBody(TableMessage.Reader r, TableVocabulary vocabulary, %s.Value v) {\n", fixedClass(st.Name))
	g.pf("        reset(v);\n")
	g.pf("        while (true) {\n")
	g.pf("            final long ref = r.get(vocabulary.refBits);\n            if (r.err) return false;\n            if (ref == 0) return true;\n")
	g.pf("            final TableVocabulary.Entry entry = vocabulary.entry(ref);\n            if (entry == null || Long.compareUnsigned(entry.id, -3L) >= 0) { r.malformed(); return false; }\n")
	for i, f := range st.Fields {
		mine := ir.TableFieldEntry(f)
		kw := "if"
		if i > 0 {
			kw = "else if"
		}
		g.pf("            %s (entry.id == 0x%016xL) {\n", kw, mine.Id)
		g.emitReadValue(f, "v."+member(f), "entry.shape", "entry.element", "                ")
		if f.Type.Optional {
			g.pf("                v.%sPresent = true;\n", member(f))
		}
		g.pf("            } ")
	}
	g.pf("else {\n                if (!TableMessage.skip(r, entry, 0)) { r.malformed(); return false; }\n            }\n")
	g.pf("        }\n    }\n")
}

// emitReadValue reads one value under its ANNOUNCED shape (docs/SPEC-TABLES.md
// §3.3): `shape` and `element` are Java expressions of the sender's Shape for
// the value and, for an array or keyed array, for its elements.
func (g *messageGen) emitReadValue(f *ir.Field, expr, shape, element, ind string) {
	switch {
	case f.Type.Kind == ir.TBool:
		g.pf("%s%s = r.get(1) != 0;\n", ind, expr)
	case f.Type.Kind == ir.TString:
		g.pf("%s{ final long n = r.count(%s); if (!r.align()) return false; final int start = (int) r.offset / 8; if (!r.skip(n * 8)) return false; final int kept = (int) java.lang.Math.min(n, %d); if (kept < n) r.report.clamped++; java.lang.System.arraycopy(r.buffer, start, %s, 0, kept); %sLength = kept; }\n", ind, shape, f.Type.Size, expr, expr)
	case f.Type.Kind == ir.TBytes && !f.Type.Pointer:
		g.pf("%s{ final long n = r.count(%s); if (!r.align()) return false; final int start = (int) r.offset / 8; if (!r.skip(n * 8)) return false; final int kept = (int) java.lang.Math.min(n, %d); if (kept < n) r.report.clamped++; java.lang.System.arraycopy(r.buffer, start, %s, 0, kept); %sLength = kept; }\n", ind, shape, f.Type.Size, expr, expr)
	case f.Type.Kind == ir.TWString:
		g.pf("%s{ final long n = r.count(%s); final int kept = (int) java.lang.Math.min(n, %d); if (kept < n) r.report.clamped++; for (int i = 0; i < n; i++) { final int u = (int) r.get(16); if (i < kept) %s[i] = (char) u; } %sLength = kept; }\n", ind, shape, f.Type.Size, expr, expr)
	case f.KeyEnum != "":
		g.keyedRead(f, expr, shape, element, ind)
	case f.Array != ir.ArrayNone:
		g.arrayRead(f, expr, shape, element, ind)
	case isStructRef(f.Type):
		g.pf("%sif (!%s.readBody(r, vocabulary, %s)) return false;\n", ind, messageClass(f.Type.Name), expr)
	case isUnionRef(f.Type):
		g.unionRead(f, expr, ind)
	case enumRef(f) != nil:
		g.enumRead(f, expr, ind)
	default:
		g.scalarRead(f, expr, shape, ind)
	}
}

func (g *messageGen) scalarRead(f *ir.Field, expr, shape, ind string) {
	kind := ir.TableFieldEntry(f).Kind
	switch kind {
	case 10:
		g.pf("%s%s = java.lang.Float.intBitsToFloat((int) r.getScalar(%s));\n", ind, expr, shape)
	case 11:
		g.pf("%s%s = java.lang.Double.longBitsToDouble(r.getScalar(%s));\n", ind, expr, shape)
	default:
		g.pf("%s%s = (%s) r.getScalar(%s);\n", ind, expr, fixedScalarType(f.Type), shape)
	}
}

func (g *messageGen) enumRead(f *ir.Field, expr, ind string) {
	en := enumRef(f)
	g.pf("%s{ final long slot = r.get(vocabulary.refBits); if (slot == 0) { %s = 0; } else { final long id = vocabulary.name(slot);\n", ind, expr)
	for i, v := range en.Variants {
		kw := "if"
		if i > 0 {
			kw = "else if"
		}
		g.pf("%s    %s (id == 0x%016xL) { %s = %d; }\n", ind, kw, ir.TableWireId(v), expr, i+1)
	}
	g.pf("%s    else { r.report.unknown++; }\n%s} }\n", ind, ind)
}

func (g *messageGen) arrayRead(f *ir.Field, expr, shape, element, ind string) {
	el := messageElementField(f)
	bound := f.ArrayBound
	counted := f.Array == ir.ArrayCounted
	companion := expr + "Count"
	g.pf("%s{ final long n = r.count(%s); ", ind, shape)
	if ir.TableMessageAligns(ir.TableFieldEntry(f).Kind, ir.TableFieldEntry(f).Shape) {
		g.pf("if (!r.align()) return false; ")
	}
	g.pf("final int keep = (int) java.lang.Math.min(n, %d); if (keep < n) r.report.clamped++; for (int i = 0; i < keep; i++) { ", bound)
	g.emitReadValue(el, expr+"[i]", element, "null", ind+"    ")
	g.pf("} ")
	if counted {
		g.pf("%s = keep; ", companion)
	}
	g.pf("}\n")
}

func (g *messageGen) keyedRead(f *ir.Field, expr, shape, element, ind string) {
	el := messageElementField(f)
	g.pf("%s{ final long n = r.count(%s); for (int i = 0; i < n; i++) { final long slot = r.get(vocabulary.refBits); final long id = vocabulary.name(slot);\n", ind, shape)
	for i, v := range f.KeyEnumRef.Variants {
		kw := "if"
		if i > 0 {
			kw = "else if"
		}
		g.pf("%s    %s (id == 0x%016xL) {\n", ind, kw, ir.TableWireId(v))
		g.emitReadValue(el, expr+"["+fmt.Sprint(i)+"]", element, "null", ind+"        ")
		g.pf("%s    } ", ind)
	}
	g.pf("else { r.report.unknown++; if (!TableMessage.skipShape(r, %s, null, 0)) { r.malformed(); return false; } }\n%s} }\n", element, ind)
}

func (g *messageGen) unionRead(f *ir.Field, expr, ind string) {
	un := f.Type.Ref.(*ir.Union)
	g.pf("%s{ final long slot = r.get(vocabulary.refBits); if (slot == 0) { %s.type = 0; } else { final TableVocabulary.Entry arm = vocabulary.entry(slot);\n", ind, expr)
	for i, v := range un.Variants {
		arm := ir.TableArmEntry(v)
		kw := "if"
		if i > 0 {
			kw = "else if"
		}
		g.pf("%s    %s (arm.id == 0x%016xL) {\n", ind, kw, arm.Id)
		g.pf("%s        %s.type = %d;", ind, expr, i+1)
		if !v.Void() {
			g.pf("\n")
			g.emitReadValue(v.F, g.unionArmExpr(un, v, expr), "arm.shape", "arm.element", ind+"        ")
		}
		g.pf("\n%s    } ", ind)
	}
	g.pf("else { %s.type = 0; r.report.unknown++; if (!TableMessage.skip(r, arm, 0)) { r.malformed(); return false; } }\n%s} }\n", expr, ind)
}

func enumRef(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

func isStructRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	_, ok := t.Ref.(*ir.Struct)
	return ok
}

// isElidableStructField reports a struct field the elision rule probes before
// writing (docs/SPEC-TABLES.md §3.3: "an all-default nested table is not
// written"). An optional `?T`, an array element and a keyed slot keep their
// own presence rules, so only a plain by-value nested table is probed.
func isElidableStructField(f *ir.Field) bool {
	return !f.Type.Optional && !f.Type.Pointer && f.Array == ir.ArrayNone &&
		f.KeyEnum == "" && isStructRef(f.Type)
}

func isUnionRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	_, ok := t.Ref.(*ir.Union)
	return ok
}

// messageRuntimeBody is the Java batch runtime shared by every generated unit.
const messageRuntimeBody = `public static final class Report {
    public boolean malformed, refused;
    public String reason;
    public long unknown, clamped;
    public int verdict;
}

public static final class Writer {
    public byte[] buffer;
    public long bits;
    public final boolean measuring;
    public boolean overflow;
    public Writer(byte[] buffer, int startBytes, boolean measuring) {
        this.buffer = buffer; this.bits = startBytes * 8L; this.measuring = measuring;
    }
    public void put(long value, int n) {
        if (n < 0 || n > 64) { overflow = true; return; }
        if (n == 0) return;
        final long at = bits; bits += n;
        if (measuring) return;
        if ((bits + 7) / 8 > buffer.length) { overflow = true; return; }
        if (n < 64) value &= (1L << n) - 1;
        final int index = (int) (at >> 3); final int shift = (int) (at & 7);
        long word = value << shift;
        if (shift != 0) word |= buffer[index] & ((1L << shift) - 1);
        final int need = (shift + n + 7) / 8;
        for (int j = 0; j < need && j < 8; j++) buffer[index + j] = (byte) (word >> (j * 8));
        if (need > 8) buffer[index + 8] = (byte) (value >> (64 - shift));
    }
    public void putScalar(long lo, long hi, int packing, int bits, long baseLo, long baseHi) {
        if (packing == 1) { final long borrow = (lo < baseLo) ? 1 : 0; lo -= baseLo; hi -= baseHi + borrow; }
        put(lo, (int) java.lang.Math.min(bits, 64));
        if (bits > 64) put(hi, bits - 64);
    }
    public void align() { put(0, (int) ((-bits) & 7)); }
    public void raw(byte[] data, int length) {
        align();
        if (measuring) { bits += (long) length * 8; return; }
        final int at = (int) (bits / 8); bits += (long) length * 8;
        if (at > buffer.length - length) { overflow = true; return; }
        java.lang.System.arraycopy(data, 0, buffer, at, length);
    }
}

public static final class Reader {
    public byte[] buffer;
    public long offset;
    public TableVocabulary vocabulary;
    public Report report;
    public boolean err;
    public Reader(byte[] buffer, int startBytes, TableVocabulary vocabulary, Report report) {
        this.buffer = buffer; this.offset = startBytes * 8L;
        this.vocabulary = vocabulary; this.report = report;
    }
    public long get(int n) {
        if (n < 0 || n > 64 || offset < 0 || n > buffer.length * 8L - offset) { err = true; malformed(); return 0; }
        if (n == 0) return 0;
        final int at = (int) (offset >> 3); final int shift = (int) (offset & 7);
        final int need = (shift + n + 7) / 8;
        long value = 0;
        for (int j = 0; j < need && j < 8; j++) value |= (buffer[at + j] & 0xFFL) << (8 * j);
        value >>>= shift;
        if (need > 8) value |= (buffer[at + 8] & 0xFFL) << (64 - shift);
        if (n < 64) value &= (1L << n) - 1;
        offset += n;
        return value;
    }
    public long getScalar(TableVocabulary.Shape s) {
        final long lo = get((int) java.lang.Math.min(s.bits, 64));
        if (s.packing == 1) return lo + s.baseLo;
        return lo;
    }
    public long count(TableVocabulary.Shape s) {
        final long span = (s.kind == 14) ? s.max - s.min : s.max;
        final int w = bitCount(span);
        return get(w) + ((s.kind == 14) ? s.min : 0);
    }
    public boolean skip(long n) { if (n < 0 || n > buffer.length * 8L - offset) { err = true; malformed(); return false; } offset += n; return true; }
    public boolean align() { return get((int) ((8 - (offset & 7)) & 7)) == 0; }
    public void malformed() { if (report != null) report.malformed = true; }
}

public static int bitCount(long n) { int c = 0; while (n != 0) { c++; n >>= 1; } return c; }

public static void refuse(Report report, String reason) {
    if (report == null) return;
    report.refused = true; report.reason = reason; report.verdict = 1;
}

public static long batchOpen(Object[] values, byte[] data, TableVocabulary vocabulary, Report report) {
    if (data == null || data.length == 0) { if (report != null) report.malformed = true; return -1; }
    if ((data[0] & 0xFF) != 2) { refuse(report, "newer_form"); return -1; }
    if (vocabulary == null || !vocabulary.announced) { refuse(report, "no_vocabulary"); return -1; }
    if (data.length < 2) { if (report != null) report.malformed = true; return -1; }
    final long count = (data[1] & 0xFFL) + 1;
    if (count > values.length) {
        // "M ABOVE THE CALLER'S CAPACITY ... the reader sets the returned count
        // to the wire's M before it returns the refusal" (§3.3)
        refuse(report, "batch_too_large");
        return count;
    }
    return count;
}

public static Reader reader(byte[] data, int startBytes, TableVocabulary vocabulary, Report report) {
    return new Reader(data, startBytes, vocabulary, report);
}

public static boolean batchClose(Reader r) {
    if (!r.align() || r.offset != r.buffer.length * 8L) { r.malformed(); return false; }
    return true;
}

public static boolean skip(Reader r, TableVocabulary.Entry entry, int depth) {
    if (depth > 128) return false;
    return skipShape(r, entry.shape, entry.element, depth);
}

static boolean skipShape(Reader r, TableVocabulary.Shape s, TableVocabulary.Shape element, int depth) {
    switch (s.kind) {
        case 0: case 32: return true;
        case 30: { final long ref = r.get(r.vocabulary.refBits); return ref == 0 || r.vocabulary.name(ref) != 0; }
        case 13: return skipBody(r, depth + 1);
        case 12: case 33: {
            final long n = r.count(s);
            final int width = s.kind == 12 ? 8 : 16;
            if (s.kind == 12 && !r.align()) return false;
            return r.skip(n * width);
        }
        case 15: {
            final long ref = r.get(r.vocabulary.refBits);
            if (ref == 0) return true;
            final TableVocabulary.Entry arm = r.vocabulary.entry(ref);
            return arm != null && Long.compareUnsigned(arm.id, -3L) < 0 && arm.shape.kind != 0
                && skipShape(r, arm.shape, arm.element, depth + 1);
        }
        case 14: case 16: {
            final long n = r.count(s);
            if (s.kind == 14 && element != null && element.kind == 6 && !r.align()) return false;
            final int width = element == null ? -1 : (int) element.bits;
            final boolean fixed = element != null && element.kind != 12 && element.kind != 13 && element.kind != 14
                && element.kind != 15 && element.kind != 16 && element.kind != 30 && element.kind != 33
                && element.kind != 0 && element.kind != 32 && width >= 0;
            if (fixed) {
                int w = width; if (s.kind == 16) w += r.vocabulary.refBits;
                return r.skip(n * w);
            }
            for (long i = 0; i < n; i++) {
                if (s.kind == 16 && !r.skip(r.vocabulary.refBits)) return false;
                if (!skipShape(r, element, null, depth + 1)) return false;
            }
            return true;
        }
        default: return s.bits >= 0 && r.skip(s.bits);
    }
}

private static boolean skipBody(Reader r, int depth) {
    if (depth > 128) return false;
    while (true) {
        final long ref = r.get(r.vocabulary.refBits);
        if (ref == 0) return true;
        final TableVocabulary.Entry e = r.vocabulary.entry(ref);
        if (e == null || !skipShape(r, e.shape, e.element, depth)) return false;
    }
}
`
