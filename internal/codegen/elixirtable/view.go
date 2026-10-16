package elixirtable

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// UnitViewModule is the generated unit-registry module (<Package>.UnitView,
// docs/SPEC-TABLES.md §8.3), emitted once per unit that declares a table.
const UnitViewModule = "UnitView"

// UnitView is the unit registry's source alone (docs/SPEC-TABLES.md §8.3),
// for the listing gate (make tables-elixir-view) over the VIEW_CORPUS units
// whose full Generate this leg still refuses by name: the registry is data
// about the IR and names no codec, so it is emitted whole for every unit.
func UnitView(u *ir.Unit) []byte { return unitViewModule(u, ir.GoExportName(u.Package)) }

// unitViewModule writes <Ns>.UnitView (docs/SPEC-TABLES.md §8.3): the unit's
// constants, enums, flags and unions with their vocabulary (§8.1: every
// variant's value and wire id, every union arm's payload or field line), then
// the type view (§8.2: every non-table struct, on the side) and the tables,
// each in declaration-name order, every entry carrying its doc comment and
// tags (§8.1). The order and the spellings are the ones internal/viewlisting
// reads, so the printed listing is byte-comparable to the compiler's (§8.7).
func unitViewModule(u *ir.Unit, ns string) []byte {
	closure := ir.TableClosure(u)
	reached := ir.TableClosureVocabulary(u)
	types, tables := viewDeclarations(u)

	var b strings.Builder
	b.WriteString(header(UnitViewModule, u.Package, "the unit registry (docs/SPEC-TABLES.md §8.3)"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "defmodule %s.%s do\n", ns, UnitViewModule)
	b.WriteString("  @moduledoc \"\"\"\n")
	b.WriteString("  The unit registry (docs/SPEC-TABLES.md §8.3): indexes every declaration,\n")
	b.WriteString("  vocabulary type, and constant in the unit in declaration-name order.\n")
	b.WriteString("  \"\"\"\n\n")
	b.WriteString("  @unit_view %{\n")
	fmt.Fprintf(&b, "    package: %s,\n", elixirQuote(u.Package))
	fmt.Fprintf(&b, "    protocol_id: 0x%016X,\n", u.ProtocolId)

	constants := slices.Sorted(maps.Keys(u.Consts))
	fmt.Fprintf(&b, "    num_constants: %d,\n", len(constants))
	if len(constants) == 0 {
		b.WriteString("    constants: [],\n")
	} else {
		b.WriteString("    constants: [\n")
		for i, n := range constants {
			c := u.Consts[n]
			integer := int64(0)
			real := "0.0"
			if c.IsFloat {
				real = viewFloatLiteral(c.Float)
			} else if c.Int != nil {
				integer = int64(c.Int.Uint64())
			}
			sep := ","
			if i == len(constants)-1 {
				sep = ""
			}
			fmt.Fprintf(&b, "      %%{name: %s, file: %s, type_name: %s, is_float: %v, int_value: %d, float_value: %s, doc: %s, tags: %s}%s\n",
				elixirQuote(c.Name), elixirQuote(viewFile(u, c.Name)), elixirQuote(c.Storage), c.IsFloat, integer, real, elixirQuote(c.Doc), elixirTags(c.Tags), sep)
		}
		b.WriteString("    ],\n")
	}

	enums := slices.Sorted(maps.Keys(u.Enums))
	fmt.Fprintf(&b, "    num_enums: %d,\n", len(enums))
	if len(enums) == 0 {
		b.WriteString("    enums: [],\n")
	} else {
		b.WriteString("    enums: [\n")
		for idx, n := range enums {
			e := u.Enums[n]
			count := len(e.Variants) + 1
			fmt.Fprintf(&b, "      %%{\n        name: %s,\n        file: %s,\n        max: %d,\n        storage_bits: %d,\n        num_variants: %d,\n        doc: %s,\n        tags: %s,\n        variants: [\n",
				elixirQuote(e.Name), elixirQuote(viewFile(u, e.Name)), e.Max, e.StorageBits, count, elixirQuote(e.Doc), elixirTags(e.Tags))
			fmt.Fprintf(&b, "          %%{value: 0, name: \"None\", id: 0x0000000000000000, doc: \"\", tags: []}")
			for i, variant := range e.Variants {
				id := uint64(0)
				if reached[e.Name] {
					id = ir.TableWireId(e.VariantWireName(i))
				}
				fmt.Fprintf(&b, ",\n          %%{value: %d, name: %s, id: 0x%016X, doc: %s, tags: %s}",
					i+1, elixirQuote(variant), id, elixirQuote(e.VariantDocs[i]), elixirTags(e.VariantTags[i]))
			}
			sep := ","
			if idx == len(enums)-1 {
				sep = ""
			}
			fmt.Fprintf(&b, "\n        ]\n      }%s\n", sep)
		}
		b.WriteString("    ],\n")
	}

	flags := slices.Sorted(maps.Keys(u.Flags))
	fmt.Fprintf(&b, "    num_flags: %d,\n", len(flags))
	if len(flags) == 0 {
		b.WriteString("    flags: [],\n")
	} else {
		b.WriteString("    flags: [\n")
		for idx, n := range flags {
			f := u.Flags[n]
			count := len(f.Variants)
			bits := viewFlagsStorageBits(f)
			fmt.Fprintf(&b, "      %%{\n        name: %s,\n        file: %s,\n        max: %d,\n        storage_bits: %d,\n        num_variants: %d,\n        doc: %s,\n        tags: %s,\n        variants: [\n",
				elixirQuote(f.Name), elixirQuote(viewFile(u, f.Name)), count-1, bits, count, elixirQuote(f.Doc), elixirTags(f.Tags))
			for i, variant := range f.Variants {
				vsep := ","
				if i == count-1 {
					vsep = ""
				}
				fmt.Fprintf(&b, "          %%{value: %d, name: %s, id: 0x0000000000000000, doc: %s, tags: %s}%s\n",
					i, elixirQuote(variant), elixirQuote(f.VariantDocs[i]), elixirTags(f.VariantTags[i]), vsep)
			}
			sep := ","
			if idx == len(flags)-1 {
				sep = ""
			}
			fmt.Fprintf(&b, "        ]\n      }%s\n", sep)
		}
		b.WriteString("    ],\n")
	}

	unions := append(slices.Sorted(maps.Keys(u.Unions)), slices.Sorted(maps.Keys(u.TableUnions))...)
	slices.Sort(unions)
	fmt.Fprintf(&b, "    num_unions: %d,\n", len(unions))
	if len(unions) == 0 {
		b.WriteString("    unions: [],\n")
	} else {
		b.WriteString("    unions: [\n")
		for idx, n := range unions {
			un := u.Unions[n]
			if un == nil {
				un = u.TableUnions[n]
			}
			count := len(un.Variants) + 1
			fmt.Fprintf(&b, "      %%{\n        name: %s,\n        file: %s,\n        max: %d,\n        storage_bits: %d,\n        num_variants: %d,\n        doc: %s,\n        tags: %s,\n        variants: [\n",
				elixirQuote(un.Name), elixirQuote(viewFile(u, un.Name)), un.Max, un.StorageBits, count, elixirQuote(un.Doc), elixirTags(un.Tags))
			fmt.Fprintf(&b, "          %%{value: 0, name: \"None\", id: 0x0000000000000000, payload_name: nil, payload: nil, field: nil, doc: \"\", tags: []}")
			for i, arm := range un.Variants {
				id := uint64(0)
				if reached[un.Name] {
					id = ir.TableWireId(arm.WireName())
				}
				payloadName := "nil"
				payload := "nil"
				field := "nil"
				switch {
				case arm.Void():
				case arm.Body():
					payloadName = elixirQuote(ir.FieldTypeSpelling(arm.F))
					payload = elixirQuote(arm.Type)
				default:
					payloadName = elixirQuote(ir.FieldTypeSpelling(arm.F))
					field = fmt.Sprintf("%%{name: %s, type_name: %s, kind: %d, array_bound: %d, offset: 0, optional: %v, doc: %s, tags: %s}",
						elixirQuote(arm.Name), elixirQuote(viewTableTypeSpelling(arm.F)), viewArmKind(arm.F), viewArmBound(arm.F), arm.F.Type.Optional, elixirQuote(arm.Doc), elixirTags(arm.Tags))
				}
				fmt.Fprintf(&b, ",\n          %%{value: %d, name: %s, id: 0x%016X, payload_name: %s, payload: %s, field: %s, doc: %s, tags: %s}",
					i+1, elixirQuote(arm.Name), id, payloadName, payload, field, elixirQuote(arm.Doc), elixirTags(arm.Tags))
			}
			sep := ","
			if idx == len(unions)-1 {
				sep = ""
			}
			fmt.Fprintf(&b, "\n        ]\n      }%s\n", sep)
		}
		b.WriteString("    ],\n")
	}

	emitViewStructList(&b, u, closure, "types", "num_types", false, types, true)
	emitViewStructList(&b, u, closure, "tables", "num_tables", true, tables, false)

	b.WriteString("  }\n\n")
	b.WriteString("  def unit_view, do: @unit_view\n")
	b.WriteString("end\n")
	return []byte(b.String())
}

// emitViewStructList writes one list of the registry: the §8.2 type view
// (isTable false) or the tables (isTable true). Each field carries its §8.1
// facts: JSON key (§16.4) and wire id, its kind and element bound, whether it
// is optional, the union it holds, and its doc and tags. A type outside the
// table closure has no wire identity, so its keys and ids are nil and zero
// (docs/SPEC-TABLES.md §8.2).
func emitViewStructList(b *strings.Builder, u *ir.Unit, closure map[string]bool, key, countKey string, isTable bool, list []*ir.Struct, trailingComma bool) {
	fmt.Fprintf(b, "    %s: %d,\n", countKey, len(list))
	comma := ""
	if trailingComma {
		comma = ","
	}
	if len(list) == 0 {
		fmt.Fprintf(b, "    %s: []%s\n", key, comma)
		return
	}
	fmt.Fprintf(b, "    %s: [\n", key)
	for idx, st := range list {
		outside := !st.IsTable && !closure[st.Name]
		fmt.Fprintf(b, "      %%{\n        name: %s,\n        file: %s,\n        table: %v,\n        doc: %s,\n        tags: %s,\n        type: %%{\n          name: %s,\n          num_fields: %d,\n          doc: %s,\n          tags: %s,\n",
			elixirQuote(st.Name), elixirQuote(viewFile(u, st.Name)), isTable, elixirQuote(st.Doc), elixirTags(st.Tags),
			elixirQuote(st.Name), len(st.Fields), elixirQuote(st.Doc), elixirTags(st.Tags))
		if len(st.Fields) == 0 {
			b.WriteString("          fields: []\n")
		} else {
			b.WriteString("          fields: [\n")
			for i, f := range st.Fields {
				json := elixirQuote(ir.TableFieldJsonKey(f))
				id := ir.TableFieldWireId(f)
				if outside {
					json = "nil"
					id = 0
				}
				arms := "nil"
				if f.Type.Kind == ir.TNamed && (u.Unions[f.Type.Name] != nil || u.TableUnions[f.Type.Name] != nil) {
					arms = fmt.Sprintf("%%{union_name: %s, payload_offset: 0}", elixirQuote(f.Type.Name))
				}
				fsep := ","
				if i == len(st.Fields)-1 {
					fsep = ""
				}
				fmt.Fprintf(b, "            %%{name: %s, json: %s, type_name: %s, id: 0x%016X, kind: %d, array_bound: %d, optional: %v, arms: %s, doc: %s, tags: %s}%s\n",
					elixirQuote(f.Name), json, elixirQuote(viewTableTypeSpelling(f)), id, viewArmKind(f), viewArmBound(f), f.Type.Optional, arms, elixirQuote(f.Doc), elixirTags(f.Tags), fsep)
			}
			b.WriteString("          ]\n")
		}
		sep := ","
		if idx == len(list)-1 {
			sep = ""
		}
		fmt.Fprintf(b, "        }\n      }%s\n", sep)
	}
	fmt.Fprintf(b, "    ]%s\n", comma)
}

// viewDeclarations splits the unit's structs into the §8.2 type view and the
// §8.3 table list, each sorted by name; a map's synthesized entry table is
// not a declaration and is not listed (docs/SPEC-TABLES.md §8.3).
func viewDeclarations(u *ir.Unit) (types, tables []*ir.Struct) {
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && !st.IsTable {
				types = append(types, st)
			}
		}
		for _, st := range f.Tables {
			if st.MapEntryOf == "" {
				tables = append(tables, st)
			}
		}
	}
	sort.Slice(types, func(i, j int) bool { return types[i].Name < types[j].Name })
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	return types, tables
}

// viewFile is the schema file a declaration was written in, as §8.3 lists it.
func viewFile(u *ir.Unit, name string) string {
	if base, ok := u.DeclFile[name]; ok {
		return base + ".schema"
	}
	return ""
}

// viewFlagsStorageBits is a flags set's storage width, the smallest of 8, 16,
// 32 and 64 bits that holds its wire bits (docs/SPEC-TABLES.md §8.1).
func viewFlagsStorageBits(f *ir.Flags) int {
	for _, w := range []int{8, 16, 32, 64} {
		if f.WireBits <= w {
			return w
		}
	}
	return 64
}

// viewTableTypeSpelling is a field's type as §8.1 spells it; a map without
// a two-field entry is spelled "map" (docs/SPEC-TABLES.md §8.1).
func viewTableTypeSpelling(f *ir.Field) string {
	if f.Type.Kind == ir.TMap && (f.MapEntry == nil || len(f.MapEntry.Fields) < 2) {
		return "map"
	}
	return ir.TableTypeSpelling(f)
}

// viewArmKind is a field's §8.1 kind: a pointer is the pointer kind, a
// fixed byte array is u8 elements, every other field its wire scalar kind.
func viewArmKind(f *ir.Field) int {
	switch {
	case f.Type.Pointer:
		return ir.TableKindPointer
	case f.Type.Kind == ir.TBytes && !f.Type.Blob():
		return ir.TableKindU8
	}
	return ir.TableWireScalarKind(f)
}

// viewArmBound is a field's §8.1 element bound: an array's length, a fixed
// string's or bytes' size, and zero for a scalar or a lone pointer.
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

// viewFloatLiteral spells a float constant (docs/SPEC-TABLES.md §8.3) as an
// Elixir float literal that round-trips, -0.0 kept.
func viewFloatLiteral(v float64) string {
	if v == 0 {
		if math.Signbit(v) {
			return "-0.0"
		}
		return "0.0"
	}
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	} else if !strings.Contains(s, ".") {
		idx := strings.IndexAny(s, "eE")
		s = s[:idx] + ".0" + s[idx:]
	}
	return s
}

// elixirQuote spells a doc comment, a tag or a name as an Elixir string
// literal byte for byte, so the doc comment §8.1 carries is the source's own
// text, interpolation refused.
func elixirQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '#':
			if i+1 < len(s) && s[i+1] == '{' {
				b.WriteString(`\#`)
			} else {
				b.WriteByte('#')
			}
		default:
			if c < 0x20 {
				fmt.Fprintf(&b, `\x%02X`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// elixirTags spells a declaration's tags (docs/SPEC-TABLES.md §8.1) as an
// Elixir list of strings, in source order.
func elixirTags(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	parts := make([]string, len(tags))
	for i, tag := range tags {
		parts[i] = elixirQuote(tag)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
