package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableCookWriteSource is the shared Java writer. It uses the same generated
// layout descriptors as Open, and reflects over the generated Value classes
// only on the cold producer path (docs/SPEC-TABLES.md §7.6).
func tableCookWriteSource(u *ir.Unit, ck *cookUnit) string {
	var b strings.Builder
	b.WriteString(tableCookWriteRuntime)
	b.WriteString("    private static TableCookInfo info(long id) {\n")
	for _, name := range ck.order {
		st := u.Tables[name]
		if st == nil {
			continue
		}
		fmt.Fprintf(&b, "        if (id == 0x%016xL) { return %sRow.cookInfo(); }\n", ir.TableWireId(st.WireName()), name)
	}
	b.WriteString("        return null;\n    }\n}\n")
	return b.String()
}

const tableCookWriteRuntime = `// The measured cook producer (docs/SPEC-TABLES.md §7.6). Numbering follows
// declaration order through generated descriptors, including by-value records
// and pointer edges. A failed plan touches no byte of the caller's buffer.
public final class TableCookWrite {
    private TableCookWrite() {}
    public static final int LITTLE = 1;
    public static final int BIG = 2;
    private static final long STRING_ID = 0x704be0d8faaffc58L;
    private static final long BYTES_ID = 0x2f2ec0474f1c4fe4L;

    private static final class Node {
        final Object value;
        final TableCookInfo info;
        final byte[] blob;
        final boolean text;
        final long id;
        long offset;
        Node(Object value, TableCookInfo info) {
            this.value = value; this.info = info; this.blob = null;
            this.text = false; this.id = info.typeId;
        }
        Node(Object value, byte[] blob, boolean text) {
            this.value = value; this.info = null; this.blob = blob;
            this.text = text; this.id = text ? STRING_ID : BYTES_ID;
        }
        long size() { return info != null ? info.size : 8L + blob.length + (text ? 1 : 0); }
        long align() { return info != null ? Math.max(8, info.align) : 8; }
    }

    public static final class Plan {
        private final java.util.ArrayList<Node> nodes = new java.util.ArrayList<>();
        private final java.util.IdentityHashMap<Object, Node> index = new java.util.IdentityHashMap<>();
        public long total;
        private long dataLength;
        private long alignment;
    }

    private static long aligned(long n, long a) {
        if (n < 0 || a <= 0 || (a & (a - 1)) != 0 || n > Long.MAX_VALUE - (a - 1)) {
            throw new IllegalArgumentException("invalid cook extent");
        }
        return (n + a - 1) & -a;
    }

    private static Object field(Object value, String name) throws ReflectiveOperationException {
        if (value == null) { throw new IllegalArgumentException("null record"); }
        return value.getClass().getField(name).get(value);
    }

    private static int count(Object value, TableCookFieldInfo f, Object source)
            throws ReflectiveOperationException {
        if (source == null) { throw new IllegalArgumentException("null array"); }
        if (f.countOffset < 0) { return f.arrayBound; }
        try {
            return ((Number) field(value, f.sourceName + "Count")).intValue();
        } catch (NoSuchFieldException ignored) {
            if (source instanceof java.util.List<?>) { return ((java.util.List<?>) source).size(); }
            throw ignored;
        }
    }

    private static Object element(Object source, int i) {
        if (source instanceof java.util.List<?>) { return ((java.util.List<?>) source).get(i); }
        return java.lang.reflect.Array.get(source, i);
    }

    private static int length(Object source) {
        if (source instanceof java.util.List<?>) { return ((java.util.List<?>) source).size(); }
        return java.lang.reflect.Array.getLength(source);
    }

    // §7.6 numbering: first visit owns the node; a repeated edge reuses it.
    private static void add(Plan p, Object value, TableCookInfo info) throws ReflectiveOperationException {
        if (value == null || p.index.containsKey(value)) { return; }
        Node n;
        if (info != null) {
            n = new Node(value, info);
        } else if (value instanceof String) {
            n = new Node(value, ((String) value).getBytes(java.nio.charset.StandardCharsets.UTF_8), true);
        } else if (value instanceof byte[]) {
            n = new Node(value, (byte[]) value, false);
        } else {
            throw new IllegalArgumentException("unknown pointer target");
        }
        p.index.put(value, n);
        p.nodes.add(n);
        if (info != null) { discover(p, value, info); }
    }

    private static void discover(Plan p, Object value, TableCookInfo info)
            throws ReflectiveOperationException {
        for (TableCookFieldInfo f : info.fields) { discoverField(p, value, f); }
    }

    // §2.3: an ABSENT optional owns no node. Its storage may still hold a
    // stale value (a cleared ?T keeps what it held), so the numbering never
    // descends it, and the store writes it as a load leaves it: the
    // value-initialized record, every reference null.
    private static boolean absentRecord(Object value, TableCookFieldInfo f)
            throws ReflectiveOperationException {
        return f.presentOffset >= 0
                && f.storage == TableCookStorage.RECORD
                && !Boolean.TRUE.equals(field(value, f.sourceName + "Present"));
    }

    private static void discoverField(Plan p, Object value, TableCookFieldInfo f)
            throws ReflectiveOperationException {
        if (absentRecord(value, f)) { return; }
        Object source = field(value, f.sourceName);
        if (f.isArray) {
            int used = count(value, f, source);
            if (used < 0 || used > f.arrayBound || (f.countOffset >= 0 && length(source) < used)) {
                throw new IllegalArgumentException("array count");
            }
            // a FIXED array's value may hold fewer than N elements: the slots
            // past it are value-initialized (§7.2) and reach no node
            used = Math.min(used, length(source));
            for (int i = 0; i < used; i++) { discoverValue(p, element(source, i), f); }
        } else {
            discoverValue(p, source, f);
        }
    }

    private static void discoverValue(Plan p, Object source, TableCookFieldInfo f)
            throws ReflectiveOperationException {
        if (f.isPointer) { add(p, source, f.record()); }
        else if (f.storage == TableCookStorage.RECORD && source != null) {
            discover(p, source, f.record());
        } else if (f.storage == TableCookStorage.UNION && source != null) {
            // the SET arm is the union's one edge (§2.6, §3.1), walked in the
            // union field's own declaration-order position
            TableCookFieldInfo arm = arm(source, f.record());
            if (arm != null) { discoverField(p, source, arm); }
        }
    }

    // The arm a union value's tag selects, or null for None. A tag no arm
    // carries is not a value this build can hold, and the plan refuses it.
    private static TableCookFieldInfo arm(Object union, TableCookInfo info)
            throws ReflectiveOperationException {
        long tag = ((Number) field(union, "tag")).longValue();
        if (tag == 0) { return null; }
        for (TableCookFieldInfo a : info.fields) {
            if (a.tag == tag) { return a; }
        }
        // a declared arm with no payload rides its tag alone
        if (tag > 0 && tag <= info.armCount) { return null; }
        throw new IllegalArgumentException("union tag");
    }

    /** Measure and validate before writing; invalid values answer null (§7.6). */
    public static Plan plan(Object root, TableCookInfo info) {
        return plan(root, info, null);
    }

    /**
     * plan, saying why it refused: nonFinite[0] turns true when the value held a NaN or an
     * infinity (docs/SPEC-TABLES.md §3.3), which no cook carries.
     */
    public static Plan plan(Object root, TableCookInfo info, boolean[] nonFinite) {
        if (root == null || info == null) { return null; }
        try {
            Plan p = new Plan();
            add(p, root, info);
            long cursor = 0;
            long align = 8;
            for (Node n : p.nodes) {
                align = Math.max(align, n.align());
                cursor = aligned(cursor, n.align());
                n.offset = cursor;
                if (n.size() < 0 || cursor > Integer.MAX_VALUE - n.size()) { return null; }
                cursor += n.size();
            }
            p.alignment = align;
            p.dataLength = aligned(cursor, align);
            if (p.nodes.size() > (Integer.MAX_VALUE - 64L - p.dataLength) / 16L) { return null; }
            p.total = 64L + p.dataLength + 16L * p.nodes.size();
            if (p.total > Integer.MAX_VALUE) { return null; }
            for (Node n : p.nodes) {
                if (n.info != null) { record(p, null, 0, 64L + n.offset, n.value, n.info); }
            }
            return p;
        } catch (NonFiniteFloat ex) {
            if (nonFinite != null) { nonFinite[0] = true; }
            return null;
        } catch (ReflectiveOperationException | RuntimeException ex) {
            return null;
        }
    }

    /** a cooked float's NaN or infinity: the plan's own refusal (docs/SPEC-TABLES.md §3.3). */
    static final class NonFiniteFloat extends IllegalArgumentException {
        private static final long serialVersionUID = 1L;

        NonFiniteFloat() {
            super("non_finite_float", null);
        }
    }

    private static void put(byte[] out, int order, long at, long value, int width) {
        if (out != null) { TableBytes.put(out, (int) at, value, width, order == BIG); }
    }

    private static long bits(Object source, TableCookStorage storage, int width) {
        if (source == null) { return 0; }
        if (storage == TableCookStorage.BOOL) { return Boolean.TRUE.equals(source) ? 1 : 0; }
        if (storage == TableCookStorage.FLOAT) {
            // A COOKED FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3): its raw bits
            // ride, -0 and the subnormals exact, and a NaN or an infinity
            // refuses the plan, so the cook is never written.
            long raw = width == 4 ? Float.floatToRawIntBits(((Number) source).floatValue()) & 0xffffffffL
                    : Double.doubleToRawLongBits(((Number) source).doubleValue());
            long exponent = width == 4 ? 0x7f800000L : 0x7ff0000000000000L;
            if ((raw & exponent) == exponent) { throw new NonFiniteFloat(); }
            return raw;
        }
        if (source instanceof Character) { return ((Character) source).charValue(); }
        return ((Number) source).longValue();
    }

    private static void value(Plan p, byte[] out, int order, long at, Object source, TableCookFieldInfo f)
            throws ReflectiveOperationException {
        if (f.isPointer) {
            Node target = p.index.get(source);
            if (source != null && target == null) { throw new IllegalArgumentException("unnumbered pointer"); }
            put(out, order, at, target == null ? 0 : (64L + target.offset - at), 8);
        } else if (f.storage == TableCookStorage.RECORD) {
            TableCookInfo r = f.record();
            Object held = source != null ? source : (r.defaults != null ? r.defaults.get() : null);
            if (held != null) { record(p, out, order, at, held, r); }
        } else if (f.storage == TableCookStorage.UNION) {
            // §7.2: the TAG at the union's base, then ONLY the set arm's bytes;
            // every other byte of the extent stays the zero write() laid down
            if (source != null) {
                TableCookInfo u = f.record();
                put(out, order, at, ((Number) field(source, "tag")).longValue(), u.tagWidth);
                TableCookFieldInfo a = arm(source, u);
                if (a != null) { store(p, out, order, at, source, a); }
            }
        } else if (f.storage == TableCookStorage.WIDE) {
            // §7.2: the low half first in a little-endian cook, the high half
            // first in a big-endian one, each half one eight-byte lane
            long hi = source == null ? 0 : ((Number) field(source, "hi")).longValue();
            long lo = source == null ? 0 : ((Number) field(source, "lo")).longValue();
            put(out, order, at, order == BIG ? hi : lo, 8);
            put(out, order, at + 8, order == BIG ? lo : hi, 8);
        } else {
            put(out, order, at, bits(source, f.storage, f.elemSize), f.elemSize);
        }
    }

    private static void record(Plan p, byte[] out, int order, long at, Object source, TableCookInfo info)
            throws ReflectiveOperationException {
        for (TableCookFieldInfo f : info.fields) { store(p, out, order, at, source, f); }
    }

    // One field line's pieces, read off the source value and stored at the
    // given offset plus the
    // descriptor's offsets: a record's field, or a union's set arm.
    private static void store(Plan p, byte[] out, int order, long at, Object source, TableCookFieldInfo f)
            throws ReflectiveOperationException {
        Object v = field(source, f.sourceName);
        if (f.presentOffset >= 0) {
            Object present = field(source, f.sourceName + "Present");
            put(out, order, at + f.presentOffset, Boolean.TRUE.equals(present) ? 1 : 0, 1);
        }
        long dest = at + f.offset;
        if (absentRecord(source, f)) {
            if (f.countOffset >= 0) { put(out, order, at + f.countOffset, 0, 4); }
            int slots = f.isArray ? f.arrayBound : 1;
            for (int i = 0; i < slots; i++) { value(p, out, order, dest + (long) i * f.elemSize, null, f); }
            return;
        }
        if (f.storage == TableCookStorage.STRING || f.storage == TableCookStorage.BYTES) {
            byte[] bytes = v instanceof String ? ((String) v).getBytes(java.nio.charset.StandardCharsets.UTF_8)
                    : (byte[]) v;
            if (bytes == null) { bytes = new byte[0]; }
            int used = bytes.length;
            if (!(v instanceof String)) {
                try { used = ((Number) field(source, f.sourceName + "Length")).intValue(); }
                catch (NoSuchFieldException ignored) { /* variable bytes use their full length */ }
            }
            if (used < 0 || used > f.arrayBound || used > bytes.length) {
                throw new IllegalArgumentException("text length");
            }
            if (out != null) { System.arraycopy(bytes, 0, out, (int) dest, used); }
            put(out, order, at + f.countOffset, used, 4);
        } else if (f.isArray) {
            int used = count(source, f, v);
            if (used < 0 || used > f.arrayBound || (f.countOffset >= 0 && length(v) < used)) {
                throw new IllegalArgumentException("array count");
            }
            if (f.countOffset >= 0) { put(out, order, at + f.countOffset, used, 4); }
            // §7.2: ALL N slots are written, and a slot past the live elements
            // holds the VALUE-INITIALIZED element — zero for a scalar, the
            // element record's declared defaults for a record
            int live = f.countOffset >= 0 ? used : Math.min(used, length(v));
            for (int i = 0; i < f.arrayBound; i++) {
                value(p, out, order, dest + (long) i * f.elemSize, i < live ? element(v, i) : null, f);
            }
        } else {
            value(p, out, order, dest, v, f);
        }
    }

    /** §7.6: after plan succeeds, write header, records, and attribution. */
    public static boolean write(Plan p, byte[] out, int order) {
        if (p == null || out == null || out.length < p.total || (order != LITTLE && order != BIG)) {
            return false;
        }
        java.util.Arrays.fill(out, 0, (int) p.total, (byte) 0);
        put(out, order, 0, 0x4b4f4f434d484353L, 8);
        put(out, order, 8, BuildVersion.value, 8);
        put(out, order, 16, order, 8);
        put(out, order, 24, p.dataLength, 8);
        put(out, order, 32, 16L * p.nodes.size(), 8);
        put(out, order, 40, p.alignment, 8);
        try {
            for (int i = 0; i < p.nodes.size(); i++) {
                Node n = p.nodes.get(i);
                long at = 64L + n.offset;
                if (n.info != null) {
                    record(p, out, order, at, n.value, n.info);
                } else {
                    put(out, order, at, n.blob.length, 4);
                    System.arraycopy(n.blob, 0, out, (int) at + 8, n.blob.length);
                }
                long entry = 64L + p.dataLength + i * 16L;
                put(out, order, entry, n.offset, 8);
                put(out, order, entry + 8, n.id, 8);
            }
            return true;
        } catch (ReflectiveOperationException | RuntimeException ex) {
            return false;
        }
    }

`
