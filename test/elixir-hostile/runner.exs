# THE ELIXIR PORT RUNS THE SHARED HOSTILE CORPUS (docs/HOSTILE-CORPUS.md).
#
# One raw input per manifest line, the port's generated reader underneath, and
# the Go engine's committed verdict as the oracle. The corpus is read as files:
# no network, no clock, no outside service. The runner prints one PASS line per
# case it agrees with, one FIX-CARD line per carried disagreement, and fails on
# a disagreement no FIX-CARD names (or a FIX-CARD whose case has gone green).
#
# usage: elixir test/elixir-hostile/runner.exs <corpus-dir> <ebin-dir> [packet-dir]
#
# A carrier maps to the port's public reader:
#   text              <Unit>.Table.from_json/2
#   wire              <Unit>.Table.load/2
#   wire_retain       <Unit>.Table.load_retain/4
#   message           <Unit>.TableMessage.announce_read/1 then load_messages/3 (capacity 1)
#   message_novocab   load_messages(nil, input)
#   announce          <Unit>.TableMessage.announce_read/1
#   announce_second   announce_read/1 twice on one vocabulary
#   announce_bytes    announce_read/2 with max_bytes 8
#   announce_entries  announce_read/2 with max_entries 1
#   packet            <Package>.<File>.read_<type>(input, 8 * size), then
#                     write_<type>!/1 re-writes the decoded value; the digest is
#                     SHA-256 of those bytes. The packet units are the ones
#                     make/hostile-packet-units.sh writes, compiled into
#                     [packet-dir]/ebin.
#
# The canonical decoded value is the port's text writer with the engine's one
# shape difference removed: the port ends its text with one newline and the
# engine's canonical text (internal/tabletext.Model.Write) does not.
#
# THE RUNNER RULES (docs/HOSTILE-CORPUS.md). Every case runs in its own process
# under a heap cap (:max_heap_size, which bounds the process stack too, so deep
# recursion is a killed process) and a time budget (5 s, or
# HOSTILE_CASE_BUDGET_MS). A raise, a throw, an exit or a kill is that case's
# answer and the run goes on; a case still running at its budget is killed and
# is over budget. After an accepted wire, wire_retain, text or message case the
# runner also measures, saves and writes the text of the decoded value, and a
# raise there is the case's answer too (a clean refusal is not). Cases run one
# at a time: the budget is wall-clock, and parallel cases sharing the bench
# would charge each other's time.

[corpus, ebin | rest] = System.argv()
Code.prepend_path(ebin)

packet_dir =
  case rest do
    [dir] -> dir
    [] -> Path.join(Path.dirname(ebin), "packet")
  end

if File.dir?(Path.join(packet_dir, "ebin")), do: Code.prepend_path(Path.join(packet_dir, "ebin"))

budget_ms =
  case System.get_env("HOSTILE_CASE_BUDGET_MS", "") do
    "" -> 5000
    ms -> String.to_integer(ms)
  end

# the heap cap, in words: 256 MiB on a 64-bit BEAM
heap_words = div(256 * 1024 * 1024, :erlang.system_info(:wordsize))

# the retaining load's two stores: roomy, so the verdict is the load's and never
# the caller's capacity
retain_bytes = 1_048_576
fresh_retain = fn -> %{records: [], used: 0, cap: retain_bytes, ids: [], id_cap: div(retain_bytes, 8)} end

empty_report = %{
  unknown: 0,
  kind_mismatch: 0,
  widened: 0,
  clamped: 0,
  malformed: false,
  retained: 0,
  retain_lost: 0,
  reason: ""
}

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

# Measure, Save and the text writer on an accepted value; a raise in any of
# them is the case's answer, a clean refusal is not.
after_accept = fn
  {:accept, _} = verdict, step ->
    try do
      step.()
      verdict
    rescue
      e -> {:after_accept_raised, Exception.message(e)}
    catch
      kind, value -> {:after_accept_threw, kind, value}
    end

  verdict, _step ->
    verdict
end

exercise = fn mod, table, value, input ->
  if input != nil, do: mod.load_measure(table, input)

  case mod.save(table, value) do
    {:ok, saved} -> mod.load_measure(table, saved)
    {:error, _} -> :ok
  end

  mod.to_json(table, value)
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

# ---- the packet carrier ----

packet_units =
  case File.read(Path.join(packet_dir, "units.tsv")) do
    {:ok, text} ->
      text
      |> String.split("\n", trim: true)
      |> Map.new(fn line -> List.to_tuple(String.split(line, "\t", parts: 2)) end)

    _ ->
      %{}
  end

# the codec module of a packet schema: <Package>.<File>, the package read off
# the schema's own `package` line
packet_module = fn schema ->
  if not Map.has_key?(packet_units, schema), do: raise("no Elixir packet unit for schema #{schema}")
  [_, package] = Regex.run(~r/^\s*package\s+([A-Za-z_][A-Za-z0-9_]*)/m, File.read!(schema))
  Module.concat([Macro.camelize(package), Path.basename(schema, ".schema")])
end

packet_fun = fn mod, prefix, type, suffix ->
  want = String.downcase(type)

  name =
    Enum.find_value(mod.__info__(:functions), fn {f, _arity} ->
      s = Atom.to_string(f)

      if String.starts_with?(s, prefix) and String.ends_with?(s, suffix) and
           s |> String.slice(String.length(prefix)..(-String.length(suffix) - 1)//1) |> String.replace("_", "") == want,
         do: f
    end)

  name || raise("#{inspect(mod)} carries no #{prefix}<#{type}>#{suffix}")
end

packet = fn schema, type, payload ->
  mod = packet_module.(schema)

  case apply(mod, packet_fun.(mod, "read_", type, ""), [payload, byte_size(payload) * 8]) do
    {:ok, value} -> {:accept, sha.(apply(mod, packet_fun.(mod, "write_", type, "!"), [value]))}
    :error -> {:refuse, "packet_refused"}
  end
end

run = fn carrier, schema, table, payload ->
  case carrier do
    "text" ->
      mod = Map.fetch!(tables, schema)

      case apply(mod, :from_json, [table, payload]) do
        {:ok, value, _report} ->
          after_accept.({:accept, digest.(mod, table, value)}, fn -> exercise.(mod, table, value, nil) end)

        {:error, r, _report} ->
          {:refuse, reason.(r)}
      end

    "wire" ->
      mod = Map.fetch!(tables, schema)

      case apply(mod, :load, [table, payload]) do
        {:ok, value, _report} ->
          after_accept.({:accept, digest.(mod, table, value)}, fn -> exercise.(mod, table, value, payload) end)

        {:error, r, _report} ->
          {:refuse, reason.(r)}
      end

    "wire_retain" ->
      mod = Map.fetch!(tables, schema)

      case mod.load_retain(table, payload, fresh_retain.(), empty_report) do
        {:ok, value, _report, retain} ->
          after_accept.({:accept, digest.(mod, table, value)}, fn ->
            mod.measure_retain(table, value, retain)
            mod.save_retain(table, value, retain, empty_report)
            mod.to_json(table, value)
          end)

        {:error, r, _report, _retain} ->
          {:refuse, reason.(r)}
      end

    "message" ->
      mod = Map.fetch!(tables, schema)
      message = Map.fetch!(messages, schema)
      {:ok, vocab} = message.announce_read(message.announce())

      case message.load_messages(vocab, payload, 1) do
        {:ok, [[]] = values} ->
          # the batch's one body sets no field: the decoded value is the table's
          # defaults, and from_json/2 is where the port states them.
          {:ok, defaults, _report} = mod.from_json(table, "{}")

          after_accept.({:accept, digest.(mod, table, defaults)}, fn ->
            message.measure_messages(vocab, values)
            message.save_messages(vocab, values)
          end)

        {:ok, values} ->
          {:unhandled_decoded_value, values}

        {:error, r} ->
          {:refuse, reason.(r)}

        {:error, r, _n} ->
          {:refuse, reason.(r)}

        {:malformed, _n, _bodies} ->
          {:refuse, "damaged"}
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

    "packet" ->
      packet.(schema, table, payload)
  end
end

# One case in its own process: the heap cap, the budget, and every raise,
# throw, exit or kill as the case's answer.
isolated = fn carrier, schema, table, payload ->
  parent = self()
  tag = make_ref()

  {pid, monitor} =
    spawn_monitor(fn ->
      Process.flag(:max_heap_size, %{size: heap_words, kill: true, error_logger: false})

      got =
        try do
          run.(carrier, schema, table, payload)
        rescue
          e -> {:raised, Exception.message(e)}
        catch
          kind, value -> {:threw, kind, value}
        end

      send(parent, {tag, got})
    end)

  receive do
    {^tag, got} ->
      Process.demonitor(monitor, [:flush])
      got

    {:DOWN, ^monitor, :process, ^pid, why} ->
      {:died, why}
  after
    budget_ms ->
      Process.exit(pid, :kill)
      Process.demonitor(monitor, [:flush])

      receive do
        {^tag, got} -> got
      after
        0 -> {:over_budget, budget_ms}
      end
  end
end

{passed, carried, bad, stale} =
  Enum.reduce(manifest, {0, 0, [], []}, fn [name, carrier, schema, table, input, _prov, verdict, want_reason, want_digest], {p, c, bad, stale} ->
    payload = File.read!(Path.join(corpus, input))
    expected = if verdict == "accept", do: {:accept, want_digest}, else: {:refuse, want_reason}

    got = isolated.(carrier, schema, table, payload)

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

names = MapSet.new(manifest, &hd/1)

orphans =
  for {name, {slug, _, _}} <- findings, not MapSet.member?(names, name) do
    IO.puts("STALE #{name}: FIX-CARD #{slug} names no case the manifest carries")
    name
  end

stale = stale ++ orphans

IO.puts("#{length(manifest)} cases: #{passed} pass, #{carried} carried as FIX-CARD, #{length(bad)} unexplained, #{length(stale)} stale")

if bad != [] or stale != [] do
  System.halt(1)
end
