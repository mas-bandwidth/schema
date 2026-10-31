// THE COOKED FORM in Java (docs/SPEC-TABLES.md §7): reader and writer, emitted ON THE
// SIDE into <Table>Cook.java.
//
// A cook is a LOAD-TRUSTED-DATA-FROM-TOOLS format, not a wire. Tooling writes a
// region for one build version and that build reads it: the header is matched,
// the root is the record at the data part's base, and nothing else happens. No
// walk, no fix-up, no allocation — which is what makes Open O(1) in the file's
// size, the bar the scale §7 is built for asks for.
//
// NOTHING DECLARES IT, exactly as nothing declares the block form. Every table
// gets an Open, and a consumer compiles this file only if it opens a cook.
//
// A COOKED RECORD IS THE BLITTABLE ROW. The region is laid out by §20.3's C ABI
// model, which is the same model <Name>Row's accessors read at — so the two
// accelerators share one set of accessors rather than growing a second ABI.
//
// WHERE JAVA DIVERGES, and it is one place: a REFERENCE IS BOUNDS-CHECKED. C++
// and C# hand back a pointer and let the walk decide, because a cook is trusted
// input and an out-of-region delta there is undefined behaviour a sanitizer
// catches. Java has no undefined behaviour to preserve: an unchecked deref is an
// ArrayIndexOutOfBoundsException escaping into a caller that asked a question,
// which is precisely what the fuzzers' oracle forbids. So `at` answers -1 for a
// delta that leaves the region, exactly as it answers -1 for a null, and the
// refusal is the reader's rather than the runtime's.
package javatable

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookMagic identifies a cooked file and carries the byte-order check with it
// (docs/SPEC-TABLES.md §7.1). It is "SCHMCOOK" read as ASCII in the byte order a
// little-endian store produces, the same shape the block's SCHMABLK takes.
const cookMagic = uint64(0x4B4F4F434D484353)

// cookHeaderBytes is §7.1's header: eight u64 words.
const cookHeaderBytes = int64(64)

// cookMaxAlign is the ceiling on the header's `alignment` word (§7).
const cookMaxAlign = int64(64)

// cookUnit is one unit's whole cook read surface.
type cookUnit struct {
	tables  []*ir.Struct                // every table with a Java Open, sorted by name
	members map[string]*ir.MemberLayout // every record the cook closure reaches
	order   []string                    // those record names, sorted
	skipped map[string]string           // table -> why it has no Java Open
	unions  []*ir.Union                 // every union the cook closure reaches, sorted by name
	align   int64                       // the unit's region alignment (§7.1)
}

func (c *cookUnit) opens(name string) bool {
	for _, st := range c.tables {
		if st.Name == name {
			return true
		}
	}
	return false
}

// cookUnitOf computes the surface. A ROOT IS ANY TABLE (§7) and every table gets
// one — with one absence this backend states rather than hides: a closure
// carrying an UNBOUNDED ARRAY has no fixed pitch to cook, and it is a named
// follow-on rather than a refusal. A UNION is a TAG beside its overlaid arms
// (§7.2), and a <Union>Row reads it where it lies.
func cookUnitOf(u *ir.Unit) *cookUnit {
	c := &cookUnit{members: map[string]*ir.MemberLayout{}, skipped: map[string]string{}}
	names := make([]string, 0, len(u.Tables))
	for name := range u.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := u.Tables[name]
		if why := cookableClosure(u, st); why != "" {
			c.skipped[name] = why
			continue
		}
		c.tables = append(c.tables, st)
	}
	unions := map[string]*ir.Union{}
	for _, st := range c.tables {
		cookWalk(u, st.Name, func(name string, ml *ir.MemberLayout) {
			c.members[name] = ml
		}, func(un *ir.Union) {
			unions[un.Name] = un
		})
	}
	for _, un := range unions {
		c.unions = append(c.unions, un)
	}
	sort.Slice(c.unions, func(i, j int) bool { return c.unions[i].Name < c.unions[j].Name })
	aligns := make([]int64, 0, len(c.members)+len(c.unions))
	for name := range c.members {
		c.order = append(c.order, name)
		aligns = append(aligns, c.members[name].Align)
	}
	for _, un := range c.unions {
		_, align, _, _ := ir.UnionLayout(u, un)
		aligns = append(aligns, align)
	}
	sort.Strings(c.order)
	c.align = ir.RegionAlignOf(aligns...)
	return c
}

// cookWalk visits every record one table's cooked region can hold: itself,
// everything it nests by value, and everything it POINTS AT — a cook's region is
// the whole graph, so a pointer edge reaches records a by-value walk never
// would. A UNION is visited too, and its arms are walked as the fields they are
// (§2.6): an arm that nests or points at a record reaches it.
func cookWalk(u *ir.Unit, name string, visit func(string, *ir.MemberLayout), union func(*ir.Union)) {
	seen := map[string]bool{}
	var walk func(string)
	var field func(*ir.Field)
	field = func(f *ir.Field) {
		// a MAP's entries are records of its generated entry table, laid in the
		// holder's node extent (§2.8, §7.6)
		if f != nil && f.IsMap() {
			walk(f.MapEntry.Name)
		}
		if f == nil || f.Type.Kind != ir.TNamed {
			return
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			walk(ref.Name)
		case *ir.Union:
			if seen["union:"+ref.Name] {
				return
			}
			seen["union:"+ref.Name] = true
			union(ref)
			for _, v := range ref.Variants {
				field(v.F)
			}
		}
	}
	walk = func(n string) {
		if seen[n] {
			return
		}
		st := cookMember(u, n)
		if st == nil {
			return
		}
		seen[n] = true
		visit(n, ir.RecordLayout(u, st))
		for _, f := range st.Fields {
			field(f)
		}
	}
	walk(name)
}

// cookableClosure answers whether Java can read one table's whole cooked region,
// and why not when it cannot.
func cookableClosure(u *ir.Unit, st *ir.Struct) string {
	seen := map[string]bool{}
	var walk func(string) string
	var field func(string, *ir.Field) string
	field = func(owner string, f *ir.Field) string {
		if f == nil {
			return ""
		}
		// A WSTRING is retired (the wstring-removed-utf8 ruling, docs/rulings.json):
		// the Java cook form carries no wstring slot, so a closure that holds
		// one by value gets the stub, named here, rather than a writer that
		// refuses every value at measure.
		if f.Type.Kind == ir.TWString && !f.Type.Pointer {
			return owner + "." + f.Name + " is a wstring, which is retired (docs/rulings.json wstring-removed-utf8); the Java cook carries no wstring slot"
		}
		// a map's key and value are its entry's fields
		if f.IsMap() {
			for _, ef := range f.MapEntry.Fields {
				if why := field(owner+"."+f.Name, ef); why != "" {
					return why
				}
			}
		}
		if f.Type.Kind != ir.TNamed {
			return ""
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Union:
			// a union is a TAG beside its overlaid arms (§7.2), each arm a
			// field line, so the closure is the arms' closures
			if seen["union:"+ref.Name] {
				return ""
			}
			seen["union:"+ref.Name] = true
			for _, v := range ref.Variants {
				if why := field(ref.Name, v.F); why != "" {
					return why
				}
			}
		case *ir.Struct:
			return walk(ref.Name)
		}
		return ""
	}
	walk = func(name string) string {
		if seen[name] {
			return ""
		}
		seen[name] = true
		m := cookMember(u, name)
		if m == nil {
			return ""
		}
		for _, f := range m.Fields {
			if why := field(name, f); why != "" {
				return why
			}
		}
		return ""
	}
	return walk(st.Name)
}

func cookMember(u *ir.Unit, name string) *ir.Struct {
	if st := u.Tables[name]; st != nil {
		return st
	}
	return u.Structs[name]
}

// cookRuntimeFiles is the shared cook runtime, one file per public type.
func cookRuntimeFiles(u *ir.Unit) map[string][]byte {
	return map[string][]byte{
		"TableCookStorage.java":   javaFile(u, "what a cooked slot HOLDS (docs/SPEC-TABLES.md §7.2).", tableCookStorageSource),
		"TableCookInfo.java":      javaFile(u, "a cooked record's reflection descriptor (docs/SPEC-TABLES.md §7).", tableCookInfoSource()),
		"TableCookFieldInfo.java": javaFile(u, "a cooked field's reflection descriptor (docs/SPEC-TABLES.md §7).", tableCookFieldInfoSource),
	}
}

const tableCookStorageSource = `// What a cooked SLOT holds, which is not always what the WIRE carries: an ENUM
// slot holds the ORDINAL at the enum's own derived storage width
// (docs/SPEC-TABLES.md §7.2), where the wire rides the variant-name hash. A walker
// reads a slot with the width elemSize gives and the signedness this names.
public enum TableCookStorage {
    RECORD, // a nested record, or an array of them: descend through it
    REFERENCE, // an eight-byte SIGNED SELF-RELATIVE delta slot (§6.3)
    BOOL,
    SIGNED,
    UNSIGNED, // an unsigned integer, an enum ordinal, a bits(N), a flags mask
    FLOAT,
    STRING, // char[N + 1] with an int32 used length beside it
    BYTES, // uint8[N] with an int32 used length beside it
    UNION, // a TAG at the union's base, the SET arm overlaid after it (§7.2)
    WIDE, // a 128-bit integer: sixteen bytes at sixteen, one eight-byte lane per half
    LIST, // a []T holder: sixteen bytes, a self-relative reference and an int32 count (§2.9)
    MAP, // a map holder: the same sixteen bytes over its sorted entry array (§2.8)
}
`

// tableCookInfoSource is the cooked record's descriptor, and it takes the
// header's own constants from the ones above rather than repeating them: a
// magic with two spellings is a magic that can disagree with itself.
func tableCookInfoSource() string {
	return `// One cooked record's layout as DATA — the mechanism behind a reflective read of
// a cooked region, and what a gate walks a whole graph with.
public final class TableCookInfo {
    /** The cook's MAGIC (docs/SPEC-TABLES.md §7.1), read before anything else: it is
     *  what establishes the byte order every other header word is written in, and
     *  it is also what separates a COOK from a BLOCK — the two accelerators carry
     *  the same build version and different magics, because a form's identity
     *  belongs in its magic rather than in a second digest.
     *
     *  The value is "SCHMCOOK" read as ASCII in the byte order a little-endian
     *  store produces, so a hex dump of a little-endian cook is legible. */
    public static final long magic = ` + fmt.Sprintf("0x%016xL", cookMagic) + `;

    /** THIS READER's byte order, as §7.1's order word records it. It is a CONSTANT
     *  and not the host's, for the reason TableBlockInfo.byteOrder states: every
     *  read goes through TableBytes, which is explicitly little-endian, so a cook
     *  of the other order is refused by its magic and by this word. */
    public static final long byteOrder = 1L;

    /** §7.1's header is 64 bytes of u64 words, and the DATA part begins at
     *  align_up(64, alignment) — DERIVED and not a header field, because a fact a
     *  reader computes is a fact two writers cannot disagree about. */
    public static final long headerBytes = ` + fmt.Sprintf("%dL", cookHeaderBytes) + `;

    /** the ceiling on the header's alignment word: the same sixty-four a block's
     *  base takes (§19.1). */
    public static final long maxAlign = ` + fmt.Sprintf("%dL", cookMaxAlign) + `;

    public String name;
    public long typeId; // attribution entry's table id (§7.6)
    /** a UNION's tag width in bytes, its tag at offset zero; 0 for a record. A
     *  union's fields are its ARMS, each carrying the tag that selects it. */
    public int tagWidth;
    /** a UNION's declared arm count (its Max), payload-free arms included. */
    public int armCount;
    /** a RECORD's VALUE-INITIALIZED element: its declared defaults, which is
     *  what a counted array's slot past the live count and a fixed array's
     *  slot past the value's elements hold (§7.2); null for a union. */
    public java.util.function.Supplier<Object> defaults;
    /** the record holds a list or a map BY VALUE, at any depth: its node is its
     *  storage rounded to eight, then its EXTENT (docs/SPEC-TABLES.md §7.6). */
    public boolean hasExtent;
    public int size;
    public int align;
    public int numFields;
    public TableCookFieldInfo[] fields;
}
`
}

const tableCookFieldInfoSource = `// One cooked field's reflection descriptor: the facts a region actually has —
// where it sits, how big it is, whether it is a POINTER EDGE, the bound its
// COUNT COMPANION is checked against, and the record it names.
public final class TableCookFieldInfo {
    /**
     * the record this field NAMES, behind a supplier so the table stays constructible in any order.
     * null when the field is a scalar. Following it is how a walker DESCENDS — a pointer's target,
     * a by-value nesting, and an array's element are all reached through this one column.
     */
    public interface InfoRef {
        TableCookInfo get();
    }

    public String name;
    public String sourceName; // generated Java Value member (§7.6)
    public int offset; // the field's offset in the record this descriptor describes
    public int size; // its whole storage there, companions included
    public int elemSize; // one element's size, for an array; the field's own otherwise
    public boolean isArray;
    public int arrayBound; // the DECLARED bound: a counted array's N, a string's or bytes' length
    public boolean isPointer; // an eight-byte SIGNED SELF-RELATIVE delta slot (§6.3)
    public int countOffset; // the count companion's offset, or -1 when the field has none
    public int presentOffset; // an optional's presence bool, or -1
    public TableCookStorage storage; // what one slot HOLDS, at elemSize bytes
    public InfoRef recordRef;
    public int tag; // a union ARM's tag (§7.2); 0 for a record's field
    /** a LIST or MAP holder: one element's size and alignment in the node extent,
     *  and the element as a field at offset zero (a map's is its entry record). */
    public int extentSize;
    public int extentAlign;
    public TableCookFieldInfo element;

    public TableCookInfo record() {
        return recordRef == null ? null : recordRef.get();
    }

    /** this field as a union ARM: the tag value that selects it. */
    public TableCookFieldInfo arm(int armTag) {
        this.tag = armTag;
        return this;
    }

    public static TableCookFieldInfo of(
            String name,
            String sourceName,
            int offset,
            int size,
            int elemSize,
            boolean isArray,
            int arrayBound,
            boolean isPointer,
            int countOffset,
            int presentOffset,
            TableCookStorage storage,
            InfoRef record) {
        TableCookFieldInfo f = new TableCookFieldInfo();
        f.name = name;
        f.sourceName = sourceName;
        f.offset = offset;
        f.size = size;
        f.elemSize = elemSize;
        f.isArray = isArray;
        f.arrayBound = arrayBound;
        f.isPointer = isPointer;
        f.countOffset = countOffset;
        f.presentOffset = presentOffset;
        f.storage = storage;
        f.recordRef = record;
        return f;
    }

    /** this field as a LIST or MAP holder over elements of the given size and alignment. */
    public TableCookFieldInfo extent(int size, int align, TableCookFieldInfo element) {
        this.extentSize = size;
        this.extentAlign = align;
        this.element = element;
        return this;
    }
}
`

// generateCookFiles emits <Table>Cook.java for every table, plus the shared cook
// runtime. A table whose closure Java's accessors cannot spell still gets a file
// saying which and why, never a silent absence.
func generateCookFiles(u *ir.Unit, ck *cookUnit) (map[string][]byte, error) {
	out := map[string][]byte{}
	if len(ck.tables) == 0 && len(ck.skipped) == 0 {
		return out, nil
	}
	maps.Copy(out, cookRuntimeFiles(u))
	out["TableCookWrite.java"] = javaFile(u, "the cook writer's measured region and target-order stores (docs/SPEC-TABLES.md §7.6).", tableCookWriteSource(u, ck))
	names := make([]string, 0, len(u.Tables))
	for name := range u.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var b strings.Builder
		if !ck.opens(name) {
			fmt.Fprintf(&b, "// table %s has NO Java cook Open: %s (docs/SPEC-TABLES.md §7, §19.3).\n", name, ck.skipped[name])
			b.WriteString("// Its wire (§3) and its cook are unaffected — only this backend's reader is\n")
			b.WriteString("// absent, and it is absent by construction rather than by refusal. A consumer\n")
			b.WriteString("// reaching for open gets a missing name from its own compiler, beside this\n")
			b.WriteString("// file, which says why.\n")
			fmt.Fprintf(&b, "public final class %sCook {\n    private %sCook() {}\n}\n", name, name)
			out[name+"Cook.java"] = javaFile(u, "the COOKED FORM's reader and writer (docs/SPEC-TABLES.md §7).", b.String())
			continue
		}
		g := &cookGen{unit: u, cook: ck, table: u.Tables[name]}
		g.emit()
		out[name+"Cook.java"] = javaFile(u, "the COOKED FORM's reader and writer (docs/SPEC-TABLES.md §7).", g.b.String())
	}
	return out, nil
}

type cookGen struct {
	unit  *ir.Unit
	cook  *cookUnit
	table *ir.Struct
	b     strings.Builder
}

func (g *cookGen) f(format string, args ...any) { fmt.Fprintf(&g.b, format, args...) }

func (g *cookGen) emit() {
	name := g.table.Name
	ml := g.cook.members[name]
	align := g.cook.align
	g.f("// %s's cook: an array and an offset, and then the root where it lies. Opening\n", name)
	g.f("// one is a HEADER MATCH and no copy; a reference is one add (docs/SPEC-TABLES.md §7).\n")
	g.f("//\n")
	g.f("// `Cook` is a CLAIMED suffix (§11). C++ spells the same claimed verbs as free\n")
	g.f("// functions — %sOpen, %sAt — and Java spells them as MEMBERS of this type,\n", name, name)
	g.f("// which is the rule the block form already follows for its accessors.\n")
	g.f("//\n")
	g.f("// A COOK IS TRUSTED INPUT, LOADED FROM DISK. Open's checks are IDENTITY checks\n")
	g.f("// — is this file for THIS build — and not a trust boundary: there is NO\n")
	g.f("// PER-NODE VALIDATION AT LOAD, ever. A file whose provenance you doubt is\n")
	g.f("// `schema cook-check`'s business, run by a person, once, offline.\n")
	g.f("//\n")
	g.f("// THE MEMORY IS THE CONSUMER'S: the array must stay put for as long as this\n")
	g.f("// handle or anything reached through it is used. Nothing here copies and\n")
	g.f("// nothing here allocates beyond the handle.\n")
	g.f("public final class %sCook {\n", name)
	g.f("    private final byte[] data;\n")
	g.f("    private final int region;       // the DATA part's base: the root sits at offset zero\n")
	g.f("    private final long regionLength; // data_length, as the header framed it\n\n")
	g.f("    private %sCook(byte[] data, int region, long regionLength) {\n", name)
	g.f("        this.data = data;\n        this.region = region;\n        this.regionLength = regionLength;\n    }\n\n")
	g.f("    // §7.1's constants, so a consumer reading this file has the facts and not a\n")
	g.f("    // description of them.\n")
	g.f("    // the greatest alignof in the region, floor eight\n")
	g.f("    public static final long regionAlignment = %dL;\n", align)
	g.f("    public static final long rootSize = %dL;\n", ml.Size)
	g.f("    public static final long rootAlign = %dL;\n\n", ml.Align)
	g.f("    public byte[] data() { return data; }\n")
	g.f("    public int region() { return region; }\n")
	g.f("    public long regionLength() { return regionLength; }\n\n")
	g.f("    /** the ROOT's offset in data — the record at the region's base. Read it with\n")
	g.f("     *  %sRow's accessors. */\n", name)
	g.f("    public int root() { return region; }\n\n")
	g.emitOpen(ml, align)
	g.emitWrite()
	g.emitAt()
	g.f("    /** this root's cook descriptors: constant data, so a reflective walk costs a\n")
	g.f("     *  lookup and not a parse. Every record the region can hold hangs off the\n")
	g.f("     *  field column, so a walker reaches the whole graph from the root. */\n")
	g.f("    public static TableCookInfo type() { return %sRow.cookInfo(); }\n", name)
	g.f("}\n")
}

// emitWrite is §7.6's Java spelling of <T>CookMeasure,
// <T>CookMeasureReason and <T>CookFrom. The shared writer plans and validates
// the complete region before its first store.
func (g *cookGen) emitWrite() {
	g.f("    public static long cookMeasure(Object value) {\n")
	g.f("        TableCookWrite.Plan p = TableCookWrite.plan(value, type());\n")
	g.f("        return p == null ? -1 : p.total;\n    }\n\n")
	g.f("    public record MeasureReason(long bytes, String reason) {}\n\n")
	// a NaN or an infinity is the cook's own refusal, named non_finite_float
	// (docs/SPEC-TABLES.md §3.3), and every other invalid value invalid_value
	g.f("    public static MeasureReason cookMeasureReason(Object value) {\n")
	g.f("        boolean[] nonFinite = new boolean[1];\n")
	g.f("        TableCookWrite.Plan p = TableCookWrite.plan(value, type(), nonFinite);\n")
	g.f("        long bytes = p == null ? -1 : p.total;\n")
	g.f("        return new MeasureReason(bytes, bytes >= 0 ? \"ok\" : nonFinite[0] ? \"non_finite_float\" : \"invalid_value\");\n    }\n\n")
	g.f("    public static boolean cookFrom(Object value, byte[] buffer, int order) {\n")
	g.f("        if (order != TableCookWrite.LITTLE && order != TableCookWrite.BIG) { return false; }\n")
	g.f("        TableCookWrite.Plan p = TableCookWrite.plan(value, type());\n")
	g.f("        if (p == null || buffer == null || buffer.length < p.total) { return false; }\n")
	g.f("        return TableCookWrite.write(p, buffer, order);\n    }\n\n")
}

func (g *cookGen) emitOpen(ml *ir.MemberLayout, align int64) {
	name := g.table.Name
	g.f("    // Open checks the header and points, and this is the WHOLE check\n")
	g.f("    // (docs/SPEC-TABLES.md §7): the magic, the byte order it establishes, the build\n")
	g.f("    // version, every RESERVED word zero, the region ALIGNMENT the header names,\n")
	g.f("    // the two part lengths against the length the caller passed — a truncated\n")
	g.f("    // file refuses — the ROOT's own storage inside the data part, and the\n")
	g.f("    // alignment of the base. Nothing per node, ever: that is what makes this\n")
	g.f("    // O(1) in the file's size.\n")
	g.f("    //\n")
	g.f("    // On a match the bytes ARE what this build wrote, in this build's layout and\n")
	g.f("    // this build's byte order, so there is nothing to validate and nothing to fix\n")
	g.f("    // up. On any failure it answers null, and the caller falls back to a wire\n")
	g.f("    // load — the path that carries every version.\n")
	g.f("    //\n")
	g.f("    // EVERY NUMBER BELOW COMES OUT OF THE FILE, so every comparison against one\n")
	g.f("    // is UNSIGNED and each term is BOUNDED BEFORE IT IS ADDED. Java's long is\n")
	g.f("    // signed, so the unsignedness is spelled at the comparison rather than in\n")
	g.f("    // the type; a header word near 2^64 must refuse, and a subtraction that\n")
	g.f("    // wrapped would be what the check after it was supposed to catch.\n")
	g.f("    //\n")
	g.f("    // THE BASE'S ALIGNMENT is its OFFSET's, for the reason <Table>Block.open\n")
	g.f("    // states: a Java caller holds an array and an index, and the index's residue\n")
	g.f("    // is the address's.\n")
	g.f("    // THE LENGTH IS A long AND THE ARRAY IS NOT: a byte[] tops out at 2 GiB,\n")
	g.f("    // so `length` can never carry a value this reader could use. The long is\n")
	g.f("    // the seat the FOREIGN-MEMORY overload takes when the JDK floor allows\n")
	g.f("    // one; MemorySegment is not stable before 22 and this backend compiles\n")
	g.f("    // at --release 17. A catalog past 2 GiB — which §7 is explicitly built\n")
	g.f("    // for — has no Java reader until then, and that is a named follow-on\n")
	g.f("    // rather than a silence.\n")
	g.f("    //\n")
	g.f("    // THE REASON BESIDE THE NULL (docs/SPEC-TABLES.md §6.5, §7): openCheck runs\n")
	g.f("    // §7's clauses in the reference's order (Go CookOpen,\n")
	g.f("    // internal/codegen/gotable/cook.go; the JavaScript leg's openCheck) and\n")
	g.f("    // answers the data offset, or the FIRST failing clause as the negated\n")
	g.f("    // TableRefuseReason ordinal of internal/codegen/gotable/refusal.go. The\n")
	g.f("    // base's alignment is checked LAST, as the reference checks it; a\n")
	g.f("    // missing array or a negative offset is the reference's null base.\n")
	g.f("    private static long openCheck(byte[] data, int offset, long length) {\n")
	g.f("        if (data == null || offset < 0) { return -7; } // unaligned_base\n")
	g.f("        if (length < %d) { return -6; } // truncated\n", cookHeaderBytes)
	g.f("        // the caller's CLAIM must be storage the caller actually has\n")
	g.f("        if (length > data.length - offset) { return -6; } // truncated\n")
	g.f("        long bytes = length;\n\n")
	g.f("        // THE MAGIC, read before anything else: it is what establishes the byte\n")
	g.f("        // order every other header word is written in. A cook of the other order\n")
	g.f("        // reads back this constant byte-reversed and refuses HERE, rather than\n")
	g.f("        // reaching a fix-up pass this design does not have.\n")
	g.f("        long magic = TableBytes.i64(data, offset);\n")
	g.f("        if (magic != TableCookInfo.magic) {\n")
	g.f("            return magic == Long.reverseBytes(TableCookInfo.magic) ? -2 : -1; // foreign_order : not_a_cook\n")
	g.f("        }\n")
	g.f("        // and the ORDER WORD does the other job: it RECORDS which order wrote the\n")
	g.f("        // file, so a refusal names the order rather than inferring it.\n")
	g.f("        if (TableBytes.i64(data, offset + 16) != TableCookInfo.byteOrder) { return -1; } // not_a_cook\n")
	g.f("        // THE BUILD VERSION: under the match-and-point rule a matching id means\n")
	g.f("        // Open checks nothing further, so it is the sole guard between this\n")
	g.f("        // runtime and a foreign region (§20).\n")
	g.f("        if (TableBytes.i64(data, offset + 8) != BuildVersion.value) { return -3; } // wrong_build_version\n")
	g.f("        // THE RESERVED WORDS: a non-zero one means a writer used a form this\n")
	g.f("        // build does not understand, and Open refuses rather than ignoring it.\n")
	g.f("        if (TableBytes.i64(data, offset + 48) != 0) { return -4; } // reserved_not_zero\n")
	g.f("        if (TableBytes.i64(data, offset + 56) != 0) { return -4; }\n\n")
	g.f("        // THE ALIGNMENT WORD is the one field the check COMPUTES WITH rather than\n")
	g.f("        // only compares against — the data part begins at align_up(64, alignment)\n")
	g.f("        // and the base is measured against it — so a word that is not an\n")
	g.f("        // alignment rounds nothing and aligns nothing.\n")
	g.f("        long alignment = TableBytes.i64(data, offset + 40);\n")
	g.f("        if (Long.compareUnsigned(alignment, %d) < 0 || Long.compareUnsigned(alignment, %d) > 0) { return -5; } // bad_alignment\n",
		ir.RegionAlignFloor, cookMaxAlign)
	g.f("        if ((alignment & (alignment - 1)) != 0) { return -5; } // a power of two\n")
	g.f("        if ((alignment %% rootAlign) != 0) { return -5; } // and a multiple of the ROOT's own alignof\n\n")
	g.f("        // THE DATA OFFSET IS DERIVED, never a header field: a fact a reader\n")
	g.f("        // computes is a fact two writers cannot disagree about, and it is 64 for\n")
	g.f("        // every unit this language can declare.\n")
	g.f("        long dataOffset = (%d + alignment - 1) & ~(alignment - 1);\n\n", cookHeaderBytes)
	g.f("        // THE TWO PART LENGTHS against the length the caller passed. The whole\n")
	g.f("        // file is dataOffset + data + attribution, and a size that is not exactly\n")
	g.f("        // that refuses: a truncated file and a file with trailing bytes are the\n")
	g.f("        // same refusal.\n")
	g.f("        long dataLength = TableBytes.i64(data, offset + 24);\n")
	g.f("        long attribution = TableBytes.i64(data, offset + 32);\n")
	g.f("        if (Long.compareUnsigned(dataLength, bytes) > 0) { return -6; }\n")
	g.f("        if (Long.compareUnsigned(attribution, bytes - dataLength) > 0) { return -6; }\n")
	g.f("        if (Long.compareUnsigned(dataOffset, bytes - dataLength - attribution) > 0) { return -6; }\n")
	g.f("        if (dataOffset + dataLength + attribution != bytes) { return -6; }\n\n")
	g.f("        // THE DATA PART MUST HOLD THE ROOT. The part lengths frame the FILE; they\n")
	g.f("        // do not say the region is at least sizeof(root). Without this a forged\n")
	g.f("        // short data part describes a root partly outside the file, and a\n")
	g.f("        // match-and-point reader would hand back storage the caller never gave it.\n")
	g.f("        if (Long.compareUnsigned(dataLength, rootSize) < 0) { return -6; }\n\n")
	g.f("        // THE ALIGNMENT OF THE BASE.\n")
	g.f("        if ((offset %% alignment) != 0) { return -7; } // unaligned_base, last as in the reference\n\n")
	g.f("        return dataOffset;\n    }\n\n")
	g.f("    public static %sCook open(byte[] data, int offset, long length) {\n", name)
	g.f("        TableCookLayout.verify();\n")
	g.f("        long dataOffset = openCheck(data, offset, length);\n")
	g.f("        if (dataOffset < 0) { return null; }\n")
	g.f("        return new %sCook(data, offset + (int) dataOffset, TableBytes.i64(data, offset + 24));\n    }\n\n", name)
	g.f("    /** openReason answers open's check as the TableRefuseReason value name\n")
	g.f("     *  (internal/codegen/gotable/refusal.go), \"ok\" for a cook that opens:\n")
	g.f("     *  the cook-reason surface (test/conformance/README.md). */\n")
	g.f("    public static String openReason(byte[] data, int offset, long length) {\n")
	g.f("        TableCookLayout.verify();\n")
	g.f("        long check = openCheck(data, offset, length);\n")
	g.f("        if (check >= 0) { return \"ok\"; }\n")
	g.f("        switch ((int) -check) {\n")
	g.f("            case 1: return \"not_a_cook\";\n")
	g.f("            case 2: return \"foreign_order\";\n")
	g.f("            case 3: return \"wrong_build_version\";\n")
	g.f("            case 4: return \"reserved_not_zero\";\n")
	g.f("            case 5: return \"bad_alignment\";\n")
	g.f("            case 6: return \"truncated\";\n")
	g.f("            default: return \"unaligned_base\";\n")
	g.f("        }\n    }\n\n")
	g.f("    /** the whole array as the file, which is how a consumer that read one spells\n")
	g.f("     *  it. */\n")
	g.f("    public static %sCook open(byte[] data) { return open(data, 0, data == null ? 0 : data.length); }\n\n", name)
}

func (g *cookGen) emitAt() {
	g.f("    // A REFERENCE IS DEREFERENCED THROUGH at, and it is the same call in a locked\n")
	g.f("    // region and an opened cook because they are the same encoding (§6.3): the\n")
	g.f("    // slot is eight bytes, SIGNED, self-relative from the SLOT'S OWN position, so\n")
	g.f("    // a deref needs no base and NULL IS A DELTA OF ZERO.\n")
	g.f("    //\n")
	g.f("    // IT TAKES THE TARGET'S SIZE, and that is not decoration. C++, C# and Rust\n")
	g.f("    // hand back a pointer and let the walk decide, because a cook is trusted\n")
	g.f("    // input and an out-of-region deref there is undefined behaviour a sanitizer\n")
	g.f("    // catches. Java has none to preserve: an unchecked deref is an\n")
	g.f("    // ArrayIndexOutOfBoundsException escaping into a caller that asked a\n")
	g.f("    // question. Bounding the target's START alone does not prevent that — it\n")
	g.f("    // moves it one call along, to the first field read past the region's end —\n")
	g.f("    // so the bound is over the WHOLE RECORD, [target, target + size), and the\n")
	g.f("    // size is the pointee's own `<Name>Row.size`, which every call site knows.\n")
	g.f("    //\n")
	g.f("    // It answers the TARGET's offset, or -1, which is null AND a delta whose\n")
	g.f("    // record does not lie wholly inside the region.\n")
	g.f("    public int at(int slot, int size) {\n")
	g.f("        long delta = TableBytes.i64(data, slot);\n")
	g.f("        if (delta == 0 || size < 0) { return -1; }\n")
	g.f("        // written as bounds ON THE DELTA so no addition can wrap: the two limits\n")
	g.f("        // are small and the delta is whatever the file carried. A size larger\n")
	g.f("        // than the region makes high < low, so nothing passes.\n")
	g.f("        long low = (long) region - slot;\n")
	g.f("        long high = (long) region + regionLength - size - slot;\n")
	g.f("        if (delta < low || delta > high) { return -1; }\n")
	g.f("        return slot + (int) delta;\n")
	g.f("    }\n\n")
}

// emitCookLayoutFile is §20.3's Java half for the COOK closure. It checks what
// Java can disagree about, for the reason TableBlockLayout states: the
// ACCESSORS' offsets against the DESCRIPTORS'.
func emitCookLayoutFile(u *ir.Unit, ck *cookUnit, set *records, withBlock bool) []byte {
	var b strings.Builder
	b.WriteString("// THE LAYOUT CONTRACT for the cook closure (docs/SPEC-TABLES.md §20.3), run ONCE.\n")
	b.WriteString("//\n")
	b.WriteString("// A cooked region is laid out by the compiler's C ABI model, and <Name>Row's\n")
	b.WriteString("// accessors read at that model's offsets. Java has no record layout of its own\n")
	b.WriteString("// to disagree with it — see TableBlockLayout — so what is checked here is the\n")
	b.WriteString("// disagreement this language CAN have: the accessors' offsets against the\n")
	b.WriteString("// descriptors' offsets, two derivations the generator makes separately.\n")
	b.WriteString("public final class TableCookLayout {\n")
	b.WriteString("    private TableCookLayout() {}\n\n")
	b.WriteString("    // THE FLAG IS SET AFTER THE CHECKS PASS, not before. A caller that\n")
	b.WriteString("    // catches the first refusal and opens again must meet the same refusal:\n")
	b.WriteString("    // a check that threw has not been done, and \"run once\" must not mean\n")
	b.WriteString("    // \"attempted once\". It is volatile for the same reason the descriptors\n")
	b.WriteString("    // use a holder — a plain boolean is not ordered against the reads the\n")
	b.WriteString("    // checks made.\n")
	b.WriteString("    private static volatile boolean checked;\n\n")
	b.WriteString("    public static void verify() {\n")
	b.WriteString("        if (checked) { return; }\n")
	if withBlock {
		b.WriteString("        // the records the BLOCK form emits are checked by its own half, and a\n")
		b.WriteString("        // cooked region holds both sets\n")
		b.WriteString("        TableBlockLayout.verify();\n")
	}
	for _, name := range ck.order {
		if !set.inCook[name] {
			continue
		}
		fmt.Fprintf(&b, "        record(\"%sRow\", %sRow.size, %sRow.align, %sRow.offsets, %sRow.cookInfo());\n",
			name, name, name, name, name)
	}
	for _, un := range ck.unions {
		fmt.Fprintf(&b, "        record(\"%sRow\", %sRow.size, %sRow.align, %sRow.offsets, %sRow.cookInfo());\n",
			un.Name, un.Name, un.Name, un.Name, un.Name)
	}
	b.WriteString("        checked = true;\n")
	b.WriteString("    }\n\n")
	b.WriteString(`    private static void record(String what, int size, int align, int[] offsets, TableCookInfo info) {
        number(what + " size", size, info.size);
        number(what + " align", align, info.align);
        number(what + " field count", offsets.length, info.numFields);
        for (int i = 0; i < offsets.length; i++) {
            number(what + "." + info.fields[i].name, offsets[i], info.fields[i].offset);
        }
    }

    private static void number(String what, int accessors, int descriptors) {
        if (accessors != descriptors) {
            throw new IllegalStateException(
                    "schema cook layout: "
                            + what
                            + " is "
                            + accessors
                            + " through the accessors and "
                            + descriptors
                            + " through the descriptors — the two halves of one generated layout "
                            + "disagree about the bytes (docs/SPEC-TABLES.md §20.3)");
        }
    }
}
`)
	return javaFile(u, "the cook closure's layout contract, run once (docs/SPEC-TABLES.md §20.3).", b.String())
}

// cookHasExtent is the tool's recordHasExtent (internal/tablecook/extent.go):
// a record holds a list or a map BY VALUE at any depth — directly, inside a
// nested record, or inside a union arm — and so its node is its storage rounded
// to the region's floor of eight, then its EXTENT (docs/SPEC-TABLES.md §7.6). A
// pointer and a byte buffer are nodes of their own and never part of it.
func cookHasExtent(u *ir.Unit, st *ir.Struct) bool {
	return cookExtentWalk(u, st, nil, map[string]bool{})
}

func cookUnionHasExtent(u *ir.Unit, un *ir.Union) bool {
	return cookExtentWalk(u, nil, un, map[string]bool{})
}

func cookExtentWalk(u *ir.Unit, st *ir.Struct, un *ir.Union, seen map[string]bool) bool {
	var fields []*ir.Field
	switch {
	case st != nil:
		if seen[st.Name] {
			return false
		}
		seen[st.Name] = true
		if m := cookMember(u, st.Name); m != nil {
			st = m
		}
		fields = st.Fields
	case un != nil:
		if seen["union:"+un.Name] {
			return false
		}
		seen["union:"+un.Name] = true
		for _, v := range un.Variants {
			if v.F != nil {
				fields = append(fields, v.F)
			}
		}
	}
	for _, f := range fields {
		if f.IsMap() || f.IsList() {
			return true
		}
		if f.Type.Pointer || f.Type.Blob() || f.Type.Kind != ir.TNamed {
			continue
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			if cookExtentWalk(u, ref, nil, seen) {
				return true
			}
		case *ir.Union:
			if cookExtentWalk(u, nil, ref, seen) {
				return true
			}
		}
	}
	return false
}
