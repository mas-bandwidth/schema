// THE VARIABLE FORM for Elixir (docs/SPEC-TABLES.md §3): form byte 1, the
// id-table wire, for the variable class beyond the card record — pointers to
// tables and the flat node table (§2.1, §3.1), blobs (*bytes and *string,
// §2.5), unbounded arrays of scalar, string and enum elements and arrays of
// pointers (§2.9), the remaining scalars, and the text form (§16). A map
// (§2.8) or a union the closure holds (§2.6) does not refuse a unit that also
// carries those lists: the roots that reach one are named uncarried and
// answer an error, so the driver writes <case>.absent rather than a wrong
// byte (§11). A unit that is only that construct is still refused by name.
//
// The wire is the same bytes the Go, C, C++ and C# backends write
// (docs/SPEC-TABLES.md §3, §3.1): the form byte, then a body of `(reference,
// kind, payload)` triples ended by the zero reference, then the id table — the
// interned ids in first-use order, then their count as one little-endian u64.
// A *string rides as a pointer index (kind 17) into the flat node table, which
// the root body carries under the reserved node-table id at kind 12 (string);
// a list rides as an array (kind 14) of framed table bodies (element kind 13);
// an enum rides under its own kind 30 carrying the reference to the variant
// name's id.
//
// Reading is wire tolerance (§4): an unknown id is skipped and counted, an
// absent field reads its default, a kind mismatch and an out-of-range value are
// reported. Identity is the name hash and `was` (§5). A BEAM term has no
// pointer identity, so a *string's node index is its position in the
// declaration-order walk, which is the same order the C++ reference and the Go
// builder write a fresh blob for each field.
package elixirtable

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// VariableTableModule is the variable form's codec module, one per unit that
// declares a variable table, claimed beside BuildVersion and the two
// accelerators' runtimes (docs/SPEC-TABLES.md §3, §11).
const VariableTableModule = "Table"

// generateVariableForm emits <ns>.Table: the id-table wire's load and save for
// every table the unit's closure holds, driven by the descriptors below and
// the shared engine in variableRuntimeSource. Nil when that closure holds no
// table. A fixed-only unit still emits it (docs/SPEC-TABLES.md §2.3, §3): the
// conformance wire is form byte 1, and an optional and a by-value nest are one
// framing, so tblp1 and tblp3 answer that byte instead of writing <case>.absent.
func generateVariableForm(u *ir.Unit, ns string) map[string][]byte {
	// The descriptors cover the WHOLE table closure (docs/SPEC-TABLES.md §3.1):
	// a variable root reaches a fixed table by value and by pointer, and a
	// fixed-only unit's own form-1 body is the same codec. Optional absence is
	// the absence of the field, not a kind (§2.3), so ?T and T share it.
	closure := ir.TableClosure(u)
	names := make([]string, 0, len(closure))
	for name := range closure {
		if closureStruct(u, name) != nil {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString(header(VariableTableModule, u.Package, "the VARIABLE FORM's wire codec (docs/SPEC-TABLES.md §3)"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "defmodule %s.%s do\n", ns, VariableTableModule)
	b.WriteString("  @moduledoc \"\"\"\n")
	b.WriteString("  The VARIABLE FORM's wire (docs/SPEC-TABLES.md §3), form byte 1: the\n")
	b.WriteString("  id-table file a table round-trips byte for byte as the Go, C, C++ and C#\n")
	b.WriteString("  backends do. `save/2` writes one value; `load/2` reads one tolerantly and\n")
	b.WriteString("  reports the unknown, mismatched and clamped events (§4). Identity is the\n")
	b.WriteString("  name hash and `was` (§5).\n")
	b.WriteString("  \"\"\"\n\n")
	b.WriteString("  import Bitwise\n\n")
	fmt.Fprintf(&b, "  @node_id 0x%016X\n", ir.TableNodeWireId)
	fmt.Fprintf(&b, "  @build_version_id 0x%016X\n", ir.TableBuildVersionWireId)
	fmt.Fprintf(&b, "  @message_vocab_id 0x%016X\n", ir.TableMessageVocabularyWireId)
	fmt.Fprintf(&b, "  @string_id 0x%016X\n", ir.StringWireTypeId)
	fmt.Fprintf(&b, "  @bytes_id 0x%016X\n", ir.BytesWireTypeId)
	fmt.Fprintf(&b, "  @region_align %d\n", ir.TableRegionAlign(u))
	b.WriteString("  # every id the closure can name, ascending (docs/SPEC-TABLES.md §6.6): a\n")
	b.WriteString("  # retained id is by definition not here, and takes its entry from the\n")
	b.WriteString("  # caller's list rather than the generated table.\n")
	var known strings.Builder
	known.WriteString("[")
	for _, id := range ir.TableWireIds(u) {
		fmt.Fprintf(&known, "0x%016X, ", id)
	}
	known.WriteString("]")
	// `MapSet.new([` keeps its bracket on the call's line, the ids two columns
	// in from the call and `])` back at the call's column (mix format)
	knownHead := "  @known_ids MapSet.new("
	b.WriteString(knownHead + exParseLiteral(known.String()).render(exWidth(knownHead), exWidth("  @known_ids "), 1) + ")\n\n")
	var tables strings.Builder
	tables.WriteString("%{\n")
	anyExtent := false
	for _, name := range names {
		st := closureStruct(u, name)
		if st == nil {
			continue
		}
		anyExtent = anyExtent || cookHasExtent(u, st)
		tables.WriteString(tableDescriptor(u, st))
	}
	tables.WriteString("}")
	b.WriteString(exAttribute("tables", tables.String()))
	var unions strings.Builder
	unions.WriteString("%{\n")
	// every `t:` an arm descriptor spells, nested ones included: the arm
	// kinds the runtime's arm clauses can be handed (withArmKinds)
	armKinds := map[string]bool{}
	unionNames := make([]string, 0, len(u.Unions)+len(u.TableUnions))
	for name := range u.Unions {
		unionNames = append(unionNames, name)
	}
	for name := range u.TableUnions {
		if _, exists := u.Unions[name]; !exists {
			unionNames = append(unionNames, name)
		}
	}
	sort.Strings(unionNames)
	for _, name := range unionNames {
		un := u.TableUnions[name]
		if un == nil {
			un = u.Unions[name]
		}
		fmt.Fprintf(&unions, "    %q => %%{by_name: %%{\n", name)
		for i, arm := range un.Variants {
			d := armDescriptor(u, un.Name, arm, i)
			for _, m := range armKindPattern.FindAllStringSubmatch(d, -1) {
				armKinds[m[1]] = true
			}
			fmt.Fprintf(&unions, "      %q => %s", arm.Name, d)
		}
		unions.WriteString("    }, by_id: %{\n")
		for _, arm := range un.Variants {
			fmt.Fprintf(&unions, "      0x%016X => %q,\n", ir.TableWireId(arm.WireName()), arm.Name)
		}
		unions.WriteString("    }},\n")
	}
	unions.WriteString("}")
	// a unit with no union carries no union runtime (withoutUnions), so the
	// attribute would be set and never read
	if len(unionNames) > 0 {
		b.WriteString("  # Union arms are field lines (docs/SPEC-TABLES.md §2.6, §3).\n")
		b.WriteString(exAttribute("unions", unions.String()))
	}
	b.WriteString("  # the node table's type vocabulary (docs/SPEC-TABLES.md §3.1): the id a\n")
	b.WriteString("  # record's type reference resolves to — a blob kind or a table name — so a\n")
	b.WriteString("  # pointer names the node it points at.\n")
	var nodeTypes strings.Builder
	nodeTypes.WriteString("%{")
	fmt.Fprintf(&nodeTypes, "0x%016X => :bytes, ", ir.BytesWireTypeId)
	fmt.Fprintf(&nodeTypes, "0x%016X => :string, ", ir.StringWireTypeId)
	for _, name := range names {
		st := closureStruct(u, name)
		if st == nil {
			continue
		}
		fmt.Fprintf(&nodeTypes, "0x%016X => %q, ", ir.TableWireId(st.WireName()), name)
	}
	nodeTypes.WriteString("}")
	b.WriteString(exAttribute("node_types", nodeTypes.String()))
	b.WriteString("  # the unit registry (docs/SPEC-TABLES.md §8.3): the descriptors a tool\n")
	b.WriteString("  # walks a record by name and kind with, no per-version struct on hand.\n")
	b.WriteString("  def descriptors, do: @tables\n\n")
	uncarried := uncarriedRootNames(u)
	b.WriteString(uncarriedAttribute(uncarried))
	cookUncarried := cookUncarriedRootNames(u)
	b.WriteString(cookUncarriedAttribute(cookUncarried))
	fmt.Fprintf(&b, "  @cook_build_version 0x%016X\n", ir.BuildVersion(u))
	fmt.Fprintf(&b, "  @cook_magic 0x%016X\n\n", uint64(0x4B4F4F434D484353))
	runtime, cook, retain := variableRuntimeSource, cookWriteSource, variableRetainSource
	if len(uncarried) == 0 {
		runtime = withoutUncarriedClauses(runtime)
	}
	if len(cookUncarried) == 0 {
		cook = withoutCookUncarriedGuard(cook)
	}
	if !anyExtent {
		cook = withoutExtentWalks(cook)
	}
	if len(unionNames) > 0 {
		runtime = withArmKinds(runtime, armKinds)
	} else {
		runtime = withoutUnions(runtime)
		cook = cookWithoutUnions(cook)
		retain = withoutRetainUnions(retain)
	}
	if len(unionNames) > 0 {
		retain = withRetainArmKinds(retain, armKinds)
	}
	b.WriteString(runtime)
	// the formatter keeps a blank line between the runtime's last `end` and
	// the cook writer's leading comment
	b.WriteString("\n")
	b.WriteString(cook)
	b.WriteString(retain)
	b.WriteString("end\n")
	return map[string][]byte{VariableTableModule + ".ex": []byte(b.String())}
}

// uncarriedRootNames names every table in the closure that reaches a map, a
// union, or an enum-keyed array (docs/SPEC-TABLES.md §2.4, §2.6, §2.8, §11).
// A map's generated entry is
// uncarried on its own. The value type a map points at stays carried when no
// field of that type is itself a map or a union: Item is a map's value and a
// fixed table, and marking it uncarried would drop a root the wire still
// carries. A holder is uncarried once a field of it names an uncarried table,
// by value, by pointer or as an array element, so Army follows Squad. Names
// are sorted.
func uncarriedRootNames(u *ir.Unit) []string {
	closure := ir.TableClosure(u)
	poison := map[string]bool{}
	changed := true
	for changed {
		changed = false
		for name := range closure {
			if poison[name] {
				continue
			}
			st := u.Tables[name]
			if st == nil {
				continue
			}
			if reachesUncarried(st, poison) {
				poison[name] = true
				changed = true
			}
		}
	}
	names := make([]string, 0, len(poison))
	for name := range poison {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// reachesUncarried reports a table the variable form must not walk
// (docs/SPEC-TABLES.md §2.4, §2.6, §2.8). A union arm's payload is not a
// field of the arm's table, so Point stays carried when Hit's arm names it.
// An enum-keyed array is kind 16 (§3.2); this walker encodes an array as
// kind 14, so a root that reaches one answers an error instead of a wrong
// body (§11).
func reachesUncarried(st *ir.Struct, poison map[string]bool) bool {
	if st.IsMapEntry() {
		return true
	}
	for _, f := range st.Fields {
		if f.IsMap() || f.KeyEnum != "" {
			return true
		}
		if f.Type.Kind != ir.TNamed {
			continue
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Union:
			return true
		case *ir.Struct:
			if poison[ref.Name] {
				return true
			}
		}
	}
	return false
}

var armKindPattern = regexp.MustCompile(`\bt: :([a-z_]+)`)

// armClauses are the runtime's clauses that handle ONE arm kind, keyed by the
// kinds that reach them. A union arm's descriptor comes out of the literal
// @unions, so Elixir's type checker knows every kind an arm can have and
// reports a clause for any other kind as one that is never used. A unit is
// emitted with only the clauses its own arms can reach.
var armClauses = []struct {
	kinds []string
	text  string
}{
	{[]string{"void"}, "  defp arm_content(%{t: :void}, _v, _idx_map, ids, order), do: {<<>>, ids, order}\n"},
	{[]string{"table"}, "  defp arm_content(%{t: :table} = a, v, idx_map, ids, order) do\n"},
	{[]string{"int"}, "  defp arm_content(%{t: :int} = a, v, _idx_map, ids, order),\n"},
	{[]string{"float"}, "  defp arm_content(%{t: :float} = a, v, _idx_map, ids, order),\n"},
	{[]string{"bool"}, "  defp arm_content(%{t: :bool}, v, _idx_map, ids, order),\n"},
	{[]string{"string"}, "  defp arm_content(%{t: :string}, v, _idx_map, ids, order), do: {v, ids, order}\n"},
	{[]string{"wstring"}, "  defp arm_content(%{t: :wstring}, v, _idx_map, ids, order), do: {utf16(v), ids, order}\n"},
	{[]string{"bytes"}, "  defp arm_content(%{t: :bytes}, v, _idx_map, ids, order),\n"},
	{[]string{"enum"}, "  defp arm_content(%{t: :enum}, nil, _idx_map, ids, order), do: {leb_encode(0), ids, order}\n"},
	{[]string{"enum"}, "  defp arm_content(%{t: :enum} = a, v, _idx_map, ids, order) do\n"},
	{[]string{"pointer", "blob"}, "  defp arm_content(%{t: t}, nil, _idx_map, ids, order) when t in [:pointer, :blob],\n"},
	{[]string{"pointer", "blob"}, "  defp arm_content(%{t: t} = a, v, idx_map, ids, order) when t in [:pointer, :blob],\n"},
	{[]string{"pointer"}, "  defp arm_node_key(:pointer, a, v), do: {a.type_id, v}\n"},
	{[]string{"blob"}, "  defp arm_node_key(:blob, _a, v), do: blob_site(v)\n"},
	{[]string{"list"}, "  defp arm_content(%{t: :list} = a, v, idx_map, ids, order) do\n"},
	{[]string{"union"}, "  defp arm_content(%{t: :union} = a, v, idx_map, ids, order),\n"},
	{[]string{"void"}, "  defp read_arm_payload(%{t: :void}, name, _inner, _framed, _nodes, _ids, report),\n"},
	{[]string{"int", "float"}, "  defp retyped_arm(%{t: t} = a, name, k, inner, report) when t in [:int, :float] do\n"},
}

// armVoidLines are the void arm's branches inside case expressions, each
// whole and found exactly as many times as listed.
var armVoidLines = []struct {
	text  string
	count int
}{
	{"      %{t: :void} -> acc\n", 2},
	{"        %{t: :void} -> \"null\"\n", 1},
	{"      {:ok, %{t: :void}} ->\n        if input == nil,\n          do: {{name, nil}, report},\n          else: {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}\n\n", 1},
}

const armNodeCheck = "        if input == nil and a.t != :pointer and a.t != :blob do\n"

// withArmKinds is the runtime for a unit whose union arms take only the kinds
// in `kinds`: every armClauses clause no such kind reaches goes (from its head
// through its last line and the blank line after it), the void branches go
// when no arm is void, and the JSON reader's null check names only the node
// kinds an arm can be. Every edit must apply, so a template change that moves
// one fails the emitter's tests.
func withArmKinds(runtime string, kinds map[string]bool) string {
	reached := func(ks []string) bool {
		for _, k := range ks {
			if kinds[k] {
				return true
			}
		}
		return false
	}
	for _, c := range armClauses {
		if reached(c.kinds) {
			continue
		}
		if strings.Count(runtime, c.text) != 1 {
			panic("elixirtable: arm clause not found exactly once: " + c.text)
		}
		i := strings.Index(runtime, c.text)
		end := i + len(c.text)
		switch {
		case strings.HasSuffix(c.text, " do\n"):
			end = i + strings.Index(runtime[i:], "\n  end\n") + len("\n  end\n")
		case strings.HasSuffix(c.text, ",\n"):
			end += strings.Index(runtime[end:], "\n") + 1
		}
		if strings.HasPrefix(runtime[end:], "\n") {
			end++
		}
		runtime = runtime[:i] + runtime[end:]
	}
	if !kinds["void"] {
		for _, l := range armVoidLines {
			if strings.Count(runtime, l.text) != l.count {
				panic("elixirtable: void arm branch not found as expected: " + l.text)
			}
			runtime = strings.ReplaceAll(runtime, l.text, "")
		}
	}
	if strings.Count(runtime, armNodeCheck) != 1 {
		panic("elixirtable: arm node check not found exactly once")
	}
	var check strings.Builder
	check.WriteString("        if input == nil")
	for _, k := range []string{"pointer", "blob"} {
		if kinds[k] {
			check.WriteString(" and a.t != :" + k)
		}
	}
	check.WriteString(" do\n")
	return strings.Replace(runtime, armNodeCheck, check.String(), 1)
}

// unionFamilies are the runtime functions that exist only to read, write,
// walk or spell a union, and unionClauses the clauses of shared functions
// that hand one a union field or a union-array element. In a unit with no
// union the literal @unions is empty, so every Map.fetch! on it is one
// Elixir's type checker proves always raises; such a unit is emitted without
// them.
var unionFamilies = []string{"collect_union_nodes", "arm_header", "arm_content", "arm_node_key",
	"read_arm", "read_arm_payload", "arm_framing_damaged?", "retyped_arm", "json_arm", "json_read_arm",
	"scan_union", "scan_arm"}

var unionClauses = []string{
	"  defp collect_list_nodes(%{elem_kind: 15}",
	"  defp emit_field(%{t: :union}",
	"  defp emit_element(%{elem_kind: 15}",
	"  defp read_payload(%{t: :union}",
	"  defp read_list_elements(%{elem_kind: 15}",
	"  defp tally_field(%{t: :union",
	"  defp tally_field(%{t: :list, elem_kind: 15}",
	"  defp json_field(%{t: :union}",
	"  defp json_list_elem(%{elem_kind: 15}",
	"  defp json_read_field(%{t: :union}",
	"  defp json_read_list_elem(%{elem_kind: 15}",
	"  defp scan_header(%{t: :union}",
	"  defp scan_element(f, 15,",
}

const unionNodeBranch = "        :union ->\n          collect_union_nodes(f.elem, Map.get(value, f.name), acc)\n\n"

// withoutUnions is the runtime for a unit with no union: unionFamilies and
// unionClauses go, each with the comment directly above it, and so does the
// node walk's union branch. Every name and clause must be found, so a
// template change that renames one fails the emitter's tests.
func withoutUnions(runtime string) string {
	if strings.Count(runtime, unionNodeBranch) != 1 {
		panic("elixirtable: the node walk's union branch not found exactly once")
	}
	runtime = strings.Replace(runtime, unionNodeBranch, "", 1)
	found := map[string]bool{}
	runtime = dropTopLevel(runtime, func(head string) bool {
		for _, name := range unionFamilies {
			if strings.HasPrefix(head, "  defp "+name+"(") {
				found[name] = true
				return true
			}
		}
		for _, c := range unionClauses {
			if strings.HasPrefix(head, c) {
				found[c] = true
				return true
			}
		}
		return false
	})
	for _, k := range append(append([]string{}, unionFamilies...), unionClauses...) {
		if !found[k] {
			panic("elixirtable: union runtime piece not found: " + k)
		}
	}
	return runtime
}

// retainUnionFamilies and retainUnionClauses are unionFamilies and unionClauses
// for the retaining pair (docs/SPEC-TABLES.md §6.6): the walks that read or
// write a union arm's body, and the clauses of shared functions that hand one a
// union field or an array of unions. A unit with no union is emitted without
// them, for the same reason.
var retainUnionFamilies = []string{"read_arm_retain", "read_arm_payload_retain",
	"arm_header_retain", "arm_content_retain"}

var retainUnionClauses = []string{
	"  defp read_payload_retain(%{t: :union}",
	"  defp emit_field_retain(%{t: :union}",
	"  defp emit_element_retain(%{elem_kind: 15}",
}

// withoutRetainUnions is the retaining pair for a unit with no union.
func withoutRetainUnions(retain string) string {
	found := map[string]bool{}
	retain = dropTopLevel(retain, func(head string) bool {
		for _, name := range retainUnionFamilies {
			if strings.HasPrefix(head, "  defp "+name+"(") {
				found[name] = true
				return true
			}
		}
		for _, c := range retainUnionClauses {
			if strings.HasPrefix(head, c) {
				found[c] = true
				return true
			}
		}
		return false
	})
	for _, k := range append(append([]string{}, retainUnionFamilies...), retainUnionClauses...) {
		if !found[k] {
			panic("elixirtable: retain union piece not found: " + k)
		}
	}
	return retain
}

// withRetainArmKinds is the retaining pair for a unit whose union arms take
// only the kinds in `kinds`: Elixir's type checker reports a clause that no arm
// of the unit can reach, so the table arm's clauses go when no arm is a table,
// the pointer arm's when none is a pointer, and the general clause when every
// arm is one of those two. Every clause must be found.
func withRetainArmKinds(retain string, kinds map[string]bool) string {
	table, pointer, other := kinds["table"], kinds["pointer"], false
	for k := range kinds {
		if k != "table" && k != "pointer" {
			other = true
		}
	}
	heads := map[string]bool{
		"  defp arm_content_retain(%{t: :table}":        table,
		"  defp arm_content_retain(a,":                  other,
		"  defp read_arm_payload_retain(%{t: :table}":   table,
		"  defp read_arm_payload_retain(%{t: :pointer}": pointer,
		"  defp read_arm_payload_retain(a,":             other,
	}
	found := map[string]bool{}
	retain = dropTopLevel(retain, func(head string) bool {
		for h, keep := range heads {
			if strings.HasPrefix(head, h) {
				found[h] = true
				return !keep
			}
		}
		return false
	})
	for h := range heads {
		if !found[h] {
			panic("elixirtable: retain arm clause not found: " + h)
		}
	}
	return retain
}

// dropTopLevel removes every module-level item of an emitted Elixir source
// whose head line `drop` names. An item starts at a line indented exactly two
// spaces (anything but `end`) and runs to the next such line, so it carries its
// own body and the blank line after it; a run of `#` lines directly above a
// head belongs to that head's item and goes with it.
func dropTopLevel(src string, drop func(head string) bool) string {
	lines := strings.SplitAfter(src, "\n")
	starts := func(l string) bool {
		return strings.HasPrefix(l, "  ") && len(l) > 2 && l[2] != ' ' && l[2] != '\n' &&
			!strings.HasPrefix(l, "  end\n") && !strings.HasPrefix(l, "  end)")
	}
	var b strings.Builder
	for i := 0; i < len(lines); {
		if !starts(lines[i]) {
			b.WriteString(lines[i])
			i++
			continue
		}
		j := i
		for j < len(lines) && strings.HasPrefix(lines[j], "  #") {
			j++
		}
		if j == len(lines) || !starts(lines[j]) {
			// a comment with no head under it stays as written
			for ; i < j; i++ {
				b.WriteString(lines[i])
			}
			continue
		}
		head := lines[j]
		k := j + 1
		for k < len(lines) && !starts(lines[k]) {
			k++
		}
		if !drop(head) {
			for ; i < k; i++ {
				b.WriteString(lines[i])
			}
			continue
		}
		i = k
	}
	return b.String()
}

// uncarriedClauses are the runtime's refusals of an uncarried root, each the
// whole clause and the blank line after it. A unit whose closure carries every
// root has an empty @uncarried, so each guard reads `table in []`: never true,
// which Elixir's type checker reports as a guard that will never succeed (and
// `table` as unused). Such a unit is emitted without them.
var uncarriedClauses = []string{
	"  def load_measure(table, _data) when table in @uncarried, do: -1\n\n",
}

// withoutUncarriedClauses is the runtime source for a unit whose @uncarried is
// empty: the uncarried refusal goes. The edit must apply, so a template change
// that moves it fails the emitter's tests rather than silently shipping the
// guard.
func withoutUncarriedClauses(runtime string) string {
	return replaceOnce(runtime, uncarriedClauses[0], "")
}

// cookUncarriedGuard is the cook plan's refusal of a root the cook writer does
// not lay out. A unit whose @cook_uncarried is empty would read `table in []`,
// a guard that never succeeds, so the plan keeps only its value check.
const cookUncarriedGuard = "      if table in @cook_uncarried or not is_map(value) do\n"

func withoutCookUncarriedGuard(cook string) string {
	return replaceOnce(cook, cookUncarriedGuard, "      if not is_map(value) do\n")
}

// replaceOnce replaces the one occurrence of old, and panics when old is not
// found exactly once, so a template edit that moves it fails the emitter's
// tests rather than silently shipping the text.
func replaceOnce(src, old, new string) string {
	if strings.Count(src, old) != 1 {
		panic("elixirtable: emitted text not found exactly once: " + old)
	}
	return strings.Replace(src, old, new, 1)
}

// cookUncarriedRootNames names every table in the closure the cook writer
// does not lay out: one that reaches a map (docs/SPEC-TABLES.md §2.8, §7.2).
// `schema cook` refuses every map-bearing unit by name (§7.2), so no tool cook
// holds this writer's entry arrays to the byte; a union, an enum-keyed array
// and every other shape is written. A holder follows a poisoned table by
// value, by pointer, as an array element or as a union arm's payload.
func cookUncarriedRootNames(u *ir.Unit) []string {
	closure := ir.TableClosure(u)
	poison := map[string]bool{}
	reaches := func(f *ir.Field) bool {
		if f == nil {
			return false
		}
		if f.IsMap() {
			return true
		}
		if st, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed {
			return poison[st.Name]
		}
		return false
	}
	changed := true
	for changed {
		changed = false
		for name := range closure {
			st := u.Tables[name]
			if poison[name] || st == nil {
				continue
			}
			hit := st.IsMapEntry()
			for _, f := range st.Fields {
				if hit {
					break
				}
				hit = reaches(f)
				if un := unionRef(f); un != nil {
					for _, arm := range un.Variants {
						hit = hit || reaches(arm.F)
					}
				}
			}
			if hit {
				poison[name] = true
				changed = true
			}
		}
	}
	names := make([]string, 0, len(poison))
	for name := range poison {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// cookUncarriedAttribute is the list cook_carried?/1 reads: the roots whose
// cook the writer refuses as invalid_value and the conformance driver answers
// absent (docs/SPEC-TABLES.md §7.2, §7.6).
func cookUncarriedAttribute(names []string) string {
	var b strings.Builder
	b.WriteString("  # roots that reach a map (docs/SPEC-TABLES.md §2.8, §7.2). `schema cook`\n")
	b.WriteString("  # refuses a map-bearing unit by name, so the cook writer does not lay out\n")
	b.WriteString("  # entry arrays no tool cook holds it to; cook_carried?/1 answers false.\n")
	var list strings.Builder
	list.WriteString("[")
	for _, name := range names {
		fmt.Fprintf(&list, "%q, ", name)
	}
	list.WriteString("]")
	b.WriteString(exAttribute("cook_uncarried", list.String()) + "\n")
	b.WriteString("  def cook_carried?(table), do: table not in @cook_uncarried\n\n")
	return b.String()
}

// uncarriedAttribute is the module attribute the public clauses guard on, and
// carried?/1 the wire-fuzz roster reads (docs/SPEC-TABLES.md §11). The list is
// string literals so `table in @uncarried` is a legal guard. An empty list
// never matches, and every root of that unit stays carried.
func uncarriedAttribute(names []string) string {
	var b strings.Builder
	b.WriteString("  # roots that reach a map, a union or an enum-keyed array (docs/SPEC-TABLES.md\n")
	b.WriteString("  # §2.4, §2.6, §2.8, §11). The id-table wire and the text form carry them\n")
	b.WriteString("  # (§3, §3.2, §16) and the cook write lays them out (§7.2); the wire-fuzz\n")
	b.WriteString("  # roster does not yet, so carried?/1 answers false for them there and the\n")
	b.WriteString("  # roster sends it no mutant.\n")
	var list strings.Builder
	list.WriteString("[")
	for _, name := range names {
		fmt.Fprintf(&list, "%q, ", name)
	}
	list.WriteString("]")
	b.WriteString(exAttribute("uncarried", list.String()) + "\n")
	b.WriteString("  def carried?(table), do: table not in @uncarried\n\n")
	return b.String()
}

// variableRuntimeSource is the id-table wire engine, driven by @tables. It is
// the whole codec: save walks the value in declaration order interning ids in
// first-use order, and load walks the body tolerantly reporting every event
// §4 names.
const variableRuntimeSource = `  # ---- the write side (docs/SPEC-TABLES.md §3) ----

  def save(table, value) when is_map(value) do
    desc = Map.fetch!(@tables, table)

    # the node table's records in §3.1's first-visit order, one record per
    # DISTINCT node: a table node two fields share is numbered once, so the
    # saved wire matches the reference backends byte for byte (§3.1), and a
    # blob is a node of its own SLOT (§2.5), so two slots holding equal bytes
    # are two records. A blob the tolerant read found named from two slots
    # refuses the save by name, blob_shared (§2.5).
    try do
      nodes = collect_root(desc, value)
      idx_map = node_index(nodes)

      blob_site_begin(:root)
      {_idx_map, ids, order, body} = save_fields(desc, value, idx_map, %{}, [], [])

      {body, _ids, order} = save_node_table(nodes, idx_map, ids, order, body)

      ids_bytes = for id <- Enum.reverse(order), into: <<>>, do: <<id::little-64>>

      {:ok, IO.iodata_to_binary([<<1>>, body, <<0>>, ids_bytes, <<length(order)::little-64>>])}
    catch
      :blob_shared -> {:error, :blob_shared}
      # every writer refuses a float that is not finite (docs/SPEC-TABLES.md §3.3)
      :non_finite_float -> {:error, :non_finite_float}
    after
      Process.delete(:elixir_blob_site)
    end
  end

  # collect_root numbers the value's nodes in §3.1's first-visit order, with
  # each blob keyed by the SLOT that holds it (blob_site/1).
  defp collect_root(desc, value) do
    blob_site_begin(:root)
    collect_nodes(desc, value, []) |> Enum.reverse()
  end

  defp node_index(nodes),
    do: nodes |> Enum.with_index(2) |> Map.new(fn {n, i} -> {node_content(n), i} end)

  # A BLOB IS NUMBERED BY ITS SLOT, never by its content (docs/SPEC-TABLES.md
  # §2.5, §3.1): a BEAM binary has no identity, so the slot is the identity —
  # the body that holds it (the root, or the table node by its content) and its
  # position among that body's blob slots in declaration order. The node walk
  # and the body writer meet a body's blob slots in the same order, so the two
  # name the same site. A {:blob_shared, bytes} slot is the tolerant read's
  # mark of a blob a second slot named, and every writer refuses it (§2.5).
  defp blob_site_begin(holder), do: Process.put(:elixir_blob_site, {holder, 0})

  defp blob_site({:blob_shared, _bytes}), do: throw(:blob_shared)

  defp blob_site(_bytes) do
    {holder, n} = Process.get(:elixir_blob_site)
    Process.put(:elixir_blob_site, {holder, n + 1})
    {:blob, holder, n}
  end

  # a table node's body is its own holder: its blob slots count from zero
  defp in_node_holder(node, fun) do
    prev = Process.put(:elixir_blob_site, {node_content(node), 0})

    try do
      fun.()
    after
      Process.put(:elixir_blob_site, prev)
    end
  end

  # collect_nodes is the declaration-order DFS walk of every node this value
  # reaches — a *string/*bytes blob and a table a pointer names — the flat node
  # table's records, in the order §3.1 numbers them (root is index 1, the first
  # node is 2). A node reached twice is one record. A node is {type_id, kind,
  # table_name, content}: kind :blob for a byte buffer, :table for a pointer
  # pointee.
  defp collect_nodes(desc, value, acc) do
    Enum.reduce(desc.fields, acc, fn f, acc ->
      case f.t do
        :blob ->
          case Map.get(value, f.name) do
            nil -> acc
            v -> [{f.type_id, :blob, blob_site(v), v} | acc]
          end

        :pointer ->
          case Map.get(value, f.name) do
            nil ->
              acc

            v ->
              push_node(acc, {f.type_id, :table, f.elem, v})
          end

        :table ->
          # an absent optional (nil) owns no edge (§2.3), and a by-value
          # table left nil holds its defaults, which name no node
          case Map.get(value, f.name) do
            nil -> acc
            v -> collect_nodes(Map.fetch!(@tables, f.elem), v, acc)
          end

        :list ->
          collect_list_nodes(f, Map.get(value, f.name) || [], acc)

        :map ->
          Enum.reduce(map_entries(f, Map.get(value, f.name, %{})), acc, fn entry, acc ->
            collect_nodes(Map.fetch!(@tables, f.elem), entry, acc)
          end)

        :union ->
          collect_union_nodes(f.elem, Map.get(value, f.name), acc)

        :keyed ->
          slots = Map.get(value, f.name) || %{}

          Enum.reduce(f.key_names, acc, fn name, acc ->
            case Map.get(slots, name) do
              nil -> acc
              v -> collect_nodes(%{fields: [f.item]}, %{f.item.name => v}, acc)
            end
          end)

        _ ->
          acc
      end
    end)
  end

  # a union's selected arm is a field line (docs/SPEC-TABLES.md §2.6, §3.1):
  # its nodes are visited where the union sits, exactly as a field's are.
  defp collect_union_nodes(_union, nil, acc), do: acc

  defp collect_union_nodes(union, {arm, payload}, acc) do
    case Map.fetch!(Map.fetch!(@unions, union).by_name, arm) do
      %{t: :void} -> acc
      a -> collect_nodes(%{fields: [a]}, %{a.name => payload}, acc)
    end
  end

  # push_node numbers a TABLE node a pointer names, once, and walks its body
  # the first time only: a node reached again is the one record (§3.1).
  defp push_node(acc, {_type_id, :table, elem, v} = node) do
    c = node_content(node)

    if Enum.any?(acc, fn n -> node_content(n) == c end) do
      acc
    else
      in_node_holder(node, fn -> collect_nodes(Map.fetch!(@tables, elem), v, [node | acc]) end)
    end
  end

  # a table node's identity is its type and content (docs/SPEC-TABLES.md
  # §3.1); a blob's is its slot (blob_site/1), so equal bytes in two slots are
  # two nodes, and an empty *bytes and an empty *string are two nodes.
  defp node_content({_type_id, :blob, site, _bytes}), do: site
  defp node_content({type_id, _kind, _ref, c}), do: {type_id, c}

  # SPEC-TABLES §2.8: textual string keys compare bytewise, integer keys by
  # numeric value. The entry table holds an ordinary key and value field.
  defp map_entries(_f, nil), do: []

  defp map_entries(f, values) do
    values
    |> Enum.sort_by(fn {key, _value} -> map_sort_key(f, key) end)
    |> Enum.map(fn {key, value} -> %{"key" => map_key_value(f, key), "value" => value} end)
  end

  defp map_sort_key(%{key_kind: 12}, key), do: key

  defp map_sort_key(_f, key) when is_binary(key) do
    case Integer.parse(key) do
      {v, ""} -> v
      _ -> key
    end
  end

  defp map_sort_key(_f, key), do: key
  defp map_key_value(%{key_kind: 12}, key), do: key
  defp map_key_value(_f, key) when is_binary(key), do: String.to_integer(key)
  defp map_key_value(_f, key), do: key

  # an unbounded array's elements are inline (kind 13) when they are tables and
  # nodes when they are pointers; scalar and string elements reach no node.
  defp collect_list_nodes(%{elem_kind: 13} = f, elements, acc) do
    Enum.reduce(elements, acc, fn e, acc ->
      collect_nodes(Map.fetch!(@tables, f.elem), e, acc)
    end)
  end

  defp collect_list_nodes(%{elem_kind: 17} = f, elements, acc) do
    Enum.reduce(elements, acc, fn
      nil, acc ->
        acc

      e, acc ->
        push_node(acc, {f.elem_type_id, :table, f.elem, e})
    end)
  end

  defp collect_list_nodes(%{elem_kind: 15} = f, elements, acc) do
    Enum.reduce(elements, acc, fn e, acc -> collect_union_nodes(f.elem, e, acc) end)
  end

  defp collect_list_nodes(_f, _elements, acc), do: acc

  # save_fields writes one body's fields and returns the iodata, the node
  # index map, the id map and the reversed first-use order.
  defp save_fields(desc, value, idx_map, ids, order, acc) do
    Enum.reduce(desc.fields, {idx_map, ids, order, acc}, fn f, {idx_map, ids, order, acc} ->
      emit_field(f, value, idx_map, ids, order, acc)
    end)
  end

  defp emit_field(%{t: :blob} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name) do
      nil ->
        {idx_map, ids, order, acc}

      v ->
        {ref, ids, order} = intern(ids, order, f.id)
        idx = Map.fetch!(idx_map, blob_site(v))
        {idx_map, ids, order, [acc, leb_encode(ref), <<f.kind>>, leb_encode(idx)]}
    end
  end

  defp emit_field(%{t: :list} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name, Map.get(f, :default)) do
      nil ->
        {idx_map, ids, order, acc}

      elements ->
        elements = pad_fixed(f, elements)

        if list_elides?(f, elements) do
          {idx_map, ids, order, acc}
        else
          {ref, ids, order} = intern(ids, order, f.id)
          {body, idx_map, ids, order} = save_list_elements(f, elements, idx_map, ids, order, [])
          array = [<<f.elem_kind>>, leb_encode(length(elements)), body]

          {idx_map, ids, order,
           [acc, leb_encode(ref), <<14>>, leb_encode(IO.iodata_length(array)), array]}
        end
    end
  end

  # a union field (docs/SPEC-TABLES.md §2.6, §3): None elides; a selected arm
  # is kind 15 and one arm header.
  defp emit_field(%{t: :union} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name) do
      nil ->
        {idx_map, ids, order, acc}

      sel ->
        {ref, ids, order} = intern(ids, order, f.id)
        {header, ids, order} = arm_header(f.elem, sel, idx_map, ids, order)
        {idx_map, ids, order, [acc, leb_encode(ref), <<15>>, header]}
    end
  end

  # an enum-keyed array (docs/SPEC-TABLES.md §2.4, §3.2): kind 16, the element
  # kind, the pair count, then per slot off its default the key's variant id
  # reference, L and the element. A slot at its default rolls its key back; no
  # pair at all rolls the field back.
  defp emit_field(%{t: :keyed} = f, value, idx_map, ids, order, acc) do
    slots = Map.get(value, f.name) || %{}
    {ref, ids1, order1} = intern(ids, order, f.id)
    item = Map.put(f.item, :elem_kind, f.elem_kind)

    {pairs, n, ids2, order2} =
      Enum.reduce(f.key_names, {[], 0, ids1, order1}, fn name, {pairs, n, ids, order} ->
        v = Map.get(slots, name, f.item.default)
        {kref, ids_k, order_k} = intern(ids, order, Map.fetch!(f.key_ids, name))

        case keyed_elem(item, v, idx_map, ids_k, order_k) do
          :elide ->
            {pairs, n, ids, order}

          {elem, ids_e, order_e} ->
            {[pairs, leb_encode(kref), leb_encode(IO.iodata_length(elem)), elem], n + 1, ids_e,
             order_e}
        end
      end)

    if n == 0 do
      {idx_map, ids, order, acc}
    else
      inner = [<<f.elem_kind>>, leb_encode(n), pairs]

      {idx_map, ids2, order2,
       [acc, leb_encode(ref), <<16>>, leb_encode(IO.iodata_length(inner)), inner]}
    end
  end

  # wstring(N) on the variable wire (docs/SPEC-TABLES.md §3): kind 33, L the
  # UTF-16LE byte count, the code units; an empty one elides.
  defp emit_field(%{t: :wstring} = f, value, idx_map, ids, order, acc) do
    v = Map.get(value, f.name, f.default)

    if v == nil or (v == "" and not Map.get(f, :optional, false)) do
      {idx_map, ids, order, acc}
    else
      {ref, ids, order} = intern(ids, order, f.id)
      units = utf16(v)
      {idx_map, ids, order, [acc, leb_encode(ref), <<33>>, leb_encode(byte_size(units)), units]}
    end
  end

  defp emit_field(%{t: :map} = f, value, idx_map, ids, order, acc) do
    # SPEC-TABLES §2.8: map storage is the same kind-14/element-13 framing
    # as a list of entry tables, with ascending key order fixed by the writer.
    entries = map_entries(f, Map.get(value, f.name, %{}))
    list = %{f | t: :list}
    emit_field(list, %{f.name => entries}, idx_map, ids, order, acc)
  end

  defp emit_field(%{t: :pointer} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name) do
      nil ->
        {idx_map, ids, order, acc}

      v ->
        {ref, ids, order} = intern(ids, order, f.id)
        idx = Map.fetch!(idx_map, {f.type_id, v})
        {idx_map, ids, order, [acc, leb_encode(ref), <<17>>, leb_encode(idx)]}
    end
  end

  defp emit_field(%{t: :table} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name) do
      nil ->
        {idx_map, ids, order, acc}

      v ->
        {ref, ids1, order1} = intern(ids, order, f.id)
        elem_desc = Map.fetch!(@tables, f.elem)
        {_idx, ids2, order2, body} = save_fields(elem_desc, v, idx_map, ids1, order1, [])

        if IO.iodata_length(body) == 0 and not Map.get(f, :optional, false) do
          # an all-default nested table rides nothing (§3): the field and its
          # speculative ids roll back, exactly as the reference's truncate does.
          # An optional stays (§2.3): presence is the field itself, so an empty
          # frame is the present default and an omitted field is absence.
          {idx_map, ids, order, acc}
        else
          framed = [body, <<0>>]

          {idx_map, ids2, order2,
           [acc, leb_encode(ref), <<13>>, leb_encode(IO.iodata_length(framed)), framed]}
        end
    end
  end

  defp emit_field(%{t: :bytes} = f, value, idx_map, ids, order, acc) do
    v = Map.get(value, f.name, f.default)

    if v == f.default do
      {idx_map, ids, order, acc}
    else
      {ref, ids, order} = intern(ids, order, f.id)
      # bytes(N) is an array of u8. The element kind is 6, not 8: 8 is u32
      # (docs/SPEC-TABLES.md §3, ir.TableKindU8).
      array = [<<6>>, leb_encode(byte_size(v)), v]

      {idx_map, ids, order,
       [acc, leb_encode(ref), <<14>>, leb_encode(IO.iodata_length(array)), array]}
    end
  end

  defp emit_field(%{t: :enum} = f, value, idx_map, ids, order, acc) do
    v = Map.get(value, f.name, f.default)

    if not emit_scalar?(f, v) do
      {idx_map, ids, order, acc}
    else
      {ref, ids, order} = intern(ids, order, f.id)

      # the nil variant is reference 0, as read_payload reads it: a tolerant
      # text read of "None" or an unknown name over a non-nil default holds nil
      {vref, ids, order} =
        case v do
          nil -> {0, ids, order}
          _ -> intern(ids, order, Map.fetch!(f.by_name, v))
        end

      {idx_map, ids, order, [acc, leb_encode(ref), <<30>>, leb_encode(vref)]}
    end
  end

  defp emit_field(%{t: :int} = f, value, idx_map, ids, order, acc) do
    v = Map.get(value, f.name, f.default)

    if not emit_scalar?(f, v) do
      {idx_map, ids, order, acc}
    else
      {ref, ids, order} = intern(ids, order, f.id)
      {idx_map, ids, order, [acc, leb_encode(ref), <<f.kind>>, int_bytes(v, f.width, f.signed)]}
    end
  end

  defp emit_field(%{t: :float} = f, value, idx_map, ids, order, acc) do
    v = Map.get(value, f.name, f.default)

    if not emit_scalar?(f, v) do
      {idx_map, ids, order, acc}
    else
      {ref, ids, order} = intern(ids, order, f.id)
      {idx_map, ids, order, [acc, leb_encode(ref), <<f.kind>>, float_bytes(v, f.width)]}
    end
  end

  defp emit_field(%{t: :string} = f, value, idx_map, ids, order, acc) do
    v = Map.get(value, f.name, f.default)

    if not emit_scalar?(f, v) do
      {idx_map, ids, order, acc}
    else
      {ref, ids, order} = intern(ids, order, f.id)
      {idx_map, ids, order, [acc, leb_encode(ref), <<f.kind>>, leb_encode(byte_size(v)), v]}
    end
  end

  defp emit_field(%{t: :bool} = f, value, idx_map, ids, order, acc) do
    v = Map.get(value, f.name, f.default)

    if not emit_scalar?(f, v) do
      {idx_map, ids, order, acc}
    else
      {ref, ids, order} = intern(ids, order, f.id)
      {idx_map, ids, order, [acc, leb_encode(ref), <<1>>, if(v, do: <<1>>, else: <<0>>)]}
    end
  end

  # SPEC-TABLES §2.3: absence is the lack of a field line. A present optional
  # must write even its zero/default value, while nil never becomes a payload.
  defp emit_scalar?(%{optional: true}, v), do: v != nil
  defp emit_scalar?(f, v), do: not same?(v, f.default)

  # ELISION COMPARES A FLOAT'S BITS (docs/SPEC-TABLES.md §3.3, the 10-15 rule):
  # -0.0 == 0.0 on the BEAM, so a -0 under a +0 default would be dropped and
  # read back as +0. Every other value compares as a value.
  # Exact equality (=:=) tells -0.0 from 0.0 since OTP 27, and a BEAM float is
  # never a NaN, so on two floats it is the bit compare, with nothing built.
  defp same?(a, b) when is_float(a) and is_float(b), do: a === b
  defp same?(a, b), do: a == b

  # SPEC-TABLES §2.3, §3: a present optional array always writes; a [N]T of
  # tables always writes; any other [N]T elides when every element is the
  # declared default, and a counted or unbounded array elides when empty.
  defp list_elides?(f, elements) do
    cond do
      Map.get(f, :optional, false) -> false
      Map.has_key?(f, :fixed_array) and f.elem_kind == 13 -> false
      Map.has_key?(f, :fixed_array) -> Enum.all?(elements, &same?(&1, f.elem_default))
      true -> elements == []
    end
  end

  # a [N]T always carries N elements (docs/SPEC-TABLES.md §3): a shorter
  # value is padded with the element default, a longer one is cut at N.
  defp pad_fixed(%{fixed_array: n} = f, elements) when is_list(elements) do
    kept = Enum.take(elements, n)
    kept ++ List.duplicate(f.elem_default, n - length(kept))
  end

  defp pad_fixed(_f, elements), do: elements

  defp utf16(v), do: :unicode.characters_to_binary(v, :utf8, {:utf16, :little})

  defp keyed_elem(%{elem_kind: 13} = item, v, idx_map, ids, order) do
    {_idx, ids2, order2, body} =
      save_fields(Map.fetch!(@tables, item.elem), v || %{}, idx_map, ids, order, [])

    if IO.iodata_length(body) == 0, do: :elide, else: {[body, <<0>>], ids2, order2}
  end

  defp keyed_elem(item, v, idx_map, ids, order) do
    if same?(v, item.default), do: :elide, else: emit_element(item, v, idx_map, ids, order)
  end

  # one arm header (docs/SPEC-TABLES.md §2.6, §3): the arm id's reference, the
  # arm's kind byte, L, and the arm's payload. None is the zero reference.
  defp arm_header(_union, nil, _idx_map, ids, order), do: {leb_encode(0), ids, order}

  defp arm_header(union, {arm, payload}, idx_map, ids, order) do
    a = Map.fetch!(Map.fetch!(@unions, union).by_name, arm)
    {aref, ids, order} = intern(ids, order, Map.get(a, :arm_id, a.id))

    {content, ids, order} =
      arm_content(a, if(payload == nil, do: a.default, else: payload), idx_map, ids, order)

    {[leb_encode(aref), <<a.arm_kind>>, leb_encode(IO.iodata_length(content)), content], ids,
     order}
  end

  # an arm's payload under its L (docs/SPEC-TABLES.md §3's arm payload table):
  # a table body with its terminator, a scalar's bytes, a string's or wide
  # string's bytes, a node index, an enum reference, an array body, or an
  # inner union's arm header. A payload-free arm is empty.
  defp arm_content(%{t: :void}, _v, _idx_map, ids, order), do: {<<>>, ids, order}

  defp arm_content(%{t: :table} = a, v, idx_map, ids, order) do
    {_idx, ids, order, body} =
      save_fields(Map.fetch!(@tables, a.elem), v || %{}, idx_map, ids, order, [])

    {[body, <<0>>], ids, order}
  end

  defp arm_content(%{t: :int} = a, v, _idx_map, ids, order),
    do: {int_bytes(v, a.width, a.signed), ids, order}

  defp arm_content(%{t: :float} = a, v, _idx_map, ids, order),
    do: {float_bytes(v, a.width), ids, order}

  defp arm_content(%{t: :bool}, v, _idx_map, ids, order),
    do: {if(v, do: <<1>>, else: <<0>>), ids, order}

  defp arm_content(%{t: :string}, v, _idx_map, ids, order), do: {v, ids, order}

  defp arm_content(%{t: :wstring}, v, _idx_map, ids, order), do: {utf16(v), ids, order}

  defp arm_content(%{t: :bytes}, v, _idx_map, ids, order),
    do: {[<<6>>, leb_encode(byte_size(v)), v], ids, order}

  defp arm_content(%{t: :enum}, nil, _idx_map, ids, order), do: {leb_encode(0), ids, order}

  defp arm_content(%{t: :enum} = a, v, _idx_map, ids, order) do
    {vref, ids, order} = intern(ids, order, Map.fetch!(a.by_name, v))
    {leb_encode(vref), ids, order}
  end

  defp arm_content(%{t: t}, nil, _idx_map, ids, order) when t in [:pointer, :blob],
    do: {leb_encode(0), ids, order}

  defp arm_content(%{t: t} = a, v, idx_map, ids, order) when t in [:pointer, :blob],
    do: {leb_encode(Map.fetch!(idx_map, arm_node_key(t, a, v))), ids, order}

  defp arm_node_key(:pointer, a, v), do: {a.type_id, v}
  defp arm_node_key(:blob, _a, v), do: blob_site(v)

  defp arm_content(%{t: :list} = a, v, idx_map, ids, order) do
    elements = pad_fixed(a, v || [])
    {body, _idx_map, ids, order} = save_list_elements(a, elements, idx_map, ids, order, [])
    {[<<a.elem_kind>>, leb_encode(length(elements)), body], ids, order}
  end

  defp arm_content(%{t: :union} = a, v, idx_map, ids, order),
    do: arm_header(a.elem, v, idx_map, ids, order)

  defp save_list_elements(_f, [], idx_map, ids, order, acc), do: {acc, idx_map, ids, order}

  defp save_list_elements(f, [e | rest], idx_map, ids, order, acc) do
    {elem, ids, order} = emit_element(f, e, idx_map, ids, order)
    save_list_elements(f, rest, idx_map, ids, order, [acc, elem])
  end

  # emit_element writes one array element by the list's element kind
  # (docs/SPEC-TABLES.md §3, §2.9): a table element is a framed body, a pointer
  # element a node index, a string element its L and bytes, an enum a variant
  # reference, and every scalar element its own bytes.
  defp emit_element(%{elem_kind: 13} = f, e, idx_map, ids, order) do
    elem_desc = Map.fetch!(@tables, f.elem)
    {_idx, ids, order, body} = save_fields(elem_desc, e, idx_map, ids, order, [])
    {[leb_encode(IO.iodata_length(body) + 1), body, <<0>>], ids, order}
  end

  # a null pointer element is node index 0 (docs/SPEC-TABLES.md §3.1). It
  # names no record, so it is not a key in the node map.
  defp emit_element(%{elem_kind: 17} = _f, nil, _idx_map, ids, order) do
    {leb_encode(0), ids, order}
  end

  defp emit_element(%{elem_kind: 17} = f, e, idx_map, ids, order) do
    idx = Map.fetch!(idx_map, {f.elem_type_id, e})
    {leb_encode(idx), ids, order}
  end

  defp emit_element(%{elem_kind: 12}, e, _idx_map, ids, order) do
    {[leb_encode(byte_size(e)), e], ids, order}
  end

  defp emit_element(%{elem_kind: 30}, nil, _idx_map, ids, order), do: {leb_encode(0), ids, order}

  defp emit_element(%{elem_kind: 30} = f, e, _idx_map, ids, order) do
    vid = Map.fetch!(f.by_name, e)
    {vref, ids, order} = intern(ids, order, vid)
    {leb_encode(vref), ids, order}
  end

  defp emit_element(%{elem_kind: 1} = _f, e, _idx_map, ids, order) do
    {if(e, do: <<1>>, else: <<0>>), ids, order}
  end

  defp emit_element(%{elem_kind: kind}, e, _idx_map, ids, order) when kind in [10, 11] do
    {float_bytes(e, kind_width(kind)), ids, order}
  end

  defp emit_element(%{elem_kind: kind}, e, _idx_map, ids, order)
       when kind in [2, 3, 4, 5, 6, 7, 8, 9, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29] do
    {int_bytes(e, kind_width(kind), kind in [2, 3, 4, 5, 18, 20, 21, 22, 23, 24]), ids, order}
  end

  defp emit_element(%{elem_kind: 15} = f, e, idx_map, ids, order),
    do: arm_header(f.elem, e, idx_map, ids, order)

  defp emit_element(_f, _e, _idx_map, ids, order), do: {<<>>, ids, order}

  # save_node_table writes the reserved node-table field once, when the value
  # carries a node (docs/SPEC-TABLES.md §3.1). Each record is the node's type id
  # reference, its body's L and the body — a blob's raw bytes or a table's field
  # walk ending in its terminator. Type ids and a table node's field ids intern
  # in the record's own order, which is the reference's first-use order.
  defp save_node_table([], _idx_map, ids, order, body), do: {body, ids, order}

  defp save_node_table(nodes, idx_map, ids, order, body) do
    {ref, ids, order} = intern(ids, order, @node_id)
    {records, ids, order} = write_node_records(nodes, idx_map, ids, order, [])
    payload = [leb_encode(length(nodes)), records]
    {[body, leb_encode(ref), <<12>>, leb_encode(IO.iodata_length(payload)), payload], ids, order}
  end

  defp write_node_records([], _idx_map, ids, order, acc), do: {acc, ids, order}

  defp write_node_records([node | rest], idx_map, ids, order, acc) do
    {tref, ids, order} = intern(ids, order, elem(node, 0))
    {nbody, ids, order} = node_body(node, idx_map, ids, order)
    acc = [acc, leb_encode(tref), leb_encode(IO.iodata_length(nbody)), nbody]
    write_node_records(rest, idx_map, ids, order, acc)
  end

  defp node_body({_type_id, :blob, _ref, bytes}, _idx_map, ids, order),
    do: {bytes, ids, order}

  defp node_body({_type_id, :table, ref, v} = node, idx_map, ids, order) do
    desc = Map.fetch!(@tables, ref)

    {_idx, ids, order, body} =
      in_node_holder(node, fn -> save_fields(desc, v, idx_map, ids, order, []) end)

    {IO.iodata_to_binary([body, <<0>>]), ids, order}
  end

  # ---- the read side (docs/SPEC-TABLES.md §3, §4) ----

  def load(table, data) when is_binary(data) do
    desc = Map.fetch!(@tables, table)

    case open(data) do
      {:error, reason} ->
        {:error, reason, reasoned(empty_report(), reason)}

      {:ok, body, ids} ->
        case entries(body, ids, desc.string_blob, desc.variable) do
          {:error, _} ->
            {:error, :malformed, reasoned(empty_report(), :malformed)}

          {:stop, _fields, _nodes, _remaining} ->
            # a ROOT body the walk could not finish is malformed; nothing below
            # the damage is read, so there is no value to save (§3)
            {:error, :malformed, reasoned(empty_report(), :malformed)}

          {:ok, fields, nodes} ->
            {{value, report}, shared} = blob_namings(fn -> decode(desc, fields, nodes, ids) end)
            report = if :malformed in nodes, do: %{report | malformed: true}, else: report
            unknown = Enum.count(nodes, &(&1 == :unknown))
            report = %{report | unknown: report.unknown + unknown}
            report = Map.put(report, :blob_shared, shared)
            {:ok, Map.merge(defaults(desc), value), reasoned(report, :ok)}
        end
    end
  catch
    # A NaN OR AN INFINITY REFUSES THE READ (docs/SPEC-TABLES.md §3.3), the
    # tolerant one too, and outranks any damage the walk would have met after
    # it: nothing lands, and the reason names the first such field.
    {:non_finite_float, name} ->
      {:error, :non_finite_float, reasoned(empty_report(), {:non_finite_float, name})}
  end

  # THE REASON IN THE REPORT (docs/SPEC-TABLES.md §4, §4.2): the load's one
  # verdict word beside its counters, spelled with the reference leg's
  # TableRefuseReason names (internal/codegen/gotable/refusal.go) exactly as
  # its measure names a wire it cannot read (tableRegionMeasureReason in
  # internal/codegen/gotable/regions.go: unknown_form for a refused form,
  # wire_damaged for damage). A clean read says an empty reason however
  # tolerant it was: a widened, clamped, unknown or mismatched field is an
  # EVENT the counters carry and never a refusal (§4). A form byte this reader
  # does not carry says unknown_form, which is a refusal and not damage (§3).
  # Framing damage says wire_damaged with the malformed flag beside it, and so
  # does a read that finished over damage below the root (malformed set, the
  # value kept), because the flag and the reason never disagree.
  defp reasoned(report, :ok),
    do: Map.put(report, :reason, if(report.malformed, do: "wire_damaged", else: ""))

  defp reasoned(report, :newer_form), do: Map.put(report, :reason, "unknown_form")

  defp reasoned(report, {:non_finite_float, name}),
    do: Map.put(report, :reason, "non_finite_float: " <> name)

  defp reasoned(report, _damage),
    do: Map.put(%{report | malformed: true}, :reason, "wire_damaged")

  # open reads the form byte and the id trailer (docs/SPEC-TABLES.md §3).
  defp open(<<>>), do: {:error, :malformed}

  defp open(<<form, _::binary>>) when form != 1, do: {:error, :newer_form}

  defp open(data) when byte_size(data) < 9, do: {:error, :malformed}

  defp open(data) do
    size = byte_size(data)
    count = :binary.decode_unsigned(binary_part(data, size - 8, 8), :little)

    if count > div(size - 9, 8) do
      {:error, :malformed}
    else
      start = size - 8 - count * 8
      ids = binary_part(data, start, count * 8)
      body = binary_part(data, 1, start - 1)

      if duplicate_ids?(ids) or ends_early?(body, ids) do
        {:error, :malformed}
      else
        {:ok, body, ids}
      end
    end
  end

  # duplicate_ids? is the id table's own framing rule (docs/SPEC-TABLES.md §3):
  # one id in two entries is a table that cannot be read whole.
  defp duplicate_ids?(ids) when byte_size(ids) == 0, do: false

  defp duplicate_ids?(ids) do
    entries =
      for i <- 0..(div(byte_size(ids), 8) - 1) do
        :binary.decode_unsigned(binary_part(ids, i * 8, 8), :little)
      end

    length(Enum.uniq(entries)) != length(entries)
  end

  # ends_early? is the framing pre-walk: a walkable body whose terminator does
  # not consume its last byte has bytes no field claims (§3).
  defp ends_early?(bin, ids) do
    case leb(bin) do
      :error ->
        false

      {0, rest} ->
        byte_size(rest) != 0

      {ref, rest} ->
        case resolve(ids, ref) do
          :error ->
            false

          _id ->
            case rest do
              <<kind, rest2::binary>> ->
                case slice(rest2, kind) do
                  {:ok, _payload, rest3} -> ends_early?(rest3, ids)
                  :error -> false
                end

              _ ->
                false
            end
        end
    end
  end

  # entries walks the body once into {id, kind, payload} triples, splitting the
  # node table out into its records (docs/SPEC-TABLES.md §3.1). transport is
  # true for a variable root: that body carries the reserved node id as its own
  # numbering and does not count it unknown. It is false for a fixed root, which
  # cannot name the id, so the id joins the field list and counts once as
  # unknown (§4). It is :nested for every other body (a by-value table, an
  # element, an entry, a node record), where the id is malformed: a second
  # numbering cannot exist.
  defp entries(bin, ids, string_blob, transport),
    do: entries(bin, ids, string_blob, transport, [], [])

  # entries returns {:ok, fields, nodes} when the body ends at its terminator,
  # or {:stop, fields, nodes, remaining} when the walk stops, the fourth item
  # being the bytes the walk left un-consumed: 0 means the body ran off its own
  # end cleanly, which is the one stop that keeps the decoded prefix (§3).
  defp entries(bin, ids, string_blob, transport, acc, nodes) do
    case leb(bin) do
      :error ->
        {:stop, Enum.reverse(acc), nodes, byte_size(bin)}

      {0, _rest} ->
        {:ok, Enum.reverse(acc), nodes}

      {ref, rest} ->
        case resolve(ids, ref) do
          :error ->
            {:stop, Enum.reverse(acc), nodes, byte_size(rest)}

          id ->
            case rest do
              <<kind, rest2::binary>> ->
                if id == @build_version_id or id == @message_vocab_id or
                     (id == @node_id and transport == :nested) do
                  # the two reserved ids ride in the ANNOUNCEMENT and nowhere
                  # else (§3.3), and the node id is a root's alone (§3.1): one
                  # in a body that is not its transport is malformed, the walk
                  # stops there and no field after it is read
                  {:stop, Enum.reverse(acc), nodes, byte_size(rest2)}
                else
                  case slice(rest2, kind) do
                    {:ok, payload, rest3} ->
                      if id == @node_id and transport == true do
                        # the reserved node id is a variable root's own transport
                        # (§3.1): the body reads it as the node table, and a body
                        # that carries it under another kind steps over it, never
                        # counting it unknown. A fixed root has no such arm.
                        if kind == 12 do
                          case node_table(payload, ids, string_blob) do
                            {:ok, ns} ->
                              entries(rest3, ids, string_blob, transport, acc, nodes ++ ns)

                            :error ->
                              {:stop, Enum.reverse(acc), nodes, byte_size(rest2)}
                          end
                        else
                          entries(rest3, ids, string_blob, transport, acc, nodes)
                        end
                      else
                        entries(
                          rest3,
                          ids,
                          string_blob,
                          transport,
                          [{id, kind, payload} | acc],
                          nodes
                        )
                      end

                    :error ->
                      # a kind this reader cannot skip, or a length its body
                      # cannot hold: the field still counts — unknown when the id
                      # is not this body's, a kind mismatch when it is — and the
                      # walk stops (§3, §4)
                      if id == @node_id and transport == true do
                        {:stop, Enum.reverse(acc), nodes, byte_size(rest2)}
                      else
                        {:stop, Enum.reverse([{id, kind, <<>>} | acc]), nodes, byte_size(rest2)}
                      end
                  end
                end

              _ ->
                {:stop, Enum.reverse(acc), nodes, 0}
            end
        end
    end
  end

  # node_table decodes the node table's records in order (docs/SPEC-TABLES.md
  # §3.1): the framing first, then a count, then per record a reference to the
  # node's type id and its body. A record is a blob node (kind :blob, holding
  # its bytes) or a table node (kind :table, holding its body bytes, decoded on
  # demand by the pointer that names it). The whole table is read or it is
  # nothing: the scan reads records until the payload is consumed, and a count
  # that disagrees with the scan makes the table malformed, so every pointer in
  # the save reads null.
  defp node_table(bin, ids, string_blob) do
    case leb(bin) do
      {l, rest} when byte_size(rest) >= l ->
        content = binary_part(rest, 0, l)

        case leb(content) do
          {n, cr} -> node_records(cr, n, [], ids, string_blob)
          :error -> :error
        end

      _ ->
        :error
    end
  end

  defp node_records(<<>>, n, acc, _ids, _string_blob) do
    if n == length(acc), do: {:ok, Enum.reverse(acc)}, else: :error
  end

  defp node_records(rest, n, acc, ids, string_blob) do
    with {sref, r1} <- leb(rest),
         true <- sref != 0 and sref <= div(byte_size(ids), 8),
         {len, r2} <- leb(r1),
         true <- byte_size(r2) >= len do
      bytes = binary_part(r2, 0, len)

      node =
        case resolve(ids, sref) do
          :error ->
            :unknown

          @string_id ->
            # a *string blob this reader can name is its UTF-8 bytes; a record
            # whose content is not text is malformed, and the root that reaches
            # no *string cannot place the record at all (§3.1)
            if string_blob do
              if utf8_valid?(bytes), do: {:blob, bytes}, else: :malformed
            else
              :unknown
            end

          tid ->
            case Map.fetch(@node_types, tid) do
              {:ok, :bytes} -> {:blob, bytes}
              {:ok, name} -> {:table, name, bytes}
              :error -> :unknown
            end
        end

      node_records(binary_part(r2, len, byte_size(r2) - len), n, [node | acc], ids, string_blob)
    else
      _ -> :error
    end
  end

  # THE TOLERANT READ OF A SHARED BLOB (docs/SPEC-TABLES.md §2.5): a blob is
  # owned by one slot, and a wire whose two slots name ONE blob record is read,
  # not refused. The first slot reads the bytes; every other slot reads
  # {:blob_shared, bytes} — the same binary, never a copy — and counts once in
  # the report's blob_shared counter, so a save, a measure and a text write of
  # that value refuse it by name rather than writing a copy per slot. A slot is
  # the body that holds it (the root, or a table node by its index) and its
  # position among that body's blob reads, so a table node a second pointer
  # decodes again names the same slots and is not a sharing.
  defp blob_namings(fun) do
    Process.put(:elixir_blob_read, %{holder: :root, n: 0, named: %{}, shared: MapSet.new()})

    try do
      result = fun.()
      {result, MapSet.size(Process.get(:elixir_blob_read).shared)}
    after
      Process.delete(:elixir_blob_read)
    end
  end

  defp in_read_holder(idx, fun) do
    case Process.get(:elixir_blob_read) do
      nil ->
        fun.()

      outer ->
        Process.put(:elixir_blob_read, %{outer | holder: {:node, idx}, n: 0})

        try do
          fun.()
        after
          inner = Process.get(:elixir_blob_read)
          Process.put(:elixir_blob_read, %{inner | holder: outer.holder, n: outer.n})
        end
    end
  end

  defp blob_named(idx, bytes) do
    case Process.get(:elixir_blob_read) do
      nil ->
        bytes

      st ->
        site = {st.holder, st.n}
        st = %{st | n: st.n + 1}

        case Map.fetch(st.named, idx) do
          :error ->
            Process.put(:elixir_blob_read, %{st | named: Map.put(st.named, idx, site)})
            bytes

          {:ok, ^site} ->
            Process.put(:elixir_blob_read, st)
            bytes

          {:ok, _first} ->
            Process.put(:elixir_blob_read, %{st | shared: MapSet.put(st.shared, site)})
            {:blob_shared, bytes}
        end
    end
  end

  # node_value resolves one node index to its value (docs/SPEC-TABLES.md §3.1):
  # index 0 is null, 1 is the root (never a pointee), and k >= 2 is record
  # k - 2. A blob node's value is its bytes; a table node's is the decoded body,
  # on demand, so a pointer inside one node can name a node the table reaches
  # later in the first-visit order. A node this reader cannot name reads null.
  defp node_value(nodes, idx, ids) do
    at = idx - 2

    if at < 0 or at >= length(nodes) do
      :out_of_range
    else
      case Enum.at(nodes, at) do
        {:blob, bytes} -> {:value, bytes}
        {:table, name, body} -> decode_node_table(nodes, name, body, ids, idx)
        _ -> :null
      end
    end
  end

  defp decode_node_table(nodes, name, body, ids, idx) do
    desc = Map.fetch!(@tables, name)

    case entries(body, ids, desc.string_blob, :nested) do
      {:ok, fields, []} ->
        {value, _report} = in_read_holder(idx, fn -> decode(desc, fields, nodes, ids) end)
        {:value, value}

      _ ->
        :null
    end
  end

  # decode folds the field triples into a value, tolerantly (docs/SPEC-TABLES.md §4).
  # A reference that resolves ABOVE the entry count is framing damage on the
  # body carrying it (§3) and STOPS the walk: the fields decoded before it
  # stand, one malformed counts, and no field after it is read. §4's ordinary
  # events — an unknown id, a kind mismatch, an out-of-range value, a reference
  # a reader cannot name — read on, and the halt is the one exception.
  defp decode(desc, fields, nodes, ids) do
    Enum.reduce_while(fields, {Map.new(), empty_report()}, fn {id, kind, payload},
                                                              {value, report} ->
      case find_field(desc, id) do
        nil ->
          {:cont, {value, %{report | unknown: report.unknown + 1}}}

        f ->
          cond do
            kind == f.kind ->
              case read_payload(f, payload, nodes, ids, report) do
                {:cont, v, report} -> {:cont, {Map.put(value, f.name, v), report}}
                {:halt, v, report} -> {:halt, {Map.put(value, f.name, v), report}}
              end

            widens?(kind, f) ->
              # WIDENED (§4): a kind that grew since the writer decodes exactly
              # at its own width, the value lands, and one widened event counts
              {v, report} = read_widened(f, kind, payload, report)
              {:cont, {Map.put(value, f.name, v), report}}

            true ->
              {:cont, {value, %{report | kind_mismatch: report.kind_mismatch + 1}}}
          end
      end
    end)
  end

  defp find_field(desc, id) do
    Enum.find(desc.fields, fn f -> f.id == id end)
  end

  defp read_payload(%{t: :string} = f, payload, _nodes, _ids, report) do
    case leb(payload) do
      {len, rest} when byte_size(rest) >= len ->
        raw = binary_part(rest, 0, len)

        cond do
          not utf8_valid?(raw) ->
            {:cont, "", %{report | malformed: true}}

          len > f.size ->
            {:cont, binary_part(raw, 0, f.size), %{report | clamped: report.clamped + 1}}

          true ->
            {:cont, raw, report}
        end

      _ ->
        {:cont, "", %{report | malformed: true}}
    end
  end

  defp read_payload(%{t: :int} = f, payload, _nodes, _ids, report) do
    v = int_of(payload, f.width, f.signed)
    {clamped, report} = clamp_int(v, f, report)

    {:cont, clamped, report}
  end

  defp read_payload(%{t: :bool}, payload, _nodes, _ids, report) do
    {:cont, binary_part(payload, 0, 1) != <<0>>, report}
  end

  defp read_payload(%{t: :enum} = f, payload, _nodes, ids, report) do
    case leb(payload) do
      {0, _} ->
        {:cont, nil, report}

      {vref, _} ->
        case resolve(ids, vref) do
          :error ->
            # a reference ABOVE the entry count is framing damage (§3), not
            # §4's unknown variant: the walk stops, one malformed counts
            {:halt, nil, %{report | malformed: true}}

          variant_id ->
            case Map.fetch(f.by_id, variant_id) do
              {:ok, name} -> {:cont, name, report}
              :error -> {:cont, nil, %{report | unknown: report.unknown + 1}}
            end
        end
    end
  end

  defp read_payload(%{t: :blob} = _f, payload, nodes, ids, report) do
    case leb(payload) do
      {0, _} ->
        {:cont, nil, report}

      {1, _} ->
        # node index 1 is the ROOT, a table, never a blob (§3.1): a *bytes or
        # *string naming it is a kind mismatch, and the pointer reads null
        {:cont, nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

      {idx, _} ->
        case node_value(nodes, idx, ids) do
          {:value, bytes} when is_binary(bytes) ->
            {:cont, blob_named(idx, bytes), report}

          {:value, _} ->
            # a table node named by a *bytes/*string pointer is a kind mismatch
            {:cont, nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

          :out_of_range ->
            # an index above node_count + 1 is framing damage; the pointer reads
            # null and the walk reads on (§3.1)
            {:cont, nil, %{report | malformed: true}}

          :null ->
            # a malformed or unknown record was counted once at the node table
            {:cont, nil, report}
        end
    end
  end

  defp read_payload(%{t: :pointer} = _f, payload, nodes, ids, report) do
    case leb(payload) do
      {0, _} ->
        {:cont, nil, report}

      {1, _} ->
        # node index 1 is the ROOT, a table body, never a pointer's pointee
        {:cont, nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

      {idx, _} ->
        case node_value(nodes, idx, ids) do
          {:value, v} when is_map(v) ->
            {:cont, v, report}

          {:value, _} ->
            # a blob node named by a *table pointer is a kind mismatch
            {:cont, nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

          :out_of_range ->
            {:cont, nil, %{report | malformed: true}}

          :null ->
            {:cont, nil, report}
        end
    end
  end

  # a nested table by value rides as a framed body (kind 13): the payload is the
  # body's L then the field walk (docs/SPEC-TABLES.md §3). A body that cannot be
  # read whole reads the table's declared defaults and one malformed counts.
  defp read_payload(%{t: :table} = f, payload, nodes, ids, report) do
    case leb(payload) do
      {_len, body} ->
        # a nested body reads as an element does (§3, §4), and its events FOLD
        # into the holder's: a fresh report here would drop every event the
        # holder counted before it
        {value, nested} = read_element(Map.fetch!(@tables, f.elem), body, nodes, ids)
        {:cont, value, merge_report(report, nested)}

      :error ->
        {:cont, %{}, %{report | malformed: true}}
    end
  end

  # bytes(N) rides as an array of u8 (kind 14, element kind 6): L then the count
  # and the bytes (docs/SPEC-TABLES.md §3). Kind 8 is u32. A count past the
  # declared size clamps.
  defp read_payload(%{t: :bytes} = f, payload, _nodes, _ids, report) do
    case leb(payload) do
      {_len, arr} ->
        case arr do
          <<6, count_rest::binary>> ->
            case leb(count_rest) do
              {n, bytes} when byte_size(bytes) >= n ->
                raw = binary_part(bytes, 0, n)
                keep = min(n, f.size)
                clamped = if n > f.size, do: 1, else: 0
                {:cont, binary_part(raw, 0, keep), %{report | clamped: report.clamped + clamped}}

              _ ->
                {:cont, <<>>, %{report | malformed: true}}
            end

          _ ->
            {:cont, <<>>, %{report | kind_mismatch: report.kind_mismatch + 1}}
        end

      :error ->
        {:cont, <<>>, %{report | malformed: true}}
    end
  end

  defp read_payload(%{t: :float} = f, payload, _nodes, _ids, report) do
    {:cont, float_of(payload, f.width, f.non_finite), report}
  end

  defp read_payload(%{t: :map} = f, payload, nodes, ids, report) do
    # SPEC-TABLES §2.8, §3: kind 14 contains framed entry tables. A map's
    # public keys use their JSON spelling; its wire keys remain typed.
    case read_payload(%{f | t: :list}, payload, nodes, ids, report) do
      {:cont, entries, report} ->
        [kf, vf | _] = Map.fetch!(@tables, f.elem).fields

        values =
          Map.new(entries, fn entry ->
            key = Map.get(entry, "key", kf.default)

            {if(is_binary(key), do: key, else: Integer.to_string(key)),
             Map.get(entry, "value", vf.default)}
          end)

        {:cont, values, report}

      other ->
        other
    end
  end

  # a union field's payload is one arm header (docs/SPEC-TABLES.md §2.6, §3).
  defp read_payload(%{t: :union} = f, payload, nodes, ids, report),
    do: read_arm(f.elem, payload, nodes, ids, report)

  # an enum-keyed array (docs/SPEC-TABLES.md §2.4, §3.2): the element kind, the
  # pair count, then (key reference, L, element) pairs. A key this reader cannot
  # name counts unknown and its element is skipped by its L.
  defp read_payload(%{t: :keyed} = f, payload, nodes, ids, report) do
    with {_len, <<ek, rest::binary>>} <- leb(payload),
         true <- ek == f.elem_kind,
         {n, pairs} <- leb(rest) do
      read_keyed_pairs(f, pairs, n, nodes, ids, %{}, report)
    else
      _ -> {:cont, %{}, %{report | kind_mismatch: report.kind_mismatch + 1}}
    end
  end

  # wstring(N) (docs/SPEC-TABLES.md §3): L UTF-16LE bytes; past N code units
  # clamps, and bytes that are not UTF-16 read empty and count malformed.
  defp read_payload(%{t: :wstring} = f, payload, _nodes, _ids, report) do
    case leb(payload) do
      {len, rest} when byte_size(rest) >= len and rem(len, 2) == 0 ->
        {raw, report} =
          if div(len, 2) > f.size do
            {binary_part(rest, 0, f.size * 2), %{report | clamped: report.clamped + 1}}
          else
            {binary_part(rest, 0, len), report}
          end

        case :unicode.characters_to_binary(raw, {:utf16, :little}, :utf8) do
          s when is_binary(s) -> {:cont, s, report}
          _ -> {:cont, "", %{report | malformed: true}}
        end

      _ ->
        {:cont, "", %{report | malformed: true}}
    end
  end

  defp read_payload(%{t: :list} = f, payload, nodes, ids, report) do
    case leb(payload) do
      {_len, arr} ->
        case arr do
          <<elem_kind, count_rest::binary>> ->
            # a narrower scalar element kind widens and counts once
            # (docs/SPEC-TABLES.md §2.9, §4). Any other disagreement skips
            # the field and counts a kind mismatch.
            cond do
              elem_kind == f.elem_kind or kind_widens?(elem_kind, f.elem_kind) ->
                case leb(count_rest) do
                  {n, elems} ->
                    report =
                      if elem_kind == f.elem_kind do
                        report
                      else
                        %{report | widened: report.widened + 1}
                      end

                    reader = %{f | elem_kind: elem_kind}
                    {n, report} = clamp_count(n, f, report)
                    {value, report} = read_list_elements(reader, elems, n, nodes, ids, [], report)
                    {:cont, value, report}

                  :error ->
                    {:cont, [], %{report | malformed: true}}
                end

              true ->
                {:cont, [], %{report | kind_mismatch: report.kind_mismatch + 1}}
            end

          _ ->
            {:cont, [], %{report | kind_mismatch: report.kind_mismatch + 1}}
        end

      :error ->
        {:cont, [], %{report | malformed: true}}
    end
  end

  # read_arm reads one arm header (docs/SPEC-TABLES.md §2.6, §3): the zero
  # reference is None; an arm id this reader cannot name counts unknown; an arm
  # header under another kind counts a kind mismatch; the arm's payload under
  # its L reads by the arm's own field line, and the value is {arm, payload}.
  defp read_arm(union, bin, nodes, ids, report) do
    case leb(bin) do
      {0, _} ->
        {:cont, nil, report}

      {aref, <<k, rest::binary>>} ->
        case resolve(ids, aref) do
          :error ->
            {:halt, nil, %{report | malformed: true}}

          arm_id ->
            un = Map.fetch!(@unions, union)

            case Map.fetch(un.by_id, arm_id) do
              :error ->
                {:cont, nil, %{report | unknown: report.unknown + 1}}

              {:ok, name} ->
                a = Map.fetch!(un.by_name, name)

                case leb(rest) do
                  {l, content} when byte_size(content) >= l and k == a.arm_kind ->
                    inner = binary_part(content, 0, l)
                    framed = binary_part(rest, 0, byte_size(rest) - byte_size(content) + l)

                    if arm_framing_damaged?(a, inner) do
                      # a fixed-width arm whose L is not its kind's width, a
                      # payload-free arm with an L, and a reference-shaped arm
                      # whose L is not its reference's own length are that
                      # arm's own framing damage: the union reads None and the
                      # parent reads on past L (§3)
                      {:cont, nil, %{report | malformed: true}}
                    else
                      read_arm_payload(a, name, inner, framed, nodes, ids, report)
                    end

                  {l, content} when byte_size(content) >= l ->
                    retyped_arm(a, name, k, binary_part(content, 0, l), report)

                  _ ->
                    {:cont, nil, %{report | malformed: true}}
                end
            end
        end

      _ ->
        {:cont, nil, %{report | malformed: true}}
    end
  end

  defp read_arm_payload(%{t: :void}, name, _inner, _framed, _nodes, _ids, report),
    do: {:cont, {name, nil}, report}

  defp read_arm_payload(a, name, inner, framed, nodes, ids, report) do
    # the length-framed kinds read their own L, so they take the framed bytes;
    # every other arm reads its payload exactly as a field does
    payload = if a.arm_kind in [12, 13, 14, 16, 33], do: framed, else: inner

    case read_payload(a, payload, nodes, ids, report) do
      {:cont, v, report} -> {:cont, {name, v}, report}
      {:halt, v, report} -> {:halt, {name, v}, report}
    end
  end

  # arm_framing_damaged? is §3's length rule for an arm whose kind byte
  # agreed: a payload-free arm carries nothing, a reference-shaped arm (a
  # pointer, an enum) is exactly its reference, and a fixed-width arm is
  # exactly its kind's width. The length-shaped kinds frame themselves.
  defp arm_framing_damaged?(a, inner) do
    cond do
      a.arm_kind == 32 -> inner != <<>>
      a.arm_kind in [17, 30] -> not match?({_, <<>>}, leb(inner))
      a.arm_kind == 1 -> byte_size(inner) != 1
      kind_width(a.arm_kind) > 0 -> byte_size(inner) != kind_width(a.arm_kind)
      true -> false
    end
  end

  # retyped_arm judges an arm that arrived under a kind the reader does not
  # declare for it by the field rules (docs/SPEC-TABLES.md §3, §4): a kind the
  # arm's own kind WIDENS decodes at the wire kind's width and counts one
  # widened, an L that is not that width is the arm's own framing damage, and
  # any other kind is a kind mismatch — the union None, the parent reading on.
  defp retyped_arm(%{t: t} = a, name, k, inner, report) when t in [:int, :float] do
    cond do
      not widens?(k, a) ->
        {:cont, nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

      byte_size(inner) != kind_width(k) ->
        {:cont, nil, %{report | malformed: true}}

      true ->
        {v, report} = read_widened(a, k, inner, report)
        {:cont, {name, v}, report}
    end
  end

  defp retyped_arm(_a, _name, _k, _inner, report),
    do: {:cont, nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

  defp read_keyed_pairs(_f, _bin, 0, _nodes, _ids, acc, report), do: {:cont, acc, report}

  defp read_keyed_pairs(f, bin, n, nodes, ids, acc, report) do
    with {kref, r1} <- leb(bin),
         {l, r2} when byte_size(r2) >= l <- leb(r1) do
      elem = binary_part(r2, 0, l)
      rest = binary_part(r2, l, byte_size(r2) - l)
      item = Map.put(f.item, :elem_kind, f.elem_kind)

      {v, report} =
        if f.elem_kind == 13 do
          {v, elem_report} = read_element(Map.fetch!(@tables, item.elem), elem, nodes, ids)
          {v, merge_report(report, elem_report)}
        else
          case read_list_elements(item, elem, 1, nodes, ids, [], report) do
            {[v], report} -> {v, report}
            {_, report} -> {item.default, report}
          end
        end

      {acc, report} =
        case resolve(ids, kref) do
          :error ->
            {acc, %{report | malformed: true}}

          kid ->
            case Map.fetch(f.key_by_id, kid) do
              {:ok, name} -> {Map.put(acc, name, v), report}
              :error -> {acc, %{report | unknown: report.unknown + 1}}
            end
        end

      read_keyed_pairs(f, rest, n - 1, nodes, ids, acc, report)
    else
      _ -> {:cont, acc, %{report | malformed: true}}
    end
  end

  # a count past a bounded array's declared bound reads the first bound
  # elements and counts one clamped, before any element is read, so the
  # elements past the bound are never asked of the body (docs/SPEC-TABLES.md §4)
  defp clamp_count(n, %{max_count: max}, report) when n > max,
    do: {max, %{report | clamped: report.clamped + 1}}

  defp clamp_count(n, _f, report), do: {n, report}

  # widens? reports whether a wire kind decodes EXACTLY into this field
  # (docs/SPEC-TABLES.md §4): a smaller integer into a wider one of the same
  # signedness, the 128-bit kinds included, and float32 into float64 — the
  # ladder kind_widens? spells, the same one an array's element kind climbs.
  defp widens?(kind, %{t: :int} = f), do: kind_widens?(kind, f.kind)
  defp widens?(kind, %{t: :float} = f), do: kind_widens?(kind, f.kind)
  defp widens?(_kind, _f), do: false

  # read_widened decodes a field whose wire kind widened (§4): the value
  # reads at the wire kind's own width and lands, one widened event counts, and an
  # out-of-range value clamps as always.
  defp read_widened(%{t: :float} = f, kind, payload, report) do
    {float_of(payload, kind_width(kind), f.non_finite), %{report | widened: report.widened + 1}}
  end

  defp read_widened(f, kind, payload, report) do
    v = int_of(payload, kind_width(kind), f.signed)
    {clamped, report} = clamp_int(v, f, report)
    {clamped, %{report | widened: report.widened + 1}}
  end

  defp kind_width(2), do: 1
  defp kind_width(3), do: 2
  defp kind_width(4), do: 4
  defp kind_width(5), do: 8
  defp kind_width(6), do: 1
  defp kind_width(7), do: 2
  defp kind_width(8), do: 4
  defp kind_width(9), do: 8
  defp kind_width(10), do: 4
  defp kind_width(11), do: 8
  defp kind_width(kind) when kind in [20, 25], do: 1
  defp kind_width(kind) when kind in [21, 26], do: 2
  defp kind_width(kind) when kind in [22, 27], do: 4
  defp kind_width(kind) when kind in [23, 28], do: 8
  defp kind_width(kind) when kind in [18, 19, 24, 29], do: 16
  defp kind_width(_), do: 0

  # kind_widens? is the scalar ladder a list's element kind may climb
  # (docs/SPEC-TABLES.md §4): a smaller signed integer into a wider one of
  # the same signedness, and float32 into float64. One widened counts for
  # the array, and the elements are then read at the wire kind's width.
  defp kind_widens?(from, to) do
    (from >= 2 and from <= 5 and ((to > from and to <= 5) or to == 18)) or
      (from >= 6 and from <= 9 and ((to > from and to <= 9) or to == 19)) or
      (from == 10 and to == 11)
  end

  # read_list_elements decodes one unbounded array's elements by the list's
  # element kind (docs/SPEC-TABLES.md §2.9, §3): a table element is a framed
  # body, a pointer element a node index, a string element its L and bytes, an
  # enum element a variant reference, and every scalar element its own bytes.
  defp read_list_elements(_f, _rest, 0, _nodes, _ids, acc, report),
    do: {Enum.reverse(acc), report}

  defp read_list_elements(%{elem_kind: 13} = f, rest, n, nodes, ids, acc, report) do
    case leb(rest) do
      {len, body} when byte_size(body) >= len ->
        sub = binary_part(body, 0, len)
        after_sub = binary_part(body, len, byte_size(body) - len)

        {value, elem_report} = read_element(Map.fetch!(@tables, f.elem), sub, nodes, ids)

        read_list_elements(
          f,
          after_sub,
          n - 1,
          nodes,
          ids,
          [value | acc],
          merge_report(report, elem_report)
        )

      _ ->
        # the element's L framing is damaged: the walk stops, one malformed
        # counts, and the elements already read stand (§4)
        {Enum.reverse(acc), %{report | malformed: true}}
    end
  end

  defp read_list_elements(%{elem_kind: 17} = f, rest, n, nodes, ids, acc, report) do
    case leb(rest) do
      {0, after_sub} ->
        read_list_elements(f, after_sub, n - 1, nodes, ids, [nil | acc], report)

      {1, after_sub} ->
        # node index 1 is the ROOT, as at a pointer field
        report = %{report | kind_mismatch: report.kind_mismatch + 1}
        read_list_elements(f, after_sub, n - 1, nodes, ids, [nil | acc], report)

      {idx, after_sub} ->
        # a SLOT resolves the way a pointer field does (§3.1): an index above
        # the node count is malformed and THAT slot reads null, the array kept
        # at its count; a blob node under a *T slot is a kind mismatch
        {value, report} =
          case node_value(nodes, idx, ids) do
            {:value, v} when is_map(v) -> {v, report}
            {:value, _} -> {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}
            :out_of_range -> {nil, %{report | malformed: true}}
            :null -> {nil, report}
          end

        read_list_elements(f, after_sub, n - 1, nodes, ids, [value | acc], report)

      :error ->
        {Enum.reverse(acc), %{report | malformed: true}}
    end
  end

  defp read_list_elements(%{elem_kind: 12} = f, rest, n, nodes, ids, acc, report) do
    case leb(rest) do
      {len, body} when byte_size(body) >= len ->
        raw = binary_part(body, 0, len)
        after_sub = binary_part(body, len, byte_size(body) - len)

        value =
          cond do
            not utf8_valid?(raw) -> ""
            len > f.size -> binary_part(raw, 0, f.size)
            true -> raw
          end

        read_list_elements(f, after_sub, n - 1, nodes, ids, [value | acc], report)

      _ ->
        {Enum.reverse(acc), %{report | malformed: true}}
    end
  end

  defp read_list_elements(%{elem_kind: 30} = f, rest, n, nodes, ids, acc, report) do
    case leb(rest) do
      {0, after_sub} ->
        read_list_elements(f, after_sub, n - 1, nodes, ids, [nil | acc], report)

      {vref, after_sub} ->
        value =
          case resolve(ids, vref) do
            :error ->
              nil

            variant_id ->
              case Map.fetch(f.by_id, variant_id) do
                {:ok, name} -> name
                :error -> :unknown
              end
          end

        # an element naming a variant this reader cannot name reads None and
        # counts unknown (docs/SPEC-TABLES.md §4)
        {value, report} =
          if value == :unknown,
            do: {nil, %{report | unknown: report.unknown + 1}},
            else: {value, report}

        read_list_elements(f, after_sub, n - 1, nodes, ids, [value | acc], report)

      :error ->
        {Enum.reverse(acc), %{report | malformed: true}}
    end
  end

  defp read_list_elements(%{elem_kind: 1} = f, rest, n, nodes, ids, acc, report) do
    case rest do
      <<b, after_sub::binary>> ->
        read_list_elements(f, after_sub, n - 1, nodes, ids, [b != 0 | acc], report)

      _ ->
        {Enum.reverse(acc), %{report | malformed: true}}
    end
  end

  defp read_list_elements(%{elem_kind: kind} = f, rest, n, nodes, ids, acc, report)
       when kind in [10, 11] do
    w = kind_width(kind)

    if byte_size(rest) >= w do
      <<raw::binary-size(^w), after_sub::binary>> = rest
      v = float_of(raw, w, f.non_finite)
      read_list_elements(f, after_sub, n - 1, nodes, ids, [v | acc], report)
    else
      {Enum.reverse(acc), %{report | malformed: true}}
    end
  end

  defp read_list_elements(%{elem_kind: kind} = f, rest, n, nodes, ids, acc, report)
       when kind in [2, 3, 4, 5, 6, 7, 8, 9, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29] do
    w = kind_width(kind)
    signed = kind in [2, 3, 4, 5, 18, 20, 21, 22, 23, 24]

    if byte_size(rest) >= w do
      <<raw::binary-size(^w), after_sub::binary>> = rest
      read_list_elements(f, after_sub, n - 1, nodes, ids, [int_of(raw, w, signed) | acc], report)
    else
      {Enum.reverse(acc), %{report | malformed: true}}
    end
  end

  defp read_list_elements(%{elem_kind: 15} = f, rest, n, nodes, ids, acc, report) do
    case slice(rest, 15) do
      {:ok, header, after_sub} ->
        {_, v, report} = read_arm(f.elem, header, nodes, ids, report)
        read_list_elements(f, after_sub, n - 1, nodes, ids, [v | acc], report)

      :error ->
        {Enum.reverse(acc), %{report | malformed: true}}
    end
  end

  defp read_list_elements(_f, _rest, _n, _nodes, _ids, acc, report),
    do: {Enum.reverse(acc), report}

  # read_element decodes one []T element (docs/SPEC-TABLES.md §2.9, §4). A body
  # that cannot be read whole — its L does not frame it, its terminator is not
  # its last byte, or its walk stops on damage — reads the element's DECLARED
  # DEFAULTS and one malformed counts, and the list reads on past it. A clean
  # body reads its value, and an event inside it folds into the list's report.
  defp read_element(desc, sub, nodes, ids) do
    case entries(sub, ids, false, :nested) do
      {:ok, fields, []} ->
        # a list element's report FOLDS into the enclosing one (§4): the
        # counters sum and malformed sticks, so an event inside an element
        # is never lost when the element's own walk returns. A body whose
        # terminator is not the last byte of its L is framing damage (§3):
        # the events its fields earned count, the element reads its
        # declared defaults, and one malformed counts.
        {value, report} = decode(desc, fields, nodes, ids)

        if ends_early?(sub, ids),
          do: {%{}, %{report | malformed: true}},
          else: {value, report}

      {:stop, fields, [], 0} ->
        # the element's body ran off its own end with no terminator: the
        # prefix decoded stands, and one malformed counts (§3)
        {value, report} = decode(desc, fields, nodes, ids)
        {value, %{report | malformed: true}}

      {:stop, fields, [], _remaining} ->
        # the element's body is damaged mid-walk: the events its prefix
        # already earned count, the element reads its declared defaults, and
        # one malformed counts (§3, §4)
        {_value, report} = decode(desc, fields, nodes, ids)
        {%{}, %{report | malformed: true}}

      _ ->
        {%{}, malformed_report()}
    end
  end

  defp malformed_report,
    do: %{
      unknown: 0,
      kind_mismatch: 0,
      widened: 0,
      clamped: 0,
      malformed: true,
      retained: 0,
      retain_lost: 0,
      reason: ""
    }

  # ---- the shared primitives ----

  defp empty_report,
    do: %{
      unknown: 0,
      kind_mismatch: 0,
      widened: 0,
      clamped: 0,
      malformed: false,
      retained: 0,
      retain_lost: 0,
      reason: ""
    }

  # defaults is a field's declared default: what an absent field reads (§4). The
  # descriptor's own default column carries it, declared default first, so an
  # int32 = 1 reads 1 and a text renders 1.
  defp defaults(desc) do
    Map.new(desc.fields, fn f ->
      {f.name, Map.get(f, :default)}
    end)
  end

  # intern assigns a reference in first-use order and returns it.
  defp intern(ids, order, id) do
    case ids do
      %{^id => ref} ->
        {ref, ids, order}

      _ ->
        ref = length(order) + 1
        {ref, Map.put(ids, id, ref), [id | order]}
    end
  end

  # canonical unsigned LEB128 (docs/SPEC-TABLES.md §3).
  defp leb(<<b, rest::binary>>) when b < 0x80, do: {b, rest}

  defp leb(bin), do: leb(bin, 0, 0)

  defp leb(<<b, rest::binary>>, i, acc) do
    cond do
      i == 9 and b > 1 ->
        :error

      (b &&& 0x80) == 0 ->
        if i > 0 and b == 0, do: :error, else: {acc ||| (b &&& 0x7F) <<< (7 * i), rest}

      true ->
        leb(rest, i + 1, acc ||| (b &&& 0x7F) <<< (7 * i))
    end
  end

  defp leb(_bin, _i, _acc), do: :error

  defp leb_encode(v) when v < 0x80, do: <<v>>

  defp leb_encode(v) do
    b = v &&& 0x7F
    rest = v >>> 7

    if rest == 0 do
      <<b>>
    else
      <<b ||| 0x80, leb_encode(rest)::binary>>
    end
  end

  defp resolve(ids, ref) do
    if ref == 0 or ref > div(byte_size(ids), 8) do
      :error
    else
      :binary.decode_unsigned(binary_part(ids, (ref - 1) * 8, 8), :little)
    end
  end

  # slice returns one field's payload bytes by its kind (§3's skip table).
  defp slice(bin, kind) when kind in [1, 2, 6, 20, 25], do: take(bin, 1)
  defp slice(bin, kind) when kind in [3, 7, 21, 26], do: take(bin, 2)
  defp slice(bin, kind) when kind in [4, 8, 10, 22, 27], do: take(bin, 4)
  defp slice(bin, kind) when kind in [5, 9, 11, 23, 28], do: take(bin, 8)
  defp slice(bin, kind) when kind in [18, 19, 24, 29], do: take(bin, 16)
  defp slice(bin, kind) when kind in [17, 30], do: leb_rest(bin)
  defp slice(bin, kind) when kind in [12, 13, 14, 16, 31, 32, 33], do: body(bin)

  defp slice(bin, 15) do
    case leb(bin) do
      :error ->
        :error

      {0, rest} ->
        {:ok, binary_part(bin, 0, byte_size(bin) - byte_size(rest)), rest}

      {_arm, <<_k, rest2::binary>>} ->
        # an arm header is a field header (§3): the arm id, the arm's kind byte,
        # then L and the payload, which body/2 frames the same way
        case body(rest2) do
          {:ok, _payload, rest3} ->
            {:ok, binary_part(bin, 0, byte_size(bin) - byte_size(rest3)), rest3}

          :error ->
            :error
        end

      _ ->
        :error
    end
  end

  defp slice(_bin, _kind), do: :error

  defp take(bin, n) when byte_size(bin) >= n,
    do: {:ok, binary_part(bin, 0, n), binary_part(bin, n, byte_size(bin) - n)}

  defp take(_bin, _n), do: :error

  defp leb_rest(bin) do
    case leb(bin) do
      {_v, rest} -> {:ok, binary_part(bin, 0, byte_size(bin) - byte_size(rest)), rest}
      :error -> :error
    end
  end

  defp body(bin) do
    case leb(bin) do
      {l, rest} when byte_size(rest) >= l ->
        {:ok, binary_part(bin, 0, byte_size(bin) - byte_size(rest) + l),
         binary_part(rest, l, byte_size(rest) - l)}

      _ ->
        :error
    end
  end

  defp int_bytes(v, 1, true), do: <<v::little-signed-8>>
  defp int_bytes(v, 2, true), do: <<v::little-signed-16>>
  defp int_bytes(v, 4, true), do: <<v::little-signed-32>>
  defp int_bytes(v, 8, true), do: <<v::little-signed-64>>
  defp int_bytes(v, 16, true), do: <<v::little-signed-128>>
  defp int_bytes(v, 1, false), do: <<v::little-unsigned-8>>
  defp int_bytes(v, 2, false), do: <<v::little-unsigned-16>>
  defp int_bytes(v, 4, false), do: <<v::little-unsigned-32>>
  defp int_bytes(v, 8, false), do: <<v::little-unsigned-64>>
  defp int_bytes(v, 16, false), do: <<v::little-unsigned-128>>

  # A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3, the same rule as the packet
  # wire's). A BEAM float holds no NaN and no
  # infinity, so the one non-finite value a writer can be handed is a float64
  # past float32's range, which would narrow to an infinity, and the write
  # refuses it. The bound is float32's largest finite value plus half its last
  # place, the least magnitude that rounds to infinity, so the test is a
  # comparison and builds nothing.
  defp float_bytes(v, 4) when v >= 3.4028235677973366e38 or v <= -3.4028235677973366e38,
    do: throw(:non_finite_float)

  defp float_bytes(v, 4), do: <<v::little-float-32>>
  defp float_bytes(v, 8), do: <<v::little-float-64>>

  # EVERY READ REFUSES A NaN OR AN INFINITY ON ITS RAW BITS (§3.3), before the
  # bits become a float and before the field is assigned: an all-ones exponent
  # throws the field's name, and the load that catches it refuses whole as
  # non_finite_float, the tolerant read included, with nothing landed. The test
  # reads the top sixteen bits, where the exponent sits at either width, so it
  # builds no bignum on the way.
  defp float_of(<<_::16, hi::little-16>>, 4, name) when (hi &&& 0x7F80) == 0x7F80,
    do: throw({:non_finite_float, name})

  defp float_of(<<_::48, hi::little-16>>, 8, name) when (hi &&& 0x7FF0) == 0x7FF0,
    do: throw({:non_finite_float, name})

  defp float_of(<<v::little-float-32>>, 4, _name), do: v
  defp float_of(<<v::little-float-64>>, 8, _name), do: v

  defp int_of(<<v::little-signed-8>>, 1, true), do: v
  defp int_of(<<v::little-signed-16>>, 2, true), do: v
  defp int_of(<<v::little-signed-32>>, 4, true), do: v
  defp int_of(<<v::little-signed-64>>, 8, true), do: v
  defp int_of(<<v::little-signed-128>>, 16, true), do: v
  defp int_of(<<v::little-unsigned-8>>, 1, false), do: v
  defp int_of(<<v::little-unsigned-16>>, 2, false), do: v
  defp int_of(<<v::little-unsigned-32>>, 4, false), do: v
  defp int_of(<<v::little-unsigned-64>>, 8, false), do: v
  defp int_of(<<v::little-unsigned-128>>, 16, false), do: v
  defp int_of(_payload, _w, _signed), do: 0

  defp clamp_int(v, %{min: lo, max: _hi}, report) when v < lo,
    do: {lo, %{report | clamped: report.clamped + 1}}

  defp clamp_int(v, %{min: _lo, max: hi}, report) when v > hi,
    do: {hi, %{report | clamped: report.clamped + 1}}

  defp clamp_int(v, _f, report), do: {v, report}

  # tableUtf8Valid (docs/SPEC-TABLES.md §4): well-formed UTF-8 without NUL.
  defp utf8_valid?(raw) do
    String.valid?(raw) and :binary.match(raw, <<0>>) == :nomatch
  end

  # ---- the text form: JSON in and out of one table (docs/SPEC-TABLES.md §16) ----
  #
  # One text for one table of the variable class (docs/SPEC-TABLES.md §16). The
  # writer emits every field in declaration order, defaults included,
  # pretty-printed two spaces per level and ending with exactly one newline
  # (§16.1); the reader places every key it can name, counts the rest and
  # clamps rather than truncating (§16.2). A node named more than once is
  # defined once under "&node" and named by "&node" alone after (§16.7).
  # A table's sharing is content equality, the same rule save uses for one
  # node record; a blob the tolerant read found named from two slots refuses
  # the text by name, blob_shared (§2.5, §16.7).

  def to_json(table, value) when is_map(value) do
    desc = Map.fetch!(@tables, table)
    value = Map.merge(defaults(desc), value)
    counts = tally_refs(desc, value, %{})

    shared_blob =
      Enum.any?(counts, fn
        {{:blob, _, _}, n} -> n > 1
        _ -> false
      end)

    if shared_blob do
      # a blob's text is a string, which has no first key for "&node" (§16.7)
      {:error, :blob_shared}
    else
      Process.put(:elixir_json_refs, counts)
      Process.put(:elixir_json_labels, %{})
      Process.put(:elixir_json_next, 0)
      Process.put(:elixir_json_refuse, false)

      try do
        text = IO.iodata_to_binary([json_object(desc, value, 0), "\n"])

        if Process.get(:elixir_json_refuse) do
          {:error, :shared}
        else
          {:ok, text}
        end
      catch
        :non_finite_float -> {:error, :non_finite_float}
      after
        Process.delete(:elixir_json_refs)
        Process.delete(:elixir_json_labels)
        Process.delete(:elixir_json_next)
        Process.delete(:elixir_json_refuse)
      end
    end
  end

  def from_json(table, text) when is_binary(text) do
    desc = Map.fetch!(@tables, table)
    Process.put(:elixir_json_defs, %{})
    Process.put(:elixir_json_open, %{})
    Process.put(:elixir_json_bad, false)
    Process.put(:elixir_json_variable, desc.variable)

    try do
      case json_value(text, 0) do
        {:ok, {:obj, pairs}, rest} ->
          if json_skip(rest) != <<>> or Process.get(:elixir_json_bad) do
            {:error, :malformed, empty_report()}
          else
            {value, report} = json_fields(desc, pairs)

            if Process.get(:elixir_json_bad) or report.malformed do
              {:error, :malformed, %{report | malformed: true}}
            else
              {:ok, Map.merge(defaults(desc), value), report}
            end
          end

        _ ->
          {:error, :malformed, empty_report()}
      end
    after
      Process.delete(:elixir_json_defs)
      Process.delete(:elixir_json_open)
      Process.delete(:elixir_json_bad)
      Process.delete(:elixir_json_variable)
    end
  end

  # tally_refs counts pointer slots and blob slots. A node named twice is one
  # label (§16.7). Descent is once per content, so a shared child is not
  # counted again through its parent.
  defp tally_refs(desc, value, acc) do
    Enum.reduce(desc.fields, acc, fn f, acc ->
      tally_field(f, Map.get(value, f.name), acc)
    end)
  end

  defp tally_field(%{t: :pointer, elem: name}, v, acc) when is_map(v),
    do: tally_node(name, v, acc)

  # a blob is its slot's own (§2.5), so equal bytes in two slots are two
  # strings; only the tolerant read's {:blob_shared, bytes} mark is a sharing
  defp tally_field(%{t: :blob, type_id: id}, {:blob_shared, _bytes}, acc),
    do: Map.put(acc, {:blob, id, :shared}, 2)

  defp tally_field(%{t: :table, elem: name}, v, acc) when is_map(v) do
    child = Map.fetch!(@tables, name)
    tally_refs(child, Map.merge(defaults(child), v), acc)
  end

  defp tally_field(%{t: :list, elem_kind: 17, elem: name}, elements, acc)
       when is_list(elements) do
    Enum.reduce(elements, acc, fn
      v, acc when is_map(v) -> tally_node(name, v, acc)
      _, acc -> acc
    end)
  end

  defp tally_field(%{t: :list, elem_kind: 13, elem: name}, elements, acc)
       when is_list(elements) do
    child = Map.fetch!(@tables, name)

    Enum.reduce(elements, acc, fn
      v, acc when is_map(v) -> tally_refs(child, Map.merge(defaults(child), v), acc)
      _, acc -> acc
    end)
  end

  defp tally_field(%{t: :map, elem: name}, values, acc) when is_map(values) do
    vf = Map.fetch!(@tables, name).fields |> Enum.at(1)
    Enum.reduce(values, acc, fn {_key, v}, acc -> tally_field(vf, v, acc) end)
  end

  defp tally_field(%{t: :union, elem: union}, {arm, payload}, acc) do
    case Map.fetch!(Map.fetch!(@unions, union).by_name, arm) do
      %{t: :void} -> acc
      a -> tally_field(a, if(payload == nil, do: a.default, else: payload), acc)
    end
  end

  defp tally_field(%{t: :list, elem_kind: 15} = f, elements, acc) when is_list(elements) do
    Enum.reduce(elements, acc, fn e, acc -> tally_field(%{t: :union, elem: f.elem}, e, acc) end)
  end

  defp tally_field(%{t: :keyed} = f, slots, acc) when is_map(slots) do
    Enum.reduce(slots, acc, fn {_name, v}, acc -> tally_field(f.item, v, acc) end)
  end

  defp tally_field(_f, _v, acc), do: acc

  defp tally_node(name, v, acc) do
    key = {:table, name, v}
    prev = Map.get(acc, key, 0)
    acc = Map.put(acc, key, prev + 1)

    if prev == 0 do
      child = Map.fetch!(@tables, name)
      tally_refs(child, Map.merge(defaults(child), v), acc)
    else
      acc
    end
  end

  # json_object writes one instance's fields in declaration order, defaults
  # included (§16.1: a text is for people and tools, so elision is not for it).
  # A value is SPARSE — only the fields the wire carried — so each level folds
  # the descriptor's defaults back in before a field is read. label 0 is a
  # plain object. A positive label is a definition: "&node" first, then the
  # fields (§16.7). The depth cap is the reader's (§16.2).
  defp json_object(desc, value, depth), do: json_object(desc, value, depth, 0)

  defp json_object(_desc, _value, depth, _label) when depth > 128 do
    Process.put(:elixir_json_refuse, true)
    "null"
  end

  defp json_object(desc, value, depth, label) do
    value = Map.merge(defaults(desc), value)

    # an absent optional writes no key: presence is the key (§2.3, §16.2)
    present =
      Enum.reject(desc.fields, fn f ->
        Map.get(f, :optional, false) and Map.get(value, f.name) == nil
      end)

    entries =
      Enum.map(present, fn f ->
        [
          "\n",
          indent(depth + 1),
          "\"",
          json_escape(f.json),
          "\": ",
          json_field(f, Map.get(value, f.name), depth + 1)
        ]
      end)

    cond do
      label != 0 and entries == [] ->
        Process.put(:elixir_json_refuse, true)
        "null"

      label != 0 ->
        head = ["{\n", indent(depth + 1), "\"&node\": ", Integer.to_string(label)]
        [head, Enum.map(entries, fn e -> [",", e] end), "\n", indent(depth), "}"]

      entries == [] ->
        "{}"

      true ->
        ["{", Enum.intersperse(entries, ","), "\n", indent(depth), "}"]
    end
  end

  # json_node writes one pointer target. A node named once is a plain object.
  # A node named again is defined at the first write, label from 1, and every
  # later write is {"&node": N} alone (§16.7).
  defp json_node(table, value, depth) do
    key = {:table, table, value}
    n = Map.get(Process.get(:elixir_json_refs, %{}), key, 1)

    if n < 2 do
      json_object(Map.fetch!(@tables, table), value, depth)
    else
      labels = Process.get(:elixir_json_labels, %{})

      case Map.get(labels, key) do
        nil ->
          next = Process.get(:elixir_json_next, 0) + 1
          Process.put(:elixir_json_next, next)
          Process.put(:elixir_json_labels, Map.put(labels, key, next))
          json_object(Map.fetch!(@tables, table), value, depth, next)

        label ->
          [
            "{\n",
            indent(depth + 1),
            "\"&node\": ",
            Integer.to_string(label),
            "\n",
            indent(depth),
            "}"
          ]
      end
    end
  end

  defp json_field(%{t: :list} = f, elements, depth) when is_list(elements) do
    case pad_fixed(f, elements) do
      [] ->
        "[]"

      elements ->
        rows =
          Enum.map(elements, fn e ->
            ["\n", indent(depth + 1), json_list_elem(f, e, depth + 1)]
          end)

        ["[", Enum.intersperse(rows, ","), "\n", indent(depth), "]"]
    end
  end

  defp json_field(%{t: :list}, _other, _depth), do: "[]"

  defp json_field(%{optional: true}, nil, _depth), do: "null"
  defp json_field(%{t: :string}, v, _depth), do: ["\"", json_escape(v), "\""]

  defp json_field(%{t: :map} = f, values, depth) do
    value_field = Map.fetch!(@tables, f.elem).fields |> Enum.at(1)
    pairs = map_entries(f, values)

    if pairs == [] do
      "{}"
    else
      [
        "{",
        Enum.intersperse(
          Enum.map(pairs, fn entry ->
            key = entry["key"]
            key_text = if is_binary(key), do: key, else: Integer.to_string(key)

            [
              "\n",
              indent(depth + 1),
              "\"",
              json_escape(key_text),
              "\": ",
              json_field(value_field, entry["value"], depth + 1)
            ]
          end),
          ","
        ),
        "\n",
        indent(depth),
        "}"
      ]
    end
  end

  defp json_field(%{t: :table} = f, v, depth),
    do: json_object(Map.fetch!(@tables, f.elem), v || %{}, depth)

  defp json_field(%{t: :pointer} = _f, nil, _depth), do: "null"
  defp json_field(%{t: :pointer} = f, v, depth), do: json_node(f.elem, v, depth)
  defp json_field(%{t: :blob}, nil, _depth), do: "null"
  defp json_field(%{t: :blob, blob: :bytes}, v, _depth), do: ["\"", Base.encode64(v), "\""]
  defp json_field(%{t: :blob}, v, _depth), do: ["\"", json_escape(v), "\""]
  defp json_field(%{t: :bytes}, v, _depth), do: ["\"", Base.encode64(v), "\""]
  defp json_field(%{t: :enum}, nil, _depth), do: "\"None\""
  defp json_field(%{t: :enum}, name, _depth), do: ["\"", json_escape(name), "\""]
  defp json_field(%{t: :union} = f, sel, depth), do: json_arm(f.elem, sel, depth)
  defp json_field(%{t: :wstring}, v, _depth), do: ["\"", json_escape(v), "\""]

  defp json_field(%{t: :keyed} = f, slots, depth) do
    slots = slots || %{}

    case f.key_names do
      [] ->
        "{}"

      names ->
        rows =
          Enum.map(names, fn name ->
            v = Map.get(slots, name, f.item.default)

            [
              "\n",
              indent(depth + 1),
              "\"",
              json_escape(name),
              "\": ",
              json_field(f.item, v, depth + 1)
            ]
          end)

        ["{", Enum.intersperse(rows, ","), "\n", indent(depth), "}"]
    end
  end

  defp json_field(%{t: :int, flags: names}, v, depth) do
    set = for {n, i} <- Enum.with_index(names), (v >>> i &&& 1) == 1, do: n

    case set do
      [] ->
        "[]"

      _ ->
        [
          "[",
          Enum.intersperse(
            Enum.map(set, fn n -> ["\n", indent(depth + 1), "\"", json_escape(n), "\""] end),
            ","
          ),
          "\n",
          indent(depth),
          "]"
        ]
    end
  end

  defp json_field(%{t: :int, frac: frac}, v, _depth), do: fixed_text(v, frac)
  defp json_field(%{t: :int}, v, _depth), do: Integer.to_string(v)
  defp json_field(%{t: :float} = f, v, _depth), do: json_float(v, f.width)
  defp json_field(%{t: :bool}, v, _depth), do: if(v, do: "true", else: "false")

  # json_list_elem writes one unbounded array's element by its element kind
  # (docs/SPEC-TABLES.md §2.9, §16.7): a table or pointer element is an object, a
  # string or enum a quoted name, and a scalar its own spelling.
  defp json_list_elem(%{elem_kind: 13} = f, e, depth),
    do: json_object(Map.fetch!(@tables, f.elem), e, depth)

  defp json_list_elem(%{elem_kind: 17} = _f, nil, _depth), do: "null"
  defp json_list_elem(%{elem_kind: 17} = f, e, depth), do: json_node(f.elem, e, depth)
  defp json_list_elem(%{elem_kind: 12}, e, _depth), do: ["\"", json_escape(e), "\""]
  defp json_list_elem(%{elem_kind: 30}, nil, _depth), do: "\"None\""
  defp json_list_elem(%{elem_kind: 30}, e, _depth), do: ["\"", json_escape(e), "\""]

  defp json_list_elem(%{elem_flags: names}, e, depth),
    do: json_field(%{t: :int, flags: names}, e, depth)

  defp json_list_elem(%{elem_kind: 1}, e, _depth), do: if(e, do: "true", else: "false")

  defp json_list_elem(%{elem_kind: kind}, e, _depth)
       when kind in [2, 3, 4, 5, 6, 7, 8, 9, 18, 19], do: Integer.to_string(e)

  defp json_list_elem(%{elem_kind: kind, elem_frac: frac}, e, _depth) when kind in 20..29,
    do: fixed_text(e, frac)

  defp json_list_elem(%{elem_kind: kind}, e, _depth) when kind in [10, 11],
    do: json_float(e, kind_width(kind))

  defp json_list_elem(%{elem_kind: 15} = f, e, depth), do: json_arm(f.elem, e, depth)
  defp json_list_elem(_f, _e, _depth), do: "null"

  # json_float spells one float as C's %.*g does, byte for byte, over the
  # float's OWN width (docs/SPEC-TABLES.md §16, "The FLOAT SPELLING", which
  # §16.7 holds for the variable class): the first precision from low to high
  # whose text reads back to the same value at that width (a float32 parsed
  # and narrowed through float32, 6 to 9 digits; a float64 parsed as is, 15 to
  # 17), and at the last precision the text as rounded, with no loop left.
  # Float.to_string is NOT this rule: it is the shortest round trip of the
  # widened double, so a float32 spelled up to seventeen digits, its exponent
  # was "e19" where %g writes "e+19", and a trailing-zero trim ate an exponent's
  # zero ("e-20" written "e-2") (I13, docs/PORTING.md).
  # THE TEXT WRITER REFUSES A FLOAT THAT IS NOT FINITE (docs/SPEC-TABLES.md
  # §3.3, §16.3): a float32 field's float64 past float32's range narrows to an
  # infinity, which has no spelling (float_bytes/2 holds the same bound).
  defp json_float(v, 4) when v >= 3.4028235677973366e38 or v <= -3.4028235677973366e38,
    do: throw(:non_finite_float)

  defp json_float(v, width) do
    if json_float_plain_integer?(v) do
      Integer.to_string(trunc(v))
    else
      {low, high} = json_float_digits(width)
      json_float_at(v, width, low, high)
    end
  end

  # A non-negative integral float below 10^6 is %g's plain integer spelling at
  # BOTH widths (docs/SPEC-TABLES.md §16, "The FLOAT SPELLING"): six significant
  # digits and a decimal exponent below the precision keep %g out of exponent
  # form, and every float32 of that magnitude is exact. The fast path spells the
  # same text the read-back loop would, without the per-element binaries the
  # audit counts (docs/PORTING.md I1, I14). The sign bit is read so -0.0 keeps
  # the slow path's "-0".
  defp json_float_plain_integer?(v) when v == trunc(v) and v >= 0.0 and v <= 999_999.0 do
    case <<v::float>> do
      <<0::1, _::63>> -> true
      _ -> false
    end
  end

  defp json_float_plain_integer?(_v), do: false

  defp json_float_digits(4), do: {6, 9}
  defp json_float_digits(_), do: {15, 17}

  defp json_float_at(v, width, digits, high) do
    text = json_format_g(v, digits)

    if digits >= high or json_float_reads_back?(text, v, width) do
      text
    else
      json_float_at(v, width, digits + 1, high)
    end
  end

  defp json_float_reads_back?(text, v, width) do
    case Float.parse(text) do
      {back, ""} -> json_float_narrow(back, width) == v
      _ -> false
    end
  rescue
    _ -> false
  end

  defp json_float_narrow(back, 4) do
    <<n::float-32>> = <<back::float-32>>
    n
  end

  defp json_float_narrow(back, _), do: back

  # json_format_g is C's %.*g for a finite double: the exponent form when the
  # decimal exponent is below -4 or at least the precision, the plain form
  # otherwise, trailing zeros and a trailing point removed, and an exponent of
  # at least two digits with its sign. The digits are the %.*e conversion's
  # (float_to_binary's :scientific, correctly rounded, ties to even), and the
  # plain form moves that one digit string's point, which is what C's %g does
  # with the exponent %e found.
  defp json_format_g(v, prec) do
    sci = :erlang.float_to_binary(v, [{:scientific, prec - 1}])
    [mant, exp_text] = String.split(sci, "e")
    exp = String.to_integer(exp_text)

    if exp < -4 or exp >= prec do
      json_trim_zeros(mant) <> "e" <> json_exp_digits(exp)
    else
      json_trim_zeros(json_plain(mant, exp))
    end
  end

  defp json_plain("-" <> mant, exp), do: "-" <> json_plain(mant, exp)

  defp json_plain(mant, exp) do
    digits = String.replace(mant, ".", "")

    if exp >= 0 do
      {whole, frac} = String.split_at(digits, exp + 1)
      if frac == "", do: whole, else: whole <> "." <> frac
    else
      "0." <> String.duplicate("0", -exp - 1) <> digits
    end
  end

  defp json_trim_zeros(s) do
    if String.contains?(s, "."),
      do: String.trim_trailing(String.trim_trailing(s, "0"), "."),
      else: s
  end

  defp json_exp_digits(exp) when exp < 0, do: "-" <> json_exp_abs(-exp)
  defp json_exp_digits(exp), do: "+" <> json_exp_abs(exp)
  defp json_exp_abs(e) when e < 10, do: "0" <> Integer.to_string(e)
  defp json_exp_abs(e), do: Integer.to_string(e)

  # a union is an object with ONE key, the arm's name, and None is {}
  # (docs/SPEC-TABLES.md §2.6, §16.2); a payload-free arm's value is null.
  defp json_arm(_union, nil, _depth), do: "{}"

  defp json_arm(union, {arm, payload}, depth) do
    a = Map.fetch!(Map.fetch!(@unions, union).by_name, arm)

    inner =
      case a do
        %{t: :void} -> "null"
        _ -> json_field(a, if(payload == nil, do: a.default, else: payload), depth + 1)
      end

    ["{\n", indent(depth + 1), "\"", json_escape(arm), "\": ", inner, "\n", indent(depth), "}"]
  end

  # Exact decimal spelling of a fixed raw integer (SPEC-TABLES §3, §4.12).
  # Elixir integers carry all 128 bits, so each base-ten digit is a remainder
  # of the dyadic fraction and no floating-point rounding enters the text.
  defp fixed_text(raw, frac) do
    scale = 1 <<< frac
    magnitude = abs(raw)
    sign = if raw < 0, do: "-", else: ""

    sign <>
      Integer.to_string(div(magnitude, scale)) <>
      "." <>
      fixed_digits(rem(magnitude, scale), scale, [])
  end

  defp fixed_digits(0, _scale, []), do: "0"
  defp fixed_digits(0, _scale, acc), do: acc |> Enum.reverse() |> IO.iodata_to_binary()

  defp fixed_digits(remain, scale, acc) do
    ten = remain * 10
    fixed_digits(rem(ten, scale), scale, [<<div(ten, scale) + ?0>> | acc])
  end

  # json_escape spells one string's bytes the way RFC 8259 requires, so a text
  # this writer emits is readable by any conforming parser (§16.3): the escapes
  # are \", \\, \b, \f, \n, \r, \t, and \u00XX for the other controls. Every
  # other byte — the card's multibyte UTF-8 included — rides raw.
  defp json_escape(bin), do: json_escape(bin, [])

  defp json_escape(<<>>, acc), do: IO.iodata_to_binary(Enum.reverse(acc))

  defp json_escape(<<c, rest::binary>>, acc) do
    esc =
      case c do
        ?" -> "\\\""
        ?\\ -> "\\\\"
        ?\b -> "\\b"
        ?\f -> "\\f"
        ?\n -> "\\n"
        ?\r -> "\\r"
        ?\t -> "\\t"
        c when c < 0x20 -> "\\u00" <> hex2(c)
        _ -> nil
      end

    if esc == nil do
      json_escape(rest, [<<c>> | acc])
    else
      json_escape(rest, [esc | acc])
    end
  end

  defp hex2(c), do: <<hexdigit(div(c, 16)), hexdigit(rem(c, 16))>>
  defp hexdigit(d) when d < 10, do: ?0 + d
  defp hexdigit(d), do: ?a + d - 10

  defp indent(depth), do: :binary.copy("  ", depth)

  # json_fields places every key it can name and counts the rest (§16.2): an
  # unknown key is skipped and counted in report.unknown, a key of the wrong
  # JSON type is skipped and counted as kind_mismatch, and the field keeps its
  # default. A value outside a bound clamps and counts, never truncates (§16.2).
  defp json_fields(desc, pairs) when is_list(pairs) do
    outer = Process.put(:elixir_json_table, desc.name)

    {value, _seen, report} =
      Enum.reduce(pairs, {%{}, MapSet.new(), empty_report()}, fn {k, raw},
                                                                 {value, seen, report} ->
        if String.starts_with?(k, "&") do
          Process.put(:elixir_json_bad, true)
          {value, seen, %{report | malformed: true}}
        else
          case Enum.find(desc.fields, fn f -> f.json == k end) do
            nil ->
              json_skip_value(raw)
              {value, seen, %{report | unknown: report.unknown + 1}}

            f ->
              report =
                if MapSet.member?(seen, f.name) do
                  Map.update(report, :duplicate, 1, &(&1 + 1))
                else
                  report
                end

              seen = MapSet.put(seen, f.name)

              if Map.get(f, :optional, false) and raw == nil do
                {Map.delete(value, f.name), seen, report}
              else
                {v, report} = json_read_field(f, raw, report)
                {Map.put(value, f.name, v), seen, report}
              end
          end
        end
      end)

    if outer == nil,
      do: Process.delete(:elixir_json_table),
      else: Process.put(:elixir_json_table, outer)

    {value, report}
  end

  # json_skip_value walks a value the reader drops (an unknown key, a kind
  # mismatch, an element past its bound), so a label defined inside it is
  # still defined and a later reference to it reads null, never malformed
  # (docs/SPEC-TABLES.md §16.2, §16.7).
  defp json_skip_value({:obj, [{"&node", label} | rest]}) when is_integer(label) and label > 0 do
    if Process.get(:elixir_json_variable, false) do
      defs = Process.get(:elixir_json_defs, %{})
      Process.put(:elixir_json_defs, Map.put_new(defs, label, :dropped))
    end

    Enum.each(rest, fn {_k, v} -> json_skip_value(v) end)
  end

  defp json_skip_value({:obj, pairs}) do
    Enum.each(pairs, fn {_k, v} -> json_skip_value(v) end)
  end

  defp json_skip_value(items) when is_list(items) do
    Enum.each(items, &json_skip_value/1)
  end

  defp json_skip_value(_other), do: :ok

  # a JSON number with a fraction or an exponent arrives as its decimal text
  # (json_number below): a fixed-point field reads that text exactly (§3,
  # §4.12), every other field reads it as the double it spells (§16.2).
  defp json_read_field(f, {:decimal, _} = raw, report) when not is_map_key(f, :frac),
    do: json_read_field(f, json_decimal_float(raw), report)

  defp json_read_field(%{optional: true}, nil, report), do: {nil, report}

  defp json_read_field(%{t: :string} = f, raw, report) when is_binary(raw) do
    report = json_text_zero(f, raw, report)

    if byte_size(raw) > f.size do
      {binary_part(raw, 0, f.size), %{report | clamped: report.clamped + 1}}
    else
      {raw, report}
    end
  end

  defp json_read_field(%{t: :string}, raw, report) do
    json_skip_value(raw)
    {"", %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  defp json_read_field(%{t: :enum} = f, raw, report) when is_binary(raw) do
    # "None" is the reserved nil spelling (docs/SPEC-TABLES.md §16; the Go
    # reference internal/tabletext/value.go EnumValue): it reads the nil variant
    # exactly as value 0 does on the wire, and is never an unknown name.
    case raw do
      "None" ->
        {nil, report}

      _ ->
        case Map.fetch(f.by_name, raw) do
          {:ok, _id} -> {raw, report}
          :error -> {nil, %{report | unknown: report.unknown + 1}}
        end
    end
  end

  # a union field reads one arm object (docs/SPEC-TABLES.md §2.6, §16.2)
  defp json_read_field(%{t: :union} = f, raw, report), do: json_read_arm(f.elem, raw, report)

  # wstring(N) (docs/SPEC-TABLES.md §3, §16.2): the text is UTF-8; past N
  # UTF-16 code units it clamps and counts.
  defp json_read_field(%{t: :wstring} = f, raw, report) when is_binary(raw) do
    report = json_text_zero(f, raw, report)
    units = utf16(raw)

    if div(byte_size(units), 2) > f.size do
      cut = binary_part(units, 0, f.size * 2)

      {:unicode.characters_to_binary(cut, {:utf16, :little}, :utf8),
       %{report | clamped: report.clamped + 1}}
    else
      {raw, report}
    end
  end

  defp json_read_field(%{t: :wstring} = f, raw, report) do
    json_skip_value(raw)
    {Map.get(f, :default, ""), %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  # an enum-keyed array (docs/SPEC-TABLES.md §2.4, §3.2, §16.2) is an object
  # keyed by variant name; a name the key enum does not have counts unknown.
  defp json_read_field(%{t: :keyed} = f, {:obj, pairs}, report) do
    Enum.reduce(pairs, {%{}, report}, fn {name, input}, {acc, report} ->
      if Map.has_key?(f.key_ids, name) do
        # a repeated slot name is counted and the last occurrence wins (§16.2)
        report =
          if Map.has_key?(acc, name),
            do: Map.update(report, :duplicate, 1, &(&1 + 1)),
            else: report

        {v, report} = json_read_field(f.item, input, report)
        {Map.put(acc, name, v), report}
      else
        json_skip_value(input)
        {acc, %{report | unknown: report.unknown + 1}}
      end
    end)
  end

  defp json_read_field(%{t: :keyed}, raw, report) do
    json_skip_value(raw)
    {%{}, %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  # a flags mask (docs/SPEC-TABLES.md §3, §16.2) is the list of its set names;
  # a name the flags do not have counts unknown.
  defp json_read_field(%{t: :int, flags: names}, raw, report) when is_list(raw) do
    Enum.reduce(raw, {0, report}, fn n, {mask, report} ->
      case Enum.find_index(names, &(&1 == n)) do
        nil -> {mask, %{report | unknown: report.unknown + 1}}
        i -> {mask ||| 1 <<< i, report}
      end
    end)
  end

  defp json_read_field(%{t: :int, flags: _} = f, raw, report) do
    json_skip_value(raw)
    {Map.get(f, :default, 0), %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  # a fixed-point field (docs/SPEC-TABLES.md §3, §4.12) stores the exact
  # scaled integer: the decimal text converts without passing through a
  # float, and a fraction finer than 2^-F is a kind mismatch.
  defp json_read_field(%{t: :int, frac: frac} = f, raw, report) do
    case fixed_raw(raw, frac) do
      {:ok, v} ->
        clamp_json_int(v, f, report)

      :error ->
        json_skip_value(raw)
        {Map.get(f, :default, 0), %{report | kind_mismatch: report.kind_mismatch + 1}}
    end
  end

  defp json_read_field(%{t: :enum}, raw, report) do
    json_skip_value(raw)
    {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  defp json_read_field(%{t: :int} = f, raw, report) when is_integer(raw) do
    clamp_json_int(raw, f, report)
  end

  defp json_read_field(%{t: :int} = f, raw, report) when is_float(raw) do
    if Float.floor(raw) == raw do
      clamp_json_int(trunc(raw), f, report)
    else
      {Map.get(f, :default, 0), %{report | kind_mismatch: report.kind_mismatch + 1}}
    end
  end

  defp json_read_field(%{t: :int} = f, raw, report) do
    json_skip_value(raw)
    {Map.get(f, :default, 0), %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  defp json_read_field(%{t: :bool}, raw, report) when is_boolean(raw), do: {raw, report}

  defp json_read_field(%{t: :bool} = f, raw, report) do
    json_skip_value(raw)
    {Map.get(f, :default, false), %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  defp json_read_field(%{t: :blob, blob: :bytes}, raw, report) when is_binary(raw) do
    case json_decode_base64(raw) do
      {:ok, bytes} -> {bytes, report}
      :error -> {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}
    end
  end

  defp json_read_field(%{t: :blob} = f, raw, report) when is_binary(raw),
    do: {raw, json_text_zero(f, raw, report)}

  defp json_read_field(%{t: :blob}, nil, report), do: {nil, report}

  defp json_read_field(%{t: :blob}, raw, report) do
    json_skip_value(raw)
    {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  defp json_read_field(%{t: :bytes} = f, raw, report) when is_binary(raw) do
    case json_decode_base64(raw) do
      {:ok, bytes} when byte_size(bytes) <= f.size ->
        {bytes, report}

      {:ok, bytes} ->
        {binary_part(bytes, 0, f.size), %{report | clamped: report.clamped + 1}}

      :error ->
        {Map.get(f, :default, <<>>), %{report | kind_mismatch: report.kind_mismatch + 1}}
    end
  end

  defp json_read_field(%{t: :bytes} = f, raw, report) do
    json_skip_value(raw)
    {Map.get(f, :default, <<>>), %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  defp json_read_field(%{t: :table} = f, {:obj, pairs}, report) do
    {v, elem_report} = json_fields(Map.fetch!(@tables, f.elem), pairs)
    {v, merge_report(report, elem_report)}
  end

  # a map (docs/SPEC-TABLES.md §2.8, §16.2) is an object keyed by the key's
  # text; entries read in the map's own ascending key order, the order the
  # writer emits, so a "&node" defined under one key and named under a later
  # one resolves the way the document spells it.
  defp json_read_field(%{t: :map} = f, {:obj, pairs}, report) do
    value_field = Map.fetch!(@tables, f.elem).fields |> Enum.at(1)

    pairs
    |> Enum.sort_by(fn {key, _} -> map_sort_key(f, key) end)
    |> Enum.reduce({%{}, report}, fn {key, input}, {acc, report} ->
      # a map key is a JSON string whatever its kind, bound for text storage
      report = json_text_zero(f, key, report)
      # a repeated key is counted and the last occurrence wins (§2.8)
      report =
        if Map.has_key?(acc, key),
          do: Map.update(report, :duplicate, 1, &(&1 + 1)),
          else: report

      {value, report} = json_read_field(value_field, input, report)
      {Map.put(acc, key, value), report}
    end)
  end

  defp json_read_field(%{t: :map}, raw, report) do
    json_skip_value(raw)
    {%{}, %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  defp json_read_field(%{t: :table}, raw, report) do
    json_skip_value(raw)
    {%{}, %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  defp json_read_field(%{t: :pointer} = _f, nil, report), do: {nil, report}

  defp json_read_field(%{t: :pointer} = f, {:obj, pairs}, report) do
    json_read_node(f.elem, pairs, report)
  end

  defp json_read_field(%{t: :pointer}, raw, report) do
    json_skip_value(raw)
    {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  defp json_read_field(%{t: :float} = f, raw, report) when is_number(raw) do
    case json_float_in_width(raw, f.width) do
      :error ->
        {Map.get(f, :default, 0.0), %{report | kind_mismatch: report.kind_mismatch + 1}}

      {:ok, v} ->
        case f do
          %{min: lo, max: _hi} when v < lo ->
            {lo * 1.0, %{report | clamped: report.clamped + 1}}

          %{min: _lo, max: hi} when v > hi ->
            {hi * 1.0, %{report | clamped: report.clamped + 1}}

          _ ->
            {v, report}
        end
    end
  end

  defp json_read_field(%{t: :float} = f, raw, report) do
    json_skip_value(raw)
    {Map.get(f, :default, 0.0), %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  defp json_read_field(%{t: :list} = f, raw, report) when is_list(raw) do
    bound = Map.get(f, :array_bound, 0)

    {elems, _placed, report} =
      Enum.reduce(raw, {[], 0, report}, fn e, {acc, placed, report} ->
        if bound > 0 and placed >= bound do
          json_skip_value(e)
          {acc, placed, %{report | clamped: report.clamped + 1}}
        else
          case json_read_list_elem(f, e, report) do
            {:skip, report} ->
              json_skip_value(e)
              {acc, placed + 1, report}

            {v, report} ->
              {[v | acc], placed + 1, report}
          end
        end
      end)

    {Enum.reverse(elems), report}
  end

  defp json_read_field(%{t: :list}, raw, report) do
    json_skip_value(raw)
    {[], %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  # json_text_zero is the one rule for a zero in text (docs/SPEC-TABLES.md
  # §16.2, "A ZERO IN TEXT TAKES ONE RULE IN EVERY FORM: NO READ STORES IT"):
  # a JSON string the read would PLACE in text storage that decodes to U+0000
  # refuses the read, malformed, and the reason names the innermost field that
  # held it as text_holds_zero: <Table>.<field>.
  defp json_text_zero(f, text, report) do
    if :binary.match(text, <<0>>) == :nomatch do
      report
    else
      Process.put(:elixir_json_bad, true)
      reason = "text_holds_zero: " <> Process.get(:elixir_json_table, "") <> "." <> f.name

      %{report | malformed: true}
      |> Map.update(:reason, reason, fn
        "" -> reason
        held -> held
      end)
    end
  end

  # clamp_json_int clamps a JSON integer to the field's declared range and
  # then to its storage domain, counting each clamp (docs/SPEC-TABLES.md §16.2).
  #
  # The order is the reference's (gotable tableJSONReadScalar): a token past
  # sixty-four bits saturates first and counts, then the declared range, then
  # the storage width, each clamp counted. A fixed-point or 128-bit field has
  # one domain, which comes first, then its range.
  defp clamp_json_int(v, f, report) do
    if Map.has_key?(f, :frac) or Map.get(f, :width) == 16 do
      {v, report} = clamp_between(v, int_domain_bounds(f), report)
      clamp_int(v, f, report)
    else
      signed = Map.get(f, :signed, false)

      sat =
        if signed,
          do: {-(1 <<< 63), (1 <<< 63) - 1},
          else: {0, (1 <<< 64) - 1}

      {v, report} = clamp_between(v, sat, report)
      {v, report} = clamp_int(v, f, report)
      clamp_between(v, int_domain_bounds(f), report)
    end
  end

  defp clamp_between(v, {lo, hi}, report) do
    cond do
      v < lo -> {lo, %{report | clamped: report.clamped + 1}}
      v > hi -> {hi, %{report | clamped: report.clamped + 1}}
      true -> {v, report}
    end
  end

  # int_domain_bounds is an integer field's storage domain (§16.2).
  defp int_domain_bounds(%{bits: bits}) when is_integer(bits) and bits > 0 do
    {0, (1 <<< bits) - 1}
  end

  defp int_domain_bounds(%{width: w, signed: true}) do
    bits = w * 8
    {-(1 <<< (bits - 1)), (1 <<< (bits - 1)) - 1}
  end

  defp int_domain_bounds(%{width: w, signed: false}) do
    bits = w * 8
    {0, (1 <<< bits) - 1}
  end

  # json_decode_base64 reads §16.2's bytes spelling: standard base64, padded
  # or unpadded (docs/SPEC-TABLES.md §16.2).
  defp json_decode_base64(""), do: {:ok, <<>>}

  defp json_decode_base64(raw) do
    if String.contains?(raw, "=") do
      Base.decode64(raw)
    else
      Base.decode64(raw, padding: false)
    end
  end

  # json_clamp_elem holds one integer element to its kind's storage domain and
  # then to the declared range, each clamp counted (docs/SPEC-TABLES.md §16.2),
  # the order a scalar field takes.
  defp json_clamp_elem(v, %{elem_kind: kind} = f, report) do
    bits = kind_width(kind) * 8

    {lo, hi} =
      if kind in 2..5 or kind == 18 or kind in 20..24,
        do: {-(1 <<< (bits - 1)), (1 <<< (bits - 1)) - 1},
        else: {0, (1 <<< bits) - 1}

    {v, report} =
      cond do
        v < lo -> {lo, %{report | clamped: report.clamped + 1}}
        v > hi -> {hi, %{report | clamped: report.clamped + 1}}
        true -> {v, report}
      end

    case f do
      %{elem_min: rlo} when v < rlo -> {rlo, %{report | clamped: report.clamped + 1}}
      %{elem_max: rhi} when v > rhi -> {rhi, %{report | clamped: report.clamped + 1}}
      _ -> {v, report}
    end
  end

  # json_float_in_width is the double a number spells, held to the field's
  # width: a number past the double's range, or one a float32 rounds to
  # infinity (the midpoint above its largest value included), spells no value
  # the field can store and reads as the wrong shape (docs/SPEC-TABLES.md
  # §16.2); no infinity is ever stored.
  # An integer token compares exactly: 2^128 - 2^103 is the midpoint and one
  # below it still rounds to the largest float32, which is what it stores, since
  # its nearest double is the midpoint itself and that would narrow to infinity.
  defp json_float_in_width(raw, 4) when is_integer(raw) do
    cond do
      abs(raw) >= 340_282_356_779_733_661_637_539_395_458_142_568_448 -> :error
      raw * 1.0 >= 3.4028235677973366e38 -> {:ok, 3.4028234663852886e38}
      raw * 1.0 <= -3.4028235677973366e38 -> {:ok, -3.4028234663852886e38}
      true -> {:ok, raw * 1.0}
    end
  end

  defp json_float_in_width(raw, 4) do
    if abs(raw) >= 3.4028235677973366e38, do: :error, else: {:ok, raw * 1.0}
  end

  defp json_float_in_width(raw, _width) do
    {:ok, raw * 1.0}
  rescue
    ArithmeticError -> :error
  end

  # json_read_list_elem reads one unbounded array's element by its element kind
  # (docs/SPEC-TABLES.md §2.9, §16.7), folding the element's report into the list's.
  # an integer element reads -0 as the 0 it is (json_number keeps the token)
  defp json_read_list_elem(%{elem_kind: kind}, {:decimal, "-0"}, report)
       when kind in [2, 3, 4, 5, 6, 7, 8, 9, 18, 19],
       do: {0, report}

  defp json_read_list_elem(f, {:decimal, _} = e, report) when not is_map_key(f, :elem_frac),
    do: json_read_list_elem(f, json_decimal_float(e), report)

  defp json_read_list_elem(%{elem_kind: 13} = f, {:obj, pairs}, report) do
    {v, elem_report} = json_fields(Map.fetch!(@tables, f.elem), pairs)
    {v, merge_report(report, elem_report)}
  end

  defp json_read_list_elem(%{elem_kind: 17} = _f, nil, report), do: {nil, report}

  defp json_read_list_elem(%{elem_kind: 17} = f, {:obj, pairs}, report) do
    json_read_node(f.elem, pairs, report)
  end

  defp json_read_list_elem(%{elem_kind: 12} = f, e, report) when is_binary(e) do
    report = json_text_zero(f, e, report)

    if byte_size(e) > f.size do
      {binary_part(e, 0, f.size), %{report | clamped: report.clamped + 1}}
    else
      {e, report}
    end
  end

  defp json_read_list_elem(%{elem_kind: 30} = f, e, report) when is_binary(e) do
    case e do
      "None" ->
        {nil, report}

      _ ->
        case Map.fetch(f.by_name, e) do
          {:ok, _id} -> {e, report}
          :error -> {nil, %{report | unknown: report.unknown + 1}}
        end
    end
  end

  defp json_read_list_elem(%{elem_flags: names}, e, report) when is_list(e),
    do: json_read_field(%{t: :int, flags: names}, e, report)

  defp json_read_list_elem(%{elem_kind: 1}, e, report) when is_boolean(e), do: {e, report}

  defp json_read_list_elem(%{elem_kind: kind} = f, e, report)
       when kind in [2, 3, 4, 5, 6, 7, 8, 9, 18, 19] and is_integer(e),
       do: json_clamp_elem(e, f, report)

  defp json_read_list_elem(%{elem_kind: kind} = f, e, report)
       when kind in [2, 3, 4, 5, 6, 7, 8, 9, 18, 19] and is_float(e) do
    if Float.floor(e) == e,
      do: json_clamp_elem(trunc(e), f, report),
      else: {:skip, %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  defp json_read_list_elem(%{elem_kind: kind, elem_frac: frac} = f, e, report)
       when kind in 20..29 do
    case fixed_raw(e, frac) do
      {:ok, raw} -> json_clamp_elem(raw, f, report)
      :error -> {:skip, %{report | kind_mismatch: report.kind_mismatch + 1}}
    end
  end

  defp json_read_list_elem(%{elem_kind: kind}, e, report)
       when kind in [10, 11] and is_number(e) do
    case json_float_in_width(e, kind_width(kind)) do
      {:ok, v} -> {v, report}
      :error -> {:skip, %{report | kind_mismatch: report.kind_mismatch + 1}}
    end
  end

  defp json_read_list_elem(%{elem_kind: 15} = f, e, report), do: json_read_arm(f.elem, e, report)

  defp json_read_list_elem(_f, _e, report),
    do: {:skip, %{report | kind_mismatch: report.kind_mismatch + 1}}

  # json_read_arm reads one union object (docs/SPEC-TABLES.md §2.6, §16.2): {}
  # is None; one key names the arm and its value reads by the arm's field
  # line; an arm this reader cannot name counts unknown and reads None; two
  # keys select nothing and the text is refused; any other shape is a kind
  # mismatch.
  defp json_read_arm(_union, {:obj, []}, report), do: {nil, report}

  defp json_read_arm(union, {:obj, [{name, input}]}, report) do
    un = Map.fetch!(@unions, union)

    case Map.fetch(un.by_name, name) do
      :error ->
        json_skip_value(input)
        {nil, %{report | unknown: report.unknown + 1}}

      {:ok, %{t: :void}} ->
        if input == nil,
          do: {{name, nil}, report},
          else: {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

      {:ok, a} ->
        if input == nil and a.t != :pointer and a.t != :blob do
          {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}
        else
          before = report.kind_mismatch
          {v, report} = json_read_field(a, input, report)

          if report.kind_mismatch > before and a.t not in [:table, :list, :union, :keyed],
            do: {nil, report},
            else: {{name, v}, report}
        end
    end
  end

  defp json_read_arm(_union, {:obj, _pairs}, report) do
    Process.put(:elixir_json_bad, true)
    {nil, %{report | malformed: true}}
  end

  defp json_read_arm(_union, raw, report) do
    json_skip_value(raw)
    {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  # json_read_node reads one pointer object (§16.7). "&node" alone, after the
  # label is defined, is that node. "&node" plus fields defines it. A label
  # with no definition, a second definition, or a reference while the
  # definition is still open is malformed. A label of another table is a kind
  # mismatch and the pointer reads null.
  defp json_read_node(table, [{"&node", label}], report) when is_integer(label) and label > 0 do
    if Map.has_key?(Process.get(:elixir_json_open, %{}), label) do
      Process.put(:elixir_json_bad, true)
      {nil, %{report | malformed: true}}
    else
      case Process.get(:elixir_json_defs, %{}) do
        %{^label => :dropped} ->
          {nil, report}

        %{^label => {^table, value}} ->
          {value, report}

        %{^label => _} ->
          {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

        _ ->
          Process.put(:elixir_json_bad, true)
          {nil, %{report | malformed: true}}
      end
    end
  end

  defp json_read_node(table, [{"&node", label} | rest_pairs], report)
       when is_integer(label) and label > 0 do
    defs = Process.get(:elixir_json_defs, %{})
    open = Process.get(:elixir_json_open, %{})

    if Map.has_key?(defs, label) or Map.has_key?(open, label) do
      Process.put(:elixir_json_bad, true)
      {nil, %{report | malformed: true}}
    else
      Process.put(:elixir_json_open, Map.put(open, label, true))
      {v, elem_report} = json_fields(Map.fetch!(@tables, table), rest_pairs)
      Process.put(:elixir_json_open, Map.delete(Process.get(:elixir_json_open, %{}), label))

      Process.put(
        :elixir_json_defs,
        Map.put(Process.get(:elixir_json_defs, %{}), label, {table, v})
      )

      {v, merge_report(report, elem_report)}
    end
  end

  defp json_read_node(table, pairs, report) when is_list(pairs) do
    {v, elem_report} = json_fields(Map.fetch!(@tables, table), pairs)
    {v, merge_report(report, elem_report)}
  end

  # merge_report folds one nested value's report into the enclosing one
  # (§16.2): the counters sum and malformed sticks, so an event inside a list
  # element is never lost when the element's own walk returns.
  defp merge_report(a, b) do
    base = %{
      unknown: a.unknown + b.unknown,
      kind_mismatch: a.kind_mismatch + b.kind_mismatch,
      widened: a.widened + b.widened,
      clamped: a.clamped + b.clamped,
      malformed: a.malformed or b.malformed,
      retained: a.retained + b.retained,
      retain_lost: a.retain_lost + b.retain_lost,
      reason: Map.get(a, :reason, "")
    }

    case Map.get(a, :duplicate, 0) + Map.get(b, :duplicate, 0) do
      0 -> base
      d -> Map.put(base, :duplicate, d)
    end
  end

  # ---- a JSON parser over the standard library (docs/SPEC-TABLES.md §16.3):
  # ---- objects, arrays, strings with escapes, numbers, booleans and null

  defp json_value(_bin, depth) when depth > 128, do: :error

  defp json_value(bin, depth) do
    case json_skip(bin) do
      <<"{", rest::binary>> -> json_object_p(rest, depth)
      <<"[", rest::binary>> -> json_array_p(rest, depth)
      <<"\"", rest::binary>> -> json_string(rest, [])
      <<"true", rest::binary>> -> {:ok, true, rest}
      <<"false", rest::binary>> -> {:ok, false, rest}
      <<"null", rest::binary>> -> {:ok, nil, rest}
      <<?-, _::binary>> = num -> json_number(num)
      <<c, _::binary>> = num when c in ?0..?9 -> json_number(num)
      _ -> :error
    end
  end

  defp json_skip(<<c, rest::binary>>) when c in [?\s, ?\t, ?\n, ?\r], do: json_skip(rest)
  # comments are accepted on read (docs/SPEC-TABLES.md §16.2); an
  # unclosed block comment leaves a byte behind, so the document is malformed.
  defp json_skip(<<"//", rest::binary>>), do: json_skip_line(rest)
  defp json_skip(<<"/*", rest::binary>>), do: json_skip_block(rest)
  defp json_skip(bin), do: bin

  defp json_skip_line(<<"\n", rest::binary>>), do: json_skip(rest)
  defp json_skip_line(<<_, rest::binary>>), do: json_skip_line(rest)
  defp json_skip_line(<<>>), do: <<>>

  defp json_skip_block(<<"*/", rest::binary>>), do: json_skip(rest)
  defp json_skip_block(<<_, rest::binary>>), do: json_skip_block(rest)
  defp json_skip_block(<<>>), do: <<0>>

  defp json_object_p(bin, depth) do
    case json_skip(bin) do
      <<"}", rest::binary>> -> {:ok, {:obj, []}, rest}
      _ -> json_object_pairs(bin, [], depth)
    end
  end

  defp json_object_pairs(bin, acc, depth) do
    case json_skip(bin) do
      <<"}", rest::binary>> ->
        {:ok, {:obj, Enum.reverse(acc)}, rest}

      <<"\"", rest::binary>> ->
        with {:ok, key, r1} <- json_string(rest, []),
             <<":", r2::binary>> <- json_skip(r1),
             {:ok, val, r3} <- json_object_val(key, acc, r2, depth + 1) do
          case json_skip(r3) do
            <<",", r4::binary>> -> json_object_pairs(r4, [{key, val} | acc], depth)
            <<"}", r4::binary>> -> {:ok, {:obj, Enum.reverse([{key, val} | acc])}, r4}
            _ -> :error
          end
        else
          _ -> :error
        end

      _ ->
        :error
    end
  end

  # json_object_val reads one member's value; in a variable-class document
  # a key opening with & is the §16.7 label and only "&node", first, is one.
  defp json_object_val(key, acc, bin, depth) do
    if String.starts_with?(key, "&") and Process.get(:elixir_json_variable, false) do
      if key != "&node" or acc != [] do
        :error
      else
        json_label(bin)
      end
    else
      json_value(bin, depth)
    end
  end

  # json_label reads a §16.7 label: a positive decimal integer of at most
  # 64 bits, no sign, fraction or exponent (docs/SPEC-TABLES.md §16.7).
  defp json_label(bin) do
    case json_skip(bin) do
      <<first, rest::binary>> when first in ?1..?9 ->
        json_label_digits(rest, first - ?0)

      _ ->
        :error
    end
  end

  defp json_label_digits(<<c, rest::binary>>, acc) when c in ?0..?9 do
    next = acc * 10 + (c - ?0)

    if next > 0xFFFFFFFFFFFFFFFF do
      :error
    else
      json_label_digits(rest, next)
    end
  end

  defp json_label_digits(rest, acc) do
    case rest do
      <<c, _::binary>> when c in [?., ?e, ?E, ?+, ?-] -> :error
      _ -> {:ok, acc, rest}
    end
  end

  defp json_array_p(bin, depth) do
    case json_skip(bin) do
      <<"]", rest::binary>> -> {:ok, [], rest}
      _ -> json_array_elems(bin, [], depth)
    end
  end

  defp json_array_elems(bin, acc, depth) do
    case json_skip(bin) do
      <<"]", rest::binary>> ->
        {:ok, Enum.reverse(acc), rest}

      _ ->
        case json_value(bin, depth + 1) do
          {:ok, val, r1} ->
            case json_skip(r1) do
              <<",", r2::binary>> -> json_array_elems(r2, [val | acc], depth)
              <<"]", r2::binary>> -> {:ok, Enum.reverse([val | acc]), r2}
              _ -> :error
            end

          :error ->
            :error
        end
    end
  end

  defp json_string(<<"\"", rest::binary>>, acc),
    do: {:ok, IO.iodata_to_binary(Enum.reverse(acc)), rest}

  defp json_string(<<"\\", rest::binary>>, acc) do
    case rest do
      <<"\"", r::binary>> ->
        json_string(r, ["\"" | acc])

      <<"\\", r::binary>> ->
        json_string(r, ["\\" | acc])

      <<"/", r::binary>> ->
        json_string(r, ["/" | acc])

      <<"b", r::binary>> ->
        json_string(r, ["\b" | acc])

      <<"f", r::binary>> ->
        json_string(r, ["\f" | acc])

      <<"n", r::binary>> ->
        json_string(r, ["\n" | acc])

      <<"r", r::binary>> ->
        json_string(r, ["\r" | acc])

      <<"t", r::binary>> ->
        json_string(r, ["\t" | acc])

      <<"u", a, b, c, d, r::binary>> ->
        with {:ok, ha} <- hex_digit(a),
             {:ok, hb} <- hex_digit(b),
             {:ok, hc} <- hex_digit(c),
             {:ok, hd} <- hex_digit(d) do
          code = ha * 4096 + hb * 256 + hc * 16 + hd
          point = if code in 0xD800..0xDFFF, do: 0xFFFD, else: code
          json_string(r, [<<point::utf8>> | acc])
        else
          _ -> :error
        end

      _ ->
        :error
    end
  end

  # a byte past ASCII must open a well-formed UTF-8 sequence: RFC 8259 texts
  # are UTF-8, and an ill-formed byte spells no code point, so the read refuses
  # the text at the string site rather than replacing or clamping it
  # (docs/SPEC-TABLES.md §16.3, conformance row str-illformed-at-bound)
  defp json_string(<<ch::utf8, rest::binary>>, acc) when ch >= 0x80,
    do: json_string(rest, [<<ch::utf8>> | acc])

  defp json_string(<<c, rest::binary>>, acc) when c >= 0x20 and c < 0x80,
    do: json_string(rest, [<<c>> | acc])

  defp json_string(_, _acc), do: :error

  defp hex_digit(c) when c in ?0..?9, do: {:ok, c - ?0}
  defp hex_digit(c) when c in ?a..?f, do: {:ok, c - ?a + 10}
  defp hex_digit(c) when c in ?A..?F, do: {:ok, c - ?A + 10}
  defp hex_digit(_), do: :error

  # json_number reads the RFC 8259 number grammar strictly (no leading zero,
  # no plus sign, digits after a point and an exponent); an integer stays an
  # integer and anything else stays its decimal text, {:decimal, token}, so a
  # fixed-point field reads it exactly and every other field reads the double
  # it spells (docs/SPEC-TABLES.md §3, §4.12, §16.2).
  defp json_number(bin) do
    with {:ok, neg, int_value, r1} <- json_num_sign(bin) |> json_num_int(),
         {:ok, frac_digits, r3} <- json_num_frac(r1),
         {:ok, exp_str, r4} <- json_num_exp(r3),
         :ok <- json_num_tail(r4) do
      sign_str = if neg, do: "-", else: ""

      cond do
        neg and int_value == 0 and frac_digits == "" and exp_str == "" ->
          # -0 KEEPS ITS SIGN (docs/SPEC-TABLES.md §3.3, §16.2): an integer
          # has no -0, so the token rides as its decimal text and a float
          # field reads -0.0 from it, where an integer field still reads 0
          {:ok, {:decimal, "-0"}, r4}

        frac_digits == "" and exp_str == "" ->
          {:ok, if(neg, do: -int_value, else: int_value), r4}

        true ->
          token =
            sign_str <>
              Integer.to_string(int_value) <>
              if(frac_digits == "", do: ".0", else: "." <> frac_digits) <> exp_str

          {:ok, {:decimal, token}, r4}
      end
    end
  end

  defp json_num_sign(<<"-", rest::binary>>), do: {:ok, true, rest}
  defp json_num_sign(<<"+", _::binary>>), do: :error
  defp json_num_sign(rest), do: {:ok, false, rest}

  # the integer part is accumulated by value, not as a text binary: one binary
  # per element for a 70000-element integer array is the allocation this audit
  # refuses (docs/SPEC-TABLES.md §16.2 gates the shape, not the cost).
  defp json_num_int({:ok, neg, rest}) do
    case json_int_value(rest, 0) do
      {:ok, value, rest2} -> {:ok, neg, value, rest2}
      :error -> :error
    end
  end

  defp json_num_int(:error), do: :error

  defp json_int_value(<<"0", c, _::binary>>, _acc) when c in ?0..?9, do: :error
  defp json_int_value(<<"0", rest::binary>>, _acc), do: {:ok, 0, rest}

  defp json_int_value(<<c, rest::binary>>, _acc) when c in ?1..?9,
    do: json_int_digits(rest, c - ?0)

  defp json_int_value(_, _acc), do: :error

  defp json_int_digits(<<c, rest::binary>>, acc) when c in ?0..?9,
    do: json_int_digits(rest, acc * 10 + (c - ?0))

  defp json_int_digits(rest, acc), do: {:ok, acc, rest}

  defp json_num_frac(<<".", c, rest::binary>>) when c in ?0..?9, do: json_digits(rest, [c])
  defp json_num_frac(<<".", _::binary>>), do: :error
  defp json_num_frac(rest), do: {:ok, "", rest}

  defp json_num_exp(<<e, s, c, rest::binary>>)
       when e in [?e, ?E] and s in [?+, ?-] and c in ?0..?9 do
    {:ok, d, r} = json_digits(rest, [c])
    {:ok, <<e, s>> <> d, r}
  end

  defp json_num_exp(<<e, c, rest::binary>>) when e in [?e, ?E] and c in ?0..?9 do
    {:ok, d, r} = json_digits(rest, [c])
    {:ok, <<e>> <> d, r}
  end

  defp json_num_exp(<<e, _::binary>>) when e in [?e, ?E], do: :error
  defp json_num_exp(rest), do: {:ok, "", rest}

  defp json_num_tail(<<c, _::binary>>) when c in [?., ?e, ?E, ?+, ?-] or c in ?0..?9, do: :error
  defp json_num_tail(_), do: :ok

  defp json_digits(<<c, rest::binary>>, acc) when c in ?0..?9, do: json_digits(rest, [c | acc])
  defp json_digits(rest, acc), do: {:ok, acc |> Enum.reverse() |> IO.iodata_to_binary(), rest}

  # json_decimal_float is the double a decimal text spells; one past the
  # double's range is :overflow_float, which no field accepts (§16.2).
  defp json_decimal_float({:decimal, token}) do
    case Float.parse(token) do
      {f, ""} -> f
      _ -> :overflow_float
    end
  rescue
    _ -> :overflow_float
  end

  # fixed_raw converts a JSON number to the exact scaled integer §3 stores
  # (docs/SPEC-TABLES.md §3, §4.12). A fraction finer than 2^-F is a kind
  # mismatch, never rounded through a float.
  defp fixed_raw(raw, frac) when is_integer(raw), do: {:ok, raw <<< frac}

  defp fixed_raw({:decimal, token}, frac) do
    [mantissa | exponent] = String.split(token, ~r/[eE]/, parts: 2)
    power = if exponent == [], do: 0, else: String.to_integer(hd(exponent))
    negative = String.starts_with?(mantissa, "-")
    unsigned = String.trim_leading(mantissa, "-")
    [whole | tail] = String.split(unsigned, ".", parts: 2)
    fraction = if tail == [], do: "", else: hd(tail)
    digits = String.to_integer(whole <> fraction)
    shift = power - byte_size(fraction)
    numerator = digits <<< frac

    cond do
      # the token cap (511 bytes) bounds the digits, so an exponent past 700
      # either way decides without the power: zero stays zero, a larger shift
      # is past every field's range (the clamp counts it) and a smaller one is
      # finer than 2^-F with no exact scaled integer
      digits == 0 ->
        {:ok, 0}

      shift > 700 ->
        v = Integer.pow(10, 700)
        {:ok, if(negative, do: -v, else: v)}

      shift < -700 ->
        :error

      true ->
        {numerator, denominator} =
          if shift >= 0,
            do: {numerator * Integer.pow(10, shift), 1},
            else: {numerator, Integer.pow(10, -shift)}

        if rem(numerator, denominator) == 0 do
          v = div(numerator, denominator)
          {:ok, if(negative, do: -v, else: v)}
        else
          :error
        end
    end
  end

  defp fixed_raw(_, _), do: :error

  # ---- the region measure (docs/SPEC-TABLES.md §4.2, §6.5) ----
  #
  # load_measure/2 answers the region bytes a variable-class Load owes, derived
  # from the FRAMING before any value is decoded: the root's record and the
  # node extent its maps and lists command, each node record's own record and
  # extent, each blob the root can reach, and the attribution directory of one
  # u64 pair per record plus the root (§6.3). A map's or a list's extent is
  # count × the element's size at its alignment wherever it sits — in a field,
  # an element, a union arm or a map entry — so a dropped entry still commands
  # its slot. It is the number the compiler's own engine derives
  # (internal/tablewire, FileRegionMeasure), so a caller sizes a region and the
  # harness's oracle holds the measure to it exactly. A count past the storage
  # cap, or past what the bytes behind it can frame, answers -1, and so does a
  # wire this reader cannot open. A fixed-class root commands its record alone.

  def load_measure(table, _data) when table in @uncarried, do: -1

  def load_measure(table, data) when is_binary(data) do
    desc = Map.fetch!(@tables, table)

    cond do
      not desc.variable ->
        region_round(desc.size)

      true ->
        case open(data) do
          {:error, _} -> -1
          {:ok, body, ids} -> measure_region(desc, body, ids)
        end
    end
  end

  defp measure_region(desc, body, ids) do
    case record_storage(desc, body, ids) do
      :refused ->
        -1

      root ->
        case node_records(body, ids) do
          {:ok, records} -> measure_records(desc, records, ids, root, 0)
          :partial -> root + 16
        end
    end
  end

  # one record's storage: its own size rounded, plus the extent its framing
  # commands, rounded again
  defp record_storage(desc, body, ids) do
    case scan_record(desc, body, ids, 0) do
      :refused -> :refused
      at -> region_round(region_round(desc.size) + at)
    end
  end

  defp measure_records(_desc, [], _ids, total, n), do: total + (n + 1) * 16

  defp measure_records(desc, [{type_id, body} | rest], ids, total, n) do
    case Map.get(desc.nodes, type_id) do
      nil ->
        blob =
          cond do
            type_id == @string_id and desc.string_blob -> region_round(8 + byte_size(body) + 1)
            type_id == @bytes_id and desc.bytes_blob -> region_round(8 + byte_size(body))
            true -> 0
          end

        measure_records(desc, rest, ids, total + blob, n + 1)

      name ->
        case record_storage(Map.fetch!(@tables, name), body, ids) do
          :refused -> -1
          bytes -> measure_records(desc, rest, ids, total + bytes, n + 1)
        end
    end
  end

  defp region_round(n), do: div(n + @region_align - 1, @region_align) * @region_align

  defp align_to(n, a), do: div(n + a - 1, a) * a

  # scan_record walks one body's field headers summing the extent every map
  # and list below it commands, and descending into every by-value table,
  # element, entry and arm on the way. Damage ends the walk with what it has
  # summed; a count the framing cannot hold answers :refused.
  defp scan_record(desc, bin, ids, at) do
    with {ref, rest} when ref != 0 <- leb(bin),
         id when id != :error <- resolve(ids, ref),
         <<kind, rest2::binary>> <- rest do
      case scan_header(find_field(desc, id), kind, rest2, ids, at) do
        :refused -> :refused
        {:cont, at2, rest3} -> scan_record(desc, rest3, ids, at2)
        {:halt, at2} -> at2
      end
    else
      _ -> at
    end
  end

  defp scan_header(%{t: :union} = f, 15, bin, ids, at) do
    case scan_union(f.elem, bin, ids, at) do
      :refused -> :refused
      {at2, rest} -> {:cont, at2, rest}
    end
  end

  defp scan_header(f, kind, bin, ids, at) when f != nil and kind in [13, 14, 16] do
    case framed_body(bin) do
      :error ->
        {:halt, at}

      {payload, rest} when kind == f.kind ->
        case scan_field(f, payload, ids, at) do
          :refused -> :refused
          at2 -> {:cont, at2, rest}
        end

      {_payload, rest} ->
        {:cont, at, rest}
    end
  end

  defp scan_header(_f, kind, bin, _ids, at) do
    case slice(bin, kind) do
      {:ok, _payload, rest} -> {:cont, at, rest}
      :error -> {:halt, at}
    end
  end

  defp framed_body(bin) do
    case leb(bin) do
      {n, rest} when byte_size(rest) >= n ->
        {binary_part(rest, 0, n), binary_part(rest, n, byte_size(rest) - n)}

      _ ->
        :error
    end
  end

  defp scan_field(%{t: t}, _body, _ids, at) when t in [:pointer, :blob], do: at

  defp scan_field(%{t: t} = f, body, ids, at) when t in [:map, :list, :keyed] do
    expected = if t == :map, do: 13, else: f.elem_kind

    with <<kind, rest::binary>> when byte_size(rest) >= 1 <- body,
         true <- kind == expected or kind_widens?(kind, expected),
         {count, elems} <- leb(rest) do
      case element_extent(f) do
        nil ->
          scan_elements(f, kind, elems, count, ids, at)

        {size, align} ->
          if count > 0x7FFFFFFF or count > div(byte_size(elems), count_floor(kind)) do
            :refused
          else
            scan_elements(f, kind, elems, count, ids, align_to(at, align) + count * size)
          end
      end
    else
      _ -> at
    end
  end

  defp scan_field(%{t: :table} = f, body, ids, at),
    do: scan_record(Map.fetch!(@tables, f.elem), body, ids, at)

  defp scan_field(_f, _body, _ids, at), do: at

  # the storage one element of a map or an unbounded list takes in its
  # holder's node extent; a fixed or counted array and an enum-keyed array
  # sit inside the record and command none
  defp element_extent(%{t: :map} = f) do
    entry = Map.fetch!(@tables, f.elem)
    {entry.size, entry.align}
  end

  defp element_extent(%{t: :list} = f) when not is_map_key(f, :array_bound),
    do: {f.elem_size, f.elem_align}

  defp element_extent(_f), do: nil

  # the fewest bytes one element of a kind can take, so a count the bytes behind
  # it cannot frame is refused before any storage is reserved for it
  defp count_floor(13), do: 2
  defp count_floor(17), do: 1
  defp count_floor(36), do: 8
  defp count_floor(kind), do: max(kind_width(kind), 1)

  # the elements of one map, list or array that are bodies of their own:
  # tables, map entries and union arms (an enum-keyed slot leads with its key)
  defp scan_elements(_f, kind, _elems, _n, _ids, at) when kind not in [13, 15], do: at
  defp scan_elements(_f, _kind, <<>>, _n, _ids, at), do: at
  defp scan_elements(_f, _kind, _elems, 0, _ids, at), do: at

  defp scan_elements(f, kind, elems, n, ids, at) do
    case scan_key(f, elems) do
      {_key, rest} -> scan_element(f, kind, rest, n, ids, at)
      :error -> at
    end
  end

  defp scan_key(%{t: :keyed}, elems), do: leb(elems)
  defp scan_key(_f, elems), do: {nil, elems}

  defp scan_element(f, 15, elems, n, ids, at) do
    case scan_union(element_name(f), elems, ids, at) do
      :refused -> :refused
      {at2, rest} -> scan_elements(f, 15, rest, n - 1, ids, at2)
    end
  end

  defp scan_element(f, kind, elems, n, ids, at) do
    case framed_body(elems) do
      :error ->
        at

      {payload, rest} ->
        case scan_record(Map.fetch!(@tables, element_name(f)), payload, ids, at) do
          :refused -> :refused
          at2 -> scan_elements(f, kind, rest, n - 1, ids, at2)
        end
    end
  end

  defp element_name(%{t: :keyed, item: item}), do: item.elem
  defp element_name(f), do: f.elem

  # scan_union reads one arm header and descends into the arm's payload the
  # way a field of the arm's type is descended into: a by-value table, a list,
  # a map, a nested union. It answers the extent and the bytes after the arm;
  # damage answers no bytes after it, which ends the enclosing walk.
  defp scan_union(union, bin, ids, at) do
    with {ref, rest} <- leb(bin) do
      if ref == 0 do
        {at, rest}
      else
        with id when id != :error <- resolve(ids, ref),
             <<_kind, rest2::binary>> <- rest,
             {payload, rest3} <- framed_body(rest2) do
          case scan_arm(union, id, payload, ids, at) do
            :refused -> :refused
            at2 -> {at2, rest3}
          end
        else
          _ -> {at, <<>>}
        end
      end
    else
      _ -> {at, <<>>}
    end
  end

  defp scan_arm(union, id, payload, ids, at) do
    un = Map.fetch!(@unions, union)

    case Map.fetch(un.by_id, id) do
      :error ->
        at

      {:ok, name} ->
        case Map.fetch!(un.by_name, name) do
          %{t: :union} = a ->
            case scan_union(a.elem, payload, ids, at) do
              :refused -> :refused
              {at2, _rest} -> at2
            end

          a ->
            scan_field(a, payload, ids, at)
        end
    end
  end

  # node_records reads the node table out of the root body (docs/SPEC-TABLES.md
  # §3.1) as {type id, body} pairs. The last node field in the body is the one
  # read, as the engine reads it; a node field that cannot be read whole, or a
  # record list that does not match its count, answers :partial, and the
  # measure is then the root's own.
  defp node_records(bin, ids), do: node_records(bin, ids, {:ok, []})

  defp node_records(bin, ids, found) do
    with {ref, rest} when ref != 0 <- leb(bin),
         id when id != :error <- resolve(ids, ref),
         <<kind, rest2::binary>> <- rest do
      cond do
        id == @node_id and kind != 12 ->
          :partial

        id == @node_id ->
          case framed_body(rest2) do
            {payload, rest3} ->
              case scan_node_table(payload, ids) do
                {:ok, records} -> node_records(rest3, ids, {:ok, records})
                :error -> :partial
              end

            :error ->
              :partial
          end

        true ->
          case slice(rest2, kind) do
            {:ok, _payload, rest3} -> node_records(rest3, ids, found)
            :error -> found
          end
      end
    else
      _ -> found
    end
  end

  defp scan_node_table(payload, ids) do
    case leb(payload) do
      {declared, rest} -> scan_node_records(rest, ids, declared, [])
      :error -> :error
    end
  end

  defp scan_node_records(<<>>, _ids, declared, acc) do
    if declared == length(acc), do: {:ok, Enum.reverse(acc)}, else: :error
  end

  defp scan_node_records(bin, ids, declared, acc) do
    with {tref, r1} when tref != 0 <- leb(bin),
         id when id != :error <- resolve(ids, tref),
         {len, r2} when byte_size(r2) >= len <- leb(r1) do
      scan_node_records(
        binary_part(r2, len, byte_size(r2) - len),
        ids,
        declared,
        [{id, binary_part(r2, 0, len)} | acc]
      )
    else
      _ -> :error
    end
  end
`

// variableRetainSource is the retained round trip (docs/SPEC-TABLES.md §6.6): a
// load that keeps what this build cannot name in the caller's two bounded
// stores, a measure and a save that re-emit it at the end of the body it came
// from. The retained record is READER-PRIVATE, so its layout is this port's
// own; only the re-emitted bytes must match the reference legs byte for byte.
const variableRetainSource = `
  # ---- retain-unknown (docs/SPEC-TABLES.md §6.6) ----
  #
  # TableRetain is the caller's two bounded stores, threaded through the three
  # verbs and never grown by the codec:
  #   %{records: [{path, id, kind, resolved}], used: bytes, cap: cap,
  #     ids: [retained id], id_cap: cap}
  # A path is a list of {field_ordinal, element_index} steps; the root body's is
  # []. A record costs 9 + byte_size(resolved) + 8 * length(path) bytes, and a
  # record the remaining capacity cannot hold whole is dropped and counted one
  # retain_lost (§6.6).

  def load_retain(table, data, retain, report) when is_binary(data) do
    desc = Map.fetch!(@tables, table)
    # §6.6: a retention buffer belongs to one loaded region; the next LoadRetain
    # into it resets both stores, keeping only the caller's capacities.
    retain = %{retain | records: [], used: 0, ids: []}

    case open(data) do
      {:error, refusal} ->
        {:error, :malformed, reasoned(report, refusal), retain}

      {:ok, body, ids} ->
        case entries(body, ids, desc.string_blob, desc.variable) do
          {:ok, fields, nodes} ->
            # the node directory's memo: a node a second pointer names is the
            # one value, read and retained once (§6.6, the path's first step)
            retain = Map.put(retain, :memo, %{})
            {value, report, retain} = decode_retain(desc, fields, nodes, ids, retain, report, [])
            report = if :malformed in nodes, do: %{report | malformed: true}, else: report
            # an unknown NODE RECORD whose type id this reader cannot name is an
            # excluded class (§6.6): one retain_lost, counted once at the record
            unknown = Enum.count(nodes, &(&1 == :unknown))

            report = %{
              report
              | unknown: report.unknown + unknown,
                retain_lost: report.retain_lost + unknown
            }

            # the records were kept newest first; they are saved in the order
            # retained
            retain = %{Map.delete(retain, :memo) | records: Enum.reverse(retain.records)}
            {:ok, Map.merge(defaults(desc), value), reasoned(report, :ok), retain}

          _ ->
            {:error, :malformed, reasoned(report, :malformed), retain}
        end
    end
  catch
    # a NaN or an infinity refuses the retaining read as it does the plain one
    # (docs/SPEC-TABLES.md §3.3): nothing lands and nothing is retained
    {:non_finite_float, name} ->
      {:error, :non_finite_float, reasoned(report, {:non_finite_float, name}),
       %{retain | records: [], used: 0, ids: []}}
  end

  def measure_retain(table, value, retain) when is_map(value) do
    case save_retain_internal(table, value, retain, empty_report()) do
      {:ok, bytes, _retain, _report} -> byte_size(bytes)
      {:error, :blob_shared} -> -1
    end
  catch
    :non_finite_float -> -1
  end

  def save_retain(table, value, retain, report) when is_map(value) do
    case save_retain_internal(table, value, retain, report) do
      {:ok, bytes, retain, report} -> {:ok, bytes, report, retain}
      {:error, :blob_shared} -> {:error, :blob_shared, report, retain}
    end
  catch
    # every writer refuses a float that is not finite (docs/SPEC-TABLES.md §3.3)
    :non_finite_float -> {:error, :non_finite_float, report, retain}
  end

  defp save_retain_internal(table, value, retain, report) do
    desc = Map.fetch!(@tables, table)

    try do
      nodes = collect_root(desc, value)
      idx_map = node_index(nodes)

      blob_site_begin(:root)

      # the records of one body are found by its path: grouped once, they are
      # looked up by the walk rather than scanned for at every body
      retain =
        retain
        |> Map.put(:matched, 0)
        |> Map.put(:by_path, Enum.group_by(retain.records, &elem(&1, 0)))

      {s, body} = save_fields_retain(desc, value, {idx_map, %{}, [], retain, report}, [], [])
      {{_idx_map, _ids, order, retain, report}, body} = save_node_table_retain(nodes, s, body)
      ids_bytes = for id <- Enum.reverse(order), into: <<>>, do: <<id::little-64>>

      # a record whose path no longer names a body was lost by the caller's
      # edit between load and save: one retain_lost each (§6.6)
      orphans = length(retain.records) - retain.matched
      report = %{report | retain_lost: report.retain_lost + orphans}
      retain = retain |> Map.delete(:matched) |> Map.delete(:by_path)

      {:ok, IO.iodata_to_binary([<<1>>, body, <<0>>, ids_bytes, <<length(order)::little-64>>]),
       retain, report}
    catch
      :blob_shared -> {:error, :blob_shared}
    after
      Process.delete(:elixir_blob_site)
    end
  end

  # one body's fields, then its retained tail. The walk's state is the node
  # index map, the id map and its first-use order, and the two caller stores
  # with the report.
  defp save_fields_retain(desc, value, s, path, acc) do
    {s, acc} =
      Enum.reduce(desc.fields, {s, acc}, fn f, {s, acc} ->
        emit_field_retain(f, value, s, path, acc)
      end)

    emit_retain_tail(s, path, acc)
  end

  # a nested table by value: its body is the step pair {ordinal, 0}. A body
  # that carries a retained field does not elide (§6.6).
  defp emit_field_retain(%{t: :table} = f, value, s, path, acc) do
    {idx_map, ids, order, retain, report} = s

    case Map.get(value, f.name) do
      nil ->
        {s, acc}

      v ->
        {ref, ids1, order1} = intern(ids, order, f.id)

        {{idx_map2, ids2, order2, retain2, report2}, body} =
          save_fields_retain(
            Map.fetch!(@tables, f.elem),
            v,
            {idx_map, ids1, order1, retain, report},
            path ++ [{f.ordinal, 0}],
            []
          )

        if IO.iodata_length(body) == 0 and not Map.get(f, :optional, false) do
          {{idx_map, ids, order, retain2, report2}, acc}
        else
          framed = [body, <<0>>]

          {{idx_map2, ids2, order2, retain2, report2},
           [acc, leb_encode(ref), <<13>>, leb_encode(IO.iodata_length(framed)), framed]}
        end
    end
  end

  # an array of tables: element i is the step pair {ordinal, i}. A map is the
  # same framing over its entry tables, in ascending key order, so an entry's
  # slot is its index.
  defp emit_field_retain(%{t: :map} = f, value, s, path, acc) do
    entries = map_entries(f, Map.get(value, f.name, %{}))
    emit_field_retain(%{f | t: :list}, %{f.name => entries}, s, path, acc)
  end

  defp emit_field_retain(%{t: :list, elem_kind: kind} = f, value, s, path, acc)
       when kind in [13, 15] do
    {idx_map, ids, order, retain, report} = s
    elements = pad_fixed(f, Map.get(value, f.name, Map.get(f, :default)))

    if elements == nil or list_elides?(f, elements) do
      {s, acc}
    else
      {ref, ids, order} = intern(ids, order, f.id)

      {body, s} =
        save_elements_retain(f, elements, 0, {idx_map, ids, order, retain, report}, path, [])

      array = [<<f.elem_kind>>, leb_encode(length(elements)), body]
      {s, [acc, leb_encode(ref), <<14>>, leb_encode(IO.iodata_length(array)), array]}
    end
  end

  # an enum-keyed array: slot k is the step pair {ordinal, k}; a slot's element
  # is a body of its own (§3.2).
  defp emit_field_retain(%{t: :keyed} = f, value, s, path, acc) do
    {idx_map, ids, order, retain, report} = s
    slots = Map.get(value, f.name) || %{}
    {ref, ids1, order1} = intern(ids, order, f.id)
    item = Map.put(f.item, :elem_kind, f.elem_kind)

    {pairs, n, {_, ids2, order2, retain2, report2}} =
      f.key_names
      |> Enum.with_index()
      |> Enum.reduce({[], 0, {idx_map, ids1, order1, retain, report}}, fn {name, slot},
                                                                          {pairs, n, s} ->
        {idx_map, ids, order, retain, report} = s
        v = Map.get(slots, name, f.item.default)
        {kref, ids_k, order_k} = intern(ids, order, Map.fetch!(f.key_ids, name))

        case keyed_elem_retain(
               item,
               v,
               {idx_map, ids_k, order_k, retain, report},
               path ++ [{f.ordinal, slot}]
             ) do
          {:elide, {_, _, _, retain, report}} ->
            {pairs, n, {idx_map, ids, order, retain, report}}

          {elem, s} ->
            {[pairs, leb_encode(kref), leb_encode(IO.iodata_length(elem)), elem], n + 1, s}
        end
      end)

    if n == 0 do
      {{idx_map, ids, order, retain2, report2}, acc}
    else
      inner = [<<f.elem_kind>>, leb_encode(n), pairs]

      {{idx_map, ids2, order2, retain2, report2},
       [acc, leb_encode(ref), <<16>>, leb_encode(IO.iodata_length(inner)), inner]}
    end
  end

  defp emit_field_retain(%{t: :union} = f, value, s, path, acc) do
    {idx_map, ids, order, retain, report} = s

    case Map.get(value, f.name) do
      nil ->
        {s, acc}

      sel ->
        {ref, ids, order} = intern(ids, order, f.id)

        {header, s} =
          arm_header_retain(f.elem, f.ordinal, sel, {idx_map, ids, order, retain, report}, path)

        {s, [acc, leb_encode(ref), <<15>>, header]}
    end
  end

  defp emit_field_retain(f, value, {idx_map, ids, order, retain, report}, _path, acc) do
    {idx_map, ids, order, acc} = emit_field(f, value, idx_map, ids, order, acc)
    {{idx_map, ids, order, retain, report}, acc}
  end

  defp keyed_elem_retain(%{elem_kind: 13} = item, v, s, path) do
    {s, body} = save_fields_retain(Map.fetch!(@tables, item.elem), v || %{}, s, path, [])
    if IO.iodata_length(body) == 0, do: {:elide, s}, else: {[body, <<0>>], s}
  end

  defp keyed_elem_retain(item, v, {idx_map, ids, order, retain, report} = s, _path) do
    if v == item.default do
      {:elide, s}
    else
      {elem, ids, order} = emit_element(item, v, idx_map, ids, order)
      {elem, {idx_map, ids, order, retain, report}}
    end
  end

  # one arm header with its arm's body retaining: the arm is the step pair
  # {union field ordinal, arm ordinal}, and not "whichever arm is set", so a
  # switched arm leaves its records no body to name (§6.6).
  defp arm_header_retain(_union, _ordinal, nil, s, _path), do: {leb_encode(0), s}

  defp arm_header_retain(union, ordinal, {arm, payload}, s, path) do
    {idx_map, ids, order, retain, report} = s
    a = Map.fetch!(Map.fetch!(@unions, union).by_name, arm)
    {aref, ids, order} = intern(ids, order, Map.get(a, :arm_id, a.id))
    arm_path = path ++ [{ordinal, a.ordinal}]

    {content, s} =
      arm_content_retain(
        a,
        if(payload == nil, do: a.default, else: payload),
        {idx_map, ids, order, retain, report},
        arm_path
      )

    {[leb_encode(aref), <<a.arm_kind>>, leb_encode(IO.iodata_length(content)), content], s}
  end

  defp arm_content_retain(%{t: :table} = a, v, s, arm_path) do
    {s, body} = save_fields_retain(Map.fetch!(@tables, a.elem), v || %{}, s, arm_path, [])
    {[body, <<0>>], s}
  end

  defp arm_content_retain(a, v, {idx_map, ids, order, retain, report}, _arm_path) do
    {content, ids, order} = arm_content(a, v, idx_map, ids, order)
    {content, {idx_map, ids, order, retain, report}}
  end

  defp save_elements_retain(_f, [], _index, s, _path, acc), do: {acc, s}

  defp save_elements_retain(f, [e | rest], index, s, path, acc) do
    {framed, s} = emit_element_retain(f, e, s, path, index)
    save_elements_retain(f, rest, index + 1, s, path, [acc, framed])
  end

  defp emit_element_retain(%{elem_kind: 13} = f, e, s, path, index) do
    {s, body} =
      save_fields_retain(Map.fetch!(@tables, f.elem), e, s, path ++ [{f.ordinal, index}], [])

    {[leb_encode(IO.iodata_length(body) + 1), body, <<0>>], s}
  end

  defp emit_element_retain(%{elem_kind: 15} = f, e, s, path, index),
    do: arm_header_retain(f.elem, f.ordinal, e, s, path ++ [{f.ordinal, index}])

  # emit_retain_tail re-emits the records whose path names the body being
  # written, in the order retained (§6.6).
  defp emit_retain_tail({idx_map, ids, order, retain, report}, path, acc) do
    records = Map.get(retain.by_path, path, [])

    retain =
      if records == [], do: retain, else: %{retain | matched: retain.matched + length(records)}

    {ids, order, retain, report, acc} =
      Enum.reduce(records, {ids, order, retain, report, acc}, fn {_path, id, kind, resolved},
                                                                 {ids, order, retain, report, acc} ->
        {ref, ids, order, retain} = retain_ref(ids, order, retain, id)

        if ref == 0 do
          {ids, order, retain, %{report | retain_lost: report.retain_lost + 1}, acc}
        else
          case retain_emit(ids, order, retain, kind, resolved) do
            {:ok, body, <<>>, ids, order, retain} ->
              {ids, order, retain, report, [acc, leb_encode(ref), <<kind>>, body]}

            _ ->
              {ids, order, retain, %{report | retain_lost: report.retain_lost + 1}, acc}
          end
        end
      end)

    {{idx_map, ids, order, retain, report}, acc}
  end

  # the node table with the node records' retained tails: a node's body is the
  # path {:node, k}, k the index it held in the loaded region's directory,
  # which a loaded node value carries with it (§6.6, the path's first step).
  defp save_node_table_retain([], s, body), do: {s, body}

  defp save_node_table_retain(nodes, {idx_map, ids, order, retain, report}, body) do
    {ref, ids, order} = intern(ids, order, @node_id)

    {records, s} =
      write_node_records_retain(nodes, {idx_map, ids, order, retain, report}, [])

    payload = [leb_encode(length(nodes)), records]

    {s, [body, leb_encode(ref), <<12>>, leb_encode(IO.iodata_length(payload)), payload]}
  end

  defp write_node_records_retain([], s, acc), do: {acc, s}

  defp write_node_records_retain([node | rest], {idx_map, ids, order, retain, report}, acc) do
    {tref, ids, order} = intern(ids, order, elem(node, 0))
    {nbody, s} = node_body_retain(node, {idx_map, ids, order, retain, report})
    acc = [acc, leb_encode(tref), leb_encode(IO.iodata_length(nbody)), nbody]
    write_node_records_retain(rest, s, acc)
  end

  defp node_body_retain({_type_id, :blob, _ref, bytes}, s), do: {bytes, s}

  defp node_body_retain({_type_id, :table, ref, v} = node, s) do
    desc = Map.fetch!(@tables, ref)
    path = [{:node, Map.get(v, :__retain_node__)}]
    {s, body} = in_node_holder(node, fn -> save_fields_retain(desc, v, s, path, []) end)
    {IO.iodata_to_binary([body, <<0>>]), s}
  end

  # ---- the retaining read: decode with a caller-owned store (§6.6) ----

  defp decode_retain(desc, fields, nodes, ids, retain, report, path) do
    Enum.reduce_while(fields, {Map.new(), report, retain}, fn {id, kind, payload},
                                                              {value, report, retain} ->
      case find_field(desc, id) do
        nil ->
          # an unknown FIELD id is the retained class (§6.6): the resolved walk
          # decides, and the six excluded classes count one retain_lost.
          if payload == <<>> do
            # a field the plain read could not delimit — its slice failed — has
            # no bytes to retain: §6.6's capture never runs, one unknown counts
            # and the enclosing read's malformed does (§3, §4).
            {:cont, {value, %{report | unknown: report.unknown + 1}, retain}}
          else
            # A RETAINED FLOAT IS TESTED LIKE A KNOWN ONE (docs/SPEC-TABLES.md
            # §3.3, §6.6): a kind-10 or kind-11 entry whose raw bits are a NaN
            # or an infinity refuses the read, named <owner>.0x<id>.
            if kind in [10, 11] do
              _ = float_of(payload, kind_width(kind), retained_float_name(desc, id))
            end

            case retain_capture(ids, kind, payload) do
              {:ok, resolved} ->
                if retain_room(retain, path, resolved) do
                  retain = retain_put(retain, path, id, kind, resolved)

                  {:cont,
                   {value, %{report | unknown: report.unknown + 1, retained: report.retained + 1},
                    retain}}
                else
                  {:cont,
                   {value,
                    %{report | unknown: report.unknown + 1, retain_lost: report.retain_lost + 1},
                    retain}}
                end

              :error ->
                {:cont,
                 {value,
                  %{report | unknown: report.unknown + 1, retain_lost: report.retain_lost + 1},
                  retain}}
            end
          end

        f ->
          cond do
            kind == f.kind ->
              case read_payload_retain(f, payload, nodes, ids, report, retain, path) do
                {:cont, v, report, retain} -> {:cont, {Map.put(value, f.name, v), report, retain}}
                {:halt, v, report, retain} -> {:halt, {Map.put(value, f.name, v), report, retain}}
              end

            widens?(kind, f) ->
              {v, report} = read_widened(f, kind, payload, report)
              {:cont, {Map.put(value, f.name, v), report, retain}}

            true ->
              {:cont, {value, %{report | kind_mismatch: report.kind_mismatch + 1}, retain}}
          end
      end
    end)
  end

  # a retained unknown float's name: the body's table and the id it cannot
  # name, as the reference leg's tableRetainedFloatName spells it
  defp retained_float_name(desc, id) do
    hex = id |> Integer.to_string(16) |> String.downcase() |> String.pad_leading(16, "0")
    desc.name <> ".0x" <> hex
  end

  # an array of tables: element i is the step pair {ordinal, i}. A later
  # occurrence of the field resets the earlier one's records (§6.6).
  defp read_payload_retain(
         %{t: :list, elem_kind: 13} = f,
         payload,
         nodes,
         ids,
         report,
         retain,
         path
       ) do
    retain = retain_reset(retain, path, f.ordinal, nil)

    with {_len, <<13, count_rest::binary>>} <- leb(payload),
         {n, elems} <- leb(count_rest) do
      {value, report, retain} =
        read_elements_retain(
          Map.fetch!(@tables, f.elem),
          elems,
          n,
          nodes,
          ids,
          [],
          report,
          retain,
          f.ordinal,
          path
        )

      clamp_retain(value, f, report, retain)
    else
      {_len, _other} -> {:cont, [], %{report | kind_mismatch: report.kind_mismatch + 1}, retain}
      _ -> {:cont, [], %{report | malformed: true}, retain}
    end
  end

  # an array of pointers: each element names a node, read once and retained
  # under the node's own path.
  defp read_payload_retain(
         %{t: :list, elem_kind: 17} = f,
         payload,
         nodes,
         ids,
         report,
         retain,
         _path
       ) do
    with {_len, <<17, count_rest::binary>>} <- leb(payload),
         {n, elems} <- leb(count_rest) do
      {value, report, retain} = read_pointers_retain(elems, n, nodes, ids, [], report, retain)
      clamp_retain(value, f, report, retain)
    else
      {_len, _other} -> {:cont, [], %{report | kind_mismatch: report.kind_mismatch + 1}, retain}
      _ -> {:cont, [], %{report | malformed: true}, retain}
    end
  end

  # a nested table by value: the step pair {ordinal, 0}. A later occurrence of
  # the field resets the earlier one's records before it is read (§6.6).
  defp read_payload_retain(%{t: :table} = f, payload, nodes, ids, report, retain, path) do
    case leb(payload) do
      {_len, body} ->
        retain = retain_reset(retain, path, f.ordinal, nil)

        {value, report, retain} =
          read_body_retain(
            Map.fetch!(@tables, f.elem),
            body,
            nodes,
            ids,
            report,
            retain,
            path ++ [{f.ordinal, 0}]
          )

        {:cont, value, report, retain}

      :error ->
        {:cont, %{}, %{report | malformed: true}, retain}
    end
  end

  defp read_payload_retain(%{t: :pointer}, payload, nodes, ids, report, retain, _path) do
    case leb(payload) do
      {0, _} ->
        {:cont, nil, report, retain}

      {1, _} ->
        {:cont, nil, %{report | kind_mismatch: report.kind_mismatch + 1}, retain}

      {idx, _} ->
        case node_value_retain(nodes, idx, ids, report, retain) do
          {{:value, v}, report, retain} when is_map(v) ->
            {:cont, v, report, retain}

          {{:value, _}, report, retain} ->
            {:cont, nil, %{report | kind_mismatch: report.kind_mismatch + 1}, retain}

          {:out_of_range, report, retain} ->
            {:cont, nil, %{report | malformed: true}, retain}

          {:null, report, retain} ->
            {:cont, nil, report, retain}
        end
    end
  end

  # a map is kind 14 over framed entry tables: entry i is the step pair
  # {ordinal, i} while it is read, and its slot in ascending key order once the
  # field is whole, because a key written twice is the later entry's (§2.8)
  # and its slot is the address a save walks (§6.6).
  defp read_payload_retain(%{t: :map} = f, payload, nodes, ids, report, retain, path) do
    case read_payload_retain(%{f | t: :list}, payload, nodes, ids, report, retain, path) do
      {:cont, entries, report, retain} ->
        [kf, vf | _] = Map.fetch!(@tables, f.elem).fields

        keyed =
          Enum.map(entries, fn entry ->
            key = Map.get(entry, "key", kf.default)

            {if(is_binary(key), do: key, else: Integer.to_string(key)),
             Map.get(entry, "value", vf.default)}
          end)

        last = keyed |> Enum.with_index() |> Map.new(fn {{key, _value}, i} -> {key, i} end)

        slots =
          last
          |> Enum.sort_by(fn {key, _i} -> map_sort_key(f, key) end)
          |> Enum.with_index()
          |> Map.new(fn {{_key, i}, slot} -> {i, slot} end)

        {:cont, Map.new(keyed), report, retain_remap(retain, path, f.ordinal, slots)}

      other ->
        other
    end
  end

  # a union field: the arm is the step pair {ordinal, arm ordinal}.
  defp read_payload_retain(%{t: :union} = f, payload, nodes, ids, report, retain, path) do
    retain = retain_reset(retain, path, f.ordinal, nil)
    read_arm_retain(f.elem, f.ordinal, payload, nodes, ids, report, retain, path)
  end

  # an enum-keyed array: slot k is the step pair {ordinal, k}; a slot written
  # again resets the earlier one's records.
  defp read_payload_retain(%{t: :keyed} = f, payload, nodes, ids, report, retain, path) do
    retain = retain_reset(retain, path, f.ordinal, nil)

    with {_len, <<ek, rest::binary>>} <- leb(payload),
         true <- ek == f.elem_kind,
         {n, pairs} <- leb(rest) do
      read_keyed_retain(f, pairs, n, nodes, ids, %{}, report, retain, path)
    else
      _ -> {:cont, %{}, %{report | kind_mismatch: report.kind_mismatch + 1}, retain}
    end
  end

  defp read_payload_retain(%{t: :enum} = f, payload, _nodes, ids, report, retain, _path) do
    case leb(payload) do
      {0, _} ->
        {:cont, nil, report, retain}

      {vref, _} ->
        case resolve(ids, vref) do
          :error ->
            {:halt, nil, %{report | malformed: true}, retain}

          variant_id ->
            case Map.fetch(f.by_id, variant_id) do
              {:ok, name} ->
                {:cont, name, report, retain}

              :error ->
                # an unknown ENUM variant in a KNOWN field is excluded (§6.6):
                # the field is the reader's, so it is reported as loss, never
                # retained silently.
                {:cont, nil,
                 %{report | unknown: report.unknown + 1, retain_lost: report.retain_lost + 1},
                 retain}
            end
        end
    end
  end

  defp read_payload_retain(f, payload, nodes, ids, report, retain, _path) do
    case read_payload(f, payload, nodes, ids, report) do
      {:cont, v, report} -> {:cont, v, report, retain}
      {:halt, v, report} -> {:halt, v, report, retain}
    end
  end

  # a bounded array keeps its first bound elements and counts one clamped
  # (§4); a record under a cut element names no body, and a save counts it.
  defp clamp_retain(value, %{max_count: max}, report, retain) when length(value) > max,
    do: {:cont, Enum.take(value, max), %{report | clamped: report.clamped + 1}, retain}

  defp clamp_retain(value, _f, report, retain), do: {:cont, value, report, retain}

  # one nested body read under its path: the entries failing to frame leave the
  # table's declared defaults and one malformed.
  defp read_body_retain(desc, body, nodes, ids, report, retain, path) do
    case entries(body, ids, desc.string_blob, :nested) do
      {:ok, fields, []} -> decode_retain(desc, fields, nodes, ids, retain, report, path)
      _ -> {%{}, %{report | malformed: true}, retain}
    end
  end

  # read_arm_retain is read_arm with the arm's body retaining: an unknown arm id
  # is an excluded class (§6.6), one retain_lost beside its unknown.
  defp read_arm_retain(union, ordinal, bin, nodes, ids, report, retain, path) do
    case leb(bin) do
      {0, _} ->
        {:cont, nil, report, retain}

      {aref, <<k, rest::binary>>} ->
        case resolve(ids, aref) do
          :error ->
            {:halt, nil, %{report | malformed: true}, retain}

          arm_id ->
            un = Map.fetch!(@unions, union)

            case Map.fetch(un.by_id, arm_id) do
              :error ->
                {:cont, nil,
                 %{report | unknown: report.unknown + 1, retain_lost: report.retain_lost + 1},
                 retain}

              {:ok, name} ->
                a = Map.fetch!(un.by_name, name)

                case leb(rest) do
                  {l, content} when byte_size(content) >= l and k == a.arm_kind ->
                    inner = binary_part(content, 0, l)
                    framed = binary_part(rest, 0, byte_size(rest) - byte_size(content) + l)
                    arm_path = path ++ [{ordinal, a.ordinal}]

                    read_arm_payload_retain(
                      a,
                      name,
                      inner,
                      framed,
                      {nodes, ids, report, retain},
                      arm_path
                    )

                  {l, content} when byte_size(content) >= l ->
                    {:cont, nil, %{report | kind_mismatch: report.kind_mismatch + 1}, retain}

                  _ ->
                    {:cont, nil, %{report | malformed: true}, retain}
                end
            end
        end

      _ ->
        {:cont, nil, %{report | malformed: true}, retain}
    end
  end

  defp read_arm_payload_retain(%{t: :table} = a, name, _inner, framed, c, arm_path) do
    {nodes, ids, report, retain} = c

    case leb(framed) do
      {_len, body} ->
        {value, report, retain} =
          read_body_retain(
            Map.fetch!(@tables, a.elem),
            body,
            nodes,
            ids,
            report,
            retain,
            arm_path
          )

        {:cont, {name, value}, report, retain}

      :error ->
        {:cont, {name, %{}}, %{report | malformed: true}, retain}
    end
  end

  defp read_arm_payload_retain(%{t: :pointer} = a, name, inner, _framed, c, arm_path) do
    {nodes, ids, report, retain} = c

    case read_payload_retain(a, inner, nodes, ids, report, retain, arm_path) do
      {:cont, v, report, retain} -> {:cont, {name, v}, report, retain}
      {:halt, v, report, retain} -> {:halt, {name, v}, report, retain}
    end
  end

  defp read_arm_payload_retain(a, name, inner, framed, c, _arm_path) do
    {nodes, ids, report, retain} = c

    case read_arm_payload(a, name, inner, framed, nodes, ids, report) do
      {:cont, v, report} -> {:cont, v, report, retain}
      {:halt, v, report} -> {:halt, v, report, retain}
    end
  end

  defp read_keyed_retain(_f, _bin, 0, _nodes, _ids, acc, report, retain, _path),
    do: {:cont, acc, report, retain}

  defp read_keyed_retain(f, bin, n, nodes, ids, acc, report, retain, path) do
    with {kref, r1} <- leb(bin),
         {l, r2} when byte_size(r2) >= l <- leb(r1) do
      elem = binary_part(r2, 0, l)
      rest = binary_part(r2, l, byte_size(r2) - l)
      item = Map.put(f.item, :elem_kind, f.elem_kind)

      key =
        case resolve(ids, kref) do
          :error -> :malformed
          kid -> Map.fetch(f.key_by_id, kid)
        end

      {acc, report, retain} =
        case key do
          {:ok, name} ->
            slot = Enum.find_index(f.key_names, &(&1 == name))
            retain = retain_reset(retain, path, f.ordinal, slot)

            {v, report, retain} =
              read_keyed_elem_retain(f, item, elem, nodes, ids, report, retain, path, slot)

            {Map.put(acc, name, v), report, retain}

          other ->
            # a slot this reader cannot name is an excluded class (§6.6): the
            # file's reader steps over its element by the element's length, so
            # nothing inside it is read, counted or retained
            report =
              if other == :malformed do
                {_v, report} = read_keyed_elem_plain(f, item, elem, nodes, ids, report)
                %{report | malformed: true}
              else
                %{report | unknown: report.unknown + 1, retain_lost: report.retain_lost + 1}
              end

            {acc, report, retain}
        end

      read_keyed_retain(f, rest, n - 1, nodes, ids, acc, report, retain, path)
    else
      _ -> {:cont, acc, %{report | malformed: true}, retain}
    end
  end

  defp read_keyed_elem_retain(f, item, elem, nodes, ids, report, retain, path, slot) do
    if f.elem_kind == 13 do
      read_element_retain(
        Map.fetch!(@tables, item.elem),
        elem,
        nodes,
        ids,
        report,
        retain,
        path,
        f.ordinal,
        slot
      )
    else
      {v, report} = read_keyed_elem_plain(f, item, elem, nodes, ids, report)
      {v, report, retain}
    end
  end

  defp read_keyed_elem_plain(f, item, elem, nodes, ids, report) do
    if f.elem_kind == 13 do
      {v, elem_report} = read_element(Map.fetch!(@tables, item.elem), elem, nodes, ids)
      {v, merge_report(report, elem_report)}
    else
      case read_list_elements(item, elem, 1, nodes, ids, [], report) do
        {[v], report} -> {v, report}
        {_, report} -> {item.default, report}
      end
    end
  end

  # the elements of an array of unions, and of pointers
  defp read_pointers_retain(_rest, 0, _nodes, _ids, acc, report, retain),
    do: {Enum.reverse(acc), report, retain}

  defp read_pointers_retain(rest, n, nodes, ids, acc, report, retain) do
    case leb(rest) do
      {0, after_sub} ->
        read_pointers_retain(after_sub, n - 1, nodes, ids, [nil | acc], report, retain)

      {idx, after_sub} ->
        {value, report, retain} =
          case node_value_retain(nodes, idx, ids, report, retain) do
            {{:value, v}, report, retain} when is_map(v) -> {v, report, retain}
            {_, report, retain} -> {nil, report, retain}
          end

        read_pointers_retain(after_sub, n - 1, nodes, ids, [value | acc], report, retain)

      :error ->
        {Enum.reverse(acc), %{report | malformed: true}, retain}
    end
  end

  # node_value_retain is node_value with the node's body retaining, once: the
  # first pointer to name a node reads it under the path {:node, k} (§6.6, the
  # path's first step) and every later one answers the same value. The value
  # carries k, so a save finds the node's records after the caller edits it.
  defp node_value_retain(nodes, idx, ids, report, retain) do
    at = idx - 2

    if at < 0 or at >= length(nodes) do
      {:out_of_range, report, retain}
    else
      case Enum.at(nodes, at) do
        {:blob, bytes} -> {{:value, bytes}, report, retain}
        {:table, name, body} -> node_table_retain(nodes, name, body, ids, idx, report, retain)
        _ -> {:null, report, retain}
      end
    end
  end

  defp node_table_retain(nodes, name, body, ids, idx, report, retain) do
    case retain.memo do
      %{^idx => :pending} ->
        {:null, report, retain}

      %{^idx => value} ->
        {{:value, value}, report, retain}

      memo ->
        desc = Map.fetch!(@tables, name)

        case entries(body, ids, desc.string_blob, :nested) do
          {:ok, fields, []} ->
            retain = %{retain | memo: Map.put(memo, idx, :pending)}

            {value, report, retain} =
              in_read_holder(idx, fn ->
                decode_retain(desc, fields, nodes, ids, retain, report, [{:node, idx}])
              end)

            value = Map.put(value, :__retain_node__, idx)
            {{:value, value}, report, %{retain | memo: Map.put(retain.memo, idx, value)}}

          _ ->
            {:null, report, retain}
        end
    end
  end

  defp read_elements_retain(_desc, _rest, 0, _nodes, _ids, acc, report, retain, _ordinal, _path),
    do: {Enum.reverse(acc), report, retain}

  defp read_elements_retain(desc, rest, n, nodes, ids, acc, report, retain, ordinal, path) do
    case leb(rest) do
      {len, body} when byte_size(body) >= len ->
        sub = binary_part(body, 0, len)
        after_sub = binary_part(body, len, byte_size(body) - len)

        {value, report, retain} =
          read_element_retain(desc, sub, nodes, ids, report, retain, path, ordinal, length(acc))

        read_elements_retain(
          desc,
          after_sub,
          n - 1,
          nodes,
          ids,
          [value | acc],
          report,
          retain,
          ordinal,
          path
        )

      _ ->
        {Enum.reverse(acc), %{report | malformed: true}, retain}
    end
  end

  defp read_element_retain(desc, sub, nodes, ids, report, retain, path, ordinal, index) do
    elem_path = path ++ [{ordinal, index}]

    if ends_early?(sub, ids) do
      {%{}, %{report | malformed: true}, retain}
    else
      case entries(sub, ids, false, :nested) do
        {:ok, fields, []} ->
          decode_retain(desc, fields, nodes, ids, retain, report, elem_path)

        {:stop, fields, [], 0} ->
          {value, report, retain} =
            decode_retain(desc, fields, nodes, ids, retain, report, elem_path)

          {value, %{report | malformed: true}, retain}

        {:stop, fields, [], _remaining} ->
          {_value, report, retain} =
            decode_retain(desc, fields, nodes, ids, retain, report, elem_path)

          {%{}, %{report | malformed: true}, retain}

        _ ->
          {%{}, %{report | malformed: true}, retain}
      end
    end
  end

  # ---- the resolving walk, capture side (docs/SPEC-TABLES.md §6.6) ----

  defp retain_room(retain, path, resolved) do
    retain.used + 9 + 8 * length(path) + byte_size(resolved) <= retain.cap
  end

  defp retain_cost(path, resolved), do: 9 + 8 * length(path) + byte_size(resolved)

  # the records are kept newest first while a load runs, and in the order
  # retained once it ends
  defp retain_put(retain, path, id, kind, resolved) do
    %{
      retain
      | records: [{path, id, kind, resolved} | retain.records],
        used: retain.used + retain_cost(path, resolved)
    }
  end

  # retain_reset discards what an earlier occurrence of a body retained, before
  # a later legal occurrence is read (§6.6, record lifetime): the records whose
  # path runs through the field's step, at one element index or at any. It
  # moves neither counter, and the capacity comes back.
  defp retain_reset(%{records: []} = retain, _path, _ordinal, _index), do: retain

  defp retain_reset(retain, path, ordinal, index) do
    n = length(path)

    {kept, used} =
      Enum.reduce(retain.records, {[], retain.used}, fn {p, _id, _kind, resolved} = rec,
                                                        {kept, used} ->
        case Enum.split(p, n) do
          {^path, [{^ordinal, i} | _]} when index == nil or i == index ->
            {kept, used - retain_cost(p, resolved)}

          _ ->
            {[rec | kept], used}
        end
      end)

    %{retain | records: Enum.reverse(kept), used: used}
  end

  # retain_remap moves a map's records from the entry's place in the wire to
  # its slot in ascending key order, and discards the records of an entry a
  # later entry with the same key replaced.
  defp retain_remap(%{records: []} = retain, _path, _ordinal, _slots), do: retain

  defp retain_remap(retain, path, ordinal, slots) do
    n = length(path)

    {kept, used} =
      Enum.reduce(retain.records, {[], retain.used}, fn {p, id, kind, resolved} = rec,
                                                        {kept, used} ->
        case Enum.split(p, n) do
          {^path, [{^ordinal, i} | tail]} ->
            case slots do
              %{^i => slot} ->
                {[{path ++ [{ordinal, slot} | tail], id, kind, resolved} | kept], used}

              _ ->
                {kept, used - retain_cost(p, resolved)}
            end

          _ ->
            {[rec | kept], used}
        end
      end)

    %{retain | records: Enum.reverse(kept), used: used}
  end

  defp retain_capture(ids, kind, payload) do
    case retain_resolve(ids, kind, payload, 0) do
      {:ok, resolved, <<>>} -> {:ok, resolved}
      _ -> :error
    end
  end

  defp retain_id(ids, r) do
    if r == 0 or r > div(byte_size(ids), 8) do
      :error
    else
      id = :binary.decode_unsigned(binary_part(ids, (r - 1) * 8, 8), :little)

      if id == 0 or id == @node_id or id == @build_version_id or id == @message_vocab_id do
        :error
      else
        {:ok, id}
      end
    end
  end

  defp retain_skip(payload, kind) do
    n =
      case kind do
        k when k in [1, 2, 6, 20, 25] -> 1
        k when k in [3, 7, 21, 26] -> 2
        k when k in [4, 8, 10, 22, 27] -> 4
        k when k in [5, 9, 11, 23, 28] -> 8
        k when k in [18, 19, 24, 29] -> 16
        _ -> nil
      end

    cond do
      n != nil and byte_size(payload) >= n ->
        {:ok, n}

      kind in [12, 31, 32, 33] ->
        case leb(payload) do
          {len, rest} when byte_size(rest) >= len ->
            {:ok, byte_size(payload) - byte_size(rest) + len}

          _ ->
            :error
        end

      true ->
        :error
    end
  end

  defp retain_resolve(_ids, 17, _payload, _depth), do: :error

  defp retain_resolve(ids, 30, payload, _depth) do
    case leb(payload) do
      {0, rest} ->
        {:ok, <<0::little-64>>, rest}

      {r, rest} ->
        case retain_id(ids, r) do
          {:ok, id} -> {:ok, <<id::little-64>>, rest}
          :error -> :error
        end

      :error ->
        :error
    end
  end

  defp retain_resolve(ids, 15, payload, depth) do
    case leb(payload) do
      {0, rest} ->
        {:ok, <<0::little-64>>, rest}

      {r, rest} ->
        with {:ok, id} <- retain_id(ids, r),
             <<arm_kind, rest2::binary>> <- rest,
             {:ok, framed, rest3} <- retain_framed(ids, arm_kind, rest2, depth) do
          {:ok, <<id::little-64, arm_kind, framed::binary>>, rest3}
        else
          _ -> :error
        end

      :error ->
        :error
    end
  end

  defp retain_resolve(ids, kind, payload, depth) when kind in [13, 14, 16] do
    retain_framed(ids, kind, payload, depth)
  end

  defp retain_resolve(_ids, kind, payload, _depth) do
    case retain_skip(payload, kind) do
      {:ok, n} ->
        {:ok, binary_part(payload, 0, n), binary_part(payload, n, byte_size(payload) - n)}

      :error ->
        :error
    end
  end

  defp retain_framed(ids, kind, payload, depth) do
    case leb(payload) do
      {n, rest} when byte_size(rest) >= n ->
        sub = binary_part(rest, 0, n)
        after_sub = binary_part(rest, n, byte_size(rest) - n)

        case retain_content(ids, kind, sub, depth + 1) do
          {:ok, content} -> {:ok, <<byte_size(content)::little-64, content::binary>>, after_sub}
          :error -> :error
        end

      _ ->
        :error
    end
  end

  defp retain_content(_ids, _kind, _sub, depth) when depth > 64, do: :error
  defp retain_content(_ids, 17, _sub, _depth), do: :error
  defp retain_content(ids, 13, sub, depth), do: retain_body(ids, sub, depth)

  defp retain_content(ids, kind, sub, depth) when kind in [14, 16] do
    case sub do
      <<ek, rest::binary>> ->
        if ek == 17 do
          :error
        else
          case leb(rest) do
            {count, elems} ->
              retain_array(ids, kind, ek, count, elems, depth, <<ek, count::little-64>>)

            :error ->
              :error
          end
        end

      _ ->
        :error
    end
  end

  defp retain_content(ids, kind, sub, depth) when kind in [15, 30] do
    case retain_resolve(ids, kind, sub, depth) do
      {:ok, resolved, <<>>} -> {:ok, resolved}
      _ -> :error
    end
  end

  defp retain_content(_ids, _kind, sub, _depth), do: {:ok, sub}

  defp retain_body(ids, sub, depth), do: retain_body_fields(ids, sub, depth, <<>>)

  defp retain_body_fields(ids, sub, depth, acc) do
    case leb(sub) do
      {0, _} ->
        {:ok, acc <> <<0::little-64>>}

      {ref, rest} ->
        with {:ok, id} <- retain_id(ids, ref),
             <<kind, rest2::binary>> <- rest,
             {:ok, resolved, rest3} <- retain_resolve(ids, kind, rest2, depth) do
          retain_body_fields(ids, rest3, depth, acc <> <<id::little-64, kind, resolved::binary>>)
        else
          _ -> :error
        end

      :error ->
        :error
    end
  end

  defp retain_array(_ids, _kind, _ek, 0, _elems, _depth, acc), do: {:ok, acc}

  defp retain_array(ids, kind, ek, count, elems, depth, acc) do
    if kind == 16 do
      case leb(elems) do
        {r, rest} ->
          with {:ok, id} <- retain_id(ids, r),
               {:ok, framed, rest2} <- retain_framed(ids, ek, rest, depth) do
            retain_array(
              ids,
              kind,
              ek,
              count - 1,
              rest2,
              depth,
              acc <> <<id::little-64, framed::binary>>
            )
          else
            _ -> :error
          end

        :error ->
          :error
      end
    else
      case retain_resolve(ids, ek, elems, depth) do
        {:ok, resolved, rest} ->
          retain_array(ids, kind, ek, count - 1, rest, depth, acc <> resolved)

        :error ->
          :error
      end
    end
  end

  # ---- the resolving walk, emit side (docs/SPEC-TABLES.md §6.6) ----

  defp retain_ref(ids, order, retain, id) do
    case ids do
      %{^id => ref} ->
        {ref, ids, order, retain}

      _ ->
        if MapSet.member?(@known_ids, id) or id in retain.ids do
          ref = length(order) + 1
          {ref, Map.put(ids, id, ref), [id | order], retain}
        else
          if length(retain.ids) >= retain.id_cap do
            {0, ids, order, retain}
          else
            ref = length(order) + 1
            {ref, Map.put(ids, id, ref), [id | order], %{retain | ids: [id | retain.ids]}}
          end
        end
    end
  end

  defp retain_emit(_ids, _order, _retain, 17, _resolved), do: :error

  defp retain_emit(ids, order, retain, 30, resolved) do
    case resolved do
      <<0::little-64, rest::binary>> ->
        {:ok, <<0>>, rest, ids, order, retain}

      <<id::little-64, rest::binary>> ->
        {ref, ids, order, retain} = retain_ref(ids, order, retain, id)
        if ref == 0, do: :error, else: {:ok, leb_encode(ref), rest, ids, order, retain}

      _ ->
        :error
    end
  end

  defp retain_emit(ids, order, retain, 15, resolved) do
    case resolved do
      <<0::little-64, rest::binary>> ->
        {:ok, <<0>>, rest, ids, order, retain}

      <<id::little-64, arm_kind, rest::binary>> ->
        {ref, ids, order, retain} = retain_ref(ids, order, retain, id)

        if ref == 0 do
          :error
        else
          case retain_emit_framed(ids, order, retain, arm_kind, rest) do
            {:ok, framed, rest2, ids, order, retain} ->
              {:ok, leb_encode(ref) <> <<arm_kind>> <> framed, rest2, ids, order, retain}

            :error ->
              :error
          end
        end

      _ ->
        :error
    end
  end

  defp retain_emit(ids, order, retain, kind, resolved) when kind in [13, 14, 16] do
    retain_emit_framed(ids, order, retain, kind, resolved)
  end

  defp retain_emit(ids, order, retain, kind, resolved) do
    case retain_skip(resolved, kind) do
      {:ok, n} ->
        {:ok, binary_part(resolved, 0, n), binary_part(resolved, n, byte_size(resolved) - n), ids,
         order, retain}

      :error ->
        :error
    end
  end

  defp retain_emit_framed(ids, order, retain, kind, resolved) do
    case resolved do
      <<len::little-64, sub::binary>> when byte_size(sub) >= len ->
        content = binary_part(sub, 0, len)
        rest = binary_part(sub, len, byte_size(sub) - len)

        case retain_emit_content(ids, order, retain, kind, content) do
          {:ok, emitted, ids, order, retain} ->
            {:ok, leb_encode(byte_size(emitted)) <> emitted, rest, ids, order, retain}

          :error ->
            :error
        end

      _ ->
        :error
    end
  end

  defp retain_emit_content(_ids, _order, _retain, 17, _sub), do: :error

  defp retain_emit_content(ids, order, retain, 13, sub),
    do: retain_emit_body(ids, order, retain, sub)

  defp retain_emit_content(ids, order, retain, kind, sub) when kind in [14, 16] do
    case sub do
      <<ek, count::little-64, elems::binary>> ->
        case retain_emit_array(
               ids,
               order,
               retain,
               kind,
               ek,
               count,
               elems,
               <<ek, leb_encode(count)::binary>>
             ) do
          {:ok, emitted, ids, order, retain} -> {:ok, emitted, ids, order, retain}
          :error -> :error
        end

      _ ->
        :error
    end
  end

  defp retain_emit_content(ids, order, retain, kind, sub) when kind in [15, 30] do
    case retain_emit(ids, order, retain, kind, sub) do
      {:ok, emitted, <<>>, ids, order, retain} -> {:ok, emitted, ids, order, retain}
      _ -> :error
    end
  end

  defp retain_emit_content(ids, order, retain, _kind, sub), do: {:ok, sub, ids, order, retain}

  defp retain_emit_body(ids, order, retain, <<0::little-64>>),
    do: {:ok, <<0>>, ids, order, retain}

  defp retain_emit_body(ids, order, retain, <<id::little-64, kind, rest::binary>>) do
    {ref, ids, order, retain} = retain_ref(ids, order, retain, id)

    if ref == 0 do
      :error
    else
      case retain_emit(ids, order, retain, kind, rest) do
        {:ok, emitted, rest2, ids, order, retain} ->
          case retain_emit_body(ids, order, retain, rest2) do
            {:ok, more, ids, order, retain} ->
              {:ok, leb_encode(ref) <> <<kind>> <> emitted <> more, ids, order, retain}

            :error ->
              :error
          end

        :error ->
          :error
      end
    end
  end

  defp retain_emit_body(_ids, _order, _retain, _sub), do: :error

  defp retain_emit_array(ids, order, retain, _kind, _ek, 0, _elems, acc),
    do: {:ok, acc, ids, order, retain}

  defp retain_emit_array(ids, order, retain, kind, ek, count, elems, acc) do
    if kind == 16 do
      case elems do
        <<key_id::little-64, rest::binary>> ->
          {ref, ids, order, retain} = retain_ref(ids, order, retain, key_id)

          if ref == 0 do
            :error
          else
            case retain_emit_framed(ids, order, retain, ek, rest) do
              {:ok, framed, rest2, ids, order, retain} ->
                retain_emit_array(
                  ids,
                  order,
                  retain,
                  kind,
                  ek,
                  count - 1,
                  rest2,
                  acc <> leb_encode(ref) <> framed
                )

              :error ->
                :error
            end
          end

        _ ->
          :error
      end
    else
      case retain_emit(ids, order, retain, ek, elems) do
        {:ok, emitted, rest, ids, order, retain} ->
          retain_emit_array(ids, order, retain, kind, ek, count - 1, rest, acc <> emitted)

        :error ->
          :error
      end
    end
  end
`

// enumRef returns the enum a field's values come from, or nil.
func enumRef(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

// unionRef is the union a field names, or nil (docs/SPEC-TABLES.md §2.6).
func unionRef(f *ir.Field) *ir.Union {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	un, _ := f.Type.Ref.(*ir.Union)
	return un
}

// flagsRef is the flags declaration a field names, or nil.
func flagsRef(f *ir.Field) *ir.Flags {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	fl, _ := f.Type.Ref.(*ir.Flags)
	return fl
}

// armDescriptor spells one union arm as the field line it is
// (docs/SPEC-TABLES.md §2.6, §3): the arm's own name and json key, its wire
// id from the arm's WireName (so a `was` on the arm keeps its identity, §5),
// and arm_kind, the kind byte of its arm header. A payload-free arm is kind 32
// with L = 0.
func armDescriptor(u *ir.Unit, owner string, arm ir.UnionVariant, ordinal int) string {
	id := ir.TableWireId(arm.WireName())
	if arm.Void() {
		// ordinal is the arm's declaration position; its cooked tag is that
		// plus one, tag 0 being None (docs/SPEC-TABLES.md §7.2)
		return fmt.Sprintf("%%{name: %q, json: %q, id: 0x%016X, kind: 32, ordinal: %d, arm_kind: 32, t: :void, default: nil},\n", arm.Name, arm.Name, id, ordinal)
	}
	f := *arm.F
	f.Name = arm.Name
	f.WasName = ""
	f.JsonKey = ""
	d := strings.TrimSuffix(fieldDescriptor(u, owner, &f, ordinal, nil), "},\n")
	// the arm header's kind byte is the arm's announced entry kind
	// (ir.TableArmEntry, docs/SPEC-TABLES.md §2.6, §3), the one answer the
	// message vocabulary already gives
	return fmt.Sprintf("%s, arm_id: 0x%016X, arm_kind: %d},\n", d, id, ir.TableArmEntry(arm).Kind)
}

// closureStruct is a closure member whether it was declared `table` or
// `type` (docs/SPEC-TABLES.md §3.1). A by-value `type` is a nested body the
// same way a nested table is, so its descriptor has to be in @tables or a
// load of the owner raises on the missing name.
func closureStruct(u *ir.Unit, name string) *ir.Struct {
	if st := u.Tables[name]; st != nil {
		return st
	}
	return u.Structs[name]
}

// tableDescriptor spells one table's fields as data the engine walks
// (docs/SPEC-TABLES.md §3, §8.1): its name, doc comment and tags, each
// field's effective id, its wire kind, and the facts a codec needs — a string's bound, an int's clamp range, an enum's
// variant names, a list's element table, a *string/*bytes blob's type id, a
// pointer's target. The descriptor covers the closure, fixed tables included,
// because a pointer can name one.
func tableDescriptor(u *ir.Unit, st *ir.Struct) string {
	var b strings.Builder
	ml := ir.RecordLayout(u, st)
	bytesBlob, str := ir.PointerReachableBlobs(st)
	// the node records a load of this root can place (docs/SPEC-TABLES.md §3.1,
	// §6.5): every table a pointer below it names, by wire type id, so the
	// region measure sizes exactly the records this root reads
	var nodes strings.Builder
	nodes.WriteString("%{")
	for _, ref := range ir.PointerReachable(st) {
		fmt.Fprintf(&nodes, "0x%016X => %q, ", ir.TableWireId(ref.WireName()), ref.Name)
	}
	nodes.WriteString("}")
	// variable is this table's own class (docs/SPEC-TABLES.md §3.1). A variable
	// root carries the node-table id as transport. A fixed root cannot name
	// that id, so a body that brings one counts it unknown, once (§4).
	fmt.Fprintf(&b, "    %q => %%{name: %q, size: %d, align: %d, type_id: 0x%016X, record: %d, has_extent: %v, string_blob: %v, bytes_blob: %v, nodes: %s, variable: %v, doc: %s, tags: %s, fields: [\n",
		st.Name, st.Name, ml.Size, ml.Align, ir.TableTypeId(st.WireName()), cookRecordBytes(u, st), cookHasExtent(u, st), str, bytesBlob, nodes.String(), ir.VariableTables(u)[st.Name], elixirQuote(st.Doc), elixirTags(st.Tags))
	for i, f := range st.Fields {
		b.WriteString("      " + fieldDescriptor(u, st.DeclaredName(), f, i, ml.FieldByName(f.Name)))
	}
	b.WriteString("    ]},\n")
	return b.String()
}

// fieldDescriptor spells one field's descriptor: its §8.1 facts (type name,
// wire id, kind, element bound) and its doc comment and tags (§8.1), beside
// what the codecs read, including the §16.2 clamp range the text reader
// applies (docs/SPEC-TABLES.md §8.1, §16.2).
//
// owner is the table, type or union that declares the field. A float field,
// or an array of floats, carries `non_finite: "<owner>.<field>"`, the name a
// read's non_finite_float refusal gives it (docs/SPEC-TABLES.md §3.3).
func fieldDescriptor(u *ir.Unit, owner string, f *ir.Field, ordinal int, fl *ir.FieldLayout) string {
	var b strings.Builder
	id := ir.TableFieldWireId(f)
	kind := ir.TableWireFieldKind(f)
	fmt.Fprintf(&b, "%%{name: %q, json: %q, type_name: %s, id: 0x%016X, kind: %d, ordinal: %d", f.Name, ir.TableFieldJsonKey(f), elixirQuote(viewTableTypeSpelling(f)), id, kind, ordinal)
	switch {
	case f.IsMap():
		// SPEC-TABLES §2.8, §3: a map is kind 14 over framed table
		// entries. Its key kind determines the canonical ascending order.
		keyKind := 0
		if len(f.MapEntry.Fields) == 2 {
			keyKind = ir.TableWireScalarKind(ir.MapKeyField(f))
		}
		fmt.Fprintf(&b, ", t: :map, elem: %q, elem_kind: 13, key_kind: %d", f.MapEntry.Name, keyKind)
	case f.Type.Pointer && f.Type.Blob():
		blob := "bytes"
		if f.Type.Kind == ir.TString {
			blob = "string"
		}
		fmt.Fprintf(&b, ", t: :blob, blob: :%s, type_id: 0x%016X", blob, ir.BlobWireTypeId(f))
	case f.KeyEnum != "":
		// SPEC-TABLES §2.4, §3.2: an enum-keyed array is kind 16, one
		// (key, L, element) pair per slot off its default, slots in the key
		// enum's value order; the text is an object keyed by variant name.
		elem := *f
		elem.Array = ir.ArrayNone
		elem.ArrayBound = 0
		elem.KeyEnum = ""
		elem.KeyEnumRef = nil
		elemKind := ir.TableWireElemKind(f)
		fmt.Fprintf(&b, ", t: :keyed, elem_kind: %d, key_names: [", elemKind)
		for i := 0; i < int(f.ArrayBound) && i < len(f.KeyEnumRef.Variants); i++ {
			fmt.Fprintf(&b, "%q, ", f.KeyEnumRef.Variants[i])
		}
		b.WriteString("], key_ids: %{")
		for i := 0; i < int(f.ArrayBound) && i < len(f.KeyEnumRef.Variants); i++ {
			fmt.Fprintf(&b, "%q => 0x%016X, ", f.KeyEnumRef.Variants[i], ir.TableWireId(f.KeyEnumRef.VariantWireName(i)))
		}
		b.WriteString("}, key_by_id: %{")
		for i := 0; i < int(f.ArrayBound) && i < len(f.KeyEnumRef.Variants); i++ {
			fmt.Fprintf(&b, "0x%016X => %q, ", ir.TableWireId(f.KeyEnumRef.VariantWireName(i)), f.KeyEnumRef.Variants[i])
		}
		b.WriteString("}")
		elemDesc := strings.TrimSuffix(fieldDescriptor(u, owner, &elem, ordinal, nil), "},\n")
		fmt.Fprintf(&b, ", item: %s}", elemDesc)
	case f.Array != ir.ArrayNone:
		elemKind := ir.TableWireElemKind(f)
		fmt.Fprintf(&b, ", t: :list, elem_kind: %d", elemKind)
		if f.Type.Kind == ir.TFixed {
			fmt.Fprintf(&b, ", elem_frac: %d", f.Type.FracBits)
		}
		if f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits || f.Type.Kind == ir.TFixed {
			// §16.2: the declared range holds every element
			if lo, hi, ok := ir.TableRawRange(f); ok {
				fmt.Fprintf(&b, ", elem_min: %s, elem_max: %s", lo.String(), hi.String())
			}
		}
		if fl := flagsRef(f); fl != nil {
			// an array of flags: each element's text is its set names
			b.WriteString(", elem_flags: [")
			for _, v := range fl.Variants {
				fmt.Fprintf(&b, "%q, ", v)
			}
			b.WriteString("]")
		}
		if f.Array != ir.ArrayList {
			fmt.Fprintf(&b, ", array_bound: %d", f.ArrayBound)
		}
		if f.Array == ir.ArrayFixed {
			// SPEC-TABLES §3: a [N]T writes all N elements; a table element
			// array is never elided, any other is elided when every element
			// sits at the declared default.
			elem := *f
			elem.Array = ir.ArrayNone
			elem.ArrayBound = 0
			elem.Type.Optional = false
			fmt.Fprintf(&b, ", fixed_array: %d, elem_default: %s", f.ArrayBound, variableFieldDefault(&elem))
		}
		switch elemKind {
		case ir.TableKindTable:
			fmt.Fprintf(&b, ", elem: %q", f.Type.Name)
		case ir.TableKindPointer:
			ref := f.Type.Ref.(*ir.Struct)
			fmt.Fprintf(&b, ", elem: %q, elem_type_id: 0x%016X", ref.Name, ir.TableWireId(ref.WireName()))
		case ir.TableKindEnum:
			writeEnumMaps(&b, enumRef(f))
		case ir.TableKindUnion:
			// SPEC-TABLES §2.6, §3: each element is one arm header, or a
			// zero reference for None.
			fmt.Fprintf(&b, ", elem: %q", f.Type.Name)
		case ir.TableKindString:
			fmt.Fprintf(&b, ", size: %d", f.Type.Size)
		}
		elemSize, elemAlign := ir.ListElementLayout(u, f)
		fmt.Fprintf(&b, ", elem_size: %d, elem_align: %d", elemSize, elemAlign)
	case f.Type.Pointer && isStruct(f):
		ref := f.Type.Ref.(*ir.Struct)
		fmt.Fprintf(&b, ", t: :pointer, elem: %q, type_id: 0x%016X", ref.Name, ir.TableWireId(ref.WireName()))
	case isStruct(f):
		fmt.Fprintf(&b, ", t: :table, elem: %q", f.Type.Name)
	case unionRef(f) != nil:
		fmt.Fprintf(&b, ", t: :union, elem: %q", f.Type.Name)
	case f.Type.Kind == ir.TBytes:
		fmt.Fprintf(&b, ", t: :bytes, size: %d", f.Type.Size)
	case enumRef(f) != nil:
		b.WriteString(", t: :enum")
		writeEnumMaps(&b, enumRef(f))
	case flagsRef(f) != nil:
		// a flags mask rides as its unsigned storage kind; the text is the
		// list of set variant names (docs/SPEC-TABLES.md §3, §16.2).
		fmt.Fprintf(&b, ", t: :int, width: %d, signed: false, flags: [", ir.TableKindWidth(ir.TableScalarKind(f)))
		for _, v := range flagsRef(f).Variants {
			fmt.Fprintf(&b, "%q, ", v)
		}
		b.WriteString("]")
	case f.Type.Kind == ir.TString:
		b.WriteString(", t: :string")
		fmt.Fprintf(&b, ", size: %d", f.Type.Size)
	case f.Type.Kind == ir.TWString:
		// wstring(N) is kind 33 on the variable wire: L counts UTF-16LE bytes,
		// two per code unit (docs/SPEC-TABLES.md §3). The value is UTF-8.
		fmt.Fprintf(&b, ", t: :wstring, size: %d", f.Type.Size)
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits || f.Type.Kind == ir.TFixed:
		b.WriteString(", t: :int")
		width := (f.Type.Width + 7) / 8
		if f.Type.Kind == ir.TFixed || f.Type.Kind == ir.TBits || f.Type.Width == 128 {
			// SPEC-TABLES §3, §4.12: the fixed-point and 128-bit kinds take
			// their storage width from the kind, never from the bit count.
			// So does bits(N): it rides as the narrowest unsigned kind that
			// holds it (4 bytes up to 32 bits, 8 above), so bits(24) and
			// bits(48) are 4 and 8 on the wire, not the 3 and 6 that
			// (N+7)/8 spells. int_bytes/int_of have no clause for those.
			width = ir.TableKindWidth(ir.TableScalarKind(f))
		}
		fmt.Fprintf(&b, ", width: %d, signed: %v", width, f.Type.Signed)
		if f.Type.Kind == ir.TFixed {
			// SPEC-TABLES §3 and §4.12: storage is the exact scaled integer;
			// text divides it by 2^F without passing through a float.
			fmt.Fprintf(&b, ", frac: %d", f.Type.FracBits)
		}
		if f.Type.Kind == ir.TBits {
			fmt.Fprintf(&b, ", bits: %d", f.Type.Width)
		}
		if lo, hi, ok := ir.TableRawRange(f); ok {
			fmt.Fprintf(&b, ", min: %s, max: %s", lo.String(), hi.String())
		}
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		// A float's payload width is its kind (docs/SPEC-TABLES.md §3).
		// FieldType.Width stays 0 for TFloat32 and TFloat64, so Width/8
		// would hand float_bytes/2 a 0 and the clause would not match.
		b.WriteString(", t: :float")
		fmt.Fprintf(&b, ", width: %d", ir.TableKindWidth(kind))
		if f.HasFloatRange {
			fmt.Fprintf(&b, ", min: %s, max: %s", fixedFloatLiteral(f.FMin), fixedFloatLiteral(f.FMax))
		}
	case f.Type.Kind == ir.TBool:
		b.WriteString(", t: :bool")
	}
	if (f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64) && !f.IsMap() {
		// the name a read's refusal of a NaN or an infinity gives this field
		// (docs/SPEC-TABLES.md §3.3): the field and the table, type or union
		// that declares it, as the reference leg's floatOwner spells it
		fmt.Fprintf(&b, ", non_finite: %q", owner+"."+f.Name)
	}
	if f.Type.Optional {
		// presence is the field, not a kind (docs/SPEC-TABLES.md §2.3): a present
		// optional writes its frame even when the body is all defaults.
		b.WriteString(", optional: true")
	}
	writeCookLayout(&b, u, f, fl)
	fmt.Fprintf(&b, ", shape: %s, bound: %s, doc: %s, tags: %s", cardShape(f), cardBound(f), elixirQuote(f.Doc), elixirTags(f.Tags))
	fmt.Fprintf(&b, ", default: %s", variableFieldDefault(f))
	b.WriteString("},\n")
	return b.String()
}

// variableFieldDefault is a field's declared default as an Elixir literal, the
// value an absent field reads and a text renders (§4, §16.1): the declared
// default overrides the type's zero, exactly as the reference's prefill does.
func variableFieldDefault(f *ir.Field) string {
	if f.Type.Optional {
		// SPEC-TABLES §2.3: a missing optional is absent, even when its
		// underlying value has a nonzero declared default.
		return "nil"
	}
	switch {
	case f.KeyEnum != "":
		return "%{}"
	case f.Array != ir.ArrayNone:
		return "[]"
	case f.IsMap():
		return "%{}"
	case f.Type.Kind == ir.TWString:
		return "\"\""
	case flagsRef(f) != nil:
		if f.HasDefault && f.DefInt != nil {
			return fixedIntLiteral(f.DefInt)
		}
		return "0"
	case f.Type.Pointer && f.Type.Blob():
		return "nil"
	case f.Type.Pointer && isStruct(f):
		return "nil"
	case isStruct(f):
		return "%{}"
	case f.Type.Kind == ir.TBytes:
		if len(f.DefBytes) == 0 {
			return "<<>>"
		}
		return fixedBinaryLiteral(f.DefBytes, 0)
	case f.Type.Kind == ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		if f.HasDefault {
			return fixedFloatLiteral(f.DefFloat)
		}
		return "0.0"
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits || f.Type.Kind == ir.TFixed:
		if f.HasDefault && f.DefInt != nil {
			return fixedIntLiteral(f.DefInt)
		}
		return "0"
	case f.Type.Kind == ir.TString:
		if len(f.DefBytes) == 0 {
			return "<<>>"
		}
		return fixedBinaryLiteral(f.DefBytes, 0)
	case enumRef(f) != nil:
		if f.HasDefault && f.DefVariant != "" {
			return fmt.Sprintf("%q", f.DefVariant)
		}
		return "nil"
	}
	return "nil"
}

// writeEnumMaps spells an enum's two name↔id maps (docs/SPEC-TABLES.md §5, §3).
func writeEnumMaps(b *strings.Builder, e *ir.Enum) {
	if e == nil {
		return
	}
	b.WriteString(", by_name: %{")
	for i, v := range e.Variants {
		fmt.Fprintf(b, "%q => 0x%016X, ", v, ir.TableWireId(e.VariantWireName(i)))
	}
	b.WriteString("}, by_id: %{")
	for i, v := range e.Variants {
		fmt.Fprintf(b, "0x%016X => %q, ", ir.TableWireId(e.VariantWireName(i)), v)
	}
	b.WriteString("}")
}

// cardShape is the descriptor's shape column (docs/SPEC-TABLES.md §8.1):
// :value for a scalar or enum field, :list for an unbounded array, :blob for a
// *string/*bytes, :pointer for a pointer to a table, :table for a nested table.
// It is the one column a tool keys a walk on without naming a type.
func cardShape(f *ir.Field) string {
	switch {
	case f.Type.Pointer && f.Type.Blob():
		return ":blob"
	case f.IsList():
		return ":list"
	case f.Type.Pointer && isStruct(f):
		return ":pointer"
	case isStruct(f):
		return ":table"
	default:
		return ":value"
	}
}

// cardBound is the descriptor's declared-bound column (docs/SPEC-TABLES.md
// §8.1): a string's size, an int's clamp range as a {min, max} tuple, and nil
// for every field that declares no bound.
func cardBound(f *ir.Field) string {
	switch {
	case f.Type.Kind == ir.TString && !f.Type.Pointer:
		return fmt.Sprintf("%d", f.Type.Size)
	case f.Type.Kind == ir.TInt && f.HasIntRange && f.IntMin != nil && f.IntMax != nil:
		return "{" + f.IntMin.String() + ", " + f.IntMax.String() + "}"
	default:
		return "nil"
	}
}
