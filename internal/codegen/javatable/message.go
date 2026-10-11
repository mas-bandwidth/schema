package javatable

// THE MESSAGE FORM's Java surface (docs/SPEC-TABLES.md §3.3), form byte 2: the
// batch verbs MeasureMessages, SaveMessages and LoadMessages over one announced
// vocabulary, emitted for `fixed table` roots — a message is a bitpacked body
// under a vocabulary announced once, so the shape has to be one the declaration
// fixes (§3.3).
//
// The Go backend (internal/codegen/gotable) and the C# backend
// (internal/codegen/cstable) are the references; the wire bytes are the C++
// reference's, pinned by testdata/conformance/tables/MANIFEST.txt. The bit
// order is the packet wire's: bit i of the stream is bit i%8 of byte i/8, low
// bit first (SPEC.md §4.3).
//
// Java forces one divergence, named at the site: the shared runtime is ONE
// FILE (TableMessage.java) with NESTED static classes, because a nested class
// needs no file of its own name, where a package-scope type would (javatable.go,
// "THE UNIT'S NAMESPACE IS THE PACKAGE"). Nothing here allocates on the read
// path: the caller owns every buffer.
import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// messageClass is the class one fixed root's message surface lands in:
// `<Root>Message`, beside the fixed form's `<Root>Fixed`.
func messageClass(name string) string { return ir.GoExportName(name) + "Message" }

func messageShapeLiteral(kind uint8, s ir.TableMessageShape) string {
	lo, hi := uint64(0), uint64(0)
	if s.Base != nil {
		v := new(big.Int).Set(s.Base)
		v.Mod(v, new(big.Int).Lsh(big.NewInt(1), 128))
		lo = v.Uint64()
		hi = new(big.Int).Rsh(v, 64).Uint64()
	}
	if kind < 18 {
		hi = 0
	}
	return fmt.Sprintf("new TableMessage.Shape(%d,%d,%d,0x%xL,0x%xL,%dL,%dL)",
		kind, s.Packing, ir.TableMessageValueBits(kind, s), lo, hi, s.Min, s.Max)
}

func messageEntryLiteral(e ir.TableVocabularyEntry) string {
	inner := "null"
	if e.Shape.Inner != nil {
		inner = messageShapeLiteral(e.Shape.Elem, *e.Shape.Inner)
	}
	return fmt.Sprintf("new TableMessage.Entry(0x%016xL,%s,%s)", e.Id, messageShapeLiteral(e.Kind, e.Shape), inner)
}

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

func tableMessageRuntime(u *ir.Unit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "public static final int batchMax = 256;\n")
	fmt.Fprintf(&b, "public static final int refBits = %d;\n", ir.TableMessageRefBits(len(ir.TableVocabulary(u))))
	fmt.Fprintf(&b, "public static final int entriesHere = %d;\n", len(ir.TableVocabulary(u)))
	fmt.Fprintf(&b, "public static final byte[] announcement = new byte[] {%s};\n", byteLiterals(ir.TableAnnouncement(u)))
	b.WriteString("public static final TableMessage.Entry[] entries = new TableMessage.Entry[] {\n")
	for _, e := range ir.TableVocabulary(u) {
		fmt.Fprintf(&b, "%s,\n", messageEntryLiteral(e))
	}
	b.WriteString("};\n")
	b.WriteString(messageRuntimeBody)
	return b.String()
}

func generateMessageFiles(u *ir.Unit) map[string][]byte {
	roots := fixedRoots(u)
	if len(roots) == 0 {
		return nil
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
		"TableMessage.java": javaFile(u, "the MESSAGE FORM's runtime (docs/SPEC-TABLES.md §3.3), form byte 2", tableMessageRuntime(u)),
	}
	for _, st := range order {
		g := &messageGen{unit: u}
		g.emitType(st, isRoot[st.Name])
		out[messageClass(st.Name)+".java"] = javaFile(u,
			fmt.Sprintf("%s under the message form (docs/SPEC-TABLES.md §3.3)", st.Name), g.b.String())
	}
	return out
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

func (g *messageGen) emitReset(st *ir.Struct) {
	g.pf("    static void reset(%s.Value v) {\n", fixedClass(st.Name))
	for _, f := range st.Fields {
		name := member(f)
		if f.Type.Optional {
			g.pf("        v.%sPresent = false;\n", name)
		}
		switch {
		case f.Type.Kind == ir.TBool:
			g.pf("        v.%s = false;\n", name)
		case f.Type.Kind == ir.TString || (f.Type.Kind == ir.TBytes && !f.Type.Pointer):
			g.pf("        v.%sLength = 0;\n", name)
		case f.Type.Kind == ir.TWString:
			g.pf("        v.%sLength = 0;\n", name)
		case f.Array != ir.ArrayNone || f.KeyEnum != "":
			for i := 0; i < int(f.ArrayBound); i++ {
				g.resetElement(messageElementField(f), fmt.Sprintf("v.%s[%d]", name, i))
			}
			if f.Array == ir.ArrayCounted {
				g.pf("        v.%sCount = 0;\n", name)
			}
		case isStructRef(f.Type):
			g.pf("        %s.reset(v.%s);\n", messageClass(f.Type.Name), name)
		case isUnionRef(f.Type):
			g.pf("        v.%s.type = 0;\n", name)
		case enumRef(f) != nil:
			g.pf("        v.%s = 0;\n", name)
		default:
			g.pf("        v.%s = 0;\n", name)
		}
	}
	g.pf("    }\n")
}

func (g *messageGen) resetElement(f *ir.Field, expr string) {
	switch {
	case f.Type.Kind == ir.TBool:
		g.pf("        %s = false;\n", expr)
	case f.Type.Kind == ir.TString || (f.Type.Kind == ir.TBytes && !f.Type.Pointer):
		// a bytes(N) element is a plain u8: no length companion
		g.pf("        %s = 0;\n", expr)
	case isStructRef(f.Type):
		g.pf("        %s.reset(%s);\n", messageClass(f.Type.Name), expr)
	case isUnionRef(f.Type):
		g.pf("        %s.type = 0;\n", expr)
	case enumRef(f) != nil:
		g.pf("        %s = 0;\n", expr)
	default:
		g.pf("        %s = 0;\n", expr)
	}
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
	g.pf("        w.align();\n        if (w.overflow) return -1;\n        return 2 + w.bits / 8;\n    }\n\n")
	g.pf("    public static long loadMessages(%s.Value[] values, byte[] data, TableMessage.Vocabulary vocabulary, TableMessage.Report report) {\n", v)
	g.pf("        final long count = TableMessage.batchOpen(values, data, vocabulary, report);\n        if (count < 0) return -1;\n")
	g.pf("        if (report != null && report.refused) return count;\n")
	g.pf("        final TableMessage.Reader r = TableMessage.reader(data, 2, report);\n")
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
		g.pf("            if (%s) { w.put(%d, TableMessage.refBits); %s }\n", g.rides(f, expr), messageSlot(g.unit, entry), g.value(f, expr))
		if f.Type.Optional {
			g.pf("            }\n")
		}
		g.pf("        }\n")
	}
	g.pf("        w.put(0, TableMessage.refBits);\n        return !w.overflow;\n    }\n")
}

func (g *messageGen) rides(f *ir.Field, expr string) string {
	switch f.Type.Kind {
	case ir.TBool:
		return expr
	case ir.TFloat32, ir.TFloat64:
		return expr + " != 0f"
	case ir.TInt, ir.TFixed:
		return expr + " != 0"
	case ir.TString, ir.TBytes, ir.TWString:
		return expr + "Length != 0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			return expr + " != 0"
		case *ir.Flags:
			return expr + " != 0"
		case *ir.Struct:
			return "true"
		case *ir.Union:
			return expr + ".type != 0"
		}
	}
	return "true"
}

// byteLiterals spells one byte slice as Java array literals.
func byteLiterals(b []byte) string {
	var s strings.Builder
	for i, v := range b {
		if i > 0 {
			s.WriteString(",")
		}
		fmt.Fprintf(&s, "(byte) 0x%02x", v)
	}
	return s.String()
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
			fmt.Fprintf(&b, " case %d: slot = TableMessage.nameSlot(0x%016xL); break;", i+1, ir.TableWireId(v))
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
	fmt.Fprintf(b, "w.putScalar(%s, %s, %d, %d, %d, 0x%xL, 0x%xL);",
		lo, hi, kind, shape.Packing, ir.TableMessageValueBits(kind, shape),
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
	fmt.Fprintf(b, " for (int i = 0; i < %d; i++) { if (%s) { long slot = TableMessage.nameSlot(0x%016xL); if (slot == 0) return false; w.put(slot, TableMessage.refBits); %s } } }",
		f.ArrayBound, g.rides(el, expr+"[i]"), ir.TableWireId(f.KeyEnumRef.Variants[0]), g.value(el, expr+"[i]"))
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
	g.pf("    static boolean readBody(TableMessage.Reader r, TableMessage.Vocabulary vocabulary, %s.Value v) {\n", fixedClass(st.Name))
	g.pf("        reset(v);\n")
	g.pf("        while (true) {\n")
	g.pf("            final long ref = r.get(vocabulary.refBits);\n            if (ref == 0) return true;\n")
	g.pf("            final TableMessage.Entry entry = vocabulary.entry(ref);\n            if (entry == null || entry.id >= -3L) { r.malformed(); return false; }\n")
	g.pf("            switch ((int) entry.id) {\n")
	for _, f := range st.Fields {
		mine := ir.TableFieldEntry(f)
		g.pf("                case 0x%016x:\n", mine.Id)
		g.emitReadValue(f, "v."+member(f), "entry", "                    ")
		if f.Type.Optional {
			g.pf("                    v.%sPresent = true;\n", member(f))
		}
		g.pf("                    break;\n")
	}
	g.pf("                default:\n                    if (!TableMessage.skip(r, vocabulary, entry, 0)) { r.malformed(); return false; }\n                    break;\n")
	g.pf("            }\n        }\n    }\n")
}

func (g *messageGen) emitReadValue(f *ir.Field, expr, entry, ind string) {
	switch {
	case f.Type.Kind == ir.TBool:
		g.pf("%s%s = r.get(1) != 0;\n", ind, expr)
	case f.Type.Kind == ir.TString:
		g.pf("%s{ final long n = r.count(%s.shape); if (!r.align()) return false; final int start = (int) r.offset / 8; if (!r.skip(n * 8)) return false; final int kept = (int) java.lang.Math.min(n, %d); if (kept < n) r.report.clamped++; java.lang.System.arraycopy(r.buffer, start, %s, 0, kept); %sLength = kept; }\n", ind, entry, f.Type.Size, expr, expr)
	case f.Type.Kind == ir.TBytes && !f.Type.Pointer:
		g.pf("%s{ final long n = r.count(%s.shape); if (!r.align()) return false; final int start = (int) r.offset / 8; if (!r.skip(n * 8)) return false; final int kept = (int) java.lang.Math.min(n, %d); if (kept < n) r.report.clamped++; java.lang.System.arraycopy(r.buffer, start, %s, 0, kept); %sLength = kept; }\n", ind, entry, f.Type.Size, expr, expr)
	case f.Type.Kind == ir.TWString:
		g.pf("%s{ final long n = r.count(%s.shape); final int kept = (int) java.lang.Math.min(n, %d); if (kept < n) r.report.clamped++; for (int i = 0; i < n; i++) { final int u = (int) r.get(16); if (i < kept) %s[i] = (char) u; } %sLength = kept; }\n", ind, entry, f.Type.Size, expr, expr)
	case f.KeyEnum != "":
		g.keyedRead(f, expr, entry, ind)
	case f.Array != ir.ArrayNone:
		g.arrayRead(f, expr, entry, ind)
	case isStructRef(f.Type):
		g.pf("%sif (!%s.readBody(r, vocabulary, %s)) return false;\n", ind, messageClass(f.Type.Name), expr)
	case isUnionRef(f.Type):
		g.unionRead(f, expr, ind)
	case enumRef(f) != nil:
		g.enumRead(f, expr, ind)
	default:
		g.scalarRead(f, expr, entry, ind)
	}
}

func (g *messageGen) scalarRead(f *ir.Field, expr, entry, ind string) {
	kind := ir.TableFieldEntry(f).Kind
	switch kind {
	case 10:
		g.pf("%s%s = java.lang.Float.intBitsToFloat((int) r.getScalar(%s.shape));\n", ind, expr, entry)
	case 11:
		g.pf("%s%s = java.lang.Double.longBitsToDouble(r.getScalar(%s.shape));\n", ind, expr, entry)
	default:
		g.pf("%s%s = (%s) r.getScalar(%s.shape);\n", ind, expr, fixedScalarType(f.Type), entry)
	}
}

func (g *messageGen) enumRead(f *ir.Field, expr, ind string) {
	en := enumRef(f)
	g.pf("%s{ final long ref = r.get(vocabulary.refBits); if (ref == 0) { %s = 0; } else { final long id = vocabulary.name(ref); switch ((int) id) {\n", ind, expr)
	for i, v := range en.Variants {
		g.pf("%s    case 0x%016x: %s = %d; break;\n", ind, ir.TableWireId(v), expr, i+1)
	}
	g.pf("%s    default: r.report.unknown++; break;\n%s} } }\n", ind, ind)
}

func (g *messageGen) arrayRead(f *ir.Field, expr, entry, ind string) {
	shape := entry + ".shape"
	el := messageElementField(f)
	bound := f.ArrayBound
	counted := f.Array == ir.ArrayCounted
	companion := expr + "Count"
	g.pf("%s{ final long n = r.count(%s); ", ind, shape)
	if ir.TableMessageAligns(ir.TableFieldEntry(f).Kind, ir.TableFieldEntry(f).Shape) {
		g.pf("if (!r.align()) return false; ")
	}
	g.pf("final int keep = (int) java.lang.Math.min(n, %d); if (keep < n) r.report.clamped++; for (int i = 0; i < keep; i++) { ", bound)
	g.emitReadValue(el, expr+"[i]", entry+".element", ind+"    ")
	g.pf("} ")
	if counted {
		g.pf("%s = keep; ", companion)
	}
	g.pf("}\n")
}

func (g *messageGen) keyedRead(f *ir.Field, expr, entry, ind string) {
	el := messageElementField(f)
	g.pf("%s{ final long n = r.count(%s.shape); for (int i = 0; i < n; i++) { final long ref = r.get(vocabulary.refBits); final long id = vocabulary.name(ref); switch ((int) id) {\n", ind, entry)
	for i, v := range f.KeyEnumRef.Variants {
		g.pf("%s    case 0x%016x: ", ind, ir.TableWireId(v))
		g.emitReadValue(el, expr+"["+fmt.Sprint(i)+"]", entry+".element", ind+"        ")
		g.pf(" break;\n")
	}
	g.pf("%s    default: r.report.unknown++; if (!TableMessage.skip(r, vocabulary, %s.element, 0)) { r.malformed(); return false; } break;\n%s} } }\n", ind, entry, ind)
}

func (g *messageGen) unionRead(f *ir.Field, expr, ind string) {
	un := f.Type.Ref.(*ir.Union)
	g.pf("%s{ final long ref = r.get(vocabulary.refBits); if (ref == 0) { %s.type = 0; } else { final TableMessage.Entry arm = vocabulary.entry(ref); switch ((int) arm.id) {\n", ind, expr)
	for i, v := range un.Variants {
		arm := ir.TableArmEntry(v)
		g.pf("%s    case 0x%016x: %s.type = %d;", ind, arm.Id, expr, i+1)
		if !v.Void() {
			g.pf(" ")
			g.emitReadValue(v.F, g.unionArmExpr(un, v, expr), "arm", ind+"        ")
			g.pf(" break;")
		} else {
			g.pf(" break;")
		}
		g.pf("\n")
	}
	g.pf("%s    default: %s.type = 0; r.report.unknown++; if (!TableMessage.skip(r, vocabulary, arm, 0)) { r.malformed(); return false; } break;\n%s} } }\n", ind, expr, ind)
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

func isUnionRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	_, ok := t.Ref.(*ir.Union)
	return ok
}

// messageRuntimeBody is the Java runtime shared by every generated unit.
const messageRuntimeBody = `public static final class Report {
    public long unknown, kindMismatch, widened, clamped, duplicate;
    public boolean malformed, refused;
    public String reason;
    public int verdict;
}

public static final class Shape {
    public final int kind, packing, bits;
    public final long baseLo, baseHi, min, max;
    public Shape(int kind, int packing, int bits, long baseLo, long baseHi, long min, long max) {
        this.kind = kind; this.packing = packing; this.bits = bits; this.baseLo = baseLo;
        this.baseHi = baseHi; this.min = min; this.max = max;
    }
}

public static final class Entry {
    public final long id;
    public final Shape shape;
    public final Shape element;
    public Entry(long id, Shape shape, Shape element) {
        this.id = id; this.shape = shape;
        this.element = element == null ? new Shape(0, 0, 0, 0, 0, 0, 0) : element;
    }
}

public static final class Vocabulary {
    public Entry[] entries;
    public int count;
    public int refBits;
    public long buildVersion;
    public boolean announced, refused;
    public long maxBytes = 65536;
    public Entry entry(long ref) {
        if (ref <= 0 || ref > count) return null;
        return entries[(int) ref - 1];
    }
    public long name(long ref) {
        final Entry e = entry(ref);
        return (e != null && e.shape.kind == 0 && e.id < -3L) ? e.id : 0;
    }
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
    public void putScalar(long lo, long hi, int kind, int packing, int bits, long baseLo, long baseHi) {
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
    public Report report;
    public Reader(byte[] buffer, int startBytes, Report report) { this.buffer = buffer; this.offset = startBytes * 8L; this.report = report; }
    public long get(int n) {
        if (n < 0 || n > 64 || offset < 0 || n > buffer.length * 8L - offset) { malformed(); return 0; }
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
    public long getScalar(Shape s) {
        final long lo = get((int) java.lang.Math.min(s.bits, 64));
        if (s.packing == 1) return lo + s.baseLo;
        return lo;
    }
    public long count(Shape s) {
        final long span = (s.kind == 14) ? s.max - s.min : s.max;
        final int w = bitCount(span);
        return get(w) + ((s.kind == 14) ? s.min : 0);
    }
    public boolean skip(long n) { if (n < 0 || n > buffer.length * 8L - offset) return false; offset += n; return true; }
    public boolean align() { return get((int) ((8 - (offset & 7)) & 7)) == 0; }
    public void malformed() { if (report != null) report.malformed = true; }
}

public static int bitCount(long n) { int c = 0; while (n != 0) { c++; n >>= 1; } return c; }

public static void refuse(Report report, String reason) {
    if (report == null) return;
    report.refused = true; report.reason = reason; report.verdict = 1;
}

public static long batchOpen(Object[] values, byte[] data, Vocabulary vocabulary, Report report) {
    if (data.length == 0) { if (report != null) report.malformed = true; return -1; }
    if (data[0] != 2) { refuse(report, "newer_form"); return -1; }
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

public static Reader reader(byte[] data, int startBytes, Report report) { return new Reader(data, startBytes, report); }

public static boolean batchClose(Reader r) {
    if (!r.align() || r.offset != r.buffer.length * 8L) { r.malformed(); return false; }
    return true;
}

public static long nameSlot(long id) {
    for (int i = 0; i < entriesHere; i++) {
        final Entry e = entries[i];
        if (e.id == id && e.shape.kind == 0) return i + 1;
    }
    return 0;
}

public static boolean skip(Reader r, Vocabulary vocabulary, Entry entry, int depth) {
    if (depth > 128) return false;
    final Shape s = entry.shape;
    switch (s.kind) {
        case 0: case 32: return true;
        case 30: { final long ref = r.get(vocabulary.refBits); return ref == 0 || vocabulary.name(ref) != 0; }
        case 13: return skipBody(r, vocabulary, depth + 1);
        case 12: case 33: {
            final long n = r.count(s);
            final int width = s.kind == 12 ? 8 : 16;
            if (s.kind == 12 && !r.align()) return false;
            return r.skip(n * width);
        }
        case 15: {
            final long ref = r.get(vocabulary.refBits);
            if (ref == 0) return true;
            final Entry arm = vocabulary.entry(ref);
            return arm != null && arm.id < -3L && arm.shape.kind != 0 && skip(r, vocabulary, arm, depth + 1);
        }
        case 14: case 16: {
            final long n = r.count(s);
            if (s.kind == 14 && entry.element.kind == 6 && !r.align()) return false;
            if (entry.element.bits >= 0) { int width = entry.element.bits; if (s.kind == 16) width += vocabulary.refBits; return r.skip(n * width); }
            for (long i = 0; i < n; i++) { if (s.kind == 16 && !r.skip(vocabulary.refBits)) return false; if (!skip(r, vocabulary, new Entry(entry.element.id, entry.element, null), depth + 1)) return false; }
            return true;
        }
        default: return s.bits >= 0 && r.skip(s.bits);
    }
}

private static boolean skipBody(Reader r, Vocabulary vocabulary, int depth) {
    if (depth > 128) return false;
    while (true) {
        final long ref = r.get(vocabulary.refBits);
        if (ref == 0) return true;
        final Entry e = vocabulary.entry(ref);
        if (e == null || !skip(r, vocabulary, e, depth)) return false;
    }
}
`
