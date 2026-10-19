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

    tables_index =
      Enum.reduce(modules, %{}, fn mod, acc -> Map.merge(acc, mod.descriptors()) end)

    :persistent_term.put({__MODULE__, :tables}, tables_index)

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
    |> Enum.map(fn dir ->
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
    end)
    |> Enum.reject(&is_nil/1)
  end

  # ---- the two properties -------------------------------------------------

  defp roundtrip(tables, runs, seed, plant) do
    Enum.each(tables, fn {mod, name, desc} ->
      gen = gen_desc(desc, 3)

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
        StreamData.bind(gen_desc(desc, 3), fn value ->
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

  defp gen_desc(desc, depth) do
    if depth <= 0 do
      StreamData.constant(defaults(desc))
    else
      desc.fields
      |> Map.new(fn f -> {f.name, gen_field(f, depth - 1)} end)
      |> StreamData.fixed_map()
    end
  end

  defp defaults(desc) do
    Map.new(desc.fields, fn f -> {f.name, Map.get(f, :default)} end)
  end

  defp gen_field(%{t: :int} = f, _depth) do
    lo = Map.get(f, :min)
    hi = Map.get(f, :max)

    cond do
      is_integer(lo) and is_integer(hi) -> StreamData.integer(lo..hi)
      is_integer(lo) -> StreamData.integer(lo..(lo + 1000))
      is_integer(hi) -> StreamData.integer((hi - 1000)..hi)
      true -> StreamData.integer(-1000..1000)
    end
  end

  defp gen_field(%{t: :bool}, _depth), do: StreamData.boolean()

  defp gen_field(%{t: :float} = f, _depth) do
    # values that are exact in the fixed-point and float32 encodings the
    # corpus uses: n / 1024 needs at most 21 bits, inside float32's mantissa
    lo = -4096
    hi = 4096
    _ = f
    StreamData.map(StreamData.integer(lo..hi), fn n -> n / 1024 end)
  end

  defp gen_field(%{t: :string} = f, _depth) do
    max = min(Map.get(f, :size) || 16, 16)
    StreamData.string(:ascii, max_length: max)
  end

  defp gen_field(%{t: :wstring} = f, _depth) do
    max = min(Map.get(f, :size) || 16, 16)
    StreamData.string(:ascii, max_length: max)
  end

  defp gen_field(%{t: :bytes} = f, _depth) do
    case Map.get(f, :size) do
      n when is_integer(n) ->
        StreamData.map(StreamData.list_of(StreamData.integer(0..255), length: n), &:erlang.list_to_binary/1)

      _ ->
        StreamData.binary(max_length: 8)
    end
  end

  defp gen_field(%{t: :enum} = f, _depth) do
    f.by_name |> Map.keys() |> StreamData.member_of()
  end

  defp gen_field(%{t: kind} = f, depth) when kind in [:table, :record] do
    name = Map.get(f, :elem)

    case name && table_desc(name) do
      nil -> StreamData.constant(Map.get(f, :default) || %{})
      desc -> gen_desc(desc, depth)
    end
  end

  defp gen_field(%{t: :list} = f, depth) do
    element = gen_element(f, depth)

    case Map.get(f, :fixed_array) do
      n when is_integer(n) ->
        StreamData.list_of(element, length: n)

      _ ->
        max = f |> Map.get(:max_count) |> then(&if(is_integer(&1), do: min(&1, 4), else: 4))
        StreamData.list_of(element, max_length: max)
    end
  end

  defp gen_field(%{t: :keyed} = f, depth) do
    item = gen_field(f.item, depth)

    f.key_names
    |> Map.new(fn key -> {key, item} end)
    |> StreamData.fixed_map()
  end

  defp gen_field(%{t: kind}, _depth) when kind in [:union, :pointer, :blob, :void] do
    StreamData.constant(nil)
  end

  defp gen_field(%{t: :map}, _depth), do: StreamData.constant(%{})

  defp gen_field(f, _depth), do: StreamData.constant(Map.get(f, :default))

  defp gen_element(f, depth) do
    case Map.get(f, :elem_t) do
      :record ->
        desc = f |> Map.get(:elem) |> table_desc()

        if desc, do: gen_desc(desc, depth), else: StreamData.constant(%{})

      :enum ->
        f.by_name |> Map.keys() |> StreamData.member_of()

      :int ->
        lo = f |> Map.get(:bound) |> bound(0, -1000)
        hi = f |> Map.get(:bound) |> bound(1, 1000)
        StreamData.integer(lo..hi)

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

  defp bound({lo, hi}, 0, _), do: lo
  defp bound({_lo, hi}, 1, _), do: hi
  defp bound(_, 0, d), do: d
  defp bound(_, 1, d), do: d

  defp table_desc(name) do
    :persistent_term.get({__MODULE__, :tables}, %{}) |> Map.get(name)
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
