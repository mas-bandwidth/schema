# THE ELIXIR PORT RUNS THE SHARED HOSTILE CORPUS (docs/HOSTILE-CORPUS.md).
#
# One raw input per manifest line, the port's generated reader underneath, and
# the Go engine's committed verdict as the oracle. The corpus is read as files:
# no network, no clock, no outside service. The runner prints one PASS line per
# case it agrees with, one FIX-CARD line per carried disagreement, and fails on
# a disagreement no FIX-CARD names (or a FIX-CARD whose case has gone green).
#
# usage: elixir test/elixir-hostile/runner.exs <corpus-dir> <ebin-dir>
#
# A carrier maps to the port's public reader:
#   text              <Unit>.Table.from_json/2
#   wire              <Unit>.Table.load/2
#   message           <Unit>.TableMessage.announce_read/1 then load_messages/3 (capacity 1)
#   message_novocab   load_messages(nil, input)
#   announce          <Unit>.TableMessage.announce_read/1
#   announce_second   announce_read/1 twice on one vocabulary
#   announce_bytes    announce_read/2 with max_bytes 8
#   announce_entries  announce_read/2 with max_entries 1
#
# The canonical decoded value is the port's text writer with the engine's one
# shape difference removed: the port ends its text with one newline and the
# engine's canonical text (internal/tabletext.Model.Write) does not.

[corpus, ebin] = System.argv()
Code.prepend_path(ebin)

tables = %{
  "tables/examples" => Tabledemo.Table,
  "tables/messages" => Messagedemo.Table,
  "tables/blobs" => Blobdemo.Table,
  "tables/pointers" => Graphdemo.Table,
  "tables/scalars" => Scalardemo.Table,
  "tables/stream" => Streamdemo.Table,
  "tables/maps" => Mapdemo.Table,
  "tables/lists" => Listdemo.Table,
  "schemas/probe.schema" => Probe.Table
}

messages = %{
  "tables/examples" => Tabledemo.TableMessage,
  "tables/messages" => Messagedemo.TableMessage
}

sha = fn text -> :crypto.hash(:sha256, text) |> Base.encode16(case: :lower) end

digest = fn mod, table, value ->
  case mod.to_json(table, value) do
    {:ok, text} -> sha.(String.trim_trailing(text, "\n"))
    other -> {:digest_error, other}
  end
end

# The engine's reason names are its own; the port's :malformed is the corpus's
# `damaged` (a read that reports a malformed event without naming a refusal).
reason = fn
  :malformed -> "damaged"
  :shared -> "damaged"
  r -> to_string(r)
end

# known findings: name<TAB>slug<TAB>input<TAB>one line
findings_path = System.get_env("SCHEMA_ELIXIR_HOSTILE_FINDINGS", "test/elixir-hostile/known-findings.tsv")

findings =
  if File.exists?(findings_path) do
    findings_path
    |> File.read!()
    |> String.split("\n")
    |> Enum.reject(&(&1 == "" or String.starts_with?(&1, "#")))
    |> Map.new(fn line ->
      [name, slug, input, text] = String.split(line, "\t", parts: 4)
      {name, {slug, input, text}}
    end)
  else
    %{}
  end

manifest =
  corpus
  |> Path.join("manifest.tsv")
  |> File.read!()
  |> String.split("\n")
  |> Enum.reject(&(&1 == "" or String.starts_with?(&1, "#")))
  |> Enum.map(&String.split(&1, "\t"))

run = fn carrier, schema, table, payload ->
  case carrier do
    "text" ->
      mod = Map.fetch!(tables, schema)
      case apply(mod, :from_json, [table, payload]) do
        {:ok, value, _report} -> {:accept, digest.(mod, table, value)}
        {:error, r, _report} -> {:refuse, reason.(r)}
      end

    "wire" ->
      mod = Map.fetch!(tables, schema)
      case apply(mod, :load, [table, payload]) do
        {:ok, value, _report} -> {:accept, digest.(mod, table, value)}
        {:error, r, _report} -> {:refuse, reason.(r)}
      end

    "message" ->
      mod = Map.fetch!(tables, schema)
      message = Map.fetch!(messages, schema)
      {:ok, vocab} = message.announce_read(message.announce())
      case message.load_messages(vocab, payload, 1) do
        {:ok, [[]]} ->
          # the batch's one body sets no field: the decoded value is the table's
          # defaults, and from_json/2 is where the port states them.
          {:ok, defaults, _report} = mod.from_json(table, "{}")
          {:accept, digest.(mod, table, defaults)}
        {:ok, values} -> {:unhandled_decoded_value, values}
        {:error, r} -> {:refuse, reason.(r)}
        {:error, r, _n} -> {:refuse, reason.(r)}
        {:malformed, _n, _bodies} -> {:refuse, "damaged"}
      end

    "message_novocab" ->
      message = Map.fetch!(messages, schema)
      case message.load_messages(nil, payload) do
        {:ok, values} -> {:unhandled_decoded_value, values}
        {:error, r} -> {:refuse, reason.(r)}
        {:error, r, _n} -> {:refuse, reason.(r)}
      end

    c when c in ["announce", "announce_bytes", "announce_entries"] ->
      message = Map.fetch!(messages, schema)

      vocab =
        case c do
          "announce" -> message.new()
          "announce_bytes" -> %{message.new() | max_bytes: 8}
          "announce_entries" -> %{message.new() | max_entries: 1}
        end

      case apply(message, :announce_read, [payload, vocab]) do
        {:ok, _vocab} -> {:accept, "-"}
        {:error, r, _vocab} -> {:refuse, reason.(r)}
      end

    "announce_second" ->
      message = Map.fetch!(messages, schema)
      {:ok, vocab} = message.announce_read(payload)
      case message.announce_read(payload, vocab) do
        {:ok, _vocab} -> {:accept, "-"}
        {:error, r, _vocab} -> {:refuse, reason.(r)}
      end
  end
end

{passed, carried, bad, stale} =
  Enum.reduce(manifest, {0, 0, [], []}, fn [name, carrier, schema, table, input, _prov, verdict, want_reason, want_digest], {p, c, bad, stale} ->
    payload = File.read!(Path.join(corpus, input))
    expected = if verdict == "accept", do: {:accept, want_digest}, else: {:refuse, want_reason}

    got =
      try do
        run.(carrier, schema, table, payload)
      rescue
        e -> {:raised, Exception.message(e)}
      catch
        kind, value -> {:threw, kind, value}
      end

    cond do
      got == expected ->
        case Map.pop(findings, name) do
          {nil, _rest} ->
            {p + 1, c, bad, stale}

          {finding, _rest} ->
            {slug, _input, _text} = finding
            IO.puts("STALE #{name}: FIX-CARD #{slug} no longer reproduces")
            {p, c, bad, [name | stale]}
        end

      Map.has_key?(findings, name) ->
        {slug, card_input, text} = Map.fetch!(findings, name)
        IO.puts("FIX-CARD: #{slug}: #{card_input} #{text}")
        {p, c + 1, bad, stale}

      true ->
        IO.puts("FAIL #{name} carrier=#{carrier} got=#{inspect(got, limit: 6)} want=#{inspect(expected, limit: 6)}")
        {p, c, [name | bad], stale}
    end
  end)

IO.puts("#{length(manifest)} cases: #{passed} pass, #{carried} carried as FIX-CARD, #{length(bad)} unexplained, #{length(stale)} stale")

if bad != [] or stale != [] do
  System.halt(1)
end
