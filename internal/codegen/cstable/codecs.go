// TABLE-wire storage, codec and descriptor emission for C# (docs/SPEC-TABLES.md),
// mirroring internal/codegen/cpptable — the reference. Readers restore
// declared defaults then overlay, skip unknown ids, skip kind mismatches,
// clamp out-of-range values, and count every event.
//
// The form-1 walk is in wire.go; storage and typed descriptors live here.
package cstable

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// csFieldType maps a field type to its C# storage spelling, mirroring the
// packet emitter's conventions so closure classes from <Base>.cs and table
// classes from this file read as one family.
func csFieldType(t ir.FieldType) string {
	if t.Blob() {
		return "TableBlob"
	}
	switch t.Kind {
	case ir.TInt, ir.TFixed:
		if t.Width == 128 {
			if t.Signed {
				return "System.Int128"
			}
			return "System.UInt128"
		}
		if t.Signed {
			return csInt(t.Width)
		}
		return csUint(t.Width)
	case ir.TBits:
		if t.Width <= 32 {
			return "uint"
		}
		return "ulong"
	case ir.TBool:
		return "bool"
	case ir.TFloat32:
		return "float"
	case ir.TFloat64:
		return "double"
	case ir.TNamed:
		if _, isFlags := t.Ref.(*ir.Flags); isFlags {
			// flags-typed fields store a plain ulong of masks, the packet
			// emitter's spelling exactly (SPEC §4.2)
			return "ulong"
		}
		return t.Name
	}
	return "/* ? */"
}

func csInt(width int) string {
	switch width {
	case 8:
		return "sbyte"
	case 16:
		return "short"
	case 32:
		return "int"
	}
	return "long"
}

func csUint(width int) string {
	switch width {
	case 8:
		return "byte"
	case 16:
		return "ushort"
	case 32:
		return "uint"
	}
	return "ulong"
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

// csIntLit renders an integer literal at the C# type's own width: a value
// past long.MaxValue needs the ul suffix, and long.MinValue has no negative
// literal form in C# (the token would be an unsigned literal negated).
func csIntLit(v *big.Int, signed bool, widthBytes int) string {
	s := v.String()
	if widthBytes < 8 {
		return s
	}
	if !signed {
		return s + "ul"
	}
	if s == "-9223372036854775808" {
		return "long.MinValue"
	}
	return s + "L"
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
// actually clamp at. The decode local is the wire kind's own width, so a
// bound sitting ON that width's limit is a comparison no decoded value can
// satisfy and the emitter drops it — the same "this check cannot fire" test
// the bits(N) width clamp already applies when N is the storage width.
// IT READS THE ENDS THE EMITTER SPELLS — ir.TableRawRange, so a fixed(I,F)
// field's whole-unit bounds are shifted by F first, and the signedness is the
// wire kind's (ir.TableKindSigned), which the signed fixed kinds share with
// the signed integers. Testing the UNSHIFTED ends against an UNSIGNED range
// is how a signed fixed field with min <= 0 lost its low clamp entirely: the
// test said "0 cannot be beaten", the emitter had -min<<F to spell.
// docs/SPEC-TABLES.md §4's semantics are untouched: an elided end is one
// that could never have clamped or counted. C# raises no diagnostic for a
// comparison that cannot fire the way the C++ compilers do (issue #342);
// the emitted shape mirrors C++'s all the same, because one table codec in
// two languages is the point.
func tableClampEnds(f *ir.Field, widthBytes int) (low, high bool) {
	signed := ir.TableKindSigned(ir.TableScalarKind(f))
	lo, hi := tableStorageRange(signed, widthBytes*8)
	rlo, rhi, ok := ir.TableRawRange(f)
	if !ok {
		return false, false
	}
	return rlo.Cmp(lo) > 0, rhi.Cmp(hi) < 0
}

// fieldDefaultExpr renders the C# expression a field's default compares
// against on the write side (elision) — identical values to the storage
// initializers, so measure, save and the reader's prefill agree.
func fieldDefaultExpr(f *ir.Field) string {
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
	case ir.TInt, ir.TBits, ir.TFixed:
		if f.HasDefault && f.DefInt != nil {
			signed := f.Type.Signed
			if f.Type.Width == 128 {
				return wideLiteral(f.DefInt, signed)
			}
			width := 4
			if f.Type.Width > 32 {
				width = 8
			}
			return csIntLit(f.DefInt, signed, width)
		}
		return "0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				return f.Type.Name + "." + f.DefVariant
			}
			return f.Type.Name + ".None"
		case *ir.Flags:
			if f.HasDefault && f.DefInt != nil {
				return f.DefInt.String() + "ul"
			}
			return "0"
		}
	}
	return "0"
}

// member is a field's C# storage member name — the PascalCase mapping the
// packet emitter uses, so one field is spelled one way across a unit.
func member(f *ir.Field) string { return ir.GoExportName(f.Name) }

// ---- storage (table declarations only; closure types come from <Base>.cs) ----

func (g *tableGen) emitTableClass(st *ir.Struct) {
	g.tf("%s", ir.DocComment(st.Doc, "", "//"))
	g.tf("// table %s — TABLE-wire storage: public fields, every buffer allocated at\n", st.Name)
	g.tf("// construction, declared defaults in the field initializers (docs/SPEC-TABLES.md)\n")
	g.tf("public sealed class %s\n{\n", st.Name)
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
	typ := csFieldType(f.Type)
	if f.IsMap() {
		typ = f.MapEntry.Name
	}
	switch {
	case f.IsList(), f.IsMap():
		g.tf("    public %s[] %s = Array.Empty<%s>();\n    public int %sCount;\n", typ, name, typ, name)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.tf("    public %s %s;\n", typ, name)
	case f.Type.Kind == ir.TWString:
		g.tf("    public char[] %s = new char[%d];\n    public int %sLength;\n", name, f.Type.Size, name)
	case f.Type.Kind == ir.TString:
		g.tf("    public byte[] %s = new byte[%d]; // string(%s): max length, used length beside it\n",
			name, f.Type.Size, ir.RenderExpr(f.Type.SizeExpr))
		g.tf("    public int %sLength;\n", name)
	case f.Type.Kind == ir.TBytes:
		g.tf("    public byte[] %s = new byte[%d]; // bytes(%s): fixed buffer, used length beside it\n",
			name, f.Type.Size, ir.RenderExpr(f.Type.SizeExpr))
		g.tf("    public int %sLength;\n", name)
	case f.KeyEnum != "":
		// ONE SLOT PER NAMED VARIANT, the key k at index k-1: nothing is
		// stored for None, and the indexer is the only place the shift
		// appears. Every named slot exists, so there is no count companion,
		// and the type derives its own extent from the enum — nothing outside
		// the array names its size (docs/SPEC-TABLES.md §2.4).
		g.tf("    public TableKeyed<%s, %s> %s = new TableKeyed<%s, %s>(); // [%s]: one slot per named variant, keyed by the value\n",
			typ, f.KeyEnum, name, typ, f.KeyEnum, f.KeyEnum)
	case f.Array == ir.ArrayFixed:
		g.tf("    public %s[] %s = new %s[%d];\n", typ, name, typ, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		g.tf("    public %s[] %s = new %s[%d]; // used count beside it; count in [0, %d]\n",
			typ, name, typ, f.ArrayBound, f.ArrayBound)
		g.tf("    public int %sCount;\n", name)
	default:
		init := ""
		if isClassRef(f.Type) {
			// pre-allocated at construction — the storage principle: nothing
			// heap-allocates per value after it exists
			init = " = new " + f.Type.Name + "()"
		} else if f.HasDefault || enumRef(f) != nil {
			init = " = " + fieldDefaultExpr(f)
			if enumRef(f) != nil {
				for _, sibling := range g.owner.Fields {
					if member(sibling) == f.Type.Name {
						init = " = global::" + capitalize(g.unit.Package) + "." + fieldDefaultExpr(f)
						break
					}
				}
			}
		}
		g.tf("    public %s %s%s;\n", typ, name, init)
	}
	if f.Type.Optional {
		// `?T` — the value plus its presence bool, and nothing else: the
		// holder stays a fixed-size record (docs/SPEC-TABLES.md §2.3). PRESENCE,
		// not content, decides whether the field rides.
		g.tf("    public bool %sPresent; // ?%s: absent until set\n", name, tableFieldTypeName(f))
	}
}

// keyedSlots renders a keyed array's RAW slot storage, the form the codecs
// index by slot number rather than by variant.
//
// A TABLE's keyed field is a TableKeyed<T>, whose slots sit behind .Slots; a
// closure `type`'s field is its PACKET storage — a plain array — because a
// type's class is emitted by the packet backend and nothing on this wire
// changes that (docs/SPEC-TABLES.md §2.4). Both are E.Max elements with the key k
// at index k-1, so only the spelling differs.
func (g *tableGen) keyedSlots(access string, f *ir.Field) string {
	name := access + member(f)
	if f.KeyEnum != "" && g.owner != nil && g.owner.IsTable {
		return name + ".Slots"
	}
	return name
}

// emitElementConstructor pre-allocates the element instances of class-typed
// arrays: every buffer exists at construction, so the read path allocates
// nothing.
func (g *tableGen) emitElementConstructor(st *ir.Struct) {
	var elems, defaults []*ir.Field
	for _, f := range st.Fields {
		if len(f.DefBytes) != 0 {
			defaults = append(defaults, f)
		}
		if f.Array != ir.ArrayNone && isClassRef(f.Type) && !f.Type.Pointer {
			elems = append(elems, f)
		}
	}
	if len(elems) == 0 && len(defaults) == 0 {
		return
	}
	g.tf("\n    public %s()\n    {\n", st.Name)
	for _, f := range elems {
		// a keyed field's slots live behind .Slots, and every one of them is a
		// named variant's: the storage has no None slot (§2.4)
		base := g.keyedSlots("", f)
		g.tf("        for (int i = 0; i < %s.Length; i++)\n        {\n", base)
		g.tf("            %s[i] = new %s();\n        }\n", base, f.Type.Name)
	}
	for _, f := range defaults {
		g.emitBufferDefault(f, "", g.tf)
	}
	g.tf("    }\n")
}

// isClassRef reports a named reference whose C# storage is a class instance:
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

// emitEnumIdentity emits one enum's value <-> table-wire id pair. Emitted by
// the file that DECLARES the enum, once per unit.
func (g *tableGen) emitEnumIdentity(e *ir.Enum) {
	g.pf("// %s on the TABLE wire: a value rides as the 64-bit hash of its VARIANT\n", e.Name)
	g.pf("// NAME, so a variant may be added anywhere, removed, or reordered and old\n")
	g.pf("// data still reads (docs/SPEC-TABLES.md §5). None rides as reference zero.\n")
	g.pf("public static bool TableEnumId(%s value, out ulong id)\n{\n", e.Name)
	g.pf("    switch (value)\n    {\n")
	g.pf("        case %s.None: id = 0; return true;\n", e.Name)
	for _, v := range e.Variants {
		g.pf("        case %s.%s: id = 0x%016x; return true;\n", e.Name, v, ir.TableWireId(e.VariantWireNameOf(v)))
	}
	g.pf("        default: id = 0; return false; // no variant names this value: no wire identity\n")
	g.pf("    }\n}\n\n")
	g.pf("public static bool TableEnumValue(ulong id, out %s value)\n{\n", e.Name)
	g.pf("    switch (id)\n    {\n")
	for _, v := range e.Variants {
		g.pf("        case 0x%016x: value = %s.%s; return true;\n", ir.TableWireId(e.VariantWireNameOf(v)), e.Name, v)
	}
	g.pf("        default: value = %s.None; return false; // an id this build cannot name\n", e.Name)
	g.pf("    }\n}\n\n")
}

// ---- guards ----

// tableGuardExprs composes each guarded field's branch condition against the
// C# storage members ("value.Active && !value.HasTarget").
func tableGuardExprs(st *ir.Struct) map[string]string {
	return guardWalk(st, true)
}

// tableGuardStrings is the schema-facing twin for the reflection descriptors
// ("at_rest", "!at_rest", "active && has_target").
func tableGuardStrings(st *ir.Struct) map[string]string {
	return guardWalk(st, false)
}

func guardWalk(st *ir.Struct, csharp bool) map[string]string {
	name := func(cond string) string {
		if csharp {
			return "value." + ir.GoExportName(cond)
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

// emitTableReset restores a value's declared defaults IN PLACE. It is the C#
// twin of the C++ reader's placement-new prefill — and it is in place on
// purpose: reusing the caller's buffers is what keeps the read path free of
// allocation.
//
// The name is VERB-FIRST and overloaded on the value's type, deliberately:
// docs/SPEC-TABLES.md §11 freezes the name-first suffixes a closure member
// claims, and a port must not quietly mint another. TableReset joins
// TableEnumId/TableEnumValue in the verb-first family instead, which claims
// nothing from a declaration's name.
func (g *tableGen) emitTableReset(st *ir.Struct) {
	g.pf("// TableReset(%s) restores %s's declared defaults in place, reusing every\n", st.Name, st.Name)
	g.pf("// buffer the value already owns. The reader calls it before overlaying.\n")
	g.pf("public static void TableReset(%s value)\n{\n", st.Name)
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
	case f.IsMap(), f.IsList():
		g.pf("    Array.Clear(value.%s, 0, value.%s.Length); value.%sCount = 0;\n", name, name, name)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		g.pf("    value.%s = null;\n", name)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TWString:
		g.pf("    Array.Clear(value.%s, 0, value.%s.Length);\n", name, name)
		g.pf("    value.%sLength = 0;\n", name)
		g.emitBufferDefault(f, "value.", g.pf)
	case f.Array != ir.ArrayNone && isClassRef(f.Type) && !f.Type.Pointer:
		g.pf("    for (int i = 0; i < %s.Length; i++)\n    {\n", g.keyedSlots("value.", f))
		g.pf("        TableReset(%s[i]);\n", g.keyedSlots("value.", f))
		g.pf("    }\n")
		if f.Array == ir.ArrayCounted {
			g.pf("    value.%sCount = 0;\n", name)
		}
	case f.Array != ir.ArrayNone:
		base := g.keyedSlots("value.", f)
		g.pf("    Array.Clear(%s, 0, %s.Length);\n", base, base)
		if f.Array == ir.ArrayCounted {
			g.pf("    value.%sCount = 0;\n", name)
		}
	default:
		if isClassRef(f.Type) {
			g.pf("    TableReset(value.%s);\n", name)
			return
		}
		defaultExpr := fieldDefaultExpr(f)
		if enumRef(f) != nil {
			defaultExpr = "global::" + capitalize(g.unit.Package) + "." + defaultExpr
		}
		g.pf("    value.%s = %s;\n", name, defaultExpr)
	}
}

// emitUnionReset restores a union's tag and every arm's storage in place.
// Tag-only left a previous list arm standing under None (#734); C++ assigns a
// fresh element and C memsets. No allocation: `new T()` trips the zero-allocation
// gate on the C# tables leg.
func (g *tableGen) emitUnionReset(un *ir.Union) {
	saved := g.owner
	g.owner = unionOwner(un)
	g.pf("// TableReset(%s) restores None and every arm in place, reusing buffers.\n", un.Name)
	g.pf("public static void TableReset(%s value)\n{\n", un.Name)
	g.pf("    value.Type = %sType.None;\n", un.Name)
	for _, f := range g.owner.Fields {
		g.emitTableResetField(f)
		if f.Type.Optional {
			g.pf("    value.%sPresent = false;\n", member(f))
		}
	}
	g.pf("}\n\n")
	g.owner = saved
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
	case ir.TWString:
		return "wstring"
	case ir.TFixed:
		return ir.TableTypeSpelling(f)
	case ir.TBytes:
		return "bytes"
	case ir.TMap:
		return ir.TableTypeSpelling(f)
	case ir.TNamed:
		return f.Type.Name
	}
	return "?"
}

// unionArmLambda renders a descriptor lambda over a union's tag values: 0 is
// the empty arm, [1, N] the declared arms in tag order.
func unionArmLambda(un *ir.Union, arm func(ir.UnionVariant) string, unknown, none string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "delegate(ulong v) { switch (v) { case 0: return %s;", none)
	for i, v := range un.Variants {
		fmt.Fprintf(&b, " case %d: return %s;", i+1, arm(v))
	}
	fmt.Fprintf(&b, " default: return %s; } }", unknown)
	return b.String()
}

func bigToDouble(v *big.Int) string {
	f, _ := new(big.Float).SetInt(v).Float64()
	return formatFloat64(f)
}

// emitTableDescriptor emits <X>TableType() — the reflection descriptor, built
// once on first use and published through the nested static holder idiom
// (schema#411).
//
// A descriptor is a mutable object with non-final fields, so under the CLI/CLR
// memory model on weakly ordered architectures (such as ARM64), an unsynchronized
// plain cache write could permit a concurrent caller to read the non-null cache
// reference before the descriptor's fields are fully initialized.
//
// The nested static class holder guarantees thread-safe, one-time
// initialization by the runtime (ECMA-335 §II.10.5.3), with all writes in Build()
// ordered happens-before any read of Instance. All self-referential / pointer
// metadata in Build() is captured in delegates, preventing recursive static
// initialization cycles.
func (g *tableGen) emitTableDescriptor(st *ir.Struct) {
	guards := tableGuardStrings(st)
	holder := st.Name + "TableInfo"
	g.pf("private static class %s\n{\n", holder)
	// the TAG lists (docs/SPEC-TABLES.md §8.1), one static per tagged field and
	// one for a tagged declaration, named from the descriptor row the way the
	// cached descriptor itself is
	var tagged bool
	for _, f := range st.Fields {
		tagged = g.emitTagsStatic(tagsSymbol(st.Name, f.Name), f.Tags) || tagged
	}
	tagged = g.emitTagsStatic(tagsSymbol(st.Name, ""), st.Tags) || tagged
	if tagged {
		g.pf("\n")
	}
	g.pf("    internal static readonly TableTypeInfo Instance = Build();\n\n")
	g.pf("    private static TableTypeInfo Build()\n    {\n")
	g.indent = "    "
	g.pf("    TableTypeInfo info = new TableTypeInfo();\n")
	g.pf("    info.Name = \"%s\";\n", st.Name)
	identity := ir.TableWireId(st.WireName())
	if g.outside {
		identity = 0
	}
	g.pf("    info.Id = 0x%016xul;\n", identity)
	g.pf("    info.NumFields = %d;\n", len(st.Fields))
	g.pf("    info.Create = delegate { return new global::%s.%s(); };\n", capitalize(g.unit.Package), st.Name)
	layout := ir.RecordLayout(g.unit, st)
	align := int64(8)
	for _, table := range g.unit.Tables {
		if a := ir.RecordLayout(g.unit, table).Align; a > align {
			align = a
		}
	}
	g.pf("    info.StorageSize = %d; info.StorageAlign = %d; info.RegionAlign = %d;\n", layout.Size, layout.Align, align)
	g.pf("    info.Variable = %t;\n", ir.VariableTables(g.unit)[st.Name])
	g.pf("    info.RootElemSlots = %d;\n", rootElemSlots(st))
	var pt strings.Builder
	pt.WriteString("info.PointerType = delegate(ulong id) { switch(id) { ")
	for _, target := range ir.PointerReachable(st) {
		fmt.Fprintf(&pt, "case 0x%016xul: return %sTableType(); ", ir.TableWireId(target.WireName()), target.Name)
	}
	pt.WriteString("default: return null; } };\n")
	g.pf("    %s", pt.String())

	var pts strings.Builder
	pts.WriteString("info.PointerTypes = delegate { return new TableTypeInfo[] { ")
	for _, target := range ir.PointerReachable(st) {
		fmt.Fprintf(&pts, "%sTableType(), ", target.Name)
	}
	pts.WriteString("}; };\n")
	g.pf("    %s", pts.String())
	bytesEdge, stringEdge := ir.PointerReachableBlobs(st)
	g.pf("    info.BytesEdge = %t; info.StringEdge = %t;\n", bytesEdge, stringEdge)
	if len(st.Fields) == 0 {
		g.pf("    info.Fields = new TableFieldInfo[0];\n")
	} else {
		g.pf("    info.Fields = new TableFieldInfo[]\n    {\n")
		for _, f := range st.Fields {
			g.emitTableFieldDescriptor(f, guards[f.Name])
		}
		g.pf("    };\n")
	}
	// the RESET hook (docs/SPEC-TABLES.md §8.1): the one column the descriptors
	// cannot express without a function — a generic walker that FILLS a value
	// establishes an absent field's defaults through it, holding no type to
	// spell. It is TableReset, the prefill the wire's read path already calls.
	g.pf("    info.Reset = delegate(object o) { TableReset((global::%s.%s)o); };\n", capitalize(g.unit.Package), st.Name)
	// the declaration's own doc and tags (docs/SPEC-TABLES.md §8.1), on the same
	// terms as a field's: the shared empty doc and a null list where the
	// declaration carries none.
	doc, numTags, tags := annotationColumns(st.Doc, st.Tags, tagsSymbol(st.Name, ""))
	g.pf("    info.Doc = %s;\n", doc)
	g.pf("    info.NumTags = %s;\n", numTags)
	g.pf("    info.Tags = %s;\n", tags)
	g.pf("    TableWire.IndexFields(info);\n")
	g.pf("    return info;\n")
	g.indent = ""
	g.pf("    }\n")
	g.pf("}\n\n")
	g.pf("public static TableTypeInfo %sTableType()\n{\n", st.Name)
	g.pf("    return %s.Instance;\n", holder)
	g.pf("}\n\n")
}

// tagsSymbol names the tag-list static one descriptor row points at: a
// table's own for the empty field name, a field's otherwise. Name-first, as
// <Name>TableInfo beside it is, so the runtime's own namespace is untouched
// (docs/SPEC-TABLES.md §11).
func tagsSymbol(owner, field string) string {
	return owner + "TableTags" + ir.GoExportName(field)
}

// emitTagsStatic emits one tag list as a static array of string literals
// (docs/SPEC-TABLES.md §8.1), and nothing at all for an item with no tags:
// absence is 0 and null in the row, never a per-row empty array. Built once
// with the descriptor and never mutated, so a walk over it allocates nothing.
// It reports whether anything was emitted.
func (g *tableGen) emitTagsStatic(name string, tags []string) bool {
	if len(tags) == 0 {
		return false
	}
	g.pf("    private static readonly string[] %s = { %s };\n", name, ir.QuotedTags(tags))
	return true
}

// annotationColumns renders a row's Doc, NumTags and Tags columns: the shared
// empty doc and a null list where the item carries none.
func annotationColumns(doc string, tags []string, tagsName string) (string, string, string) {
	docColumn := "TableDocNone"
	if doc != "" {
		docColumn = ir.QuoteDoc(doc)
	}
	list := "null"
	if len(tags) > 0 {
		list = tagsName
	}
	return docColumn, fmt.Sprintf("%d", len(tags)), list
}

// ---- the storage columns: C#'s spelling of C++'s offset and elem_size ----

// storageExpr is the C# expression for a field's storage member on an instance
// reached as `o`, cast back to its own class. A keyed array's slots live behind
// .Slots on a table and are a plain array on a closure `type` (§2.4), and
// keyedSlots already knows which.
func (g *tableGen) storageExpr(f *ir.Field) string {
	return g.keyedSlots(fmt.Sprintf("((global::%s.%s)o).", capitalize(g.unit.Package), g.owner.Name), f)
}

// elementExpr is storageExpr indexed where the field is an array, and
// storageExpr itself where it is not — the walker passes 0 for a scalar.
func (g *tableGen) elementExpr(f *ir.Field) string {
	if f.Array != ir.ArrayNone || f.IsMap() {
		return g.storageExpr(f) + "[i]"
	}
	return g.storageExpr(f)
}

// csRawGet renders one element as the ulong the descriptor's GetRaw hands back:
// an integer sign-extended, a bool as 0 or 1, an enum or flags mask as its
// value, a float as its IEEE-754 bit pattern. Sign extension happens HERE
// rather than in the walker, because the C# storage type already knows its own
// signedness and C++'s width switch has nothing to switch on.
func csRawGet(expr string, t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return expr + " ? 1ul : 0ul"
	case ir.TFloat32:
		return "(ulong)unchecked((uint)BitConverter.SingleToInt32Bits(" + expr + "))"
	case ir.TFloat64:
		return "unchecked((ulong)BitConverter.DoubleToInt64Bits(" + expr + "))"
	case ir.TInt:
		if t.Signed {
			return "(ulong)(long)" + expr
		}
		return "(ulong)" + expr
	case ir.TBits:
		return "(ulong)" + expr
	case ir.TNamed:
		if _, isFlags := t.Ref.(*ir.Flags); isFlags {
			return expr
		}
		return "(ulong)" + expr
	}
	return "0ul"
}

// csRawSet is its inverse. The cast is unchecked because the walker has already
// clamped to the field's declared range and to its storage width (§16.2), so a
// value reaching here fits — and an unchecked build and a checked one must
// generate the same source.
func csRawSet(expr, src string, t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return expr + " = " + src + " != 0;"
	case ir.TFloat32:
		return expr + " = BitConverter.Int32BitsToSingle(unchecked((int)(uint)" + src + "));"
	case ir.TFloat64:
		return expr + " = BitConverter.Int64BitsToDouble(unchecked((long)" + src + "));"
	case ir.TInt:
		if t.Signed {
			return expr + " = unchecked((" + csFieldType(t) + ")(long)" + src + ");"
		}
		return expr + " = unchecked((" + csFieldType(t) + ")" + src + ");"
	case ir.TBits:
		return expr + " = unchecked((" + csFieldType(t) + ")" + src + ");"
	case ir.TNamed:
		if _, isFlags := t.Ref.(*ir.Flags); isFlags {
			return expr + " = " + src + ";"
		}
		return expr + " = unchecked((" + t.Name + ")" + src + ");"
	}
	return ""
}

// csElemWidth is the STORAGE width of one element in bytes — C++'s elem_size
// where it has a C# meaning, and the last bound a numeric read clamps to
// (§16.2). 0 on every kind whose storage is not a fixed-width number.
func csElemWidth(t ir.FieldType) int {
	switch t.Kind {
	case ir.TBool:
		return 1
	case ir.TFloat32:
		return 4
	case ir.TFloat64:
		return 8
	case ir.TInt, ir.TFixed:
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
// generic walker reaches the value through. Exactly one of GetRaw/GetChild/
// GetBuffer is non-null, and the companions follow the counted and optional
// columns beside them.
func (g *tableGen) tableStorageColumns(f *ir.Field) string {
	var b strings.Builder
	name := member(f)
	ownerType := "global::" + capitalize(g.unit.Package) + "." + g.owner.Name
	cast := fmt.Sprintf("((%s)o).", ownerType)
	switch {
	case f.Type.Pointer, f.IsMap():
		typ := csFieldType(f.Type)
		if f.IsMap() {
			typ = f.MapEntry.Name
		}
		fmt.Fprintf(&b, ", GetChild = delegate(object o, int i) { return %s; }, SetChild = delegate(object o, int i, object v) { %s = (%s)v; }", g.elementExpr(f), g.elementExpr(f), typ)
	case f.Type.Kind == ir.TWString:
		fmt.Fprintf(&b, ", GetChars = delegate(object o) { return %s; }, GetCount = delegate(object o) { return %s%sLength; }, SetCount = delegate(object o, int n) { %s%sLength = n; }", g.storageExpr(f), cast, name, cast, name)
	case ir.TableKindWide(tableScalarKind(f)):
		fmt.Fprintf(&b, ", GetWide = delegate(object o, int i) { return unchecked((UInt128)(%s)%s); }, SetWide = delegate(object o, int i, UInt128 r) { %s = unchecked((%s)r); }", csFieldType(f.Type), g.elementExpr(f), g.elementExpr(f), csFieldType(f.Type))
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		fmt.Fprintf(&b, ", GetBuffer = delegate(object o) { return %s; }", g.storageExpr(f))
		fmt.Fprintf(&b, ", GetCount = delegate(object o) { return %s%sLength; }", cast, name)
		fmt.Fprintf(&b, ", SetCount = delegate(object o, int n) { %s%sLength = n; }", cast, name)
	case isClassRef(f.Type):
		fmt.Fprintf(&b, ", GetChild = delegate(object o, int i) { return %s; }", g.elementExpr(f))
	default:
		fmt.Fprintf(&b, ", GetRaw = delegate(object o, int i) { return %s; }", csRawGet(g.elementExpr(f), f.Type))
		fmt.Fprintf(&b, ", SetRaw = delegate(object o, int i, ulong r) { %s }", csRawSet(g.elementExpr(f), "r", f.Type))
	}
	if f.CountedOnWire() || f.IsMap() {
		fmt.Fprintf(&b, ", GetCount = delegate(object o) { return %s%sCount; }", cast, name)
		fmt.Fprintf(&b, ", SetCount = delegate(object o, int n) { %s%sCount = n; }", cast, name)
	}
	if f.IsMap() || f.IsList() {
		typ := csFieldType(f.Type)
		class := isClassRef(f.Type) && !f.Type.Pointer
		if f.IsMap() {
			typ = f.MapEntry.Name
			class = true
		}
		fmt.Fprintf(&b, ", EnsureCount = delegate(object o, int n) { var value = (%s)o; if (value.%s.Length < n) { Array.Resize(ref value.%s, Math.Max(n, Math.Max(4, value.%s.Length * 2))); }", ownerType, name, name, name)
		if class {
			fmt.Fprintf(&b, " for (int i = 0; i < n; i++) { if (value.%s[i] == null) { value.%s[i] = new global::%s.%s(); } }", name, name, capitalize(g.unit.Package), typ)
		}
		b.WriteString(" }")
	}
	if f.Type.Optional {
		fmt.Fprintf(&b, ", GetPresent = delegate(object o) { return %s%sPresent; }", cast, name)
		fmt.Fprintf(&b, ", SetPresent = delegate(object o, bool p) { %s%sPresent = p; }", cast, name)
	}
	return b.String()
}

func (g *tableGen) emitTableFieldDescriptor(f *ir.Field, guard string) {
	id := ir.TableFieldWireId(f)
	if g.outside {
		id = 0
	}
	kind := tableScalarKind(f)
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		kind = tkU8
	}
	isArray := f.Array != ir.ArrayNone || f.IsMap() || (f.Type.Kind == ir.TBytes && !f.Type.Pointer)
	counted := f.CountedOnWire() || f.IsMap() || (!f.Type.Pointer && (f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString))

	// the count column, spelled the way the storage spells its own extent: a
	// keyed array DERIVES it from the key enum, so nothing outside the array
	// names its size (docs/SPEC-TABLES.md §2.4, §8.1)
	bound := "0"
	switch {
	case f.IsMap(), f.IsList():
		bound = "int.MaxValue"
	case f.KeyEnum != "":
		bound = fmt.Sprintf("(int)global::%s.%s.Max", capitalize(g.unit.Package), f.KeyEnum)
	case f.Array != ir.ArrayNone:
		bound = strconv.FormatInt(f.ArrayBound, 10)
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString:
		bound = strconv.FormatInt(f.Type.Size, 10)
	}

	tableRef := "null"
	if f.IsMap() {
		tableRef = fmt.Sprintf("delegate { return %sTableType(); }", f.MapEntry.Name)
		kind = 13
	}
	if _, isStruct := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && isStruct {
		tableRef = fmt.Sprintf("delegate { return %sTableType(); }", f.Type.Name)
	}

	// the KEY's vocabulary on an enum-keyed array (docs/SPEC-TABLES.md §8):
	// functions of the KEY, not of the storage index — a walker stepping
	// [0, ArrayBound) asks about index + 1 and prints slots by name without
	// the schema files. KeyId(0) is 0 and KeyName(0) is "None", the reserved
	// id that says None keys no slot; no storage index maps to it (§2.4, §8).
	keyTypeName, keyName, keyId := "null", "null", "null"
	if f.KeyEnum != "" {
		keyTypeName = fmt.Sprintf("%q", f.KeyEnum)
		keyName = fmt.Sprintf("delegate(ulong v) { return EnumName%s(v); }", f.KeyEnum)
		keyId = fmt.Sprintf("delegate(ulong v) { ulong id; TableEnumId((global::%s.%s)v, out id); return id; }", capitalize(g.unit.Package), f.KeyEnum)
	}

	hasRange := "false"
	rangeMin, rangeMax := "0.0", "0.0"
	if f.Type.Kind == ir.TBits && !f.HasIntRange {
		// bits(N) declares its range by its WIDTH: [0, 2^N - 1]. The codec has
		// always clamped a read to it (docs/SPEC-TABLES.md §4); carrying it here is
		// what lets a generic walker apply the same bound without re-deriving
		// it from the type name (§8.1).
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
	// field's BITS are each a named set indexed by [0, EnumMax]. An enum's and
	// a union's names carry the table-wire id they ride under; a flags variant
	// has none, and that missing id is what tells the two apart at runtime
	// (docs/SPEC-TABLES.md §4, §5, §8).
	enumMax := "-1"
	enumName := "null"
	variantId := "null"
	arms := "null"
	switch ref := f.Type.Ref.(type) {
	case *ir.Enum:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", ref.Max)
			enumName = fmt.Sprintf("delegate(ulong v) { return EnumName%s(v); }", f.Type.Name)
			variantId = fmt.Sprintf("delegate(ulong v) { ulong id; TableEnumId((global::%s.%s)v, out id); return id; }", capitalize(g.unit.Package), f.Type.Name)
		}
	case *ir.Flags:
		if f.Type.Kind == ir.TNamed {
			// a flags mask is the wire's one POSITIONAL vocabulary
			// (docs/SPEC-TABLES.md §4): its variants are BIT POSITIONS, so the
			// descriptor names bits, and there is no variant id.
			enumMax = fmt.Sprintf("%d", len(ref.Variants)-1)
			enumName = fmt.Sprintf("delegate(ulong v) { return FlagName%s((int)v); }", f.Type.Name)
		}
	case *ir.Union:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", len(ref.Variants))
			enumName = unionArmLambda(ref, func(v ir.UnionVariant) string {
				return fmt.Sprintf("%q", v.Name)
			}, "\"???\"", "\"None\"")
			variantId = unionArmLambda(ref, func(v ir.UnionVariant) string {
				return fmt.Sprintf("(ulong)0x%016x", ir.TableWireId(v.WireName()))
			}, "(ulong)0", "(ulong)0")
			arms = g.unionArmsValue(ref)
		}
	}

	if g.outside {
		if variantId != "null" {
			variantId = "delegate(ulong v) { return 0; }"
		}
		if keyId != "null" {
			keyId = "delegate(ulong v) { return 0; }"
		}
	}

	// what a PERSON wrote about the field (docs/SPEC-TABLES.md §8.1): its ///
	// block and its tags, the shared empty doc and a null list where it
	// carries none.
	doc, numTags, tags := annotationColumns(f.Doc, f.Tags, tagsSymbol(g.owner.Name, f.Name))
	if g.arm && len(f.Tags) > 0 {
		tags = armTags(f.Tags)
	}

	jsonName := fmt.Sprintf("%q", ir.TableFieldJsonKey(f))
	if g.outside {
		jsonName = "null"
	}
	g.pf("        new TableFieldInfo { Name = \"%s\", Json = %s, TypeName = \"%s\", Id = 0x%016x, Kind = %d, IsArray = %v, Counted = %v, Optional = %v, ArrayBound = %s, ElemWidth = %d, HasRange = %s, RangeMin = %s, RangeMax = %s, EnumMax = %s, EnumName = %s, VariantId = %s, KeyTypeName = %s, KeyName = %s, KeyId = %s, Guard = \"%s\", TableRef = %s, Arms = %s, Doc = %s, NumTags = %s, Tags = %s%s },\n",
		f.Name, jsonName, tableFieldTypeName(f), id, kind, isArray, counted, f.Type.Optional, bound,
		csElemWidth(f.Type), hasRange, rangeMin, rangeMax, enumMax, enumName, variantId,
		keyTypeName, keyName, keyId, guard, tableRef, arms, doc, numTags, tags, g.tableStorageColumns(f)+g.wireColumns(f))
}

func rootElemSlots(st *ir.Struct) int {
	slots := int64(0)
	for _, f := range st.Fields {
		if f.KeyEnum == "" && tableScalarKind(f) == ir.TableKindTable {
			if f.IsList() || f.ArrayBound >= 256 {
				return 256
			}
			if f.Array != ir.ArrayNone && f.ArrayBound > 0 {
				slots += f.ArrayBound
				if slots >= 256 {
					return 256
				}
			}
		}
	}
	return int(slots)
}
