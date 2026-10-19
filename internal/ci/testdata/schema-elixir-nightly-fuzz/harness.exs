# The Elixir port's nightly wire/text fuzzer (schema-elixir-nightly-fuzz).
#
# One BEAM, one generated unit's Table module per line of the corpus file the
# make target writes. For every table the module's own descriptors name, VALID
# encodings are generated through the public API (the declared default value
# and one non-default variant per scalar field), spliced with the valid
# encodings found in the Go fuzz corpus and the conformance vectors, then
# mutated and fed to every reader entry point the generated module carries.
#
# The mutator is StreamData (vendored under vendor/stream_data, pinned in the
# make fragment): a generator chooses the table, seed, reader, mutation kind,
# bit, boundary value and offsets, and `StreamData.check_all/3` is the property
# runner. Coverage feedback closes the loop: every generated Table module is
# compiled through `:cover`, and a mutant batch that covers a source line no
# earlier batch covered is added to the splice corpus, so later StreamData
# choices are drawn from inputs that reached new code as well as the valid
# seeds. Nothing here is a bare `:rand` loop.
#
# The readers fed:
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
  @runs_per_batch 250
  @max_cov_corpus 256

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
    started = System.monotonic_time(:millisecond)
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

    File.mkdir_p!(saved)
    deadline = started + seconds * 1000
    count = Enum.sum(Enum.map(titles, &length(&1.tables)))
    covered = start_cover(titles, seconds)

    IO.puts(
      "schema-elixir-nightly-fuzz: #{count} seeds over #{length(titles)} units, " <>
        "#{length(extra)} extra seed files, #{seconds}s, #{length(@readers)} readers, " <>
        "StreamData + :cover"
    )

    state = %{
      titles: titles,
      extra: extra,
      cov: [],
      saved: saved,
      mutants: 0,
      findings: 0,
      batch_inputs: [],
      covered: covered
    }

    Process.put(:schema_elixir_fuzz, state)
    state = stream_loop(deadline, 0)
    IO.puts("schema-elixir-nightly-fuzz: #{state.mutants} mutants, #{state.findings} findings at seed #{seed()}")

    if state.findings > 0 do
      IO.puts(
        :stderr,
        "schema-elixir-nightly-fuzz: #{state.findings} saved input(s) under #{saved}; replay them with the replay target"
      )

      System.halt(1)
    end
  end

  # StreamData is the property runner: each batch is a fresh generator seed and
  # a bounded run count, and the property checks the wall clock before feeding
  # one generated mutant. Coverage is sampled after each batch; a batch that
  # adds a covered source line feeds the next batch's splice corpus.
  defp stream_loop(deadline, round) do
    state = Process.get(:schema_elixir_fuzz)

    if System.monotonic_time(:millisecond) >= deadline or state.findings >= @max_findings do
      state
    else
      Process.put(:schema_elixir_fuzz, %{state | batch_inputs: []})

      try do
        StreamData.check_all(
          mutation_generator(length(state.titles), @readers),
          [initial_seed: {seed() + round, seed() + 369 + round, seed() + 7 + round}, max_runs: @runs_per_batch, max_run_time: 5_000],
          fn spec ->
            if System.monotonic_time(:millisecond) >= deadline, do: throw(:fuzz_done)
            mutant(spec)
            {:ok, spec}
          end
        )
      catch
        :throw, :fuzz_done -> :ok
      end

      Process.put(:schema_elixir_fuzz, apply_coverage(Process.get(:schema_elixir_fuzz)))
      stream_loop(deadline, round + 1)
    end
  end

  # The generator chooses every parameter of one mutant. The fields are read in
  # mutant/1 in the same order.
  defp mutation_generator(title_count, readers) do
    StreamData.tuple({
      StreamData.integer(0..(title_count - 1)),
      StreamData.integer(),
      StreamData.member_of(readers),
      StreamData.member_of([:bit_flip, :truncate, :splice, :boundary_integer]),
      StreamData.integer(),
      StreamData.integer(0..255),
      StreamData.integer(0..7),
      StreamData.member_of([0, 1, 0x7F, 0x80, 0xFF, 0x7FFF, 0xFFFF, 0xFFFFFFFF, 0xFFFFFFFFFFFFFFFF]),
      StreamData.integer()
    })
  end

  defp mutant({title_i, seed_i, kind, mutation, source_i, byte, bit, boundary, offset}) do
    state = Process.get(:schema_elixir_fuzz)
    title = Enum.at(state.titles, rem(abs(title_i), length(state.titles)))
    seed = Enum.at(title.tables, rem(abs(seed_i), length(title.tables)))
    sources = state.extra ++ state.cov
    input = mutate(kind, seed, sources, {mutation, source_i, byte, bit, boundary, offset})

    state = %{state | mutants: state.mutants + 1, batch_inputs: [%{bytes: input, json: kind == :json} | state.batch_inputs]}

    case read(kind, title.module, seed.table, input) do
      :ok ->
        Process.put(:schema_elixir_fuzz, state)

      {:finding, reason} ->
        input = minimize(kind, title.module, seed.table, input)
        path = save(state.saved, title.unit, seed.table, kind, input)
        IO.puts(:stderr, "FINDING #{title.unit} #{seed.table} #{kind} #{path}: #{reason}")
        Process.put(:schema_elixir_fuzz, %{state | findings: state.findings + 1})
    end
  end

  # ---- coverage feedback ---------------------------------------------------

  # Instrumenting a large generated module through :cover costs a few seconds,
  # so the count is capped by the chosen budget: the harness deadline starts
  # before this work and the timed pass must still get most of its budget to
  # mutate. A ten-minute night instruments the whole corpus; a one-minute
  # dispatch instruments a representative sample and keeps its loop.
  defp start_cover(titles, seconds) do
    case :cover.start() do
      {:ok, _} -> :ok
      {:error, {:already_started, _}} -> :ok
      other -> IO.puts(:stderr, "cover: start returned #{inspect(other)}")
    end

    total = length(titles)
    instrumented = max(1, min(total, div(seconds, 15)))
    titles = Enum.take(titles, instrumented)
    IO.puts(:stderr, "cover: instrumenting #{instrumented}/#{total} unit module(s)")

    for %{module: module} <- titles, into: %{} do
      case :cover.compile_beam(module) do
        {:ok, _} ->
          {module, MapSet.new()}

        other ->
          IO.puts(:stderr, "cover: #{inspect(module)} is not instrumented: #{inspect(other)}")
          {module, MapSet.new()}
      end
    end
  end

  defp apply_coverage(state) do
    {covered, added} =
      Enum.reduce(state.covered, {state.covered, 0}, fn {module, seen}, {acc, added} ->
        lines = covered_lines(module)
        fresh = MapSet.difference(lines, seen)

        if MapSet.size(fresh) > 0 do
          IO.puts(:stderr, "cover: #{inspect(module)} +#{MapSet.size(fresh)} line(s)")
        end

        {Map.put(acc, module, MapSet.union(seen, lines)), added + MapSet.size(fresh)}
      end)

    cov =
      if added > 0 do
        (state.cov ++ Enum.reverse(state.batch_inputs))
        |> Enum.uniq_by(& &1.bytes)
        |> Enum.take(-@max_cov_corpus)
      else
        state.cov
      end

    %{state | covered: covered, cov: cov, batch_inputs: []}
  end

  defp covered_lines(module) do
    case :cover.analyse(module, :coverage, :line) do
      {:ok, rows} ->
        for {{^module, line}, {covered, _not}} <- rows, covered > 0, into: MapSet.new(), do: line

      _ ->
        MapSet.new()
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

  # Every choice here comes from the StreamData generator in mutant/1: the
  # mutation kind, the pool index, the bit, the boundary value and the offset.
  defp mutate(:json, %{json: nil} = seed, sources, params), do: mutate(:load, seed, sources, params)

  defp mutate(kind, seed, sources, {mutation, source_i, byte, bit, boundary, offset}) do
    pool = if kind == :json, do: Enum.filter(sources, &Map.get(&1, :json, false)), else: sources
    source = if kind == :json, do: seed.json, else: seed.wire

    source =
      if rem(abs(source_i), 4) == 0 and pool != [] do
        Enum.at(pool, rem(abs(source_i), length(pool))).bytes
      else
        source
      end

    mutate_bytes(source, Enum.map(pool, & &1.bytes), {mutation, source_i, byte, bit, boundary, offset})
  end

  defp mutate_bytes(bytes, _extra, {_mutation, _, _, _, _, _}) when byte_size(bytes) == 0, do: <<>>

  defp mutate_bytes(bytes, extra, {mutation, source_i, byte, bit, boundary, offset}) do
    case mutation do
      :bit_flip -> bit_flip(bytes, byte, bit, offset)
      :truncate -> truncate(bytes, offset)
      :splice -> splice(bytes, extra, source_i, offset)
      :boundary_integer -> boundary_integer(bytes, boundary, offset)
    end
  end

  # a bit flip at a generated offset
  defp bit_flip(bytes, _byte, bit, offset) do
    size = byte_size(bytes)
    at = offset_at(offset, size)
    <<head::binary-size(^at), b, tail::binary>> = bytes
    <<head::binary, Bitwise.bxor(b, Bitwise.bsl(1, rem(abs(bit), 8))), tail::binary>>
  end

  # truncation at a generated offset
  defp truncate(bytes, offset) do
    size = byte_size(bytes)
    binary_part(bytes, 0, offset_at(offset, size))
  end

  # splice: the head of one seed and the tail of another, both offsets generated
  defp splice(bytes, extra, source_i, offset) do
    size = byte_size(bytes)
    other = if extra == [], do: bytes, else: Enum.at(extra, rem(abs(source_i), length(extra)))

    if byte_size(other) == 0 do
      bytes
    else
      at = offset_at(offset, size)
      from = offset_at(source_i, byte_size(other))
      <<head::binary-size(^at), _::binary>> = bytes
      tail = binary_part(other, from, byte_size(other) - from)
      head <> tail
    end
  end

  # a generated boundary integer written little-endian at a generated offset
  defp boundary_integer(bytes, boundary, offset) do
    size = byte_size(bytes)
    wide = <<boundary::little-unsigned-64>>
    at = offset_at(offset, size)
    before = binary_part(bytes, 0, at)
    after_at = min(size, at + 8)
    after_bytes = binary_part(bytes, after_at, size - after_at)
    before <> wide <> after_bytes
  end

  # A negative generated offset means "from the tail": the id trailer is the
  # last eight bytes, and the generator reaching it must not depend on large
  # random integers.
  defp offset_at(offset, size) when offset < 0, do: size - 1 - rem(-offset, size)
  defp offset_at(offset, size), do: rem(offset, size)

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
