package unitytable

import (
	"fmt"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

var (
	maxU64Big = new(big.Int).SetUint64(math.MaxUint64)
	minI64Big = big.NewInt(math.MinInt64)
)

func csUintType(bits int) string {
	switch bits {
	case 8:
		return "byte"
	case 16:
		return "ushort"
	case 32:
		return "uint"
	}
	return "ulong"
}

func csIntType(bits int) string {
	switch bits {
	case 8:
		return "sbyte"
	case 16:
		return "short"
	case 32:
		return "int"
	}
	return "long"
}

func scalarStorageType(t ir.FieldType) string {
	switch t.Kind {
	case ir.TInt, ir.TFixed:
		if t.Width == 128 {
			if t.Signed {
				return "Int128Value"
			}
			return "UInt128Value"
		}
		if t.Signed {
			return csIntType(t.Width)
		}
		return csUintType(t.Width)
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
		return t.Name
	}
	return ""
}

func formatFloat64Lit(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEN") {
		s += ".0"
	}
	return s
}

func formatFloat32Lit(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 32)
	if !strings.ContainsAny(s, ".eEN") {
		s += ".0"
	}
	return s + "f"
}

func lit128Expr(v *big.Int, signed bool) string {
	u := new(big.Int).Set(v)
	if u.Sign() < 0 {
		u.Add(u, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	hi := new(big.Int).Rsh(u, 64)
	lo := new(big.Int).And(u, maxU64Big)
	typ := "UInt128Value"
	if signed {
		typ = "Int128Value"
	}
	return fmt.Sprintf("new %s(0x%xUL, 0x%xUL)", typ, hi, lo)
}

func flagsLitExpr(d *ir.Flags, mask *big.Int) string {
	if mask == nil || mask.Sign() == 0 {
		return fmt.Sprintf("(%s)0", d.Name)
	}
	rest := new(big.Int).Set(mask)
	var parts []string
	for i, v := range d.Variants {
		if rest.Bit(i) != 0 {
			parts = append(parts, d.Name+"."+v)
			rest.SetBit(rest, i, 0)
		}
	}
	if rest.Sign() != 0 {
		parts = append(parts, fmt.Sprintf("(%s)0x%sUL", d.Name, rest.Text(16)))
	}
	return strings.Join(parts, " | ")
}

func scalarDefaultExpr(f *ir.Field) string {
	switch {
	case f.Type.Kind == ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case f.DefVariant != "":
		return f.Type.Name + "." + f.DefVariant
	case f.Type.Kind == ir.TNamed:
		if fl, ok := f.Type.Ref.(*ir.Flags); ok {
			return flagsLitExpr(fl, f.DefInt)
		}
		if en, ok := f.Type.Ref.(*ir.Enum); ok {
			return en.Name + ".None"
		}
	case f.Type.Kind == ir.TFloat32:
		if f.HasDefault {
			return formatFloat32Lit(f.DefFloat)
		}
		return "0.0f"
	case f.Type.Kind == ir.TFloat64:
		if f.HasDefault {
			return formatFloat64Lit(f.DefFloat)
		}
		return "0.0"
	case f.Type.Width == 128:
		if f.HasDefault && f.DefInt != nil {
			return lit128Expr(f.DefInt, f.Type.Signed)
		}
		if f.Type.Signed {
			return "Int128Value.Zero"
		}
		return "UInt128Value.Zero"
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed || f.Type.Kind == ir.TBits:
		v := new(big.Int)
		if f.HasDefault && f.DefInt != nil {
			v = f.DefInt
		}
		t := scalarStorageType(f.Type)
		if f.Type.Kind != ir.TBits && f.Type.Signed {
			if v.Cmp(minI64Big) == 0 {
				return fmt.Sprintf("(%s)long.MinValue", t)
			}
			return fmt.Sprintf("(%s)(%sL)", t, v.String())
		}
		return fmt.Sprintf("(%s)(%sUL)", t, v.String())
	}
	return "default"
}

func emitScalarAssert(b *strings.Builder, f *ir.Field, v string) {
	switch f.Type.Kind {
	case ir.TInt, ir.TFixed, ir.TBits:
		var lo, hi *big.Int
		var ok bool
		if f.Type.Kind == ir.TBits {
			w := ir.TableFixedStorageBytes(f.Type)
			if int64(f.Type.Width) < 8*w {
				lo = new(big.Int)
				hi = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
				ok = true
			}
		} else {
			lo, hi, ok = ir.TableRawRange(f)
		}
		if !ok {
			return
		}
		if f.Type.Width == 128 {
			fmt.Fprintf(b, "Debug.Assert(%s >= %s && %s <= %s, \"%s is outside its declared range\");\n",
				v, lit128Expr(lo, f.Type.Signed), v, lit128Expr(hi, f.Type.Signed), f.Name)
			return
		}
		if f.Type.Kind != ir.TBits && f.Type.Signed {
			loS, hiS := lo.String(), hi.String()
			if lo.Cmp(minI64Big) == 0 {
				loS = "long.MinValue"
			}
			fmt.Fprintf(b, "Debug.Assert(%s >= %s && %s <= %s, \"%s is outside its declared range [%s, %s]\");\n",
				v, loS, v, hiS, f.Name, lo.String(), hi.String())
			return
		}
		fmt.Fprintf(b, "Debug.Assert(%s, \"%s is outside its declared range [%s, %s]\");\n",
			unsignedTest(v, lo, hi, true), f.Name, lo.String(), hi.String())
	case ir.TFloat32:
		fmt.Fprintf(b, "Debug.Assert(!float.IsNaN(%s) && !float.IsInfinity(%s), \"%s is not finite\");\n", v, v, f.Name)
		if f.HasFloatRange {
			fmt.Fprintf(b, "Debug.Assert(%s >= %s && %s <= %s, \"%s is outside its declared range\");\n",
				v, formatFloat32Lit(f.FMin), v, formatFloat32Lit(f.FMax), f.Name)
		}
	case ir.TFloat64:
		fmt.Fprintf(b, "Debug.Assert(!double.IsNaN(%s) && !double.IsInfinity(%s), \"%s is not finite\");\n", v, v, f.Name)
		if f.HasFloatRange {
			fmt.Fprintf(b, "Debug.Assert(%s >= %s && %s <= %s, \"%s is outside its declared range\");\n",
				v, formatFloat64Lit(f.FMin), v, formatFloat64Lit(f.FMax), f.Name)
		}
	case ir.TNamed:
		if en, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(b, "Debug.Assert((ulong)%s <= %dUL, \"%s is outside enum %s\");\n", v, len(en.Variants), f.Name, en.Name)
			for i, variant := range en.Variants {
				if enumVariantDeprecated(en, i) {
					fmt.Fprintf(b, "Debug.Assert(%s != %s.%s, \"%s names deprecated variant %s\");\n", v, en.Name, variant, f.Name, variant)
				}
			}
		}
	}
}

func enumVariantDeprecated(en *ir.Enum, idx int) bool {
	if idx < len(en.VariantDeprecated) && en.VariantDeprecated[idx] {
		return true
	}
	if idx < len(en.VariantTags) {
		if slices.Contains(en.VariantTags[idx], "deprecated") {
			return true
		}
	}
	return false
}

func hasFixedFieldClosure(u *ir.Unit, f *ir.Field, visiting map[string]bool) bool {
	if f.Type.Pointer || f.IsList() || f.IsMap() || f.Type.TextUnbounded() {
		return false
	}
	if f.Type.Kind != ir.TNamed {
		return true
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Enum, *ir.Flags:
		return true
	case *ir.Struct:
		if !r.IsTable {
			return true
		}
		return hasFixedStructClosure(u, r, visiting)
	case *ir.Union:
		if u.TableUnions[r.Name] == nil {
			return true
		}
		return hasFixedUnionClosure(u, r, visiting)
	}
	return false
}

func hasFixedStructClosure(u *ir.Unit, st *ir.Struct, visiting map[string]bool) bool {
	key := "s:" + st.Name
	if visiting[key] {
		return false
	}
	visiting[key] = true
	defer delete(visiting, key)
	for _, f := range st.Fields {
		if !hasFixedFieldClosure(u, f, visiting) {
			return false
		}
	}
	return true
}

func hasFixedUnionClosure(u *ir.Unit, un *ir.Union, visiting map[string]bool) bool {
	key := "u:" + un.Name
	if visiting[key] {
		return false
	}
	visiting[key] = true
	defer delete(visiting, key)
	for _, v := range un.Variants {
		if !v.Void() && !hasFixedFieldClosure(u, v.F, visiting) {
			return false
		}
	}
	return true
}

func hasFixedStorageClosure(u *ir.Unit, st *ir.Struct) bool {
	return hasFixedStructClosure(u, st, map[string]bool{})
}

func packetStructHasInit(st *ir.Struct) bool {
	seen := map[*ir.Struct]bool{}
	var walk func(s *ir.Struct) bool
	walk = func(s *ir.Struct) bool {
		if seen[s] {
			return false
		}
		seen[s] = true
		for _, f := range s.Fields {
			if f.HasDefault || f.BornCount() > 0 {
				return true
			}
			if inner, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed && walk(inner) {
				return true
			}
		}
		return false
	}
	return walk(st)
}

func structHasInitFn(st *ir.Struct) bool {
	if st.IsTable {
		return true
	}
	return packetStructHasInit(st)
}

func emitFixedStorageField(b *strings.Builder, f *ir.Field, memberName string) {
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		fmt.Fprintf(b, "    public Buffer%d<byte> %s;\n", f.Type.Size, memberName)
		fmt.Fprintf(b, "    public int %sLength;\n", memberName)
	case f.Array == ir.ArrayFixed:
		fmt.Fprintf(b, "    public Buffer%d<%s> %s;\n", f.ArrayBound, scalarStorageType(f.Type), memberName)
	case f.Array == ir.ArrayCounted:
		fmt.Fprintf(b, "    public Buffer%d<%s> %s;\n", f.ArrayBound, scalarStorageType(f.Type), memberName)
		fmt.Fprintf(b, "    public int %sCount;\n", memberName)
	default:
		fmt.Fprintf(b, "    public %s %s;\n", scalarStorageType(f.Type), memberName)
	}
	if f.Type.Optional {
		fmt.Fprintf(b, "    public bool %sPresent;\n", memberName)
	}
}

func emitTableUnion(types, schema *strings.Builder, u *ir.Unit, un *ir.Union) {
	if !hasFixedUnionClosure(u, un, map[string]bool{}) {
		return
	}
	tagBits := ir.StorageBitsFor(int64(len(un.Variants)))
	fmt.Fprintf(types, "public enum %sType : %s\n{\n    None = 0,\n", un.Name, csUintType(tagBits))
	for i, v := range un.Variants {
		types.WriteString(ir.DocComment(v.Doc, "    ", "//"))
		fmt.Fprintf(types, "    %s = %d,\n", ir.GoExportName(v.Name), i+1)
	}
	types.WriteString("}\n\n")

	fmt.Fprintf(schema, "public const %sType %sTypeMax = (%sType)%d;\n\n", un.Name, un.Name, un.Name, len(un.Variants))
	fmt.Fprintf(schema, "public static string EnumName%sType(%sType value)\n{\n    switch (value)\n    {\n        case %sType.None: return \"None\";\n", un.Name, un.Name, un.Name)
	for _, v := range un.Variants {
		arm := ir.GoExportName(v.Name)
		fmt.Fprintf(schema, "        case %sType.%s: return %q;\n", un.Name, arm, arm)
	}
	schema.WriteString("        default: return \"???\";\n    }\n}\n\n")

	types.WriteString(ir.DocComment(un.Doc, "", "//"))
	fmt.Fprintf(types, "[Serializable]\npublic struct %s\n{\n    public %sType Type;\n", un.Name, un.Name)
	for _, v := range un.Variants {
		if v.Void() {
			continue
		}
		emitFixedStorageField(types, v.F, ir.GoExportName(v.Name))
	}
	types.WriteString("}\n\n")
}

func emitInitFixedField(b *strings.Builder, f *ir.Field, prefix, ind string) {
	if f.Type.Optional {
		return
	}
	name := prefix + member(f)
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		if f.HasDefault && len(f.DefBytes) > 0 {
			for i, by := range f.DefBytes {
				fmt.Fprintf(b, "%s%s.E%d = 0x%02x;\n", ind, name, i, by)
			}
			fmt.Fprintf(b, "%s%sLength = %d;\n", ind, name, len(f.DefBytes))
		}
	case f.Array != ir.ArrayNone:
		count := f.ArrayBound
		if f.Array == ir.ArrayCounted {
			count = f.BornCount()
			if count > 0 {
				fmt.Fprintf(b, "%s%sCount = %d;\n", ind, name, count)
			}
		}
		if inner, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed && structHasInitFn(inner) && count > 0 {
			fmt.Fprintf(b, "%s{\n%s    Span<%s> span = %s.AsSpan();\n", ind, ind, inner.Name, name)
			fmt.Fprintf(b, "%s    for (int i = 0; i < %d; i++) { Init%s(ref span[i]); }\n%s}\n", ind, count, inner.Name, ind)
		}
	case f.HasDefault:
		expr := scalarDefaultExpr(f)
		if expr != "false" && expr != "0.0f" && expr != "0.0" && !strings.HasSuffix(expr, ".Zero") && !strings.HasSuffix(expr, ".None") && !strings.HasSuffix(expr, "(0UL)") && !strings.HasSuffix(expr, "(0L)") && expr != fmt.Sprintf("(%s)0", f.Type.Name) {
			fmt.Fprintf(b, "%s%s = %s;\n", ind, name, expr)
		}
	case f.Type.Kind == ir.TNamed:
		if inner, ok := f.Type.Ref.(*ir.Struct); ok && structHasInitFn(inner) {
			fmt.Fprintf(b, "%sInit%s(ref %s);\n", ind, inner.Name, name)
		}
	}
}

func unitUses128(u *ir.Unit) bool {
	checkField := func(f *ir.Field) bool {
		return f != nil && (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Width == 128
	}
	for _, st := range u.Tables {
		if slices.ContainsFunc(st.Fields, checkField) {
			return true
		}
	}
	for _, st := range u.Structs {
		if slices.ContainsFunc(st.Fields, checkField) {
			return true
		}
	}
	for _, un := range u.TableUnions {
		for _, v := range un.Variants {
			if checkField(v.F) {
				return true
			}
		}
	}
	for _, un := range u.Unions {
		for _, v := range un.Variants {
			if checkField(v.F) {
				return true
			}
		}
	}
	return false
}
