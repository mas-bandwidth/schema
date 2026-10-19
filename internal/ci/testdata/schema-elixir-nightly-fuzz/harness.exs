# The Elixir port's nightly wire/text fuzzer (schema-elixir-nightly-fuzz).
#
# One BEAM, one generated unit's Table module per line of the corpus file the
# make target writes. For every table the module's own descriptors name, VALID
# encodings are generated through the public API (the declared default value
# and one non-default variant per scalar field), spliced with the valid
# encodings found in the Go fuzz corpus and the conformance vectors, then
# mutated and fed to every reader entry point the generated module carries:
#
#   load            Table.load/2 on mutated wire bytes
#   load_retain     Table.load_retain/4 on mutated wire bytes
#   load_measure    Table.load_measure/2 on mutated wire bytes
#   strict          load, save the value back, load the saved bytes again
#   json            Table.from_json/2 on mutated text, then to_json back
#   save_retain     load the mutant, then Table.save_retain/4 of its value
#   measure_retain  load the mutant, then Table.measure_retain/3 of its value
#   cook_from       load the mutant, measure it, then Table.cook_from/4
#   cook_measure    load the mutant, then Table.cook_measure/2 of its value
#
# The oracle: a refusal is a clean {:error, atom, report} tuple (or -1/false
# from the measure and cook verbs). A raise, an exit or a throw that escapes a
# reader is a finding. A strict read or a JSON read that returns a value with
# report.malformed set is a finding, and so is a from_json value that to_json
# refuses. A hang is the caller's timeout (the make target wraps this script in
# `timeout`).
#
# Mutations are the four shapes the card names: bit flips, truncation, splice
# with another seed, and boundary integers written at a random offset. Each
# kind mutates its own seed: JSON text for :json, wire bytes for the rest.
#
# usage: harness.exs <corpus> <seconds> <saved-dir> [seed-dir...]
#        harness.exs --replay <corpus> <saved-dir> [seed-dir...]
#
# The clock is an argument (seconds); the seed is SCHEMA_ELIXIR_FUZZ_SEED,
# default 20261018.

defmodule SchemaElixirNightlyFuzz do
  @readers [
    :load,
    :load_retain,
    :load_measure,
    :strict,
    :json,
    :save_retain,
    :measure_retain,
    :cook_from,
    :cook_measure
  ]
  @max_extra_seeds 64
  @max_findings 32
  @max_variants 4
  @max_table_extras 3
  @retain_cap 65_536

  def main(argv) do
    case argv do
      ["--replay", corpus, saved | _rest] ->
        replay(corpus, saved)

      [corpus, seconds, saved | seed_dirs] ->
        run(corpus, String.to_integer(seconds), saved, seed_dirs)

      _ ->
        IO.puts(:stderr, "usage: harness.exs <corpus> <seconds> <saved-dir> [seed-dir...]")
        System.halt(2)
    end
  end

  # ---- the nightly run -----------------------------------------------------

  defp run(corpus, seconds, saved, seed_dirs) do
    units = read_corpus(corpus)
    if units == [], do: raise("the corpus names no unit")

    extra = extra_seeds(seed_dirs)

    titles =
      for %{name: name, path: path, module_name: module_name} <- units do
        module = Module.concat([module_name, Table])
        tables = seed_tables(module, extra)

        if tables == [],
          do: raise("unit #{name} (#{path}) has no table that yields a valid encoding")

        %{unit: name, module: module, path: path, tables: tables}
      end

    :rand.seed(:exsss, {seed(), seed() + 369, seed() + 7})
    File.mkdir_p!(saved)
    deadline = System.monotonic_time(:millisecond) + seconds * 1000
    count = Enum.sum(Enum.map(titles, &length(&1.tables)))

    IO.puts(
      "schema-elixir-nightly-fuzz: #{count} seeds over #{length(titles)} units, " <>
        "#{length(extra)} extra seed files, #{seconds}s, #{length(@readers)} readers"
    )

    {mutants, findings} = loop(titles, extra, deadline, 0, 0, saved)
    IO.puts("schema-elixir-nightly-fuzz: #{mutants} mutants, #{findings} findings at seed #{seed()}")

    if findings > 0 do
      IO.puts(
        :stderr,
        "schema-elixir-nightly-fuzz: #{findings} saved input(s) under #{saved}; replay them with the replay target"
      )

      System.halt(1)
    end
  end

  defp loop(titles, extra, deadline, mutants, findings, saved) do
    if System.monotonic_time(:millisecond) >= deadline or findings >= @max_findings do
      {mutants, findings}
    else
      title = Enum.at(titles, :rand.uniform(length(titles)) - 1)
      seed = Enum.at(title.tables, :rand.uniform(length(title.tables)) - 1)
      kind = Enum.at(@readers, :rand.uniform(length(@readers)) - 1)
      input = mutate(kind, seed, extra)

      case read(kind, title.module, seed.table, input) do
        :ok ->
          loop(titles, extra, deadline, mutants + 1, findings, saved)

        {:finding, reason} ->
          input = minimize(kind, title.module, seed.table, input)
          path = save(saved, title.unit, seed.table, kind, input)
          IO.puts(:stderr, "FINDING #{title.unit} #{seed.table} #{kind} #{path}: #{reason}")
          loop(titles, extra, deadline, mutants + 1, findings + 1, saved)
      end
    end
  end

  # ---- the replay arm (no clock, no mutation) ------------------------------

  defp replay(corpus, saved) do
    units = Map.new(read_corpus(corpus), &{&1.name, &1})

    files =
      case File.ls(saved) do
        {:ok, files} ->
          Enum.reject(files, &(&1 == ".gitkeep"))

        {:error, reason} ->
          IO.puts(:stderr, "schema-elixir-nightly-fuzz replay: cannot read #{saved}: #{reason}")
          System.halt(1)
      end

    if files == [] do
      IO.puts("schema-elixir-nightly-fuzz replay: no saved inputs (0)")
    else
      Enum.each(files, fn file ->
        {unit, table, kind} = decode_name(file)
        title = Map.fetch!(units, unit)
        module = Module.concat([title.module_name, Table])
        input = File.read!(Path.join(saved, file))

        case read(kind, module, table, input) do
          :ok ->
            :ok

          {:finding, reason} ->
            IO.puts(:stderr, "REPLAY FAILED #{file}: #{reason}")
            System.halt(1)
        end
      end)

      IO.puts(
        "schema-elixir-nightly-fuzz replay: #{length(files)} saved input(s) still refused or round-trip clean"
      )
    end
  end

  # ---- the readers and the oracle ------------------------------------------

  defp read(kind, module, table, input) do
    guarded(fn -> dispatch(kind, module, table, input) end, Atom.to_string(kind))
  end

  defp guarded(fun, label) do
    try do
      fun.()
    rescue
      error -> {:finding, "#{label} raised #{Exception.format(:error, error, __STACKTRACE__)}"}
    catch
      kind, value -> {:finding, "#{label} #{kind} #{inspect(value)}"}
    end
  end

  defp dispatch(:load, module, table, bytes), do: judge_load(module.load(table, bytes))

  defp dispatch(:load_retain, module, table, bytes),
    do: judge_load_retain(module.load_retain(table, bytes, empty_retain(), empty_report()))

  defp dispatch(:load_measure, module, table, bytes),
    do: judge_measure("load_measure", module.load_measure(table, bytes))

  defp dispatch(:strict, module, table, bytes), do: judge_strict(module, table, bytes)
  defp dispatch(:json, module, table, text), do: judge_json(module, table, text)
  defp dispatch(:save_retain, module, table, bytes), do: judge_value(:save_retain, module, table, bytes)
  defp dispatch(:measure_retain, module, table, bytes), do: judge_value(:measure_retain, module, table, bytes)
  defp dispatch(:cook_from, module, table, bytes), do: judge_value(:cook_from, module, table, bytes)
  defp dispatch(:cook_measure, module, table, bytes), do: judge_value(:cook_measure, module, table, bytes)

  defp judge_load({:ok, _value, _report}), do: :ok
  defp judge_load({:error, _reason, _report}), do: :ok
  defp judge_load(other), do: {:finding, "load returned #{inspect(other)}"}

  defp judge_load_retain({:ok, _value, _report, _retain}), do: :ok
  defp judge_load_retain({:error, _reason, _report, _retain}), do: :ok
  defp judge_load_retain(other), do: {:finding, "load_retain returned #{inspect(other)}"}

  defp judge_measure(_label, n) when is_integer(n) and n >= -1, do: :ok
  defp judge_measure(label, other), do: {:finding, "#{label} returned #{inspect(other)}"}

  defp judge_strict(module, table, bytes) do
    case module.load(table, bytes) do
      {:ok, value, _report} ->
        case module.save(table, value) do
          {:ok, wire} ->
            case module.load(table, wire) do
              {:ok, _value2, _report2} -> :ok
              {:error, reason, _report2} -> {:finding, "strict: saved bytes refused (#{inspect(reason)})"}
              other -> {:finding, "strict: reload returned #{inspect(other)}"}
            end

          {:error, reason} ->
            {:finding, "strict: save refused a loaded value (#{inspect(reason)})"}

          other ->
            {:finding, "strict: save returned #{inspect(other)}"}
        end

      {:error, _reason, _report} ->
        :ok

      other ->
        {:finding, "strict: load returned #{inspect(other)}"}
    end
  end

  defp judge_json(module, table, text) do
    case module.from_json(table, text) do
      {:ok, value, report} ->
        if Map.get(report, :malformed, false) do
          {:finding, "json: from_json accepted malformed text with a value"}
        else
          case module.to_json(table, value) do
            {:ok, _text} ->
              :ok

            {:error, reason} ->
              {:finding, "json: to_json refused a value from_json accepted (#{inspect(reason)})"}

            other ->
              {:finding, "json: to_json returned #{inspect(other)}"}
          end
        end

      {:error, _reason, _report} ->
        :ok

      other ->
        {:finding, "json returned #{inspect(other)}"}
    end
  end

  defp judge_value(kind, module, table, bytes) do
    case module.load(table, bytes) do
      {:ok, value, _report} -> judge_operation(kind, module, table, value)
      {:error, _reason, _report} -> :ok
      other -> {:finding, "#{kind}: load returned #{inspect(other)}"}
    end
  end

  defp judge_operation(:save_retain, module, table, value) do
    case module.save_retain(table, value, empty_retain(), empty_report()) do
      {:ok, _bytes, _report, _retain} -> :ok
      {:error, _reason, _report, _retain} -> :ok
      other -> {:finding, "save_retain returned #{inspect(other)}"}
    end
  end

  defp judge_operation(:measure_retain, module, table, value) do
    judge_measure("measure_retain", module.measure_retain(table, value, empty_retain()))
  end

  defp judge_operation(:cook_measure, module, table, value) do
    judge_measure("cook_measure", module.cook_measure(table, value))
  end

  defp judge_operation(:cook_from, module, table, value) do
    case module.cook_measure(table, value) do
      n when is_integer(n) and n >= 0 ->
        buffer = :binary.copy(<<0>>, n)

        case module.cook_from(table, value, buffer, :little) do
          {true, bytes} ->
            if byte_size(bytes) >= n,
              do: :ok,
              else: {:finding, "cook_from wrote fewer bytes than cook_measure: #{byte_size(bytes)} < #{n}"}

          false ->
            {:finding, "cook_from refused a value cook_measure measured at #{n} bytes"}

          other ->
            {:finding, "cook_from returned #{inspect(other)}"}
        end

      _ ->
        :ok
    end
  end

  defp empty_report do
    %{
      unknown: 0,
      kind_mismatch: 0,
      widened: 0,
      clamped: 0,
      malformed: false,
      retained: 0,
      retain_lost: 0
    }
  end

  defp empty_retain do
    %{records: [], used: 0, cap: @retain_cap, ids: [], id_cap: @retain_cap}
  end

  # ---- valid encodings, one or more per table ------------------------------

  defp seed_tables(module, extra) do
    for {table, desc} <- module.descriptors(), reduce: [] do
      acc -> acc ++ table_seeds(module, table, desc, extra)
    end
  end

  defp table_seeds(module, table, desc, extra) do
    base = default_value(module, table)

    values =
      [base | field_variants(base, desc.fields)]
      |> Enum.take(@max_variants)

    local = Enum.flat_map(values, &encode(module, table, &1))

    wire_extra =
      extra
      |> Enum.filter(fn %{bytes: bytes} -> match?({:ok, _, _}, module.load(table, bytes)) end)
      |> Enum.take(@max_table_extras)
      |> Enum.flat_map(fn %{bytes: bytes} ->
        case module.load(table, bytes) do
          {:ok, value, _report} -> encode(module, table, value)
          _ -> []
        end
      end)

    json_extra =
      extra
      |> Enum.filter(& &1.json)
      |> Enum.filter(fn %{bytes: text} -> match?({:ok, _, _}, module.from_json(table, text)) end)
      |> Enum.take(@max_table_extras)
      |> Enum.flat_map(fn %{bytes: text} ->
        case module.from_json(table, text) do
          {:ok, value, _report} -> encode(module, table, value)
          _ -> []
        end
      end)

    (local ++ wire_extra ++ json_extra) |> Enum.uniq_by(& &1.wire)
  end

  defp default_value(module, table) do
    try do
      case module.to_json(table, %{}) do
        {:ok, text} ->
          case module.from_json(table, text) do
            {:ok, value, _report} -> value
            _ -> %{}
          end

        _ ->
          %{}
      end
    rescue
      _ -> %{}
    end
  end

  defp field_variants(base, fields) do
    Enum.flat_map(fields, fn f ->
      case alternate(f) do
        {:ok, value} -> [Map.put(base, f.name, value)]
        :skip -> []
      end
    end)
  end

  defp alternate(f) do
    case f.t do
      :int ->
        cond do
          not is_integer(f.default) -> :skip
          Map.has_key?(f, :max) and is_integer(f.max) -> {:ok, if(f.default == f.max, do: f.min, else: f.max)}
          true -> {:ok, f.default + 1}
        end

      :enum ->
        case Enum.find(Map.keys(f.by_name), &(&1 != f.default)) do
          nil -> :skip
          name -> {:ok, name}
        end

      :bool ->
        {:ok, not f.default}

      :float ->
        cond do
          Map.has_key?(f, :max) and is_number(f.max) -> {:ok, if(f.default == f.max, do: f.min, else: f.max)}
          true -> {:ok, if(f.default == 0.0, do: 1.5, else: 0.0)}
        end

      :string ->
        {:ok, "fuzz"}

      :wstring ->
        {:ok, "fuzz"}

      :blob ->
        {:ok, <<0, 1, 2, 255>>}

      :bytes ->
        {:ok, <<0, 1, 2, 255>>}

      _ ->
        :skip
    end
  end

  defp encode(module, table, value) do
    try do
      case module.save(table, value) do
        {:ok, wire} ->
          json =
            case module.to_json(table, value) do
              {:ok, text} -> text
              _ -> nil
            end

          [%{table: table, wire: wire, json: json}]

        _ ->
          []
      end
    rescue
      _ -> []
    end
  end

  # ---- the four mutations the card names -----------------------------------

  defp mutate(:json, %{json: nil} = seed, extra), do: mutate(:load, seed, extra)

  defp mutate(kind, seed, extra) do
    pool = if kind == :json, do: Enum.filter(extra, & &1.json), else: extra
    source = if kind == :json, do: seed.json, else: seed.wire

    source =
      if :rand.uniform(4) == 1 and pool != [] do
        Enum.random(pool).bytes
      else
        source
      end

    mutate_bytes(source, Enum.map(pool, & &1.bytes))
  end

  defp mutate_bytes(bytes, extra) do
    size = byte_size(bytes)

    if size == 0 do
      <<>>
    else
      case :rand.uniform(4) do
        1 -> bit_flip(bytes, size)
        2 -> truncate(bytes, size)
        3 -> splice(bytes, size, extra)
        4 -> boundary_integer(bytes, size)
      end
    end
  end

  # a bit flip at a random offset
  defp bit_flip(bytes, size) do
    at = :rand.uniform(size) - 1
    <<head::binary-size(^at), b, tail::binary>> = bytes
    <<head::binary, Bitwise.bxor(b, Bitwise.bsl(1, :rand.uniform(8) - 1)), tail::binary>>
  end

  # truncation at a random offset
  defp truncate(bytes, size), do: binary_part(bytes, 0, :rand.uniform(size))

  # splice: the head of one seed and the tail of another
  defp splice(bytes, size, extra) do
    other = if extra == [], do: bytes, else: Enum.random(extra)

    if byte_size(other) == 0 do
      bytes
    else
      at = :rand.uniform(size) - 1
      from = :rand.uniform(byte_size(other)) - 1
      <<head::binary-size(^at), _::binary>> = bytes
      tail = binary_part(other, from, byte_size(other) - from)
      head <> tail
    end
  end

  # a boundary integer written little-endian at a random offset
  defp boundary_integer(bytes, size) do
    values = [0, 1, 0x7F, 0x80, 0xFF, 0x7FFF, 0xFFFF, 0xFFFFFFFF, 0xFFFFFFFFFFFFFFFF]
    wide = <<Enum.random(values)::little-unsigned-64>>
    at = :rand.uniform(size) - 1
    before = binary_part(bytes, 0, at)
    after_at = min(size, at + 8)
    after_bytes = binary_part(bytes, after_at, size - after_at)
    before <> wide <> after_bytes
  end

  # ---- corpus, seed files, saved inputs ------------------------------------

  defp read_corpus(path) do
    path
    |> File.read!()
    |> String.split("\n", trim: true)
    |> Enum.reject(&String.starts_with?(&1, "#"))
    |> Enum.map(fn line ->
      case String.split(line) do
        ["unit", name, schema_path, module_name] ->
          %{name: name, path: schema_path, module_name: module_name}

        _ ->
          raise("bad corpus line: #{line}")
      end
    end)
  end

  defp extra_seeds(dirs) do
    dirs
    |> Enum.flat_map(fn dir ->
      if File.dir?(dir) do
        Path.wildcard(Path.join(dir, "**/*"))
        |> Enum.filter(&File.regular?/1)
        |> Enum.sort()
        |> Enum.map(fn path ->
          bytes = File.read!(path)
          text = String.trim_leading(bytes)
          %{path: path, bytes: bytes, json: String.starts_with?(text, "{") or String.starts_with?(text, "[")}
        end)
      else
        []
      end
    end)
    |> Enum.uniq_by(& &1.bytes)
    |> Enum.take(@max_extra_seeds)
  end

  defp save(dir, unit, table, kind, input) do
    digest = Integer.to_string(:erlang.phash2(input))
    name = Enum.join([unit, table, kind, digest], "__") <> ".bin"
    path = Path.join(dir, name)
    if File.exists?(path), do: path, else: File.write!(path, input)
    path
  end

  defp minimize(kind, module, table, input) do
    size = byte_size(input)

    if size <= 1 do
      input
    else
      candidate = binary_part(input, 0, div(size, 2))

      case read(kind, module, table, candidate) do
        {:finding, _reason} -> minimize(kind, module, table, candidate)
        :ok -> input
      end
    end
  end

  defp decode_name(file) do
    case String.split(file, "__") do
      [unit, table, kind, _n | _] ->
        {unit, table, String.to_existing_atom(kind)}

      _ ->
        raise("saved input name does not name unit__table__kind: #{file}")
    end
  end

  defp seed do
    System.get_env("SCHEMA_ELIXIR_FUZZ_SEED", "20261018") |> String.to_integer()
  end
end

SchemaElixirNightlyFuzz.main(System.argv())
