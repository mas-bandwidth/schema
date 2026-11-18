package unitytable

import (
	"fmt"
	"math/bits"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	cookHeaderBytes = int64(64)
	cookMaxAlign    = int64(64)
	cookMagicWord   = uint64(0x4B4F4F434D484353)
)

const unityBlockCookRuntimeSource = `public enum TableRefuseReason
{
    ok,
    not_a_cook,
    foreign_order,
    wrong_build_version,
    reserved_not_zero,
    bad_alignment,
    truncated,
    unaligned_base,
    bad_layout,
    unknown_form,
    count_over_length,
    count_over_extent_cap,
    blob_over_size_cap,
    data_cycle,
    newer_form,
    previous_form,
    message_form_as_file,
    no_layout,
    layout_malformed,
    plan_too_large,
    batch_too_large,
    layout_count_mismatch,
    layout_kind_unknown,
    layout_kind_invalid,
    layout_size_mismatch,
    layout_tree_unclosed,
    layout_too_deep,
    layout_record_too_large,
    non_finite_float,
    integrity_mismatch,
    blob_shared,
    kind_mismatch
}

internal static TableRefuseReason _ParseRefuseReason(string why)
{
    if (string.IsNullOrEmpty(why)) { return TableRefuseReason.ok; }
    if (Enum.TryParse<TableRefuseReason>(why, false, out TableRefuseReason parsed)) { return parsed; }
    if (why.StartsWith("non_finite_float", StringComparison.Ordinal)) { return TableRefuseReason.non_finite_float; }
    return TableRefuseReason.bad_layout;
}

public sealed class TableBlockRefusal
{
    public string Array = "";
    public long Count;
    public long Maximum;
}

public sealed class TableBlockFieldInfo
{
    public string Name;
    public int Offset;
    public int Size;
    public byte Kind;
    public bool OutOfLine;
    public int OffsetOfOffset;
    public int CountOffset;
    public int StrideOffset;
    public int Stride;
    public bool IsArray;
    public bool Counted;
    public bool Optional;
    public int ArrayBound;
    public int ElemSize;
    public int PresentOffset;
    public Func<TableBlockInfo> ElementRef;
    public TableBlockInfo Element => ElementRef == null ? null : ElementRef();
}

public sealed class TableBlockInfo
{
    public string Name;
    public ulong BuildVersion;
    public int Size;
    public int Align;
    public int NumFields;
    public TableBlockFieldInfo[] Fields;
}

public enum TableCookStorage
{
    Signed,
    Unsigned,
    Boolean,
    Float,
    Record,
    Reference,
    String,
    Bytes,
}

public sealed class TableCookFieldInfo
{
    public string Name;
    public int Offset;
    public int Size;
    public int ElemSize;
    public bool IsArray;
    public int ArrayBound;
    public bool IsPointer;
    public bool Unbounded;
    public int CountOffset;
    public int PresentOffset;
    public TableCookStorage Storage;
    public Func<TableCookInfo> RecordRef;
    public TableCookInfo Record => RecordRef == null ? null : RecordRef();
}

public sealed class TableCookInfo
{
    public string Name;
    public int Size;
    public int Align;
    public int NumFields;
    public TableCookFieldInfo[] Fields;
}

public const ulong TableBlockMagic = 0x4B434C424D484353UL;
public static ulong TableBlockByteOrder => BitConverter.IsLittleEndian ? 1UL : 2UL;

public const ulong TableCookMagic = 0x4B4F4F434D484353UL;
public static ulong TableCookByteOrder => BitConverter.IsLittleEndian ? 1UL : 2UL;
public const long TableCookHeaderBytes = 64;
public const long TableCookMaxAlign = 64;

public static ulong TableBlockRead64(ReadOnlySpan<byte> bytes, int at)
{
    return BitConverter.IsLittleEndian
        ? BinaryPrimitives.ReadUInt64LittleEndian(bytes.Slice(at, 8))
        : BinaryPrimitives.ReadUInt64BigEndian(bytes.Slice(at, 8));
}

public static ulong TableCookRead64(ReadOnlySpan<byte> bytes, int at)
{
    return TableBlockRead64(bytes, at);
}

internal static int _CookTextStart(byte[] bytes, int regionEnd, int slot)
{
    if (bytes == null || slot < 0 || slot > regionEnd - 8) { return -1; }
    long delta = BinaryPrimitives.ReadInt64LittleEndian(bytes.AsSpan(slot, 8));
    if (delta == 0) { return -1; }
    long target = slot + delta;
    if (target < 0 || target > regionEnd - 8) { return -1; }
    uint used = BinaryPrimitives.ReadUInt32LittleEndian(bytes.AsSpan((int)target, 4));
    if (used > (ulong)(regionEnd - target - 8)) { return -1; }
    return (int)target;
}

`

func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func isRowStructRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed || t.Pointer {
		return false
	}
	_, ok := t.Ref.(*ir.Struct)
	return ok
}

func isRowUnionRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed || t.Pointer {
		return false
	}
	_, ok := t.Ref.(*ir.Union)
	return ok
}

var reservedRowMembers = map[string]bool{
	"Bytes": true, "Base": true, "Length": true, "At": true, "RegionEnd": true,
	"RowSize": true, "RowAlign": true, "BlockMaxBytes": true, "ProjectionSize": true,
	"NonFiniteFloat": true, "Type": true, "Open": true, "OpenWithReason": true,
	"Begin": true, "Cursor": true, "Root": true,
}

func rowAccessorBase(f *ir.Field) string {
	base := member(f)
	for _, suf := range []string{"", "Length", "Bytes", "Bound", "At", "Present", "Count", "Find", "Pitch", "OffsetOf", "Cursor", "Stride", "Max"} {
		if reservedRowMembers[base+suf] {
			return base + "_"
		}
	}
	return base
}

func rowScalarCsType(pkg string, t ir.FieldType) string {
	if t.Kind == ir.TNamed {
		return "global::" + pkg + "." + t.Name
	}
	return scalarStorageType(t)
}

func rowReadExpr(pkg string, f *ir.Field, bytesExpr, atExpr string) string {
	t := f.Type
	sp := fmt.Sprintf("%s.AsSpan(%s)", bytesExpr, atExpr)
	switch t.Kind {
	case ir.TBool:
		return fmt.Sprintf("(%s[%s] != 0)", bytesExpr, atExpr)
	case ir.TInt, ir.TFixed:
		if t.Width == 128 {
			lo := fmt.Sprintf("BinaryPrimitives.ReadUInt64LittleEndian(%s.Slice(0, 8))", sp)
			hi := fmt.Sprintf("BinaryPrimitives.ReadUInt64LittleEndian(%s.Slice(8, 8))", sp)
			if t.Signed {
				return fmt.Sprintf("new Int128Value(%s, %s)", hi, lo)
			}
			return fmt.Sprintf("new UInt128Value(%s, %s)", hi, lo)
		}
		if t.Signed {
			switch t.Width {
			case 8:
				return fmt.Sprintf("unchecked((sbyte)%s[%s])", bytesExpr, atExpr)
			case 16:
				return fmt.Sprintf("BinaryPrimitives.ReadInt16LittleEndian(%s.Slice(0, 2))", sp)
			case 32:
				return fmt.Sprintf("BinaryPrimitives.ReadInt32LittleEndian(%s.Slice(0, 4))", sp)
			default:
				return fmt.Sprintf("BinaryPrimitives.ReadInt64LittleEndian(%s.Slice(0, 8))", sp)
			}
		}
		switch t.Width {
		case 8:
			return fmt.Sprintf("%s[%s]", bytesExpr, atExpr)
		case 16:
			return fmt.Sprintf("BinaryPrimitives.ReadUInt16LittleEndian(%s.Slice(0, 2))", sp)
		case 32:
			return fmt.Sprintf("BinaryPrimitives.ReadUInt32LittleEndian(%s.Slice(0, 4))", sp)
		default:
			return fmt.Sprintf("BinaryPrimitives.ReadUInt64LittleEndian(%s.Slice(0, 8))", sp)
		}
	case ir.TBits:
		if t.Width <= 8 {
			return fmt.Sprintf("(uint)%s[%s]", bytesExpr, atExpr)
		}
		if t.Width <= 16 {
			return fmt.Sprintf("(uint)BinaryPrimitives.ReadUInt16LittleEndian(%s.Slice(0, 2))", sp)
		}
		if t.Width <= 32 {
			return fmt.Sprintf("BinaryPrimitives.ReadUInt32LittleEndian(%s.Slice(0, 4))", sp)
		}
		return fmt.Sprintf("BinaryPrimitives.ReadUInt64LittleEndian(%s.Slice(0, 8))", sp)
	case ir.TNamed:
		qual := "global::" + pkg + "." + t.Name
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			w := (ref.StorageBits + 7) / 8
			switch w {
			case 1:
				return fmt.Sprintf("(%s)%s[%s]", qual, bytesExpr, atExpr)
			case 2:
				return fmt.Sprintf("(%s)BinaryPrimitives.ReadUInt16LittleEndian(%s.Slice(0, 2))", qual, sp)
			case 4:
				return fmt.Sprintf("(%s)BinaryPrimitives.ReadUInt32LittleEndian(%s.Slice(0, 4))", qual, sp)
			default:
				return fmt.Sprintf("(%s)BinaryPrimitives.ReadUInt64LittleEndian(%s.Slice(0, 8))", qual, sp)
			}
		case *ir.Flags:
			return fmt.Sprintf("(%s)BinaryPrimitives.ReadUInt64LittleEndian(%s.Slice(0, 8))", qual, sp)
		}
	}
	return "default"
}

func emitRowScalarMember(b *strings.Builder, u *ir.Unit, pkg, ownerName string, f *ir.Field, isMethod bool, sigHead, bytesExpr, atExpr, ind string) {
	if f.Type.Kind == ir.TFloat32 {
		ownerField := ownerName + "." + f.Name
		if isMethod {
			fmt.Fprintf(b, "%spublic float %s\n%s{\n", ind, sigHead, ind)
		} else {
			fmt.Fprintf(b, "%spublic float %s\n%s{\n%s    get\n%s    {\n", ind, sigHead, ind, ind, ind)
		}
		bi := ind + "    "
		if !isMethod {
			bi = ind + "        "
		}
		fmt.Fprintf(b, "%suint bits = BinaryPrimitives.ReadUInt32LittleEndian(%s.AsSpan(%s, 4));\n", bi, bytesExpr, atExpr)
		fmt.Fprintf(b, "%sif ((bits & 0x7F800000u) != 0x7F800000u) { return Schema.TableBitsToFloat(bits); }\n", bi)
		fmt.Fprintf(b, "%sif (NonFiniteFloat.Length == 0) { NonFiniteFloat = %q; }\n", bi, ownerField)
		fmt.Fprintf(b, "%sreturn 0.0f;\n", bi)
		if !isMethod {
			fmt.Fprintf(b, "%s    }\n", ind)
		}
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	if f.Type.Kind == ir.TFloat64 {
		ownerField := ownerName + "." + f.Name
		if isMethod {
			fmt.Fprintf(b, "%spublic double %s\n%s{\n", ind, sigHead, ind)
		} else {
			fmt.Fprintf(b, "%spublic double %s\n%s{\n%s    get\n%s    {\n", ind, sigHead, ind, ind, ind)
		}
		bi := ind + "    "
		if !isMethod {
			bi = ind + "        "
		}
		fmt.Fprintf(b, "%sulong bits = BinaryPrimitives.ReadUInt64LittleEndian(%s.AsSpan(%s, 8));\n", bi, bytesExpr, atExpr)
		fmt.Fprintf(b, "%sif ((bits & 0x7FF0000000000000UL) != 0x7FF0000000000000UL) { return Schema.TableBitsToDouble(bits); }\n", bi)
		fmt.Fprintf(b, "%sif (NonFiniteFloat.Length == 0) { NonFiniteFloat = %q; }\n", bi, ownerField)
		fmt.Fprintf(b, "%sreturn 0.0;\n", bi)
		if !isMethod {
			fmt.Fprintf(b, "%s    }\n", ind)
		}
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	retT := rowScalarCsType(pkg, f.Type)
	expr := rowReadExpr(pkg, f, bytesExpr, atExpr)
	if isMethod {
		fmt.Fprintf(b, "%spublic %s %s { return %s; }\n", ind, retT, sigHead, expr)
	} else {
		fmt.Fprintf(b, "%spublic %s %s => %s;\n", ind, retT, sigHead, expr)
	}
}

func emitInlineRowAccessors(b *strings.Builder, u *ir.Unit, pkg, ownerName string, ml *ir.MemberLayout, bl *ir.BlockLayout, bytesExpr, originExpr, endExpr, ind string) {
	fmt.Fprintf(b, "%spublic string NonFiniteFloat = \"\";\n", ind)
	for _, fl := range ml.Fields {
		f := fl.Field
		if bl != nil && ir.BlockOutOfLine(f) {
			continue
		}
		facts := ir.BlockFieldOf(u, f, fl.Offset, bl != nil)
		acc := rowAccessorBase(f)
		switch {
		case f.IsMap():
			emitCookMapRowAccessors(b, u, pkg, f, fl.Offset, acc, bytesExpr, originExpr, endExpr, ind)
			continue
		case f.IsList() && bl == nil:
			emitCookListRowAccessors(b, u, pkg, ownerName, f, fl.Offset, acc, bytesExpr, originExpr, endExpr, ind)
			continue
		case f.Type.TextUnbounded() && bl == nil:
			fmt.Fprintf(b, "%spublic int %sLength { get { int p = Schema._CookTextStart(%s, %s, %s + %d); return p < 0 ? 0 : (int)BinaryPrimitives.ReadUInt32LittleEndian(%s.AsSpan(p, 4)); } }\n",
				ind, acc, bytesExpr, endExpr, originExpr, fl.Offset, bytesExpr)
			fmt.Fprintf(b, "%spublic ReadOnlySpan<byte> %sBytes() { int p = Schema._CookTextStart(%s, %s, %s + %d); return p < 0 ? ReadOnlySpan<byte>.Empty : %s.AsSpan(p + 8, %sLength); }\n",
				ind, acc, bytesExpr, endExpr, originExpr, fl.Offset, bytesExpr, acc)
		case f.Type.Pointer && f.Array == ir.ArrayNone:
			fmt.Fprintf(b, "%spublic int %s => %s + %d;\n", ind, acc, originExpr, fl.Offset)
		case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
			fmt.Fprintf(b, "%spublic int %sLength { get { int used = BinaryPrimitives.ReadInt32LittleEndian(%s.AsSpan(%s + %d, 4)); return used < 0 || used > %d ? 0 : used; } }\n",
				ind, acc, bytesExpr, originExpr, facts.CountOffset, f.Type.Size)
			fmt.Fprintf(b, "%spublic ReadOnlySpan<byte> %sBytes => %s.AsSpan(%s + %d, %sLength);\n",
				ind, acc, bytesExpr, originExpr, fl.Offset, acc)
		case facts.IsArray && isRowStructRef(f.Type):
			rowT := f.Type.Name + "Row"
			fmt.Fprintf(b, "%spublic const int %sBound = %d;\n", ind, acc, facts.ArrayBound)
			fmt.Fprintf(b, "%spublic %s %sAt(int index, %s into) { into.At = %s + %d + index * %d; return into; }\n",
				ind, rowT, acc, rowT, originExpr, fl.Offset, facts.ElemSize)
		case facts.IsArray && (f.Type.Pointer || isRowUnionRef(f.Type)):
			fmt.Fprintf(b, "%spublic const int %sBound = %d;\n", ind, acc, facts.ArrayBound)
			fmt.Fprintf(b, "%spublic int %sAt(int index) { return %s + %d + index * %d; }\n",
				ind, acc, originExpr, fl.Offset, facts.ElemSize)
		case facts.IsArray:
			fmt.Fprintf(b, "%spublic const int %sBound = %d;\n", ind, acc, facts.ArrayBound)
			if facts.Counted && facts.CountOffset >= 0 {
				fmt.Fprintf(b, "%spublic int %sCount { get { int used = BinaryPrimitives.ReadInt32LittleEndian(%s.AsSpan(%s + %d, 4)); return used < 0 || used > %d ? 0 : used; } }\n",
					ind, acc, bytesExpr, originExpr, facts.CountOffset, facts.ArrayBound)
			}
			elem := *f
			elem.Array = ir.ArrayNone
			elem.ArrayBound = 0
			emitRowScalarMember(b, u, pkg, ownerName, &elem, true, fmt.Sprintf("%sAt(int index)", acc), bytesExpr, fmt.Sprintf("%s + %d + index * %d", originExpr, fl.Offset, facts.ElemSize), ind)
		case isRowStructRef(f.Type):
			rowT := f.Type.Name + "Row"
			fmt.Fprintf(b, "%spublic %s %s(%s into) { into.At = %s + %d; return into; }\n",
				ind, rowT, acc, rowT, originExpr, fl.Offset)
		case isRowUnionRef(f.Type):
			fmt.Fprintf(b, "%spublic int %s => %s + %d;\n", ind, acc, originExpr, fl.Offset)
		default:
			emitRowScalarMember(b, u, pkg, ownerName, f, false, acc, bytesExpr, fmt.Sprintf("%s + %d", originExpr, fl.Offset), ind)
		}
		if facts.Optional && facts.PresentOffset >= 0 {
			fmt.Fprintf(b, "%spublic bool %sPresent => %s[%s + %d] != 0;\n", ind, acc, bytesExpr, originExpr, facts.PresentOffset)
		}
	}
}

func emitCookListRowAccessors(b *strings.Builder, u *ir.Unit, pkg, ownerName string, f *ir.Field, offset int64, acc, bytesExpr, originExpr, endExpr, ind string) {
	size, _ := ir.ListElementLayout(u, f)
	fmt.Fprintf(b, "%spublic int %sCount\n%s{\n%s    get\n%s    {\n", ind, acc, ind, ind, ind)
	fmt.Fprintf(b, "%s        int slot = %s + %d;\n", ind, originExpr, offset)
	fmt.Fprintf(b, "%s        if (slot < 0 || slot > %s - 12) { return 0; }\n", ind, endExpr)
	fmt.Fprintf(b, "%s        long delta = BinaryPrimitives.ReadInt64LittleEndian(%s.AsSpan(slot, 8));\n", ind, bytesExpr)
	fmt.Fprintf(b, "%s        int count = BinaryPrimitives.ReadInt32LittleEndian(%s.AsSpan(slot + 8, 4));\n", ind, bytesExpr)
	fmt.Fprintf(b, "%s        if (count <= 0 || delta == 0 || slot + delta < 0 || slot + delta > %s) { return 0; }\n", ind, endExpr)
	if size > 0 {
		fmt.Fprintf(b, "%s        return count > (%s - (slot + delta)) / %d ? 0 : count;\n", ind, endExpr, size)
	} else {
		fmt.Fprintf(b, "%s        return count;\n", ind)
	}
	fmt.Fprintf(b, "%s    }\n%s}\n", ind, ind)
	fmt.Fprintf(b, "%sint _%sElements => (%s + %d) + checked((int)BinaryPrimitives.ReadInt64LittleEndian(%s.AsSpan(%s + %d, 8)));\n",
		ind, acc, originExpr, offset, bytesExpr, originExpr, offset)
	elem := *f
	elem.Array = ir.ArrayNone
	elem.ArrayBound = 0
	atExpr := fmt.Sprintf("_%sElements + index * %d", acc, size)
	switch {
	case isRowStructRef(f.Type):
		rowT := f.Type.Name + "Row"
		fmt.Fprintf(b, "%spublic %s %sAt(int index, %s into) { into.At = %s; return into; }\n", ind, rowT, acc, rowT, atExpr)
	case f.Type.Pointer || isRowUnionRef(f.Type):
		fmt.Fprintf(b, "%spublic int %sAt(int index) { return %s; }\n", ind, acc, atExpr)
	default:
		emitRowScalarMember(b, u, pkg, ownerName, &elem, true, fmt.Sprintf("%sAt(int index)", acc), bytesExpr, atExpr, ind)
	}
}

func emitCookMapRowAccessors(b *strings.Builder, u *ir.Unit, pkg string, f *ir.Field, offset int64, acc, bytesExpr, originExpr, endExpr, ind string) {
	entryRow := f.MapEntry.Name + "Row"
	el := ir.RecordLayout(u, f.MapEntry)
	key := ir.MapKeyField(f)
	keyOff := el.Fields[0].Offset
	fmt.Fprintf(b, "%spublic int %sCount\n%s{\n%s    get\n%s    {\n", ind, acc, ind, ind, ind)
	fmt.Fprintf(b, "%s        int slot = %s + %d;\n", ind, originExpr, offset)
	fmt.Fprintf(b, "%s        if (slot < 0 || slot > %s - 12) { return 0; }\n", ind, endExpr)
	fmt.Fprintf(b, "%s        long delta = BinaryPrimitives.ReadInt64LittleEndian(%s.AsSpan(slot, 8));\n", ind, bytesExpr)
	fmt.Fprintf(b, "%s        int count = BinaryPrimitives.ReadInt32LittleEndian(%s.AsSpan(slot + 8, 4));\n", ind, bytesExpr)
	fmt.Fprintf(b, "%s        if (count <= 0 || delta == 0 || slot + delta < 0 || slot + delta > %s) { return 0; }\n", ind, endExpr)
	fmt.Fprintf(b, "%s        return count > (%s - (slot + delta)) / %d ? 0 : count;\n", ind, endExpr, el.Size)
	fmt.Fprintf(b, "%s    }\n%s}\n", ind, ind)
	fmt.Fprintf(b, "%sint _%sEntries => (%s + %d) + checked((int)BinaryPrimitives.ReadInt64LittleEndian(%s.AsSpan(%s + %d, 8)));\n",
		ind, acc, originExpr, offset, bytesExpr, originExpr, offset)
	fmt.Fprintf(b, "%spublic %s %sAt(int index, %s into) { into.At = _%sEntries + index * %d; return into; }\n",
		ind, entryRow, acc, entryRow, acc, el.Size)

	keyParamT := rowScalarCsType(pkg, key.Type)
	if key.Type.Kind == ir.TString {
		keyParamT = "ReadOnlySpan<byte>"
	}
	fmt.Fprintf(b, "%spublic %s %sFind(%s key, %s into)\n%s{\n", ind, entryRow, acc, keyParamT, entryRow, ind)
	fmt.Fprintf(b, "%s    int count = %sCount;\n%s    int first = count > 0 ? _%sEntries : 0;\n%s    int lo = 0, hi = count;\n", ind, acc, ind, acc, ind)
	fmt.Fprintf(b, "%s    while (lo < hi)\n%s    {\n%s        int mid = lo + ((hi - lo) >> 1);\n%s        if (_%sOrder(first + mid * %d, key) < 0) { lo = mid + 1; } else { hi = mid; }\n%s    }\n",
		ind, ind, ind, ind, acc, el.Size, ind)
	fmt.Fprintf(b, "%s    if (lo == count || _%sOrder(first + lo * %d, key) != 0) { return null; }\n", ind, acc, el.Size)
	fmt.Fprintf(b, "%s    into.At = first + lo * %d;\n%s    return into;\n%s}\n", ind, el.Size, ind, ind)

	fmt.Fprintf(b, "%sint _%sOrder(int entry, %s key)\n%s{\n", ind, acc, keyParamT, ind)
	switch {
	case key.Type.Kind == ir.TString:
		if key.Type.TextUnbounded() {
			fmt.Fprintf(b, "%s    int data = Schema._CookTextStart(%s, %s, entry + %d);\n", ind, bytesExpr, endExpr, keyOff)
			fmt.Fprintf(b, "%s    int used = data < 0 ? 0 : (int)BinaryPrimitives.ReadUInt32LittleEndian(%s.AsSpan(data, 4));\n", ind, bytesExpr)
			fmt.Fprintf(b, "%s    return %s.AsSpan(data < 0 ? 0 : data + 8, used).SequenceCompareTo(key);\n", ind, bytesExpr)
		} else {
			pieces := ir.FieldPieces(u, key, keyOff)
			fmt.Fprintf(b, "%s    int used = BinaryPrimitives.ReadInt32LittleEndian(%s.AsSpan(entry + %d, 4));\n", ind, bytesExpr, pieces[1].Offset)
			fmt.Fprintf(b, "%s    if (used < 0 || used > %d) { used = 0; }\n", ind, key.Type.Size)
			fmt.Fprintf(b, "%s    return %s.AsSpan(entry + %d, used).SequenceCompareTo(key);\n", ind, bytesExpr, keyOff)
		}
	case key.Type.Width == 128:
		stored := rowReadExpr(pkg, key, bytesExpr, fmt.Sprintf("entry + %d", keyOff))
		if key.Type.Signed {
			fmt.Fprintf(b, "%s    return Schema.WideCompareSigned(%s, key);\n", ind, stored)
		} else {
			fmt.Fprintf(b, "%s    return Schema.WideCompareUnsigned(%s, key);\n", ind, stored)
		}
	default:
		stored := rowReadExpr(pkg, key, bytesExpr, fmt.Sprintf("entry + %d", keyOff))
		fmt.Fprintf(b, "%s    return (%s).CompareTo(key);\n", ind, stored)
	}
	fmt.Fprintf(b, "%s}\n", ind)
}

func emitRowCursorClass(b *strings.Builder, u *ir.Unit, pkg, record string, ml *ir.MemberLayout) {
	if ml == nil {
		return
	}
	fmt.Fprintf(b, "public sealed class %sRow\n{\n", record)
	b.WriteString("    public readonly byte[] Bytes;\n    public int At;\n    public readonly int RegionEnd;\n\n")
	fmt.Fprintf(b, "    public %sRow(byte[] bytes, int at, int regionEnd = -1)\n    {\n        Bytes = bytes;\n        At = at;\n        RegionEnd = regionEnd < 0 ? (bytes == null ? 0 : bytes.Length) : regionEnd;\n    }\n\n", record)
	fmt.Fprintf(b, "    public const int RowSize = %d;\n    public const int RowAlign = %d;\n\n", ml.Size, ml.Align)
	emitInlineRowAccessors(b, u, pkg, record, ml, nil, "Bytes", "At", "RegionEnd", "    ")
	b.WriteString("}\n\n")
}

func blockInfoSym(record string) string {
	if record == "" {
		return "Type"
	}
	return "Row" + record
}

func blockDescRecords(blocks *ir.BlockUnit, bl *ir.BlockLayout) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(name string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		ml := blocks.Layout(name)
		if ml == nil {
			return
		}
		seen[name] = true
		out = append(out, name)
		for _, fl := range ml.Fields {
			if isRowStructRef(fl.Field.Type) {
				walk(fl.Field.Type.Name)
			}
		}
	}
	for _, a := range bl.Arrays {
		walk(a.ElemName)
	}
	for _, fl := range bl.Projection.Fields {
		if !ir.BlockOutOfLine(fl.Field) && isRowStructRef(fl.Field.Type) {
			walk(fl.Field.Type.Name)
		}
	}
	sort.Strings(out)
	return out
}

func emitBlockRecordDesc(b *strings.Builder, u *ir.Unit, record string, ml *ir.MemberLayout, bl *ir.BlockLayout) {
	if ml == nil {
		return
	}
	sym := blockInfoSym(record)
	name := record
	if bl != nil {
		name = bl.Table.Name
	}
	fmt.Fprintf(b, "    public static readonly Schema.TableBlockInfo %s = new Schema.TableBlockInfo\n    {\n", sym)
	fmt.Fprintf(b, "        Name = %q,\n        BuildVersion = Schema.BuildVersion,\n        Size = %d,\n        Align = %d,\n        NumFields = %d,\n",
		name, ml.Size, ml.Align, len(ml.Fields))
	if len(ml.Fields) == 0 {
		b.WriteString("        Fields = Array.Empty<Schema.TableBlockFieldInfo>()\n    };\n\n")
		return
	}
	b.WriteString("        Fields = new Schema.TableBlockFieldInfo[]\n        {\n")
	for _, fl := range ml.Fields {
		f := fl.Field
		kind := tableScalarKind(f)
		if f.Type.Kind == ir.TBytes {
			kind = tkU8
		}
		facts := ir.BlockFieldOf(u, f, fl.Offset, bl != nil)
		if bl != nil {
			if a := bl.ArrayByName(f.Name); a != nil {
				fmt.Fprintf(b, "            new Schema.TableBlockFieldInfo { Name = %q, Offset = %d, Size = %d, Kind = %d, OutOfLine = true, OffsetOfOffset = %d, CountOffset = %d, StrideOffset = %d, Stride = %d, IsArray = true, Counted = true, Optional = false, ArrayBound = %d, ElemSize = 0, PresentOffset = -1, ElementRef = delegate { return %s; } },\n",
					f.Name, fl.Offset, fl.Size, kind, a.OffsetOfOffset, a.CountOffset, a.StrideOffset, a.Stride, a.Max, blockInfoSym(a.ElemName))
				continue
			}
		}
		elemRef := "null"
		if isRowStructRef(f.Type) {
			elemRef = fmt.Sprintf("delegate { return %s; }", blockInfoSym(f.Type.Name))
		}
		fmt.Fprintf(b, "            new Schema.TableBlockFieldInfo { Name = %q, Offset = %d, Size = %d, Kind = %d, OutOfLine = false, OffsetOfOffset = -1, CountOffset = %d, StrideOffset = -1, Stride = 0, IsArray = %v, Counted = %v, Optional = %v, ArrayBound = %d, ElemSize = %d, PresentOffset = %d, ElementRef = %s },\n",
			f.Name, fl.Offset, fl.Size, kind, facts.CountOffset, facts.IsArray, facts.Counted, facts.Optional, facts.ArrayBound, facts.ElemSize, facts.PresentOffset, elemRef)
	}
	b.WriteString("        }\n    };\n\n")
}

func emitBlockClass(b *strings.Builder, u *ir.Unit, pkg string, blocks *ir.BlockUnit, bl *ir.BlockLayout) {
	name := bl.Table.Name
	fmt.Fprintf(b, "public sealed class %sBlock\n{\n", name)
	b.WriteString("    public readonly byte[] Bytes;\n    public readonly int Base;\n    public readonly int Length;\n\n")
	fmt.Fprintf(b, "    private %sBlock(byte[] bytes, int baseOffset, int length)\n    {\n        Bytes = bytes;\n        Base = baseOffset;\n        Length = length;\n    }\n\n", name)
	fmt.Fprintf(b, "    public const int BlockMaxBytes = %d;\n    public const int ProjectionSize = %d;\n\n", bl.MaxBytes, bl.Projection.Size)
	emitInlineRowAccessors(b, u, pkg, name, &bl.Projection, bl, "Bytes", "Base", "(Base + Length)", "    ")

	for _, a := range bl.Arrays {
		acc := rowAccessorBase(a.Field)
		rowT := a.ElemName + "Row"
		fmt.Fprintf(b, "\n    public const int %sStride = %d;\n    public const int %sMax = %d;\n    public const int %sProjectionOffset = %d;\n",
			acc, a.Stride, acc, a.Max, acc, a.TripleOffset)
		fmt.Fprintf(b, "    public int %sOffsetOf => checked((int)BinaryPrimitives.ReadUInt64LittleEndian(Bytes.AsSpan(Base + %d, 8)));\n", acc, a.OffsetOfOffset)
		fmt.Fprintf(b, "    public int %sCount => checked((int)BinaryPrimitives.ReadUInt32LittleEndian(Bytes.AsSpan(Base + %d, 4)));\n", acc, a.CountOffset)
		fmt.Fprintf(b, "    public int %sPitch => checked((int)BinaryPrimitives.ReadUInt32LittleEndian(Bytes.AsSpan(Base + %d, 4)));\n", acc, a.StrideOffset)
		fmt.Fprintf(b, "    public %s %sCursor() { return new %s(Bytes, Base, Base + Length); }\n", rowT, acc, rowT)
		fmt.Fprintf(b, "    public %s %sAt(int index, %s into) { into.At = Base + %sOffsetOf + index * %sPitch; return into; }\n", rowT, acc, rowT, acc, acc)
		fmt.Fprintf(b, "    public ReadOnlySpan<byte> %sBytes() { return Bytes.AsSpan(Base + %sOffsetOf, %sCount * %sPitch); }\n", acc, acc, acc, acc)
	}

	// Begin (block-build)
	var params []string
	params = append(params, "byte[] bytes", "int baseOffset")
	for _, a := range bl.Arrays {
		params = append(params, fmt.Sprintf("int %sCount", ir.GoExportName(a.Field.Name)))
	}
	params = append(params, "Schema.TableBlockRefusal refusal = null")
	fmt.Fprintf(b, "\n    public static %sBlock Begin(%s)\n    {\n", name, strings.Join(params, ", "))
	b.WriteString("        if (refusal != null) { refusal.Array = \"\"; refusal.Count = 0; refusal.Maximum = 0; }\n")
	for _, a := range bl.Arrays {
		pName := ir.GoExportName(a.Field.Name) + "Count"
		fmt.Fprintf(b, "        if (%s < 0 || %s > %d)\n        {\n            if (refusal != null) { refusal.Array = %q; refusal.Count = %s; refusal.Maximum = %d; }\n            return null;\n        }\n",
			pName, pName, a.Max, a.Field.Name, pName, a.Max)
	}
	fmt.Fprintf(b, "        if (bytes == null || baseOffset < 0 || baseOffset %% %d != 0) { return null; }\n", ir.BlockAlign)
	b.WriteString("        if (bytes.Length - baseOffset < BlockMaxBytes) { return null; }\n")
	b.WriteString("        BinaryPrimitives.WriteUInt64LittleEndian(bytes.AsSpan(baseOffset, 8), Schema.TableBlockMagic);\n")
	b.WriteString("        BinaryPrimitives.WriteUInt64LittleEndian(bytes.AsSpan(baseOffset + 8, 8), Schema.BuildVersion);\n")
	b.WriteString("        BinaryPrimitives.WriteUInt64LittleEndian(bytes.AsSpan(baseOffset + 16, 8), Schema.TableBlockByteOrder);\n")
	b.WriteString("        long offset = ProjectionSize;\n")
	for _, a := range bl.Arrays {
		pName := ir.GoExportName(a.Field.Name) + "Count"
		align := max(a.ElemAlign(), int64(ir.BlockAlign))
		fmt.Fprintf(b, "        offset = (offset + %dL) & ~%dL;\n", align-1, align-1)
		fmt.Fprintf(b, "        BinaryPrimitives.WriteUInt64LittleEndian(bytes.AsSpan(baseOffset + %d, 8), (ulong)offset);\n", a.OffsetOfOffset)
		fmt.Fprintf(b, "        BinaryPrimitives.WriteUInt32LittleEndian(bytes.AsSpan(baseOffset + %d, 4), (uint)%s);\n", a.CountOffset, pName)
		fmt.Fprintf(b, "        BinaryPrimitives.WriteUInt32LittleEndian(bytes.AsSpan(baseOffset + %d, 4), %du);\n", a.StrideOffset, a.Stride)
		fmt.Fprintf(b, "        offset += (long)%s * %dL;\n", pName, a.Stride)
	}
	fmt.Fprintf(b, "        int used = checked((int)((offset + %dL) & ~%dL));\n", ir.BlockAlign-1, ir.BlockAlign-1)
	fmt.Fprintf(b, "        return new %sBlock(bytes, baseOffset, used);\n    }\n\n", name)

	// Open & OpenWithReason
	fmt.Fprintf(b, "    public static bool Open(out %sBlock block, byte[] bytes, int baseOffset, long extent)\n    {\n        return Open(out block, bytes, baseOffset, extent, out _);\n    }\n\n", name)
	fmt.Fprintf(b, "    public static bool Open(out %sBlock block, byte[] bytes, int baseOffset, long extent, out Schema.TableRefuseReason reason)\n    {\n        block = null;\n", name)
	b.WriteString("        if (bytes == null || baseOffset < 0) { reason = Schema.TableRefuseReason.unaligned_base; return false; }\n")
	fmt.Fprintf(b, "        if (extent < %d || extent > bytes.LongLength - baseOffset) { reason = Schema.TableRefuseReason.truncated; return false; }\n", bl.Projection.Size)
	b.WriteString("        ulong magic = Schema.TableBlockRead64(bytes, baseOffset);\n")
	fmt.Fprintf(b, "        if (magic != Schema.TableBlockMagic) { reason = magic == 0x%016xUL ? Schema.TableRefuseReason.foreign_order : Schema.TableRefuseReason.not_a_cook; return false; }\n", bits.ReverseBytes64(ir.BlockMagic))
	b.WriteString("        if (Schema.TableBlockRead64(bytes, baseOffset + 16) != Schema.TableBlockByteOrder) { reason = Schema.TableRefuseReason.not_a_cook; return false; }\n")
	b.WriteString("        if (Schema.TableBlockRead64(bytes, baseOffset + 8) != Schema.BuildVersion) { reason = Schema.TableRefuseReason.wrong_build_version; return false; }\n")
	fmt.Fprintf(b, "        long used = %dL;\n", bl.Projection.Size)
	for _, a := range bl.Arrays {
		acc := rowAccessorBase(a.Field)
		align := max(a.ElemAlign(), int64(ir.BlockAlign))
		b.WriteString("        {\n")
		fmt.Fprintf(b, "            ulong offsetOf = BinaryPrimitives.ReadUInt64LittleEndian(bytes.AsSpan(baseOffset + %d, 8));\n", a.OffsetOfOffset)
		fmt.Fprintf(b, "            uint count = BinaryPrimitives.ReadUInt32LittleEndian(bytes.AsSpan(baseOffset + %d, 4));\n", a.CountOffset)
		fmt.Fprintf(b, "            uint stride = BinaryPrimitives.ReadUInt32LittleEndian(bytes.AsSpan(baseOffset + %d, 4));\n", a.StrideOffset)
		fmt.Fprintf(b, "            if (stride != %sStride || count > %sMax) { reason = Schema.TableRefuseReason.bad_layout; return false; }\n", acc, acc)
		fmt.Fprintf(b, "            if (offsetOf < %dUL || offsetOf %% %dUL != 0 || offsetOf > (ulong)extent) { reason = Schema.TableRefuseReason.bad_layout; return false; }\n", bl.Projection.Size, align)
		b.WriteString("            long rows = (long)count * stride;\n")
		b.WriteString("            if (rows > extent - (long)offsetOf) { reason = Schema.TableRefuseReason.bad_layout; return false; }\n")
		b.WriteString("            long end = (long)offsetOf + rows;\n            if (end > used) { used = end; }\n        }\n")
	}
	fmt.Fprintf(b, "        long padding = (%dL - (used %% %dL)) %% %dL;\n", ir.BlockAlign, ir.BlockAlign, ir.BlockAlign)
	b.WriteString("        if (padding > extent - used) { reason = Schema.TableRefuseReason.truncated; return false; }\n")
	b.WriteString("        used += padding;\n")
	fmt.Fprintf(b, "        if (baseOffset %% %d != 0) { reason = Schema.TableRefuseReason.unaligned_base; return false; }\n", ir.BlockAlign)
	b.WriteString("        reason = Schema.TableRefuseReason.ok;\n")
	fmt.Fprintf(b, "        block = new %sBlock(bytes, baseOffset, checked((int)used));\n        return true;\n    }\n\n", name)

	for _, r := range blockDescRecords(blocks, bl) {
		emitBlockRecordDesc(b, u, r, blocks.Layout(r), nil)
	}
	emitBlockRecordDesc(b, u, "", &bl.Projection, bl)
	b.WriteString("}\n\n")
}

type unityCookUnit struct {
	tables  []*ir.Struct
	members map[string]*ir.MemberLayout
	order   []string
	skipped map[string]string
}

func cookMemberStruct(u *ir.Unit, name string) *ir.Struct {
	if st := u.Tables[name]; st != nil {
		return st
	}
	return u.Structs[name]
}

func cookDeclFileBase(u *ir.Unit, record string) string {
	for range 64 {
		if base := u.DeclFile[record]; base != "" {
			return base
		}
		st := cookMemberStruct(u, record)
		if st == nil || st.MapEntryOf == "" {
			return ""
		}
		record, _, _ = strings.Cut(st.MapEntryOf, ".")
	}
	return ""
}

func cookHasUnionClosure(u *ir.Unit, st *ir.Struct) string {
	seen := map[string]bool{}
	var walk func(string) string
	walk = func(name string) string {
		if seen[name] {
			return ""
		}
		seen[name] = true
		m := cookMemberStruct(u, name)
		if m == nil {
			return ""
		}
		for _, f := range m.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Union:
				return "closure holds union " + ref.Name
			case *ir.Struct:
				if why := walk(ref.Name); why != "" {
					return why
				}
			}
		}
		return ""
	}
	return walk(st.Name)
}

func cookWalkRecords(u *ir.Unit, root string, visit func(string, *ir.MemberLayout)) {
	seen := map[string]bool{}
	var walk func(string)
	walk = func(n string) {
		if seen[n] {
			return
		}
		st := cookMemberStruct(u, n)
		if st == nil {
			return
		}
		seen[n] = true
		visit(n, ir.RecordLayout(u, st))
		for _, f := range st.Fields {
			if f.Type.Kind == ir.TNamed {
				if ref, ok := f.Type.Ref.(*ir.Struct); ok {
					walk(ref.Name)
				}
			}
		}
	}
	walk(root)
}

func buildUnityCookUnit(u *ir.Unit) *unityCookUnit {
	c := &unityCookUnit{members: map[string]*ir.MemberLayout{}, skipped: map[string]string{}}
	for _, name := range sortedMapKeys(u.Tables) {
		st := u.Tables[name]
		if st.MapEntryOf != "" || !st.CarriesForm("cook") {
			continue
		}
		if why := cookHasUnionClosure(u, st); why != "" {
			c.skipped[name] = why
			continue
		}
		c.tables = append(c.tables, st)
	}
	for _, st := range c.tables {
		cookWalkRecords(u, st.Name, func(name string, ml *ir.MemberLayout) {
			c.members[name] = ml
		})
	}
	for grew := true; grew; {
		grew = false
		for _, name := range sortedMapKeys(c.members) {
			st := cookMemberStruct(u, name)
			if st == nil {
				continue
			}
			for _, f := range st.Fields {
				if !f.IsMap() || c.members[f.MapEntry.Name] != nil {
					continue
				}
				if cookHasUnionClosure(u, f.MapEntry) != "" {
					continue
				}
				cookWalkRecords(u, f.MapEntry.Name, func(n string, ml *ir.MemberLayout) {
					c.members[n] = ml
				})
				grew = true
			}
		}
	}
	for name := range c.members {
		c.order = append(c.order, name)
	}
	sort.Strings(c.order)
	return c
}

func (c *unityCookUnit) opens(name string) bool {
	for _, st := range c.tables {
		if st.Name == name {
			return true
		}
	}
	return false
}

func cookStorageKindCs(f *ir.Field) string {
	if f.Type.Pointer || f.Type.TextUnbounded() {
		return "Reference"
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "Boolean"
	case ir.TFloat32, ir.TFloat64:
		return "Float"
	case ir.TString:
		return "String"
	case ir.TBytes:
		return "Bytes"
	case ir.TBits:
		return "Unsigned"
	case ir.TInt, ir.TFixed:
		if f.Type.Signed {
			return "Signed"
		}
		return "Unsigned"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return "Unsigned"
		case *ir.Struct:
			return "Record"
		}
	}
	return "Record"
}

func cookInfoSym(owner, record string) string {
	if owner == record {
		return "Type"
	}
	return "Record" + record
}

func emitCookClass(b *strings.Builder, u *ir.Unit, c *unityCookUnit, st *ir.Struct, ml *ir.MemberLayout) {
	name := st.Name
	rowT := name + "Row"
	fmt.Fprintf(b, "public sealed class %sCook\n{\n", name)
	b.WriteString("    public readonly byte[] Bytes;\n    public readonly int Region;\n    public readonly int Length;\n\n")
	b.WriteString("    public const int RefNone = -1;\n    public const int RefOutside = -2;\n\n")
	fmt.Fprintf(b, "    private %sCook(byte[] bytes, int region, int length)\n    {\n        Bytes = bytes;\n        Region = region;\n        Length = length;\n    }\n\n", name)
	fmt.Fprintf(b, "    public %s Cursor() { return new %s(Bytes, Region, Region + Length); }\n", rowT, rowT)
	fmt.Fprintf(b, "    public %s Root(%s into) { into.At = Region; return into; }\n\n", rowT, rowT)

	fmt.Fprintf(b, "    public static bool Open(out %sCook cook, byte[] bytes, int at, long length)\n    {\n        return Open(out cook, bytes, at, length, out _);\n    }\n\n", name)
	fmt.Fprintf(b, "    public static bool Open(out %sCook cook, byte[] bytes, int at, long length, out Schema.TableRefuseReason reason)\n    {\n        cook = null;\n", name)
	b.WriteString("        if (bytes == null || at < 0) { reason = Schema.TableRefuseReason.unaligned_base; return false; }\n")
	fmt.Fprintf(b, "        if (length < %dL || length > bytes.LongLength - at) { reason = Schema.TableRefuseReason.truncated; return false; }\n", cookHeaderBytes)
	b.WriteString("        ulong magic = Schema.TableCookRead64(bytes, at);\n")
	fmt.Fprintf(b, "        if (magic != Schema.TableCookMagic) { reason = magic == 0x%016xUL ? Schema.TableRefuseReason.foreign_order : Schema.TableRefuseReason.not_a_cook; return false; }\n", bits.ReverseBytes64(cookMagicWord))
	b.WriteString("        if (Schema.TableCookRead64(bytes, at + 16) != Schema.TableCookByteOrder) { reason = Schema.TableRefuseReason.not_a_cook; return false; }\n")
	b.WriteString("        if (Schema.TableCookRead64(bytes, at + 8) != Schema.BuildVersion) { reason = Schema.TableRefuseReason.wrong_build_version; return false; }\n")
	b.WriteString("        if (Schema.TableCookRead64(bytes, at + 48) != 0 || Schema.TableCookRead64(bytes, at + 56) != 0) { reason = Schema.TableRefuseReason.reserved_not_zero; return false; }\n")
	b.WriteString("        ulong alignment = Schema.TableCookRead64(bytes, at + 40);\n")
	fmt.Fprintf(b, "        if (alignment < %dUL || alignment > %dUL || (alignment & (alignment - 1UL)) != 0 || alignment %% %dUL != 0) { reason = Schema.TableRefuseReason.bad_alignment; return false; }\n",
		ir.RegionAlignFloor, cookMaxAlign, ml.Align)
	fmt.Fprintf(b, "        long align = (long)alignment;\n        long dataOffset = (%dL + align - 1L) & ~(align - 1L);\n", cookHeaderBytes)
	b.WriteString("        long dataLength = unchecked((long)Schema.TableCookRead64(bytes, at + 24));\n")
	b.WriteString("        long attribution = unchecked((long)Schema.TableCookRead64(bytes, at + 32));\n")
	fmt.Fprintf(b, "        if (dataLength < 0 || attribution < 0 || dataOffset > length || dataLength > length - dataOffset || attribution != length - dataOffset - dataLength || dataLength < %dL || dataLength > int.MaxValue - at - dataOffset) { reason = Schema.TableRefuseReason.truncated; return false; }\n", ml.Size)
	b.WriteString("        if (at % align != 0) { reason = Schema.TableRefuseReason.unaligned_base; return false; }\n")
	b.WriteString("        reason = Schema.TableRefuseReason.ok;\n")
	fmt.Fprintf(b, "        cook = new %sCook(bytes, checked((int)(at + dataOffset)), checked((int)dataLength));\n        return true;\n    }\n\n", name)

	b.WriteString("    public int At(int slot, int size)\n    {\n")
	b.WriteString("        if (size < 0 || Length < 8 || slot < Region || slot - Region > Length - 8) { return RefOutside; }\n")
	b.WriteString("        long delta = BinaryPrimitives.ReadInt64LittleEndian(Bytes.AsSpan(slot, 8));\n")
	b.WriteString("        if (delta == 0) { return RefNone; }\n")
	b.WriteString("        long relative = slot - Region;\n")
	b.WriteString("        if (delta < -relative || delta > (long)Length - size - relative) { return RefOutside; }\n")
	b.WriteString("        return checked((int)(slot + delta));\n    }\n\n")

	var records []string
	cookWalkRecords(u, st.Name, func(n string, _ *ir.MemberLayout) { records = append(records, n) })
	sort.Strings(records)
	for _, r := range records {
		rml := c.members[r]
		if rml == nil {
			continue
		}
		sym := cookInfoSym(name, r)
		fmt.Fprintf(b, "    public static readonly Schema.TableCookInfo %s = new Schema.TableCookInfo\n    {\n        Name = %q,\n        Size = %d,\n        Align = %d,\n        NumFields = %d,\n",
			sym, r, rml.Size, rml.Align, len(rml.Fields))
		if len(rml.Fields) == 0 {
			b.WriteString("        Fields = Array.Empty<Schema.TableCookFieldInfo>()\n    };\n\n")
			continue
		}
		b.WriteString("        Fields = new Schema.TableCookFieldInfo[]\n        {\n")
		for _, fl := range rml.Fields {
			f := fl.Field
			recRef := "null"
			if f.Type.Kind == ir.TNamed {
				if ref, ok := f.Type.Ref.(*ir.Struct); ok && c.members[ref.Name] != nil {
					recRef = fmt.Sprintf("delegate { return %s; }", cookInfoSym(name, ref.Name))
				}
			}
			countOff := int64(-1)
			if f.Array == ir.ArrayCounted {
				pieces := ir.FieldPieces(u, f, fl.Offset)
				countOff = pieces[len(pieces)-1].Offset
				if f.Type.Optional {
					countOff = pieces[len(pieces)-2].Offset
				}
			}
			if (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && !f.Type.Pointer && !f.Type.TextUnbounded() {
				pieces := ir.FieldPieces(u, f, fl.Offset)
				countOff = pieces[1].Offset
			}
			isArr := f.KeyEnum != "" || f.Array != ir.ArrayNone
			bound := int64(1)
			elemSize := fl.Size
			if isArr {
				bound = f.ArrayBound
				pieces := ir.FieldPieces(u, f, fl.Offset)
				if bound > 0 {
					elemSize = pieces[0].Size / bound
				} else {
					elemSize = pieces[0].Size
					isArr = false
				}
			}
			if (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && !f.Type.Pointer && !f.Type.TextUnbounded() {
				bound = f.Type.Size
				isArr = false
			}
			presOff := int64(-1)
			if f.Type.Optional && !f.Type.Pointer && !f.Type.TextUnbounded() {
				pieces := ir.FieldPieces(u, f, fl.Offset)
				presOff = pieces[len(pieces)-1].Offset
			}
			fmt.Fprintf(b, "            new Schema.TableCookFieldInfo { Name = %q, Offset = %d, Size = %d, ElemSize = %d, IsArray = %v, ArrayBound = %d, IsPointer = %v, Unbounded = %v, CountOffset = %d, PresentOffset = %d, Storage = Schema.TableCookStorage.%s, RecordRef = %s },\n",
				f.Name, fl.Offset, fl.Size, elemSize, isArr, bound, f.Type.Pointer, f.Type.TextUnbounded(), countOff, presOff, cookStorageKindCs(f), recRef)
		}
		b.WriteString("        }\n    };\n\n")
	}
	b.WriteString("}\n\n")
}

func emitBlockAndCookFiles(u *ir.Unit) map[string][]byte {
	out := map[string][]byte{}
	if len(u.Tables) == 0 {
		return out
	}
	pkg := namespaceOf(u.Package)
	blocks := ir.Blocks(u)
	cooks := buildUnityCookUnit(u)
	usings := []string{"System", "System.Buffers.Binary", "Serialize"}
	seenBlockRec := map[string]bool{}
	seenBlockTbl := map[string]bool{}
	seenCookRec := map[string]bool{}
	seenCookTbl := map[string]bool{}

	for _, f := range u.Files {
		var blockTypes strings.Builder
		for _, rec := range blocks.Order {
			if u.DeclFile[rec] != f.Base {
				continue
			}
			if ml := blocks.Layout(rec); ml != nil {
				seenBlockRec[rec] = true
				emitRowCursorClass(&blockTypes, u, pkg, rec, ml)
			}
		}
		for _, t := range f.Tables {
			if bl := blocks.Block(t.Name); bl != nil {
				seenBlockTbl[t.Name] = true
				emitBlockClass(&blockTypes, u, pkg, blocks, bl)
			}
		}
		if blockTypes.Len() > 0 {
			out[f.Base+".Block.cs"] = []byte(header(u, f.Base+".schema", usings...) + wrap(u, blockTypes.String(), ""))
		}

		var cookTypes strings.Builder
		for _, rec := range cooks.order {
			if cookDeclFileBase(u, rec) != f.Base || blocks.Layout(rec) != nil {
				continue
			}
			seenCookRec[rec] = true
			emitRowCursorClass(&cookTypes, u, pkg, rec, cooks.members[rec])
		}
		for _, t := range f.Tables {
			if cooks.opens(t.Name) {
				seenCookTbl[t.Name] = true
				emitCookClass(&cookTypes, u, cooks, t, cooks.members[t.Name])
			}
		}
		if cookTypes.Len() > 0 {
			out[f.Base+".Cook.cs"] = []byte(header(u, f.Base+".schema", usings...) + wrap(u, cookTypes.String(), ""))
		}
	}

	var orphanBlocks strings.Builder
	for _, rec := range blocks.Order {
		if !seenBlockRec[rec] {
			if ml := blocks.Layout(rec); ml != nil {
				seenBlockRec[rec] = true
				emitRowCursorClass(&orphanBlocks, u, pkg, rec, ml)
			}
		}
	}
	for _, name := range sortedMapKeys(u.Tables) {
		if !seenBlockTbl[name] {
			if bl := blocks.Block(name); bl != nil {
				emitBlockClass(&orphanBlocks, u, pkg, blocks, bl)
			}
		}
	}
	if orphanBlocks.Len() > 0 {
		out["Tables.Block.cs"] = []byte(header(u, "the unit's blocks", usings...) + wrap(u, orphanBlocks.String(), ""))
	}

	var orphanCooks strings.Builder
	for _, rec := range cooks.order {
		if !seenCookRec[rec] && blocks.Layout(rec) == nil {
			emitRowCursorClass(&orphanCooks, u, pkg, rec, cooks.members[rec])
		}
	}
	for _, name := range sortedMapKeys(u.Tables) {
		if !seenCookTbl[name] && cooks.opens(name) {
			emitCookClass(&orphanCooks, u, cooks, u.Tables[name], cooks.members[name])
		}
	}
	if orphanCooks.Len() > 0 {
		out["Tables.Cook.cs"] = []byte(header(u, "the unit's cooks", usings...) + wrap(u, orphanCooks.String(), ""))
	}
	return out
}
