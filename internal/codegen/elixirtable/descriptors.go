// The reflection descriptors (docs/SPEC-TABLES.md §8) and the text form's
// per-table entry points (§16), for Elixir.
//
// The descriptors are MODULE ATTRIBUTES — literals in the module's literal
// area, reachable from any process, copied by nothing — and they carry the
// STRUCT KEY of each field rather than a storage offset, because an Elixir
// struct has keys and no addresses. That is the deviation the package doc
// names, and it is what lets the text form be ONE generic walk over the
// descriptors rather than a codec per table.
//
// Every indirection is a {module, function} pair rather than a captured fun,
// so the whole descriptor stays a term a module attribute can hold.
package elixirtable

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) emitDescriptors(members []*ir.Struct) {
	g.pf("# ---- reflection descriptors (tables only, docs/SPEC-TABLES.md §8) ----\n")
	g.pf("#\n")
	g.pf("# Field descriptors for every type in the table closure: name, wire id and\n")
	g.pf("# kind, the struct key the value sits under, bounds, ranges, the enum/union\n")
	g.pf("# vocabulary and its wire ids, and branch guards — enough to walk, print,\n")
	g.pf("# diff or bind any table value at runtime with no schema files on hand.\n")
	g.pf("# They are module attributes, so this costs a literal read rather than a\n")
	g.pf("# parse, and they are immutable, so any process may read them.\n\n")
	for _, st := range members {
		g.emitDescriptor(st)
	}
}

func (g *gen) emitDescriptor(st *ir.Struct) {
	g.owner = st
	guards := guardStrings(st)
	name := fn("table_type", st.Name)
	g.pf("@%s %%{\n", name)
	g.pf("  name: %q,\n", st.Name)
	g.pf("  new: {%s, :%s},\n", g.fileMod(st.Name), fn("defaults", st.Name))
	g.pf("  fields: [\n")
	for _, f := range st.Fields {
		g.pf("    %s,\n", g.descriptorRow(st, f, guards[f.Name]))
	}
	g.pf("  ]\n")
	g.pf("}\n")
	g.pf("def %s, do: @%s\n\n", name, name)
	g.owner = nil
}

// descriptorRow renders one field descriptor map.
func (g *gen) descriptorRow(st *ir.Struct, f *ir.Field, guard string) string {
	var b strings.Builder

	// the descriptor's kind column is the FIELD's kind, and for an array, a
	// string or a `bytes` it is the ELEMENT's: `bytes` rides as an array of u8
	// and its element kind is what tells the text form it is base64 rather than
	// a positional array of numbers.
	kind := ir.TableScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = ir.TableElemKind(f)
	}

	array := ":none"
	bound := int64(0)
	switch {
	case f.Type.Kind == ir.TString:
		array, bound = ":string", f.Type.Size
	case f.Type.Kind == ir.TBytes:
		array, bound = ":bytes", f.Type.Size
	case f.KeyEnum != "":
		array, bound = ":keyed", f.ArrayBound
	case f.Array == ir.ArrayFixed:
		array, bound = ":fixed", f.ArrayBound
	case f.Array == ir.ArrayCounted:
		array, bound = ":counted", f.ArrayBound
	}

	// the declared [min, max], and a bits(N) field's IMPLIED one: N bits hold
	// [0, 2^N - 1] whether or not the declaration spells it, and the text form's
	// walk is what needs it — the wire codec masks by the storage width, and a
	// text carries a number with no width at all
	rng := "nil"
	switch {
	case f.HasIntRange:
		rng = fmt.Sprintf("{%s, %s}", intLit(f.IntMin), intLit(f.IntMax))
	case f.HasFloatRange:
		rng = fmt.Sprintf("{%s, %s}", formatFloat(f.FMin, f.Type.Kind == ir.TFloat32), formatFloat(f.FMax, f.Type.Kind == ir.TFloat32))
	case f.Type.Kind == ir.TBits && f.Type.Width < 64:
		hi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		rng = fmt.Sprintf("{0, %s}", hi.String())
	}

	enum, flags, table, arms, unionNew := "nil", "nil", "nil", "nil", "nil"
	switch {
	case isEnum(f):
		e := enumOf(f)
		enum = g.enumVocabulary(e)
	case isFlags(f):
		fl := flagsOf(f)
		names := make([]string, 0, len(fl.Variants))
		for _, v := range fl.Variants {
			names = append(names, fmt.Sprintf("%q", v))
		}
		flags = "[" + strings.Join(names, ", ") + "]"
	case isUnion(f):
		un := unionOf(f)
		arms = fmt.Sprintf("{%s, :%s}", g.fileMod(un.Name), fn("table_union", un.Name))
		unionNew = fmt.Sprintf("{%s, :%s}", g.fileMod(un.Name), fn("union_defaults", un.Name))
	case isStruct(f):
		table = fmt.Sprintf("{%s, :%s}", g.fileMod(f.Type.Name), fn("table_type", f.Type.Name))
	}

	keyEnum := "nil"
	if f.KeyEnum != "" {
		keyEnum = g.enumVocabulary(f.KeyEnumRef)
	}

	fmt.Fprintf(&b, "%%{\n")
	fmt.Fprintf(&b, "      name: %q,\n", f.Name)
	fmt.Fprintf(&b, "      json: %q,\n", ir.TableFieldJsonKey(f))
	fmt.Fprintf(&b, "      type_name: %q,\n", tableFieldTypeName(f))
	fmt.Fprintf(&b, "      key: :%s,\n", f.Name)
	fmt.Fprintf(&b, "      id: 0x%04X,\n", ir.TableFieldId(f))
	fmt.Fprintf(&b, "      kind: %d,\n", kind)
	fmt.Fprintf(&b, "      array: %s,\n", array)
	fmt.Fprintf(&b, "      bound: %d,\n", bound)
	fmt.Fprintf(&b, "      tuple: %v,\n", f.KeyEnum != "" && g.keyedIsTuple())
	fmt.Fprintf(&b, "      optional: %v,\n", f.Type.Optional)
	fmt.Fprintf(&b, "      guard: %q,\n", guard)
	fmt.Fprintf(&b, "      table: %s,\n", table)
	fmt.Fprintf(&b, "      arms: %s,\n", arms)
	fmt.Fprintf(&b, "      union_new: %s,\n", unionNew)
	fmt.Fprintf(&b, "      enum: %s,\n", enum)
	fmt.Fprintf(&b, "      flags: %s,\n", flags)
	fmt.Fprintf(&b, "      key_enum: %s,\n", keyEnum)
	fmt.Fprintf(&b, "      range: %s,\n", rng)
	fmt.Fprintf(&b, "      storage_bytes: %d,\n", storageBytes(f))
	fmt.Fprintf(&b, "      elem_default: %s\n", g.elementDefault(f))
	fmt.Fprintf(&b, "    }")
	return b.String()
}

// enumVocabulary is the pair of name/value functions a descriptor carries for
// an enum, plus its extent.
func (g *gen) enumVocabulary(e *ir.Enum) string {
	home := g.fileMod(e.Name)
	lo := elixirName(e.Name)
	return fmt.Sprintf("%%{name: {%s, :enum_name_%s}, value: {%s, :enum_value_%s}, id: {%s, :table_id_%s}, max: %d}",
		home, lo, home, lo, home, lo, e.Max)
}

// storageBytes is the width of a field's own storage slot, which the text
// form's integer clamp bounds against. It is the wire kind's width for a plain
// integer, and the declared storage for `bits(N)`, which is wider than the kind
// it rides in — which is why a bits(N) field's own bound is the range column's
// job and not this one's.
func storageBytes(f *ir.Field) int {
	switch f.Type.Kind {
	case ir.TInt:
		return f.Type.Width / 8
	case ir.TBits:
		if f.Type.Width <= 32 {
			return 4
		}
		return 8
	}
	return tableKindWidth(ir.TableScalarKind(f))
}

// emitUnionInfos emits one arm table per union the file's closure reaches, and
// the union's own empty value. Position 0 is None, so a tag indexes the list
// directly.
func (g *gen) emitUnionInfos(members []*ir.Struct) {
	seen := map[string]*ir.Union{}
	for _, st := range members {
		for _, f := range st.Fields {
			if un := unionOf(f); un != nil {
				if home, ok := g.unit.DeclFile[un.Name]; ok && home != g.file.Base {
					continue
				}
				seen[un.Name] = un
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		g.emitUnionInfo(seen[name])
	}
}

func (g *gen) emitUnionInfo(un *ir.Union) {
	g.pf("# %s's arms, for the generic walk: position 0 is None, so a tag\n", un.Name)
	g.pf("# indexes this list directly (docs/SPEC-TABLES.md §8).\n")
	name := fn("table_union", un.Name)
	g.pf("@%s [\n", name)
	g.pf("  %%{name: \"None\", key: nil, table: nil},\n")
	for _, v := range un.Variants {
		g.pf("  %%{name: %q, key: :%s, table: {%s, :%s}},\n",
			v.Name, v.Name, g.fileMod(v.Type), fn("table_type", v.Type))
	}
	g.pf("]\n")
	g.pf("def %s, do: @%s\n\n", name, name)
	g.pf("def %s, do: %%%s{}\n\n", fn("union_defaults", un.Name), g.mod(un.Name))
}

// ---- the text form's per-table entry points (docs/SPEC-TABLES.md §16) ----

func (g *gen) emitJson(members []*ir.Struct) {
	g.pf("# ---- the text form (docs/SPEC-TABLES.md §16) ----\n")
	g.pf("#\n")
	g.pf("# One table, one text, one walk over the reflection descriptors. These are\n")
	g.pf("# the per-table entry points; the walk itself lives once in the shared\n")
	g.pf("# runtime, which is what makes the text form schema's rather than a\n")
	g.pf("# packer's (§16.1).\n\n")
	for _, st := range members {
		g.pf("# %s fills one %s from the §16 text, reporting every tolerance\n", fn("from_json", st.Name), st.Name)
		g.pf("# event. Unknown keys skip, a duplicate key is last-wins, a key with the\n")
		g.pf("# wrong JSON type is skipped and counted, never coerced.\n")
		g.pf("def %s(text), do: R.json_read(text, %s())\n\n", fn("from_json", st.Name), fn("table_type", st.Name))

		g.pf("# %s renders the §16 text: {:ok, text}, or :error where a value has\n", fn("to_json", st.Name))
		g.pf("# no text spelling at all (§16.3). The canonical text ends with exactly one\n")
		g.pf("# newline. %s! answers the text or raises.\n", fn("to_json", st.Name))
		g.pf("def %s(value), do: R.json_write(value, %s())\n\n", fn("to_json", st.Name), fn("table_type", st.Name))
		g.emitBang(fn("to_json", st.Name), "§16.3")

		g.pf("# THE MEASURE DEVIATION, named: a BEAM caller owns no buffer for the text\n")
		g.pf("# to be written into, so there is nothing for a measure pass to size. The\n")
		g.pf("# byte length is the text's own, and measure and write agree by\n")
		g.pf("# construction rather than by a second walk (§16.1).\n")
		g.pf("def %s(value) do\n", fn("to_json_measure", st.Name))
		g.pf("  with {:ok, text} <- %s(value), do: {:ok, byte_size(text)}\n", fn("to_json", st.Name))
		g.pf("end\n\n")
		g.emitBang(fn("to_json_measure", st.Name), "§16.3")
	}
}
