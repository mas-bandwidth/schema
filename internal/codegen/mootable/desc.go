package mootable

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE DESCRIPTOR (docs/SPEC-TABLES.md §8.1). Beside every table's type and
// constructor, <Name>Descriptor is the table AS FROZEN DATA, everything the
// one runtime walk of each form reads (runtime/schema_tables.moo names the
// fields). It is the Luau port's descriptor with moo's value domain:
//
//   - an id (a node type id, a field's or a variant's wire id) is its eight
//     little-endian bytes as a string ([IdLit]), never a wide value;
//   - an integer-shaped value carries its storage class `cls` ("n" a number,
//     "fx" a Q48.16 number in whole units, "I64" "U64" "I128" "U128" the
//     serialize.moo classes) beside its kind's width `kw` and signedness, and
//     its declared bounds `lo`/`hi` are RAW and in the KIND's domain (a number
//     at kw <= 4, the kind's wide class at 8 and 16), the shape the wire and
//     the clamp compare;
//   - a float is its bit pattern: a float32 a number, a float64 a Uint64, and
//     a declared range's ends are binary64 bit pairs {lo, hi}.

// emitDescriptor writes <Name>Descriptor for a table, a map entry or a packet
// type a table reaches.
func emitDescriptor(g *Gen, t *ir.Struct) {
	g.Pf("-- %sDescriptor — %s as data: node type id, class, and one entry per field\n", t.Name, t.Name)
	g.Pf("-- (docs/SPEC-TABLES.md §3, §8.1)\n")
	g.Pf("%s.%sDescriptor = St.table({\n", g.Self, t.Name)
	g.Pf("\tname = %q,\n", t.Name)
	g.Pf("\tid = %s, -- FNV-1a 64 of %q (§3.1)\n", IdLit(ir.TableWireId(t.WireName())), t.WireName())
	g.Pf("\ttable = %v,\n", t.IsTable)
	if t.IsTable && t.DeclaredForms() {
		g.Pf("\tforms = {")
		for _, form := range t.Forms {
			g.Pf(" %s = true,", form)
		}
		g.Pf(" },\n")
	}
	g.Pf("\tfixed = %v,\n", t.FixedDeclared)
	g.Pf("\tvariable = %v,\n", ir.VariableTables(g.Unit)[t.Name])
	if t.JsonArray {
		g.Pf("\tjsonArray = true,\n")
	}
	if t.Integrity != "" {
		g.Pf("\tintegrity = %q,\n", t.Integrity)
	}
	var placeable []string
	for _, r := range ir.PointerReachable(t) {
		placeable = append(placeable, fmt.Sprintf("%q", r.Name))
		requireHome(g, r.Name)
	}
	sort.Strings(placeable)
	bytesEdge, stringEdge := ir.PointerReachableBlobs(t)
	g.Pf("\tplace = { tables = { %s }, str = %v, bytes = %v },\n", strings.Join(placeable, ", "), stringEdge, bytesEdge)
	g.Pf("\tfields = {\n")
	guards := guardPairs(t)
	for i, f := range t.Fields {
		g.Pf("\t\t%s,\n", fieldDesc(g, t.Name, f, i, guardLua(guards[f.Name])))
	}
	g.Pf("\t},\n")
	g.Pf("\tnew = %s,\n", strings.TrimSuffix(g.Ctor(t.Name), "()"))
	g.Pf("})\n\n")
}

// emitUnionDescriptor writes <Name>Descriptor for a union: its arms, each a
// field line under the arm's id (docs/SPEC-TABLES.md §2.6, §3).
func emitUnionDescriptor(g *Gen, un *ir.Union) {
	g.Pf("-- %sDescriptor — union %s as data: one entry per arm, tag order (§2.6)\n", un.Name, un.Name)
	g.Pf("%s.%sDescriptor = St.union({\n", g.Self, un.Name)
	g.Pf("\tname = %q,\n", un.Name)
	g.Pf("\tarms = {\n")
	for i, v := range un.Variants {
		id := IdLit(ir.TableWireId(v.WireName()))
		if v.Void() {
			g.Pf("\t\t{ id = %s, name = %q, slot = \"\", tag = %d, s = \"void\" },\n", id, v.Name, i+1)
			continue
		}
		f := armField(v)
		d := fieldDesc(g, un.Name, f, i, "")
		g.Pf("\t\t%s, tag = %d },\n", strings.TrimSuffix(d, " }"), i+1)
	}
	g.Pf("\t},\n")
	g.Pf("\tnew = %s,\n", strings.TrimSuffix(g.Ctor(un.Name), "()"))
	g.Pf("})\n\n")
}

// requireHome requires the table module that registers a descriptor, so a
// walk that names it by string finds it registered.
func requireHome(g *Gen, name string) {
	if b := g.Unit.DeclFile[name]; b != "" {
		g.Require(b + "Table")
		return
	}
	for _, f := range g.Unit.Files {
		for _, t := range f.Tables {
			if t.Name == name {
				g.Require(f.Base + "Table")
				return
			}
		}
	}
}

// guardPairs composes each guarded field's branch condition as a conjunction
// of (bool slot name, required value) pairs — data a save walk evaluates
// against an instance (docs/SPEC-TABLES.md §3: a field under a FALSE GUARD is
// elided, whatever its storage holds).
func guardPairs(st *ir.Struct) map[string][][2]any {
	guards := map[string][][2]any{}
	var walk func(items []ir.Item, cond [][2]any)
	walk = func(items []ir.Item, cond [][2]any) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if len(cond) > 0 {
					guards[item.F.Name] = cond
				}
			case *ir.Branch:
				pos := [2]any{ir.GoExportName(item.Cond), true}
				neg := [2]any{ir.GoExportName(item.Cond), false}
				if item.Neg {
					pos, neg = neg, pos
				}
				walk(item.Then, append(append([][2]any{}, cond...), pos))
				walk(item.Else, append(append([][2]any{}, cond...), neg))
			}
		}
	}
	walk(st.Items, nil)
	return guards
}

func guardLua(pairs [][2]any) string {
	if len(pairs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, fmt.Sprintf("{ %q, %v }", p[0].(string), p[1].(bool)))
	}
	return "guard = { " + strings.Join(parts, ", ") + " }"
}

// fieldDesc renders one field's descriptor: its wire id, name, slot, text
// key, ordinal, header kind, SHAPE and what the shape needs. The shapes:
//
//	bool, int, float   a scalar at its kind's width
//	str                string(N) (a moo string) or wstring(N) (wide = true:
//	                   a buffer of UTF-16LE units and its Length), kind 12/33
//	text               an unbounded string, kind 12 (string?)
//	bytes              bytes(N), kind 14 over element kind 6 (a buffer)
//	enum               kind 30, the variant name's id
//	ptr, sblob, bblob  *T, *string, *bytes: kind 17, a node index
//	tab, union         a table or packet type by value (kind 13); a union (15)
//	arr, map           [N]T / [..N]T / []T / [E]T over an element; map[K]V
func fieldDesc(g *Gen, owner string, f *ir.Field, ordinal int, guard string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{ id = %s, name = %q, owner = %q, slot = %q, json = %q, o = %d, k = %d, ",
		IdLit(ir.TableFieldWireId(f)), f.Name, owner, ir.GoExportName(f.Name),
		ir.TableFieldJsonKey(f), ordinal, ir.TableWireFieldKind(f))
	if f.Type.Optional {
		fmt.Fprintf(&b, "present = %q, ", ir.GoExportName(f.Name+"_present"))
	}
	if f.Required {
		b.WriteString("required = true, ")
	}
	if f.IsTextList() && f.TextTotal > 0 {
		fmt.Fprintf(&b, "total = %d, ", f.TextTotal)
	}
	if f.Deprecated {
		b.WriteString("deprecated = true, ")
	}
	if guard != "" {
		b.WriteString(guard + ", ")
	}
	switch {
	case f.IsMap():
		requireHome(g, f.MapEntry.Name)
		fmt.Fprintf(&b, "s = \"map\", t = %q, ek = %d, bound = 0, unbounded = true }", f.MapEntry.Name, ir.TableWireElemKind(f))
	case f.Array != ir.ArrayNone:
		array := map[ir.ArrayKind]string{ir.ArrayFixed: "fixed", ir.ArrayCounted: "counted", ir.ArrayList: "list"}[f.Array]
		if f.KeyEnum != "" {
			array = "keyed"
		}
		unbounded := ""
		if f.Array == ir.ArrayList {
			unbounded = ", unbounded = true"
		}
		keyvs := ""
		if f.KeyEnumRef != nil {
			keyvs = ", keyvs = " + enumVariants(f.KeyEnumRef)
		}
		fmt.Fprintf(&b, "s = \"arr\", array = %q, bound = %d, min = %d, ek = %d%s%s, e = %s }",
			array, f.ArrayBound, f.ArrayMin, ir.TableWireElemKind(f), keyvs, unbounded, elemDesc(g, f))
	default:
		b.WriteString(valueDesc(g, f, true))
		b.WriteString(" }")
	}
	return b.String()
}

// elemDesc is an array's element descriptor: the facts a field of the
// element's type carries, at its zero (§2.9). A plain text list's element is
// kind 12, its own L and bytes (§2.13).
func elemDesc(g *Gen, f *ir.Field) string {
	if ir.TextListPlain(f) {
		return fmt.Sprintf("{ s = \"str\", bound = %d, wide = false }", ir.TextListValue(f).Type.Size)
	}
	e := *f
	e.Array = ir.ArrayNone
	e.KeyEnum = ""
	e.KeyEnumRef = nil
	e.HasDefault = false
	byteElem := ""
	if isByteElem(f) && f.Array != ir.ArrayList {
		byteElem = ", buf = true"
	}
	// an element names its own wire kind too: the fixed form's plan compares
	// it with the writer's layout entry (fixedform.go, table_fixed.moo)
	kind := ""
	if k := ir.TableWireScalarKind(&e); k != 0 {
		kind = fmt.Sprintf(", k = %d", k)
	}
	return "{ " + valueDesc(g, &e, false) + byteElem + kind + " }"
}

// valueDesc is the shape of a non-array value and what it needs; def adds the
// declared default an absent field reads as (§4) and elides at (§3).
func valueDesc(g *Gen, f *ir.Field, def bool) string {
	t := f.Type
	switch {
	case t.Pointer && t.Blob() && t.Kind == ir.TString:
		return "s = \"sblob\""
	case t.Pointer && t.Blob():
		return "s = \"bblob\""
	case t.Pointer:
		requireHome(g, t.Name)
		return fmt.Sprintf("s = \"ptr\", t = %q", t.Name)
	case t.TextUnbounded():
		return "s = \"text\""
	case t.Kind == ir.TString || t.Kind == ir.TWString:
		d := ""
		if def {
			d = ", def = " + luaString(f.DefBytes)
		}
		return fmt.Sprintf("s = \"str\", bound = %d, wide = %v%s", t.Size, t.Kind == ir.TWString, d)
	case t.Kind == ir.TBytes:
		d := ""
		if def {
			d = ", def = " + luaString(f.DefBytes)
		}
		return fmt.Sprintf("s = \"bytes\", bound = %d, ek = %d%s", t.Size, ir.TableKindU8, d)
	case t.Kind == ir.TBool:
		if def {
			return fmt.Sprintf("s = \"bool\", def = %v", f.HasDefault && f.DefBool)
		}
		return "s = \"bool\", def = false"
	case t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64:
		parts := []string{"s = \"float\"", fmt.Sprintf("f32 = %v", t.Kind == ir.TFloat32)}
		if f.HasFloatRange {
			parts = append(parts, "flo = "+f64Pair(f.FMin), "fhi = "+f64Pair(f.FMax), "fres = "+f64Pair(f.Resolution))
			if t.Kind == ir.TFloat32 {
				// the ends a float32 read clamps TO, as float32(v) rounds them
				parts = append(parts, fmt.Sprintf("flo32 = 0x%08x, fhi32 = 0x%08x", math.Float32bits(float32(f.FMin)), math.Float32bits(float32(f.FMax))))
			}
		}
		_, init := g.scalar(f, def)
		parts = append(parts, "def = "+init)
		return strings.Join(parts, ", ")
	case t.Kind == ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			_, init := g.scalar(f, def)
			return fmt.Sprintf("s = \"enum\", t = %q, vs = %s, fw = %d, def = %s", r.Name, enumVariants(r), r.StorageBits/8, init)
		case *ir.Flags:
			return intDesc(g, f, def)
		case *ir.Union:
			requireHome(g, r.Name)
			return fmt.Sprintf("s = \"union\", t = %q", r.Name)
		case *ir.Struct:
			requireHome(g, r.Name)
			return fmt.Sprintf("s = \"tab\", t = %q", r.Name)
		}
	}
	return intDesc(g, f, def)
}

// f64Pair is a float64's bits as the {lo, hi} pair of 32-bit halves the
// runtime compares (moo holds no float; docs/PORT-MOO.md).
func f64Pair(v float64) string {
	b := math.Float64bits(v)
	return fmt.Sprintf("{ 0x%08x, 0x%08x }", uint32(b), uint32(b>>32))
}

// enumVariants is an enum's variants as { name, wire id, deprecated }
// triples: identity is the variant NAME's hash (§2.15, §5).
func enumVariants(e *ir.Enum) string {
	parts := make([]string, len(e.Variants))
	for i := range e.Variants {
		deprecated := i < len(e.VariantDeprecated) && e.VariantDeprecated[i]
		parts[i] = fmt.Sprintf("{ %q, %s, %v }", e.Variants[i], IdLit(ir.TableWireId(e.VariantWireName(i))), deprecated)
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// kindClass is the wide class a kind's raw value is held in by the runtime:
// "" (a number) at four bytes or fewer.
func kindClass(kw int, signed bool) string {
	switch {
	case kw == 16 && signed:
		return "Int128"
	case kw == 16:
		return "Uint128"
	case kw == 8 && signed:
		return "Int64"
	case kw == 8:
		return "Uint64"
	}
	return ""
}

// rawLit renders a raw bound in the kind's domain.
func rawLit(g *Gen, kw int, signed bool, v *big.Int) string {
	if c := kindClass(kw, signed); c != "" {
		return g.K(WideNew(c, v, kw*8))
	}
	return v.String()
}

// intDesc is what an integer-shaped value needs (docs/SPEC-TABLES.md §3, §4,
// §15): its kind's width kw and signedness, its storage class cls, the
// declared range a read clamps to (lo/hi, RAW, in the kind's domain),
// bits(N)'s width, and a fixed field's (I, F).
func intDesc(g *Gen, f *ir.Field, def bool) string {
	kind := ir.TableWireScalarKind(f)
	kw := ir.TableKindWidth(kind)
	signed := ir.TableKindSigned(kind)
	parts := []string{"s = \"int\"", fmt.Sprintf("kw = %d", kw), fmt.Sprintf("signed = %v", signed), fmt.Sprintf("cls = %q", storageClass(f))}
	// fw is the value's FIXED-FORM storage width (docs/SPEC-TABLES.md §3.4),
	// which a bits(N) holds wider than its kind: the plan widens on it
	if fw := ir.TableFixedStorageBytes(f.Type); fw > 0 {
		parts = append(parts, fmt.Sprintf("fw = %d", fw))
	}
	if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
		lo, hi := f.IntMin, f.IntMax
		if f.Type.Kind == ir.TFixed {
			lo = new(big.Int).Lsh(lo, uint(f.Type.FracBits))
			hi = new(big.Int).Lsh(hi, uint(f.Type.FracBits))
		}
		parts = append(parts, "lo = "+rawLit(g, kw, signed, lo), "hi = "+rawLit(g, kw, signed, hi))
	}
	if f.Type.Kind == ir.TBits {
		parts = append(parts, fmt.Sprintf("bits = %d", f.Type.Width))
	}
	if f.Type.Kind == ir.TFixed {
		parts = append(parts, fmt.Sprintf("ibits = %d, fbits = %d", f.Type.IntBits, f.Type.FracBits))
	}
	if f.Type.Timestamp {
		parts = append(parts, "timestamp = true")
	}
	_, init := g.scalar(f, def)
	parts = append(parts, "def = "+init)
	return strings.Join(parts, ", ")
}
