package javatable

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The VARIABLE FORM (docs/SPEC-TABLES.md §3), form byte 1, is the table wire
// the card record rides: a body of `id reference, kind (u8), payload` fields
// terminated by a zero reference, then the id-table trailer read from the end
// (§3), and the flat node table of §3.1 for the `*string` blob pointers the
// card's `done_when`, `source`, `body` and `extra[].value` carry. This file
// emits the Java runtime and the per-table load/save for exactly the card's
// constructs: variable table roots and tables as list elements, bool/int32/
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

    // A FLOAT IS FINITE ON EVERY TABLE WIRE (docs/SPEC-TABLES.md §3.3, the
    // packet wire's own rule): a read that meets a NaN or an
    // infinity in a float's raw bits refuses as non_finite_float, the
    // tolerant load too, naming the first such field, and lands nothing.
    // Every finite float32 widens exactly through the ordinary conversion.
    public static void nonFiniteFloat(Report report, String field) {
        if (report.reason == null) {
            report.reason = "non_finite_float: " + field;
        }
        report.refused = true;
    }

    // a retained unknown float is named by the table that holds it and the
    // id it cannot name (§6.6): <Table>.0x<id, sixteen hex digits>
    public static String retainedFloatName(String owner, long id) {
        String hex = Long.toHexString(id);
        return owner + ".0x" + "0000000000000000".substring(hex.length()) + hex;
    }

    // The two RESERVED ids the node table names (docs/SPEC-TABLES.md §3.1, §5):
    // the node-table field's own id, and the reserved type id a *string blob
    // record rides under (fnv1a64("string")).
    public static final long NODE_ID = 0xFFFFFFFFFFFFFFFFL;
    public static final long STRING_ID = 0x704be0d8faaffc58L;
    public static final long BYTES_ID = 0x2f2ec0474f1c4fe4L;
    public static final long BUILD_ID = 0xFFFFFFFFFFFFFFFEL;
    public static final long VOCAB_ID = 0xFFFFFFFFFFFFFFFDL;

    // The report counters of §4: unknown, kind mismatch, clamp, malformed and
    // widened. A refusal (an unknown form byte) moves none of them. Retained and
    // retain_lost (§6.6) accumulate across a retained load and save.
    public static final class Report {
        public int unknown;
        public int kindMismatch;
        public int clamped;
        public boolean malformed;
        public int widened;
        public int duplicate;
        public boolean refused;
        public String reason;
        public int retained;
        public int retainLost;

        // reset puts every counter back to its start, so one Report can serve
        // call after call (a reused Measure) without a fresh object per call.
        public void reset() {
            unknown = 0;
            kindMismatch = 0;
            clamped = 0;
            malformed = false;
            widened = 0;
            refused = false;
            reason = null;
            retained = 0;
            retainLost = 0;
        }
    }

    // A reader over a byte[] the caller owns. The id table is resolved once,
    // at open, into a long[] — or, for a reused reader (Measure), read IN
    // PLACE from the trailer at idBase (ids == null), so the walk owns no
    // array. fail is THIS reader's own framing damage, set by its own
    // primitive reads, so a caller can stop the walk it is on without a nested
    // body's damage (which shares the report) stopping the walk above it (§3).
    public static final class Reader {
        public byte[] buf;
        public int off;
        public int limit;
        public long[] ids;
        public int idBase;
        public final Report report;
        public boolean fail;
        public boolean rootBody;

        public Reader(byte[] buf, long[] ids, Report report) {
            this.buf = buf;
            this.ids = ids;
            this.report = report;
            this.limit = buf.length;
        }

        private Reader(byte[] buf, long[] ids, int idBase, Report report) {
            this.buf = buf;
            this.ids = ids;
            this.idBase = idBase;
            this.report = report;
            this.limit = buf.length;
        }

        Reader(Report report) {
            this.report = report;
        }

        public boolean has(int n) {
            return n >= 0 && (long) off + (long) n <= limit;
        }

        public int get8() {
            return buf[off++] & 0xff;
        }

        public int get16() {
            int v = (buf[off] & 0xff) | (buf[off + 1] & 0xff) << 8;
            off += 2;
            return v;
        }

        public int get32() {
            int v =
                    (buf[off] & 0xff)
                            | (buf[off + 1] & 0xff) << 8
                            | (buf[off + 2] & 0xff) << 16
                            | (buf[off + 3] & 0xff) << 24;
            off += 4;
            return v;
        }

        public long get64() {
            long v = 0;
            for (int i = 0; i < 8; i++) {
                v |= (long) (buf[off + i] & 0xff) << (8 * i);
            }
            off += 8;
            return v;
        }

        // count reads a BOUNDED collection's COUNT, the LEB128 after its
        // element kind, against the ENCLOSING body rather than the
        // collection's own L, as the engine reads it (internal/tablewire
        // arrayBody, §3, §4): a count that runs past L is still a count, judged
        // with the kind byte and clamped to the bound, and the elements after
        // it then find no bytes. An unbounded list's count is read inside its
        // own L (the count refusal, §2.9).
        public long count(int outer) {
            int own = limit;
            if (outer > limit) {
                limit = outer;
            }
            long v = leb();
            limit = own;
            return v;
        }

        // unsigned reads one little-endian unsigned integer of 1, 2, 4 or 8
        // bytes: a flags element at its WIRE width (§4's widening).
        public long unsigned(int width) {
            switch (width) {
                case 1:
                    return get8();
                case 2:
                    return get16();
                case 4:
                    return get32() & 0xffffffffL;
                default:
                    return get64();
            }
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
                if (!has(1)) {
                    off = start;
                    report.malformed = true;
                    fail = true;
                    return 0;
                }
                int b = get8();
                if (shift == 63 && (b & 0xfe) != 0) {
                    off = start;
                    report.malformed = true;
                    fail = true;
                    return 0;
                }
                v |= (long) (b & 0x7f) << shift;
                if ((b & 0x80) == 0) {
                    if (i > 0 && b == 0) {
                        off = start;
                        report.malformed = true;
                        fail = true;
                        return 0;
                    }
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
        // In place (ids == null) the table is the trailer from idBase to the
        // count word, so its length is read off the buffer, not stored.
        public long resolve(long ref) {
            int count = ids != null ? ids.length : (buf.length - 8 - idBase) >>> 3;
            if (ref <= 0 || ref > count) {
                report.malformed = true;
                fail = true;
                return -1;
            }
            if (ids != null) {
                return ids[(int) ref - 1];
            }
            return u64(buf, idBase + ((int) ref - 1) * 8);
        }

        // body reads an L-framed payload: L, then L bytes, as a sub-reader
        // bounded by its own L, exactly as a nested body is bounded (§3).
        public Reader body() {
            long n = leb();
            if (fail || !has((int) n)) {
                report.malformed = true;
                fail = true;
                return null;
            }
            Reader sub = new Reader(buf, ids, idBase, report);
            sub.off = off;
            sub.limit = off + (int) n;
            off += (int) n;
            return sub;
        }

        // bodyEnd reads an L-framed payload's L exactly as body() does and
        // answers the offset just past its L bytes, or -1 on the damage body()
        // answers null for — WITHOUT a sub-reader and without moving off past
        // the L: the caller narrows limit to the answer, walks the body in
        // place, then puts off, limit and fail back (§3). An in-place walk is
        // how the measure reads a nested body with no object per body.
        public int bodyEnd() {
            long n = leb();
            if (fail || !has((int) n)) {
                report.malformed = true;
                fail = true;
                return -1;
            }
            return off + (int) n;
        }

        // skip steps over a field of the given kind by its framing (§3). It
        // moves fail on a kind a reader cannot skip — the reserved float16 or
        // one past the closed set — and on a payload the framing cannot cover.
        public void skip(int kind) {
            switch (kind) {
                case 1:
                case 2:
                case 6:
                case 20:
                case 25:
                    if (has(1)) {
                        off += 1;
                    } else {
                        report.malformed = true;
                        fail = true;
                    }
                    break;
                case 3:
                case 7:
                case 21:
                case 26:
                    if (has(2)) {
                        off += 2;
                    } else {
                        report.malformed = true;
                        fail = true;
                    }
                    break;
                case 4:
                case 8:
                case 10:
                case 22:
                case 27:
                    if (has(4)) {
                        off += 4;
                    } else {
                        report.malformed = true;
                        fail = true;
                    }
                    break;
                case 5:
                case 9:
                case 11:
                case 23:
                case 28:
                case 36:
                    if (has(8)) {
                        off += 8;
                    } else {
                        report.malformed = true;
                        fail = true;
                    }
                    break;
                case 18:
                case 19:
                case 24:
                case 29:
                    if (has(16)) {
                        off += 16;
                    } else {
                        report.malformed = true;
                        fail = true;
                    }
                    break;
                case 17:
                case 30:
                    leb();
                    break;
                case 12:
                case 13:
                case 14:
                case 16:
                case 31:
                case 32:
                case 33:
                    {
                        long n = leb();
                        if (!fail && has((int) n)) {
                            off += (int) n;
                        } else {
                            report.malformed = true;
                            fail = true;
                        }
                        break;
                    }
                // §3.3, §4: skipping an unknown kind-15 field reads the arm
                // id always, then the arm's kind byte and L only when the arm
                // is nonzero AND a byte remains in this reader. A truncated arm
                // therefore consumes no neighbour's byte and leaves the body
                // ending at its own L, so the fields decoded before the damage
                // stand and the parent reads on.
                case 15:
                    {
                        long arm = leb();
                        if (!fail && arm != 0) {
                            if (!has(1)) {
                                report.malformed = true;
                                fail = true;
                            } else {
                                get8();
                                long n = leb();
                                if (!fail && has((int) n)) {
                                    off += (int) n;
                                } else {
                                    report.malformed = true;
                                    fail = true;
                                }
                            }
                        }
                        break;
                    }
                default:
                    report.malformed = true;
                    fail = true;
            }
        }
    }

    // The ID TABLE (docs/SPEC-TABLES.md §3): one shared over the whole file, so
    // a list element's field ids intern into the same table the root's do.
    public static final class Ids {
        public final long[] entries;
        public int count;
        public boolean overflow;
        // nonFinite is the SAVE's refusal of a NaN or an infinity
        // (docs/SPEC-TABLES.md §3.3): it lives on the one id table every
        // writer of a save shares, sub-writers included, so the save sees it.
        public boolean nonFinite;

        public Ids(int cap) {
            entries = new long[cap];
        }

        public long ref(long id) {
            for (int i = 0; i < count; i++) {
                if (entries[i] == id) {
                    return i + 1;
                }
            }
            if (count >= entries.length) {
                overflow = true;
                return 0;
            }
            entries[count++] = id;
            return count;
        }
    }

    // A writer over a byte[] the caller owns. The id table is written in
    // first-use order at the trailer (§3). A GROWABLE writer (the Saver's)
    // grows its buffer instead of overflowing, so one buffer, grown to the
    // largest wire it has written, serves every later save; a nested body is
    // written IN PLACE and framed afterwards (frame, frameField), so no body
    // owns a scratch array of its own.
    public static final class Writer {
        public byte[] buf;
        public int off;
        public final Ids ids;
        public boolean overflow;
        public boolean growable;

        public Writer(byte[] buf, Ids ids) {
            this.buf = buf;
            this.ids = ids;
        }

        // room makes n more bytes fit at off: a growable writer grows (the
        // scratch is sized by the wire it writes, then kept); a fixed one
        // answers false and marks overflow.
        boolean room(int n) {
            if ((long) off + n <= buf.length) {
                return true;
            }
            if (!growable) {
                overflow = true;
                return false;
            }
            long want = Math.max((long) buf.length * 2, (long) off + n);
            if (want > Integer.MAX_VALUE - 8) {
                overflow = true;
                return false;
            }
            buf = java.util.Arrays.copyOf(buf, (int) want);
            return true;
        }

        // lebLen is the canonical LEB128 spelling's length for v (§3).
        public static int lebLen(long v) {
            int n = 1;
            while ((v >>>= 7) != 0) {
                n++;
            }
            return n;
        }

        private int putLebAt(int at, long v) {
            while (true) {
                int b = (int) (v & 0x7f);
                v >>>= 7;
                if (v != 0) {
                    buf[at++] = (byte) (b | 0x80);
                } else {
                    buf[at++] = (byte) b;
                    return at;
                }
            }
        }

        // frame turns the bytes written since start into an L-framed payload:
        // it slides them up by L's own length and writes L in front (§3).
        public void frame(int start) {
            int len = off - start;
            int k = lebLen(len);
            if (!room(k)) {
                return;
            }
            System.arraycopy(buf, start, buf, start + k, len);
            putLebAt(start, len);
            off += k;
        }

        // frameField turns the bytes written since start into one whole field:
        // the reference, the kind byte and L, then the payload (§3).
        public void frameField(int start, long ref, int kind) {
            int len = off - start;
            int k = lebLen(ref) + 1 + lebLen(len);
            if (!room(k)) {
                return;
            }
            System.arraycopy(buf, start, buf, start + k, len);
            int at = putLebAt(start, ref);
            buf[at++] = (byte) kind;
            putLebAt(at, len);
            off += k;
        }

        // frameBody replaces a payload's optional leading L with its actual
        // length. Union arms write directly into the Saver buffer (§3).
        public void frameBody(int start, int drop) {
            int len = off - start - drop;
            int k = lebLen(len);
            if (k > drop && !room(k - drop)) {
                return;
            }
            System.arraycopy(buf, start + drop, buf, start + k, len);
            putLebAt(start, len);
            off = start + k + len;
        }

        // A keyed entry is its key reference, its length, then the value's
        // unframed payload (§3.2). It uses the Saver buffer in place.
        public void frameEntry(int start, long key, int drop) {
            int len = off - start - drop;
            int k = lebLen(key) + lebLen(len);
            if (k > drop && !room(k - drop)) {
                return;
            }
            System.arraycopy(buf, start + drop, buf, start + k, len);
            int at = putLebAt(start, key);
            putLebAt(at, len);
            off = start + k + len;
        }

        // The keyed payload's L covers its element kind, count and entries.
        public void frameKeyed(int start, int kind, int count) {
            int entries = off - start;
            int length = 1 + lebLen(count) + entries;
            int k = lebLen(length) + 1 + lebLen(count);
            if (!room(k)) {
                return;
            }
            System.arraycopy(buf, start, buf, start + k, entries);
            int at = putLebAt(start, length);
            buf[at++] = (byte) kind;
            putLebAt(at, count);
            off += k;
        }

        // putUtf8 writes one string as L then its UTF-8 bytes, byte for byte
        // what String.getBytes(UTF_8) spells (a lone surrogate is '?'), with no
        // byte[] of its own.
        public void putUtf8(String s) {
            int n = s.length();
            long len = 0;
            for (int i = 0; i < n; i++) {
                char c = s.charAt(i);
                if (c < 0x80) {
                    len += 1;
                } else if (c < 0x800) {
                    len += 2;
                } else if (Character.isHighSurrogate(c)
                        && i + 1 < n
                        && Character.isLowSurrogate(s.charAt(i + 1))) {
                    len += 4;
                    i++;
                } else if (Character.isSurrogate(c)) {
                    len += 1;
                } else {
                    len += 3;
                }
            }
            putLeb(len);
            for (int i = 0; i < n; i++) {
                char c = s.charAt(i);
                if (c < 0x80) {
                    put8(c);
                } else if (c < 0x800) {
                    put8(0xc0 | (c >> 6));
                    put8(0x80 | (c & 0x3f));
                } else if (Character.isHighSurrogate(c)
                        && i + 1 < n
                        && Character.isLowSurrogate(s.charAt(i + 1))) {
                    int cp = Character.toCodePoint(c, s.charAt(++i));
                    put8(0xf0 | (cp >> 18));
                    put8(0x80 | ((cp >> 12) & 0x3f));
                    put8(0x80 | ((cp >> 6) & 0x3f));
                    put8(0x80 | (cp & 0x3f));
                } else if (Character.isSurrogate(c)) {
                    put8('?');
                } else {
                    put8(0xe0 | (c >> 12));
                    put8(0x80 | ((c >> 6) & 0x3f));
                    put8(0x80 | (c & 0x3f));
                }
            }
        }

        // sub returns a writer over the SAME id table: a list element's body is
        // written into scratch and framed by its caller, but its field ids
        // intern into the one table the whole file shares (§3).
        public Writer sub(byte[] buf) {
            return new Writer(buf, ids);
        }

        public void put8(int b) {
            if (off >= buf.length && !room(1)) {
                return;
            }
            buf[off++] = (byte) b;
        }

        public void put16(int v) {
            for (int i = 0; i < 2; i++) {
                put8((v >>> (8 * i)) & 0xff);
            }
        }

        public void put32(int v) {
            for (int i = 0; i < 4; i++) {
                put8((v >>> (8 * i)) & 0xff);
            }
        }

        public void put64(long v) {
            for (int i = 0; i < 8; i++) {
                put8((int) (v >>> (8 * i)) & 0xff);
            }
        }

        // putF32 and putF64 write a float's RAW bits, -0 and the subnormals
        // exact, and refuse a NaN or an infinity for the whole save
        // (docs/SPEC-TABLES.md §3.3): no table writer emits one.
        public void putF32(float v) {
            int bits = Float.floatToRawIntBits(v);
            if ((bits & 0x7f800000) == 0x7f800000) {
                ids.nonFinite = true;
            }
            put32(bits);
        }

        public void putF64(double v) {
            long bits = Double.doubleToRawLongBits(v);
            if ((bits & 0x7ff0000000000000L) == 0x7ff0000000000000L) {
                ids.nonFinite = true;
            }
            put64(bits);
        }

        public void putLeb(long v) {
            while (true) {
                int b = (int) (v & 0x7f);
                v >>>= 7;
                if (v != 0) {
                    put8(b | 0x80);
                } else {
                    put8(b);
                    break;
                }
            }
        }

        public void putBytes(byte[] b, int at, int len) {
            for (int i = 0; i < len; i++) {
                put8(b[at + i] & 0xff);
            }
        }

        // ref interns an id into the table in first-use order and returns its
        // reference (1-based).
        public long ref(long id) {
            long r = ids.ref(id);
            if (ids.overflow) {
                overflow = true;
            }
            return r;
        }

        // trailer writes the id table: the entries, then the fixed u64 count.
        public void trailer() {
            for (int i = 0; i < ids.count; i++) {
                put64(ids.entries[i]);
            }
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
        int base = trailerBase(data, report);
        if (base < 0) {
            return null;
        }
        int count = (data.length - 8 - base) / 8;
        long[] ids = new long[count];
        for (int i = 0; i < count; i++) {
            ids[i] = u64(data, base + i * 8);
        }
        Reader r = new Reader(data, ids, report);
        r.off = 1;
        r.limit = base;
        r.rootBody = true;
        return r;
    }

    // trailerBase is open's every check, made IN PLACE on the caller's bytes:
    // the form byte, the id table's count and its duplicate ids, and the
    // body's extent (§3). It answers the offset of the table's first entry, or
    // -1 with the report moved exactly as open moves it, and allocates nothing.
    public static int trailerBase(byte[] data, Report report) {
        if (data.length < 1) {
            report.malformed = true;
            return -1;
        }
        int form = data[0] & 0xff;
        if (form != 1) {
            // A REFUSED form byte names its direction (docs/SPEC-TABLES.md §3,
            // §3.3, §6.5), spelled as the reference leg spells it (gotable
            // fixedform.go and message.go) and as the JavaScript leg does:
            // message_form_as_file for a batch (form 2) handed to a file root,
            // newer_form for any other byte. A widening is not a refusal: the
            // widened counter counts it and reason stays null (§4.1). Damage
            // is the malformed flag and names no reason.
            report.refused = true;
            report.reason = form == 2 ? "message_form_as_file" : "newer_form";
            return -1;
        }
        if (data.length < 9) {
            report.malformed = true;
            return -1;
        }
        long count = u64(data, data.length - 8);
        // §3, THE DISTINCTNESS CHECK: an entry count above 4096 is malformed
        // whatever the buffer would hold
        if (count < 0 || count > 4096 || count > (data.length - 9) / 8) {
            report.malformed = true;
            return -1;
        }
        int base = data.length - 8 - (int) count * 8;
        for (int i = 0; i < count; i++) {
            long id = u64(data, base + i * 8);
            for (int j = 0; j < i; j++) {
                if (u64(data, base + j * 8) == id) {
                    report.malformed = true;
                    return -1;
                }
            }
        }
        int end = bodyExtent(data, (int) count, 1, base);
        if (end >= 0 && end != base) {
            report.malformed = true;
            return -1;
        }
        return base;
    }

    // u64 reads one little-endian u64 at at.
    public static long u64(byte[] b, int at) {
        long v = 0;
        for (int i = 0; i < 8; i++) {
            v |= (long) (b[at + i] & 0xff) << (8 * i);
        }
        return v;
    }

    // bodyExtent walks the framing of the body in [off, limit) to the zero
    // reference that ends it (§3). It answers the offset just past the
    // terminator, or -1 where the walk cannot reach one at all — §4's framing
    // damage on that body, not the file's. It moves no counter: it is the OPEN
    // check that tells a body that ENDED EARLY from one that is merely damaged.
    public static int bodyExtent(byte[] buf, int idCount, int off, int limit) {
        while (off < limit) {
            int end = lebEnd(buf, off, limit);
            if (end < 0) {
                return -1;
            }
            long ref = lebValue(buf, off);
            if (ref < 0) {
                return -1;
            }
            off = end;
            if (ref == 0) {
                return off;
            }
            if (ref > idCount) {
                return -1;
            }
            if (off >= limit) {
                return -1;
            }
            int kind = buf[off] & 0xff;
            off++;
            off = skipExtent(buf, kind, off, limit);
            if (off < 0) {
                return -1;
            }
        }
        return -1;
    }

    // lebEnd checks one canonical unsigned LEB128 at off, bounded by limit,
    // and answers the offset just past it, or -1 for a bad encoding:
    // truncated, past ten bytes, a tenth byte above the 64th value bit, or a
    // non-minimal spelling (§3). lebValue then reads the value lebEnd passed.
    // The pair answers in two ints what one array per call used to.
    public static int lebEnd(byte[] buf, int off, int limit) {
        for (int i = 0; ; i++) {
            if (off + i >= limit || i >= 10) {
                return -1;
            }
            int b = buf[off + i] & 0xff;
            if (i == 9 && b > 1) {
                return -1;
            }
            if ((b & 0x80) == 0) {
                if (i > 0 && b == 0) {
                    return -1;
                }
                return off + i + 1;
            }
        }
    }

    public static long lebValue(byte[] buf, int off) {
        long v = 0;
        int shift = 0;
        for (int i = 0; i < 10; i++) {
            int b = buf[off + i] & 0xff;
            v |= (long) (b & 0x7f) << shift;
            if ((b & 0x80) == 0) {
                return v;
            }
            shift += 7;
        }
        return v;
    }

    // skipExtent advances past one field's payload by its kind (§3), for the
    // extent walk. -1 is a payload the framing cannot cover, which is a kind a
    // reader does not know (kind 34, the reserved float16, or one past the
    // closed set) or a payload that runs past the body.
    public static int skipExtent(byte[] buf, int kind, int off, int limit) {
        int w;
        switch (kind) {
            case 1:
            case 2:
            case 6:
            case 20:
            case 25:
                w = 1;
                break;
            case 3:
            case 7:
            case 21:
            case 26:
                w = 2;
                break;
            case 4:
            case 8:
            case 10:
            case 22:
            case 27:
                w = 4;
                break;
            case 5:
            case 9:
            case 11:
            case 23:
            case 28:
            case 36:
                w = 8;
                break;
            case 18:
            case 19:
            case 24:
            case 29:
                w = 16;
                break;
            case 17:
            case 30:
                {
                    int e = lebEnd(buf, off, limit);
                    if (e < 0) {
                        return -1;
                    }
                    return lebValue(buf, off) < 0 ? -1 : e;
                }
            case 12:
            case 13:
            case 14:
            case 16:
            case 31:
            case 32:
            case 33:
                {
                    int e = lebEnd(buf, off, limit);
                    if (e < 0) {
                        return -1;
                    }
                    long n = lebValue(buf, off);
                    if (n < 0 || n > limit - e) {
                        return -1;
                    }
                    return e + (int) n;
                }
            case 15:
                {
                    int e = lebEnd(buf, off, limit);
                    if (e < 0) {
                        return -1;
                    }
                    long arm = lebValue(buf, off);
                    if (arm < 0) {
                        return -1;
                    }
                    if (arm == 0) {
                        return e;
                    }
                    int at = e;
                    if (at >= limit) {
                        return -1;
                    }
                    at++; // the arm's kind byte
                    int ne = lebEnd(buf, at, limit);
                    if (ne < 0) {
                        return -1;
                    }
                    long n = lebValue(buf, at);
                    if (n < 0 || n > limit - ne) {
                        return -1;
                    }
                    return ne + (int) n;
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
            if (c == 0) {
                return false;
            }
            if (c < 0x80) {
                i++;
                continue;
            }
            if (c >= 0xc2 && c <= 0xdf) {
                if (i + 1 >= end || (b[i + 1] & 0xc0) != 0x80) {
                    return false;
                }
                i += 2;
            } else if (c >= 0xe0 && c <= 0xef) {
                if (i + 2 >= end) {
                    return false;
                }
                int c1 = b[i + 1] & 0xff, c2 = b[i + 2] & 0xff;
                if ((c1 & 0xc0) != 0x80 || (c2 & 0xc0) != 0x80) {
                    return false;
                }
                if (c == 0xe0 && c1 < 0xa0) {
                    return false;
                }
                if (c == 0xed && c1 > 0x9f) {
                    return false;
                }
                i += 3;
            } else if (c >= 0xf0 && c <= 0xf4) {
                if (i + 3 >= end) {
                    return false;
                }
                int c1 = b[i + 1] & 0xff, c2 = b[i + 2] & 0xff, c3 = b[i + 3] & 0xff;
                if ((c1 & 0xc0) != 0x80 || (c2 & 0xc0) != 0x80 || (c3 & 0xc0) != 0x80) {
                    return false;
                }
                if (c == 0xf0 && c1 < 0x90) {
                    return false;
                }
                if (c == 0xf4 && c1 > 0x8f) {
                    return false;
                }
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
        public byte[] bytes;
        public Object table;
        public boolean textMalformed;

        public NodeRecord(long typeId, byte[] body) {
            this.typeId = typeId;
            this.body = body;
        }
    }

    // nodes reads a node-table field's payload (§3.1) and returns its records.
    // A type id this reader cannot name keeps its index and counts unknown;
    // a text blob whose content is not text counts malformed; in both, the
    // record resolves to null (its typeId is cleared) and every pointer naming
    // it reads null (§3.1, §4). Structural damage — a bad reference, a size
    // past the field, a declared count the scan does not match, or bytes left
    // over — is malformed for the whole table and answers null.
    public static NodeRecord[] nodes(Reader r, boolean retain) {
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
            if (r.fail || ref == 0) {
                r.report.malformed = true;
                return null;
            }
            long typeId = r.resolve(ref);
            if (r.fail) {
                r.report.malformed = true;
                return null;
            }
            long size = r.leb();
            if (r.fail || size < 0 || !r.has((int) size)) {
                r.report.malformed = true;
                return null;
            }
            byte[] body = new byte[(int) size];
            System.arraycopy(r.buf, r.off, body, 0, (int) size);
            r.off += (int) size;
            list.add(new NodeRecord(typeId, body));
        }
        if (declared != list.size()) {
            r.report.malformed = true;
            return null;
        }
        // Type resolution (§3.1), past the whole scan: a record whose type id
        // this reader cannot name keeps its index and counts unknown ONCE, and
        // a text blob whose content is not text counts malformed; in both the
        // record resolves to null (its type id cleared). A NODE RECORD whose
        // type id this reader cannot name is an excluded class (§6.6): the
        // retaining read counts one retain_lost and never retains it.
        for (NodeRecord rec : list) {
            if (rec.typeId == STRING_ID) {
                if (!textValid(rec.body, 0, rec.body.length)) {
                    r.report.malformed = true;
                    rec.typeId = 0;
                    rec.textMalformed = true;
                } else {
                    rec.value = new String(rec.body, java.nio.charset.StandardCharsets.UTF_8);
                }
            } else if (rec.typeId == BYTES_ID) {
                rec.bytes = rec.body;
            }
        }
        return list.toArray(new NodeRecord[0]);
    }

    // NODE SIZER is how a generated root answers one node table record
    // (docs/SPEC-TABLES.md §6.5): its type id gives its storage size, a TABLE
    // record's own body is walked for the maps and lists it carries, and a
    // blob record is sized from its length and reserved type id. The root
    // supplies it because only the root's pointer-reachable set says which
    // type ids command storage.
    public interface NodeSizer {
        long size(long typeId, Reader r, int off, int len, Measure m);
    }

    // MEASURE is the region measure's reusable state (§6.5): the report, the
    // one reader the whole walk narrows in place, and the node table's record
    // count and region bytes, scanned in place (measureNodes). A caller that
    // keeps one Measure measures call after call with NO allocation; the
    // one-argument loadMeasure allocates exactly this one handle per call.
    public static final class Measure {
        public final Report report = new Report();
        public final Reader reader = new Reader(report);
        // nodeRecords is the last whole node table's record count, or -1 when
        // there is none or it did not scan whole; nodeBlobBytes is the region
        // its blob records take and nodeTableBytes the region its TABLE
        // records take, both at the unit's alignment (§6.5).
        public long nodeRecords;
        public long nodeBlobBytes;
        public long nodeTableBytes;
        // nodeSizer is the generated root's answer for a record's type id. It
        // is installed once per root class, never allocated per call.
        public NodeSizer nodeSizer;

        // open is TableVariable.open over the caller's bytes into the one
        // reader: the same checks (trailerBase), the id table read in place.
        public Reader open(byte[] data) {
            report.reset();
            nodeRecords = -1;
            nodeBlobBytes = 0;
            nodeTableBytes = 0;
            int base = trailerBase(data, report);
            if (base < 0) {
                return null;
            }
            Reader r = reader;
            r.buf = data;
            r.ids = null;
            r.idBase = base;
            r.off = 1;
            r.limit = base;
            r.fail = false;
            return r;
        }
    }

    // measureNodeTable is the oracle's nodeTableBytes over the root body
    // (docs/SPEC-TABLES.md §3.1, §6.5): the top-level fields are stepped by
    // their framing until the terminator or the first damage, the LAST field
    // under the reserved node id is the table, and a node field under a kind
    // other than 12 or with an L past the body leaves no whole table
    // (nodeRecords -1). No node field is an empty, whole table.
    public static void measureNodeTable(Reader r, Measure m, long align) {
        m.nodeRecords = -1;
        m.nodeBlobBytes = 0;
        m.nodeTableBytes = 0;
        r.fail = false;
        int start = -1;
        int end = -1;
        while (true) {
            long ref = r.leb();
            if (r.fail || ref == 0) {
                break;
            }
            long id = r.resolve(ref);
            if (r.fail || !r.has(1)) {
                break;
            }
            int kind = r.get8();
            if (id == NODE_ID) {
                if (kind != 12) {
                    return;
                }
                int e = r.bodyEnd();
                if (e < 0) {
                    return;
                }
                start = r.off;
                end = e;
                r.off = e;
                continue;
            }
            r.skip(kind);
            if (r.fail) {
                break;
            }
        }
        if (start < 0) {
            m.nodeRecords = 0;
            return;
        }
        r.off = start;
        r.limit = end;
        r.fail = false;
        measureNodes(r, m, align);
    }

    // measureNodes is the region-measure half's node-table scan (§6.5), over
    // the reader as narrowed to the node-table payload: the same framing walk
    // as nodes() with no counters past malformed and no type-id clearing. A
    // record is sized by its type id whether or not its text is well-formed:
    // a blob record adds its framed length, header and terminator, and a TABLE
    // record adds its own rounded layout plus the maps and lists its body
    // carries, exactly as the oracle's FileRegionMeasure sizes it. It copies
    // no body. Damage leaves no table (nodeRecords -1), as a null scan did.
    //
    // The framing is scanned WHOLE before any record is sized, as the
    // oracle's scanNodeRecords is: a record that would refuse storage in a
    // table that does not frame whole is never reached (§6.5).
    public static void measureNodes(Reader r, Measure m, long align) {
        m.nodeRecords = -1;
        m.nodeBlobBytes = 0;
        m.nodeTableBytes = 0;
        long declared = r.leb();
        if (r.fail || declared < 0 || declared > 0x7fffffffL) {
            r.report.malformed = true;
            return;
        }
        int first = r.off;
        long count = 0;
        while (r.off < r.limit) {
            long ref = r.leb();
            if (r.fail || ref == 0) {
                r.report.malformed = true;
                return;
            }
            r.resolve(ref);
            if (r.fail) {
                r.report.malformed = true;
                return;
            }
            long size = r.leb();
            if (r.fail || size < 0 || size > (long) (r.limit - r.off)) {
                r.report.malformed = true;
                return;
            }
            r.off += (int) size;
            count++;
        }
        if (declared != count) {
            r.report.malformed = true;
            return;
        }
        r.off = first;
        long blobs = 0;
        long bodies = 0;
        while (r.off < r.limit) {
            long typeId = r.resolve(r.leb());
            int n = (int) r.leb();
            int bodyOff = r.off;
            if (m.nodeSizer != null) {
                long sized = m.nodeSizer.size(typeId, r, bodyOff, n, m);
                if (typeId == STRING_ID || typeId == BYTES_ID) {
                    blobs += sized;
                } else {
                    bodies += sized;
                }
            } else if (typeId == STRING_ID) {
                blobs += (8 + n + 1 + align - 1) & -align;
            }
            if (r.report.refused) {
                return;
            }
            r.off = bodyOff + n;
        }
        m.nodeRecords = count;
        m.nodeBlobBytes = blobs;
        m.nodeTableBytes = bodies;
    }

    // node resolves a body field's pointer payload — a node index, 1 the root,
    // k >= 2 the (k-2)-th record — to its record, or null for the null index 0
    // and for an index that names no record (§3.1).
    public static NodeRecord node(NodeRecord[] recs, long index) {
        if (index == 0) {
            return null;
        }
        if (index < 2 || recs == null || index - 2 >= recs.length) {
            return null;
        }
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
    public static NodeTable readNodeTable(Reader r, Report report, boolean retain) {
        int start = r.off;
        boolean startFail = r.fail;
        Reader payload = null;
        boolean bad = false;
        while (true) {
            long ref = r.leb();
            if (r.fail || ref == 0) {
                break;
            }
            long id = r.resolve(ref);
            if (r.fail) {
                break;
            }
            if (!r.has(1)) {
                break;
            }
            int kind = r.get8();
            if (id == NODE_ID) {
                if (kind != 12) {
                    report.malformed = true;
                    bad = true;
                    break;
                }
                long n = r.leb();
                if (r.fail || n < 0 || !r.has((int) n)) {
                    report.malformed = true;
                    bad = true;
                    break;
                }
                Reader sub = new Reader(r.buf, r.ids, report);
                sub.off = r.off;
                sub.limit = r.off + (int) n;
                r.off += (int) n;
                payload = sub;
                continue;
            }
            r.skip(kind);
            if (r.fail) {
                break;
            }
        }
        r.off = start;
        r.fail = startFail;
        // A node-table field that is PRESENT but could not be read whole leaves
        // the numbering not-good (§3.1): every pointer resolves null and moves
        // no counter, exactly as the oracle's decodeState.good=false does
        // (internal/tablewire/decodenodes.go). An ABSENT field is an empty but
        // valid numbering, and a whole read carries the records.
        NodeTable nt = new NodeTable();
        if (bad) {
            nt.bad = true;
            return nt;
        }
        if (payload == null) {
            return nt;
        }
        nt.recs = nodes(payload, retain);
        if (nt.recs == null) {
            nt.bad = true;
        }
        return nt;
    }

    // boolByte reads one bool byte: a byte past 1 is malformed and reads true,
    // as the engine's scalarAt reads it (§3).
    static boolean boolByte(int b, Report report) {
        if (b > 1) {
            report.malformed = true;
        }
        return b != 0;
    }

    // resolveString resolves one *string pointer index against the numbering
    // (§3.1): 0 is null; 1 is the ROOT — a table, not a blob, so it is a kind
    // mismatch; an index past the records is malformed; and a record whose
    // type id is not the string id reads null, its unknown already counted at
    // the scan.
    public static String resolveString(NodeRecord[] recs, long index, Report report) {
        if (index == 0) {
            return null;
        }
        if (index == 1) {
            report.kindMismatch++;
            return null;
        }
        if (index < 2 || recs == null || index - 2 >= recs.length) {
            report.malformed = true;
            return null;
        }
        // a record this reader cannot name (or a text record that was not
        // text) reads null, counted once at the scan; a record of another
        // type — a table, or the other blob kind — is a kind mismatch (§2.5)
        NodeRecord rec = recs[(int) (index - 2)];
        if (rec.typeId == 0) {
            return null;
        }
        if (rec.typeId != STRING_ID) {
            report.kindMismatch++;
            return null;
        }
        return rec.value;
    }

    // resolveBytes resolves one *bytes pointer index against the numbering
    // (§3.1), exactly as resolveString resolves a *string: 0 is null, 1 is the
    // ROOT (a kind mismatch), an index past the records is malformed.
    public static byte[] resolveBytes(NodeRecord[] recs, long index, Report report) {
        if (index == 0) {
            return null;
        }
        if (index == 1) {
            report.kindMismatch++;
            return null;
        }
        if (index < 2 || recs == null || index - 2 >= recs.length) {
            report.malformed = true;
            return null;
        }
        // a record this reader cannot name (or a text record that was not
        // text) reads null, counted once at the scan; a record of another
        // type — a table, or the other blob kind — is a kind mismatch (§2.5)
        NodeRecord rec = recs[(int) (index - 2)];
        if (rec.typeId == 0) {
            return null;
        }
        if (rec.typeId != BYTES_ID) {
            report.kindMismatch++;
            return null;
        }
        return rec.bytes;
    }

    // Wide values use UTF-16 code units in the public value API. Wire bytes
    // are always little endian, independent of the cooked region's order.
    static Reader fieldPayload(Reader raw, int kind) {
        if (kind != 12 && kind != 13 && kind != 14 && kind != 16 && kind != 33) return raw;
        int size = raw.limit - raw.off;
        Writer framed = new Writer(new byte[size + 10], new Ids(1));
        framed.putLeb(size);
        framed.putBytes(raw.buf, raw.off, size);
        Reader r = new Reader(framed.buf, raw.ids, raw.report);
        r.limit = framed.off;
        return r;
    }

    static int lebLen(long value) {
        int n = 1;
        while ((value >>>= 7) != 0) n++;
        return n;
    }

    // put places one element of a FIXED array by slot (docs/SPEC-TABLES.md
    // §3.3): a repeated occurrence overwrites the slot it reaches and leaves
    // every later slot standing, which is the oracle's in-place arrayBody.
    // A counted array's generated read clears first and uses add instead.
    static <T> void put(java.util.List<T> list, int at, T value) {
        while (list.size() < at) list.add(value);
        if (at < list.size()) list.set(at, value); else list.add(value);
    }

    static boolean wideValid(char[] text) {
        if (text == null) return false;
        for (int i = 0; i < text.length; i++) {
            char c = text[i];
            if (c == 0 || Character.isLowSurrogate(c)) return false;
            if (Character.isHighSurrogate(c)) {
                if (++i == text.length || !Character.isLowSurrogate(text[i])) return false;
            }
        }
        return true;
    }

    static char[] wideClamp(char[] text, int bound, Report report) {
        if (text.length <= bound) return text;
        int keep = bound;
        if (keep > 0 && Character.isHighSurrogate(text[keep - 1])) keep--;
        report.clamped++;
        return java.util.Arrays.copyOf(text, keep);
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

    @SuppressWarnings("unchecked")
    public static <T> T resolveTable(
            NodeRecord[] recs,
            long index,
            long targetTypeId,
            Object root,
            long rootTypeId,
            Report report) {
        if (index == 0) {
            return null;
        }
        if (index == 1) {
            if (root != null && targetTypeId == rootTypeId) {
                return (T) root;
            }
            report.kindMismatch++;
            return null;
        }
        if (index < 2 || recs == null || index - 2 >= recs.length) {
            report.malformed = true;
            return null;
        }
        NodeRecord rec = recs[(int) (index - 2)];
        if (rec.typeId == 0) {
            return null;
        }
        if (rec.typeId != targetTypeId) {
            report.kindMismatch++;
            return null;
        }
        return (T) rec.table;
    }

    // NodeGraph is the save-side numbering (§3.1): each non-null pointer
    // target, by IDENTITY, takes the next node index in first-visit order (2,
    // 3, ...; the root is 1) and its use count. It is an open-addressed
    // identity table in parallel arrays — no boxed index, no entry object, no
    // byte[] per blob (a *string's bytes are spelled at write, putUtf8) — and
    // reset() empties it for the next save while keeping its arrays, so a
    // reused Saver numbers a graph with nothing allocated per node.
    public static final class NodeGraph {
        private Object[] keys = new Object[16];
        private long[] keyIndex = new long[16];
        private int[] keyUses = new int[16];
        private boolean[] keyOpen = new boolean[16];
        private int keyCount;
        private Object[] nodeValue = new Object[8];
        private long[] nodeType = new long[8];
        private boolean[] nodeBlob = new boolean[8];
        private int nodeCount;
        public final java.util.IdentityHashMap<Object, Long> labels =
                new java.util.IdentityHashMap<>();

        public void reset() {
            if (keyCount > 0) {
                java.util.Arrays.fill(keys, null);
                keyCount = 0;
            }
            if (nodeCount > 0) {
                java.util.Arrays.fill(nodeValue, 0, nodeCount, null);
                nodeCount = 0;
            }
            if (!labels.isEmpty()) {
                labels.clear();
            }
        }

        private int slot(Object obj) {
            int mask = keys.length - 1;
            int h = System.identityHashCode(obj);
            int i = (h ^ (h >>> 16)) & mask;
            while (keys[i] != null && keys[i] != obj) {
                i = (i + 1) & mask;
            }
            return i;
        }

        private int find(Object obj) {
            int i = slot(obj);
            return keys[i] == obj ? i : -1;
        }

        private int insert(Object obj) {
            int i = slot(obj);
            if (keys[i] == obj) {
                return i;
            }
            if ((keyCount + 1) * 2 > keys.length) {
                Object[] ok = keys;
                long[] oi = keyIndex;
                int[] ou = keyUses;
                boolean[] oo = keyOpen;
                int cap = keys.length * 2;
                keys = new Object[cap];
                keyIndex = new long[cap];
                keyUses = new int[cap];
                keyOpen = new boolean[cap];
                for (int j = 0; j < ok.length; j++) {
                    if (ok[j] != null) {
                        int k = slot(ok[j]);
                        keys[k] = ok[j];
                        keyIndex[k] = oi[j];
                        keyUses[k] = ou[j];
                        keyOpen[k] = oo[j];
                    }
                }
                i = slot(obj);
            }
            keys[i] = obj;
            keyIndex[i] = 0;
            keyUses[i] = 0;
            keyOpen[i] = false;
            keyCount++;
            return i;
        }

        public long index(Object obj) {
            if (obj == null) return 0;
            int i = find(obj);
            return i < 0 ? 0 : keyIndex[i];
        }

        public boolean indexed(Object obj) {
            return index(obj) != 0;
        }

        public int useCount(Object obj) {
            if (obj == null) return 0;
            int i = find(obj);
            return i < 0 ? 0 : keyUses[i];
        }

        // setOpen marks a target whose body is being numbered: a pointer that
        // reaches it before it is indexed is a cycle the walk refuses.
        public void setOpen(Object obj, boolean open) {
            if (obj == null) return;
            keyOpen[insert(obj)] = open;
        }

        public boolean recordPointer(Object target, long typeId, boolean isBlob) {
            if (target == null) return true;
            int i = insert(target);
            if (keyOpen[i]) {
                return false;
            }
            // A BLOB IS OWNED BY ONE SLOT (docs/SPEC-TABLES.md §2.5): a second
            // slot naming the same blob is blob_shared, and every writer
            // refuses it rather than writing it once or a copy per slot.
            if (isBlob && keyIndex[i] != 0) {
                return false;
            }
            keyUses[i]++;
            if (keyIndex[i] == 0) {
                if (nodeCount == nodeValue.length) {
                    int cap = nodeCount * 2;
                    nodeValue = java.util.Arrays.copyOf(nodeValue, cap);
                    nodeType = java.util.Arrays.copyOf(nodeType, cap);
                    nodeBlob = java.util.Arrays.copyOf(nodeBlob, cap);
                }
                keyIndex[i] = (long) (nodeCount + 2);
                nodeValue[nodeCount] = target;
                nodeType[nodeCount] = typeId;
                nodeBlob[nodeCount] = isBlob;
                nodeCount++;
            }
            return true;
        }

        public int nodeCount() {
            return nodeCount;
        }

        public Object nodeValue(int i) {
            return nodeValue[i];
        }

        public long nodeType(int i) {
            return nodeType[i];
        }

        public boolean nodeBlob(int i) {
            return nodeBlob[i];
        }
    }

    // SAVER is a save's reusable state: the numbering, the id table and ONE
    // growable buffer every body of the wire is written into in place. A
    // caller that keeps one Saver saves call after call with nothing
    // allocated but the answer (a byte[] of the wire's size); the buffer grows
    // to the largest wire written and is kept. The one-argument save makes a
    // fresh Saver per call.
    public static final class Saver {
        public final NodeGraph graph = new NodeGraph();
        public final Ids ids = new Ids(64);
        public final Writer writer;

        public Saver() {
            this(256);
        }

        public Saver(int capacity) {
            writer = new Writer(new byte[Math.max(capacity, 16)], ids);
            writer.growable = true;
        }

        // begin empties the numbering, the id table and the buffer for a save.
        public Writer begin() {
            graph.reset();
            ids.count = 0;
            ids.overflow = false;
            ids.nonFinite = false;
            writer.off = 0;
            writer.overflow = false;
            return writer;
        }
    }

    // NodeList is the retaining save's numbering: each non-null *string
    // interned here in first-visit order takes the next node index (2, 3, ...;
    // the root is 1, §3.1) and its body bytes, written once into the node table.
    public static final class NodeList {
        private static final class Entry {
            final long typeId;
            final byte[] body;

            Entry(long typeId, byte[] body) {
                this.typeId = typeId;
                this.body = body;
            }
        }

        private final java.util.ArrayList<Entry> entries = new java.util.ArrayList<>();
        private final java.util.IdentityHashMap<String, Long> seen =
                new java.util.IdentityHashMap<>();
        private final java.util.IdentityHashMap<byte[], Long> seenBytes =
                new java.util.IdentityHashMap<>();
        private int next = 2;

        public long ref(String s) {
            Long have = seen.get(s);
            if (have != null) {
                return have.longValue();
            }
            seen.put(s, Long.valueOf(next));
            entries.add(new Entry(STRING_ID, s.getBytes(java.nio.charset.StandardCharsets.UTF_8)));
            return next++;
        }

        public long refBytes(byte[] b) {
            Long have = seenBytes.get(b);
            if (have != null) {
                return have.longValue();
            }
            seenBytes.put(b, Long.valueOf(next));
            entries.add(new Entry(BYTES_ID, b));
            return next++;
        }

        public int size() {
            return entries.size();
        }

        public long typeId(int i) {
            return entries.get(i).typeId;
        }

        public byte[] body(int i) {
            return entries.get(i).body;
        }
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

    static void writeFlags(StringBuilder b, long bits, String[] names, int depth) {
        b.append('[');
        boolean first = true;
        for (int i = 0; i < names.length; i++)
            if ((bits & (1L << i)) != 0) {
                if (!first) b.append(',');
                first = false;
                indent(b, depth + 1);
                esc(b, names[i]);
            }
        if (!first) indent(b, depth);
        b.append(']');
    }

    static Long readFlags(In in, String[] names) {
        if (in.peek() != '[') {
            in.skipValue();
            in.rep.kindMismatch++;
            return null;
        }
        in.p++;
        long bits = 0;
        while (in.peek() != ']') {
            if (in.peek() == 0) {
                in.bad = true;
                return null;
            }
            if (in.peek() != '"') {
                in.rep.kindMismatch++;
                in.skipValue();
            } else {
                String name = in.scanString();
                if (in.bad) return null;
                boolean found = false;
                for (int i = 0; i < names.length; i++)
                    if (names[i].equals(name)) {
                        bits |= 1L << i;
                        found = true;
                        break;
                    }
                if (!found) in.rep.unknown++;
            }
            int separator = in.peek();
            if (separator == ',') {
                in.p++;
                continue;
            }
            if (separator == ']') break;
            in.bad = true;
            return null;
        }
        in.p++;
        return bits;
    }

    private static final char[] HEX = "0123456789abcdef".toCharArray();

    // indent starts a new line at the given depth: two spaces per level.
    public static void indent(StringBuilder b, int depth) {
        b.append('\n');
        for (int i = 0; i < depth; i++) {
            b.append("  ");
        }
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
                case '"':
                    b.append("\\\"");
                    break;
                case '\\':
                    b.append("\\\\");
                    break;
                case '\b':
                    b.append("\\b");
                    break;
                case '\f':
                    b.append("\\f");
                    break;
                case '\n':
                    b.append("\\n");
                    break;
                case '\r':
                    b.append("\\r");
                    break;
                case '\t':
                    b.append("\\t");
                    break;
                default:
                    if (c < 0x20) {
                        b.append("\\u00").append(HEX[(c >> 4) & 0xf]).append(HEX[c & 0xf]);
                    } else if (c >= 0xd800 && c <= 0xdfff) {
                        if (c >= 0xdc00
                                || i + 1 >= s.length()
                                || s.charAt(i + 1) < 0xdc00
                                || s.charAt(i + 1) > 0xdfff) {
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

    // REFUSED is how the walk refuses a value it cannot name (§16.3): a
    // non-finite float has no JSON spelling, and toJson answers null rather
    // than write text that does not say what the instance holds.
    public static final class Refused extends RuntimeException {
        private static final long serialVersionUID = 1L;

        public Refused(String why) {
            super(why, null, false, false);
        }
    }

    // writeFloat spells one float the way every port does (§16.2): C's %.*g
    // at the FEWEST significant digits, from the kind's floor (six for a
    // float32, fifteen for a float64) up, that read back to the same value,
    // and at the kind's ceiling (nine, seventeen) when none below it does. The
    // read-back test is the engine's own (internal/tabletext writeFloat): the
    // text parsed to a double, narrowed to a float for a float32. It is NOT
    // Float.toString or Double.toString, whose shortest spelling is Java's own
    // ("1.0E10", "0.0") and whose digits can differ from C's on a tie.
    public static void writeFloat(StringBuilder b, double value, boolean single) {
        if (!Double.isFinite(value)) {
            throw new Refused("a non-finite float has no JSON spelling");
        }
        int low = 15;
        int high = 17;
        if (single) {
            low = 6;
            high = 9;
        }
        for (int digits = low; ; digits++) {
            String text = formatG(value, digits);
            if (digits >= high) {
                b.append(text);
                return;
            }
            double back = Double.parseDouble(text);
            if (single ? (double) (float) back == value : back == value) {
                b.append(text);
                return;
            }
        }
    }

    // formatG is C's %.*g for a finite double, spelled out: the double's EXACT
    // decimal expansion (BigDecimal) rounded to prec significant digits at
    // HALF_EVEN, as C and Go round, because java.util.Formatter rounds HALF_UP
    // and the two answers differ on a tie; then the exponent form when the
    // decimal exponent is below -4 or at least prec, the plain form otherwise,
    // trailing zeros and a trailing point removed, an exponent of at least two
    // digits, and a negative zero's sign kept.
    static String formatG(double value, int prec) {
        if (prec < 1) {
            prec = 1;
        }
        String digits = "0";
        int exp = 0;
        if (value != 0.0) {
            java.math.BigDecimal r =
                    new java.math.BigDecimal(Math.abs(value))
                            .round(
                                    new java.math.MathContext(
                                            prec, java.math.RoundingMode.HALF_EVEN));
            String u = r.unscaledValue().toString();
            exp = u.length() - 1 - r.scale();
            int end = u.length();
            while (end > 1 && u.charAt(end - 1) == '0') {
                end--;
            }
            digits = u.substring(0, end);
        }
        StringBuilder s = new StringBuilder();
        if (Double.doubleToRawLongBits(value) < 0) {
            s.append('-');
        }
        if (exp < -4 || exp >= prec) {
            s.append(digits.charAt(0));
            if (digits.length() > 1) {
                s.append('.').append(digits, 1, digits.length());
            }
            s.append('e').append(exp < 0 ? '-' : '+');
            int a = Math.abs(exp);
            if (a < 10) {
                s.append('0');
            }
            s.append(a);
        } else if (exp < 0) {
            s.append("0.");
            for (int i = -1; i > exp; i--) {
                s.append('0');
            }
            s.append(digits);
        } else if (digits.length() <= exp + 1) {
            s.append(digits);
            for (int i = digits.length(); i <= exp; i++) {
                s.append('0');
            }
        } else {
            s.append(digits, 0, exp + 1).append('.').append(digits, exp + 1, digits.length());
        }
        return s.toString();
    }

    // THE READER: a cursor over the byte[] the caller owns, one report shared
    // with the wire (§4). Nothing allocates beyond the strings it reads.
    public static final class LabelEntry {
        public final Object value;
        public final long typeId;
        public boolean open;

        public LabelEntry(Object value, long typeId, boolean open) {
            this.value = value;
            this.typeId = typeId;
            this.open = open;
        }
    }

    public static final class In {
        // §16.2: the text walk's nesting cap, the same 128 the Go oracle
        // uses (internal/tabletext maxJsonDepth), so a tree past it is
        // refused rather than read.
        static final int MAX_JSON_DEPTH = 128;
        public final java.util.Map<Long, LabelEntry> labels = new java.util.HashMap<>();

        final byte[] t;
        int p;
        final TableVariable.Report rep;
        boolean bad;
        // rejected: the last number read was the wrong shape for its kind (counted
        // kind_mismatch); the caller leaves the field's current value standing.
        boolean rejected;

        In(byte[] t, TableVariable.Report rep) {
            this.t = t;
            this.rep = rep;
        }

        // space skips whitespace and both comment forms (§16.2). An unclosed
        // block comment is malformed, as an unclosed string is.
        void space() {
            while (true) {
                if (p >= t.length) {
                    return;
                }
                byte c = t[p];
                if (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
                    p++;
                    continue;
                }
                if (c == '/' && p + 1 < t.length) {
                    if (t[p + 1] == '/') {
                        p += 2;
                        while (p < t.length && t[p] != '\n') {
                            p++;
                        }
                        continue;
                    }
                    if (t[p + 1] == '*') {
                        p += 2;
                        while (p + 1 < t.length && !(t[p] == '*' && t[p + 1] == '/')) {
                            p++;
                        }
                        if (p + 1 >= t.length) {
                            bad = true;
                            return;
                        }
                        p += 2;
                        continue;
                    }
                }
                return;
            }
        }

        public long scanLabel() {
            space();
            if (p >= t.length || t[p] < '1' || t[p] > '9') {
                bad = true;
                return -1;
            }
            long label = 0;
            while (p < t.length && t[p] >= '0' && t[p] <= '9') {
                int digit = t[p] - '0';
                p++;
                if (label > (Long.MAX_VALUE - digit) / 10) {
                    bad = true;
                    return -1;
                }
                label = label * 10 + digit;
            }
            return label;
        }

        public double number() {
            rejected = false;
            boolean[] integral = new boolean[1];
            String tok = scanNumber(integral);
            if (bad || tok == null) {
                return 0.0;
            }
            double value;
            try {
                value = Double.parseDouble(tok);
            } catch (NumberFormatException e) {
                bad = true;
                return 0.0;
            }
            // §16.2, §16.3: a magnitude the format cannot hold is the wrong
            // shape for the kind, never stored as an infinity the writer
            // would refuse.
            if (!Double.isFinite(value)) {
                rep.kindMismatch++;
                rejected = true;
                return 0.0;
            }
            return value;
        }

        // number32 reads one number as a float32 in ONE correctly-rounded
        // conversion at the field's own width, as the engine reads it
        // (strconv.ParseFloat(token, 32)). Parsing to a double and narrowing
        // rounds twice, and the nearest double to a decimal just off a float32
        // midpoint can BE that midpoint (§16.2).
        public float number32() {
            rejected = false;
            boolean[] integral = new boolean[1];
            String tok = scanNumber(integral);
            if (bad || tok == null) {
                return 0.0f;
            }
            float value;
            try {
                value = Float.parseFloat(tok);
            } catch (NumberFormatException e) {
                bad = true;
                return 0.0f;
            }
            // §16.2: the conversion at the field's own width turns a decimal
            // at or past the float32 overflow midpoint into an infinity; that
            // is the wrong shape for the kind (§16.3).
            if (!Float.isFinite(value)) {
                rep.kindMismatch++;
                rejected = true;
                return 0.0f;
            }
            return value;
        }

        int peek() {
            space();
            return p < t.length ? (t[p] & 0xff) : 0;
        }

        boolean lit(String w) {
            space();
            if (p + w.length() > t.length) {
                bad = true;
                return false;
            }
            for (int i = 0; i < w.length(); i++) {
                if ((char) (t[p + i] & 0xff) != w.charAt(i)) {
                    bad = true;
                    return false;
                }
            }
            p += w.length();
            return true;
        }

        // isNull reports a null literal without consuming it.
        boolean isNull() {
            return peek() == 'n';
        }

        int hex4() {
            if (p + 4 > t.length) {
                return -1;
            }
            int v = 0;
            for (int i = 0; i < 4; i++) {
                int c = t[p + i] & 0xff;
                int d;
                if (c >= '0' && c <= '9') {
                    d = c - '0';
                } else if (c >= 'a' && c <= 'f') {
                    d = c - 'a' + 10;
                } else if (c >= 'A' && c <= 'F') {
                    d = c - 'A' + 10;
                } else {
                    return -1;
                }
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
            if (p >= t.length || t[p] != '"') {
                bad = true;
                return null;
            }
            p++;
            StringBuilder b = new StringBuilder();
            while (true) {
                if (p >= t.length) {
                    bad = true;
                    return null;
                }
                int c = t[p] & 0xff;
                p++;
                if (c == '"') {
                    return b.toString();
                }
                if (c == '\\') {
                    if (p >= t.length) {
                        bad = true;
                        return null;
                    }
                    int e = t[p] & 0xff;
                    p++;
                    switch (e) {
                        case '"':
                            b.append('"');
                            break;
                        case '\\':
                            b.append('\\');
                            break;
                        case '/':
                            b.append('/');
                            break;
                        case 'b':
                            b.append('\b');
                            break;
                        case 'f':
                            b.append('\f');
                            break;
                        case 'n':
                            b.append('\n');
                            break;
                        case 'r':
                            b.append('\r');
                            break;
                        case 't':
                            b.append('\t');
                            break;
                        case 'u':
                            {
                                int hi = hex4();
                                if (hi < 0) {
                                    bad = true;
                                    return null;
                                }
                                if (hi >= 0xd800
                                        && hi <= 0xdbff
                                        && p + 2 <= t.length
                                        && t[p] == '\\'
                                        && t[p + 1] == 'u') {
                                    int mark = p;
                                    p += 2;
                                    int lo = hex4();
                                    if (lo >= 0xdc00 && lo <= 0xdfff) {
                                        b.appendCodePoint(
                                                0x10000 + ((hi - 0xd800) << 10) + (lo - 0xdc00));
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
                        default:
                            bad = true;
                            return null;
                    }
                } else if (c < 0x20) {
                    bad = true;
                    return null;
                } else if (c < 0x80) {
                    b.append((char) c);
                } else {
                    int cp = utf8(c);
                    if (cp < 0) {
                        // §16.3: a raw ill-formed byte refuses the read by name; a
                        // silent U+FFFD would make two different texts one value.
                        bad = true;
                        if (rep.reason == null) {
                            rep.reason = "string_not_utf8: <root>";
                        }
                        return null;
                    }
                    b.appendCodePoint(cp);
                }
            }
        }

        // utf8 reads one UTF-8 sequence whose lead byte is c (>= 0x80). -1 is a
        // stray or ill-formed sequence, read as one replacement (§16.3).
        int utf8(int lead) {
            int want, cp;
            if (lead >= 0xc2 && lead <= 0xdf) {
                want = 2;
                cp = lead & 0x1f;
            } else if (lead >= 0xe0 && lead <= 0xef) {
                want = 3;
                cp = lead & 0x0f;
            } else if (lead >= 0xf0 && lead <= 0xf4) {
                want = 4;
                cp = lead & 0x07;
            } else {
                return -1;
            }
            if (p + want - 1 > t.length) {
                return -1;
            }
            for (int i = 1; i < want; i++) {
                int n = t[p + i - 1] & 0xff;
                if ((n & 0xc0) != 0x80) {
                    return -1;
                }
                cp = (cp << 6) | (n & 0x3f);
            }
            p += want - 1;
            if (want == 3 && (cp < 0x800 || (cp >= 0xd800 && cp <= 0xdfff))) {
                return -1;
            }
            if (want == 4 && cp < 0x10000) {
                return -1;
            }
            if (cp > 0x10ffff) {
                return -1;
            }
            return cp;
        }

        // scanNumber reads one JSON number token (RFC 8259 §6) and reports
        // whether it is an integral spelling: no fraction, no exponent (§16.2).
        // A token that is not a JSON number is malformed.
        public String scanNumber(boolean[] integral) {
            space();
            int start = p;
            if (p < t.length && t[p] == '-') {
                p++;
            }
            if (p >= t.length) {
                bad = true;
                return null;
            }
            if (t[p] == '0') {
                p++;
            } else if (t[p] >= '1' && t[p] <= '9') {
                while (p < t.length && t[p] >= '0' && t[p] <= '9') {
                    p++;
                }
            } else {
                bad = true;
                return null;
            }
            integral[0] = true;
            if (p < t.length && t[p] == '.') {
                p++;
                if (p >= t.length || t[p] < '0' || t[p] > '9') {
                    bad = true;
                    return null;
                }
                while (p < t.length && t[p] >= '0' && t[p] <= '9') {
                    p++;
                }
                integral[0] = false;
            }
            if (p < t.length && (t[p] == 'e' || t[p] == 'E')) {
                p++;
                if (p < t.length && (t[p] == '-' || t[p] == '+')) {
                    p++;
                }
                if (p >= t.length || t[p] < '0' || t[p] > '9') {
                    bad = true;
                    return null;
                }
                while (p < t.length && t[p] >= '0' && t[p] <= '9') {
                    p++;
                }
                integral[0] = false;
            }
            return new String(t, start, p - start, java.nio.charset.StandardCharsets.US_ASCII);
        }

        // integer reads one number into a long, clamping to the field's declared
        // range and counting a genuinely fractional value as a kind mismatch
        // (never coerced), a value past the range as a clamp (§16.2).
        public long integer(boolean signed, long min, long max) {
            rejected = false;
            boolean[] integral = new boolean[1];
            String tok = scanNumber(integral);
            if (bad) {
                return 0;
            }
            long v;
            if (integral[0]) {
                if (signed) {
                    try {
                        v = Long.parseLong(tok);
                    } catch (NumberFormatException e) {
                        rep.clamped++;
                        return tok.charAt(0) == '-' ? min : max;
                    }
                } else {
                    if (tok.charAt(0) == '-') {
                        if (!"-0".equals(tok)) {
                            rep.clamped++;
                            return min;
                        }
                        v = 0;
                    } else {
                        try {
                            v = Long.parseUnsignedLong(tok);
                        } catch (NumberFormatException e) {
                            rep.clamped++; // past what sixty-four bits hold
                            // the largest unsigned magnitude; the range checks below add
                            // a second clamp when the bound is narrower than uint64
                            v = -1L;
                        }
                    }
                }
            } else {
                double d;
                try {
                    d = Double.parseDouble(tok);
                } catch (NumberFormatException e) {
                    bad = true;
                    return 0;
                }
                if (signed) {
                    if (!Double.isFinite(d) || d != (double) (long) d) {
                        rep.kindMismatch++;
                        rejected = true;
                        return 0;
                    }
                    v = (long) d;
                } else {
                    if (!Double.isFinite(d) || Math.rint(d) != d) {
                        rep.kindMismatch++;
                        rejected = true;
                        return 0;
                    }
                    if (d < 0.0) {
                        rep.clamped++;
                        return min;
                    }
                    if (d >= 18446744073709551616.0) {
                        rep.clamped++;
                        return max;
                    }
                    v =
                            d < 9223372036854775808.0
                                    ? (long) d
                                    : (long) (d - 9223372036854775808.0) + Long.MIN_VALUE;
                }
            }
            if (signed) {
                if (v < min) {
                    rep.clamped++;
                    return min;
                }
                if (v > max) {
                    rep.clamped++;
                    return max;
                }
            } else {
                if (Long.compareUnsigned(v, min) < 0) {
                    rep.clamped++;
                    return min;
                }
                if (Long.compareUnsigned(v, max) > 0) {
                    rep.clamped++;
                    return max;
                }
            }
            return v;
        }

        // scanStored reads a string the walk PLACES in text storage (a string(N)
        // or *string value, a text-list element, a map key). A stored string that
        // decodes to U+0000 refuses the read (§16.3): no wire carries a zero in
        // text. A string only compared or skipped goes through scanString.
        public String scanStored() {
            String s = scanString();
            if (s != null && s.indexOf('\0') >= 0) {
                bad = true;
                if (rep.reason == null) {
                    rep.reason = "text_holds_zero";
                }
                return null;
            }
            return s;
        }

        // readString reads one string and clamps it at the bound, counting the
        // clamp (§16.2). The cut lands at a code point boundary, never through
        // a multi-byte character.
        public String readString(int bound) {
            String s = scanStored();
            if (s == null) {
                return "";
            }
            return clamp(s, bound);
        }

        String clamp(String s, int bound) {
            byte[] b = s.getBytes(java.nio.charset.StandardCharsets.UTF_8);
            if (b.length <= bound) {
                return s;
            }
            rep.clamped++;
            int keep = bound;
            // back off to a code point boundary
            while (keep > 0 && (b[keep] & 0xc0) == 0x80) {
                keep--;
            }
            return new String(b, 0, keep, java.nio.charset.StandardCharsets.UTF_8);
        }

        // skipValue steps over one value of any shape, so an unknown key's
        // value is dropped whole and never read as a field (§16.2, §4).
        public void skipValue() {
            skipValue(0);
        }

        void skipValue(int depth) {
            int c = peek();
            switch (c) {
                case '{':
                    skipContainer('}', depth);
                    return;
                case '[':
                    skipContainer(']', depth);
                    return;
                case '"':
                    scanString();
                    return;
                case 't':
                    lit("true");
                    return;
                case 'f':
                    lit("false");
                    return;
                case 'n':
                    lit("null");
                    return;
                case 0:
                    bad = true;
                    return;
                default:
                    {
                        boolean[] integral = new boolean[1];
                        scanNumber(integral);
                    }
            }
        }

        void skipContainer(char closing, int depth) {
            if (depth > MAX_JSON_DEPTH) {
                bad = true;
                return;
            }
            p++; // the opening bracket
            while (true) {
                int c = peek();
                if (c == closing) {
                    p++;
                    return;
                }
                if (c == 0) {
                    bad = true;
                    return;
                }
                if (closing == '}') {
                    String key = scanString();
                    if (bad) {
                        return;
                    }
                    if (peek() != ':') {
                        bad = true;
                        return;
                    }
                    p++;
                    // A DEFINITION IN A SKIPPED VALUE STILL TAKES ITS LABEL, with no
                    // node, so a later reference reads null rather than refusing
                    // (docs/SPEC-TABLES.md §16.7, §17.5; read.go's skippedAmpersand).
                    if ("&node".equals(key)) {
                        long labelId = scanLabel();
                        if (bad) {
                            return;
                        }
                        if (!labels.containsKey(labelId)) {
                            labels.put(labelId, new LabelEntry(null, 0L, false));
                        }
                        c = peek();
                        if (c == ',') {
                            p++;
                            continue;
                        }
                        if (c == closing) {
                            p++;
                            return;
                        }
                        bad = true;
                        return;
                    }
                }
                skipValue(depth + 1);
                if (bad) {
                    return;
                }
                c = peek();
                if (c == ',') {
                    p++;
                    continue;
                }
                if (c == closing) {
                    p++;
                    return;
                }
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
	tables := cardVariableTables(u)
	if len(tables) == 0 {
		return map[string][]byte{}, nil
	}
	out := map[string][]byte{
		"TableVariable.java": javaFile(u, "the VARIABLE FORM's runtime (docs/SPEC-TABLES.md §3), form byte 1", strings.TrimSuffix(tableVariableRuntime, "}\n")+variableWideRuntime+"}\n"),
		"TableJson.java":     javaFile(u, "the TEXT form's runtime (docs/SPEC-TABLES.md §16), JSON in and out", strings.TrimSuffix(tableJsonRuntime, "}\n")+variableWideJsonRuntime+"}\n"),
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
	for _, unions := range []map[string]*ir.Union{u.Unions, u.TableUnions} {
		for _, un := range unions {
			if refuseVariableFormClosure(variableUnionStruct(un), map[string]bool{}) == nil {
				out[un.Name+"Variable.java"] = javaFile(u, "the active union arm on the variable wire (§2.6, §3)", generateVariableUnion(u, un))
			}
		}
	}

	return out, nil
}

func isStruct(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	_, ok := f.Type.Ref.(*ir.Struct)
	return ok
}

func isPointerArray(f *ir.Field) bool {
	return f.Array != ir.ArrayNone && f.Type.Pointer && isStruct(f)
}

func isStructArray(f *ir.Field) bool {
	return f.Array != ir.ArrayNone && !f.Type.Pointer && isStruct(f)
}

func isScalarArray(f *ir.Field) bool {
	if f.Array == ir.ArrayNone {
		return false
	}
	if f.Type.Pointer {
		return false
	}
	if f.Type.Kind == ir.TNamed {
		_, isSt := f.Type.Ref.(*ir.Struct)
		return !isSt
	}
	return true
}

// Keep kind selection shared with the oracle's IR rules (§3).
func variableWireKind(f *ir.Field) int     { return ir.TableWireFieldKind(f) }
func variableElemWireKind(f *ir.Field) int { return ir.TableWireElemKind(f) }

// countRead is the Java expression that reads a collection's count after its
// element kind: a bounded collection's against the enclosing body (Reader.count,
// as the engine's arrayBody), an unbounded list's inside its own L.
func countRead(f *ir.Field, reader string) string {
	if f.ArrayBound > 0 {
		return reader + ".count(r.limit)"
	}
	return reader + ".leb()"
}

func varMember(f *ir.Field) string {
	m := member(f)
	if javaReserved[m] {
		return m + "_"
	}
	return m
}

func variableListElemType(f *ir.Field) string {
	if variableWide(f) {
		typ := variableWideType(f)
		switch typ {
		case "int":
			return "Integer"
		case "long":
			return "Long"
		}
		return typ
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "Boolean"
	case ir.TInt, ir.TBits:
		if f.Type.Width <= 32 {
			return "Integer"
		}
		return "Long"
	case ir.TFloat32:
		return "Float"
	case ir.TFloat64:
		return "Double"
	case ir.TString:
		return "String"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			return "Integer"
		case *ir.Union:
			return f.Type.Name + "Variable.Value"
		case *ir.Flags:
			return "Long"
		}
	}
	return "Object"
}

func variableValueType(f *ir.Field) string {
	if variableWide(f) && f.Array == ir.ArrayNone {
		typ := variableWideType(f)
		return typ
	}
	if isPointerArray(f) || isStructArray(f) {
		return "List<" + f.Type.Name + "Variable.Value>"
	}
	if isScalarArray(f) {
		return "List<" + variableListElemType(f) + ">"
	}
	if f.Type.Pointer {
		if f.Type.Kind == ir.TString {
			return "String"
		}
		if f.Type.Kind == ir.TBytes {
			return "byte[]"
		}
		return f.Type.Name + "Variable.Value"
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "boolean"
	case ir.TInt, ir.TBits:
		if f.Type.Width <= 32 {
			return "int"
		}
		return "long"
	case ir.TFloat32:
		return "float"
	case ir.TFloat64:
		return "double"
	case ir.TString:
		return "String"
	case ir.TWString:
		return "char[]"
	case ir.TBytes:
		return "byte[]"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			return "int"
		case *ir.Flags:
			return "long"
		case *ir.Struct, *ir.Union:
			return f.Type.Name + "Variable.Value"
		}
	}
	return "Object"
}

func variableDefault(f *ir.Field) string {
	if f.Array != ir.ArrayNone {
		return "new ArrayList<>()"
	}
	if variableWide(f) {
		return variableWideDefault(f)
	}
	if f.Type.Pointer {
		return "null"
	}
	if f.Type.Kind == ir.TBytes {
		vals := make([]string, len(f.DefBytes))
		for i, value := range f.DefBytes {
			vals[i] = fmt.Sprintf("(byte)%d", value)
		}
		return "new byte[]{" + strings.Join(vals, ",") + "}"
	}
	if f.HasDefault {
		switch f.Type.Kind {
		case ir.TBool:
			if f.DefBool {
				return "true"
			}
			return "false"
		case ir.TFloat32:
			return fmt.Sprintf("%gf", f.DefFloat)
		case ir.TFloat64:
			return fmt.Sprintf("%gd", f.DefFloat)
		case ir.TString:
			if len(f.DefBytes) > 0 {
				return fmt.Sprintf("%q", string(f.DefBytes))
			}
			return "\"\""
		case ir.TNamed:
			if enum, ok := f.Type.Ref.(*ir.Enum); ok && f.DefVariant != "" {
				for i, v := range enum.Variants {
					if v == f.DefVariant {
						return fmt.Sprintf("%d", i+1)
					}
				}
			}
			if _, ok := f.Type.Ref.(*ir.Flags); ok && f.DefInt != nil {
				return f.DefInt.String() + "L"
			}
		default:
			if f.DefInt != nil {
				if f.Type.Width <= 32 {
					return f.DefInt.String()
				}
				return f.DefInt.String() + "L"
			}
		}
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "false"
	case ir.TInt, ir.TBits:
		if f.Type.Width <= 32 {
			return "0"
		}
		return "0L"
	case ir.TFloat32:
		return "0.0f"
	case ir.TFloat64:
		return "0.0d"
	case ir.TString:
		return "\"\""
	case ir.TWString:
		return "new char[0]"
	case ir.TBytes:
		return "null"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			return "0"
		case *ir.Flags:
			return "0L"
		case *ir.Struct, *ir.Union:
			return "new " + f.Type.Name + "Variable.Value()"
		}
	}
	return "null"
}

func generateTableVariable(u *ir.Unit, st *ir.Struct) (string, error) {
	st = variableStruct(st)
	tables := cardVariableTables(u)
	var b strings.Builder
	b.WriteString("import java.nio.charset.StandardCharsets;\n")
	b.WriteString("import java.util.ArrayList;\n")
	b.WriteString("import java.util.List;\n\n")
	fmt.Fprintf(&b, "public final class %sVariable {\n", st.Name)
	b.WriteString("    private " + st.Name + "Variable() {}\n\n")

	writeVariableMapHelpers(&b, u, st)
	writeVariableKeyedHelpers(&b, u, st)

	// The value class
	fmt.Fprintf(&b, "    public static final class Value {\n")
	for _, f := range st.Fields {
		fmt.Fprintf(&b, "        public %s %s = %s;\n", variableValueType(f), varMember(f), variableDefault(f))
		if f.Type.Optional {
			fmt.Fprintf(&b, "        public boolean %sPresent;\n", varMember(f))
		}
		if f.Type.Blob() || (f.Type.Pointer && isStruct(f) && f.Array == ir.ArrayNone) {
			fmt.Fprintf(&b, "        public long %sIdx;\n", varMember(f))
		} else if isPointerArray(f) {
			fmt.Fprintf(&b, "        public List<Long> %sIdx = new ArrayList<>();\n", varMember(f))
		}
	}
	b.WriteString("\n        public Value() {\n")
	for _, f := range st.Fields {
		if f.KeyEnum != "" {
			fmt.Fprintf(&b, "            for (int i = 0; i < %d; i++) {\n                %s.add(%s);\n", f.ArrayBound, varMember(f), variableDefault(variableElement(f)))
			if f.Type.Pointer {
				fmt.Fprintf(&b, "                %sIdx.add(0L);\n", varMember(f))
			}
			b.WriteString("            }\n")
		}
	}
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")

	// Constants
	fmt.Fprintf(&b, "    public static final long TYPE_ID = 0x%016xL;\n", ir.TableWireId(st.WireName()))
	for _, f := range st.Fields {
		fmt.Fprintf(&b, "    public static final long F_%s = 0x%016xL;\n", varMember(f), ir.TableFieldWireId(f))
	}
	b.WriteString("\n")

	// Dispatch helpers
	b.WriteString("    static Object createNode(long typeId) {\n")
	for _, t := range tables {
		fmt.Fprintf(&b, "        if (typeId == %sVariable.TYPE_ID) { return new %sVariable.Value(); }\n", t.Name, t.Name)
	}
	b.WriteString("        return null;\n    }\n\n")

	b.WriteString("    static void decodeNodeBody(TableVariable.Reader r, Object val, long typeId, TableVariable.Report report) {\n")
	for _, t := range tables {
		fmt.Fprintf(&b, "        if (typeId == %sVariable.TYPE_ID) { %sVariable.readFields(r, (%sVariable.Value) val, null, report); return; }\n", t.Name, t.Name, t.Name)
	}
	b.WriteString("    }\n\n")

	b.WriteString("    static void writeNodeBody(TableVariable.Writer w, TableVariable.NodeGraph graph, Object val, long typeId) {\n")
	for _, t := range tables {
		fmt.Fprintf(&b, "        if (typeId == %sVariable.TYPE_ID) { %sVariable.writeFields(w, graph, (%sVariable.Value) val); return; }\n", t.Name, t.Name, t.Name)
	}
	b.WriteString("    }\n\n")

	b.WriteString("    static void resolveNodePointers(Object val, long typeId, TableVariable.NodeTable nt, Object root, long rootTypeId, TableVariable.Report report) {\n")
	for _, t := range tables {
		fmt.Fprintf(&b, "        if (typeId == %sVariable.TYPE_ID) { %sVariable.resolveNodes((%sVariable.Value) val, nt, root, rootTypeId, report); return; }\n", t.Name, t.Name, t.Name)
	}
	b.WriteString("    }\n\n")

	// buildGraph
	b.WriteString("    public static boolean buildGraph(TableVariable.NodeGraph g, Value v) {\n")
	b.WriteString("        if (v == null) { return true; }\n")
	for _, f := range st.Fields {
		open := openGuard(&b, st, f)
		writeVariableGraphField(&b, f)
		closeGuard(&b, open)
	}
	b.WriteString("        return true;\n    }\n\n")

	// readFields
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
	for i, f := range st.Fields {
		if !first {
			b.WriteString("            } else ")
		} else {
			b.WriteString("            ")
		}
		first = false
		fmt.Fprintf(&b, "if (id == F_%s) {\n", varMember(f))
		writeFieldRead(&b, u, st, f, false, i, false)
	}
	b.WriteString("            } else if (id == TableVariable.NODE_ID) {\n")
	if st.FixedDeclared {
		b.WriteString("                if (r.rootBody) { r.skip(kind); report.unknown++; }\n")
	} else {
		b.WriteString("                if (nt != null && r.rootBody) { r.skip(kind); }\n")
	}
	b.WriteString("                else { report.malformed = true; return false; }\n")
	b.WriteString("            } else if (id == TableVariable.BUILD_ID || id == TableVariable.VOCAB_ID) {\n")
	b.WriteString("                report.malformed = true; return false;\n")
	b.WriteString("            } else {\n")
	b.WriteString("                r.skip(kind); report.unknown++;\n")
	b.WriteString("            }\n")
	b.WriteString("            if (r.fail) { return false; }\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")

	// resolveNodes
	fmt.Fprintf(&b, "    public static void resolveNodes(Value v, TableVariable.NodeTable nt, TableVariable.Report report) {\n")
	fmt.Fprintf(&b, "        resolveNodes(v, nt, v, TYPE_ID, report);\n    }\n\n")

	fmt.Fprintf(&b, "    public static void resolveNodes(Value v, TableVariable.NodeTable nt, Object root, long rootTypeId, TableVariable.Report report) {\n")
	b.WriteString("        if (nt.bad) { return; }\n")
	for _, f := range st.Fields {
		writeVariableResolveField(&b, f)
	}
	b.WriteString("    }\n\n")

	b.WriteString("    static boolean isDefault(Value v, TableVariable.Writer w, TableVariable.NodeGraph graph) {\n")
	b.WriteString("        if (v == null) return true;\n")
	b.WriteString("        int at = w.off;\n")
	b.WriteString("        int mark = w.ids.count;\n")
	b.WriteString("        boolean overflow = w.overflow;\n")
	b.WriteString("        writeFields(w, graph, v);\n")
	b.WriteString("        boolean empty = w.off == at;\n")
	b.WriteString("        w.off = at;\n")
	b.WriteString("        w.ids.count = mark;\n")
	b.WriteString("        w.overflow = overflow;\n")
	b.WriteString("        return empty;\n")
	b.WriteString("    }\n\n")
	// writeFields
	fmt.Fprintf(&b, "    public static void writeFields(TableVariable.Writer w, TableVariable.NodeGraph graph, Value v) {\n")
	for _, f := range st.Fields {
		open := openGuard(&b, st, f)
		writeFieldSave(&b, u, st, f)
		closeGuard(&b, open)
	}
	b.WriteString("    }\n\n")

	// Enum helpers
	for _, f := range st.Fields {
		if enum, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(&b, "    private static int %sEnum(long vid, TableVariable.Report report) {\n", varMember(f))
			for i := range enum.Variants {
				fmt.Fprintf(&b, "        if (vid == 0x%016xL) { return %d; }\n", ir.TableWireId(enum.VariantWireName(i)), i+1)
			}
			b.WriteString("        report.unknown++;\n        return 0;\n    }\n\n")
		}
	}

	// save: ONE growable buffer (the Saver's), every nested body written in
	// place and framed after it (Writer.frame, frameField), so the only array
	// a reused Saver allocates per call is the answer.
	b.WriteString("    // save writes v as the variable form (§3). The one-argument form makes a\n")
	b.WriteString("    // fresh Saver; a caller that keeps a Saver allocates only the answer.\n")
	fmt.Fprintf(&b, "    public static byte[] save(Value v) {\n")
	fmt.Fprintf(&b, "        return save(v, new TableVariable.Saver());\n")
	fmt.Fprintf(&b, "    }\n\n")
	fmt.Fprintf(&b, "    public static byte[] save(Value v, TableVariable.Saver saver) {\n")
	fmt.Fprintf(&b, "        TableVariable.Writer w = saver.begin();\n")
	fmt.Fprintf(&b, "        TableVariable.NodeGraph graph = saver.graph;\n")
	fmt.Fprintf(&b, "        graph.setOpen(v, true);\n")
	fmt.Fprintf(&b, "        if (!buildGraph(graph, v)) { return null; }\n")
	fmt.Fprintf(&b, "        graph.setOpen(v, false);\n")
	fmt.Fprintf(&b, "        w.put8(1);\n")
	fmt.Fprintf(&b, "        writeFields(w, graph, v);\n")
	fmt.Fprintf(&b, "        int nodes = graph.nodeCount();\n")
	fmt.Fprintf(&b, "        if (nodes > 0) {\n")
	fmt.Fprintf(&b, "            long nodeRef = w.ref(TableVariable.NODE_ID);\n")
	fmt.Fprintf(&b, "            int table = w.off;\n")
	fmt.Fprintf(&b, "            w.putLeb(nodes);\n")
	fmt.Fprintf(&b, "            for (int i = 0; i < nodes; i++) {\n")
	fmt.Fprintf(&b, "                Object value = graph.nodeValue(i);\n")
	fmt.Fprintf(&b, "                long typeId = graph.nodeType(i);\n")
	fmt.Fprintf(&b, "                w.putLeb(w.ref(typeId));\n")
	fmt.Fprintf(&b, "                if (graph.nodeBlob(i)) {\n")
	fmt.Fprintf(&b, "                    if (value instanceof String) {\n")
	fmt.Fprintf(&b, "                        w.putUtf8((String) value);\n")
	fmt.Fprintf(&b, "                    } else {\n")
	fmt.Fprintf(&b, "                        byte[] bytes = (byte[]) value;\n")
	fmt.Fprintf(&b, "                        w.putLeb(bytes.length);\n")
	fmt.Fprintf(&b, "                        w.putBytes(bytes, 0, bytes.length);\n")
	fmt.Fprintf(&b, "                    }\n")
	fmt.Fprintf(&b, "                } else {\n")
	fmt.Fprintf(&b, "                    int body = w.off;\n")
	fmt.Fprintf(&b, "                    writeNodeBody(w, graph, value, typeId);\n")
	fmt.Fprintf(&b, "                    w.put8(0);\n")
	fmt.Fprintf(&b, "                    w.frame(body);\n")
	fmt.Fprintf(&b, "                }\n")
	fmt.Fprintf(&b, "            }\n")
	fmt.Fprintf(&b, "            w.frameField(table, nodeRef, 12);\n")
	fmt.Fprintf(&b, "        }\n")
	fmt.Fprintf(&b, "        w.put8(0);\n")
	fmt.Fprintf(&b, "        w.trailer();\n")
	// A NaN OR AN INFINITY ANYWHERE IN THE VALUE REFUSES THE SAVE
	// (docs/SPEC-TABLES.md §3.3): the writer marked it on the shared id table.
	fmt.Fprintf(&b, "        if (w.ids.nonFinite) {\n")
	fmt.Fprintf(&b, "            return null;\n")
	fmt.Fprintf(&b, "        }\n")
	fmt.Fprintf(&b, "        return java.util.Arrays.copyOf(w.buf, w.off);\n")
	fmt.Fprintf(&b, "    }\n\n")

	// load
	fmt.Fprintf(&b, "    public static Value load(byte[] data, TableVariable.Report report) {\n")
	fmt.Fprintf(&b, "        TableVariable.Reader r = TableVariable.open(data, report);\n")
	fmt.Fprintf(&b, "        if (r == null) { return null; }\n")
	fmt.Fprintf(&b, "        Value v = new Value();\n")
	if st.FixedDeclared {
		fmt.Fprintf(&b, "        readFields(r, v, null, report);\n")
		fmt.Fprintf(&b, "        if (report.refused) { return null; }\n")
		fmt.Fprintf(&b, "        if (report.malformed) { return null; } // DAMAGE DISCARDS THE WHOLE LOAD (§4)\n")
		fmt.Fprintf(&b, "        return v;\n")
		fmt.Fprintf(&b, "    }\n\n")
	} else {
		fmt.Fprintf(&b, "        TableVariable.NodeTable nt = TableVariable.readNodeTable(r, report, false);\n")
		fmt.Fprintf(&b, "        if (nt.recs != null && !nt.bad) {\n")
		fmt.Fprintf(&b, "            for (TableVariable.NodeRecord rec : nt.recs) {\n")
		fmt.Fprintf(&b, "                if (rec.typeId == TableVariable.STRING_ID) {\n")
		fmt.Fprintf(&b, "                    if (!TableVariable.textValid(rec.body, 0, rec.body.length)) {\n")
		fmt.Fprintf(&b, "                        report.malformed = true;\n")
		fmt.Fprintf(&b, "                        rec.typeId = 0;\n")
		fmt.Fprintf(&b, "                    } else {\n")
		fmt.Fprintf(&b, "                        rec.value = new String(rec.body, StandardCharsets.UTF_8);\n")
		fmt.Fprintf(&b, "                    }\n")
		fmt.Fprintf(&b, "                } else if (rec.typeId == TableVariable.BYTES_ID) {\n")
		fmt.Fprintf(&b, "                    rec.bytes = rec.body;\n")
		fmt.Fprintf(&b, "                } else if (!rec.textMalformed) {\n")
		fmt.Fprintf(&b, "                    // §3.1: a cleared text node is damage; an actual id of zero is unknown.\n")
		fmt.Fprintf(&b, "                    rec.table = createNode(rec.typeId);\n")
		fmt.Fprintf(&b, "                    if (rec.table == null) {\n")
		fmt.Fprintf(&b, "                        report.unknown++;\n")
		fmt.Fprintf(&b, "                        rec.typeId = 0;\n")
		fmt.Fprintf(&b, "                    }\n")
		fmt.Fprintf(&b, "                }\n")
		fmt.Fprintf(&b, "            }\n")
		fmt.Fprintf(&b, "            for (TableVariable.NodeRecord rec : nt.recs) {\n")
		fmt.Fprintf(&b, "                if (rec.table != null) {\n")
		fmt.Fprintf(&b, "                    TableVariable.Reader br = new TableVariable.Reader(rec.body, r.ids, report);\n")
		fmt.Fprintf(&b, "                    decodeNodeBody(br, rec.table, rec.typeId, report);\n")
		fmt.Fprintf(&b, "                }\n")
		fmt.Fprintf(&b, "            }\n")
		fmt.Fprintf(&b, "        }\n")
		fmt.Fprintf(&b, "        readFields(r, v, nt, report);\n")
		fmt.Fprintf(&b, "        if (report.refused) { return null; }\n")
		// A READ THAT STOPPED EARLY never reached the container headers past
		// the stop, but the oracle framing walk does (internal/tablewire
		// extent.go FileRegionMeasure): a LIST COUNT whose elements cannot fit
		// its own L inside a MAP VALUE the read abandoned is still
		// count_over_length, and the file is refused, not loaded (docs/SPEC-TABLES.md
		// §6.5 "The two load paths", §6.2, §3.3; §2.8, §2.9). The stop is not
		// always counted as malformed: a value ref that resolves to the wrong
		// kind stops the entry as kind_mismatch, and the list past it is never
		// read either. So the walk runs on every load that has not refused:
		// measureFields IS the oracle walk, and the load refuses exactly where
		// loadMeasure answers -1, whatever the read reported.
		fmt.Fprintf(&b, "        TableVariable.Measure measure = new TableVariable.Measure();\n")
		fmt.Fprintf(&b, "        if (loadMeasure(data, measure) < 0) {\n")
		fmt.Fprintf(&b, "            report.refused = true;\n")
		fmt.Fprintf(&b, "            if (measure.report.reason != null) { report.reason = measure.report.reason; }\n")
		fmt.Fprintf(&b, "            return null;\n")
		fmt.Fprintf(&b, "        }\n")
		fmt.Fprintf(&b, "        resolveNodes(v, nt, v, TYPE_ID, report);\n")
		fmt.Fprintf(&b, "        if (nt.recs != null && !nt.bad) {\n")
		fmt.Fprintf(&b, "            for (TableVariable.NodeRecord rec : nt.recs) {\n")
		fmt.Fprintf(&b, "                if (rec.table != null) {\n")
		fmt.Fprintf(&b, "                    resolveNodePointers(rec.table, rec.typeId, nt, v, TYPE_ID, report);\n")
		fmt.Fprintf(&b, "                }\n")
		fmt.Fprintf(&b, "            }\n")
		fmt.Fprintf(&b, "        }\n")
		fmt.Fprintf(&b, "        if (report.malformed) { return null; } // DAMAGE DISCARDS THE WHOLE LOAD (§4)\n")
		fmt.Fprintf(&b, "        return v;\n")
		fmt.Fprintf(&b, "    }\n\n")
	}

	writeLoadMeasure(&b, u, st)
	writeJsonCodec(&b, u, st)

	b.WriteString("}\n")
	source := b.String()
	if st.IsMapEntry() {
		// Entries are codec bodies reached through their holder, never roots
		// (SPEC-TABLES §2.8). Keep root conveniences out of the public API.
		for _, signature := range []string{"byte[] save(", "Value load(", "long loadMeasure(", "byte[] toJson(", "Value fromJson("} {
			source = strings.ReplaceAll(source, "public static "+signature, "static "+signature)
		}
	}
	return source, nil
}

func writeLoadMeasure(b *strings.Builder, u *ir.Unit, st *ir.Struct) {
	align := ir.TableRegionAlign(u)
	recordSize := ir.RecordLayout(u, st).Size
	bytesBlob, strBlob := ir.PointerReachableBlobs(st)
	b.WriteString("    // loadMeasure sizes the region a tolerant load would need (docs/SPEC-TABLES.md §6.5).\n")
	fmt.Fprintf(b, "    private static final long ALIGN = %d;\n", align)
	fmt.Fprintf(b, "    private static final long RECORD_SIZE = %d;\n", recordSize)
	b.WriteString("    private static long alignUp(long n, long a) { return (n + a - 1) & -a; }\n")
	b.WriteString("    private static long round(long n) { return (n + ALIGN - 1) & -ALIGN; }\n")
	b.WriteString("    // A node table record's region is its rounded record layout plus the\n")
	b.WriteString("    // maps and lists its own body carries (docs/SPEC-TABLES.md §6.5).\n")
	b.WriteString("    // The reader is narrowed to the record body IN PLACE, so measuring a\n")
	b.WriteString("    // record copies nothing and allocates nothing.\n")
	b.WriteString("    static long measureNode(TableVariable.Reader r, int off, int len) {\n")
	b.WriteString("        int lim = r.limit;\n")
	b.WriteString("        boolean fail = r.fail;\n")
	b.WriteString("        r.limit = off + len;\n")
	b.WriteString("        r.off = off;\n")
	b.WriteString("        r.fail = false;\n")
	b.WriteString("        long extent = measureFields(r, null, r.report);\n")
	b.WriteString("        r.limit = lim;\n")
	b.WriteString("        r.fail = fail;\n")
	b.WriteString("        return round(round(RECORD_SIZE) + extent);\n")
	b.WriteString("    }\n\n")
	b.WriteString("    // NODE_SIZER is this root's answer for one node table record: the\n")
	b.WriteString("    // pointer-reachable TABLE types it can place, plus the two reserved\n")
	b.WriteString("    // BLOB type ids this root's pointer closure names (docs/SPEC-TABLES.md\n")
	b.WriteString("    // §2.5, §3.1, §6.5). A type id outside that set commands no region.\n")
	// written in google-java-format --aosp's own shape (docs/JAVA-DIALECT.md,
	// make tables-java-clean): the initializer breaks after `=` and the
	// signature one parameter per line, which Canonical does not lay out
	b.WriteString("    private static final TableVariable.NodeSizer NODE_SIZER =\n")
	b.WriteString("            new TableVariable.NodeSizer() {\n")
	b.WriteString("                public long size(\n")
	b.WriteString("                        long typeId,\n")
	b.WriteString("                        TableVariable.Reader r,\n")
	b.WriteString("                        int off,\n")
	b.WriteString("                        int len,\n")
	b.WriteString("                        TableVariable.Measure m) {\n")
	for _, reach := range ir.PointerReachable(st) {
		fmt.Fprintf(b, "                    if (typeId == %sVariable.TYPE_ID) {\n", reach.Name)
		fmt.Fprintf(b, "                        return %sVariable.measureNode(r, off, len);\n", reach.Name)
		b.WriteString("                    }\n")
	}
	if strBlob {
		b.WriteString("                    if (typeId == TableVariable.STRING_ID) {\n")
		b.WriteString("                        return (8 + len + 1 + ALIGN - 1) & -ALIGN;\n")
		b.WriteString("                    }\n")
	}
	if bytesBlob {
		b.WriteString("                    if (typeId == TableVariable.BYTES_ID) {\n")
		b.WriteString("                        return (8 + len + ALIGN - 1) & -ALIGN;\n")
		b.WriteString("                    }\n")
	}
	b.WriteString("                    return 0;\n")
	b.WriteString("                }\n")
	b.WriteString("            };\n\n")
	b.WriteString("    // The one-argument form allocates ONE Measure handle per call; a caller\n")
	b.WriteString("    // that keeps a Measure measures with no allocation at all: the walk reads\n")
	b.WriteString("    // every nested body in place on the Measure's one reader (Reader.bodyEnd)\n")
	b.WriteString("    // and the node table without copying a record (TableVariable.measureNodes).\n")
	fmt.Fprintf(b, "    public static long loadMeasure(byte[] data) {\n")
	fmt.Fprintf(b, "        return loadMeasure(data, new TableVariable.Measure());\n")
	fmt.Fprintf(b, "    }\n\n")
	fmt.Fprintf(b, "    public static long loadMeasure(byte[] data, TableVariable.Measure m) {\n")
	fmt.Fprintf(b, "        TableVariable.Reader r = m.open(data);\n")
	fmt.Fprintf(b, "        if (r == null) { return -1; }\n")
	fmt.Fprintf(b, "        m.nodeSizer = NODE_SIZER;\n")
	fmt.Fprintf(b, "        int start = r.off;\n")
	fmt.Fprintf(b, "        int limit = r.limit;\n")
	fmt.Fprintf(b, "        long extent = measureAt(r, m.report, 0);\n")
	fmt.Fprintf(b, "        if (m.report.refused) { return -1; }\n")
	fmt.Fprintf(b, "        long total = round(round(RECORD_SIZE) + extent);\n")
	b.WriteString("        // the node table is found by its own scan of the root body, as the\n")
	b.WriteString("        // oracle's nodeTableBytes finds it, not by the record walk (§3.1)\n")
	fmt.Fprintf(b, "        r.off = start;\n")
	fmt.Fprintf(b, "        r.limit = limit;\n")
	fmt.Fprintf(b, "        TableVariable.measureNodeTable(r, m, ALIGN);\n")
	fmt.Fprintf(b, "        if (m.report.refused) { return -1; }\n")
	fmt.Fprintf(b, "        long records = 0;\n")
	fmt.Fprintf(b, "        if (m.nodeRecords >= 0) {\n")
	fmt.Fprintf(b, "            records = m.nodeRecords;\n")
	fmt.Fprintf(b, "            total += m.nodeBlobBytes + m.nodeTableBytes;\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "        total += (records + 1) * 16;\n")
	fmt.Fprintf(b, "        return total;\n")
	fmt.Fprintf(b, "    }\n\n")

	writeMeasureWalk(b, u, st)
}

// writeMeasureWalk emits the record walk of the region measure
// (docs/SPEC-TABLES.md §6.5): measureAt is internal/tablewire's
// extentScan.record over this table's body, field by field, and each
// measureField_/measureUnion_/measureArm_ method is extentScan.field or
// extentScan.union for one field, union or arm. The running extent `at` is
// threaded through every nested body exactly as the oracle's one counter is,
// so a nested map or list aligns against the offset its parents reached. A
// storage refusal (§6.5: a count past 2^31-1 or past what its body can frame)
// sets report.refused and unwinds. Every body is walked IN PLACE on the one
// reader: limit narrowed, then off, limit and fail put back.
func writeMeasureWalk(b *strings.Builder, u *ir.Unit, st *ir.Struct) {
	b.WriteString("    static long measureFields(TableVariable.Reader r, TableVariable.Measure nt, TableVariable.Report report) {\n")
	b.WriteString("        return measureAt(r, report, 0);\n")
	b.WriteString("    }\n\n")

	b.WriteString("    // measureAt walks one body of this table on r (docs/SPEC-TABLES.md §6.5)\n")
	b.WriteString("    // and answers the extent counter advanced by the maps and lists it carries.\n")
	b.WriteString("    static long measureAt(TableVariable.Reader r, TableVariable.Report report, long at) {\n")
	b.WriteString("        r.fail = false;\n")
	b.WriteString("        while (true) {\n")
	b.WriteString("            long ref = r.leb();\n")
	b.WriteString("            if (r.fail || ref == 0) { return at; }\n")
	b.WriteString("            long id = r.resolve(ref);\n")
	b.WriteString("            if (r.fail || !r.has(1)) { return at; }\n")
	b.WriteString("            int kind = r.get8();\n")
	var methods []func()
	unions := map[*ir.Union]bool{}
	var unionOrder []*ir.Union
	var noteUnion func(un *ir.Union)
	noteUnion = func(un *ir.Union) {
		if unions[un] {
			return
		}
		unions[un] = true
		unionOrder = append(unionOrder, un)
		for _, v := range un.Variants {
			if v.F == nil || v.F.Type.Pointer {
				continue
			}
			if inner, ok := v.F.Type.Ref.(*ir.Union); ok {
				noteUnion(inner)
			}
		}
	}
	for _, f := range st.Fields {
		if un, ok := f.Type.Ref.(*ir.Union); ok && f.Array == ir.ArrayNone && !f.Type.Pointer {
			noteUnion(un)
			fmt.Fprintf(b, "            if (id == F_%s && kind == %d) {\n", varMember(f), ir.TableKindUnion)
			fmt.Fprintf(b, "                at = measureUnion_%s(r, report, at);\n", javaIdent(un.Name))
			b.WriteString("                if (report.refused) { return at; }\n")
			b.WriteString("                continue;\n")
			b.WriteString("            }\n")
		}
		wk := ir.TableWireFieldKind(f)
		if !measureFieldActs(f) || (wk != ir.TableKindTable && wk != ir.TableKindArray && wk != ir.TableKindKeyed) {
			continue
		}
		name := "measureField_" + varMember(f)
		fmt.Fprintf(b, "            if (id == F_%s && (kind == %d || kind == %d || kind == %d)) {\n", varMember(f), ir.TableKindTable, ir.TableKindArray, ir.TableKindKeyed)
		b.WriteString("                int end = r.bodyEnd();\n")
		b.WriteString("                if (end < 0) { return at; }\n")
		fmt.Fprintf(b, "                if (kind == %d) {\n", wk)
		b.WriteString("                    int lim = r.limit;\n")
		b.WriteString("                    r.limit = end;\n")
		fmt.Fprintf(b, "                    at = %s(r, report, at);\n", name)
		b.WriteString("                    r.limit = lim;\n")
		b.WriteString("                    r.fail = false;\n")
		b.WriteString("                    if (report.refused) { return at; }\n")
		b.WriteString("                }\n")
		b.WriteString("                r.off = end;\n")
		b.WriteString("                continue;\n")
		b.WriteString("            }\n")
		f := f
		methods = append(methods, func() { writeMeasureField(b, u, f, name, noteUnion) })
	}
	b.WriteString("            r.skip(kind);\n")
	b.WriteString("            if (r.fail) { return at; }\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")
	for _, m := range methods {
		m()
	}
	for i := 0; i < len(unionOrder); i++ { // writeMeasureField may note more unions
		writeMeasureUnion(b, u, unionOrder[i], noteUnion)
	}
}

// measureFieldActs reports whether extentScan.field does anything for f
// (docs/SPEC-TABLES.md §6.5): a by-value pointer is a node index and commands
// nothing here; a container or a nested table is walked.
func measureFieldActs(f *ir.Field) bool {
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		return false
	}
	if f.IsMap() || f.IsList() || f.Array != ir.ArrayNone || f.KeyEnum != "" {
		return true
	}
	_, ok := f.Type.Ref.(*ir.Struct)
	return ok
}

func javaIdent(name string) string {
	var b strings.Builder
	for _, c := range name {
		if c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			b.WriteRune(c)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

// writeMeasureField emits extentScan.field for f as one method over r,
// narrowed to the field's payload (docs/SPEC-TABLES.md §6.5): a container's
// element kind (exact, or one the declared kind widens from), its count, the
// storage refusal and the aligned element storage a map or list commands, then
// each TABLE or UNION element's own walk; a nested table's body is walked as
// that table's record.
func writeMeasureField(b *strings.Builder, u *ir.Unit, f *ir.Field, name string, noteUnion func(*ir.Union)) {
	fmt.Fprintf(b, "    // %s is extentScan.field for %s (docs/SPEC-TABLES.md §6.5).\n", name, f.Name)
	fmt.Fprintf(b, "    static long %s(TableVariable.Reader r, TableVariable.Report report, long at) {\n", name)
	if !measureFieldActs(f) {
		b.WriteString("        return at;\n")
		b.WriteString("    }\n\n")
		return
	}
	if !f.IsMap() && !f.IsList() && f.Array == ir.ArrayNone && f.KeyEnum == "" {
		st := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        return %sVariable.measureAt(r, report, at);\n", st.Name)
		b.WriteString("    }\n\n")
		return
	}
	expected := ir.TableWireElemKind(f)
	if f.IsMap() {
		expected = ir.TableKindTable
	}
	var accepted []int
	for k := range 256 {
		if k == expected || ir.TableKindWidens(k, expected) {
			accepted = append(accepted, k)
		}
	}
	var cond []string
	for _, k := range accepted {
		cond = append(cond, fmt.Sprintf("ek != %d", k))
	}
	b.WriteString("        if (!r.has(2)) { return at; }\n")
	b.WriteString("        int ek = r.get8();\n")
	fmt.Fprintf(b, "        if (%s) { return at; }\n", strings.Join(cond, " && "))
	b.WriteString("        long count = r.leb();\n")
	b.WriteString("        if (r.fail) { return at; }\n")
	if f.IsMap() || f.IsList() {
		var size, align int64
		if f.IsMap() {
			ml := ir.RecordLayout(u, f.MapEntry)
			size, align = ml.Size, ml.Align
		} else {
			size, align = ir.ListElementLayout(u, f)
		}
		b.WriteString("        if (count < 0 || count > 0x7FFFFFFFL) { report.refused = true; report.reason = \"count_refusal\"; return at; }\n")
		b.WriteString("        long floor = 1;\n")
		for _, k := range accepted {
			floor := int64(ir.TableKindWidth(k))
			if k == ir.TableKindPointer {
				floor = 1
			}
			if k == ir.TableKindTable {
				floor = 2
			}
			if floor < 1 {
				floor = 1
			}
			if floor != 1 {
				fmt.Fprintf(b, "        if (ek == %d) { floor = %d; }\n", k, floor)
			}
		}
		b.WriteString("        if (count > (long) (r.limit - r.off) / floor) { report.refused = true; return at; }\n")
		fmt.Fprintf(b, "        at = ((at + %d - 1) & -%dL) + count * %dL;\n", align, align, size)
	}
	walksTable, walksUnion := false, false
	for _, k := range accepted {
		walksTable = walksTable || k == ir.TableKindTable
		walksUnion = walksUnion || k == ir.TableKindUnion
	}
	if !walksTable && !walksUnion {
		b.WriteString("        return at;\n")
		b.WriteString("    }\n\n")
		return
	}
	b.WriteString("        for (long i = 0; Long.compareUnsigned(i, count) < 0 && r.has(1); i++) {\n")
	if f.KeyEnum != "" {
		b.WriteString("            r.leb();\n")
		b.WriteString("            if (r.fail) { return at; }\n")
	}
	if walksUnion {
		if un, ok := f.Type.Ref.(*ir.Union); ok {
			noteUnion(un)
			fmt.Fprintf(b, "            if (ek == %d) { at = measureUnion_%s(r, report, at); if (report.refused) { return at; } continue; }\n", ir.TableKindUnion, javaIdent(un.Name))
		} else {
			// the oracle reads nothing for a union element it cannot name
			fmt.Fprintf(b, "            if (ek == %d) { continue; }\n", ir.TableKindUnion)
		}
	}
	b.WriteString("            int end = r.bodyEnd();\n")
	b.WriteString("            if (end < 0) { return at; }\n")
	elem, _ := f.Type.Ref.(*ir.Struct)
	if f.IsMap() {
		elem = f.MapEntry
	}
	if elem != nil {
		b.WriteString("            int lim = r.limit;\n")
		b.WriteString("            r.limit = end;\n")
		fmt.Fprintf(b, "            at = %sVariable.measureAt(r, report, at);\n", elem.Name)
		b.WriteString("            r.limit = lim;\n")
		b.WriteString("            r.fail = false;\n")
		b.WriteString("            if (report.refused) { return at; }\n")
	}
	b.WriteString("            r.off = end;\n")
	b.WriteString("        }\n")
	b.WriteString("        return at;\n")
	b.WriteString("    }\n\n")
}

// writeMeasureUnion emits extentScan.union for un (docs/SPEC-TABLES.md §2.6,
// §6.5): the arm reference, its kind byte and L-framed body, and the first arm
// of that wire id that is neither void nor a pointer walked over that body. A
// framing failure ends the enclosing body, as the oracle's cursor to the end
// does.
func writeMeasureUnion(b *strings.Builder, u *ir.Unit, un *ir.Union, noteUnion func(*ir.Union)) {
	name := javaIdent(un.Name)
	fmt.Fprintf(b, "    // measureUnion_%s is extentScan.union for union %s (docs/SPEC-TABLES.md §6.5).\n", name, un.Name)
	fmt.Fprintf(b, "    static long measureUnion_%s(TableVariable.Reader r, TableVariable.Report report, long at) {\n", name)
	b.WriteString("        long ref = r.leb();\n")
	b.WriteString("        if (r.fail) { r.off = r.limit; return at; }\n")
	b.WriteString("        if (ref == 0) { return at; }\n")
	b.WriteString("        long id = r.resolve(ref);\n")
	b.WriteString("        if (r.fail || !r.has(1)) { r.off = r.limit; return at; }\n")
	b.WriteString("        r.get8();\n")
	b.WriteString("        int end = r.bodyEnd();\n")
	b.WriteString("        if (end < 0) { r.off = r.limit; return at; }\n")
	b.WriteString("        int lim = r.limit;\n")
	b.WriteString("        r.limit = end;\n")
	seen := map[uint64]bool{}
	var arms []func()
	first := true
	for i, v := range un.Variants {
		if v.F == nil || v.F.Type.Pointer {
			continue
		}
		id := ir.TableWireId(v.WireName())
		if seen[id] {
			continue
		}
		seen[id] = true
		var call string
		if inner, ok := v.F.Type.Ref.(*ir.Union); ok && v.F.Array == ir.ArrayNone {
			noteUnion(inner)
			call = fmt.Sprintf("measureUnion_%s(r, report, at)", javaIdent(inner.Name))
		} else if measureFieldActs(v.F) {
			arm := fmt.Sprintf("measureArm_%s_%d", name, i)
			call = arm + "(r, report, at)"
			f := v.F
			arms = append(arms, func() { writeMeasureField(b, u, f, arm, noteUnion) })
		} else {
			// the arm is matched and walks nothing; a later arm of the same
			// id is never reached, so the id stays claimed
			continue
		}
		if !first {
			b.WriteString("        else ")
		} else {
			b.WriteString("        ")
		}
		first = false
		fmt.Fprintf(b, "if (id == 0x%xL) { at = %s; }\n", id, call)
	}
	b.WriteString("        r.off = end;\n")
	b.WriteString("        r.limit = lim;\n")
	b.WriteString("        r.fail = false;\n")
	b.WriteString("        return at;\n")
	b.WriteString("    }\n\n")
	for _, a := range arms {
		a()
	}
}

func writeJsonCodec(b *strings.Builder, u *ir.Unit, st *ir.Struct) {
	fmt.Fprintf(b, "    public static byte[] toJson(Value v) {\n")
	fmt.Fprintf(b, "        TableVariable.NodeGraph graph = new TableVariable.NodeGraph();\n")
	fmt.Fprintf(b, "        graph.setOpen(v, true);\n")
	fmt.Fprintf(b, "        if (!buildGraph(graph, v)) { return null; }\n")
	fmt.Fprintf(b, "        graph.setOpen(v, false);\n")
	fmt.Fprintf(b, "        StringBuilder b = new StringBuilder();\n")
	fmt.Fprintf(b, "        try { jsonFields(b, graph, v, 0); } catch (TableJson.Refused e) { return null; }\n")
	fmt.Fprintf(b, "        b.append('\\n');\n")
	fmt.Fprintf(b, "        return b.toString().getBytes(StandardCharsets.UTF_8);\n")
	fmt.Fprintf(b, "    }\n\n")

	fmt.Fprintf(b, "    public static Value fromJson(byte[] text, TableVariable.Report report) {\n")
	fmt.Fprintf(b, "        TableJson.In in = new TableJson.In(text, report);\n")
	fmt.Fprintf(b, "        Value v = new Value();\n")
	fmt.Fprintf(b, "        jsonReadFields(in, v);\n")
	fmt.Fprintf(b, "        if (in.bad) { report.malformed = true; }\n")
	fmt.Fprintf(b, "        if (report.malformed) { return null; } // DAMAGE DISCARDS THE WHOLE TEXT READ (§4, §16.2)\n")
	fmt.Fprintf(b, "        return v;\n")
	fmt.Fprintf(b, "    }\n\n")

	fmt.Fprintf(b, "    static void jsonFields(StringBuilder b, TableVariable.NodeGraph graph, Value v, int depth) {\n")
	b.WriteString("        b.append('{');\n")
	b.WriteString("        boolean empty = jsonFieldsBody(b, graph, v, depth + 1, true);\n")
	b.WriteString("        if (!empty) TableJson.indent(b, depth);\n")
	b.WriteString("        b.append('}');\n")
	b.WriteString("    }\n\n")

	fmt.Fprintf(b, "    static boolean jsonFieldsBody(StringBuilder b, TableVariable.NodeGraph graph, Value v, int depth, boolean first) {\n")
	for _, f := range st.Fields {
		guarded := openGuard(b, st, f)
		if f.Type.Optional {
			fmt.Fprintf(b, "        if(v.%sPresent){\n", varMember(f))
		}
		b.WriteString("        if (!first) { b.append(','); }\n")
		b.WriteString("        first = false;\n")
		b.WriteString("        TableJson.indent(b, depth);\n")
		fmt.Fprintf(b, "        TableJson.esc(b, %q);\n", jsonKey(f))
		b.WriteString("        b.append(\": \");\n")
		writeJsonFieldValue(b, u, st, f)
		if f.Type.Optional {
			b.WriteString("        }\n")
		}
		closeGuard(b, guarded)
	}
	b.WriteString("        return first;\n")
	b.WriteString("    }\n\n")

	fmt.Fprintf(b, "    public static void writeJsonPointer(StringBuilder b, TableVariable.NodeGraph graph, Value child, int depth) {\n")
	b.WriteString("        if (child == null) { b.append(\"null\"); return; }\n")
	b.WriteString("        if (graph.useCount(child) <= 1) {\n")
	fmt.Fprintf(b, "            jsonFields(b, graph, child, depth);\n")
	b.WriteString("            return;\n")
	b.WriteString("        }\n")
	b.WriteString("        Long label = graph.labels.get(child);\n")
	b.WriteString("        if (label == null) {\n")
	b.WriteString("            label = (long) (graph.labels.size() + 1);\n")
	b.WriteString("            graph.labels.put(child, label);\n")
	b.WriteString("            b.append('{');\n")
	b.WriteString("            TableJson.indent(b, depth + 1);\n")
	b.WriteString("            b.append(\"\\\"&node\\\": \").append(label);\n")
	fmt.Fprintf(b, "            jsonFieldsBody(b, graph, child, depth + 1, false);\n")
	b.WriteString("            TableJson.indent(b, depth);\n")
	b.WriteString("            b.append('}');\n")
	b.WriteString("        } else {\n")
	b.WriteString("            b.append('{');\n")
	b.WriteString("            TableJson.indent(b, depth + 1);\n")
	b.WriteString("            b.append(\"\\\"&node\\\": \").append(label);\n")
	b.WriteString("            TableJson.indent(b, depth);\n")
	b.WriteString("            b.append('}');\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")

	fmt.Fprintf(b, "    public static Value readJsonPointer(TableJson.In in, long targetTypeId) {\n")
	b.WriteString("        if (in.peek() == 'n') { in.lit(\"null\"); return null; }\n")
	b.WriteString("        if (in.peek() != '{') { in.skipValue(); in.rep.kindMismatch++; return null; }\n")
	b.WriteString("        int start = in.p;\n")
	b.WriteString("        in.p++;\n")
	b.WriteString("        in.space();\n")
	b.WriteString("        if (in.peek() == '}') { in.p = start; Value v = new Value(); jsonReadFields(in, v); return v; }\n")
	b.WriteString("        String key = in.scanString();\n")
	b.WriteString("        if (in.bad || key == null) return null;\n")
	b.WriteString("        if (!\"&node\".equals(key)) { in.p = start; Value v = new Value(); jsonReadFields(in, v); return v; }\n")
	b.WriteString("        if (in.peek() != ':') { in.bad = true; return null; }\n")
	b.WriteString("        in.p++;\n")
	b.WriteString("        long labelId = in.scanLabel();\n")
	b.WriteString("        if (in.bad || labelId <= 0) return null;\n")
	b.WriteString("        int c = in.peek();\n")
	b.WriteString("        if (c == '}') {\n")
	b.WriteString("            in.p++;\n")
	b.WriteString("            TableJson.LabelEntry entry = in.labels.get(labelId);\n")
	b.WriteString("            if (entry == null || entry.open) { in.bad = true; return null; }\n")
	b.WriteString("            if (entry.typeId == 0) { return null; }\n")
	b.WriteString("            if (entry.typeId != targetTypeId) { in.rep.kindMismatch++; return null; }\n")
	b.WriteString("            return (Value) entry.value;\n")
	b.WriteString("        } else if (c == ',') {\n")
	b.WriteString("            in.p++;\n")
	b.WriteString("            if (in.peek() == '}') { in.bad = true; return null; }\n")
	b.WriteString("            if (in.labels.containsKey(labelId)) { in.bad = true; return null; }\n")
	b.WriteString("            Value child = new Value();\n")
	b.WriteString("            TableJson.LabelEntry entry = new TableJson.LabelEntry(child, targetTypeId, true);\n")
	b.WriteString("            in.labels.put(labelId, entry);\n")
	fmt.Fprintf(b, "            jsonReadFieldsBody(in, child);\n")
	b.WriteString("            entry.open = false;\n")
	b.WriteString("            return child;\n")
	b.WriteString("        } else {\n")
	b.WriteString("            in.bad = true; return null;\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")

	fmt.Fprintf(b, "    static void jsonReadFields(TableJson.In in, Value v) {\n")
	b.WriteString("        in.space();\n")
	b.WriteString("        if (in.peek() != '{') { in.bad = true; return; }\n")
	b.WriteString("        in.p++;\n")
	b.WriteString("        jsonReadFieldsBody(in, v);\n")
	b.WriteString("    }\n\n")

	fmt.Fprintf(b, "    static void jsonReadFieldsBody(TableJson.In in, Value v) {\n")
	// A REPEATED FIELD KEY IS A DUPLICATE KEY (docs/SPEC-TABLES.md §16.2): the
	// last occurrence wins whole, and the repeat is counted BEFORE the value's
	// shape is judged, so a repeat the walk then refuses is still a repeat.
	fmt.Fprintf(b, "        boolean[] seenField = new boolean[%d];\n", len(st.Fields))
	b.WriteString("        while (true) {\n")
	b.WriteString("            int c = in.peek();\n")
	b.WriteString("            if (c == 0) { in.bad = true; return; }\n")
	b.WriteString("            if (c == '}') { in.p++; return; }\n")
	b.WriteString("            String key = in.scanString();\n")
	b.WriteString("            if (in.bad) { return; }\n")
	b.WriteString("            if (key != null && key.startsWith(\"&\")) { in.bad = true; return; }\n")
	b.WriteString("            if (in.peek() != ':') { in.bad = true; return; }\n")
	b.WriteString("            in.p++;\n")
	for i, f := range st.Fields {
		fmt.Fprintf(b, "            if (%q.equals(key)) {\n", jsonKey(f))
		fmt.Fprintf(b, "                boolean repeated = seenField[%d];\n", i)
		fmt.Fprintf(b, "                if (repeated) { in.rep.duplicate++; } seenField[%d] = true;\n", i)
		// §16.2: the wrong JSON type for the kind is skipped and counted
		// kind_mismatch, never coerced; null goes to the field's own reader,
		// which is where a `?T` and a pointer take their absence.
		// PRESENCE OF THE KEY IS PRESENCE (§16.2): a `?T` whose value is the wrong
		// shape is still present, at its defaults.
		present := ""
		if f.Type.Optional {
			present = fmt.Sprintf(" v.%sPresent = true;", varMember(f))
		}
		fmt.Fprintf(b, "                if (in.peek() != 'n' && !(%s)) { in.skipValue(); in.rep.kindMismatch++;%s } else {\n", variableJsonShape(f, "in.peek()"), present)
		// RE-ESTABLISHMENT HAPPENS ON PLACEMENT (§16.2): a repeat the walk takes
		// replaces the first occurrence whole, at the field's defaults; only
		// `null` on a field that takes it is a placement too.
		// A scalar's read overwrites it whole, or leaves it standing when the
		// value is rejected, so only the composites are reset first.
		if f.KeyEnum == "" && !f.IsMap() && (f.Array != ir.ArrayNone || isStruct(f)) {
			takesNull := "in.peek() != 'n'"
			if f.Type.Optional || f.Type.Pointer {
				takesNull = "true"
			}
			fmt.Fprintf(b, "                if (repeated && %s) { v.%s = %s; }\n", takesNull, varMember(f), variableDefault(f))
		}
		readJsonFieldValue(b, u, st, f)
		b.WriteString("                }\n")
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

func jsonKey(f *ir.Field) string { return ir.TableFieldJsonKey(f) }

func jsonIntRange(f *ir.Field) (min, max string) {
	if f.Type.Signed && f.Type.Kind == ir.TInt {
		switch f.Type.Width {
		case 8:
			min, max = "-128L", "127L"
		case 16:
			min, max = "-32768L", "32767L"
		case 32:
			min, max = "Integer.MIN_VALUE", "Integer.MAX_VALUE"
		default:
			min, max = "Long.MIN_VALUE", "Long.MAX_VALUE"
		}
		if f.HasIntRange {
			min = f.IntMin.String()
			max = f.IntMax.String()
			if f.Type.Width > 32 {
				min += "L"
				max += "L"
			}
		}
	} else {
		if f.Type.Width < 64 {
			min, max = "0L", fmt.Sprintf("0x%xL", (uint64(1)<<uint(f.Type.Width))-1)
			if f.HasIntRange {
				min = f.IntMin.String() + "L"
				max = f.IntMax.String() + "L"
			}
		} else {
			min, max = "0L", "0xffffffffffffffffL"
			if f.HasIntRange {
				if f.IntMin.Cmp(big.NewInt(math.MaxInt64)) > 0 {
					min = fmt.Sprintf("0x%xL", f.IntMin)
				} else {
					min = f.IntMin.String() + "L"
				}
				if f.IntMax.Cmp(big.NewInt(math.MaxInt64)) > 0 {
					max = fmt.Sprintf("0x%xL", f.IntMax)
				} else {
					max = f.IntMax.String() + "L"
				}
			}
		}
	}
	return min, max
}

func writeJsonArrayValue(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	m := varMember(f)
	fmt.Fprintf(b, "        if (v.%s.isEmpty()) { b.append(\"[]\"); } else {\n", m)
	b.WriteString("            b.append('[');\n")
	fmt.Fprintf(b, "            for (int i = 0; i < v.%s.size(); i++) {\n", m)
	b.WriteString("                if (i > 0) { b.append(','); }\n")
	b.WriteString("                TableJson.indent(b, depth + 1);\n")
	if variableWide(f) {
		fmt.Fprintf(b, "                b.append(%s);\n", variableWideJson(f, "v."+m+".get(i)"))
	} else {
		switch f.Type.Kind {
		case ir.TBool:
			fmt.Fprintf(b, "                b.append(v.%s.get(i) ? \"true\" : \"false\");\n", m)
		case ir.TInt:
			if f.Type.Signed {
				if f.Type.Width <= 32 {
					fmt.Fprintf(b, "                b.append(Integer.toString(v.%s.get(i)));\n", m)
				} else {
					fmt.Fprintf(b, "                b.append(Long.toString(v.%s.get(i)));\n", m)
				}
			} else {
				if f.Type.Width <= 32 {
					fmt.Fprintf(b, "                b.append(Integer.toUnsignedString(v.%s.get(i)));\n", m)
				} else {
					fmt.Fprintf(b, "                b.append(Long.toUnsignedString(v.%s.get(i)));\n", m)
				}
			}
		case ir.TBits:
			if f.Type.Width <= 32 {
				fmt.Fprintf(b, "                b.append(Integer.toUnsignedString(v.%s.get(i)));\n", m)
			} else {
				fmt.Fprintf(b, "                b.append(Long.toUnsignedString(v.%s.get(i)));\n", m)
			}
		case ir.TFloat32:
			fmt.Fprintf(b, "                TableJson.writeFloat(b, v.%s.get(i), true);\n", m)
		case ir.TFloat64:
			fmt.Fprintf(b, "                TableJson.writeFloat(b, v.%s.get(i), false);\n", m)
		case ir.TString:
			fmt.Fprintf(b, "                TableJson.esc(b, v.%s.get(i));\n", m)
		case ir.TNamed:
			if un := variableUnion(f); un != nil {
				fmt.Fprintf(b, "                %sVariable.jsonFields(b,graph,v.%s.get(i),depth+1);\n", un.Name, m)
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Enum:
				fmt.Fprintf(b, "                switch (v.%s.get(i)) {\n", m)
				for j := range ref.Variants {
					fmt.Fprintf(b, "                    case %d: TableJson.esc(b, %q); break;\n", j+1, ref.Variants[j])
				}
				b.WriteString("                    default: TableJson.esc(b, \"None\"); break;\n                }\n")
			case *ir.Flags:
				fmt.Fprintf(b, "                TableJson.writeFlags(b,v.%s.get(i),%s,depth+1);\n", m, variableFlagNames(ref))
			}
		}
	}
	b.WriteString("            }\n")
	b.WriteString("            TableJson.indent(b, depth);\n")
	b.WriteString("            b.append(']');\n")
	b.WriteString("        }\n")
}

func readJsonArrayValue(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	m := varMember(f)
	fmt.Fprintf(b, "                if (in.peek() == '[') {\n")
	b.WriteString("                    in.p++;\n")
	b.WriteString("                    int idx = 0;\n")
	if f.Array != ir.ArrayList {
		fmt.Fprintf(b, "                    int ab = %d;\n", f.ArrayBound)
	} else {
		b.WriteString("                    int ab = -1;\n")
	}
	b.WriteString("                    while (true) {\n")
	b.WriteString("                        int ec = in.peek();\n")
	b.WriteString("                        if (ec == ']') { in.p++; break; }\n")
	b.WriteString("                        if (ec == 0) { in.bad = true; break; }\n")
	b.WriteString("                        if (ab >= 0 && idx >= ab) {\n")
	b.WriteString("                            in.rep.clamped++;\n")
	b.WriteString("                            in.skipValue();\n")
	b.WriteString("                        } else {\n")
	// the element read is the else block's body, one level deeper than the
	// templates below write it; it is indented whole on the way out so the
	// formatter's shape holds (make tables-java-clean)
	outer := b
	b = &strings.Builder{}
	if variableWide(f) {
		writeVariableWideJsonRead(b, f, "v."+m, true)
	} else {
		switch f.Type.Kind {
		case ir.TBool:
			fmt.Fprintf(b, "                        if (ec == 't') { in.lit(\"true\"); v.%s.add(true); }\n", m)
			fmt.Fprintf(b, "                        else if (ec == 'f') { in.lit(\"false\"); v.%s.add(false); }\n", m)
			b.WriteString("                        else { in.skipValue(); in.rep.kindMismatch++; }\n")
		case ir.TInt:
			min, max := jsonIntRange(f)
			if f.Type.Width <= 32 {
				fmt.Fprintf(b, "                        if (ec == '-' || (ec >= '0' && ec <= '9')) { v.%s.add((int) in.integer(%t, %s, %s)); }\n", m, f.Type.Signed, min, max)
			} else {
				fmt.Fprintf(b, "                        if (ec == '-' || (ec >= '0' && ec <= '9')) { v.%s.add(in.integer(%t, %s, %s)); }\n", m, f.Type.Signed, min, max)
			}
			b.WriteString("                        else { in.skipValue(); in.rep.kindMismatch++; }\n")
		case ir.TBits:
			min, max := jsonIntRange(f)
			if f.Type.Width <= 32 {
				fmt.Fprintf(b, "                        if (ec == '-' || (ec >= '0' && ec <= '9')) { v.%s.add((int) in.integer(false, %s, %s)); }\n", m, min, max)
			} else {
				fmt.Fprintf(b, "                        if (ec == '-' || (ec >= '0' && ec <= '9')) { v.%s.add(in.integer(false, %s, %s)); }\n", m, min, max)
			}
			b.WriteString("                        else { in.skipValue(); in.rep.kindMismatch++; }\n")
		case ir.TFloat32:
			fmt.Fprintf(b, "                        if (ec == '-' || (ec >= '0' && ec <= '9')) { v.%s.add(in.number32()); }\n", m)
			b.WriteString("                        else { in.skipValue(); in.rep.kindMismatch++; }\n")
		case ir.TFloat64:
			fmt.Fprintf(b, "                        if (ec == '-' || (ec >= '0' && ec <= '9')) { v.%s.add(in.number()); }\n", m)
			b.WriteString("                        else { in.skipValue(); in.rep.kindMismatch++; }\n")
		case ir.TString:
			fmt.Fprintf(b, "                        if (ec == '\"') { v.%s.add(in.readString(%d)); }\n", m, f.Type.Size)
			b.WriteString("                        else { in.skipValue(); in.rep.kindMismatch++; }\n")
		case ir.TNamed:
			if un := variableUnion(f); un != nil {
				// §16.2: an element of the wrong JSON shape is skipped and
				// counted kind_mismatch, exactly as the field gate does.
				fmt.Fprintf(b, "                        if (ec != '{') { in.skipValue(); in.rep.kindMismatch++; v.%s.add(new %sVariable.Value()); } else { v.%s.add(%sVariable.readJson(in)); }\n", m, un.Name, m, un.Name)
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Enum:
				b.WriteString("                        if (ec == '\"') {\n")
				b.WriteString("                            String s = in.scanString();\n")
				b.WriteString("                            if(\"None\".equals(s)){v." + m + ".add(0);}else ")
				for j := range ref.Variants {
					fmt.Fprintf(b, "                            if (%q.equals(s)) { v.%s.add(%d); }\n", ref.Variants[j], m, j+1)
					b.WriteString("                            else ")
				}
				fmt.Fprintf(b, "{ v.%s.add(0); in.rep.unknown++; }\n", m)
				b.WriteString("                        } else { in.skipValue(); in.rep.kindMismatch++; }\n")
			case *ir.Flags:
				fmt.Fprintf(b, "                        {Long bits=TableJson.readFlags(in,%s);v.%s.add(bits==null?0L:bits);}\n", variableFlagNames(ref), m)
			}
		}
	}
	for _, line := range strings.SplitAfter(b.String(), "\n") {
		if strings.TrimSpace(line) != "" && strings.HasPrefix(line, " ") {
			outer.WriteString("    ")
		}
		outer.WriteString(line)
	}
	b = outer
	b.WriteString("                            idx++;\n")
	b.WriteString("                        }\n")
	b.WriteString("                        ec = in.peek();\n")
	b.WriteString("                        if (ec == ',') { in.p++; continue; }\n")
	b.WriteString("                        if (ec == ']') { in.p++; break; }\n")
	b.WriteString("                        in.bad = true; break;\n")
	b.WriteString("                    }\n")
	b.WriteString("                } else { in.skipValue(); in.rep.kindMismatch++; }\n")
}

func writeJsonFieldValue(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	if variableWide(f) && f.Array == ir.ArrayNone && !f.Type.Optional {
		fmt.Fprintf(b, "        b.append(%s);\n", variableWideJson(f, "v."+varMember(f)))
		return
	}
	if flags, ok := f.Type.Ref.(*ir.Flags); ok && f.Array == ir.ArrayNone && !f.Type.Optional {
		fmt.Fprintf(b, "        TableJson.writeFlags(b,v.%s,%s,depth);\n", varMember(f), variableFlagNames(flags))
		return
	}
	if un := variableUnion(f); un != nil && f.Array == ir.ArrayNone && !f.Type.Optional {
		fmt.Fprintf(b, "        %sVariable.jsonFields(b,graph,v.%s,depth);\n", un.Name, varMember(f))
		return
	}
	if f.KeyEnum != "" && !f.Type.Optional {
		writeJsonKeyedValue(b, u, st, f)
		return
	}
	if f.Array == ir.ArrayFixed && f.KeyEnum == "" && !f.Type.Optional {
		one := *f
		one.Array = ir.ArrayNone
		one.ArrayBound = 0
		elem := variableValueType(&one)
		switch elem {
		case "int":
			elem = "Integer"
		case "long":
			elem = "Long"
		case "float":
			elem = "Float"
		case "double":
			elem = "Double"
		case "boolean":
			elem = "Boolean"
		}
		fmt.Fprintf(b, "        { List<%s> padded=new ArrayList<>();for(int j=0;j<%d;j++)padded.add(j<v.%s.size()?v.%s.get(j):%s);\n", elem, f.ArrayBound, varMember(f), varMember(f), variableDefault(&one))
		one = *f
		one.Array = ir.ArrayList
		var body strings.Builder
		writeJsonFieldValue(&body, u, st, &one)
		b.WriteString(strings.ReplaceAll(body.String(), "v."+varMember(f), "padded"))
		b.WriteString("        }\n")
		return
	}
	if f.Type.Optional {
		fmt.Fprintf(b, "        if(!v.%sPresent){b.append(\"null\");}else{\n", varMember(f))
		one := *f
		one.Type.Optional = false
		writeJsonFieldValue(b, u, st, &one)
		b.WriteString("        }\n")
		return
	}
	if f.IsMap() {
		writeJsonMapValue(b, u, st, f)
		return
	}
	m := varMember(f)
	switch {
	case f.Type.Kind == ir.TWString:
		fmt.Fprintf(b, "        TableJson.esc(b, new String(v.%s));\n", m)
	case isPointerArray(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        if (v.%s.isEmpty()) { b.append(\"[]\"); } else {\n", m)
		b.WriteString("            b.append('[');\n")
		fmt.Fprintf(b, "            for (int i = 0; i < v.%s.size(); i++) {\n", m)
		b.WriteString("                if (i > 0) { b.append(','); }\n")
		b.WriteString("                TableJson.indent(b, depth + 1);\n")
		fmt.Fprintf(b, "                %sVariable.writeJsonPointer(b, graph, v.%s.get(i), depth + 1);\n", target.Name, m)
		b.WriteString("            }\n")
		b.WriteString("            TableJson.indent(b, depth);\n")
		b.WriteString("            b.append(']');\n")
		b.WriteString("        }\n")
	case isStructArray(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        if (v.%s.isEmpty()) { b.append(\"[]\"); } else {\n", m)
		b.WriteString("            b.append('[');\n")
		fmt.Fprintf(b, "            for (int i = 0; i < v.%s.size(); i++) {\n", m)
		b.WriteString("                if (i > 0) { b.append(','); }\n")
		b.WriteString("                TableJson.indent(b, depth + 1);\n")
		fmt.Fprintf(b, "                %sVariable.jsonFields(b, graph, v.%s.get(i), depth + 1);\n", target.Name, m)
		b.WriteString("            }\n")
		b.WriteString("            TableJson.indent(b, depth);\n")
		b.WriteString("            b.append(']');\n")
		b.WriteString("        }\n")
	case isScalarArray(f):
		writeJsonArrayValue(b, u, st, f)
	case f.Type.Kind == ir.TString && f.Type.Blob():
		fmt.Fprintf(b, "        if (v.%s == null) { b.append(\"null\"); } else { TableJson.esc(b, v.%s); }\n", m, m)
	case f.Type.Kind == ir.TBytes && f.Type.Blob():
		fmt.Fprintf(b, "        if (v.%s == null) { b.append(\"null\"); } else { TableJson.esc(b, java.util.Base64.getEncoder().encodeToString(v.%s)); }\n", m, m)
	case f.Type.Pointer && isStruct(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        %sVariable.writeJsonPointer(b, graph, v.%s, depth);\n", target.Name, m)
	case isStruct(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        %sVariable.jsonFields(b, graph, v.%s, depth);\n", target.Name, m)
	case f.Type.Kind == ir.TBytes:
		fmt.Fprintf(b, "        TableJson.esc(b,java.util.Base64.getEncoder().encodeToString(v.%s));\n", m)
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(b, "        TableJson.esc(b, v.%s);\n", m)
	case f.Type.Kind == ir.TBool:
		fmt.Fprintf(b, "        b.append(v.%s ? \"true\" : \"false\");\n", m)
	case f.Type.Kind == ir.TFloat32:
		fmt.Fprintf(b, "        TableJson.writeFloat(b, v.%s, true);\n", m)
	case f.Type.Kind == ir.TFloat64:
		fmt.Fprintf(b, "        TableJson.writeFloat(b, v.%s, false);\n", m)
	case (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits) && f.Type.Width <= 32:
		if f.Type.Signed && f.Type.Kind == ir.TInt {
			fmt.Fprintf(b, "        b.append(Integer.toString(v.%s));\n", m)
		} else {
			fmt.Fprintf(b, "        b.append(Integer.toUnsignedString(v.%s));\n", m)
		}
	case (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits):
		if f.Type.Signed && f.Type.Kind == ir.TInt {
			fmt.Fprintf(b, "        b.append(Long.toString(v.%s));\n", m)
		} else {
			fmt.Fprintf(b, "        b.append(Long.toUnsignedString(v.%s));\n", m)
		}
	case f.Type.Kind == ir.TNamed:
		if enum, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(b, "        switch (v.%s) {\n", m)
			for i := range enum.Variants {
				fmt.Fprintf(b, "            case %d: TableJson.esc(b, %q); break;\n", i+1, enum.Variants[i])
			}
			b.WriteString("            default: TableJson.esc(b, \"None\"); break;\n        }\n")
		} else if _, ok := f.Type.Ref.(*ir.Flags); ok {
			fmt.Fprintf(b, "        b.append(Long.toString(v.%s));\n", m)
		}
	}
}

func readJsonFieldValue(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	if variableWide(f) && f.Array == ir.ArrayNone && !f.Type.Optional {
		writeVariableWideJsonRead(b, f, "v."+varMember(f), false)
		return
	}
	if flags, ok := f.Type.Ref.(*ir.Flags); ok && f.Array == ir.ArrayNone && !f.Type.Optional {
		fmt.Fprintf(b, "                {Long bits=TableJson.readFlags(in,%s);if(bits!=null)v.%s=bits;}\n", variableFlagNames(flags), varMember(f))
		return
	}
	if un := variableUnion(f); un != nil && f.Array == ir.ArrayNone && !f.Type.Optional {
		fmt.Fprintf(b, "                v.%s=%sVariable.readJson(in);\n", varMember(f), un.Name)
		return
	}
	if f.KeyEnum != "" && !f.Type.Optional {
		readJsonKeyedValue(b, u, st, f)
		return
	}
	if f.Type.Optional {
		m := varMember(f)
		fmt.Fprintf(b, "                if(in.peek()=='n'){in.lit(\"null\");v.%sPresent=false;v.%s=%s;}else{\n", m, m, variableDefault(f))
		fmt.Fprintf(b, "                boolean selected=%s;\n", variableJsonShape(f, "in.peek()"))
		one := *f
		one.Type.Optional = false
		if f.Array != ir.ArrayNone {
			fmt.Fprintf(b, "                if(selected)v.%s.clear();\n", m)
		}
		readJsonFieldValue(b, u, st, &one)
		fmt.Fprintf(b, "                if(selected&&!in.bad)v.%sPresent=true;}\n", m)
		return
	}
	if f.IsMap() {
		readJsonMapValue(b, u, st, f)
		return
	}
	m := varMember(f)
	switch {
	case f.Type.Kind == ir.TWString:
		fmt.Fprintf(b, "                if (in.peek() == '\"') { String text = in.scanStored(); if (text != null) { char[] units = text.toCharArray(); if (!TableVariable.wideValid(units)) { in.rep.malformed = true; v.%s = new char[0]; } else { v.%s = TableVariable.wideClamp(units, %d, in.rep); } } } else { in.skipValue(); in.rep.kindMismatch++; }\n", m, m, f.Type.Size)
	case isPointerArray(f):
		target := f.Type.Ref.(*ir.Struct)
		b.WriteString("                if (in.peek() == '[') {\n")
		b.WriteString("                    in.p++;\n")
		b.WriteString("                    while (true) {\n")
		b.WriteString("                        int ec = in.peek();\n")
		b.WriteString("                        if (ec == ']') { in.p++; break; }\n")
		b.WriteString("                        if (ec == 0) { in.bad = true; break; }\n")
		fmt.Fprintf(b, "                        %sVariable.Value elem = %sVariable.readJsonPointer(in, %sVariable.TYPE_ID);\n", target.Name, target.Name, target.Name)
		fmt.Fprintf(b, "                        v.%s.add(elem);\n", m)
		b.WriteString("                        ec = in.peek();\n")
		b.WriteString("                        if (ec == ',') { in.p++; continue; }\n")
		b.WriteString("                        if (ec == ']') { in.p++; break; }\n")
		b.WriteString("                        in.bad = true; break;\n")
		b.WriteString("                    }\n")
		b.WriteString("                } else { in.skipValue(); in.rep.kindMismatch++; }\n")
	case isStructArray(f):
		target := f.Type.Ref.(*ir.Struct)
		b.WriteString("                if (in.peek() == '[') {\n")
		b.WriteString("                    in.p++;\n")
		b.WriteString("                    int idx = 0;\n")
		if f.Array != ir.ArrayList {
			fmt.Fprintf(b, "                    int ab = %d;\n", f.ArrayBound)
		} else {
			b.WriteString("                    int ab = -1;\n")
		}
		b.WriteString("                    while (true) {\n")
		b.WriteString("                        int ec = in.peek();\n")
		b.WriteString("                        if (ec == ']') { in.p++; break; }\n")
		b.WriteString("                        if (ec == 0) { in.bad = true; break; }\n")
		b.WriteString("                        if (ab >= 0 && idx >= ab) {\n")
		b.WriteString("                            // MORE ELEMENTS THAN THE BOUND: keep the prefix and\n")
		b.WriteString("                            // count the excess, the wire's rule (docs/SPEC-TABLES.md\n")
		b.WriteString("                            // §4, §16.2). A definition in the dropped tail still\n")
		b.WriteString("                            // takes its label, so a reference reads null (§16.7).\n")
		b.WriteString("                            in.rep.clamped++;\n")
		b.WriteString("                            in.skipValue();\n")
		fmt.Fprintf(b, "                        } else if (ec != '{') { in.skipValue(); in.rep.kindMismatch++; v.%s.add(new %sVariable.Value()); }\n", m, target.Name)
		fmt.Fprintf(b, "                        else { %sVariable.Value e = new %sVariable.Value(); %sVariable.jsonReadFields(in, e); v.%s.add(e); }\n", target.Name, target.Name, target.Name, m)
		b.WriteString("                        idx++;\n")
		b.WriteString("                        ec = in.peek();\n")
		b.WriteString("                        if (ec == ',') { in.p++; continue; }\n")
		b.WriteString("                        if (ec == ']') { in.p++; break; }\n")
		b.WriteString("                        in.bad = true; break;\n")
		b.WriteString("                    }\n")
		b.WriteString("                } else { in.skipValue(); in.rep.kindMismatch++; }\n")
	case isScalarArray(f):
		readJsonArrayValue(b, u, st, f)
	case f.Type.Kind == ir.TString && f.Type.Blob():
		fmt.Fprintf(b, "                if (in.peek() == 'n') { in.lit(\"null\"); v.%s = null; }\n", m)
		fmt.Fprintf(b, "                else if (in.peek() == '\"') { v.%s = in.scanStored(); }\n", m)
		b.WriteString("                else { in.skipValue(); in.rep.kindMismatch++; }\n")
	case f.Type.Kind == ir.TBytes && f.Type.Blob():
		fmt.Fprintf(b, "                if (in.peek() == 'n') { in.lit(\"null\"); v.%s = null; }\n", m)
		// §16.2: a string that is not base64 is the wrong shape for *bytes —
		// counted kind_mismatch and left null, never a broken read.
		fmt.Fprintf(b, "                else if (in.peek() == '\"') { String s = in.scanString(); if (s != null) { try { v.%s = java.util.Base64.getDecoder().decode(s); } catch (IllegalArgumentException invalid) { in.rep.kindMismatch++; v.%s = null; } } }\n", m, m)
		b.WriteString("                else { in.skipValue(); in.rep.kindMismatch++; }\n")
	case f.Type.Pointer && isStruct(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "                v.%s = %sVariable.readJsonPointer(in, %sVariable.TYPE_ID);\n", m, target.Name, target.Name)
	case isStruct(f):
		target := f.Type.Ref.(*ir.Struct)
		b.WriteString("                if (in.peek() == '{') {\n")
		fmt.Fprintf(b, "                    %sVariable.jsonReadFields(in, v.%s);\n", target.Name, m)
		b.WriteString("                } else { in.skipValue(); in.rep.kindMismatch++; }\n")
	case f.Type.Kind == ir.TBytes:
		fmt.Fprintf(b, "                if(in.peek()=='\"'){String text=in.scanString();if(text!=null){try {byte[] bytes=java.util.Base64.getDecoder().decode(text);int keep=Math.min(bytes.length,%d);v.%s=java.util.Arrays.copyOf(bytes,keep);if(bytes.length>keep)in.rep.clamped++;}catch(IllegalArgumentException invalid){in.rep.kindMismatch++;}}}else {in.skipValue();in.rep.kindMismatch++;}\n", f.Type.Size, m)
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(b, "                if (in.peek() == '\"') { v.%s = in.readString(%d); }\n", m, f.Type.Size)
		b.WriteString("                else { in.skipValue(); in.rep.kindMismatch++; }\n")
	case f.Type.Kind == ir.TBool:
		b.WriteString("                if (in.peek() == 't') { in.lit(\"true\"); v." + m + " = true; }\n")
		b.WriteString("                else if (in.peek() == 'f') { in.lit(\"false\"); v." + m + " = false; }\n")
		b.WriteString("                else { in.skipValue(); in.rep.kindMismatch++; }\n")
	case f.Type.Kind == ir.TFloat32:
		fmt.Fprintf(b, "                if (in.peek() == '-' || (in.peek() >= '0' && in.peek() <= '9')) { float placed = in.number32(); if (!in.rejected) { v.%s = placed; } }\n", m)
		b.WriteString("                else { in.skipValue(); in.rep.kindMismatch++; }\n")
	case f.Type.Kind == ir.TFloat64:
		fmt.Fprintf(b, "                if (in.peek() == '-' || (in.peek() >= '0' && in.peek() <= '9')) { double placed = in.number(); if (!in.rejected) { v.%s = placed; } }\n", m)
		b.WriteString("                else { in.skipValue(); in.rep.kindMismatch++; }\n")
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits:
		min, max := jsonIntRange(f)
		signed := f.Type.Signed && f.Type.Kind == ir.TInt
		if f.Type.Width <= 32 {
			fmt.Fprintf(b, "                if (in.peek() == '-' || (in.peek() >= '0' && in.peek() <= '9')) { long placed = in.integer(%t, %s, %s); if (!in.rejected) { v.%s = (int) placed; } }\n", signed, min, max, m)
		} else {
			fmt.Fprintf(b, "                if (in.peek() == '-' || (in.peek() >= '0' && in.peek() <= '9')) { long placed = in.integer(%t, %s, %s); if (!in.rejected) { v.%s = placed; } }\n", signed, min, max, m)
		}
		b.WriteString("                else { in.skipValue(); in.rep.kindMismatch++; }\n")
	case f.Type.Kind == ir.TNamed:
		if enum, ok := f.Type.Ref.(*ir.Enum); ok {
			b.WriteString("                if (in.peek() == '\"') {\n")
			b.WriteString("                    String s = in.scanString();\n")
			fmt.Fprintf(b, "                    if(\"None\".equals(s)){v.%s=0;}else ", m)
			for i := range enum.Variants {
				fmt.Fprintf(b, "                    if (%q.equals(s)) { v.%s = %d; }\n", enum.Variants[i], m, i+1)
				b.WriteString("                    else ")
			}
			fmt.Fprintf(b, "{ v.%s = 0; in.rep.unknown++; }\n", m)
			b.WriteString("                } else { in.skipValue(); in.rep.kindMismatch++; }\n")
		} else if _, ok := f.Type.Ref.(*ir.Flags); ok {
			fmt.Fprintf(b, "                if (in.peek() == '-' || (in.peek() >= '0' && in.peek() <= '9')) { v.%s = in.integer(true, Long.MIN_VALUE, Long.MAX_VALUE); }\n", m)
			b.WriteString("                else { in.skipValue(); in.rep.kindMismatch++; }\n")
		}
	}
}

func writeFieldRead(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field, retain bool, ordinal int, overlongNone bool) {
	if variableWide(f) && f.Array == ir.ArrayNone && !f.Type.Optional {
		writeVariableWideRead(b, f)
		return
	}
	if variableWide(f) && f.Array != ir.ArrayNone && f.KeyEnum == "" && !f.Type.Optional {
		writeVariableWideArrayRead(b, f)
		return
	}
	if variableUnion(f) != nil && !f.Type.Optional {
		writeUnionFieldRead(b, u, st, f, retain, ordinal)
		return
	}
	if f.KeyEnum != "" && !f.Type.Optional {
		writeKeyedFieldRead(b, u, st, f, retain, ordinal)
		return
	}
	if f.Type.Optional {
		m := varMember(f)
		one := *f
		one.Type.Optional = false
		var condition strings.Builder
		fmt.Fprintf(&condition, "kind==%d", variableWireKind(f))
		for kind := 1; kind <= 33; kind++ {
			if ir.TableKindWidens(kind, variableWireKind(f)) {
				fmt.Fprintf(&condition, "||kind==%d", kind)
			}
		}
		fmt.Fprintf(b, "                boolean selected=(%s);\n", condition.String())
		if f.Array != ir.ArrayNone {
			// A FIELD RODE, so the optional is present, unless its element kind
			// is another array's (the engine's arrayBody, §2.3, §4): a body too
			// short for its header is inert but present, a damaged count is
			// present, and an element kind that WIDENS to the declared one is
			// the field's own.
			fmt.Fprintf(b, "                if(selected){TableVariable.Reader probe=new TableVariable.Reader(r.buf,r.ids,new TableVariable.Report());probe.off=r.off;probe.limit=r.limit;TableVariable.Reader ar=probe.body();selected=ar!=null;if(ar!=null&&ar.has(2)){int pk=ar.get8();%s;selected=ar.fail||(%s);}}\n", countRead(f, "ar"), variableCompatibleKinds(variableElemWireKind(f), "pk"))
		}
		writeFieldRead(b, u, st, &one, retain, ordinal, overlongNone)
		fmt.Fprintf(b, "                if(selected&&!r.fail)v.%sPresent=true;\n", m)
		return
	}
	if f.IsMap() {
		writeMapFieldRead(b, u, st, f, retain, ordinal)
		return
	}
	m := varMember(f)
	switch {
	case f.Type.Kind == ir.TWString:
		fmt.Fprintf(b, "                if (kind == 33) { TableVariable.Reader text = r.body(); if (text != null) { if (((text.limit - text.off) & 1) != 0) { report.malformed = true; v.%s = new char[0]; } else { char[] units = new char[(text.limit - text.off) / 2]; for (int i = 0; i < units.length; i++) units[i] = (char) (text.get8() | text.get8() << 8); if (!TableVariable.wideValid(units)) { report.malformed = true; v.%s = new char[0]; } else { v.%s = TableVariable.wideClamp(units, %d, report); } } } } else { r.skip(kind); report.kindMismatch++; }\n", m, m, m, f.Type.Size)
	case isPointerArray(f):
		fmt.Fprintf(b, "                if (kind == 14) {\n")
		fmt.Fprintf(b, "                    TableVariable.Reader ar = r.body();\n")
		fmt.Fprintf(b, "                    if (ar != null && ar.has(2)) {\n")
		fmt.Fprintf(b, "                        int ek = ar.get8();\n")
		// the count is read with the kind byte, before either is judged:
		// a damaged count is framing damage whatever the kind (arrayBody, §4)
		fmt.Fprintf(b, "                        long n = %s;\n", countRead(f, "ar"))
		fmt.Fprintf(b, "                        if (!ar.fail && ek != 17) { report.kindMismatch++; } else {\n")
		// EVERY DECODED POINTER SLOT KEEPS ITS ROW (docs/SPEC-TABLES.md §2.1,
		// §3.1, §3.3): a bad node table makes every index resolve to null, but
		// the array still decoded its slots, so the value list stays as long
		// as the index list and a save writes one null per slot, as the fixed
		// struct-array read below already does.
		if f.ArrayBound > 0 {
			fmt.Fprintf(b, "                            if (ar.fail) { /* malformed count keeps the prefix (§3, §4) */ } else {\n")
			fmt.Fprintf(b, "                                if (n < 0 || n > %d) { n = %d; report.clamped++; }\n", f.ArrayBound, f.ArrayBound)
			if f.Array != ir.ArrayFixed {
				fmt.Fprintf(b, "                                v.%s.clear();\n", m)
				fmt.Fprintf(b, "                                v.%sIdx.clear();\n", m)
			}
			fmt.Fprintf(b, "                                for (long i = 0; i < n; i++) {\n")
			fmt.Fprintf(b, "                                    long idx = ar.leb();\n")
			fmt.Fprintf(b, "                                    if (ar.fail) break;\n")
			for _, member := range []string{m, m + "Idx"} {
				if f.Array == ir.ArrayFixed {
					fmt.Fprintf(b, "                                    TableVariable.put(v.%s, (int) i, %s);\n", member, map[bool]string{true: "null", false: "idx"}[member == m])
				} else {
					fmt.Fprintf(b, "                                    v.%s.add(%s);\n", member, map[bool]string{true: "null", false: "idx"}[member == m])
				}
			}
			fmt.Fprintf(b, "                                }\n")
			fmt.Fprintf(b, "                            }\n")
		} else {
			fmt.Fprintf(b, "                            if (ar.fail) { /* malformed count keeps the prefix (§3, §4) */ } else if (n >= 0 && n <= 0x7FFFFFFFL && n <= (long) (ar.limit - ar.off)) {\n")
			fmt.Fprintf(b, "                                v.%s.clear();\n", m)
			fmt.Fprintf(b, "                                v.%sIdx.clear();\n", m)
			fmt.Fprintf(b, "                                for (long i = 0; i < n; i++) {\n")
			fmt.Fprintf(b, "                                    long idx = ar.leb();\n")
			fmt.Fprintf(b, "                                    if (ar.fail) break;\n")
			fmt.Fprintf(b, "                                    v.%s.add(null);\n", m)
			fmt.Fprintf(b, "                                    v.%sIdx.add(idx);\n", m)
			fmt.Fprintf(b, "                                }\n")
			fmt.Fprintf(b, "                            } else { report.refused = true; if (n < 0 || n > 0x7FFFFFFFL) { report.reason = \"count_refusal\"; } }\n")
		}
		fmt.Fprintf(b, "                        }\n")
		fmt.Fprintf(b, "                    }\n")
		fmt.Fprintf(b, "                } else { r.skip(kind); report.kindMismatch++; }\n")
	case isStructArray(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "                if (kind == 14) {\n")
		fmt.Fprintf(b, "                    TableVariable.Reader ar = r.body();\n")
		fmt.Fprintf(b, "                    if (ar != null && ar.has(2)) {\n")
		fmt.Fprintf(b, "                        int ek = ar.get8();\n")
		// the count is read with the kind byte, before either is judged:
		// a damaged count is framing damage whatever the kind (arrayBody, §4)
		fmt.Fprintf(b, "                        long n = %s;\n", countRead(f, "ar"))
		fmt.Fprintf(b, "                        if (!ar.fail && ek != 13) { report.kindMismatch++; } else {\n")
		if f.ArrayBound > 0 {
			fmt.Fprintf(b, "                            if (ar.fail) { /* malformed count keeps the prefix (§3, §4) */ } else {\n")
		} else {
			fmt.Fprintf(b, "                            if (ar.fail) { /* malformed count keeps the prefix (§3, §4) */ } else if (n >= 0 && n <= 0x7FFFFFFFL && n <= (long) (ar.limit - ar.off) / 2) {\n")
		}
		if retain {
			fmt.Fprintf(b, "                                retain.discardField(path, depth, %d);\n", ordinal)
		}
		if f.ArrayBound > 0 {
			fmt.Fprintf(b, "                                if (n < 0 || n > %d) { n = %d; report.clamped++; }\n", f.ArrayBound, f.ArrayBound)
		}
		if f.Array != ir.ArrayFixed {
			fmt.Fprintf(b, "                                v.%s.clear();\n", m)
		}
		fmt.Fprintf(b, "                                for (long i = 0; i < n; i++) {\n")
		fmt.Fprintf(b, "                                    TableVariable.Reader er = ar.body();\n")
		fmt.Fprintf(b, "                                    if (er == null) break;\n")
		fmt.Fprintf(b, "                                    %sVariable.Value elem = new %sVariable.Value();\n", target.Name, target.Name)
		if retain {
			fmt.Fprintf(b, "                                    path[2 * depth] = %d; path[2 * depth + 1] = (int) i;\n", ordinal)
			fmt.Fprintf(b, "                                    %sRetain.readFieldsRetain(er, elem, nt, retain, report, path, depth + 1);\n", target.Name)
		} else {
			fmt.Fprintf(b, "                                    %sVariable.readFields(er, elem, nt, report);\n", target.Name)
		}
		fmt.Fprintf(b, "                                    if (er.off != er.limit) { report.malformed = true; elem = new %sVariable.Value(); } // §4: a body's unclaimed bytes reset this element.\n", target.Name)
		if f.Array == ir.ArrayFixed {
			fmt.Fprintf(b, "                                    TableVariable.put(v.%s, (int) i, elem);\n", m)
		} else {
			fmt.Fprintf(b, "                                    v.%s.add(elem);\n", m)
		}
		fmt.Fprintf(b, "                                }\n")
		if f.ArrayBound > 0 {
			fmt.Fprintf(b, "                            }\n")
		} else {
			fmt.Fprintf(b, "                            } else { report.refused = true; if (n < 0 || n > 0x7FFFFFFFL) { report.reason = \"count_refusal\"; } }\n")
		}
		fmt.Fprintf(b, "                        }\n")
		fmt.Fprintf(b, "                    }\n")
		fmt.Fprintf(b, "                } else { r.skip(kind); report.kindMismatch++; }\n")
	case isScalarArray(f):
		writeArrayFieldRead(b, u, st, f)
	case f.Type.Blob() || (f.Type.Pointer && isStruct(f)):
		fmt.Fprintf(b, "                if (kind == 17) { v.%sIdx = r.leb(); }\n", m)
		fmt.Fprintf(b, "                else { r.skip(kind); report.kindMismatch++; }\n")
	case isStruct(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "                if (kind == 13) {\n")
		fmt.Fprintf(b, "                    TableVariable.Reader br = r.body();\n")
		fmt.Fprintf(b, "                    if (br != null) {\n")
		fmt.Fprintf(b, "                        v.%s = new %sVariable.Value();\n", m, target.Name)
		if retain {
			fmt.Fprintf(b, "                        retain.discardField(path, depth, %d); path[2 * depth] = %d; path[2 * depth + 1] = 0;\n", ordinal, ordinal)
			fmt.Fprintf(b, "                        %sRetain.readFieldsRetain(br, v.%s, nt, retain, report, path, depth + 1);\n", target.Name, m)
		} else {
			fmt.Fprintf(b, "                        %sVariable.readFields(br, v.%s, nt, report);\n", target.Name, m)
		}
		if overlongNone {
			// AN ARM WHOSE TABLE BODY'S L RUNS PAST ITS TERMINATOR reads None
			// exactly as the oracle's union read does (docs/SPEC-TABLES.md §3):
			// the payload stops at the terminator and the arm is not selected.
			fmt.Fprintf(b, "                        if (br.off != br.limit) { report.malformed = true; return new Value(); }\n")
		} else {
			fmt.Fprintf(b, "                        if (br.off != br.limit) { report.malformed = true; v.%s = new %sVariable.Value(); } // §4: reset a nested body that did not fill its length.\n", m, target.Name)
		}
		fmt.Fprintf(b, "                    }\n")
		fmt.Fprintf(b, "                } else { r.skip(kind); report.kindMismatch++; }\n")
	// bytes(N) rides the scalar-u8 array shape (§3.2), with a bounded copy.
	// The body reads its element kind AND its count before it compares the kind
	// (docs/SPEC-TABLES.md §3.3, §4): a count the body cannot carry is framing
	// damage and counts malformed, never a kind mismatch, exactly as the
	// oracle's arrayBody decides it.
	// A count the body cannot cover keeps the decoded prefix and flags malformed
	// (docs/SPEC-TABLES.md §4, "Framing damage"): the field body's L says where
	// the next field begins, so a short array loses only its missing tail.
	case f.Type.Kind == ir.TBytes:
		fmt.Fprintf(b, "                if(kind==14){long L=r.leb();if(r.fail||L<0||L>r.limit-r.off){report.malformed=true;r.fail=true;}else if(L<2){r.off+=(int)L;}else {int end=r.off+(int)L;int ek=r.get8();long n=r.leb();if(r.fail){r.fail=false;report.malformed=true;}else if(ek!=6){report.kindMismatch++;}else {long keep=n;if(Long.compareUnsigned(keep,%dL)>0){keep=%dL;report.clamped++;}int avail=end-r.off;if(avail<0)avail=0;int take=(int)keep<avail?(int)keep:avail;v.%s=java.util.Arrays.copyOfRange(r.buf,r.off,r.off+take);if(take<(int)keep)report.malformed=true;}r.off=end;}}else {r.skip(kind);report.kindMismatch++;}\n", f.Type.Size, f.Type.Size, m)
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(b, "                if (kind == 12) { long L = r.leb(); int n = (int) L; if (!r.fail && r.has(n)) { if (!TableVariable.textValid(r.buf, r.off, n)) { report.malformed = true; v.%s = %s; } else { int keep = n; if (keep > %d) { keep = %d; while (keep > 0 && (r.buf[r.off + keep] & 0xc0) == 0x80) { keep--; } report.clamped++; } v.%s = new String(r.buf, r.off, keep, StandardCharsets.UTF_8); } r.off += n; } else if (!r.fail) { report.malformed = true; r.fail = true; } }\n", m, variableDefault(f), f.Type.Size, f.Type.Size, m)
		fmt.Fprintf(b, "                else { r.skip(kind); report.kindMismatch++; }\n")
	case f.Type.Kind == ir.TBool:
		fmt.Fprintf(b, "                if (kind == 1) { if (r.has(1)) { int bv = r.get8(); if (bv > 1) { report.malformed = true; } v.%s = bv != 0; } else { report.malformed = true; r.fail = true; } }\n", m)
		fmt.Fprintf(b, "                else { r.skip(kind); report.kindMismatch++; }\n")
	// A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS, before the bits become
	// a value and before the field is written (docs/SPEC-TABLES.md §3.3; Glenn,
	// 2026-10-27, "same rule as packets"): the read names the field and the
	// load, tolerant included, refuses as non_finite_float. The float rung
	// (kind 10 into a float64) is the ordinary conversion, exact for every
	// finite float32.
	case f.Type.Kind == ir.TFloat32:
		fmt.Fprintf(b, "                if (kind == 10) { if (r.has(4)) { int fbits = r.get32(); if ((fbits & 0x7f800000) == 0x7f800000) { TableVariable.nonFiniteFloat(report, %q); } else { v.%s = Float.intBitsToFloat(fbits); } } else { report.malformed = true; r.fail = true; } }\n", floatOwnerName(st, f), m)
		fmt.Fprintf(b, "                else { r.skip(kind); report.kindMismatch++; }\n")
	case f.Type.Kind == ir.TFloat64:
		fmt.Fprintf(b, "                if (kind == 11) { if (r.has(8)) { long fbits = r.get64(); if ((fbits & 0x7ff0000000000000L) == 0x7ff0000000000000L) { TableVariable.nonFiniteFloat(report, %q); } else { v.%s = Double.longBitsToDouble(fbits); } } else { report.malformed = true; r.fail = true; } }\n", floatOwnerName(st, f), m)
		fmt.Fprintf(b, "                else if (kind == 10) { if (r.has(4)) { int fbits = r.get32(); if ((fbits & 0x7f800000) == 0x7f800000) { TableVariable.nonFiniteFloat(report, %q); } else { v.%s = (double) Float.intBitsToFloat(fbits); } report.widened++; } else { report.malformed = true; r.fail = true; } }\n", floatOwnerName(st, f), m)
		fmt.Fprintf(b, "                else { r.skip(kind); report.kindMismatch++; }\n")
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits:
		writeIntFieldRead(b, f, m)
	case f.Type.Kind == ir.TNamed:
		if _, ok := f.Type.Ref.(*ir.Enum); ok {
			fmt.Fprintf(b, "                if (kind == 30) { long vref = r.leb(); if (!r.fail) { if (vref == 0) { v.%s = 0; } else { long vid = r.resolve(vref); if (!r.fail) { v.%s = %sEnum(vid, report); } } } }\n", m, m, m)
			b.WriteString("                else { r.skip(kind); report.kindMismatch++; }\n")
		} else if _, ok := f.Type.Ref.(*ir.Flags); ok {
			// a `flags` field rides the u64 rung (kind 9) and widens from the
			// three unsigned rungs below it (§4), whatever its storage width.
			fmt.Fprintf(b, "                if (kind == 9 || kind == 8 || kind == 7 || kind == 6) {\n")
			fmt.Fprintf(b, "                    if (kind == 9) { if (r.has(8)) { v.%s = r.get64(); } else { report.malformed = true; r.fail = true; } }\n", m)
			fmt.Fprintf(b, "                    else if (kind == 8) { if (r.has(4)) { v.%s = ((long) r.get32()) & 0xffffffffL; report.widened++; } else { report.malformed = true; r.fail = true; } }\n", m)
			fmt.Fprintf(b, "                    else if (kind == 7) { if (r.has(2)) { v.%s = r.get16(); report.widened++; } else { report.malformed = true; r.fail = true; } }\n", m)
			fmt.Fprintf(b, "                    else { if (r.has(1)) { v.%s = r.get8(); report.widened++; } else { report.malformed = true; r.fail = true; } }\n", m)
			b.WriteString("                } else { r.skip(kind); report.kindMismatch++; }\n")
		}
	}
}

func writeArrayFieldRead(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	m := varMember(f)
	ek := variableElemWireKind(f)
	cond := variableCompatibleKinds(ek, "ek")
	countCheck := "else if (n < 0 || n > 0x7FFFFFFFL) { report.refused = true; report.reason = \"count_refusal\"; } else {"
	if f.ArrayBound > 0 {
		countCheck = fmt.Sprintf("else { if (n < 0 || n > %d) { n = %d; report.clamped++; }", f.ArrayBound, f.ArrayBound)
	}
	// The header is the element kind AND the count, read before either is
	// judged (the engine's arrayBody, §3, §4): a damaged count is framing
	// damage whatever the kind byte says, and only a whole header of another
	// element kind is a kind mismatch.
	fmt.Fprintf(b, "                if (kind == 14) { TableVariable.Reader ar = r.body(); if (ar != null) { if (ar.has(2)) { int ek = ar.get8(); long n = %s; if (ar.fail) { } else if (!(%s)) { report.kindMismatch++; } %s if (ek != %d) { report.widened++; } ", countRead(f, "ar"), cond, countCheck, ek)
	if f.Array != ir.ArrayFixed {
		fmt.Fprintf(b, "v.%s.clear(); ", m)
	}
	writeArrayElemRead(b, f, m, floatOwnerName(st, f))
	fmt.Fprintf(b, " } } } }\n")
	fmt.Fprintf(b, "                else { r.skip(kind); report.kindMismatch++; }\n")
}

// floatOwnerName names a float field for the non_finite_float refusal
// (docs/SPEC-TABLES.md §3.3) as <Owner>.<field>, the owner being the table,
// type or union that declares it — the name the Go reference records.
func floatOwnerName(st *ir.Struct, f *ir.Field) string {
	if st == nil {
		return f.Name
	}
	return st.DeclaredName() + "." + f.Name
}

func writeArrayElemRead(b *strings.Builder, f *ir.Field, m, owner string) {
	refusePrefix := func(widthExpr string) string {
		if f.Array == ir.ArrayList {
			return fmt.Sprintf("if (n > (long) (ar.limit - ar.off) / (%s)) { report.refused = true; } else ", widthExpr)
		}
		return ""
	}
	// A FIXED array's repeated occurrence OVERWRITES the slots it reaches and
	// leaves every slot it does not reach as the earlier occurrence left it
	// (docs/SPEC-TABLES.md §3.3): the oracle's arrayBody writes slot i in place
	// and stops at the first element the damaged body cannot carry. A counted
	// array still clears: its live count is the value, so the prefix the body
	// covers is the whole of it (§2.9, §4).
	place := func(value string) string {
		if f.Array == ir.ArrayFixed {
			return fmt.Sprintf("TableVariable.put(v.%s, (int) i, %s)", m, value)
		}
		return fmt.Sprintf("v.%s.add(%s)", m, value)
	}
	switch f.Type.Kind {
	case ir.TBool:
		fmt.Fprintf(b, "%s{ for (long i = 0; i < n; i++) { if (ar.has(1)) { %s; } else { report.malformed = true; ar.fail = true; break; } } }", refusePrefix("1"), place("TableVariable.boolByte(ar.get8(), report)"))
	case ir.TInt, ir.TBits:
		lo, hi := variableRawBounds(f)
		clamp := ""
		if f.HasIntRange {
			clamp = fmt.Sprintf("raw = TableVariable.clampInteger(raw, %s, %s, report); ", lo, hi)
		}
		fmt.Fprintf(b, "%s{ for (long i = 0; i < n; i++) { java.math.BigInteger raw = TableVariable.readInteger(ar, ek); if (raw == null) break; %s%s; } }", refusePrefix("TableVariable.integerWidth(ek)"), clamp, place(variableRawStorage(f, "raw")))
	// an element's NaN or infinity refuses the read on its raw bits, as a
	// scalar's does (docs/SPEC-TABLES.md §3.3); the walk goes on, landing the
	// element's place with zero so the count the body carries is kept
	case ir.TFloat32:
		fmt.Fprintf(b, "%s{ for (long i = 0; i < n; i++) { if (ar.has(4)) { int fbits = ar.get32(); if ((fbits & 0x7f800000) == 0x7f800000) { TableVariable.nonFiniteFloat(report, %q); fbits = 0; } %s; } else { report.malformed = true; ar.fail = true; break; } } }", refusePrefix("4"), owner, place("Float.intBitsToFloat(fbits)"))
	case ir.TFloat64:
		fmt.Fprintf(b, "%s{ for (long i = 0; i < n; i++) { if (ek == 10) { if (ar.has(4)) { int fbits = ar.get32(); if ((fbits & 0x7f800000) == 0x7f800000) { TableVariable.nonFiniteFloat(report, %q); fbits = 0; } %s; } else { report.malformed = true; ar.fail = true; break; } } else { if (ar.has(8)) { long fbits = ar.get64(); if ((fbits & 0x7ff0000000000000L) == 0x7ff0000000000000L) { TableVariable.nonFiniteFloat(report, %q); fbits = 0L; } %s; } else { report.malformed = true; ar.fail = true; break; } } } }", refusePrefix("ek == 10 ? 4 : 8"), owner, place("(double) Float.intBitsToFloat(fbits)"), owner, place("Double.longBitsToDouble(fbits)"))
	case ir.TString:
		fmt.Fprintf(b, "for (long i = 0; i < n; i++) { long L = ar.leb(); int len = (int) L; if (ar.fail) { break; } if (!ar.has(len)) { report.malformed = true; ar.fail = true; break; } if (!TableVariable.textValid(ar.buf, ar.off, len)) { report.malformed = true; %s; } else { int keep = len; if (keep > %d) { keep = %d; while (keep > 0 && (ar.buf[ar.off + keep] & 0xc0) == 0x80) { keep--; } report.clamped++; } %s; } ar.off += len; }", place(`""`), f.Type.Size, f.Type.Size, place("new String(ar.buf, ar.off, keep, StandardCharsets.UTF_8)"))
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			fmt.Fprintf(b, "for (long i = 0; i < n; i++) { long vref = ar.leb(); if (ar.fail) { break; } if (vref == 0) { %s; } else { long vid = ar.resolve(vref); if (!ar.fail) { %s; } } }", place("0"), place(fmt.Sprintf("%sEnum(vid, report)", m)))
		case *ir.Flags:
			// a WIDENED element kind (§4) is read at ITS width, not the
			// declared mask's: one u8 element is one byte on the wire
			fmt.Fprintf(b, "int ew = ek == 6 ? 1 : ek == 7 ? 2 : ek == 8 ? 4 : 8; %s{ for (long i = 0; i < n; i++) { if (ar.has(ew)) { %s; } else { report.malformed = true; ar.fail = true; break; } } }", refusePrefix("ew"), place("ar.unsigned(ew)"))
		}
	}
}

func writeIntFieldRead(b *strings.Builder, f *ir.Field, m string) {
	k := variableWireKind(f)
	width := f.Type.Width
	read, has := "r.get32()", "4"
	switch ir.TableKindWidth(k) {
	case 1:
		read, has = "r.get8()", "1"
		if f.Type.Signed {
			read = "(byte) r.get8()"
		}
	case 2:
		read, has = "r.get16()", "2"
		if f.Type.Signed {
			read = "(short) r.get16()"
		}
	case 8:
		read, has = "r.get64()", "8"
	}
	var widen []int
	if f.Type.Signed && f.Type.Kind == ir.TInt {
		widen = []int{2, 3}
		if width > 32 {
			widen = append(widen, 4)
		}
	} else {
		widen = []int{6, 7}
		if width > 32 {
			widen = append(widen, 8)
		}
	}
	allowed := widen[:0]
	for _, candidate := range widen {
		if ir.TableKindWidth(candidate) < ir.TableKindWidth(k) {
			allowed = append(allowed, candidate)
		}
	}
	widen = allowed
	var cond strings.Builder
	fmt.Fprintf(&cond, "kind == %d", k)
	for _, w := range widen {
		if w != k {
			fmt.Fprintf(&cond, " || kind == %d", w)
		}
	}
	fmt.Fprintf(b, "                if (%s) {\n", cond.String())
	fmt.Fprintf(b, "                    if (kind == %d) { if (r.has(%s)) { v.%s = %s; } else { report.malformed = true; r.fail = true; } }\n", k, has, m, read)
	for _, w := range widen {
		if w == k {
			continue
		}
		wread, whas := "", ""
		switch w {
		case 2:
			wread, whas = "(byte) r.get8()", "1"
		case 3:
			wread, whas = "(short) r.get16()", "2"
		case 4:
			wread, whas = "r.get32()", "4"
		case 6:
			wread, whas = "r.get8()", "1"
		case 7:
			wread, whas = "r.get16()", "2"
		case 8:
			wread, whas = "((long) r.get32()) & 0xffffffffL", "4"
		}
		fmt.Fprintf(b, "                    else if (kind == %d) { if (r.has(%s)) { v.%s = %s; report.widened++; } else { report.malformed = true; r.fail = true; } }\n", w, whas, m, wread)
	}
	if f.HasIntRange {
		if f.Type.Signed && f.Type.Kind == ir.TInt {
			min, max := f.IntMin.String(), f.IntMax.String()
			if width > 32 {
				min += "L"
				max += "L"
			}
			fmt.Fprintf(b, "                    if (!r.fail && (v.%s < %s || v.%s > %s)) { if (v.%s < %s) { v.%s = %s; } else { v.%s = %s; } report.clamped++; }\n", m, min, m, max, m, min, m, min, m, max)
		} else {
			if width <= 32 {
				min, max := f.IntMin.String()+"L", f.IntMax.String()+"L"
				fmt.Fprintf(b, "                    if (!r.fail && (((long) v.%s & 0xffffffffL) < %s || ((long) v.%s & 0xffffffffL) > %s)) { if (((long) v.%s & 0xffffffffL) < %s) { v.%s = (int) %s; } else { v.%s = (int) %s; } report.clamped++; }\n", m, min, m, max, m, min, m, min, m, max)
			} else {
				min := f.IntMin.String() + "L"
				if f.IntMin.Cmp(big.NewInt(math.MaxInt64)) > 0 {
					min = fmt.Sprintf("0x%xL", f.IntMin)
				}
				max := f.IntMax.String() + "L"
				if f.IntMax.Cmp(big.NewInt(math.MaxInt64)) > 0 {
					max = fmt.Sprintf("0x%xL", f.IntMax)
				}
				fmt.Fprintf(b, "                    if (!r.fail && (Long.compareUnsigned(v.%s, %s) < 0 || Long.compareUnsigned(v.%s, %s) > 0)) { if (Long.compareUnsigned(v.%s, %s) < 0) { v.%s = %s; } else { v.%s = %s; } report.clamped++; }\n", m, min, m, max, m, min, m, min, m, max)
			}
		}
	}
	b.WriteString("                } else { r.skip(kind); report.kindMismatch++; }\n")
}

func writeArrayFieldSave(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	writeArrayFieldSaveRef(b, f, "F_"+varMember(f))
}

func writeArrayFieldSaveRef(b *strings.Builder, f *ir.Field, fieldRef string) {
	m := varMember(f)
	ek := variableElemWireKind(f)
	fmt.Fprintf(b, "        if (!v.%s.isEmpty()) {\n", m)
	fmt.Fprintf(b, "            w.putLeb(w.ref(%s)); w.put8(14);\n", fieldRef)
	fmt.Fprintf(b, "            int list = w.off;\n")
	fmt.Fprintf(b, "            w.put8(%d); w.putLeb(v.%s.size());\n", ek, m)
	writeArrayElemSave(b, f, m)
	fmt.Fprintf(b, "            w.frame(list);\n")
	fmt.Fprintf(b, "        }\n")
}

func writeArrayElemSave(b *strings.Builder, f *ir.Field, m string) {
	if variableWide(f) {
		one := variableElement(f)
		fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) {\n", m)
		fmt.Fprintf(b, "                %s element = v.%s.get(i);\n", variableValueType(one), m)
		// writeVariablePayload writes at the method body's level; this one
		// sits two blocks in, inside the loop.
		var pb strings.Builder
		writeVariablePayload(&pb, nil, "", one, "element", "w", 0)
		b.WriteString(shiftLines(pb.String(), 8))
		b.WriteString("            }\n")
		return
	}
	switch f.Type.Kind {
	case ir.TBool:
		fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) { Boolean e = v.%s.get(i); w.put8(e ? 1 : 0); }\n", m, m)
	case ir.TInt:
		switch {
		case f.Type.Width <= 8:
			fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) { Integer e = v.%s.get(i); w.put8(e); }\n", m, m)
		case f.Type.Width <= 16:
			fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) { Integer e = v.%s.get(i); w.put16(e); }\n", m, m)
		case f.Type.Width <= 32:
			fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) { Integer e = v.%s.get(i); w.put32(e); }\n", m, m)
		default:
			fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) { Long e = v.%s.get(i); w.put64(e); }\n", m, m)
		}
	case ir.TBits:
		switch {
		case f.Type.Width <= 8:
			fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) { Integer e = v.%s.get(i); w.put8(e); }\n", m, m)
		case f.Type.Width <= 16:
			fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) { Integer e = v.%s.get(i); w.put16(e); }\n", m, m)
		case f.Type.Width <= 32:
			fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) { Integer e = v.%s.get(i); w.put32(e); }\n", m, m)
		default:
			fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) { Long e = v.%s.get(i); w.put64(e); }\n", m, m)
		}
	case ir.TFloat32:
		fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) { Float e = v.%s.get(i); w.putF32(e); }\n", m, m)
	case ir.TFloat64:
		fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) { Double e = v.%s.get(i); w.putF64(e); }\n", m, m)
	case ir.TString:
		fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) { String e = v.%s.get(i); w.putUtf8(e); }\n", m, m)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) { Integer e = v.%s.get(i); switch (e) {\n", m, m)
			for i := range ref.Variants {
				fmt.Fprintf(b, "                case %d: w.putLeb(w.ref(0x%016xL)); break;\n", i+1, ir.TableWireId(ref.VariantWireName(i)))
			}
			b.WriteString("                default: w.putLeb(0); break;\n            } }\n")
		case *ir.Flags:
			fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) { Long e = v.%s.get(i); w.put64(e); }\n", m, m)
		}
	}
}

func writeFieldSave(b *strings.Builder, u *ir.Unit, st *ir.Struct, f *ir.Field) {
	if variableWide(f) && f.Array == ir.ArrayNone && !f.Type.Optional {
		fmt.Fprintf(b, "        if(!(%s)){w.putLeb(w.ref(F_%s));w.put8(%d);\n", variableDefaultTest(f, "v."+varMember(f)), varMember(f), variableWireKind(f))
		writeVariablePayload(b, u, st.Name, f, "v."+varMember(f), "w", 0)
		b.WriteString("        }\n")
		return
	}
	if f.Array == ir.ArrayFixed && f.KeyEnum == "" && !f.Type.Optional {
		if isStruct(f) && !f.Type.Pointer {
			fmt.Fprintf(b, "        {w.putLeb(w.ref(F_%s));w.put8(14);\n", varMember(f))
			writeVariablePayload(b, u, st.Name, f, "v."+varMember(f), "w", 0)
			b.WriteString("        }\n")
			return
		}
		one := variableElement(f)
		expr := "v." + varMember(f) + ".get(i)"
		fmt.Fprintf(b, "        {boolean present=false;for(int i=0;i<v.%s.size();i++)if(!(%s)){present=true;break;}if(present){w.putLeb(w.ref(F_%s));w.put8(14);\n", varMember(f), variableDefaultTest(one, expr), varMember(f))
		writeVariablePayload(b, u, st.Name, f, "v."+varMember(f), "w", 0)
		b.WriteString("        }}\n")
		return
	}
	if variableUnion(f) != nil && !f.Type.Optional {
		writeUnionFieldSave(b, u, st, f)
		return
	}
	if f.KeyEnum != "" && !f.Type.Optional {
		fmt.Fprintf(b, "        writeKeyedField_%s(w,graph,v.%s);\n", varMember(f), varMember(f))
		return
	}
	if f.Type.Optional {
		fmt.Fprintf(b, "        if(v.%sPresent){w.putLeb(w.ref(F_%s));w.put8(%d);\n", varMember(f), varMember(f), variableWireKind(f))
		one := *f
		one.Type.Optional = false
		writeVariablePayload(b, u, st.Name, &one, "v."+varMember(f), "w", 0)
		b.WriteString("        }\n")
		return
	}
	m := varMember(f)
	switch {
	case f.Type.Kind == ir.TWString:
		fmt.Fprintf(b, "        if (v.%s.length != 0) { w.putLeb(w.ref(F_%s)); w.put8(33); w.putLeb(v.%s.length * 2L); for (int i = 0; i < v.%s.length; i++) { char c = v.%s[i]; w.put8(c); w.put8(c >>> 8); } }\n", m, m, m, m, m)
	case isPointerArray(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        if (!v.%s.isEmpty()) {\n", m)
		fmt.Fprintf(b, "            w.putLeb(w.ref(F_%s)); w.put8(14);\n", m)
		fmt.Fprintf(b, "            int list = w.off;\n")
		fmt.Fprintf(b, "            w.put8(17); w.putLeb(v.%s.size());\n", m)
		fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) {\n", m)
		fmt.Fprintf(b, "                %sVariable.Value e = v.%s.get(i);\n", target.Name, m)
		fmt.Fprintf(b, "                w.putLeb(e == null ? 0L : graph.index(e));\n")
		fmt.Fprintf(b, "            }\n")
		fmt.Fprintf(b, "            w.frame(list);\n")
		fmt.Fprintf(b, "        }\n")
	case isStructArray(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        if (!v.%s.isEmpty()) {\n", m)
		fmt.Fprintf(b, "            w.putLeb(w.ref(F_%s)); w.put8(14);\n", m)
		fmt.Fprintf(b, "            int list = w.off;\n")
		fmt.Fprintf(b, "            w.put8(13); w.putLeb(v.%s.size());\n", m)
		fmt.Fprintf(b, "            for (int i = 0, n = v.%s.size(); i < n; i++) {\n", m)
		fmt.Fprintf(b, "                %sVariable.Value e = v.%s.get(i);\n", target.Name, m)
		fmt.Fprintf(b, "                int body = w.off;\n")
		fmt.Fprintf(b, "                %sVariable.writeFields(w, graph, e);\n", target.Name)
		fmt.Fprintf(b, "                w.put8(0);\n")
		fmt.Fprintf(b, "                w.frame(body);\n")
		fmt.Fprintf(b, "            }\n")
		fmt.Fprintf(b, "            w.frame(list);\n")
		fmt.Fprintf(b, "        }\n")
	case isScalarArray(f):
		writeArrayFieldSave(b, u, st, f)
	case f.Type.Kind == ir.TString && f.Type.Blob():
		fmt.Fprintf(b, "        if (v.%s != null) { w.putLeb(w.ref(F_%s)); w.put8(17); w.putLeb(graph.index(v.%s)); }\n", m, m, m)
	case f.Type.Kind == ir.TBytes && f.Type.Blob():
		fmt.Fprintf(b, "        if (v.%s != null) { w.putLeb(w.ref(F_%s)); w.put8(17); w.putLeb(graph.index(v.%s)); }\n", m, m, m)
	case f.Type.Pointer && isStruct(f):
		fmt.Fprintf(b, "        if (v.%s != null) { w.putLeb(w.ref(F_%s)); w.put8(17); w.putLeb(graph.index(v.%s)); }\n", m, m, m)
	case isStruct(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        if (v.%s != null) {\n", m)
		fmt.Fprintf(b, "            int mark = w.ids.count;\n")
		fmt.Fprintf(b, "            long ref = w.ref(F_%s);\n", m)
		fmt.Fprintf(b, "            int body = w.off;\n")
		fmt.Fprintf(b, "            %sVariable.writeFields(w, graph, v.%s);\n", target.Name, m)
		fmt.Fprintf(b, "            if (w.off > body) {\n")
		fmt.Fprintf(b, "                w.put8(0);\n")
		fmt.Fprintf(b, "                w.frameField(body, ref, 13);\n")
		fmt.Fprintf(b, "            } else { w.ids.count = mark; }\n")
		fmt.Fprintf(b, "        }\n")
	case f.Type.Kind == ir.TBytes:
		fmt.Fprintf(b, "        if(v.%s!=null&&!java.util.Arrays.equals(v.%s,%s)){w.putLeb(w.ref(F_%s));w.put8(14);int list=w.off;w.put8(6);w.putLeb(v.%s.length);w.putBytes(v.%s,0,v.%s.length);w.frame(list);}\n", m, m, variableDefault(f), m, m, m, m)
	case f.Type.Kind == ir.TString:
		def := variableDefault(f)
		if def == "\"\"" {
			fmt.Fprintf(b, "        if (!v.%s.isEmpty()) { w.putLeb(w.ref(F_%s)); w.put8(12); w.putUtf8(v.%s); }\n", m, m, m)
		} else {
			fmt.Fprintf(b, "        if (!%s.equals(v.%s)) { w.putLeb(w.ref(F_%s)); w.put8(12); w.putUtf8(v.%s); }\n", def, m, m, m)
		}
	case f.Type.Kind == ir.TBool:
		def := variableDefault(f)
		fmt.Fprintf(b, "        if (v.%s != %s) { w.putLeb(w.ref(F_%s)); w.put8(1); w.put8(v.%s ? 1 : 0); }\n", m, def, m, m)
	// A FLOAT ELIDES BY ITS BITS (docs/SPEC-TABLES.md §3.3, the 10-15 rule):
	// a -0 under a +0 default rides, and the raw bits are what is written.
	case f.Type.Kind == ir.TFloat32, f.Type.Kind == ir.TFloat64:
		bits := "Float.floatToRawIntBits"
		if f.Type.Kind == ir.TFloat64 {
			bits = "Double.doubleToRawLongBits"
		}
		fmt.Fprintf(b, "        if (%s(v.%s) != %s(%s)) { w.putLeb(w.ref(F_%s)); w.put8(%d); %s }\n", bits, m, bits, variableDefault(f), m, variableWireKind(f), variableFloatPut(f, "w", "v."+m))
	case (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits) && f.Type.Width <= 32:
		k := variableWireKind(f)
		def := variableDefault(f)
		fmt.Fprintf(b, "        if (v.%s != %s) { w.putLeb(w.ref(F_%s)); w.put8(%d); w.put%d(v.%s); }\n", m, def, m, k, ir.TableKindWidth(k)*8, m)
	case (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits):
		k := variableWireKind(f)
		def := variableDefault(f)
		fmt.Fprintf(b, "        if (v.%s != %s) { w.putLeb(w.ref(F_%s)); w.put8(%d); w.put64(v.%s); }\n", m, def, m, k, m)
	case f.Type.Kind == ir.TNamed:
		if enum, ok := f.Type.Ref.(*ir.Enum); ok {
			def := variableDefault(f)
			fmt.Fprintf(b, "        if (v.%s != %s) {\n            w.putLeb(w.ref(F_%s));\n            w.put8(30);\n            switch (v.%s) {\n", m, def, m, m)
			for i := range enum.Variants {
				fmt.Fprintf(b, "                case %d: w.putLeb(w.ref(0x%016xL)); break;\n", i+1, ir.TableWireId(enum.VariantWireName(i)))
			}
			b.WriteString("                default: w.putLeb(0); break;\n            }\n        }\n")
		} else if _, ok := f.Type.Ref.(*ir.Flags); ok {
			def := variableDefault(f)
			fmt.Fprintf(b, "        if (v.%s != %s) { w.putLeb(w.ref(F_%s)); w.put8(9); w.put64(v.%s); }\n", m, def, m, m)
		}
	}
}

// Graph numbering descends only present payloads (§2.3, §3.1).
func writeVariableGraphField(b *strings.Builder, f *ir.Field) {
	if f.Type.Optional {
		fmt.Fprintf(b, "        if(v.%sPresent){\n", varMember(f))
	}
	m := varMember(f)
	if f.IsMap() {
		fmt.Fprintf(b, "        v.%s = order_%s(v.%s);\n", m, m, m)
	}
	if f.Type.Kind == ir.TWString {
		fmt.Fprintf(b, "        if (!TableVariable.wideValid(v.%s) || v.%s.length > %d) return false;\n", m, m, f.Type.Size)
	}
	if un := variableUnion(f); un != nil {
		if f.Array != ir.ArrayNone {
			fmt.Fprintf(b, "        for (int i = 0, n = v.%s.size(); i < n; i++) { %sVariable.Value arm = v.%s.get(i); if (!%sVariable.buildGraph(g, arm)) return false; }\n", m, un.Name, m, un.Name)
		} else {
			fmt.Fprintf(b, "        if(!%sVariable.buildGraph(g,v.%s))return false;\n", un.Name, m)
		}
		if f.Type.Optional {
			b.WriteString("        }\n")
		}
		return
	}
	switch {
	case f.Type.Kind == ir.TString && f.Type.Blob():
		fmt.Fprintf(b, "        if (v.%s != null) { if (!g.recordPointer(v.%s, TableVariable.STRING_ID, true)) return false; }\n", m, m)
	case f.Type.Kind == ir.TBytes && f.Type.Blob():
		fmt.Fprintf(b, "        if (v.%s != null) { if (!g.recordPointer(v.%s, TableVariable.BYTES_ID, true)) return false; }\n", m, m)
	case isPointerArray(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        for (int i = 0, n = v.%s.size(); i < n; i++) {\n", m)
		fmt.Fprintf(b, "            %sVariable.Value elem = v.%s.get(i);\n", target.Name, m)
		fmt.Fprintf(b, "            if (elem != null) {\n")
		fmt.Fprintf(b, "                boolean isNew = !g.indexed(elem);\n")
		fmt.Fprintf(b, "                if (!g.recordPointer(elem, %sVariable.TYPE_ID, false)) return false;\n", target.Name)
		fmt.Fprintf(b, "                if (isNew) {\n")
		fmt.Fprintf(b, "                    g.setOpen(elem, true);\n")
		fmt.Fprintf(b, "                    if (!%sVariable.buildGraph(g, elem)) return false;\n", target.Name)
		fmt.Fprintf(b, "                    g.setOpen(elem, false);\n")
		fmt.Fprintf(b, "                }\n")
		fmt.Fprintf(b, "            }\n")
		fmt.Fprintf(b, "        }\n")
	case f.Type.Pointer && isStruct(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        if (v.%s != null) {\n", m)
		fmt.Fprintf(b, "            boolean isNew = !g.indexed(v.%s);\n", m)
		fmt.Fprintf(b, "            if (!g.recordPointer(v.%s, %sVariable.TYPE_ID, false)) return false;\n", m, target.Name)
		fmt.Fprintf(b, "            if (isNew) {\n")
		fmt.Fprintf(b, "                g.setOpen(v.%s, true);\n", m)
		fmt.Fprintf(b, "                if (!%sVariable.buildGraph(g, v.%s)) return false;\n", target.Name, m)
		fmt.Fprintf(b, "                g.setOpen(v.%s, false);\n", m)
		fmt.Fprintf(b, "            }\n")
		fmt.Fprintf(b, "        }\n")
	case isStructArray(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        for (int i = 0, n = v.%s.size(); i < n; i++) {\n", m)
		fmt.Fprintf(b, "            %sVariable.Value elem = v.%s.get(i);\n", target.Name, m)
		fmt.Fprintf(b, "            if (elem != null) { if (!%sVariable.buildGraph(g, elem)) return false; }\n", target.Name)
		fmt.Fprintf(b, "        }\n")
	case isStruct(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        if (v.%s != null) { if (!%sVariable.buildGraph(g, v.%s)) return false; }\n", m, target.Name, m)
	}
	if f.Type.Optional {
		b.WriteString("        }\n")
	}
}

func variableJsonShape(f *ir.Field, peek string) string {
	if f.IsMap() || f.KeyEnum != "" {
		return peek + "=='{'"
	}
	if f.Array != ir.ArrayNone {
		return peek + "=='['"
	}
	switch f.Type.Kind {
	case ir.TString, ir.TWString, ir.TBytes:
		return peek + "=='\"'"
	case ir.TBool:
		return "(" + peek + "=='t'||" + peek + "=='f')"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Struct, *ir.Union:
			return peek + "=='{'"
		case *ir.Enum:
			return peek + "=='\"'"
		case *ir.Flags:
			return peek + "=='['"
		}
	}
	return "(" + peek + "=='-'||(" + peek + ">='0'&&" + peek + "<='9'))"
}

func writeVariableResolveField(b *strings.Builder, f *ir.Field) {
	m := varMember(f)
	if un := variableUnion(f); un != nil {
		if f.Array != ir.ArrayNone {
			fmt.Fprintf(b, "        for(%sVariable.Value arm:v.%s){%sVariable.resolveNodes(arm,nt,root,rootTypeId,report);}\n", un.Name, m, un.Name)
		} else {
			fmt.Fprintf(b, "        %sVariable.resolveNodes(v.%s,nt,root,rootTypeId,report);\n", un.Name, m)
		}
		return
	}
	switch {
	case f.Type.Kind == ir.TString && f.Type.Blob():
		fmt.Fprintf(b, "        if (v.%sIdx != 0) { v.%s = TableVariable.resolveString(nt.recs, v.%sIdx, report); }\n", m, m, m)
	case f.Type.Kind == ir.TBytes && f.Type.Blob():
		fmt.Fprintf(b, "        if (v.%sIdx != 0) { v.%s = TableVariable.resolveBytes(nt.recs, v.%sIdx, report); }\n", m, m, m)
	case isPointerArray(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        if (!v.%sIdx.isEmpty()) {\n", m)
		fmt.Fprintf(b, "            v.%s.clear();\n", m)
		fmt.Fprintf(b, "            for (Long idx : v.%sIdx) {\n", m)
		fmt.Fprintf(b, "                v.%s.add((%sVariable.Value) TableVariable.resolveTable(nt.recs, idx.longValue(), %sVariable.TYPE_ID, root, rootTypeId, report));\n", m, target.Name, target.Name)
		fmt.Fprintf(b, "            }\n")
		fmt.Fprintf(b, "        }\n")
	case f.Type.Pointer && isStruct(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        if (v.%sIdx != 0) { v.%s = (%sVariable.Value) TableVariable.resolveTable(nt.recs, v.%sIdx, %sVariable.TYPE_ID, root, rootTypeId, report); }\n", m, m, target.Name, m, target.Name)
	case isStructArray(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        for (%sVariable.Value elem : v.%s) { if (elem != null) %sVariable.resolveNodes(elem, nt, root, rootTypeId, report); }\n", target.Name, m, target.Name)
	case isStruct(f):
		target := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(b, "        if (v.%s != null) { %sVariable.resolveNodes(v.%s, nt, root, rootTypeId, report); }\n", m, target.Name, m)
	}
}

func variableFlagNames(flags *ir.Flags) string {
	var names []string
	for _, name := range flags.Variants {
		names = append(names, fmt.Sprintf("%q", name))
	}
	return "new String[]{" + strings.Join(names, ",") + "}"
}

// shiftLines moves every non-empty line of s by n columns (n < 0 moves it
// left, never past its own indentation): a fragment written at one block
// level, placed at another.
func shiftLines(s string, n int) string {
	lines := strings.SplitAfter(s, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if n >= 0 {
			lines[i] = strings.Repeat(" ", n) + l
			continue
		}
		cut := 0
		for cut < -n && cut < len(l) && l[cut] == ' ' {
			cut++
		}
		lines[i] = l[cut:]
	}
	return strings.Join(lines, "")
}
