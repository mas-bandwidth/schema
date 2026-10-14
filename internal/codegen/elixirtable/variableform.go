// THE VARIABLE FORM for Elixir (docs/SPEC-TABLES.md §3): form byte 1, the
// id-table wire. It carries the card record's slice — a plain `table` root and
// tables as list elements; bool, int32, int64 and enum with their defaults and
// ranges; string(N); *string; and unbounded lists of tables []T (§2.9) — and,
// beyond that slice, pointers to tables and the flat node table (§2.1, §3.1),
// *bytes blobs (§2.5), the unsigned integer widths those values declare, and
// unbounded arrays of scalars (§2.9). A variable table beside a fixed one
// still gets this codec (§2.7). Constructs this backend still does not carry
// are refused by name at the boundary (§11) only when this form owns the unit.
//
// The wire is the same bytes the Go, C, C++ and C# backends write
// (docs/SPEC-TABLES.md §3, §3.1): the form byte, then a body of `(reference,
// kind, payload)` triples ended by the zero reference, then the id table — the
// interned ids in first-use order, then their count as one little-endian u64.
// A *string or a *bytes rides as a pointer index (kind 17) into the flat node
// table (§2.5), which the root body carries under the reserved node-table id
// at kind 12; a table pointer is the same index naming a table record (§2.1).
// A list of tables rides as an array (kind 14) of framed table bodies (element
// kind 13). A list of scalars is the same kind 14 with the elements packed at
// the element kind's width and no per-element length (§2.9). An enum rides
// under its own kind 30 carrying the reference to the variant name's id.
//
// Reading is wire tolerance (§4): an unknown id is skipped and counted, an
// absent field reads its default, a kind mismatch and an out-of-range value are
// reported. Identity is the name hash and `was` (§5). A BEAM term has no
// pointer identity, so a node's index is its position in the declaration-order
// walk and two slots share a record when they hold equal terms.
package elixirtable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// VariableTableModule is the variable form's codec module, one per unit that
// declares a variable table, claimed beside BuildVersion and the two
// accelerators' runtimes (docs/SPEC-TABLES.md §3, §11).
const VariableTableModule = "Table"

// generateVariableForm emits <ns>.Table: the id-table wire's load and save for
// every variable table the unit's closure holds, driven by the descriptors
// below and the shared engine in variableRuntimeSource. Nil when the unit
// declares no variable table.
func generateVariableForm(u *ir.Unit, ns string) map[string][]byte {
	variable := ir.VariableTables(u)
	if len(variable) == 0 {
		return nil
	}
	// Variable tables, plus every fixed table in the closure. A pointer or a
	// by-value nest names the fixed table's descriptor (docs/SPEC-TABLES.md
	// §2.1, §2.2, §3.1). A unit with no variable table never reaches here.
	names := make([]string, 0, len(variable))
	seen := map[string]bool{}
	for name := range variable {
		if u.Tables[name] != nil {
			names = append(names, name)
			seen[name] = true
		}
	}
	for name := range ir.TableClosure(u) {
		if seen[name] {
			continue
		}
		if u.Tables[name] != nil {
			names = append(names, name)
		}
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
	b.WriteString("  @known_ids MapSet.new([\n")
	for _, id := range ir.TableWireIds(u) {
		fmt.Fprintf(&b, "    0x%016X,\n", id)
	}
	b.WriteString("  ])\n\n")
	b.WriteString("  @tables %{\n")
	for _, name := range names {
		st := u.Tables[name]
		if st == nil {
			continue
		}
		b.WriteString(tableDescriptor(u, st))
	}
	b.WriteString("  }\n")
	// @table_types is the node record's type id to the table it places
	// (docs/SPEC-TABLES.md §3.1). The two reserved blob ids are not tables.
	b.WriteString("  @table_types %{\n")
	for _, name := range names {
		st := u.Tables[name]
		if st == nil {
			continue
		}
		fmt.Fprintf(&b, "    0x%016X => %q,\n", ir.TableWireId(st.WireName()), name)
	}
	b.WriteString("  }\n")
	b.WriteString("  # the unit registry (docs/SPEC-TABLES.md §8.3): the descriptors a tool\n")
	b.WriteString("  # walks a record by name and kind with, no per-version struct on hand.\n")
	b.WriteString("  def descriptors, do: @tables\n\n")
	b.WriteString(variableRuntimeSource)
	b.WriteString(variableRetainSource)
	b.WriteString("end\n")
	return map[string][]byte{VariableTableModule + ".ex": []byte(b.String())}
}

// variableRuntimeSource is the id-table wire engine, driven by @tables. It is
// the whole codec: save walks the value in declaration order interning ids in
// first-use order, and load walks the body tolerantly reporting every event
// §4 names.
const variableRuntimeSource = `  # ---- the write side (docs/SPEC-TABLES.md §3) ----

  # bits(N) is refused until it is carried. A missing t: used to raise
  # key :t not found on the walk (channel bits(6)). save and load answer
  # {:error, {:bits, name}} before that walk, including through a nested table.
  defp bits_refused(desc), do: bits_refused(desc, MapSet.new())

  defp bits_refused(desc, seen) do
    id = desc.type_id

    if MapSet.member?(seen, id) do
      nil
    else
      seen = MapSet.put(seen, id)

      Enum.find_value(desc.fields, fn f ->
        table = Map.get(f, :table)
        elem = Map.get(f, :elem)

        cond do
          Map.get(f, :t) == :bits ->
            f.name

          is_binary(table) and Map.has_key?(@tables, table) ->
            bits_refused(Map.fetch!(@tables, table), seen)

          is_binary(elem) and Map.has_key?(@tables, elem) ->
            bits_refused(Map.fetch!(@tables, elem), seen)

          true ->
            nil
        end
      end)
    end
  end

  def save(table, value) when is_map(value) do
    desc = Map.fetch!(@tables, table)

    case bits_refused(desc) do
      nil -> save_carried(table, value)
      name -> {:error, {:bits, name}}
    end
  end

  defp save_carried(table, value) do
    desc = Map.fetch!(@tables, table)

    # the node table's records in §3.1's first-visit order, one record per
    # DISTINCT blob: a blob two fields share is numbered once, so the saved
    # wire matches the reference backends byte for byte (§3.1)
    nodes = collect_nodes(desc, value, []) |> Enum.reverse()
    idx_map = nodes |> Enum.with_index(2) |> Map.new(fn {v, i} -> {v, i} end)

    {_idx_map, ids, order, body} = save_fields(desc, value, idx_map, %{}, [], [])

    {body, _ids, order} = save_node_table(nodes, idx_map, ids, order, body)

    ids_bytes = for id <- Enum.reverse(order), into: <<>>, do: <<id::little-64>>

    {:ok, IO.iodata_to_binary([<<1>>, body, <<0>>, ids_bytes, <<length(order)::little-64>>])}
  end

  # collect_nodes is the pre-order walk of every pointer edge this value
  # reaches (docs/SPEC-TABLES.md §2.1, §2.5, §3.1): the flat node table's
  # records. Root is index 1 and the first record is 2. The accumulator is
  # prepended and reversed once by the caller, so a node is numbered before
  # the edges inside it. A node reached twice is one record. A BEAM term has
  # no pointer identity, so equality of the term is the share.
  defp collect_nodes(desc, value, acc) do
    Enum.reduce(desc.fields, acc, fn f, acc ->
      case f.t do
        :blob ->
          case Map.get(value, f.name) do
            nil ->
              acc

            v ->
              if Enum.any?(acc, fn e -> e == v end), do: acc, else: [v | acc]
          end

        :bblob ->
          case Map.get(value, f.name) do
            nil ->
              acc

            v ->
              tagged = {:bblob, v}
              if Enum.any?(acc, fn e -> e == tagged end), do: acc, else: [tagged | acc]
          end

        :ptr ->
          case Map.get(value, f.name) do
            nil ->
              acc

            v ->
              tagged = {:table, f.table, v}

              if Enum.any?(acc, fn e -> e == tagged end) do
                acc
              else
                child = Map.fetch!(@tables, f.table)
                collect_nodes(child, v, [tagged | acc])
              end
          end

        # a by-value table is not a node. The pointer edges inside it are
        # (docs/SPEC-TABLES.md §2.1, §3).
        :nest ->
          case Map.get(value, f.name) do
            v when is_map(v) ->
              collect_nodes(Map.fetch!(@tables, f.table), v, acc)

            _ ->
              acc
          end

        :list ->
          # a scalar element carries no node. A pointer element is a node of
          # its own. A table element is by value and the edges are inside it
          # (docs/SPEC-TABLES.md §2.1, §2.9, §3.1).
          case f.elem_t do
            :tab ->
              Enum.reduce(Map.get(value, f.name, []), acc, fn e, acc ->
                collect_nodes(Map.fetch!(@tables, f.elem), e, acc)
              end)

            :ptr ->
              Enum.reduce(Map.get(value, f.name, []), acc, fn e, acc ->
                case e do
                  nil ->
                    acc

                  v ->
                    tagged = {:table, f.elem, v}

                    if Enum.any?(acc, fn n -> n == tagged end) do
                      acc
                    else
                      child = Map.fetch!(@tables, f.elem)
                      collect_nodes(child, v, [tagged | acc])
                    end
                end
              end)

            _ ->
              acc
          end

        _ ->
          acc
      end
    end)
  end

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
        idx = Map.fetch!(idx_map, v)
        {idx_map, ids, order, [acc, leb_encode(ref), <<f.kind>>, leb_encode(idx)]}
    end
  end

  # *bytes is the same kind-17 index as *string, naming a bytes record
  # (docs/SPEC-TABLES.md §2.5, §3.1). Null is elided.
  defp emit_field(%{t: :bblob} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name) do
      nil ->
        {idx_map, ids, order, acc}

      v ->
        {ref, ids, order} = intern(ids, order, f.id)
        idx = Map.fetch!(idx_map, {:bblob, v})
        {idx_map, ids, order, [acc, leb_encode(ref), <<f.kind>>, leb_encode(idx)]}
    end
  end

  # *T is a kind-17 node index (docs/SPEC-TABLES.md §2.1, §3.1). Null is elided.
  # A non-null pointer always rides, even when the node's body is all default.
  defp emit_field(%{t: :ptr} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name) do
      nil ->
        {idx_map, ids, order, acc}

      v ->
        {ref, ids, order} = intern(ids, order, f.id)
        idx = Map.fetch!(idx_map, {:table, f.table, v})
        {idx_map, ids, order, [acc, leb_encode(ref), <<f.kind>>, leb_encode(idx)]}
    end
  end

  # a scalar unbounded array is kind 14, the element kind, the count, then
  # the elements packed at their width — no per-element length
  # (docs/SPEC-TABLES.md §2.9, §3). An empty array elides. A zero element
  # still rides, because position is identity inside an array.
  defp emit_field(%{t: :list, elem_t: :int} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name, []) do
      [] ->
        {idx_map, ids, order, acc}

      elements ->
        {ref, ids, order} = intern(ids, order, f.id)
        packed = Enum.map(elements, fn v -> int_bytes(v, f.width, f.signed) end)
        array = [<<f.ek>>, leb_encode(length(elements)), packed]
        {idx_map, ids, order, [acc, leb_encode(ref), <<14>>, leb_encode(IO.iodata_length(array)), array]}
    end
  end

  # []*T is kind 14 over element kind 17: each slot is a node index, and null
  # rides as 0 in its slot (docs/SPEC-TABLES.md §2.1, §2.9, §3.1).
  defp emit_field(%{t: :list, elem_t: :ptr} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name, []) do
      [] ->
        {idx_map, ids, order, acc}

      elements ->
        {ref, ids, order} = intern(ids, order, f.id)

        packed =
          Enum.map(elements, fn e ->
            idx =
              case e do
                nil -> 0
                v -> Map.fetch!(idx_map, {:table, f.elem, v})
              end

            leb_encode(idx)
          end)

        array = [<<f.ek>>, leb_encode(length(elements)), packed]
        {idx_map, ids, order, [acc, leb_encode(ref), <<14>>, leb_encode(IO.iodata_length(array)), array]}
    end
  end

  defp emit_field(%{t: :list, elem_t: :tab} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name, []) do
      [] ->
        {idx_map, ids, order, acc}

      elements ->
        {ref, ids, order} = intern(ids, order, f.id)
        elem_desc = Map.fetch!(@tables, f.elem)
        {body, idx_map, ids, order} = save_elements(elem_desc, elements, idx_map, ids, order, [])
        array = [<<f.ek>>, leb_encode(length(elements)), body]
        {idx_map, ids, order, [acc, leb_encode(ref), <<14>>, leb_encode(IO.iodata_length(array)), array]}
    end
  end

  # an element class this codec does not carry yet elides only while empty
  # (docs/SPEC-TABLES.md §2.9). A present one is not encoded: writing nothing
  # would be a wrong answer (§11).
  defp emit_field(%{t: :list} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name, []) do
      [] ->
        {idx_map, ids, order, acc}

      _ ->
        raise "elixir variable form: #{f.name} element class is not carried"
    end
  end

  defp emit_field(%{t: :enum} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name) do
      nil ->
        {idx_map, ids, order, acc}

      variant ->
        vid = Map.fetch!(f.by_name, variant)
        {ref, ids, order} = intern(ids, order, f.id)
        {vref, ids, order} = intern(ids, order, vid)
        {idx_map, ids, order, [acc, leb_encode(ref), <<30>>, leb_encode(vref)]}
    end
  end

  # an int elides at its declared default, which is 0 when none is declared
  # (docs/SPEC-TABLES.md §3, §4). A stored 0 still rides when the default is
  # not 0.
  defp emit_field(%{t: :int} = f, value, idx_map, ids, order, acc) do
    default = Map.get(f, :default, 0)

    case Map.get(value, f.name, default) do
      v when v == default ->
        {idx_map, ids, order, acc}

      v ->
        {ref, ids, order} = intern(ids, order, f.id)
        {idx_map, ids, order, [acc, leb_encode(ref), <<f.kind>>, int_bytes(v, f.width, f.signed)]}
    end
  end

  # a by-value table is kind 13: the field id, then a framed body. A body that
  # is only the terminator elides, and the field id interned to find that out
  # is rolled back (docs/SPEC-TABLES.md §3).
  defp emit_field(%{t: :nest} = f, value, idx_map, ids, order, acc) do
    child = Map.fetch!(@tables, f.table)
    merged = Map.merge(defaults(child), Map.get(value, f.name) || %{})
    {ref, ids1, order1} = intern(ids, order, f.id)
    {idx_map, ids2, order2, body} = save_fields(child, merged, idx_map, ids1, order1, [])

    if IO.iodata_length(body) == 0 do
      {idx_map, ids, order, acc}
    else
      framed = [body, <<0>>]
      {idx_map, ids2, order2, [acc, leb_encode(ref), <<f.kind>>, leb_encode(IO.iodata_length(framed)), framed]}
    end
  end

  defp emit_field(%{t: :string} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name, "") do
      "" ->
        {idx_map, ids, order, acc}

      v ->
        {ref, ids, order} = intern(ids, order, f.id)
        {idx_map, ids, order, [acc, leb_encode(ref), <<f.kind>>, leb_encode(byte_size(v)), v]}
    end
  end

  defp emit_field(%{t: :bool} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name, false) do
      false ->
        {idx_map, ids, order, acc}

      _ ->
        {ref, ids, order} = intern(ids, order, f.id)
        {idx_map, ids, order, [acc, leb_encode(ref), <<1>>, <<1>>]}
    end
  end

  defp save_elements(_desc, [], idx_map, ids, order, acc), do: {acc, idx_map, ids, order}

  defp save_elements(desc, [e | rest], idx_map, ids, order, acc) do
    {idx_map, ids, order, body} = save_fields(desc, e, idx_map, ids, order, [])
    framed = [leb_encode(IO.iodata_length(body) + 1), body, <<0>>]
    save_elements(desc, rest, idx_map, ids, order, [acc, framed])
  end

  # save_node_table writes the reserved node-table field once, when the value
  # reaches a pointer (docs/SPEC-TABLES.md §3.1). Records follow the numbering.
  defp save_node_table([], _idx_map, ids, order, body), do: {body, ids, order}

  defp save_node_table(nodes, idx_map, ids, order, body) do
    {ref, ids, order} = intern(ids, order, @node_id)

    {recs, ids, order} =
      Enum.reduce(nodes, {[], ids, order}, fn node, {recs, ids, order} ->
        {rec, ids, order} = write_node(node, idx_map, ids, order)
        {[rec | recs], ids, order}
      end)

    table = Enum.reverse(recs)
    payload = [leb_encode(length(nodes)), table]
    {[body, leb_encode(ref), <<12>>, leb_encode(IO.iodata_length(payload)), payload], ids, order}
  end

  # write_node is one record (docs/SPEC-TABLES.md §2.5, §3.1): a *string or a
  # *bytes is its bytes under the reserved type id, with no fields and no
  # terminator; a table is a framed body ending in the zero reference.
  defp write_node(v, _idx_map, ids, order) when is_binary(v) do
    {sref, ids, order} = intern(ids, order, @string_id)
    {[leb_encode(sref), leb_encode(byte_size(v)), v], ids, order}
  end

  defp write_node({:bblob, v}, _idx_map, ids, order) do
    {bref, ids, order} = intern(ids, order, @bytes_id)
    {[leb_encode(bref), leb_encode(byte_size(v)), v], ids, order}
  end

  defp write_node({:table, name, v}, idx_map, ids, order) do
    desc = Map.fetch!(@tables, name)
    {tref, ids, order} = intern(ids, order, desc.type_id)
    {_idx, ids, order, fields} = save_fields(desc, v, idx_map, ids, order, [])
    node_body = [fields, <<0>>]
    {[leb_encode(tref), leb_encode(IO.iodata_length(node_body)), node_body], ids, order}
  end

  # ---- the read side (docs/SPEC-TABLES.md §3, §4) ----

  def load(table, data) when is_binary(data) do
    desc = Map.fetch!(@tables, table)

    case bits_refused(desc) do
      nil -> load_carried(table, data)
      name -> {:error, {:bits, name}, empty_report()}
    end
  end

  defp load_carried(table, data) when is_binary(data) do
    desc = Map.fetch!(@tables, table)

    case open(data) do
      {:error, reason} ->
        {:error, reason, empty_report()}

      {:ok, body, ids} ->
        case entries(body, ids, desc.string_blob, desc.bytes_blob) do
          {:error, _} ->
            {:error, :malformed, empty_report()}

          {:stop, _fields, _nodes, _remaining} ->
            # a ROOT body the walk could not finish is malformed; nothing below
            # the damage is read, so there is no value to save (§3)
            {:error, :malformed, empty_report()}

          {:ok, fields, nodes} ->
            {nodes, node_report} = materialize_nodes(nodes, ids)
            {value, report} = decode(desc, fields, nodes, ids)
            report = merge_report(report, node_report)
            report = if :malformed in nodes, do: %{report | malformed: true}, else: report
            unknown = Enum.count(nodes, &(&1 == :unknown))
            report = %{report | unknown: report.unknown + unknown}
            {:ok, Map.merge(defaults(desc), value), report}
        end
    end
  end

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
      for i <- 0..div(byte_size(ids), 8) - 1 do
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
  # node table out into its records (docs/SPEC-TABLES.md §3.1).
  defp entries(bin, ids, string_blob, bytes_blob), do: entries(bin, ids, string_blob, bytes_blob, [], [])

  # entries returns {:ok, fields, nodes} when the body ends at its terminator,
  # or {:stop, fields, nodes, remaining} when the walk stops, the fourth item
  # being the bytes the walk left un-consumed: 0 means the body ran off its own
  # end cleanly, which is the one stop that keeps the decoded prefix (§3).
  defp entries(bin, ids, string_blob, bytes_blob, acc, nodes) do
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
                if id == @build_version_id or id == @message_vocab_id do
                  # the two reserved ids ride in the ANNOUNCEMENT and nowhere
                  # else (§3.3): one in any body but the announcement's own is
                  # malformed, the walk stops there and no field after it is
                  # read (§3.1)
                  {:stop, Enum.reverse(acc), nodes, byte_size(rest2)}
                else
                  case slice(rest2, kind) do
                    {:ok, payload, rest3} ->
                      if id == @node_id do
                        # the reserved node id is this reader's own (§3.1): the
                        # root body reads it as the node table, and a body that
                        # carries it under another kind steps over it, never
                        # counting it unknown
                        if kind == 12 do
                          case node_table(payload, ids, string_blob, bytes_blob) do
                            {:ok, ns} -> entries(rest3, ids, string_blob, bytes_blob, acc, nodes ++ ns)
                            :error -> {:stop, Enum.reverse(acc), nodes, byte_size(rest2)}
                          end
                        else
                          entries(rest3, ids, string_blob, bytes_blob, acc, nodes)
                        end

                      else
                        entries(rest3, ids, string_blob, bytes_blob, [{id, kind, payload} | acc], nodes)
                      end

                    :error ->
                      # a kind this reader cannot skip, or a length its body
                      # cannot hold: the field still counts — unknown when the id
                      # is not this body's, a kind mismatch when it is — and the
                      # walk stops (§3, §4)
                      if id == @node_id do
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
  # §3.1): a count, then per record a type-id reference and a body. A *string
  # body is text, a *bytes body is raw bytes (§2.5), and a table body is a
  # framed record decoded once every record's framing is known. The whole
  # table is read or it is nothing: a count that disagrees with the scan makes
  # the table malformed, so every pointer in the save reads null.
  defp node_table(bin, ids, string_blob, bytes_blob) do
    case leb(bin) do
      {l, rest} when byte_size(rest) >= l ->
        content = binary_part(rest, 0, l)

        case leb(content) do
          {n, cr} -> node_records(cr, n, [], ids, string_blob, bytes_blob)
          :error -> :error
        end

      _ ->
        :error
    end
  end

  defp node_records(<<>>, n, acc, _ids, _string_blob, _bytes_blob) do
    if n == length(acc), do: {:ok, Enum.reverse(acc)}, else: :error
  end

  defp node_records(rest, n, acc, ids, string_blob, bytes_blob) do
    with {sref, r1} <- leb(rest),
         true <- sref != 0 and sref <= div(byte_size(ids), 8),
         {len, r2} <- leb(r1),
         true <- byte_size(r2) >= len do
      bytes = binary_part(r2, 0, len)
      type_id = resolve(ids, sref)

      node =
        cond do
          string_blob and type_id == @string_id ->
            if utf8_valid?(bytes), do: bytes, else: :malformed

          bytes_blob and type_id == @bytes_id ->
            {:bytes, bytes}

          true ->
            case Map.get(@table_types, type_id) do
              nil ->
                # a record whose type id this reader cannot name keeps its index
                # and every pointer naming it reads null; the unknown is counted
                # once at the record, not once per pointer (§3.1)
                :unknown

              name ->
                {:raw, name, bytes}
            end
        end

      node_records(binary_part(r2, len, byte_size(r2) - len), n, [node | acc], ids, string_blob, bytes_blob)
    else
      _ -> :error
    end
  end

  # materialize_nodes decodes table records after the scan, so a pointer
  # inside one record can name another (docs/SPEC-TABLES.md §3.1). String and
  # byte records are already their bytes. A card wire has no table record, and
  # this is the identity on that list.
  defp materialize_nodes(nodes, ids) do
    if Enum.any?(nodes, &match?({:raw, _, _}, &1)) do
      last = length(nodes) - 1

      {rev, _memo, report} =
        Enum.reduce(0..last, {[], %{}, empty_report()}, fn i, {acc, memo, report} ->
          {v, memo, report} = force_node(nodes, i, ids, memo, report)
          {[v | acc], memo, report}
        end)

      {Enum.reverse(rev), report}
    else
      {nodes, empty_report()}
    end
  end

  defp force_node(nodes, i, ids, memo, report) do
    case Map.get(memo, i, :missing) do
      :missing ->
        case Enum.at(nodes, i) do
          {:raw, name, body} ->
            memo = Map.put(memo, i, :open)
            {map, memo, report} = decode_table_node(name, body, nodes, ids, memo, report)
            wrapped = {:table, name, map}
            {wrapped, Map.put(memo, i, wrapped), report}

          other ->
            {other, Map.put(memo, i, other), report}
        end

      :open ->
        {nil, memo, %{report | malformed: true}}

      v ->
        {v, memo, report}
    end
  end

  defp decode_table_node(name, body, nodes, ids, memo, report) do
    desc = Map.fetch!(@tables, name)

    if ends_early?(body, ids) do
      {defaults(desc), memo, %{report | malformed: true}}
    else
      case entries(body, ids, false, false) do
        {:ok, fields, _} ->
          {value, memo, report} = decode_fields_resolving(desc, fields, nodes, ids, memo, report)
          {Map.merge(defaults(desc), value), memo, report}

        _ ->
          {defaults(desc), memo, %{report | malformed: true}}
      end
    end
  end

  defp decode_fields_resolving(desc, fields, nodes, ids, memo, report) do
    Enum.reduce(fields, {%{}, memo, report}, fn {id, kind, payload}, {value, memo, report} ->
      case find_field(desc, id) do
        nil ->
          {value, memo, %{report | unknown: report.unknown + 1}}

        f ->
          cond do
            kind == f.kind ->
              {v, memo, report} = read_resolving(f, payload, nodes, ids, memo, report)
              {Map.put(value, f.name, v), memo, report}

            widens?(kind, f) ->
              {v, report} = read_widened(f, kind, payload, report)
              {Map.put(value, f.name, v), memo, report}

            true ->
              {value, memo, %{report | kind_mismatch: report.kind_mismatch + 1}}
          end
      end
    end)
  end

  defp read_resolving(%{t: :blob} = f, payload, nodes, ids, memo, report) do
    take_pointer(f, payload, nodes, ids, memo, report, :blob)
  end

  defp read_resolving(%{t: :bblob} = f, payload, nodes, ids, memo, report) do
    take_pointer(f, payload, nodes, ids, memo, report, :bblob)
  end

  defp read_resolving(%{t: :ptr} = f, payload, nodes, ids, memo, report) do
    take_pointer(f, payload, nodes, ids, memo, report, :ptr)
  end

  defp read_resolving(f, payload, nodes, ids, memo, report) do
    case read_payload(f, payload, nodes, ids, report) do
      {:cont, v, report} -> {v, memo, report}
      {:halt, v, report} -> {v, memo, %{report | malformed: true}}
    end
  end

  # take_pointer resolves one kind-17 index against the numbering
  # (docs/SPEC-TABLES.md §3.1). 0 is null, 1 is the root (never a blob, and a
  # table slot names it only when the root's own type is that slot's), and
  # k + 1 is record k.
  defp take_pointer(f, payload, nodes, ids, memo, report, which) do
    case leb(payload) do
      {0, _} ->
        {nil, memo, report}

      {1, _} ->
        {nil, memo, %{report | kind_mismatch: report.kind_mismatch + 1}}

      {idx, _} ->
        at = idx - 2

        cond do
          at < 0 or at >= length(nodes) ->
            {nil, memo, %{report | malformed: true}}

          Enum.at(nodes, at) == :malformed or Enum.at(nodes, at) == :unknown ->
            {nil, memo, report}

          true ->
            {node, memo, report} = force_node(nodes, at, ids, memo, report)

            case {which, node} do
              {:blob, bin} when is_binary(bin) ->
                {bin, memo, report}

              {:bblob, {:bytes, bin}} ->
                {bin, memo, report}

              {:ptr, {:table, name, map}} ->
                if name == f.table do
                  {map, memo, report}
                else
                  {nil, memo, %{report | kind_mismatch: report.kind_mismatch + 1}}
                end

              _ ->
                {nil, memo, %{report | kind_mismatch: report.kind_mismatch + 1}}
            end
        end

      _ ->
        {nil, memo, %{report | malformed: true}}
    end
  end

  # decode folds the field triples into a value, tolerantly (docs/SPEC-TABLES.md §4).
  # A reference that resolves ABOVE the entry count is framing damage on the
  # body carrying it (§3) and STOPS the walk: the fields decoded before it
  # stand, one malformed counts, and no field after it is read. §4's ordinary
  # events — an unknown id, a kind mismatch, an out-of-range value, a reference
  # a reader cannot name — read on, and the halt is the one exception.
  defp decode(desc, fields, nodes, ids) do
    Enum.reduce_while(fields, {Map.new(), empty_report()}, fn {id, kind, payload}, {value, report} ->
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

  defp read_payload(%{t: :blob} = _f, payload, nodes, _ids, report) do
    case leb(payload) do
      {0, _} ->
        {:cont, nil, report}

      {1, _} ->
        # node index 1 is the ROOT, a table, never a blob (§3.1): a *string
        # naming it is a kind mismatch, and the pointer reads null
        {:cont, nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

      {idx, _} ->
        at = idx - 2

        cond do
          at < 0 or at >= length(nodes) ->
            # an index above node_count + 1 is framing damage; the pointer reads
            # null and the walk reads on (§3.1)
            {:cont, nil, %{report | malformed: true}}

          Enum.at(nodes, at) == :malformed ->
            # a text blob whose content was refused reads null; the malformed
            # was counted once at the node table (§3.1)
            {:cont, nil, report}

          Enum.at(nodes, at) == :unknown ->
            # a record whose type id this reader cannot name reads null; the
            # unknown was counted once at the node table (§3.1)
            {:cont, nil, report}

          true ->
            {:cont, Enum.at(nodes, at), report}
        end
    end
  end

  # *bytes names a bytes record (docs/SPEC-TABLES.md §2.5, §3.1).
  defp read_payload(%{t: :bblob} = f, payload, nodes, ids, report) do
    read_placed(f, payload, nodes, ids, report, :bblob)
  end

  # *T names a table record of the field's own table (docs/SPEC-TABLES.md §2.1, §3.1).
  defp read_payload(%{t: :ptr} = f, payload, nodes, ids, report) do
    read_placed(f, payload, nodes, ids, report, :ptr)
  end

  # a by-value table's payload is a framed body (docs/SPEC-TABLES.md §3). An
  # absent field never reaches here: the caller fills the declared defaults.
  defp read_payload(%{t: :nest} = f, payload, nodes, ids, report) do
    child = Map.fetch!(@tables, f.table)

    case leb(payload) do
      {_len, body} ->
        case entries(body, ids, false, false) do
          {:ok, fields, []} ->
            {value, elem_report} = decode(child, fields, nodes, ids)
            {:cont, Map.merge(defaults(child), value), merge_report(report, elem_report)}

          {:stop, fields, [], 0} ->
            {value, elem_report} = decode(child, fields, nodes, ids)
            {:cont, Map.merge(defaults(child), value), merge_report(%{report | malformed: true}, elem_report)}

          _ ->
            {:cont, defaults(child), %{report | malformed: true}}
        end

      :error ->
        {:cont, defaults(child), %{report | malformed: true}}
    end
  end

  defp read_placed(f, payload, nodes, _ids, report, which) do
    case leb(payload) do
      {0, _} ->
        {:cont, nil, report}

      {1, _} ->
        {:cont, nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

      {idx, _} ->
        at = idx - 2

        cond do
          at < 0 or at >= length(nodes) ->
            {:cont, nil, %{report | malformed: true}}

          Enum.at(nodes, at) == :malformed or Enum.at(nodes, at) == :unknown ->
            {:cont, nil, report}

          true ->
            case {which, Enum.at(nodes, at)} do
              {:bblob, {:bytes, bin}} ->
                {:cont, bin, report}

              {:ptr, {:table, name, map}} ->
                if name == f.table do
                  {:cont, map, report}
                else
                  {:cont, nil, %{report | kind_mismatch: report.kind_mismatch + 1}}
                end

              _ ->
                {:cont, nil, %{report | kind_mismatch: report.kind_mismatch + 1}}
            end
        end

      _ ->
        {:cont, nil, %{report | malformed: true}}
    end
  end

  # a scalar array's body is the element kind, the count, then packed values
  # (docs/SPEC-TABLES.md §2.9, §3). A narrower kind on the same signedness
  # ladder widens (§4); any other element kind is a mismatch and the field
  # keeps its default.
  defp read_payload(%{t: :list, elem_t: :int} = f, payload, _nodes, _ids, report) do
    case leb(payload) do
      {_len, arr} ->
        case arr do
          <<ek, count_rest::binary>> ->
            case leb(count_rest) do
              {n, elems} ->
                cond do
                  ek == f.ek ->
                    {value, report} = read_packed_ints(elems, n, f.width, f.signed, f, [], report)
                    {:cont, value, report}

                  int_elem_widens?(ek, f) ->
                    {value, report} = read_packed_ints(elems, n, kind_width(ek), f.signed, f, [], report)
                    {:cont, value, %{report | widened: report.widened + 1}}

                  true ->
                    {:cont, [], %{report | kind_mismatch: report.kind_mismatch + 1}}
                end

              :error ->
                {:cont, [], %{report | malformed: true}}
            end

          _ ->
            {:cont, [], %{report | malformed: true}}
        end

      :error ->
        {:cont, [], %{report | malformed: true}}
    end
  end

  defp read_payload(%{t: :list, elem_t: :ptr} = f, payload, nodes, _ids, report) do
    case leb(payload) do
      {_len, arr} ->
        case arr do
          <<ek, count_rest::binary>> when ek == f.ek ->
            case leb(count_rest) do
              {n, elems} ->
                {value, report} = read_ptr_elems(f, elems, n, nodes, [], report)
                {:cont, value, report}

              :error ->
                {:cont, [], %{report | malformed: true}}
            end

          <<_ek, _rest::binary>> ->
            {:cont, [], %{report | kind_mismatch: report.kind_mismatch + 1}}

          _ ->
            {:cont, [], %{report | malformed: true}}
        end

      :error ->
        {:cont, [], %{report | malformed: true}}
    end
  end

  defp read_payload(%{t: :list, elem_t: :tab} = f, payload, nodes, ids, report) do
    case leb(payload) do
      {_len, arr} ->
        elem_desc = Map.fetch!(@tables, f.elem)

        case arr do
          <<ek, count_rest::binary>> when ek == f.ek ->
            case leb(count_rest) do
              {n, elems} ->
                {value, report} = read_elements(elem_desc, elems, n, nodes, ids, [], report)
                {:cont, value, report}

              :error ->
                {:cont, [], %{report | malformed: true}}
            end

          <<_ek, _rest::binary>> ->
            {:cont, [], %{report | kind_mismatch: report.kind_mismatch + 1}}

          _ ->
            {:cont, [], %{report | malformed: true}}
        end

      :error ->
        {:cont, [], %{report | malformed: true}}
    end
  end

  defp read_payload(%{t: :list}, _payload, _nodes, _ids, report) do
    {:cont, [], %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  # widens? reports whether a wire kind decodes EXACTLY into this int field
  # (docs/SPEC-TABLES.md §4): a smaller signed integer into a wider one of the
  # same signedness. The card declares int32 and int64 only, so the ladder is
  # kinds 2, 3, 4, 5 and every kind below the declared one widens.
  defp widens?(kind, %{t: :int, signed: true} = f), do: kind >= 2 and kind < f.kind
  defp widens?(_kind, _f), do: false

  # read_widened decodes an int field whose wire kind widened (§4): the value
  # reads at the wire kind's own width and lands, one widened event counts, and an
  # out-of-range value clamps as always.
  defp read_widened(f, kind, payload, report) do
    v = int_of(payload, kind_width(kind), f.signed)
    {clamped, report} = clamp_int(v, f, report)
    {clamped, %{report | widened: report.widened + 1}}
  end

  defp kind_width(2), do: 1
  defp kind_width(3), do: 2
  defp kind_width(4), do: 4
  defp kind_width(5), do: 8
  defp kind_width(_), do: 0

  defp int_elem_widens?(ek, %{signed: true, ek: declared})
       when is_integer(ek) and ek >= 2 and ek <= 5 and ek < declared,
       do: true

  defp int_elem_widens?(_ek, _f), do: false

  defp read_packed_ints(_rest, 0, _w, _signed, _f, acc, report), do: {Enum.reverse(acc), report}

  defp read_packed_ints(rest, n, w, signed, f, acc, report) when w > 0 and byte_size(rest) >= w do
    <<chunk::binary-size(^w), tail::binary>> = rest
    {v, report} = clamp_int(int_of(chunk, w, signed), f, report)
    read_packed_ints(tail, n - 1, w, signed, f, [v | acc], report)
  end

  defp read_packed_ints(_rest, _n, _w, _signed, _f, acc, report),
    do: {Enum.reverse(acc), %{report | malformed: true}}

  # one slot of []*T is a node index (docs/SPEC-TABLES.md §2.1, §3.1). 0 is
  # null. 1 names the root and is a kind mismatch. A damaged index stops the
  # walk and the elements already read stand (§4).
  defp read_ptr_elems(_f, _rest, 0, _nodes, acc, report), do: {Enum.reverse(acc), report}

  defp read_ptr_elems(f, rest, n, nodes, acc, report) do
    case leb(rest) do
      {0, after_idx} ->
        read_ptr_elems(f, after_idx, n - 1, nodes, [nil | acc], report)

      {1, after_idx} ->
        read_ptr_elems(f, after_idx, n - 1, nodes, [nil | acc], %{report | kind_mismatch: report.kind_mismatch + 1})

      {idx, after_idx} ->
        at = idx - 2

        cond do
          at < 0 or at >= length(nodes) ->
            {Enum.reverse(acc), %{report | malformed: true}}

          Enum.at(nodes, at) == :malformed or Enum.at(nodes, at) == :unknown ->
            read_ptr_elems(f, after_idx, n - 1, nodes, [nil | acc], report)

          true ->
            case Enum.at(nodes, at) do
              {:table, name, map} ->
                if name == f.elem do
                  read_ptr_elems(f, after_idx, n - 1, nodes, [map | acc], report)
                else
                  read_ptr_elems(f, after_idx, n - 1, nodes, [nil | acc], %{report | kind_mismatch: report.kind_mismatch + 1})
                end

              _ ->
                read_ptr_elems(f, after_idx, n - 1, nodes, [nil | acc], %{report | kind_mismatch: report.kind_mismatch + 1})
            end
        end

      :error ->
        {Enum.reverse(acc), %{report | malformed: true}}
    end
  end

  defp read_elements(_desc, _rest, 0, _nodes, _ids, acc, report), do: {Enum.reverse(acc), report}

  defp read_elements(desc, rest, n, nodes, ids, acc, report) do
    case leb(rest) do
      {len, body} when byte_size(body) >= len ->
        sub = binary_part(body, 0, len)
        after_sub = binary_part(body, len, byte_size(body) - len)

        {value, elem_report} = read_element(desc, sub, nodes, ids)
        read_elements(desc, after_sub, n - 1, nodes, ids, [value | acc], merge_report(report, elem_report))

      _ ->
        # the element's L framing is damaged: the walk stops, one malformed
        # counts, and the elements already read stand (§4)
        {Enum.reverse(acc), %{report | malformed: true}}
    end
  end

  # read_element decodes one []T element (docs/SPEC-TABLES.md §2.9, §4). A body
  # that cannot be read whole — its L does not frame it, its terminator is not
  # its last byte, or its walk stops on damage — reads the element's DECLARED
  # DEFAULTS and one malformed counts, and the list reads on past it. A clean
  # body reads its value, and an event inside it folds into the list's report.
  defp read_element(desc, sub, nodes, ids) do
    if ends_early?(sub, ids) do
      {%{}, malformed_report()}
    else
      case entries(sub, ids, false, false) do
        {:ok, fields, []} ->
          # a list element's report FOLDS into the enclosing one (§4): the
          # counters sum and malformed sticks, so an event inside an element
          # is never lost when the element's own walk returns.
          decode(desc, fields, nodes, ids)

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
  end

  defp malformed_report, do: %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: true, retained: 0, retain_lost: 0}

  # ---- the shared primitives ----

  defp empty_report, do: %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}

  # defaults is a field's declared default: what an absent field reads (§4).
  defp defaults(desc) do
    Map.new(desc.fields, fn f ->
      d =
        case f.t do
          :string -> ""
          :int -> Map.get(f, :default, 0)
          :nest -> defaults(Map.fetch!(@tables, f.table))
          :list -> []
          :enum -> nil
          :blob -> nil
          :bblob -> nil
          :ptr -> nil
          :bool -> false
        end

      {f.name, d}
    end)
  end

  # intern assigns a reference in first-use order and returns it.
  defp intern(ids, order, id) do
    case ids do
      %{^id => ref} -> {ref, ids, order}
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
      i == 9 and b > 1 -> :error
      (b &&& 0x80) == 0 ->
        if i > 0 and b == 0, do: :error, else: {acc ||| ((b &&& 0x7F) <<< (7 * i)), rest}
      true -> leb(rest, i + 1, acc ||| ((b &&& 0x7F) <<< (7 * i)))
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
      <<(b ||| 0x80), leb_encode(rest)::binary>>
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

  defp int_bytes(v, 4, true), do: <<v::little-signed-32>>
  defp int_bytes(v, 4, false), do: <<v::little-unsigned-32>>
  defp int_bytes(v, 8, true), do: <<v::little-signed-64>>
  defp int_bytes(v, 8, false), do: <<v::little-unsigned-64>>

  defp int_of(<<v::little-signed-8>>, 1, true), do: v
  defp int_of(<<v::little-signed-16>>, 2, true), do: v
  defp int_of(<<v::little-signed-32>>, 4, true), do: v
  defp int_of(<<v::little-signed-64>>, 8, true), do: v
  defp int_of(<<v::little-unsigned-32>>, 4, false), do: v
  defp int_of(<<v::little-unsigned-64>>, 8, false), do: v
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
  # The card's constructs only, one text for one table. The writer emits every
  # field in declaration order, defaults included, pretty-printed two spaces per
  # level and ending with exactly one newline (§16.1); the reader places every
  # key it can name, counts the rest and clamps rather than truncating (§16.2).

  def to_json(table, value) when is_map(value) do
    desc = Map.fetch!(@tables, table)
    {:ok, IO.iodata_to_binary([json_object(desc, Map.merge(defaults(desc), value), 0), "\n"])}
  end

  def from_json(table, text) when is_binary(text) do
    desc = Map.fetch!(@tables, table)

    case json_value(text) do
      {:ok, doc, _rest} when is_map(doc) ->
        {value, report} = json_fields(desc, doc)
        {:ok, Map.merge(defaults(desc), value), report}

      {:ok, _other, _rest} ->
        {:error, :malformed, empty_report()}

      :error ->
        {:error, :malformed, empty_report()}
    end
  end

  # json_object writes one instance's fields in declaration order, defaults
  # included (§16.1: a text is for people and tools, so elision is not for it).
  defp json_object(desc, value, depth) do
    entries =
      Enum.map(desc.fields, fn f ->
        ["\n", indent(depth + 1), "\"", json_escape(f.json), "\": ",
         json_field(f, Map.get(value, f.name), depth + 1)]
      end)

    ["{", Enum.intersperse(entries, ","), "\n", indent(depth), "}"]
  end

  # a scalar list is one number per line (docs/SPEC-TABLES.md §16.1, §16.7).
  defp json_field(%{t: :list, elem_t: :int}, elements, depth) when is_list(elements) do
    case elements do
      [] ->
        "[]"

      _ ->
        rows =
          Enum.map(elements, fn e ->
            ["\n", indent(depth + 1), Integer.to_string(e)]
          end)

        ["[", Enum.intersperse(rows, ","), "\n", indent(depth), "]"]
    end
  end

  defp json_field(%{t: :list, elem_t: :int}, _other, _depth), do: "[]"

  defp json_field(%{t: :list} = f, elements, depth) when is_list(elements) do
    case elements do
      [] ->
        "[]"

      _ ->
        elem_desc = Map.fetch!(@tables, f.elem)

        rows =
          Enum.map(elements, fn e ->
            ["\n", indent(depth + 1), json_object(elem_desc, e, depth + 1)]
          end)

        ["[", Enum.intersperse(rows, ","), "\n", indent(depth), "]"]
    end
  end

  defp json_field(%{t: :list}, _other, _depth), do: "[]"

  defp json_field(%{t: :string}, v, _depth), do: ["\"", json_escape(v), "\""]
  defp json_field(%{t: :blob}, nil, _depth), do: "null"
  defp json_field(%{t: :blob}, v, _depth), do: ["\"", json_escape(v), "\""]
  # *bytes is base64 (docs/SPEC-TABLES.md §16.7); null is null.
  defp json_field(%{t: :bblob}, nil, _depth), do: "null"
  defp json_field(%{t: :bblob}, v, _depth) when is_binary(v), do: ["\"", Base.encode64(v), "\""]
  # a table pointer inlines the node, or null (docs/SPEC-TABLES.md §16.7).
  defp json_field(%{t: :ptr}, nil, _depth), do: "null"

  defp json_field(%{t: :ptr} = f, v, depth) when is_map(v) do
    desc = Map.fetch!(@tables, f.table)
    json_object(desc, Map.merge(defaults(desc), v), depth)
  end

  # a by-value table inlines, defaults included (docs/SPEC-TABLES.md §16.1, §16.7).
  defp json_field(%{t: :nest} = f, v, depth) when is_map(v) do
    desc = Map.fetch!(@tables, f.table)
    json_object(desc, Map.merge(defaults(desc), v), depth)
  end

  defp json_field(%{t: :nest} = f, _v, depth) do
    desc = Map.fetch!(@tables, f.table)
    json_object(desc, defaults(desc), depth)
  end
  defp json_field(%{t: :enum}, nil, _depth), do: "\"None\""
  defp json_field(%{t: :enum}, name, _depth), do: ["\"", json_escape(name), "\""]
  defp json_field(%{t: :int}, v, _depth), do: Integer.to_string(v)
  defp json_field(%{t: :bool}, v, _depth), do: if(v, do: "true", else: "false")

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
  defp json_fields(desc, doc) when is_map(doc) do
    {value, report} =
      Enum.reduce(desc.fields, {%{}, empty_report()}, fn f, {value, report} ->
        case Map.fetch(doc, f.json) do
          {:ok, raw} ->
            {v, report} = json_read_field(f, raw, report)
            {Map.put(value, f.name, v), report}

          :error ->
            {value, report}
        end
      end)

    known = MapSet.new(desc.fields, & &1.json)
    unknown = Enum.count(Map.keys(doc), fn k -> not MapSet.member?(known, k) end)
    {value, %{report | unknown: report.unknown + unknown}}
  end

  defp json_read_field(%{t: :string} = f, raw, report) when is_binary(raw) do
    if byte_size(raw) > f.size do
      {binary_part(raw, 0, f.size), %{report | clamped: report.clamped + 1}}
    else
      {raw, report}
    end
  end

  defp json_read_field(%{t: :string}, _raw, report),
    do: {"", %{report | kind_mismatch: report.kind_mismatch + 1}}

  defp json_read_field(%{t: :enum} = f, raw, report) when is_binary(raw) do
    # "None" is the reserved nil spelling (docs/SPEC-TABLES.md §16; the Go
    # reference internal/tabletext/value.go EnumValue): it reads the nil variant
    # exactly as value 0 does on the wire, and is never an unknown name.
    case raw do
      "None" -> {nil, report}
      _ ->
        case Map.fetch(f.by_name, raw) do
          {:ok, _id} -> {raw, report}
          :error -> {nil, %{report | unknown: report.unknown + 1}}
        end
    end
  end

  defp json_read_field(%{t: :enum}, _raw, report),
    do: {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

  defp json_read_field(%{t: :int} = f, raw, report) when is_integer(raw) do
    {v, report} = clamp_int(raw, f, report)
    {v, report}
  end

  defp json_read_field(%{t: :int}, _raw, report),
    do: {0, %{report | kind_mismatch: report.kind_mismatch + 1}}

  defp json_read_field(%{t: :bool}, raw, report) when is_boolean(raw), do: {raw, report}

  defp json_read_field(%{t: :bool}, _raw, report),
    do: {false, %{report | kind_mismatch: report.kind_mismatch + 1}}

  defp json_read_field(%{t: :blob}, raw, report) when is_binary(raw), do: {raw, report}
  defp json_read_field(%{t: :blob}, nil, report), do: {nil, report}

  defp json_read_field(%{t: :blob}, _raw, report),
    do: {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

  # *bytes text is base64 (docs/SPEC-TABLES.md §16.7). A body that is not
  # base64 is the wrong shape: the reference stays null and one mismatch counts.
  defp json_read_field(%{t: :bblob}, raw, report) when is_binary(raw) do
    case Base.decode64(raw) do
      {:ok, bin} -> {bin, report}
      :error -> {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}
    end
  end

  defp json_read_field(%{t: :bblob}, nil, report), do: {nil, report}

  defp json_read_field(%{t: :bblob}, _raw, report),
    do: {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

  defp json_read_field(%{t: :ptr} = f, raw, report) when is_map(raw) do
    desc = Map.fetch!(@tables, f.table)
    {v, report} = json_fields(desc, raw)
    {Map.merge(defaults(desc), v), report}
  end

  defp json_read_field(%{t: :ptr}, nil, report), do: {nil, report}

  defp json_read_field(%{t: :nest} = f, raw, report) when is_map(raw) do
    desc = Map.fetch!(@tables, f.table)
    {v, report} = json_fields(desc, raw)
    {Map.merge(defaults(desc), v), report}
  end

  defp json_read_field(%{t: :nest} = f, _raw, report) do
    {defaults(Map.fetch!(@tables, f.table)), %{report | kind_mismatch: report.kind_mismatch + 1}}
  end

  defp json_read_field(%{t: :ptr}, _raw, report),
    do: {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

  defp json_read_field(%{t: :list, elem_t: :int} = f, raw, report) when is_list(raw) do
    {elems, report} =
      Enum.reduce(raw, {[], report}, fn e, {acc, report} ->
        case e do
          n when is_integer(n) ->
            {v, report} = clamp_int(n, f, report)
            {[v | acc], report}

          _ ->
            {acc, %{report | kind_mismatch: report.kind_mismatch + 1}}
        end
      end)

    {Enum.reverse(elems), report}
  end

  defp json_read_field(%{t: :list} = f, raw, report) when is_list(raw) do
    # an empty list names no element table. A fixed table used only as an
    # element type is not in @tables, and fetching it for [] would be a miss
    # (docs/SPEC-TABLES.md §16.2, §2.9).
    if raw == [] do
      {[], report}
    else
    elem_desc = Map.fetch!(@tables, f.elem)

    {elems, report} =
      Enum.reduce(raw, {[], report}, fn e, {acc, report} ->
        case e do
          m when is_map(m) ->
            {v, elem_report} = json_fields(elem_desc, m)
            {[v | acc], merge_report(report, elem_report)}

          _ ->
            {acc, %{report | kind_mismatch: report.kind_mismatch + 1}}
        end
      end)

    {Enum.reverse(elems), report}
    end
  end

  defp json_read_field(%{t: :list}, _raw, report),
    do: {[], %{report | kind_mismatch: report.kind_mismatch + 1}}

  # merge_report folds one nested value's report into the enclosing one
  # (§16.2): the counters sum and malformed sticks, so an event inside a list
  # element is never lost when the element's own walk returns.
  defp merge_report(a, b) do
    %{
      unknown: a.unknown + b.unknown,
      kind_mismatch: a.kind_mismatch + b.kind_mismatch,
      widened: a.widened + b.widened,
      clamped: a.clamped + b.clamped,
      malformed: a.malformed or b.malformed,
      retained: a.retained + b.retained,
      retain_lost: a.retain_lost + b.retain_lost
    }
  end

  # ---- a JSON parser over the standard library (docs/SPEC-TABLES.md §16.3):
  # ---- objects, arrays, strings with escapes, numbers, booleans and null

  defp json_value(bin) do
    case json_skip(bin) do
      <<"{", rest::binary>> -> json_object_p(rest, %{})
      <<"[", rest::binary>> -> json_array_p(rest, [])
      <<"\"", rest::binary>> -> json_string(rest, [])
      <<"true", rest::binary>> -> {:ok, true, rest}
      <<"false", rest::binary>> -> {:ok, false, rest}
      <<"null", rest::binary>> -> {:ok, nil, rest}
      <<?-, _::binary>> = num -> json_number(num, [])
      <<c, _::binary>> = num when c in ?0..?9 -> json_number(num, [])
      _ -> :error
    end
  end

  defp json_skip(<<c, rest::binary>>) when c in [?\s, ?\t, ?\n, ?\r], do: json_skip(rest)
  defp json_skip(bin), do: bin

  defp json_object_p(bin, acc) do
    case json_skip(bin) do
      <<",", rest::binary>> -> json_object_p(rest, acc)

      <<"}", rest::binary>> ->
        {:ok, acc, rest}

      <<"\"", rest::binary>> ->
        with {:ok, key, r1} <- json_string(rest, []),
             <<":", r2::binary>> <- json_skip(r1),
             {:ok, val, r3} <- json_value(r2) do
          json_object_p(r3, Map.put(acc, key, val))
        else
          _ -> :error
        end

      _ ->
        :error
    end
  end

  defp json_array_p(bin, acc) do
    case json_skip(bin) do
      <<",", rest::binary>> -> json_array_p(rest, acc)

      <<"]", rest::binary>> ->
        {:ok, Enum.reverse(acc), rest}

      _ ->
        case json_value(bin) do
          {:ok, val, rest} -> json_array_p(rest, [val | acc])
          :error -> :error
        end
    end
  end

  defp json_string(<<"\"", rest::binary>>, acc),
    do: {:ok, IO.iodata_to_binary(Enum.reverse(acc)), rest}

  defp json_string(<<"\\", rest::binary>>, acc) do
    case rest do
      <<"\"", r::binary>> -> json_string(r, ["\"" | acc])
      <<"\\", r::binary>> -> json_string(r, ["\\" | acc])
      <<"/", r::binary>> -> json_string(r, ["/" | acc])
      <<"b", r::binary>> -> json_string(r, ["\b" | acc])
      <<"f", r::binary>> -> json_string(r, ["\f" | acc])
      <<"n", r::binary>> -> json_string(r, ["\n" | acc])
      <<"r", r::binary>> -> json_string(r, ["\r" | acc])
      <<"t", r::binary>> -> json_string(r, ["\t" | acc])
      <<"u", a, b, c, d, r::binary>> ->
        code = hexval(a) * 4096 + hexval(b) * 256 + hexval(c) * 16 + hexval(d)
        point = if code in 0xD800..0xDFFF, do: 0xFFFD, else: code
        json_string(r, [<<point::utf8>> | acc])

      _ ->
        :error
    end
  end

  defp json_string(<<c, rest::binary>>, acc) when c >= 0x20, do: json_string(rest, [<<c>> | acc])
  defp json_string(_, _acc), do: :error

  defp hexval(c) when c in ?0..?9, do: c - ?0
  defp hexval(c) when c in ?a..?f, do: c - ?a + 10
  defp hexval(c) when c in ?A..?F, do: c - ?A + 10
  defp hexval(_), do: 0

  defp json_number(bin, acc) do
    case bin do
      <<c, rest::binary>> when c in ?0..?9 -> json_number(rest, [c | acc])
      rest ->
        digits = acc |> Enum.reverse() |> IO.iodata_to_binary()
        {:ok, String.to_integer(digits), rest}
    end
  end

  # ---- the region measure (docs/SPEC-TABLES.md §4.2, §6.5) ----
  #
  # load_measure/2 answers the region bytes a variable-class Load owes: the
  # root's node bytes (its record plus every list extent the value reaches),
  # each node record's storage, and the attribution directory of one u64 pair
  # per record plus the root (§6.3). It is the number the Go, C, C++ and C#
  # backends answer, so a caller sizes a region and the harness's oracle holds
  # the measure to the framing's bound. A wire this reader cannot open, or a
  # list count past the storage cap, answers -1.

  def load_measure(table, data) when is_binary(data) do
    desc = Map.fetch!(@tables, table)

    case open(data) do
      {:error, _} ->
        -1

      {:ok, body, ids} ->
        with {:ok, at} <- extent(desc, body, ids),
             {:ok, count, blobs} <- measure_nodes(body, ids) do
          region_round(region_round(desc.size) + at) + blobs + (count + 1) * 16
        else
          _ -> -1
        end
    end
  end

  defp region_round(n), do: div(n + @region_align - 1, @region_align) * @region_align

  defp align_to(n, a), do: div(n + a - 1, a) * a

  # extent walks one body's triples summing the storage its list fields command
  # (docs/SPEC-TABLES.md §3.1, §6.5): a list's elements are inside its holder's
  # node extent, count × elem_size at elem_align, and the walk recurses into
  # each element body so a list inside a list is counted where it lives.
  defp extent(desc, bin, ids), do: extent(desc, bin, ids, 0)

  defp extent(desc, bin, ids, at) do
    case leb(bin) do
      :error ->
        {:ok, at}

      {0, _rest} ->
        {:ok, at}

      {ref, rest} ->
        case resolve(ids, ref) do
          :error ->
            {:ok, at}

          id ->
            case rest do
              <<kind, rest2::binary>> ->
                field = find_field(desc, id)

                if field != nil and field.t == :list and kind == 14 do
                  case slice(rest2, kind) do
                    {:ok, payload, rest3} ->
                      case leb(payload) do
                        {_l, arr} ->
                          case list_extent(field, arr, ids, at) do
                            :error -> :error
                            {:ok, at2} -> extent(desc, rest3, ids, at2)
                          end

                        :error ->
                          {:ok, at}
                      end

                    :error ->
                      {:ok, at}
                  end
                else
                  case slice(rest2, kind) do
                    {:ok, _payload, rest3} -> extent(desc, rest3, ids, at)
                    :error -> {:ok, at}
                  end
                end

              _ ->
                {:ok, at}
            end
        end
    end
  end

  defp list_extent(field, arr, ids, at) do
    case arr do
      <<ek, count_rest::binary>> when ek == field.ek ->
        case leb(count_rest) do
          :error ->
            :error

          {count, elems} ->
            if count > 0x7FFFFFFF do
              :error
            else
              if count > div(byte_size(elems), 2) do
                :error
              else
                at = align_to(at, field.elem_align) + count * field.elem_size

                if field.elem_t == :tab do
                  elem_desc = Map.fetch!(@tables, field.elem)
                  elements_extent(elem_desc, elems, count, ids, at)
                else
                  # a scalar or pointer element has no nested list body of its
                  # own in this measure (docs/SPEC-TABLES.md §2.9, §6.5)
                  {:ok, at}
                end
              end
            end
        end

      _ ->
        {:ok, at}
    end
  end

  defp elements_extent(_desc, _elems, 0, _ids, at), do: {:ok, at}

  defp elements_extent(desc, elems, n, ids, at) do
    case leb(elems) do
      :error ->
        {:ok, at}

      {len, rest} when byte_size(rest) >= len ->
        body = binary_part(rest, 0, len)
        after_body = binary_part(rest, len, byte_size(rest) - len)

        case extent(desc, body, ids, at) do
          :error -> :error
          {:ok, at2} -> elements_extent(desc, after_body, n - 1, ids, at2)
        end

      _ ->
        {:ok, at}
    end
  end

  # measure_nodes reads the node table out of the root body and sums the storage
  # each record commands (docs/SPEC-TABLES.md §3.1): a *string blob is its u32
  # length header, its bytes and its terminator, rounded; a type id this build
  # cannot name commands none. A node table that cannot be read whole commands
  # nothing, exactly as a reader that cannot name it steps over it.
  defp measure_nodes(bin, ids) do
    case leb(bin) do
      :error ->
        {:ok, 0, 0}

      {0, _rest} ->
        {:ok, 0, 0}

      {ref, rest} ->
        case resolve(ids, ref) do
          :error ->
            {:ok, 0, 0}

          id ->
            case rest do
              <<kind, rest2::binary>> ->
                case slice(rest2, kind) do
                  {:ok, payload, rest3} ->
                    if id == @node_id and kind == 12 do
                      case measure_node_table(payload, ids) do
                        {:ok, count, blobs} -> {:ok, count, blobs}
                        :error -> {:ok, 0, 0}
                      end
                    else
                      measure_nodes(rest3, ids)
                    end

                  :error ->
                    {:ok, 0, 0}
                end

              _ ->
                {:ok, 0, 0}
            end
        end
    end
  end

  defp measure_node_table(bin, ids) do
    case leb(bin) do
      {l, rest} when byte_size(rest) >= l ->
        content = binary_part(rest, 0, l)

        case leb(content) do
          {n, cr} -> measure_records(cr, n, ids, 0, 0)
          :error -> :error
        end

      _ ->
        :error
    end
  end

  defp measure_records(_rest, 0, _ids, count, blobs), do: {:ok, count, blobs}

  defp measure_records(rest, n, ids, count, blobs) do
    with {tref, r1} <- leb(rest),
         {len, r2} <- leb(r1),
         true <- byte_size(r2) >= len do
      blobs =
        case resolve(ids, tref) do
          @string_id -> blobs + region_round(8 + len + 1)
          _ -> blobs
        end

      measure_records(binary_part(r2, len, byte_size(r2) - len), n - 1, ids, count + 1, blobs)
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
      {:error, _} ->
        {:error, :malformed, report, retain}

      {:ok, body, ids} ->
        case entries(body, ids, desc.string_blob, desc.bytes_blob) do
          {:ok, fields, nodes} ->
            {value, report, retain} = decode_retain(desc, fields, nodes, ids, retain, report, [])
            report = if :malformed in nodes, do: %{report | malformed: true}, else: report
            # an unknown NODE RECORD whose type id this reader cannot name is an
            # excluded class (§6.6): one retain_lost, counted once at the record
            unknown = Enum.count(nodes, &(&1 == :unknown))
            report = %{report | unknown: report.unknown + unknown, retain_lost: report.retain_lost + unknown}
            {:ok, Map.merge(defaults(desc), value), report, retain}

          _ ->
            {:error, :malformed, report, retain}
        end
    end
  end

  def measure_retain(table, value, retain) when is_map(value) do
    {:ok, bytes, _retain, _report} = save_retain_internal(table, value, retain, empty_report())
    byte_size(bytes)
  end

  def save_retain(table, value, retain, report) when is_map(value) do
    {:ok, bytes, retain, report} = save_retain_internal(table, value, retain, report)
    {:ok, bytes, report, retain}
  end

  defp save_retain_internal(table, value, retain, report) do
    desc = Map.fetch!(@tables, table)
    nodes = collect_nodes(desc, value, []) |> Enum.reverse()
    idx_map = nodes |> Enum.with_index(2) |> Map.new(fn {v, i} -> {v, i} end)
    {_idx_map, ids, order, retain, report, body} =
      save_fields_retain(desc, value, idx_map, %{}, [], retain, report, [], [])

    {body, ids, order} = save_node_table(nodes, idx_map, ids, order, body)
    ids_bytes = for id <- Enum.reverse(order), into: <<>>, do: <<id::little-64>>
    {:ok, IO.iodata_to_binary([<<1>>, body, <<0>>, ids_bytes, <<length(order)::little-64>>]), retain, report}
  end

  defp save_fields_retain(desc, value, idx_map, ids, order, retain, report, path, acc) do
    {idx_map, ids, order, retain, report, acc} =
      Enum.reduce(desc.fields, {idx_map, ids, order, retain, report, acc}, fn f, {idx_map, ids, order, retain, report, acc} ->
        emit_field_retain(f, value, idx_map, ids, order, retain, report, path, acc)
      end)

    {ids, order, retain, report, acc} = emit_retain_tail(retain, ids, order, report, path, acc)
    {idx_map, ids, order, retain, report, acc}
  end

  defp emit_field_retain(%{t: :list, elem_t: :tab} = f, value, idx_map, ids, order, retain, report, path, acc) do
    case Map.get(value, f.name, []) do
      [] ->
        {idx_map, ids, order, retain, report, acc}

      elements ->
        {ref, ids, order} = intern(ids, order, f.id)
        elem_desc = Map.fetch!(@tables, f.elem)
        {body, idx_map, ids, order, retain, report} =
          save_elements_retain(elem_desc, elements, 0, idx_map, ids, order, retain, report, f.ordinal, path, [])

        array = [<<f.ek>>, leb_encode(length(elements)), body]
        {idx_map, ids, order, retain, report, [acc, leb_encode(ref), <<14>>, leb_encode(IO.iodata_length(array)), array]}
    end
  end

  defp emit_field_retain(%{t: :list} = f, value, idx_map, ids, order, retain, report, _path, acc) do
    {idx_map, ids, order, acc} = emit_field(f, value, idx_map, ids, order, acc)
    {idx_map, ids, order, retain, report, acc}
  end

  defp emit_field_retain(f, value, idx_map, ids, order, retain, report, _path, acc) do
    {idx_map, ids, order, acc} = emit_field(f, value, idx_map, ids, order, acc)
    {idx_map, ids, order, retain, report, acc}
  end

  defp save_elements_retain(_desc, [], _index, idx_map, ids, order, retain, report, _ordinal, _path, acc),
    do: {acc, idx_map, ids, order, retain, report}

  defp save_elements_retain(desc, [e | rest], index, idx_map, ids, order, retain, report, ordinal, path, acc) do
    elem_path = path ++ [{ordinal, index}]
    {idx_map, ids, order, retain, report, body} =
      save_fields_retain(desc, e, idx_map, ids, order, retain, report, elem_path, [])

    framed = [leb_encode(IO.iodata_length(body) + 1), body, <<0>>]
    save_elements_retain(desc, rest, index + 1, idx_map, ids, order, retain, report, ordinal, path, [acc, framed])
  end

  # emit_retain_tail re-emits the records whose path names the body being
  # written, in the order retained (§6.6).
  defp emit_retain_tail(retain, ids, order, report, path, acc) do
    Enum.reduce(retain.records, {ids, order, retain, report, acc}, fn rec, {ids, order, retain, report, acc} ->
      case rec do
        {^path, id, kind, resolved} ->
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

        _ ->
          {ids, order, retain, report, acc}
      end
    end)
  end

  # ---- the retaining read: decode with a caller-owned store (§6.6) ----

  defp decode_retain(desc, fields, nodes, ids, retain, report, path) do
    Enum.reduce_while(fields, {Map.new(), report, retain}, fn {id, kind, payload}, {value, report, retain} ->
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
            case retain_capture(ids, kind, payload) do
              {:ok, resolved} ->
                if retain_room(retain, path, resolved) do
                  retain = retain_put(retain, path, id, kind, resolved)
                  {:cont, {value, %{report | unknown: report.unknown + 1, retained: report.retained + 1}, retain}}
                else
                  {:cont, {value, %{report | unknown: report.unknown + 1, retain_lost: report.retain_lost + 1}, retain}}
                end

              :error ->
                {:cont, {value, %{report | unknown: report.unknown + 1, retain_lost: report.retain_lost + 1}, retain}}
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

  defp read_payload_retain(%{t: :list, elem_t: :tab} = f, payload, nodes, ids, report, retain, path) do
    case leb(payload) do
      {_len, arr} ->
        elem_desc = Map.fetch!(@tables, f.elem)

        case arr do
          <<ek, count_rest::binary>> when ek == f.ek ->
            case leb(count_rest) do
              {n, elems} ->
                {value, report, retain} = read_elements_retain(elem_desc, elems, n, nodes, ids, [], report, retain, f.ordinal, path)
                {:cont, value, report, retain}

              :error ->
                {:cont, [], %{report | malformed: true}, retain}
            end

          _ ->
            {:cont, [], %{report | kind_mismatch: report.kind_mismatch + 1}, retain}
        end

      :error ->
        {:cont, [], %{report | malformed: true}, retain}
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
                {:cont, nil, %{report | unknown: report.unknown + 1, retain_lost: report.retain_lost + 1}, retain}
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

  defp read_elements_retain(_desc, _rest, 0, _nodes, _ids, acc, report, retain, _ordinal, _path),
    do: {Enum.reverse(acc), report, retain}

  defp read_elements_retain(desc, rest, n, nodes, ids, acc, report, retain, ordinal, path) do
    case leb(rest) do
      {len, body} when byte_size(body) >= len ->
        sub = binary_part(body, 0, len)
        after_sub = binary_part(body, len, byte_size(body) - len)
        {value, report, retain} = read_element_retain(desc, sub, nodes, ids, report, retain, path, ordinal, length(acc))
        read_elements_retain(desc, after_sub, n - 1, nodes, ids, [value | acc], report, retain, ordinal, path)

      _ ->
        {Enum.reverse(acc), %{report | malformed: true}, retain}
    end
  end

  defp read_element_retain(desc, sub, nodes, ids, report, retain, path, ordinal, index) do
    elem_path = path ++ [{ordinal, index}]

    if ends_early?(sub, ids) do
      {%{}, malformed_report(), retain}
    else
      case entries(sub, ids, false, false) do
        {:ok, fields, []} ->
          decode_retain(desc, fields, nodes, ids, retain, report, elem_path)

        {:stop, fields, [], 0} ->
          {value, report, retain} = decode_retain(desc, fields, nodes, ids, retain, report, elem_path)
          {value, %{report | malformed: true}, retain}

        {:stop, fields, [], _remaining} ->
          {_value, report, retain} = decode_retain(desc, fields, nodes, ids, retain, report, elem_path)
          {%{}, %{report | malformed: true}, retain}

        _ ->
          {%{}, malformed_report(), retain}
      end
    end
  end

  # ---- the resolving walk, capture side (docs/SPEC-TABLES.md §6.6) ----

  defp retain_room(retain, path, resolved) do
    retain.used + 9 + 8 * length(path) + byte_size(resolved) <= retain.cap
  end

  defp retain_put(retain, path, id, kind, resolved) do
    cost = 9 + 8 * length(path) + byte_size(resolved)
    %{retain | records: retain.records ++ [{path, id, kind, resolved}], used: retain.used + cost}
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
          {len, rest} when byte_size(rest) >= len -> {:ok, byte_size(payload) - byte_size(rest) + len}
          _ -> :error
        end

      true ->
        :error
    end
  end

  defp retain_resolve(_ids, 17, _payload, _depth), do: :error

  defp retain_resolve(ids, 30, payload, _depth) do
    case leb(payload) do
      {0, rest} -> {:ok, <<0::little-64>>, rest}
      {r, rest} ->
        case retain_id(ids, r) do
          {:ok, id} -> {:ok, <<id::little-64>>, rest}
          :error -> :error
        end
      :error -> :error
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
      {:ok, n} -> {:ok, binary_part(payload, 0, n), binary_part(payload, n, byte_size(payload) - n)}
      :error -> :error
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
            {count, elems} -> retain_array(ids, kind, ek, count, elems, depth, <<ek, count::little-64>>)
            :error -> :error
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
            retain_array(ids, kind, ek, count - 1, rest2, depth, acc <> <<id::little-64, framed::binary>>)
          else
            _ -> :error
          end

        :error ->
          :error
      end
    else
      case retain_resolve(ids, ek, elems, depth) do
        {:ok, resolved, rest} -> retain_array(ids, kind, ek, count - 1, rest, depth, acc <> resolved)
        :error -> :error
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
      <<0::little-64, rest::binary>> -> {:ok, <<0>>, rest, ids, order, retain}
      <<id::little-64, rest::binary>> ->
        {ref, ids, order, retain} = retain_ref(ids, order, retain, id)
        if ref == 0, do: :error, else: {:ok, leb_encode(ref), rest, ids, order, retain}
      _ -> :error
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
            {:ok, framed, rest2, ids, order, retain} -> {:ok, leb_encode(ref) <> <<arm_kind>> <> framed, rest2, ids, order, retain}
            :error -> :error
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
      {:ok, n} -> {:ok, binary_part(resolved, 0, n), binary_part(resolved, n, byte_size(resolved) - n), ids, order, retain}
      :error -> :error
    end
  end

  defp retain_emit_framed(ids, order, retain, kind, resolved) do
    case resolved do
      <<len::little-64, sub::binary>> when byte_size(sub) >= len ->
        content = binary_part(sub, 0, len)
        rest = binary_part(sub, len, byte_size(sub) - len)

        case retain_emit_content(ids, order, retain, kind, content) do
          {:ok, emitted, ids, order, retain} -> {:ok, leb_encode(byte_size(emitted)) <> emitted, rest, ids, order, retain}
          :error -> :error
        end

      _ ->
        :error
    end
  end

  defp retain_emit_content(_ids, _order, _retain, 17, _sub), do: :error
  defp retain_emit_content(ids, order, retain, 13, sub), do: retain_emit_body(ids, order, retain, sub)

  defp retain_emit_content(ids, order, retain, kind, sub) when kind in [14, 16] do
    case sub do
      <<ek, count::little-64, elems::binary>> ->
        case retain_emit_array(ids, order, retain, kind, ek, count, elems, <<ek, leb_encode(count)::binary>>) do
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

  defp retain_emit_body(ids, order, retain, <<0::little-64>>), do: {:ok, <<0>>, ids, order, retain}

  defp retain_emit_body(ids, order, retain, <<id::little-64, kind, rest::binary>>) do
    {ref, ids, order, retain} = retain_ref(ids, order, retain, id)

    if ref == 0 do
      :error
    else
      case retain_emit(ids, order, retain, kind, rest) do
        {:ok, emitted, rest2, ids, order, retain} ->
          case retain_emit_body(ids, order, retain, rest2) do
            {:ok, more, ids, order, retain} -> {:ok, leb_encode(ref) <> <<kind>> <> emitted <> more, ids, order, retain}
            :error -> :error
          end

        :error ->
          :error
      end
    end
  end

  defp retain_emit_body(_ids, _order, _retain, _sub), do: :error

  defp retain_emit_array(ids, order, retain, _kind, _ek, 0, _elems, acc), do: {:ok, acc, ids, order, retain}

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
                retain_emit_array(ids, order, retain, kind, ek, count - 1, rest2, acc <> leb_encode(ref) <> framed)

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

// tableDescriptor spells one variable table's fields as data the engine walks
// (docs/SPEC-TABLES.md §3): each field's effective id, its wire kind, and the
// facts a codec needs — a string's bound, an int's clamp range, an enum's
// variant names, a list's element table, a *string's blob.
func tableDescriptor(u *ir.Unit, st *ir.Struct) string {
	var b strings.Builder
	ml := ir.RecordLayout(u, st)
	bytes, str := ir.PointerReachableBlobs(st)
	fmt.Fprintf(&b, "    %q => %%{size: %d, string_blob: %v, bytes_blob: %v, type_id: 0x%016X, fields: [\n", st.Name, ml.Size, str, bytes, ir.TableWireId(st.WireName()))
	for i, f := range st.Fields {
		b.WriteString("      " + fieldDescriptor(u, f, i))
	}
	b.WriteString("    ]},\n")
	return b.String()
}

func fieldDescriptor(u *ir.Unit, f *ir.Field, ordinal int) string {
	var b strings.Builder
	id := ir.TableFieldWireId(f)
	kind := ir.TableWireFieldKind(f)
	fmt.Fprintf(&b, "%%{name: %q, json: %q, id: 0x%016X, kind: %d, ordinal: %d", f.Name, ir.TableFieldJsonKey(f), id, kind, ordinal)
	switch {
	case f.CountedOnWire() && f.KeyEnum == "" && !f.IsMap():
		// A list is classified before a single pointer: []*T is an array of
		// node indexes, not one pointer. [..N]T is the same counted body as
		// []T (docs/SPEC-TABLES.md §2.1, §2.9, §3).
		writeListDescriptor(&b, u, f)
	case f.Type.Pointer && f.Type.Blob() && f.Type.Kind == ir.TBytes:
		// *bytes is a blob node under the reserved bytes id (docs/SPEC-TABLES.md §2.5).
		b.WriteString(", t: :bblob")
	case f.Type.Pointer && f.Type.Blob():
		b.WriteString(", t: :blob")
	case f.Type.Pointer && f.Type.Kind == ir.TNamed:
		// *T is a node index into the flat node table (docs/SPEC-TABLES.md §2.1, §3.1).
		name := f.Type.Name
		if st, ok := f.Type.Ref.(*ir.Struct); ok && st != nil && st.Name != "" {
			name = st.Name
		}
		if name != "" {
			b.WriteString(", t: :ptr")
			fmt.Fprintf(&b, ", table: %q", name)
		}
	case nestName(f) != "":
		// a table nested by value is kind 13, not a node (docs/SPEC-TABLES.md §3).
		b.WriteString(", t: :nest")
		fmt.Fprintf(&b, ", table: %q", nestName(f))
	case enumRef(f) != nil:
		b.WriteString(", t: :enum, by_name: %{")
		e := enumRef(f)
		for i, v := range e.Variants {
			fmt.Fprintf(&b, "%q => 0x%016X, ", v, ir.TableWireId(e.VariantWireName(i)))
		}
		b.WriteString("}, by_id: %{")
		for i, v := range e.Variants {
			fmt.Fprintf(&b, "0x%016X => %q, ", ir.TableWireId(e.VariantWireName(i)), v)
		}
		b.WriteString("}")
	case f.Type.Kind == ir.TString:
		b.WriteString(", t: :string")
		fmt.Fprintf(&b, ", size: %d", f.Type.Size)
	case f.Type.Kind == ir.TInt:
		b.WriteString(", t: :int")
		fmt.Fprintf(&b, ", width: %d, signed: %v", f.Type.Width/8, f.Type.Signed)
		if f.HasDefault && f.DefInt != nil {
			fmt.Fprintf(&b, ", default: %s", f.DefInt.String())
		}
		if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
			fmt.Fprintf(&b, ", min: %s, max: %s", f.IntMin.String(), f.IntMax.String())
		}
	case f.Type.Kind == ir.TBool:
		b.WriteString(", t: :bool")
	case f.Type.Kind == ir.TBits:
		// bits(N) stays uncarried. The key has to be present: without it,
		// save raised key :t not found (channel bits(6)).
		b.WriteString(", t: :bits")
	}
	fmt.Fprintf(&b, ", shape: %s, bound: %s", cardShape(f), cardBound(f))
	b.WriteString("},\n")
	return b.String()
}

// cardShape is the descriptor's shape column (docs/SPEC-TABLES.md §8.1):
// :value for a scalar or enum field, :list for an unbounded array, :blob for a
// *string. It is the one column a tool keys a walk on without naming a type.
func cardShape(f *ir.Field) string {
	switch {
	case f.IsList():
		return ":list"
	case f.Type.Pointer && f.Type.Blob():
		return ":blob"
	default:
		return ":value"
	}
}

// writeListDescriptor spells one []T or []*T field (docs/SPEC-TABLES.md §2.9,
// §3). elem_t says how an element rides: :tab is a framed table body, :ptr is
// a node index, :int is a packed scalar, and :other is a class this codec
// does not carry yet.
func writeListDescriptor(b *strings.Builder, u *ir.Unit, f *ir.Field) {
	b.WriteString(", t: :list")
	fmt.Fprintf(b, ", ek: %d", ir.TableWireElemKind(f))
	elemSize, elemAlign := ir.ListElementLayout(u, f)
	fmt.Fprintf(b, ", elem_size: %d, elem_align: %d", elemSize, elemAlign)
	name, isStruct := listStructName(f)
	switch {
	case f.Type.Pointer && isStruct:
		b.WriteString(", elem_t: :ptr")
		fmt.Fprintf(b, ", elem: %q", name)
	case isStruct:
		b.WriteString(", elem_t: :tab")
		fmt.Fprintf(b, ", elem: %q", name)
	case f.Type.Kind == ir.TInt:
		b.WriteString(", elem_t: :int")
		fmt.Fprintf(b, ", width: %d, signed: %v", f.Type.Width/8, f.Type.Signed)
		if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
			fmt.Fprintf(b, ", min: %s, max: %s", f.IntMin.String(), f.IntMax.String())
		}
	default:
		b.WriteString(", elem_t: :other")
		if f.Type.Name != "" {
			fmt.Fprintf(b, ", elem: %q", f.Type.Name)
		}
	}
}

// nestName is the table a by-value field nests, or "" when the field is not
// one (docs/SPEC-TABLES.md §3). A pointer, a list and a keyed array are not.
func nestName(f *ir.Field) string {
	if f == nil || f.Type.Pointer || f.Array != ir.ArrayNone || f.KeyEnum != "" || f.IsMap() {
		return ""
	}
	if f.Type.Kind != ir.TNamed {
		return ""
	}
	st, ok := f.Type.Ref.(*ir.Struct)
	if !ok || st == nil {
		return ""
	}
	if st.Name != "" {
		return st.Name
	}
	return f.Type.Name
}

func listStructName(f *ir.Field) (string, bool) {
	if f.Type.Kind != ir.TNamed {
		return "", false
	}
	st, ok := f.Type.Ref.(*ir.Struct)
	if !ok || st == nil {
		return "", false
	}
	if st.Name != "" {
		return st.Name, true
	}
	return f.Type.Name, f.Type.Name != ""
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
