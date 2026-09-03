// Package cstable emits a unit's C# table surface (SPEC-TABLES.md): the
// TABLE-wire codecs in <Base>Table.cs, and the two ACCELERATORS on the side in
// <Base>Block.cs (§19) and <Base>Cook.cs (§7). One file per unit file, emitted
// only when the unit declares tables.
//
// The WIRE half is the FIXED class only: storage classes for the `table`
// declarations, then measure/save/load codecs and reflection descriptors for
// the whole TABLE CLOSURE (every table plus everything one references,
// transitively).
//
// The two ACCELERATORS reach further, because neither needs a codec: a block
// and a cook are POINTED AT, not parsed. They are blittable records plus a
// header match, so they cover the VARIABLE class too — a pointered unit's cooks
// open in full while its wire codecs do not exist.
//
// The C++ backend (internal/codegen/cpptable) is the REFERENCE: this port
// mirrors its framing, its elision decisions, its clamps and its report
// events byte for byte, and invents no contract of its own. Where C# forces
// a different spelling the reason is stated at the site.
//
// Storage follows the C# PACKET emitter's conventions exactly
// (internal/codegen/csharp): sealed classes with public fields, string(N)
// and bytes(N) as a pre-allocated byte[N] beside an int used length, arrays
// as a pre-allocated T[N] beside an int used count, unions as a tag beside
// one pre-allocated arm per variant. That is not a free choice — a table's
// closure contains plain `type` declarations whose storage the packet
// emitter already wrote, and the table codecs decode into those very
// classes. One unit, one spelling.
//
// Nothing on the read path allocates: every buffer exists at construction,
// the caller owns the wire span and the report, and Load overlays a value in
// place after restoring its declared defaults.
//
// The VARIABLE class ON THE WIRE — the arena, the builder, the region and the
// node-table codec — is a named follow-on: a unit whose closure declares a
// pointer gets no <Base>Table.cs, and the refusal is NAMED in every source the
// unit does emit rather than left as a missing symbol (§11).
package cstable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// table-wire kinds (SPEC-TABLES.md §3) — the numbers are the wire's, not a
// backend's, and they are duplicated from cpptable deliberately: a port that
// derives them from the reference emitter's private helpers would break the
// day the two files disagree, and this way a disagreement shows up in the
// shared golden bytes instead.
const (
	tkBool   = 1
	tkI8     = 2
	tkI16    = 3
	tkI32    = 4
	tkI64    = 5
	tkU8     = 6
	tkU16    = 7
	tkU32    = 8
	tkU64    = 9
	tkF32    = 10
	tkF64    = 11
	tkString = 12
	tkTable  = 13
	tkArray  = 14
	tkUnion  = 15
	// an ENUM-KEYED array body is its OWN kind (SPEC-TABLES.md §3.2): the
	// positional array body and the keyed one are incompatible, so a reader
	// meeting the other must see a KIND MISMATCH and skip, never misdecode.
	tkKeyed = 16
)

func tableScalarKind(f *ir.Field) int {
	switch f.Type.Kind {
	case ir.TBool:
		return tkBool
	case ir.TInt:
		if f.Type.Signed {
			switch f.Type.Width {
			case 8:
				return tkI8
			case 16:
				return tkI16
			case 32:
				return tkI32
			default:
				return tkI64
			}
		}
		switch f.Type.Width {
		case 8:
			return tkU8
		case 16:
			return tkU16
		case 32:
			return tkU32
		default:
			return tkU64
		}
	case ir.TBits:
		switch {
		case f.Type.Width <= 8:
			return tkU8
		case f.Type.Width <= 16:
			return tkU16
		case f.Type.Width <= 32:
			return tkU32
		default:
			return tkU64
		}
	case ir.TFloat32:
		return tkF32
	case ir.TFloat64:
		return tkF64
	case ir.TString:
		return tkString
	case ir.TBytes:
		return tkArray
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			// an enum value rides as the u16 hash of its VARIANT NAME
			// (SPEC-TABLES.md §5), whatever the declaration-side width
			return tkU16
		case *ir.Flags:
			return tkU64
		case *ir.Struct:
			return tkTable
		case *ir.Union:
			return tkUnion
		}
	}
	return 0
}

func tableKindWidth(kind int) int {
	switch kind {
	case tkBool, tkI8, tkU8:
		return 1
	case tkI16, tkU16:
		return 2
	case tkI32, tkU32, tkF32:
		return 4
	case tkI64, tkU64, tkF64:
		return 8
	}
	return 0
}

func tablePut(width int) string { return fmt.Sprintf("Put%d", width*8) }
func tableGet(width int) string { return fmt.Sprintf("Get%d", width*8) }

// csKindStorage is the C# type a fixed-width wire kind decodes through — the
// twin of C++'s intN_t/uintN_t locals, so the clamp comparisons happen at the
// wire's own width before they land in storage.
func csKindStorage(kind int) string {
	switch kind {
	case tkI8:
		return "sbyte"
	case tkI16:
		return "short"
	case tkI32:
		return "int"
	case tkI64:
		return "long"
	case tkU8:
		return "byte"
	case tkU16:
		return "ushort"
	case tkU32:
		return "uint"
	case tkU64:
		return "ulong"
	}
	return "uint"
}

type tableGen struct {
	unit     *ir.Unit
	file     *ir.File
	home     bool       // this file carries the unit's shared table runtime
	anyKeyed bool       // the unit declares at least one enum-keyed array
	owner    *ir.Struct // the closure member whose codec is being emitted
	types    strings.Builder
	schema   strings.Builder
	indent   string // extra per-line indent while emitting inside a branch guard
}

// tf prints into the namespace-level region (storage classes).
func (g *tableGen) tf(format string, args ...any) {
	fmt.Fprintf(&g.types, format, args...)
}

// pf prints into the Schema class region, honoring the guard indent.
func (g *tableGen) pf(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	if g.indent != "" && s != "" {
		trailing := strings.HasSuffix(s, "\n")
		if trailing {
			s = s[:len(s)-1]
		}
		s = g.indent + strings.ReplaceAll(s, "\n", "\n"+g.indent)
		if trailing {
			s += "\n"
		}
	}
	g.schema.WriteString(s)
}

// Generate emits <Base>Table.cs for every unit file when the unit declares
// tables, and nothing when it does not — a table-free unit's generated C# is
// byte-identical with or without this package.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	// The two ACCELERATORS are emitted ON THE SIDE and neither needs a wire
	// codec: the BLOCK form (§19) points at bytes a producer wrote, and the
	// COOK (§7) points at a region the tooling wrote. Both are pure readers
	// over blittable records, so both reach further than this backend's wire
	// half does — a unit whose variable class the wire cannot spell still gets
	// its cooks opened.
	blocks := ir.Blocks(u)
	out, err := generateBlockFiles(u, blocks)
	if err != nil {
		return nil, err
	}
	cooks, err := generateCookFiles(u, blocks)
	if err != nil {
		return nil, err
	}
	for name, data := range cooks {
		out[name] = data
	}
	// The VARIABLE-CLASS refusal (SPEC-TABLES.md §2.2, §11) is a refusal of the
	// WIRE SURFACE, which is the half the variable class is missing: no arena,
	// no builder, no region and no node-table codec. It is named rather than
	// silent — every generated Cook file of the unit opens with the banner
	// below, naming each table and the follow-on — and no <Base>Table.cs is
	// emitted, so a consumer that reaches for Save or Load gets a missing name
	// from its own compiler beside a file that says why.
	if names := variableTableNames(u); len(names) > 0 {
		for name, data := range out {
			out[name] = append([]byte(variableClassBanner(names)), data...)
		}
		return out, nil
	}
	closure := ir.TableClosure(u)
	home := ir.ProtocolIdHome(u)
	anyKeyed := unitHasKeyedArray(u, closure)
	// the identity pair of an enum is emitted ONCE per unit, by the file that
	// declares it: `Schema` is one partial class across a unit's files, so a
	// second definition is a compile error rather than C++'s harmless
	// re-inclusion behind a guard
	usedEnums := closureEnums(u, closure)
	for _, f := range u.Files {
		g := &tableGen{unit: u, file: f, home: f.Base == home, anyKeyed: anyKeyed}
		var members []*ir.Struct
		members = append(members, f.Tables...)
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				members = append(members, st)
			}
		}
		if g.home {
			g.tf("%s", tableRuntime(anyKeyed))
			g.pf("%s", tableBitHelpers())
		}
		for _, st := range members {
			if st.IsTable {
				g.owner = st
				g.emitTableClass(st)
			}
		}
		for _, e := range fileEnums(f, usedEnums) {
			g.emitEnumIdentity(e)
		}
		for _, st := range members {
			g.owner = st
			g.emitTableReset(st)
			g.emitTableMeasure(st)
			g.emitTableWrite(st)
			g.emitTableSave(st)
			g.emitTableRead(st)
		}
		if len(members) > 0 {
			g.pf("// ---- reflection descriptors (tables only, SPEC-TABLES.md §8) ----\n\n")
			for _, st := range members {
				g.owner = st
				g.emitTableDescriptor(st)
			}
		}
		out[f.Base+"Table.cs"] = g.assemble()
	}
	return out, nil
}

// variableTableNames is the unit's variable-length tables, sorted — the tables
// whose WIRE surface this backend does not emit.
func variableTableNames(u *ir.Unit) []string {
	variable := ir.VariableTables(u)
	if len(variable) == 0 {
		return nil
	}
	names := make([]string, 0, len(variable))
	for name := range variable {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// variableClassBanner is the VARIABLE-class refusal, written where a consumer
// meets it (SPEC-TABLES.md §2.2, §11). The refusal is of the WIRE half and of
// nothing else: the accelerators below are pure readers over blittable records
// and need no codec, so they are emitted. What is absent is Measure, Save,
// Load, the arena and the builder — named here rather than left as a missing
// symbol with no explanation.
func variableClassBanner(names []string) string {
	return "// THE C# WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME (SPEC-TABLES.md §11).\n" +
		"//\n" +
		"// It declares variable-length tables (" + englishList(names) + "), and the C# table\n" +
		"// backend's VARIABLE CLASS — the arena, the builder, the region and the node-table\n" +
		"// codec — is a named follow-on (§15). No <Base>Table.cs is emitted for this unit,\n" +
		"// so a consumer reaching for Measure, Save or Load gets a missing name from its own\n" +
		"// compiler, beside this file, which says why.\n" +
		"//\n" +
		"// What IS emitted is the two ACCELERATORS, because neither needs a codec: a block\n" +
		"// (§19) and a cook (§7) are pointed at, not parsed. A build that loads this unit's\n" +
		"// cooked assets is served in full; one that wants the tolerant wire is not, and\n" +
		"// runs the tool or the C++ backend for it.\n\n"
}

func englishList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// closureEnums is every enum whose values ride in the unit's table closure.
func closureEnums(u *ir.Unit, closure map[string]bool) map[string]*ir.Enum {
	used := map[string]*ir.Enum{}
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			// an enum-keyed array's KEY rides as a variant hash too, so its
			// enum needs the identity pair even when no field has that type
			if f.KeyEnumRef != nil {
				used[f.KeyEnumRef.Name] = f.KeyEnumRef
			}
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if e, isEnum := f.Type.Ref.(*ir.Enum); isEnum {
				used[e.Name] = e
			}
		}
	}
	return used
}

// fileEnums is the used enums this file DECLARES, in declaration order.
func fileEnums(f *ir.File, used map[string]*ir.Enum) []*ir.Enum {
	var out []*ir.Enum
	for _, d := range f.Decls {
		if e, ok := d.(*ir.Enum); ok {
			if u, live := used[e.Name]; live {
				out = append(out, u)
			}
		}
	}
	return out
}

func (g *tableGen) assemble() []byte {
	var h strings.Builder
	fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	h.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "// package %s — the TABLE wire (SPEC-TABLES.md): evolution-tolerant, neutral\n", g.unit.Package)
	h.WriteString("// bytes, no serialize dependency. Tables version by field id, never by the\n")
	h.WriteString("// unit's protocol id.\n")
	if g.home {
		h.WriteString("//\n")
		h.WriteString("// Measure/Save/Load are name-first free functions on Schema (C# has no\n")
		h.WriteString("// namespace-level functions): <Name>Measure gives the exact wire size,\n")
		h.WriteString("// <Name>Save writes exactly that many bytes into the caller's span,\n")
		h.WriteString("// <Name>Load overlays a value in place and reports every tolerance event.\n")
		h.WriteString("// Nothing here allocates: the caller owns the value, the span and the report.\n")
	}
	h.WriteString("\n")
	h.WriteString("using System;\n\n")
	// block namespace, not file-scoped: Unity's compiler is C# 9, and Unity is
	// the consumer this backend exists for (schema#262)
	fmt.Fprintf(&h, "namespace %s\n{\n\n", capitalize(g.unit.Package))
	var body strings.Builder
	body.WriteString(g.types.String())
	if g.schema.Len() > 0 {
		body.WriteString("// Schema carries every generated function of the unit — C# has no\n")
		body.WriteString("// namespace-level functions, so the static class is their home; partial,\n")
		body.WriteString("// one slice per generated file.\n")
		body.WriteString("public static partial class Schema\n{\n")
		body.WriteString(indent4(g.schema.String()))
		body.WriteString("}\n")
	}
	h.WriteString(indent4(body.String()))
	h.WriteString("\n}\n")
	return []byte(h.String())
}

func indent4(s string) string {
	s = strings.TrimRight(s, "\n")
	var b strings.Builder
	for line := range strings.SplitSeq(s, "\n") {
		if line == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// unitHasKeyedArray reports whether any closure member declares an enum-keyed
// array, which is what decides whether the unit carries the keyed storage type.
func unitHasKeyedArray(u *ir.Unit, closure map[string]bool) bool {
	for name := range closure {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.KeyEnum != "" {
				return true
			}
		}
	}
	return false
}

// tableKeyedStorage is the storage type behind `ships [ShipType]ShipConfig`
// (SPEC-TABLES.md §2.4). Emitted only into a unit that declares a keyed array,
// so a unit without one is byte-identical to what it was.
//
// WHAT C# SPELLS DIFFERENTLY, stated where a reader meets it. C++'s accessor
// takes the key's own enum type; C# has no non-boxing generic enum-to-int
// conversion, so the indexer cannot. `(int)(object)key` throws for any enum
// whose backing is not exactly int (it is an unbox, not a conversion —
// measured), and Convert.ToInt32 boxes on every index, which is per-frame
// garbage in the consumer this port exists for.
//
// So the indexer takes the key's VALUE as an int and the caller writes the
// cast: `hull.Turrets[(int)Weapon.Missile]`. The CAST is the port's; the SHIFT
// is not — the indexer subtracts one exactly as C++'s does, so no call site in
// any language spells it (§2.4). The None refusal survives as the runtime
// guard, and it stands in EVERY build — as C++'s does (§2.4): the two ports
// refuse the same key in the same configurations, differing only in how a
// language ends a program.
//
// ITERATION carries over with the same RANGE and one difference in what it
// hands out (§2.4): a struct enumerator over the whole storage, so `foreach`
// allocates nothing. Its key is the ENUM VALUE, 1..E.Max, the same currency
// the indexer takes, so the two halves of the surface agree and agree with
// every other port's.
//
// The EXTENT is derived from the enum's own `Max` member, once per closed
// generic type, so nothing outside the array names its size — no constructor
// argument and no generated constant (§2.4). C# has no compile-time enum
// arithmetic, so the one read is reflective and cached in a static; it happens
// at type initialization and never on a per-value path.
//
// The entry's Element is a `readonly T`, so where the element type is a CLASS
// — a nested table, the common case — it is the live instance and mutating it
// through the iteration is visible. Where it is a VALUE — a scalar, an enum —
// it is a COPY: C# iteration READS those, and the indexer is how they are
// written. C++ hands out a reference in both cases; a ref-yielding enumerator
// here is a follow-on, not this construct's shape. The wire enforces the slot
// rule from the other side regardless: a None key never rides (§3.2).
const tableKeyedStorage = `// An ENUM-KEYED array's storage: E.Max slots, ONE PER NAMED VARIANT, with the
// key k at index k-1 — the storage SHIFTS LEFT and nothing is stored for None.
//
// NOTHING OUTSIDE THE ARRAY NAMES ITS SIZE: the extent comes from the key
// enum's own Max member, read once per closed generic type and cached, so
// there is no constructor argument and no constant a consumer could put one
// out of step with.
//
// NONE IS THE NULL KEY: it names no slot, it never rides on the wire, a stored
// key of 0 is malformed, and INDEXING BY IT IS AN ERROR — a throw from the
// indexer, which stands in every build exactly as the C++ abort does.
//
// ITERATION is the surface a consumer of the WHOLE array wants: foreach walks
// every stored slot and yields the KEY, 1..E.Max, beside the element, so no
// caller spells a bound, a lower limit or the shift. The enumerator is a
// struct and foreach binds it by pattern, so the walk allocates nothing.
//
// The entry's Element is a readonly T: a CLASS element (a nested table) is the
// live instance and mutating it through the iteration is visible, and a VALUE
// element (a scalar, an enum) is a COPY — iteration READS those and the
// indexer is how they are written.
//
// Slots is public and is what the generated codecs walk, by STORAGE INDEX; the
// indexer is for callers and takes the KEY, and it is the one place the None
// key can be caught in C#.
public sealed class TableKeyed<T, E> where E : struct, System.Enum
{
    // the extent is the enum's, derived here and named nowhere else. C# has no
    // compile-time enum arithmetic, so this reads the generated Max member
    // once, at type initialization — never on a per-value path.
    public static readonly int SlotCount =
        (int)System.Convert.ToInt64(System.Enum.Parse(typeof(E), "Max"));

    public readonly T[] Slots;

    public TableKeyed()
    {
        Slots = new T[SlotCount];
    }

    public T this[int key]
    {
        get
        {
            RefuseNone(key);
            return Slots[key - 1];
        }
        set
        {
            RefuseNone(key);
            Slots[key - 1] = value;
        }
    }

    static void RefuseNone(int key)
    {
        if (key == 0)
        {
            throw new ArgumentOutOfRangeException("key",
                "None is the null key of an enum-keyed array: it keys no slot");
        }
    }

    public Enumerator GetEnumerator()
    {
        return new Enumerator(Slots);
    }

    // one slot: the KEY it holds and its element. Deconstruct so a caller may
    // write: foreach (var (key, element) in keyed).
    public readonly struct Entry
    {
        public readonly int Key;
        public readonly T Element;

        public Entry(int key, T element)
        {
            Key = key;
            Element = element;
        }

        public void Deconstruct(out int key, out T element)
        {
            key = Key;
            element = Element;
        }
    }

    public struct Enumerator
    {
        readonly T[] slots;
        int index; // the STORAGE index; the key it holds is index + 1

        public Enumerator(T[] slots)
        {
            this.slots = slots;
            this.index = -1; // the first MoveNext lands on the first stored slot
        }

        public bool MoveNext()
        {
            index++;
            return index < slots.Length;
        }

        public Entry Current
        {
            get { return new Entry(index + 1, slots[index]); }
        }
    }
}

`

// tableBitHelpers is the float <-> IEEE-754 bit pattern pair, on Schema
// beside the codecs. Emitted once per unit, with the runtime.
func tableBitHelpers() string {
	return `// the IEEE-754 bit patterns the wire carries for f32 and f64 (SPEC-TABLES.md §3)
public static float TableBitsToFloat(uint bits)
{
    return BitConverter.Int32BitsToSingle(unchecked((int)bits));
}

public static uint TableFloatToBits(float value)
{
    return unchecked((uint)BitConverter.SingleToInt32Bits(value));
}

public static double TableBitsToDouble(ulong bits)
{
    return BitConverter.Int64BitsToDouble(unchecked((long)bits));
}

public static ulong TableDoubleToBits(double value)
{
    return unchecked((ulong)BitConverter.DoubleToInt64Bits(value));
}

`
}

// tableRuntime is the unit's shared table runtime, emitted into ONE file per
// unit (the protocol-id home). C++ emits it into every header behind an
// include guard; C# compiles a unit's files together into one assembly, so a
// second copy would be a duplicate-definition error instead.
func tableRuntime(anyKeyed bool) string {
	keyedStorage := ""
	if anyKeyed {
		keyedStorage = tableKeyedStorage
	}
	return keyedStorage + `// The table-wire read report — the permissive contract's ledger. Silence
// (all zero) means the data matched this reader's schema exactly.
public sealed class TableReport
{
    public int Unknown;        // unknown field ids skipped (newer data)
    public int KindMismatch;   // known id, changed type — skipped, never misdecoded
    public int Clamped;        // out-of-range values clamped to declared bounds
    // duplicate is the TEXT FORM's counter and the WIRE NEVER RAISES IT
    // (SPEC-TABLES.md §4, §16.2): a body carrying an id twice is legal input
    // whose last occurrence wins, silently. It rides on this struct because a
    // caller has one report type, not two — so a wire read always leaves it
    // zero, and it is here for the JSON walk that has not been ported yet.
    public int Duplicate;
    public bool Malformed;     // framing damage; decode stopped, partial result kept
}

// ---- reflection (tables only, SPEC-TABLES.md §8) ----
//
// Static field descriptors for every type in the table closure: name, wire
// id and kind, bounds, ranges, the enum/union vocabulary and its wire ids,
// and branch guards — enough to walk, print, diff or bind any table value at
// runtime with no schema files on hand. <Name>TableType() returns the
// descriptor.
//
// FOUR of the C++ surface's columns are absent, and all four are MEMORY facts
// with no C# twin: TableFieldInfo's offset, elem_size and count_offset, and
// TableTypeInfo's size (the storage struct's sizeof). A C# field has no
// offsetof and a C# class has no meaningful sizeof; a walker reaches storage
// through the language's own reflection, not through bytes. Every other
// column is here, name for name.

public sealed class TableFieldInfo
{
    public string Name;         // schema field name, e.g. "health"
    public string TypeName;     // schema type name, e.g. "float32", "Grade"
    public ushort Id;           // table-wire field id (name hash; the was alias's hash after a rename)
    public byte Kind;           // table-wire kind; for arrays/strings/bytes, the ELEMENT kind
    public bool IsArray;        // fixed or counted array (bytes included)
    public bool Counted;        // a <name>Count/<name>Length companion exists
    public bool Optional;       // a ?T field: a <name>Present bool decides whether it rides
    public int ArrayBound;      // array capacity / string max length; 0 for plain scalars
    public bool HasRange;       // a declared [min, max] (int or float)
    public double RangeMin;     // NOTE: int64 ranges beyond 2^53 lose precision here
    public double RangeMax;
    public long EnumMax;        // enums: highest valid value (None = 0 always valid);
                                // unions: the arm count (tag range [0, EnumMax]); else -1
    public Func<ulong, string> EnumName;  // enums: value -> name; unions: tag -> arm name; else null
    // the TABLE-WIRE id of one variant (SPEC-TABLES.md §5): for an enum, the
    // hash of the variant's name; for a union, the hash of the arm's name.
    // 0 is the reserved id — an enum's None, a union's empty. null for every
    // other kind. Walk [0, EnumMax] to enumerate a vocabulary and its ids.
    public Func<ulong, ushort> VariantId;

    // an ENUM-KEYED array (SPEC-TABLES.md §2.4, §8): the array has one slot
    // per variant of KeyTypeName, indexed by the variant's value, and its
    // slots ride under variant ids rather than positions. KeyName and KeyId
    // are the key's vocabulary — walk [0, ArrayBound) to print slots by name.
    // SLOT 0 IS NONE'S AND IS NEVER VALID: KeyId(0) is 0, the one reserved id
    // no declared name can hold, and KeyName(0) is "None", so a walker
    // enumerating slots skips it rather than printing a None row. All three
    // are null on every other field.
    public string KeyTypeName;
    public Func<ulong, string> KeyName;
    public Func<ulong, ushort> KeyId;

    public string Guard;        // branch guard, e.g. "at_rest" or "!at_rest"; "" if unguarded

    // the nested table's descriptor, or null. Held as a factory rather than a
    // value so a descriptor graph needs no initialization order: C# gives no
    // ordering guarantee across a partial class's files, and a table may name
    // one declared later.
    public Func<TableTypeInfo> TableRef;
    public TableTypeInfo Table
    {
        get { return TableRef == null ? null : TableRef(); }
    }
}

public sealed class TableTypeInfo
{
    public string Name;         // schema type name
    public int NumFields;
    public TableFieldInfo[] Fields;
}

// TableWriter is a ref struct over the caller's span: the wire is written in
// place, and nothing here allocates.
public ref struct TableWriter
{
    public Span<byte> Buffer;
    public int Offset;
    public bool Overflow;

    public TableWriter(Span<byte> buffer)
    {
        Buffer = buffer;
        Offset = 0;
        Overflow = false;
    }

    public void Raw(ReadOnlySpan<byte> data)
    {
        if (Offset + (long)data.Length > Buffer.Length) { Overflow = true; return; }
        data.CopyTo(Buffer.Slice(Offset, data.Length));
        Offset += data.Length;
    }
    public void Put8(byte v)
    {
        if (Offset + 1 > Buffer.Length) { Overflow = true; return; }
        Buffer[Offset] = v;
        Offset += 1;
    }
    public void Put16(ushort v)
    {
        if (Offset + 2 > Buffer.Length) { Overflow = true; return; }
        Buffer[Offset] = (byte)v;
        Buffer[Offset + 1] = (byte)(v >> 8);
        Offset += 2;
    }
    public void Put32(uint v)
    {
        if (Offset + 4 > Buffer.Length) { Overflow = true; return; }
        Buffer[Offset] = (byte)v;
        Buffer[Offset + 1] = (byte)(v >> 8);
        Buffer[Offset + 2] = (byte)(v >> 16);
        Buffer[Offset + 3] = (byte)(v >> 24);
        Offset += 4;
    }
    public void Put64(ulong v)
    {
        Put32((uint)v);
        Put32((uint)(v >> 32));
    }
    public void Patch32(int at, uint v)
    {
        if (at + 4 > Buffer.Length) { Overflow = true; return; }
        Buffer[at] = (byte)v;
        Buffer[at + 1] = (byte)(v >> 8);
        Buffer[at + 2] = (byte)(v >> 16);
        Buffer[at + 3] = (byte)(v >> 24);
    }
}

// TableReader is a ref struct over the caller's span. A nested body is read
// through a sub-reader sliced out of this one, so an inner decode can never
// reach past its own framing.
public ref struct TableReader
{
    public ReadOnlySpan<byte> Buffer;
    public int Offset;
    public TableReport Report;

    public TableReader(ReadOnlySpan<byte> buffer, TableReport report)
    {
        Buffer = buffer;
        Offset = 0;
        Report = report;
    }

    public bool Has(long bytes) { return Offset + bytes <= Buffer.Length; }
    public byte Get8() { return Buffer[Offset++]; }
    public ushort Get16()
    {
        ushort v = (ushort)(Buffer[Offset] | (Buffer[Offset + 1] << 8));
        Offset += 2;
        return v;
    }
    public uint Get32()
    {
        uint v = (uint)Buffer[Offset] | ((uint)Buffer[Offset + 1] << 8) |
                 ((uint)Buffer[Offset + 2] << 16) | ((uint)Buffer[Offset + 3] << 24);
        Offset += 4;
        return v;
    }
    public ulong Get64()
    {
        ulong lo = Get32();
        ulong hi = Get32();
        return lo | (hi << 32);
    }

    // skip one payload by kind; false = framing damage
    public bool Skip(byte kind)
    {
        switch (kind)
        {
            case 1: case 2: case 6:
                if (!Has(1)) { return false; }
                Offset += 1;
                return true;
            case 3: case 7:
                if (!Has(2)) { return false; }
                Offset += 2;
                return true;
            case 4: case 8: case 10:
                if (!Has(4)) { return false; }
                Offset += 4;
                return true;
            case 5: case 9: case 11:
                if (!Has(8)) { return false; }
                Offset += 8;
                return true;
            case 12: case 13: case 14: case 16:
            {
                if (!Has(4)) { return false; }
                uint n = Get32();
                if (!Has(n)) { return false; }
                Offset += (int)n;
                return true;
            }
            case 15: // union: u16 arm id, then the arm length-prefixed (id 0 = empty, no body)
            {
                if (!Has(2)) { return false; }
                if (Get16() == 0) { return true; }
                if (!Has(4)) { return false; }
                uint n = Get32();
                if (!Has(n)) { return false; }
                Offset += (int)n;
                return true;
            }
        }
        return false;
    }
}

`
}
