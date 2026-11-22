package unitytable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func emitTableSurfaces(b *strings.Builder, u *ir.Unit, st *ir.Struct, msgStructs map[string]bool) {
	if st.IsMapEntry() {
		return
	}
	pkg := namespaceOf(u.Package)
	qual := typeRefInSchema(pkg, st.Name)
	fixed := isFixedStruct(u, st)
	variable := ir.VariableTables(u)[st.Name]
	idCap := ir.TableWireIdCapacity(u)

	if st.CarriesForm("wire") {
		if variable {
			fmt.Fprintf(b, "public static long %sLoadMeasure(ReadOnlySpan<byte> bytes)\n{\n    return TableWire.LoadMeasure(%sTableType, bytes, out _);\n}\n\n", st.Name, st.Name)
			fmt.Fprintf(b, "public static long %sLoadMeasure(ReadOnlySpan<byte> bytes, out TableRefuseReason reason)\n{\n    long size = TableWire.LoadMeasure(%sTableType, bytes, out string why);\n    reason = _ParseRefuseReason(why);\n    return size;\n}\n\n", st.Name, st.Name)
		}

		if fixed {
			fmt.Fprintf(b, "public static long %sMeasure(ref %s value)\n{\n    var c = _Pull_%s(ref value);\n    Span<ulong> ids = stackalloc ulong[%d];\n    return TableWire.Save(c, %sTableType, Span<byte>.Empty, ids, true);\n}\n\n", st.Name, qual, st.Name, idCap, st.Name)
			fmt.Fprintf(b, "public static long %sMeasure(%s value)\n{\n    return %sMeasure(ref value);\n}\n\n", st.Name, qual, st.Name)

			fmt.Fprintf(b, "public static long %sSave(ref %s value, Span<byte> buffer)\n{\n    var c = _Pull_%s(ref value);\n    Span<ulong> ids = stackalloc ulong[%d];\n    return TableWire.Save(c, %sTableType, buffer, ids, false);\n}\n\n", st.Name, qual, st.Name, idCap, st.Name)
			fmt.Fprintf(b, "public static long %sSave(%s value, Span<byte> buffer)\n{\n    return %sSave(ref value, buffer);\n}\n\n", st.Name, qual, st.Name)

			fmt.Fprintf(b, "public static bool %sLoad(ref %s value, ReadOnlySpan<byte> bytes, ref TableReport report)\n{\n    Init%s(ref value);\n    var c = new __Carrier_%s();\n    var box = new _R();\n    bool ok = TableWire.Load(c, %sTableType, bytes, box) == TableWire.Verdict.Ok;\n    _FinishReport(box, ref report, ok);\n    if (ok) { _Push_%s(c, ref value); }\n    return ok;\n}\n\n",
				st.Name, qual, st.Name, st.Name, st.Name, st.Name)
		} else {
			fmt.Fprintf(b, "public static long %sMeasure(ref %s value)\n{\n    if (value == null) { return -1; }\n    var c = _Pull_%s(value);\n    Span<ulong> ids = stackalloc ulong[%d];\n    return TableWire.Save(c, %sTableType, Span<byte>.Empty, ids, true);\n}\n\n", st.Name, qual, st.Name, idCap, st.Name)
			fmt.Fprintf(b, "public static long %sMeasure(%s value)\n{\n    return %sMeasure(ref value);\n}\n\n", st.Name, qual, st.Name)

			fmt.Fprintf(b, "public static long %sSave(ref %s value, Span<byte> buffer)\n{\n    if (value == null) { return -1; }\n    var c = _Pull_%s(value);\n    Span<ulong> ids = stackalloc ulong[%d];\n    return TableWire.Save(c, %sTableType, buffer, ids, false);\n}\n\n", st.Name, qual, st.Name, idCap, st.Name)
			fmt.Fprintf(b, "public static long %sSave(%s value, Span<byte> buffer)\n{\n    return %sSave(ref value, buffer);\n}\n\n", st.Name, qual, st.Name)

			fmt.Fprintf(b, "public static bool %sLoad(ref %s value, ReadOnlySpan<byte> bytes, ref TableReport report)\n{\n    if (value == null) { value = new %s(); } else { _PushInto_%s(new __Carrier_%s(), value, new System.Collections.Generic.Dictionary<object, object>(RefEq.Instance), null); }\n    var c = new __Carrier_%s();\n    var box = new _R();\n    bool ok = TableWire.Load(c, %sTableType, bytes, box) == TableWire.Verdict.Ok;\n    _FinishReport(box, ref report, ok);\n    if (ok) { _Push_%s(c, ref value); }\n    return ok;\n}\n\n",
				st.Name, qual, qual, st.Name, st.Name, st.Name, st.Name, st.Name)
			fmt.Fprintf(b, "public static bool %sLoad(%s value, ReadOnlySpan<byte> bytes, ref TableReport report)\n{\n    if (value == null) { report = default; report.Refused = true; report.Reason = TableReport.WireDamaged; return false; }\n    return %sLoad(ref value, bytes, ref report);\n}\n\n",
				st.Name, qual, st.Name)
		}

		if variable {
			emitRetainSurfaces(b, st, qual, fixed, idCap)
		}
	}

	if st.CarriesForm("json") {
		if fixed {
			fmt.Fprintf(b, "public static bool %sFromJson(ref %s value, ReadOnlySpan<byte> text, ref TableReport report)\n{\n    Init%s(ref value);\n    var c = new __Carrier_%s();\n    var box = new _R();\n    bool ok = TableJson.Read(c, %sTableType, text, box);\n    _FinishReport(box, ref report, ok);\n    if (ok) { _Push_%s(c, ref value); }\n    return ok;\n}\n\n",
				st.Name, qual, st.Name, st.Name, st.Name, st.Name)
			fmt.Fprintf(b, "public static long %sToJsonMeasure(ref %s value)\n{\n    var c = _Pull_%s(ref value);\n    return TableJson.Write(c, %sTableType, Span<byte>.Empty, true);\n}\n\n", st.Name, qual, st.Name, st.Name)
			fmt.Fprintf(b, "public static long %sToJsonMeasure(%s value)\n{\n    return %sToJsonMeasure(ref value);\n}\n\n", st.Name, qual, st.Name)
			fmt.Fprintf(b, "public static long %sToJson(ref %s value, Span<byte> buffer)\n{\n    var c = _Pull_%s(ref value);\n    return TableJson.Write(c, %sTableType, buffer, false);\n}\n\n", st.Name, qual, st.Name, st.Name)
			fmt.Fprintf(b, "public static long %sToJson(%s value, Span<byte> buffer)\n{\n    return %sToJson(ref value, buffer);\n}\n\n", st.Name, qual, st.Name)
		} else {
			fmt.Fprintf(b, "public static bool %sFromJson(ref %s value, ReadOnlySpan<byte> text, ref TableReport report)\n{\n    if (value == null) { value = new %s(); } else { _PushInto_%s(new __Carrier_%s(), value, new System.Collections.Generic.Dictionary<object, object>(RefEq.Instance), null); }\n    var c = new __Carrier_%s();\n    var box = new _R();\n    bool ok = TableJson.Read(c, %sTableType, text, box);\n    _FinishReport(box, ref report, ok);\n    if (ok) { _Push_%s(c, ref value); }\n    return ok;\n}\n\n",
				st.Name, qual, qual, st.Name, st.Name, st.Name, st.Name, st.Name)
			fmt.Fprintf(b, "public static bool %sFromJson(%s value, ReadOnlySpan<byte> text, ref TableReport report)\n{\n    if (value == null) { report = default; report.Refused = true; report.Reason = TableReport.WireDamaged; return false; }\n    return %sFromJson(ref value, text, ref report);\n}\n\n",
				st.Name, qual, st.Name)
			fmt.Fprintf(b, "public static long %sToJsonMeasure(ref %s value)\n{\n    if (value == null) { return -1; }\n    var c = _Pull_%s(value);\n    return TableJson.Write(c, %sTableType, Span<byte>.Empty, true);\n}\n\n", st.Name, qual, st.Name, st.Name)
			fmt.Fprintf(b, "public static long %sToJsonMeasure(%s value)\n{\n    return %sToJsonMeasure(ref value);\n}\n\n", st.Name, qual, st.Name)
			fmt.Fprintf(b, "public static long %sToJson(ref %s value, Span<byte> buffer)\n{\n    if (value == null) { return -1; }\n    var c = _Pull_%s(value);\n    return TableJson.Write(c, %sTableType, buffer, false);\n}\n\n", st.Name, qual, st.Name, st.Name)
			fmt.Fprintf(b, "public static long %sToJson(%s value, Span<byte> buffer)\n{\n    return %sToJson(ref value, buffer);\n}\n\n", st.Name, qual, st.Name)
		}
	}

	for index, f := range st.Fields {
		if !f.IsTextList() {
			continue
		}
		prefix := st.Name + member(f)
		desc := fmt.Sprintf("%sTableType.Fields[%d]", st.Name, index)
		if fixed {
			fmt.Fprintf(b, "public static ReadOnlySpan<byte> %sTextAt(ref %s value, int index, out bool present)\n{\n    var c = _Pull_%s(ref value);\n    return TableWire.TextAt(c, %s, index, out present);\n}\n\n", prefix, qual, st.Name, desc)
			fmt.Fprintf(b, "public static bool %sAppend(ref %s value, ReadOnlySpan<byte> text)\n{\n    var c = _Pull_%s(ref value);\n    if (!TableWire.TextAppend(c, %s, text, true)) { return false; }\n    _Push_%s(c, ref value);\n    return true;\n}\n\n", prefix, qual, st.Name, desc, st.Name)
			if ir.TextListOptional(f) {
				fmt.Fprintf(b, "public static bool %sAppendAbsent(ref %s value)\n{\n    var c = _Pull_%s(ref value);\n    if (!TableWire.TextAppend(c, %s, ReadOnlySpan<byte>.Empty, false)) { return false; }\n    _Push_%s(c, ref value);\n    return true;\n}\n\n", prefix, qual, st.Name, desc, st.Name)
			}
		} else {
			fmt.Fprintf(b, "public static ReadOnlySpan<byte> %sTextAt(%s value, int index, out bool present)\n{\n    if (value == null) { present = false; return default; }\n    var c = _Pull_%s(value);\n    return TableWire.TextAt(c, %s, index, out present);\n}\n\n", prefix, qual, st.Name, desc)
			fmt.Fprintf(b, "public static bool %sAppend(%s value, ReadOnlySpan<byte> text)\n{\n    if (value == null) { return false; }\n    var c = _Pull_%s(value);\n    if (!TableWire.TextAppend(c, %s, text, true)) { return false; }\n    _PushInto_%s(c, value, new System.Collections.Generic.Dictionary<object, object>(RefEq.Instance), null);\n    return true;\n}\n\n", prefix, qual, st.Name, desc, st.Name)
			if ir.TextListOptional(f) {
				fmt.Fprintf(b, "public static bool %sAppendAbsent(%s value)\n{\n    if (value == null) { return false; }\n    var c = _Pull_%s(value);\n    if (!TableWire.TextAppend(c, %s, ReadOnlySpan<byte>.Empty, false)) { return false; }\n    _PushInto_%s(c, value, new System.Collections.Generic.Dictionary<object, object>(RefEq.Instance), null);\n    return true;\n}\n\n", prefix, qual, st.Name, desc, st.Name)
			}
		}
	}

	if st.CarriesForm("cook") {
		if fixed {
			fmt.Fprintf(b, "public static long %sCookMeasure(ref %s value)\n{\n    var c = _Pull_%s(ref value);\n    return TableWire.Cook(c, %sTableType, Span<byte>.Empty, TableByteOrder.Little, true);\n}\n\n", st.Name, qual, st.Name, st.Name)
			fmt.Fprintf(b, "public static long %sCookMeasure(%s value)\n{\n    return %sCookMeasure(ref value);\n}\n\n", st.Name, qual, st.Name)
			fmt.Fprintf(b, "public static bool %sCook(ref %s value, Span<byte> bytes, TableByteOrder order = TableByteOrder.Little)\n{\n    var c = _Pull_%s(ref value);\n    return TableWire.Cook(c, %sTableType, bytes, order, false) >= 0;\n}\n\n", st.Name, qual, st.Name, st.Name)
			fmt.Fprintf(b, "public static bool %sCook(%s value, Span<byte> bytes, TableByteOrder order = TableByteOrder.Little)\n{\n    return %sCook(ref value, bytes, order);\n}\n\n", st.Name, qual, st.Name)
		} else {
			fmt.Fprintf(b, "public static long %sCookMeasure(%s value)\n{\n    if (value == null) { return -1; }\n    var c = _Pull_%s(value);\n    return TableWire.Cook(c, %sTableType, Span<byte>.Empty, TableByteOrder.Little, true);\n}\n\n", st.Name, qual, st.Name, st.Name)
			fmt.Fprintf(b, "public static bool %sCook(%s value, Span<byte> bytes, TableByteOrder order = TableByteOrder.Little)\n{\n    if (value == null) { return false; }\n    var c = _Pull_%s(value);\n    return TableWire.Cook(c, %sTableType, bytes, order, false) >= 0;\n}\n\n", st.Name, qual, st.Name, st.Name)
		}
	}

	if msgStructs[st.Name] && st.IsTable && st.FixedDeclared {
		emitMessageSurfaces(b, st, qual)
	}
}

func emitRetainSurfaces(b *strings.Builder, st *ir.Struct, qual string, fixed bool, idCap int) {
	if fixed {
		fmt.Fprintf(b, "public static bool %sLoadRetain(ref %s value, ReadOnlySpan<byte> bytes, TableRetain retain, ref TableReport report)\n{\n    Init%s(ref value);\n    var c = new __Carrier_%s();\n    var box = new _R();\n    bool ok = TableWire.Load(c, %sTableType, bytes, box, retain, out _) == TableWire.Verdict.Ok;\n    _FinishReport(box, ref report, ok);\n    if (ok)\n    {\n        _Push_%s(c, ref value);\n        if (retain != null) { retain.NodeOwners = new object[] { null, _StructRootOwner }; }\n    }\n    return ok;\n}\n\n",
			st.Name, qual, st.Name, st.Name, st.Name, st.Name)

		fmt.Fprintf(b, "public static long %sMeasureRetain(ref %s value, TableRetain retain)\n{\n    var oldMap = new System.Collections.Generic.Dictionary<object, uint>(RefEq.Instance);\n    var c = _Pull_%s(ref value, null, oldMap, retain?.NodeOwners);\n    if (retain != null && retain.NodeOwners != null && retain.NodeOwners.Length > 1 && ReferenceEquals(retain.NodeOwners[1], _StructRootOwner)) { oldMap[c] = 1; }\n    Span<ulong> vocab = stackalloc ulong[%d];\n    Span<int> slots = stackalloc int[%d];\n    return TableWire.SaveRetainCarrier(c, %sTableType, Span<byte>.Empty, vocab, slots, retain, oldMap, null, true);\n}\n\n",
			st.Name, qual, st.Name, idCap, idCap, st.Name)
		fmt.Fprintf(b, "public static long %sMeasureRetain(%s value, TableRetain retain)\n{\n    return %sMeasureRetain(ref value, retain);\n}\n\n", st.Name, qual, st.Name)

		fmt.Fprintf(b, "public static long %sSaveRetain(ref %s value, TableRetain retain, Span<byte> bytes, ref TableReport report)\n{\n    var oldMap = new System.Collections.Generic.Dictionary<object, uint>(RefEq.Instance);\n    var c = _Pull_%s(ref value, null, oldMap, retain?.NodeOwners);\n    if (retain != null && retain.NodeOwners != null && retain.NodeOwners.Length > 1 && ReferenceEquals(retain.NodeOwners[1], _StructRootOwner)) { oldMap[c] = 1; }\n    Span<ulong> vocab = stackalloc ulong[%d];\n    Span<int> slots = stackalloc int[%d];\n    var box = new _R();\n    long n = TableWire.SaveRetainCarrier(c, %sTableType, bytes, vocab, slots, retain, oldMap, box, false);\n    report.RetainLost += box.RetainLost;\n    return n;\n}\n\n",
			st.Name, qual, st.Name, idCap, idCap, st.Name)
		fmt.Fprintf(b, "public static long %sSaveRetain(%s value, TableRetain retain, Span<byte> bytes, ref TableReport report)\n{\n    return %sSaveRetain(ref value, retain, bytes, ref report);\n}\n\n", st.Name, qual, st.Name)
		return
	}

	fmt.Fprintf(b, "public static bool %sLoadRetain(ref %s value, ReadOnlySpan<byte> bytes, TableRetain retain, ref TableReport report)\n{\n    if (value == null) { value = new %s(); } else { _PushInto_%s(new __Carrier_%s(), value, new System.Collections.Generic.Dictionary<object, object>(RefEq.Instance), null); }\n    var c = new __Carrier_%s();\n    var box = new _R();\n    bool ok = TableWire.Load(c, %sTableType, bytes, box, retain, out TableWire.Graph g) == TableWire.Verdict.Ok;\n    _FinishReport(box, ref report, ok);\n    if (ok)\n    {\n        var nodeOut = new System.Collections.Generic.Dictionary<object, object>(RefEq.Instance);\n        _Push_%s(c, ref value, null, nodeOut);\n        if (retain != null)\n        {\n            int nc = g == null ? 0 : g.Nodes.Count;\n            retain.NodeOwners = new object[2 + nc];\n            retain.NodeOwners[1] = value;\n            for (int i = 0; i < nc; i++)\n            {\n                object cn = g.Nodes[i].Value;\n                if (cn != null && nodeOut.TryGetValue(cn, out object uo)) { retain.NodeOwners[2 + i] = uo; }\n            }\n        }\n    }\n    return ok;\n}\n\n",
		st.Name, qual, qual, st.Name, st.Name, st.Name, st.Name, st.Name)
	fmt.Fprintf(b, "public static bool %sLoadRetain(%s value, ReadOnlySpan<byte> bytes, TableRetain retain, ref TableReport report)\n{\n    if (value == null) { if (retain != null) { retain.Reset(); } report = default; report.Refused = true; report.Reason = TableReport.WireDamaged; return false; }\n    return %sLoadRetain(ref value, bytes, retain, ref report);\n}\n\n",
		st.Name, qual, st.Name)

	fmt.Fprintf(b, "public static long %sMeasureRetain(%s value, TableRetain retain)\n{\n    if (value == null) { return -1; }\n    var oldMap = new System.Collections.Generic.Dictionary<object, uint>(RefEq.Instance);\n    var c = _Pull_%s(value, null, oldMap, retain?.NodeOwners);\n    Span<ulong> vocab = stackalloc ulong[%d];\n    Span<int> slots = stackalloc int[%d];\n    return TableWire.SaveRetainCarrier(c, %sTableType, Span<byte>.Empty, vocab, slots, retain, oldMap, null, true);\n}\n\n",
		st.Name, qual, st.Name, idCap, idCap, st.Name)

	fmt.Fprintf(b, "public static long %sSaveRetain(%s value, TableRetain retain, Span<byte> bytes, ref TableReport report)\n{\n    if (value == null) { return -1; }\n    var oldMap = new System.Collections.Generic.Dictionary<object, uint>(RefEq.Instance);\n    var c = _Pull_%s(value, null, oldMap, retain?.NodeOwners);\n    Span<ulong> vocab = stackalloc ulong[%d];\n    Span<int> slots = stackalloc int[%d];\n    var box = new _R();\n    long n = TableWire.SaveRetainCarrier(c, %sTableType, bytes, vocab, slots, retain, oldMap, box, false);\n    report.RetainLost += box.RetainLost;\n    return n;\n}\n\n",
		st.Name, qual, st.Name, idCap, idCap, st.Name)
}

func emitMessageSurfaces(b *strings.Builder, st *ir.Struct, qual string) {
	n := st.Name
	fmt.Fprintf(b, "public static long %sMeasureMessages(ReadOnlySpan<%s> values)\n{\n    if (values.Length < 1 || values.Length > 256) { return -1; }\n    object[] arr = new object[values.Length];\n    for (int i = 0; i < values.Length; i++) { %s tmp = values[i]; arr[i] = _Pull_%s(ref tmp); }\n    return TableWire.MessageSave(arr, %sTableType, Span<byte>.Empty, true);\n}\n\n",
		n, qual, qual, n, n)

	fmt.Fprintf(b, "public static long %sSaveMessages(ReadOnlySpan<%s> values, Span<byte> bytes)\n{\n    TableReport ignored = default;\n    return %sSaveMessages(values, bytes, ref ignored);\n}\n\n",
		n, qual, n)

	fmt.Fprintf(b, "public static long %sSaveMessages(ReadOnlySpan<%s> values, Span<byte> bytes, ref TableReport report)\n{\n    if (values.Length > 256)\n    {\n        report = default;\n        report.Refused = true;\n        report.Reason = TableReport.BatchTooLarge;\n        report.ReasonText = \"batch_too_large\";\n        return -1;\n    }\n    if (values.Length < 1) { return -1; }\n    object[] arr = new object[values.Length];\n    for (int i = 0; i < values.Length; i++) { %s tmp = values[i]; arr[i] = _Pull_%s(ref tmp); }\n    return TableWire.MessageSave(arr, %sTableType, bytes, false);\n}\n\n",
		n, qual, qual, n, n)

	fmt.Fprintf(b, "public static bool %sLoadMessages(Span<%s> values, ReadOnlySpan<byte> bytes, TableVocabulary vocabulary, ref TableReport report, out int count)\n{\n    count = 0;\n    int need = bytes.Length >= 2 ? bytes[1] + 1 : 1;\n    int alloc = Math.Min(values.Length, Math.Max(1, need));\n    object[] arr = new object[values.Length];\n    for (int i = 0; i < alloc; i++) { arr[i] = new __Carrier_%s(); }\n    var box = new _R();\n    bool ok = TableWire.MessageLoad(arr, %sTableType, bytes, vocabulary, box, out count) == TableWire.Verdict.Ok;\n    _FinishReport(box, ref report, ok);\n    if (ok)\n    {\n        for (int i = 0; i < count; i++) { _Push_%s((__Carrier_%s)arr[i], ref values[i]); }\n    }\n    else\n    {\n        // R4: every value handed in goes back to its declared default.\n        count = 0;\n        for (int i = 0; i < values.Length; i++) { Init%s(ref values[i]); }\n    }\n    return ok;\n}\n\n",
		n, qual, n, n, n, n, n)
}
