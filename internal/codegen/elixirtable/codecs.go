// TABLE-wire storage, codec and identity emission for Elixir
// (docs/SPEC-TABLES.md), mirroring internal/codegen/cpptable — the reference.
// Readers start from the declared defaults and overlay, skip unknown ids, skip
// kind mismatches, clamp out-of-range values, and count every event.
package elixirtable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableModule assembles <Base>Table.ex: one struct module per table the file
// declares, then the file-scope module carrying the per-enum wire identity,
// the codecs, the reflection descriptors and the text form's entry points.
func (g *gen) tableModule() []byte {
	members := g.members()
	if len(members) == 0 {
		return nil
	}

	// the struct modules come FIRST and in dependency order: a defstruct
	// default holding %Other{} is evaluated at compile time, so Other must
	// already be compiled (the packet emitter's own rule, SPEC §6.1).
	for _, st := range members {
		if st.IsTable {
			g.emitStorage(st)
		}
	}

	g.pf("defmodule %s.%sTable do\n", g.ns, moduleBase(g.file.Base))
	g.pf("%s", tableModuleBanner)
	g.indent = "  "
	g.pf("alias %s.TableRuntime, as: R\n\n", g.ns)

	for _, e := range g.tableEnums(members) {
		g.emitEnumIdentity(e)
	}
	for _, st := range members {
		g.owner = st
		g.emitDefaults(st)
	}
	for _, st := range members {
		g.owner = st
		g.emitKeyedAccessors(st)
	}
	for _, st := range members {
		g.owner = st
		g.emitMeasure(st)
		g.emitSave(st)
		g.emitLoad(st)
	}
	g.owner = nil
	g.emitUnionInfos(members)
	g.emitDescriptors(members)
	g.emitJson(members)
	g.indent = ""
	g.pf("end\n")

	var b strings.Builder
	b.WriteString(header(g.file.Base, g.unit.Package,
		fmt.Sprintf("the TABLE wire (docs/SPEC-TABLES.md); protocol id 0x%016X names packets only, and a table versions by field id", g.unit.ProtocolId)))
	b.WriteString("\n")
	b.WriteString(g.body.String())
	return []byte(b.String())
}

const tableModuleBanner = `  @moduledoc """
  The TABLE wire for this file's declarations (docs/SPEC-TABLES.md §3).

  measure_<name>/1 gives the exact wire size and writes nothing; save_<name>/1
  returns exactly that many bytes; load_<name>/1 returns {value, report} and
  reports every tolerance event (§4). The report is a VALUE the caller owns —
  the BEAM has no mutable struct, so it is threaded rather than pointed at.

  Nothing here holds a buffer: save builds an iolist of binary literals and the
  runtime flattens it once, and load walks sub-binaries of the caller's own
  binary, which the BEAM shares rather than copies.
  """

`

// tableEnums is every enum a file's closure members reach, sorted, so the
// per-enum wire identity is emitted once per file that DECLARES it.
func (g *gen) tableEnums(members []*ir.Struct) []*ir.Enum {
	seen := map[string]*ir.Enum{}
	for _, st := range members {
		for _, f := range st.Fields {
			if e := enumOf(f); e != nil {
				seen[e.Name] = e
			}
			if f.KeyEnumRef != nil {
				seen[f.KeyEnum] = f.KeyEnumRef
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*ir.Enum, 0, len(names))
	for _, name := range names {
		if home, ok := g.unit.DeclFile[name]; ok && home != g.file.Base {
			continue
		}
		out = append(out, seen[name])
	}
	return out
}

// ---- storage ----

func (g *gen) emitStorage(st *ir.Struct) {
	g.pf("# table %s — TABLE-wire storage (docs/SPEC-TABLES.md): every field carries\n", st.Name)
	g.pf("# its declared default at construction, so %%%s{} IS the value a read\n", g.mod(st.Name))
	g.pf("# starts from and the write side elides against.\n")
	g.pf("defmodule %s do\n", g.mod(st.Name))
	if len(st.Fields) == 0 {
		g.pf("  # empty body — presence is the payload\n")
		g.pf("  defstruct []\n")
		g.pf("end\n\n")
		return
	}
	g.owner = st
	parts := make([]string, 0, len(st.Fields))
	for _, f := range st.Fields {
		if note := g.storageNote(f); note != "" {
			g.pf("  # %s: %s\n", f.Name, note)
		}
		parts = append(parts, fmt.Sprintf("%s: %s", f.Name, g.storageDefault(f)))
	}
	g.owner = nil
	g.pf("  defstruct %s\n", strings.Join(parts, ",\n            "))
	g.pf("end\n\n")
}

// storageNote is what a COLD READER needs beside a folded literal: the variant
// an enum default names, the range a clamp compares against, and the shape a
// container's default takes. A defstruct default evaluates at compile time, so
// the emitter folds a symbolic default to its value — and a reader who cannot
// see the schema deserves to be told which value that was.
func (g *gen) storageNote(f *ir.Field) string {
	var notes []string
	switch {
	case f.Type.Optional:
		notes = append(notes, fmt.Sprintf("?%s — nil is the absence; presence decides whether it rides (§2.3)", tableFieldTypeName(f)))
	case f.Type.Kind == ir.TString:
		notes = append(notes, fmt.Sprintf("string(%d) — a binary whose byte_size IS the used length", f.Type.Size))
	case f.Type.Kind == ir.TBytes:
		notes = append(notes, fmt.Sprintf("bytes(%d) — a binary whose byte_size IS the used length", f.Type.Size))
	case f.KeyEnum != "":
		notes = append(notes, fmt.Sprintf("[%s] — a tuple of %d slots, key k at index k - 1 (§2.4)", f.KeyEnum, arrayLen(f)))
	case f.Array == ir.ArrayCounted:
		notes = append(notes, fmt.Sprintf("counted array — a list of up to %d elements, and length IS the count", f.ArrayBound))
	case f.Array == ir.ArrayFixed:
		notes = append(notes, fmt.Sprintf("fixed array — a list of exactly %d elements", f.ArrayBound))
	case isEnum(f) && f.HasDefault && f.DefVariant != "":
		notes = append(notes, fmt.Sprintf("%s.%s, folded — a defstruct default evaluates at compile time", f.Type.Name, f.DefVariant))
	case isEnum(f):
		notes = append(notes, fmt.Sprintf("%s — the ORDINAL; the wire rides the variant's name hash (§5)", f.Type.Name))
	case isFlags(f):
		notes = append(notes, fmt.Sprintf("%s — the raw mask; variants ride by BIT POSITION (§4)", f.Type.Name))
	case isUnion(f):
		notes = append(notes, fmt.Sprintf("union %s — the tag beside one pre-allocated arm per variant", f.Type.Name))
	}
	if f.HasIntRange {
		notes = append(notes, fmt.Sprintf("wire [%s, %s] — a value outside it CLAMPS and counts (§4)", intLit(f.IntMin), intLit(f.IntMax)))
	}
	if f.Guard != "" {
		notes = append(notes, fmt.Sprintf("under %s — a field its guard excludes is elided (§3)", f.Guard))
	}
	return strings.Join(notes, "; ")
}

// storageDefault is a field's construction value: the declared default, laid
// over the §5 zero form.
func (g *gen) storageDefault(f *ir.Field) string {
	switch {
	case f.Type.Optional:
		// `?T`: absent until set — `nil` IS the absence (the package note)
		return "nil"
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		return "<<>>"
	case f.KeyEnum != "":
		// a tuple of E.Max slots; index k - 1 is variant k's (§2.4)
		return fmt.Sprintf("List.to_tuple(List.duplicate(%s, %d))", g.elementDefault(f), arrayLen(f))
	case f.Array == ir.ArrayFixed:
		return fmt.Sprintf("List.duplicate(%s, %d)", g.elementDefault(f), f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		return "[]"
	}
	return g.scalarDefault(f)
}

// elementDefault is the value an array or keyed slot carries at construction.
func (g *gen) elementDefault(f *ir.Field) string {
	if isStruct(f) {
		return "%" + g.mod(f.Type.Name) + "{}"
	}
	return g.scalarDefault(f)
}

// scalarDefault is a scalar field's declared default as an Elixir expression —
// the value the write side elides against.
func (g *gen) scalarDefault(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TFloat32:
		if f.HasDefault {
			return formatFloat(f.DefFloat, true)
		}
		return "0.0"
	case ir.TFloat64:
		if f.HasDefault {
			return formatFloat(f.DefFloat, false)
		}
		return "0.0"
	case ir.TInt, ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			return intLit(f.DefInt)
		}
		return "0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				return fmt.Sprintf("%d", enumValue(enumOf(f), f.DefVariant))
			}
			return "0"
		case *ir.Flags:
			return "0"
		case *ir.Struct:
			return "%" + g.mod(f.Type.Name) + "{}"
		case *ir.Union:
			return "%" + g.mod(f.Type.Name) + "{}"
		}
	}
	return "0"
}

func enumValue(e *ir.Enum, name string) int64 {
	if name == "None" {
		return 0
	}
	for i, v := range e.Variants {
		if v == name {
			return int64(i + 1)
		}
	}
	return 0
}

// emitDefaults is the value a load starts from: %Mod{} for a table this
// backend declares, and the packet emitter's own construction for a closure
// type. One function so a codec never spells the choice twice.
func (g *gen) emitDefaults(st *ir.Struct) {
	g.pf("# %s is %s at its declared defaults — what a read overlays\n", fn("defaults", st.Name), st.Name)
	g.pf("# and what the write side elides against (docs/SPEC-TABLES.md §4).\n")
	g.pf("def %s, do: %%%s{}\n\n", fn("defaults", st.Name), g.mod(st.Name))
}

// ---- the enum-keyed array's surface (docs/SPEC-TABLES.md §2.4) ----
//
// THE SHIFT IS NEVER WRITTEN AT A CALL SITE. The accessor takes the KEY — the
// enum value, which is this target's integer — refuses None and a key past
// E.Max in EVERY configuration, and subtracts the one itself. The BEAM has no
// compile-out assert, so "the refusal stands in every build" costs nothing to
// hold here: an ArgumentError is the packet emitter's own misuse convention.
//
// ITERATION IS THE SAFETY, and it yields the KEY beside the element — never a
// storage index, in this port as in every other.
func (g *gen) emitKeyedAccessors(st *ir.Struct) {
	lo := elixirName(st.Name)
	for _, f := range st.Fields {
		if f.KeyEnum == "" {
			continue
		}
		max := arrayLen(f)
		read, write := g.keyedRead(f), g.keyedWrite(f)
		g.pf("# %s.%s is keyed by %s: one slot per named variant, and NOTHING is\n", st.Name, f.Name, f.KeyEnum)
		g.pf("# stored for None (docs/SPEC-TABLES.md §2.4). The key k lives at storage\n")
		g.pf("# index k - 1, and these three functions are the only place that\n")
		g.pf("# subtraction appears.\n")
		g.pf("def %s_%s_at(value, key) when key >= 1 and key <= %d, do: %s\n\n", lo, f.Name, max, read)
		g.emitKeyedRefusal(fmt.Sprintf("%s_%s_at(_value, key)", lo, f.Name), st, f, max)

		g.pf("# the WRITE half, because a BEAM value is immutable: iteration reads and\n")
		g.pf("# this places. It refuses the same two keys, at the same one place.\n")
		g.pf("def %s_%s_put(value, key, element) when key >= 1 and key <= %d, do: %s\n\n", lo, f.Name, max, write)
		g.emitKeyedRefusal(fmt.Sprintf("%s_%s_put(_value, key, _element)", lo, f.Name), st, f, max)

		g.pf("# ITERATION yields the KEY beside the element, keys 1..%d — a storage\n", max)
		g.pf("# index reaches no call site (docs/SPEC-TABLES.md §2.4).\n")
		g.pf("def %s_%s_each(value) do\n", lo, f.Name)
		g.pf("  %s\n", g.keyedList(f))
		g.pf("  |> Enum.with_index(1)\n")
		g.pf("  |> Enum.map(fn {element, key} -> {key, element} end)\n")
		g.pf("end\n\n")
	}
}

func (g *gen) emitKeyedRefusal(head string, st *ir.Struct, f *ir.Field, max int64) {
	g.pf("def %s do\n", head)
	g.pf("  raise ArgumentError,\n")
	g.pf("        \"#{inspect(key)} keys no slot of %s.%s: None keys nothing, and \" <>\n", st.Name, f.Name)
	g.pf("          \"%s.Max is %d (docs/SPEC-TABLES.md §2.4)\"\n", f.KeyEnum, max)
	g.pf("end\n\n")
}

func (g *gen) keyedRead(f *ir.Field) string {
	if g.keyedIsTuple() {
		return fmt.Sprintf("elem(value.%s, key - 1)", f.Name)
	}
	return fmt.Sprintf("Enum.at(value.%s, key - 1)", f.Name)
}

func (g *gen) keyedWrite(f *ir.Field) string {
	if g.keyedIsTuple() {
		return fmt.Sprintf("%%{value | %s: put_elem(value.%s, key - 1, element)}", f.Name, f.Name)
	}
	return fmt.Sprintf("%%{value | %s: List.replace_at(value.%s, key - 1, element)}", f.Name, f.Name)
}

func (g *gen) keyedList(f *ir.Field) string {
	if g.keyedIsTuple() {
		return fmt.Sprintf("Tuple.to_list(value.%s)", f.Name)
	}
	return "value." + f.Name
}

// ---- the per-enum wire identity (docs/SPEC-TABLES.md §5) ----

func (g *gen) emitEnumIdentity(e *ir.Enum) {
	lo := elixirName(e.Name)
	g.pf("# %s on the TABLE wire: a value rides as the u16 hash of its VARIANT\n", e.Name)
	g.pf("# NAME, so a variant may be added anywhere, removed, or reordered and old\n")
	g.pf("# data still reads (docs/SPEC-TABLES.md §5). None is the one reserved id, 0.\n")
	g.pf("def table_id_%s(0), do: 0\n", lo)
	for i, v := range e.Variants {
		g.pf("def table_id_%s(%d), do: 0x%04X\n", lo, i+1, ir.VariantId(v))
	}
	g.pf("# no variant names this value: no wire identity\n")
	g.pf("def table_id_%s(_), do: nil\n\n", lo)

	g.pf("def table_value_%s(0), do: 0\n", lo)
	for i, v := range e.Variants {
		g.pf("def table_value_%s(0x%04X), do: %d\n", lo, ir.VariantId(v), i+1)
	}
	g.pf("# an id this build cannot name\n")
	g.pf("def table_value_%s(_), do: nil\n\n", lo)

	g.pf("def enum_name_%s(0), do: \"None\"\n", lo)
	for i, v := range e.Variants {
		g.pf("def enum_name_%s(%d), do: %q\n", lo, i+1, v)
	}
	g.pf("def enum_name_%s(_), do: nil\n\n", lo)

	g.pf("def enum_value_%s(\"None\"), do: 0\n", lo)
	for i, v := range e.Variants {
		g.pf("def enum_value_%s(%q), do: %d\n", lo, v, i+1)
	}
	g.pf("def enum_value_%s(_), do: nil\n\n", lo)
}

// enumIdent renders a call to one of the per-enum identity functions, reaching
// across files where the enum is declared elsewhere in the unit.
func (g *gen) enumIdent(e *ir.Enum, verb, arg string) string {
	name := verb + "_" + elixirName(e.Name)
	if home, ok := g.unit.DeclFile[e.Name]; ok && home != g.file.Base {
		return fmt.Sprintf("%s.%sTable.%s(%s)", g.ns, moduleBase(home), name, arg)
	}
	return fmt.Sprintf("%s(%s)", name, arg)
}

// ---- guards ----

// guardExprs composes each guarded field's branch condition from the wire
// tree, so the writer and measurer keep untaken-branch fields off the wire.
func guardExprs(st *ir.Struct) map[string]string { return guardWalk(st, "value.") }

// guardStrings is the value-free twin for the reflection descriptors.
func guardStrings(st *ir.Struct) map[string]string { return guardWalk(st, "") }

func guardWalk(st *ir.Struct, prefix string) map[string]string {
	guards := map[string]string{}
	var walk func(items []ir.Item, cond string)
	walk = func(items []ir.Item, cond string) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if cond != "" {
					guards[item.F.Name] = cond
				}
			case *ir.Branch:
				pos, neg := prefix+item.Cond, "!"+prefix+item.Cond
				if prefix == "" {
					pos, neg = item.Cond, "!"+item.Cond
				}
				if item.Neg {
					pos, neg = neg, pos
				}
				and := func(a, b string) string {
					if a == "" {
						return b
					}
					return a + " && " + b
				}
				walk(item.Then, and(cond, pos))
				walk(item.Else, and(cond, neg))
			}
		}
	}
	walk(st.Items, "")
	return guards
}

// ---- measure ----

func (g *gen) emitMeasure(st *ir.Struct) {
	name := fn("measure", st.Name)
	g.pf("# %s is the EXACT encoded size of a value, with no writing. A value\n", name)
	g.pf("# violating its storage invariants measures as -1, exactly as the write side\n")
	g.pf("# refuses it (docs/SPEC-TABLES.md §5).\n")
	g.pf("def %s(value) do\n", name)
	g.pf("  try do\n")
	g.pf("    %s(value)\n", fn("measure_body", st.Name))
	g.pf("  catch\n")
	g.pf("    :refused -> -1\n")
	g.pf("  end\n")
	g.pf("end\n\n")

	body := fn("measure_body", st.Name)
	if len(st.Fields) == 0 {
		g.pf("def %s(_value), do: 2 # terminator: an empty type's presence is its payload\n\n", body)
		return
	}
	g.pf("def %s(value) do\n", body)
	terms := []string{"2"}
	guards := guardExprs(st)
	for _, f := range st.Fields {
		terms = append(terms, fmt.Sprintf("m_%s_%s(value)", elixirName(st.Name), f.Name))
	}
	g.pf("  # 2 is the terminator\n")
	g.pf("  %s\n", strings.Join(terms, " +\n    "))
	g.pf("end\n\n")

	for _, f := range st.Fields {
		g.emitMeasureField(st, f, guards[f.Name])
	}
}

func (g *gen) emitMeasureField(st *ir.Struct, f *ir.Field, guard string) {
	name := fmt.Sprintf("m_%s_%s", elixirName(st.Name), f.Name)
	g.pf("defp %s(value) do\n", name)
	g.indent += "  "
	if guard != "" {
		g.pf("# %s — a field under a false guard is elided whatever its storage holds\n", guard)
		g.pf("if %s do\n", guard)
		g.indent += "  "
	}
	g.measureBody(f)
	if guard != "" {
		g.indent = strings.TrimSuffix(g.indent, "  ")
		g.pf("else\n  0\nend\n")
	}
	g.indent = strings.TrimSuffix(g.indent, "  ")
	g.pf("end\n\n")
}

func (g *gen) measureBody(f *ir.Field) {
	kind := ir.TableScalarKind(f)
	width := tableKindWidth(kind)
	switch {
	case f.Type.Optional:
		// an optional's PRESENCE is the payload: it rides even when the value
		// is entirely default, so absent and present-at-default stay distinct
		g.pf("# ?%s: presence decides, not content\n", tableFieldTypeName(f))
		g.pf("case value.%s do\n", f.Name)
		g.pf("  nil ->\n    0\n\n")
		g.pf("  slot ->\n")
		switch {
		case kind == tkTable:
			g.pf("    body = %s(slot)\n", g.call("measure_body", f.Type.Name))
			g.pf("    3 + 4 + body\n")
		case isEnum(f):
			g.pf("    if %s == nil, do: throw(:refused)\n", g.enumIdent(enumOf(f), "table_id", "slot"))
			g.pf("    3 + 2\n")
		default:
			g.pf("    _ = slot\n    3 + %d\n", width)
		}
		g.pf("end\n")
	case f.Type.Kind == ir.TString:
		g.pf("used = byte_size(value.%s)\n", f.Name)
		g.pf("if used > %d, do: throw(:refused) # storage invariant\n", f.Type.Size)
		g.pf("if used > 0, do: 3 + 4 + used, else: 0\n")
	case f.Type.Kind == ir.TBytes:
		g.pf("used = byte_size(value.%s)\n", f.Name)
		g.pf("if used > %d, do: throw(:refused) # storage invariant\n", f.Type.Size)
		g.pf("if used > 0, do: 3 + 4 + 5 + used, else: 0\n")
	case f.KeyEnum != "":
		g.measureKeyed(f, kind, width)
	case f.Array == ir.ArrayCounted:
		g.pf("count = length(value.%s)\n", f.Name)
		g.pf("if count > %d, do: throw(:refused) # storage invariant\n", f.ArrayBound)
		g.pf("if count == 0 do\n  0\nelse\n")
		g.indent += "  "
		g.measureArrayBody(f, kind, width, "count")
		g.indent = strings.TrimSuffix(g.indent, "  ")
		g.pf("end\n")
	case f.Array == ir.ArrayFixed:
		g.pf("if length(value.%s) != %d, do: throw(:refused) # storage invariant\n", f.Name, f.ArrayBound)
		if kind == tkTable {
			// a fixed array of TABLES always rides: an all-default test would
			// have to measure every element to answer, so the form is the same
			// shape a counted one takes
			g.measureArrayBody(f, kind, width, fmt.Sprintf("%d", f.ArrayBound))
			break
		}
		g.pf("if Enum.all?(value.%s, &(&1 == %s)) do\n  0\nelse\n", f.Name, g.arrayElementDefault(f))
		g.indent += "  "
		g.measureArrayBody(f, kind, width, fmt.Sprintf("%d", f.ArrayBound))
		g.indent = strings.TrimSuffix(g.indent, "  ")
		g.pf("end\n")
	case kind == tkTable:
		g.pf("body = %s(value.%s)\n", g.call("measure_body", f.Type.Name), f.Name)
		g.pf("# an all-default nested table elides\n")
		g.pf("if body > 2, do: 3 + 4 + body, else: 0\n")
	case kind == tkUnion:
		g.measureUnion(f)
	case isEnum(f):
		g.pf("if value.%s == %s do\n  0\nelse\n", f.Name, g.scalarDefault(f))
		g.pf("  if %s == nil, do: throw(:refused)\n", g.enumIdent(enumOf(f), "table_id", "value."+f.Name))
		g.pf("  3 + 2 # the variant's name hash\n")
		g.pf("end\n")
	default:
		g.pf("if %s, do: 3 + %d, else: 0\n", g.nonDefaultTest(f), width)
	}
}

// nonDefaultTest is the condition a scalar field rides under: it is not at its
// declared default. A BOOL reads as itself, which is the same test written the
// way the language writes it.
func (g *gen) nonDefaultTest(f *ir.Field) string {
	if f.Type.Kind == ir.TBool {
		if f.HasDefault && f.DefBool {
			return "not value." + f.Name
		}
		return "value." + f.Name
	}
	return fmt.Sprintf("value.%s != %s", f.Name, g.scalarDefault(f))
}

// arrayElementDefault is the value a fixed array's slots are compared against
// for the all-default elision.
func (g *gen) arrayElementDefault(f *ir.Field) string {
	return g.elementDefault(f)
}

func (g *gen) measureArrayBody(f *ir.Field, kind, width int, count string) {
	if kind == tkTable {
		g.pf("Enum.reduce(value.%s, 3 + 4 + 5, fn element, acc ->\n", f.Name)
		g.pf("  acc + 4 + %s(element)\n", g.call("measure_body", f.Type.Name))
		g.pf("end)\n")
		return
	}
	if isEnum(f) {
		g.pf("# every element must be nameable\n")
		g.pf("Enum.each(value.%s, fn element ->\n", f.Name)
		g.pf("  if %s == nil, do: throw(:refused)\n", g.enumIdent(enumOf(f), "table_id", "element"))
		g.pf("end)\n")
	}
	g.pf("3 + 4 + 5 + %s * %d\n", count, width)
}

func (g *gen) measureUnion(f *ir.Field) {
	un := unionOf(f)
	g.pf("case value.%s.type do\n", f.Name)
	g.pf("  0 ->\n    0 # None elides — TLV absence is the None\n\n")
	for i, v := range un.Variants {
		g.pf("  %d ->\n", i+1)
		g.pf("    # the u16 ARM ID, then the arm length-prefixed\n")
		g.pf("    3 + 2 + 4 + %s(value.%s.%s)\n\n", g.call("measure_body", v.Type), f.Name, v.Name)
	}
	g.pf("  _ ->\n    throw(:refused) # a tag no arm names\n")
	g.pf("end\n")
}

func (g *gen) measureKeyed(f *ir.Field, kind, width int) {
	g.pf("# [%s]: every stored slot is a named variant's; i is the STORAGE index\n", f.KeyEnum)
	g.pf("# and the key it holds is i + 1 (docs/SPEC-TABLES.md §2.4)\n")
	g.pf("{pairs, keyed_bytes} =\n")
	g.pf("  Enum.reduce(0..%d, {0, 0}, fn i, {pairs, bytes} ->\n", arrayLen(f)-1)
	g.pf("    slot = %s\n", g.keyedAt(f, "i"))
	g.indent += "    "
	switch {
	case kind == tkTable:
		g.pf("element = %s(slot)\n", g.call("measure_body", f.Type.Name))
		g.pf("if element <= 2 do\n")
		g.pf("  {pairs, bytes} # an all-default slot elides\n")
		g.pf("else\n")
		g.pf("  if %s == nil, do: throw(:refused)\n", g.enumIdent(f.KeyEnumRef, "table_id", "i + 1"))
		g.pf("  {pairs + 1, bytes + 2 + 4 + element} # key, length, body\n")
		g.pf("end\n")
	case isEnum(f):
		g.pf("if slot == 0 do\n")
		g.pf("  {pairs, bytes} # a default slot elides\n")
		g.pf("else\n")
		g.pf("  if %s == nil, do: throw(:refused)\n", g.enumIdent(enumOf(f), "table_id", "slot"))
		g.pf("  if %s == nil, do: throw(:refused)\n", g.enumIdent(f.KeyEnumRef, "table_id", "i + 1"))
		g.pf("  {pairs + 1, bytes + 2 + 4 + %d} # key, length, element\n", width)
		g.pf("end\n")
	default:
		g.pf("if slot == %s do\n", g.elementDefault(f))
		g.pf("  {pairs, bytes} # a default slot elides\n")
		g.pf("else\n")
		g.pf("  if %s == nil, do: throw(:refused)\n", g.enumIdent(f.KeyEnumRef, "table_id", "i + 1"))
		g.pf("  {pairs + 1, bytes + 2 + 4 + %d} # key, length, element\n", width)
		g.pf("end\n")
	}
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("  end)\n\n")
	g.pf("if pairs > 0, do: 3 + 4 + 5 + keyed_bytes, else: 0\n")
}

// ---- save ----

func (g *gen) emitSave(st *ir.Struct) {
	name := fn("save", st.Name)
	g.pf("# %s writes exactly %s(value) bytes.\n", name, fn("measure", st.Name))
	g.pf("def %s(value) do\n", name)
	g.pf("  try do\n")
	g.pf("    IO.iodata_to_binary(%s(value))\n", fn("save_body", st.Name))
	g.pf("  catch\n")
	g.pf("    :refused -> :refused\n")
	g.pf("  end\n")
	g.pf("end\n\n")

	body := fn("save_body", st.Name)
	if len(st.Fields) == 0 {
		g.pf("def %s(_value), do: [<<0::little-unsigned-16>>] # terminator\n\n", body)
		return
	}
	g.pf("def %s(value) do\n", body)
	parts := make([]string, 0, len(st.Fields)+1)
	for _, f := range st.Fields {
		parts = append(parts, fmt.Sprintf("s_%s_%s(value)", elixirName(st.Name), f.Name))
	}
	parts = append(parts, "<<0::little-unsigned-16>>")
	g.pf("  [\n    %s\n  ]\n", strings.Join(parts, ",\n    "))
	g.pf("end\n\n")

	guards := guardExprs(st)
	for _, f := range st.Fields {
		g.emitSaveField(st, f, guards[f.Name])
	}
}

func (g *gen) emitSaveField(st *ir.Struct, f *ir.Field, guard string) {
	name := fmt.Sprintf("s_%s_%s", elixirName(st.Name), f.Name)
	g.pf("defp %s(value) do\n", name)
	g.indent += "  "
	if guard != "" {
		g.pf("if %s do\n", guard)
		g.indent += "  "
	}
	g.saveBody(f)
	if guard != "" {
		g.indent = strings.TrimSuffix(g.indent, "  ")
		g.pf("else\n  []\nend\n")
	}
	g.indent = strings.TrimSuffix(g.indent, "  ")
	g.pf("end\n\n")
}

// tag renders the two framing bytes a field opens with: its id and its kind.
func tag(f *ir.Field) string {
	return fmt.Sprintf("<<0x%04X::little-unsigned-16, %d::little-unsigned-8>>",
		ir.TableFieldId(f), ir.TableFieldKind(f))
}

func (g *gen) saveBody(f *ir.Field) {
	kind := ir.TableScalarKind(f)
	switch {
	case f.Type.Optional:
		g.pf("case value.%s do\n", f.Name)
		g.pf("  nil ->\n    []\n\n")
		g.pf("  slot ->\n")
		switch {
		case kind == tkTable:
			g.pf("    body = %s(slot)\n", g.call("measure_body", f.Type.Name))
			g.pf("    [%s, <<body::little-unsigned-32>>, %s(slot)]\n", tag(f), g.call("save_body", f.Type.Name))
		case isEnum(f):
			g.pf("    id = %s\n", g.enumIdent(enumOf(f), "table_id", "slot"))
			g.pf("    if id == nil, do: throw(:refused)\n")
			g.pf("    [%s, <<id::little-unsigned-16>>]\n", tag(f))
		default:
			g.pf("    [%s, <<%s::%s>>]\n", tag(f), g.scalarToWire(f, "slot"), wireSpec(kind))
		}
		g.pf("end\n")
	case f.Type.Kind == ir.TString:
		g.pf("used = byte_size(value.%s)\n", f.Name)
		g.pf("if used > %d, do: throw(:refused) # storage invariant\n", f.Type.Size)
		g.pf("if used == 0 do\n  []\nelse\n")
		g.pf("  [%s, <<used::little-unsigned-32>>, value.%s]\nend\n", tag(f), f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("used = byte_size(value.%s)\n", f.Name)
		g.pf("if used > %d, do: throw(:refused) # storage invariant\n", f.Type.Size)
		g.pf("if used == 0 do\n  []\nelse\n")
		g.pf("  [\n")
		g.pf("    %s,\n", tag(f))
		g.pf("    <<5 + used::little-unsigned-32, %d::little-unsigned-8, used::little-unsigned-32>>,\n", ir.TableElemKind(f))
		g.pf("    value.%s\n  ]\nend\n", f.Name)
	case f.KeyEnum != "":
		g.saveKeyed(f, kind)
	case f.Array == ir.ArrayCounted:
		g.pf("count = length(value.%s)\n", f.Name)
		g.pf("if count > %d, do: throw(:refused) # storage invariant\n", f.ArrayBound)
		g.pf("if count == 0 do\n  []\nelse\n")
		g.indent += "  "
		g.saveArrayBody(f, kind, "count")
		g.indent = strings.TrimSuffix(g.indent, "  ")
		g.pf("end\n")
	case f.Array == ir.ArrayFixed:
		g.pf("if length(value.%s) != %d, do: throw(:refused) # storage invariant\n", f.Name, f.ArrayBound)
		if kind == tkTable {
			g.saveArrayBody(f, kind, fmt.Sprintf("%d", f.ArrayBound))
			break
		}
		g.pf("if Enum.all?(value.%s, &(&1 == %s)) do\n  []\nelse\n", f.Name, g.arrayElementDefault(f))
		g.indent += "  "
		g.saveArrayBody(f, kind, fmt.Sprintf("%d", f.ArrayBound))
		g.indent = strings.TrimSuffix(g.indent, "  ")
		g.pf("end\n")
	case kind == tkTable:
		g.pf("body = %s(value.%s)\n", g.call("measure_body", f.Type.Name), f.Name)
		g.pf("# an all-default nested table elides\n")
		g.pf("if body <= 2 do\n  []\nelse\n")
		g.pf("  [%s, <<body::little-unsigned-32>>, %s(value.%s)]\nend\n",
			tag(f), g.call("save_body", f.Type.Name), f.Name)
	case kind == tkUnion:
		g.saveUnion(f)
	case isEnum(f):
		g.pf("if value.%s == %s do\n  []\nelse\n", f.Name, g.scalarDefault(f))
		g.pf("  id = %s\n", g.enumIdent(enumOf(f), "table_id", "value."+f.Name))
		g.pf("  if id == nil, do: throw(:refused)\n")
		g.pf("  [%s, <<id::little-unsigned-16>>]\nend\n", tag(f))
	default:
		g.pf("if %s do\n", g.nonDefaultTest(f))
		g.pf("  [%s, <<%s::%s>>]\nelse\n  []\nend\n",
			tag(f), g.scalarToWire(f, "value."+f.Name), wireSpec(kind))
	}
}

func (g *gen) saveArrayBody(f *ir.Field, kind int, count string) {
	elemKind := ir.TableElemKind(f)
	switch {
	case kind == tkTable:
		g.pf("elements =\n")
		g.pf("  Enum.map(value.%s, fn element ->\n", f.Name)
		g.pf("    [<<%s(element)::little-unsigned-32>>, %s(element)]\n",
			g.call("measure_body", f.Type.Name), g.call("save_body", f.Type.Name))
		g.pf("  end)\n\n")
		g.pf("body = IO.iodata_to_binary(elements)\n")
		g.pf("[\n")
		g.pf("  %s,\n", tag(f))
		g.pf("  <<5 + byte_size(body)::little-unsigned-32, %d::little-unsigned-8, %s::little-unsigned-32>>,\n", elemKind, count)
		g.pf("  body\n]\n")
	case isEnum(f):
		g.pf("elements =\n")
		g.pf("  Enum.map(value.%s, fn element ->\n", f.Name)
		g.pf("    id = %s\n", g.enumIdent(enumOf(f), "table_id", "element"))
		g.pf("    if id == nil, do: throw(:refused)\n")
		g.pf("    <<id::little-unsigned-16>>\n")
		g.pf("  end)\n\n")
		g.pf("[\n")
		g.pf("  %s,\n", tag(f))
		g.pf("  <<5 + %s * 2::little-unsigned-32, %d::little-unsigned-8, %s::little-unsigned-32>>,\n", count, elemKind, count)
		g.pf("  elements\n]\n")
	default:
		width := tableKindWidth(kind)
		g.pf("elements =\n")
		g.pf("  Enum.map(value.%s, fn element -> <<%s::%s>> end)\n\n",
			f.Name, g.scalarToWire(f, "element"), wireSpec(kind))
		g.pf("[\n")
		g.pf("  %s,\n", tag(f))
		g.pf("  <<5 + %s * %d::little-unsigned-32, %d::little-unsigned-8, %s::little-unsigned-32>>,\n",
			count, width, elemKind, count)
		g.pf("  elements\n]\n")
	}
}

func (g *gen) saveUnion(f *ir.Field) {
	un := unionOf(f)
	g.pf("# the ARM ID is the hash of the arm's NAME (docs/SPEC-TABLES.md §5), so\n")
	g.pf("# arms may be added anywhere, removed and reordered\n")
	g.pf("case value.%s.type do\n", f.Name)
	g.pf("  0 ->\n    []\n\n")
	for i, v := range un.Variants {
		g.pf("  %d ->\n", i+1)
		g.pf("    arm = value.%s.%s\n", f.Name, v.Name)
		g.pf("    body = %s(arm)\n", g.call("measure_body", v.Type))
		g.pf("    [\n")
		g.pf("      %s,\n", tag(f))
		g.pf("      <<0x%04X::little-unsigned-16, body::little-unsigned-32>>,\n", ir.VariantId(v.Name))
		g.pf("      %s(arm)\n    ]\n\n", g.call("save_body", v.Type))
	}
	g.pf("  _ ->\n    throw(:refused)\n")
	g.pf("end\n")
}

func (g *gen) saveKeyed(f *ir.Field, kind int) {
	g.pf("# KIND 16, not 14: a keyed body and a positional one are incompatible, so\n")
	g.pf("# a reader of the other kind must see a kind mismatch and skip, never\n")
	g.pf("# misdecode (docs/SPEC-TABLES.md §3.2). ASCENDING BY VARIANT ORDINAL, which\n")
	g.pf("# is slot order — this writer's choice; a reader finds each by its key.\n")
	g.pf("pairs =\n")
	g.pf("  Enum.flat_map(0..%d, fn i ->\n", arrayLen(f)-1)
	g.pf("    slot = %s\n", g.keyedAt(f, "i"))
	g.indent += "    "
	g.pf("key = %s\n", g.enumIdent(f.KeyEnumRef, "table_id", "i + 1"))
	switch {
	case kind == tkTable:
		g.pf("element = %s(slot)\n\n", g.call("measure_body", f.Type.Name))
		g.pf("if element <= 2 do\n")
		g.pf("  [] # an all-default slot elides\n")
		g.pf("else\n")
		g.pf("  if key == nil, do: throw(:refused)\n")
		g.pf("  # the slot's VARIANT id, not its position\n")
		g.pf("  [[<<key::little-unsigned-16, element::little-unsigned-32>>, %s(slot)]]\n", g.call("save_body", f.Type.Name))
		g.pf("end\n")
	case isEnum(f):
		g.pf("if slot == 0 do\n")
		g.pf("  [] # a default slot elides\n")
		g.pf("else\n")
		g.pf("  id = %s\n", g.enumIdent(enumOf(f), "table_id", "slot"))
		g.pf("  if id == nil or key == nil, do: throw(:refused)\n")
		g.pf("  [<<key::little-unsigned-16, 2::little-unsigned-32, id::little-unsigned-16>>]\n")
		g.pf("end\n")
	default:
		width := tableKindWidth(kind)
		g.pf("if slot == %s do\n", g.elementDefault(f))
		g.pf("  [] # a default slot elides\n")
		g.pf("else\n")
		g.pf("  if key == nil, do: throw(:refused)\n")
		g.pf("  [<<key::little-unsigned-16, %d::little-unsigned-32, %s::%s>>]\n",
			width, g.scalarToWire(f, "slot"), wireSpec(kind))
		g.pf("end\n")
	}
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("  end)\n\n")
	g.pf("if pairs == [] do\n  []\nelse\n")
	g.pf("  body = IO.iodata_to_binary(pairs)\n")
	g.pf("  [\n")
	g.pf("    %s,\n", tag(f))
	g.pf("    <<5 + byte_size(body)::little-unsigned-32, %d::little-unsigned-8,\n", ir.TableScalarKind(f))
	g.pf("      length(pairs)::little-unsigned-32>>,\n")
	g.pf("    body\n  ]\nend\n")
}

// scalarToWire renders the expression that puts one scalar into its wire word.
func (g *gen) scalarToWire(f *ir.Field, expr string) string {
	switch f.Type.Kind {
	case ir.TBool:
		return fmt.Sprintf("R.bool_bits(%s)", expr)
	case ir.TFloat32:
		return fmt.Sprintf("R.f32_bits(%s)", expr)
	case ir.TFloat64:
		return fmt.Sprintf("R.f64_bits(%s)", expr)
	}
	return expr
}

// ---- load ----

func (g *gen) emitLoad(st *ir.Struct) {
	lo := elixirName(st.Name)
	g.pf("# %s overlays one value from a table body and reports every\n", fn("load", st.Name))
	g.pf("# tolerance event (docs/SPEC-TABLES.md §4). The report is the caller's.\n")
	g.pf("def %s(data), do: %s(data, R.report())\n\n", fn("load", st.Name), fn("load", st.Name))
	g.pf("def %s(data, report), do: %s(data, report)\n\n",
		fn("load", st.Name), fn("load_body", st.Name))

	g.pf("# the read starts from the DECLARED DEFAULTS and overlays, so a field the\n")
	g.pf("# body never mentions reads as its default and a repeated id re-establishes\n")
	g.pf("# nothing an earlier occurrence left (docs/SPEC-TABLES.md §4).\n")
	g.pf("def %s(data, report) do\n", fn("load_body", st.Name))
	g.pf("  fields_%s(data, %s(), report)\n", lo, fn("defaults", st.Name))
	g.pf("end\n\n")

	g.pf("defp fields_%s(<<0::little-unsigned-16, _rest::binary>>, value, report), do: {value, report}\n\n", lo)
	g.pf("defp fields_%s(<<id::little-unsigned-16, kind::little-unsigned-8, rest::binary>>, value, report) do\n", lo)
	g.pf("  result =\n")
	g.pf("    case id do\n")
	for _, f := range st.Fields {
		g.pf("      0x%04X -> f_%s_%s(kind, rest, value, report)\n", ir.TableFieldId(f), lo, f.Name)
	}
	g.pf("      _ -> R.skip_unknown(kind, rest, value, report)\n")
	g.pf("    end\n\n")
	g.pf("  case result do\n")
	g.pf("    {:ok, rest2, value2, report2} -> fields_%s(rest2, value2, report2)\n", lo)
	g.pf("    {:stop, value2, report2} -> {value2, R.malformed(report2)}\n")
	g.pf("  end\n")
	g.pf("end\n\n")
	g.pf("defp fields_%s(_short, value, report), do: {value, R.malformed(report)}\n\n", lo)

	for _, f := range st.Fields {
		g.emitLoadField(st, f)
	}
}

func (g *gen) emitLoadField(st *ir.Struct, f *ir.Field) {
	lo := elixirName(st.Name)
	name := fmt.Sprintf("f_%s_%s", lo, f.Name)
	kind := ir.TableScalarKind(f)
	g.pf("defp %s(%d, rest, value, report) do\n", name, ir.TableFieldKind(f))
	g.indent += "  "
	g.loadPayload(f, kind)
	g.indent = strings.TrimSuffix(g.indent, "  ")
	g.pf("end\n\n")
	g.pf("defp %s(kind, rest, value, report) do\n", name)
	g.pf("  # a field that changed KIND between builds: skipped, never misdecoded\n")
	g.pf("  R.skip_mismatch(kind, rest, value, report)\n")
	g.pf("end\n\n")
}

// place renders the store of one decoded value into the instance, threading
// the optional's presence: `nil` is the absence, so placing a value IS the
// presence.
func place(f *ir.Field, expr string) string {
	return fmt.Sprintf("%%{value | %s: %s}", f.Name, expr)
}

func (g *gen) loadPayload(f *ir.Field, kind int) {
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("case rest do\n")
		g.pf("  <<len::little-unsigned-32, body::binary-size(len), rest2::binary>> ->\n")
		g.pf("    {text, report} = R.clamp_bytes(body, %d, report)\n", f.Type.Size)
		g.pf("    {:ok, rest2, %s, report}\n\n", place(f, "text"))
		g.pf("  _ ->\n    {:stop, value, report}\n")
		g.pf("end\n")
	case f.Type.Kind == ir.TBytes:
		g.loadArray(f, ir.TableElemKind(f), f.Type.Size, "bytes")
	case f.KeyEnum != "":
		g.loadKeyed(f, kind)
	case f.Array == ir.ArrayCounted:
		g.loadArray(f, kind, f.ArrayBound, "counted")
	case f.Array == ir.ArrayFixed:
		g.loadArray(f, kind, f.ArrayBound, "fixed")
	case kind == tkTable:
		g.pf("case rest do\n")
		g.pf("  <<len::little-unsigned-32, body::binary-size(len), rest2::binary>> ->\n")
		g.pf("    {nested, report} = %s(body, report)\n", g.call("load_body", f.Type.Name))
		g.pf("    {:ok, rest2, %s, report}\n\n", place(f, "nested"))
		g.pf("  _ ->\n    {:stop, value, report}\n")
		g.pf("end\n")
	case kind == tkUnion:
		g.loadUnion(f)
	case isEnum(f):
		g.pf("case rest do\n")
		g.pf("  <<variant::little-unsigned-16, rest2::binary>> ->\n")
		g.pf("    case %s do\n", g.enumIdent(enumOf(f), "table_value", "variant"))
		g.pf("      nil -> {:ok, rest2, %s, R.unknown(report)}\n", place(f, "0"))
		g.pf("      slot -> {:ok, rest2, %s, report}\n", place(f, "slot"))
		g.pf("    end\n\n")
		g.pf("  _ ->\n    {:stop, value, report}\n")
		g.pf("end\n")
	default:
		g.pf("case rest do\n")
		g.pf("  <<raw::%s, rest2::binary>> ->\n", wireSpec(kind))
		g.indent += "    "
		g.emitScalarDecode(f, kind, "raw", "decoded")
		g.pf("{:ok, rest2, %s, report}\n\n", place(f, "decoded"))
		g.indent = strings.TrimSuffix(g.indent, "    ")
		g.pf("  _ ->\n    {:stop, value, report}\n")
		g.pf("end\n")
	}
}

// emitScalarDecode binds `out` to the value one raw wire word decodes to,
// clamping to the field's declared range on the way and counting the clamp.
func (g *gen) emitScalarDecode(f *ir.Field, kind int, raw, out string) {
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("%s = %s != 0\n", out, raw)
		return
	case ir.TFloat32:
		g.pf("%s = R.f32_value(%s)\n", out, raw)
		return
	case ir.TFloat64:
		g.pf("%s = R.f64_value(%s)\n", out, raw)
		return
	}
	if isFlags(f) {
		g.pf("%s = %s\n", out, raw)
		return
	}
	low, high := false, false
	if f.HasIntRange {
		low, high = clampEnds(f, tableKindWidth(kind))
	}
	switch {
	case low && high:
		g.pf("{%s, report} = R.clamp(%s, %s, %s, report)\n", out, raw, intLit(f.IntMin), intLit(f.IntMax))
	case low:
		g.pf("{%s, report} = R.clamp_low(%s, %s, report)\n", out, raw, intLit(f.IntMin))
	case high:
		g.pf("{%s, report} = R.clamp_high(%s, %s, report)\n", out, raw, intLit(f.IntMax))
	default:
		g.pf("%s = %s\n", out, raw)
	}
}

// loadArray reads an array body: the element kind and count, then the bounded
// prefix. `shape` is "fixed" (every slot kept, the tail at its defaults),
// "counted" (the list IS the count) or "bytes".
func (g *gen) loadArray(f *ir.Field, elemKind int, bound int64, shape string) {
	g.pf("case rest do\n")
	g.pf("  <<len::little-unsigned-32, body::binary-size(len), rest2::binary>> ->\n")
	g.indent += "    "
	g.pf("case body do\n")
	g.pf("  <<element_kind::little-unsigned-8, count::little-unsigned-32, elements::binary>> ->\n")
	g.indent += "    "
	g.pf("if element_kind != %d do\n", elemKind)
	g.pf("  # the ELEMENT kind is part of an array's identity, not only its\n")
	g.pf("  # framing (docs/SPEC-TABLES.md §3): a mismatch skips the whole field\n")
	g.pf("  {:ok, rest2, value, R.kind_mismatch(report)}\n")
	g.pf("else\n")
	g.indent += "  "
	g.pf("{keep, report} = R.clamp_count(count, %d, report)\n\n", bound)
	if shape == "bytes" {
		g.pf("case elements do\n")
		g.pf("  <<taken::binary-size(^keep), _::binary>> ->\n")
		g.pf("    {:ok, rest2, %s, report}\n\n", place(f, "taken"))
		g.pf("  _ ->\n")
		g.pf("    # a count the body cannot cover: the decoded prefix stands and\n")
		g.pf("    # the parent reads on past the length (§4)\n")
		g.pf("    {:ok, rest2, %s, R.malformed(report)}\n", place(f, "elements"))
		g.pf("end\n")
	} else {
		g.pf("{list, report} =\n")
		g.pf("  R.take(elements, keep, report, fn element_body, report ->\n")
		g.indent += "    "
		g.loadElement(f, elemKind)
		g.indent = strings.TrimSuffix(g.indent, "    ")
		g.pf("  end)\n\n")
		if shape == "fixed" {
			g.pf("# a fixed array keeps every slot: the tail stays at its declared\n")
			g.pf("# defaults, exactly as a short wire count leaves it (§4)\n")
			g.pf("filled = list ++ Enum.drop(%s, length(list))\n", g.storageDefault(f))
			g.pf("{:ok, rest2, %s, report}\n", place(f, "filled"))
		} else {
			g.pf("{:ok, rest2, %s, report}\n", place(f, "list"))
		}
	}
	g.indent = strings.TrimSuffix(g.indent, "  ")
	g.pf("end\n")
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("\n  _ ->\n")
	g.pf("    # a body too short to carry an element kind and a count at all\n")
	g.pf("    {:ok, rest2, value, report}\n")
	g.pf("end\n")
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("\n  _ ->\n    {:stop, value, report}\n")
	g.pf("end\n")
}

// loadElement decodes ONE array element out of `element_body`, returning
// {:ok, value, rest, report} | :short — the shape R.take drives.
func (g *gen) loadElement(f *ir.Field, elemKind int) {
	switch {
	case elemKind == tkTable:
		g.pf("case element_body do\n")
		g.pf("  <<len::little-unsigned-32, body::binary-size(len), tail::binary>> ->\n")
		g.pf("    {element, report} = %s(body, report)\n", g.call("load_body", f.Type.Name))
		g.pf("    {:ok, element, tail, report}\n\n")
		g.pf("  _ ->\n    :short\n")
		g.pf("end\n")
	case isEnum(f):
		g.pf("case element_body do\n")
		g.pf("  <<variant::little-unsigned-16, tail::binary>> ->\n")
		g.pf("    case %s do\n", g.enumIdent(enumOf(f), "table_value", "variant"))
		g.pf("      nil -> {:ok, 0, tail, R.unknown(report)}\n")
		g.pf("      slot -> {:ok, slot, tail, report}\n")
		g.pf("    end\n\n")
		g.pf("  _ ->\n    :short\n")
		g.pf("end\n")
	default:
		g.pf("case element_body do\n")
		g.pf("  <<raw::%s, tail::binary>> ->\n", wireSpec(elemKind))
		g.indent += "    "
		g.emitScalarDecode(f, elemKind, "raw", "decoded")
		g.pf("{:ok, decoded, tail, report}\n\n")
		g.indent = strings.TrimSuffix(g.indent, "    ")
		g.pf("  _ ->\n    :short\n")
		g.pf("end\n")
	}
}

func (g *gen) loadUnion(f *ir.Field) {
	un := unionOf(f)
	g.pf("case rest do\n")
	g.pf("  <<0::little-unsigned-16, rest2::binary>> ->\n")
	g.pf("    # empty: the arm id is the whole payload\n")
	g.pf("    {:ok, rest2, %s, report}\n\n", place(f, "%"+g.mod(f.Type.Name)+"{}"))
	g.pf("  <<arm::little-unsigned-16, len::little-unsigned-32, body::binary-size(len), rest2::binary>> ->\n")
	g.indent += "    "
	g.pf("case arm do\n")
	for i, v := range un.Variants {
		g.pf("  0x%04X ->\n", ir.VariantId(v.Name))
		g.pf("    {payload, report} = %s(body, report)\n", g.call("load_body", v.Type))
		g.pf("    {:ok, rest2, %s, report}\n\n",
			place(f, fmt.Sprintf("%%%s{type: %d, %s: payload}", g.mod(f.Type.Name), i+1, v.Name)))
	}
	g.pf("  _ ->\n")
	g.pf("    # an arm this reader cannot name: the value reads EMPTY and the body\n")
	g.pf("    # is skipped by its length, never misdecoded. Re-establishing is\n")
	g.pf("    # explicit: a repeated field id must not leave an arm decoded by an\n")
	g.pf("    # earlier occurrence standing (§4).\n")
	g.pf("    {:ok, rest2, %s, R.unknown(report)}\n", place(f, "%"+g.mod(f.Type.Name)+"{}"))
	g.pf("end\n")
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("\n  _ ->\n    {:stop, value, report}\n")
	g.pf("end\n")
}

func (g *gen) loadKeyed(f *ir.Field, kind int) {
	g.pf("case rest do\n")
	g.pf("  <<len::little-unsigned-32, body::binary-size(len), rest2::binary>> ->\n")
	g.indent += "    "
	g.pf("case body do\n")
	g.pf("  <<element_kind::little-unsigned-8, count::little-unsigned-32, pairs::binary>> ->\n")
	g.indent += "    "
	g.pf("if element_kind != %d do\n", ir.TableScalarKind(f))
	g.pf("  {:ok, rest2, value, R.kind_mismatch(report)}\n")
	g.pf("else\n")
	g.indent += "  "
	g.pf("{slots, report} =\n")
	g.pf("  R.keyed(pairs, count, %s, report, fn key, element, slots, report ->\n", g.keyedStart(f))
	g.indent += "    "
	g.pf("case %s do\n", g.enumIdent(f.KeyEnumRef, "table_value", "key"))
	g.pf("  nil ->\n")
	g.pf("    # a slot this reader cannot name\n")
	g.pf("    {slots, R.unknown(report)}\n\n")
	g.pf("  variant ->\n")
	g.indent += "    "
	g.pf("index = variant - 1\n\n")
	switch {
	case kind == tkTable:
		g.pf("{decoded, report} = %s(element, report)\n", g.call("load_body", f.Type.Name))
		g.pf("{R.put_slot(slots, index, decoded), report}\n")
	case isEnum(f):
		g.pf("case element do\n")
		g.pf("  <<variant_id::little-unsigned-16, _::binary>> ->\n")
		g.pf("    case %s do\n", g.enumIdent(enumOf(f), "table_value", "variant_id"))
		g.pf("      nil -> {R.put_slot(slots, index, 0), R.unknown(report)}\n")
		g.pf("      slot -> {R.put_slot(slots, index, slot), report}\n")
		g.pf("    end\n\n")
		g.pf("  _ ->\n    {slots, R.malformed(report)}\n")
		g.pf("end\n")
	default:
		g.pf("case element do\n")
		g.pf("  <<raw::%s, _::binary>> ->\n", wireSpec(kind))
		g.indent += "    "
		g.emitScalarDecode(f, kind, "raw", "decoded")
		g.pf("{R.put_slot(slots, index, decoded), report}\n\n")
		g.indent = strings.TrimSuffix(g.indent, "    ")
		g.pf("  _ ->\n    {slots, R.malformed(report)}\n")
		g.pf("end\n")
	}
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("end\n")
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("  end)\n\n")
	g.pf("{:ok, rest2, %s, report}\n", place(f, g.keyedFinish(f)))
	g.indent = strings.TrimSuffix(g.indent, "  ")
	g.pf("end\n")
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("\n  _ ->\n    {:ok, rest2, value, report}\n")
	g.pf("end\n")
	g.indent = strings.TrimSuffix(g.indent, "    ")
	g.pf("\n  _ ->\n    {:stop, value, report}\n")
	g.pf("end\n")
}

// keyedStart is the slot accumulator a keyed read builds into: a LIST of the
// declared defaults, converted back to the storage spelling at the end. A list
// is the accumulator either way, because a tuple's put_elem copies the whole
// tuple per slot and there are at most E.Max of them.
func (g *gen) keyedStart(f *ir.Field) string {
	return fmt.Sprintf("List.duplicate(%s, %d)", g.elementDefault(f), arrayLen(f))
}

func (g *gen) keyedFinish(f *ir.Field) string {
	if g.keyedIsTuple() {
		return "List.to_tuple(slots)"
	}
	return "slots"
}
