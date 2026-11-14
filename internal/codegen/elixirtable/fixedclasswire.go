package elixirtable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// withFixedClassWire makes a declared fixed root's table-wire load and save
// the fixed form (docs/SPEC-TABLES.md §3, §3.4). Form 3 is that root's file.
// Form 1 is previous_form and nothing is decoded. A read returns the one
// record — the whole value — or a refusal, never a prefix of the batch.
// Variable roots stay on the id-table codec below these clauses.
func withFixedClassWire(u *ir.Unit, ns, runtime string) string {
	roots := fixedRoots(u)
	fixedNames := map[string]bool{}
	for _, st := range roots {
		fixedNames[st.Name] = true
	}
	var noFile []*ir.Struct
	for _, st := range ir.TableFixedRoots(u) {
		if !fixedNames[st.Name] {
			noFile = append(noFile, st)
		}
	}
	var save, load strings.Builder
	if len(roots) > 0 {
		save.WriteString("  # A declared fixed root's file is form 3 (docs/SPEC-TABLES.md §3, §3.4).\n")
		save.WriteString("  # The write is the fixed form's own. The variable codec below is for\n")
		save.WriteString("  # variable roots, including one that holds a fixed table.\n\n")
		save.WriteString(fixedClassHelpers(u))
	}
	load.WriteString(strictHelpers)
	// THE ONE READ (docs/plans/strict-only.md; docs/SPEC-TABLES.md §4): load/2
	// returns the whole value or refuses the whole load. A declared fixed root
	// with no file form refuses every file wire (§3.4). Its clauses sit with
	// the other load clauses, just before the variable read's own.
	if len(noFile) > 0 {
		load.WriteString("  # A declared fixed root past the fixed-form ceiling has no file form.\n\n")
	}
	for _, st := range noFile {
		fmt.Fprintf(&load, "  def load(%q, _data),\n    do: {:error, :no_file_form, Map.put(empty_report(), :reason, \"unknown_form\")}\n\n", st.Name)
	}
	if len(roots) > 0 {
		load.WriteString("  # A declared fixed root is read as form 3, one record or a refusal.\n")
		load.WriteString("  # Form 1 is previous_form: the form byte is named and nothing is decoded.\n\n")
	}
	for _, st := range roots {
		mod := fmt.Sprintf("%s.%s%s", ns, moduleBase(u.DeclFile[st.Name]), FixedModuleSuffix)
		snake := ir.RustSnake(st.Name)
		save.WriteString(fixedRootSaveClause(st.Name, mod+"."+snake+"_fixed_save([value])"))
		load.WriteString(fixedRootLoadClauses(st.Name, mod+"."+snake+"_fixed_load(data)"))
	}
	if len(roots) > 0 {
		runtime = insertOnce(runtime, "  # ---- the write side (docs/SPEC-TABLES.md §3) ----\n", save.String())
	}
	return insertOnce(runtime, "  # ---- the read side (docs/SPEC-TABLES.md §3, §4) ----\n", load.String())
}

const strictHelpers = `  # AN UNKNOWN FIELD IS SKIPPED, NEVER TRUSTED (docs/SPEC-TABLES.md §3, §4):
  # its payload is walked as the framing alone describes it, nested bodies,
  # arrays, keyed pairs and arms included, and whatever no writer emits there
  # refuses the whole read: a reference past the id table, a repeated id or
  # key, a reserved id, a bool past 1, ill-formed text, a float that is not
  # finite, a header that does not fill its L. Pointer payloads are the reach
  # walk's (strict_wire_reachable!/4).
  defp strict_unknown!(kind, payload, _nodes, ids) do
    case unknown_payload(payload, kind, ids, 0) do
      {:ok, <<>>} -> :ok
      _ -> throw(:wire_damaged)
    end
  end

  defp unknown_payload(buf, kind, ids, depth) do
    cond do
      kind in 1..11 or kind in 18..29 or kind == 36 ->
        w = unknown_width(kind)

        case buf do
          <<v::binary-size(^w), rest::binary>> ->
            if unknown_scalar?(v, kind), do: {:ok, rest}, else: :error

          _ ->
            :error
        end

      kind == 17 ->
        case leb(buf) do
          {_idx, rest} -> {:ok, rest}
          :error -> :error
        end

      kind == 30 ->
        case leb(buf) do
          {ref, rest} when ref <= div(byte_size(ids), 8) -> {:ok, rest}
          _ -> :error
        end

      kind == 15 ->
        unknown_arm(buf, ids, depth)

      kind in [12, 13, 14, 16, 31, 32] ->
        case leb(buf) do
          {n, rest} when byte_size(rest) >= n ->
            <<content::binary-size(^n), after_l::binary>> = rest

            if unknown_content?(content, kind, ids, depth),
              do: {:ok, after_l},
              else: :error

          _ ->
            :error
        end

      true ->
        :error
    end
  end

  defp unknown_width(kind) when kind in [1, 2, 6, 20, 25], do: 1
  defp unknown_width(kind) when kind in [3, 7, 21, 26], do: 2
  defp unknown_width(kind) when kind in [4, 8, 10], do: 4
  defp unknown_width(kind) when kind in [5, 9, 11], do: 8
  defp unknown_width(kind), do: kind_width(kind)

  defp unknown_scalar?(<<b>>, 1), do: b <= 1
  defp unknown_scalar?(<<bits::little-32>>, 10), do: (bits &&& 0x7F800000) != 0x7F800000

  defp unknown_scalar?(<<bits::little-64>>, 11),
    do: (bits &&& 0x7FF0000000000000) != 0x7FF0000000000000

  defp unknown_scalar?(_v, _kind), do: true

  defp unknown_content?(b, 12, _ids, _depth),
    do: utf8_valid?(b) and :binary.match(b, <<0>>) == :nomatch

  defp unknown_content?(_b, 31, _ids, _depth), do: true
  defp unknown_content?(b, 32, _ids, _depth), do: b == <<>>

  defp unknown_content?(_b, kind, _ids, depth) when kind in [13, 14, 16] and depth >= 128,
    do: false

  defp unknown_content?(b, 13, ids, depth), do: unknown_body?(b, ids, depth + 1, MapSet.new())
  defp unknown_content?(b, 14, ids, depth), do: unknown_array?(b, ids, depth + 1)
  defp unknown_content?(b, 16, ids, depth), do: unknown_keyed?(b, ids, depth + 1)

  defp unknown_content?(b, kind, ids, depth) do
    case unknown_payload(b, kind, ids, depth) do
      {:ok, <<>>} -> true
      _ -> false
    end
  end

  defp unknown_body?(b, ids, depth, seen) do
    case leb(b) do
      {0, rest} ->
        rest == <<>>

      {ref, <<kind, rest::binary>>} ->
        id = resolve(ids, ref)

        cond do
          id == :error or MapSet.member?(seen, id) ->
            false

          id in [@build_version_id, @message_vocab_id, @node_id] ->
            false

          true ->
            case unknown_payload(rest, kind, ids, depth) do
              {:ok, after_payload} ->
                unknown_body?(after_payload, ids, depth, MapSet.put(seen, id))

              :error ->
                false
            end
        end

      _ ->
        false
    end
  end

  defp unknown_array?(<<ek, rest::binary>>, ids, depth) when rest != <<>> do
    case leb(rest) do
      {n, elems} when n <= 0x7FFFFFFF -> unknown_elements?(elems, ek, n, ids, depth)
      _ -> false
    end
  end

  defp unknown_array?(_b, _ids, _depth), do: false

  defp unknown_elements?(rest, _ek, 0, _ids, _depth), do: rest == <<>>

  defp unknown_elements?(rest, ek, n, ids, depth) do
    case unknown_payload(rest, ek, ids, depth) do
      {:ok, after_elem} -> unknown_elements?(after_elem, ek, n - 1, ids, depth)
      :error -> false
    end
  end

  defp unknown_keyed?(<<ek, rest::binary>>, ids, depth) when rest != <<>> do
    case leb(rest) do
      {n, pairs} when n <= 0x7FFFFFFF -> unknown_pairs?(pairs, ek, n, ids, depth, MapSet.new())
      _ -> false
    end
  end

  defp unknown_keyed?(_b, _ids, _depth), do: false

  defp unknown_pairs?(rest, _ek, 0, _ids, _depth, _seen), do: rest == <<>>

  defp unknown_pairs?(rest, ek, n, ids, depth, seen) do
    with {key, r1} <- leb(rest),
         true <- key != 0 and key <= div(byte_size(ids), 8) and not MapSet.member?(seen, key),
         {len, r2} when byte_size(r2) >= len <- leb(r1),
         <<content::binary-size(^len), r3::binary>> <- r2,
         true <- unknown_content?(content, ek, ids, depth) do
      unknown_pairs?(r3, ek, n - 1, ids, depth, MapSet.put(seen, key))
    else
      _ -> false
    end
  end

  defp unknown_arm(buf, ids, depth) do
    case leb(buf) do
      {0, rest} ->
        {:ok, rest}

      {ref, <<kind, rest::binary>>} when ref <= div(byte_size(ids), 8) ->
        with {n, r1} when byte_size(r1) >= n <- leb(rest),
             <<content::binary-size(^n), r2::binary>> <- r1 do
          deeper = if kind in [13, 14, 15, 16], do: depth + 1, else: depth

          cond do
            kind in [13, 14, 15, 16] and depth >= 128 -> :error
            unknown_content?(content, kind, ids, deeper) -> {:ok, r2}
            true -> :error
          end
        else
          _ -> :error
        end

      _ ->
        :error
    end
  end

  # A node record no pointer reaches is damage (§3, §4): the reach is
  # syntactic, over the wire's own pointer payloads in the root body and in
  # every reached node's body, known and unknown fields alike, so a node
  # named only by a skipped field still counts as reached and a node named
  # by no payload at all refuses the whole load. A record of a type this
  # reader cannot name is held to the same rule, and its body's payloads
  # reach as a known record's do (internal/tablewire's wire check).
  defp strict_wire_reachable!(fields, node_fields, nodes, ids) do
    indexed = Enum.with_index(nodes, 2)

    node_indices =
      for {node, idx} <- indexed, node != :malformed, do: idx

    unknown_fields =
      for {{:unknown, body}, idx} <- indexed, is_binary(body), into: %{} do
        case entries(body, ids, false, :nested) do
          {:ok, fs, []} -> {idx, fs}
          _ -> throw(:wire_damaged)
        end
      end

    bodies = node_fields |> Map.merge(unknown_fields) |> Map.put(1, fields)
    seen = wire_reach([1], bodies, MapSet.new(), ids)

    if Enum.any?(node_indices, fn idx -> not MapSet.member?(seen, idx) end),
      do: throw(:wire_damaged)

    # A BLOB NAMED FROM TWO SLOTS refuses the whole read (§2.5): every pointer
    # payload in every body names, a skipped field's too, and each body is
    # counted once however many pointers reach it.
    names =
      bodies
      |> Map.values()
      |> Enum.flat_map(&wire_field_refs(&1, ids))
      |> Enum.frequencies()

    # A POINTER PAST THE NODE TABLE is damage wherever it rides (§3.1)
    last = length(nodes) + 1
    if Enum.any?(Map.keys(names), &(&1 > last)), do: throw(:wire_damaged)

    if Enum.any?(indexed, fn {node, idx} -> blob_node?(node) and Map.get(names, idx, 0) > 1 end),
      do: throw(:wire_damaged)

    :ok
  end

  defp blob_node?({:blob, _kind, _bytes}), do: true
  defp blob_node?({:unknown, :blob}), do: true
  defp blob_node?(_node), do: false

  defp wire_reach([], _bodies, seen, _ids), do: seen

  defp wire_reach([idx | rest], bodies, seen, ids) do
    if MapSet.member?(seen, idx) do
      wire_reach(rest, bodies, seen, ids)
    else
      refs =
        case Map.fetch(bodies, idx) do
          {:ok, fields} -> wire_field_refs(fields, ids)
          :error -> []
        end

      wire_reach(refs ++ rest, bodies, MapSet.put(seen, idx), ids)
    end
  end

  defp wire_field_refs(fields, ids) do
    Enum.flat_map(fields, fn
      {_id, kind, payload} when is_binary(payload) -> wire_payload_refs(kind, payload, ids)
      _ -> []
    end)
  end

  defp wire_payload_refs(17, payload, _ids) do
    case leb(payload) do
      {idx, _rest} -> [idx]
      :error -> []
    end
  end

  defp wire_payload_refs(13, payload, ids) do
    case leb(payload) do
      {len, body} when byte_size(body) >= len -> wire_body_refs(binary_part(body, 0, len), ids)
      _ -> []
    end
  end

  defp wire_payload_refs(14, payload, ids), do: wire_array_refs(payload, false, ids)

  defp wire_payload_refs(16, payload, ids), do: wire_array_refs(payload, true, ids)

  defp wire_payload_refs(15, payload, ids) do
    case leb(payload) do
      {0, _rest} ->
        []

      {_arm, <<kind, rest::binary>>} ->
        case leb(rest) do
          {l, content} when byte_size(content) >= l ->
            inner = binary_part(content, 0, l)
            framed = binary_part(rest, 0, byte_size(rest) - byte_size(content) + l)
            arm_payload = if kind in [12, 13, 14, 16], do: framed, else: inner
            wire_payload_refs(kind, arm_payload, ids)

          _ ->
            []
        end

      _ ->
        []
    end
  end

  defp wire_payload_refs(_kind, _payload, _ids), do: []

  defp wire_array_refs(payload, keyed, ids) do
    with {_len, body} <- leb(payload),
         <<elem_kind, rest::binary>> <- body,
         {n, elems} <- leb(rest) do
      wire_elem_refs(elem_kind, elems, n, keyed, ids, [])
    else
      _ -> []
    end
  end

  defp wire_elem_refs(_kind, _rest, 0, _keyed, _ids, acc), do: Enum.reverse(acc)

  defp wire_elem_refs(kind, rest, n, true, ids, acc) when n > 0 do
    with {_key, r1} <- leb(rest),
         {l, r2} when byte_size(r2) >= l <- leb(r1) do
      elem = binary_part(r2, 0, l)
      after_elem = binary_part(r2, l, byte_size(r2) - l)

      refs =
        if kind == 13 do
          wire_body_refs(elem, ids)
        else
          case wire_one_elem_refs(kind, elem, ids) do
            {r, _} -> r
            :error -> []
          end
        end

      wire_elem_refs(kind, after_elem, n - 1, true, ids, refs ++ acc)
    else
      _ -> Enum.reverse(acc)
    end
  end

  defp wire_elem_refs(kind, rest, n, false, ids, acc) when n > 0 do
    case wire_one_elem_refs(kind, rest, ids) do
      {refs, after_elem} -> wire_elem_refs(kind, after_elem, n - 1, false, ids, refs ++ acc)
      :error -> Enum.reverse(acc)
    end
  end

  defp wire_elem_refs(_kind, _rest, _n, _keyed, _ids, acc), do: Enum.reverse(acc)

  defp wire_one_elem_refs(17, rest, _ids) do
    case leb(rest) do
      {idx, after_elem} -> {[idx], after_elem}
      :error -> :error
    end
  end

  defp wire_one_elem_refs(13, rest, ids) do
    case leb(rest) do
      {len, body} when byte_size(body) >= len ->
        {wire_body_refs(binary_part(body, 0, len), ids),
         binary_part(body, len, byte_size(body) - len)}

      _ ->
        :error
    end
  end

  defp wire_one_elem_refs(15, rest, ids) do
    case leb(rest) do
      {0, after_elem} ->
        {[], after_elem}

      {_arm, <<kind, after_kind::binary>>} ->
        case leb(after_kind) do
          {l, content} when byte_size(content) >= l ->
            inner = binary_part(content, 0, l)
            framed = binary_part(after_kind, 0, byte_size(after_kind) - byte_size(content) + l)
            after_elem = binary_part(content, l, byte_size(content) - l)
            arm_payload = if kind in [12, 13, 14, 16], do: framed, else: inner
            {wire_payload_refs(kind, arm_payload, ids), after_elem}

          _ ->
            :error
        end

      _ ->
        :error
    end
  end

  defp wire_one_elem_refs(kind, rest, ids) when kind in [12, 13, 14, 16, 31, 32] do
    case body(rest) do
      {:ok, payload, after_elem} -> {wire_payload_refs(kind, payload, ids), after_elem}
      :error -> :error
    end
  end

  defp wire_one_elem_refs(kind, rest, _ids) when kind in [30] do
    case leb(rest) do
      {_idx, after_elem} -> {[], after_elem}
      :error -> :error
    end
  end

  defp wire_one_elem_refs(kind, rest, _ids) do
    w = kind_width(kind)

    if w > 0 and byte_size(rest) >= w do
      {[], binary_part(rest, w, byte_size(rest) - w)}
    else
      :error
    end
  end

  defp wire_body_refs(bin, ids), do: wire_body_refs(bin, ids, MapSet.new())

  defp wire_body_refs(<<>>, _ids, _seen), do: []

  defp wire_body_refs(bin, ids, seen) do
    case leb(bin) do
      {0, _rest} ->
        []

      {ref, rest} ->
        _ = seen

        case rest do
          <<kind, rest2::binary>> ->
            case slice(rest2, kind) do
              {:ok, payload, rest3} ->
                wire_payload_refs(kind, payload, ids) ++
                  wire_body_refs(rest3, ids, MapSet.put(seen, ref))

              :error ->
                []
            end

          _ ->
            []
        end

      :error ->
        []
    end
  end

`

// fixedClassHelpers is the conversion between the fixed form's value
// surface (a struct with atom keys, an atom-tagged union, an atom enum and
// the fixed report) and the Table API's surface (a string-keyed map, a
// string-tagged union, a string enum and the variable report) that the
// descriptors, the text form and the cook writer walk. Only the one fixed
// record a Table load returns, and the one map a Table save is handed, are
// converted; the wire itself stays form 3.
func fixedClassHelpers(u *ir.Unit) string {
	unionTo, unionFrom := fixedClassUnionPassthrough, fixedClassUnionPassthroughFrom
	if len(u.Unions)+len(u.TableUnions) > 0 {
		unionTo, unionFrom = fixedClassUnionTo, fixedClassUnionFrom
		if !hasVoidUnionArm(u) {
			unionFrom = fixedClassUnionFromNoVoid
		}
	}
	return fixedClassHelperHead + unionTo + unionFrom + fixedClassHelperTail
}

func hasVoidUnionArm(u *ir.Unit) bool {
	for _, un := range u.Unions {
		for _, v := range un.Variants {
			if v.Void() {
				return true
			}
		}
	}
	for _, un := range u.TableUnions {
		for _, v := range un.Variants {
			if v.Void() {
				return true
			}
		}
	}
	return false
}

const fixedClassUnionPassthrough = `
  defp fixed_class_union_to_table(_f, value), do: value
`

const fixedClassUnionPassthroughFrom = `
  defp fixed_class_union_from_table(_f, value), do: value
`

const fixedClassUnionTo = `
  defp fixed_class_union_to_table(f, value) do
    case value do
      nil ->
        nil

      {arm, payload} when is_atom(arm) ->
        union = Map.fetch!(@unions, Map.fetch!(f, :elem))
        snake = Atom.to_string(arm)

        name =
          Enum.find(Map.keys(union.by_name), snake, fn n ->
            fixed_class_snake(n) == snake
          end)

        case Map.get(union.by_name, name) do
          nil -> {name, payload}
          desc -> {name, fixed_class_value_to_table(desc, payload)}
        end

      arm when is_atom(arm) ->
        union = Map.fetch!(@unions, Map.fetch!(f, :elem))
        snake = Atom.to_string(arm)

        name =
          Enum.find(Map.keys(union.by_name), snake, fn n ->
            fixed_class_snake(n) == snake
          end)

        {name, nil}

      _ ->
        value
    end
  end
`

const fixedClassUnionFrom = `
  defp fixed_class_union_from_table(f, value) do
    case value do
      nil ->
        nil

      {arm, payload} when is_binary(arm) ->
        union = Map.fetch!(@unions, Map.fetch!(f, :elem))
        atom = String.to_atom(fixed_class_snake(arm))

        case Map.get(union.by_name, arm) do
          nil ->
            {atom, payload}

          desc ->
            if Map.get(desc, :t) == :void,
              do: atom,
              else: {atom, fixed_class_value_from_table(desc, payload)}
        end

      {arm, payload} when is_atom(arm) ->
        {arm, payload}

      _ ->
        value
    end
  end
`

const fixedClassUnionFromNoVoid = `
  defp fixed_class_union_from_table(f, value) do
    case value do
      nil ->
        nil

      {arm, payload} when is_binary(arm) ->
        union = Map.fetch!(@unions, Map.fetch!(f, :elem))
        atom = String.to_atom(fixed_class_snake(arm))

        case Map.get(union.by_name, arm) do
          nil ->
            {atom, payload}

          desc ->
            {atom, fixed_class_value_from_table(desc, payload)}
        end

      {arm, payload} when is_atom(arm) ->
        {arm, payload}

      _ ->
        value
    end
  end
`

const fixedClassHelperHead = `  # A fixed root's Table value is the Table API's value: the fixed record
  # converted by the descriptors above, so the text form, the cook writer
  # and a re-save walk the same string-keyed map a variable root returns.
  defp fixed_class_report(report) do
    %{
      unknown: Map.get(report, :unknown, 0),
      widened: Map.get(report, :widened, 0),
      retained: Map.get(report, :retained, 0),
      retain_lost: Map.get(report, :retain_lost, 0),
      layout_hash: Map.get(report, :layout_hash, 0),
      missing_required: Map.get(report, :missing_required, 0),
      blob_shared: Map.get(report, :blob_shared, 0),
      reason: Map.get(report, :reason, "")
    }
  end

  defp fixed_class_snake(name) do
    name
    |> String.replace(~r/([A-Z]+)([A-Z][a-z])/, "\\1_\\2")
    |> String.replace(~r/([a-z0-9])([A-Z])/, "\\1_\\2")
    |> String.downcase()
  end

  defp fixed_class_to_table(table, value) when is_map(value) do
    desc = Map.fetch!(@tables, table)

    Map.new(desc.fields, fn f ->
      {f.name, fixed_class_field_to_table(f, value)}
    end)
  end

  defp fixed_class_field_to_table(f, value) do
    raw = Map.get(value, String.to_atom(f.name))

    if Map.get(f, :optional, false) do
      present = Map.get(value, String.to_atom(f.name <> "_present"), false)
      if present, do: fixed_class_value_to_table(f, raw), else: nil
    else
      fixed_class_value_to_table(f, raw)
    end
  end

  defp fixed_class_value_to_table(f, value) do
    case Map.get(f, :t) do
      :table ->
        if is_map(value),
          do: fixed_class_to_table(Map.fetch!(f, :elem), value),
          else: nil

      :list ->
        if is_list(value),
          do: Enum.map(value, fn e -> fixed_class_elem_to_table(f, e) end),
          else: []

      :union ->
        fixed_class_union_to_table(f, value)

      :enum ->
        fixed_class_enum_to_table(f, value)

      :keyed ->
        fixed_class_keyed_to_table(f, value)

      _ ->
        value
    end
  end

  defp fixed_class_elem_to_table(f, value) do
    cond do
      Map.get(f, :elem_kind) == 13 and is_map(value) ->
        fixed_class_to_table(Map.fetch!(f, :elem), value)

      Map.get(f, :elem_kind) == 15 ->
        fixed_class_union_to_table(f, value)

      Map.has_key?(f, :by_name) ->
        fixed_class_enum_to_table(f, value)

      true ->
        value
    end
  end

  defp fixed_class_enum_to_table(f, value) when is_atom(value) do
    if value == :none do
      if Map.get(f, :optional, false), do: "None", else: nil
    else
      snake = Atom.to_string(value)
      names = Map.keys(Map.get(f, :by_name, %{}))
      Enum.find(names, snake, fn n -> fixed_class_snake(n) == snake end)
    end
  end

  defp fixed_class_enum_to_table(_f, value), do: value

  defp fixed_class_keyed_to_table(f, value) when is_list(value) do
    item = Map.fetch!(f, :item)

    Map.new(Enum.with_index(Map.fetch!(f, :key_names)), fn {name, i} ->
      {name, fixed_class_value_to_table(item, Enum.at(value, i))}
    end)
  end

  defp fixed_class_keyed_to_table(f, value) when is_map(value) do
    item = Map.fetch!(f, :item)

    Map.new(value, fn {k, v} ->
      {to_string(k), fixed_class_value_to_table(item, v)}
    end)
  end

  defp fixed_class_keyed_to_table(_f, _value), do: %{}

`

const fixedClassHelperTail = `
  defp fixed_class_from_table(table, value) when is_map(value) do
    desc = Map.fetch!(@tables, table)

    Enum.reduce(desc.fields, %{}, fn f, acc ->
      atom = String.to_atom(f.name)

      raw =
        cond do
          Map.has_key?(value, f.name) -> Map.get(value, f.name)
          Map.has_key?(value, atom) -> Map.get(value, atom)
          true -> Map.get(f, :default)
        end

      if Map.get(f, :optional, false) do
        present = String.to_atom(f.name <> "_present")

        payload =
          if raw == nil,
            do: fixed_class_absent_payload(f),
            else: fixed_class_value_from_table(f, raw)

        acc
        |> Map.put(atom, payload)
        |> Map.put(present, raw != nil)
      else
        Map.put(acc, atom, fixed_class_value_from_table(f, raw))
      end
    end)
  end

  defp fixed_class_absent_payload(f) do
    case Map.get(f, :t) do
      :table -> fixed_class_from_table(Map.fetch!(f, :elem), %{})
      :list -> []
      :enum -> :none
      :keyed -> fixed_class_keyed_from_table(f, %{})
      :string -> <<>>
      :bytes -> <<>>
      :bool -> false
      :float -> 0.0
      _ -> 0
    end
  end

  defp fixed_class_value_from_table(f, value) do
    case Map.get(f, :t) do
      :table ->
        if is_map(value),
          do: fixed_class_from_table(Map.fetch!(f, :elem), value),
          else: fixed_class_from_table(Map.fetch!(f, :elem), %{})

      :list ->
        if is_list(value),
          do: fixed_class_list_from_table(f, value),
          else: []

      :union ->
        fixed_class_union_from_table(f, value)

      :enum ->
        fixed_class_enum_from_table(f, value)

      :keyed ->
        fixed_class_keyed_from_table(f, value)

      _ ->
        if value == nil, do: Map.get(f, :default), else: value
    end
  end

  defp fixed_class_list_from_table(f, value) do
    elems = Enum.map(value, fn e -> fixed_class_elem_from_table(f, e) end)

    case Map.get(f, :fixed_array) do
      n when is_integer(n) ->
        kept = Enum.take(elems, n)
        kept ++ List.duplicate(fixed_class_elem_from_table(f, Map.get(f, :elem_default)), max(n - length(kept), 0))

      _ ->
        elems
    end
  end

  defp fixed_class_elem_from_table(f, value) do
    cond do
      Map.get(f, :elem_kind) == 13 and is_map(value) ->
        fixed_class_from_table(Map.fetch!(f, :elem), value)

      Map.get(f, :elem_kind) == 13 ->
        fixed_class_from_table(Map.fetch!(f, :elem), %{})

      Map.get(f, :elem_kind) == 15 ->
        fixed_class_union_from_table(f, value)

      Map.has_key?(f, :by_name) ->
        fixed_class_enum_from_table(f, value)

      true ->
        value
    end
  end

  defp fixed_class_enum_from_table(_f, nil), do: :none

  defp fixed_class_enum_from_table(_f, value) when is_atom(value), do: value

  defp fixed_class_enum_from_table(_f, "None"), do: :none

  defp fixed_class_enum_from_table(_f, value) when is_binary(value),
    do: String.to_atom(fixed_class_snake(value))

  defp fixed_class_enum_from_table(_f, value), do: value

  defp fixed_class_keyed_from_table(f, value) when is_map(value) do
    item = Map.fetch!(f, :item)

    Enum.map(Map.fetch!(f, :key_names), fn name ->
      raw =
        cond do
          Map.has_key?(value, name) -> Map.get(value, name)
          Map.has_key?(value, String.to_atom(fixed_class_snake(name))) -> Map.get(value, String.to_atom(fixed_class_snake(name)))
          true -> Map.get(item, :default)
        end

      fixed_class_value_from_table(item, raw)
    end)
  end

  defp fixed_class_keyed_from_table(f, value) when is_list(value) do
    item = Map.fetch!(f, :item)
    Enum.map(value, fn v -> fixed_class_value_from_table(item, v) end)
  end

  defp fixed_class_keyed_from_table(f, _value) do
    item = Map.fetch!(f, :item)

    Enum.map(Map.fetch!(f, :key_names), fn _name ->
      fixed_class_value_from_table(item, Map.get(item, :default))
    end)
  end

`

func insertOnce(src, anchor, clause string) string {
	if strings.Count(src, anchor) != 1 {
		panic("elixirtable: fixed-class wire anchor not found exactly once: " + anchor)
	}
	return strings.Replace(src, anchor, clause+anchor, 1)
}

// The heads and calls below are what Elixir 1.20.4's mix format (line length
// 98) keeps. A case head stays intact through 101 columns and breaks at 102.
// A save assignment breaks at `=` past 98, and a broken assignment keeps a
// blank line before the next statement.
func fixedRootSaveClause(name, call string) string {
	var b strings.Builder
	b.WriteString(exSaveHead(name))
	b.WriteString("    value = fixed_class_from_table(" + fmt.Sprintf("%q", name) + ", value)\n")
	one := "    data = " + call
	if exWidth(one) <= exLineLimit {
		b.WriteString(one + "\n")
	} else {
		b.WriteString("    data =\n")
		b.WriteString(exBreakSaveCall(call))
		b.WriteString("\n")
	}
	b.WriteString("    {:ok, IO.iodata_to_binary(data)}\n")
	b.WriteString("  end\n\n")
	return b.String()
}

func exBreakSaveCall(call string) string {
	if cont := "      " + call; exWidth(cont) <= exLineLimit {
		return cont + "\n"
	}
	head := strings.TrimSuffix(call, "([value])")
	if bracket := "      " + head + "(["; exWidth(bracket) <= exLineLimit {
		return bracket + "\n        value\n      ])\n"
	}
	return "      " + head + "(\n        [value]\n      )\n"
}

func fixedRootLoadClauses(name, call string) string {
	var b strings.Builder
	b.WriteString(exLoadFormHead(name))
	b.WriteString("    report = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, layout_hash: 0}\n")
	b.WriteString("    {:error, :previous_form, fixed_class_report(report)}\n")
	b.WriteString("  end\n\n")
	b.WriteString(exLoadDataHead(name))
	b.WriteString(exLoadCase(call))
	b.WriteString("      {:ok, [value], report} ->\n")
	b.WriteString("        {:ok, fixed_class_to_table(" + fmt.Sprintf("%q", name) + ", value), fixed_class_report(report)}\n")
	b.WriteString("      {:ok, [], report} -> {:error, :truncated, fixed_class_report(report)}\n")
	b.WriteString("      {:error, reason, report} -> {:error, reason, fixed_class_report(report)}\n")
	b.WriteString("      {:ok, _values, report} -> {:error, :batch_too_large, fixed_class_report(report)}\n")
	b.WriteString("    end\n")
	b.WriteString("  end\n\n")
	return b.String()
}

func exLoadCase(call string) string {
	one := "    case " + call + " do"
	// Elixir 1.20.4 keeps this head through 101 columns.
	if exWidth(one) <= exLineLimit+3 {
		return one + "\n"
	}
	head := strings.TrimSuffix(call, "(data)")
	return "    case " + head + "(\n           data\n         ) do\n"
}

func exSaveHead(name string) string {
	one := fmt.Sprintf("  def save(%q, value) when is_map(value) do", name)
	if exWidth(one) <= exLineLimit {
		return one + "\n"
	}
	short := fmt.Sprintf("  def save(%q, value)", name)
	if exWidth(short) <= exLineLimit {
		return short + "\n      when is_map(value) do\n"
	}
	return fmt.Sprintf("  def save(\n        %q,\n        value\n      )\n      when is_map(value) do\n", name)
}

func exLoadFormHead(name string) string {
	one := fmt.Sprintf("  def load(%q, <<1, _::binary>>) do", name)
	if exWidth(one) <= exLineLimit {
		return one + "\n"
	}
	return fmt.Sprintf("  def load(\n        %q,\n        <<1, _::binary>>\n      ) do\n", name)
}

func exLoadDataHead(name string) string {
	one := fmt.Sprintf("  def load(%q, data) when is_binary(data) do", name)
	if exWidth(one) <= exLineLimit {
		return one + "\n"
	}
	short := fmt.Sprintf("  def load(%q, data)", name)
	if exWidth(short) <= exLineLimit {
		return short + "\n      when is_binary(data) do\n"
	}
	return fmt.Sprintf("  def load(\n        %q,\n        data\n      )\n      when is_binary(data) do\n", name)
}
