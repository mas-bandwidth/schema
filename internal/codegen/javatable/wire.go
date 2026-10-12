// The ID-TABLE WIRE's Java runtime (docs/SPEC-TABLES.md §3), the form's
// foundation (schema#517).
//
// Java emitted NO table wire after the previous-form port was removed rather
// than carried (#761); the C++ reference and the compiler's own engine
// (internal/tablewire) carry the current form, and this file is the first piece
// of the Java port of it. It is deliberately the PRIMITIVES and not a codec:
// every body the form writes is built from these and nothing else, so pinning
// them first is what lets the codecs land over a form that already agrees with
// §3 at the byte level.
//
// WHAT THE FORM IS, and the one place each fact is written:
//
//   - the FORM BYTE is `1` and it is read FIRST, so a byte this reader does not
//     know is a REFUSAL by name and never damage;
//   - identity is `fnv1a64(name)` at SIXTY-FOUR bits, for a field, an enum
//     variant, a union arm and a table's own name alike — no fold, no rebound;
//   - every length, count, index and id reference is one CANONICAL LEB128, and
//     a spelling that could have been shorter is MALFORMED;
//   - the ID TABLE holds every id the body used, once each, in FIRST-USE order,
//     and the body names them by 1-BASED reference — reference `0` names no id;
//   - its ENTRY COUNT is the last eight bytes of the file, a fixed
//     little-endian u64, so a reader finds the table from the END;
//   - three kinds ride as numbers: `30` an enum carrying the reference to its
//     variant name's id, `31` the escape, `32` the payload-free arm;
//   - the reserved node-table id is `0xFFFFFFFFFFFFFFFF`.
//
// ONE PUBLIC TYPE PER FILE, Java's rule, so this is TableIds.java and its
// spelling is registered in internal/tablenames (docs/SPEC-TABLES.md §11).
package javatable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wireRuntimeFiles is the id-table wire's runtime, one file per public type.
func wireRuntimeFiles(u *ir.Unit) map[string][]byte {
	return map[string][]byte{
		"TableIds.java": javaFile(u, "the id-table wire's primitives — the form byte, fnv1a64 identity, canonical LEB128 and the first-use id table (docs/SPEC-TABLES.md §3).", tableWireSource),
	}
}

const tableWireSource = `// The ID-TABLE WIRE's primitives (docs/SPEC-TABLES.md §3). A body is
// ` + "`id reference, kind, payload`" + ` terminated by the ZERO reference, and every
// number in it is a CANONICAL LEB128. This class is the form's vocabulary, not
// a codec: a codec is generated per table above it.
//
// It reaches past no bounds and allocates nothing: the array belongs to the
// caller, and every method here either writes into it, reads from it, or
// answers a question about it.
public final class TableIds {
    private TableIds() {}

    /** The form byte, read FIRST (docs/SPEC-TABLES.md §3). A byte this reader
     *  does not know is a REFUSAL by name, never damage. */
    public static final int form = 1;

    /** The reserved node-table id: the node table rides in ONE field under it
     *  (§3), and its 64-bit L frames a numbering of any size. */
    public static final long reservedId = 0xffffffffffffffffL;

    /** The three kinds that ride with the form. */
    public static final int kindEnum = 30;         // an enum: the reference to its variant name's id
    public static final int kindEscape = 31;       // the escape
    public static final int kindNoPayloadArm = 32; // the payload-free arm

    /** FNV-1a's 64-bit basis and prime. Identity at SIXTY-FOUR bits, with no
     *  fold and no rebound (§3). */
    public static final long fnvOffset = 0xcbf29ce484222325L;
    public static final long fnvPrime = 0x100000001b3L;

    /** fnv1a64 over the UTF-8 bytes of a name — a field's id, an enum
     *  variant's, a union arm's and a table's own name id are all this. */
    public static long fnv1a64(String name) {
        long hash = fnvOffset;
        byte[] bytes = name.getBytes(java.nio.charset.StandardCharsets.UTF_8);
        for (byte b : bytes) {
            hash ^= (b & 0xffL);
            hash *= fnvPrime;
        }
        return hash;
    }

    /** the number of bytes a canonical LEB128 spelling of value needs. A
     *  length cannot be patched in place any more, so a body measures it before
     *  it rides (§3). */
    public static int lebSize(long value) {
        int n = 1;
        long v = value >>> 7;
        while (v != 0) {
            n++;
            v >>>= 7;
        }
        return n;
    }

    /** write a CANONICAL LEB128 and return the offset past it. */
    public static int putLeb(byte[] data, int at, long value) {
        long v = value;
        while (true) {
            int b = (int) (v & 0x7f);
            v >>>= 7;
            if (v != 0) {
                data[at++] = (byte) (b | 0x80);
            } else {
                data[at++] = (byte) b;
                return at;
            }
        }
    }

    /** read one CANONICAL LEB128 at at, bounded by limit, into value[0], and
     *  return the number of bytes read. A NON-MINIMAL spelling is MALFORMED
     *  and returns 0: the last byte of a multi-byte spelling that is zero means
     *  the value had a shorter one (§3). A decode that runs past limit, or past
     *  sixty-four bits, returns 0 as well. */
    public static int getLeb(byte[] data, int at, int limit, long[] value) {
        long result = 0;
        int shift = 0;
        int start = at;
        while (true) {
            if (at >= limit) {
                value[0] = -1;
                return 0;
            }
            int b = data[at++] & 0xff;
            result |= (long) (b & 0x7f) << shift;
            if ((b & 0x80) == 0) {
                if (at - start > 1 && b == 0) {
                    value[0] = -1;
                    return 0;
                }
                value[0] = result;
                return at - start;
            }
            shift += 7;
            if (shift > 63) {
                value[0] = -1;
                return 0;
            }
        }
    }

    /** the id table's FIRST-USE rule: the 1-based reference to id, adding it
     *  when it is new. Reference 0 names no id, so a returned reference is
     *  never zero. */
    public static int reference(long id, long[] ids, int[] count) {
        for (int i = 0; i < count[0]; i++) {
            if (ids[i] == id) {
                return i + 1;
            }
        }
        ids[count[0]++] = id;
        return count[0];
    }

    /** the id a 1-based reference names, or 0 for reference 0. */
    public static long idAt(long[] ids, int count, int reference) {
        if (reference <= 0 || reference > count) {
            return 0;
        }
        return ids[reference - 1];
    }

    /** the id table's entry count: the LAST EIGHT BYTES of the file, a fixed
     *  little-endian u64 — the reader finds the table from the END (§3). A file
     *  shorter than eight bytes has no table and answers -1. */
    public static long idTableCount(byte[] data, int length) {
        if (length < 8) {
            return -1;
        }
        long count = 0;
        int at = length - 8;
        for (int i = 0; i < 8; i++) {
            count |= (data[at + i] & 0xffL) << (8 * i);
        }
        return count;
    }
}
`
