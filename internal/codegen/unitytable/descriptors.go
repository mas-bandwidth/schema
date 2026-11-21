package unitytable

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	tkBool   = 1
	tkI8     = 2
	tkI16    = 3
	tkI32    = 4
	tkI64    = 5
	tkU8     = 6
	tkU16    = 7
	tkU32    = 8
	tkU64    = 9
	tkF32    = 10
	tkF64    = 11
	tkString = 12
	tkTable  = 13
	tkArray  = 14
	tkUnion  = 15
)

func tableScalarKind(f *ir.Field) int {
	if f.Type.Kind == ir.TNamed {
		if _, ok := f.Type.Ref.(*ir.Enum); ok {
			return 30
		}
	}
	return ir.TableScalarKind(f)
}

func tableKindWidth(kind int) int {
	switch kind {
	case tkBool, tkI8, tkU8:
		return 1
	case tkI16, tkU16:
		return 2
	case tkI32, tkU32, tkF32:
		return 4
	case tkI64, tkU64, tkF64, ir.TableKindTimestamp:
		return 8
	}
	return 0
}

func csIntLit(v *big.Int, signed bool, widthBytes int) string {
	s := v.String()
	if widthBytes < 8 {
		return s
	}
	if !signed {
		return s + "UL"
	}
	if s == "-9223372036854775808" {
		return "long.MinValue"
	}
	return s + "L"
}

func tableStorageRange(signed bool, bits int) (*big.Int, *big.Int) {
	one := big.NewInt(1)
	if signed {
		hi := new(big.Int).Lsh(one, uint(bits-1))
		return new(big.Int).Neg(hi), new(big.Int).Sub(hi, one)
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one)
}

func tableClampEnds(f *ir.Field, widthBytes int) (low, high bool) {
	signed := ir.TableKindSigned(ir.TableScalarKind(f))
	lo, hi := tableStorageRange(signed, widthBytes*8)
	rlo, rhi, ok := ir.TableRawRange(f)
	if !ok {
		return false, false
	}
	return rlo.Cmp(lo) > 0, rhi.Cmp(hi) < 0
}

func csQuote(s string) string { return strconv.Quote(s) }

func csQuoteList(items []string) string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = strconv.Quote(s)
	}
	return strings.Join(out, ", ")
}

func csList(items []string, sep string) string {
	if len(items) == 0 {
		return "{ }"
	}
	return "{ " + strings.Join(items, sep) + " }"
}

func armTags(tags []string) string {
	quoted := make([]string, len(tags))
	for i, tag := range tags {
		quoted[i] = csQuote(tag)
	}
	return "new string[] " + csList(quoted, ", ")
}

func byteLiterals(bytes []byte) string {
	parts := make([]string, len(bytes))
	for i, v := range bytes {
		parts[i] = fmt.Sprintf("0x%02x", v)
	}
	return strings.Join(parts, ", ")
}

func bigToDouble(v *big.Int) string {
	f, _ := new(big.Float).SetInt(v).Float64()
	return formatFloat64Lit(f)
}

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

func csRawGet(expr string, t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "(" + expr + ") ? 1UL : 0UL"
	case ir.TFloat32:
		return "(ulong)TableFloatToBits(" + expr + ")"
	case ir.TFloat64:
		return "TableDoubleToBits(" + expr + ")"
	case ir.TInt, ir.TFixed:
		if t.Signed {
			return "unchecked((ulong)(long)(" + expr + "))"
		}
		return "(ulong)(" + expr + ")"
	case ir.TBits, ir.TNamed:
		return "(ulong)(" + expr + ")"
	}
	return "0UL"
}

func csRawSet(pkg, expr, src string, t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return expr + " = " + src + " != 0;"
	case ir.TFloat32:
		return expr + " = TableBitsToFloat(unchecked((uint)" + src + "));"
	case ir.TFloat64:
		return expr + " = TableBitsToDouble(" + src + ");"
	case ir.TInt, ir.TFixed:
		st := scalarStorageType(t)
		if t.Signed {
			return expr + " = unchecked((" + st + ")(long)" + src + ");"
		}
		return expr + " = unchecked((" + st + ")" + src + ");"
	case ir.TBits:
		return expr + " = unchecked((" + scalarStorageType(t) + ")" + src + ");"
	case ir.TNamed:
		return expr + " = unchecked((global::" + pkg + "." + t.Name + ")" + src + ");"
	}
	return ""
}

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

func tableGuardExprs(st *ir.Struct) map[string]string   { return guardWalk(st, true) }
func tableGuardStrings(st *ir.Struct) map[string]string { return guardWalk(st, false) }

func unionOwner(un *ir.Union) *ir.Struct {
	owner := &ir.Struct{Name: un.Name}
	for _, v := range un.Variants {
		if !v.Void() {
			owner.Fields = append(owner.Fields, v.F)
		}
	}
	return owner
}

func emitEnumIdentity(b *strings.Builder, pkg string, e *ir.Enum) {
	qual := "global::" + pkg + "." + e.Name
	fmt.Fprintf(b, "public static bool TableEnumId(%s value, out ulong id)\n{\n    switch (value)\n    {\n        case %s.None: id = 0; return true;\n", qual, qual)
	for _, v := range e.Variants {
		fmt.Fprintf(b, "        case %s.%s: id = 0x%016xUL; return true;\n", qual, v, ir.TableWireId(e.VariantWireNameOf(v)))
	}
	b.WriteString("        default: id = 0; return false;\n    }\n}\n\n")
	fmt.Fprintf(b, "public static bool TableEnumValue(ulong id, out %s value)\n{\n    switch (id)\n    {\n", qual)
	for _, v := range e.Variants {
		fmt.Fprintf(b, "        case 0x%016xUL: value = %s.%s; return true;\n", ir.TableWireId(e.VariantWireNameOf(v)), qual, v)
	}
	fmt.Fprintf(b, "        default: value = %s.None; return false;\n    }\n}\n\n", qual)
}

func messageClosure(u *ir.Unit) (structs, unions map[string]bool) {
	structs, unions = map[string]bool{}, map[string]bool{}
	var walk func(st *ir.Struct)
	var field func(f *ir.Field)
	field = func(f *ir.Field) {
		if f == nil || f.Type.Pointer || f.Type.Kind != ir.TNamed {
			return
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			walk(ref)
		case *ir.Union:
			if unions[ref.Name] {
				return
			}
			unions[ref.Name] = true
			for _, v := range ref.Variants {
				field(v.F)
			}
		}
	}
	walk = func(st *ir.Struct) {
		if structs[st.Name] {
			return
		}
		structs[st.Name] = true
		for _, f := range st.Fields {
			field(f)
		}
	}
	for name := range ir.TableClosure(u) {
		if st := u.Tables[name]; st != nil && st.FixedDeclared {
			walk(st)
		}
	}
	return structs, unions
}

func declaredTables(u *ir.Unit) []*ir.Struct {
	closure := ir.TableClosure(u)
	out := make([]*ir.Struct, 0, len(closure))
	for name := range closure {
		if st := u.Tables[name]; st != nil && !st.IsMapEntry() {
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
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

type descCtx struct {
	unit    *ir.Unit
	pkg     string
	owner   *ir.Struct
	arm     bool
	armName string
	outside bool
	msgS    map[string]bool
	msgU    map[string]bool
}

func tagsSymbol(owner, field string) string {
	return owner + "TableTags" + ir.GoExportName(field)
}

func annotationColumns(doc string, tags []string, tagsName string) (string, string, string) {
	docCol := "TableDocNone"
	if doc != "" {
		docCol = csQuote(doc)
	}
	list := "null"
	if len(tags) > 0 {
		list = tagsName
	}
	return docCol, fmt.Sprintf("%d", len(tags)), list
}

func enumNameLambda(pkg, enum string) string {
	return fmt.Sprintf("delegate (ulong v) { return v <= (ulong)%sMax ? EnumName%s((global::%s.%s)v) : \"???\"; }", enum, enum, pkg, enum)
}

func unionArmLambda(un *ir.Union, arm func(ir.UnionVariant) string, unknown, none string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "delegate (ulong v) { switch (v) { case 0: return %s;", none)
	for i, v := range un.Variants {
		fmt.Fprintf(&b, " case %d: return %s;", i+1, arm(v))
	}
	fmt.Fprintf(&b, " default: return %s; } }", unknown)
	return b.String()
}

func (c *descCtx) fieldMember(f *ir.Field) string {
	if c.arm && c.armName != "" {
		return c.armName
	}
	return member(f)
}

func (c *descCtx) storageExpr(f *ir.Field) string {
	return fmt.Sprintf("((__Carrier_%s)o).%s", c.owner.Name, c.fieldMember(f))
}

func (c *descCtx) elementExpr(f *ir.Field) string {
	if f.Array != ir.ArrayNone || f.IsMap() {
		return c.storageExpr(f) + "[i]"
	}
	return c.storageExpr(f)
}

func (c *descCtx) storageColumns(f *ir.Field) string {
	var b strings.Builder
	m := c.fieldMember(f)
	carrierT := "__Carrier_" + c.owner.Name
	cast := fmt.Sprintf("((%s)o).", carrierT)
	elemT := carrierElemType(c.pkg, f)
	switch {
	case f.Type.Pointer, f.IsMap():
		fmt.Fprintf(&b, ", GetChild = delegate (object o, int i) { return %s; }, SetChild = delegate (object o, int i, object v) { %s = (%s)v; }", c.elementExpr(f), c.elementExpr(f), elemT)
	case ir.TableKindWide(tableScalarKind(f)):
		st := scalarStorageType(f.Type)
		fmt.Fprintf(&b, ", GetWide = delegate (object o, int i) { return (UInt128Value)%s; }, SetWide = delegate (object o, int i, UInt128Value r) { %s = (%s)r; }", c.elementExpr(f), c.elementExpr(f), st)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		fmt.Fprintf(&b, ", GetBuffer = delegate (object o) { return %s; }", c.storageExpr(f))
		fmt.Fprintf(&b, ", GetCount = delegate (object o) { return %s%sLength; }", cast, m)
		fmt.Fprintf(&b, ", SetCount = delegate (object o, int n) { %s%sLength = n; }", cast, m)
	case isCarrierClassElem(f):
		fmt.Fprintf(&b, ", GetChild = delegate (object o, int i) { return %s; }", c.elementExpr(f))
	default:
		fmt.Fprintf(&b, ", GetRaw = delegate (object o, int i) { return %s; }", csRawGet(c.elementExpr(f), f.Type))
		fmt.Fprintf(&b, ", SetRaw = delegate (object o, int i, ulong r) { %s }", csRawSet(c.pkg, c.elementExpr(f), "r", f.Type))
	}
	if f.CountedOnWire() || f.IsMap() {
		fmt.Fprintf(&b, ", GetCount = delegate (object o) { return %s%sCount; }", cast, m)
		fmt.Fprintf(&b, ", SetCount = delegate (object o, int n) { %s%sCount = n; }", cast, m)
	}
	if f.IsMap() || f.IsList() {
		cls := isCarrierClassElem(f)
		fmt.Fprintf(&b, ", EnsureCount = delegate (object o, int n) { var value = (%s)o; if (value.%s.Length < n) { global::System.Array.Resize(ref value.%s, global::System.Math.Max(n, global::System.Math.Max(4, value.%s.Length * 2))); }", carrierT, m, m, m)
		if cls {
			fmt.Fprintf(&b, " for (int i = 0; i < n; i++) { if (value.%s[i] == null) { value.%s[i] = new %s(); } }", m, m, elemT)
		}
		b.WriteString(" }")
	}
	if f.Type.Optional {
		fmt.Fprintf(&b, ", GetPresent = delegate (object o) { return %s%sPresent; }", cast, m)
		fmt.Fprintf(&b, ", SetPresent = delegate (object o, bool p) { %s%sPresent = p; }", cast, m)
	}
	if f.Type.TextUnbounded() {
		fmt.Fprintf(&b, ", Unbounded = true, EnsureCount = delegate (object o, int n) { var value = (%s)o; if (value.%s.Length < n) { global::System.Array.Resize(ref value.%s, n); } }", carrierT, m, m)
	}
	if f.IsTextList() {
		fmt.Fprintf(&b, ", TextList = true, TextField = %q, TextTotal = %dL", c.owner.Name+"."+f.Name, f.TextTotal)
	}
	return b.String()
}

func (c *descCtx) nativeColumns(f *ir.Field) string {
	at := int64(0)
	if !c.arm {
		if field := ir.RecordLayout(c.unit, c.owner).FieldByName(f.Name); field != nil {
			at = field.Offset
		}
	}
	pieces := ir.FieldPieces(c.unit, f, at)
	count, present := int64(-1), int64(-1)
	if f.Type.Optional {
		present = pieces[len(pieces)-1].Offset
	}
	if !f.Type.TextUnbounded() && !f.IsList() && !f.IsMap() && (f.CountedOnWire() || (!f.Type.Pointer && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes))) {
		count = pieces[1].Offset
	}
	element := pieces[0].Size
	if f.IsList() || f.IsMap() {
		count = at + 8
		if f.IsMap() {
			element = ir.RecordLayout(c.unit, f.MapEntry).Size
		} else {
			element, _ = ir.ListElementLayout(c.unit, f)
		}
	} else if f.Array != ir.ArrayNone && f.ArrayBound > 0 {
		element /= f.ArrayBound
	}
	ordinal := 0
	if !c.arm {
		for i, field := range c.owner.Fields {
			if field == f {
				ordinal = i
				break
			}
		}
	}
	columns := fmt.Sprintf(", Ordinal = %d, NativeOffset = %d, NativeElementSize = %d, NativeCountOffset = %d, NativePresentOffset = %d", ordinal, at, element, count, present)
	if !c.arm {
		guard := tableGuardStrings(c.owner)[f.Name]
		if guard != "" {
			var offsets, values []string
			for term := range strings.SplitSeq(guard, " && ") {
				truth := !strings.HasPrefix(term, "!")
				name := strings.TrimPrefix(term, "!")
				field := ir.RecordLayout(c.unit, c.owner).FieldByName(name)
				offsets = append(offsets, fmt.Sprint(field.Offset))
				values = append(values, fmt.Sprint(truth))
			}
			columns += ", NativeGuardOffsets = new int[] {" + strings.Join(offsets, ",") + "}, NativeGuardValues = new bool[] {" + strings.Join(values, ",") + "}"
		}
	}
	return columns
}

func messageSlotCols(u *ir.Unit, f *ir.Field) string {
	columns := fmt.Sprintf(", MessageSlot = %d", tablewire.MessageVocabularySlots(u)[ir.TableFieldEntry(f).Key()])
	lo, hi, ok := ir.TableRawRange(f)
	if f.Type.Kind == ir.TBits {
		lo, hi, ok = big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1)), true
	} else if !ok && ir.TableKindWide(tableScalarKind(f)) {
		lo, hi = tableStorageRange(f.Type.Signed, f.Type.Width)
		ok = true
	}
	if ok {
		columns += fmt.Sprintf(", MessageBounded = true, MessageSigned = %t, MessageMin = %s, MessageMax = %s", f.Type.Signed, lit128Expr(lo, false), lit128Expr(hi, false))
	}
	return columns
}

func wideColumns(f *ir.Field) string {
	def := big.NewInt(0)
	if f.HasDefault && f.DefInt != nil {
		def = f.DefInt
	}
	var b strings.Builder
	fmt.Fprintf(&b, ", DefaultWide = %s, WideSigned = %v, FracBits = %d", lit128Expr(def, false), f.Type.Signed, f.Type.FracBits)
	lo, hi, bounded := ir.TableRawRange(f)
	if bounded {
		typ := "UInt128Value"
		if f.Type.Signed {
			typ = "Int128Value"
		}
		validLambda := fmt.Sprintf("delegate (UInt128Value raw) { %s v = (%s)raw; return v >= %s && v <= %s; }", typ, typ, lit128Expr(lo, f.Type.Signed), lit128Expr(hi, f.Type.Signed))
		if f.Required {
			fmt.Fprintf(&b, ", RequiredValidWide = %s", validLambda)
		}
		fmt.Fprintf(&b, ", WriteValidWide = %s", validLambda)
		fmt.Fprintf(&b, ", RangeWide = delegate (UInt128Value raw, _R r) { %s v = (%s)raw; if (v < %s || v > %s) { if (!r.Refused) { r.Refused = true; r.Reason = \"out_of_range\"; } } return raw; }", typ, typ, lit128Expr(lo, f.Type.Signed), lit128Expr(hi, f.Type.Signed))
	}
	return b.String()
}

func (c *descCtx) wireColumns(f *ir.Field) string {
	var b strings.Builder
	if c.owner.IsMapEntry() && !c.owner.TextElem && len(c.owner.Fields) > 0 && f == c.owner.Fields[0] {
		b.WriteString(", MapKey = true")
	}
	if (c.arm && c.msgU[c.owner.Name]) || (!c.arm && c.msgS[c.owner.Name]) {
		b.WriteString(messageSlotCols(c.unit, f))
	}
	b.WriteString(c.nativeColumns(f))
	if f.Required {
		b.WriteString(", Required = true")
	}
	if f.IsList() || f.IsMap() {
		size, align := int64(0), int64(0)
		if f.IsMap() {
			layout := ir.RecordLayout(c.unit, f.MapEntry)
			size, align = layout.Size, layout.Align
		} else {
			size, align = ir.ListElementLayout(c.unit, f)
		}
		fmt.Fprintf(&b, ", StorageSize = %d, StorageAlign = %d, Dynamic = true, Map = %t", size, align, f.IsMap())
	}
	if f.Type.Pointer {
		id := ir.TableWireId(f.Type.Name)
		if st, ok := f.Type.Ref.(*ir.Struct); ok {
			id = ir.TableWireId(st.WireName())
		}
		blob := 0
		if f.Type.Blob() {
			id = ir.BlobWireTypeId(f)
			blob = 14
			if f.Type.Kind == ir.TString {
				blob = 12
			}
		}
		fmt.Fprintf(&b, ", PointerTypeId = 0x%016xUL, BlobKind = %d", id, blob)
	}
	carrierT := "__Carrier_" + c.owner.Name
	if !c.arm {
		guards := tableGuardExprs(c.owner)
		if guard := guards[f.Name]; guard != "" {
			fmt.Fprintf(&b, ", WireGuard = delegate (object o) { var value = (%s)o; return %s; }", carrierT, guard)
		}
	}
	var resetBuf strings.Builder
	emitCarrierResetField(&resetBuf, c.pkg, f, c.fieldMember(f), "")
	fmt.Fprintf(&b, ", ResetField = delegate (object o) { var value = (%s)o; %s }", carrierT, strings.Join(strings.Fields(resetBuf.String()), " "))
	if len(f.DefBytes) > 0 {
		fmt.Fprintf(&b, ", DefaultBytes = new byte[] { %s }", byteLiterals(f.DefBytes))
	}
	if ir.TableKindWide(tableScalarKind(f)) {
		b.WriteString(wideColumns(f))
		return b.String()
	}
	if f.IsMap() || f.Type.Pointer || f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || isCarrierClassElem(f) {
		return b.String()
	}
	defExpr := scalarDefaultExprQual(c.pkg, f)
	fmt.Fprintf(&b, ", DefaultRaw = %s", csRawGet(defExpr, f.Type))
	kind := tableScalarKind(f)
	if kind == tkF32 || kind == tkF64 {
		owner := c.owner.DeclaredName() + "." + f.Name
		fmt.Fprintf(&b, ", NonFiniteField = %q, NonFiniteReason = %q", owner, "non_finite_float: "+owner)
	}
	if kind == tkBool {
		return b.String()
	}
	if isEnumRef(f.Type) {
		if en, ok := f.Type.Ref.(*ir.Enum); ok {
			var depConds []string
			for i := range en.Variants {
				if enumVariantDeprecated(en, i) {
					depConds = append(depConds, fmt.Sprintf("v == %dUL", i+1))
				}
			}
			if len(depConds) > 0 {
				fmt.Fprintf(&b, ", EnumDeprecated = delegate (ulong v) { return %s; }", strings.Join(depConds, " || "))
				fmt.Fprintf(&b, ", WriteValidRaw = delegate (ulong v) { return v <= %dUL && !(%s); }", len(en.Variants), strings.Join(depConds, " || "))
			}
		}
		return b.String()
	}
	if f.HasIntRange {
		signed := (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed
		rlo, rhi, _ := ir.TableRawRange(f)
		typ, val := "ulong", "raw"
		if signed {
			typ, val = "long", "unchecked((long)raw)"
		}
		fmt.Fprintf(&b, ", RangeJsonInteger = delegate (ulong raw, _R r) { %s v = %s; if (v < %s || v > %s) { if (!r.Refused) { r.Refused = true; r.Reason = \"out_of_range\"; } } return raw; }",
			typ, val, csIntLit(rlo, signed, 8), csIntLit(rhi, signed, 8))
	} else if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
		high := (uint64(1) << f.Type.Width) - 1
		fmt.Fprintf(&b, ", RangeJsonInteger = delegate (ulong raw, _R r) { if (raw > %dUL) { if (!r.Refused) { r.Refused = true; r.Reason = \"out_of_range\"; } } return raw; }", high)
	}
	if f.HasIntRange && tableKindWidth(kind) <= 8 {
		st := scalarStorageType(f.Type)
		val := strings.TrimSuffix(strings.TrimPrefix(csRawSet(c.pkg, "v", "raw", f.Type), "v = "), ";")
		signed := (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed
		width := tableKindWidth(kind)
		rlo, rhi, _ := ir.TableRawRange(f)
		validLambda := fmt.Sprintf("delegate (ulong raw) { %s v = %s; return v >= %s && v <= %s; }", st, val, csIntLit(rlo, signed, width), csIntLit(rhi, signed, width))
		if f.Required {
			fmt.Fprintf(&b, ", RequiredValidRaw = %s", validLambda)
		}
		fmt.Fprintf(&b, ", WriteValidRaw = %s", validLambda)
	} else if f.Type.Kind == ir.TBits && int64(f.Type.Width) < 8*ir.TableFixedStorageBytes(f.Type) {
		high := (uint64(1) << f.Type.Width) - 1
		fmt.Fprintf(&b, ", WriteValidRaw = delegate (ulong raw) { return raw <= %dUL; }", high)
	}
	if f.HasFloatRange {
		b.WriteString(", HasFloatRange = true")
		st := scalarStorageType(f.Type)
		val := strings.TrimSuffix(strings.TrimPrefix(csRawSet(c.pkg, "v", "raw", f.Type), "v = "), ";")
		lit := formatFloat64Lit
		if kind == tkF32 {
			lit = formatFloat32Lit
		}
		validLambda := fmt.Sprintf("delegate (ulong raw) { %s v = %s; return v >= %s && v <= %s; }", st, val, lit(f.FMin), lit(f.FMax))
		if f.Required {
			fmt.Fprintf(&b, ", RequiredValidRaw = %s", validLambda)
		}
		fmt.Fprintf(&b, ", WriteValidRaw = %s", validLambda)
	}
	var rc strings.Builder
	st := scalarStorageType(f.Type)
	if f.Type.Kind == ir.TNamed {
		st = "global::" + c.pkg + "." + f.Type.Name
	}
	fmt.Fprintf(&rc, "%s v = %s; ", st, strings.TrimSuffix(strings.TrimPrefix(csRawSet(c.pkg, "v", "raw", f.Type), "v = "), ";"))
	if f.HasIntRange {
		width := tableKindWidth(kind)
		signed := (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed
		rlo, rhi, _ := ir.TableRawRange(f)
		low, high := tableClampEnds(f, width)
		if low {
			fmt.Fprintf(&rc, "if (v < %s) { if (!r.Refused) { r.Refused = true; r.Reason = \"out_of_range\"; } } ", csIntLit(rlo, signed, width))
		}
		if high {
			fmt.Fprintf(&rc, "if (v > %s) { if (!r.Refused) { r.Refused = true; r.Reason = \"out_of_range\"; } } ", csIntLit(rhi, signed, width))
		}
	} else if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
		fmt.Fprintf(&rc, "if ((ulong)v > %dUL) { if (!r.Refused) { r.Refused = true; r.Reason = \"out_of_range\"; } } ", (uint64(1)<<f.Type.Width)-1)
	}
	if f.HasFloatRange {
		lit := formatFloat64Lit
		if kind == tkF32 {
			lit = formatFloat32Lit
		}
		fmt.Fprintf(&rc, "if (v < %s || v > %s) { if (!r.Refused) { r.Refused = true; r.Reason = \"out_of_range\"; } } ", lit(f.FMin), lit(f.FMax))
		// A COMPRESSED FLOAT'S -0 LANDS AS +0 (docs/SPEC-TABLES.md §3.3): -0
		// + +0 is +0, and every other value is itself
		rc.WriteString("v = v + 0; ")
	}
	fmt.Fprintf(&rc, "return %s;", csRawGet("v", f.Type))
	fmt.Fprintf(&b, ", RangeRaw = delegate (ulong raw, _R r) { %s }", rc.String())
	return b.String()
}

func (c *descCtx) unionArmsExpr(un *ir.Union) string {
	var b strings.Builder
	carrierU := "__Carrier_" + un.Name
	fmt.Fprintf(&b, "new TableUnionInfo { GetTag = delegate (object o) { return (ulong)((%s)o).Type; }", carrierU)
	fmt.Fprintf(&b, ", Create = delegate { return new %s(); }", carrierU)
	_, _, tagSize, armOffset := ir.UnionLayout(c.unit, un)
	fmt.Fprintf(&b, ", NativeTagSize = %d, NativeArmOffset = %d", tagSize, armOffset)
	fmt.Fprintf(&b, ", SetTag = delegate (object o, ulong t) { ((%s)o).Type = unchecked((global::%s.%sType)t); }", carrierU, c.pkg, un.Name)
	b.WriteString(", Arms = new TableUnionArmInfo[] { new TableUnionArmInfo()")
	for _, v := range un.Variants {
		b.WriteString(", new TableUnionArmInfo { ")
		if c.msgU[un.Name] {
			fmt.Fprintf(&b, "MessageSlot = %d, ", tablewire.MessageVocabularySlots(c.unit)[ir.TableArmEntry(v).Key()])
		}
		if !v.Void() {
			sub := &descCtx{
				unit: c.unit, pkg: c.pkg, owner: unionOwner(un),
				arm: true, armName: ir.GoExportName(v.Name), outside: c.outside,
				msgS: c.msgS, msgU: c.msgU,
			}
			fmt.Fprintf(&b, "Field = %s", sub.fieldExpr(v.F, ""))
			if v.Body() {
				fmt.Fprintf(&b, ", TableRef = delegate { return %sTableType; }, Payload = delegate (object o) { return ((%s)o).%s; }",
					v.Type, carrierU, ir.GoExportName(v.Name))
			}
		}
		b.WriteString(" }")
	}
	b.WriteString(" } }")
	return b.String()
}

func (c *descCtx) fieldExpr(f *ir.Field, guard string) string {
	id := ir.TableFieldWireId(f)
	if c.outside {
		id = 0
	}
	kind := tableScalarKind(f)
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		kind = tkU8
	}
	isArray := f.Array != ir.ArrayNone || f.IsMap() || (f.Type.Kind == ir.TBytes && !f.Type.Pointer)
	counted := f.CountedOnWire() || f.IsMap() || (!f.Type.Pointer && (f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString))

	bound := "0"
	switch {
	case f.IsMap(), f.IsList(), f.Type.TextUnbounded():
		bound = "int.MaxValue"
	case f.KeyEnum != "":
		bound = fmt.Sprintf("(int)global::%s.Schema.%sMax", c.pkg, f.KeyEnum)
	case f.Array != ir.ArrayNone:
		bound = strconv.FormatInt(f.ArrayBound, 10)
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString:
		bound = strconv.FormatInt(f.Type.Size, 10)
	}
	arrayMin := int64(0)
	if f.Array == ir.ArrayCounted {
		arrayMin = f.ArrayMin
	}

	tableRef := "null"
	if f.IsMap() {
		tableRef = fmt.Sprintf("delegate { return %sTableType; }", f.MapEntry.Name)
		kind = 13
	}
	if _, isStruct := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && isStruct {
		tableRef = fmt.Sprintf("delegate { return %sTableType; }", f.Type.Name)
	}

	keyTypeName, keyName, keyId := "null", "null", "null"
	if f.KeyEnum != "" {
		keyTypeName = fmt.Sprintf("%q", f.KeyEnum)
		keyName = enumNameLambda(c.pkg, f.KeyEnum)
		keyId = fmt.Sprintf("delegate (ulong v) { ulong id; TableEnumId((global::%s.%s)v, out id); return id; }", c.pkg, f.KeyEnum)
	}

	hasRange := "false"
	rangeMin, rangeMax := "0.0", "0.0"
	if f.Type.Kind == ir.TBits && !f.HasIntRange {
		maxV := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		hasRange = "true"
		rangeMin, rangeMax = "0.0", bigToDouble(maxV)
	}
	if f.HasIntRange {
		hasRange = "true"
		rangeMin, rangeMax = bigToDouble(f.IntMin), bigToDouble(f.IntMax)
	} else if f.HasFloatRange {
		hasRange = "true"
		rangeMin, rangeMax = formatFloat64Lit(f.FMin), formatFloat64Lit(f.FMax)
	}

	enumMax := "-1"
	enumName := "null"
	variantId := "null"
	arms := "null"
	switch ref := f.Type.Ref.(type) {
	case *ir.Enum:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", ref.Max)
			enumName = enumNameLambda(c.pkg, f.Type.Name)
			variantId = fmt.Sprintf("delegate (ulong v) { ulong id; TableEnumId((global::%s.%s)v, out id); return id; }", c.pkg, f.Type.Name)
		}
	case *ir.Flags:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", len(ref.Variants)-1)
			enumName = fmt.Sprintf("delegate (ulong v) { return FlagName%s((int)v); }", f.Type.Name)
		}
	case *ir.Union:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", len(ref.Variants))
			enumName = unionArmLambda(ref, func(v ir.UnionVariant) string {
				return fmt.Sprintf("%q", v.Name)
			}, "\"???\"", "\"None\"")
			variantId = unionArmLambda(ref, func(v ir.UnionVariant) string {
				return fmt.Sprintf("(ulong)0x%016xUL", ir.TableWireId(v.WireName()))
			}, "(ulong)0", "(ulong)0")
			arms = c.unionArmsExpr(ref)
		}
	}

	if c.outside {
		if variantId != "null" {
			variantId = "delegate (ulong v) { return 0; }"
		}
		if keyId != "null" {
			keyId = "delegate (ulong v) { return 0; }"
		}
	}

	doc, numTags, tags := annotationColumns(f.Doc, f.Tags, tagsSymbol(c.owner.Name, f.Name))
	if c.arm && len(f.Tags) > 0 {
		tags = armTags(f.Tags)
	}
	jsonName := csQuote(ir.TableFieldJsonKey(f))
	if c.outside {
		jsonName = "null"
	}
	return fmt.Sprintf("new TableFieldInfo { Name = %q, Json = %s, TypeName = %q, Id = 0x%016xUL, Kind = %d, IsArray = %v, Counted = %v, Optional = %v, ArrayMin = %d, ArrayBound = %s, ElemWidth = %d, HasRange = %s, RangeMin = %s, RangeMax = %s, EnumMax = %s, EnumName = %s, VariantId = %s, KeyTypeName = %s, KeyName = %s, KeyId = %s, Guard = %q, TableRef = %s, Arms = %s, Doc = %s, NumTags = %s, Tags = %s%s }",
		f.Name, jsonName, tableFieldTypeName(f), id, kind, isArray, counted, f.Type.Optional, arrayMin, bound,
		csElemWidth(f.Type), hasRange, rangeMin, rangeMax, enumMax, enumName, variantId,
		keyTypeName, keyName, keyId, guard, tableRef, arms, doc, numTags, tags, c.storageColumns(f)+c.wireColumns(f))
}

func emitTableDescriptor(b *strings.Builder, u *ir.Unit, st *ir.Struct, outside bool, msgS, msgU map[string]bool) {
	pkg := namespaceOf(u.Package)
	c := &descCtx{unit: u, pkg: pkg, owner: st, outside: outside, msgS: msgS, msgU: msgU}
	guards := tableGuardStrings(st)
	holder := st.Name + "TableInfo"
	fmt.Fprintf(b, "private static class %s\n{\n", holder)
	for _, f := range st.Fields {
		if len(f.Tags) > 0 {
			fmt.Fprintf(b, "    private static readonly string[] %s = { %s };\n", tagsSymbol(st.Name, f.Name), csQuoteList(f.Tags))
		}
	}
	if len(st.Tags) > 0 {
		fmt.Fprintf(b, "    private static readonly string[] %s = { %s };\n", tagsSymbol(st.Name, ""), csQuoteList(st.Tags))
	}
	b.WriteString("    internal static readonly TableTypeInfo Instance = Build();\n\n")
	b.WriteString("    private static TableTypeInfo Build()\n    {\n")
	b.WriteString("        TableTypeInfo info = new TableTypeInfo();\n")
	fmt.Fprintf(b, "        info.Name = %q;\n", st.Name)
	identity := ir.TableWireId(st.WireName())
	if outside {
		identity = 0
	}
	fmt.Fprintf(b, "        info.Id = 0x%016xUL;\n", identity)
	fmt.Fprintf(b, "        info.NumFields = %d;\n", len(st.Fields))
	for _, f := range st.Fields {
		if f.Required {
			b.WriteString("        info.HasRequired = true;\n")
			break
		}
	}
	fmt.Fprintf(b, "        info.Create = delegate { return new __Carrier_%s(); };\n", st.Name)
	layout := ir.RecordLayout(u, st)
	align := int64(8)
	for _, table := range u.Tables {
		if a := ir.CookNodeAlign(u, table); a > align {
			align = a
		}
	}
	fmt.Fprintf(b, "        info.StorageSize = %d; info.StorageAlign = %d; info.RegionAlign = %d;\n", layout.Size, ir.CookNodeAlign(u, st), align)
	fmt.Fprintf(b, "        info.Fixed = %t;\n", st.IsTable && st.FixedDeclared)
	fmt.Fprintf(b, "        info.Variable = %t;\n", ir.VariableTables(u)[st.Name])
	fmt.Fprintf(b, "        info.Integrity = %t;\n", st.Integrity == "crc32c")
	if st.JsonArray {
		b.WriteString("        info.JsonArray = true;\n")
	}
	fmt.Fprintf(b, "        info.RootElemSlots = %d;\n", rootElemSlots(st))

	var pt strings.Builder
	pt.WriteString("        info.PointerType = delegate (ulong id) { switch (id) { ")
	for _, target := range ir.PointerReachable(st) {
		fmt.Fprintf(&pt, "case 0x%016xUL: return %sTableType; ", ir.TableWireId(target.WireName()), target.Name)
	}
	pt.WriteString("default: return null; } };\n")
	b.WriteString(pt.String())

	var pts strings.Builder
	pts.WriteString("        info.PointerTypes = delegate { return new TableTypeInfo[] { ")
	for _, target := range ir.PointerReachable(st) {
		fmt.Fprintf(&pts, "%sTableType, ", target.Name)
	}
	pts.WriteString("}; };\n")
	b.WriteString(pts.String())

	var dt strings.Builder
	dt.WriteString("        info.DeclaredType = delegate (ulong id) { switch (id) { ")
	for _, target := range declaredTables(u) {
		fmt.Fprintf(&dt, "case 0x%016xUL: return %sTableType; ", ir.TableWireId(target.WireName()), target.Name)
	}
	dt.WriteString("default: return null; } };\n")
	b.WriteString(dt.String())

	bytesEdge, stringEdge := ir.PointerReachableBlobs(st)
	fmt.Fprintf(b, "        info.BytesEdge = %t; info.StringEdge = %t;\n", bytesEdge, stringEdge)

	if len(st.Fields) == 0 {
		b.WriteString("        info.Fields = global::System.Array.Empty<TableFieldInfo>();\n")
	} else {
		b.WriteString("        info.Fields = new TableFieldInfo[]\n        {\n")
		for _, f := range st.Fields {
			fmt.Fprintf(b, "            %s,\n", c.fieldExpr(f, guards[f.Name]))
		}
		b.WriteString("        };\n")
	}
	fmt.Fprintf(b, "        info.Reset = delegate (object o) { TableReset((__Carrier_%s)o); };\n", st.Name)
	doc, numTags, tags := annotationColumns(st.Doc, st.Tags, tagsSymbol(st.Name, ""))
	fmt.Fprintf(b, "        info.Doc = %s;\n        info.NumTags = %s;\n        info.Tags = %s;\n", doc, numTags, tags)
	b.WriteString("        TableWire.IndexFields(info);\n")
	b.WriteString("        return info;\n    }\n}\n\n")
	fmt.Fprintf(b, "public static TableTypeInfo %sTableType => %s.Instance;\n\n", st.Name, holder)
}
