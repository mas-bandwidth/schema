// TABLE-wire storage, codec and descriptor emission for Java
// (docs/SPEC-TABLES.md), mirroring internal/codegen/cpptable — the reference —
// and following internal/codegen/cstable, the worked managed-language port.
// Readers restore declared defaults then overlay, skip unknown ids, skip kind
// mismatches, clamp out-of-range values, and count every event.
package javatable

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

var (
	minInt32 = big.NewInt(-1 << 31)
	maxInt32 = big.NewInt(1<<31 - 1)
	minInt64 = new(big.Int).Lsh(big.NewInt(-1), 63)
	maxInt64 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 63), big.NewInt(1))
)

func fitsInt32(v *big.Int) bool { return v.Cmp(minInt32) >= 0 && v.Cmp(maxInt32) <= 0 }

// javaIntLit renders an integer as a Java literal — the packet emitter's own
// rule, so one number is spelled one way across a unit.
func javaIntLit(v *big.Int) string {
	if fitsInt32(v) {
		return v.String()
	}
	if v.Cmp(minInt64) == 0 {
		return "0x8000000000000000L"
	}
	if v.Cmp(minInt64) > 0 && v.Cmp(maxInt64) <= 0 {
		return v.String() + "L"
	}
	u := new(big.Int).Set(v)
	if u.Sign() < 0 {
		u.Add(u, new(big.Int).Lsh(big.NewInt(1), 64))
	}
	return fmt.Sprintf("0x%xL", u)
}

// narrowLit renders an integer for a narrow-typed context: the plain decimal
// when the value is representable, else the bit pattern behind an explicit
// cast — (byte) 200 is -56, the storage pattern of unsigned 200.
func narrowLit(typ string, v *big.Int) string {
	switch typ {
	case "byte":
		if v.IsInt64() && v.Int64() >= -128 && v.Int64() <= 127 {
			return v.String()
		}
		return fmt.Sprintf("(byte) %s", new(big.Int).And(v, big.NewInt(0xff)).String())
	case "short":
		if v.IsInt64() && v.Int64() >= -32768 && v.Int64() <= 32767 {
			return v.String()
		}
		return fmt.Sprintf("(short) %s", new(big.Int).And(v, big.NewInt(0xffff)).String())
	case "int":
		if fitsInt32(v) {
			return v.String()
		}
		return fmt.Sprintf("(int) %s", javaIntLit(new(big.Int).And(v, big.NewInt(0xffffffff))))
	}
	return javaIntLit(v)
}

// formatFloat64 / formatFloat32 render float literals at the storage type's
// own precision, so the emitted clamp bounds and defaults are exactly the
// values the runtime compares against.
func formatFloat64(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func formatFloat32(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 32)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s + "f"
}

func intJavaType(width int) string {
	switch {
	case width <= 8:
		return "byte"
	case width <= 16:
		return "short"
	case width <= 32:
		return "int"
	}
	return "long"
}

func enumJavaType(storageBits int) string { return intJavaType(storageBits) }

func tagJavaType(max int64) string {
	switch {
	case max <= 0x7f:
		return "byte"
	case max <= 0x7fff:
		return "short"
	case max <= 0x7fffffff:
		return "int"
	}
	return "long"
}

// javaFieldType maps a field type to its Java storage spelling, mirroring the
// packet emitter's conventions so closure classes from <Base>.java and table
// classes from <Base>Table.java read as one family.
func (g *tableGen) javaFieldType(t ir.FieldType) string {
	switch t.Kind {
	case ir.TInt:
		return intJavaType(t.Width)
	case ir.TBits:
		if t.Width <= 32 {
			return "int"
		}
		return "long"
	case ir.TBool:
		return "boolean"
	case ir.TFloat32:
		return "float"
	case ir.TFloat64:
		return "double"
	case ir.TNamed:
		if _, isFlags := t.Ref.(*ir.Flags); isFlags {
			// flags-typed fields store a plain long of masks, the packet
			// emitter's spelling exactly (SPEC §4.2)
			return "long"
		}
		if e, isEnum := t.Ref.(*ir.Enum); isEnum {
			return enumJavaType(e.StorageBits)
		}
		return g.ref(t.Name)
	}
	return "/* ? */"
}

// zeroLit is the element zero of a scalar array — the literal java.util.Arrays
// .fill's overload for that element type takes.
func zeroLit(typ string) string {
	switch typ {
	case "boolean":
		return "false"
	case "byte":
		return "(byte) 0"
	case "short":
		return "(short) 0"
	case "int":
		return "0"
	case "long":
		return "0L"
	case "float":
		return "0.0f"
	case "double":
		return "0.0"
	}
	return "null"
}

// cast narrows a value of `from` into `to`, and says nothing when the
// assignment already widens.
func cast(to, from string) string {
	if to == from {
		return ""
	}
	switch to {
	case "byte", "short":
		return "(" + to + ") "
	case "int":
		if from == "long" {
			return "(int) "
		}
	}
	return ""
}

// tableStorageRange is the inclusive range an integer storage of the given
// width can hold.
func tableStorageRange(signed bool, bits int) (*big.Int, *big.Int) {
	one := big.NewInt(1)
	if signed {
		hi := new(big.Int).Lsh(one, uint(bits-1))
		return new(big.Int).Neg(hi), new(big.Int).Sub(hi, one)
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one)
}

// tableClampEnds answers which ends of a declared min/max range a read can
// actually clamp at. The decode local holds the wire kind's whole range, so a
// bound sitting ON that width's limit is a comparison no decoded value can
// satisfy and the emitter drops it — the same "this check cannot fire" test the
// bits(N) width clamp already applies when N is the storage width.
func tableClampEnds(f *ir.Field, widthBytes int) (low, high bool) {
	signed := f.Type.Kind == ir.TInt && f.Type.Signed
	lo, hi := tableStorageRange(signed, widthBytes*8)
	return f.IntMin.Cmp(lo) > 0, f.IntMax.Cmp(hi) < 0
}

// fieldDefaultExpr renders the Java expression a field's default compares
// against on the write side (elision) — identical values to the storage
// initializers, so measure, save and the reader's prefill agree.
func (g *tableGen) fieldDefaultExpr(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TFloat32:
		if f.HasDefault {
			return formatFloat32(f.DefFloat)
		}
		return "0.0f"
	case ir.TFloat64:
		if f.HasDefault {
			return formatFloat64(f.DefFloat)
		}
		return "0.0"
	case ir.TInt, ir.TBits:
		typ := g.javaFieldType(f.Type)
		if f.HasDefault && f.DefInt != nil {
			return narrowLit(typ, f.DefInt)
		}
		return zeroLit(typ)
	case ir.TNamed:
		if _, isEnum := f.Type.Ref.(*ir.Enum); isEnum {
			if f.HasDefault && f.DefVariant != "" {
				return g.packetRef(f.Type.Name) + "." + javaName(f.DefVariant)
			}
			return g.packetRef(f.Type.Name) + ".none"
		}
		if _, isFlags := f.Type.Ref.(*ir.Flags); isFlags {
			return "0L"
		}
	}
	return "0"
}

// member is a field's Java storage member name — the lowerCamel mapping the
// packet emitter uses, so one field is spelled one way across a unit.
func member(f *ir.Field) string { return javaName(f.Name) }

// ---- storage (table declarations only; closure types come from <Base>.java) ----

func (g *tableGen) emitTableClass(st *ir.Struct) {
	g.tf("%s", ir.DocComment(st.Doc, "", "//"))
	g.tf("// table %s — TABLE-wire storage: public fields, every buffer allocated at\n", st.Name)
	g.tf("// construction, declared defaults in the field initializers (docs/SPEC-TABLES.md)\n")
	g.tf("public static final class %s {\n", st.Name)
	prevGuard := ""
	for _, f := range st.Fields {
		if f.Guard != prevGuard {
			if f.Guard != "" {
				g.tf("\n    // %s — guarded fields stay off the wire when the guard says so;\n", f.Guard)
				g.tf("    // a read's restored defaults stand in for the untaken side\n")
			} else {
				g.tf("\n")
			}
			prevGuard = f.Guard
		}
		g.tf("%s", ir.DocComment(f.Doc, "    ", "//"))
		g.emitTableStorageField(f)
	}
	g.emitElementConstructor(st)
	g.tf("}\n\n")
}

func (g *tableGen) emitTableStorageField(f *ir.Field) {
	name := member(f)
	typ := g.javaFieldType(f.Type)
	switch {
	case f.Type.Kind == ir.TString:
		g.tf("    public final byte[] %s = new byte[%d]; // string(%s): max length, used length beside it\n",
			name, f.Type.Size, ir.RenderExpr(f.Type.SizeExpr))
		g.tf("    public int %sLength;\n", name)
	case f.Type.Kind == ir.TBytes:
		g.tf("    public final byte[] %s = new byte[%d]; // bytes(%s): fixed buffer, used length beside it\n",
			name, f.Type.Size, ir.RenderExpr(f.Type.SizeExpr))
		g.tf("    public int %sLength;\n", name)
	case f.KeyEnum != "":
		// ONE SLOT PER NAMED VARIANT, the key k at index k-1: nothing is stored
		// for None, and the accessor pair below is the only place the shift
		// appears. Every named slot exists, so there is no count companion, and
		// the extent is the key enum's own max — nothing outside the array
		// names its size (docs/SPEC-TABLES.md §2.4).
		g.tf("    public final %s[] %s = new %s[%s.max]; // [%s]: one slot per named variant, keyed by the value\n",
			typ, name, typ, g.packetRef(f.KeyEnum), f.KeyEnum)
		g.tf("    /** the slot the KEY names — never the storage index, and never None (§2.4). */\n")
		g.tf("    public %s %s(int key) { return %s[TableKeyed.slot(key)]; }\n", typ, name, name)
		if !isClassRef(f.Type) {
			g.tf("    public void %s(int key, %s value) { %s[TableKeyed.slot(key)] = value; }\n", name, typ, name)
		}
	case f.Array == ir.ArrayFixed:
		g.tf("    public final %s[] %s = new %s[%d];\n", typ, name, typ, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		g.tf("    public final %s[] %s = new %s[%d]; // used count beside it; count in [0, %d]\n",
			typ, name, typ, f.ArrayBound, f.ArrayBound)
		g.tf("    public int %sCount;\n", name)
	default:
		if isClassRef(f.Type) {
			// pre-allocated at construction — the storage principle: nothing
			// heap-allocates per value after it exists
			g.tf("    public final %s %s = new %s();\n", typ, name, typ)
		} else {
			init := ""
			if f.HasDefault || enumRef(f) != nil {
				init = " = " + g.fieldDefaultExpr(f)
			}
			g.tf("    public %s %s%s;\n", typ, name, init)
		}
	}
	if f.Type.Optional {
		// `?T` — the value plus its presence bool, and nothing else: the holder
		// stays a fixed-size record (docs/SPEC-TABLES.md §2.3). PRESENCE, not
		// content, decides whether the field rides.
		g.tf("    public boolean %sPresent; // ?%s: absent until set\n", name, tableFieldTypeName(f))
	}
}

// keyedSlots renders a keyed array's RAW slot storage — in Java the storage IS
// the array, on a table and on a closure `type` alike, because Java has no
// generic container that could hold a primitive slot without boxing
// (TableKeyed's comment states the whole of that divergence).
func (g *tableGen) keyedSlots(access string, f *ir.Field) string { return access + member(f) }

// emitElementConstructor pre-allocates the element instances of class-typed
// arrays: every buffer exists at construction, so the read path allocates
// nothing.
func (g *tableGen) emitElementConstructor(st *ir.Struct) {
	var elems []*ir.Field
	for _, f := range st.Fields {
		if (f.Array != ir.ArrayNone || f.KeyEnum != "") && isClassRef(f.Type) {
			elems = append(elems, f)
		}
	}
	if len(elems) == 0 {
		return
	}
	g.tf("\n    public %s() {\n", st.Name)
	for _, f := range elems {
		base := member(f)
		g.tf("        for (int i = 0; i < %s.length; i++) {\n", base)
		g.tf("            %s[i] = new %s();\n        }\n", base, g.ref(f.Type.Name))
	}
	g.tf("    }\n")
}

// isClassRef reports a named reference whose Java storage is a class instance:
// a generated struct/table class or a union.
func isClassRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	switch t.Ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}

// ---- enum identity on the table wire (docs/SPEC-TABLES.md §5) ----

func enumRef(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

// enumWiden lifts an enum-typed storage read to the unsigned long the identity
// functions take: Java's storage is the same-width SIGNED type, so a variant
// past the signed half would otherwise arrive negative.
func enumWiden(expr string, e *ir.Enum) string {
	switch enumJavaType(e.StorageBits) {
	case "byte":
		return "(" + expr + " & 0xffL)"
	case "short":
		return "(" + expr + " & 0xffffL)"
	case "int":
		return "(" + expr + " & 0xffffffffL)"
	}
	return expr
}

// emitEnumIdentity emits TableEnumId / TableEnumValue: one static method per
// enum in the unit's table closure, in ONE file each, because Java's unit scope
// is the package and a second definition anywhere in it is a duplicate class.
func emitEnumIdentity(u *ir.Unit, enums []*ir.Enum, ids bool) []byte {
	var b strings.Builder
	if ids {
		b.WriteString("// An enum value -> its TABLE-wire variant id (docs/SPEC-TABLES.md §5): a value\n")
		b.WriteString("// rides as the u16 hash of its VARIANT NAME, so a variant may be added\n")
		b.WriteString("// anywhere, removed, or reordered and old data still reads. None is the one\n")
		b.WriteString("// reserved id, 0, and -1 is \"no variant names this value\" — a value with no\n")
		b.WriteString("// wire identity, which measure and save refuse rather than writing None over.\n")
		b.WriteString("public final class TableEnumId {\n")
		b.WriteString("    private TableEnumId() {}\n")
		for _, e := range enums {
			fmt.Fprintf(&b, "\n    public static int %s(long value) {\n", javaName(e.Name))
			b.WriteString("        if (value == 0L) { return 0; }\n")
			for i, v := range e.Variants {
				// variants pack DENSE FROM 1, None being 0 (SPEC §4.2)
				fmt.Fprintf(&b, "        if (value == %dL) { return 0x%04x; }\n", i+1, ir.VariantId(v))
			}
			b.WriteString("        return -1; // no variant names this value: no wire identity\n")
			b.WriteString("    }\n")
		}
		b.WriteString("}\n")
		return javaFile(u, "TableEnumId", "an enum value -> its table-wire variant id (docs/SPEC-TABLES.md §5).", b.String())
	}
	b.WriteString("// A TABLE-wire variant id -> its enum value (docs/SPEC-TABLES.md §5). 0 is None,\n")
	b.WriteString("// and -1 is an id this build cannot name — counted unknown by the reader and\n")
	b.WriteString("// read as None, exactly as an unknown FIELD id is skipped and counted.\n")
	b.WriteString("public final class TableEnumValue {\n")
	b.WriteString("    private TableEnumValue() {}\n")
	for _, e := range enums {
		fmt.Fprintf(&b, "\n    public static long %s(int id) {\n", javaName(e.Name))
		b.WriteString("        switch (id) {\n")
		b.WriteString("            case 0: return 0L;\n")
		for i, v := range e.Variants {
			fmt.Fprintf(&b, "            case 0x%04x: return %dL;\n", ir.VariantId(v), i+1)
		}
		b.WriteString("            default: return -1L;\n")
		b.WriteString("        }\n    }\n")
	}
	b.WriteString("}\n")
	return javaFile(u, "TableEnumValue", "a table-wire variant id -> its enum value (docs/SPEC-TABLES.md §5).", b.String())
}

// ---- guards ----

// tableGuardExprs composes each guarded field's branch condition against the
// Java storage members ("value.active && !value.hasTarget").
func tableGuardExprs(st *ir.Struct) map[string]string { return guardWalk(st, true) }

// tableGuardStrings is the schema-facing twin for the reflection descriptors
// ("at_rest", "!at_rest", "active && has_target").
func tableGuardStrings(st *ir.Struct) map[string]string { return guardWalk(st, false) }

func guardWalk(st *ir.Struct, java bool) map[string]string {
	name := func(cond string) string {
		if java {
			return "value." + javaName(cond)
		}
		return cond
	}
	guards := map[string]string{}
	var walk func(items []ir.Item, cond string)
	walk = func(items []ir.Item, cond string) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if cond != "" {
					guards[item.F.Name] = cond
				}
			case *ir.Branch:
				pos, neg := name(item.Cond), "!"+name(item.Cond)
				if item.Neg {
					pos, neg = neg, pos
				}
				and := func(a, b string) string {
					if a == "" {
						return b
					}
					return a + " && " + b
				}
				walk(item.Then, and(cond, pos))
				walk(item.Else, and(cond, neg))
			}
		}
	}
	walk(st.Items, "")
	return guards
}

// ---- reset: the reader's prefill ----

// emitTableReset restores a value's declared defaults IN PLACE — the Java twin
// of the C++ reader's placement-new prefill, and in place on purpose: reusing
// the caller's buffers is what keeps the read path free of allocation.
//
// The name is NAME-FIRST, `<name>Reset`, which is the spelling §11 already
// claims for every closure member and the one Java's own rule wants. C++ needs
// a verb-first overload set because its arena's generic Alloc reaches a node's
// declared defaults by argument-dependent lookup; Java has neither the arena
// nor ADL, so nothing here has to be overloaded and no family name is minted.
func (g *tableGen) emitTableReset(st *ir.Struct) {
	g.pf("// %s restores %s's declared defaults in place, reusing every buffer the\n", method(st.Name, "Reset"), st.Name)
	g.pf("// value already owns. The reader calls it before overlaying.\n")
	g.pf("public static void %s(%s value) {\n", method(st.Name, "Reset"), g.ref(st.Name))
	if len(st.Fields) == 0 {
		g.pf("    // empty type: nothing to restore\n")
	}
	for _, f := range st.Fields {
		g.emitTableResetField(f)
		if f.Type.Optional {
			g.pf("    value.%sPresent = false;\n", member(f))
		}
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitTableResetField(f *ir.Field) {
	name := member(f)
	switch {
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("    java.util.Arrays.fill(value.%s, (byte) 0);\n", name)
		g.pf("    value.%sLength = 0;\n", name)
	case (f.Array != ir.ArrayNone || f.KeyEnum != "") && isClassRef(f.Type):
		base := g.keyedSlots("value.", f)
		g.pf("    for (int i = 0; i < %s.length; i++) {\n", base)
		if un, isUnion := f.Type.Ref.(*ir.Union); isUnion {
			g.pf("        %s[i].type = %s.none;\n", base, g.tagRef(un.Name))
		} else {
			g.pf("        %s(%s[i]);\n", g.call(f.Type.Name, "Reset"), base)
		}
		g.pf("    }\n")
		if f.Array == ir.ArrayCounted {
			g.pf("    value.%sCount = 0;\n", name)
		}
	case f.Array != ir.ArrayNone || f.KeyEnum != "":
		base := g.keyedSlots("value.", f)
		g.pf("    java.util.Arrays.fill(%s, %s);\n", base, zeroLit(g.javaFieldType(f.Type)))
		if f.Array == ir.ArrayCounted {
			g.pf("    value.%sCount = 0;\n", name)
		}
	default:
		if un, isUnion := f.Type.Ref.(*ir.Union); isUnion && f.Type.Kind == ir.TNamed {
			// the tag is the whole reset: an arm zero-establishes when the
			// reader selects it, exactly as the packet reader does
			g.pf("    value.%s.type = %s.none;\n", name, g.tagRef(un.Name))
			return
		}
		if isClassRef(f.Type) {
			g.pf("    %s(value.%s);\n", g.call(f.Type.Name, "Reset"), name)
			return
		}
		g.pf("    value.%s = %s;\n", name, g.fieldDefaultExpr(f))
	}
}

// ---- measure ----

// emitTableMeasure emits <X>Measure: the EXACT encoded size of a value, writing
// nothing — the parallel-generation lever. Mirrors <X>SaveBody's elision
// decisions branch for branch: for any value, Save writes exactly this many
// bytes into a buffer of exactly this size. A value violating its storage
// invariants measures as -1, exactly as the write side refuses it.
func (g *tableGen) emitTableMeasure(st *ir.Struct) {
	g.pf("public static long %s(%s value) {\n", method(st.Name, "Measure"), g.ref(st.Name))
	g.pf("    long bytes = 2; // terminator\n")
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("    if (%s) {\n", cond)
			g.indent = "    "
			g.emitTableMeasureField(f)
			g.indent = ""
			g.pf("    }\n")
			continue
		}
		g.emitTableMeasureField(f)
	}
	g.pf("    return bytes;\n}\n\n")
}

func (g *tableGen) emitTableMeasureField(f *ir.Field) {
	name := member(f)
	kind := tableScalarKind(f)
	width := tableKindWidth(kind)
	switch {
	case f.Type.Optional:
		// an optional's PRESENCE is the payload: it rides even when the value is
		// entirely default, exactly as a pointer's pointee does — otherwise
		// absent and present-at-default would be one value on the wire
		g.pf("    if (value.%sPresent) { // ?%s: presence decides, not content\n", name, tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.pf("        long body = %s(value.%s);\n", g.call(f.Type.Name, "Measure"), name)
			g.pf("        if (body < 0) { return -1; }\n")
			g.pf("        bytes += 3 + 4 + body; // %s\n", f.Name)
		case enumRef(f) != nil:
			g.pf("        if (TableEnumId.%s(%s) < 0) { return -1; } // no variant names this value\n",
				javaName(f.Type.Name), enumWiden("value."+name, enumRef(f)))
			g.pf("        bytes += 3 + 2; // %s: the variant's name hash\n", f.Name)
		default:
			g.pf("        bytes += 3 + %d; // %s\n", width, f.Name)
		}
		g.pf("    }\n")
	case f.KeyEnum != "":
		// enum-keyed: the body carries (variant id, length-prefixed element)
		// pairs, so a slot lands by NAME however the enum moved. A slot at its
		// default elides like any default, and an empty array elides whole.
		g.pf("    {\n")
		g.pf("        long pairs = 0, keyedBytes = 0;\n")
		g.pf("        for (int i = 0; i < %d; i++) { // [%s]: every stored slot is a named variant's\n", f.ArrayBound, f.KeyEnum)
		g.emitKeyedSlotRides(f, kind, "            ", "return -1;")
		if kind == tkTable {
			g.pf("            pairs++; keyedBytes += 2 + 4 + elemBytes; // key, length, body\n")
		} else {
			g.pf("            pairs++; keyedBytes += 2 + 4 + %d; // key, length, element\n", width)
		}
		g.pf("        }\n")
		g.pf("        if (pairs > 0) { bytes += 3 + 4 + 5 + keyedBytes; } // %s\n", f.Name)
		g.pf("    }\n")
	case f.Type.Kind == ir.TString:
		g.pf("    if (value.%sLength < 0 || value.%sLength > %d) { return -1; } // storage invariant\n", name, name, f.Type.Size)
		g.pf("    if (value.%sLength > 0) { bytes += 3 + 4 + value.%sLength; } // %s\n", name, name, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    if (value.%sLength < 0 || value.%sLength > %d) { return -1; } // storage invariant\n", name, name, f.Type.Size)
		g.pf("    if (value.%sLength > 0) { bytes += 3 + 4 + 5 + value.%sLength; } // %s\n", name, name, f.Name)
	case f.Array == ir.ArrayCounted && kind == tkTable:
		g.pf("    if (value.%sCount < 0 || value.%sCount > %d) { return -1; } // storage invariant\n", name, name, f.ArrayBound)
		g.pf("    if (value.%sCount > 0) {\n", name)
		g.pf("        bytes += 3 + 4 + 5; // %s\n", f.Name)
		g.pf("        for (int i = 0; i < value.%sCount; i++) {\n", name)
		g.pf("            long elem = %s(value.%s[i]);\n", g.call(f.Type.Name, "Measure"), name)
		g.pf("            if (elem < 0) { return -1; }\n")
		g.pf("            bytes += 4 + elem;\n")
		g.pf("        }\n    }\n")
	case f.Array == ir.ArrayCounted:
		g.pf("    if (value.%sCount < 0 || value.%sCount > %d) { return -1; } // storage invariant\n", name, name, f.ArrayBound)
		g.pf("    if (value.%sCount > 0) {\n", name)
		g.emitEnumElementCheck(f, fmt.Sprintf("value.%s[i]", name), fmt.Sprintf("value.%sCount", name), "        ", "return -1;")
		g.pf("        bytes += 3 + 4 + 5 + (long) value.%sCount * %d; // %s\n", name, width, f.Name)
		g.pf("    }\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		g.pf("    {\n")
		g.pf("        bytes += 3 + 4 + 5; // %s (fixed [%d])\n", f.Name, f.ArrayBound)
		g.pf("        for (int i = 0; i < %d; i++) {\n", f.ArrayBound)
		g.pf("            long elem = %s(value.%s[i]);\n", g.call(f.Type.Name, "Measure"), name)
		g.pf("            if (elem < 0) { return -1; }\n")
		g.pf("            bytes += 4 + elem;\n")
		g.pf("        }\n    }\n")
	case f.Array == ir.ArrayFixed:
		g.pf("    {\n")
		g.pf("        boolean allDefault = true;\n")
		g.pf("        for (int i = 0; i < %d; i++) { if (value.%s[i] != %s) { allDefault = false; break; } }\n",
			f.ArrayBound, name, g.fieldDefaultExpr(f))
		g.pf("        if (!allDefault) {\n")
		g.emitEnumElementCheck(f, fmt.Sprintf("value.%s[i]", name), fmt.Sprintf("%d", f.ArrayBound), "            ", "return -1;")
		g.pf("            bytes += 3 + 4 + 5 + %d; // %s\n", f.ArrayBound*int64(width), f.Name)
		g.pf("        }\n    }\n")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("    switch (value.%s.type) { // %s\n", name, f.Name)
		g.pf("        case %s.none: break; // None elides — TLV absence is the None\n", g.tagRef(un.Name))
		for _, v := range un.Variants {
			g.pf("        case %s.%s: {\n", g.tagRef(un.Name), javaName(v.Name))
			g.pf("            long arm = %s(value.%s.%s);\n", g.call(v.Type, "Measure"), name, javaName(v.Name))
			g.pf("            if (arm < 0) { return -1; }\n")
			g.pf("            bytes += 3 + 2 + 4 + arm; // the u16 ARM ID, then the arm length-prefixed\n            break;\n        }\n")
		}
		g.pf("        default: return -1; // invalid tag — the write side refuses it too\n")
		g.pf("    }\n")
	case kind == tkTable:
		g.pf("    {\n")
		g.pf("        long body = %s(value.%s);\n", g.call(f.Type.Name, "Measure"), name)
		g.pf("        if (body < 0) { return -1; }\n")
		g.pf("        if (body > 2) { bytes += 3 + 4 + body; } // %s: all-default nested elides\n", f.Name)
		g.pf("    }\n")
	case enumRef(f) != nil:
		g.pf("    if (value.%s != %s) {\n", name, g.fieldDefaultExpr(f))
		g.pf("        if (TableEnumId.%s(%s) < 0) { return -1; } // no variant names this value\n",
			javaName(f.Type.Name), enumWiden("value."+name, enumRef(f)))
		g.pf("        bytes += 3 + 2; // %s: the variant's name hash\n    }\n", f.Name)
	default:
		g.pf("    if (value.%s != %s) { bytes += 3 + %d; } // %s\n", name, g.fieldDefaultExpr(f), width, f.Name)
	}
}

// emitKeyedSlotRides emits the head of an enum-keyed array's per-slot loop
// (docs/SPEC-TABLES.md §2.4, §3.2). It elides a slot holding its default, refuses
// a slot whose value or whose KEY no variant names, and leaves `keyId` holding
// the slot's wire id. For a table element `elemBytes` holds the measured body,
// so measure and save decide elision on the same number.
func (g *tableGen) emitKeyedSlotRides(f *ir.Field, kind int, ind, onBad string) {
	expr := g.keyedSlots("value.", f) + "[i]"
	switch {
	case kind == tkTable:
		g.pf("%slong elemBytes = %s(%s);\n", ind, g.call(f.Type.Name, "Measure"), expr)
		g.pf("%sif (elemBytes < 0) { %s }\n", ind, onBad)
		g.pf("%sif (elemBytes <= 2) { continue; } // an all-default slot elides\n", ind)
	case enumRef(f) != nil:
		g.pf("%sif (%s == %s) { continue; } // a default slot elides\n", ind, expr, g.fieldDefaultExpr(f))
		g.pf("%sif (TableEnumId.%s(%s) < 0) { %s } // no variant names this value\n",
			ind, javaName(f.Type.Name), enumWiden(expr, enumRef(f)), onBad)
	default:
		g.pf("%sif (%s == %s) { continue; } // a default slot elides\n", ind, expr, g.fieldDefaultExpr(f))
	}
	g.pf("%sint keyId = TableEnumId.%s(i + 1); // i is the STORAGE index; the key it holds is i + 1\n",
		ind, javaName(f.KeyEnum))
	g.pf("%sif (keyId < 0) { %s }\n", ind, onBad)
}

// emitEnumElementCheck validates an enum ARRAY's elements before they ride: a
// value no variant names has no wire identity, so the value is refused rather
// than silently written as None (the union tag's rule, applied to enums).
func (g *tableGen) emitEnumElementCheck(f *ir.Field, expr, count, ind, onBad string) {
	e := enumRef(f)
	if e == nil {
		return
	}
	g.pf("%sfor (int i = 0; i < %s; i++) { // %s: every element must be nameable\n", ind, count, f.Name)
	g.pf("%s    if (TableEnumId.%s(%s) < 0) { %s }\n%s}\n", ind, javaName(f.Type.Name), enumWiden(expr, e), onBad, ind)
}

// ---- write / save ----

func (g *tableGen) emitTableWrite(st *ir.Struct) {
	g.pf("public static boolean %s(TableWriter w, %s value) {\n", method(st.Name, "SaveBody"), g.ref(st.Name))
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("    if (%s) {\n", cond)
			g.indent = "    "
			g.emitTableWriteField(f)
			g.indent = ""
			g.pf("    }\n")
			continue
		}
		g.emitTableWriteField(f)
	}
	g.pf("    w.put16(0); // terminator\n")
	g.pf("    return !w.overflow;\n}\n\n")
}

// emitTableSave emits the buffer-level entry of the measure/save pair: <X>Save
// writes into the caller's array and returns the bytes written — exactly
// <X>Measure's answer — or -1 when the room is too small.
func (g *tableGen) emitTableSave(st *ir.Struct) {
	ref := g.ref(st.Name)
	g.pf("public static long %s(%s value, byte[] buffer) {\n", method(st.Name, "Save"), ref)
	g.pf("    return %s(value, buffer, 0, buffer.length);\n}\n\n", method(st.Name, "Save"))
	g.pf("// the convenience form: ONE TableWriter per call. A hot loop hoists its own\n")
	g.pf("// writer and calls %s, which allocates nothing at all.\n", method(st.Name, "SaveBody"))
	g.pf("public static long %s(%s value, byte[] buffer, int offset, int length) {\n", method(st.Name, "Save"), ref)
	g.pf("    TableWriter w = new TableWriter(buffer, offset, length);\n")
	g.pf("    if (!%s(w, value)) { return -1; }\n", method(st.Name, "SaveBody"))
	g.pf("    return w.offset - offset; // == %s(value)\n}\n\n", method(st.Name, "Measure"))
}

func (g *tableGen) emitTableWriteField(f *ir.Field) {
	name := member(f)
	id := ir.TableFieldId(f)
	kind := tableScalarKind(f)
	switch {
	case f.Type.Optional:
		// present: the payload ALWAYS rides, all-default included — the
		// pointer's rule, and what makes ?T, *T and a plain nesting
		// wire-identical (docs/SPEC-TABLES.md §2.3, §3.1)
		g.pf("    if (value.%sPresent) { // ?%s\n", name, tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.pf("        long body = %s(value.%s);\n", g.call(f.Type.Name, "Measure"), name)
			g.pf("        if (body < 0) { return false; } // storage invariant, refused as measure refuses it\n")
			g.pf("        w.put16(0x%04x); w.put8(%d); // %s\n", id, tkTable, f.Name)
			g.pf("        w.put32((int) body);\n")
			g.pf("        if (!%s(w, value.%s)) { return false; }\n", g.call(f.Type.Name, "SaveBody"), name)
		case enumRef(f) != nil:
			g.pf("        int variantId = TableEnumId.%s(%s);\n", javaName(f.Type.Name), enumWiden("value."+name, enumRef(f)))
			g.pf("        if (variantId < 0) { return false; }\n")
			g.pf("        w.put16(0x%04x); w.put8(%d); // %s\n", id, kind, f.Name)
			g.pf("        w.put16(variantId);\n")
		default:
			g.pf("        w.put16(0x%04x); w.put8(%d); // %s\n", id, kind, f.Name)
			g.emitTableWriteElement(f, kind, "value."+name, "        ")
		}
		g.pf("    }\n")
	case f.KeyEnum != "":
		// the slots ride KEYED: (variant id, length-prefixed element) pairs,
		// counted like any array's elements. Two passes so the count is known
		// before the header rides, and so measure and save agree byte for byte.
		g.pf("    {\n")
		g.pf("        int pairs = 0;\n")
		g.pf("        for (int i = 0; i < %d; i++) { // [%s]: every stored slot is a named variant's\n", f.ArrayBound, f.KeyEnum)
		g.emitKeyedSlotRides(f, kind, "            ", "return false;")
		g.pf("            pairs++;\n")
		g.pf("        }\n")
		g.pf("        if (pairs > 0) {\n")
		g.pf("            // KIND 16, not 14: a keyed body and a positional one are\n")
		g.pf("            // incompatible, so a reader of the other kind must see a kind\n")
		g.pf("            // mismatch and skip, never misdecode (docs/SPEC-TABLES.md §3.2)\n")
		g.pf("            w.put16(0x%04x); w.put8(%d); // %s (keyed by %s)\n", id, tkKeyed, f.Name, f.KeyEnum)
		g.pf("            int lenAt = w.offset; w.put32(0);\n")
		g.pf("            w.put8(%d); w.put32(pairs);\n", kind)
		g.pf("            // ASCENDING BY VARIANT ORDINAL, which is slot order — this\n")
		g.pf("            // writer's choice, and a reader must not rely on it: every\n")
		g.pf("            // slot is found by its key (docs/SPEC-TABLES.md §3.2)\n")
		g.pf("            for (int i = 0; i < %d; i++) {\n", f.ArrayBound)
		g.emitKeyedSlotRides(f, kind, "                ", "return false;")
		g.pf("                w.put16(keyId); // the slot's VARIANT id, not its position\n")
		g.pf("                int elemLenAt = w.offset; w.put32(0);\n")
		if kind == tkTable {
			g.pf("                if (!%s(w, %s[i])) { return false; }\n",
				g.call(f.Type.Name, "SaveBody"), g.keyedSlots("value.", f))
		} else {
			g.emitTableWriteElement(f, kind, g.keyedSlots("value.", f)+"[i]", "                ")
		}
		g.pf("                w.patch32(elemLenAt, w.offset - elemLenAt - 4);\n")
		g.pf("            }\n")
		g.pf("            w.patch32(lenAt, w.offset - lenAt - 4);\n")
		g.pf("        }\n    }\n")
	case f.Type.Kind == ir.TString:
		g.pf("    if (value.%sLength < 0 || value.%sLength > %d) { return false; } // storage invariant\n", name, name, f.Type.Size)
		g.pf("    if (value.%sLength > 0) {\n", name)
		g.pf("        w.put16(0x%04x); w.put8(%d); // %s\n", id, tkString, f.Name)
		g.pf("        w.put32(value.%sLength);\n", name)
		g.pf("        w.raw(value.%s, 0, value.%sLength);\n    }\n", name, name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    if (value.%sLength < 0 || value.%sLength > %d) { return false; } // storage invariant\n", name, name, f.Type.Size)
		g.pf("    if (value.%sLength > 0) {\n", name)
		g.pf("        w.put16(0x%04x); w.put8(%d); // %s\n", id, tkArray, f.Name)
		g.pf("        w.put32(5 + value.%sLength);\n", name)
		g.pf("        w.put8(%d); w.put32(value.%sLength);\n", tkU8, name)
		g.pf("        w.raw(value.%s, 0, value.%sLength);\n    }\n", name, name)
	case f.Array == ir.ArrayCounted:
		g.pf("    if (value.%sCount < 0 || value.%sCount > %d) { return false; } // storage invariant\n", name, name, f.ArrayBound)
		g.pf("    if (value.%sCount > 0) {\n", name)
		g.pf("        w.put16(0x%04x); w.put8(%d); // %s\n", id, tkArray, f.Name)
		g.pf("        int lenAt = w.offset; w.put32(0);\n")
		g.pf("        w.put8(%d); w.put32(value.%sCount);\n", kind, name)
		g.pf("        for (int i = 0; i < value.%sCount; i++) {\n", name)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "            ")
		g.pf("        }\n")
		g.pf("        w.patch32(lenAt, w.offset - lenAt - 4);\n    }\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		// fixed arrays of tables always ride — position is identity there
		g.pf("    {\n")
		g.pf("        w.put16(0x%04x); w.put8(%d); // %s (fixed [%d])\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("        int lenAt = w.offset; w.put32(0);\n")
		g.pf("        w.put8(%d); w.put32(%d);\n", kind, f.ArrayBound)
		g.pf("        for (int i = 0; i < %d; i++) {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "            ")
		g.pf("        }\n")
		g.pf("        w.patch32(lenAt, w.offset - lenAt - 4);\n    }\n")
	case f.Array == ir.ArrayFixed:
		// fixed arrays are positional; an all-default array elides entirely
		// (parity with the reader's restored defaults)
		g.pf("    {\n")
		g.pf("        boolean allDefault = true;\n")
		g.pf("        for (int i = 0; i < %d; i++) { if (value.%s[i] != %s) { allDefault = false; break; } }\n",
			f.ArrayBound, name, g.fieldDefaultExpr(f))
		g.pf("        if (!allDefault) {\n")
		g.pf("            w.put16(0x%04x); w.put8(%d); // %s (fixed [%d])\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("            int lenAt = w.offset; w.put32(0);\n")
		g.pf("            w.put8(%d); w.put32(%d);\n", kind, f.ArrayBound)
		g.pf("            for (int i = 0; i < %d; i++) {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "                ")
		g.pf("            }\n")
		g.pf("            w.patch32(lenAt, w.offset - lenAt - 4);\n        }\n    }\n")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		tagType := g.tagRef(un.Name)
		g.pf("    if (value.%s.type != %s.none) {\n", name, tagType)
		g.pf("        w.put16(0x%04x); w.put8(%d); // %s\n", id, tkUnion, f.Name)
		g.pf("        // the ARM ID is the hash of the arm's NAME (docs/SPEC-TABLES.md §5), so\n")
		g.pf("        // arms may be added anywhere, removed and reordered\n")
		g.pf("        switch (value.%s.type) {\n", name)
		for _, v := range un.Variants {
			g.pf("            case %s.%s: w.put16(0x%04x); break;\n", tagType, javaName(v.Name), ir.VariantId(v.Name))
		}
		g.pf("            default: return false; // write validates the tag before it rides\n")
		g.pf("        }\n")
		g.pf("        int lenAt = w.offset; w.put32(0);\n")
		g.pf("        switch (value.%s.type) {\n", name)
		for _, v := range un.Variants {
			g.pf("            case %s.%s: if (!%s(w, value.%s.%s)) { return false; } break;\n",
				tagType, javaName(v.Name), g.call(v.Type, "SaveBody"), name, javaName(v.Name))
		}
		g.pf("            default: return false; // write validates the tag before it rides\n")
		g.pf("        }\n")
		g.pf("        w.patch32(lenAt, w.offset - lenAt - 4);\n    }\n")
	case kind == tkTable:
		// elision is decided BEFORE any byte is emitted: measuring first keeps
		// an all-default nested field from touching the buffer at all
		g.pf("    {\n")
		g.pf("        long body = %s(value.%s);\n", g.call(f.Type.Name, "Measure"), name)
		g.pf("        if (body < 0) { return false; } // storage invariant, refused as measure refuses it\n")
		g.pf("        if (body > 2) { // all-default nested elides\n")
		g.pf("            w.put16(0x%04x); w.put8(%d); // %s\n", id, tkTable, f.Name)
		g.pf("            w.put32((int) body);\n")
		g.pf("            if (!%s(w, value.%s)) { return false; }\n", g.call(f.Type.Name, "SaveBody"), name)
		g.pf("        }\n    }\n")
	case enumRef(f) != nil:
		// the id is resolved BEFORE the header rides: a value no variant names
		// has no wire identity, and the write refuses it rather than writing None
		g.pf("    if (value.%s != %s) {\n", name, g.fieldDefaultExpr(f))
		g.pf("        int variantId = TableEnumId.%s(%s);\n", javaName(f.Type.Name), enumWiden("value."+name, enumRef(f)))
		g.pf("        if (variantId < 0) { return false; }\n")
		g.pf("        w.put16(0x%04x); w.put8(%d); // %s\n", id, kind, f.Name)
		g.pf("        w.put16(variantId);\n    }\n")
	default:
		g.pf("    if (value.%s != %s) {\n", name, g.fieldDefaultExpr(f))
		g.pf("        w.put16(0x%04x); w.put8(%d); // %s\n", id, kind, f.Name)
		g.emitTableWriteElement(f, kind, "value."+name, "        ")
		g.pf("    }\n")
	}
}

func (g *tableGen) emitTableWriteElement(f *ir.Field, kind int, expr, ind string) {
	if e := enumRef(f); e != nil {
		g.pf("%s{\n%s    int writeElementId = TableEnumId.%s(%s);\n", ind, ind, javaName(f.Type.Name), enumWiden(expr, e))
		g.pf("%s    if (writeElementId < 0) { return false; }\n", ind)
		g.pf("%s    w.put16(writeElementId);\n%s}\n", ind, ind)
		return
	}
	switch kind {
	case tkBool:
		g.pf("%sw.put8(%s ? 1 : 0);\n", ind, expr)
	case tkF32:
		g.pf("%sw.put32(Float.floatToRawIntBits(%s));\n", ind, expr)
	case tkF64:
		g.pf("%sw.put64(Double.doubleToRawLongBits(%s));\n", ind, expr)
	case tkTable:
		g.pf("%s{\n%s    int elemLenAt = w.offset; w.put32(0);\n", ind, ind)
		g.pf("%s    if (!%s(w, %s)) { return false; }\n", ind, g.call(f.Type.Name, "SaveBody"), expr)
		g.pf("%s    w.patch32(elemLenAt, w.offset - elemLenAt - 4);\n%s}\n", ind, ind)
	default:
		width := tableKindWidth(kind)
		putType := "int"
		if width == 8 {
			putType = "long"
		}
		g.pf("%sw.%s(%s(%s));\n", ind, tablePut(width), cast(putType, g.javaFieldType(f.Type)), expr)
	}
}

// ---- read ----

func (g *tableGen) emitTableRead(st *ir.Struct) {
	ref := g.ref(st.Name)
	g.pf("public static boolean %s(TableReader r, %s value) {\n", method(st.Name, "LoadBody"), ref)
	g.pf("    %s(value); // restore declared defaults in place, then overlay\n", g.call(st.Name, "Reset"))
	g.pf("    for (;;) {\n")
	g.pf("        if (!r.has(2)) { r.report.malformed = true; return false; }\n")
	g.pf("        int fieldId = r.get16();\n")
	g.pf("        if (fieldId == 0) { return true; }\n")
	g.pf("        if (!r.has(1)) { r.report.malformed = true; return false; }\n")
	g.pf("        int kind = r.get8();\n")
	if len(st.Fields) > 0 {
		g.pf("        switch (fieldId) {\n")
		for _, f := range st.Fields {
			id := ir.TableFieldId(f)
			kind := tableScalarKind(f)
			wireKind := kind
			if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
				wireKind = tkArray
			}
			if f.KeyEnum != "" {
				// a KEYED body is its own kind, so the positional-to-keyed edit
				// (and its reverse) reads as a kind mismatch and is counted,
				// never decoded as the other body (docs/SPEC-TABLES.md §3.2)
				wireKind = tkKeyed
			}
			if f.Type.Kind == ir.TBytes {
				kind = tkU8 // bytes travel as an array of u8 elements
			}
			g.pf("            case 0x%04x: { // %s\n", id, f.Name)
			g.pf("                if (kind != %d) {\n", wireKind)
			g.pf("                    r.report.kindMismatch++;\n")
			g.pf("                    if (!r.skip(kind)) { r.report.malformed = true; return false; }\n")
			g.pf("                    break;\n                }\n")
			g.emitTableReadField(f, kind)
			if f.Type.Optional {
				// the field rode, so it is PRESENT — content decides nothing
				// here either (docs/SPEC-TABLES.md §2.3)
				g.pf("                value.%sPresent = true;\n", member(f))
			}
			g.pf("                break;\n            }\n")
		}
		g.pf("            default: {\n")
		g.pf("                r.report.unknown++;\n")
		g.pf("                if (!r.skip(kind)) { r.report.malformed = true; return false; }\n")
		g.pf("                break;\n            }\n")
		g.pf("        }\n    }\n}\n\n")
	} else {
		g.pf("        r.report.unknown++;\n")
		g.pf("        if (!r.skip(kind)) { r.report.malformed = true; return false; }\n")
		g.pf("    }\n}\n\n")
	}

	g.pf("public static boolean %s(%s value, byte[] bytes, TableReport report) {\n", method(st.Name, "Load"), ref)
	g.pf("    return %s(value, bytes, 0, bytes.length, report);\n}\n\n", method(st.Name, "Load"))
	g.pf("// the convenience form: ONE TableReader per call. A hot loop hoists its own\n")
	g.pf("// reader and calls %s, which allocates nothing at all.\n", method(st.Name, "LoadBody"))
	g.pf("public static boolean %s(%s value, byte[] bytes, int offset, int length, TableReport report) {\n", method(st.Name, "Load"), ref)
	g.pf("    TableReader r = new TableReader(bytes, offset, length, report != null ? report : new TableReport());\n")
	g.pf("    return %s(r, value);\n}\n\n", method(st.Name, "LoadBody"))
}

func (g *tableGen) emitTableReadField(f *ir.Field, kind int) {
	name := member(f)
	ind := "                "
	switch {
	case f.KeyEnum != "":
		// each pair is placed by its VARIANT id, so a slot lands by name however
		// the enum moved; an id this reader cannot name is skipped by its length
		// and counted unknown, and a slot the writer never sent keeps the
		// prefill's default (docs/SPEC-TABLES.md §3.2)
		g.pf("%sif (!r.has(4)) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%slong bodyLen = r.get32() & 0xffffffffL;\n", ind)
		g.pf("%sif (!r.has(bodyLen)) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%sint bodyEnd = r.offset + (int) bodyLen;\n", ind)
		g.pf("%sif (bodyLen >= 5) {\n", ind)
		g.pf("%s    int elemKind = r.get8();\n", ind)
		g.pf("%s    long count = r.get32() & 0xffffffffL;\n", ind)
		g.pf("%s    if (elemKind != %d) { r.report.kindMismatch++; r.offset = bodyEnd; break; }\n", ind, kind)
		g.pf("%s    int bodyLimit = r.limit;\n", ind)
		g.pf("%s    r.limit = bodyEnd; // the pairs are BOUNDED by the field body\n", ind)
		g.pf("%s    for (long i = 0; i < count; i++) {\n", ind)
		g.pf("%s        if (!r.has(2)) { r.report.malformed = true; break; }\n", ind)
		g.pf("%s        int key = r.get16();\n", ind)
		g.pf("%s        if (!r.has(4)) { r.report.malformed = true; break; }\n", ind)
		g.pf("%s        long elemLen = r.get32() & 0xffffffffL;\n", ind)
		g.pf("%s        if (!r.has(elemLen)) { r.report.malformed = true; break; }\n", ind)
		g.pf("%s        int elemEnd = r.offset + (int) elemLen;\n", ind)
		g.pf("%s        if (key == 0) {\n", ind)
		g.pf("%s            // None is the NULL KEY: 0 is the reserved id no declared\n", ind)
		g.pf("%s            // name can fold to, so a body carrying one is DAMAGED, not\n", ind)
		g.pf("%s            // merely foreign. Framing damage stops this body, keeps what\n", ind)
		g.pf("%s            // it decoded, and the parent reads on past the length\n", ind)
		g.pf("%s            // (docs/SPEC-TABLES.md §3.2, §4).\n", ind)
		g.pf("%s            r.report.malformed = true;\n%s            break;\n%s        }\n", ind, ind, ind)
		g.pf("%s        long slot = TableEnumValue.%s(key);\n", ind, javaName(f.KeyEnum))
		g.pf("%s        if (slot < 0) {\n", ind)
		g.pf("%s            r.report.unknown++; // a slot this reader cannot name\n", ind)
		g.pf("%s            r.offset = elemEnd;\n%s            continue;\n%s        }\n", ind, ind, ind)
		g.pf("%s        int slotLimit = r.limit;\n", ind)
		g.pf("%s        r.limit = elemEnd;\n", ind)
		// the key k lives at STORAGE INDEX k-1 (docs/SPEC-TABLES.md §2.4)
		slot := g.keyedSlots("value.", f) + "[(int) slot - 1]"
		if kind == tkTable {
			g.pf("%s        %s(r, %s);\n", ind, g.call(f.Type.Name, "LoadBody"), slot)
		} else {
			g.emitTableReadScalarFrom(f, kind, slot, ind+"        ",
				"r.report.malformed = true; r.limit = slotLimit; r.offset = elemEnd; continue;")
		}
		g.pf("%s        r.limit = slotLimit;\n", ind)
		g.pf("%s        r.offset = elemEnd;\n", ind)
		g.pf("%s    }\n", ind)
		g.pf("%s    r.limit = bodyLimit;\n", ind)
		g.pf("%s}\n", ind)
		g.pf("%sr.offset = bodyEnd; // unread pairs and slack skip via the length\n", ind)
	case f.Type.Kind == ir.TString:
		g.pf("%sif (!r.has(4)) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%slong len = r.get32() & 0xffffffffL;\n", ind)
		g.pf("%sif (!r.has(len)) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%sint keep = (int) len;\n", ind)
		g.pf("%sif (keep > %d) { keep = %d; r.report.clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%sSystem.arraycopy(r.buffer, r.offset, value.%s, 0, keep);\n", ind, name)
		g.pf("%svalue.%sLength = keep;\n", ind, name)
		g.pf("%sr.offset += (int) len;\n", ind)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		counted := f.Type.Kind == ir.TBytes || f.Array == ir.ArrayCounted
		g.pf("%sif (!r.has(4)) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%slong bodyLen = r.get32() & 0xffffffffL;\n", ind)
		g.pf("%sif (!r.has(bodyLen)) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%sint bodyEnd = r.offset + (int) bodyLen;\n", ind)
		g.pf("%sif (bodyLen >= 5) {\n", ind)
		g.pf("%s    int elemKind = r.get8();\n", ind)
		g.pf("%s    long count = r.get32() & 0xffffffffL;\n", ind)
		g.pf("%s    if (elemKind != %d) { r.report.kindMismatch++; r.offset = bodyEnd; break; }\n", ind, kind)
		g.pf("%s    int keep = (int) count;\n", ind)
		g.pf("%s    if (count > %d) { keep = %d; r.report.clamped++; }\n", ind, bound, bound)
		g.pf("%s    // elements are BOUNDED by the field body: a count the length\n", ind)
		g.pf("%s    // cannot cover keeps the decoded prefix, flags malformed, and\n", ind)
		g.pf("%s    // the parent continues at the next field — following fields'\n", ind)
		g.pf("%s    // bytes are never fabricated into elements\n", ind)
		g.pf("%s    int bodyLimit = r.limit;\n", ind)
		g.pf("%s    r.limit = bodyEnd;\n", ind)
		if counted {
			g.pf("%s    int decoded = 0;\n", ind)
		}
		g.pf("%s    for (int i = 0; i < keep; i++) {\n", ind)
		g.emitTableReadElement(f, kind, ind+"        ")
		if counted {
			g.pf("%s        decoded = i + 1;\n", ind)
		}
		g.pf("%s    }\n", ind)
		g.pf("%s    r.limit = bodyLimit;\n", ind)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s    value.%sLength = decoded;\n", ind, name)
		} else if f.Array == ir.ArrayCounted {
			g.pf("%s    value.%sCount = decoded;\n", ind, name)
		}
		g.pf("%s}\n", ind)
		g.pf("%sr.offset = bodyEnd; // excess elements and slack skip via the length\n", ind)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		tagType := g.tagRef(un.Name)
		g.pf("%sif (!r.has(2)) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%sint armId = r.get16();\n", ind)
		g.pf("%sif (armId == 0) { value.%s.type = %s.none; break; } // empty: the id is the whole payload\n", ind, name, tagType)
		g.pf("%sif (!r.has(4)) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%slong bodyLen = r.get32() & 0xffffffffL;\n", ind)
		g.pf("%sif (!r.has(bodyLen)) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%sint bodyEnd = r.offset + (int) bodyLen;\n", ind)
		g.pf("%s{\n%s    int bodyLimit = r.limit;\n%s    r.limit = bodyEnd;\n", ind, ind, ind)
		g.pf("%s    switch (armId) { // the arm's NAME hash (docs/SPEC-TABLES.md §5)\n", ind)
		for _, v := range un.Variants {
			g.pf("%s        case 0x%04x: // %s\n%s            value.%s.type = %s.%s;\n%s            %s(r, value.%s.%s);\n%s            break;\n",
				ind, ir.VariantId(v.Name), v.Name, ind, name, tagType, javaName(v.Name),
				ind, g.call(v.Type, "LoadBody"), name, javaName(v.Name), ind)
		}
		g.pf("%s        default:\n", ind)
		g.pf("%s            // an arm this reader cannot name: the value reads EMPTY and\n", ind)
		g.pf("%s            // the body is skipped by its length, never misdecoded. The\n", ind)
		g.pf("%s            // reset is explicit, not the prefill's: a repeated field id\n", ind)
		g.pf("%s            // must not leave an arm decoded by an earlier occurrence\n", ind)
		g.pf("%s            // standing (docs/SPEC-TABLES.md §4).\n", ind)
		g.pf("%s            value.%s.type = %s.none;\n", ind, name, tagType)
		g.pf("%s            r.report.unknown++;\n%s            break;\n", ind, ind)
		g.pf("%s    }\n%s    r.limit = bodyLimit;\n%s}\n", ind, ind, ind)
		g.pf("%sr.offset = bodyEnd;\n", ind)
	case kind == tkTable:
		g.pf("%sif (!r.has(4)) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%slong bodyLen = r.get32() & 0xffffffffL;\n", ind)
		g.pf("%sif (!r.has(bodyLen)) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%sint bodyEnd = r.offset + (int) bodyLen;\n", ind)
		g.pf("%s{\n%s    int bodyLimit = r.limit;\n%s    r.limit = bodyEnd;\n", ind, ind, ind)
		g.pf("%s    %s(r, value.%s);\n", ind, g.call(f.Type.Name, "LoadBody"), name)
		g.pf("%s    r.limit = bodyLimit;\n%s}\n", ind, ind)
		g.pf("%sr.offset = bodyEnd;\n", ind)
	default:
		g.emitTableReadScalarFrom(f, kind, "value."+name, ind, "r.report.malformed = true; return false;")
	}
}

// emitTableReadElement decodes one array element from inside the field body;
// truncation keeps the decoded prefix and flags malformed without stopping the
// parent decode.
func (g *tableGen) emitTableReadElement(f *ir.Field, kind int, ind string) {
	name := member(f)
	switch kind {
	case tkTable:
		g.pf("%sif (!r.has(4)) { r.report.malformed = true; break; }\n", ind)
		g.pf("%slong elemLen = r.get32() & 0xffffffffL;\n", ind)
		g.pf("%sif (!r.has(elemLen)) { r.report.malformed = true; break; }\n", ind)
		g.pf("%sint elemEnd = r.offset + (int) elemLen;\n", ind)
		g.pf("%s{\n%s    int elemLimit = r.limit;\n%s    r.limit = elemEnd;\n", ind, ind, ind)
		g.pf("%s    %s(r, value.%s[i]);\n", ind, g.call(f.Type.Name, "LoadBody"), name)
		g.pf("%s    r.limit = elemLimit;\n%s}\n", ind, ind)
		g.pf("%sr.offset = elemEnd;\n", ind)
	default:
		g.emitTableReadScalarFrom(f, kind, fmt.Sprintf("value.%s[i]", name), ind,
			"r.report.malformed = true; break;")
	}
}

// javaReadExpr is one fixed-width scalar read, widened into the decode local's
// type so a clamp compares the value the wire carried.
func javaReadExpr(kind int) string {
	switch kind {
	case tkI8:
		return "(byte) r.get8()"
	case tkU8:
		return "r.get8()"
	case tkI16:
		return "(short) r.get16()"
	case tkU16:
		return "r.get16()"
	case tkI32:
		return "r.get32()"
	case tkU32:
		return "r.get32() & 0xffffffffL"
	}
	return "r.get64()"
}

// emitTableReadScalarFrom decodes one fixed-width scalar into a storage member,
// with the range clamps the schema declares. onTrunc is the truncation action:
// a scalar FIELD stops the decode (outer framing damage), an array ELEMENT
// keeps the prefix and breaks, a keyed SLOT skips to the next pair.
func (g *tableGen) emitTableReadScalarFrom(f *ir.Field, kind int, lvalue, ind, onTrunc string) {
	width := tableKindWidth(kind)
	g.pf("%sif (!r.has(%d)) { %s }\n", ind, width, onTrunc)
	if e := enumRef(f); e != nil {
		// identity is the variant's NAME (docs/SPEC-TABLES.md §5): an id this build
		// cannot name reads as None and counts as unknown, exactly as an unknown
		// FIELD id does — same event, one counter
		g.pf("%s{\n%s    long decodedEnum = TableEnumValue.%s(r.get16());\n", ind, ind, javaName(f.Type.Name))
		g.pf("%s    if (decodedEnum < 0) { decodedEnum = 0; r.report.unknown++; }\n", ind)
		g.pf("%s    %s = %sdecodedEnum;\n%s}\n", ind, lvalue, cast(enumJavaType(e.StorageBits), "long"), ind)
		return
	}
	storage := g.javaFieldType(f.Type)
	if f.Type.Kind == ir.TBytes {
		// a `bytes` field's ELEMENTS are bytes; the field's own Java storage is
		// the buffer, which is not what a slot assignment narrows to
		storage = "byte"
	}
	switch kind {
	case tkBool:
		g.pf("%s%s = r.get8() != 0;\n", ind, lvalue)
	case tkF32:
		if f.HasFloatRange {
			g.pf("%s{\n%s    float decodedF = Float.intBitsToFloat(r.get32());\n", ind, ind)
			g.pf("%s    if (decodedF < %s) { decodedF = %s; r.report.clamped++; }\n", ind, formatFloat32(f.FMin), formatFloat32(f.FMin))
			g.pf("%s    else if (decodedF > %s) { decodedF = %s; r.report.clamped++; }\n", ind, formatFloat32(f.FMax), formatFloat32(f.FMax))
			g.pf("%s    %s = decodedF;\n%s}\n", ind, lvalue, ind)
			return
		}
		g.pf("%s%s = Float.intBitsToFloat(r.get32());\n", ind, lvalue)
	case tkF64:
		g.pf("%s%s = Double.longBitsToDouble(r.get64());\n", ind, lvalue)
	default:
		local := javaDecodeType(kind)
		unsigned := javaDecodeUnsigned(kind)
		lit := func(v *big.Int) string {
			if local == "int" {
				return narrowLit("int", v)
			}
			return javaIntLit(v)
		}
		below := func(v string) string {
			if unsigned {
				return "Long.compareUnsigned(decodedV, " + v + ") < 0"
			}
			return "decodedV < " + v
		}
		above := func(v string) string {
			if unsigned {
				return "Long.compareUnsigned(decodedV, " + v + ") > 0"
			}
			return "decodedV > " + v
		}
		g.pf("%s{\n%s    %s decodedV = %s;\n", ind, ind, local, javaReadExpr(kind))
		if f.HasIntRange {
			low, high := tableClampEnds(f, width)
			if low {
				lo := lit(f.IntMin)
				g.pf("%s    if (%s) { decodedV = %s; r.report.clamped++; }\n", ind, below(lo), lo)
			}
			if high {
				hi := lit(f.IntMax)
				lead := "if"
				if low {
					lead = "else if"
				}
				g.pf("%s    %s (%s) { decodedV = %s; r.report.clamped++; }\n", ind, lead, above(hi), hi)
			}
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
			maxv := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
			mv := lit(maxv)
			g.pf("%s    if (%s) { decodedV = %s; r.report.clamped++; } // bits(%d) width clamp\n",
				ind, above(mv), mv, f.Type.Width)
		}
		g.pf("%s    %s = %sdecodedV;\n%s}\n", ind, lvalue, cast(storage, local), ind)
	}
}

// ---- reflection descriptors ----

// tableFieldTypeName renders a field's schema-facing type name for the
// descriptor ("float32", "bits(9)", "Grade", "GunnerSettings").
func tableFieldTypeName(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt:
		prefix := "int"
		if !f.Type.Signed {
			prefix = "uint"
		}
		return fmt.Sprintf("%s%d", prefix, f.Type.Width)
	case ir.TBits:
		return fmt.Sprintf("bits(%d)", f.Type.Width)
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TNamed:
		return f.Type.Name
	}
	return "?"
}

// unionArmLambda renders a descriptor lambda over a union's tag values: 0 is
// the empty arm, [1, N] the declared arms in tag order. Java's switch takes no
// long, so the chain is spelled with ifs.
func unionArmLambda(un *ir.Union, arm func(ir.UnionVariant) string, unknown, none string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "(v) -> { if (v == 0L) { return %s; }", none)
	for i, v := range un.Variants {
		fmt.Fprintf(&b, " if (v == %dL) { return %s; }", i+1, arm(v))
	}
	fmt.Fprintf(&b, " return %s; }", unknown)
	return b.String()
}

func bigToDouble(v *big.Int) string {
	f, _ := new(big.Float).SetInt(v).Float64()
	return formatFloat64(f)
}

// emitTableDescriptor emits <name>TableType() — the reflection descriptor,
// built once on first use and published through the HOLDER IDIOM.
//
// THE PUBLICATION IS THE WHOLE POINT, and a plain `if (cache != null)` would be
// wrong here rather than merely unfashionable. A descriptor is a mutable object
// with non-final fields, so under JLS §17.4 a second thread may read a non-null
// cache and still see `fields == null` — the writes that filled it are not
// ordered against the write that published it. The build being idempotent makes
// the duplicate CONSTRUCTION benign and says nothing about the PUBLICATION. On
// the estate's aarch64 targets that reordering is permitted, and the first
// caller of it is <Table>Block.open, which is the one path a racing
// NullPointerException must never escape from.
//
// The holder gives the ordering for free: a class initializes once, under the
// JVM's own lock, and every write made during its initialization happens-before
// any read of a field it published (JLS §12.4.2). Lazy, lock-free after the
// first use, and allocation-free — the same three properties the cache had,
// with the guarantee the cache was missing.
func (g *tableGen) emitTableDescriptor(st *ir.Struct) {
	guards := tableGuardStrings(st)
	ref := g.ref(st.Name)
	holder := st.Name + "TableTypeHolder"
	g.pf("// %s's reflection descriptor (docs/SPEC-TABLES.md §8), built once and\n", st.Name)
	g.pf("// published by class initialization — see the holder note in the emitter.\n")
	g.pf("private static final class %s {\n", holder)
	// the TAG lists (docs/SPEC-TABLES.md §8.1), one constant per tagged field
	// and one for a tagged declaration, named from the descriptor row: a
	// field's by its own member spelling, the declaration's by `tags`. They
	// stand ahead of INFO, so class initialization fills them first.
	var tagged bool
	for _, f := range st.Fields {
		tagged = g.emitTagsStatic(member(f)+"Tags", f.Tags) || tagged
	}
	tagged = g.emitTagsStatic("tags", st.Tags) || tagged
	if tagged {
		g.pf("\n")
	}
	g.pf("    static final TableTypeInfo INFO = build();\n\n")
	g.pf("    private static TableTypeInfo build() {\n")
	g.pf("        TableTypeInfo info = new TableTypeInfo();\n")
	g.pf("        info.name = \"%s\";\n", st.Name)
	g.pf("        info.numFields = %d;\n", len(st.Fields))
	g.pf("        TableFieldInfo[] fields = new TableFieldInfo[%d];\n", len(st.Fields))
	if len(st.Fields) > 0 {
		g.pf("        TableFieldInfo f;\n")
		g.indent = "    "
		for i, f := range st.Fields {
			g.emitTableFieldDescriptor(i, f, guards[f.Name])
		}
		g.indent = ""
	}
	g.pf("        info.fields = fields;\n")
	// the RESET hook (docs/SPEC-TABLES.md §8.1): the one column the descriptors
	// cannot express without a function — a generic walker that FILLS a value
	// establishes an absent field's defaults through it, holding no type to
	// spell. It is <name>Reset, the prefill the wire's read path already calls.
	g.pf("        info.reset = (o) -> %s((%s) o);\n", g.call(st.Name, "Reset"), ref)
	// the declaration's OWN doc and tags (docs/SPEC-TABLES.md §8.1), so a
	// walker that entered a nested table through the table column reads that
	// declaration's annotations there and looks nothing up
	g.pf("        %s\n", annotationColumns("info", st.Doc, st.Tags, "tags"))
	g.pf("        return info;\n    }\n}\n\n")
	g.pf("public static TableTypeInfo %s() { return %s.INFO; }\n\n", method(st.Name, "TableType"), holder)
}

// emitTagsStatic emits one tag list as a constant array of string literals
// (docs/SPEC-TABLES.md §8.1), and nothing at all for an item with no tags:
// absence is 0 and null in the row, never a per-row empty array. It reports
// whether it emitted anything.
func (g *tableGen) emitTagsStatic(name string, tags []string) bool {
	if len(tags) == 0 {
		return false
	}
	g.pf("    static final String[] %s = { %s };\n", name, ir.QuotedTags(tags))
	return true
}

// annotationColumns renders one row's doc, numTags and tags assignments, `row`
// being the descriptor the columns land on: the SHARED empty doc and a null
// list where the item carries none (docs/SPEC-TABLES.md §8.1).
func annotationColumns(row, doc string, tags []string, tagsName string) string {
	docColumn := "TableDocNone.value"
	if doc != "" {
		docColumn = ir.QuoteDoc(doc)
	}
	list := "null"
	if len(tags) > 0 {
		list = tagsName
	}
	return fmt.Sprintf("%s.doc = %s; %s.numTags = %d; %s.tags = %s;", row, docColumn, row, len(tags), row, list)
}

// ---- the storage columns: Java's spelling of C++'s offset and elem_size ----

// storageExpr is the Java expression for a field's storage member on an
// instance reached as `o`, cast back to its own class.
func (g *tableGen) storageExpr(f *ir.Field) string {
	return fmt.Sprintf("((%s) o).%s", g.ref(g.owner.Name), member(f))
}

// elementExpr is storageExpr indexed where the field is an array, and
// storageExpr itself where it is not — the walker passes 0 for a scalar.
func (g *tableGen) elementExpr(f *ir.Field) string {
	if f.Array != ir.ArrayNone || f.KeyEnum != "" {
		return g.storageExpr(f) + "[i]"
	}
	return g.storageExpr(f)
}

// javaRawGet renders one element as the long the descriptor's getRaw hands
// back: an integer sign-extended, a bool as 0 or 1, an enum or flags mask as
// its value, a float as its IEEE-754 bit pattern. Zero extension happens HERE
// rather than in the walker, because the Java storage type already knows its
// own width and C++'s width switch has nothing to switch on.
func javaRawGet(expr string, t ir.FieldType, storage string) string {
	switch t.Kind {
	case ir.TBool:
		return expr + " ? 1L : 0L"
	case ir.TFloat32:
		return "Float.floatToRawIntBits(" + expr + ") & 0xffffffffL"
	case ir.TFloat64:
		return "Double.doubleToRawLongBits(" + expr + ")"
	case ir.TInt:
		if t.Signed {
			// long storage IS the descriptor's currency; casting it would be a
			// redundant cast, which -Xlint:all makes a build failure
			if storage == "long" {
				return expr
			}
			return "(long) " + expr
		}
		return unsignedWiden(expr, storage)
	case ir.TBits:
		return unsignedWiden(expr, storage)
	case ir.TNamed:
		if _, isFlags := t.Ref.(*ir.Flags); isFlags {
			return expr
		}
		return unsignedWiden(expr, storage)
	}
	return "0L"
}

// unsignedWiden lifts a bit-transparent signed storage read into the unsigned
// long the descriptor's currency is.
func unsignedWiden(expr, storage string) string {
	switch storage {
	case "byte":
		return expr + " & 0xffL"
	case "short":
		return expr + " & 0xffffL"
	case "int":
		return expr + " & 0xffffffffL"
	}
	return expr
}

// javaRawSet is its inverse. The narrowing cast is unchecked because the walker
// has already clamped to the field's declared range and to its storage width
// (§16.2), so a value reaching here fits.
func javaRawSet(expr, src string, t ir.FieldType, storage string) string {
	switch t.Kind {
	case ir.TBool:
		return expr + " = " + src + " != 0"
	case ir.TFloat32:
		return expr + " = Float.intBitsToFloat((int) " + src + ")"
	case ir.TFloat64:
		return expr + " = Double.longBitsToDouble(" + src + ")"
	}
	return expr + " = " + cast(storage, "long") + src
}

// javaElemWidth is the STORAGE width of one element in bytes — C++'s elem_size
// where it has a Java meaning, and the last bound a numeric read clamps to
// (§16.2). 0 on every kind whose storage is not a fixed-width number.
func javaElemWidth(t ir.FieldType) int {
	switch t.Kind {
	case ir.TBool:
		return 1
	case ir.TFloat32:
		return 4
	case ir.TFloat64:
		return 8
	case ir.TInt:
		return t.Width / 8
	case ir.TBits:
		if t.Width <= 32 {
			return 4
		}
		return 8
	}
	return 0
}

// tableStorageColumns renders a field's storage columns: the accessor pairs a
// generic walker reaches the value through. Exactly one of getRaw/getChild/
// getBuffer is non-null, and the companions follow the counted and optional
// columns beside them.
func (g *tableGen) tableStorageColumns(f *ir.Field) string {
	var b strings.Builder
	name := member(f)
	cast := fmt.Sprintf("((%s) o).", g.ref(g.owner.Name))
	storage := g.javaFieldType(f.Type)
	switch {
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		fmt.Fprintf(&b, "f.getBuffer = (o) -> %s; ", g.storageExpr(f))
		fmt.Fprintf(&b, "f.getCount = (o) -> %s%sLength; ", cast, name)
		fmt.Fprintf(&b, "f.setCount = (o, n) -> %s%sLength = n; ", cast, name)
	case isClassRef(f.Type):
		fmt.Fprintf(&b, "f.getChild = (o, i) -> %s; ", g.elementExpr(f))
	default:
		fmt.Fprintf(&b, "f.getRaw = (o, i) -> %s; ", javaRawGet(g.elementExpr(f), f.Type, storage))
		fmt.Fprintf(&b, "f.setRaw = (o, i, raw) -> %s; ", javaRawSet(g.elementExpr(f), "raw", f.Type, storage))
	}
	if f.Array == ir.ArrayCounted {
		fmt.Fprintf(&b, "f.getCount = (o) -> %s%sCount; ", cast, name)
		fmt.Fprintf(&b, "f.setCount = (o, n) -> %s%sCount = n; ", cast, name)
	}
	if f.Type.Optional {
		fmt.Fprintf(&b, "f.getPresent = (o) -> %s%sPresent; ", cast, name)
		fmt.Fprintf(&b, "f.setPresent = (o, p) -> %s%sPresent = p; ", cast, name)
	}
	return strings.TrimRight(b.String(), " ")
}

// emitUnionArms renders a union field's arms column: the tag's accessor pair
// and one entry per arm, index 0 being the EMPTY arm, which carries neither
// payload nor descriptor. Built with the descriptor and cached with it, so a
// walk over a union allocates nothing.
func (g *tableGen) emitUnionArms(un *ir.Union) {
	ref := g.ref(un.Name)
	tagType := tagJavaType(un.Max)
	g.pf("        TableUnionInfo u = new TableUnionInfo();\n")
	g.pf("        u.getTag = (o) -> %s;\n", unsignedWiden("(("+ref+") o).type", tagType))
	g.pf("        u.setTag = (o, t) -> ((%s) o).type = %st;\n", ref, cast(tagType, "long"))
	g.pf("        TableUnionArmInfo[] as = new TableUnionArmInfo[%d];\n", len(un.Variants)+1)
	g.pf("        as[0] = new TableUnionArmInfo();\n")
	for i, v := range un.Variants {
		g.pf("        as[%d] = new TableUnionArmInfo();\n", i+1)
		g.pf("        as[%d].tableRef = () -> %s();\n", i+1, g.call(v.Type, "TableType"))
		g.pf("        as[%d].payload = (o) -> ((%s) o).%s;\n", i+1, ref, javaName(v.Name))
	}
	g.pf("        u.arms = as;\n")
	g.pf("        f.arms = u;\n")
}

func (g *tableGen) emitTableFieldDescriptor(index int, f *ir.Field, guard string) {
	id := ir.TableFieldId(f)
	kind := tableScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = tkU8
	}
	isArray := f.Array != ir.ArrayNone || f.KeyEnum != "" || f.Type.Kind == ir.TBytes
	counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString

	// the count column, spelled the way the storage spells its own extent: a
	// keyed array DERIVES it from the key enum, so nothing outside the array
	// names its size (docs/SPEC-TABLES.md §2.4, §8.1)
	bound := "0"
	switch {
	case f.KeyEnum != "":
		bound = g.packetRef(f.KeyEnum) + ".max"
	case f.Array != ir.ArrayNone:
		bound = strconv.FormatInt(f.ArrayBound, 10)
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString:
		bound = strconv.FormatInt(f.Type.Size, 10)
	}

	tableRef := "null"
	if _, isStruct := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && isStruct {
		tableRef = fmt.Sprintf("() -> %s()", g.call(f.Type.Name, "TableType"))
	}

	// the KEY's vocabulary on an enum-keyed array (docs/SPEC-TABLES.md §8):
	// functions of the KEY, not of the storage index.
	keyTypeName, keyName, keyId := "null", "null", "null"
	if f.KeyEnum != "" {
		keyTypeName = fmt.Sprintf("%q", f.KeyEnum)
		keyName = fmt.Sprintf("(v) -> %s.enumName%s(v)", g.declFile(f.KeyEnum), f.KeyEnum)
		keyId = fmt.Sprintf("(v) -> TableEnumId.%s(v)", javaName(f.KeyEnum))
	}

	hasRange := "false"
	rangeMin, rangeMax := "0.0", "0.0"
	if f.Type.Kind == ir.TBits && !f.HasIntRange {
		// bits(N) declares its range by its WIDTH: [0, 2^N - 1]
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		hasRange = "true"
		rangeMin, rangeMax = "0.0", bigToDouble(max)
	}
	if f.HasIntRange {
		hasRange = "true"
		rangeMin, rangeMax = bigToDouble(f.IntMin), bigToDouble(f.IntMax)
	} else if f.HasFloatRange {
		hasRange = "true"
		rangeMin, rangeMax = formatFloat64(f.FMin), formatFloat64(f.FMax)
	}

	// the VOCABULARY columns: an enum's values, a union's arms and a flags
	// field's BITS are each a named set indexed by [0, enumMax].
	enumMax := "-1L"
	enumName := "null"
	variantId := "null"
	var armsOf *ir.Union
	switch ref := f.Type.Ref.(type) {
	case *ir.Enum:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%dL", ref.Max)
			enumName = fmt.Sprintf("(v) -> %s.enumName%s(v)", g.declFile(f.Type.Name), f.Type.Name)
			variantId = fmt.Sprintf("(v) -> TableEnumId.%s(v)", javaName(f.Type.Name))
		}
	case *ir.Flags:
		if f.Type.Kind == ir.TNamed {
			// a flags mask is the wire's one POSITIONAL vocabulary
			// (docs/SPEC-TABLES.md §4): its variants are BIT POSITIONS, so the
			// descriptor names bits, and there is no variant id.
			enumMax = fmt.Sprintf("%dL", len(ref.Variants)-1)
			enumName = fmt.Sprintf("(v) -> %s.flagName%s((int) v)", g.declFile(f.Type.Name), f.Type.Name)
		}
	case *ir.Union:
		if f.Type.Kind == ir.TNamed && f.Array == ir.ArrayNone {
			enumMax = fmt.Sprintf("%dL", len(ref.Variants))
			enumName = unionArmLambda(ref, func(v ir.UnionVariant) string {
				return fmt.Sprintf("%q", v.Name)
			}, `"???"`, `"None"`)
			variantId = unionArmLambda(ref, func(v ir.UnionVariant) string {
				return fmt.Sprintf("0x%04x", ir.VariantId(v.Name))
			}, "0", "0")
			armsOf = ref
		}
	}

	g.pf("    f = new TableFieldInfo();\n")
	g.pf("    f.name = %q; f.json = %q; f.typeName = %q; f.id = 0x%04x; f.kind = %d; f.isArray = %v; f.counted = %v; f.optional = %v; f.arrayBound = %s; f.elemWidth = %d;\n",
		f.Name, ir.TableFieldJsonKey(f), tableFieldTypeName(f), id, kind, isArray, counted, f.Type.Optional, bound, javaElemWidth(f.Type))
	g.pf("    f.hasRange = %s; f.rangeMin = %s; f.rangeMax = %s; f.enumMax = %s; f.guard = %q;\n",
		hasRange, rangeMin, rangeMax, enumMax, guard)
	g.pf("    f.enumName = %s; f.variantId = %s;\n", enumName, variantId)
	g.pf("    f.keyTypeName = %s; f.keyName = %s; f.keyId = %s;\n", keyTypeName, keyName, keyId)
	g.pf("    f.tableRef = %s;\n", tableRef)
	g.pf("    %s\n", g.tableStorageColumns(f))
	if armsOf != nil {
		g.pf("    {\n")
		g.emitUnionArms(armsOf)
		g.pf("    }\n")
	}
	g.pf("    %s\n", annotationColumns("f", f.Doc, f.Tags, member(f)+"Tags"))
	g.pf("    fields[%d] = f;\n", index)
}
