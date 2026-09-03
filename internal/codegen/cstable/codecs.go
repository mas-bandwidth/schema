// TABLE-wire storage, codec and descriptor emission for C# (SPEC-TABLES.md),
// mirroring internal/codegen/cpptable — the reference. Readers restore
// declared defaults then overlay, skip unknown ids, skip kind mismatches,
// clamp out-of-range values, and count every event.
//
// SEAM — the fixed-class language items landing beside this port
// (schema#260 `?T` optional fields, schema#255 `[E]T` enum-keyed arrays) are
// NOT implemented here: this backend emits the fixed class exactly as main
// defines it today. Both fold in at three places, and nowhere else:
//   - emitTableStorageField / emitTableReset — the storage spelling and its
//     reset (an optional's presence companion; an enum-keyed array's extent),
//   - emitTableMeasureField / emitTableWriteField — the elision decision
//     (an absent optional writes nothing; an enum-keyed array rides as the
//     array kind its C++ twin picks),
//   - emitTableReadField — the overlay.
//
// Follow whatever the C++ reference does at those same three sites.
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
	switch t.Kind {
	case ir.TInt:
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
	case ir.TInt, ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			signed := f.Type.Kind == ir.TInt && f.Type.Signed
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
	g.tf("// table %s — TABLE-wire storage: public fields, every buffer allocated at\n", st.Name)
	g.tf("// construction, declared defaults in the field initializers (SPEC-TABLES.md)\n")
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
		g.emitTableStorageField(f)
	}
	g.emitElementConstructor(st)
	g.tf("}\n\n")
}

func (g *tableGen) emitTableStorageField(f *ir.Field) {
	name := member(f)
	typ := csFieldType(f.Type)
	switch {
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
		// the array names its size (SPEC-TABLES.md §2.4).
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
		}
		g.tf("    public %s %s%s;\n", typ, name, init)
	}
	if f.Type.Optional {
		// `?T` — the value plus its presence bool, and nothing else: the
		// holder stays a fixed-size record (SPEC-TABLES.md §2.3). PRESENCE,
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
// changes that (SPEC-TABLES.md §2.4). Both are E.Max elements with the key k
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
	var elems []*ir.Field
	for _, f := range st.Fields {
		if f.Array != ir.ArrayNone && isClassRef(f.Type) {
			elems = append(elems, f)
		}
	}
	if len(elems) == 0 {
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

// ---- enum identity on the table wire (SPEC-TABLES.md §5) ----

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
	g.pf("// %s on the TABLE wire: a value rides as the u16 hash of its VARIANT\n", e.Name)
	g.pf("// NAME, so a variant may be added anywhere, removed, or reordered and old\n")
	g.pf("// data still reads (SPEC-TABLES.md §5). None is the one reserved id, 0.\n")
	g.pf("public static bool TableEnumId(%s value, out ushort id)\n{\n", e.Name)
	g.pf("    switch (value)\n    {\n")
	g.pf("        case %s.None: id = 0; return true;\n", e.Name)
	for _, v := range e.Variants {
		g.pf("        case %s.%s: id = 0x%04x; return true;\n", e.Name, v, ir.VariantId(v))
	}
	g.pf("        default: id = 0; return false; // no variant names this value: no wire identity\n")
	g.pf("    }\n}\n\n")
	g.pf("public static bool TableEnumValue(ushort id, out %s value)\n{\n", e.Name)
	g.pf("    switch (id)\n    {\n")
	g.pf("        case 0: value = %s.None; return true;\n", e.Name)
	for _, v := range e.Variants {
		g.pf("        case 0x%04x: value = %s.%s; return true;\n", ir.VariantId(v), e.Name, v)
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
// SPEC-TABLES.md §11 freezes the 23 name-first suffixes a closure member
// claims, and a port must not quietly mint a 24th. TableReset joins
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
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("    Array.Clear(value.%s, 0, value.%s.Length);\n", name, name)
		g.pf("    value.%sLength = 0;\n", name)
	case f.Array != ir.ArrayNone && isClassRef(f.Type):
		g.pf("    for (int i = 0; i < %s.Length; i++)\n    {\n", g.keyedSlots("value.", f))
		if _, isUnion := f.Type.Ref.(*ir.Union); isUnion {
			g.pf("        %s[i].Type = %sType.None;\n", g.keyedSlots("value.", f), f.Type.Name)
		} else {
			g.pf("        TableReset(%s[i]);\n", g.keyedSlots("value.", f))
		}
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
		if _, isUnion := f.Type.Ref.(*ir.Union); isUnion && f.Type.Kind == ir.TNamed {
			// the tag is the whole reset: an arm zero-establishes when the
			// reader selects it, exactly as the packet reader does
			g.pf("    value.%s.Type = %sType.None;\n", name, f.Type.Name)
			return
		}
		if isClassRef(f.Type) {
			g.pf("    TableReset(value.%s);\n", name)
			return
		}
		g.pf("    value.%s = %s;\n", name, fieldDefaultExpr(f))
	}
}

// ---- measure ----

// emitTableMeasure emits <X>Measure: the EXACT encoded size of a value,
// writing nothing — the parallel-generation lever. Every nested table on the
// wire is length-prefixed, so a caller can measure subtables in parallel,
// prefix-sum the offsets and scatter-write disjoint ranges. Mirrors
// <X>SaveBody's elision decisions branch for branch: for any value, Save
// writes exactly this many bytes into a buffer of exactly this size. A value
// violating its storage invariants (a count or length outside its bound, an
// out-of-range union tag, an enum value no variant names) measures as -1,
// exactly as the write side refuses it.
func (g *tableGen) emitTableMeasure(st *ir.Struct) {
	g.pf("public static long %sMeasure(%s value)\n{\n", st.Name, st.Name)
	g.pf("    long bytes = 2; // terminator\n")
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("    if (%s)\n    {\n", cond)
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
		// an optional's PRESENCE is the payload: it rides even when the value
		// is entirely default, exactly as a pointer's pointee does — otherwise
		// absent and present-at-default would be one value on the wire
		g.pf("    if (value.%sPresent) // ?%s: presence decides, not content\n    {\n", name, tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.pf("        long body = %sMeasure(value.%s);\n", f.Type.Name, name)
			g.pf("        if (body < 0) { return -1; }\n")
			g.pf("        bytes += 3 + 4 + body; // %s\n", f.Name)
		case enumRef(f) != nil:
			g.pf("        ushort id;\n")
			g.pf("        if (!TableEnumId(value.%s, out id)) { return -1; } // no variant names this value\n", name)
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
		g.pf("        for (int i = 0; i < %d; i++) // [%s]: every stored slot is a named variant's\n        {\n", f.ArrayBound, f.KeyEnum)
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
		g.pf("    if (value.%sCount > 0)\n    {\n", name)
		g.pf("        bytes += 3 + 4 + 5; // %s\n", f.Name)
		g.pf("        for (int i = 0; i < value.%sCount; i++)\n        {\n", name)
		g.pf("            long elem = %sMeasure(value.%s[i]);\n", f.Type.Name, name)
		g.pf("            if (elem < 0) { return -1; }\n")
		g.pf("            bytes += 4 + elem;\n")
		g.pf("        }\n    }\n")
	case f.Array == ir.ArrayCounted:
		g.pf("    if (value.%sCount < 0 || value.%sCount > %d) { return -1; } // storage invariant\n", name, name, f.ArrayBound)
		g.pf("    if (value.%sCount > 0)\n    {\n", name)
		g.emitEnumElementCheck(f, fmt.Sprintf("value.%s[i]", name), fmt.Sprintf("value.%sCount", name), "        ", "return -1;")
		g.pf("        bytes += 3 + 4 + 5 + (long)value.%sCount * %d; // %s\n", name, width, f.Name)
		g.pf("    }\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		g.pf("    {\n")
		g.pf("        bytes += 3 + 4 + 5; // %s (fixed [%d])\n", f.Name, f.ArrayBound)
		g.pf("        for (int i = 0; i < %d; i++)\n        {\n", f.ArrayBound)
		g.pf("            long elem = %sMeasure(value.%s[i]);\n", f.Type.Name, name)
		g.pf("            if (elem < 0) { return -1; }\n")
		g.pf("            bytes += 4 + elem;\n")
		g.pf("        }\n    }\n")
	case f.Array == ir.ArrayFixed:
		g.pf("    {\n")
		g.pf("        bool allDefault = true;\n")
		g.pf("        for (int i = 0; i < %d; i++) { if (value.%s[i] != %s) { allDefault = false; break; } }\n",
			f.ArrayBound, name, fieldDefaultExpr(f))
		g.pf("        if (!allDefault)\n        {\n")
		g.emitEnumElementCheck(f, fmt.Sprintf("value.%s[i]", name), fmt.Sprintf("%d", f.ArrayBound), "            ", "return -1;")
		g.pf("            bytes += 3 + 4 + 5 + %d; // %s\n", f.ArrayBound*int64(width), f.Name)
		g.pf("        }\n    }\n")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("    switch (value.%s.Type) // %s\n    {\n", name, f.Name)
		g.pf("        case %sType.None: break; // None elides — TLV absence is the None\n", un.Name)
		for _, v := range un.Variants {
			g.pf("        case %sType.%s:\n        {\n", un.Name, ir.GoExportName(v.Name))
			g.pf("            long arm = %sMeasure(value.%s.%s);\n", v.Type, name, ir.GoExportName(v.Name))
			g.pf("            if (arm < 0) { return -1; }\n")
			g.pf("            bytes += 3 + 2 + 4 + arm; // the u16 ARM ID, then the arm length-prefixed\n            break;\n        }\n")
		}
		g.pf("        default: return -1; // invalid tag — the write side refuses it too\n")
		g.pf("    }\n")
	case kind == tkTable:
		g.pf("    {\n")
		g.pf("        long body = %sMeasure(value.%s);\n", f.Type.Name, name)
		g.pf("        if (body < 0) { return -1; }\n")
		g.pf("        if (body > 2) { bytes += 3 + 4 + body; } // %s: all-default nested elides\n", f.Name)
		g.pf("    }\n")
	case enumRef(f) != nil:
		g.pf("    if (value.%s != %s)\n    {\n", name, fieldDefaultExpr(f))
		g.pf("        ushort id;\n")
		g.pf("        if (!TableEnumId(value.%s, out id)) { return -1; } // no variant names this value\n", name)
		g.pf("        bytes += 3 + 2; // %s: the variant's name hash\n    }\n", f.Name)
	default:
		g.pf("    if (value.%s != %s) { bytes += 3 + %d; } // %s\n", name, fieldDefaultExpr(f), width, f.Name)
	}
}

// emitKeyedSlotRides emits the head of an enum-keyed array's per-slot loop
// (SPEC-TABLES.md §2.4, §3.2). It elides a slot holding its default, refuses a
// slot whose value or whose KEY no variant names — a value with no wire
// identity is refused rather than silently renamed, the enum rule applied to
// slots — and leaves `keyId` holding the slot's wire id. For a table element
// `elemBytes` holds the measured body, so measure and save decide elision on
// the same number.
func (g *tableGen) emitKeyedSlotRides(f *ir.Field, kind int, ind, onBad string) {
	expr := g.keyedSlots("value.", f) + "[i]"
	switch {
	case kind == tkTable:
		g.pf("%slong elemBytes = %sMeasure(%s);\n", ind, f.Type.Name, expr)
		g.pf("%sif (elemBytes < 0) { %s }\n", ind, onBad)
		g.pf("%sif (elemBytes <= 2) { continue; } // an all-default slot elides\n", ind)
	case enumRef(f) != nil:
		g.pf("%sif (%s == %s) { continue; } // a default slot elides\n", ind, expr, fieldDefaultExpr(f))
		g.pf("%sushort elementId;\n", ind)
		g.pf("%sif (!TableEnumId(%s, out elementId)) { %s } // no variant names this value\n", ind, expr, onBad)
	default:
		g.pf("%sif (%s == %s) { continue; } // a default slot elides\n", ind, expr, fieldDefaultExpr(f))
	}
	g.pf("%sushort keyId;\n", ind)
	g.pf("%sif (!TableEnumId((%s)(i + 1), out keyId)) { %s } // i is the STORAGE index; the key it holds is i + 1\n",
		ind, f.KeyEnum, onBad)
}

// emitEnumElementCheck validates an enum ARRAY's elements before they ride: a
// value no variant names has no wire identity, so the value is refused rather
// than silently written as None (the union tag's rule, applied to enums).
func (g *tableGen) emitEnumElementCheck(f *ir.Field, expr, count, ind, onBad string) {
	if enumRef(f) == nil {
		return
	}
	g.pf("%sfor (int i = 0; i < %s; i++) // %s: every element must be nameable\n", ind, count, f.Name)
	g.pf("%s{\n%s    ushort elementId;\n", ind, ind)
	g.pf("%s    if (!TableEnumId(%s, out elementId)) { %s }\n%s}\n", ind, expr, onBad, ind)
}

// ---- write / save ----

func (g *tableGen) emitTableWrite(st *ir.Struct) {
	g.pf("public static bool %sSaveBody(ref TableWriter w, %s value)\n{\n", st.Name, st.Name)
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("    if (%s)\n    {\n", cond)
			g.indent = "    "
			g.emitTableWriteField(f)
			g.indent = ""
			g.pf("    }\n")
			continue
		}
		g.emitTableWriteField(f)
	}
	g.pf("    w.Put16(0); // terminator\n")
	g.pf("    return !w.Overflow;\n}\n\n")
}

// emitTableSave emits the buffer-level entry of the measure/save pair:
// <X>Save writes into the caller's span and returns the bytes written —
// exactly <X>Measure's answer — or -1 when the span is too small. No
// allocation anywhere: the caller owns the buffer.
func (g *tableGen) emitTableSave(st *ir.Struct) {
	g.pf("public static long %sSave(%s value, Span<byte> buffer)\n{\n", st.Name, st.Name)
	g.pf("    TableWriter w = new TableWriter(buffer);\n")
	g.pf("    if (!%sSaveBody(ref w, value)) { return -1; }\n", st.Name)
	g.pf("    return w.Offset; // == %sMeasure(value)\n}\n\n", st.Name)
}

func (g *tableGen) emitTableWriteField(f *ir.Field) {
	name := member(f)
	id := ir.TableFieldId(f)
	kind := tableScalarKind(f)
	switch {
	case f.Type.Optional:
		// present: the payload ALWAYS rides, all-default included — the
		// pointer's rule, and what makes ?T, *T and a plain nesting
		// wire-identical (SPEC-TABLES.md §2.3, §3.1)
		g.pf("    if (value.%sPresent) // ?%s\n    {\n", name, tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.pf("        long body = %sMeasure(value.%s);\n", f.Type.Name, name)
			g.pf("        if (body < 0) { return false; } // storage invariant, refused as measure refuses it\n")
			g.pf("        w.Put16(0x%04x); w.Put8(%d); // %s\n", id, tkTable, f.Name)
			g.pf("        w.Put32((uint)body);\n")
			g.pf("        if (!%sSaveBody(ref w, value.%s)) { return false; }\n", f.Type.Name, name)
		case enumRef(f) != nil:
			g.pf("        ushort variantId;\n")
			g.pf("        if (!TableEnumId(value.%s, out variantId)) { return false; }\n", name)
			g.pf("        w.Put16(0x%04x); w.Put8(%d); // %s\n", id, kind, f.Name)
			g.pf("        w.Put16(variantId);\n")
		default:
			g.pf("        w.Put16(0x%04x); w.Put8(%d); // %s\n", id, kind, f.Name)
			g.emitTableWriteElement(f, kind, "value."+name, "        ")
		}
		g.pf("    }\n")
	case f.KeyEnum != "":
		// the slots ride KEYED: (variant id, length-prefixed element) pairs,
		// counted like any array's elements. Two passes so the count is known
		// before the header rides, and so measure and save agree byte for byte.
		g.pf("    {\n")
		g.pf("        uint pairs = 0;\n")
		g.pf("        for (int i = 0; i < %d; i++) // [%s]: every stored slot is a named variant's\n        {\n", f.ArrayBound, f.KeyEnum)
		g.emitKeyedSlotRides(f, kind, "            ", "return false;")
		g.pf("            pairs++;\n")
		g.pf("        }\n")
		g.pf("        if (pairs > 0)\n        {\n")
		g.pf("            // KIND 16, not 14: a keyed body and a positional one are\n")
		g.pf("            // incompatible, so a reader of the other kind must see a kind\n")
		g.pf("            // mismatch and skip, never misdecode (SPEC-TABLES.md §3.2)\n")
		g.pf("            w.Put16(0x%04x); w.Put8(%d); // %s (keyed by %s)\n", id, tkKeyed, f.Name, f.KeyEnum)
		g.pf("            int lenAt = w.Offset; w.Put32(0);\n")
		g.pf("            w.Put8(%d); w.Put32(pairs);\n", kind)
		g.pf("            // ASCENDING BY VARIANT ORDINAL, which is slot order — this\n")
		g.pf("            // writer's choice, and a reader must not rely on it: every\n")
		g.pf("            // slot is found by its key (SPEC-TABLES.md §3.2)\n")
		g.pf("            for (int i = 0; i < %d; i++)\n            {\n", f.ArrayBound)
		g.emitKeyedSlotRides(f, kind, "                ", "return false;")
		g.pf("                w.Put16(keyId); // the slot's VARIANT id, not its position\n")
		g.pf("                int elemLenAt = w.Offset; w.Put32(0);\n")
		if kind == tkTable {
			g.pf("                if (!%sSaveBody(ref w, %s[i])) { return false; }\n", f.Type.Name, g.keyedSlots("value.", f))
		} else {
			g.emitTableWriteElement(f, kind, g.keyedSlots("value.", f)+"[i]", "                ")
		}
		g.pf("                w.Patch32(elemLenAt, (uint)(w.Offset - elemLenAt - 4));\n")
		g.pf("            }\n")
		g.pf("            w.Patch32(lenAt, (uint)(w.Offset - lenAt - 4));\n")
		g.pf("        }\n    }\n")
	case f.Type.Kind == ir.TString:
		g.pf("    if (value.%sLength < 0 || value.%sLength > %d) { return false; } // storage invariant\n", name, name, f.Type.Size)
		g.pf("    if (value.%sLength > 0)\n    {\n", name)
		g.pf("        w.Put16(0x%04x); w.Put8(%d); // %s\n", id, tkString, f.Name)
		g.pf("        w.Put32((uint)value.%sLength);\n", name)
		g.pf("        w.Raw(new ReadOnlySpan<byte>(value.%s, 0, value.%sLength));\n    }\n", name, name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    if (value.%sLength < 0 || value.%sLength > %d) { return false; } // storage invariant\n", name, name, f.Type.Size)
		g.pf("    if (value.%sLength > 0)\n    {\n", name)
		g.pf("        w.Put16(0x%04x); w.Put8(%d); // %s\n", id, tkArray, f.Name)
		g.pf("        w.Put32((uint)(5 + value.%sLength));\n", name)
		g.pf("        w.Put8(%d); w.Put32((uint)value.%sLength);\n", tkU8, name)
		g.pf("        w.Raw(new ReadOnlySpan<byte>(value.%s, 0, value.%sLength));\n    }\n", name, name)
	case f.Array == ir.ArrayCounted:
		g.pf("    if (value.%sCount < 0 || value.%sCount > %d) { return false; } // storage invariant\n", name, name, f.ArrayBound)
		g.pf("    if (value.%sCount > 0)\n    {\n", name)
		g.pf("        w.Put16(0x%04x); w.Put8(%d); // %s\n", id, tkArray, f.Name)
		g.pf("        int lenAt = w.Offset; w.Put32(0);\n")
		g.pf("        w.Put8(%d); w.Put32((uint)value.%sCount);\n", kind, name)
		g.pf("        for (int i = 0; i < value.%sCount; i++)\n        {\n", name)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "            ")
		g.pf("        }\n")
		g.pf("        w.Patch32(lenAt, (uint)(w.Offset - lenAt - 4));\n    }\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		// fixed arrays of tables always ride — position is identity there
		g.pf("    {\n")
		g.pf("        w.Put16(0x%04x); w.Put8(%d); // %s (fixed [%d])\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("        int lenAt = w.Offset; w.Put32(0);\n")
		g.pf("        w.Put8(%d); w.Put32(%d);\n", kind, f.ArrayBound)
		g.pf("        for (int i = 0; i < %d; i++)\n        {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "            ")
		g.pf("        }\n")
		g.pf("        w.Patch32(lenAt, (uint)(w.Offset - lenAt - 4));\n    }\n")
	case f.Array == ir.ArrayFixed:
		// fixed arrays are positional; an all-default array elides entirely
		// (parity with the reader's restored defaults)
		g.pf("    {\n")
		g.pf("        bool allDefault = true;\n")
		g.pf("        for (int i = 0; i < %d; i++) { if (value.%s[i] != %s) { allDefault = false; break; } }\n",
			f.ArrayBound, name, fieldDefaultExpr(f))
		g.pf("        if (!allDefault)\n        {\n")
		g.pf("            w.Put16(0x%04x); w.Put8(%d); // %s (fixed [%d])\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("            int lenAt = w.Offset; w.Put32(0);\n")
		g.pf("            w.Put8(%d); w.Put32(%d);\n", kind, f.ArrayBound)
		g.pf("            for (int i = 0; i < %d; i++)\n            {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "                ")
		g.pf("            }\n")
		g.pf("            w.Patch32(lenAt, (uint)(w.Offset - lenAt - 4));\n        }\n    }\n")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("    if (value.%s.Type != %sType.None)\n    {\n", name, un.Name)
		g.pf("        w.Put16(0x%04x); w.Put8(%d); // %s\n", id, tkUnion, f.Name)
		g.pf("        // the ARM ID is the hash of the arm's NAME (SPEC-TABLES.md §5), so\n")
		g.pf("        // arms may be added anywhere, removed and reordered\n")
		g.pf("        switch (value.%s.Type)\n        {\n", name)
		for _, v := range un.Variants {
			g.pf("            case %sType.%s: w.Put16(0x%04x); break;\n", un.Name, ir.GoExportName(v.Name), ir.VariantId(v.Name))
		}
		g.pf("            default: return false; // write validates the tag before it rides\n")
		g.pf("        }\n")
		g.pf("        int lenAt = w.Offset; w.Put32(0);\n")
		g.pf("        switch (value.%s.Type)\n        {\n", name)
		for _, v := range un.Variants {
			g.pf("            case %sType.%s: if (!%sSaveBody(ref w, value.%s.%s)) { return false; } break;\n",
				un.Name, ir.GoExportName(v.Name), v.Type, name, ir.GoExportName(v.Name))
		}
		g.pf("            default: return false; // write validates the tag before it rides\n")
		g.pf("        }\n")
		g.pf("        w.Patch32(lenAt, (uint)(w.Offset - lenAt - 4));\n    }\n")
	case kind == tkTable:
		// elision is decided BEFORE any byte is emitted: measuring first keeps
		// an all-default nested field from touching the buffer at all, so
		// saving into a buffer of exactly Measure's size never trips overflow
		// on transient header bytes
		g.pf("    {\n")
		g.pf("        long body = %sMeasure(value.%s);\n", f.Type.Name, name)
		g.pf("        if (body < 0) { return false; } // storage invariant, refused as measure refuses it\n")
		g.pf("        if (body > 2) // all-default nested elides\n        {\n")
		g.pf("            w.Put16(0x%04x); w.Put8(%d); // %s\n", id, tkTable, f.Name)
		g.pf("            w.Put32((uint)body);\n")
		g.pf("            if (!%sSaveBody(ref w, value.%s)) { return false; }\n", f.Type.Name, name)
		g.pf("        }\n    }\n")
	case enumRef(f) != nil:
		// the id is resolved BEFORE the header rides: a value no variant names
		// has no wire identity, and the write refuses it rather than writing None
		g.pf("    if (value.%s != %s)\n    {\n", name, fieldDefaultExpr(f))
		g.pf("        ushort variantId;\n")
		g.pf("        if (!TableEnumId(value.%s, out variantId)) { return false; }\n", name)
		g.pf("        w.Put16(0x%04x); w.Put8(%d); // %s\n", id, kind, f.Name)
		g.pf("        w.Put16(variantId);\n    }\n")
	default:
		g.pf("    if (value.%s != %s)\n    {\n", name, fieldDefaultExpr(f))
		g.pf("        w.Put16(0x%04x); w.Put8(%d); // %s\n", id, kind, f.Name)
		g.emitTableWriteElement(f, kind, "value."+name, "        ")
		g.pf("    }\n")
	}
}

func (g *tableGen) emitTableWriteElement(f *ir.Field, kind int, expr, ind string) {
	if enumRef(f) != nil {
		// writeElementId, not elementId: C# forbids a nested block from
		// declaring a name an enclosing scope already holds, and an
		// enum-keyed array's slot loop is holding one (emitKeyedSlotRides).
		g.pf("%s{\n%s    ushort writeElementId;\n", ind, ind)
		g.pf("%s    if (!TableEnumId(%s, out writeElementId)) { return false; }\n", ind, expr)
		g.pf("%s    w.Put16(writeElementId);\n%s}\n", ind, ind)
		return
	}
	switch kind {
	case tkBool:
		g.pf("%sw.Put8(%s ? (byte)1 : (byte)0);\n", ind, expr)
	case tkF32:
		g.pf("%sw.Put32(TableFloatToBits(%s));\n", ind, expr)
	case tkF64:
		g.pf("%sw.Put64(TableDoubleToBits(%s));\n", ind, expr)
	case tkTable:
		g.pf("%s{\n%s    int elemLenAt = w.Offset; w.Put32(0);\n", ind, ind)
		g.pf("%s    if (!%sSaveBody(ref w, %s)) { return false; }\n", ind, f.Type.Name, expr)
		g.pf("%s    w.Patch32(elemLenAt, (uint)(w.Offset - elemLenAt - 4));\n%s}\n", ind, ind)
	default:
		width := tableKindWidth(kind)
		g.pf("%sw.%s(unchecked((%s)(%s)));\n", ind, tablePut(width), csUint(width*8), expr)
	}
}

// ---- read ----

func (g *tableGen) emitTableRead(st *ir.Struct) {
	g.pf("public static bool %sLoadBody(ref TableReader r, %s value)\n{\n", st.Name, st.Name)
	g.pf("    TableReset(value); // restore declared defaults in place, then overlay\n")
	g.pf("    for (;;)\n    {\n")
	g.pf("        if (!r.Has(2)) { r.Report.Malformed = true; return false; }\n")
	g.pf("        ushort fieldId = r.Get16();\n")
	g.pf("        if (fieldId == 0) { return true; }\n")
	g.pf("        if (!r.Has(1)) { r.Report.Malformed = true; return false; }\n")
	g.pf("        byte kind = r.Get8();\n")
	if len(st.Fields) > 0 {
		g.pf("        switch (fieldId)\n        {\n")
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
				// never decoded as the other body (SPEC-TABLES.md §3.2)
				wireKind = tkKeyed
			}
			if f.Type.Kind == ir.TBytes {
				kind = tkU8 // bytes travel as an array of u8 elements
			}
			g.pf("            case 0x%04x: // %s\n            {\n", id, f.Name)
			g.pf("                if (kind != %d)\n                {\n", wireKind)
			g.pf("                    r.Report.KindMismatch++;\n")
			g.pf("                    if (!r.Skip(kind)) { r.Report.Malformed = true; return false; }\n")
			g.pf("                    break;\n                }\n")
			g.emitTableReadField(f, kind)
			if f.Type.Optional {
				// the field rode, so it is PRESENT — content decides nothing
				// here either (SPEC-TABLES.md §2.3)
				g.pf("                value.%sPresent = true;\n", member(f))
			}
			g.pf("                break;\n            }\n")
		}
		g.pf("            default:\n            {\n")
		g.pf("                r.Report.Unknown++;\n")
		g.pf("                if (!r.Skip(kind)) { r.Report.Malformed = true; return false; }\n")
		g.pf("                break;\n            }\n")
		g.pf("        }\n    }\n}\n\n")
	} else {
		g.pf("        r.Report.Unknown++;\n")
		g.pf("        if (!r.Skip(kind)) { r.Report.Malformed = true; return false; }\n")
		g.pf("    }\n}\n\n")
	}

	g.pf("public static bool %sLoad(%s value, ReadOnlySpan<byte> bytes, TableReport report)\n{\n", st.Name, st.Name)
	g.pf("    TableReader r = new TableReader(bytes, report != null ? report : new TableReport());\n")
	g.pf("    return %sLoadBody(ref r, value);\n}\n\n", st.Name)
}

func (g *tableGen) emitTableReadField(f *ir.Field, kind int) {
	name := member(f)
	ind := "                "
	switch {
	case f.KeyEnum != "":
		// each pair is placed by its VARIANT id, so a slot lands by name
		// however the enum moved; an id this reader cannot name is skipped by
		// its length and counted unknown, and a slot the writer never sent
		// keeps the prefill's default (SPEC-TABLES.md §3.2)
		g.pf("%sif (!r.Has(4)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%suint bodyLen = r.Get32();\n", ind)
		g.pf("%sif (!r.Has(bodyLen)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%sint bodyEnd = r.Offset + (int)bodyLen;\n", ind)
		g.pf("%sif (bodyLen >= 5)\n%s{\n", ind, ind)
		g.pf("%s    byte elemKind = r.Get8();\n", ind)
		g.pf("%s    uint count = r.Get32();\n", ind)
		g.pf("%s    if (elemKind != %d) { r.Report.KindMismatch++; r.Offset = bodyEnd; break; }\n", ind, kind)
		g.pf("%s    TableReader sub = new TableReader(r.Buffer.Slice(r.Offset, bodyEnd - r.Offset), r.Report);\n", ind)
		g.pf("%s    for (uint i = 0; i < count; i++)\n%s    {\n", ind, ind)
		g.pf("%s        if (!sub.Has(2)) { r.Report.Malformed = true; break; }\n", ind)
		g.pf("%s        ushort key = sub.Get16();\n", ind)
		g.pf("%s        if (!sub.Has(4)) { r.Report.Malformed = true; break; }\n", ind)
		g.pf("%s        uint elemLen = sub.Get32();\n", ind)
		g.pf("%s        if (!sub.Has(elemLen)) { r.Report.Malformed = true; break; }\n", ind)
		g.pf("%s        if (key == 0)\n%s        {\n", ind, ind)
		g.pf("%s            // None is the NULL KEY: 0 is the reserved id no declared\n", ind)
		g.pf("%s            // name can fold to, so a body carrying one is DAMAGED, not\n", ind)
		g.pf("%s            // merely foreign. Framing damage stops this body, keeps what\n", ind)
		g.pf("%s            // it decoded, and the parent reads on past the length\n", ind)
		g.pf("%s            // (SPEC-TABLES.md §3.2, §4).\n", ind)
		g.pf("%s            r.Report.Malformed = true;\n%s            break;\n%s        }\n", ind, ind, ind)
		g.pf("%s        %s slot;\n", ind, f.KeyEnum)
		g.pf("%s        if (!TableEnumValue(key, out slot))\n%s        {\n", ind, ind)
		g.pf("%s            r.Report.Unknown++; // a slot this reader cannot name\n", ind)
		g.pf("%s            sub.Offset += (int)elemLen;\n%s            continue;\n%s        }\n", ind, ind, ind)
		g.pf("%s        {\n%s            TableReader elem = new TableReader(sub.Buffer.Slice(sub.Offset, (int)elemLen), r.Report);\n", ind, ind)
		// the key k lives at STORAGE INDEX k-1 (SPEC-TABLES.md §2.4)
		slot := g.keyedSlots("value.", f) + "[(int)slot - 1]"
		if kind == tkTable {
			g.pf("%s            %sLoadBody(ref elem, %s);\n", ind, f.Type.Name, slot)
		} else {
			g.emitTableReadScalarFrom(f, kind, slot, ind+"            ", "elem",
				"r.Report.Malformed = true; sub.Offset += (int)elemLen; continue;")
		}
		g.pf("%s        }\n", ind)
		g.pf("%s        sub.Offset += (int)elemLen;\n", ind)
		g.pf("%s    }\n%s}\n", ind, ind)
		g.pf("%sr.Offset = bodyEnd; // unread pairs and slack skip via the length\n", ind)
	case f.Type.Kind == ir.TString:
		g.pf("%sif (!r.Has(4)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%suint len = r.Get32();\n", ind)
		g.pf("%sif (!r.Has(len)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%suint keep = len;\n", ind)
		g.pf("%sif (keep > %d) { keep = %d; r.Report.Clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%sr.Buffer.Slice(r.Offset, (int)keep).CopyTo(new Span<byte>(value.%s, 0, (int)keep));\n", ind, name)
		g.pf("%svalue.%sLength = (int)keep;\n", ind, name)
		g.pf("%sr.Offset += (int)len;\n", ind)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		counted := f.Type.Kind == ir.TBytes || f.Array == ir.ArrayCounted
		g.pf("%sif (!r.Has(4)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%suint bodyLen = r.Get32();\n", ind)
		g.pf("%sif (!r.Has(bodyLen)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%sint bodyEnd = r.Offset + (int)bodyLen;\n", ind)
		g.pf("%sif (bodyLen >= 5)\n%s{\n", ind, ind)
		g.pf("%s    byte elemKind = r.Get8();\n", ind)
		g.pf("%s    uint count = r.Get32();\n", ind)
		g.pf("%s    if (elemKind != %d) { r.Report.KindMismatch++; r.Offset = bodyEnd; break; }\n", ind, kind)
		g.pf("%s    uint keep = count;\n", ind)
		g.pf("%s    if (keep > %d) { keep = %d; r.Report.Clamped++; }\n", ind, bound, bound)
		g.pf("%s    // elements are BOUNDED by the field body: a count the length\n", ind)
		g.pf("%s    // cannot cover keeps the decoded prefix, flags malformed, and\n", ind)
		g.pf("%s    // the parent continues at the next field — following fields'\n", ind)
		g.pf("%s    // bytes are never fabricated into elements\n", ind)
		g.pf("%s    TableReader sub = new TableReader(r.Buffer.Slice(r.Offset, bodyEnd - r.Offset), r.Report);\n", ind)
		if counted {
			g.pf("%s    uint decoded = 0;\n", ind)
		}
		g.pf("%s    for (uint i = 0; i < keep; i++)\n%s    {\n", ind, ind)
		g.emitTableReadElement(f, kind, ind+"        ")
		if counted {
			g.pf("%s        decoded = i + 1;\n", ind)
		}
		g.pf("%s    }\n", ind)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s    value.%sLength = (int)decoded;\n", ind, name)
		} else if f.Array == ir.ArrayCounted {
			g.pf("%s    value.%sCount = (int)decoded;\n", ind, name)
		}
		g.pf("%s}\n", ind)
		g.pf("%sr.Offset = bodyEnd; // excess elements and slack skip via the length\n", ind)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("%sif (!r.Has(2)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%sushort armId = r.Get16();\n", ind)
		g.pf("%sif (armId == 0) { value.%s.Type = %sType.None; break; } // empty: the id is the whole payload\n", ind, name, un.Name)
		g.pf("%sif (!r.Has(4)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%suint bodyLen = r.Get32();\n", ind)
		g.pf("%sif (!r.Has(bodyLen)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%s{\n%s    TableReader sub = new TableReader(r.Buffer.Slice(r.Offset, (int)bodyLen), r.Report);\n", ind, ind)
		g.pf("%s    switch (armId) // the arm's NAME hash (SPEC-TABLES.md §5)\n%s    {\n", ind, ind)
		for _, v := range un.Variants {
			g.pf("%s        case 0x%04x: // %s\n%s            value.%s.Type = %sType.%s;\n%s            %sLoadBody(ref sub, value.%s.%s);\n%s            break;\n",
				ind, ir.VariantId(v.Name), v.Name, ind, name, un.Name, ir.GoExportName(v.Name),
				ind, v.Type, name, ir.GoExportName(v.Name), ind)
		}
		g.pf("%s        default:\n", ind)
		g.pf("%s            // an arm this reader cannot name: the value reads EMPTY and\n", ind)
		g.pf("%s            // the body is skipped by its length, never misdecoded. The\n", ind)
		g.pf("%s            // reset is explicit, not the prefill's: a repeated field id\n", ind)
		g.pf("%s            // must not leave an arm decoded by an earlier occurrence\n", ind)
		g.pf("%s            // standing (SPEC-TABLES.md §4).\n", ind)
		g.pf("%s            value.%s.Type = %sType.None;\n", ind, name, un.Name)
		g.pf("%s            r.Report.Unknown++;\n%s            break;\n", ind, ind)
		g.pf("%s    }\n%s}\n", ind, ind)
		g.pf("%sr.Offset += (int)bodyLen;\n", ind)
	case kind == tkTable:
		g.pf("%sif (!r.Has(4)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%suint bodyLen = r.Get32();\n", ind)
		g.pf("%sif (!r.Has(bodyLen)) { r.Report.Malformed = true; return false; }\n", ind)
		g.pf("%s{\n%s    TableReader sub = new TableReader(r.Buffer.Slice(r.Offset, (int)bodyLen), r.Report);\n", ind, ind)
		g.pf("%s    %sLoadBody(ref sub, value.%s);\n", ind, f.Type.Name, name)
		g.pf("%s}\n", ind)
		g.pf("%sr.Offset += (int)bodyLen;\n", ind)
	default:
		g.emitTableReadScalarFrom(f, kind, "value."+name, ind,
			"r", "r.Report.Malformed = true; return false;")
	}
}

// emitTableReadElement decodes one array element from the field-body
// sub-reader; truncation keeps the decoded prefix and flags malformed
// without stopping the parent decode.
func (g *tableGen) emitTableReadElement(f *ir.Field, kind int, ind string) {
	name := member(f)
	switch kind {
	case tkTable:
		g.pf("%sif (!sub.Has(4)) { r.Report.Malformed = true; break; }\n", ind)
		g.pf("%suint elemLen = sub.Get32();\n", ind)
		g.pf("%sif (!sub.Has(elemLen)) { r.Report.Malformed = true; break; }\n", ind)
		g.pf("%s{\n%s    TableReader elem = new TableReader(sub.Buffer.Slice(sub.Offset, (int)elemLen), r.Report);\n", ind, ind)
		g.pf("%s    %sLoadBody(ref elem, value.%s[i]);\n", ind, f.Type.Name, name)
		g.pf("%s}\n", ind)
		g.pf("%ssub.Offset += (int)elemLen;\n", ind)
	default:
		g.emitTableReadScalarFrom(f, kind, fmt.Sprintf("value.%s[i]", name), ind,
			"sub", "r.Report.Malformed = true; break;")
	}
}

// emitTableReadScalarFrom decodes one fixed-width scalar from the named
// reader into a storage member, with the range clamps the schema declares.
// onTrunc is the truncation action: a scalar FIELD stops the decode (outer
// framing damage), an array ELEMENT keeps the prefix and breaks.
func (g *tableGen) emitTableReadScalarFrom(f *ir.Field, kind int, lvalue, ind, rdr, onTrunc string) {
	width := tableKindWidth(kind)
	g.pf("%sif (!%s.Has(%d)) { %s }\n", ind, rdr, width, onTrunc)
	if enum := enumRef(f); enum != nil {
		// identity is the variant's NAME (SPEC-TABLES.md §5): an id this build
		// cannot name reads as None and counts as unknown, exactly as an
		// unknown FIELD id does — same event, one counter
		g.pf("%s{\n%s    ushort variant = %s.Get16();\n", ind, ind, rdr)
		g.pf("%s    %s decodedEnum;\n", ind, f.Type.Name)
		g.pf("%s    if (!TableEnumValue(variant, out decodedEnum))\n%s    {\n", ind, ind)
		g.pf("%s        r.Report.Unknown++;\n%s    }\n", ind, ind)
		g.pf("%s    %s = decodedEnum;\n%s}\n", ind, lvalue, ind)
		return
	}
	switch kind {
	case tkBool:
		g.pf("%s%s = %s.Get8() != 0;\n", ind, lvalue, rdr)
	case tkF32:
		if f.HasFloatRange {
			g.pf("%s{\n%s    float decodedF = TableBitsToFloat(%s.Get32());\n", ind, ind, rdr)
			g.pf("%s    if (decodedF < %s) { decodedF = %s; r.Report.Clamped++; }\n", ind, formatFloat32(f.FMin), formatFloat32(f.FMin))
			g.pf("%s    else if (decodedF > %s) { decodedF = %s; r.Report.Clamped++; }\n", ind, formatFloat32(f.FMax), formatFloat32(f.FMax))
			g.pf("%s    %s = decodedF;\n%s}\n", ind, lvalue, ind)
			return
		}
		g.pf("%s%s = TableBitsToFloat(%s.Get32());\n", ind, lvalue, rdr)
	case tkF64:
		g.pf("%s%s = TableBitsToDouble(%s.Get64());\n", ind, lvalue, rdr)
	default:
		signed := f.Type.Kind == ir.TInt && f.Type.Signed
		storage := csKindStorage(kind)
		g.pf("%s{\n%s    %s decodedV = unchecked((%s)%s.%s());\n", ind, ind, storage, storage, rdr, tableGet(width))
		if f.HasIntRange {
			lo := csIntLit(f.IntMin, signed, width)
			hi := csIntLit(f.IntMax, signed, width)
			g.pf("%s    if (decodedV < %s) { decodedV = %s; r.Report.Clamped++; }\n", ind, lo, lo)
			g.pf("%s    else if (decodedV > %s) { decodedV = %s; r.Report.Clamped++; }\n", ind, hi, hi)
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
			maxv := (uint64(1) << f.Type.Width) - 1
			g.pf("%s    if (decodedV > %d) { decodedV = %d; r.Report.Clamped++; } // bits(%d) width clamp\n", ind, maxv, maxv, f.Type.Width)
		}
		g.pf("%s    %s = decodedV;\n%s}\n", ind, lvalue, ind)
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
// once on first use and cached. The build is idempotent, so the benign race
// two threads can run on first use produces two equivalent descriptors and
// then one; nothing mutable is ever published.
func (g *tableGen) emitTableDescriptor(st *ir.Struct) {
	guards := tableGuardStrings(st)
	g.pf("private static TableTypeInfo %sTableInfo;\n", st.Name)
	g.pf("public static TableTypeInfo %sTableType()\n{\n", st.Name)
	g.pf("    TableTypeInfo info = %sTableInfo;\n", st.Name)
	g.pf("    if (info != null) { return info; }\n")
	g.pf("    info = new TableTypeInfo();\n")
	g.pf("    info.Name = \"%s\";\n", st.Name)
	g.pf("    info.NumFields = %d;\n", len(st.Fields))
	if len(st.Fields) == 0 {
		g.pf("    info.Fields = new TableFieldInfo[0];\n")
	} else {
		g.pf("    info.Fields = new TableFieldInfo[]\n    {\n")
		for _, f := range st.Fields {
			g.emitTableFieldDescriptor(f, guards[f.Name])
		}
		g.pf("    };\n")
	}
	g.pf("    %sTableInfo = info;\n", st.Name)
	g.pf("    return info;\n}\n\n")
}

func (g *tableGen) emitTableFieldDescriptor(f *ir.Field, guard string) {
	id := ir.TableFieldId(f)
	kind := tableScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = tkU8
	}
	isArray := f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes
	counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString

	// the count column, spelled the way the storage spells its own extent: a
	// keyed array DERIVES it from the key enum, so nothing outside the array
	// names its size (SPEC-TABLES.md §2.4, §8.1)
	bound := "0"
	switch {
	case f.KeyEnum != "":
		bound = fmt.Sprintf("(int)%s.Max", f.KeyEnum)
	case f.Array != ir.ArrayNone:
		bound = strconv.FormatInt(f.ArrayBound, 10)
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString:
		bound = strconv.FormatInt(f.Type.Size, 10)
	}

	tableRef := "null"
	if _, isStruct := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && isStruct {
		tableRef = fmt.Sprintf("delegate { return %sTableType(); }", f.Type.Name)
	}

	// the KEY's vocabulary on an enum-keyed array (SPEC-TABLES.md §8):
	// functions of the KEY, not of the storage index — a walker stepping
	// [0, ArrayBound) asks about index + 1 and prints slots by name without
	// the schema files. KeyId(0) is 0 and KeyName(0) is "None", the reserved
	// id that says None keys no slot; no storage index maps to it (§2.4, §8).
	keyTypeName, keyName, keyId := "null", "null", "null"
	if f.KeyEnum != "" {
		keyTypeName = fmt.Sprintf("%q", f.KeyEnum)
		keyName = fmt.Sprintf("delegate(ulong v) { return EnumName%s(v); }", f.KeyEnum)
		keyId = fmt.Sprintf("delegate(ulong v) { ushort id; TableEnumId((%s)v, out id); return id; }", f.KeyEnum)
	}

	hasRange := "false"
	rangeMin, rangeMax := "0.0", "0.0"
	if f.HasIntRange {
		hasRange = "true"
		rangeMin, rangeMax = bigToDouble(f.IntMin), bigToDouble(f.IntMax)
	} else if f.HasFloatRange {
		hasRange = "true"
		rangeMin, rangeMax = formatFloat64(f.FMin), formatFloat64(f.FMax)
	}

	// the VOCABULARY columns: an enum's values and a union's arms are both a
	// named set indexed by [0, EnumMax], and each name carries the table-wire
	// id it rides under (SPEC-TABLES.md §5, §8)
	enumMax := "-1"
	enumName := "null"
	variantId := "null"
	switch ref := f.Type.Ref.(type) {
	case *ir.Enum:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", ref.Max)
			enumName = fmt.Sprintf("delegate(ulong v) { return EnumName%s(v); }", f.Type.Name)
			variantId = fmt.Sprintf("delegate(ulong v) { ushort id; TableEnumId((%s)v, out id); return id; }", f.Type.Name)
		}
	case *ir.Union:
		if f.Type.Kind == ir.TNamed && f.Array == ir.ArrayNone {
			enumMax = fmt.Sprintf("%d", len(ref.Variants))
			enumName = unionArmLambda(ref, func(v ir.UnionVariant) string {
				return fmt.Sprintf("%q", v.Name)
			}, "\"???\"", "\"None\"")
			variantId = unionArmLambda(ref, func(v ir.UnionVariant) string {
				return fmt.Sprintf("(ushort)0x%04x", ir.VariantId(v.Name))
			}, "(ushort)0", "(ushort)0")
		}
	}

	g.pf("        new TableFieldInfo { Name = \"%s\", TypeName = \"%s\", Id = 0x%04x, Kind = %d, IsArray = %v, Counted = %v, Optional = %v, ArrayBound = %s, HasRange = %s, RangeMin = %s, RangeMax = %s, EnumMax = %s, EnumName = %s, VariantId = %s, KeyTypeName = %s, KeyName = %s, KeyId = %s, Guard = \"%s\", TableRef = %s },\n",
		f.Name, tableFieldTypeName(f), id, kind, isArray, counted, f.Type.Optional, bound,
		hasRange, rangeMin, rangeMax, enumMax, enumName, variantId,
		keyTypeName, keyName, keyId, guard, tableRef)
}
