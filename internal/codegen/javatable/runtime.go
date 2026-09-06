// The unit's shared TABLE-WIRE runtime in Java, one PUBLIC TYPE PER FILE.
//
// C# emits the same surface once per unit into a home file, and C++ emits it
// into every header behind an include guard. Java has a third rule and it
// settles the question: a public type lives in a file of its own name, so
// "where does the shared runtime live" has one answer per type and no file
// order can move it.
//
// Every spelling here is a PACKAGE-LEVEL name and every one of them is
// registered in internal/tablenames, which is what makes the §11 promise hold
// for this backend: a schema declaring `TableReport` in a unit with tables is
// refused by the checker rather than generating a second TableReport.java.
package javatable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// javaFile wraps one generated Java source in the banner every file carries.
func javaFile(u *ir.Unit, name, summary, body string) []byte {
	var b strings.Builder
	b.WriteString(generatedFrom("", u))
	b.WriteString(license)
	fmt.Fprintf(&b, "// package %s — %s\n\n", u.Package, summary)
	fmt.Fprintf(&b, "package %s;\n\n", u.Package)
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	_ = name
	return []byte(b.String())
}

// wireRuntimeFiles is the whole shared wire runtime, one file per type.
func wireRuntimeFiles(u *ir.Unit, anyKeyed bool) map[string][]byte {
	out := map[string][]byte{
		"TableReport.java":       javaFile(u, "TableReport", "the TABLE wire's read report (docs/SPEC-TABLES.md §4).", tableReportSource),
		"TableWriter.java":       javaFile(u, "TableWriter", "the TABLE wire's writer over the caller's array (docs/SPEC-TABLES.md §3).", tableWriterSource),
		"TableReader.java":       javaFile(u, "TableReader", "the TABLE wire's reader over the caller's array (docs/SPEC-TABLES.md §3).", tableReaderSource),
		"TableTypeInfo.java":     javaFile(u, "TableTypeInfo", "a table's reflection descriptor (docs/SPEC-TABLES.md §8).", tableTypeInfoSource),
		"TableFieldInfo.java":    javaFile(u, "TableFieldInfo", "a field's reflection descriptor (docs/SPEC-TABLES.md §8).", tableFieldInfoSource),
		"TableDocNone.java":      javaFile(u, "TableDocNone", "the one shared empty doc (docs/SPEC-TABLES.md §8.1).", tableDocNoneSource),
		"TableUnionInfo.java":    javaFile(u, "TableUnionInfo", "a union field's tag and its arms (docs/SPEC-TABLES.md §8.1).", tableUnionInfoSource),
		"TableUnionArmInfo.java": javaFile(u, "TableUnionArmInfo", "one union arm's payload and descriptor (docs/SPEC-TABLES.md §8.1).", tableUnionArmInfoSource),
	}
	if anyKeyed {
		out["TableKeyed.java"] = javaFile(u, "TableKeyed", "an enum-keyed array's slot rule (docs/SPEC-TABLES.md §2.4).", tableKeyedSource)
	}
	return out
}

const tableReportSource = `// The table-wire read report — the permissive contract's ledger. Silence (all
// zero) means the data matched this reader's schema exactly.
//
// The report is an object the CALLER owns: it is passed in, filled in place and
// read afterwards, so a hot loop keeps one and never allocates for a decode.
public final class TableReport {
    public int unknown;        // unknown field ids skipped (newer data)
    public int kindMismatch;   // known id, changed type — skipped, never misdecoded
    public int clamped;        // out-of-range values clamped to declared bounds
    // duplicate is the TEXT FORM's counter and the WIRE NEVER RAISES IT
    // (docs/SPEC-TABLES.md §4, §16.2): a body carrying an id twice is legal input
    // whose last occurrence wins, silently. It rides on this class because a
    // caller has one report type, not two — so a wire read always leaves it
    // zero, and <Name>FromJson is what raises it.
    public int duplicate;
    public boolean malformed;  // framing damage; decode stopped, partial result kept

    /** Back to silence, in place — so one report serves a whole loop. */
    public TableReport clear() {
        unknown = 0;
        kindMismatch = 0;
        clamped = 0;
        duplicate = 0;
        malformed = false;
        return this;
    }
}
`

const tableWriterSource = `// The wire writer over the CALLER's array: the bytes are written in place and
// nothing here allocates. A caller with a hot loop keeps one writer and calls
// reset before each save.
public final class TableWriter {
    public byte[] buffer;
    public int offset;   // the next byte to write, absolute in buffer
    public int limit;    // one past the last byte this writer may touch
    public boolean overflow;

    public TableWriter() {
        this.buffer = null;
        this.offset = 0;
        this.limit = 0;
        this.overflow = false;
    }

    public TableWriter(byte[] buffer, int offset, int length) {
        reset(buffer, offset, length);
    }

    public TableWriter reset(byte[] buffer, int offset, int length) {
        this.buffer = buffer;
        this.offset = offset;
        this.limit = offset + length;
        this.overflow = false;
        return this;
    }

    public void raw(byte[] data, int from, int count) {
        if (count < 0 || offset + count > limit) { overflow = true; return; }
        System.arraycopy(data, from, buffer, offset, count);
        offset += count;
    }

    public void put8(int v) {
        if (offset + 1 > limit) { overflow = true; return; }
        buffer[offset] = (byte) v;
        offset += 1;
    }

    public void put16(int v) {
        if (offset + 2 > limit) { overflow = true; return; }
        buffer[offset] = (byte) v;
        buffer[offset + 1] = (byte) (v >>> 8);
        offset += 2;
    }

    public void put32(int v) {
        if (offset + 4 > limit) { overflow = true; return; }
        buffer[offset] = (byte) v;
        buffer[offset + 1] = (byte) (v >>> 8);
        buffer[offset + 2] = (byte) (v >>> 16);
        buffer[offset + 3] = (byte) (v >>> 24);
        offset += 4;
    }

    public void put64(long v) {
        put32((int) v);
        put32((int) (v >>> 32));
    }

    public void patch32(int at, int v) {
        if (at + 4 > limit) { overflow = true; return; }
        buffer[at] = (byte) v;
        buffer[at + 1] = (byte) (v >>> 8);
        buffer[at + 2] = (byte) (v >>> 16);
        buffer[at + 3] = (byte) (v >>> 24);
    }
}
`

const tableReaderSource = `// The wire reader over the CALLER's array. A nested body is bounded by MOVING
// THE LIMIT rather than by slicing a sub-reader: C# and C++ carry a ref struct
// or a span pair on the stack, and Java has neither, so a sliced reader would
// be one allocation per nested body on the read path. The limit is saved,
// narrowed, and restored by the generated code around every nested decode, so
// an inner decode can never reach past its own framing — the same guarantee,
// with nothing allocated.
//
// A caller with a hot loop keeps one reader and calls reset before each load.
public final class TableReader {
    public byte[] buffer;
    public int offset;  // the next byte to read, absolute in buffer
    public int limit;   // one past the last byte this reader may touch
    public TableReport report;

    public TableReader() {
        this.buffer = null;
        this.offset = 0;
        this.limit = 0;
        this.report = null;
    }

    public TableReader(byte[] buffer, int offset, int length, TableReport report) {
        reset(buffer, offset, length, report);
    }

    public TableReader reset(byte[] buffer, int offset, int length, TableReport report) {
        this.buffer = buffer;
        this.offset = offset;
        this.limit = offset + length;
        this.report = report;
        return this;
    }

    public boolean has(long bytes) { return bytes >= 0 && offset + bytes <= limit; }

    public int get8() { return buffer[offset++] & 0xff; }

    public int get16() {
        int v = (buffer[offset] & 0xff) | ((buffer[offset + 1] & 0xff) << 8);
        offset += 2;
        return v;
    }

    /** the four bytes as a bit-transparent int; an unsigned reading is get32() & 0xffffffffL */
    public int get32() {
        int v = (buffer[offset] & 0xff) | ((buffer[offset + 1] & 0xff) << 8) |
                ((buffer[offset + 2] & 0xff) << 16) | ((buffer[offset + 3] & 0xff) << 24);
        offset += 4;
        return v;
    }

    public long get64() {
        long lo = get32() & 0xffffffffL;
        long hi = get32() & 0xffffffffL;
        return lo | (hi << 32);
    }

    /** skip one payload by kind; false = framing damage */
    public boolean skip(int kind) {
        switch (kind) {
            case 1: case 2: case 6:
                if (!has(1)) { return false; }
                offset += 1;
                return true;
            case 3: case 7:
                if (!has(2)) { return false; }
                offset += 2;
                return true;
            // 17 is a NODE INDEX (docs/SPEC-TABLES.md §3.1): four bytes, so it costs
            // one row here and a reader without the kind still skips a pointer field
            case 4: case 8: case 10: case 17:
                if (!has(4)) { return false; }
                offset += 4;
                return true;
            case 5: case 9: case 11:
                if (!has(8)) { return false; }
                offset += 8;
                return true;
            case 12: case 13: case 14: case 16: {
                if (!has(4)) { return false; }
                long n = get32() & 0xffffffffL;
                if (!has(n)) { return false; }
                offset += (int) n;
                return true;
            }
            case 15: { // union: u16 arm id, then the arm length-prefixed (id 0 = empty, no body)
                if (!has(2)) { return false; }
                if (get16() == 0) { return true; }
                if (!has(4)) { return false; }
                long n = get32() & 0xffffffffL;
                if (!has(n)) { return false; }
                offset += (int) n;
                return true;
            }
            default:
                return false;
        }
    }
}
`

// tableKeyedSource is the enum-keyed array's SLOT RULE, and that is all it is.
//
// C# spells the storage itself — a generic TableKeyed<T, E> whose indexer takes
// the key and whose enumerator yields it. Java cannot: a generic class over a
// primitive element boxes on every slot, and an enum-keyed array of int32 is
// exactly that case, so the per-frame garbage the C# port was written to avoid
// would arrive here through the generic. So the STORAGE is a plain typed array
// on the owning class — `int[] scores`, one slot per named variant, the key k
// at index k-1 — and the SURFACE the shift lives behind is a pair of accessors
// generated beside it plus the three helpers here. No call site in any language
// spells the shift, which is the property §2.4 actually asks for.
const tableKeyedSource = `// An ENUM-KEYED array's slot rule (docs/SPEC-TABLES.md §2.4). The storage is a
// plain array on the owning class with E.max slots, ONE PER NAMED VARIANT, and
// the key k at index k-1 — the storage SHIFTS LEFT and nothing is stored for
// None. This is the ONE place that shift is spelled.
//
// NOTHING OUTSIDE THE ARRAY NAMES ITS SIZE: the extent is the key enum's own
// max member, named once at the array's construction and nowhere else.
//
// NONE IS THE NULL KEY: it names no slot, it never rides on the wire, a stored
// key of 0 is malformed, and INDEXING BY IT IS AN ERROR — a throw from slot(),
// which stands in every build exactly as the C++ abort does.
//
// ITERATION is the surface a consumer of the WHOLE array wants, and it is a
// counted loop over KEYS rather than over storage indices:
//
//     for (int i = 0; i < TableKeyed.count(hull.turrets); i++) {
//         int key = TableKeyed.key(i);
//         WeaponConfig turret = hull.turrets(key);
//     }
//
// so no caller spells a bound, a lower limit or the shift, and nothing
// allocates.
public final class TableKeyed {
    private TableKeyed() {}

    /** the STORAGE index the key k names — k - 1, and None names none. */
    public static int slot(int key) {
        if (key == 0) {
            throw new IllegalArgumentException(
                    "None is the null key of an enum-keyed array: it keys no slot");
        }
        return key - 1;
    }

    /** the KEY the storage index i holds — i + 1. */
    public static int key(int slot) { return slot + 1; }

    public static int count(Object[] slots) { return slots.length; }
    public static int count(boolean[] slots) { return slots.length; }
    public static int count(byte[] slots) { return slots.length; }
    public static int count(short[] slots) { return slots.length; }
    public static int count(int[] slots) { return slots.length; }
    public static int count(long[] slots) { return slots.length; }
    public static int count(float[] slots) { return slots.length; }
    public static int count(double[] slots) { return slots.length; }
}
`

// tableDocNoneSource is the unit's ONE empty doc, and it is a class of its own
// file because that is where this backend puts a unit-level constant —
// BuildVersion's home is the same one, for the same package-scope reason.
const tableDocNoneSource = `// THE SHARED EMPTY DOC (docs/SPEC-TABLES.md §8.1): a declaration with no ///
// block carries a doc column naming this one constant, so absence costs a unit
// no string data and a printer concatenates doc columns with no null test. Every
// absent doc in the unit — a field's and a type's alike — is this definition,
// and no descriptor row spells an empty literal of its own.
public final class TableDocNone {
    private TableDocNone() {}

    public static final String value = "";
}
`

// tableFieldInfoSource is §8's field descriptor, in Java's own currency.
//
// C++ locates a field with an offset and a width, because its storage is one
// flat struct; a Java field has no offset and a Java object has no sizeof, so
// the descriptor carries the reader and the writer the emitter wrote instead —
// the same divergence the C# port states, for the same reason. The accessors
// are built once with the descriptor and cached with it, so a walk allocates
// nothing.
const tableFieldInfoSource = `// One field's reflection descriptor (docs/SPEC-TABLES.md §8): name, wire id and
// kind, bounds, ranges, the enum/union vocabulary and its wire ids, branch
// guards — enough to walk, print, diff or bind any table value at runtime with
// no schema files on hand.
//
// THE MEMORY COLUMNS ARE SPELLED AS ACCESSORS, and that is the whole of this
// surface's divergence from C++'s. A Java field has no offset and a Java object
// has no meaningful sizeof, so the descriptor carries the reader and the writer
// the emitter wrote. Same ROLE, one place, in the language's own currency: a
// generic walker — the text form (§16) — reaches storage through these and
// through nothing else.
//
// The accessors are built once, with the descriptor, and cached with it. They
// take the owning instance as an Object, which is a reference for every storage
// class this backend emits, and the raw value crosses as a long, which the
// interfaces below carry unboxed. Nothing on a walk allocates.
public final class TableFieldInfo {
    /** one NUMERIC element, read: an integer sign-extended into the long, a bool as
     *  0 or 1, an enum or a flags mask as its value, a float as its IEEE-754 bit
     *  pattern. The int is the element index — the array slot, or a keyed array's
     *  STORAGE index — and 0 for a field that is not an array. */
    public interface RawGet { long get(Object owner, int index); }
    /** its inverse. */
    public interface RawSet { void set(Object owner, int index, long raw); }
    /** the OBJECT a nested table, a union or a class-typed element is stored as. */
    public interface ChildGet { Object get(Object owner, int index); }
    /** a string(N)'s or bytes(N)'s backing array. */
    public interface BufferGet { byte[] get(Object owner); }
    /** a counted companion: a string's length, a bytes' length, a counted array's count. */
    public interface CountGet { int get(Object owner); }
    public interface CountSet { void set(Object owner, int count); }
    /** an optional's presence bool. */
    public interface PresentGet { boolean get(Object owner); }
    public interface PresentSet { void set(Object owner, boolean present); }
    /** a vocabulary entry's name: an enum value, a union tag, a flags bit. */
    public interface NameOf { String of(long value); }
    /** a vocabulary entry's TABLE-WIRE id (docs/SPEC-TABLES.md §5); 0 is the reserved id. */
    public interface IdOf { int of(long value); }
    /** the nested table's descriptor, held lazily so a descriptor graph needs no
     *  initialization order: a table may name one declared later. */
    public interface TypeRef { TableTypeInfo get(); }

    public String name;        // schema field name, e.g. "health"
    public String json;        // the TEXT form's key: the json = "key" attribute, else name (§16.4)
    public String typeName;    // schema type name, e.g. "float32", "Grade"
    public int id;             // table-wire field id (name hash; the was alias's hash after a rename)
    public int kind;           // table-wire kind; for arrays/strings/bytes, the ELEMENT kind
    public boolean isArray;    // fixed or counted array (bytes included)
    public boolean counted;    // a <name>Count/<name>Length companion exists
    public boolean optional;   // a ?T field: a <name>Present bool decides whether it rides
    public int arrayBound;     // array capacity / string max length; 0 for plain scalars
    // the STORAGE width of one element in bytes, C++'s elem_size where it has a
    // Java meaning: the last bound a numeric read clamps to (§16.2). 0 on every
    // kind whose storage is not a fixed-width number.
    public int elemWidth;
    public boolean hasRange;   // a declared [min, max] (int or float)
    public double rangeMin;    // NOTE: int64 ranges beyond 2^53 lose precision here
    public double rangeMax;
    public long enumMax;       // enums: highest valid value (None = 0 always valid);
                               // unions: the arm count (tag range [0, enumMax]); else -1
    public NameOf enumName;    // enums: value -> name; unions: tag -> arm name; else null
    // the TABLE-WIRE id of one variant (docs/SPEC-TABLES.md §5): for an enum, the
    // hash of the variant's name; for a union, the hash of the arm's name. 0 is
    // the reserved id — an enum's None, a union's empty. null for every other
    // kind. Walk [0, enumMax] to enumerate a vocabulary and its ids.
    public IdOf variantId;

    // an ENUM-KEYED array (docs/SPEC-TABLES.md §2.4, §8): the array has one slot per
    // variant of keyTypeName, indexed by the variant's value, and its slots ride
    // under variant ids rather than positions. keyName and keyId are the key's
    // vocabulary — walk [0, arrayBound) to print slots by name. SLOT 0 IS NONE'S
    // AND IS NEVER VALID: keyId(0) is 0, the one reserved id no declared name can
    // hold, and keyName(0) is "None", so a walker enumerating slots skips it
    // rather than printing a None row. All three are null on every other field.
    public String keyTypeName;
    public NameOf keyName;
    public IdOf keyId;

    public String guard;       // branch guard, e.g. "at_rest" or "!at_rest"; "" if unguarded

    public TypeRef tableRef;   // the nested table's descriptor, or null
    public TableTypeInfo table() { return tableRef == null ? null : tableRef.get(); }

    // a UNION field's shape (docs/SPEC-TABLES.md §8.1): the tag, and each arm's
    // payload by its own descriptor. A VALUE rather than C++'s factory — the
    // laziness a descriptor graph needs lives in each arm's tableRef — and null
    // on every other kind, which is what tells an enum field from a union one:
    // both carry a value -> name function and a variant id.
    public TableUnionInfo arms;

    // what a PERSON wrote about the field (docs/SPEC-TABLES.md §8.1): the ///
    // block above it, verbatim (SPEC §4.1) — TableDocNone.value when there is
    // none, never null — and its tags (SPEC §4.2) in declared order, 0 and null
    // when there are none. Constant data built with the descriptor, so a walk
    // that prints every doc and every tag allocates nothing.
    public String doc;
    public int numTags;
    public String[] tags;

    // ---- the storage location, in Java's own currency ----
    public RawGet getRaw;
    public RawSet setRaw;
    public ChildGet getChild;
    public BufferGet getBuffer;
    public CountGet getCount;
    public CountSet setCount;
    public PresentGet getPresent;
    public PresentSet setPresent;
}
`

const tableUnionArmInfoSource = `// One union arm's payload (docs/SPEC-TABLES.md §8.1). Arms run [0, enumMax];
// index 0 is the EMPTY arm and carries neither payload nor descriptor.
public final class TableUnionArmInfo {
    /** the arm's storage, given the union's. */
    public interface PayloadOf { Object of(Object union); }

    public TableFieldInfo.TypeRef tableRef;
    public TableTypeInfo table() { return tableRef == null ? null : tableRef.get(); }
    public PayloadOf payload;
}
`

const tableUnionInfoSource = `// A union field's shape: the tag, and the arms indexed by it. C++ carries the
// tag's offset and width; Java carries the pair that reads and writes it, for
// the reason TableFieldInfo's accessors exist.
public final class TableUnionInfo {
    public interface TagGet { long of(Object union); }
    public interface TagSet { void set(Object union, long tag); }

    public TagGet getTag;
    public TagSet setTag;
    public TableUnionArmInfo[] arms;
}
`

const tableTypeInfoSource = `// One table's reflection descriptor (docs/SPEC-TABLES.md §8).
public final class TableTypeInfo {
    /** put one instance back at its declared defaults, in place. A generic walker
     *  that FILLS a value has to establish the defaults an absent field takes, and
     *  it holds no type to spell — this is the one thing the columns could not
     *  express without a function (§8.1). It calls <name>Reset, the same prefill the
     *  wire's read path calls. */
    public interface Reset { void reset(Object value); }

    public String name;        // schema type name
    public int numFields;
    public TableFieldInfo[] fields;
    public Reset reset;

    // the declaration's own doc and tags, on the same terms as a field's
    // (docs/SPEC-TABLES.md §8.1)
    public String doc;
    public int numTags;
    public String[] tags;
}
`
