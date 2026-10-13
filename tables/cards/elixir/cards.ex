# cards.ex — the owning facade for the Elixir card leg (tables/cards/cards.go).
#
# The generated Cards.Table codec answers the wire and text forms; this facade
# is the thin owning layer a customer imports: a Record that keeps its own wire
# binary, checked writes that refuse before publishing, and the two customer
# ceilings the schema deliberately leaves unbounded (docs/SPEC-TABLES.md §2.5,
# §2.9). It is the Go facade's calls in Elixir's own spelling, ported from
# tables/cards/cards.go, parse.go and render.go.
defmodule Cards.Cards do
  @moduledoc """
  The owning facade over the generated `Cards.Table` codec (docs/SPEC-TABLES.md
  §2, §6.2). A `Record` keeps its own wire binary; a checked write refuses
  before any bytes are published.
  """

  # MaxBodyBytes is the customer ceiling on one card's opaque body
  # (tables/cards/cards.go: MaxBodyBytes).
  @max_body_bytes 64 * 1024

  # MaxListEntries is the customer ceiling on each collection field
  # (tables/cards/cards.go: MaxListEntries).
  @max_list_entries 512

  # The mapped header vocabulary, including RESULT and the agreeing tier line
  # (tables/cards/parse.go: reservedHeaderKeys).
  @reserved_header_keys ~w(RESULT tier REPO BASE KIND PRIORITY PATHS NEW SHARED DEPENDS-ON TEST DONE-WHEN DEADLINE SOURCE)

  # The customer's exact case-sensitive stranded set (tables/cards/parse.go:
  # strandedKeys).
  @stranded_keys ~w(KIND PATHS TEST LEGS SOURCE START STOP DEPENDS-ON PRIORITY NEW SHARED REPO BASE tier RESULT)

  # The admitted header grammar (tables/cards/parse.go: headerLineRE).
  @header_line_re ~r/^([A-Za-z][A-Za-z0-9-]*):\s*(.*)$/

  # The admitted key grammar alone (tables/cards/parse.go: headerKeyRE).
  @header_key_re ~r/^[A-Za-z][A-Za-z0-9-]*$/

  # The customer spelling DEADLINE accepts (tables/cards/parse.go: deadlineRE).
  @deadline_re ~r/^finish within ([0-9]+) minutes\.$/

  defmodule Record do
    @moduledoc "A card record owning its wire binary and its loaded value."
    defstruct [:wire, :value, :report]
  end

  # Load reads wire into a Record through the generated codec and the checked
  # write (tables/cards/cards.go: Load). Failures and malformed wire are errors;
  # tolerant events ride in the returned report. The wire is kept as-is: a BEAM
  # binary is immutable, so the record owns its bytes and no generated term
  # holds a sub-binary of caller input past the call.
  def load(wire) when is_binary(wire) do
    case Cards.Table.load("Card", wire) do
      {:ok, value, report} ->
        case check_card(value) do
          :ok -> {:ok, %Record{wire: wire, value: value, report: report}, report}
          {:error, reason} -> {:error, reason, report}
        end

      {:error, reason, report} ->
        {:error, reason, report}
    end
  end

  # FromJSON builds a Record from JSON text through the generated codec and the
  # checked write (tables/cards/cards.go: FromJSON). Unknown, mismatched, clamped
  # and malformed inputs are refused, as are the customer bounds and any body or
  # element that is not well-formed UTF-8 without NUL. Bytes are published only
  # on complete success.
  def from_json(text) when is_binary(text) do
    case Cards.Table.from_json("Card", text) do
      {:ok, value, report} ->
        if report.unknown > 0 or report.kind_mismatch > 0 or report.clamped > 0 do
          {:error,
           "cards: JSON has loss events (unknown=#{report.unknown} kindMismatch=#{report.kind_mismatch} clamped=#{report.clamped}): refused, never truncated"}
        else
          case check_card(value) do
            :ok ->
              case Cards.Table.save("Card", value) do
                {:ok, wire} -> {:ok, %Record{wire: wire, value: value, report: report}}
                _ -> {:error, "cards: packing failed"}
              end

            {:error, reason} ->
              {:error, reason}
          end
        end

      {:error, :malformed, _report} ->
        {:error, "cards: JSON is malformed"}
    end
  end

  # ToJSON writes the record's known-field JSON text and returns the record's
  # load report (tables/cards/cards.go: ToJSON).
  def to_json(%Record{value: value} = record) do
    case Cards.Table.to_json("Card", value) do
      {:ok, text} -> {:ok, text, record.report}
      _ -> {:error, "cards: the card has no JSON spelling", record.report}
    end
  end

  def to_json(nil), do: {:error, "cards: nil record", %{}}

  # Wire returns the record's wire bytes (tables/cards/cards.go: Wire). A BEAM
  # binary is immutable, so the returned value is already a defensive copy.
  def wire(%Record{wire: wire}), do: wire
  def wire(nil), do: nil

  # Parse reads admitted brief text and publishes a typed Record only on
  # complete checked success (tables/cards/parse.go: Parse). The body is the
  # exact suffix after the last header line's terminator.
  def parse(text) when is_binary(text) do
    cond do
      not String.valid?(text) ->
        {:error, "cards: brief is not valid UTF-8"}

      :binary.match(text, <<0>>) != :nomatch ->
        {:error, "cards: brief contains a NUL byte"}

      true ->
        lines = split_lines(text)

        if lines == [] do
          {:error, "cards: brief has no RESULT line"}
        else
          parse_lines(lines, text)
        end
    end
  end

  # Render reads the record's value and emits the admitted brief text
  # (tables/cards/render.go: Render): RESULT first, the mapped keys in a fixed
  # documented order, extras in stored order, then the body bytes verbatim.
  def render(%Record{value: value} = record) do
    with {:ok, tier} <- tier_string(Map.get(value, "tier")),
         {:ok, prio} <- priority_string(Map.get(value, "priority")) do
      out =
        [
          "RESULT: ",
          Map.get(value, "id", ""),
          " sha=",
          Map.get(value, "sha", ""),
          " tier: ",
          tier,
          "\n",
          emit_str("REPO", Map.get(value, "repo", "")),
          emit_str("BASE", Map.get(value, "_base", "")),
          emit_str("KIND", Map.get(value, "kind", "")),
          emit_priority(prio),
          emit_list("PATHS", collect_paths(Map.get(value, "paths", []))),
          emit_list("NEW", collect_paths(Map.get(value, "_new", []))),
          emit_list("SHARED", collect_paths(Map.get(value, "shared", []))),
          emit_list("DEPENDS-ON", collect_paths(Map.get(value, "needs", []))),
          emit_str("TEST", Map.get(value, "test", "")),
          emit_ptr("DONE-WHEN", Map.get(value, "done")),
          emit_deadline(Map.get(value, "deadline_minutes", 0)),
          emit_ptr("SOURCE", Map.get(value, "source")),
          emit_extras(Map.get(value, "extra", [])),
          emit_body(Map.get(value, "body"))
        ]

      {:ok, IO.iodata_to_binary(out), record.report}
    end
  end

  def render(nil), do: {:error, "cards: nil record", %{}}

  # ---- the checked write (tables/cards/cards.go: checkCard) ----

  defp check_card(value) do
    with :ok <- check_body(Map.get(value, "body")),
         :ok <- check_path_list(Map.get(value, "paths", []), "paths"),
         :ok <- check_path_list(Map.get(value, "_new", []), "_new"),
         :ok <- check_path_list(Map.get(value, "shared", []), "shared"),
         :ok <- check_id_list(Map.get(value, "needs", []), "needs"),
         :ok <- check_extras(Map.get(value, "extra", [])),
         :ok <- check_consumers(Map.get(value, "consumers", [])) do
      :ok
    end
  end

  defp check_body(nil), do: :ok

  defp check_body(body) do
    cond do
      byte_size(body) > @max_body_bytes ->
        {:error, "cards: body is #{byte_size(body)} bytes, past the #{@max_body_bytes}-byte ceiling"}

      not valid_utf8_no_nul?(body) ->
        {:error, "cards: body is not well-formed UTF-8 without NUL"}

      true ->
        :ok
    end
  end

  defp check_path_list(list, name) do
    with :ok <- check_count(list, name),
         :ok <- check_elements(list, name) do
      :ok
    end
  end

  defp check_id_list(list, name) do
    with :ok <- check_count(list, name),
         :ok <- check_elements(list, name) do
      :ok
    end
  end

  defp check_count(list, name) do
    if length(list) > @max_list_entries do
      {:error,
       "cards: #{name} has #{length(list)} entries, past the #{@max_list_entries}-entry ceiling (docs/SPEC-TABLES.md §2.9)"}
    else
      :ok
    end
  end

  defp check_elements(list, name) do
    Enum.reduce_while(list, :ok, fn e, :ok ->
      s = Map.get(e, "s", "")

      if valid_utf8_no_nul?(s) do
        {:cont, :ok}
      else
        {:halt, {:error, "cards: #{name} has an element that is not well-formed UTF-8 without NUL"}}
      end
    end)
  end

  defp check_extras(list) do
    with :ok <- check_count(list, "extra") do
      Enum.reduce_while(list, :ok, fn e, :ok ->
        key = Map.get(e, "key", "")
        value = Map.get(e, "value")

        cond do
          not valid_utf8_no_nul?(key) ->
            {:halt, {:error, "cards: extra has a key that is not well-formed UTF-8 without NUL"}}

          not Regex.match?(@header_key_re, key) ->
            {:halt, {:error, "cards: extra key #{inspect(key)} is not an admitted header key"}}

          :binary.match(key, ["\r", "\n"]) != :nomatch ->
            {:halt, {:error, "cards: extra has a key with a CR or LF"}}

          key in @reserved_header_keys ->
            {:halt, {:error, "cards: extra key #{inspect(key)} duplicates a mapped header key"}}

          value != nil and not valid_utf8_no_nul?(value) ->
            {:halt, {:error, "cards: extra has a value that is not well-formed UTF-8 without NUL"}}

          value != nil and :binary.match(value, ["\r", "\n"]) != :nomatch ->
            {:halt, {:error, "cards: extra has a value with a CR or LF"}}

          true ->
            {:cont, :ok}
        end
      end)
    end
  end

defp check_consumers(list) do
    with :ok <- check_count(list, "consumers") do
      Enum.reduce_while(list, :ok, fn e, :ok ->
        fields = ~w(kind who route model outcome line)

        result =
          Enum.reduce_while(fields, :ok, fn f, :ok ->
            s = Map.get(e, f, "")

            if valid_utf8_no_nul?(s) do
              {:cont, :ok}
            else
              {:halt,
               {:error, "cards: consumers has a #{f} that is not well-formed UTF-8 without NUL"}}
            end
          end)

        case result do
          :ok -> {:cont, :ok}
          {:error, reason} -> {:halt, {:error, reason}}
        end
      end)
    end
  end

  defp valid_utf8_no_nul?(bin) do
    String.valid?(bin) and :binary.match(bin, <<0>>) == :nomatch
  end

  # ---- the parse half (tables/cards/parse.go) ----

  defp parse_lines([first | rest], text) do
    case match_header(first.content) do
      {key, value, true} when key == "RESULT" ->
        case apply_result(value) do
          {:ok, p} ->
            do_parse(rest, text, first.term_end, p, %{"RESULT" => 1}, false)

          {:error, reason} ->
            {:error, reason}
        end

      _ ->
        {:error, "cards: line 1 is not a RESULT line"}
    end
  end

defp do_parse([], text, last_header_end, p, _seen, _prose) do
    body = binary_part(text, last_header_end, byte_size(text) - last_header_end)
    build(p, body)
  end

  defp do_parse([ln | rest], text, last_header_end, p, seen, prose) do
    cond do
      is_blank(ln.content) ->
        do_parse(rest, text, last_header_end, p, seen, prose)

      true ->
        case match_header(ln.content) do
          {_k, _v, false} ->
            do_parse(rest, text, last_header_end, p, seen, true)

          {k, v, true} ->
            cond do
              prose ->
                if k in @stranded_keys do
                  {:error, "cards: stranded header key #{inspect(k)} on line #{ln.num}"}
                else
                  do_parse(rest, text, last_header_end, p, seen, prose)
                end

              true ->
                case apply_header(p, k, v, ln.num, seen) do
                  {:ok, p2, seen2} ->
                    do_parse(rest, text, ln.term_end, p2, seen2, prose)

                  {:error, reason} ->
                    {:error, reason}
                end
            end
        end
    end
  end

  defp split_lines(text), do: do_split(text, 0, 1, [])

  defp do_split(<<>>, _off, _n, acc), do: Enum.reverse(acc)

  defp do_split(text, off, n, acc) do
    case :binary.match(text, "\n") do
      :nomatch ->
        content = strip_cr(text)
        Enum.reverse([%{content: content, term_end: off + byte_size(text), num: n} | acc])

      {j, 1} ->
        content = text |> binary_part(0, j) |> strip_cr()
        term_end = off + j + 1
        rest = binary_part(text, j + 1, byte_size(text) - j - 1)
        do_split(rest, term_end, n + 1, [%{content: content, term_end: term_end, num: n} | acc])
    end
  end

  defp strip_cr(<<>>), do: <<>>

  defp strip_cr(content) do
    if byte_size(content) > 0 and :binary.last(content) == ?\r do
      binary_part(content, 0, byte_size(content) - 1)
    else
      content
    end
  end

  defp match_header(line) do
    case Regex.run(@header_line_re, line) do
      [_, key, value] -> {key, value, true}
      _ -> {"", "", false}
    end
  end

  defp apply_result(value) do
    with {:ok, tier_str, rest} <- split_tier(value),
         {:ok, id_str, sha_str} <- split_sha(rest),
         :ok <- check_str_bound("id", id_str, 64),
         :ok <- check_sha(sha_str),
         {:ok, tier} <- parse_tier(tier_str) do
      {:ok,
       %{
         id: id_str,
         sha: sha_str,
         tier: tier,
         repo: "",
         base: "",
         kind: "",
         priority: nil,
         stream: "",
         paths: [],
         news: [],
         shared: [],
         needs: [],
         test: "",
         done_when: "",
         source: "",
         deadline: 0,
         extras: []
       }}
    end
  end

  defp split_tier(value) do
    case last_index(value, " tier:") do
      nil ->
        {:error, ~s(cards: RESULT line has no " tier:")}

      i ->
        tier_str = value |> binary_part(i + 6, byte_size(value) - i - 6) |> String.trim()
        {:ok, tier_str, binary_part(value, 0, i)}
    end
  end

  defp split_sha(rest) do
    case last_index(rest, " sha=") do
      nil ->
        {:error, ~s(cards: RESULT line has no " sha=")}

      j ->
        id_str = rest |> binary_part(0, j) |> String.trim()
        sha_str = binary_part(rest, j + 5, byte_size(rest) - j - 5)
        {:ok, id_str, sha_str}
    end
  end

  defp check_sha(sha_str) do
    if is_hex(sha_str) and (byte_size(sha_str) == 12 or byte_size(sha_str) == 40) do
      :ok
    else
      {:error, "cards: RESULT sha #{inspect(sha_str)} is not 12 or 40 hex digits"}
    end
  end

  defp apply_header(p, key, value, num, seen) do
    cond do
      :binary.match(value, ["\r", "\n"]) != :nomatch ->
        {:error, "cards: #{key} on line #{num} has a CR or LF in its value"}

      key == "tier" ->
        if Map.has_key?(seen, "tier") do
          {:error, "cards: duplicate header key #{inspect(key)} on lines #{seen["tier"]} and #{num}"}
        else
          case parse_tier(String.trim(value)) do
            {:ok, t} ->
              if t != p.tier do
                {:error, "cards: tier on line #{num} disagrees with RESULT"}
              else
                {:ok, p, Map.put(seen, "tier", num)}
              end

            {:error, reason} ->
              {:error, "cards: tier on line #{num}: #{reason}"}
          end
        end

      key == "RESULT" ->
        {:error, "cards: duplicate header key #{inspect(key)} on lines #{seen["RESULT"]} and #{num}"}

      key in @reserved_header_keys ->
        if Map.has_key?(seen, key) do
          {:error, "cards: duplicate header key #{inspect(key)} on lines #{seen[key]} and #{num}"}
        else
          case assign_mapped(p, key, value, num) do
            {:ok, p2} -> {:ok, p2, Map.put(seen, key, num)}
            {:error, reason} -> {:error, reason}
          end
        end

      true ->
        case check_str_bound("extra key", key, 32) do
          :ok ->
            {:ok, %{p | extras: p.extras ++ [%{key: key, value: value}]}, seen}

          {:error, reason} ->
            {:error, "cards: line #{num}: #{reason}"}
        end
    end
  end

  defp assign_mapped(p, key, value, num) do
    case key do
      "REPO" -> assign_str(p, :repo, value, 96, "REPO", num)
      "BASE" -> assign_str(p, :base, value, 96, "BASE", num)
      "KIND" -> assign_str(p, :kind, value, 24, "KIND", num)
      "PRIORITY" ->
        case parse_priority(String.trim(value)) do
          {:ok, pr} -> {:ok, %{p | priority: pr}}
          {:error, reason} -> {:error, "cards: PRIORITY on line #{num}: #{reason}"}
        end

      "PATHS" ->
        case split_list_checked(value, 192, true, "PATHS", num) do
          {:ok, el} -> {:ok, %{p | paths: el}}
          {:error, reason} -> {:error, reason}
        end

      "NEW" ->
        case split_list_checked(value, 192, false, "NEW", num) do
          {:ok, el} -> {:ok, %{p | news: el}}
          {:error, reason} -> {:error, reason}
        end

      "SHARED" ->
        case split_list_checked(value, 192, false, "SHARED", num) do
          {:ok, el} -> {:ok, %{p | shared: el}}
          {:error, reason} -> {:error, reason}
        end

      "DEPENDS-ON" ->
        case split_list_checked(value, 64, false, "DEPENDS-ON", num) do
          {:ok, el} -> {:ok, %{p | needs: el}}
          {:error, reason} -> {:error, reason}
        end

      "TEST" -> assign_str(p, :test, value, 192, "TEST", num)
      "DONE-WHEN" -> {:ok, %{p | done_when: value}}
      "DEADLINE" ->
        case parse_deadline(value) do
          {:ok, n} -> {:ok, %{p | deadline: n}}
          {:error, reason} -> {:error, "cards: DEADLINE on line #{num}: #{reason}"}
        end

      "SOURCE" -> {:ok, %{p | source: value}}
    end
  end

  defp assign_str(p, field, value, bound, name, num) do
    case check_str_bound(name, value, bound) do
      :ok -> {:ok, Map.put(p, field, value)}
      {:error, reason} -> {:error, "cards: #{name} on line #{num}: #{reason}"}
    end
  end

  defp check_str_bound(name, value, bound) do
    if byte_size(value) > bound do
      {:error, "cards: #{name} is #{byte_size(value)} bytes, past the #{bound}-byte bound"}
    else
      :ok
    end
  end

  defp split_list_checked(value, bound, none_sentinel, name, num) do
    v = String.trim(value)

    cond do
      v == "" or v == "-" ->
        {:ok, []}

      none_sentinel and v == "none" ->
        {:ok, []}

      true ->
        parts = String.split(v, ",")

        out =
          Enum.reduce(parts, {:ok, []}, fn part, {:ok, acc} ->
            part = String.trim(part)

            if part == "" do
              {:ok, acc}
            else
              case check_str_bound(name <> " element", part, bound) do
                :ok -> {:ok, acc ++ [part]}
                {:error, reason} -> {:error, "cards: #{name} on line #{num}: #{reason}"}
              end
            end
          end)

        case out do
          {:ok, el} ->
            if length(el) > @max_list_entries do
              {:error,
               "cards: #{name} on line #{num} has #{length(el)} entries, past the #{@max_list_entries}-entry ceiling"}
            else
              {:ok, el}
            end

          {:error, reason} ->
            {:error, reason}
        end
    end
  end

  defp parse_tier("flash"), do: {:ok, "flash"}
  defp parse_tier("pro"), do: {:ok, "pro"}
  defp parse_tier("heavy"), do: {:ok, "heavy"}
  defp parse_tier("frontier"), do: {:ok, "frontier"}
  defp parse_tier(s), do: {:error, "unknown tier #{inspect(s)}"}

  defp parse_priority("normal"), do: {:ok, "normal"}
  defp parse_priority("high"), do: {:ok, "high"}
  defp parse_priority("critical"), do: {:ok, "critical"}
  defp parse_priority("blocker"), do: {:ok, "blocker"}
  defp parse_priority(s), do: {:error, "unknown priority #{inspect(s)}"}

  defp parse_deadline(s) do
    case Regex.run(@deadline_re, String.trim(s)) do
      [_, digits] ->
        n = String.to_integer(digits)

        if n < 0 or n > 100_000 do
          {:error, "minutes out of range"}
        else
          {:ok, n}
        end

      _ ->
        {:error, ~s(not of the form "finish within N minutes.")}
    end
  end

  defp is_hex(s) do
    s != "" and Regex.match?(~r/^[0-9a-fA-F]+$/, s)
  end

  defp is_blank(line) do
    line == "" or Regex.match?(~r/^[ \t\r]+$/, line)
  end

  defp last_index(bin, sub) do
    case :binary.matches(bin, sub) do
      [] -> nil
      matches -> elem(List.last(matches), 0)
    end
  end

  # build publishes the parsed card through the generated codec and checked
  # write (tables/cards/parse.go: build).
  defp build(p, body) do
    value = %{
      "id" => p.id,
      "sha" => p.sha,
      "tier" => p.tier,
      "repo" => p.repo,
      "_base" => p.base,
      "kind" => p.kind,
      "priority" => p.priority,
      "stream" => p.stream,
      "paths" => Enum.map(p.paths, &%{"s" => &1}),
      "_new" => Enum.map(p.news, &%{"s" => &1}),
      "shared" => Enum.map(p.shared, &%{"s" => &1}),
      "needs" => Enum.map(p.needs, &%{"s" => &1}),
      "test" => p.test,
      "done" => if(p.done_when == "", do: nil, else: p.done_when),
      "deadline_minutes" => p.deadline,
      "source" => if(p.source == "", do: nil, else: p.source),
      "extra" => Enum.map(p.extras, fn e -> %{"key" => e.key, "value" => e.value} end),
      "body" => if(body == "", do: nil, else: body)
    }

    case check_card(value) do
      :ok ->
        case Cards.Table.save("Card", value) do
          {:ok, wire} -> {:ok, %Record{wire: wire, value: value, report: %{}}}
          _ -> {:error, "cards: packing failed"}
        end

      {:error, reason} ->
        {:error, reason}
    end
  end

  # ---- the render half (tables/cards/render.go) ----

  defp tier_string("flash"), do: {:ok, "flash"}
  defp tier_string("pro"), do: {:ok, "pro"}
  defp tier_string("heavy"), do: {:ok, "heavy"}
  defp tier_string("frontier"), do: {:ok, "frontier"}
  defp tier_string(t), do: {:error, "cards: tier #{inspect(t)} has no header spelling"}

  defp priority_string(nil), do: {:ok, ""}
  defp priority_string("normal"), do: {:ok, "normal"}
  defp priority_string("high"), do: {:ok, "high"}
  defp priority_string("critical"), do: {:ok, "critical"}
  defp priority_string("blocker"), do: {:ok, "blocker"}
  defp priority_string(p), do: {:error, "cards: priority #{inspect(p)} has no header spelling"}

  defp emit_str(_key, ""), do: []
  defp emit_str(key, value), do: [key, ": ", value, "\n"]

  defp emit_priority(""), do: []
  defp emit_priority(prio), do: ["PRIORITY: ", prio, "\n"]

  defp emit_ptr(_key, nil), do: []
  defp emit_ptr(key, value), do: [key, ": ", value, "\n"]

  defp emit_deadline(0), do: []
  defp emit_deadline(minutes), do: ["DEADLINE: finish within ", Integer.to_string(minutes), " minutes.\n"]

  defp emit_list(_key, []), do: []

  defp emit_list(key, elems) do
    [key, ": ", Enum.intersperse(elems, ", "), "\n"]
  end

  defp emit_extras([]), do: []

  defp emit_extras(extras) do
    Enum.map(extras, fn e ->
      [Map.get(e, "key", ""), ": ", Map.get(e, "value", "") || "", "\n"]
    end)
  end

  defp emit_body(nil), do: []
  defp emit_body(body), do: [body]

  defp collect_paths(list), do: Enum.map(list, &Map.get(&1, "s", ""))
end
