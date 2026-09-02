// Package cpptable emits <Base>Table.h — the TABLE-wire C++ codecs
// (SPEC-TABLES.md). One header per unit file, emitted only when the unit
// declares tables: storage structs for the `table` declarations, then
// measure/save/load codecs and reflection descriptors for the whole
// TABLE CLOSURE (every table plus everything one references, transitively).
//
// The wire is neutral, evolution-tolerant TLV: field identity is the name
// hash, unknown fields skip, absent fields default, changed kinds skip
// (never misdecode), out-of-range values clamp, framing damage stops the
// decode with a partial result — and every event lands in the TableReport.
// Plain byte code with NO serialize dependency, so a Table header is
// includable from any translation unit; the encode surface is a
// measure/save split, so a caller can measure nested tables in parallel,
// prefix-sum offsets, and scatter-write disjoint ranges from N workers.
// Generated codecs allocate nothing: the caller owns every buffer.
package cpptable

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// table-wire kinds (SPEC-TABLES.md) — the whole vocabulary of the neutral
// wire: plain little-endian scalars, length-prefixed strings/bytes/tables.
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
			// an enum value rides as the u16 hash of its VARIANT NAME, whatever
			// the declaration-side storage width (SPEC-TABLES.md §5): identity
			// is the name here, exactly as it is for a field
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

func tablePut(width int) string { return fmt.Sprintf("put%d", width*8) }
func tableGet(width int) string { return fmt.Sprintf("get%d", width*8) }

type tableGen struct {
	unit           *ir.Unit
	file           *ir.File
	anyVariable    bool            // the unit declares at least one variable-length table
	variable       map[string]bool // the derived VARIABLE-LENGTH members (ir.VariableTables)
	targets        map[string]bool // tables some pointer targets (ir.PointerTargets)
	body           strings.Builder
	includes       map[string]bool // referenced files -> #include "<base>Table.h"
	nativeIncludes map[string]bool // cpp_include headers of mapped types
	indent         string          // extra per-line indent while emitting inside a branch guard
}

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
	g.body.WriteString(s)
}

func (g *tableGen) noteRef(name string) {
	if base, ok := g.unit.DeclFile[name]; ok && base != g.file.Base {
		g.includes[base] = true
	}
}

// formatFloat renders a float literal; single-precision literals format at
// FLOAT32 precision, so the emitted clamp bounds and defaults are exactly the
// values the runtime compares against.
func formatFloat(v float64, single bool) string {
	bitSize := 64
	if single {
		bitSize = 32
	}
	s := strconv.FormatFloat(v, 'g', -1, bitSize)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	if single {
		s += "f"
	}
	return s
}

// tablePrimitives is the shared runtime, emitted into every Table.h behind a
// per-package guard — one definition per TU whatever the include order, and a
// lone Table.h works standalone.
func tablePrimitives(pkg string, anyVariable bool) string {
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_PRIMITIVES"
	// the two pointer-era descriptor members exist only in a unit that HAS
	// pointers: a unit of value-only tables emits the descriptor surface it
	// always emitted, to the byte (SPEC-TABLES.md §2, the zero-cost gate)
	pointerFieldMember, pointerTypeMember := "", ""
	if anyVariable {
		pointerFieldMember = "\n    bool is_pointer;        // a *T pointer field: storage is a 4-byte TableRef; the target is a table"
		pointerTypeMember = "\n    // the DERIVED mode (SPEC-TABLES.md): false = fixed-size, a plain\n" +
			"    // relocatable struct; true = variable-length, built through a Builder\n" +
			"    // and read through a region root. Nobody declares it; the compiler\n" +
			"    // works it out.\n    bool variable;"
	}
	return `#ifndef ` + guard + `
#define ` + guard + `

namespace ` + pkg + ` {

// The table-wire read report — the permissive contract's ledger. Silence
// (all zero) means the data matched this reader's schema exactly.
struct TableReport
{
    int32_t unknown = 0;       // unknown field ids skipped (newer data)
    int32_t kind_mismatch = 0; // known id, changed type — skipped, never misdecoded
    int32_t clamped = 0;       // out-of-range values clamped to declared bounds
    bool malformed = false;    // framing damage; decode stopped, partial result kept
};

// ---- reflection (tables only, SPEC-TABLES.md) ----
//
// Static field descriptors for every type in the table closure: name, wire
// id/kind, storage offset, bounds, ranges, enum names and branch guards —
// enough to walk, print, diff, edit or bind any table value at runtime with
// no RTTI and no schema files. TableType<X>() returns X's descriptor.

struct TableTypeInfo;

struct TableFieldInfo
{
    const char * name;      // schema field name, e.g. "health"
    const char * type_name; // schema type name, e.g. "float32", "Grade"
    uint16_t id;            // table-wire field id (name hash; the was alias's hash after a rename)
    uint8_t kind;           // table-wire kind; for arrays/strings/bytes, the ELEMENT kind
    bool is_array;          // fixed or counted array (bytes included)` + pointerFieldMember + `
    bool counted;           // a _count/_length int32 companion exists (counted arrays, strings, bytes)
    bool optional;          // a ?T field: a _present bool companion decides whether it rides
    int32_t array_bound;    // array capacity / string max length; 0 for plain scalars
    uint32_t offset;        // offsetof the storage member
    uint32_t elem_size;     // sizeof the member (element size for arrays)
    uint32_t count_offset;  // offsetof the _count/_length companion, or 0xffffffff
    uint32_t present_offset; // offsetof the _present companion, or 0xffffffff
    const TableTypeInfo * table; // nested table's descriptor, or NULL
    bool has_range;         // a declared [min, max] (int or float)
    double range_min;       // NOTE: int64 ranges beyond 2^53 lose precision here
    double range_max;
    int64_t enum_max;       // enums: highest valid value (None = 0 always valid);
                            // unions: the arm count (tag range [0, enum_max]); else -1
    const char * (*enum_name)( uint64_t value ); // enums: value -> name; unions: tag -> arm name; else NULL
    // the TABLE-WIRE id of one variant (SPEC-TABLES.md §5): for an enum, the
    // hash of the variant's name; for a union, the hash of the arm's name.
    // 0 is the reserved id — an enum's None, a union's empty. NULL for every
    // other kind. Walk [0, enum_max] to enumerate a vocabulary and its ids.
    uint16_t (*variant_id)( uint64_t value );
    // an ENUM-KEYED array (SPEC-TABLES.md §2.4): the array has one slot per
    // variant of key_type_name, indexed by the variant's value, and its slots
    // ride under variant ids rather than positions. key_name and key_id are
    // the key's vocabulary — walk [0, array_bound) to print slots by name.
    // NULL on every other field.
    const char * key_type_name;
    const char * (*key_name)( uint64_t value );
    uint16_t (*key_id)( uint64_t value );
    const char * guard;     // branch guard, e.g. "at_rest" or "!at_rest"; "" if unguarded
};

struct TableTypeInfo
{
    const char * name;   // schema type name
    uint32_t size;       // sizeof the storage struct
    int32_t num_fields;
    const TableFieldInfo * fields;` + pointerTypeMember + `
};

struct TableWriter
{
    uint8_t * buffer;
    int64_t capacity;
    int64_t offset = 0;
    bool overflow = false;

    TableWriter( uint8_t * buffer, int64_t capacity ) : buffer( buffer ), capacity( capacity ) {}

    void raw( const void * data, int64_t bytes )
    {
        if ( offset + bytes > capacity ) { overflow = true; return; }
        memcpy( buffer + offset, data, (size_t) bytes );
        offset += bytes;
    }
    void put8( uint8_t v )   { raw( &v, 1 ); }
    void put16( uint16_t v ) { uint8_t b[2] = { uint8_t( v ), uint8_t( v >> 8 ) }; raw( b, 2 ); }
    void put32( uint32_t v ) { uint8_t b[4] = { uint8_t( v ), uint8_t( v >> 8 ), uint8_t( v >> 16 ), uint8_t( v >> 24 ) }; raw( b, 4 ); }
    void put64( uint64_t v ) { put32( uint32_t( v ) ); put32( uint32_t( v >> 32 ) ); }
    void patch32( int64_t at, uint32_t v )
    {
        if ( at + 4 > capacity ) { overflow = true; return; }
        buffer[at] = uint8_t( v ); buffer[at+1] = uint8_t( v >> 8 );
        buffer[at+2] = uint8_t( v >> 16 ); buffer[at+3] = uint8_t( v >> 24 );
    }
};

struct TableReader
{
    const uint8_t * buffer;
    int64_t size;
    int64_t offset = 0;
    TableReport * report;

    TableReader( const uint8_t * buffer, int64_t size, TableReport * report )
        : buffer( buffer ), size( size ), report( report ) {}

    bool has( int64_t bytes ) const { return offset + bytes <= size; }
    uint8_t get8()   { return buffer[offset++]; }
    uint16_t get16() { uint16_t v = uint16_t( buffer[offset] ) | uint16_t( buffer[offset+1] ) << 8; offset += 2; return v; }
    uint32_t get32() { uint32_t v = uint32_t( buffer[offset] ) | uint32_t( buffer[offset+1] ) << 8 | uint32_t( buffer[offset+2] ) << 16 | uint32_t( buffer[offset+3] ) << 24; offset += 4; return v; }
    uint64_t get64() { uint64_t lo = get32(); uint64_t hi = get32(); return lo | ( hi << 32 ); }

    // skip one payload by kind; false = framing damage
    bool skip( uint8_t kind )
    {
        switch ( kind )
        {
            case 1: case 2: case 6: return has( 1 ) ? ( offset += 1, true ) : false;
            case 3: case 7:         return has( 2 ) ? ( offset += 2, true ) : false;
            case 4: case 8: case 10: return has( 4 ) ? ( offset += 4, true ) : false;
            case 5: case 9: case 11: return has( 8 ) ? ( offset += 8, true ) : false;
            case 12: case 13: case 14:
            {
                if ( !has( 4 ) ) return false;
                uint32_t n = get32();
                return has( n ) ? ( offset += n, true ) : false;
            }
            case 15: // union: u16 arm id, then the arm length-prefixed (id 0 = empty, no body)
            {
                if ( !has( 2 ) ) return false;
                if ( get16() == 0 ) return true;
                if ( !has( 4 ) ) return false;
                uint32_t n = get32();
                return has( n ) ? ( offset += n, true ) : false;
            }
        }
        return false;
    }
};

inline float table_bits_to_float( uint32_t bits ) { float f; memcpy( &f, &bits, 4 ); return f; }
inline uint32_t table_float_to_bits( float f ) { uint32_t b; memcpy( &b, &f, 4 ); return b; }
inline double table_bits_to_double( uint64_t bits ) { double d; memcpy( &d, &bits, 8 ); return d; }
inline uint64_t table_double_to_bits( double d ) { uint64_t b; memcpy( &b, &d, 8 ); return b; }

} // namespace ` + pkg + `

#endif // ` + guard + `
`
}

// Generate emits <Base>Table.h for every unit file when the unit declares
// tables, and nothing when it does not — a table-free unit's generated tree
// is byte-identical with or without this package.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := checkIncludeCycle(u); err != nil {
		return nil, err
	}
	closure := ir.TableClosure(u)
	variable := ir.VariableTables(u)
	targets := ir.PointerTargets(u)
	anyVariable := len(variable) > 0
	out := map[string][]byte{}
	for _, f := range u.Files {
		g := &tableGen{unit: u, file: f, anyVariable: anyVariable, variable: variable, targets: targets,
			includes: map[string]bool{}, nativeIncludes: map[string]bool{}}
		var members []*ir.Struct
		members = append(members, orderTables(f.Tables)...)
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				members = append(members, st)
			}
		}
		if len(members) > 0 {
			for _, st := range members {
				if st.IsTable {
					g.emitTableStruct(st)
				}
			}
			for _, e := range tableEnums(members) {
				g.emitEnumIdentity(e)
			}
			g.emitCodecDeclarations(members)
			for _, st := range members {
				g.emitTableMeasure(st)
				g.emitTableWrite(st)
				g.emitTableSave(st)
				g.emitTableRead(st)
			}
			g.emitVariableSurface(members)
			g.emitRelocatabilityPreamble()
			for _, st := range members {
				g.pf("static_assert( std::is_trivially_copyable<%s>::value, \"%s must stay relocatable\" );\n", st.Name, st.Name)
				g.pf("static_assert( std::is_standard_layout<%s>::value, \"%s must stay standard-layout for offsetof\" );\n", st.Name, st.Name)
			}
			g.pf("\n")
			g.pf("// ---- reflection descriptors (tables only, SPEC-TABLES.md) ----\n\n")
			for _, st := range members {
				g.pf("inline const TableTypeInfo * %sTableType();\n", st.Name)
			}
			if anyVariable {
				g.pf("// The descriptors are CONSTANT-INITIALISED data, and a field's target is\n")
				g.pf("// the ADDRESS of another descriptor. These declarations are what let a\n")
				g.pf("// self- or mutually-referential graph — Node naming itself through *Node —\n")
				g.pf("// be expressed as constant data instead of a lazy link, which could not\n")
				g.pf("// have been written race-free OR recursion-safe. The whole reflection\n")
				g.pf("// surface is therefore immutable: read it from any thread, any time.\n")
				for _, st := range members {
					g.pf("extern const TableTypeInfo %sTableInfo;\n", st.Name)
				}
			}
			g.pf("\n")
			for _, st := range members {
				g.emitTableDescriptor(st)
			}
		} else {
			g.pf("// no tables declared or referenced in this file — codecs are emitted\n")
			g.pf("// for the table closure only (`table` declarations and what they reach)\n")
		}
		var h strings.Builder
		fmt.Fprintf(&h, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n// your choice. See the LICENSE exception in the schema compiler; the compiler is\n// AGPL-3.0, its output is not.\n", f.Base)
		fmt.Fprintf(&h, "// package %s — protocol id 0x%016x (packets only: tables version by field id, not by protocol id)\n", u.Package, u.ProtocolId)
		h.WriteString("// The TABLE wire (evolution-tolerant, SPEC-TABLES.md): no serialize\n")
		h.WriteString("// dependency — includable from any TU.\n\n")
		h.WriteString("#pragma once\n\n#include <cstdint>\n#include <cstring>\n#include <cstddef> // offsetof, for the reflection descriptors\n#include <new> // in-place prefill (placement new): no giant stack temporaries\n#include <type_traits> // the enforced relocatability asserts\n")
		if anyVariable {
			// VARIABLE-LENGTH tables only: a unit of pointer-free tables pays
			// for neither header (SPEC-TABLES.md §2, the zero-cost gate)
			h.WriteString("#include <cstdlib> // the arena's segments (the AUTHORING path may allocate)\n")
			h.WriteString("#include <atomic> // one atomic per slab: the arena is lock-free by ownership\n")
		}
		fmt.Fprintf(&h, "\n#include \"%s.h\"\n", f.Base)
		names := make([]string, 0, len(g.includes))
		for n := range g.includes {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&h, "#include \"%sTable.h\"\n", n)
		}
		native := make([]string, 0, len(g.nativeIncludes))
		for n := range g.nativeIncludes {
			native = append(native, n)
		}
		sort.Strings(native)
		for _, n := range native {
			fmt.Fprintf(&h, "#include \"%s\"\n", n)
		}
		h.WriteString("\n")
		h.WriteString(tablePrimitives(u.Package, anyVariable))
		if anyVariable {
			h.WriteString("\n")
			h.WriteString(tableArenaRuntime(u.Package))
		}
		fmt.Fprintf(&h, "\nnamespace %s {\n\n", u.Package)
		h.WriteString(g.body.String())
		fmt.Fprintf(&h, "} // namespace %s\n", u.Package)
		out[f.Base+"Table.h"] = []byte(h.String())
	}
	return out, nil
}

// checkIncludeCycle refuses cross-file reference cycles among the table
// closure: <A>Table.h and <B>Table.h including each other cannot compile
// (each needs the other's structs complete). Composition cycles are already
// refused by the checker; this is the FILE-level shadow of the same rule.
func checkIncludeCycle(u *ir.Unit) error {
	closure := ir.TableClosure(u)
	deps := map[string]map[string]bool{}
	for _, f := range u.Files {
		set := map[string]bool{}
		note := func(name string) {
			if base, ok := u.DeclFile[name]; ok && base != f.Base {
				set[base] = true
			}
		}
		var members []*ir.Struct
		members = append(members, f.Tables...)
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && closure[st.Name] {
				members = append(members, st)
			}
		}
		for _, st := range members {
			for _, fld := range st.Fields {
				if fld.Type.Kind != ir.TNamed {
					continue
				}
				note(fld.Type.Name)
				if un, isUnion := fld.Type.Ref.(*ir.Union); isUnion {
					for _, v := range un.Variants {
						note(v.Type)
					}
				}
			}
		}
		deps[f.Base] = set
	}
	color := map[string]int{}
	var path []string
	var visit func(base string) error
	visit = func(base string) error {
		switch color[base] {
		case 1:
			return fmt.Errorf("table include cycle: %s -> %s — the generated Table headers would include each other; move a declaration so the cross-file reference graph is acyclic (SPEC-TABLES.md)",
				strings.Join(path, " -> "), base)
		case 2:
			return nil
		}
		color[base] = 1
		path = append(path, base)
		targets := make([]string, 0, len(deps[base]))
		for t := range deps[base] {
			targets = append(targets, t)
		}
		sort.Strings(targets)
		for _, t := range targets {
			if err := visit(t); err != nil {
				return err
			}
		}
		path = path[:len(path)-1]
		color[base] = 2
		return nil
	}
	bases := make([]string, 0, len(deps))
	for b := range deps {
		bases = append(bases, b)
	}
	sort.Strings(bases)
	for _, b := range bases {
		if err := visit(b); err != nil {
			return err
		}
	}
	return nil
}

// orderTables returns a file's tables with every same-file table preceding
// its by-value users — schema references are order-free, C++ is not. Stable:
// declaration order survives wherever no dependency forces otherwise.
// (Cycles are refused by the checker; the fallback below is defensive.)
func orderTables(tables []*ir.Struct) []*ir.Struct {
	n := len(tables)
	byName := map[string]int{}
	for i, st := range tables {
		byName[st.Name] = i
	}
	adj := make([][]int, n)
	indeg := make([]int, n)
	for i, st := range tables {
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if ref, ok := f.Type.Ref.(*ir.Struct); ok && ref.IsTable {
				if j, ok := byName[ref.Name]; ok && j != i {
					adj[j] = append(adj[j], i)
					indeg[i]++
				}
			}
		}
	}
	order := make([]*ir.Struct, 0, n)
	done := make([]bool, n)
	for len(order) < n {
		pick := -1
		for i := range n {
			if !done[i] && indeg[i] == 0 {
				pick = i
				break
			}
		}
		if pick == -1 {
			for i := range n {
				if !done[i] {
					pick = i
					break
				}
			}
		}
		done[pick] = true
		order = append(order, tables[pick])
		for _, t := range adj[pick] {
			indeg[t]--
		}
	}
	return order
}
