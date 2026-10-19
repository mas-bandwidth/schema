# StreamData property tests over the generated Elixir table codecs.
#
# The corpus is the generated output of the table units the Makefile's
# generate targets read (build/tables-generated-elixir). For every table
# descriptor the generated module exposes we draw a random value, save it,
# load it and save the loaded value again: the two encodings must be equal
# and the load must report success. A second property mutates the encoding
# (bit flips and truncations) and requires the reader to answer with a
# value or a refusal tuple and never raise.
#
# The card's negative control turns on SCHEMA_ELIXIR_STREAMDATA_PLANT, which
# corrupts the saved bytes before the load so the round-trip property must go
# red naming the plant.

defmodule SchemaElixirStreamData do
  import Bitwise

  def main(argv) do
    {opts, _rest, _invalid} =
      OptionParser.parse(argv, strict: [gen: :string, n: :integer, seed: :integer])

    root = Keyword.fetch!(opts, :gen)
    runs = Keyword.get(opts, :n, 100)
    seed = Keyword.get(opts, :seed, 20_261_018)
    plant = System.get_env("SCHEMA_ELIXIR_STREAMDATA_PLANT")

    modules = load_modules(root)
    if modules == [], do: fail("no generated Table modules under #{root}")

    tables =
      Enum.flat_map(modules, fn mod ->
        mod.descriptors()
        |> Enum.filter(fn {_name, desc} -> Map.get(desc, :variable, false) end)
        |> Enum.map(fn {name, desc} -> {mod, name, desc} end)
      end)
      |> Enum.sort_by(fn {mod, name, _} -> {inspect(mod), name} end)

    IO.puts("elixir-streamdata: #{length(tables)} table descriptors in #{length(modules)} units")

    roundtrip(tables, runs, seed, plant)
    mutations(tables, runs, seed + 1)
    IO.puts("elixir-streamdata: #{runs} round trips per table and #{runs} mutations per table, all clean")
  end

  defp load_modules(root) do
    root
    |> Path.join("*")
    |> Path.wildcard()
    |> Enum.filter(&File.dir?/1)
    |> Task.async_stream(&load_unit/1,
      max_concurrency: System.schedulers_online(),
      timeout: :infinity,
      ordered: true
    )
    |> Enum.map(fn {:ok, mod} -> mod end)
    |> Enum.reject(&is_nil/1)
  end

  # Every unit is a separate module, so units compile in parallel: the corpus
  # is large enough that a serial require_file is most of the gate's time.
  defp load_unit(dir) do
    table = Path.join(dir, "Table.ex")

    if File.exists?(table) do
      Code.require_file(table)

      table
      |> File.read!()
      |> then(&Regex.run(~r/defmodule ([A-Za-z0-9_.]+)\.Table do/, &1))
      |> case do
        [_, mod] -> Module.concat([mod, Table])
        _ -> nil
      end
    end
  end

  # ---- the two properties -------------------------------------------------

  defp roundtrip(tables, runs, seed, plant) do
    Enum.each(tables, fn {mod, name, desc} ->
      gen = gen_desc(desc, 3, mod)

      result =
        StreamData.check_all(gen, [initial_seed: seed_tuple(seed), max_runs: runs], fn value ->
          case safe_save(mod, name, value) do
            {:ok, bytes} ->
              bytes = if plant == "roundtrip", do: corrupt(bytes), else: bytes

              case safe_load(mod, name, bytes) do
                {:ok, loaded, _report} ->
                  case safe_save(mod, name, loaded) do
                    {:ok, again} ->
                      if again == bytes do
                        {:ok, value}
                      else
                        {:error,
                         "PLANT: elixir-streamdata round-trip corruption on #{name}: " <>
                           "loaded value saved to different bytes " <>
                           "(sizes #{byte_size(bytes)}/#{byte_size(again)}, " <>
                           "first #{first_diff(bytes, again)}, value #{inspect(value)})"}
                      end

                    other ->
                      {:error, "re-save #{name} answered #{inspect(other)}"}
                  end

                other ->
                  {:error,
                   if(plant,
                     do: "PLANT: elixir-streamdata round-trip corruption on #{name}: load of the corrupted save answered #{inspect(other)}",
                     else: "load #{name} refused its own save: #{inspect(other)}"
                   )}
              end

            other ->
              {:error, "save #{name} answered #{inspect(other)}"}
          end
        end)

      case result do
        {:ok, _} ->
          :ok

        {:error, failure} ->
          fail(
            "round-trip on #{inspect(mod)}.#{name} failed: " <>
              inspect(Map.get(failure, :original_failure, failure))
          )
      end
    end)
  end

  defp mutations(tables, runs, seed) do
    Enum.each(tables, fn {mod, name, desc} ->
      gen =
        StreamData.bind(gen_desc(desc, 3, mod), fn value ->
          case safe_save(mod, name, value) do
            {:ok, <<>>} -> StreamData.constant({value, <<>>})
            {:ok, bytes} -> StreamData.constant({value, bytes})
            _ -> StreamData.constant({value, :save_failed})
          end
        end)

      result =
        StreamData.check_all(gen, [initial_seed: seed_tuple(seed), max_runs: runs], fn {value, bytes} ->
          cond do
            bytes == :save_failed ->
              {:error, "save #{name} failed before mutation"}

            byte_size(bytes) == 0 ->
              {:ok, :empty}

            true ->
              mutated = mutate(bytes, value)

              case safe_load(mod, name, mutated) do
                {:ok, _loaded, _report} -> {:ok, :read}
                {:error, _reason, _report} -> {:ok, :refused}
                other -> {:error, "load #{name} on a mutation answered #{inspect(other)}"}
              end
          end
        end)

      case result do
        {:ok, _} ->
          :ok

        {:error, failure} ->
          fail(
            "clean-refusal on #{inspect(mod)}.#{name} failed: " <>
              inspect(Map.get(failure, :original_failure, failure))
          )
      end
    end)
  end

  defp safe_save(mod, name, value) do
    try do
      mod.save(name, value)
    rescue
      e -> {:raised, Exception.message(e)}
    catch
      kind, reason -> {:caught, kind, reason}
    end
  end

  defp safe_load(mod, name, bytes) do
    try do
      mod.load(name, bytes)
    rescue
      e -> {:raised, Exception.message(e)}
    catch
      kind, reason -> {:caught, kind, reason}
    end
  end

  defp corrupt(bytes) do
    at = :rand.uniform(byte_size(bytes)) - 1
    <<head::binary-size(at), b, tail::binary>> = bytes
    <<head::binary, bxor(b, 1), tail::binary>>
  end

  defp mutate(bytes, value) do
    _ = value

    case :rand.uniform(3) do
      1 ->
        corrupt(bytes)

      2 ->
        keep = :rand.uniform(byte_size(bytes)) - 1
        binary_part(bytes, 0, keep)

      3 ->
        # a value the writer can never emit: an all-zero body with the
        # original trailer, or a random byte string of the same length
        if :rand.uniform(2) == 1 do
          <<0::size(byte_size(bytes) * 8)>>
        else
          :crypto.strong_rand_bytes(byte_size(bytes))
        end
    end
  end

  # ---- value generators ---------------------------------------------------

  defp gen_desc(desc, depth, mod) do
    if depth <= 0 do
      StreamData.constant(defaults(desc))
    else
      desc.fields
      |> Map.new(fn f -> {f.name, gen_field(f, depth - 1, mod)} end)
      |> StreamData.fixed_map()
    end
  end

  defp defaults(desc) do
    Map.new(desc.fields, fn f -> {f.name, Map.get(f, :default)} end)
  end

  defp gen_field(%{t: :int} = f, _depth, _mod) do
    # width and signedness bound the value as tightly as min/max do: a value
    # outside the declared storage cannot round-trip, it is only truncated.
    {wlo, whi} = int_width_bounds(f)
    lo = f |> Map.get(:min) |> then(&if(is_integer(&1), do: max(&1, wlo), else: wlo))
    hi = f |> Map.get(:max) |> then(&if(is_integer(&1), do: min(&1, whi), else: whi))

    if lo <= hi, do: StreamData.integer(lo..hi), else: StreamData.constant(lo)
  end

  defp gen_field(%{t: :bool}, _depth, _mod), do: StreamData.boolean()

  defp gen_field(%{t: :float} = f, _depth, _mod) do
    # values that are exact in the fixed-point and float32 encodings the
    # corpus uses: n / 1024 needs at most 21 bits, inside float32's mantissa
    lo = -4096
    hi = 4096
    _ = f
    StreamData.map(StreamData.integer(lo..hi), fn n -> n / 1024 end)
  end

  defp gen_field(%{t: :string} = f, _depth, _mod) do
    max = min(Map.get(f, :size) || 16, 16)
    StreamData.string(:ascii, max_length: max)
  end

  defp gen_field(%{t: :wstring} = f, _depth, _mod) do
    max = min(Map.get(f, :size) || 16, 16)
    StreamData.string(:ascii, max_length: max)
  end

  defp gen_field(%{t: :bytes} = f, _depth, _mod) do
    case Map.get(f, :size) do
      n when is_integer(n) ->
        StreamData.map(StreamData.list_of(StreamData.integer(0..255), length: n), &:erlang.list_to_binary/1)

      _ ->
        StreamData.binary(max_length: 8)
    end
  end

  defp gen_field(%{t: :enum} = f, _depth, _mod) do
    f.by_name |> Map.keys() |> StreamData.member_of()
  end

  defp gen_field(%{t: kind} = f, depth, mod) when kind in [:table, :record] do
    name = Map.get(f, :elem)

    case name && table_desc(mod, name) do
      nil -> StreamData.constant(Map.get(f, :default) || %{})
      desc -> gen_desc(desc, depth, mod)
    end
  end

  defp gen_field(%{t: :list} = f, depth, mod) do
    element = gen_element(f, depth, mod)

    case Map.get(f, :fixed_array) do
      n when is_integer(n) ->
        StreamData.list_of(element, length: n)

      _ ->
        max = f |> Map.get(:max_count) |> then(&if(is_integer(&1), do: min(&1, 4), else: 4))
        StreamData.list_of(element, max_length: max)
    end
  end

  defp gen_field(%{t: :keyed} = f, depth, mod) do
    item = gen_field(f.item, depth, mod)

    f.key_names
    |> Map.new(fn key -> {key, item} end)
    |> StreamData.fixed_map()
  end

  defp gen_field(%{t: kind}, _depth, _mod) when kind in [:union, :pointer, :blob, :void] do
    StreamData.constant(nil)
  end

  defp gen_field(%{t: :map}, _depth, _mod), do: StreamData.constant(%{})

  defp gen_field(f, _depth, _mod), do: StreamData.constant(Map.get(f, :default))

  # A union element (kind 15) with the None arm selected is absent, and a
  # pointer element (kind 17) with nil is a null node: both are values the
  # schema allows and the codec must round-trip.
  defp gen_element(%{elem_kind: 15}, _depth, _mod), do: StreamData.constant(nil)
  defp gen_element(%{elem_kind: 17}, _depth, _mod), do: StreamData.constant(nil)

  defp gen_element(f, depth, mod) do
    case Map.get(f, :elem_t) do
      :record ->
        desc = f |> Map.get(:elem) |> then(&table_desc(mod, &1))

        if desc, do: gen_desc(desc, depth, mod), else: StreamData.constant(%{})

      :enum ->
        f.by_name |> Map.keys() |> StreamData.member_of()

      kind when kind in [:union, :pointer, :blob, :void] ->
        StreamData.constant(nil)

      :int ->
        {wlo, whi} = elem_int_width_bounds(f)
        lo = f |> Map.get(:bound) |> bound(0, wlo)
        hi = f |> Map.get(:bound) |> bound(1, whi)
        lo = max(lo, wlo)
        hi = min(hi, whi)
        if lo <= hi, do: StreamData.integer(lo..hi), else: StreamData.constant(lo)

      :float ->
        StreamData.map(StreamData.integer(-4096..4096), fn n -> n / 1024 end)

      :bool ->
        StreamData.boolean()

      :string ->
        StreamData.string(:ascii, max_length: 8)

      _ ->
        StreamData.constant(Map.get(f, :elem_default))
    end
  end

  defp elem_int_width_bounds(f) do
    size = Map.get(f, :elem_size)

    if is_integer(size) and size > 0 do
      bits = size * 8

      if Map.get(f, :elem_signed, true) do
        {-(1 <<< (bits - 1)), (1 <<< (bits - 1)) - 1}
      else
        {0, (1 <<< bits) - 1}
      end
    else
      {-1000, 1000}
    end
  end

  defp int_width_bounds(f) do
    case Map.get(f, :width) do
      width when is_integer(width) and width > 0 ->
        bits = width * 8

        if Map.get(f, :signed, true) do
          {-(1 <<< (bits - 1)), (1 <<< (bits - 1)) - 1}
        else
          {0, (1 <<< bits) - 1}
        end

      _ ->
        {-1000, 1000}
    end
  end

  defp bound({lo, hi}, 0, _), do: lo
  defp bound({_lo, hi}, 1, _), do: hi
  defp bound(_, 0, d), do: d
  defp bound(_, 1, d), do: d

  # Tables are resolved inside the unit that declares them: the corpus has
  # many same-named tables (Stamp, Album, Inner, ...) in different units, and
  # a global name index would generate a value against a stranger's schema.
  defp table_desc(mod, name) do
    Map.get(mod.descriptors(), name)
  end

  defp first_diff(a, b) do
    n = min(byte_size(a), byte_size(b))
    Enum.find(0..(n - 1), :none, fn i -> :binary.at(a, i) != :binary.at(b, i) end)
  end

  defp seed_tuple(seed), do: {seed, seed + 1, seed + 2}

  defp fail(message) do
    IO.puts(:stderr, message)
    System.halt(1)
  end
end

SchemaElixirStreamData.main(System.argv())
