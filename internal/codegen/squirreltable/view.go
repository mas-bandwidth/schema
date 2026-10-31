// THE VIEW (docs/SPEC-TABLES.md §8): <T>TableFields, <T>TableInfo and the
// unit registry, emitted beside the table module through [RegisterSurface].
//
// The table module already carries the internal descriptor its own runtime
// walks. This file adds the STANDARD view surface on top of it: every field's
// facts in the vocabulary §8.1 names, an EXPLICIT unbounded marker for a
// container with no declared bound (§8.1), and exact integer bounds — Squirrel
// integers where the bound fits the language's 64-bit integer, and the
// [lo, hi] lane pair for the 128-bit kinds (§8.1, docs/PORT-SQUIRREL.md). The
// registry is the §8.3 entry point over the types and tables the unit declares.
//
// Nothing here writes a wire byte: a view describes and adds no codec (§8.2).
package squirreltable

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// tkU8 is the table-wire kind of a uint8 (docs/SPEC-TABLES.md §3): a
// non-pointer bytes(N) rides as an array of u8, and type_name tells it from a
// [..N]uint8 (§8.1).
const viewKindU8 = 6

func init() {
	RegisterSurface(Surface{Name: "view", Table: viewTable, Module: viewModule})
}

// viewTable emits <T>TableFields and <T>TableInfo for one member of the table
// closure (docs/SPEC-TABLES.md §8.1). The fields are in declaration order.
func viewTable(m *Module, st *ir.Struct) {
	// absence has a spelling (§8.1): one shared empty doc and one shared empty
	// tag list, named here so every unannotated row refers to them
	emitViewNone(m)
	m.Printf("_M.%sTableFields <- [\n", st.Name)
	for _, f := range st.Fields {
		m.Printf("  %s,\n", viewField(f))
	}
	m.Printf("];\n")
	m.Printf("_M.%sTableInfo <- {\n", st.Name)
	m.Printf("  name = %s,\n", squirrel.StringLit([]byte(st.DeclaredName())))
	m.Printf("  typeId = %s,\n", squirrel.HexLit(ir.TableWireId(st.WireName())))
	m.Printf("  table = %v,\n", st.IsTable)
	m.Printf("  variable = %v,\n", ir.VariableTables(m.Unit)[st.Name])
	m.Printf("  numFields = %d,\n", len(st.Fields))
	m.Printf("  fields = _M.%sTableFields,\n", st.Name)
	m.Printf("  doc = %s,\n", viewDoc(st.Doc))
	m.Printf("  numTags = %d,\n", len(st.Tags))
	m.Printf("  tags = %s,\n", viewTags(st.Tags))
	m.Printf("};\n\n")
}

// viewField renders one field's §8.1 facts. The vocabulary follows the Go
// emitter's TableFieldInfo columns so the same facts are compared in both
// languages: name, text key, type spellings, wire id and kind, storage shape,
// bounds, the exact ranges, the vocabulary extent, pointer identity and the
// field's own doc and tags. `unbounded` is the ONE TELL of a container with no
// declared bound (a []T, a map, a text list), explicit beside arrayBound 0.
func viewField(f *ir.Field) string {
	name := f.Name
	json := ir.TableFieldJsonKey(f)
	typeName := viewTypeName(f)
	declared := ir.TableTypeSpelling(f)
	id := ir.TableFieldWireId(f)
	kind := viewFieldKind(f)
	isArray := f.IsMap() || f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Pointer) || f.KeyEnum != ""
	counted := f.Array == ir.ArrayCounted || !f.Type.Pointer && !f.Type.TextUnbounded() &&
		(f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString) ||
		f.IsList() || f.IsMap()
	bound := viewArrayBound(f)
	unbounded := f.IsList() || f.IsMap()

	hasRange := false
	intMin, intMax, uintMin, uintMax := "0", "0", "0", "0"
	if f.Type.Kind == ir.TBits && !f.HasIntRange {
		// bits(N) declares [0, 2^N - 1] by its WIDTH (§8.1, §4)
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		hasRange = true
		uintMin, uintMax = "0", intLit(f, max)
	}
	if f.HasIntRange {
		hasRange = true
		if !ir.TableKindWide(kind) {
			if f.Type.Signed {
				intMin, intMax = intLit(f, f.IntMin), intLit(f, f.IntMax)
			} else {
				uintMin, uintMax = intLit(f, f.IntMin), intLit(f, f.IntMax)
			}
		}
	} else if f.HasFloatRange {
		hasRange = true
	}
	wide, wideMin, wideMax := false, "null", "null"
	if ir.TableKindWide(kind) {
		if lo, hi, ok := ir.TableRawRange(f); ok {
			wide = true
			wideMin, wideMax = viewWidePair(lo), viewWidePair(hi)
		}
	}

	enumMax := int64(-1)
	if f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			enumMax = ref.Max
		case *ir.Flags:
			enumMax = int64(len(ref.Variants) - 1)
		case *ir.Union:
			enumMax = int64(len(ref.Variants))
		}
	}

	return fmt.Sprintf("{ name = %s, json = %s, typeName = %s, declaredTypeName = %s, id = %s, kind = %d, "+
		"isArray = %v, counted = %v, optional = %v, arrayBound = %d, unbounded = %v, "+
		"hasRange = %v, rangeIntMin = %s, rangeIntMax = %s, rangeUintMin = %s, rangeUintMax = %s, "+
		"fracBits = %d, pointer = %v, targetId = %s, wide = %v, wideMin = %s, wideMax = %s, "+
		"enumMax = %d, doc = %s, numTags = %d, tags = %s }",
		squirrel.StringLit([]byte(name)), squirrel.StringLit([]byte(json)),
		squirrel.StringLit([]byte(typeName)), squirrel.StringLit([]byte(declared)),
		squirrel.HexLit(id), kind, isArray, counted, f.Type.Optional, bound, unbounded,
		hasRange, intMin, intMax, uintMin, uintMax,
		f.Type.FracBits, f.Type.Pointer, squirrel.HexLit(viewTargetId(f)),
		wide, wideMin, wideMax, enumMax, viewDoc(f.Doc), len(f.Tags), viewTags(f.Tags))
}

// viewFieldKind is §8.1's kind: the element's wire kind on an array line, the
// pointer kind 17 for a *T (whose slot holds a node index, §3.1), and u8 for
// a non-pointer bytes(N).
func viewFieldKind(f *ir.Field) int {
	if f.IsMap() {
		return ir.TableKindTable
	}
	if f.Type.Pointer {
		return ir.TableKindPointer
	}
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		return viewKindU8
	}
	return ir.TableWireScalarKind(f)
}

// viewArrayBound is §8.1's bound: a fixed, bounded or keyed array's capacity,
// a string's, bytes' or wstring's declared maximum, and 0 for a scalar, a lone
// *T and an unbounded container (where `unbounded` is the tell).
func viewArrayBound(f *ir.Field) int64 {
	switch {
	case f.KeyEnum != "":
		return f.KeyEnumRef.Max
	case f.Array != ir.ArrayNone:
		return f.ArrayBound
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString:
		return f.Type.Size
	}
	return 0
}

// viewTargetId is the table-wire id a *T points at (§2.1, §5), 0 when the field
// is not a pointer.
func viewTargetId(f *ir.Field) uint64 {
	if !f.Type.Pointer {
		return 0
	}
	if st, ok := f.Type.Ref.(*ir.Struct); ok {
		return ir.TableWireId(st.WireName())
	}
	return ir.TableWireId(viewTypeName(f))
}

// viewTypeName is §8.1's type_name: the scalar's declared spelling without its
// array prefix, and a declared type's schema name.
func viewTypeName(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt:
		if f.Type.Timestamp {
			return "timestamp"
		}
		prefix := "int"
		if !f.Type.Signed {
			prefix = "uint"
		}
		return fmt.Sprintf("%s%d", prefix, f.Type.Width)
	case ir.TBits:
		return fmt.Sprintf("bits(%d)", f.Type.Width)
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TFixed:
		return ir.FieldTypeSpelling(f)
	case ir.TWString:
		return "wstring"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TNamed:
		return f.Type.DeclaredName()
	}
	return "?"
}

// viewWidePair renders a 128-bit bound as its [lo, hi] lane pair, low first,
// each lane the bits a Squirrel 64-bit integer holds (§8.1's wide columns).
func viewWidePair(v *big.Int) string {
	bits := new(big.Int).Set(v)
	if bits.Sign() < 0 {
		bits.Add(bits, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	lo := new(big.Int).And(bits, mask)
	hi := new(big.Int).Rsh(bits, 64)
	return "[" + squirrel.IntLit(lo) + ", " + squirrel.IntLit(hi) + "]"
}

func viewDoc(doc string) string {
	if doc == "" {
		return "_M.TableDocNone"
	}
	return squirrel.StringLit([]byte(doc))
}

func viewTags(tags []string) string {
	if len(tags) == 0 {
		return "_M.TableTagsNone"
	}
	parts := make([]string, len(tags))
	for i, tag := range tags {
		parts[i] = squirrel.StringLit([]byte(tag))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// viewModule emits the §8.3 registry once per unit: the package and protocol
// id, then the types, tables, enums, flags, unions and constants, each set in
// DECLARATION-NAME order. A map's generated entry is claimed, not declared, so
// it is not a registry row (§2.8, §8.3).
func viewModule(m *Module) {
	u := m.Unit
	emitViewNone(m)
	file := func(name string) string {
		if base, ok := u.DeclFile[name]; ok {
			return base + ".schema"
		}
		return ""
	}
	row := func(st *ir.Struct) string {
		return fmt.Sprintf("{ name = %s, file = %s, table = %v, type = _M.%sTableInfo, doc = %s, numTags = %d, tags = %s }",
			squirrel.StringLit([]byte(st.DeclaredName())), squirrel.StringLit([]byte(file(st.Name))), st.IsTable,
			st.Name, viewDoc(st.Doc), len(st.Tags), viewTags(st.Tags))
	}
	var types, tables []*ir.Struct
	for _, st := range m.Closure {
		if st.MapEntryOf != "" || st.IsMapEntry() {
			continue
		}
		if st.IsTable {
			tables = append(tables, st)
		} else {
			types = append(types, st)
		}
	}
	sort.Slice(types, func(i, j int) bool { return types[i].DeclaredName() < types[j].DeclaredName() })
	sort.Slice(tables, func(i, j int) bool { return tables[i].DeclaredName() < tables[j].DeclaredName() })
	set := func(rows []*ir.Struct) string {
		parts := make([]string, len(rows))
		for i, st := range rows {
			parts[i] = row(st)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	reached := ir.TableClosureVocabulary(u)
	var enumNames, flagNames, constNames []string
	for n := range u.Enums {
		enumNames = append(enumNames, n)
	}
	for n := range u.Flags {
		flagNames = append(flagNames, n)
	}
	for n := range u.Consts {
		constNames = append(constNames, n)
	}
	sort.Strings(enumNames)
	sort.Strings(flagNames)
	sort.Strings(constNames)
	var enums, flags, constants []string
	for _, n := range enumNames {
		e := u.Enums[n]
		rows := []string{`{ value = 0, name = "None", id = 0x0000000000000000 }`}
		for i, variant := range e.Variants {
			id := uint64(0)
			if reached[n] {
				id = ir.TableWireId(e.VariantWireName(i))
			}
			rows = append(rows, fmt.Sprintf("{ value = %d, name = %s, id = %s }", i+1,
				squirrel.StringLit([]byte(variant)), squirrel.HexLit(id)))
		}
		enums = append(enums, fmt.Sprintf("{ name = %s, file = %s, max = %d, storageBits = %d, variants = [%s] }",
			squirrel.StringLit([]byte(n)), squirrel.StringLit([]byte(file(n))), e.Max, e.StorageBits, strings.Join(rows, ", ")))
	}
	for _, n := range flagNames {
		f := u.Flags[n]
		rows := make([]string, len(f.Variants))
		for i, variant := range f.Variants {
			rows[i] = fmt.Sprintf("{ value = %d, name = %s, id = 0x0000000000000000 }", i, squirrel.StringLit([]byte(variant)))
		}
		bits := 64
		for _, width := range []int{8, 16, 32, 64} {
			if f.WireBits <= width {
				bits = width
				break
			}
		}
		flags = append(flags, fmt.Sprintf("{ name = %s, file = %s, max = %d, storageBits = %d, variants = [%s] }",
			squirrel.StringLit([]byte(n)), squirrel.StringLit([]byte(file(n))), len(f.Variants)-1, bits, strings.Join(rows, ", ")))
	}
	for _, n := range constNames {
		c := u.Consts[n]
		integer := "0"
		floating := "0.0"
		if c.IsFloat {
			floating = squirrel.Float64Lit(c.Float)
		} else if c.Int != nil {
			if c.Int.IsInt64() || c.Int.Sign() >= 0 && c.Int.BitLen() <= 64 {
				integer = squirrel.IntLit(c.Int)
			} else {
				integer = squirrel.Int128Lit(c.Int)
			}
		}
		constants = append(constants, fmt.Sprintf("{ name = %s, file = %s, typeName = %s, isFloat = %v, intValue = %s, floatValue = %s }",
			squirrel.StringLit([]byte(n)), squirrel.StringLit([]byte(file(n))), squirrel.StringLit([]byte(c.Storage)),
			c.IsFloat, integer, floating))
	}
	m.Printf("_M.TableUnitView <- {\n")
	m.Printf("  package = %s,\n", squirrel.StringLit([]byte(u.Package)))
	m.Printf("  protocolId = %s,\n", squirrel.HexLit(u.ProtocolId))
	m.Printf("  types = %s,\n", set(types))
	m.Printf("  tables = %s,\n", set(tables))
	m.Printf("  enums = [%s],\n", strings.Join(enums, ", "))
	m.Printf("  flags = [%s],\n", strings.Join(flags, ", "))
	m.Printf("  unions = [],\n")
	m.Printf("  constants = [%s],\n", strings.Join(constants, ", "))
	m.Printf("};\n")
	m.Printf("_M.UnitView <- function() { return _M.TableUnitView; };\n\n")
}

// emitViewNone writes the shared empty doc and tag list once per module, before
// the first row that refers to them.
func emitViewNone(m *Module) {
	if m.viewNone {
		return
	}
	m.viewNone = true
	m.Printf("_M.TableDocNone <- \"\";\n")
	m.Printf("_M.TableTagsNone <- [];\n")
}
