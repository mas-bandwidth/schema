package unitytable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func carrierElemType(pkg string, f *ir.Field) string {
	if f.IsMap() {
		return "__Carrier_" + f.MapEntry.Name
	}
	if f.Type.Blob() {
		return "TableBlob"
	}
	if f.Type.Kind == ir.TNamed {
		switch f.Type.Ref.(type) {
		case *ir.Struct, *ir.Union:
			return "__Carrier_" + f.Type.Name
		default:
			return "global::" + pkg + "." + f.Type.Name
		}
	}
	return scalarStorageType(f.Type)
}

func isCarrierClassElem(f *ir.Field) bool {
	if f.IsMap() {
		return true
	}
	if f.Type.Pointer || f.Type.Kind != ir.TNamed {
		return false
	}
	switch f.Type.Ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}

func emitCarrierFieldDecl(b *strings.Builder, pkg string, f *ir.Field, m string) {
	elemT := carrierElemType(pkg, f)
	switch {
	case f.IsList(), f.IsMap():
		fmt.Fprintf(b, "    public %s[] %s = global::System.Array.Empty<%s>();\n    public int %sCount;\n", elemT, m, elemT, m)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		fmt.Fprintf(b, "    public %s %s;\n", elemT, m)
	case f.Type.TextUnbounded():
		fmt.Fprintf(b, "    public byte[] %s = global::System.Array.Empty<byte>();\n    public int %sLength;\n", m, m)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		fmt.Fprintf(b, "    public byte[] %s = new byte[%d];\n    public int %sLength;\n", m, f.Type.Size, m)
	case f.Array == ir.ArrayFixed:
		fmt.Fprintf(b, "    public %s[] %s = new %s[%d];\n", elemT, m, elemT, f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		fmt.Fprintf(b, "    public %s[] %s = new %s[%d];\n", elemT, m, elemT, f.ArrayBound)
		if f.BornCount() > 0 && !f.Type.Optional {
			fmt.Fprintf(b, "    public int %sCount = %d;\n", m, f.BornCount())
		} else {
			fmt.Fprintf(b, "    public int %sCount;\n", m)
		}
	default:
		init := ""
		if isCarrierClassElem(f) {
			init = " = new " + elemT + "()"
		} else if f.HasDefault || (f.Type.Kind == ir.TNamed && isEnumRef(f.Type)) {
			init = " = " + scalarDefaultExprQual(pkg, f)
		}
		fmt.Fprintf(b, "    public %s %s%s;\n", elemT, m, init)
	}
	if f.Type.Optional {
		fmt.Fprintf(b, "    public bool %sPresent;\n", m)
	}
}

func emitCarrierCtor(b *strings.Builder, pkg, carrierName string, fields []*ir.Field, names []string) {
	var body strings.Builder
	for idx, f := range fields {
		m := names[idx]
		elemT := carrierElemType(pkg, f)
		if (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && !f.Type.Pointer && !f.Type.TextUnbounded() && len(f.DefBytes) > 0 && !f.Type.Optional {
			for i, by := range f.DefBytes {
				fmt.Fprintf(&body, "        %s[%d] = 0x%02x;\n", m, i, by)
			}
			fmt.Fprintf(&body, "        %sLength = %d;\n", m, len(f.DefBytes))
		}
		if f.Array != ir.ArrayNone && !f.IsList() && !f.IsMap() && isCarrierClassElem(f) {
			fmt.Fprintf(&body, "        for (int i = 0; i < %s.Length; i++) { %s[i] = new %s(); }\n", m, m, elemT)
		}
	}
	if body.Len() == 0 {
		return
	}
	fmt.Fprintf(b, "    public %s()\n    {\n%s    }\n", carrierName, body.String())
}

func emitCarrierResetField(b *strings.Builder, pkg string, f *ir.Field, m, ind string) {
	switch {
	case f.IsMap(), f.IsList():
		fmt.Fprintf(b, "%sglobal::System.Array.Clear(value.%s, 0, value.%s.Length); value.%sCount = 0;\n", ind, m, m, m)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		fmt.Fprintf(b, "%svalue.%s = null;\n", ind, m)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		fmt.Fprintf(b, "%sglobal::System.Array.Clear(value.%s, 0, value.%s.Length);\n%svalue.%sLength = 0;\n", ind, m, m, ind, m)
		if !f.Type.TextUnbounded() && len(f.DefBytes) > 0 && !f.Type.Optional {
			for i, by := range f.DefBytes {
				fmt.Fprintf(b, "%svalue.%s[%d] = 0x%02x;\n", ind, m, i, by)
			}
			fmt.Fprintf(b, "%svalue.%sLength = %d;\n", ind, m, len(f.DefBytes))
		}
	case f.Array != ir.ArrayNone && isCarrierClassElem(f):
		fmt.Fprintf(b, "%sfor (int i = 0; i < value.%s.Length; i++) { TableReset(value.%s[i]); }\n", ind, m, m)
		if f.Array == ir.ArrayCounted {
			cnt := int64(0)
			if !f.Type.Optional {
				cnt = f.BornCount()
			}
			fmt.Fprintf(b, "%svalue.%sCount = %d;\n", ind, m, cnt)
		}
	case f.Array != ir.ArrayNone:
		fmt.Fprintf(b, "%sglobal::System.Array.Clear(value.%s, 0, value.%s.Length);\n", ind, m, m)
		if f.Array == ir.ArrayCounted {
			cnt := int64(0)
			if !f.Type.Optional {
				cnt = f.BornCount()
			}
			fmt.Fprintf(b, "%svalue.%sCount = %d;\n", ind, m, cnt)
		}
	default:
		if isCarrierClassElem(f) {
			fmt.Fprintf(b, "%sTableReset(value.%s);\n", ind, m)
		} else {
			fmt.Fprintf(b, "%svalue.%s = %s;\n", ind, m, scalarDefaultExprQual(pkg, f))
		}
	}
	if f.Type.Optional {
		fmt.Fprintf(b, "%svalue.%sPresent = false;\n", ind, m)
	}
}

func emitCarrierStruct(b *strings.Builder, u *ir.Unit, st *ir.Struct) {
	pkg := namespaceOf(u.Package)
	cname := "__Carrier_" + st.Name
	fmt.Fprintf(b, "internal sealed class %s\n{\n", cname)
	names := make([]string, len(st.Fields))
	for i, f := range st.Fields {
		names[i] = member(f)
		emitCarrierFieldDecl(b, pkg, f, names[i])
	}
	emitCarrierCtor(b, pkg, cname, st.Fields, names)
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "internal static void TableReset(%s value)\n{\n", cname)
	for i, f := range st.Fields {
		emitCarrierResetField(b, pkg, f, names[i], "    ")
	}
	b.WriteString("}\n\n")

	emitStructPullPush(b, u, pkg, st)
}

func emitCarrierUnion(b *strings.Builder, u *ir.Unit, un *ir.Union) {
	pkg := namespaceOf(u.Package)
	cname := "__Carrier_" + un.Name
	fmt.Fprintf(b, "internal sealed class %s\n{\n    public global::%s.%sType Type;\n", cname, pkg, un.Name)
	var fields []*ir.Field
	var names []string
	for _, v := range un.Variants {
		if v.Void() {
			continue
		}
		armName := ir.GoExportName(v.Name)
		fields = append(fields, v.F)
		names = append(names, armName)
		emitCarrierFieldDecl(b, pkg, v.F, armName)
	}
	emitCarrierCtor(b, pkg, cname, fields, names)
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "internal static void TableReset(%s value)\n{\n    value.Type = global::%s.%sType.None;\n", cname, pkg, un.Name)
	for i, f := range fields {
		emitCarrierResetField(b, pkg, f, names[i], "    ")
	}
	b.WriteString("}\n\n")

	emitUnionPullPush(b, u, pkg, un, fields, names)
}

func emitPullElement(b *strings.Builder, u *ir.Unit, pkg string, f *ir.Field, srcExpr, dstExpr, ind string) {
	switch {
	case f.IsMap():
		eName := f.MapEntry.Name
		fmt.Fprintf(b, "%s%s = %s == null ? new __Carrier_%s() : _Pull_%s(%s, map, oldMap, oldOwners);\n", ind, dstExpr, srcExpr, eName, eName, srcExpr)
	case f.Type.Blob():
		fmt.Fprintf(b, "%sif (%s == null) { %s = null; } else { if (!map.TryGetValue(%s, out object _cb)) { _cb = new TableBlob(%s.Data ?? global::System.Array.Empty<byte>()); map[%s] = _cb; } %s = (TableBlob)_cb; }\n",
			ind, srcExpr, dstExpr, srcExpr, srcExpr, srcExpr, dstExpr)
	case f.Type.Pointer:
		tName := f.Type.Name
		tSt := f.Type.Ref.(*ir.Struct)
		if isFixedStruct(u, tSt) {
			fmt.Fprintf(b, "%sif (%s == null) { %s = null; } else { if (!map.TryGetValue(%s, out object _cn)) { var _nn = new __Carrier_%s(); map[%s] = _nn; _RecordOldOwner(%s, _nn, oldMap, oldOwners); _PullInto_%s(ref %s.Value, _nn, map, oldMap, oldOwners); _cn = _nn; } %s = (__Carrier_%s)_cn; }\n",
				ind, srcExpr, dstExpr, srcExpr, tName, srcExpr, srcExpr, tName, srcExpr, dstExpr, tName)
		} else {
			fmt.Fprintf(b, "%sif (%s == null) { %s = null; } else { if (!map.TryGetValue(%s, out object _cn)) { var _nn = new __Carrier_%s(); map[%s] = _nn; _RecordOldOwner(%s, _nn, oldMap, oldOwners); _PullInto_%s(%s, _nn, map, oldMap, oldOwners); _cn = _nn; } %s = (__Carrier_%s)_cn; }\n",
				ind, srcExpr, dstExpr, srcExpr, tName, srcExpr, srcExpr, tName, srcExpr, dstExpr, tName)
		}
	case f.Type.Kind == ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			if isFixedStruct(u, ref) {
				fmt.Fprintf(b, "%s{ var _tmp = %s; _PullInto_%s(ref _tmp, %s, map, oldMap, oldOwners); }\n", ind, srcExpr, ref.Name, dstExpr)
			} else {
				fmt.Fprintf(b, "%sif (%s == null) { TableReset(%s); } else { _PullInto_%s(%s, %s, map, oldMap, oldOwners); }\n", ind, srcExpr, dstExpr, ref.Name, srcExpr, dstExpr)
			}
		case *ir.Union:
			if isFixedUnion(u, ref) {
				fmt.Fprintf(b, "%s{ var _tmp = %s; _PullInto_%s(ref _tmp, %s, map, oldMap, oldOwners); }\n", ind, srcExpr, ref.Name, dstExpr)
			} else {
				fmt.Fprintf(b, "%sif (%s == null) { TableReset(%s); } else { _PullInto_%s(%s, %s, map, oldMap, oldOwners); }\n", ind, srcExpr, dstExpr, ref.Name, srcExpr, dstExpr)
			}
		default:
			fmt.Fprintf(b, "%s%s = %s;\n", ind, dstExpr, srcExpr)
		}
	default:
		fmt.Fprintf(b, "%s%s = %s;\n", ind, dstExpr, srcExpr)
	}
}

func emitPushElement(b *strings.Builder, u *ir.Unit, pkg string, f *ir.Field, srcExpr, dstExpr, ind string) {
	switch {
	case f.IsMap():
		eName := f.MapEntry.Name
		qual := "global::" + pkg + "." + eName
		fmt.Fprintf(b, "%s{ var _me = new %s(); if (%s != null) { _PushInto_%s(%s, _me, map, nodeOut); } %s = _me; }\n", ind, qual, srcExpr, eName, srcExpr, dstExpr)
	case f.Type.Blob():
		fmt.Fprintf(b, "%sif (%s == null) { %s = null; } else { if (!map.TryGetValue(%s, out object _ub)) { byte[] _bd = %s.Data; _ub = new TableBlob(_bd == null ? global::System.Array.Empty<byte>() : (byte[])_bd.Clone()); map[%s] = _ub; } %s = (TableBlob)_ub; }\n",
			ind, srcExpr, dstExpr, srcExpr, srcExpr, srcExpr, dstExpr)
	case f.Type.Pointer:
		tName := f.Type.Name
		qual := "global::" + pkg + "." + tName
		tSt := f.Type.Ref.(*ir.Struct)
		if isFixedStruct(u, tSt) {
			fmt.Fprintf(b, "%sif (%s == null) { %s = null; } else { if (!map.TryGetValue(%s, out object _un)) { var _nr = new Ref<%s>(); map[%s] = _nr; if (nodeOut != null) { nodeOut[%s] = _nr; } _Push_%s(%s, ref _nr.Value, map, nodeOut); _un = _nr; } %s = (Ref<%s>)_un; }\n",
				ind, srcExpr, dstExpr, srcExpr, qual, srcExpr, srcExpr, tName, srcExpr, dstExpr, qual)
		} else {
			fmt.Fprintf(b, "%sif (%s == null) { %s = null; } else { if (!map.TryGetValue(%s, out object _un)) { var _ni = new %s(); map[%s] = _ni; if (nodeOut != null) { nodeOut[%s] = _ni; } _PushInto_%s(%s, _ni, map, nodeOut); _un = _ni; } %s = (%s)_un; }\n",
				ind, srcExpr, dstExpr, srcExpr, qual, srcExpr, srcExpr, tName, srcExpr, dstExpr, qual)
		}
	case f.Type.Kind == ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			qual := "global::" + pkg + "." + ref.Name
			if isFixedStruct(u, ref) {
				fmt.Fprintf(b, "%s{ %s _tmp = default; _Push_%s(%s, ref _tmp, map, nodeOut); %s = _tmp; }\n", ind, qual, ref.Name, srcExpr, dstExpr)
			} else {
				fmt.Fprintf(b, "%s{ var _tmp = %s ?? new %s(); _PushInto_%s(%s, _tmp, map, nodeOut); %s = _tmp; }\n", ind, dstExpr, qual, ref.Name, srcExpr, dstExpr)
			}
		case *ir.Union:
			qual := "global::" + pkg + "." + ref.Name
			if isFixedUnion(u, ref) {
				fmt.Fprintf(b, "%s{ %s _tmp = default; _Push_%s(%s, ref _tmp, map, nodeOut); %s = _tmp; }\n", ind, qual, ref.Name, srcExpr, dstExpr)
			} else {
				fmt.Fprintf(b, "%s{ var _tmp = %s ?? new %s(); _PushInto_%s(%s, _tmp, map, nodeOut); %s = _tmp; }\n", ind, dstExpr, qual, ref.Name, srcExpr, dstExpr)
			}
		default:
			fmt.Fprintf(b, "%s%s = %s;\n", ind, dstExpr, srcExpr)
		}
	default:
		fmt.Fprintf(b, "%s%s = %s;\n", ind, dstExpr, srcExpr)
	}
}

func emitPullFieldBody(b *strings.Builder, u *ir.Unit, pkg string, f *ir.Field, m string, fixedHolder, isTable bool) {
	if f.Type.Optional {
		fmt.Fprintf(b, "    dst.%sPresent = src.%sPresent;\n", m, m)
	}
	elemCarrierT := carrierElemType(pkg, f)
	switch {
	case f.IsList(), f.IsMap():
		fmt.Fprintf(b, "    {\n        int _raw = src.%sCount;\n        int _alloc = _raw > 0 ? _raw : 0;\n        if (dst.%s.Length < _alloc) { global::System.Array.Resize(ref dst.%s, _alloc); }\n        int _copy = src.%s == null ? 0 : global::System.Math.Min(_alloc, src.%s.Length);\n        for (int _i = 0; _i < _copy; _i++)\n        {\n", m, m, m, m, m)
		if isCarrierClassElem(f) {
			fmt.Fprintf(b, "            if (dst.%s[_i] == null) { dst.%s[_i] = new %s(); }\n", m, m, elemCarrierT)
		}
		emitPullElement(b, u, pkg, f, fmt.Sprintf("src.%s[_i]", m), fmt.Sprintf("dst.%s[_i]", m), "            ")
		fmt.Fprintf(b, "        }\n")
		if isCarrierClassElem(f) {
			fmt.Fprintf(b, "        for (int _i = _copy; _i < _alloc; _i++) { if (dst.%s[_i] == null) { dst.%s[_i] = new %s(); } else { TableReset(dst.%s[_i]); } }\n", m, m, elemCarrierT, m)
		}
		fmt.Fprintf(b, "        dst.%sCount = _raw;\n    }\n", m)
	case f.Type.TextUnbounded():
		fmt.Fprintf(b, "    {\n        int _raw = src.%sLength;\n        int _copy = src.%s == null || _raw <= 0 ? 0 : global::System.Math.Min(_raw, src.%s.Length);\n        if (dst.%s.Length < _copy) { global::System.Array.Resize(ref dst.%s, _copy); }\n        if (_copy > 0) { global::System.Array.Copy(src.%s, 0, dst.%s, 0, _copy); }\n        dst.%sLength = _raw;\n    }\n",
			m, m, m, m, m, m, m, m)
	case (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && !f.Type.Pointer:
		if fixedHolder {
			fmt.Fprintf(b, "    {\n        int _raw = src.%sLength;\n        int _copy = _raw <= 0 ? 0 : global::System.Math.Min(_raw, %d);\n        global::System.Array.Clear(dst.%s, 0, %d);\n        if (_copy > 0) { src.%s.AsSpan().Slice(0, _copy).CopyTo(dst.%s); }\n        dst.%sLength = _raw;\n    }\n",
				m, f.Type.Size, m, f.Type.Size, m, m, m)
		} else {
			fmt.Fprintf(b, "    {\n        int _raw = src.%sLength;\n        int _copy = src.%s == null || _raw <= 0 ? 0 : global::System.Math.Min(_raw, global::System.Math.Min((int)%d, src.%s.Length));\n        global::System.Array.Clear(dst.%s, 0, %d);\n        if (_copy > 0) { global::System.Array.Copy(src.%s, 0, dst.%s, 0, _copy); }\n        dst.%sLength = _raw;\n    }\n",
				m, m, f.Type.Size, m, m, f.Type.Size, m, m, m)
		}
	case f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted:
		limitExpr := fmt.Sprintf("%d", f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			fmt.Fprintf(b, "    dst.%sCount = src.%sCount;\n", m, m)
			limitExpr = fmt.Sprintf("(src.%sCount <= 0 ? 0 : global::System.Math.Min(src.%sCount, %d))", m, m, f.ArrayBound)
		}
		if fixedHolder {
			elemUserT := scalarStorageType(f.Type)
			fmt.Fprintf(b, "    {\n        int _n = %s;\n        Span<%s> _sp = src.%s.AsSpan();\n        for (int _i = 0; _i < _n; _i++)\n        {\n", limitExpr, elemUserT, m)
			emitPullElement(b, u, pkg, f, "_sp[_i]", fmt.Sprintf("dst.%s[_i]", m), "            ")
			b.WriteString("        }\n    }\n")
		} else {
			if f.KeyEnum != "" && isTable {
				fmt.Fprintf(b, "    if (src.%s != null && src.%s.Slots != null)\n    {\n        int _n = global::System.Math.Min(%s, src.%s.Slots.Length);\n        for (int _i = 0; _i < _n; _i++)\n        {\n", m, m, limitExpr, m)
				emitPullElement(b, u, pkg, f, fmt.Sprintf("src.%s.Slots[_i]", m), fmt.Sprintf("dst.%s[_i]", m), "            ")
				b.WriteString("        }\n    }\n")
			} else {
				slots := fmt.Sprintf("src.%s", m)
				fmt.Fprintf(b, "    if (%s != null)\n    {\n        int _n = global::System.Math.Min(%s, %s.Length);\n        for (int _i = 0; _i < _n; _i++)\n        {\n", slots, limitExpr, slots)
				emitPullElement(b, u, pkg, f, fmt.Sprintf("%s[_i]", slots), fmt.Sprintf("dst.%s[_i]", m), "            ")
				b.WriteString("        }\n    }\n")
			}
		}
	default:
		emitPullElement(b, u, pkg, f, "src."+m, "dst."+m, "    ")
	}
}

func emitPushFieldBody(b *strings.Builder, u *ir.Unit, pkg string, f *ir.Field, m string, fixedHolder, isTable bool) {
	if f.Type.Optional {
		fmt.Fprintf(b, "    dst.%sPresent = src.%sPresent;\n", m, m)
	}
	userElemT := userElementType(u, pkg, f)
	switch {
	case f.IsList(), f.IsMap():
		fmt.Fprintf(b, "    {\n        int _n = src.%sCount;\n        dst.%s = _n <= 0 ? global::System.Array.Empty<%s>() : new %s[_n];\n        for (int _i = 0; _i < _n; _i++)\n        {\n", m, m, userElemT, userElemT)
		emitPushElement(b, u, pkg, f, fmt.Sprintf("src.%s[_i]", m), fmt.Sprintf("dst.%s[_i]", m), "            ")
		fmt.Fprintf(b, "        }\n        dst.%sCount = _n;\n    }\n", m)
	case f.Type.TextUnbounded():
		fmt.Fprintf(b, "    {\n        int _n = src.%sLength;\n        dst.%s = _n <= 0 ? global::System.Array.Empty<byte>() : src.%s.AsSpan(0, _n).ToArray();\n        dst.%sLength = _n;\n    }\n", m, m, m, m)
	case (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && !f.Type.Pointer:
		if fixedHolder {
			fmt.Fprintf(b, "    {\n        dst.%s = default;\n        int _n = global::System.Math.Clamp(src.%sLength, 0, %d);\n        if (_n > 0) { src.%s.AsSpan(0, _n).CopyTo(dst.%s.AsSpan()); }\n        dst.%sLength = _n;\n    }\n",
				m, m, f.Type.Size, m, m, m)
		} else {
			fmt.Fprintf(b, "    {\n        if (dst.%s == null || dst.%s.Length < %d) { dst.%s = new byte[%d]; } else { global::System.Array.Clear(dst.%s, 0, dst.%s.Length); }\n        int _n = global::System.Math.Clamp(src.%sLength, 0, %d);\n        if (_n > 0) { global::System.Array.Copy(src.%s, 0, dst.%s, 0, _n); }\n        dst.%sLength = _n;\n    }\n",
				m, m, f.Type.Size, m, f.Type.Size, m, m, m, f.Type.Size, m, m, m)
		}
	case f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted:
		if fixedHolder {
			elemUserT := scalarStorageType(f.Type)
			fmt.Fprintf(b, "    {\n        dst.%s = default;\n        Span<%s> _sp = dst.%s.AsSpan();\n        for (int _i = 0; _i < %d; _i++)\n        {\n", m, elemUserT, m, f.ArrayBound)
			emitPushElement(b, u, pkg, f, fmt.Sprintf("src.%s[_i]", m), "_sp[_i]", "            ")
			b.WriteString("        }\n")
			if f.Array == ir.ArrayCounted {
				fmt.Fprintf(b, "        dst.%sCount = src.%sCount;\n", m, m)
			}
			b.WriteString("    }\n")
		} else {
			if f.KeyEnum != "" && isTable {
				keyT := "global::" + pkg + "." + f.KeyEnum
				fmt.Fprintf(b, "    {\n        if (dst.%s == null) { dst.%s = new TableKeyed<%s, %s>(); }\n        for (int _i = 0; _i < %d; _i++)\n        {\n", m, m, userElemT, keyT, f.ArrayBound)
				emitPushElement(b, u, pkg, f, fmt.Sprintf("src.%s[_i]", m), fmt.Sprintf("dst.%s.Slots[_i]", m), "            ")
				b.WriteString("        }\n    }\n")
			} else {
				fmt.Fprintf(b, "    {\n        if (dst.%s == null || dst.%s.Length < %d) { dst.%s = new %s[%d]; }\n        for (int _i = 0; _i < %d; _i++)\n        {\n", m, m, f.ArrayBound, m, userElemT, f.ArrayBound, f.ArrayBound)
				emitPushElement(b, u, pkg, f, fmt.Sprintf("src.%s[_i]", m), fmt.Sprintf("dst.%s[_i]", m), "            ")
				b.WriteString("        }\n")
				if f.Array == ir.ArrayCounted {
					fmt.Fprintf(b, "        dst.%sCount = src.%sCount;\n", m, m)
				}
				b.WriteString("    }\n")
			}
		}
	default:
		emitPushElement(b, u, pkg, f, "src."+m, "dst."+m, "    ")
	}
}

func emitStructPullPush(b *strings.Builder, u *ir.Unit, pkg string, st *ir.Struct) {
	qual := "global::" + pkg + "." + st.Name
	cname := "__Carrier_" + st.Name
	fixed := isFixedStruct(u, st)
	if fixed {
		fmt.Fprintf(b, "internal static void _PullInto_%s(ref %s src, %s dst, global::System.Collections.Generic.Dictionary<object, object> map, global::System.Collections.Generic.Dictionary<object, uint> oldMap, object[] oldOwners)\n{\n", st.Name, qual, cname)
		for _, f := range st.Fields {
			emitPullFieldBody(b, u, pkg, f, member(f), true, st.IsTable)
		}
		b.WriteString("}\n\n")
		fmt.Fprintf(b, "internal static %s _Pull_%s(ref %s src, global::System.Collections.Generic.Dictionary<object, object> map = null, global::System.Collections.Generic.Dictionary<object, uint> oldMap = null, object[] oldOwners = null)\n{\n    map ??= new global::System.Collections.Generic.Dictionary<object, object>(RefEq.Instance);\n    var dst = new %s();\n    _PullInto_%s(ref src, dst, map, oldMap, oldOwners);\n    return dst;\n}\n\n", cname, st.Name, qual, cname, st.Name)

		fmt.Fprintf(b, "internal static void _Push_%s(%s src, ref %s dst, global::System.Collections.Generic.Dictionary<object, object> map = null, global::System.Collections.Generic.Dictionary<object, object> nodeOut = null)\n{\n    map ??= new global::System.Collections.Generic.Dictionary<object, object>(RefEq.Instance);\n    dst = default;\n", st.Name, cname, qual)
		for _, f := range st.Fields {
			emitPushFieldBody(b, u, pkg, f, member(f), true, st.IsTable)
		}
		b.WriteString("}\n\n")
		return
	}

	fmt.Fprintf(b, "internal static void _PullInto_%s(%s src, %s dst, global::System.Collections.Generic.Dictionary<object, object> map, global::System.Collections.Generic.Dictionary<object, uint> oldMap, object[] oldOwners)\n{\n    if (src == null) { TableReset(dst); return; }\n", st.Name, qual, cname)
	for _, f := range st.Fields {
		emitPullFieldBody(b, u, pkg, f, member(f), false, st.IsTable)
	}
	b.WriteString("}\n\n")
	fmt.Fprintf(b, "internal static %s _Pull_%s(%s src, global::System.Collections.Generic.Dictionary<object, object> map = null, global::System.Collections.Generic.Dictionary<object, uint> oldMap = null, object[] oldOwners = null)\n{\n    if (src == null) { return null; }\n    map ??= new global::System.Collections.Generic.Dictionary<object, object>(RefEq.Instance);\n    if (map.TryGetValue(src, out object existing)) { return (%s)existing; }\n    var dst = new %s();\n    map[src] = dst;\n    _RecordOldOwner(src, dst, oldMap, oldOwners);\n    _PullInto_%s(src, dst, map, oldMap, oldOwners);\n    return dst;\n}\n\n", cname, st.Name, qual, cname, cname, st.Name)

	fmt.Fprintf(b, "internal static void _PushInto_%s(%s src, %s dst, global::System.Collections.Generic.Dictionary<object, object> map, global::System.Collections.Generic.Dictionary<object, object> nodeOut)\n{\n", st.Name, cname, qual)
	for _, f := range st.Fields {
		emitPushFieldBody(b, u, pkg, f, member(f), false, st.IsTable)
	}
	b.WriteString("}\n\n")
	fmt.Fprintf(b, "internal static void _Push_%s(%s src, ref %s dst, global::System.Collections.Generic.Dictionary<object, object> map = null, global::System.Collections.Generic.Dictionary<object, object> nodeOut = null)\n{\n    map ??= new global::System.Collections.Generic.Dictionary<object, object>(RefEq.Instance);\n    if (dst == null) { dst = new %s(); }\n    map[src] = dst;\n    if (nodeOut != null) { nodeOut[src] = dst; }\n    _PushInto_%s(src, dst, map, nodeOut);\n}\n\n", st.Name, cname, qual, qual, st.Name)
}

func emitUnionPullPush(b *strings.Builder, u *ir.Unit, pkg string, un *ir.Union, fields []*ir.Field, names []string) {
	qual := "global::" + pkg + "." + un.Name
	cname := "__Carrier_" + un.Name
	fixed := isFixedUnion(u, un)
	if fixed {
		fmt.Fprintf(b, "internal static void _PullInto_%s(ref %s src, %s dst, global::System.Collections.Generic.Dictionary<object, object> map, global::System.Collections.Generic.Dictionary<object, uint> oldMap, object[] oldOwners)\n{\n    TableReset(dst);\n    dst.Type = src.Type;\n", un.Name, qual, cname)
		for i, f := range fields {
			emitPullFieldBody(b, u, pkg, f, names[i], true, false)
		}
		b.WriteString("}\n\n")

		fmt.Fprintf(b, "internal static void _Push_%s(%s src, ref %s dst, global::System.Collections.Generic.Dictionary<object, object> map = null, global::System.Collections.Generic.Dictionary<object, object> nodeOut = null)\n{\n    map ??= new global::System.Collections.Generic.Dictionary<object, object>(RefEq.Instance);\n    dst = default;\n    dst.Type = src.Type;\n", un.Name, cname, qual)
		for i, f := range fields {
			emitPushFieldBody(b, u, pkg, f, names[i], true, false)
		}
		b.WriteString("}\n\n")
		return
	}

	fmt.Fprintf(b, "internal static void _PullInto_%s(%s src, %s dst, global::System.Collections.Generic.Dictionary<object, object> map, global::System.Collections.Generic.Dictionary<object, uint> oldMap, object[] oldOwners)\n{\n    TableReset(dst);\n    if (src == null) { return; }\n    dst.Type = src.Type;\n", un.Name, qual, cname)
	for i, f := range fields {
		emitPullFieldBody(b, u, pkg, f, names[i], false, false)
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "internal static void _PushInto_%s(%s src, %s dst, global::System.Collections.Generic.Dictionary<object, object> map, global::System.Collections.Generic.Dictionary<object, object> nodeOut)\n{\n    dst.Type = src.Type;\n", un.Name, cname, qual)
	for i, f := range fields {
		emitPushFieldBody(b, u, pkg, f, names[i], false, false)
	}
	b.WriteString("}\n\n")
}
