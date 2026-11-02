// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3), form byte 2: a batch of
// bitpacked bodies under one announced vocabulary. This file carries both the
// ANNOUNCEMENT surface (`announce`, `announceMeasure` and `announceRead` on
// `TableVocabulary`) and the BATCH surface (`TableMessage` and `<Type>Message`
// with `measureMessages`, `saveMessages` and `loadMessages`).
//
// THE FORM IS A FIXED TABLE'S (§3.3), so this surface is emitted for a unit
// with a `fixed table` root and for no other: a message has to step through a
// shape the declaration fixes.
package javatable

import (
	"fmt"
	"sort"
	"strings"

	javapkt "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// messageAnnouncementClass is the public type the announcement surface lives in.
const messageAnnouncementClass = "TableVocabulary"

// messageRuntimeClass is the public type the message-form batch bit stream and
// vocabulary walk live in.
const messageRuntimeClass = "TableMessage"

// messageClass is the public class one type's message-form codec lands in.
func messageClass(name string) string { return ir.GoExportName(name) + "Message" }

// messageNames is every class name this form claims for a unit, checked against
// schema file basenames by checkNames.
func messageNames(u *ir.Unit) []string {
	roots := fixedRoots(u)
	if len(roots) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var order []*ir.Struct
	for _, st := range roots {
		fixedCollectTypes(st, seen, &order)
	}
	out := []string{messageAnnouncementClass, messageRuntimeClass}
	for _, st := range order {
		out = append(out, messageClass(st.Name))
	}
	sort.Strings(out)
	return out
}

// generateMessageFiles emits the announcement and batch surfaces for a unit
// with a fixed root. Without one there is no message form to read (§3.3) and
// the files are not written.
func generateMessageFiles(u *ir.Unit) (map[string][]byte, error) {
	roots := fixedRoots(u)
	if len(roots) == 0 {
		return map[string][]byte{}, nil
	}
	seen := map[string]bool{}
	var order []*ir.Struct
	for _, st := range roots {
		fixedCollectTypes(st, seen, &order)
	}
	isRoot := map[string]bool{}
	for _, st := range roots {
		isRoot[st.Name] = true
	}
	vocab := ir.TableVocabulary(u)
	refBits := ir.TableMessageRefBits(len(vocab))

	annBody := strings.Replace(tableMessageSurface, "%%ANNOUNCEMENT%%", javaByteArrayLiteral(ir.TableAnnouncement(u)), 1)
	out := map[string][]byte{
		messageAnnouncementClass + ".java": javaFile(u, "the MESSAGE FORM's announcement surface (docs/SPEC-TABLES.md §3.3), form byte 2", annBody),
		messageRuntimeClass + ".java":      javaFile(u, "the MESSAGE FORM's batch bit stream and vocabulary walk (docs/SPEC-TABLES.md §3.3), form byte 2", tableMessageBatchSurface),
	}
	for _, st := range order {
		g := &messageGen{
			unit:    u,
			vocab:   vocab,
			refBits: refBits,
		}
		g.emitType(st, isRoot[st.Name])
		out[messageClass(st.Name)+".java"] = javaFile(u,
			fmt.Sprintf("%s under the message form (docs/SPEC-TABLES.md §3.3)", st.Name), g.hoistLongCalls(g.b.String()))
	}
	return out, nil
}

// javaByteArrayLiteral spells a byte[] initializer. Java bytes are SIGNED, so
// a value above 0x7f needs the cast the Go literal does not.
func javaByteArrayLiteral(b []byte) string {
	var s strings.Builder
	s.WriteString("new byte[] {")
	for i, v := range b {
		if i > 0 {
			s.WriteString(", ")
		}
		fmt.Fprintf(&s, "(byte) 0x%02x", v)
	}
	s.WriteString("}")
	return s.String()
}

// tableMessageSurface is the generated TableVocabulary.java body. It is one
// template: the announcement bytes are the only per-unit fact, because the
// whole announcement is a compile-time constant of the unit (§3.3) and the
// reader is shared byte-for-byte across units.
const tableMessageSurface = `// The announcement surface of the MESSAGE FORM (docs/SPEC-TABLES.md §3.3),
// form byte 2. ` + "`announce`" + ` and ` + "`announceMeasure`" + ` are the unit's
// compile-time constant and its length; ` + "`announceRead`" + ` parses an endpoint's
// announcement into the caller's resolved vocabulary.
public final class TableVocabulary {
    public TableVocabulary() {}

    // THE WHOLE ANNOUNCEMENT IS A COMPILE-TIME CONSTANT OF THE UNIT, every byte
    // of it settled by the compiler, so a backend emits ` + "`announce`" + ` and
    // ` + "`announceMeasure`" + ` as a constant byte array and its length (§3.3).
    static final byte[] tableAnnouncement = %%ANNOUNCEMENT%%;

    public static int announceMeasure() {
        return tableAnnouncement.length;
    }

    // Copy the announcement into the caller's buffer and answer its length, or
    // -1 when the buffer is too small (the C++ reference's ` + "`" + `-1` + "`" + `, §3.3).
    public static int announce(byte[] buffer) {
        if (buffer == null || buffer.length < tableAnnouncement.length) {
            return -1;
        }
        System.arraycopy(tableAnnouncement, 0, buffer, 0, tableAnnouncement.length);
        return tableAnnouncement.length;
    }

    // ---- the resolved vocabulary: the caller's storage, declared by capacity ----

    // One resolved entry (docs/SPEC-TABLES.md §3.3, "A RESOLVED ENTRY IS WHAT A
    // DECODE READS AND NOTHING MORE"): the id, the kind, and the shape.
    public static final class Entry {
        public long id;
        public int kind;
        public Shape shape;
        public Shape element;
    }

    // The width and range facts a reader needs to skip a field exactly and to
    // decode one whose declaration has moved (docs/SPEC-TABLES.md §3.3).
    public static final class Shape {
        public int kind;
        public int packing;
        public long bits;
        public long baseLo, baseHi;
        public long min, max;
        public float qMin, qMax, qRes, qDelta;
        public long steps;
    }

    // The read report across announcement and message-batch operations (§3.3, §4).
    public static final class Report {
        public boolean malformed;
        public boolean refused;
        public String reason;
        public int unknown;
        public int kindMismatch;
        public int widened;
        public int clamped;

        public void zero() {
            malformed = false;
            refused = false;
            reason = null;
            unknown = 0;
            kindMismatch = 0;
            widened = 0;
            clamped = 0;
        }
    }

    // The caller's storage, declared by capacity and filled once. The
    // announcement is delivered once and holds for the connection's life, so
    // the resolved vocabulary does too (§3.3).
    public Entry[] entries;
    public int count;
    public int refBits;
    public long buildVersion;
    public boolean announced;
    public boolean refused;
    public int maxEntries = 4096;
    public int maxBytes = 65536;

    public void reset(int capacity) {
        this.entries = new Entry[capacity];
        this.count = 0;
        this.refBits = 0;
        this.buildVersion = 0;
        this.announced = false;
        this.refused = false;
        this.maxEntries = capacity;
        this.maxBytes = 65536;
    }

    // NO RE-ANNOUNCEMENT, EVER, and REFUSAL IS TERMINAL (§3.3): a first
    // announcement sets the vocabulary or is refused, and every announcement
    // after it — refused or not — is ` + "`" + `second_announcement` + "`" + `.
    public static boolean announceRead(TableVocabulary v, byte[] data, Report report) {
        if (report == null) {
            report = new Report();
        }
        if (v.announced || v.refused) {
            report.refused = true;
            report.reason = "second_announcement";
            return false;
        }
        boolean ok = announceReadInner(v, data, report);
        if (!ok) {
            v.refused = true;
        }
        return ok;
    }

    // THE ANNOUNCEMENT IS AN ORDINARY FORM-1 FILE (§3.3), so it is read by the
    // same three parts a file is: the form byte, the root body of fields, and
    // the trailer of ids. The two strict checks are the two facts an
    // announcement's body must carry, and a failed one is MALFORMED, not a
    // refusal (docs/SPEC-TABLES.md §3.3).
    private static boolean announceReadInner(TableVocabulary v, byte[] data, Report report) {
        if (data == null || data.length == 0) {
            report.malformed = true;
            return false;
        }
        if ((data[0] & 0xff) != 1) {
            report.refused = true;
            report.reason = (data[0] & 0xff) == 2 ? "message_form_as_file" : "newer_form";
            return false;
        }
        if (data.length < 9) {
            report.malformed = true;
            return false;
        }
        long entryCount = readU64(data, data.length - 8);
        if (entryCount < 0 || entryCount > (data.length - 9) / 8) {
            report.malformed = true;
            return false;
        }
        int idsStart = data.length - 8 - (int) entryCount * 8;
        Reader r = new Reader(data, 1);
        int versionCount = 0;
        int vocabCount = 0;
        byte[] vocab = null;
        while (true) {
            long ref = r.leb();
            if (r.err) {
                report.malformed = true;
                return false;
            }
            if (ref == 0) {
                break;
            }
            if (Long.compareUnsigned(ref, entryCount) > 0) {
                report.malformed = true;
                return false;
            }
            long id = readU64(data, idsStart + (int) (ref - 1) * 8);
            int kind = r.u8();
            if (r.err) {
                report.malformed = true;
                return false;
            }
            if (id == -2L) { // the reserved build-version id, 0xFFFFFFFFFFFFFFFE
                if (kind != 9 || !r.has(8)) {
                    report.malformed = true;
                    return false;
                }
                v.buildVersion = r.u64();
                versionCount++;
            } else if (id == -3L) { // the reserved vocabulary id, 0xFFFFFFFFFFFFFFFD
                if (kind != 14) {
                    report.malformed = true;
                    return false;
                }
                long len = r.leb();
                if (r.err || len < 0 || len > r.buf.length - r.at) {
                    report.malformed = true;
                    return false;
                }
                int bodyStart = r.at;
                Reader sub = new Reader(r.buf, bodyStart);
                int elem = sub.u8();
                if (sub.err || elem != 6) {
                    report.malformed = true;
                    return false;
                }
                long n = sub.leb();
                if (sub.err || n < 0 || n != len - (sub.at - bodyStart)) {
                    report.malformed = true;
                    return false;
                }
                // A VOCABULARY PAST A BOUND IS REFUSED BEFORE ANYTHING IS
                // ALLOCATED: the BYTE bound is 64 KiB, checked from the field's
                // own L before an entry is touched (§3.3).
                long limit = v.maxBytes > 0 ? v.maxBytes : 65536L;
                if (Long.compareUnsigned(n, limit) > 0) {
                    report.refused = true;
                    report.reason = "vocabulary_too_large";
                    return false;
                }
                vocab = new byte[(int) n];
                System.arraycopy(r.buf, sub.at, vocab, 0, (int) n);
                r.at = bodyStart + (int) len;
                vocabCount++;
            } else {
                report.unknown++;
                if (!skip(r, kind)) {
                    report.malformed = true;
                    return false;
                }
            }
        }
        if (r.at != idsStart) {
            report.malformed = true;
            return false;
        }
        // THE TWO STRICT CHECKS: the build version present exactly once, and the
        // vocabulary present exactly once. A failed check says the bytes are not
        // an announcement rather than that this endpoint declined to announce (§3.3).
        if (versionCount != 1 || vocabCount != 1) {
            report.malformed = true;
            return false;
        }
        // Resolve the vocabulary once: every entry an id, a kind and a shape.
        // The ENTRY bound is the caller's capacity, narrowed by maxEntries when
        // the receiver sets one, and an announcement above it is refused by
        // name as vocabulary_too_large before an entry is touched (§3.3).
        int entryBound = v.entries.length;
        if (v.maxEntries > 0 && v.maxEntries < entryBound) {
            entryBound = v.maxEntries;
        }
        Reader src = new Reader(vocab, 0);
        int count = 0;
        boolean nodeSeen = false;
        while (src.at < vocab.length) {
            if (count >= entryBound) {
                report.refused = true;
                report.reason = "vocabulary_too_large";
                return false;
            }
            Entry e = new Entry();
            if (!entryRead(src, e) || e.id == -3L || e.id == -2L || (e.id == -1L && nodeSeen)) {
                report.malformed = true;
                return false;
            }
            if (e.id == -1L) {
                nodeSeen = true;
            }
            // TWO ENTRIES THAT AGREE ON ALL THREE PARTS ARE MALFORMED (§3.3).
            for (int i = 0; i < count; i++) {
                if (v.entries[i].id == e.id
                        && v.entries[i].kind == e.kind
                        && sameShape(v.entries[i], e)) {
                    report.malformed = true;
                    return false;
                }
            }
            v.entries[count++] = e;
        }
        v.count = count;
        v.refBits = Math.max(1, bitCount(count));
        v.announced = true;
        return true;
    }

    // ---- the file-form reader the announcement is (§3) ----

    static final class Reader {
        final byte[] buf;
        int at;
        boolean err;

        Reader(byte[] buf, int at) {
            this.buf = buf;
            this.at = at;
        }

        boolean has(int n) {
            return at + n <= buf.length;
        }

        int u8() {
            if (!has(1)) {
                err = true;
                return 0;
            }
            return buf[at++] & 0xff;
        }

        long u32() {
            if (!has(4)) {
                err = true;
                return 0;
            }
            long v = 0;
            for (int i = 0; i < 4; i++) {
                v |= (long) (buf[at + i] & 0xff) << (8 * i);
            }
            at += 4;
            return v;
        }

        long u64() {
            if (!has(8)) {
                err = true;
                return 0;
            }
            long v = 0;
            for (int i = 0; i < 8; i++) {
                v |= (long) (buf[at + i] & 0xff) << (8 * i);
            }
            at += 8;
            return v;
        }

        long leb() {
            long v = 0;
            for (int i = 0; i < 10; i++) {
                if (!has(1)) {
                    err = true;
                    return 0;
                }
                int b = buf[at++] & 0xff;
                if (i == 9 && (b & 0xfe) != 0) {
                    err = true;
                    return 0;
                }
                v |= (long) (b & 0x7f) << (7 * i);
                if ((b & 0x80) == 0) {
                    if (i > 0 && b == 0) {
                        err = true;
                        return 0;
                    } // a longer spelling of the same number
                    return v;
                }
            }
            err = true;
            return 0;
        }
    }

    static long readU64(byte[] buf, int at) {
        long v = 0;
        for (int i = 0; i < 8; i++) {
            v |= (long) (buf[at + i] & 0xff) << (8 * i);
        }
        return v;
    }

    static int bitCount(long n) {
        if (n <= 0) {
            return 0;
        }
        return 64 - Long.numberOfLeadingZeros(n);
    }

    static boolean knownKind(int kind) {
        return kind <= 33;
    }

    static int kindWidth(int kind) {
        switch (kind) {
            case 1:
            case 2:
            case 6:
            case 20:
            case 25:
                return 1;
            case 3:
            case 7:
            case 21:
            case 26:
                return 2;
            case 4:
            case 8:
            case 10:
            case 17:
            case 22:
            case 27:
                return 4;
            case 5:
            case 9:
            case 11:
            case 23:
            case 28:
            case 36:
                return 8;
            case 18:
            case 19:
            case 24:
            case 29:
                return 16;
            default:
                return 0;
        }
    }

    // Skip an unknown field by its announced kind (§3.3): a tolerant reader
    // steps over a field it cannot name exactly, by the width the kind states.
    static boolean skip(Reader r, int kind) {
        int w = kindWidth(kind);
        if (w > 0) {
            if (!r.has(w)) {
                return false;
            }
            r.at += w;
            return true;
        }
        switch (kind) {
            case 12:
            case 13:
            case 14:
                { // string, table, array: a length then its bytes
                    long n = r.leb();
                    if (r.err || n < 0 || n > r.buf.length - r.at) {
                        return false;
                    }
                    r.at += (int) n;
                    return true;
                }
            default:
                return false;
        }
    }

    static Shape shapeRead(Reader r, int kind) {
        Shape s = new Shape();
        s.kind = kind;
        if ((kind >= 2 && kind <= 10) || (kind >= 18 && kind <= 29)) {
            if (!r.has(1)) {
                return null;
            }
            s.packing = r.u8();
            if (s.packing == 0) {
                s.bits = (long) kindWidth(kind) * 8;
                return s;
            }
            if (s.packing == 1 && kind != 10) {
                long bits = r.leb();
                if (r.err || bits > (long) kindWidth(kind) * 8) {
                    return null;
                }
                s.bits = bits;
                if (kind >= 18) {
                    if (!r.has(16)) {
                        return null;
                    }
                    s.baseLo = r.u64();
                    s.baseHi = r.u64();
                } else {
                    long base = r.leb();
                    if (r.err) {
                        return null;
                    }
                    if (kind >= 2 && kind <= 5) {
                        base = (base >>> 1) ^ -(base & 1);
                    } // zigzag, the signed kind's base
                    s.baseLo = base;
                }
                return s;
            }
            if (s.packing == 2 && kind == 10) {
                if (!r.has(12)) {
                    return null;
                }
                s.qMin = Float.intBitsToFloat((int) r.u32());
                s.qMax = Float.intBitsToFloat((int) r.u32());
                s.qRes = Float.intBitsToFloat((int) r.u32());
                if (!(s.qMin < s.qMax) || !(s.qRes > 0)) {
                    return null;
                }
                s.qDelta = s.qMax - s.qMin;
                float values = s.qDelta / s.qRes;
                // A TRIPLE WHOSE delta OR delta/res IS NOT FINITE IN FLOAT32 is a
                // hostile width like any other (§3.3): refused, no vocabulary.
                if (!Float.isFinite(s.qDelta) || !Float.isFinite(values)) {
                    return null;
                }
                values = Math.max(1.0f, Math.min(4294967040.0f, values));
                // count = ceil(delta / res) clamped to [1, 4294967040] (§3.3), a
                // count the 32-bit unsigned width holds, then bits_required(0, count).
                long count = (long) Math.ceil((double) values);
                s.steps = count;
                s.bits = bitCount(count);
                return s;
            }
            return null; // a packing outside the closed set
        }
        if (kind == 12 || kind == 33) {
            long max = r.leb();
            if (r.err || max > 0x7fffffffL) {
                return null;
            }
            s.max = max;
            return s;
        }
        if (kind == 14 || kind == 16) {
            if (kind == 14) {
                long min = r.leb();
                if (r.err || min > 0xffffffffL) {
                    return null;
                }
                s.min = min;
            }
            long max = r.leb();
            if (r.err || max > 0xffffffffL || max < s.min) {
                return null;
            }
            s.max = max;
            return s;
        }
        return s;
    }

    static boolean entryRead(Reader r, Entry e) {
        if (!r.has(9)) {
            return false;
        }
        e.id = r.u64();
        int kind = r.u8();
        if (!knownKind(kind)) {
            return false;
        }
        e.kind = kind;
        e.shape = shapeRead(r, kind);
        if (e.shape == null) {
            return false;
        }
        if (kind == 14 || kind == 16) {
            if (!r.has(1)) {
                return false;
            }
            int elem = r.u8();
            if (!knownKind(elem) || elem == 12 || elem == 33) {
                return false;
            }
            e.element = shapeRead(r, elem);
            if (e.element == null) {
                return false;
            }
            if (elem == 14 || elem == 16) {
                if (!r.has(1)) {
                    return false;
                }
                int inner = r.u8();
                if (!knownKind(inner) || inner == 12 || inner == 33) {
                    return false;
                }
            }
        }
        return !r.err;
    }

    static boolean sameShape(Entry a, Entry b) {
        if (!sameShapeFacts(a.shape, b.shape)) {
            return false;
        }
        if (a.element == null || b.element == null) {
            return a.element == b.element;
        }
        return sameShapeFacts(a.element, b.element);
    }

    static boolean sameShapeFacts(Shape a, Shape b) {
        return a.kind == b.kind
                && a.packing == b.packing
                && a.bits == b.bits
                && a.baseLo == b.baseLo
                && a.baseHi == b.baseHi
                && a.min == b.min
                && a.max == b.max
                && a.qMin == b.qMin
                && a.qMax == b.qMax
                && a.qRes == b.qRes
                && a.steps == b.steps;
    }
}
`

// tableMessageBatchSurface is the generated TableMessage.java body: the
// low-bit-first bit writer and bit reader, the vocabulary skip walk, and the
// scalar pack/unpack and clamp helpers shared across `<Type>Message.java`.
const tableMessageBatchSurface = `
// The batch bit stream and vocabulary walk of the MESSAGE FORM
// (docs/SPEC-TABLES.md §3.3), form byte 2.
public final class TableMessage {
    private TableMessage() {}

    public static final byte form = 2;
    public static final int batchMax = 256;

    // Low-bit-first bit writer. Measuring walks the identical stream, including
    // alignment, without touching a buffer.
    public static final class Writer {
        public final byte[] buf;
        public final int base;
        public final boolean measuring;
        public long bits;
        public boolean overflow;

        public Writer(byte[] buf, int base, boolean measuring) {
            this.buf = buf;
            this.base = base;
            this.measuring = measuring;
        }

        public void put(long v, int n) {
            if (n < 0 || n > 64) { overflow = true; return; }
            if (n == 0 || overflow) { return; }
            long next = bits + (long) n;
            if (next < bits) { overflow = true; return; }
            if (!measuring) {
                long endByte = (long) base + ((next + 7L) >>> 3);
                if (buf == null || endByte > (long) buf.length) {
                    overflow = true;
                    return;
                }
                for (int i = 0; i < n; i++) {
                    long pos = bits + (long) i;
                    int byteIdx = base + (int) (pos >>> 3);
                    int bitIdx = (int) (pos & 7L);
                    if (((v >>> i) & 1L) != 0L) {
                        buf[byteIdx] |= (byte) (1 << bitIdx);
                    } else {
                        buf[byteIdx] &= (byte) ~(1 << bitIdx);
                    }
                }
            }
            bits = next;
        }

        public void align() {
            int rem = (int) (bits & 7L);
            if (rem != 0) { put(0L, 8 - rem); }
        }

        public void raw(byte[] src, int n) {
            align();
            if (n < 0) { overflow = true; return; }
            if (n == 0 || overflow) { return; }
            if (!measuring) {
                int off = base + (int) (bits >>> 3);
                if (buf == null || (long) off + (long) n > (long) buf.length) {
                    overflow = true;
                    return;
                }
                if (src != null) {
                    int copy = Math.min(n, src.length);
                    if (copy > 0) {
                        System.arraycopy(src, 0, buf, off, copy);
                    }
                    for (int i = copy; i < n; i++) {
                        buf[off + i] = 0;
                    }
                } else {
                    for (int i = 0; i < n; i++) {
                        buf[off + i] = 0;
                    }
                }
            }
            bits += (long) n << 3;
        }

        public void truncate(long mark) {
            if (!measuring && !overflow && buf != null) {
                for (long p = mark; p < bits; p++) {
                    int byteIdx = base + (int) (p >>> 3);
                    int bitIdx = (int) (p & 7L);
                    if (byteIdx < buf.length) {
                        buf[byteIdx] &= (byte) ~(1 << bitIdx);
                    }
                }
            }
            bits = mark;
        }

        public int bytesWritten() {
            return base + (int) ((bits + 7L) >>> 3);
        }
    }

    // Low-bit-first bit reader over a batch's bytes.
    public static final class Reader {
        public final byte[] buf;
        public final long totalBits;
        public final TableVocabulary vocab;
        public final TableVocabulary.Report report;
        public long at;
        public boolean err;

        public Reader(byte[] buf, int startByte, TableVocabulary vocab, TableVocabulary.Report report) {
            this.buf = buf;
            this.totalBits = buf == null ? 0L : (long) buf.length << 3;
            this.vocab = vocab;
            this.report = report;
            this.at = (long) startByte << 3;
        }

        public long get(int n) {
            if (n == 0) { return 0L; }
            if (err || n < 0 || n > 64 || at < 0L || (long) n > totalBits - at) {
                err = true;
                return 0L;
            }
            long v = 0L;
            for (int i = 0; i < n; i++) {
                long pos = at + (long) i;
                int byteIdx = (int) (pos >>> 3);
                int bitIdx = (int) (pos & 7L);
                if (((buf[byteIdx] >>> bitIdx) & 1) != 0) {
                    v |= 1L << i;
                }
            }
            at += (long) n;
            return v;
        }

        public boolean skipBits(long n) {
            if (err || n < 0L || n > totalBits - at) {
                err = true;
                return false;
            }
            at += n;
            return true;
        }

        public boolean align() {
            int pad = (int) ((8L - (at & 7L)) & 7L);
            if (pad == 0) { return !err; }
            long v = get(pad);
            if (err || v != 0L) {
                err = true;
                return false;
            }
            return true;
        }

        public int atByte() {
            return (int) (at >>> 3);
        }

        public TableVocabulary.Entry ref() {
            long slot = get(vocab.refBits);
            if (err || slot == 0L) { return null; }
            if (slot < 0L || slot > (long) vocab.count) {
                err = true;
                return null;
            }
            TableVocabulary.Entry e = vocab.entries[(int) slot - 1];
            if (reservedId(e.id)) {
                err = true;
                return null;
            }
            return e;
        }

        public TableVocabulary.Entry nameRef() {
            long slot = get(vocab.refBits);
            if (err || slot == 0L) { return null; }
            if (slot < 0L || slot > (long) vocab.count) {
                err = true;
                return null;
            }
            TableVocabulary.Entry e = vocab.entries[(int) slot - 1];
            if (reservedId(e.id) || e.kind != 0) {
                err = true;
                return null;
            }
            return e;
        }

        public TableVocabulary.Entry armRef() {
            long slot = get(vocab.refBits);
            if (err || slot == 0L) { return null; }
            if (slot < 0L || slot > (long) vocab.count) {
                err = true;
                return null;
            }
            TableVocabulary.Entry e = vocab.entries[(int) slot - 1];
            if (reservedId(e.id) || e.kind == 0) {
                err = true;
                return null;
            }
            return e;
        }
    }

    public static boolean reservedId(long id) {
        return id == -1L || id == -2L || id == -3L;
    }

    public static boolean kindWidens(int from, int to) {
        return (from >= 2 && from <= 5 && ((to > from && to <= 5) || to == 18))
                || (from >= 6 && from <= 9 && ((to > from && to <= 9) || to == 19))
                || (from == 10 && to == 11);
    }

    public static boolean entryWidens(TableVocabulary.Entry e, int myKind, int myElemKind) {
        int eElem = e.element != null ? e.element.kind : 0;
        if (eElem == myElemKind) {
            return kindWidens(e.kind, myKind);
        }
        if (e.kind == 14 && myKind == 14) {
            return kindWidens(eElem, myElemKind);
        }
        return false;
    }

    public static long readCount(Reader r, TableVocabulary.Shape s) {
        long min = s.kind == 14 ? s.min : 0L;
        int width = TableVocabulary.bitCount(s.max - min);
        long raw = width > 0 ? r.get(width) : 0L;
        return raw + min;
    }

    public static long valueBits(TableVocabulary.Shape s) {
        if (s == null) { return -1L; }
        switch (s.kind) {
            case 0: case 32: return 0L;
            case 1: return 1L;
            case 11: return 64L;
            case 30: return 16L;
        }
        if ((s.kind >= 2 && s.kind <= 10) || (s.kind >= 18 && s.kind <= 29)) {
            return s.bits;
        }
        return -1L;
    }

    public static long elementRunBits(int kind, TableVocabulary.Shape elem) {
        if (kind != 14 || elem == null) { return -1L; }
        switch (elem.kind) {
            case 13: case 15: case 30: case 17: return -1L;
        }
        return valueBits(elem);
    }

    public static boolean skipRun(Reader r, long n, long width) {
        if (width == 0L) { return !r.err; }
        if (width < 0L || n < 0L || n > Long.MAX_VALUE / width) {
            r.err = true;
            return false;
        }
        return r.skipBits(n * width);
    }

    public static boolean skip(Reader r, TableVocabulary.Entry e) {
        return skipAt(r, e.shape, e.element, 0);
    }

    public static boolean skipAt(Reader r, TableVocabulary.Shape s, TableVocabulary.Shape elem, int depth) {
        if (depth > 128 || s == null) { r.err = true; return false; }
        switch (s.kind) {
            case 0: case 32:
                return !r.err;
            case 1:
                return r.skipBits(1L);
            case 11:
                return r.skipBits(64L);
            case 30: {
                long ref = r.get(r.vocab.refBits);
                if (r.err) { return false; }
                if (ref == 0L) { return true; }
                if (ref < 0L || ref > (long) r.vocab.count) { r.err = true; return false; }
                TableVocabulary.Entry ve = r.vocab.entries[(int) ref - 1];
                if (reservedId(ve.id) || ve.kind != 0) { r.err = true; return false; }
                return true;
            }
            case 13:
                return skipBody(r, depth + 1);
            case 17:
                r.err = true;
                return false;
            case 15: {
                long ref = r.get(r.vocab.refBits);
                if (r.err) { return false; }
                if (ref == 0L) { return true; }
                if (ref < 0L || ref > (long) r.vocab.count) { r.err = true; return false; }
                TableVocabulary.Entry arm = r.vocab.entries[(int) ref - 1];
                if (reservedId(arm.id) || arm.kind == 0) { r.err = true; return false; }
                return skipAt(r, arm.shape, arm.element, depth + 1);
            }
            case 12: {
                long n = r.get(TableVocabulary.bitCount(s.max));
                if (r.err || !r.align()) { return false; }
                return r.skipBits(n * 8L);
            }
            case 33: {
                long n = r.get(TableVocabulary.bitCount(s.max));
                if (r.err) { return false; }
                return r.skipBits(n * 16L);
            }
            case 31: {
                if (!r.align()) { return false; }
                long n = r.get(32);
                if (r.err) { return false; }
                return r.skipBits(n * 8L);
            }
            case 14: case 16: {
                long n = readCount(r, s);
                if (r.err) { return false; }
                if (s.kind == 14 && elem != null && elem.kind == 6 && !r.align()) {
                    return false;
                }
                long run = elementRunBits(s.kind, elem);
                if (run >= 0L) {
                    return skipRun(r, n, run);
                }
                for (long i = 0; i < n; i++) {
                    if (s.kind == 16) {
                        long kref = r.get(r.vocab.refBits);
                        if (r.err || kref < 0L || kref > (long) r.vocab.count) {
                            r.err = true;
                            return false;
                        }
                    }
                    if (!skipAt(r, elem, null, depth + 1)) {
                        return false;
                    }
                }
                return true;
            }
        }
        long w = valueBits(s);
        if (w < 0L) { r.err = true; return false; }
        return r.skipBits(w);
    }

    public static boolean skipBody(Reader r, int depth) {
        if (depth > 128) { r.err = true; return false; }
        while (true) {
            long ref = r.get(r.vocab.refBits);
            if (r.err) { return false; }
            if (ref == 0L) { return true; }
            if (ref < 0L || ref > (long) r.vocab.count) { r.err = true; return false; }
            TableVocabulary.Entry e = r.vocab.entries[(int) ref - 1];
            if (!skipAt(r, e.shape, e.element, depth)) { return false; }
        }
    }

    public static void writeScalar(Writer w, int packing, int bits, long baseLo, long baseHi, long lo, long hi) {
        if (packing == 1) {
            long borrow = Long.compareUnsigned(lo, baseLo) < 0 ? 1L : 0L;
            lo -= baseLo;
            hi -= baseHi + borrow;
        }
        if (bits <= 64) {
            w.put(lo, bits);
        } else {
            w.put(lo, 64);
            w.put(hi, bits - 64);
        }
    }

    public static void writeFloat32Quantized(Writer w, float v, float qMin, float qDelta, long qCount, int bits) {
        float ratio = (v - qMin) / qDelta;
        if (!(ratio >= 0.0f)) {
            ratio = 0.0f;
        } else if (!(ratio <= 1.0f)) {
            ratio = 1.0f;
        }
        float scaled = ratio * (float) qCount + 0.5f;
        long index = (long) Math.floor((double) scaled);
        if (index > qCount) {
            index = qCount;
        }
        w.put(index, bits);
    }

    public static long readSigned(Reader r, TableVocabulary.Shape s, long min, long max, boolean hasRange) {
        long raw = r.get((int) s.bits);
        if (r.err) { return 0L; }
        long v;
        if (s.packing == 1) {
            v = raw + s.baseLo;
        } else if (s.bits > 0 && s.bits < 64) {
            int shift = 64 - (int) s.bits;
            v = (raw << shift) >> shift;
        } else {
            v = raw;
        }
        if (hasRange) {
            if (v < min) { v = min; r.report.clamped++; }
            else if (v > max) { v = max; r.report.clamped++; }
        }
        return v;
    }

    public static long readUnsigned(Reader r, TableVocabulary.Shape s, long min, long max, boolean hasRange, int bitsWidth) {
        long raw = r.get((int) s.bits);
        if (r.err) { return 0L; }
        long v = (s.packing == 1) ? (raw + s.baseLo) : raw;
        if (hasRange) {
            if (Long.compareUnsigned(v, min) < 0) { v = min; r.report.clamped++; }
            else if (Long.compareUnsigned(v, max) > 0) { v = max; r.report.clamped++; }
        }
        if (bitsWidth > 0 && bitsWidth < 64) {
            long maxBits = (1L << bitsWidth) - 1L;
            if (Long.compareUnsigned(v, maxBits) > 0) {
                v = maxBits;
                r.report.clamped++;
            }
            v &= maxBits;
        }
        return v;
    }

    public static long[] read128Raw(Reader r, TableVocabulary.Shape s, boolean signed) {
        long lo, hi;
        if (s.kind == 18 || s.kind == 19 || s.kind == 24 || s.kind == 29) {
            lo = r.get((int) Math.min(s.bits, 64L));
            hi = s.bits > 64L ? r.get((int) (s.bits - 64L)) : 0L;
            if (r.err) { return new long[]{0L, 0L}; }
            if (s.packing == 1) {
                long sum = lo + s.baseLo;
                long carry = Long.compareUnsigned(sum, lo) < 0 ? 1L : 0L;
                lo = sum;
                hi = hi + s.baseHi + carry;
            }
        } else {
            long raw = r.get((int) s.bits);
            if (r.err) { return new long[]{0L, 0L}; }
            if (s.packing == 1) {
                lo = raw + s.baseLo;
            } else if (signed && s.bits > 0 && s.bits < 64) {
                int shift = 64 - (int) s.bits;
                lo = (raw << shift) >> shift;
            } else {
                lo = raw;
            }
            hi = (signed && lo < 0L) ? -1L : 0L;
        }
        return new long[]{lo, hi};
    }

    public static int compareUnsigned128(long aLo, long aHi, long bLo, long bHi) {
        int c = Long.compareUnsigned(aHi, bHi);
        return c != 0 ? c : Long.compareUnsigned(aLo, bLo);
    }

    public static int compareSigned128(long aLo, long aHi, long bLo, long bHi) {
        int c = Long.compare(aHi, bHi);
        return c != 0 ? c : Long.compareUnsigned(aLo, bLo);
    }

    // A FLOAT IS FINITE ON THE MESSAGE WIRE (docs/SPEC-TABLES.md §3.3, the
    // packet wire's own rule): a float carried whole whose raw bits
    // are a NaN or an infinity refuses the batch as non_finite_float, naming
    // the first such field, before any range is applied. A quantized float's
    // index always names a finite value.
    public static void nonFiniteFloat(TableVocabulary.Report report, String field) {
        if (!report.refused) {
            report.refused = true;
            report.reason = "non_finite_float: " + field;
        }
    }

    public static float readFloat32(Reader r, TableVocabulary.Shape s, float min, float max, boolean hasRange, String field) {
        float v;
        if (s.packing == 2) {
            long index = r.get((int) s.bits);
            if (r.err || Long.compareUnsigned(index, s.steps) > 0) {
                r.err = true;
                return 0.0f;
            }
            float ratio = (float) index / (float) s.steps;
            float scaled = ratio * s.qDelta;
            v = scaled + s.qMin;
        } else {
            int raw = (int) r.get(32);
            if (r.err) { return 0.0f; }
            if ((raw & 0x7f800000) == 0x7f800000) {
                nonFiniteFloat(r.report, field);
                return 0.0f;
            }
            v = Float.intBitsToFloat(raw);
        }
        if (hasRange) {
            if (v < min) { v = min; r.report.clamped++; }
            else if (v > max) { v = max; r.report.clamped++; }
        }
        return v;
    }

    public static double readFloat64(Reader r, TableVocabulary.Shape s, double min, double max, boolean hasRange, String field) {
        if (s.kind == 10) {
            if (s.packing == 2) {
                long index = r.get((int) s.bits);
                if (r.err || Long.compareUnsigned(index, s.steps) > 0) {
                    r.err = true;
                    return 0.0;
                }
                float ratio = (float) index / (float) s.steps;
                float scaled = ratio * s.qDelta;
                return (double) (scaled + s.qMin);
            }
            int raw = (int) r.get(32);
            if (r.err) { return 0.0; }
            if ((raw & 0x7f800000) == 0x7f800000) {
                nonFiniteFloat(r.report, field);
                return 0.0;
            }
            // the float rung is the ordinary conversion: exact for every finite float32
            return (double) Float.intBitsToFloat(raw);
        }
        long raw = r.get(64);
        if (r.err) { return 0.0; }
        if ((raw & 0x7ff0000000000000L) == 0x7ff0000000000000L) {
            nonFiniteFloat(r.report, field);
            return 0.0;
        }
        double v = Double.longBitsToDouble(raw);
        if (hasRange) {
            if (v < min) { v = min; r.report.clamped++; }
            else if (v > max) { v = max; r.report.clamped++; }
        }
        return v;
    }

    public static boolean utf8Valid(byte[] b, int off, int len) {
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

    public static int utf8Clamp(byte[] b, int off, int max) {
        int i = 0;
        while (i < max) {
            int c = b[off + i] & 0xff;
            int step = c < 0x80 ? 1 : (c < 0xe0 ? 2 : (c < 0xf0 ? 3 : 4));
            if (i + step > max) { break; }
            i += step;
        }
        return i;
    }

    public static boolean bytesEqual(byte[] buf, int len, byte[] want) {
        if (len != want.length) { return false; }
        for (int i = 0; i < len; i++) {
            if (buf[i] != want[i]) { return false; }
        }
        return true;
    }
}
`

// ---------------------------------------------------------------------------
// PER-TYPE MESSAGE-FORM CODEC (<Type>Message.java)
// ---------------------------------------------------------------------------

type messageGen struct {
	unit    *ir.Unit
	vocab   []ir.TableVocabularyEntry
	refBits int
	b       strings.Builder
	serial  int
	// owner is the declaration whose body a load is being emitted for, the
	// <Owner> of a non_finite_float refusal's <Owner>.<field> (§3.3)
	owner string
}

func (g *messageGen) pf(format string, args ...any) { fmt.Fprintf(&g.b, format, args...) }

func (g *messageGen) nextTemp(prefix string) string {
	g.serial++
	return fmt.Sprintf("%s%d", prefix, g.serial)
}

// hoistLongCalls puts each `if (call) {` line that would run past the
// formatter's 100 columns into a shape java.Canonical already lays out the
// formatter's way: the call is assigned to a fresh boolean on the line above
// and the `if` tests that local (docs/JAVA-DIALECT.md: generated Java is
// google-java-format --aosp clean). Only a condition that is ONE call, maybe
// negated, is hoisted, so evaluation order and short-circuiting are unchanged.
//
// It reads the layout java.Canonical gives the file, because that pass moves
// the indentation of a case body, and the generator's last pass runs it
// again over the result (it is idempotent).
func (g *messageGen) hoistLongCalls(src string) string {
	var out strings.Builder
	for _, line := range strings.SplitAfter(string(javapkt.Canonical([]byte(src))), "\n") {
		ind, neg, call, rest, ok := splitCallCondition(line)
		if !ok || len(ind)+len("if (")+len(neg)+len(call)+len(") {") <= javaColumns {
			out.WriteString(line)
			continue
		}
		v := g.nextTemp("ok")
		fmt.Fprintf(&out, "%sboolean %s = %s;\n", ind, v, call)
		fmt.Fprintf(&out, "%sif (%s%s) {%s", ind, neg, v, rest)
	}
	return out.String()
}

// javaColumns is google-java-format's column limit.
const javaColumns = 100

// splitCallCondition reads a line `<ind>if (<neg><call>) {<rest>` whose
// condition is a single method call `Class.method(...)` with no literal in
// it; ok is false for any other shape.
func splitCallCondition(line string) (ind, neg, call, rest string, ok bool) {
	t := strings.TrimLeft(line, " ")
	ind = line[:len(line)-len(t)]
	if !strings.HasPrefix(t, "if (") {
		return "", "", "", "", false
	}
	t = t[len("if ("):]
	if strings.HasPrefix(t, "!") {
		neg, t = "!", t[1:]
	}
	open := strings.IndexByte(t, '(')
	if open <= 0 || !strings.Contains(t[:open], ".") {
		return "", "", "", "", false
	}
	for _, c := range t[:open] {
		if c != '.' && c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return "", "", "", "", false
		}
	}
	depth := 0
	for i := open; i < len(t); i++ {
		switch t[i] {
		case '"', '\'':
			return "", "", "", "", false
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				if !strings.HasPrefix(t[i+1:], ") {") {
					return "", "", "", "", false
				}
				return ind, neg, t[:i+1], t[i+1+len(") {"):], true
			}
		}
	}
	return "", "", "", "", false
}

func (g *messageGen) slotOf(e ir.TableVocabularyEntry) int {
	key := e.Key()
	for i, v := range g.vocab {
		if v.Key() == key {
			return i + 1
		}
	}
	panic(fmt.Sprintf("message entry outside vocabulary: %s", key))
}

func (g *messageGen) nameSlotOf(id uint64) int {
	for i, v := range g.vocab {
		if v.Id == id && v.Kind == 0 {
			return i + 1
		}
	}
	panic(fmt.Sprintf("message name id outside vocabulary: 0x%016x", id))
}

func (g *messageGen) emitType(st *ir.Struct, root bool) {
	cls := messageClass(st.Name)
	valType := fixedValue(st.Name)

	g.pf("// %s under the MESSAGE FORM (docs/SPEC-TABLES.md §3.3), form byte 2.\n", st.Name)
	g.pf("public final class %s {\n", cls)
	g.pf("    private %s() {}\n\n", cls)

	g.emitReset(st, valType)
	g.emitSaveBody(st, valType)
	g.emitLoadBody(st, valType)
	if root {
		g.emitRootBatch(st, valType)
	}
	g.pf("}\n")
}

// ---- reset ----------------------------------------------------------------

func (g *messageGen) emitReset(st *ir.Struct, valType string) {
	g.pf("    public static void reset(%s v) {\n", valType)
	for _, f := range st.Fields {
		g.emitResetField(f, "v", 8)
	}
	g.pf("    }\n\n")
}

func (g *messageGen) emitResetField(f *ir.Field, owner string, indent int) {
	ind := strings.Repeat(" ", indent)
	name := javaName(f.Name)
	expr := owner + "." + name
	if f.Type.Optional {
		g.pf("%s%sPresent = false;\n", ind, expr)
	}
	switch {
	case f.Type.Kind == ir.TWString && f.Array == ir.ArrayNone && f.KeyEnum == "":
		g.pf("%sjava.util.Arrays.fill(%s, (char) 0);\n", ind, expr)
		g.pf("%s%sLength = 0;\n", ind, expr)
		return
	case (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && f.Array == ir.ArrayNone && f.KeyEnum == "":
		g.pf("%sjava.util.Arrays.fill(%s, (byte) 0);\n", ind, expr)
		if len(f.DefBytes) > 0 {
			g.pf("%s%s;\n", ind, fixedByteDefaultExpr(expr, f.DefBytes))
			g.pf("%s%sLength = %d;\n", ind, expr, len(f.DefBytes))
		} else {
			g.pf("%s%sLength = 0;\n", ind, expr)
		}
		return
	}
	if f.KeyEnum != "" || f.Array != ir.ArrayNone {
		g.emitResetArray(f, expr, indent)
		return
	}
	g.emitResetSingle(f, expr, indent)
}

func (g *messageGen) emitResetArray(f *ir.Field, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	typ := fixedScalarType(f.Type)
	switch {
	case f.Type.Kind == ir.TNamed && isStructType(f.Type):
		st := f.Type.Ref.(*ir.Struct)
		g.pf("%sfor (int i = 0; i < %s.length; i++) {\n", ind, expr)
		g.pf("%s    %s.reset(%s[i]);\n", ind, messageClass(st.Name), expr)
		g.pf("%s}\n", ind)
	case f.Type.Kind == ir.TNamed && isUnionType(f.Type):
		un := f.Type.Ref.(*ir.Union)
		idx := g.nextTemp("i")
		g.pf("%sfor (int %s = 0; %s < %s.length; %s++) {\n", ind, idx, idx, expr, idx)
		g.emitResetUnion(un, fmt.Sprintf("%s[%s]", expr, idx), indent+4)
		g.pf("%s}\n", ind)
	case typ == "Int128" || typ == "UInt128":
		def := typ + ".zero"
		if init := fixedScalarDefault(f); init != "" {
			def = init
		}
		g.pf("%sjava.util.Arrays.fill(%s, %s);\n", ind, expr, def)
	default:
		def := fixedZeroLit(typ)
		if init := fixedScalarDefault(f); init != "" {
			def = init
		}
		g.pf("%sjava.util.Arrays.fill(%s, %s);\n", ind, expr, def)
	}
	if f.Array == ir.ArrayCounted {
		g.pf("%s%sCount = %d;\n", ind, expr, f.BornCount())
	}
}

func (g *messageGen) emitResetSingle(f *ir.Field, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	typ := fixedScalarType(f.Type)
	switch {
	case f.Type.Kind == ir.TNamed && isStructType(f.Type):
		st := f.Type.Ref.(*ir.Struct)
		g.pf("%s%s.reset(%s);\n", ind, messageClass(st.Name), expr)
	case f.Type.Kind == ir.TNamed && isUnionType(f.Type):
		un := f.Type.Ref.(*ir.Union)
		g.emitResetUnion(un, expr, indent)
	case typ == "Int128" || typ == "UInt128":
		def := typ + ".zero"
		if init := fixedScalarDefault(f); init != "" {
			def = init
		}
		g.pf("%s%s = %s;\n", ind, expr, def)
	default:
		def := fixedZeroLit(typ)
		if init := fixedScalarDefault(f); init != "" {
			def = init
		}
		g.pf("%s%s = %s;\n", ind, expr, def)
	}
}

func (g *messageGen) emitResetUnion(un *ir.Union, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	g.pf("%s%s.type = 0;\n", ind, expr)
	for _, v := range un.Variants {
		if v.F != nil {
			g.emitResetField(v.F, expr, indent)
		}
	}
}

func fixedZeroLit(typ string) string {
	switch typ {
	case "boolean":
		return "false"
	case "byte":
		return "(byte) 0"
	case "short":
		return "(short) 0"
	case "int":
		return "0"
	case "long":
		return "0L"
	case "float":
		return "0.0f"
	case "double":
		return "0.0"
	}
	return "0"
}

func isStructType(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	_, ok := t.Ref.(*ir.Struct)
	return ok
}

func isUnionType(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	_, ok := t.Ref.(*ir.Union)
	return ok
}

func messageEnumRef(f *ir.Field) *ir.Enum {
	if f == nil || f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

// ---- guards ---------------------------------------------------------------

func messageGuardExprs(st *ir.Struct, owner string) map[string]string {
	name := func(cond string) string {
		return owner + "." + javaName(cond)
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

// ---- saveMessageBody ------------------------------------------------------

func (g *messageGen) emitSaveBody(st *ir.Struct, valType string) {
	g.pf("    public static boolean saveMessageBody(TableMessage.Writer w, %s v) {\n", valType)
	g.pf("        if (v == null) { return false; }\n")
	guards := messageGuardExprs(st, "v")
	for _, f := range st.Fields {
		g.emitSaveField(f, "v", guards[f.Name], 8)
	}
	g.pf("        w.put(0L, %d);\n", g.refBits)
	g.pf("        return !w.overflow;\n")
	g.pf("    }\n\n")
}

func (g *messageGen) emitSaveField(f *ir.Field, owner, guard string, indent int) {
	ind := strings.Repeat(" ", indent)
	name := javaName(f.Name)
	expr := owner + "." + name
	entry := ir.TableFieldEntry(f)
	slot := g.slotOf(entry)

	cond := guard
	if f.Type.Optional {
		if cond != "" {
			cond = "(" + cond + ") && " + expr + "Present"
		} else {
			cond = expr + "Present"
		}
	}
	g.pf("%s{\n", ind)
	inner := indent + 4
	if cond != "" {
		g.pf("%s    if (%s) {\n", ind, cond)
		inner += 4
	}
	iind := strings.Repeat(" ", inner)
	g.emitWriteBoundsCheck(f, owner, inner)

	if !f.Type.Optional && isStructType(f.Type) && f.Array == ir.ArrayNone && f.KeyEnum == "" {
		st := f.Type.Ref.(*ir.Struct)
		probe := g.nextTemp("probe")
		// A nested table whose fields all ride nothing is elided whole: its
		// body is only its own zero reference (docs/SPEC-TABLES.md §3.3, "a
		// nested table | its fields in place, ending at its own zero
		// reference"). Its ride status is measured into a probe writer first,
		// so nothing is ever written into the real stream that is later
		// thrown away — a buffer of exactly the measured size must never see
		// a transient write past its end.
		// The probe's own answer counts: a body it refuses (a NaN or an
		// infinity, docs/SPEC-TABLES.md §3.3) refuses the holder too, even
		// when what it wrote before refusing looks like an elided body.
		g.pf("%sTableMessage.Writer %s = new TableMessage.Writer(null, 0, true);\n", iind, probe)
		g.pf("%sif (!%s.saveMessageBody(%s, %s)) { return false; }\n", iind, messageClass(st.Name), probe, expr)
		g.pf("%sif (%s.bits != %dL) {\n", iind, probe, g.refBits)
		g.pf("%s    w.put(%dL, %d);\n", iind, slot, g.refBits)
		g.pf("%s    if (!%s.saveMessageBody(w, %s)) { return false; }\n", iind, messageClass(st.Name), expr)
		g.pf("%s}\n", iind)
	} else {
		rides := "true"
		if !f.Type.Optional {
			rides = g.emitMessageRides(f, owner, inner)
		}
		g.pf("%sif (%s) {\n", iind, rides)
		g.pf("%s    w.put(%dL, %d);\n", iind, slot, g.refBits)
		g.emitSavePayload(f, entry, owner, inner+4)
		g.pf("%s}\n", iind)
	}

	if cond != "" {
		g.pf("%s    }\n", ind)
	}
	g.pf("%s}\n", ind)
}

func (g *messageGen) emitWriteBoundsCheck(f *ir.Field, owner string, indent int) {
	ind := strings.Repeat(" ", indent)
	name := javaName(f.Name)
	expr := owner + "." + name
	switch {
	case (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString) && f.Array == ir.ArrayNone && f.KeyEnum == "":
		g.pf("%sif (%sLength < 0 || %sLength > %d) { return false; }\n", ind, expr, expr, f.Type.Size)
	case f.Array == ir.ArrayCounted && f.KeyEnum == "":
		g.pf("%sif (%sCount < %d || %sCount > %d) { return false; }\n", ind, expr, f.ArrayMin, expr, f.ArrayBound)
	}
}

func (g *messageGen) emitMessageRides(f *ir.Field, owner string, indent int) string {
	ind := strings.Repeat(" ", indent)
	name := javaName(f.Name)
	expr := owner + "." + name
	if f.KeyEnum != "" {
		pairs := g.nextTemp("pairs")
		g.emitKeyedCount(f, expr, pairs, indent)
		return pairs + " > 0"
	}
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
		if len(f.DefBytes) == 0 {
			return expr + "Length > 0"
		}
		return fmt.Sprintf("!TableMessage.bytesEqual(%s, %sLength, %s)", expr, expr, javaByteArrayLiteral(f.DefBytes))
	}
	if f.Type.Kind == ir.TWString {
		return expr + "Length > 0"
	}
	if f.Array == ir.ArrayCounted {
		return expr + "Count > 0"
	}
	if f.Array == ir.ArrayFixed {
		if isStructType(f.Type) {
			return "true"
		}
		allDef := g.nextTemp("allDef")
		idx := g.nextTemp("i")
		g.pf("%sboolean %s = true;\n", ind, allDef)
		g.pf("%sfor (int %s = 0; %s < %d; %s++) {\n", ind, idx, idx, f.ArrayBound, idx)
		elemRides := g.scalarOrUnionRidesExpr(f, fmt.Sprintf("%s[%s]", expr, idx))
		g.pf("%s    if (%s) { %s = false; break; }\n", ind, elemRides, allDef)
		g.pf("%s}\n", ind)
		return "!" + allDef
	}
	return g.scalarOrUnionRidesExpr(f, expr)
}

func (g *messageGen) scalarOrUnionRidesExpr(f *ir.Field, expr string) string {
	if isUnionType(f.Type) {
		return expr + ".type != 0"
	}
	typ := fixedScalarType(f.Type)
	if typ == "Int128" || typ == "UInt128" {
		var defLo, defHi uint64
		if f.HasDefault && f.DefInt != nil {
			defLo, defHi = fixed128Halves(f.DefInt)
		}
		return fmt.Sprintf("(%s.lo != %sL || %s.hi != %sL)", expr, fixedHexLong(defLo), expr, fixedHexLong(defHi))
	}
	def := fixedZeroLit(typ)
	if init := fixedScalarDefault(f); init != "" {
		def = init
	}
	// A FLOAT RIDES BY ITS BITS (docs/SPEC-TABLES.md §3.3, the 10-15 rule): a
	// -0 under a +0 default is not the default.
	switch typ {
	case "float":
		return "Float.floatToRawIntBits(" + expr + ") != Float.floatToRawIntBits(" + def + ")"
	case "double":
		return "Double.doubleToRawLongBits(" + expr + ") != Double.doubleToRawLongBits(" + def + ")"
	}
	return expr + " != " + def
}

func (g *messageGen) emitKeyedCount(f *ir.Field, expr, pairsVar string, indent int) {
	ind := strings.Repeat(" ", indent)
	idx := g.nextTemp("i")
	g.pf("%sint %s = 0;\n", ind, pairsVar)
	g.pf("%sfor (int %s = 0; %s < %d; %s++) {\n", ind, idx, idx, f.KeyEnumRef.Max, idx)
	if isStructType(f.Type) {
		st := f.Type.Ref.(*ir.Struct)
		probe := g.nextTemp("probe")
		g.pf("%s    TableMessage.Writer %s = new TableMessage.Writer(null, 0, true);\n", ind, probe)
		g.pf("%s    if (!%s.saveMessageBody(%s, %s[%s])) { return false; }\n", ind, messageClass(st.Name), probe, expr, idx)
		g.pf("%s    if (%s.bits > %dL) { %s++; }\n", ind, probe, g.refBits, pairsVar)
	} else {
		cond := g.scalarOrUnionRidesExpr(f, fmt.Sprintf("%s[%s]", expr, idx))
		g.pf("%s    if (%s) { %s++; }\n", ind, cond, pairsVar)
	}
	g.pf("%s}\n", ind)
}

func (g *messageGen) emitSavePayload(f *ir.Field, entry ir.TableVocabularyEntry, owner string, indent int) {
	ind := strings.Repeat(" ", indent)
	name := javaName(f.Name)
	expr := owner + "." + name
	shape := entry.Shape

	g.emitWriteBoundsCheck(f, owner, indent)

	switch {
	case f.Type.Kind == ir.TString && f.Array == ir.ArrayNone && f.KeyEnum == "":
		countBits := ir.TableMessageBitsRequired(0, shape.Max)
		g.pf("%sw.put((long) %sLength, %d);\n", ind, expr, countBits)
		g.pf("%sw.raw(%s, %sLength);\n", ind, expr, expr)

	case f.Type.Kind == ir.TBytes && f.Array == ir.ArrayNone && f.KeyEnum == "":
		countBits := ir.TableMessageCountBits(shape)
		g.pf("%sw.put((long) %sLength - %dL, %d);\n", ind, expr, shape.Min, countBits)
		g.pf("%sw.raw(%s, %sLength);\n", ind, expr, expr)

	case f.Type.Kind == ir.TWString && f.Array == ir.ArrayNone && f.KeyEnum == "":
		countBits := ir.TableMessageBitsRequired(0, shape.Max)
		idx := g.nextTemp("i")
		g.pf("%sw.put((long) %sLength, %d);\n", ind, expr, countBits)
		g.pf("%sfor (int %s = 0; %s < %sLength; %s++) {\n", ind, idx, idx, expr, idx)
		g.pf("%s    w.put((long) (%s[%s] & 0xffff), 16);\n", ind, expr, idx)
		g.pf("%s}\n", ind)

	case f.KeyEnum != "":
		pairs := g.nextTemp("pairs")
		g.emitKeyedCount(f, expr, pairs, indent)
		countBits := ir.TableMessageBitsRequired(0, shape.Max)
		g.pf("%sw.put((long) %s, %d);\n", ind, pairs, countBits)
		inner := ir.TableMessageShape{}
		if shape.Inner != nil {
			inner = *shape.Inner
		}
		for slotIdx, vName := range f.KeyEnumRef.Variants {
			_ = vName
			keyID := ir.TableWireId(f.KeyEnumRef.VariantWireName(slotIdx))
			keySlot := g.nameSlotOf(keyID)
			elemExpr := fmt.Sprintf("%s[%d]", expr, slotIdx)
			if isStructType(f.Type) {
				st := f.Type.Ref.(*ir.Struct)
				probe := g.nextTemp("probe")
				g.pf("%s{\n", ind)
				g.pf("%s    TableMessage.Writer %s = new TableMessage.Writer(null, 0, true);\n", ind, probe)
				g.pf("%s    if (!%s.saveMessageBody(%s, %s)) { return false; }\n", ind, messageClass(st.Name), probe, elemExpr)
				g.pf("%s    if (%s.bits > %dL) {\n", ind, probe, g.refBits)
				g.pf("%s        w.put(%dL, %d);\n", ind, keySlot, g.refBits)
				g.pf("%s        if (!%s.saveMessageBody(w, %s)) { return false; }\n", ind, messageClass(st.Name), elemExpr)
				g.pf("%s    }\n", ind)
				g.pf("%s}\n", ind)
			} else {
				cond := g.scalarOrUnionRidesExpr(f, elemExpr)
				g.pf("%sif (%s) {\n", ind, cond)
				g.pf("%s    w.put(%dL, %d);\n", ind, keySlot, g.refBits)
				g.emitSaveElement(f, shape.Elem, inner, elemExpr, indent+4)
				g.pf("%s}\n", ind)
			}
		}

	case f.Array != ir.ArrayNone:
		countExpr := fmt.Sprintf("%d", f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			countExpr = expr + "Count"
		}
		if countBits := ir.TableMessageCountBits(shape); countBits > 0 {
			g.pf("%sw.put((long) (%s) - %dL, %d);\n", ind, countExpr, shape.Min, countBits)
		}
		if ir.TableMessageAligns(entry.Kind, shape) {
			g.pf("%sw.align();\n", ind)
		}
		inner := ir.TableMessageShape{}
		if shape.Inner != nil {
			inner = *shape.Inner
		}
		idx := g.nextTemp("i")
		g.pf("%sfor (int %s = 0; %s < %s; %s++) {\n", ind, idx, idx, countExpr, idx)
		g.emitSaveElement(f, shape.Elem, inner, fmt.Sprintf("%s[%s]", expr, idx), indent+4)
		g.pf("%s}\n", ind)

	default:
		g.emitSaveElement(f, entry.Kind, shape, expr, indent)
	}
}

func (g *messageGen) emitSaveElement(f *ir.Field, kind uint8, shape ir.TableMessageShape, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	switch {
	case isStructType(f.Type):
		st := f.Type.Ref.(*ir.Struct)
		g.pf("%sif (!%s.saveMessageBody(w, %s)) { return false; }\n", ind, messageClass(st.Name), expr)

	case isUnionType(f.Type):
		un := f.Type.Ref.(*ir.Union)
		g.pf("%sswitch (%s.type) {\n", ind, expr)
		g.pf("%s    case 0:\n", ind)
		g.pf("%s        w.put(0L, %d);\n", ind, g.refBits)
		g.pf("%s        break;\n", ind)
		for i, v := range un.Variants {
			armEntry := ir.TableArmEntry(v)
			armSlot := g.slotOf(armEntry)
			g.pf("%s    case %d: {\n", ind, i+1)
			g.pf("%s        w.put(%dL, %d);\n", ind, armSlot, g.refBits)
			if !v.Void() {
				g.emitSavePayload(v.F, armEntry, expr, indent+8)
			}
			g.pf("%s        break;\n", ind)
			g.pf("%s    }\n", ind)
		}
		g.pf("%s    default:\n", ind)
		g.pf("%s        return false;\n", ind)
		g.pf("%s}\n", ind)

	case messageEnumRef(f) != nil:
		en := messageEnumRef(f)
		swExpr := expr
		if fixedScalarType(f.Type) == "long" {
			swExpr = "(int) " + expr
		}
		g.pf("%sswitch (%s) {\n", ind, swExpr)
		g.pf("%s    case 0:\n", ind)
		g.pf("%s        w.put(0L, %d);\n", ind, g.refBits)
		g.pf("%s        break;\n", ind)
		for i := range en.Variants {
			vid := ir.TableWireId(en.VariantWireName(i))
			vSlot := g.nameSlotOf(vid)
			g.pf("%s    case %d:\n", ind, i+1)
			g.pf("%s        w.put(%dL, %d);\n", ind, vSlot, g.refBits)
			g.pf("%s        break;\n", ind)
		}
		g.pf("%s    default:\n", ind)
		g.pf("%s        return false;\n", ind)
		g.pf("%s}\n", ind)

	case int(kind) == ir.TableKindBool:
		g.pf("%sw.put(%s ? 1L : 0L, 1);\n", ind, expr)

	// A NaN OR AN INFINITY IS REFUSED, never quantized or clamped
	// (docs/SPEC-TABLES.md §3.3, both special-values rules): the body answers
	// false before the quantizer runs, so measure and save both return -1.
	case int(kind) == ir.TableKindF64:
		g.pf("%sif (!Double.isFinite(%s)) { return false; }\n", ind, expr)
		g.pf("%sw.put(Double.doubleToRawLongBits(%s), 64);\n", ind, expr)

	case int(kind) == ir.TableKindF32:
		g.pf("%sif (!Float.isFinite(%s)) { return false; }\n", ind, expr)
		if shape.Packing == ir.TableMessageQuantized {
			count, delta, _ := ir.TableMessageQuantization(shape)
			g.pf("%sTableMessage.writeFloat32Quantized(w, %s, %s, %s, %dL, %d);\n",
				ind, expr, fixedFloatLit(shape.QMin), fixedFloatLit(delta), count, shape.Bits)
		} else {
			g.pf("%sw.put((long) Float.floatToRawIntBits(%s) & 0xffffffffL, 32);\n", ind, expr)
		}

	default:
		bits := int(ir.TableMessageValueBits(kind, shape))
		var baseLo, baseHi uint64
		if shape.Packing == ir.TableMessageRanged && shape.Base != nil {
			baseLo, baseHi = fixed128Halves(shape.Base)
		}
		typ := fixedScalarType(f.Type)
		if typ == "Int128" || typ == "UInt128" {
			g.pf("%sTableMessage.writeScalar(w, %d, %d, %sL, %sL, %s.lo, %s.hi);\n",
				ind, shape.Packing, bits, fixedHexLong(baseLo), fixedHexLong(baseHi), expr, expr)
		} else {
			signed := (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed
			valExpr := expr
			if typ != "long" {
				valExpr = "(long) " + expr
			}
			if !signed {
				switch typ {
				case "byte":
					valExpr = fmt.Sprintf("((long) %s & 0xffL)", expr)
				case "short":
					valExpr = fmt.Sprintf("((long) %s & 0xffffL)", expr)
				case "int":
					valExpr = fmt.Sprintf("((long) %s & 0xffffffffL)", expr)
				}
			}
			g.pf("%sTableMessage.writeScalar(w, %d, %d, %sL, 0L, %s, 0L);\n",
				ind, shape.Packing, bits, fixedHexLong(baseLo), valExpr)
		}
	}
}

// ---- loadMessageBody ------------------------------------------------------

func (g *messageGen) emitLoadBody(st *ir.Struct, valType string) {
	g.owner = st.DeclaredName()
	g.pf("    public static boolean loadMessageBody(TableMessage.Reader r, %s v) {\n", valType)
	g.pf("        reset(v);\n")
	g.pf("        while (true) {\n")
	g.pf("            TableVocabulary.Entry entry = r.ref();\n")
	g.pf("            if (r.err) { r.report.malformed = true; return false; }\n")
	g.pf("            if (entry == null) { return true; }\n")
	if len(st.Fields) == 0 {
		g.pf("            r.report.unknown++;\n")
		g.pf("            if (!TableMessage.skip(r, entry)) { r.report.malformed = true; return false; }\n")
	} else {
		g.pf("            int idHi = (int) (entry.id >>> 32);\n")
		g.pf("            int idLo = (int) entry.id;\n")
		for idx, f := range st.Fields {
			mine := ir.TableFieldEntry(f)
			hi := int32(uint32(mine.Id >> 32))
			lo := int32(uint32(mine.Id))
			prefix := "if"
			if idx > 0 {
				prefix = "} else if"
			}
			g.pf("            %s (idHi == 0x%08x && idLo == 0x%08x) {\n", prefix, uint32(hi), uint32(lo))
			g.emitLoadFieldCase(f, mine, "v", "entry", 16)
		}
		g.pf("            } else {\n")
		g.pf("                r.report.unknown++;\n")
		g.pf("                if (!TableMessage.skip(r, entry)) { r.report.malformed = true; return false; }\n")
		g.pf("            }\n")
	}
	g.pf("        }\n")
	g.pf("    }\n\n")
}

func (g *messageGen) emitLoadFieldCase(f *ir.Field, mine ir.TableVocabularyEntry, owner, entryVar string, indent int) {
	ind := strings.Repeat(" ", indent)
	name := javaName(f.Name)
	expr := owner + "." + name

	g.pf("%sboolean widened = false;\n", ind)
	g.pf("%sint elemKind = %s.element != null ? %s.element.kind : 0;\n", ind, entryVar, entryVar)
	g.pf("%sif (%s.kind != %d || elemKind != %d) {\n", ind, entryVar, mine.Kind, mine.Shape.Elem)
	g.pf("%s    if (TableMessage.entryWidens(%s, %d, %d)) {\n", ind, entryVar, mine.Kind, mine.Shape.Elem)
	g.pf("%s        widened = true;\n", ind)
	g.pf("%s    } else {\n", ind)
	g.pf("%s        r.report.kindMismatch++;\n", ind)
	g.pf("%s        if (!TableMessage.skip(r, %s)) { r.report.malformed = true; return false; }\n", ind, entryVar)
	g.pf("%s        continue;\n", ind)
	g.pf("%s    }\n", ind)
	g.pf("%s}\n", ind)

	g.emitLoadPayload(f, owner, entryVar, indent)
	if f.Type.Optional {
		g.pf("%s%sPresent = true;\n", ind, expr)
	}
	g.pf("%sif (widened) { r.report.widened++; }\n", ind)
}

func (g *messageGen) emitLoadPayload(f *ir.Field, owner, entryVar string, indent int) {
	ind := strings.Repeat(" ", indent)
	name := javaName(f.Name)
	expr := owner + "." + name

	switch {
	case f.Type.Kind == ir.TString && f.Array == ir.ArrayNone && f.KeyEnum == "":
		nVar := g.nextTemp("n")
		startVar := g.nextTemp("start")
		keptVar := g.nextTemp("kept")
		g.pf("%slong %s = TableMessage.readCount(r, %s.shape);\n", ind, nVar, entryVar)
		g.pf("%sif (r.err || !r.align()) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%sint %s = r.atByte();\n", ind, startVar)
		g.pf("%sif (!r.skipBits(%s * 8L)) { r.report.malformed = true; return false; }\n", ind, nVar)
		g.pf("%sif (!TableMessage.utf8Valid(r.buf, %s, (int) %s)) { r.report.malformed = true; return false; }\n", ind, startVar, nVar)
		g.pf("%sint %s = (int) Math.min(%s, %dL);\n", ind, keptVar, nVar, f.Type.Size)
		g.pf("%sif ((long) %s < %s) {\n", ind, keptVar, nVar)
		g.pf("%s    %s = TableMessage.utf8Clamp(r.buf, %s, %s);\n", ind, keptVar, startVar, keptVar)
		g.pf("%s    r.report.clamped++;\n", ind)
		g.pf("%s}\n", ind)
		g.pf("%sjava.util.Arrays.fill(%s, (byte) 0);\n", ind, expr)
		g.pf("%sSystem.arraycopy(r.buf, %s, %s, 0, %s);\n", ind, startVar, expr, keptVar)
		g.pf("%s%sLength = %s;\n", ind, expr, keptVar)

	case f.Type.Kind == ir.TBytes && f.Array == ir.ArrayNone && f.KeyEnum == "":
		nVar := g.nextTemp("n")
		startVar := g.nextTemp("start")
		keptVar := g.nextTemp("kept")
		g.pf("%slong %s = TableMessage.readCount(r, %s.shape);\n", ind, nVar, entryVar)
		g.pf("%sif (r.err || !r.align()) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%sint %s = r.atByte();\n", ind, startVar)
		g.pf("%sif (!r.skipBits(%s * 8L)) { r.report.malformed = true; return false; }\n", ind, nVar)
		g.pf("%sint %s = (int) Math.min(%s, %dL);\n", ind, keptVar, nVar, f.Type.Size)
		g.pf("%sif ((long) %s < %s) { r.report.clamped++; }\n", ind, keptVar, nVar)
		g.pf("%sjava.util.Arrays.fill(%s, (byte) 0);\n", ind, expr)
		g.pf("%sSystem.arraycopy(r.buf, %s, %s, 0, %s);\n", ind, startVar, expr, keptVar)
		g.pf("%s%sLength = %s;\n", ind, expr, keptVar)

	case f.Type.Kind == ir.TWString && f.Array == ir.ArrayNone && f.KeyEnum == "":
		nVar := g.nextTemp("n")
		keptVar := g.nextTemp("kept")
		highVar := g.nextTemp("high")
		idxVar := g.nextTemp("i")
		uVar := g.nextTemp("u")
		g.pf("%slong %s = TableMessage.readCount(r, %s.shape);\n", ind, nVar, entryVar)
		g.pf("%sif (r.err || %s < 0L || %s > (r.totalBits - r.at) / 16L) { r.report.malformed = true; return false; }\n", ind, nVar, nVar)
		g.pf("%sint %s = (int) Math.min(%s, %dL);\n", ind, keptVar, nVar, f.Type.Size)
		g.pf("%sif ((long) %s < %s) { r.report.clamped++; }\n", ind, keptVar, nVar)
		g.pf("%sjava.util.Arrays.fill(%s, (char) 0);\n", ind, expr)
		g.pf("%sboolean %s = false;\n", ind, highVar)
		g.pf("%sfor (long %s = 0; %s < %s; %s++) {\n", ind, idxVar, idxVar, nVar, idxVar)
		g.pf("%s    int %s = (int) r.get(16);\n", ind, uVar)
		g.pf("%s    if (%s == 0 || (%s >= 0xdc00 && %s <= 0xdfff && !%s) || (%s && (%s < 0xdc00 || %s > 0xdfff))) {\n",
			ind, uVar, uVar, uVar, highVar, highVar, uVar, uVar)
		g.pf("%s        r.report.malformed = true;\n", ind)
		g.pf("%s        return false;\n", ind)
		g.pf("%s    }\n", ind)
		g.pf("%s    %s = (%s >= 0xd800 && %s <= 0xdbff);\n", ind, highVar, uVar, uVar)
		g.pf("%s    if (%s < (long) %s) { %s[(int) %s] = (char) %s; }\n", ind, idxVar, keptVar, expr, idxVar, uVar)
		g.pf("%s}\n", ind)
		g.pf("%sif (%s) { r.report.malformed = true; return false; }\n", ind, highVar)
		g.pf("%sif (%s > 0 && %s[%s - 1] >= 0xd800 && %s[%s - 1] <= 0xdbff) { %s--; %s[%s] = 0; }\n",
			ind, keptVar, expr, keptVar, expr, keptVar, keptVar, expr, keptVar)
		g.pf("%s%sLength = %s;\n", ind, expr, keptVar)

	case f.KeyEnum != "":
		nVar := g.nextTemp("n")
		idxVar := g.nextTemp("i")
		keyVar := g.nextTemp("key")
		slotVar := g.nextTemp("slot")
		scratchVar := g.nextTemp("scratch")
		typ := fixedScalarType(f.Type)
		g.pf("%slong %s = TableMessage.readCount(r, %s.shape);\n", ind, nVar, entryVar)
		g.pf("%sif (r.err) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%sfor (long %s = 0; %s < %s; %s++) {\n", ind, idxVar, idxVar, nVar, idxVar)
		g.pf("%s    TableVocabulary.Entry %s = r.nameRef();\n", ind, keyVar)
		g.pf("%s    if (%s == null || r.err) { r.report.malformed = true; return false; }\n", ind, keyVar)
		g.pf("%s    int %s = -1;\n", ind, slotVar)
		g.pf("%s    int kHi = (int) (%s.id >>> 32);\n", ind, keyVar)
		g.pf("%s    int kLo = (int) %s.id;\n", ind, keyVar)
		for sIdx := range f.KeyEnumRef.Variants {
			vid := ir.TableWireId(f.KeyEnumRef.VariantWireName(sIdx))
			hi := uint32(vid >> 32)
			lo := uint32(vid)
			prefix := "if"
			if sIdx > 0 {
				prefix = "} else if"
			}
			g.pf("%s    %s (kHi == 0x%08x && kLo == 0x%08x) {\n", ind, prefix, hi, lo)
			g.pf("%s        %s = %d;\n", ind, slotVar, sIdx)
		}
		if len(f.KeyEnumRef.Variants) > 0 {
			g.pf("%s    }\n", ind)
		}
		g.pf("%s    if (%s >= 0) {\n", ind, slotVar)
		g.emitLoadElement(f, expr+"["+slotVar+"]", entryVar+".element", indent+8)
		g.pf("%s    } else {\n", ind)
		g.pf("%s        r.report.unknown++;\n", ind)
		g.pf("%s        %s %s = %s;\n", ind, typ, scratchVar, g.scratchInitExpr(f))
		g.emitLoadElement(f, scratchVar, entryVar+".element", indent+8)
		g.pf("%s    }\n", ind)
		g.pf("%s}\n", ind)

	case f.Array != ir.ArrayNone:
		nVar := g.nextTemp("n")
		keepVar := g.nextTemp("keep")
		runVar := g.nextTemp("run")
		walkVar := g.nextTemp("walk")
		idxVar := g.nextTemp("i")
		scratchVar := g.nextTemp("scratch")
		typ := fixedScalarType(f.Type)
		// THE ARRAY IS NOT RESET HERE: reset(v) already initialized it once for
		// this body, and a load that stops at damage keeps what it decoded
		// (docs/SPEC-TABLES.md §3.3, §2.9). Re-reading the array in place so a
		// later malformed read cannot wipe an earlier successful occurrence, as
		// the oracle's array decode does.
		g.pf("%slong %s = TableMessage.readCount(r, %s.shape);\n", ind, nVar, entryVar)
		g.pf("%sif (r.err) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%sif (%s.element.kind == 6 && !r.align()) { r.report.malformed = true; return false; }\n", ind, entryVar)
		g.pf("%slong %s = Math.min(%s, %dL);\n", ind, keepVar, nVar, f.ArrayBound)
		g.pf("%sif (%s < %s) { r.report.clamped++; }\n", ind, keepVar, nVar)
		g.pf("%slong %s = TableMessage.elementRunBits(14, %s.element);\n", ind, runVar, entryVar)
		g.pf("%slong %s = (%s >= 0L && %s > %s) ? %s : %s;\n", ind, walkVar, runVar, nVar, keepVar, keepVar, nVar)
		g.pf("%sfor (long %s = 0; %s < %s; %s++) {\n", ind, idxVar, idxVar, walkVar, idxVar)
		g.pf("%s    if (%s < %s) {\n", ind, idxVar, keepVar)
		g.emitLoadElement(f, fmt.Sprintf("%s[(int) %s]", expr, idxVar), entryVar+".element", indent+8)
		g.pf("%s    } else {\n", ind)
		g.pf("%s        %s %s = %s;\n", ind, typ, scratchVar, g.scratchInitExpr(f))
		g.emitLoadElement(f, scratchVar, entryVar+".element", indent+8)
		g.pf("%s    }\n", ind)
		g.pf("%s}\n", ind)
		// the unread tail of the run is skipped only when one is left; nested
		// rather than joined by && so the call stands alone in its condition
		// and hoistLongCalls can lay it out (docs/JAVA-DIALECT.md)
		g.pf("%sif (%s < %s) {\n", ind, walkVar, nVar)
		g.pf("%s    if (!TableMessage.skipRun(r, %s - %s, %s)) { r.report.malformed = true; return false; }\n",
			ind, nVar, walkVar, runVar)
		g.pf("%s}\n", ind)
		if f.Array == ir.ArrayCounted {
			g.pf("%s%sCount = (int) %s;\n", ind, expr, keepVar)
		}

	default:
		g.emitLoadElement(f, expr, entryVar+".shape", indent)
	}
}

func (g *messageGen) scratchInitExpr(f *ir.Field) string {
	typ := fixedScalarType(f.Type)
	if fixedIsObject(f.Type) {
		if typ == "Int128" || typ == "UInt128" {
			return typ + ".zero"
		}
		return "new " + typ + "()"
	}
	return fixedZeroLit(typ)
}

func (g *messageGen) emitLoadElement(f *ir.Field, targetExpr, shapeExpr string, indent int) {
	ind := strings.Repeat(" ", indent)
	switch {
	case isStructType(f.Type):
		st := f.Type.Ref.(*ir.Struct)
		if strings.Contains(targetExpr, "[") {
			// an element reached through an index is named by a local, so the
			// call's argument is one name the formatter never breaks
			// (docs/JAVA-DIALECT.md); the object is the same one, loaded in place
			elem := g.nextTemp("elem")
			g.pf("%s%s %s = %s;\n", ind, fixedValue(st.Name), elem, targetExpr)
			targetExpr = elem
		}
		g.pf("%sif (!%s.loadMessageBody(r, %s)) { return false; }\n", ind, messageClass(st.Name), targetExpr)

	case isUnionType(f.Type):
		un := f.Type.Ref.(*ir.Union)
		g.emitLoadUnion(un, targetExpr, indent)

	case messageEnumRef(f) != nil:
		en := messageEnumRef(f)
		typ := fixedScalarType(f.Type)
		refVar := g.nextTemp("ref")
		veVar := g.nextTemp("ve")
		g.pf("%s{\n", ind)
		g.pf("%s    long %s = r.get(r.vocab.refBits);\n", ind, refVar)
		g.pf("%s    if (r.err) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%s    if (%s == 0L) {\n", ind, refVar)
		g.pf("%s        %s = %s;\n", ind, targetExpr, fixedNarrow(typ, "0"))
		g.pf("%s    } else {\n", ind)
		g.pf("%s        if (%s < 0L || %s > (long) r.vocab.count) { r.report.malformed = true; return false; }\n", ind, refVar, refVar)
		g.pf("%s        TableVocabulary.Entry %s = r.vocab.entries[(int) %s - 1];\n", ind, veVar, refVar)
		g.pf("%s        if (TableMessage.reservedId(%s.id) || %s.kind != 0) { r.report.malformed = true; return false; }\n", ind, veVar, veVar)
		g.pf("%s        int vHi = (int) (%s.id >>> 32);\n", ind, veVar)
		g.pf("%s        int vLo = (int) %s.id;\n", ind, veVar)
		for i := range en.Variants {
			vid := ir.TableWireId(en.VariantWireName(i))
			hi := uint32(vid >> 32)
			lo := uint32(vid)
			prefix := "if"
			if i > 0 {
				prefix = "} else if"
			}
			g.pf("%s        %s (vHi == 0x%08x && vLo == 0x%08x) {\n", ind, prefix, hi, lo)
			g.pf("%s            %s = %s;\n", ind, targetExpr, fixedNarrow(typ, fmt.Sprintf("%d", i+1)))
		}
		if len(en.Variants) > 0 {
			g.pf("%s        } else {\n", ind)
			g.pf("%s            %s = %s;\n", ind, targetExpr, fixedNarrow(typ, "0"))
			g.pf("%s            r.report.unknown++;\n", ind)
			g.pf("%s        }\n", ind)
		} else {
			g.pf("%s        %s = %s;\n", ind, targetExpr, fixedNarrow(typ, "0"))
			g.pf("%s        r.report.unknown++;\n", ind)
		}
		g.pf("%s    }\n", ind)
		g.pf("%s}\n", ind)

	case f.Type.Kind == ir.TBool:
		vVar := g.nextTemp("v")
		g.pf("%s{\n", ind)
		g.pf("%s    long %s = r.get(1);\n", ind, vVar)
		g.pf("%s    if (r.err) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%s    %s = (%s != 0L);\n", ind, targetExpr, vVar)
		g.pf("%s}\n", ind)

	case f.Type.Kind == ir.TFloat32:
		vVar := g.nextTemp("v")
		lo, hi, hasRange := fixedFloatRange(f)
		if !hasRange {
			lo, hi = "0.0f", "0.0f"
		}
		g.pf("%s{\n", ind)
		g.pf("%s    float %s = TableMessage.readFloat32(r, %s, %s, %s, %t, %q);\n", ind, vVar, shapeExpr, lo, hi, hasRange, g.owner+"."+f.Name)
		g.pf("%s    if (r.err) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%s    %s = %s;\n", ind, targetExpr, vVar)
		g.pf("%s}\n", ind)

	case f.Type.Kind == ir.TFloat64:
		vVar := g.nextTemp("v")
		lo, hi, hasRange := fixedFloatRange(f)
		if !hasRange {
			lo, hi = "0.0", "0.0"
		}
		g.pf("%s{\n", ind)
		g.pf("%s    double %s = TableMessage.readFloat64(r, %s, %s, %s, %t, %q);\n", ind, vVar, shapeExpr, lo, hi, hasRange, g.owner+"."+f.Name)
		g.pf("%s    if (r.err) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%s    %s = %s;\n", ind, targetExpr, vVar)
		g.pf("%s}\n", ind)

	default:
		typ := fixedScalarType(f.Type)
		signed := (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed
		if typ == "Int128" || typ == "UInt128" {
			rawVar := g.nextTemp("raw")
			loVar := g.nextTemp("lo")
			hiVar := g.nextTemp("hi")
			g.pf("%s{\n", ind)
			g.pf("%s    long[] %s = TableMessage.read128Raw(r, %s, %t);\n", ind, rawVar, shapeExpr, signed)
			g.pf("%s    if (r.err) { r.report.malformed = true; return false; }\n", ind)
			g.pf("%s    long %s = %s[0], %s = %s[1];\n", ind, loVar, rawVar, hiVar, rawVar)
			if rlo, rhi, ok := ir.TableRawRange(f); ok && f.HasIntRange {
				minLo, minHi := fixed128Halves(rlo)
				maxLo, maxHi := fixed128Halves(rhi)
				cmpFn := "TableMessage.compareUnsigned128"
				if signed {
					cmpFn = "TableMessage.compareSigned128"
				}
				// both comparisons are pure and read lo/hi before either arm
				// writes them, so they are taken into locals first: a statement
				// java.Canonical lays out the formatter's way (docs/JAVA-DIALECT.md)
				belowVar := g.nextTemp("below")
				aboveVar := g.nextTemp("above")
				g.pf("%s    int %s = %s(%s, %s, %sL, %sL);\n", ind, belowVar, cmpFn, loVar, hiVar, fixedHexLong(minLo), fixedHexLong(minHi))
				g.pf("%s    int %s = %s(%s, %s, %sL, %sL);\n", ind, aboveVar, cmpFn, loVar, hiVar, fixedHexLong(maxLo), fixedHexLong(maxHi))
				g.pf("%s    if (%s < 0) { %s = %sL; %s = %sL; r.report.clamped++; }\n",
					ind, belowVar, loVar, fixedHexLong(minLo), hiVar, fixedHexLong(minHi))
				g.pf("%s    else if (%s > 0) { %s = %sL; %s = %sL; r.report.clamped++; }\n",
					ind, aboveVar, loVar, fixedHexLong(maxLo), hiVar, fixedHexLong(maxHi))
			}
			g.pf("%s    %s = new %s(%s, %s);\n", ind, targetExpr, typ, hiVar, loVar)
			g.pf("%s}\n", ind)
			return
		}
		vVar := g.nextTemp("v")
		g.pf("%s{\n", ind)
		if signed {
			var minLit, maxLit int64
			hasRange := false
			if rlo, rhi, ok := ir.TableRawRange(f); ok && f.HasIntRange {
				minLit, maxLit, hasRange = rlo.Int64(), rhi.Int64(), true
			}
			g.pf("%s    long %s = TableMessage.readSigned(r, %s, %dL, %dL, %t);\n", ind, vVar, shapeExpr, minLit, maxLit, hasRange)
		} else {
			var minLit, maxLit uint64
			hasRange := false
			if rlo, rhi, ok := ir.TableRawRange(f); ok && f.HasIntRange {
				minLit, maxLit, hasRange = rlo.Uint64(), rhi.Uint64(), true
			}
			bitsWidth := 0
			if f.Type.Kind == ir.TBits {
				bitsWidth = f.Type.Width
			}
			g.pf("%s    long %s = TableMessage.readUnsigned(r, %s, %sL, %sL, %t, %d);\n",
				ind, vVar, shapeExpr, fixedHexLong(minLit), fixedHexLong(maxLit), hasRange, bitsWidth)
		}
		g.pf("%s    if (r.err) { r.report.malformed = true; return false; }\n", ind)
		g.pf("%s    %s = %s;\n", ind, targetExpr, fixedNarrow(typ, vVar))
		g.pf("%s}\n", ind)
	}
}

func (g *messageGen) emitLoadUnion(un *ir.Union, expr string, indent int) {
	ind := strings.Repeat(" ", indent)
	refVar := g.nextTemp("ref")
	armVar := g.nextTemp("arm")
	aHiVar := g.nextTemp("aHi")
	aLoVar := g.nextTemp("aLo")

	outer := g.owner
	g.owner = un.Name
	defer func() { g.owner = outer }()
	g.emitResetUnion(un, expr, indent)
	g.pf("%s{\n", ind)
	g.pf("%s    long %s = r.get(r.vocab.refBits);\n", ind, refVar)
	g.pf("%s    if (r.err) { r.report.malformed = true; return false; }\n", ind)
	g.pf("%s    if (%s == 0L) {\n", ind, refVar)
	g.pf("%s        %s.type = 0;\n", ind, expr)
	g.pf("%s    } else {\n", ind)
	g.pf("%s        if (%s < 0L || %s > (long) r.vocab.count) { r.report.malformed = true; return false; }\n", ind, refVar, refVar)
	g.pf("%s        TableVocabulary.Entry %s = r.vocab.entries[(int) %s - 1];\n", ind, armVar, refVar)
	g.pf("%s        if (TableMessage.reservedId(%s.id) || %s.kind == 0) { r.report.malformed = true; return false; }\n", ind, armVar, armVar)
	g.pf("%s        int %s = (int) (%s.id >>> 32);\n", ind, aHiVar, armVar)
	g.pf("%s        int %s = (int) %s.id;\n", ind, aLoVar, armVar)
	for i, v := range un.Variants {
		mine := ir.TableArmEntry(v)
		hi := uint32(mine.Id >> 32)
		lo := uint32(mine.Id)
		widenedVar := g.nextTemp("armWidened")
		elemKindVar := g.nextTemp("armElemKind")
		prefix := "if"
		if i > 0 {
			prefix = "} else if"
		}
		g.pf("%s        %s (%s == 0x%08x && %s == 0x%08x) {\n", ind, prefix, aHiVar, hi, aLoVar, lo)
		g.pf("%s            boolean %s = false;\n", ind, widenedVar)
		g.pf("%s            int %s = %s.element != null ? %s.element.kind : 0;\n", ind, elemKindVar, armVar, armVar)
		g.pf("%s            if (%s.kind != %d || %s != %d) {\n", ind, armVar, mine.Kind, elemKindVar, mine.Shape.Elem)
		g.pf("%s                if (TableMessage.entryWidens(%s, %d, %d)) {\n", ind, armVar, mine.Kind, mine.Shape.Elem)
		g.pf("%s                    %s = true;\n", ind, widenedVar)
		g.pf("%s                } else {\n", ind)
		g.pf("%s                    %s.type = 0;\n", ind, expr)
		g.pf("%s                    r.report.kindMismatch++;\n", ind)
		g.pf("%s                    if (!TableMessage.skip(r, %s)) { r.report.malformed = true; return false; }\n", ind, armVar)
		g.pf("%s                    %s = false;\n", ind, widenedVar)
		g.pf("%s                }\n", ind)
		g.pf("%s            }\n", ind)
		g.pf("%s            if (%s.kind == %d && %s == %d || %s) {\n", ind, armVar, mine.Kind, elemKindVar, mine.Shape.Elem, widenedVar)
		g.pf("%s                %s.type = %d;\n", ind, expr, i+1)
		if !v.Void() {
			g.emitLoadPayload(v.F, expr, armVar, indent+16)
		}
		g.pf("%s                if (%s) { r.report.widened++; }\n", ind, widenedVar)
		g.pf("%s            }\n", ind)
	}
	if len(un.Variants) > 0 {
		g.pf("%s        } else {\n", ind)
		g.pf("%s            %s.type = 0;\n", ind, expr)
		g.pf("%s            r.report.unknown++;\n", ind)
		g.pf("%s            if (!TableMessage.skip(r, %s)) { r.report.malformed = true; return false; }\n", ind, armVar)
		g.pf("%s        }\n", ind)
	} else {
		g.pf("%s        %s.type = 0;\n", ind, expr)
		g.pf("%s        r.report.unknown++;\n", ind)
		g.pf("%s        if (!TableMessage.skip(r, %s)) { r.report.malformed = true; return false; }\n", ind, armVar)
	}
	g.pf("%s    }\n", ind)
	g.pf("%s}\n", ind)
}

// ---- root batch entry points ----------------------------------------------

func (g *messageGen) emitRootBatch(st *ir.Struct, valType string) {
	_ = st
	g.pf("    public static int measureMessages(%s[] values, TableVocabulary.Report report) {\n", valType)
	g.pf("        return measureMessages(values, values == null ? 0 : values.length, report);\n")
	g.pf("    }\n\n")

	g.pf("    public static int measureMessages(%s[] values, int count, TableVocabulary.Report report) {\n", valType)
	g.pf("        if (report != null) { report.zero(); }\n")
	g.pf("        if (count < 1 || count > TableMessage.batchMax) {\n")
	g.pf("            if (report != null) {\n")
	g.pf("                report.refused = true;\n")
	g.pf("                report.reason = \"batch_too_large\";\n")
	g.pf("            }\n")
	g.pf("            return -1;\n")
	g.pf("        }\n")
	g.pf("        if (values == null || count > values.length) { return -1; }\n")
	g.pf("        TableMessage.Writer w = new TableMessage.Writer(null, 2, true);\n")
	g.pf("        for (int i = 0; i < count; i++) {\n")
	g.pf("            if (values[i] == null || !saveMessageBody(w, values[i])) { return -1; }\n")
	g.pf("        }\n")
	g.pf("        w.align();\n")
	g.pf("        if (w.overflow) { return -1; }\n")
	g.pf("        return w.bytesWritten();\n")
	g.pf("    }\n\n")

	g.pf("    public static int saveMessages(%s[] values, byte[] buffer, TableVocabulary.Report report) {\n", valType)
	g.pf("        return saveMessages(values, values == null ? 0 : values.length, buffer, report);\n")
	g.pf("    }\n\n")

	g.pf("    public static int saveMessages(%s[] values, int count, byte[] buffer, TableVocabulary.Report report) {\n", valType)
	g.pf("        if (report != null) { report.zero(); }\n")
	g.pf("        if (count < 1 || count > TableMessage.batchMax) {\n")
	g.pf("            if (report != null) {\n")
	g.pf("                report.refused = true;\n")
	g.pf("                report.reason = \"batch_too_large\";\n")
	g.pf("            }\n")
	g.pf("            return -1;\n")
	g.pf("        }\n")
	g.pf("        if (values == null || count > values.length || buffer == null || buffer.length < 2) { return -1; }\n")
	g.pf("        TableMessage.Writer mw = new TableMessage.Writer(null, 2, true);\n")
	g.pf("        for (int i = 0; i < count; i++) {\n")
	g.pf("            if (values[i] == null || !saveMessageBody(mw, values[i])) { return -1; }\n")
	g.pf("        }\n")
	g.pf("        mw.align();\n")
	g.pf("        if (mw.overflow || buffer.length < mw.bytesWritten()) { return -1; }\n")
	g.pf("        buffer[0] = TableMessage.form;\n")
	g.pf("        buffer[1] = (byte) (count - 1);\n")
	g.pf("        TableMessage.Writer w = new TableMessage.Writer(buffer, 2, false);\n")
	g.pf("        for (int i = 0; i < count; i++) {\n")
	g.pf("            if (!saveMessageBody(w, values[i])) { return -1; }\n")
	g.pf("        }\n")
	g.pf("        w.align();\n")
	g.pf("        if (w.overflow) { return -1; }\n")
	g.pf("        return w.bytesWritten();\n")
	g.pf("    }\n\n")

	g.pf("    public static int loadMessages(%s[] values, TableVocabulary vocab, byte[] data, TableVocabulary.Report report) {\n", valType)
	g.pf("        return loadMessages(values, values == null ? 0 : values.length, vocab, data, report);\n")
	g.pf("    }\n\n")

	g.pf("    public static int loadMessages(%s[] values, int capacity, TableVocabulary vocab, byte[] data, TableVocabulary.Report report) {\n", valType)
	g.pf("        if (report == null) { report = new TableVocabulary.Report(); } else { report.zero(); }\n")
	g.pf("        if (data == null || data.length == 0) { report.malformed = true; return 0; }\n")
	g.pf("        if ((data[0] & 0xff) != 2) {\n")
	g.pf("            report.refused = true;\n")
	g.pf("            report.reason = \"newer_form\";\n")
	g.pf("            return 0;\n")
	g.pf("        }\n")
	g.pf("        if (vocab == null || !vocab.announced) {\n")
	g.pf("            report.refused = true;\n")
	g.pf("            report.reason = \"no_vocabulary\";\n")
	g.pf("            return 0;\n")
	g.pf("        }\n")
	g.pf("        if (data.length < 2) { report.malformed = true; return 0; }\n")
	g.pf("        int count = (data[1] & 0xff) + 1;\n")
	g.pf("        int cap = values == null ? 0 : Math.min(capacity, values.length);\n")
	g.pf("        if (count > cap) {\n")
	g.pf("            report.refused = true;\n")
	g.pf("            report.reason = \"batch_too_large\";\n")
	g.pf("            return count;\n")
	g.pf("        }\n")
	g.pf("        TableMessage.Reader r = new TableMessage.Reader(data, 2, vocab, report);\n")
	g.pf("        for (int i = 0; i < count; i++) {\n")
	g.pf("            if (values[i] == null) { values[i] = new %s(); }\n", valType)
	// A NON-FINITE FLOAT IN ANY BODY REFUSES THE BATCH (docs/SPEC-TABLES.md
	// §3.3): no body of it is delivered, so every body read so far is reset
	// and the count is zero.
	g.pf("            boolean read = loadMessageBody(r, values[i]);\n")
	g.pf("            if (report.refused) {\n")
	g.pf("                for (int j = 0; j <= i; j++) { reset(values[j]); }\n")
	g.pf("                return 0;\n")
	g.pf("            }\n")
	// DAMAGE DISCARDS THE WHOLE BATCH (docs/SPEC-TABLES.md §4): a body that
	// does not read, or a batch whose bits do not end where the last body
	// does, delivers zero bodies, every body read so far reset.
	g.pf("            if (!read || report.malformed) {\n")
	g.pf("                report.malformed = true;\n")
	g.pf("                for (int j = 0; j <= i; j++) { reset(values[j]); }\n")
	g.pf("                return 0;\n")
	g.pf("            }\n")
	g.pf("        }\n")
	g.pf("        if (!r.align() || r.at != r.totalBits) {\n")
	g.pf("            report.malformed = true;\n")
	g.pf("            for (int j = 0; j < count; j++) { reset(values[j]); }\n")
	g.pf("            return 0;\n")
	g.pf("        }\n")
	g.pf("        return count;\n")
	g.pf("    }\n")
}
