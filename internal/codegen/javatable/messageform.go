// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3), form byte 2: a batch of
// bitpacked bodies under one announced vocabulary. This file carries the
// ANNOUNCEMENT surface of that form — the compile-time constant byte array the
// page names (§3.3, "the announcement is a compile-time constant byte array
// with a length") and its reader, `announce`, `announceMeasure` and
// `announceRead` — and the resolved-vocabulary storage `AnnounceRead` fills
// ("THE RESOLVED VOCABULARY'S STORAGE IS THE CALLER'S, DECLARED BY CAPACITY",
// §3.3). The batch surface (`measureMessages`, `saveMessages`,
// `loadMessages`) is a later card.
//
// THE FORM IS A FIXED TABLE'S (§3.3), so this surface is emitted for a unit
// with a `fixed table` root and for no other: a message has to step through a
// shape the declaration fixes.
//
// JAVA's one-public-type-per-file rule (docs/SPEC-TABLES.md, "JAVA emits ...")
// puts the whole announcement surface in ONE file: the resolved entry, its
// shape and the read report are nested static types of TableVocabulary, where
// C++ and C# spend a type of their own, because Java has no unit class to hang
// package-level helpers on and a nested type claims one name instead of four.
package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// messageAnnouncementClass is the one public type the announcement surface
// lives in. Java has no unit class to hang a package-level function on, so
// `announce`, `announceMeasure` and `announceRead` are static members of
// TableVocabulary, and the entry, shape and report types are nested inside it.
const messageAnnouncementClass = "TableVocabulary"

// generateMessageFiles emits the announcement surface for a unit with a fixed
// root. Without one there is no message form to read (§3.3) and the file is
// not written.
func generateMessageFiles(u *ir.Unit) (map[string][]byte, error) {
	if len(fixedRoots(u)) == 0 {
		return map[string][]byte{}, nil
	}
	body := strings.Replace(tableMessageSurface, "%%ANNOUNCEMENT%%", javaByteArrayLiteral(ir.TableAnnouncement(u)), 1)
	return map[string][]byte{
		messageAnnouncementClass + ".java": javaFile(u, "the MESSAGE FORM's announcement surface (docs/SPEC-TABLES.md §3.3), form byte 2", body),
	}, nil
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
const tableMessageSurface = `
// The announcement surface of the MESSAGE FORM (docs/SPEC-TABLES.md §3.3),
// form byte 2. ` + "`Announce`" + ` and ` + "`announceMeasure`" + ` are the unit's
// compile-time constant and its length; ` + "`announceRead`" + ` parses a peer's
// announcement into the caller's resolved vocabulary. The batch surface is a
// later card.
public final class TableVocabulary {
    public TableVocabulary() {}

    // THE WHOLE ANNOUNCEMENT IS A COMPILE-TIME CONSTANT OF THE UNIT, every byte
    // of it settled by the compiler, so a backend emits ` + "`Announce`" + ` and
    // ` + "`announceMeasure`" + ` as a constant byte array and its length (§3.3).
    static final byte[] tableAnnouncement = %%ANNOUNCEMENT%%;

    public static int announceMeasure() { return tableAnnouncement.length; }

    // Copy the announcement into the caller's buffer and answer its length, or
    // -1 when the buffer is too small (the C++ reference's ` + "`-1`" + `, §3.3).
    public static int announce(byte[] buffer) {
        if (buffer == null || buffer.length < tableAnnouncement.length) { return -1; }
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

    // The read report, pared to what the announcement surface names: the strict
    // checks answer ` + "`malformed`" + ` and the refusals answer a reason.
    public static final class Report {
        public boolean malformed;
        public boolean refused;
        public String reason;
        public int unknown;
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
    // after it — refused or not — is ` + "`second_announcement`" + `.
    public static boolean announceRead(TableVocabulary v, byte[] data, Report report) {
        if (report == null) { report = new Report(); }
        if (v.announced || v.refused) {
            report.refused = true;
            report.reason = "second_announcement";
            return false;
        }
        boolean ok = announceReadInner(v, data, report);
        if (!ok) { v.refused = true; }
        return ok;
    }

    // THE ANNOUNCEMENT IS AN ORDINARY FORM-1 FILE (§3.3), so it is read by the
    // same three parts a file is: the form byte, the root body of fields, and
    // the trailer of ids. The two strict checks are the two facts an
    // announcement's body must carry, and a failed one is MALFORMED, not a
    // refusal (docs/SPEC-TABLES.md §3.3).
    private static boolean announceReadInner(TableVocabulary v, byte[] data, Report report) {
        if (data == null || data.length == 0) { report.malformed = true; return false; }
        if ((data[0] & 0xff) != 1) {
            report.refused = true;
            report.reason = (data[0] & 0xff) == 2 ? "message_form_as_file" : "newer_form";
            return false;
        }
        if (data.length < 9) { report.malformed = true; return false; }
        long entryCount = readU64(data, data.length - 8);
        if (entryCount < 0 || entryCount > (data.length - 9) / 8) { report.malformed = true; return false; }
        int idsStart = data.length - 8 - (int) entryCount * 8;
        Reader r = new Reader(data, 1);
        int versionCount = 0;
        int vocabCount = 0;
        byte[] vocab = null;
        while (true) {
            long ref = r.leb();
            if (r.err) { report.malformed = true; return false; }
            if (ref == 0) { break; }
            if (Long.compareUnsigned(ref, entryCount) > 0) { report.malformed = true; return false; }
            long id = readU64(data, idsStart + (int) (ref - 1) * 8);
            int kind = r.u8();
            if (r.err) { report.malformed = true; return false; }
            if (id == -2L) { // the reserved build-version id, 0xFFFFFFFFFFFFFFFE
                if (kind != 9 || !r.has(8)) { report.malformed = true; return false; }
                v.buildVersion = r.u64();
                versionCount++;
            } else if (id == -3L) { // the reserved vocabulary id, 0xFFFFFFFFFFFFFFFD
                if (kind != 14) { report.malformed = true; return false; }
                long len = r.leb();
                if (r.err || len < 0 || len > r.buf.length - r.at) { report.malformed = true; return false; }
                int bodyStart = r.at;
                Reader sub = new Reader(r.buf, bodyStart);
                int elem = sub.u8();
                if (sub.err || elem != 6) { report.malformed = true; return false; }
                long n = sub.leb();
                if (sub.err || n < 0 || n != len - (sub.at - bodyStart)) { report.malformed = true; return false; }
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
                if (!skip(r, kind)) { report.malformed = true; return false; }
            }
        }
        if (r.at != idsStart) { report.malformed = true; return false; }
        // THE TWO STRICT CHECKS: the build version present exactly once, and the
        // vocabulary present exactly once. A failed check says the bytes are not
        // an announcement rather than that this peer declined to announce (§3.3).
        if (versionCount != 1 || vocabCount != 1) { report.malformed = true; return false; }
        // Resolve the vocabulary once: every entry an id, a kind and a shape.
        // The ENTRY bound is the caller's capacity, and an announcement above it
        // is refused by name as vocabulary_too_large before an entry is touched
        // (§3.3).
        Reader src = new Reader(vocab, 0);
        int count = 0;
        boolean nodeSeen = false;
        while (src.at < vocab.length) {
            if (count >= v.entries.length) {
                report.refused = true;
                report.reason = "vocabulary_too_large";
                return false;
            }
            Entry e = new Entry();
            if (!entryRead(src, e) || e.id == -3L || e.id == -2L || (e.id == -1L && nodeSeen)) {
                report.malformed = true;
                return false;
            }
            if (e.id == -1L) { nodeSeen = true; }
            // TWO ENTRIES THAT AGREE ON ALL THREE PARTS ARE MALFORMED (§3.3).
            for (int i = 0; i < count; i++) {
                if (v.entries[i].id == e.id && v.entries[i].kind == e.kind && sameShape(v.entries[i], e)) {
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
        Reader(byte[] buf, int at) { this.buf = buf; this.at = at; }
        boolean has(int n) { return at + n <= buf.length; }
        int u8() { if (!has(1)) { err = true; return 0; } return buf[at++] & 0xff; }
        long u32() {
            if (!has(4)) { err = true; return 0; }
            long v = 0;
            for (int i = 0; i < 4; i++) { v |= (long) (buf[at + i] & 0xff) << (8 * i); }
            at += 4;
            return v;
        }
        long u64() {
            if (!has(8)) { err = true; return 0; }
            long v = 0;
            for (int i = 0; i < 8; i++) { v |= (long) (buf[at + i] & 0xff) << (8 * i); }
            at += 8;
            return v;
        }
        long leb() {
            long v = 0;
            for (int i = 0; i < 10; i++) {
                if (!has(1)) { err = true; return 0; }
                int b = buf[at++] & 0xff;
                if (i == 9 && (b & 0xfe) != 0) { err = true; return 0; }
                v |= (long) (b & 0x7f) << (7 * i);
                if ((b & 0x80) == 0) {
                    if (i > 0 && b == 0) { err = true; return 0; } // a longer spelling of the same number
                    return v;
                }
            }
            err = true;
            return 0;
        }
    }

    static long readU64(byte[] buf, int at) {
        long v = 0;
        for (int i = 0; i < 8; i++) { v |= (long) (buf[at + i] & 0xff) << (8 * i); }
        return v;
    }

    static int bitCount(long n) {
        if (n <= 0) { return 0; }
        return 64 - Long.numberOfLeadingZeros(n);
    }

    static boolean knownKind(int kind) { return kind <= 33; }

    static int kindWidth(int kind) {
        switch (kind) {
            case 1: case 2: case 6: case 20: case 25: return 1;
            case 3: case 7: case 21: case 26: return 2;
            case 4: case 8: case 10: case 17: case 22: case 27: return 4;
            case 5: case 9: case 11: case 23: case 28: return 8;
            case 18: case 19: case 24: case 29: return 16;
            default: return 0;
        }
    }

    // Skip an unknown field by its announced kind (§3.3): a tolerant reader
    // steps over a field it cannot name exactly, by the width the kind states.
    static boolean skip(Reader r, int kind) {
        int w = kindWidth(kind);
        if (w > 0) { if (!r.has(w)) { return false; } r.at += w; return true; }
        switch (kind) {
            case 12: case 13: case 14: { // string, table, array: a length then its bytes
                long n = r.leb();
                if (r.err || n < 0 || n > r.buf.length - r.at) { return false; }
                r.at += (int) n;
                return true;
            }
            default: return false;
        }
    }

    static Shape shapeRead(Reader r, int kind) {
        Shape s = new Shape();
        s.kind = kind;
        if ((kind >= 2 && kind <= 10) || (kind >= 18 && kind <= 29)) {
            if (!r.has(1)) { return null; }
            s.packing = r.u8();
            if (s.packing == 0) { s.bits = (long) kindWidth(kind) * 8; return s; }
            if (s.packing == 1 && kind != 10) {
                long bits = r.leb();
                if (r.err || bits > (long) kindWidth(kind) * 8) { return null; }
                s.bits = bits;
                if (kind >= 18) {
                    if (!r.has(16)) { return null; }
                    s.baseLo = r.u64();
                    s.baseHi = r.u64();
                } else {
                    long base = r.leb();
                    if (r.err) { return null; }
                    if (kind >= 2 && kind <= 5) { base = (base >> 1) ^ -(base & 1); } // zigzag, the signed kind's base
                    s.baseLo = base;
                }
                return s;
            }
            if (s.packing == 2 && kind == 10) {
                if (!r.has(12)) { return null; }
                s.qMin = Float.intBitsToFloat((int) r.u32());
                s.qMax = Float.intBitsToFloat((int) r.u32());
                s.qRes = Float.intBitsToFloat((int) r.u32());
                if (!(s.qMin < s.qMax) || !(s.qRes > 0)) { return null; }
                s.qDelta = s.qMax - s.qMin;
                float values = s.qDelta / s.qRes;
                // A TRIPLE WHOSE delta OR delta/res IS NOT FINITE IN FLOAT32 is a
                // hostile width like any other (§3.3): refused, no vocabulary.
                if (!Float.isFinite(s.qDelta) || !Float.isFinite(values)) { return null; }
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
            if (r.err || max > 0x7fffffffL) { return null; }
            s.max = max;
            return s;
        }
        if (kind == 14 || kind == 16) {
            if (kind == 14) {
                long min = r.leb();
                if (r.err || min > 0xffffffffL) { return null; }
                s.min = min;
            }
            long max = r.leb();
            if (r.err || max > 0xffffffffL || max < s.min) { return null; }
            s.max = max;
            return s;
        }
        return s;
    }

    static boolean entryRead(Reader r, Entry e) {
        if (!r.has(9)) { return false; }
        e.id = r.u64();
        int kind = r.u8();
        if (!knownKind(kind)) { return false; }
        e.kind = kind;
        e.shape = shapeRead(r, kind);
        if (e.shape == null) { return false; }
        if (kind == 14 || kind == 16) {
            if (!r.has(1)) { return false; }
            int elem = r.u8();
            if (!knownKind(elem) || elem == 12 || elem == 33) { return false; }
            e.element = shapeRead(r, elem);
            if (e.element == null) { return false; }
            if (elem == 14 || elem == 16) {
                if (!r.has(1)) { return false; }
                int inner = r.u8();
                if (!knownKind(inner) || inner == 12 || inner == 33) { return false; }
            }
        }
        return !r.err;
    }

    static boolean sameShape(Entry a, Entry b) {
        if (!sameShapeFacts(a.shape, b.shape)) { return false; }
        if (a.element == null || b.element == null) { return a.element == b.element; }
        return sameShapeFacts(a.element, b.element);
    }

    static boolean sameShapeFacts(Shape a, Shape b) {
        return a.kind == b.kind && a.packing == b.packing && a.bits == b.bits
            && a.baseLo == b.baseLo && a.baseHi == b.baseHi && a.min == b.min
            && a.max == b.max && a.qMin == b.qMin && a.qMax == b.qMax
            && a.qRes == b.qRes && a.steps == b.steps;
    }

    // docs/SPEC.md Floating-point discipline and docs/FIXED-FORM-ALGORITHM.md §5.1.
    // The product rounds to float32 before the add. Java does not contract a
    // multiply-add, so these bits are the unfused result.
    public static int dequantizeBits(int index, float qmin, float delta, int count) {
        if (index < 0) { index = 0; }
        if (index > count) { index = count; }
        float normalized = (float) index / (float) count;
        float scaled = normalized * delta;
        return Float.floatToRawIntBits(scaled + qmin);
    }
}
`
