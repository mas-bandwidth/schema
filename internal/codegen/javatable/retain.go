package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE RETAIN-UNKNOWN SURFACE (docs/SPEC-TABLES.md §6.6), the card's slice. A
// retained load, measure and save with caller-owned storage (bytes and ids,
// never grown on demand) and one report that accumulates across the pair; the
// retained data re-emitted in the same canonical order the Go and C backends
// write — the retained tail at the END of its own body, before the node-table
// field, ids interned in first-use order. The section's exclusions are
// reported as loss, never retained silently.
//
// THE WALK IS A PORT of the reference's resolving walk (internal/tablewire/
// retain.go, "WHICH KINDS THE RESOLVING WALK TOUCHES"): the capture resolves
// every reference to the sixty-four-bit id it names and recomputes every
// framing length, and the save resolves them back, so re-emitting into an id
// table ordered differently is still correct. Every field kind §6.6 retains —
// the scalars, strings, tables, arrays, unions and enums — is captured and
// re-emitted; every excluded class — a node index (kind 17) at any depth, an
// unknown enum variant in a known field, the reserved ids — is one retain_lost
// and never retained silently.
func generateRetainFiles(u *ir.Unit) (map[string][]byte, error) {
	tables := cardVariableTables(u)
	if len(tables) == 0 {
		return map[string][]byte{}, nil
	}
	out := map[string][]byte{
		"TableRetain.java": javaFile(u, "the retain-unknown storage and resolving walk (docs/SPEC-TABLES.md §6.6)", tableRetainRuntime(u)),
	}
	for _, st := range tables {
		src, err := generateTableRetain(u, st)
		if err != nil {
			return nil, err
		}
		out[st.Name+"Retain.java"] = javaFile(u,
			fmt.Sprintf("the retained load, measure and save for table %s (docs/SPEC-TABLES.md §6.6)", st.Name),
			src)
	}
	return out, nil
}

// generateTableRetain emits one <Type>Retain.java: a load that captures the
// unknown fields a read met and a measure/save that re-emits them as the body's
// retained tail. The known fields' read and write is the variable codec's own
// (docs/SPEC-TABLES.md §3), reused unchanged; the retain half adds only the
// unknown arm's capture and the tail's emit.
func generateTableRetain(u *ir.Unit, st *ir.Struct) (string, error) {
	st = variableStruct(st)
	var b strings.Builder
	b.WriteString("import java.nio.charset.StandardCharsets;\n\n")
	fmt.Fprintf(&b, "public final class %sRetain {\n", st.Name)
	fmt.Fprintf(&b, "    private %sRetain() {}\n\n", st.Name)

	// The enum readers the known-field read reuses (docs/SPEC-TABLES.md §3, §4):
	// a variant reference the table cannot name reads None (0) and counts it —
	// and in the retaining read that unknown variant is an excluded class, one
	// retain_lost (§6.6: the field is the reader's, so a retained copy would be
	// a second occurrence of an id the reader already wrote).
	for _, f := range st.Fields {
		if enum, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(&b, "    private static int %sEnum(long vid, TableVariable.Report report) {\n", varMember(f))
			for i := range enum.Variants {
				fmt.Fprintf(&b, "        if (vid == 0x%016xL) { return %d; }\n", ir.TableWireId(enum.VariantWireName(i)), i+1)
			}
			b.WriteString("        report.unknown++;\n        report.retainLost++;\n        return 0;\n    }\n\n")
		}
	}

	// loadRetain: the variable codec's load, plus the unknown arm's capture.
	fmt.Fprintf(&b, "    // loadRetain is %sVariable.load with the unknown arm capturing\n", st.Name)
	b.WriteString("    // (§6.6): a field this reader cannot name is resolved and copied into\n")
	b.WriteString("    // the caller's retain storage, and an excluded class is reported as\n")
	b.WriteString("    // loss, never retained silently.\n")
	fmt.Fprintf(&b, "    public static %sVariable.Value loadRetain(byte[] data, TableRetain retain, TableVariable.Report report) {\n", st.Name)
	b.WriteString("        if (retain == null) { return " + st.Name + "Variable.load(data, report); }\n")
	b.WriteString("        int[] path = new int[128];\n")
	fmt.Fprintf(&b, "        TableVariable.Reader r = TableVariable.open(data, report);\n")
	b.WriteString("        if (r == null) { return null; }\n")
	b.WriteString("        retain.used = 0; retain.idUsed = 0; retain.nodeKeys.clear();\n")
	fmt.Fprintf(&b, "        %sVariable.Value v = new %sVariable.Value();\n", st.Name, st.Name)
	if st.FixedDeclared {
		fmt.Fprintf(&b, "        readFieldsRetain(r, v, null, retain, report, path, 0);\n")
		b.WriteString("        if (report.refused) { return null; }\n")
		b.WriteString("        return v;\n")
		b.WriteString("    }\n\n")
	} else {
		fmt.Fprintf(&b, "        TableVariable.NodeTable nt = TableVariable.readNodeTable(r, report, true);\n")
		b.WriteString("        if (nt.recs != null && !nt.bad) {\n")
		b.WriteString("            for (TableVariable.NodeRecord rec : nt.recs) {\n")
		b.WriteString("                if (!rec.textMalformed && rec.typeId != TableVariable.STRING_ID && rec.typeId != TableVariable.BYTES_ID) {\n")
		fmt.Fprintf(&b, "                    rec.table = %sVariable.createNode(rec.typeId);\n", st.Name)
		b.WriteString("                    if (rec.table == null) { report.unknown++; report.retainLost++; rec.typeId = 0; }\n")
		b.WriteString("                }\n            }\n")
		// A NODE BODY RETAINS LIKE THE ROOT'S (§6.6: "in the root body and in
		// every node record"): its unknown fields are captured under the node's
		// own path, keyed by its index in the writer's node table.
		b.WriteString("            int[] np = new int[128];\n")
		b.WriteString("            for (int i = 0; i < nt.recs.length; i++) {\n")
		b.WriteString("                TableVariable.NodeRecord rec = nt.recs[i];\n")
		b.WriteString("                if (rec.table == null) { continue; }\n")
		b.WriteString("                retain.nodeKeys.put(rec.table, i);\n")
		b.WriteString("                np[0] = -(i + 1); np[1] = 0;\n")
		b.WriteString("                decodeNodeBodyRetain(new TableVariable.Reader(rec.body, r.ids, report), rec.table, rec.typeId, retain, report, np);\n")
		b.WriteString("            }\n        }\n")
		fmt.Fprintf(&b, "        readFieldsRetain(r, v, nt, retain, report, path, 0);\n")
		b.WriteString("        if (report.refused) { return null; }\n")
		// THE SAME FRAMING WALK the plain load runs after its read: a load refuses
		// exactly where loadMeasure answers -1 (docs/SPEC-TABLES.md §6.5), so a
		// container whose count its framing cannot cover is refused whole here
		// too, never retained around (§6.6).
		b.WriteString("        TableVariable.Measure measure = new TableVariable.Measure();\n")
		fmt.Fprintf(&b, "        if (%sVariable.loadMeasure(data, measure) < 0) {\n", st.Name)
		b.WriteString("            report.refused = true;\n")
		b.WriteString("            if (measure.report.reason != null) { report.reason = measure.report.reason; }\n")
		b.WriteString("            return null;\n")
		b.WriteString("        }\n")
		fmt.Fprintf(&b, "        %sVariable.resolveNodes(v, nt, report);\n", st.Name)
		b.WriteString("        if (nt.recs != null && !nt.bad) {\n")
		fmt.Fprintf(&b, "            for (TableVariable.NodeRecord rec : nt.recs) { if (rec.table != null) %sVariable.resolveNodePointers(rec.table, rec.typeId, nt, v, %sVariable.TYPE_ID, report); }\n", st.Name, st.Name)
		b.WriteString("        }\n")
		b.WriteString("        return v;\n")
		b.WriteString("    }\n\n")
	}

	// THE NODE BODY DISPATCH of the retaining pair: a node record's body read
	// and written through its own type's retaining walk at depth 1, under the
	// node's path (§6.6).
	b.WriteString("    static void decodeNodeBodyRetain(TableVariable.Reader r, Object val, long typeId, TableRetain retain, TableVariable.Report report, int[] path) {\n")
	for _, t := range cardVariableTables(u) {
		fmt.Fprintf(&b, "        if (typeId == %sVariable.TYPE_ID) { %sRetain.readFieldsRetain(r, (%sVariable.Value) val, null, retain, report, path, 1); return; }\n", t.Name, t.Name, t.Name)
	}
	b.WriteString("    }\n\n")
	b.WriteString("    static void writeNodeBodyRetain(TableVariable.Writer w, TableVariable.NodeGraph nodes, Object val, long typeId, TableRetain retain, TableVariable.Report report, int[] path) {\n")
	for _, t := range cardVariableTables(u) {
		fmt.Fprintf(&b, "        if (typeId == %sVariable.TYPE_ID) { %sRetain.writeFieldsRetain(w, nodes, (%sVariable.Value) val, retain, report, path, 1); emitTail(w, retain, report, path, 1); return; }\n", t.Name, t.Name, t.Name)
	}
	b.WriteString("    }\n\n")

	// readFieldsRetain: the variable readFields, with capture in the unknown arm
	// and the body's path threaded into list elements (§6.6).
	fmt.Fprintf(&b, "    static void readFieldsRetain(TableVariable.Reader r, %sVariable.Value v, TableVariable.NodeTable nt, TableRetain retain, TableVariable.Report report, int[] path, int depth) {\n", st.Name)
	b.WriteString("        while (true) {\n")
	b.WriteString("            long ref = r.leb();\n")
	b.WriteString("            if (r.fail) { return; }\n")
	b.WriteString("            if (ref == 0) { return; }\n")
	b.WriteString("            long id = r.resolve(ref);\n")
	b.WriteString("            if (r.fail) { return; }\n")
	b.WriteString("            if (!r.has(1)) { report.malformed = true; return; }\n")
	b.WriteString("            int kind = r.get8();\n")
	first := true
	for i, f := range st.Fields {
		if !first {
			b.WriteString("            } else ")
		} else {
			b.WriteString("            ")
		}
		first = false
		fmt.Fprintf(&b, "if (id == %sVariable.F_%s) {\n", st.Name, varMember(f))
		writeFieldRead(&b, u, st, f, true, i, false)
	}
	b.WriteString("            } else if (id == TableVariable.NODE_ID) {\n")
	if st.FixedDeclared {
		b.WriteString("                if (r.rootBody) { captureUnknown(r, id, kind, retain, report, path, depth); }\n")
	} else {
		b.WriteString("                if (nt != null && r.rootBody) { r.skip(kind); }\n")
	}
	b.WriteString("                else { report.malformed = true; return; }\n")
	b.WriteString("            } else if (id == TableVariable.BUILD_ID || id == TableVariable.VOCAB_ID) {\n")
	b.WriteString("                report.malformed = true; return;\n")
	b.WriteString("            } else {\n")
	b.WriteString("                captureUnknown(r, id, kind, retain, report, path, depth);\n")
	b.WriteString("            }\n")
	b.WriteString("            if (r.fail) { return; }\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")

	// captureUnknown: the plain read's skip delimits the payload, and the
	// resolving walk in TableRetain turns it into the resolved record, or an
	// excluded class into one retain_lost. The record carries the body's path so
	// the save re-emits it into the body it came from (§6.6).
	b.WriteString("    static void captureUnknown(TableVariable.Reader r, long id, int kind, TableRetain retain, TableVariable.Report report, int[] path, int depth) {\n")
	b.WriteString("        int pStart = r.off;\n")
	b.WriteString("        r.skip(kind);\n")
	b.WriteString("        report.unknown++;\n")
	b.WriteString("        if (r.fail) { return; }\n")
	b.WriteString("        int pEnd = r.off;\n")
	// A RETAINED UNKNOWN FLOAT IS TESTED LIKE A KNOWN ONE (docs/SPEC-TABLES.md
	// §3.3, §6.6, ruling 3 of 2026-10-27): kind 10's or 11's raw bits, and a
	// NaN or an infinity refuses the read as non_finite_float, named
	// <Table>.0x<id>, and is never retained.
	fmt.Fprintf(&b, "        if (kind == 10 && pEnd - pStart == 4 && (get32(r.buf, pStart) & 0x7f800000) == 0x7f800000 || kind == 11 && pEnd - pStart == 8 && (get64(r.buf, pStart) & 0x7ff0000000000000L) == 0x7ff0000000000000L) { TableVariable.nonFiniteFloat(report, TableVariable.retainedFloatName(%q, id)); return; }\n", st.DeclaredName())
	b.WriteString("        byte[] resolved = TableRetain.resolve(r.buf, r.ids, pStart, pEnd, kind);\n")
	b.WriteString("        if (resolved == null) { report.retainLost++; return; }\n")
	b.WriteString("        int total = 14 + 8 * depth + resolved.length;\n")
	b.WriteString("        if (retain.used + total > retain.capacity) { report.retainLost++; return; }\n")
	b.WriteString("        byte[] b = retain.bytes;\n")
	b.WriteString("        int at = retain.used;\n")
	b.WriteString("        put32(b, at, total); at += 4;\n")
	b.WriteString("        b[at++] = (byte) depth;\n")
	b.WriteString("        for (int k = 0; k < depth; k++) { put32(b, at, path[2 * k]); at += 4; put32(b, at, path[2 * k + 1]); at += 4; }\n")
	b.WriteString("        put64(b, at, id); at += 8;\n")
	b.WriteString("        b[at++] = (byte) kind;\n")
	b.WriteString("        System.arraycopy(resolved, 0, b, at, resolved.length);\n")
	b.WriteString("        retain.used += total;\n")
	b.WriteString("        report.retained++;\n")
	b.WriteString("    }\n\n")

	// measureRetain and saveRetain share the write walk; measure just sizes.
	fmt.Fprintf(&b, "    public static long measureRetain(%sVariable.Value v, TableRetain retain) {\n", st.Name)
	fmt.Fprintf(&b, "        byte[] buf = new byte[1 << 20];\n")
	b.WriteString("        int n = saveInto(v, retain, buf, null);\n")
	b.WriteString("        return n < 0 ? -1 : n;\n")
	b.WriteString("    }\n\n")

	fmt.Fprintf(&b, "    public static byte[] saveRetain(%sVariable.Value v, TableRetain retain, TableVariable.Report report) {\n", st.Name)
	b.WriteString("        if (report == null) { return null; }\n")
	b.WriteString("        byte[] buf = new byte[1 << 20];\n")
	b.WriteString("        int n = saveInto(v, retain, buf, report);\n")
	b.WriteString("        if (n < 0) { return null; }\n")
	b.WriteString("        return java.util.Arrays.copyOf(buf, n);\n")
	b.WriteString("    }\n\n")

	// saveInto: the variable save, with the retained tail appended at the end
	// of the body, before the node table (§6.6), ids interned in first-use order.
	fmt.Fprintf(&b, "    static int saveInto(%sVariable.Value v, TableRetain retain, byte[] buf, TableVariable.Report report) {\n", st.Name)
	b.WriteString("        int[] path = new int[128];\n")
	b.WriteString("        TableVariable.Ids ids = new TableVariable.Ids(64);\n")
	b.WriteString("        TableVariable.Writer w = new TableVariable.Writer(buf, ids);\n")
	b.WriteString("        TableVariable.NodeGraph nodes = new TableVariable.NodeGraph();\n")
	b.WriteString("        nodes.setOpen(v, true);\n")
	fmt.Fprintf(&b, "        if (!%sVariable.buildGraph(nodes, v)) return -1;\n", st.Name)
	b.WriteString("        nodes.setOpen(v, false);\n")
	b.WriteString("        w.put8(1);\n")
	b.WriteString("        writeFieldsRetain(w, nodes, v, retain, report, path, 0);\n")
	b.WriteString("        emitTail(w, retain, report, path, 0);\n")
	b.WriteString("        int nodeCount = nodes.nodeCount();\n")
	b.WriteString("        if (nodeCount > 0) {\n")
	b.WriteString("            long nodeRef = w.ref(TableVariable.NODE_ID);\n")
	b.WriteString("            int table = w.off;\n")
	b.WriteString("            w.putLeb(nodeCount);\n")
	b.WriteString("            for (int i = 0; i < nodeCount; i++) {\n")
	b.WriteString("                Object value = nodes.nodeValue(i);\n")
	b.WriteString("                long typeId = nodes.nodeType(i);\n")
	b.WriteString("                w.putLeb(w.ref(typeId));\n")
	b.WriteString("                if (nodes.nodeBlob(i)) {\n")
	b.WriteString("                    if (value instanceof String) {\n")
	b.WriteString("                        w.putUtf8((String) value);\n")
	b.WriteString("                    } else {\n")
	b.WriteString("                        byte[] bytes = (byte[]) value;\n")
	b.WriteString("                        w.putLeb(bytes.length);\n")
	b.WriteString("                        w.putBytes(bytes, 0, bytes.length);\n")
	b.WriteString("                    }\n")
	b.WriteString("                } else {\n")
	b.WriteString("                    int body = w.off;\n")
	b.WriteString("                    Integer key = retain == null ? null : retain.nodeKeys.get(value);\n")
	b.WriteString("                    if (key != null) { path[0] = -(key + 1); path[1] = 0; writeNodeBodyRetain(w, nodes, value, typeId, retain, report, path); }\n")
	fmt.Fprintf(&b, "                    else { %sVariable.writeNodeBody(w, nodes, value, typeId); }\n", st.Name)
	b.WriteString("                    w.put8(0);\n")
	b.WriteString("                    w.frame(body);\n")
	b.WriteString("                }\n")
	b.WriteString("            }\n")
	b.WriteString("            w.frameField(table, nodeRef, 12);\n")
	b.WriteString("        }\n")
	b.WriteString("        w.put8(0);\n")
	b.WriteString("        w.trailer();\n")
	// a NaN or an infinity refuses the save (docs/SPEC-TABLES.md §3.3)
	b.WriteString("        return w.overflow || ids.nonFinite ? -1 : w.off;\n")
	b.WriteString("    }\n\n")

	// writeFieldsRetain: the canonical write, threading the path into list
	// elements so each element's body carries its own retained tail (§6.6).
	fmt.Fprintf(&b, "    static void writeFieldsRetain(TableVariable.Writer w, TableVariable.NodeGraph nodes, %sVariable.Value v, TableRetain retain, TableVariable.Report report, int[] path, int depth) {\n", st.Name)
	b.WriteString("        TableVariable.NodeGraph graph = nodes;\n")
	for i, f := range st.Fields {
		open := openGuard(&b, st, f)
		writeFieldSaveRetain(&b, u, st, f, i)
		closeGuard(&b, open)
	}
	b.WriteString("    }\n\n")

	// writeBodyRetain: a body is its declared fields, then the retained tail for
	// THIS body, then the terminator (§6.6: the tail at the END of its own body).
	fmt.Fprintf(&b, "    static void writeBodyRetain(TableVariable.Writer w, TableVariable.NodeGraph nodes, %sVariable.Value v, TableRetain retain, TableVariable.Report report, int[] path, int depth) {\n", st.Name)
	b.WriteString("        writeFieldsRetain(w, nodes, v, retain, report, path, depth);\n")
	b.WriteString("        emitTail(w, retain, report, path, depth);\n")
	b.WriteString("        w.put8(0);\n")
	b.WriteString("    }\n\n")

	// emitTail: re-emit each retained record of THIS body in retention order,
	// resolving its payload back onto the wire. A record's path picks its body;
	// one is built into scratch and committed whole, so a record that goes bad
	// partway rolls its ids back and counts once, exactly as the reference's
	// tail does (§6.6).
	b.WriteString("    static void emitTail(TableVariable.Writer w, TableRetain retain, TableVariable.Report report, int[] path, int depth) {\n")
	b.WriteString("        if (retain == null) { return; }\n")
	b.WriteString("        retain.idUsed = 0;\n")
	b.WriteString("        int at = 0;\n")
	b.WriteString("        while (at < retain.used) {\n")
	b.WriteString("            int len = get32(retain.bytes, at);\n")
	b.WriteString("            int d = retain.bytes[at + 4] & 0xff;\n")
	b.WriteString("            boolean match = d == depth;\n")
	b.WriteString("            for (int k = 0; match && k < d; k++) { if (get32(retain.bytes, at + 5 + 8 * k) != path[2 * k] || get32(retain.bytes, at + 9 + 8 * k) != path[2 * k + 1]) { match = false; } }\n")
	b.WriteString("            if (match) {\n")
	b.WriteString("                int hdr = 14 + 8 * d;\n")
	b.WriteString("                long id = get64(retain.bytes, at + 5 + 8 * d);\n")
	b.WriteString("                int kind = retain.bytes[at + 13 + 8 * d] & 0xff;\n")
	b.WriteString("                int idsMark = w.ids.count;\n")
	b.WriteString("                int listMark = retain.idUsed;\n")
	b.WriteString("                TableVariable.Writer rec = w.sub(new byte[1 << 20]);\n")
	b.WriteString("                int[] pat = new int[]{at + hdr};\n")
	b.WriteString("                // the record is ONE loss however deep it fails, counted once\n")
	b.WriteString("                // below, so the walk itself is handed no report (§6.6)\n")
	b.WriteString("                long ref = TableRetain.ref(rec, retain, id, null);\n")
	b.WriteString("                boolean ok = ref != 0;\n")
	b.WriteString("                if (ok) {\n")
	b.WriteString("                    rec.putLeb(ref);\n")
	b.WriteString("                    rec.put8(kind);\n")
	b.WriteString("                    ok = TableRetain.emit(rec, retain, null, retain.bytes, pat, kind);\n")
	b.WriteString("                }\n")
	b.WriteString("                if (ok) { w.putBytes(rec.buf, 0, rec.off); }\n")
	b.WriteString("                else {\n")
	b.WriteString("                    w.ids.count = idsMark;\n")
	b.WriteString("                    w.ids.overflow = false;\n")
	b.WriteString("                    retain.idUsed = listMark;\n")
	b.WriteString("                    if (report != null) { report.retainLost++; }\n")
	b.WriteString("                }\n")
	b.WriteString("            }\n")
	b.WriteString("            at += len;\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")

	// The little-endian record header primitives.
	b.WriteString("    static void put32(byte[] b, int at, int v) { for (int i = 0; i < 4; i++) { b[at + i] = (byte) (v >>> (8 * i)); } }\n")
	b.WriteString("    static void put64(byte[] b, int at, long v) { for (int i = 0; i < 8; i++) { b[at + i] = (byte) (v >>> (8 * i)); } }\n")
	b.WriteString("    static int get32(byte[] b, int at) { return (b[at] & 0xff) | (b[at + 1] & 0xff) << 8 | (b[at + 2] & 0xff) << 16 | (b[at + 3] & 0xff) << 24; }\n")
	b.WriteString("    static long get64(byte[] b, int at) { long v = 0; for (int i = 0; i < 8; i++) { v |= (long) (b[at + i] & 0xff) << (8 * i); } return v; }\n\n")
	b.WriteString("}\n")
	source := b.String()
	if st.IsMapEntry() {
		for _, signature := range []string{st.Name + "Variable.Value loadRetain(", "long measureRetain(", "byte[] saveRetain("} {
			source = strings.ReplaceAll(source, "public static "+signature, "static "+signature)
		}
	}
	return source, nil
}

// writeFieldSaveRetain emits the canonical write of one field for the retaining
// save. It is writeFieldSave with the field's id spelled through the VARIABLE
// class (the retain class defines no field-id constants of its own) and, for a
// list field, the element bodies threaded with the retain path so each carries
// its own retained tail (docs/SPEC-TABLES.md §6.6).
func writeFieldSaveRetain(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field, ordinal int) {
	if f.Type.Optional {
		fmt.Fprintf(b, "        if(v.%sPresent){w.putLeb(w.ref(%sVariable.F_%s));w.put8(%d);\n", varMember(f), st.Name, varMember(f), variableWireKind(f))
		one := *f
		one.Type.Optional = false
		fmt.Fprintf(b, "path[2*depth]=%d;path[2*depth+1]=0;\n", ordinal)
		writeVariableRetainedPayload(b, u, st.Name, &one, "v."+varMember(f), "w", 0, "depth+1")
		b.WriteString("        }\n")
		return
	}
	if f.KeyEnum != "" {
		fmt.Fprintf(b, "        {int mark=w.ids.count;long ref=w.ref(%sVariable.F_%s);TableVariable.Writer body=w.sub(new byte[1<<20]);path[2*depth]=%d;path[2*depth+1]=0;int count=%sVariable.writeKeyedPayloadRetain_%s(body,nodes,v.%s,retain,report,path,depth+1);if(count>0){w.putLeb(ref);w.put8(16);w.putBytes(body.buf,0,body.off);}else w.ids.count=mark;}\n", st.Name, varMember(f), ordinal, st.Name, varMember(f), varMember(f))
		return
	}
	if variableUnion(f) != nil {
		condition := "v." + varMember(f) + ".tag!=0"
		if f.Array != ir.ArrayNone {
			condition = "!v." + varMember(f) + ".isEmpty()"
		}
		fmt.Fprintf(b, "if(%s){w.putLeb(w.ref(%sVariable.F_%s));w.put8(%d);path[2*depth]=%d;path[2*depth+1]=0;\n", condition, st.Name, varMember(f), variableWireKind(f), ordinal)
		writeVariableRetainedPayload(b, u, st.Name, f, "v."+varMember(f), "w", 0, "depth+1")
		b.WriteString("}\n")
		return
	}
	if variableWide(f) && f.Array == ir.ArrayNone {
		var body strings.Builder
		writeFieldSave(&body, u, st, f)
		b.WriteString(strings.ReplaceAll(body.String(), "F_"+varMember(f), st.Name+"Variable.F_"+varMember(f)))
		return
	}
	m := varMember(f)
	fieldRef := fmt.Sprintf("%sVariable.F_%s", st.Name, m)
	// A POINTER, alone or in an array, is a node index (kind 17) into the
	// node table the save builds from the same graph, so its write is the
	// variable codec's own; the pointee's body rides in that table.
	if isStruct(f) && f.Type.Pointer {
		var body strings.Builder
		writeFieldSave(&body, u, st, f)
		b.WriteString(strings.ReplaceAll(body.String(), "F_"+m, fieldRef))
		return
	}
	// A SCALAR LEAF is the variable codec's own write, which elides the
	// DECLARED default (docs/SPEC-TABLES.md §4.2): a retaining save that tested
	// against zero instead wrote `build int32 = 1` at 1 where the engine elides it.
	_, isEnum := f.Type.Ref.(*ir.Enum)
	// a FLAGS field is a scalar leaf too: the switch below has no arm for it,
	// so a retaining save that fell through wrote nothing for a set of flags
	// off its declared default
	_, isFlags := f.Type.Ref.(*ir.Flags)
	scalar := f.Array == ir.ArrayNone && (f.Type.Kind == ir.TBool || f.Type.Kind == ir.TInt || isEnum || isFlags ||
		(f.Type.Kind == ir.TString && !f.Type.Blob()))
	if scalar || (!isStruct(f) && f.Type.Kind != ir.TNamed && f.Type.Kind != ir.TString && f.Type.Kind != ir.TWString && f.Type.Kind != ir.TBool && f.Type.Kind != ir.TInt && !isScalarArray(f)) {
		var body strings.Builder
		writeFieldSave(&body, u, st, f)
		b.WriteString(strings.ReplaceAll(body.String(), "F_"+m, fieldRef))
		return
	}
	switch {
	case isStruct(f) && f.Array == ir.ArrayNone && !f.Type.Pointer:
		fmt.Fprintf(b, "        if(v.%s!=null){int mark=w.ids.count;long ref=w.ref(%s);TableVariable.Writer sw=w.sub(new byte[1<<20]);path[2*depth]=%d;path[2*depth+1]=0;%sRetain.writeBodyRetain(sw,nodes,v.%s,retain,report,path,depth+1);if(sw.off>1){w.putLeb(ref);w.put8(13);w.putLeb(sw.off);w.putBytes(sw.buf,0,sw.off);}else w.ids.count=mark;}\n", m, fieldRef, ordinal, f.Type.Name, m)
	case f.Type.Kind == ir.TWString:
		fmt.Fprintf(b, "        if (v.%s.length != 0) { w.putLeb(w.ref(%s)); w.put8(33); w.putLeb(v.%s.length * 2L); for (char c : v.%s) { w.put8(c); w.put8(c >>> 8); } }\n", m, fieldRef, m, m)
	case isScalarArray(f):
		writeArrayFieldSaveRef(b, f, fieldRef)
	case f.Type.Kind == ir.TString && f.Type.Blob():
		fmt.Fprintf(b, "        if (v.%s != null) { w.putLeb(w.ref(%s)); w.put8(17); w.putLeb(nodes.index(v.%s)); }\n", m, fieldRef, m)
	case f.Type.Kind == ir.TNamed:
		if elem, ok := f.Type.Ref.(*ir.Struct); ok && f.Array != ir.ArrayNone {
			// EVERY ARRAY OF TABLES — unbounded, counted or fixed — is kind 14
			// over element bodies, and each element body carries its own
			// retained tail (§6.6). A fixed array is written whole, as the
			// variable codec writes it; the others only when they hold one.
			if f.Array == ir.ArrayFixed {
				b.WriteString("        {\n")
			} else {
				fmt.Fprintf(b, "        if (!v.%s.isEmpty()) {\n", m)
			}
			fmt.Fprintf(b, "            w.putLeb(w.ref(%s)); w.put8(14);\n", fieldRef)
			// written IN PLACE and framed after, as the variable codec writes it
			// (no scratch per array, none per element: a 1 MiB scratch per element
			// made the retaining save of eighty-eight bodies 60 times slower)
			b.WriteString("            int list = w.off;\n")
			fmt.Fprintf(b, "            w.put8(13); w.putLeb(v.%s.size());\n", m)
			fmt.Fprintf(b, "            for (int i = 0; i < v.%s.size(); i++) {\n", m)
			fmt.Fprintf(b, "                %sVariable.Value e = v.%s.get(i);\n", elem.Name, m)
			b.WriteString("                int body = w.off;\n")
			fmt.Fprintf(b, "                path[2 * depth] = %d; path[2 * depth + 1] = i;\n", ordinal)
			fmt.Fprintf(b, "                %sRetain.writeBodyRetain(w, nodes, e, retain, report, path, depth + 1);\n", elem.Name)
			b.WriteString("                w.frame(body);\n")
			b.WriteString("            }\n")
			b.WriteString("            w.frame(list);\n")
			b.WriteString("        }\n")
		}
	}
}

// tableRetainRuntime returns the shared retain runtime for one unit, with the
// KNOWN id set the save-side interner uses to tell a nameable id from a
// retained one (docs/SPEC-TABLES.md §6.6).
func tableRetainRuntime(u *ir.Unit) string {
	known := ir.TableWireIds(u)
	var ids strings.Builder
	for i, id := range known {
		if i > 0 {
			ids.WriteString(", ")
		}
		fmt.Fprintf(&ids, "0x%016xL", id)
	}
	return strings.ReplaceAll(tableRetainRuntimeBody, "/*KNOWN*/", ids.String())
}

const tableRetainRuntimeBody = `// The retain-unknown storage and resolving walk (docs/SPEC-TABLES.md §6.6): a
// caller-owned byte buffer and a caller-owned id list, never grown on demand,
// plus the two walks that move a field each way. The report counters —
// retained and retain_lost — ride TableVariable.Report and accumulate across
// the load and the save.
public final class TableRetain {
    public byte[] bytes;
    public int capacity;
    public long[] ids;
    public int idCapacity;
    public int used;
    public int idUsed;
    // THE NODE RECORDS a retaining load read, by the record's index in the
    // writer's node table: a node body's retained fields carry the path
    // (-(index + 1), 0), and the save finds a node's index by its object, since
    // the save renumbers the graph from its own edges (§3.1, §6.6). Reset by
    // every retaining load.
    final java.util.IdentityHashMap<Object, Integer> nodeKeys = new java.util.IdentityHashMap<>();

    // §6.6: an accepted replacement discards the old body's retained tail,
    // including descendants, without moving either cumulative report counter.
    void discardField(int[] path, int depth, int field) {
        int write = 0;
        for (int at = 0; at < used; ) {
            int len =
                    (bytes[at] & 255)
                            | ((bytes[at + 1] & 255) << 8)
                            | ((bytes[at + 2] & 255) << 16)
                            | ((bytes[at + 3] & 255) << 24);
            int d = bytes[at + 4] & 255;
            boolean match = d > depth;
            for (int k = 0; match && k < depth; k++) {
                int p = at + 5 + 8 * k;
                int f =
                        (bytes[p] & 255)
                                | ((bytes[p + 1] & 255) << 8)
                                | ((bytes[p + 2] & 255) << 16)
                                | ((bytes[p + 3] & 255) << 24);
                int i =
                        (bytes[p + 4] & 255)
                                | ((bytes[p + 5] & 255) << 8)
                                | ((bytes[p + 6] & 255) << 16)
                                | ((bytes[p + 7] & 255) << 24);
                match = f == path[2 * k] && i == path[2 * k + 1];
            }
            if (match) {
                int p = at + 5 + 8 * depth;
                int f =
                        (bytes[p] & 255)
                                | ((bytes[p + 1] & 255) << 8)
                                | ((bytes[p + 2] & 255) << 16)
                                | ((bytes[p + 3] & 255) << 24);
                match = f == field;
            }
            if (!match) {
                System.arraycopy(bytes, at, bytes, write, len);
                write += len;
            }
            at += len;
        }
        used = write;
    }

    // A winning duplicate map key replaces the entry itself and every child.
    void discardPath(int[] path, int depth) {
        int write = 0;
        for (int at = 0; at < used; ) {
            int len =
                    (bytes[at] & 255)
                            | ((bytes[at + 1] & 255) << 8)
                            | ((bytes[at + 2] & 255) << 16)
                            | ((bytes[at + 3] & 255) << 24);
            int d = bytes[at + 4] & 255;
            boolean match = d >= depth;
            for (int k = 0; match && k < depth; k++) {
                int p = at + 5 + 8 * k;
                int f =
                        (bytes[p] & 255)
                                | ((bytes[p + 1] & 255) << 8)
                                | ((bytes[p + 2] & 255) << 16)
                                | ((bytes[p + 3] & 255) << 24);
                int i =
                        (bytes[p + 4] & 255)
                                | ((bytes[p + 5] & 255) << 8)
                                | ((bytes[p + 6] & 255) << 16)
                                | ((bytes[p + 7] & 255) << 24);
                match = f == path[2 * k] && i == path[2 * k + 1];
            }
            if (!match) {
                System.arraycopy(bytes, at, bytes, write, len);
                write += len;
            }
            at += len;
        }
        used = write;
    }

    public TableRetain(int byteCapacity, int idCapacity) {
        this.bytes = new byte[byteCapacity];
        this.capacity = byteCapacity;
        this.ids = new long[idCapacity];
        this.idCapacity = idCapacity;
    }

    // The ids this build can name (docs/SPEC-TABLES.md §6.6), ascending: every
    // field id, enum variant id, union arm id and reserved id the unit's
    // closure can spell. A resolved id inside a retained record takes its
    // trailer entry from the generated table when it is here, and from the
    // caller's list otherwise.
    static final long[] KNOWN = {
        /*KNOWN*/
    };

    static boolean nameable(long id) {
        int lo = 0, hi = KNOWN.length - 1;
        while (lo <= hi) {
            int m = (lo + hi) >>> 1;
            long k = KNOWN[m];
            if (k == id) {
                return true;
            }
            if (k < id) {
                lo = m + 1;
            } else {
                hi = m - 1;
            }
        }
        return false;
    }

    // ref interns one resolved id into the save's one numbering (§6.6): a
    // nameable id takes the generated table, any other takes the caller's list
    // first, one retainLost past its capacity. Answers the reference, 0 when a
    // store is full.
    static long ref(
            TableVariable.Writer w, TableRetain retain, long id, TableVariable.Report report) {
        if (!nameable(id)) {
            boolean seen = false;
            for (int i = 0; i < retain.idUsed; i++) {
                if (retain.ids[i] == id) {
                    seen = true;
                    break;
                }
            }
            if (!seen) {
                if (retain.idUsed >= retain.idCapacity) {
                    if (report != null) {
                        report.retainLost++;
                    }
                    return 0;
                }
                retain.ids[retain.idUsed++] = id;
            }
        }
        long r = w.ref(id);
        if (w.ids.overflow) {
            if (report != null) {
                report.retainLost++;
            }
            return 0;
        }
        return r;
    }

    // A resolving cursor over one self-framed payload (§6.6). The walk's own
    // damage is a dropped record and never malformed, so it reads the bytes
    // directly and touches no report; the outer framing was validated by the
    // plain read's skip before the walk began.
    static final class Resolver {
        final byte[] buf;
        final long[] ids;
        int off;
        final int limit;
        boolean bad;

        Resolver(byte[] buf, long[] ids, int off, int limit) {
            this.buf = buf;
            this.ids = ids;
            this.off = off;
            this.limit = limit;
        }

        long leb() {
            long v = 0;
            int shift = 0;
            for (int i = 0; i < 10; i++) {
                if (off + i >= limit) {
                    bad = true;
                    return 0;
                }
                int b = buf[off + i] & 0xff;
                if (i == 9 && b > 1) {
                    bad = true;
                    return 0;
                }
                v |= (long) (b & 0x7f) << shift;
                if ((b & 0x80) == 0) {
                    if (i > 0 && b == 0) {
                        bad = true;
                        return 0;
                    }
                    off += i + 1;
                    return v;
                }
                shift += 7;
            }
            bad = true;
            return 0;
        }

        int u8() {
            if (off >= limit) {
                bad = true;
                return 0;
            }
            return buf[off++] & 0xff;
        }

        long id(long ref) {
            if (ref <= 0 || ref > ids.length) {
                bad = true;
                return 0;
            }
            long v = ids[(int) ref - 1];
            if (v == 0
                    || v == TableVariable.NODE_ID
                    || v == TableVariable.BUILD_ID
                    || v == TableVariable.VOCAB_ID) {
                bad = true;
                return 0;
            }
            return v;
        }
    }

    // resolve reads one payload the plain read already delimited and writes its
    // RESOLVED form — every reference widened to eight bytes, every framing
    // length recomputed. null is an excluded class or a shape the walk cannot
    // take (§6.6).
    static byte[] resolve(byte[] buf, long[] ids, int off, int limit, int kind) {
        Resolver r = new Resolver(buf, ids, off, limit);
        byte[] out = new byte[1 << 20];
        int[] o = new int[] {0};
        if (!resolvePayload(r, kind, out, o, 0)) {
            return null;
        }
        if (r.off != r.limit || r.bad) {
            return null;
        }
        return java.util.Arrays.copyOf(out, o[0]);
    }

    static boolean resolvePayload(Resolver r, int kind, byte[] out, int[] o, int depth) {
        if (depth > 64 || r.bad) {
            r.bad = true;
            return false;
        }
        switch (kind) {
            case 17:
                return false;
            case 30:
                {
                    long ref = r.leb();
                    if (r.bad) {
                        return false;
                    }
                    if (ref == 0) {
                        out64(out, o, 0L);
                        return true;
                    }
                    long id = r.id(ref);
                    if (r.bad) {
                        return false;
                    }
                    out64(out, o, id);
                    return true;
                }
            case 15:
                {
                    long arm = r.leb();
                    if (r.bad) {
                        return false;
                    }
                    if (arm == 0) {
                        out64(out, o, 0L);
                        return true;
                    }
                    long id = r.id(arm);
                    if (r.bad) {
                        return false;
                    }
                    out64(out, o, id);
                    int armKind = r.u8();
                    if (r.bad) {
                        return false;
                    }
                    out8(out, o, armKind);
                    return resolveFramed(r, armKind, out, o, depth);
                }
            case 13:
            case 14:
            case 16:
                return resolveFramed(r, kind, out, o, depth);
            default:
                {
                    int from = r.off;
                    if (!skipPayload(r, kind)) {
                        return false;
                    }
                    int n = r.off - from;
                    if (o[0] + n > out.length) {
                        r.bad = true;
                        return false;
                    }
                    System.arraycopy(r.buf, from, out, o[0], n);
                    o[0] += n;
                    return true;
                }
        }
    }

    static boolean resolveFramed(Resolver r, int kind, byte[] out, int[] o, int depth) {
        long n = r.leb();
        if (r.bad || n < 0 || n > r.limit - r.off) {
            r.bad = true;
            return false;
        }
        int at = o[0];
        out64(out, o, 0L);
        Resolver sub = new Resolver(r.buf, r.ids, r.off, r.off + (int) n);
        r.off += (int) n;
        if (!resolveContent(sub, kind, out, o, depth + 1)) {
            return false;
        }
        if (sub.off != sub.limit || sub.bad) {
            r.bad = true;
            return false;
        }
        put64At(out, at, o[0] - at - 8);
        return true;
    }

    static boolean resolveContent(Resolver c, int kind, byte[] out, int[] o, int depth) {
        if (depth > 64 || c.bad) {
            c.bad = true;
            return false;
        }
        switch (kind) {
            case 17:
                return false;
            case 13:
                return resolveBody(c, out, o, depth);
            case 14:
            case 16:
                {
                    int ek = c.u8();
                    if (c.bad) {
                        return false;
                    }
                    if (ek == 17) {
                        return false;
                    }
                    out8(out, o, ek);
                    long count = c.leb();
                    if (c.bad) {
                        return false;
                    }
                    out64(out, o, count);
                    for (long i = 0; i < count; i++) {
                        if (c.bad) {
                            return false;
                        }
                        if (kind == 16) {
                            long keyRef = c.leb();
                            if (c.bad) {
                                return false;
                            }
                            long keyId = c.id(keyRef);
                            if (c.bad) {
                                return false;
                            }
                            out64(out, o, keyId);
                            if (!resolveFramed(c, ek, out, o, depth)) {
                                return false;
                            }
                        } else if (!resolvePayload(c, ek, out, o, depth)) {
                            return false;
                        }
                        if (c.off > c.limit) {
                            c.bad = true;
                            return false;
                        }
                    }
                    return true;
                }
            case 15:
            case 30:
                return resolvePayload(c, kind, out, o, depth);
            default:
                {
                    int n = c.limit - c.off;
                    if (o[0] + n > out.length) {
                        c.bad = true;
                        return false;
                    }
                    System.arraycopy(c.buf, c.off, out, o[0], n);
                    o[0] += n;
                    c.off = c.limit;
                    return true;
                }
        }
    }

    static boolean resolveBody(Resolver c, byte[] out, int[] o, int depth) {
        while (true) {
            if (c.bad) {
                return false;
            }
            long ref = c.leb();
            if (c.bad) {
                return false;
            }
            if (ref == 0) {
                out64(out, o, 0L);
                return true;
            }
            long id = c.id(ref);
            if (c.bad) {
                return false;
            }
            out64(out, o, id);
            int kind = c.u8();
            if (c.bad) {
                return false;
            }
            out8(out, o, kind);
            if (!resolvePayload(c, kind, out, o, depth)) {
                return false;
            }
            if (c.off > c.limit) {
                c.bad = true;
                return false;
            }
        }
    }

    // skipPayload steps one payload by its framing (§3), the same four rules the
    // plain read's skip takes; false for a kind the walk does not copy.
    static boolean skipPayload(Resolver r, int kind) {
        switch (kind) {
            case 1:
            case 2:
            case 6:
            case 20:
            case 25:
                if (r.limit - r.off >= 1) {
                    r.off += 1;
                    return true;
                }
                break;
            case 3:
            case 7:
            case 21:
            case 26:
                if (r.limit - r.off >= 2) {
                    r.off += 2;
                    return true;
                }
                break;
            case 4:
            case 8:
            case 10:
            case 22:
            case 27:
                if (r.limit - r.off >= 4) {
                    r.off += 4;
                    return true;
                }
                break;
            case 5:
            case 9:
            case 11:
            case 23:
            case 28:
            case 36:
                if (r.limit - r.off >= 8) {
                    r.off += 8;
                    return true;
                }
                break;
            case 18:
            case 19:
            case 24:
            case 29:
                if (r.limit - r.off >= 16) {
                    r.off += 16;
                    return true;
                }
                break;
            case 12:
            case 31:
            case 32:
            case 33:
                {
                    long n = r.leb();
                    if (!r.bad && n >= 0 && n <= r.limit - r.off) {
                        r.off += (int) n;
                        return true;
                    }
                    break;
                }
        }
        r.bad = true;
        return false;
    }

    static void out64(byte[] out, int[] o, long v) {
        for (int i = 0; i < 8; i++) {
            out[o[0] + i] = (byte) (v >>> (8 * i));
        }
        o[0] += 8;
    }

    static void out8(byte[] out, int[] o, int v) {
        out[o[0]++] = (byte) v;
    }

    static void put64At(byte[] out, int at, long v) {
        for (int i = 0; i < 8; i++) {
            out[at + i] = (byte) (v >>> (8 * i));
        }
    }

    // THE RESOLVING WALK, emit half (§6.6): read one resolved payload and write
    // its wire form, re-interning every id into this file's one table.
    static boolean emit(
            TableVariable.Writer w,
            TableRetain retain,
            TableVariable.Report report,
            byte[] b,
            int[] at,
            int kind) {
        return emitPayload(w, retain, report, b, at, kind);
    }

    static boolean emitPayload(
            TableVariable.Writer w,
            TableRetain retain,
            TableVariable.Report report,
            byte[] b,
            int[] at,
            int kind) {
        switch (kind) {
            case 30:
                {
                    long id = get64(b, at);
                    if (id == 0) {
                        w.putLeb(0);
                        return true;
                    }
                    long ref = ref(w, retain, id, report);
                    if (ref == 0) {
                        return false;
                    }
                    w.putLeb(ref);
                    return true;
                }
            case 15:
                {
                    long arm = get64(b, at);
                    if (arm == 0) {
                        w.putLeb(0);
                        return true;
                    }
                    long ref = ref(w, retain, arm, report);
                    if (ref == 0) {
                        return false;
                    }
                    w.putLeb(ref);
                    int armKind = get8(b, at);
                    w.put8(armKind);
                    return emitFramed(w, retain, report, b, at, armKind);
                }
            case 13:
            case 14:
            case 16:
                return emitFramed(w, retain, report, b, at, kind);
            default:
                return emitVerbatim(w, b, at, kind);
        }
    }

    static boolean emitFramed(
            TableVariable.Writer w,
            TableRetain retain,
            TableVariable.Report report,
            byte[] b,
            int[] at,
            int kind) {
        long contentLen = get64(b, at);
        if (contentLen < 0 || contentLen > b.length - at[0]) {
            return false;
        }
        int end = at[0] + (int) contentLen;
        TableVariable.Writer sub = w.sub(new byte[1 << 20]);
        if (!emitContent(sub, retain, report, b, at, end, kind)) {
            return false;
        }
        if (at[0] != end) {
            return false;
        }
        w.putLeb(sub.off);
        w.putBytes(sub.buf, 0, sub.off);
        return true;
    }

    static boolean emitContent(
            TableVariable.Writer w,
            TableRetain retain,
            TableVariable.Report report,
            byte[] b,
            int[] at,
            int end,
            int kind) {
        switch (kind) {
            case 13:
                return emitBody(w, retain, report, b, at, end);
            case 14:
            case 16:
                {
                    int ek = get8(b, at);
                    w.put8(ek);
                    long count = get64(b, at);
                    w.putLeb(count);
                    for (long i = 0; i < count; i++) {
                        if (kind == 16) {
                            long keyId = get64(b, at);
                            long keyRef = ref(w, retain, keyId, report);
                            if (keyRef == 0) {
                                return false;
                            }
                            w.putLeb(keyRef);
                            if (!emitFramed(w, retain, report, b, at, ek)) {
                                return false;
                            }
                        } else if (!emitPayload(w, retain, report, b, at, ek)) {
                            return false;
                        }
                        if (at[0] > end) {
                            return false;
                        }
                    }
                    return at[0] == end;
                }
            case 15:
            case 30:
                return emitPayload(w, retain, report, b, at, kind);
            default:
                {
                    w.putBytes(b, at[0], end - at[0]);
                    at[0] = end;
                    return true;
                }
        }
    }

    static boolean emitBody(
            TableVariable.Writer w,
            TableRetain retain,
            TableVariable.Report report,
            byte[] b,
            int[] at,
            int end) {
        while (true) {
            long id = get64(b, at);
            if (id == 0) {
                w.put8(0);
                return true;
            }
            long ref = ref(w, retain, id, report);
            if (ref == 0) {
                return false;
            }
            w.putLeb(ref);
            int kind = get8(b, at);
            w.put8(kind);
            if (!emitPayload(w, retain, report, b, at, kind)) {
                return false;
            }
            if (at[0] > end) {
                return false;
            }
        }
    }

    static boolean emitVerbatim(TableVariable.Writer w, byte[] b, int[] at, int kind) {
        switch (kind) {
            case 1:
            case 2:
            case 6:
            case 20:
            case 25:
                w.putBytes(b, at[0], 1);
                at[0] += 1;
                return true;
            case 3:
            case 7:
            case 21:
            case 26:
                w.putBytes(b, at[0], 2);
                at[0] += 2;
                return true;
            case 4:
            case 8:
            case 10:
            case 22:
            case 27:
                w.putBytes(b, at[0], 4);
                at[0] += 4;
                return true;
            case 5:
            case 9:
            case 11:
            case 23:
            case 28:
            case 36:
                w.putBytes(b, at[0], 8);
                at[0] += 8;
                return true;
            case 18:
            case 19:
            case 24:
            case 29:
                w.putBytes(b, at[0], 16);
                at[0] += 16;
                return true;
            case 12:
            case 31:
            case 32:
            case 33:
                {
                    long n = getLeb(b, at);
                    if (n < 0) {
                        return false;
                    }
                    w.putLeb(n);
                    w.putBytes(b, at[0], (int) n);
                    at[0] += (int) n;
                    return true;
                }
        }
        return false;
    }

    static long get64(byte[] b, int[] at) {
        long v = 0;
        for (int i = 0; i < 8; i++) {
            v |= (long) (b[at[0] + i] & 0xff) << (8 * i);
        }
        at[0] += 8;
        return v;
    }

    static int get8(byte[] b, int[] at) {
        return b[at[0]++] & 0xff;
    }

    static long getLeb(byte[] b, int[] at) {
        long v = 0;
        int shift = 0;
        for (int i = 0; i < 10; i++) {
            if (at[0] + i >= b.length) {
                return -1;
            }
            int x = b[at[0] + i] & 0xff;
            if (i == 9 && x > 1) {
                return -1;
            }
            v |= (long) (x & 0x7f) << shift;
            if ((x & 0x80) == 0) {
                if (i > 0 && x == 0) {
                    return -1;
                }
                at[0] += i + 1;
                return v;
            }
            shift += 7;
        }
        return -1;
    }
}
`
