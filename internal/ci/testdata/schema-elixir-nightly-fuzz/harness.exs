# The Elixir port's nightly wire/text fuzzer (schema-elixir-nightly-fuzz).
#
# One BEAM, one generated unit's Table module per line of the corpus file the
# make target writes. For every table the module's own descriptors name, a
# VALID encoding is generated through the public API (to_json of the default
# value, from_json back to a value, save to the wire), then mutated and fed to
# the three strict readers:
#
#   load    Table.load/2 on mutated wire bytes
#   strict  load, save the value back, and load the saved bytes again; a load
#           that succeeds but cannot round-trip is a finding
#   json    Table.from_json/2 on mutated text, then to_json of the value
#
# The oracle: a refusal is a clean {:error, atom, report} tuple. A raise, an
# exit or a throw that escapes a reader is a finding, and so is a silent
# success that no round trip can carry. A hang is the caller's timeout (the
# make target wraps this script in `timeout`).
#
# Mutations are the four shapes the card names: bit flips, truncation, splice
# with another seed, and boundary integers written at a random offset. The
# seeds are this unit's own valid encodings; extra raw seed files come from the
# Go fuzz corpus and the conformance vectors named by --seeds.
#
# usage: harness.exs <corpus> <seconds> <saved-dir> [seed-dir...]
#        harness.exs --replay <corpus> <saved-dir> [seed-dir...]
#
# The clock is an argument (seconds, and CHECK_SECONDS is never used by the
# replay arm); the seed is SCHEMA_ELIXIR_FUZZ_SEED, default 20261018.

defmodule SchemaElixirNightlyFuzz do
  @readers [:load, :strict, :json]
  @max_extra_seeds 64

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

    titles =
      for %{name: name, path: path, module_name: module_name} <- units do
        module = Module.concat([module_name, Table])
        tables = seed_tables(module)
        if tables == [], do: raise("unit #{name} (#{path}) has no table that yields a valid encoding")
        %{unit: name, module: module, path: path, tables: tables}
      end

    extra = extra_seeds(seed_dirs)
    :rand.seed(:exsss, {seed(), seed() + 369, seed() + 7})
    File.mkdir_p!(saved)
    deadline = System.monotonic_time(:millisecond) + seconds * 1000
    count = Enum.sum(Enum.map(titles, &length(&1.tables)))
    IO.puts("schema-elixir-nightly-fuzz: #{count} tables over #{length(titles)} units, #{length(extra)} extra seed files, #{seconds}s")
    {mutants, findings} = loop(titles, extra, deadline, 0, 0, saved)
    IO.puts("schema-elixir-nightly-fuzz: #{mutants} mutants, #{findings} findings at seed #{seed()}")

    if findings > 0 do
      IO.puts(:stderr, "schema-elixir-nightly-fuzz: #{findings} saved input(s) under #{saved}; replay them with the replay target")
      System.halt(1)
    end
  end

  defp loop(titles, extra, deadline, mutants, findings, saved) do
    if System.monotonic_time(:millisecond) >= deadline do
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
          path = save(saved, title.unit, seed.table, kind, input)
          IO.puts(:stderr, "FINDING #{title.unit} #{seed.table} #{kind} #{path}: #{reason}")
          loop(titles, extra, deadline, mutants + 1, findings + 1, saved)
      end
    end
  end

  # ---- the replay arm (no clock, no mutation) ------------------------------

  defp replay(corpus, saved) do
    units = Map.new(read_corpus(corpus), &{&1.name, &1})
    files = saved |> File.ls!() |> Enum.reject(&(&1 == ".gitkeep"))

    if files == [] do
      IO.puts("schema-elixir-nightly-fuzz replay: no saved inputs (0)")
    else
      Enum.each(files, fn file ->
        {unit, table, kind} = decode_name(file)
        title = Map.fetch!(units, unit)
        input = File.read!(Path.join(saved, file))

        case read(kind, title.module, table, input) do
          :ok ->
            :ok

          {:finding, reason} ->
            IO.puts(:stderr, "REPLAY FAILED #{file}: #{reason}")
            System.halt(1)
        end
      end)

      IO.puts("schema-elixir-nightly-fuzz replay: #{length(files)} saved input(s) still refused or round-trip clean")
    end
  end

  # ---- the three readers and the oracle ------------------------------------

  defp read(:load, module, table, bytes) do
    try do
      case module.load(table, bytes) do
        {:ok, _value, _report} -> :ok
        {:error, _reason, _report} -> :ok
        other -> {:finding, "load returned #{inspect(other)}"}
      end
    rescue
      error -> {:finding, "load raised #{Exception.format(:error, error, __STACKTRACE__)}"}
    catch
      kind, value -> {:finding, "load #{kind} #{inspect(value)}"}
    end
  end

  defp read(:strict, module, table, bytes) do
    try do
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
    rescue
      error -> {:finding, "strict raised #{Exception.format(:error, error, __STACKTRACE__)}"}
    catch
      kind, value -> {:finding, "strict #{kind} #{inspect(value)}"}
    end
  end

  defp read(:json, module, table, text) do
    try do
      case module.from_json(table, text) do
        {:ok, value, _report} ->
          case module.to_json(table, value) do
            {:ok, _text} -> :ok
            {:error, _reason} -> :ok
            other -> {:finding, "json: to_json returned #{inspect(other)}"}
          end

        {:error, _reason, _report} ->
          :ok

        other ->
          {:finding, "json returned #{inspect(other)}"}
      end
    rescue
      error -> {:finding, "json raised #{Exception.format(:error, error, __STACKTRACE__)}"}
    catch
      kind, value -> {:finding, "json #{kind} #{inspect(value)}"}
    end
  end

  # ---- valid encodings, one per table --------------------------------------

  defp seed_tables(module) do
    for {table, _desc} <- module.descriptors(), reduce: [] do
      acc ->
        try do
          case module.to_json(table, %{}) do
            {:ok, text} ->
              case module.from_json(table, text) do
                {:ok, value, _report} ->
                  case module.save(table, value) do
                    {:ok, wire} -> [%{table: table, wire: wire, json: text} | acc]
                    _ -> acc
                  end

                _ ->
                  case module.save(table, %{}) do
                    {:ok, wire} -> [%{table: table, wire: wire, json: text} | acc]
                    _ -> acc
                  end
              end

            _ ->
              case module.save(table, %{}) do
                {:ok, wire} -> [%{table: table, wire: wire, json: nil} | acc]
                _ -> acc
              end
          end
        rescue
          _ -> acc
        end
    end
  end

  # ---- the four mutations the card names -----------------------------------

  defp mutate(:json, %{json: nil} = seed, extra), do: mutate(:load, seed, extra)

  defp mutate(_kind, seed, extra) do
    source = if :rand.uniform(4) == 1 and extra != [], do: Enum.random(extra), else: seed_base(seed)
    mutate_bytes(source, extra)
  end

  defp seed_base(%{wire: wire}), do: wire

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
        |> Enum.take(@max_extra_seeds)
        |> Enum.map(&File.read!/1)
      else
        []
      end
    end)
    |> Enum.uniq()
  end

  defp save(dir, unit, table, kind, input) do
    name = Enum.join([unit, table, kind, Integer.to_string(System.unique_integer([:positive]))], "__") <> ".bin"
    path = Path.join(dir, name)
    File.write!(path, input)
    path
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
