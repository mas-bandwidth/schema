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
	if len(roots) == 0 {
		return runtime
	}
	var save, load strings.Builder
	save.WriteString("  # A declared fixed root's file is form 3 (docs/SPEC-TABLES.md §3, §3.4).\n")
	save.WriteString("  # The write is the fixed form's own. The variable codec below is for\n")
	save.WriteString("  # variable roots, including one that holds a fixed table.\n\n")
	load.WriteString("  # A declared fixed root is read as form 3, one record or a refusal.\n")
	load.WriteString("  # Form 1 is previous_form: the form byte is named and nothing is decoded.\n\n")
	save.WriteString(fixedClassHelpers(u))
	for _, st := range roots {
		mod := fmt.Sprintf("%s.%s%s", ns, moduleBase(u.DeclFile[st.Name]), FixedModuleSuffix)
		snake := ir.RustSnake(st.Name)
		save.WriteString(fixedRootSaveClause(st.Name, mod+"."+snake+"_fixed_save([value])"))
		load.WriteString(fixedRootLoadClauses(st.Name, mod+"."+snake+"_fixed_load(data)"))
	}
	runtime = insertOnce(runtime, "  # ---- the write side (docs/SPEC-TABLES.md §3) ----\n", save.String())
	runtime = insertOnce(runtime, "  # ---- the read side (docs/SPEC-TABLES.md §3, §4) ----\n", load.String())
	return runtime
}

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
	}
	return fixedClassHelperHead + unionTo + unionFrom + fixedClassHelperTail
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

      :wstring ->
        fixed_class_wide_to_table(value)

      :keyed ->
        fixed_class_keyed_to_table(f, value)

      _ ->
        value
    end
  end

  defp fixed_class_wide_to_table(value) when is_binary(value) do
    case :unicode.characters_to_binary(value, {:utf16, :little}, :utf8) do
      text when is_binary(text) -> text
      _ -> value
    end
  end

  defp fixed_class_wide_to_table(value), do: value

  defp fixed_class_elem_to_table(f, value) do
    cond do
      Map.get(f, :elem_kind) == 13 and is_map(value) ->
        fixed_class_to_table(Map.fetch!(f, :elem), value)

      Map.get(f, :elem_kind) == 15 ->
        fixed_class_union_to_table(f, value)

      Map.get(f, :elem_kind) == 33 ->
        fixed_class_wide_to_table(value)

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
      :wstring -> <<>>
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
          do: Enum.map(value, fn e -> fixed_class_elem_from_table(f, e) end),
          else: []

      :union ->
        fixed_class_union_from_table(f, value)

      :enum ->
        fixed_class_enum_from_table(f, value)

      :wstring ->
        fixed_class_wide_from_table(value)

      :keyed ->
        fixed_class_keyed_from_table(f, value)

      _ ->
        if value == nil, do: Map.get(f, :default), else: value
    end
  end

  defp fixed_class_wide_from_table(value) when is_binary(value) do
    case :unicode.characters_to_binary(value, :utf8, {:utf16, :little}) do
      text when is_binary(text) -> text
      _ -> value
    end
  end

  defp fixed_class_wide_from_table(_value), do: <<>>

  defp fixed_class_elem_from_table(f, value) do
    cond do
      Map.get(f, :elem_kind) == 13 and is_map(value) ->
        fixed_class_from_table(Map.fetch!(f, :elem), value)

      Map.get(f, :elem_kind) == 13 ->
        fixed_class_from_table(Map.fetch!(f, :elem), %{})

      Map.get(f, :elem_kind) == 15 ->
        fixed_class_union_from_table(f, value)

      Map.get(f, :elem_kind) == 33 ->
        fixed_class_wide_from_table(value)

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
