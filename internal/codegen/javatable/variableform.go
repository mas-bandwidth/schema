package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The VARIABLE FORM (docs/SPEC-TABLES.md §3), form byte 1, is the table wire
// the card record rides: a body of `id reference, kind (u8), payload` fields
// terminated by a zero reference, then the id-table trailer read from the end
// (§3), and the flat node table of §3.1 for the `*string` blob pointers the
// card's `done_when`, `source`, `body` and `extra[].value` carry. This file
// emits the Java runtime and the per-table load/save for exactly the card's
// constructs: plain `table` roots and tables as list elements, bool/int32/
// int64/enum fields, string(N), the *string byte buffer (§2.5) and unbounded
// lists of tables ([]T, §2.9). Every other variable-class construct is refused
// by name before this emitter runs (refuseVariableForm, javatable.go).

// tableVariableRuntime is the Java runtime the variable form's generated
// load/save share: the LEB128 reader/writer, the id-table trailer, the node
// table and the report counters (§4). One public type per file, so the runtime
// is one class (TableVariable.java) and the per-table codecs are one class
// each (<Type>Variable.java).
const tableVariableRuntime = `// The VARIABLE FORM's runtime (docs/SPEC-TABLES.md §3), form byte 1.
// A body is a sequence of fields, each "id reference, kind (u8), payload",
// terminated by a zero reference; the id table is the trailer, read from the
// end of the wire. A reference is an unsigned LEB128 naming a slot of the id
// table, counted from 1; reference 0 names no id and is the terminator.
public final class TableVariable {
    private TableVariable() {}

    // The two RESERVED ids the node table names (docs/SPEC-TABLES.md §3.1, §5):
    // the node-table field's own id, and the reserved type id a *string blob
    // record rides under (fnv1a64("string")).
    public static final long NODE_ID = 0xFFFFFFFFFFFFFFFFL;
    public static final long STRING_ID = 0x704be0d8faaffc58L;
    public static final long BUILD_ID = 0xFFFFFFFFFFFFFFFEL;
    public static final long VOCAB_ID = 0xFFFFFFFFFFFFFFFDL;

    // The report counters of §4: unknown, kind mismatch, clamp, malformed and
    // widened. A refusal (an unknown form byte) moves none of them.
    public static final class Report {
public int unknown;
        public int kindMismatch;
        public int clamped;
        public boolean malformed;
        public int widened;
        public boolean refused;
        public String reason;
    }

    // A reader over a byte[] the caller owns. Nothing allocates on the read
    // path: the id table is resolved once, at open, into a long[]. fail is
    // THIS reader's own framing damage, set by its own primitive reads, so a
    // caller can stop the walk it is on without a nested body's damage (which
    // shares the report) stopping the walk above it (§3).
    public static final class Reader {
        public final byte[] buf;
        public int off;
        public int limit;
        public final long[] ids;
        public final Report report;
        public boolean fail;

        public Reader(byte[] buf, long[] ids, Report report) {
            this.buf = buf;
            this.ids = ids;
            this.report = report;
            this.limit = buf.length;
        }

        public boolean has(int n) { return n >= 0 && (long) off + (long) n <= limit; }

        public int get8() { return buf[off++] & 0xff; }

        public int get16() {
            int v = (buf[off] & 0xff) | (buf[off + 1] & 0xff) << 8;
            off += 2;
            return v;
        }

        public int get32() {
            int v = (buf[off] & 0xff) | (buf[off + 1] & 0xff) << 8
                    | (buf[off + 2] & 0xff) << 16 | (buf[off + 3] & 0xff) << 24;
            off += 4;
            return v;
        }

        public long get64() {
            long v = 0;
            for (int i = 0; i < 8; i++) { v |= (long) (buf[off + i] & 0xff) << (8 * i); }
            off += 8;
            return v;
        }

        // leb reads one canonical unsigned LEB128. A non-minimal spelling or a
        // value past 64 bits is malformed (§3). On damage the cursor stays
        // where the spelling began, exactly as the oracle's read does, so the
        // caller's "body ended at its own L" check measures the same spot.
        public long leb() {
            long v = 0;
            int shift = 0;
            int start = off;
            for (int i = 0; i < 10; i++) {
                if (!has(1)) { off = start; report.malformed = true; fail = true; return 0; }
                int b = get8();
                if (shift == 63 && (b & 0xfe) != 0) { off = start; report.malformed = true; fail = true; return 0; }
                v |= (long) (b & 0x7f) << shift;
                if ((b & 0x80) == 0) {
                    if (i > 0 && b == 0) { off = start; report.malformed = true; fail = true; return 0; }
                    return v;
                }
                shift += 7;
            }
            off = start;
            report.malformed = true;
            fail = true;
            return 0;
        }

        // resolve turns a reference into the id it names, or -1 when it is past
        // the table (§3).
        public long resolve(long ref) {
            if (ref <= 0 || ref > ids.length) { report.malformed = true; fail = true; return -1; }
            return ids[(int) ref - 1];
        }

        // body reads an L-framed payload: L, then L bytes, as a sub-reader
        // bounded by its own L, exactly as a nested body is bounded (§3).
        public Reader body() {
            long n = leb();
            if (fail || !has((int) n)) { report.malformed = true; fail = true; return null; }
            Reader sub = new Reader(buf, ids, report);
            sub.off = off;
            sub.limit = off + (int) n;
            off += (int) n;
            return sub;
        }

        // skip steps over a field of the given kind by its framing (§3). It
        // moves fail on a kind a reader cannot skip — the reserved float16 or
        // one past the closed set — and on a payload the framing cannot cover.
        public void skip(int kind) {
            switch (kind) {
                case 1: case 2: case 6: case 20: case 25:
                    if (has(1)) { off += 1; } else { report.malformed = true; fail = true; }
                    break;
                case 3: case 7: case 21: case 26:
                    if (has(2)) { off += 2; } else { report.malformed = true; fail = true; }
                    break;
                case 4: case 8: case 10: case 22: case 27:
                    if (has(4)) { off += 4; } else { report.malformed = true; fail = true; }
                    break;
                case 5: case 9: case 11: case 23: case 28:
                    if (has(8)) { off += 8; } else { report.malformed = true; fail = true; }
                    break;
                case 18: case 19: case 24: case 29:
                    if (has(16)) { off += 16; } else { report.malformed = true; fail = true; }
                    break;
                case 17: case 30: leb(); break;
                case 12: case 13: case 14: case 16: case 31: case 32: case 33: {
                    long n = leb();
                    if (!fail && has((int) n)) { off += (int) n; } else { report.malformed = true; fail = true; }
                    break;
                }
                case 15: {
                    long arm = leb();
                    if (arm != 0) { int k = get8(); long n = leb(); if (!fail && has((int) n)) { off += (int) n; } else { report.malformed = true; fail = true; } }
                    break;
                }
                default: report.malformed = true; fail = true;
            }
        }
    }

    // The ID TABLE (docs/SPEC-TABLES.md §3): one shared over the whole file, so
    // a list element's field ids intern into the same table the root's do.
    public static final class Ids {
        public final long[] entries;
        public int count;
        public boolean overflow;
        public Ids(int cap) { entries = new long[cap]; }

        public long ref(long id) {
            for (int i = 0; i < count; i++) { if (entries[i] == id) { return i + 1; } }
            if (count >= entries.length) { overflow = true; return 0; }
            entries[count++] = id;
            return count;
        }
    }

    // A writer over a byte[] the caller owns. The id table is written in
    // first-use order at the trailer (§3).
    public static final class Writer {
        public final byte[] buf;
        public int off;
        public final Ids ids;
        public boolean overflow;

        public Writer(byte[] buf, Ids ids) {
            this.buf = buf;
            this.ids = ids;
        }

        // sub returns a writer over the SAME id table: a list element's body is
        // written into scratch and framed by its caller, but its field ids
        // intern into the one table the whole file shares (§3).
        public Writer sub(byte[] buf) { return new Writer(buf, ids); }

        public void put8(int b) {
            if (off >= buf.length) { overflow = true; return; }
            buf[off++] = (byte) b;
        }

        public void put32(int v) {
            for (int i = 0; i < 4; i++) { put8((v >>> (8 * i)) & 0xff); }
        }

        public void put64(long v) {
            for (int i = 0; i < 8; i++) { put8((int) (v >>> (8 * i)) & 0xff); }
        }

        public void putLeb(long v) {
            while (true) {
                int b = (int) (v & 0x7f);
                v >>>= 7;
                if (v != 0) { put8(b | 0x80); } else { put8(b); break; }
            }
        }

        public void putBytes(byte[] b, int at, int len) {
            for (int i = 0; i < len; i++) { put8(b[at + i] & 0xff); }
        }

        // ref interns an id into the table in first-use order and returns its
        // reference (1-based).
        public long ref(long id) {
            long r = ids.ref(id);
            if (ids.overflow) { overflow = true; }
            return r;
        }

        // trailer writes the id table: the entries, then the fixed u64 count.
        public void trailer() {
            for (int i = 0; i < ids.count; i++) { put64(ids.entries[i]); }
            put64(ids.count);
        }
    }

    // open reads the form byte and resolves the id table from the end (§3).
    // A form byte this build does not carry is a refusal, not damage. The
    // framing checks are STRICT here (§3): a table that carries one id twice,
    // or a body whose terminator does not land at the table's first entry —
    // bytes no field claims — is malformed for the whole wire, and open says
    // so before any value is read.
    public static Reader open(byte[] data, Report report) {
        if (data.length < 1) { report.malformed = true; return null; }
        int form = data[0] & 0xff;
        if (form != 1) {
            report.refused = true;
            report.reason = "unsupported wire form";
            return null;
        }
        if (data.length < 9) { report.malformed = true; return null; }
        long count = 0;
        for (int i = 0; i < 8; i++) { count |= (long) (data[data.length - 8 + i] & 0xff) << (8 * i); }
        if (count < 0 || count > (data.length - 9) / 8) { report.malformed = true; return null; }
        long[] ids = new long[(int) count];
        int base = data.length - 8 - (int) count * 8;
        for (int i = 0; i < count; i++) {
            long id = 0;
            for (int j = 0; j < 8; j++) { id |= (long) (data[base + i * 8 + j] & 0xff) << (8 * j); }
            for (int j = 0; j < i; j++) {
                if (ids[j] == id) { report.malformed = true; return null; }
            }
            ids[i] = id;
        }
        int end = bodyExtent(data, ids, 1, base);
        if (end >= 0 && end != base) { report.malformed = true; return null; }
        Reader r = new Reader(data, ids, report);
        r.off = 1;
        r.limit = base;
        return r;
    }

    // bodyExtent walks the framing of the body in [off, limit) to the zero
    // reference that ends it (§3). It answers the offset just past the
    // terminator, or -1 where the walk cannot reach one at all — §4's framing
    // damage on that body, not the file's. It moves no counter: it is the OPEN
    // check that tells a body that ENDED EARLY from one that is merely damaged.
    public static int bodyExtent(byte[] buf, long[] ids, int off, int limit) {
        while (off < limit) {
            long[] ref = lebAt(buf, off, limit);
            if (ref[0] < 0) { return -1; }
            off = (int) ref[1];
            if (ref[0] == 0) { return off; }
            if (ref[0] > ids.length) { return -1; }
            if (off >= limit) { return -1; }
            int kind = buf[off] & 0xff;
            off++;
            off = skipExtent(buf, kind, off, limit);
            if (off < 0) { return -1; }
        }
        return -1;
    }

    // lebAt reads one canonical unsigned LEB128 at off, bounded by limit. It
    // answers the value and the offset just past it, or {-1, -1} for a bad
    // encoding: truncated, past ten bytes, a tenth byte above the 64th value
    // bit, or a non-minimal spelling (§3).
    public static long[] lebAt(byte[] buf, int off, int limit) {
        long v = 0;
        int shift = 0;
        for (int i = 0; ; i++) {
            if (off + i >= limit || i >= 10) { return new long[]{-1, -1}; }
            int b = buf[off + i] & 0xff;
            if (i == 9 && b > 1) { return new long[]{-1, -1}; }
            v |= (long) (b & 0x7f) << shift;
            if ((b & 0x80) == 0) {
                if (i > 0 && b == 0) { return new long[]{-1, -1}; }
                return new long[]{v, off + i + 1};
            }
            shift += 7;
        }
    }

    // skipExtent advances past one field's payload by its kind (§3), for the
    // extent walk. -1 is a payload the framing cannot cover, which is a kind a
    // reader does not know (kind 34, the reserved float16, or one past the
    // closed set) or a payload that runs past the body.
    public static int skipExtent(byte[] buf, int kind, int off, int limit) {
        int w;
        switch (kind) {
            case 1: case 2: case 6: case 20: case 25:
                w = 1; break;
            case 3: case 7: case 21: case 26:
                w = 2; break;
            case 4: case 8: case 10: case 22: case 27:
                w = 4; break;
            case 5: case 9: case 11: case 23: case 28:
                w = 8; break;
            case 18: case 19: case 24: case 29:
                w = 16; break;
            case 17: case 30: {
                long[] v = lebAt(buf, off, limit);
                return v[0] < 0 ? -1 : (int) v[1];
            }
            case 12: case 13: case 14: case 16: case 31: case 32: case 33: {
                long[] v = lebAt(buf, off, limit);
                if (v[0] < 0 || v[0] > limit - (int) v[1]) { return -1; }
                return (int) v[1] + (int) v[0];
            }
            case 15: {
                long[] arm = lebAt(buf, off, limit);
                if (arm[0] < 0) { return -1; }
                if (arm[0] == 0) { return (int) arm[1]; }
                int at = (int) arm[1];
                if (at >= limit) { return -1; }
                at++; // the arm's kind byte
                long[] n = lebAt(buf, at, limit);
                if (n[0] < 0 || n[0] > limit - (int) n[1]) { return -1; }
                return (int) n[1] + (int) n[0];
            }
            default:
                return -1;
        }
        return off + w <= limit ? off + w : -1;
    }

    // textValid reports whether a kind-12 payload is well-formed UTF-8 with no
    // zero byte among its bytes (docs/SPEC-TABLES.md §3, §4): checked as it
    // ARRIVES, before the reader's own bound, because a payload that is not
    // text is not text at whatever length the reader would have kept.
    public static boolean textValid(byte[] b, int off, int len) {
        int i = off;
        int end = off + len;
        while (i < end) {
            int c = b[i] & 0xff;
            if (c == 0) { return false; }
            if (c < 0x80) { i++; continue; }
            if (c >= 0xc2 && c <= 0xdf) {
                if (i + 1 >= end || (b[i + 1] & 0xc0) != 0x80) { return false; }
                i += 2;
            } else if (c >= 0xe0 && c <= 0xef) {
                if (i + 2 >= end) { return false; }
                int c1 = b[i + 1] & 0xff, c2 = b[i + 2] & 0xff;
                if ((c1 & 0xc0) != 0x80 || (c2 & 0xc0) != 0x80) { return false; }
                if (c == 0xe0 && c1 < 0xa0) { return false; }
                if (c == 0xed && c1 > 0x9f) { return false; }
                i += 3;
            } else if (c >= 0xf0 && c <= 0xf4) {
                if (i + 3 >= end) { return false; }
                int c1 = b[i + 1] & 0xff, c2 = b[i + 2] & 0xff, c3 = b[i + 3] & 0xff;
                if ((c1 & 0xc0) != 0x80 || (c2 & 0xc0) != 0x80 || (c3 & 0xc0) != 0x80) { return false; }
                if (c == 0xf0 && c1 < 0x90) { return false; }
                if (c == 0xf4 && c1 > 0x8f) { return false; }
                i += 4;
            } else {
                return false;
            }
        }
        return true;
    }

    // A NODE-TABLE RECORD (docs/SPEC-TABLES.md §3.1): the type id the record
    // rides under and its body bytes. For the card's *string the type id is the
    // reserved string id (ir.StringWireTypeId) and the body is the raw UTF-8
    // bytes; a *bytes buffer rides under the reserved bytes id.
    public static final class NodeRecord {
        public long typeId;
        public final byte[] body;
        public String value;
        public NodeRecord(long typeId, byte[] body) { this.typeId = typeId; this.body = body; }
    }

    // nodes reads a node-table field's payload (§3.1) and returns its records.
    // A type id this reader cannot name keeps its index and counts unknown;
    // a text blob whose content is not text counts malformed; in both, the
    // record resolves to null (its typeId is cleared) and every pointer naming
    // it reads null (§3.1, §4). Structural damage — a bad reference, a size
    // past the field, a declared count the scan does not match, or bytes left
    // over — is malformed for the whole table and answers null.
    public static NodeRecord[] nodes(Reader r) {
        long declared = r.leb();
        if (r.fail || declared < 0 || declared > 0x7fffffffL) {
            r.report.malformed = true;
            return null;
        }
        // The node table is WHOLE or it is NOTHING (docs/SPEC-TABLES.md §3.1):
        // the records are scanned structurally FIRST, and a record that cannot
        // be read, a node_count the scan does not match, or bytes left over is
        // malformed for the whole table with NO unknown counted — the oracle's
        // scanNodeRecords returns no records on a bad scan and its unknown
        // count runs only past a whole one (internal/tablewire/decodenodes.go).
        java.util.ArrayList<NodeRecord> list = new java.util.ArrayList<>();
        while (r.off < r.limit) {
            long ref = r.leb();
            if (r.fail || ref == 0) { r.report.malformed = true; return null; }
            long typeId = r.resolve(ref);
            if (r.fail) { r.report.malformed = true; return null; }
            long size = r.leb();
            if (r.fail || size < 0 || !r.has((int) size)) { r.report.malformed = true; return null; }
            byte[] body = new byte[(int) size];
            System.arraycopy(r.buf, r.off, body, 0, (int) size);
            r.off += (int) size;
            list.add(new NodeRecord(typeId, body));
        }
        if (declared != list.size()) { r.report.malformed = true; return null; }
        // Type resolution (§3.1), past the whole scan: a record whose type id
        // this reader cannot name keeps its index and counts unknown ONCE, and
        // a text blob whose content is not text counts malformed; in both the
        // record resolves to null (its type id cleared).
        for (NodeRecord rec : list) {
            if (rec.typeId != STRING_ID) {
                r.report.unknown++;
                rec.typeId = 0;
            } else if (!textValid(rec.body, 0, rec.body.length)) {
                r.report.malformed = true;
                rec.typeId = 0;
            } else {
                rec.value = new String(rec.body, java.nio.charset.StandardCharsets.UTF_8);
            }
        }
        return list.toArray(new NodeRecord[0]);
    }

    // nodesRaw is the region-measure half's scan (§6.5): the same framing walk
    // with no counters and no type-id clearing, so the caller can size a blob
    // record's bytes whether or not its text is well-formed, exactly as the
    // oracle's FileRegionMeasure does.
    public static NodeRecord[] nodesRaw(Reader r) {
        long declared = r.leb();
        if (r.fail || declared < 0 || declared > 0x7fffffffL) {
            r.report.malformed = true;
            return null;
        }
        java.util.ArrayList<NodeRecord> list = new java.util.ArrayList<>();
        while (r.off < r.limit) {
            long ref = r.leb();
            if (r.fail || ref == 0) { r.report.malformed = true; return null; }
            long typeId = r.resolve(ref);
            if (r.fail) { r.report.malformed = true; return null; }
            long size = r.leb();
            if (r.fail || size < 0 || !r.has((int) size)) { r.report.malformed = true; return null; }
            byte[] body = new byte[(int) size];
            System.arraycopy(r.buf, r.off, body, 0, (int) size);
            r.off += (int) size;
            list.add(new NodeRecord(typeId, body));
        }
        if (declared != list.size()) { r.report.malformed = true; return null; }
        return list.toArray(new NodeRecord[0]);
    }

    // node resolves a body field's pointer payload — a node index, 1 the root,
    // k >= 2 the (k-2)-th record — to its record, or null for the null index 0
    // and for an index that names no record (§3.1).
    public static NodeRecord node(NodeRecord[] recs, long index) {
        if (index == 0) { return null; }
        if (index < 2 || recs == null || index - 2 >= recs.length) { return null; }
        return recs[(int) (index - 2)];
    }

    // readNodeTable scans the ROOT body for the reserved node-table field and
    // reads its records BEFORE the body decode (§3.1): the numbering is read
    // first, so a field the body cannot decode does not lose the table. The
    // scan walks the WHOLE body, and a repeated node-table field is the one
    // §3's repeated-field rule keeps: the LAST occurrence wins, exactly as the
    // oracle's scan does (internal/tablewire/nodetable.go, nodeTableBytes),
    // because a reader that broke at the first would read the good numbering a
    // damaged repeat was planted to hide. A field under a kind other than 12,
    // a length the body cannot cover, or a payload whose records do not scan
    // is malformed for the whole table and marks the numbering bad (NodeTable.
    // bad), and the scan returns the body cursor to where it began for the
    // decode itself.
    public static NodeTable readNodeTable(Reader r, Report report) {
        int start = r.off;
        boolean startFail = r.fail;
        Reader payload = null;
        boolean bad = false;
        while (true) {
            long ref = r.leb();
            if (r.fail || ref == 0) { break; }
            long id = r.resolve(ref);
            if (r.fail) { break; }
            if (!r.has(1)) { break; }
            int kind = r.get8();
            if (id == NODE_ID) {
                if (kind != 12) { report.malformed = true; bad = true; break; }
                long n = r.leb();
                if (r.fail || n < 0 || !r.has((int) n)) { report.malformed = true; bad = true; break; }
                Reader sub = new Reader(r.buf, r.ids, report);
                sub.off = r.off;
                sub.limit = r.off + (int) n;
                r.off += (int) n;
                payload = sub;
                continue;
            }
            r.skip(kind);
            if (r.fail) { break; }
        }
        r.off = start;
        r.fail = startFail;
        // A node-table field that is PRESENT but could not be read whole leaves
        // the numbering not-good (§3.1): every pointer resolves null and moves
        // no counter, exactly as the oracle's decodeState.good=false does
        // (internal/tablewire/decodenodes.go). An ABSENT field is an empty but
        // valid numbering, and a whole read carries the records.
        NodeTable nt = new NodeTable();
        if (bad) { nt.bad = true; return nt; }
        if (payload == null) { return nt; }
        nt.recs = nodes(payload);
        if (nt.recs == null) { nt.bad = true; }
        return nt;
    }

    // resolveString resolves one *string pointer index against the numbering
    // (§3.1): 0 is null; 1 is the ROOT — a table, not a blob, so it is a kind
    // mismatch; an index past the records is malformed; and a record whose
    // type id is not the string id reads null, its unknown already counted at
    // the scan.
    public static String resolveString(NodeRecord[] recs, long index, Report report) {
        if (index == 0) { return null; }
        if (index == 1) { report.kindMismatch++; return null; }
        if (index < 2 || recs == null || index - 2 >= recs.length) { report.malformed = true; return null; }
        return recs[(int) (index - 2)].value;
    }

    // NodeTable is a load-side holder: the root body's node records, filled
    // when the reserved node-table field is met and read by every deferred
    // pointer index afterward (§3.1).
    public static final class NodeTable {
        public NodeRecord[] recs;
        // bad is true when a node-table field was PRESENT but could not be read
        // whole: the numbering failed, so every pointer resolves null and moves
        // no counter (§3.1). It is false when the field is absent — an empty
        // but valid numbering — or was read whole.
        public boolean bad;
    }

    // NodeList is a save-side numbering: each non-null *string interned here in
    // first-visit order takes the next node index (2, 3, ...; the root is 1,
    // §3.1) and its body bytes, written once into the node table.
    public static final class NodeList {
        private final java.util.ArrayList<byte[]> bodies = new java.util.ArrayList<>();
        private final java.util.IdentityHashMap<String, Long> seen = new java.util.IdentityHashMap<>();
        private int next = 2;
        public long ref(String s) {
            Long have = seen.get(s);
            if (have != null) { return have.longValue(); }
            seen.put(s, Long.valueOf(next));
            bodies.add(s.getBytes(java.nio.charset.StandardCharsets.UTF_8));
            return next++;
        }
        public int size() { return bodies.size(); }
        public byte[] body(int i) { return bodies.get(i); }
    }
}
`

// tableJsonRuntime is the TEXT form's runtime (docs/SPEC-TABLES.md §16), shared
// by the variable form's generated FromJson/ToJson: the pretty writer's
// escaping and indentation and the tolerant reader — whitespace and comments,
// string escapes, JSON numbers, the skip of an unknown value. The per-table
// codec names the keys and the value shape; this class holds the walk's own
// spelling, which is why one table and another write byte-identical text.
const tableJsonRuntime = `// The TEXT FORM's runtime (docs/SPEC-TABLES.md §16): pretty JSON in and out.
// A text is one table as one object; the writer is pretty-printed (one entry
// per line, two-space indent) and ends with exactly one newline (§16.1); the
// reader accepts whitespace and // and /* */ comments, tolerates unknown keys
// (skipped and counted), a key of the wrong shape (counted), a string longer
// than its bound (clamped at a code point boundary and counted) and a number
// outside its range (clamped and counted) (§16.2).
public final class TableJson {
    private TableJson() {}

    private static final char[] HEX = "0123456789abcdef".toCharArray();

    // indent starts a new line at the given depth: two spaces per level.
    public static void indent(StringBuilder b, int depth) {
        b.append('\n');
        for (int i = 0; i < depth; i++) { b.append("  "); }
    }

    // esc writes one string as a JSON string, escaping the code points JSON
    // requires and passing every other code point through raw (§16.2). A
    // control character becomes an escape; a lone surrogate is a replacement
    // character (§16.3).
    public static void esc(StringBuilder b, String s) {
        b.append('"');
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            switch (c) {
                case '"':  b.append("\\\""); break;
                case '\\': b.append("\\\\"); break;
                case '\b': b.append("\\b"); break;
                case '\f': b.append("\\f"); break;
                case '\n': b.append("\\n"); break;
                case '\r': b.append("\\r"); break;
                case '\t': b.append("\\t"); break;
                default:
                    if (c < 0x20) {
                        b.append("\\u00").append(HEX[(c >> 4) & 0xf]).append(HEX[c & 0xf]);
                    } else if (c >= 0xd800 && c <= 0xdfff) {
                        if (c >= 0xdc00 || i + 1 >= s.length()
                                || s.charAt(i + 1) < 0xdc00 || s.charAt(i + 1) > 0xdfff) {
                            b.append('\ufffd');
                        } else {
                            b.append(c).append(s.charAt(i + 1));
                            i++;
                        }
                    } else {
                        b.append(c);
                    }
            }
        }
        b.append('"');
    }

    // THE READER: a cursor over the byte[] the caller owns, one report shared
    // with the wire (§4). Nothing allocates beyond the strings it reads.
    public static final class In {
        final byte[] t;
        int p;
        final TableVariable.Report rep;
        boolean bad;

        In(byte[] t, TableVariable.Report rep) { this.t = t; this.rep = rep; }

        // space skips whitespace and both comment forms (§16.2). An unclosed
        // block comment is malformed, as an unclosed string is.
        void space() {
            while (true) {
                if (p >= t.length) { return; }
                byte c = t[p];
                if (c == ' ' || c == '\t' || c == '\n' || c == '\r') { p++; continue; }
                if (c == '/' && p + 1 < t.length) {
                    if (t[p + 1] == '/') {
                        p += 2;
                        while (p < t.length && t[p] != '\n') { p++; }
                        continue;
                    }
                    if (t[p + 1] == '*') {
                        p += 2;
                        while (p + 1 < t.length && !(t[p] == '*' && t[p + 1] == '/')) { p++; }
                        if (p + 1 >= t.length) { bad = true; return; }
                        p += 2;
                        continue;
                    }
                }
                return;
            }
        }

        int peek() { space(); return p < t.length ? (t[p] & 0xff) : 0; }

        boolean lit(String w) {
            space();
            if (p + w.length() > t.length) { bad = true; return false; }
            for (int i = 0; i < w.length(); i++) {
                if ((char) (t[p + i] & 0xff) != w.charAt(i)) { bad = true; return false; }
            }
            p += w.length();
            return true;
        }

        // isNull reports a null literal without consuming it.
        boolean isNull() { return peek() == 'n'; }

        int hex4() {
            if (p + 4 > t.length) { return -1; }
            int v = 0;
            for (int i = 0; i < 4; i++) {
                int c = t[p + i] & 0xff;
                int d;
                if (c >= '0' && c <= '9') { d = c - '0'; }
                else if (c >= 'a' && c <= 'f') { d = c - 'a' + 10; }
                else if (c >= 'A' && c <= 'F') { d = c - 'A' + 10; }
                else { return -1; }
                v = (v << 4) | d;
            }
            p += 4;
            return v;
        }

        // scanString reads one JSON string, decoding escapes into a String. A
        // raw control character in the body, an invalid escape, or a lone
        // surrogate half is malformed (§16.2); a surrogate half that never met
        // its partner reads as the replacement character (§16.3).
        public String scanString() {
            space();
            if (p >= t.length || t[p] != '"') { bad = true; return null; }
            p++;
            StringBuilder b = new StringBuilder();
            while (true) {
                if (p >= t.length) { bad = true; return null; }
                int c = t[p] & 0xff;
                p++;
                if (c == '"') { return b.toString(); }
                if (c == '\\') {
                    if (p >= t.length) { bad = true; return null; }
                    int e = t[p] & 0xff;
                    p++;
                    switch (e) {
                        case '"': b.append('"'); break;
                        case '\\': b.append('\\'); break;
                        case '/': b.append('/'); break;
                        case 'b': b.append('\b'); break;
                        case 'f': b.append('\f'); break;
                        case 'n': b.append('\n'); break;
                        case 'r': b.append('\r'); break;
                        case 't': b.append('\t'); break;
                        case 'u': {
                            int hi = hex4();
                            if (hi < 0) { bad = true; return null; }
                            if (hi >= 0xd800 && hi <= 0xdbff && p + 2 <= t.length
                                    && t[p] == '\\' && t[p + 1] == 'u') {
                                int mark = p;
                                p += 2;
                                int lo = hex4();
                                if (lo >= 0xdc00 && lo <= 0xdfff) {
                                    b.appendCodePoint(0x10000 + ((hi - 0xd800) << 10) + (lo - 0xdc00));
                                } else {
                                    p = mark;
                                    b.append('\ufffd');
                                }
                            } else if (hi >= 0xd800 && hi <= 0xdfff) {
                                b.append('\ufffd');
                            } else {
                                b.append((char) hi);
                            }
                            break;
                        }
                        default: bad = true; return null;
                    }
                } else if (c < 0x20) {
                    bad = true; return null;
                } else if (c < 0x80) {
                    b.append((char) c);
                } else {
                    int cp = utf8(c);
                    if (cp < 0) { b.append('\ufffd'); } else { b.appendCodePoint(cp); }
                }
            }
        }

        // utf8 reads one UTF-8 sequence whose lead byte is c (>= 0x80). -1 is a
        // stray or ill-formed sequence, read as one replacement (§16.3).
        int utf8(int lead) {
            int want, cp;
            if (lead >= 0xc2 && lead <= 0xdf) { want = 2; cp = lead & 0x1f; }
            else if (lead >= 0xe0 && lead <= 0xef) { want = 3; cp = lead & 0x0f; }
            else if (lead >= 0xf0 && lead <= 0xf4) { want = 4; cp = lead & 0x07; }
            else { return -1; }
            if (p + want - 1 > t.length) { return -1; }
            for (int i = 1; i < want; i++) {
                int n = t[p + i - 1] & 0xff;
                if ((n & 0xc0) != 0x80) { return -1; }
                cp = (cp << 6) | (n & 0x3f);
            }
            p += want - 1;
            if (want == 3 && cp < 0x800) { return -1; }
            if (want == 4 && cp < 0x10000) { return -1; }
            if (cp > 0x10ffff) { return -1; }
            return cp;
        }

        // scanNumber reads one JSON number token (RFC 8259 §6) and reports
        // whether it is an integral spelling: no fraction, no exponent (§16.2).
        // A token that is not a JSON number is malformed.
        public String scanNumber(boolean[] integral) {
            space();
            int start = p;
            if (p < t.length && t[p] == '-') { p++; }
            if (p >= t.length) { bad = true; return null; }
            if (t[p] == '0') { p++; }
            else if (t[p] >= '1' && t[p] <= '9') {
                while (p < t.length && t[p] >= '0' && t[p] <= '9') { p++; }
            } else { bad = true; return null; }
            integral[0] = true;
            if (p < t.length && t[p] == '.') {
                p++;
                if (p >= t.length || t[p] < '0' || t[p] > '9') { bad = true; return null; }
                while (p < t.length && t[p] >= '0' && t[p] <= '9') { p++; }
                integral[0] = false;
            }
            if (p < t.length && (t[p] == 'e' || t[p] == 'E')) {
                p++;
                if (p < t.length && (t[p] == '-' || t[p] == '+')) { p++; }
                if (p >= t.length || t[p] < '0' || t[p] > '9') { bad = true; return null; }
                while (p < t.length && t[p] >= '0' && t[p] <= '9') { p++; }
                integral[0] = false;
            }
            return new String(t, start, p - start, java.nio.charset.StandardCharsets.US_ASCII);
        }

        // integer reads one number into a long, clamping to the field's declared
        // range and counting a genuinely fractional value as a kind mismatch
        // (never coerced), a value past the range as a clamp (§16.2).
        public long integer(boolean signed, long min, long max) {
            boolean[] integral = new boolean[1];
            String tok = scanNumber(integral);
            if (bad) { return 0; }
            long v;
            if (integral[0]) {
                try {
                    v = Long.parseLong(tok);
                } catch (NumberFormatException e) {
                    rep.clamped++;
                    return tok.charAt(0) == '-' ? min : max;
                }
            } else {
                double d;
                try {
                    d = Double.parseDouble(tok);
                } catch (NumberFormatException e) {
                    bad = true;
                    return 0;
                }
                if (!Double.isFinite(d) || d != (double) (long) d) { rep.kindMismatch++; return 0; }
                v = (long) d;
            }
            if (!signed && v < 0) { rep.clamped++; return 0; }
            if (v < min) { rep.clamped++; return min; }
            if (v > max) { rep.clamped++; return max; }
            return v;
        }

        // readString reads one string and clamps it at the bound, counting the
        // clamp (§16.2). The cut lands at a code point boundary, never through
        // a multi-byte character.
        public String readString(int bound) {
            String s = scanString();
            if (s == null) { return ""; }
            return clamp(s, bound);
        }

        String clamp(String s, int bound) {
            byte[] b = s.getBytes(java.nio.charset.StandardCharsets.UTF_8);
            if (b.length <= bound) { return s; }
            rep.clamped++;
            int keep = bound;
            // back off to a code point boundary
            while (keep > 0 && (b[keep] & 0xc0) == 0x80) { keep--; }
            return new String(b, 0, keep, java.nio.charset.StandardCharsets.UTF_8);
        }

        // skipValue steps over one value of any shape, so an unknown key's
        // value is dropped whole and never read as a field (§16.2, §4).
        public void skipValue() {
            int c = peek();
            switch (c) {
                case '{': skipContainer('}'); return;
                case '[': skipContainer(']'); return;
                case '"': scanString(); return;
                case 't': lit("true"); return;
                case 'f': lit("false"); return;
                case 'n': lit("null"); return;
                case 0: bad = true; return;
                default: {
                    boolean[] integral = new boolean[1];
                    scanNumber(integral);
                }
            }
        }

        void skipContainer(char closing) {
            p++; // the opening bracket
            while (true) {
                int c = peek();
                if (c == closing) { p++; return; }
                if (c == 0) { bad = true; return; }
                if (closing == '}') {
                    scanString();
                    if (bad) { return; }
                    if (peek() != ':') { bad = true; return; }
                    p++;
                }
                skipValue();
                if (bad) { return; }
                c = peek();
                if (c == ',') { p++; continue; }
                if (c == closing) { p++; return; }
                bad = true;
                return;
            }
        }
    }
}
`

// generateVariableFiles emits the variable-form runtime and one load/save codec
// per variable table (<Type>Variable.java). The codecs write form byte 1
// (docs/SPEC-TABLES.md §3) for the card's constructs — bool/int32/int64/enum
// fields, string(N), the *string byte buffer (§2.5) and unbounded lists of
// tables (§2.9) — and read it tolerantly (§4): unknown ids skipped and counted,
// absent fields defaulted, kind mismatch reported. The id table, the node table
// and the first-use ordering are the runtime's; the per-table codec names the
// field ids and the value shape only.
func generateVariableFiles(u *ir.Unit) (map[string][]byte, error) {
	// The variable wire is emitted for the variable tables that pass the card
	// boundary (docs/SPEC-TABLES.md §2); a variable table with any other
	// construct stays ABSENT — its wire is not emitted, never mis-encoded —
	// exactly as the Rust backend's scopedVariableModules does
	// (internal/codegen/rusttable/variable.go). The by-name refusal (§11) is
	// refuseVariableForm's own job, on the card's path (cardsform_test.go), not
	// this one's: a unit that mixes a fixed table with a pointered variable
	// table (tables/pointers) still emits its accelerators and fixed form.
	tables := cardVariableTables(u)
	if len(tables) == 0 {
		return map[string][]byte{}, nil
	}
	out := map[string][]byte{
		"TableVariable.java": javaFile(u, "the VARIABLE FORM's runtime (docs/SPEC-TABLES.md §3), form byte 1", tableVariableRuntime),
		"TableJson.java":     javaFile(u, "the TEXT form's runtime (docs/SPEC-TABLES.md §16), JSON in and out", tableJsonRuntime),
	}
	for _, st := range tables {
		src, err := generateTableVariable(u, st)
		if err != nil {
			return nil, err
		}
		out[st.Name+"Variable.java"] = javaFile(u,
			fmt.Sprintf("the variable form's tolerant load and canonical save for table %s (docs/SPEC-TABLES.md §3, §4)", st.Name),
			src)
	}
	return out, nil
}

// variableWireKind is a field's wire kind (docs/SPEC-TABLES.md §3): the closed
// set's numbers, with an enum riding as kind 30 carrying its variant reference,
// a *string byte buffer as kind 17 (a node index) and an unbounded list of
// tables as kind 14 with element kind 13.
func variableWireKind(f *ir.Field) int {
	switch f.Type.Kind {
	case ir.TBool:
		return 1
	case ir.TInt:
		if f.Type.Signed {
			switch f.Type.Width {
			case 8:
				return 2
			case 16:
				return 3
			case 32:
				return 4
			default:
				return 5
			}
		}
		switch f.Type.Width {
		case 8:
			return 6
		case 16:
			return 7
		case 32:
			return 8
		default:
			return 9
		}
	case ir.TString:
		if f.Type.Blob() {
			return 17
		}
		return 12
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			return 30
		case *ir.Struct:
			if f.Array == ir.ArrayList {
				return 14
			}
			return 13
		}
	}
	return 0
}

// varMember is a field's Java member name in the VARIABLE form's value class.
// A field whose Java spelling is a reserved word (the card's `_new` -> `new`)
// takes a trailing underscore, which the fixed form's raw member() must not
// silently apply, so the escape lives here and checkNames skips variable tables.
func varMember(f *ir.Field) string {
	m := member(f)
	if javaReserved[m] {
		return m + "_"
	}
	return m
}

// variableValueType is a field's Java storage type in the generated value class.
func variableValueType(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "boolean"
	case ir.TInt:
		if f.Type.Width <= 32 {
			return "int"
		}
		return "long"
	case ir.TString:
		return "String"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			return "int"
		case *ir.Struct:
			if f.Array == ir.ArrayList {
				return "List<" + f.Type.Name + "Variable.Value>"
			}
			return f.Type.Name + "Variable.Value"
		}
	}
	return "Object"
}

// variableDefault is a field's Java zero value in the generated value class. A
// *string byte buffer is born null (absence), a string(N) born empty.
func variableDefault(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "false"
	case ir.TInt:
		if f.Type.Width <= 32 {
			return "0"
		}
		return "0L"
	case ir.TString:
		if f.Type.Blob() {
			return "null"
		}
		return "\"\""
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			return "0"
		case *ir.Struct:
			if f.Array == ir.ArrayList {
				return "new ArrayList<>()"
			}
			return "null"
		}
	}
	return "null"
}

// generateTableVariable emits one <Type>Variable.java: a nested Value class, the
// field-id constants, readFields (tolerant) and writeFields (canonical), plus a
// full load/save that frames a root body with the form byte, the node table and
// the id-table trailer.
func generateTableVariable(u *ir.Unit, st *ir.Struct) (string, error) {
	var b strings.Builder
	b.WriteString("import java.nio.charset.StandardCharsets;\n")
	b.WriteString("import java.util.ArrayList;\n")
	b.WriteString("import java.util.List;\n\n")
	fmt.Fprintf(&b, "public final class %sVariable {\n", st.Name)
	b.WriteString("    private " + st.Name + "Variable() {}\n\n")

	// The value class: one public member per field.
	fmt.Fprintf(&b, "    public static final class Value {\n")
	for _, f := range st.Fields {
		fmt.Fprintf(&b, "        public %s %s = %s;\n", variableValueType(f), varMember(f), variableDefault(f))
		if f.Type.Kind == ir.TString && f.Type.Blob() {
			fmt.Fprintf(&b, "        public long %sIdx;\n", varMember(f))
		}
	}
	b.WriteString("    }\n\n")

	// Field-id constants (§5): fnv1a64 of the field's wire name.
	for _, f := range st.Fields {
		fmt.Fprintf(&b, "    public static final long F_%s = 0x%016xL;\n", varMember(f), ir.TableFieldWireId(f))
	}
	b.WriteString("\n")

	// readFields: tolerant, deferring *string indices until the node table is
	// known. nt is non-null only for the root body, whose reserved field carries
	// the node table.
	fmt.Fprintf(&b, "    public static boolean readFields(TableVariable.Reader r, Value v, TableVariable.NodeTable nt, TableVariable.Report report) {\n")
	b.WriteString("        while (true) {\n")
	b.WriteString("            long ref = r.leb();\n")
	b.WriteString("            if (r.fail) { return false; }\n")
	b.WriteString("            if (ref == 0) { return true; }\n")
	b.WriteString("            long id = r.resolve(ref);\n")
	b.WriteString("            if (r.fail) { return false; }\n")
	b.WriteString("            if (!r.has(1)) { report.malformed = true; return false; }\n")
	b.WriteString("            int kind = r.get8();\n")
	first := true
	for _, f := range st.Fields {
		if !first {
			b.WriteString("            } else ")
		} else {
			b.WriteString("            ")
		}
		first = false
		fmt.Fprintf(&b, "if (id == F_%s) {\n", varMember(f))
		writeFieldRead(&b, u, st, f)
	}
	b.WriteString("            } else if (id == TableVariable.NODE_ID) {\n")
	b.WriteString("                if (nt != null) { r.skip(kind); }\n")
	b.WriteString("                else { report.malformed = true; return false; }\n")
	b.WriteString("            } else if (id == TableVariable.BUILD_ID || id == TableVariable.VOCAB_ID) {\n")
	b.WriteString("                report.malformed = true; return false;\n")
	b.WriteString("            } else {\n")
	b.WriteString("                r.skip(kind); report.unknown++;\n")
	b.WriteString("            }\n")
	b.WriteString("            if (r.fail) { return false; }\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")

	// resolveNodes: turn every deferred *string index into its bytes, counting a
	// root index as a kind mismatch and an out-of-range index as malformed (§3.1).
	// A numbering that failed (nt.bad) resolves nothing: every pointer stays null
	// and no counter moves, exactly as the oracle's resolveCell returns before
	// its switch when decodeState.good is false (internal/tablewire/decodenodes.go).
	fmt.Fprintf(&b, "    public static void resolveNodes(Value v, TableVariable.NodeTable nt, TableVariable.Report report) {\n")
	b.WriteString("        if (nt.bad) { return; }\n")
	for _, f := range st.Fields {
		if f.Type.Kind == ir.TString && f.Type.Blob() {
			fmt.Fprintf(&b, "        if (v.%sIdx != 0) { v.%s = TableVariable.resolveString(nt.recs, v.%sIdx, report); }\n", varMember(f), varMember(f), varMember(f))
		} else if f.Array == ir.ArrayList {
			if elem, ok := f.Type.Ref.(*ir.Struct); ok {
				fmt.Fprintf(&b, "        for (%sVariable.Value e : v.%s) { %sVariable.resolveNodes(e, nt, report); }\n", elem.Name, varMember(f), elem.Name)
			}
		}
	}
	b.WriteString("    }\n\n")

	// writeFields: canonical, in declaration order, eliding defaults.
	fmt.Fprintf(&b, "    public static void writeFields(TableVariable.Writer w, TableVariable.NodeList nodes, Value v) {\n")
	for _, f := range st.Fields {
		writeFieldSave(&b, u, st, f)
	}
	b.WriteString("    }\n\n")

	// The enum readers: a variant reference the table cannot name is §4's
	// ordinary unknown, the field reading None (0).
	for _, f := range st.Fields {
		if enum, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(&b, "    private static int %sEnum(long vid, TableVariable.Report report) {\n", varMember(f))
			for i := range enum.Variants {
				fmt.Fprintf(&b, "        if (vid == 0x%016xL) { return %d; }\n", ir.TableWireId(enum.VariantWireName(i)), i+1)
			}
			b.WriteString("        report.unknown++;\n        return 0;\n    }\n\n")
		}
	}

	// The root framing: form byte, writeFields, node table (last, §3.1), trailer.
	fmt.Fprintf(&b, "    public static byte[] save(Value v) {\n")
	fmt.Fprintf(&b, "        byte[] buf = new byte[1 << 20];\n")
	fmt.Fprintf(&b, "        byte[] scratch = new byte[1 << 20];\n")
	fmt.Fprintf(&b, "        TableVariable.Ids ids = new TableVariable.Ids(64);\n")
	fmt.Fprintf(&b, "        TableVariable.Writer w = new TableVariable.Writer(buf, ids);\n")
	fmt.Fprintf(&b, "        TableVariable.NodeList nodes = new TableVariable.NodeList();\n")
	fmt.Fprintf(&b, "        w.put8(1);\n")
	fmt.Fprintf(&b, "        writeFields(w, nodes, v);\n")
	fmt.Fprintf(&b, "        if (nodes.size() > 0) {\n")
	fmt.Fprintf(&b, "            long nodeRef = w.ref(TableVariable.NODE_ID);\n")
	fmt.Fprintf(&b, "            TableVariable.Writer nw = w.sub(scratch);\n")
	fmt.Fprintf(&b, "            nw.putLeb(nodes.size());\n")
	fmt.Fprintf(&b, "            for (int i = 0; i < nodes.size(); i++) {\n")
	fmt.Fprintf(&b, "                byte[] body = nodes.body(i);\n")
	fmt.Fprintf(&b, "                nw.putLeb(nw.ref(TableVariable.STRING_ID));\n")
	fmt.Fprintf(&b, "                nw.putLeb(body.length);\n")
	fmt.Fprintf(&b, "                nw.putBytes(body, 0, body.length);\n")
	fmt.Fprintf(&b, "            }\n")
	fmt.Fprintf(&b, "            w.putLeb(nodeRef);\n")
	fmt.Fprintf(&b, "            w.put8(12);\n")
	fmt.Fprintf(&b, "            w.putLeb(nw.off);\n")
	fmt.Fprintf(&b, "            w.putBytes(scratch, 0, nw.off);\n")
	fmt.Fprintf(&b, "        }\n")
	fmt.Fprintf(&b, "        w.put8(0);\n")
	fmt.Fprintf(&b, "        w.trailer();\n")
	fmt.Fprintf(&b, "        return java.util.Arrays.copyOf(buf, w.off);\n")
	fmt.Fprintf(&b, "    }\n\n")

	// load: form byte, id table, readFields, then resolveNodes.
	fmt.Fprintf(&b, "    public static Value load(byte[] data, TableVariable.Report report) {\n")
	fmt.Fprintf(&b, "        TableVariable.Reader r = TableVariable.open(data, report);\n")
	fmt.Fprintf(&b, "        if (r == null) { return null; }\n")
	fmt.Fprintf(&b, "        Value v = new Value();\n")
	fmt.Fprintf(&b, "        TableVariable.NodeTable nt = TableVariable.readNodeTable(r, report);\n")
	fmt.Fprintf(&b, "        readFields(r, v, nt, report);\n")
	fmt.Fprintf(&b, "        if (report.refused) { return null; }\n")
	fmt.Fprintf(&b, "        resolveNodes(v, nt, report);\n")
	fmt.Fprintf(&b, "        return v;\n")
	fmt.Fprintf(&b, "    }\n\n")

	writeLoadMeasure(&b, u, st)
	writeJsonCodec(&b, u, st)

	b.WriteString("}\n")
	return b.String(), nil
}

// writeLoadMeasure emits the region measure (docs/SPEC-TABLES.md §6.5) a
// tolerant load would ask for: the canonical extent the independent oracle's
// FileRegionMeasure derives from the framing. The wire-fuzz harness holds the
// leg to it (§4.2), so this is the variable form's answer to LoadMeasure — the
// root's rounded record, the extent its list elements command, and the blob
// node records' rounded bodies plus the directory.
func writeLoadMeasure(b *strings.Builder, u *ir.Unit, st *ir.Struct) {
	align := ir.TableRegionAlign(u)
	recordSize := ir.RecordLayout(u, st).Size
	b.WriteString("    // loadMeasure sizes the region a tolerant load would need (§6.5).\n")
	fmt.Fprintf(b, "    private static final long ALIGN = %d;\n", align)
	fmt.Fprintf(b, "    private static final long RECORD_SIZE = %d;\n", recordSize)
	b.WriteString("    private static long alignUp(long n, long a) { return (n + a - 1) & -a; }\n")
	b.WriteString("    private static long round(long n) { return (n + ALIGN - 1) & -ALIGN; }\n")
	fmt.Fprintf(b, "    public static long loadMeasure(byte[] data) {\n")
	fmt.Fprintf(b, "        TableVariable.Report rep = new TableVariable.Report();\n")
	fmt.Fprintf(b, "        TableVariable.Reader r = TableVariable.open(data, rep);\n")
	fmt.Fprintf(b, "        if (r == null) { return -1; }\n")
	fmt.Fprintf(b, "        TableVariable.NodeTable nt = new TableVariable.NodeTable();\n")
	fmt.Fprintf(b, "        long extent = measureFields(r, nt, rep);\n")
	fmt.Fprintf(b, "        if (rep.refused) { return -1; }\n")
	fmt.Fprintf(b, "        long total = round(round(RECORD_SIZE) + extent);\n")
	fmt.Fprintf(b, "        int records = 0;\n")
	fmt.Fprintf(b, "        if (nt.recs != null) {\n")
	fmt.Fprintf(b, "            records = nt.recs.length;\n")
	fmt.Fprintf(b, "            for (TableVariable.NodeRecord rec : nt.recs) {\n")
	fmt.Fprintf(b, "                if (rec.typeId == TableVariable.STRING_ID) { total += round(8 + rec.body.length + 1); }\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "        total += (long) (records + 1) * 16;\n")
	fmt.Fprintf(b, "        return total;\n")
	fmt.Fprintf(b, "    }\n\n")

	// measureFields walks the body once, accumulating the extent the list
	// elements command, and capturing the node table for the blob records.
	fmt.Fprintf(b, "    static long measureFields(TableVariable.Reader r, TableVariable.NodeTable nt, TableVariable.Report report) {\n")
	fmt.Fprintf(b, "        long extent = 0;\n")
	fmt.Fprintf(b, "        while (true) {\n")
	fmt.Fprintf(b, "            long ref = r.leb();\n")
	fmt.Fprintf(b, "            if (ref == 0) { return extent; }\n")
	fmt.Fprintf(b, "            int kind = r.get8();\n")
	fmt.Fprintf(b, "            long id = r.resolve(ref);\n")
	first := true
	for _, f := range st.Fields {
		if !first {
			b.WriteString("            } else ")
		} else {
			b.WriteString("            ")
		}
		first = false
		fmt.Fprintf(b, "if (id == F_%s) {\n", varMember(f))
		if elem, ok := f.Type.Ref.(*ir.Struct); ok && f.Array == ir.ArrayList {
			size, elemAlign := ir.ListElementLayout(u, f)
			fmt.Fprintf(b, "                if (kind == 14) {\n")
			fmt.Fprintf(b, "                    TableVariable.Reader ar = r.body();\n")
			fmt.Fprintf(b, "                    if (ar != null) { if (!ar.has(1)) { report.malformed = true; r.fail = true; } else { int ek = ar.get8(); if (ek == 13) { long n = ar.leb(); if (n < 0 || n > 0x7FFFFFFFL || n > (long) (ar.limit - ar.off) / 2) { report.refused = true; } else { extent = alignUp(extent, %d) + n * %d; for (long i = 0; i < n; i++) { TableVariable.Reader er = ar.body(); if (er == null) { break; } extent += %sVariable.measureFields(er, null, report); } } } } }\n", elemAlign, size, elem.Name)
			fmt.Fprintf(b, "                } else { r.skip(kind); }\n")
		} else {
			fmt.Fprintf(b, "                r.skip(kind);\n")
		}
	}
	b.WriteString("            } else if (id == TableVariable.NODE_ID && nt != null) {\n")
	b.WriteString("                if (kind == 12) { TableVariable.Reader sub = r.body(); if (sub != null) { nt.recs = TableVariable.nodesRaw(sub); } }\n")
	b.WriteString("                else { r.skip(kind); }\n")
	b.WriteString("            } else {\n")
	b.WriteString("                r.skip(kind);\n")
	b.WriteString("            }\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")
}

// writeJsonCodec emits the TEXT form for one variable table (docs/SPEC-TABLES.md
// §16): a pretty JSON writer, every field in declaration order with defaults
// included, and a tolerant reader that names the same keys. The shared
// spelling — indentation, escaping, the skip of an unknown value — lives in
// TableJson; here the codec names the keys and the value shape only.
func writeJsonCodec(b *strings.Builder, u *ir.Unit, st *ir.Struct) {
	b.WriteString("    // THE TEXT FORM (docs/SPEC-TABLES.md §16): pretty JSON in and out.\n")
	b.WriteString("    public static byte[] toJson(Value v) {\n")
	b.WriteString("        StringBuilder b = new StringBuilder();\n")
	b.WriteString("        jsonFields(b, v, 0);\n")
	b.WriteString("        b.append('\\n');\n")
	b.WriteString("        return b.toString().getBytes(StandardCharsets.UTF_8);\n")
	b.WriteString("    }\n\n")

	b.WriteString("    public static Value fromJson(byte[] text, TableVariable.Report report) {\n")
	b.WriteString("        TableJson.In in = new TableJson.In(text, report);\n")
	b.WriteString("        Value v = new Value();\n")
	b.WriteString("        jsonReadFields(in, v);\n")
	b.WriteString("        if (in.bad) { report.malformed = true; }\n")
	b.WriteString("        return v;\n")
	b.WriteString("    }\n\n")

	// jsonFields: the writer's one walk over the fields, in declaration order.
	fmt.Fprintf(b, "    static void jsonFields(StringBuilder b, Value v, int depth) {\n")
	b.WriteString("        b.append('{');\n")
	b.WriteString("        boolean first = true;\n")
	for _, f := range st.Fields {
		fmt.Fprintf(b, "        if (!first) { b.append(','); }\n")
		b.WriteString("        first = false;\n")
		b.WriteString("        TableJson.indent(b, depth + 1);\n")
		fmt.Fprintf(b, "        TableJson.esc(b, %q);\n", jsonKey(f))
		b.WriteString("        b.append(\": \");\n")
		writeJsonFieldValue(b, u, st, f)
	}
	b.WriteString("        TableJson.indent(b, depth);\n")
	b.WriteString("        b.append('}');\n")
	b.WriteString("    }\n\n")

	// jsonReadFields: the reader's walk, matching each key by name.
	fmt.Fprintf(b, "    static void jsonReadFields(TableJson.In in, Value v) {\n")
	b.WriteString("        in.space();\n")
	b.WriteString("        if (in.peek() != '{') { in.bad = true; return; }\n")
	b.WriteString("        in.p++;\n")
	b.WriteString("        while (true) {\n")
	b.WriteString("            int c = in.peek();\n")
	b.WriteString("            if (c == 0) { in.bad = true; return; }\n")
	b.WriteString("            if (c == '}') { in.p++; return; }\n")
	b.WriteString("            String key = in.scanString();\n")
	b.WriteString("            if (in.bad) { return; }\n")
	b.WriteString("            if (in.peek() != ':') { in.bad = true; return; }\n")
	b.WriteString("            in.p++;\n")
	for _, f := range st.Fields {
		fmt.Fprintf(b, "            if (%q.equals(key)) {\n", jsonKey(f))
		readJsonFieldValue(b, u, st, f)
		b.WriteString("            } else ")
	}
	b.WriteString("{\n")
	b.WriteString("                in.skipValue(); in.rep.unknown++;\n")
	b.WriteString("            }\n")
	b.WriteString("            c = in.peek();\n")
	b.WriteString("            if (c == ',') { in.p++; continue; }\n")
	b.WriteString("            if (c == '}') { in.p++; return; }\n")
	b.WriteString("            in.bad = true; return;\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")
}

// jsonKey is a field's key in the text form (docs/SPEC-TABLES.md §16.3): the
// `json = "key"` attribute where one is declared, the field's own name
// otherwise.
func jsonKey(f *ir.Field) string { return ir.TableFieldJsonKey(f) }

// jsonIntRange is a field's clamped range in the text form (docs/SPEC-TABLES.md
// §16.2): the declared [min, max] where one is given, the storage width's whole
// range otherwise — the wire's clamp rule, carried across.
func jsonIntRange(f *ir.Field) (min, max string) {
	if f.Type.Width <= 32 {
		min, max = "Integer.MIN_VALUE", "Integer.MAX_VALUE"
	} else {
		min, max = "Long.MIN_VALUE", "Long.MAX_VALUE"
	}
	if f.HasIntRange {
		min = f.IntMin.String()
		max = f.IntMax.String()
	}
	return min, max
}

// writeJsonFieldValue emits the writer's spelling of one field's value at the
// field's own depth (docs/SPEC-TABLES.md §16.2), for the card's constructs:
// bool, int32/int64, string(N), the *string byte buffer (§2.5), an enum by
// variant name and an unbounded list of tables (§2.9).
func writeJsonFieldValue(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	m := varMember(f)
	switch {
	case f.Type.Kind == ir.TString && f.Type.Blob():
		fmt.Fprintf(b, "        if (v.%s == null) { b.append(\"null\"); } else { TableJson.esc(b, v.%s); }\n", m, m)
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(b, "        TableJson.esc(b, v.%s);\n", m)
	case f.Type.Kind == ir.TBool:
		fmt.Fprintf(b, "        b.append(v.%s ? \"true\" : \"false\");\n", m)
	case f.Type.Kind == ir.TInt && f.Type.Width <= 32:
		fmt.Fprintf(b, "        b.append(Integer.toString(v.%s));\n", m)
	case f.Type.Kind == ir.TInt:
		fmt.Fprintf(b, "        b.append(Long.toString(v.%s));\n", m)
	case f.Type.Kind == ir.TNamed:
		if enum, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(b, "        switch (v.%s) {\n", m)
			for i := range enum.Variants {
				fmt.Fprintf(b, "            case %d: TableJson.esc(b, %q); break;\n", i+1, enum.Variants[i])
			}
			b.WriteString("            default: TableJson.esc(b, \"None\"); break;\n")
			b.WriteString("        }\n")
		} else if elem, ok := f.Type.Ref.(*ir.Struct); ok && f.Array == ir.ArrayList {
			fmt.Fprintf(b, "        if (v.%s.isEmpty()) { b.append(\"[]\"); } else {\n", m)
			b.WriteString("            b.append('[');\n")
			fmt.Fprintf(b, "            for (int i = 0; i < v.%s.size(); i++) {\n", m)
			b.WriteString("                if (i > 0) { b.append(','); }\n")
			b.WriteString("                TableJson.indent(b, depth + 2);\n")
			fmt.Fprintf(b, "                %sVariable.jsonFields(b, v.%s.get(i), depth + 2);\n", elem.Name, m)
			b.WriteString("            }\n")
			b.WriteString("            TableJson.indent(b, depth + 1);\n")
			b.WriteString("            b.append(']');\n")
			b.WriteString("        }\n")
		}
	}
}

// readJsonFieldValue emits the reader's spelling of one field's value, checking
// the JSON shape and counting a wrong one as a kind mismatch, never coercing
// (docs/SPEC-TABLES.md §16.2).
func readJsonFieldValue(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	m := varMember(f)
	switch {
	case f.Type.Kind == ir.TString && f.Type.Blob():
		fmt.Fprintf(b, "                if (in.peek() == 'n') { in.lit(\"null\"); v.%s = null; }\n", m)
		fmt.Fprintf(b, "                else if (in.peek() == '\"') { v.%s = in.scanString(); }\n", m)
		b.WriteString("                else { in.skipValue(); in.rep.kindMismatch++; }\n")
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(b, "                if (in.peek() == '\"') { v.%s = in.readString(%d); }\n", m, f.Type.Size)
		b.WriteString("                else { in.skipValue(); in.rep.kindMismatch++; }\n")
	case f.Type.Kind == ir.TBool:
		b.WriteString("                if (in.peek() == 't') { in.lit(\"true\"); v." + m + " = true; }\n")
		b.WriteString("                else if (in.peek() == 'f') { in.lit(\"false\"); v." + m + " = false; }\n")
		b.WriteString("                else { in.skipValue(); in.rep.kindMismatch++; }\n")
	case f.Type.Kind == ir.TInt:
		min, max := jsonIntRange(f)
		if f.Type.Width <= 32 {
			fmt.Fprintf(b, "                if (in.peek() == '-' || (in.peek() >= '0' && in.peek() <= '9')) { v.%s = (int) in.integer(true, %s, %s); }\n", m, min, max)
		} else {
			fmt.Fprintf(b, "                if (in.peek() == '-' || (in.peek() >= '0' && in.peek() <= '9')) { v.%s = in.integer(true, %s, %s); }\n", m, min, max)
		}
		b.WriteString("                else { in.skipValue(); in.rep.kindMismatch++; }\n")
	case f.Type.Kind == ir.TNamed:
		if enum, ok := f.Type.Ref.(*ir.Enum); ok {
			b.WriteString("                if (in.peek() == '\"') {\n")
			b.WriteString("                    String s = in.scanString();\n")
			for i := range enum.Variants {
				fmt.Fprintf(b, "                    if (%q.equals(s)) { v.%s = %d; }\n", enum.Variants[i], m, i+1)
				b.WriteString("                    else ")
			}
			fmt.Fprintf(b, "{ v.%s = 0; in.rep.unknown++; }\n", m)
			b.WriteString("                } else { in.skipValue(); in.rep.kindMismatch++; }\n")
		} else if elem, ok := f.Type.Ref.(*ir.Struct); ok && f.Array == ir.ArrayList {
			b.WriteString("                if (in.peek() == '[') {\n")
			b.WriteString("                    in.p++;\n")
			b.WriteString("                    while (true) {\n")
			b.WriteString("                        int ec = in.peek();\n")
			b.WriteString("                        if (ec == ']') { in.p++; break; }\n")
			b.WriteString("                        if (ec == 0) { in.bad = true; break; }\n")
			fmt.Fprintf(b, "                        if (ec != '{') { in.skipValue(); in.rep.kindMismatch++; }\n")
			fmt.Fprintf(b, "                        else { %sVariable.Value e = new %sVariable.Value(); %sVariable.jsonReadFields(in, e); v.%s.add(e); }\n", elem.Name, elem.Name, elem.Name, m)
			b.WriteString("                        ec = in.peek();\n")
			b.WriteString("                        if (ec == ',') { in.p++; continue; }\n")
			b.WriteString("                        if (ec == ']') { in.p++; break; }\n")
			b.WriteString("                        in.bad = true; break;\n")
			b.WriteString("                    }\n")
			b.WriteString("                } else { in.skipValue(); in.rep.kindMismatch++; }\n")
		}
	}
}

// writeFieldRead emits the tolerant read of one field into v.
func writeFieldRead(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	m := varMember(f)
	switch {
	case f.Type.Kind == ir.TString && f.Type.Blob():
		fmt.Fprintf(b, "                if (kind == 17) { v.%sIdx = r.leb(); }\n", m)
		fmt.Fprintf(b, "                else { r.skip(kind); report.kindMismatch++; }\n")
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(b, "                if (kind == 12) { long L = r.leb(); int n = (int) L; if (!r.fail && r.has(n)) { if (!TableVariable.textValid(r.buf, r.off, n)) { report.malformed = true; v.%s = \"\"; } else { int keep = n; if (keep > %d) { keep = %d; while (keep > 0 && (r.buf[r.off + keep] & 0xc0) == 0x80) { keep--; } report.clamped++; } v.%s = new String(r.buf, r.off, keep, StandardCharsets.UTF_8); } r.off += n; } else if (!r.fail) { report.malformed = true; r.fail = true; } }\n", m, f.Type.Size, f.Type.Size, m)
		fmt.Fprintf(b, "                else { r.skip(kind); report.kindMismatch++; }\n")
	case f.Type.Kind == ir.TBool:
		fmt.Fprintf(b, "                if (kind == 1) { if (r.has(1)) { v.%s = r.get8() != 0; } else { report.malformed = true; r.fail = true; } }\n", m)
		fmt.Fprintf(b, "                else { r.skip(kind); report.kindMismatch++; }\n")
	case f.Type.Kind == ir.TInt:
		writeIntFieldRead(b, f, m)
	case f.Type.Kind == ir.TNamed:
		if _, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(b, "                if (kind == 30) { long vref = r.leb(); if (!r.fail) { if (vref == 0) { v.%s = 0; } else { long vid = r.resolve(vref); if (!r.fail) { v.%s = %sEnum(vid, report); } } } }\n", m, m, m)
			b.WriteString("                else { r.skip(kind); report.kindMismatch++; }\n")
		} else if elem, ok := f.Type.Ref.(*ir.Struct); ok && f.Array == ir.ArrayList {
			fmt.Fprintf(b, "                if (kind == 14) { TableVariable.Reader ar = r.body(); if (ar != null) { if (ar.has(2)) { int ek = ar.get8(); if (ek == 13) { long n = ar.leb(); if (ar.fail) { } else if (n < 0 || n > 0x7FFFFFFFL || n > (long) (ar.limit - ar.off) / 2) { report.refused = true; } else { v.%s.clear(); for (long i = 0; i < n; i++) { TableVariable.Reader er = ar.body(); if (er == null) { break; } %sVariable.Value e = new %sVariable.Value(); %sVariable.readFields(er, e, null, report); if (er.off != er.limit) { report.malformed = true; e = new %sVariable.Value(); } v.%s.add(e); } } } else { report.kindMismatch++; } } } }\n", m, elem.Name, elem.Name, elem.Name, elem.Name, m)
			fmt.Fprintf(b, "                else { r.skip(kind); report.kindMismatch++; }\n")
		}
	}
}

// writeIntFieldRead emits the tolerant read of one integer field, carrying the
// signed widening ladder (§4): an int32 field reads an int8 or int16 payload
// sign-extended and counts `widened`, and an int64 field reads int8/int16/int32
// the same way. A declared range clamps every read value as the wire clamps
// (§4); a kind outside the field's own ladder is the ordinary kind mismatch.
func writeIntFieldRead(b *strings.Builder, f *ir.Field, m string) {
	k := variableWireKind(f)
	width := f.Type.Width
	read, has := "r.get32()", "4"
	if width > 32 {
		read, has = "r.get64()", "8"
	}
	var widen []int
	if f.Type.Signed {
		widen = []int{2, 3}
		if width > 32 {
			widen = append(widen, 4)
		}
	}
	cond := fmt.Sprintf("kind == %d", k)
	for _, w := range widen {
		cond += fmt.Sprintf(" || kind == %d", w)
	}
	fmt.Fprintf(b, "                if (%s) {\n", cond)
	fmt.Fprintf(b, "                    if (kind == %d) { if (r.has(%s)) { v.%s = %s; } else { report.malformed = true; r.fail = true; } }\n", k, has, m, read)
	for _, w := range widen {
		wread, whas := "", ""
		switch w {
		case 2:
			wread, whas = "(byte) r.get8()", "1"
		case 3:
			wread, whas = "(short) r.get16()", "2"
		case 4:
			wread, whas = "r.get32()", "4"
		}
		fmt.Fprintf(b, "                    else if (kind == %d) { if (r.has(%s)) { v.%s = %s; report.widened++; } else { report.malformed = true; r.fail = true; } }\n", w, whas, m, wread)
	}
	if f.HasIntRange {
		min, max := f.IntMin.String(), f.IntMax.String()
		if width > 32 {
			min += "L"
			max += "L"
		}
		fmt.Fprintf(b, "                    if (!r.fail && (v.%s < %s || v.%s > %s)) { if (v.%s < %s) { v.%s = %s; } else { v.%s = %s; } report.clamped++; }\n", m, min, m, max, m, min, m, min, m, max)
	}
	b.WriteString("                } else { r.skip(kind); report.kindMismatch++; }\n")
}

// writeFieldSave emits the canonical write of one field, eliding its default.
func writeFieldSave(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	m := varMember(f)
	switch {
	case f.Type.Kind == ir.TString && f.Type.Blob():
		fmt.Fprintf(b, "        if (v.%s != null) { w.putLeb(w.ref(F_%s)); w.put8(17); w.putLeb(nodes.ref(v.%s)); }\n", m, m, m)
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(b, "        if (!v.%s.isEmpty()) { byte[] b = v.%s.getBytes(StandardCharsets.UTF_8); w.putLeb(w.ref(F_%s)); w.put8(12); w.putLeb(b.length); w.putBytes(b, 0, b.length); }\n", m, m, m)
	case f.Type.Kind == ir.TBool:
		fmt.Fprintf(b, "        if (v.%s) { w.putLeb(w.ref(F_%s)); w.put8(1); w.put8(1); }\n", m, m)
	case f.Type.Kind == ir.TInt && f.Type.Width <= 32:
		k := variableWireKind(f)
		fmt.Fprintf(b, "        if (v.%s != 0) { w.putLeb(w.ref(F_%s)); w.put8(%d); w.put32(v.%s); }\n", m, m, k, m)
	case f.Type.Kind == ir.TInt:
		k := variableWireKind(f)
		fmt.Fprintf(b, "        if (v.%s != 0L) { w.putLeb(w.ref(F_%s)); w.put8(%d); w.put64(v.%s); }\n", m, m, k, m)
	case f.Type.Kind == ir.TNamed:
		if enum, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(b, "        if (v.%s != 0) { w.putLeb(w.ref(F_%s)); w.put8(30); switch (v.%s) {\n", m, m, m)
			for i := range enum.Variants {
				fmt.Fprintf(b, "            case %d: w.putLeb(w.ref(0x%016xL)); break;\n", i+1, ir.TableWireId(enum.VariantWireName(i)))
			}
			b.WriteString("            default: w.putLeb(0); break;\n")
			b.WriteString("        } }\n")
		} else if elem, ok := f.Type.Ref.(*ir.Struct); ok && f.Array == ir.ArrayList {
			fmt.Fprintf(b, "        if (!v.%s.isEmpty()) {\n", m)
			fmt.Fprintf(b, "            w.putLeb(w.ref(F_%s)); w.put8(14);\n", m)
			fmt.Fprintf(b, "            TableVariable.Writer aw = w.sub(new byte[1 << 20]);\n")
			fmt.Fprintf(b, "            aw.put8(13); aw.putLeb(v.%s.size());\n", m)
			fmt.Fprintf(b, "            for (%sVariable.Value e : v.%s) {\n", elem.Name, m)
			fmt.Fprintf(b, "                TableVariable.Writer ew = aw.sub(new byte[1 << 20]);\n")
			fmt.Fprintf(b, "                %sVariable.writeFields(ew, nodes, e);\n", elem.Name)
			fmt.Fprintf(b, "                ew.put8(0);\n")
			fmt.Fprintf(b, "                aw.putLeb(ew.off);\n")
			fmt.Fprintf(b, "                aw.putBytes(ew.buf, 0, ew.off);\n")
			fmt.Fprintf(b, "            }\n")
			fmt.Fprintf(b, "            w.putLeb(aw.off);\n")
			fmt.Fprintf(b, "            w.putBytes(aw.buf, 0, aw.off);\n")
			fmt.Fprintf(b, "        }\n")
		}
	}
}
