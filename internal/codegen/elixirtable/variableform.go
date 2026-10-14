// THE VARIABLE FORM for Elixir (docs/SPEC-TABLES.md §3): form byte 1, the
// id-table wire, for exactly the constructs the card record uses — a plain
// `table` root and tables as list elements; bool, int32, int64 and enum with
// their defaults and ranges; string(N); *string (the string blob, §2.5); and
// unbounded lists of tables []T (§2.9). The rest of the variable class is
// refused by name at the backend's boundary (§11) and never reaches here.
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
	// The descriptors cover the WHOLE table closure (docs/SPEC-TABLES.md §3.1):
	// a variable root reaches a fixed table by value and by pointer, so a node
	// of that type has to be named beside the variable ones. The nil gate above
	// keeps a fixed-only unit from emitting the variable form at all.
	closure := ir.TableClosure(u)
	names := make([]string, 0, len(closure))
	for name := range closure {
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
	b.WriteString("  # the node table's type vocabulary (docs/SPEC-TABLES.md §3.1): the id a\n")
	b.WriteString("  # record's type reference resolves to — a blob kind or a table name — so a\n")
	b.WriteString("  # pointer names the node it points at.\n")
	b.WriteString("  @node_types %{\n")
	fmt.Fprintf(&b, "    0x%016X => :bytes,\n", ir.BytesWireTypeId)
	fmt.Fprintf(&b, "    0x%016X => :string,\n", ir.StringWireTypeId)
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

  def save(table, value) when is_map(value) do
    desc = Map.fetch!(@tables, table)

    # the node table's records in §3.1's first-visit order, one record per
    # DISTINCT node: a node two fields share is numbered once, so the saved
    # wire matches the reference backends byte for byte (§3.1)
    nodes = collect_nodes(desc, value, []) |> Enum.reverse()
    idx_map = nodes |> Enum.with_index(2) |> Map.new(fn {n, i} -> {node_content(n), i} end)

    {_idx_map, ids, order, body} = save_fields(desc, value, idx_map, %{}, [], [])

    {body, _ids, order} = save_node_table(nodes, idx_map, ids, order, body)

    ids_bytes = for id <- Enum.reverse(order), into: <<>>, do: <<id::little-64>>

    {:ok, IO.iodata_to_binary([<<1>>, body, <<0>>, ids_bytes, <<length(order)::little-64>>])}
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
            v -> push_node(acc, {f.type_id, :blob, nil, v})
          end

        :pointer ->
          case Map.get(value, f.name) do
            nil -> acc
            v ->
              acc = push_node(acc, {f.type_id, :table, f.elem, v})
              collect_nodes(Map.fetch!(@tables, f.elem), v, acc)
          end

        :table ->
          collect_nodes(Map.fetch!(@tables, f.elem), Map.get(value, f.name, %{}), acc)

        :list ->
          collect_list_nodes(f, Map.get(value, f.name, []), acc)

        _ ->
          acc
      end
    end)
  end

  defp push_node(acc, node) do
    c = node_content(node)

    if Enum.any?(acc, fn n -> node_content(n) == c end), do: acc, else: [node | acc]
  end

  defp node_content({_type_id, _kind, _ref, c}), do: c

  # an unbounded array's elements are inline (kind 13) when they are tables and
  # nodes when they are pointers; scalar and string elements reach no node.
  defp collect_list_nodes(%{elem_kind: 13} = f, elements, acc) do
    Enum.reduce(elements, acc, fn e, acc ->
      collect_nodes(Map.fetch!(@tables, f.elem), e, acc)
    end)
  end

  defp collect_list_nodes(%{elem_kind: 17} = f, elements, acc) do
    Enum.reduce(elements, acc, fn
      nil, acc -> acc
      e, acc ->
        acc = push_node(acc, {f.elem_type_id, :table, f.elem, e})
        collect_nodes(Map.fetch!(@tables, f.elem), e, acc)
    end)
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
        idx = Map.fetch!(idx_map, v)
        {idx_map, ids, order, [acc, leb_encode(ref), <<f.kind>>, leb_encode(idx)]}
    end
  end

  defp emit_field(%{t: :list} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name, []) do
      [] ->
        {idx_map, ids, order, acc}

      elements ->
        {ref, ids, order} = intern(ids, order, f.id)
        {body, idx_map, ids, order} = save_list_elements(f, elements, idx_map, ids, order, [])
        array = [<<f.elem_kind>>, leb_encode(length(elements)), body]
        {idx_map, ids, order, [acc, leb_encode(ref), <<14>>, leb_encode(IO.iodata_length(array)), array]}
    end
  end

  defp emit_field(%{t: :pointer} = f, value, idx_map, ids, order, acc) do
    case Map.get(value, f.name) do
      nil ->
        {idx_map, ids, order, acc}

      v ->
        {ref, ids, order} = intern(ids, order, f.id)
        idx = Map.fetch!(idx_map, v)
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

        if IO.iodata_length(body) == 0 do
          # an all-default nested table rides nothing (§3): the field and its
          # speculative ids roll back, exactly as the reference's truncate does
          {idx_map, ids, order, acc}
        else
          framed = [body, <<0>>]
          {idx_map, ids2, order2, [acc, leb_encode(ref), <<13>>, leb_encode(IO.iodata_length(framed)), framed]}
        end
    end
  end

  defp emit_field(%{t: :bytes} = f, value, idx_map, ids, order, acc) do
    v = Map.get(value, f.name, f.default)

    if v == f.default do
      {idx_map, ids, order, acc}
    else
      {ref, ids, order} = intern(ids, order, f.id)
      array = [<<8>>, leb_encode(byte_size(v)), v]
      {idx_map, ids, order, [acc, leb_encode(ref), <<14>>, leb_encode(IO.iodata_length(array)), array]}
    end
  end

  defp emit_field(%{t: :enum} = f, value, idx_map, ids, order, acc) do
    v = Map.get(value, f.name, f.default)

    if v == f.default do
      {idx_map, ids, order, acc}
    else
      vid = Map.fetch!(f.by_name, v)
      {ref, ids, order} = intern(ids, order, f.id)
      {vref, ids, order} = intern(ids, order, vid)
      {idx_map, ids, order, [acc, leb_encode(ref), <<30>>, leb_encode(vref)]}
    end
  end

  defp emit_field(%{t: :int} = f, value, idx_map, ids, order, acc) do
    v = Map.get(value, f.name, f.default)

    if v == f.default do
      {idx_map, ids, order, acc}
    else
      {ref, ids, order} = intern(ids, order, f.id)
      {idx_map, ids, order, [acc, leb_encode(ref), <<f.kind>>, int_bytes(v, f.width, f.signed)]}
    end
  end

  defp emit_field(%{t: :float} = f, value, idx_map, ids, order, acc) do
    v = Map.get(value, f.name, f.default)

    if v == f.default do
      {idx_map, ids, order, acc}
    else
      {ref, ids, order} = intern(ids, order, f.id)
      {idx_map, ids, order, [acc, leb_encode(ref), <<f.kind>>, float_bytes(v, f.width)]}
    end
  end

  defp emit_field(%{t: :string} = f, value, idx_map, ids, order, acc) do
    v = Map.get(value, f.name, f.default)

    if v == f.default do
      {idx_map, ids, order, acc}
    else
      {ref, ids, order} = intern(ids, order, f.id)
      {idx_map, ids, order, [acc, leb_encode(ref), <<f.kind>>, leb_encode(byte_size(v)), v]}
    end
  end

  defp emit_field(%{t: :bool} = f, value, idx_map, ids, order, acc) do
    v = Map.get(value, f.name, f.default)

    if v == f.default do
      {idx_map, ids, order, acc}
    else
      {ref, ids, order} = intern(ids, order, f.id)
      {idx_map, ids, order, [acc, leb_encode(ref), <<1>>, <<1>>]}
    end
  end

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

  defp emit_element(%{elem_kind: 17} = f, e, idx_map, ids, order) do
    idx = Map.fetch!(idx_map, e)
    {leb_encode(idx), ids, order}
  end

  defp emit_element(%{elem_kind: 12} = f, e, _idx_map, ids, order) do
    {[leb_encode(byte_size(e)), e], ids, order}
  end

  defp emit_element(%{elem_kind: 30} = f, e, _idx_map, ids, order) do
    vid = Map.fetch!(f.by_name, e)
    {vref, ids, order} = intern(ids, order, vid)
    {leb_encode(vref), ids, order}
  end

  defp emit_element(%{elem_kind: 1} = _f, e, _idx_map, ids, order) do
    {if(e, do: <<1>>, else: <<0>>), ids, order}
  end

  defp emit_element(%{elem_kind: kind} = f, e, _idx_map, ids, order) when kind in [10, 11] do
    {float_bytes(e, kind_width(kind)), ids, order}
  end

  defp emit_element(%{elem_kind: kind} = f, e, _idx_map, ids, order) when kind in [2, 3, 4, 5, 6, 7, 8, 9] do
    {int_bytes(e, kind_width(kind), kind < 6), ids, order}
  end

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

  defp node_body({_type_id, :table, ref, v}, idx_map, ids, order) do
    desc = Map.fetch!(@tables, ref)
    {_idx, ids, order, body} = save_fields(desc, v, idx_map, ids, order, [])
    {IO.iodata_to_binary([body, <<0>>]), ids, order}
  end

  # ---- the read side (docs/SPEC-TABLES.md §3, §4) ----

  def load(table, data) when is_binary(data) do
    desc = Map.fetch!(@tables, table)

    case open(data) do
      {:error, reason} ->
        {:error, reason, empty_report()}

      {:ok, body, ids} ->
        case entries(body, ids, desc.string_blob) do
          {:error, _} ->
            {:error, :malformed, empty_report()}

          {:stop, _fields, _nodes, _remaining} ->
            # a ROOT body the walk could not finish is malformed; nothing below
            # the damage is read, so there is no value to save (§3)
            {:error, :malformed, empty_report()}

          {:ok, fields, nodes} ->
            {value, report} = decode(desc, fields, nodes, ids)
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
  defp entries(bin, ids, string_blob), do: entries(bin, ids, string_blob, [], [])

  # entries returns {:ok, fields, nodes} when the body ends at its terminator,
  # or {:stop, fields, nodes, remaining} when the walk stops, the fourth item
  # being the bytes the walk left un-consumed: 0 means the body ran off its own
  # end cleanly, which is the one stop that keeps the decoded prefix (§3).
  defp entries(bin, ids, string_blob, acc, nodes) do
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
                          case node_table(payload, ids, string_blob) do
                            {:ok, ns} -> entries(rest3, ids, string_blob, acc, nodes ++ ns)
                            :error -> {:stop, Enum.reverse(acc), nodes, byte_size(rest2)}
                          end
                        else
                          entries(rest3, ids, string_blob, acc, nodes)
                        end

                      else
                        entries(rest3, ids, string_blob, [{id, kind, payload} | acc], nodes)
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
        {:table, name, body} -> decode_node_table(nodes, name, body, ids)
        _ -> :null
      end
    end
  end

  defp decode_node_table(nodes, name, body, ids) do
    desc = Map.fetch!(@tables, name)

    case entries(body, ids, desc.string_blob) do
      {:ok, fields, []} ->
        {value, _report} = decode(desc, fields, nodes, ids)
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
            {:cont, bytes, report}

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
        elem_desc = Map.fetch!(@tables, f.elem)

        case entries(body, ids, elem_desc.string_blob) do
          {:ok, fields, []} ->
            {value, report} = decode(elem_desc, fields, nodes, ids)
            {:cont, value, report}

          _ ->
            {:cont, %{}, %{report | malformed: true}}
        end

      :error ->
        {:cont, %{}, %{report | malformed: true}}
    end
  end

  # bytes(N) rides as an array of u8 (kind 14, element kind 8): L then the count
  # and the bytes (docs/SPEC-TABLES.md §3). A count past the declared size clamps.
  defp read_payload(%{t: :bytes} = f, payload, _nodes, _ids, report) do
    case leb(payload) do
      {_len, arr} ->
        case arr do
          <<8, count_rest::binary>> ->
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
    {:cont, float_of(payload, f.width), report}
  end

  defp read_payload(%{t: :list} = f, payload, nodes, ids, report) do
    case leb(payload) do
      {_len, arr} ->
        case arr do
          <<elem_kind, count_rest::binary>> ->
            if elem_kind == f.elem_kind do
              case leb(count_rest) do
                {n, elems} ->
                  {value, report} = read_list_elements(f, elems, n, nodes, ids, [], report)
                  {:cont, value, report}

                :error ->
                  {:cont, [], %{report | malformed: true}}
              end
            else
              {:cont, [], %{report | kind_mismatch: report.kind_mismatch + 1}}
            end

          _ ->
            {:cont, [], %{report | kind_mismatch: report.kind_mismatch + 1}}
        end

      :error ->
        {:cont, [], %{report | malformed: true}}
    end
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
  defp kind_width(6), do: 1
  defp kind_width(7), do: 2
  defp kind_width(8), do: 4
  defp kind_width(9), do: 8
  defp kind_width(10), do: 4
  defp kind_width(11), do: 8
  defp kind_width(_), do: 0

  # read_list_elements decodes one unbounded array's elements by the list's
  # element kind (docs/SPEC-TABLES.md §2.9, §3): a table element is a framed
  # body, a pointer element a node index, a string element its L and bytes, an
  # enum element a variant reference, and every scalar element its own bytes.
  defp read_list_elements(_f, _rest, 0, _nodes, _ids, acc, report), do: {Enum.reverse(acc), report}

  defp read_list_elements(%{elem_kind: 13} = f, rest, n, nodes, ids, acc, report) do
    case leb(rest) do
      {len, body} when byte_size(body) >= len ->
        sub = binary_part(body, 0, len)
        after_sub = binary_part(body, len, byte_size(body) - len)

        {value, elem_report} = read_element(Map.fetch!(@tables, f.elem), sub, nodes, ids)
        read_list_elements(f, after_sub, n - 1, nodes, ids, [value | acc], merge_report(report, elem_report))

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

      {idx, after_sub} ->
        value =
          case node_value(nodes, idx, ids) do
            {:value, v} when is_map(v) -> v
            _ -> nil
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
                :error -> nil
              end
          end

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

  defp read_list_elements(%{elem_kind: kind} = f, rest, n, nodes, ids, acc, report) when kind in [10, 11] do
    w = kind_width(kind)

    if byte_size(rest) >= w do
      <<raw::binary-size(w), after_sub::binary>> = rest
      read_list_elements(f, after_sub, n - 1, nodes, ids, [float_of(raw, w) | acc], report)
    else
      {Enum.reverse(acc), %{report | malformed: true}}
    end
  end

  defp read_list_elements(%{elem_kind: kind} = f, rest, n, nodes, ids, acc, report) when kind in [2, 3, 4, 5, 6, 7, 8, 9] do
    w = kind_width(kind)
    signed = kind < 6

    if byte_size(rest) >= w do
      <<raw::binary-size(w), after_sub::binary>> = rest
      read_list_elements(f, after_sub, n - 1, nodes, ids, [int_of(raw, w, signed) | acc], report)
    else
      {Enum.reverse(acc), %{report | malformed: true}}
    end
  end

  defp read_list_elements(_f, _rest, _n, _nodes, _ids, acc, report), do: {Enum.reverse(acc), report}

  # read_element decodes one []T element (docs/SPEC-TABLES.md §2.9, §4). A body
  # that cannot be read whole — its L does not frame it, its terminator is not
  # its last byte, or its walk stops on damage — reads the element's DECLARED
  # DEFAULTS and one malformed counts, and the list reads on past it. A clean
  # body reads its value, and an event inside it folds into the list's report.
  defp read_element(desc, sub, nodes, ids) do
    if ends_early?(sub, ids) do
      {%{}, malformed_report()}
    else
      case entries(sub, ids, false) do
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

  defp int_bytes(v, 1, true), do: <<v::little-signed-8>>
  defp int_bytes(v, 2, true), do: <<v::little-signed-16>>
  defp int_bytes(v, 4, true), do: <<v::little-signed-32>>
  defp int_bytes(v, 8, true), do: <<v::little-signed-64>>
  defp int_bytes(v, 1, false), do: <<v::little-unsigned-8>>
  defp int_bytes(v, 2, false), do: <<v::little-unsigned-16>>
  defp int_bytes(v, 4, false), do: <<v::little-unsigned-32>>
  defp int_bytes(v, 8, false), do: <<v::little-unsigned-64>>

  defp float_bytes(v, 4), do: <<v::little-float-32>>
  defp float_bytes(v, 8), do: <<v::little-float-64>>

  defp float_of(<<v::little-float-32>>, 4), do: v
  defp float_of(<<v::little-float-64>>, 8), do: v
  defp float_of(_payload, _w), do: 0.0

  defp int_of(<<v::little-signed-8>>, 1, true), do: v
  defp int_of(<<v::little-signed-16>>, 2, true), do: v
  defp int_of(<<v::little-signed-32>>, 4, true), do: v
  defp int_of(<<v::little-signed-64>>, 8, true), do: v
  defp int_of(<<v::little-unsigned-8>>, 1, false), do: v
  defp int_of(<<v::little-unsigned-16>>, 2, false), do: v
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
  # A value is SPARSE — only the fields the wire carried — so each level folds
  # the descriptor's defaults back in before a field is read.
  defp json_object(desc, value, depth) do
    value = Map.merge(defaults(desc), value)

    entries =
      Enum.map(desc.fields, fn f ->
        ["\n", indent(depth + 1), "\"", json_escape(f.json), "\": ",
         json_field(f, Map.get(value, f.name), depth + 1)]
      end)

    ["{", Enum.intersperse(entries, ","), "\n", indent(depth), "}"]
  end

  defp json_field(%{t: :list} = f, elements, depth) when is_list(elements) do
    case elements do
      [] ->
        "[]"

      _ ->
        rows =
          Enum.map(elements, fn e ->
            ["\n", indent(depth + 1), json_list_elem(f, e, depth + 1)]
          end)

        ["[", Enum.intersperse(rows, ","), "\n", indent(depth), "]"]
    end
  end

  defp json_field(%{t: :list}, _other, _depth), do: "[]"

  # json_list_elem writes one unbounded array's element by its element kind
  # (docs/SPEC-TABLES.md §2.9, §16.7): a table or pointer element is an object, a
  # string or enum a quoted name, and a scalar its own spelling.
  defp json_list_elem(%{elem_kind: 13} = f, e, depth), do: json_object(Map.fetch!(@tables, f.elem), e, depth)
  defp json_list_elem(%{elem_kind: 17} = f, nil, _depth), do: "null"
  defp json_list_elem(%{elem_kind: 17} = f, e, depth), do: json_object(Map.fetch!(@tables, f.elem), e, depth)
  defp json_list_elem(%{elem_kind: 12}, e, _depth), do: ["\"", json_escape(e), "\""]
  defp json_list_elem(%{elem_kind: 30}, nil, _depth), do: "\"None\""
  defp json_list_elem(%{elem_kind: 30}, e, _depth), do: ["\"", json_escape(e), "\""]
  defp json_list_elem(%{elem_kind: 1}, e, _depth), do: if(e, do: "true", else: "false")
  defp json_list_elem(%{elem_kind: kind}, e, _depth) when kind in [2, 3, 4, 5, 6, 7, 8, 9], do: Integer.to_string(e)
  defp json_list_elem(%{elem_kind: kind}, e, _depth) when kind in [10, 11], do: json_float(e)
  defp json_list_elem(_f, _e, _depth), do: "null"

  defp json_field(%{t: :string}, v, _depth), do: ["\"", json_escape(v), "\""]
  defp json_field(%{t: :table} = f, v, depth), do: json_object(Map.fetch!(@tables, f.elem), v || %{}, depth)
  defp json_field(%{t: :pointer} = f, nil, _depth), do: "null"
  defp json_field(%{t: :pointer} = f, v, depth), do: json_object(Map.fetch!(@tables, f.elem), v, depth)
  defp json_field(%{t: :blob}, nil, _depth), do: "null"
  defp json_field(%{t: :blob, blob: :bytes}, v, _depth), do: ["\"", Base.encode64(v), "\""]
  defp json_field(%{t: :blob}, v, _depth), do: ["\"", json_escape(v), "\""]
  defp json_field(%{t: :bytes}, v, _depth), do: ["\"", Base.encode64(v), "\""]
  defp json_field(%{t: :enum}, nil, _depth), do: "\"None\""
  defp json_field(%{t: :enum}, name, _depth), do: ["\"", json_escape(name), "\""]
  defp json_field(%{t: :int}, v, _depth), do: Integer.to_string(v)
  defp json_field(%{t: :float}, v, _depth), do: json_float(v)
  defp json_field(%{t: :bool}, v, _depth), do: if(v, do: "true", else: "false")

  # json_float spells one float the way the reference does (§16.7): shortest
  # round-trip, a trailing ".0" dropped so 2.0 writes "2".
  defp json_float(v) do
    s = Float.to_string(v)
    if String.contains?(s, "."), do: String.trim_trailing(String.trim_trailing(s, "0"), "."), else: s
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

  defp json_read_field(%{t: :blob, blob: :bytes}, raw, report) when is_binary(raw) do
    case Base.decode64(raw) do
      {:ok, bytes} -> {bytes, report}
      :error -> {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}
    end
  end

  defp json_read_field(%{t: :blob}, raw, report) when is_binary(raw), do: {raw, report}
  defp json_read_field(%{t: :blob}, nil, report), do: {nil, report}

  defp json_read_field(%{t: :blob}, _raw, report),
    do: {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

  defp json_read_field(%{t: :bytes} = f, raw, report) when is_binary(raw) do
    case Base.decode64(raw) do
      {:ok, bytes} when byte_size(bytes) <= f.size ->
        {bytes, report}

      {:ok, bytes} ->
        {binary_part(bytes, 0, f.size), %{report | clamped: report.clamped + 1}}

      :error ->
        {<<>>, %{report | kind_mismatch: report.kind_mismatch + 1}}
    end
  end

  defp json_read_field(%{t: :bytes}, _raw, report),
    do: {<<>>, %{report | kind_mismatch: report.kind_mismatch + 1}}

  defp json_read_field(%{t: :table} = f, raw, report) when is_map(raw) do
    {v, elem_report} = json_fields(Map.fetch!(@tables, f.elem), raw)
    {v, merge_report(report, elem_report)}
  end

  defp json_read_field(%{t: :table}, _raw, report),
    do: {%{}, %{report | kind_mismatch: report.kind_mismatch + 1}}

  defp json_read_field(%{t: :pointer} = f, nil, report), do: {nil, report}

  defp json_read_field(%{t: :pointer} = f, raw, report) when is_map(raw) do
    {v, elem_report} = json_fields(Map.fetch!(@tables, f.elem), raw)
    {v, merge_report(report, elem_report)}
  end

  defp json_read_field(%{t: :pointer}, _raw, report),
    do: {nil, %{report | kind_mismatch: report.kind_mismatch + 1}}

  defp json_read_field(%{t: :float}, raw, report) when is_number(raw), do: {raw * 1.0, report}

  defp json_read_field(%{t: :float}, _raw, report),
    do: {0.0, %{report | kind_mismatch: report.kind_mismatch + 1}}

  defp json_read_field(%{t: :list} = f, raw, report) when is_list(raw) do
    {elems, report} =
      Enum.reduce(raw, {[], report}, fn e, {acc, report} ->
        case json_read_list_elem(f, e, report) do
          {:skip, report} -> {acc, report}
          {v, report} -> {[v | acc], report}
        end
      end)

    {Enum.reverse(elems), report}
  end

  defp json_read_field(%{t: :list}, _raw, report),
    do: {[], %{report | kind_mismatch: report.kind_mismatch + 1}}

  # json_read_list_elem reads one unbounded array's element by its element kind
  # (docs/SPEC-TABLES.md §2.9, §16.7), folding the element's report into the list's.
  defp json_read_list_elem(%{elem_kind: 13} = f, e, report) when is_map(e) do
    {v, elem_report} = json_fields(Map.fetch!(@tables, f.elem), e)
    {v, merge_report(report, elem_report)}
  end

  defp json_read_list_elem(%{elem_kind: 17} = f, nil, report), do: {nil, report}

  defp json_read_list_elem(%{elem_kind: 17} = f, e, report) when is_map(e) do
    {v, elem_report} = json_fields(Map.fetch!(@tables, f.elem), e)
    {v, merge_report(report, elem_report)}
  end

  defp json_read_list_elem(%{elem_kind: 12} = f, e, report) when is_binary(e) do
    if byte_size(e) > f.size do
      {binary_part(e, 0, f.size), %{report | clamped: report.clamped + 1}}
    else
      {e, report}
    end
  end

  defp json_read_list_elem(%{elem_kind: 30} = f, e, report) when is_binary(e) do
    case e do
      "None" -> {nil, report}
      _ ->
        case Map.fetch(f.by_name, e) do
          {:ok, _id} -> {e, report}
          :error -> {nil, %{report | unknown: report.unknown + 1}}
        end
    end
  end

  defp json_read_list_elem(%{elem_kind: 1}, e, report) when is_boolean(e), do: {e, report}
  defp json_read_list_elem(%{elem_kind: kind}, e, report) when kind in [2, 3, 4, 5, 6, 7, 8, 9] and is_integer(e), do: {e, report}
  defp json_read_list_elem(%{elem_kind: kind}, e, report) when kind in [10, 11] and is_number(e), do: {e * 1.0, report}
  defp json_read_list_elem(_f, _e, report), do: {:skip, %{report | kind_mismatch: report.kind_mismatch + 1}}

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
      <<13, count_rest::binary>> ->
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
                elem_desc = Map.fetch!(@tables, field.elem)
                elements_extent(elem_desc, elems, count, ids, at)
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
        case entries(body, ids, desc.string_blob) do
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
    idx_map = nodes |> Enum.with_index(2) |> Map.new(fn {n, i} -> {node_content(n), i} end)
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

  defp emit_field_retain(%{t: :list} = f, value, idx_map, ids, order, retain, report, path, acc) do
    case Map.get(value, f.name, []) do
      [] ->
        {idx_map, ids, order, retain, report, acc}

      elements ->
        {ref, ids, order} = intern(ids, order, f.id)
        elem_desc = Map.fetch!(@tables, f.elem)
        {body, idx_map, ids, order, retain, report} =
          save_elements_retain(elem_desc, elements, 0, idx_map, ids, order, retain, report, f.ordinal, path, [])

        array = [<<13>>, leb_encode(length(elements)), body]
        {idx_map, ids, order, retain, report, [acc, leb_encode(ref), <<14>>, leb_encode(IO.iodata_length(array)), array]}
    end
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

  defp read_payload_retain(%{t: :list} = f, payload, nodes, ids, report, retain, path) do
    case leb(payload) do
      {_len, arr} ->
        elem_desc = Map.fetch!(@tables, f.elem)

        case arr do
          <<13, count_rest::binary>> ->
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
      case entries(sub, ids, false) do
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

// tableDescriptor spells one table's fields as data the engine walks
// (docs/SPEC-TABLES.md §3): each field's effective id, its wire kind, and the
// facts a codec needs — a string's bound, an int's clamp range, an enum's
// variant names, a list's element table, a *string/*bytes blob's type id, a
// pointer's target. The descriptor covers the closure, fixed tables included,
// because a pointer can name one.
func tableDescriptor(u *ir.Unit, st *ir.Struct) string {
	var b strings.Builder
	ml := ir.RecordLayout(u, st)
	_, str := ir.PointerReachableBlobs(st)
	fmt.Fprintf(&b, "    %q => %%{size: %d, string_blob: %v, fields: [\n", st.Name, ml.Size, str)
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
	case f.Type.Pointer && f.Type.Blob():
		blob := "bytes"
		if f.Type.Kind == ir.TString {
			blob = "string"
		}
		fmt.Fprintf(&b, ", t: :blob, blob: :%s, type_id: 0x%016X", blob, ir.BlobWireTypeId(f))
	case f.Array != ir.ArrayNone:
		elemKind := ir.TableWireElemKind(f)
		fmt.Fprintf(&b, ", t: :list, elem_kind: %d", elemKind)
		if f.Array != ir.ArrayList {
			fmt.Fprintf(&b, ", bound: %d", f.ArrayBound)
		}
		switch elemKind {
		case ir.TableKindTable:
			fmt.Fprintf(&b, ", elem: %q", f.Type.Name)
		case ir.TableKindPointer:
			ref := f.Type.Ref.(*ir.Struct)
			fmt.Fprintf(&b, ", elem: %q, elem_type_id: 0x%016X", ref.Name, ir.TableWireId(ref.WireName()))
		case ir.TableKindEnum:
			writeEnumMaps(&b, enumRef(f))
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
	case f.Type.Kind == ir.TBytes:
		fmt.Fprintf(&b, ", t: :bytes, size: %d", f.Type.Size)
	case enumRef(f) != nil:
		b.WriteString(", t: :enum")
		writeEnumMaps(&b, enumRef(f))
	case f.Type.Kind == ir.TString:
		b.WriteString(", t: :string")
		fmt.Fprintf(&b, ", size: %d", f.Type.Size)
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits:
		b.WriteString(", t: :int")
		width := (f.Type.Width + 7) / 8
		fmt.Fprintf(&b, ", width: %d, signed: %v", width, f.Type.Signed)
		if f.Type.Kind == ir.TBits {
			fmt.Fprintf(&b, ", bits: %d", f.Type.Width)
		}
		if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
			fmt.Fprintf(&b, ", min: %s, max: %s", f.IntMin.String(), f.IntMax.String())
		}
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		b.WriteString(", t: :float")
		fmt.Fprintf(&b, ", width: %d", f.Type.Width/8)
	case f.Type.Kind == ir.TBool:
		b.WriteString(", t: :bool")
	}
	fmt.Fprintf(&b, ", shape: %s, bound: %s", cardShape(f), cardBound(f))
	fmt.Fprintf(&b, ", default: %s", variableFieldDefault(f))
	b.WriteString("},\n")
	return b.String()
}

// variableFieldDefault is a field's declared default as an Elixir literal, the
// value an absent field reads and a text renders (§4, §16.1): the declared
// default overrides the type's zero, exactly as the reference's prefill does.
func variableFieldDefault(f *ir.Field) string {
	switch {
	case f.Array != ir.ArrayNone:
		return "[]"
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
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits:
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
