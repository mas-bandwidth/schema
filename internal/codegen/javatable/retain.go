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
// The card's unknown fields are SCALAR (bool, int8..int64) and STRING (kind 12)
// values: both are copied VERBATIM by §6.6's resolving walk, because neither
// carries a reference to resolve. The exclusions a card wire can reach — a node
// index (kind 17, the *string pointer) and an unknown enum variant (kind 30) —
// count one retain_lost each and are never retained.
func generateRetainFiles(u *ir.Unit) (map[string][]byte, error) {
	tables := cardVariableTables(u)
	if len(tables) == 0 {
		return map[string][]byte{}, nil
	}
	out := map[string][]byte{
		"TableRetain.java": javaFile(u, "the retain-unknown storage and walk (docs/SPEC-TABLES.md §6.6)", tableRetainRuntime),
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
// unknown scalar and string fields a read met, and a measure/save that re-emits
// them as the body's retained tail. The known fields' read and write is the
// variable codec's own (docs/SPEC-TABLES.md §3), reused unchanged; the retain
// half adds only the unknown arm's capture and the tail's emit.
func generateTableRetain(u *ir.Unit, st *ir.Struct) (string, error) {
	var b strings.Builder
	b.WriteString("import java.nio.charset.StandardCharsets;\n\n")
	fmt.Fprintf(&b, "public final class %sRetain {\n", st.Name)
	fmt.Fprintf(&b, "    private %sRetain() {}\n\n", st.Name)

	// The enum readers the known-field read reuses (docs/SPEC-TABLES.md §3, §4):
	// a variant reference the table cannot name reads None (0) and counts it.
	for _, f := range st.Fields {
		if enum, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(&b, "    private static int %sEnum(long vid, TableVariable.Report report) {\n", varMember(f))
			for i := range enum.Variants {
				fmt.Fprintf(&b, "        if (vid == 0x%016xL) { return %d; }\n", ir.TableWireId(enum.VariantWireName(i)), i+1)
			}
			b.WriteString("        report.unknown++;\n        return 0;\n    }\n\n")
		}
	}

	// loadRetain: the variable codec's load, plus the unknown arm's capture.
	fmt.Fprintf(&b, "    // loadRetain is %sVariable.load with the unknown arm capturing\n", st.Name)
	b.WriteString("    // (§6.6): a scalar or string field this reader cannot name is copied\n")
	b.WriteString("    // verbatim into the caller's retain storage, and a node index or enum\n")
	b.WriteString("    // variant is reported as loss, never retained silently.\n")
	fmt.Fprintf(&b, "    public static %sVariable.Value loadRetain(byte[] data, TableRetain retain, TableVariable.Report report) {\n", st.Name)
	fmt.Fprintf(&b, "        TableVariable.Reader r = TableVariable.open(data, report);\n")
	b.WriteString("        if (r == null) { return null; }\n")
	b.WriteString("        if (retain != null) { retain.used = 0; retain.idUsed = 0; }\n")
	fmt.Fprintf(&b, "        %sVariable.Value v = new %sVariable.Value();\n", st.Name, st.Name)
	fmt.Fprintf(&b, "        TableVariable.NodeTable nt = TableVariable.readNodeTable(r, report);\n")
	fmt.Fprintf(&b, "        readFieldsRetain(r, v, nt, retain, report);\n")
	b.WriteString("        if (report.refused) { return null; }\n")
	fmt.Fprintf(&b, "        %sVariable.resolveNodes(v, nt, report);\n", st.Name)
	b.WriteString("        return v;\n")
	b.WriteString("    }\n\n")

	// readFieldsRetain: the variable readFields, with capture in the unknown arm.
	fmt.Fprintf(&b, "    static void readFieldsRetain(TableVariable.Reader r, %sVariable.Value v, TableVariable.NodeTable nt, TableRetain retain, TableVariable.Report report) {\n", st.Name)
	b.WriteString("        while (true) {\n")
	b.WriteString("            long ref = r.leb();\n")
	b.WriteString("            if (r.fail) { return; }\n")
	b.WriteString("            if (ref == 0) { return; }\n")
	b.WriteString("            long id = r.resolve(ref);\n")
	b.WriteString("            if (r.fail) { return; }\n")
	b.WriteString("            if (!r.has(1)) { report.malformed = true; return; }\n")
	b.WriteString("            int kind = r.get8();\n")
	first := true
	for _, f := range st.Fields {
		if !first {
			b.WriteString("            } else ")
		} else {
			b.WriteString("            ")
		}
		first = false
		fmt.Fprintf(&b, "if (id == %sVariable.F_%s) {\n", st.Name, varMember(f))
		writeFieldRead(&b, u, st, f)
	}
	b.WriteString("            } else if (id == TableVariable.NODE_ID) {\n")
	b.WriteString("                if (nt != null) { r.skip(kind); }\n")
	b.WriteString("                else { report.malformed = true; return; }\n")
	b.WriteString("            } else if (id == TableVariable.BUILD_ID || id == TableVariable.VOCAB_ID) {\n")
	b.WriteString("                report.malformed = true; return;\n")
	b.WriteString("            } else {\n")
	b.WriteString("                captureUnknown(r, id, kind, retain, report);\n")
	b.WriteString("            }\n")
	b.WriteString("            if (r.fail) { return; }\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")

	// captureUnknown: copy a scalar/string field's payload verbatim, or count
	// the excluded classes as loss.
	b.WriteString("    static void captureUnknown(TableVariable.Reader r, long id, int kind, TableRetain retain, TableVariable.Report report) {\n")
	b.WriteString("        int pStart = r.off;\n")
	b.WriteString("        r.skip(kind);\n")
	b.WriteString("        report.unknown++;\n")
	b.WriteString("        if (r.fail) { return; }\n")
	b.WriteString("        int pEnd = r.off;\n")
	b.WriteString("        if (retain == null) { return; }\n")
	b.WriteString("        if (!retainable(kind)) { report.retainLost++; return; }\n")
	b.WriteString("        int len = pEnd - pStart;\n")
	b.WriteString("        if (retain.used + 13 + len > retain.capacity) { report.retainLost++; return; }\n")
	b.WriteString("        byte[] b = retain.bytes;\n")
	b.WriteString("        int at = retain.used;\n")
	b.WriteString("        put32(b, at, 13 + len); at += 4;\n")
	b.WriteString("        put64(b, at, id); at += 8;\n")
	b.WriteString("        b[at++] = (byte) kind;\n")
	b.WriteString("        System.arraycopy(r.buf, pStart, b, at, len);\n")
	b.WriteString("        retain.used += 13 + len;\n")
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
	b.WriteString("        TableVariable.Ids ids = new TableVariable.Ids(64);\n")
	b.WriteString("        TableVariable.Writer w = new TableVariable.Writer(buf, ids);\n")
	b.WriteString("        TableVariable.NodeList nodes = new TableVariable.NodeList();\n")
	b.WriteString("        w.put8(1);\n")
	fmt.Fprintf(&b, "        %sVariable.writeFields(w, nodes, v);\n", st.Name)
	b.WriteString("        emitTail(w, retain, report);\n")
	b.WriteString("        if (nodes.size() > 0) {\n")
	b.WriteString("            long nodeRef = w.ref(TableVariable.NODE_ID);\n")
	b.WriteString("            TableVariable.Writer nw = w.sub(new byte[1 << 20]);\n")
	b.WriteString("            nw.putLeb(nodes.size());\n")
	b.WriteString("            for (int i = 0; i < nodes.size(); i++) {\n")
	b.WriteString("                byte[] body = nodes.body(i);\n")
	b.WriteString("                nw.putLeb(nw.ref(TableVariable.STRING_ID));\n")
	b.WriteString("                nw.putLeb(body.length);\n")
	b.WriteString("                nw.putBytes(body, 0, body.length);\n")
	b.WriteString("            }\n")
	b.WriteString("            w.putLeb(nodeRef);\n")
	b.WriteString("            w.put8(12);\n")
	b.WriteString("            w.putLeb(nw.off);\n")
	b.WriteString("            w.putBytes(nw.buf, 0, nw.off);\n")
	b.WriteString("        }\n")
	b.WriteString("        w.put8(0);\n")
	b.WriteString("        w.trailer();\n")
	b.WriteString("        return w.overflow ? -1 : w.off;\n")
	b.WriteString("    }\n\n")

	// emitTail: re-emit each retained record in retention order, interning its
	// id into the file's own id table in first-use order (§6.6); a record whose
	// id has no entry in the caller's id list is dropped and counted once.
	b.WriteString("    static void emitTail(TableVariable.Writer w, TableRetain retain, TableVariable.Report report) {\n")
	b.WriteString("        if (retain == null) { return; }\n")
	b.WriteString("        retain.idUsed = 0;\n")
	b.WriteString("        int at = 0;\n")
	b.WriteString("        while (at < retain.used) {\n")
	b.WriteString("            int len = get32(retain.bytes, at);\n")
	b.WriteString("            long id = get64(retain.bytes, at + 4);\n")
	b.WriteString("            int kind = retain.bytes[at + 12] & 0xff;\n")
	b.WriteString("            int pay = len - 13;\n")
	b.WriteString("            boolean seen = false;\n")
	b.WriteString("            for (int i = 0; i < retain.idUsed; i++) { if (retain.ids[i] == id) { seen = true; break; } }\n")
	b.WriteString("            if (!seen && retain.idUsed >= retain.idCapacity) { if (report != null) { report.retainLost++; } at += len; continue; }\n")
	b.WriteString("            if (!seen) { retain.ids[retain.idUsed++] = id; }\n")
	b.WriteString("            long ref = w.ref(id);\n")
	b.WriteString("            if (w.ids.overflow) { if (report != null) { report.retainLost++; } at += len; continue; }\n")
	b.WriteString("            w.putLeb(ref);\n")
	b.WriteString("            w.put8(kind);\n")
	b.WriteString("            w.putBytes(retain.bytes, at + 13, pay);\n")
	b.WriteString("            at += len;\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")

	// The little-endian record header primitives.
	b.WriteString("    static void put32(byte[] b, int at, int v) { for (int i = 0; i < 4; i++) { b[at + i] = (byte) (v >>> (8 * i)); } }\n")
	b.WriteString("    static void put64(byte[] b, int at, long v) { for (int i = 0; i < 8; i++) { b[at + i] = (byte) (v >>> (8 * i)); } }\n")
	b.WriteString("    static int get32(byte[] b, int at) { return (b[at] & 0xff) | (b[at + 1] & 0xff) << 8 | (b[at + 2] & 0xff) << 16 | (b[at + 3] & 0xff) << 24; }\n")
	b.WriteString("    static long get64(byte[] b, int at) { long v = 0; for (int i = 0; i < 8; i++) { v |= (long) (b[at + i] & 0xff) << (8 * i); } return v; }\n\n")

	// retainable: the verbatim-copy kinds the card's walk retains.
	b.WriteString("    static boolean retainable(int kind) {\n")
	b.WriteString("        switch (kind) {\n")
	b.WriteString("            case 1: case 2: case 3: case 4: case 5: case 6: case 7: case 8:\n")
	b.WriteString("            case 9: case 10: case 11: case 12: case 18: case 19: case 20:\n")
	b.WriteString("            case 21: case 22: case 23: case 24: case 25: case 26: case 27:\n")
	b.WriteString("            case 28: case 29: return true;\n")
	b.WriteString("            default: return false;\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return b.String(), nil
}

const tableRetainRuntime = `// The retain-unknown storage (docs/SPEC-TABLES.md §6.6): a caller-owned byte
// buffer and a caller-owned id list, never grown on demand, plus a read cursor
// the save walks. The report counters — retained and retain_lost — ride
// TableVariable.Report and accumulate across the load and the save.
public final class TableRetain {
    public byte[] bytes;
    public int capacity;
    public long[] ids;
    public int idCapacity;
    public int used;
    public int idUsed;

    public TableRetain(int byteCapacity, int idCapacity) {
        this.bytes = new byte[byteCapacity];
        this.capacity = byteCapacity;
        this.ids = new long[idCapacity];
        this.idCapacity = idCapacity;
    }
}
`
