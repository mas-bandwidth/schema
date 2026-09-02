// Package cstable emits <Base>Table.cs — the TABLE-wire C# codecs
// (SPEC-TABLES.md), the FIXED class only. One file per unit file, emitted
// only when the unit declares tables: storage classes for the `table`
// declarations, then measure/save/load codecs and reflection descriptors for
// the whole TABLE CLOSURE (every table plus everything one references,
// transitively).
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
// The VARIABLE class (pointers, arena, region, cooked) is a named follow-on:
// a unit whose closure declares a pointer is refused by name.
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
	unit   *ir.Unit
	file   *ir.File
	home   bool // this file carries the unit's shared table runtime
	types  strings.Builder
	schema strings.Builder
	indent string // extra per-line indent while emitting inside a branch guard
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
	if err := refusePointers(u); err != nil {
		return nil, err
	}
	closure := ir.TableClosure(u)
	home := ir.ProtocolIdHome(u)
	// the identity pair of an enum is emitted ONCE per unit, by the file that
	// declares it: `Schema` is one partial class across a unit's files, so a
	// second definition is a compile error rather than C++'s harmless
	// re-inclusion behind a guard
	usedEnums := closureEnums(u, closure)
	out := map[string][]byte{}
	for _, f := range u.Files {
		g := &tableGen{unit: u, file: f, home: f.Base == home}
		var members []*ir.Struct
		members = append(members, f.Tables...)
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				members = append(members, st)
			}
		}
		if g.home {
			g.tf("%s", tableRuntime())
			g.pf("%s", tableBitHelpers())
		}
		for _, st := range members {
			if st.IsTable {
				g.emitTableClass(st)
			}
		}
		for _, e := range fileEnums(f, usedEnums) {
			g.emitEnumIdentity(e)
		}
		for _, st := range members {
			g.emitTableReset(st)
			g.emitTableMeasure(st)
			g.emitTableWrite(st)
			g.emitTableSave(st)
			g.emitTableRead(st)
		}
		if len(members) > 0 {
			g.pf("// ---- reflection descriptors (tables only, SPEC-TABLES.md §8) ----\n\n")
			for _, st := range members {
				g.emitTableDescriptor(st)
			}
		}
		out[f.Base+"Table.cs"] = g.assemble()
	}
	return out, nil
}

// refusePointers is the VARIABLE-class refusal (SPEC-TABLES.md §2.2): the C#
// backend implements the fixed class, and a unit whose closure declares a
// pointer anywhere is refused by name rather than emitted with the pointered
// tables silently missing.
func refusePointers(u *ir.Unit) error {
	variable := ir.VariableTables(u)
	if len(variable) == 0 {
		return nil
	}
	names := make([]string, 0, len(variable))
	for name := range variable {
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Errorf("unit declares variable-length tables (%s) — the C# table backend's variable class is a named follow-on; the fixed class (no pointer in the by-value closure) is what this backend emits today (SPEC-TABLES.md §2.2, §15)",
		englishList(names))
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
func tableRuntime() string {
	return `// The table-wire read report — the permissive contract's ledger. Silence
// (all zero) means the data matched this reader's schema exactly.
public sealed class TableReport
{
    public int Unknown;        // unknown field ids skipped (newer data)
    public int KindMismatch;   // known id, changed type — skipped, never misdecoded
    public int Clamped;        // out-of-range values clamped to declared bounds
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
            case 12: case 13: case 14:
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
