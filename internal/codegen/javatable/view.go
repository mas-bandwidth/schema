package javatable

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE REFLECTION DESCRIPTORS (docs/SPEC-TABLES.md §8), for EVERY declaration
// the unit holds: one type descriptor per table (§8.1) and per `type` (§8.2,
// the type view), each carrying its fields' name, wire kind, shape, declared
// bound, text key, type spelling, wire id, optionality, doc comment and tags;
// the unit's vocabulary (enums, flags, unions with their arms' payload
// descriptors, constants); and the unit registry with its UnitView (§8.3),
// which lists every table and every type. The Go leg
// (internal/codegen/gotable/view.go) is the model; the listing a program
// prints from TableRegistry.unitView() is byte-compared against the
// compiler's own (internal/viewlisting, §8.7) by `make tables-java-view`.
//
// A field's SHAPE (docs/SPEC-TABLES.md §8.1) is the three-way the constructs
// split into: `value` for a scalar, an enum, a nested record and an inline
// string(N); `blob` for the *string and *bytes byte buffers (§2.5); `list` for
// an array (§2.9). A field's DECLARED BOUND is an array's capacity (fixed,
// bounded or enum-keyed) and a string's or byte buffer's declared length; 0
// for a blob, an unbounded list, a map and a scalar (fieldCapacity).
//
// A TABLE's or TYPE's field also carries the walkable columns (viewaccess.go):
// its place in the cooked record (offset, element size, count and presence
// companions), the nested table's descriptor, and the cached accessors that
// read and write the field on <Name>Variable.Value, because a Java field has no
// address (§8.1). A union ARM's descriptor states the one thing §8.7 asks of
// it: every arm of a union overlays the union's payload, so each arm field's
// offset is 0.
func generateViewFiles(u *ir.Unit) (map[string][]byte, error) {
	types, tables := viewDeclarations(u)
	if len(types)+len(tables) == 0 {
		return map[string][]byte{}, nil
	}
	closure := ir.TableClosure(u)
	out := map[string][]byte{
		"TableFieldInfo.java":      javaFile(u, "a field's descriptor (docs/SPEC-TABLES.md §8.1)", tableFieldInfoRuntime),
		"TableTypeInfo.java":       javaFile(u, "a table's or a type's descriptor (docs/SPEC-TABLES.md §8.1, §8.2)", tableTypeInfoRuntime),
		"TableVariantInfo.java":    javaFile(u, "one row of an enum, flags or union vocabulary (docs/SPEC-TABLES.md §8.3)", tableVariantInfoRuntime),
		"TableVocabularyInfo.java": javaFile(u, "an enum, flags or union declaration's descriptor (docs/SPEC-TABLES.md §8.3)", tableVocabularyInfoRuntime),
		"TableConstantInfo.java":   javaFile(u, "a constant's descriptor (docs/SPEC-TABLES.md §8.3)", tableConstantInfoRuntime),
		"TableViewEntry.java":      javaFile(u, "one registry entry: a table or a type (docs/SPEC-TABLES.md §8.3)", tableViewEntryRuntime),
	}
	valued := map[string]bool{}
	for _, st := range cardVariableTables(u) {
		valued[st.Name] = true
	}
	for _, st := range append(slices.Clone(types), tables...) {
		out[st.Name+"Descriptor.java"] = javaFile(u,
			fmt.Sprintf("the descriptor for %s %s (docs/SPEC-TABLES.md §8)", declWhat(st), st.Name),
			descriptorFor(u, st, !st.IsTable && !closure[st.Name], valued[st.Name]))
	}
	// A map's generated entry table is no registry row (§2.8), but a map
	// field's nested-table column names its descriptor, so a walker steps
	// into an entry exactly as into any record (§8.1).
	for _, n := range slices.Sorted(maps.Keys(u.Tables)) {
		if st := u.Tables[n]; st != nil && st.IsMapEntry() {
			out[st.Name+"Descriptor.java"] = javaFile(u,
				fmt.Sprintf("the descriptor for map entry %s (docs/SPEC-TABLES.md §2.8, §8.1)", st.Name),
				descriptorFor(u, st, false, valued[st.Name]))
		}
	}
	out["TableRegistry.java"] = javaFile(u, "the unit's registry of descriptors (docs/SPEC-TABLES.md §8.3)", registryFor(tables, types))
	out["TableUnitView.java"] = javaFile(u, "the unit's view: every declaration, its doc and tags (docs/SPEC-TABLES.md §8.3)", unitViewFor(u, types, tables))
	return out, nil
}

func declWhat(st *ir.Struct) string {
	if st.IsTable {
		return "table"
	}
	return "type"
}

// viewDeclarations is the registry's two sets, the `type`s and the tables, each
// in declaration-NAME order (docs/SPEC-TABLES.md §8.3). A map's generated entry
// table is claimed rather than declared (§2.8), so it is no row of the
// registry.
func viewDeclarations(u *ir.Unit) (types, tables []*ir.Struct) {
	for _, n := range slices.Sorted(maps.Keys(u.Structs)) {
		if st := u.Structs[n]; st != nil && !st.IsTable {
			types = append(types, st)
		}
	}
	for _, n := range slices.Sorted(maps.Keys(u.Tables)) {
		if st := u.Tables[n]; st != nil && !st.IsMapEntry() {
			tables = append(tables, st)
		}
	}
	return types, tables
}

// fieldShape is a field's descriptor shape (docs/SPEC-TABLES.md §8.1).
func fieldShape(f *ir.Field) string {
	switch {
	case f.Type.Kind == ir.TString && f.Type.Blob():
		return "blob"
	case f.Type.Kind == ir.TBytes && f.Type.Blob():
		return "blob"
	case f.Array != ir.ArrayNone:
		return "list"
	}
	return "value"
}

// viewArmKind and viewArmBound are an arm field's kind and bound as §8.7 reads
// them off the arm's FIELD descriptor: a pointer arm states the wire it rides,
// a byte buffer is an array of u8, and the bound is the array's capacity or
// the string's or buffer's maximum length.
func viewArmKind(f *ir.Field) int {
	switch {
	case f.Type.Pointer:
		return ir.TableKindPointer
	case f.Type.Kind == ir.TBytes && !f.Type.Blob():
		return ir.TableKindU8 // a byte buffer is an array of u8
	}
	return ir.TableWireScalarKind(f)
}

func viewArmBound(f *ir.Field) int64 {
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		return 0
	case f.Array != ir.ArrayNone:
		return f.ArrayBound
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString:
		return f.Type.Size
	}
	return 0
}

// javaString is a Java string literal for any text, a doc comment included.
// Java translates \uXXXX escapes BEFORE it lexes a literal, so a control
// character is written as an octal escape (a \u000a would end the line inside
// the literal), and anything outside ASCII as \uXXXX UTF-16 units.
func javaString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\%03o`, r)
		case r < 0x80:
			b.WriteRune(r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			r -= 0x10000
			fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		}
	}
	b.WriteByte('"')
	return b.String()
}

// javaTags is a declaration's tags (SPEC §4.2) as a String[] in declared order.
func javaTags(tags []string) string {
	if len(tags) == 0 {
		return "new String[0]"
	}
	parts := make([]string, len(tags))
	for i, t := range tags {
		parts[i] = javaString(t)
	}
	return "new String[] {" + strings.Join(parts, ", ") + "}"
}

func viewFile(u *ir.Unit, name string) string {
	if base, ok := u.DeclFile[name]; ok {
		return base + ".schema"
	}
	return ""
}

// fieldDescriptor is one field's descriptor row (docs/SPEC-TABLES.md §8.1):
// the wire columns of `of` and the view columns of `view`. A `type` no table
// closure reaches has no table wire, so its text key is null and its id 0
// (§8.2).
func fieldDescriptor(f *ir.Field, kind int, bound int64, outside bool) string {
	json, id := javaString(ir.TableFieldJsonKey(f)), ir.TableFieldWireId(f)
	if outside {
		json, id = "null", 0
	}
	named := ""
	if f.Type.Kind == ir.TNamed {
		named = f.Type.Name
	}
	return fmt.Sprintf("TableFieldInfo.of(%q, %d, %q, %dL).view(%s, %s, 0x%016xL, %v, %s, %s, %s)",
		f.Name, kind, fieldShape(f), bound, json, javaString(ir.TableTypeSpelling(f)), id,
		f.Type.Optional, javaString(named), javaString(f.Doc), javaTags(f.Tags))
}

// descriptorFor is one table's or type's descriptor (docs/SPEC-TABLES.md §8.1,
// §8.2): every field's declaration columns, its cooked-record place and, on a
// declaration with a Value class (valued), its accessors and the type's create
// and reset hooks.
func descriptorFor(u *ir.Unit, st *ir.Struct, outside, valued bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "public final class %sDescriptor {\n", st.Name)
	fmt.Fprintf(&b, "    private %sDescriptor() {}\n\n", st.Name)
	b.WriteString("    // Published by CLASS INITIALIZATION, not a plain cached write, because\n")
	b.WriteString("    // the field array is not final and a racing reader could otherwise see a\n")
	b.WriteString("    // non-null descriptor with a null field array (JLS §17.4).\n")
	b.WriteString("    private static final class Holder {\n")
	b.WriteString("        static final TableTypeInfo TABLE = build();\n\n")
	b.WriteString("        private static TableTypeInfo build() {\n")
	b.WriteString("        TableTypeInfo t = new TableTypeInfo();\n")
	fmt.Fprintf(&b, "        t.name = %q;\n", st.Name)
	fmt.Fprintf(&b, "        t.table = %v;\n", st.IsTable)
	fmt.Fprintf(&b, "        t.doc = %s;\n", javaString(st.Doc))
	fmt.Fprintf(&b, "        t.tags = %s;\n", javaTags(st.Tags))
	b.WriteString("        t.fields = new TableFieldInfo[] {\n")
	for _, f := range st.Fields {
		p := cookedPlace(u, st, f)
		row := fieldDescriptor(f, variableWireKind(f), fieldCapacity(f), outside) +
			fmt.Sprintf(".at(%v, %v, %d, %d, %d, %d)", p.isArray, p.counted, p.offset, p.elementSize, p.countOffset, p.presentOffset)
		if valued {
			row += accessorColumns(st.Name+"Variable.Value", f)
		} else if ref, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed {
			row += fmt.Sprintf(".nested(() -> %sDescriptor.tableType())", ref.Name)
		}
		fmt.Fprintf(&b, "            %s,\n", row)
	}
	b.WriteString("        };\n")
	if valued {
		b.WriteString(typeHooks(st))
	}
	b.WriteString("        return t;\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")
	b.WriteString("    public static TableTypeInfo tableType() { return Holder.TABLE; }\n")
	b.WriteString("}\n")
	return b.String()
}

// registryFor is the unit registry (docs/SPEC-TABLES.md §8.3): every table's
// and every type's descriptor, tables first, each set in declaration-name
// order, and the unit's view.
func registryFor(tables, types []*ir.Struct) string {
	var b strings.Builder
	b.WriteString("public final class TableRegistry {\n")
	b.WriteString("    private TableRegistry() {}\n\n")
	b.WriteString("    // Every table's and every type's descriptor (docs/SPEC-TABLES.md §8.3),\n")
	b.WriteString("    // tables first, each in declaration-name order. A fresh array per call;\n")
	b.WriteString("    // the descriptors are immutable after class initialization.\n")
	b.WriteString("    public static TableTypeInfo[] tables() {\n")
	b.WriteString("        return new TableTypeInfo[] {\n")
	for _, st := range append(slices.Clone(tables), types...) {
		fmt.Fprintf(&b, "            %sDescriptor.tableType(),\n", st.Name)
	}
	b.WriteString("        };\n")
	b.WriteString("    }\n\n")
	b.WriteString("    // The unit's view (docs/SPEC-TABLES.md §8.3): every declaration with its\n")
	b.WriteString("    // doc comment and tags, built once.\n")
	b.WriteString("    public static TableUnitView unitView() { return TableUnitView.unit(); }\n")
	b.WriteString("}\n")
	return b.String()
}

// unitViewFor is TableUnitView: the runtime type and this unit's one instance
// (docs/SPEC-TABLES.md §8.3). Every set is in declaration-name order, a
// variant list in declared order with None first for an enum and a union.
func unitViewFor(u *ir.Unit, types, tables []*ir.Struct) string {
	reached := ir.TableClosureVocabulary(u)
	var b strings.Builder
	b.WriteString(tableUnitViewRuntime)
	b.WriteString("    private static final class Holder {\n")
	b.WriteString("        static final TableUnitView UNIT = build();\n\n")
	b.WriteString("        private static TableUnitView build() {\n")
	b.WriteString("        TableUnitView v = new TableUnitView();\n")
	fmt.Fprintf(&b, "        v.packageName = %q;\n", u.Package)
	fmt.Fprintf(&b, "        v.protocolId = 0x%016xL;\n", u.ProtocolId)
	for _, set := range []struct {
		member string
		decls  []*ir.Struct
	}{{"types", types}, {"tables", tables}} {
		fmt.Fprintf(&b, "        v.%s = new TableViewEntry[] {\n", set.member)
		for _, st := range set.decls {
			fmt.Fprintf(&b, "            TableViewEntry.of(%q, %s, %v, %sDescriptor.tableType(), %s, %s),\n",
				st.Name, javaString(viewFile(u, st.Name)), st.IsTable, st.Name, javaString(st.Doc), javaTags(st.Tags))
		}
		b.WriteString("        };\n")
	}
	// enums: row 0 is None, then the variants in DECLARED order
	b.WriteString("        v.enums = new TableVocabularyInfo[] {\n")
	for _, n := range slices.Sorted(maps.Keys(u.Enums)) {
		e := u.Enums[n]
		fmt.Fprintf(&b, "            TableVocabularyInfo.of(%q, %s, %dL, %d, %s, %s, new TableVariantInfo[] {\n",
			n, javaString(viewFile(u, n)), e.Max, e.StorageBits, javaString(e.Doc), javaTags(e.Tags))
		b.WriteString("                TableVariantInfo.of(0L, \"None\", 0x0000000000000000L, \"\", new String[0]),\n")
		for i, variant := range e.Variants {
			id := uint64(0)
			if reached[n] {
				id = ir.TableWireId(e.VariantWireName(i))
			}
			fmt.Fprintf(&b, "                TableVariantInfo.of(%dL, %q, 0x%016xL, %s, %s),\n",
				i+1, variant, id, javaString(e.VariantDocs[i]), javaTags(e.VariantTags[i]))
		}
		b.WriteString("            }),\n")
	}
	b.WriteString("        };\n")
	// flags: a BIT INDEX per variant, and no wire id: a mask's variants ride by
	// position (docs/SPEC-TABLES.md §4, §8.3)
	b.WriteString("        v.flags = new TableVocabularyInfo[] {\n")
	for _, n := range slices.Sorted(maps.Keys(u.Flags)) {
		f := u.Flags[n]
		bits := 64
		for _, w := range []int{8, 16, 32, 64} {
			if f.WireBits <= w {
				bits = w
				break
			}
		}
		fmt.Fprintf(&b, "            TableVocabularyInfo.of(%q, %s, %dL, %d, %s, %s, new TableVariantInfo[] {\n",
			n, javaString(viewFile(u, n)), len(f.Variants)-1, bits, javaString(f.Doc), javaTags(f.Tags))
		for i, variant := range f.Variants {
			fmt.Fprintf(&b, "                TableVariantInfo.of(%dL, %q, 0x0000000000000000L, %s, %s),\n",
				i, variant, javaString(f.VariantDocs[i]), javaTags(f.VariantTags[i]))
		}
		b.WriteString("            }),\n")
	}
	b.WriteString("        };\n")
	// unions, the packet and the table ones in one name-ordered set: an arm
	// carries its payload's spelling and either the payload's record
	// descriptor (a body arm) or its own field descriptor (a general arm)
	all := map[string]*ir.Union{}
	maps.Copy(all, u.Unions)
	maps.Copy(all, u.TableUnions)
	b.WriteString("        v.unions = new TableVocabularyInfo[] {\n")
	for _, n := range slices.Sorted(maps.Keys(all)) {
		un := all[n]
		fmt.Fprintf(&b, "            TableVocabularyInfo.of(%q, %s, %dL, %d, %s, %s, new TableVariantInfo[] {\n",
			n, javaString(viewFile(u, n)), un.Max, un.StorageBits, javaString(un.Doc), javaTags(un.Tags))
		b.WriteString("                TableVariantInfo.of(0L, \"None\", 0x0000000000000000L, \"\", new String[0]),\n")
		for i, arm := range un.Variants {
			id := uint64(0)
			if reached[n] {
				id = ir.TableWireId(arm.WireName())
			}
			row := fmt.Sprintf("TableVariantInfo.of(%dL, %q, 0x%016xL, %s, %s)", i+1, arm.Name, id, javaString(arm.Doc), javaTags(arm.Tags))
			switch {
			case arm.Void():
			case arm.Body():
				row += fmt.Sprintf(".body(%s, %sDescriptor.tableType())", javaString(ir.FieldTypeSpelling(arm.F)), arm.Ref.Name)
			default:
				f := *arm.F
				f.Name = arm.Name
				row += fmt.Sprintf(".field(%s, %s)", javaString(ir.FieldTypeSpelling(arm.F)),
					fieldDescriptor(&f, viewArmKind(arm.F), viewArmBound(arm.F), false))
			}
			fmt.Fprintf(&b, "                %s,\n", row)
		}
		b.WriteString("            }),\n")
	}
	b.WriteString("        };\n")
	b.WriteString("        v.constants = new TableConstantInfo[] {\n")
	for _, n := range slices.Sorted(maps.Keys(u.Consts)) {
		c := u.Consts[n]
		integer, real := int64(0), uint64(0)
		if c.IsFloat {
			real = math.Float64bits(c.Float)
		} else if c.Int != nil {
			integer = int64(c.Int.Uint64())
		}
		fmt.Fprintf(&b, "            TableConstantInfo.of(%q, %s, %s, %v, %dL, Double.longBitsToDouble(0x%016xL), %s, %s),\n",
			n, javaString(viewFile(u, n)), javaString(c.Storage), c.IsFloat, integer, real, javaString(c.Doc), javaTags(c.Tags))
	}
	b.WriteString("        };\n")
	b.WriteString("        return v;\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n\n")
	b.WriteString("    public static TableUnitView unit() { return Holder.UNIT; }\n")
	b.WriteString("}\n")
	return b.String()
}

const tableFieldInfoRuntime = `public final class TableFieldInfo {
    public String name;
    /** the field's wire kind (docs/SPEC-TABLES.md §3); an arm's, the kind its payload rides. */
    public int kind;
    /** value, blob or list (§8.1). */
    public String shape;
    /** the declared bound: an array's capacity (fixed, bounded or enum-keyed), a
     *  string's or byte buffer's length; 0 for a scalar, a blob, a list, a map. */
    public long bound;
    /** the text form's key (§16.3); null on a type no table closure reaches. */
    public String json;
    /** the declared type, spelled as the schema spells it. */
    public String typeName;
    /** the table-wire id (§5); 0 on a type no table closure reaches. */
    public long id;
    public boolean optional;
    /** the declaration a named field refers to, "" for none. */
    public String named;
    /** an array, a byte buffer, a list or a map: the accessors take an element index. */
    public boolean isArray;
    /** a count or length companion exists (a bounded array, a string, a list, a map). */
    public boolean counted;
    /** the field's byte offset in the COOKED record (§7.2, the layout <Name>Row reads);
     *  0 for every union arm, which all overlay the payload. */
    public int offset;
    /** one element's cooked size: the scalar, one array element, one list element. */
    public int elementSize;
    /** the count/length companion's cooked offset, or -1. */
    public int countOffset;
    /** the presence companion's cooked offset (an optional field), or -1. */
    public int presentOffset;
    public String doc;
    public String[] tags;

    // THE ACCESSORS (§8.1): a Java field has no address, so a descriptor carries
    // the pair that reads and writes it on <Name>Variable.Value, built once with
    // the descriptor and cached with it. A field's element is read through the
    // one lane its type takes (long, double or object); the others are null.
    // index is the element for an array and ignored otherwise. Every accessor is
    // null on a declaration with no Value class (a type no table reaches, §8.2).
    public interface Count { int count(Object holder); }
    public interface Resize { void resize(Object holder, int count); }
    public interface LongGet { long get(Object holder, int index); }
    public interface LongSet { void set(Object holder, int index, long value); }
    public interface DoubleGet { double get(Object holder, int index); }
    public interface DoubleSet { void set(Object holder, int index, double value); }
    public interface ObjectGet { Object get(Object holder, int index); }
    public interface ObjectSet { void set(Object holder, int index, Object value); }
    public interface PresentGet { boolean get(Object holder); }
    public interface PresentSet { void set(Object holder, boolean present); }

    /** an array's element count: a list's size, an enum-keyed array's extent. */
    public Count count;
    /** grows (with default elements) or shrinks an array; null for an enum-keyed array. */
    public Resize resize;
    /** an integer-valued element: bool as 0/1, enum value, flags mask, bits(N), integers to 64 bits (unsigned zero-extended). */
    public LongGet getLong;
    public LongSet setLong;
    /** a float32 or float64 element. */
    public DoubleGet getDouble;
    public DoubleSet setDouble;
    /** an element held by reference: a string, a byte buffer, a nested record's or union's Value, a 128-bit value. */
    public ObjectGet getObject;
    public ObjectSet setObject;
    /** an optional field's presence companion. */
    public PresentGet present;
    public PresentSet setPresent;
    /** the nested record's descriptor (a nested table, an array of records, a pointer, a map's entry), else null. */
    public java.util.function.Supplier<TableTypeInfo> nested;

    /** the nested record's descriptor, or null (§8.1). */
    public TableTypeInfo table() { return nested == null ? null : nested.get(); }

    public static TableFieldInfo of(String name, int kind, String shape, long bound) {
        TableFieldInfo f = new TableFieldInfo();
        f.name = name;
        f.kind = kind;
        f.shape = shape;
        f.bound = bound;
        return f;
    }

    TableFieldInfo at(boolean isArray, boolean counted, int offset, int elementSize, int countOffset, int presentOffset) {
        this.isArray = isArray;
        this.counted = counted;
        this.offset = offset;
        this.elementSize = elementSize;
        this.countOffset = countOffset;
        this.presentOffset = presentOffset;
        return this;
    }

    TableFieldInfo nested(java.util.function.Supplier<TableTypeInfo> nested) { this.nested = nested; return this; }
    TableFieldInfo items(Count count, Resize resize) { this.count = count; this.resize = resize; return this; }
    TableFieldInfo longs(LongGet get, LongSet set) { this.getLong = get; this.setLong = set; return this; }
    TableFieldInfo doubles(DoubleGet get, DoubleSet set) { this.getDouble = get; this.setDouble = set; return this; }
    TableFieldInfo objects(ObjectGet get, ObjectSet set) { this.getObject = get; this.setObject = set; return this; }
    TableFieldInfo presence(PresentGet get, PresentSet set) { this.present = get; this.setPresent = set; return this; }

    TableFieldInfo view(String json, String typeName, long id, boolean optional, String named, String doc, String[] tags) {
        this.json = json;
        this.typeName = typeName;
        this.id = id;
        this.optional = optional;
        this.named = named;
        this.doc = doc;
        this.tags = tags;
        return this;
    }
}
`

const tableTypeInfoRuntime = `public final class TableTypeInfo {
    public String name;
    /** true for a table, false for a type (docs/SPEC-TABLES.md §8.2). */
    public boolean table;
    public TableFieldInfo[] fields;
    public String doc;
    public String[] tags;
    /** a fresh instance at its declared defaults (<Name>Variable.Value); null on a type no table reaches. */
    public java.util.function.Supplier<Object> create;
    /** THE RESET HOOK (§8.1): puts one instance back at its declared defaults, in place,
     *  one member at a time; null on a type no table reaches. */
    public java.util.function.Consumer<Object> reset;
}
`

const tableVariantInfoRuntime = `public final class TableVariantInfo {
    /** the variant's value: an enum's or a union's tag, a flag's bit index. */
    public long value;
    public String name;
    /** the table-wire id (§5); 0 for a flag and for a declaration no table closure reaches. */
    public long id;
    /** a union arm's payload type, spelled as the schema spells it; null for a void arm and every non-arm row. */
    public String payloadName;
    /** a BODY arm's record descriptor, else null. */
    public TableTypeInfo payload;
    /** a GENERAL arm's field descriptor, else null. */
    public TableFieldInfo field;
    public String doc;
    public String[] tags;

    static TableVariantInfo of(long value, String name, long id, String doc, String[] tags) {
        TableVariantInfo v = new TableVariantInfo();
        v.value = value;
        v.name = name;
        v.id = id;
        v.doc = doc;
        v.tags = tags;
        return v;
    }

    TableVariantInfo body(String payloadName, TableTypeInfo payload) {
        this.payloadName = payloadName;
        this.payload = payload;
        return this;
    }

    TableVariantInfo field(String payloadName, TableFieldInfo field) {
        this.payloadName = payloadName;
        this.field = field;
        return this;
    }
}
`

const tableVocabularyInfoRuntime = `public final class TableVocabularyInfo {
    public String name;
    /** the schema file that declares it. */
    public String file;
    public long max;
    public int storageBits;
    /** None first for an enum and a union, then the variants in declared order. */
    public TableVariantInfo[] variants;
    public String doc;
    public String[] tags;

    static TableVocabularyInfo of(String name, String file, long max, int storageBits, String doc, String[] tags, TableVariantInfo[] variants) {
        TableVocabularyInfo v = new TableVocabularyInfo();
        v.name = name;
        v.file = file;
        v.max = max;
        v.storageBits = storageBits;
        v.doc = doc;
        v.tags = tags;
        v.variants = variants;
        return v;
    }
}
`

const tableConstantInfoRuntime = `public final class TableConstantInfo {
    public String name;
    public String file;
    public String typeName;
    public boolean isFloat;
    public long intValue;
    public double floatValue;
    public String doc;
    public String[] tags;

    static TableConstantInfo of(String name, String file, String typeName, boolean isFloat, long intValue, double floatValue, String doc, String[] tags) {
        TableConstantInfo c = new TableConstantInfo();
        c.name = name;
        c.file = file;
        c.typeName = typeName;
        c.isFloat = isFloat;
        c.intValue = intValue;
        c.floatValue = floatValue;
        c.doc = doc;
        c.tags = tags;
        return c;
    }
}
`

const tableViewEntryRuntime = `public final class TableViewEntry {
    public String name;
    public String file;
    public boolean table;
    public TableTypeInfo type;
    public String doc;
    public String[] tags;

    static TableViewEntry of(String name, String file, boolean table, TableTypeInfo type, String doc, String[] tags) {
        TableViewEntry e = new TableViewEntry();
        e.name = name;
        e.file = file;
        e.table = table;
        e.type = type;
        e.doc = doc;
        e.tags = tags;
        return e;
    }
}
`

const tableUnitViewRuntime = `public final class TableUnitView {
    private TableUnitView() {}

    public String packageName;
    public long protocolId;
    /** every type and every table, each in declaration-name order. */
    public TableViewEntry[] types;
    public TableViewEntry[] tables;
    /** every enum, flags and union (packet and table unions in one set), and every constant, each in name order. */
    public TableVocabularyInfo[] enums;
    public TableVocabularyInfo[] flags;
    public TableVocabularyInfo[] unions;
    public TableConstantInfo[] constants;

    // Published by CLASS INITIALIZATION (JLS §12.4), so every reader sees the
    // whole view or waits for it.
`
