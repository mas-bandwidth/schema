# StreamData property tests over the generated Elixir table codecs.
#
# The corpus is the generated output of the table units the Makefile's
# generate targets read (build/tables-generated-elixir). For every table
# descriptor the generated module exposes (variable-form and fixed-form tables
# alike) we draw a random value, save it, load it, check that the load report
# is clean and that `load(save(v))` equals `v` under the spec's value equality
# (docs/SPEC-TABLES.md §2.3, §2.4, §3, §3.2, §4), and save the loaded value
# again to verify byte equality (`save(load(save(v))) == save(v)`). A second
# property constructs non-canonical LEB128 wires and requires strict
# `{:error, :malformed, _}` refusal (docs/SPEC-TABLES.md §3), and mutates the
# encoding (bit flips, truncations, random/zero bytes) requiring the reader to
# answer with a value or a refusal tuple and never raise.
#
# The card's negative control turns on SCHEMA_ELIXIR_STREAMDATA_PLANT, which
# corrupts the saved bytes before the load so the round-trip property must go
# red naming the plant; the Makefile negative control also plants a lossy
# `int_bytes` (`min(v, 100)`) and a lax LEB128 reader to verify that both
# turn the gate red.

defmodule SchemaElixirStreamData do
  import Bitwise

  def main(argv) do
    {opts, _rest, _invalid} =
      OptionParser.parse(argv, strict: [gen: :string, n: :integer, seed: :integer])

    root = Keyword.fetch!(opts, :gen)
    runs = Keyword.get(opts, :n, 100)
    seed = Keyword.get(opts, :seed, 20_261_018)
    plant = System.get_env("SCHEMA_ELIXIR_STREAMDATA_PLANT")

    units = load_modules(root)
    if units == [], do: fail("no generated Table modules under #{root}")

    unions = Map.new(units, fn {mod, _rt_mod, u} -> {mod, u} end)
    runtimes = Map.new(units, fn {mod, rt_mod, _u} -> {mod, rt_mod} end)

    tables =
      Enum.flat_map(units, fn {mod, _rt_mod, _u} ->
        mod.descriptors()
        |> Enum.map(fn {name, desc} -> {mod, name, desc} end)
      end)
      |> Enum.sort_by(fn {mod, name, _} -> {inspect(mod), name} end)

    IO.puts("elixir-streamdata: #{length(tables)} table descriptors in #{length(units)} units")

    roundtrip(tables, unions, runs, seed, plant)
    mutations(tables, unions, runtimes, runs, seed + 1)

    IO.puts(
      "elixir-streamdata: #{runs} round trips per table and #{runs} mutations per table, all clean"
    )
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
    |> Enum.map(fn {:ok, unit} -> unit end)
    |> Enum.reject(&is_nil/1)
  end

  # Every unit is a separate module, so units compile in parallel: the corpus
  # is large enough that a serial require_file is most of the gate's time.
  # Compiled BEAM files are cached under Mix.Project.build_path() keyed by the
  # SHA-256 of the source files so unchanged units load immediately while any
  # planted source change compiles fresh.
  defp load_unit(dir) do
    table = Path.join(dir, "Table.ex")
    runtime = Path.join(dir, "TableRuntime.ex")

    if File.exists?(table) do
      source = File.read!(table)

      case Regex.run(~r/defmodule ([A-Za-z0-9_.]+)\.Table do/, source) do
        [_, prefix] ->
          mod = Module.concat([prefix, Table])
          rt_mod = if File.exists?(runtime), do: Module.concat([prefix, TableRuntime]), else: nil
          ensure_compiled(dir, table, runtime, source, mod, rt_mod)
          {mod, rt_mod, extract_unions(source)}

        _ ->
          nil
      end
    end
  end

  defp ensure_compiled(dir, table, runtime, source, mod, rt_mod) do
    rt_source = if rt_mod && File.exists?(runtime), do: File.read!(runtime), else: <<>>
    hash = :crypto.hash(:sha256, [source, 0, rt_source]) |> Base.encode16(case: :lower)

    cache_dir =
      Path.join([
        Mix.Project.build_path(),
        "gen_ebin",
        "#{Path.basename(dir)}-#{binary_part(hash, 0, 16)}"
      ])

    done = Path.join(cache_dir, ".done")

    if not File.exists?(done) do
      File.rm_rf!(cache_dir)
      File.mkdir_p!(cache_dir)
      files = if rt_mod && File.exists?(runtime), do: [runtime, table], else: [table]

      for file <- files do
        for {m, bin} <- Code.compile_file(file) do
          File.write!(Path.join(cache_dir, "#{m}.beam"), bin)
        end
      end

      File.write!(done, <<>>)
    end

    Code.prepend_path(cache_dir)
    Code.ensure_loaded!(mod)
    if rt_mod, do: Code.ensure_loaded!(rt_mod)
  end

  defp extract_unions(source) do
    case Code.string_to_quoted(source) do
      {:ok, ast} ->
        {_ast, found} =
          Macro.prewalk(ast, %{}, fn
            {:@, _, [{:unions, _, [unions_ast]}]} = node, _acc ->
              {val, _} = Code.eval_quoted(unions_ast)
              {node, val}

            node, acc ->
              {node, acc}
          end)

        found

      _ ->
        %{}
    end
  end

  # ---- the two properties -------------------------------------------------

  @clean_report %{
    unknown: 0,
    kind_mismatch: 0,
    widened: 0,
    clamped: 0,
    malformed: false,
    retained: 0,
    retain_lost: 0
  }

  defp roundtrip(tables, unions, runs, seed, plant) do
    Enum.each(tables, fn {mod, name, desc} ->
      gen = gen_desc(desc, 3, mod, unions)

      result =
        StreamData.check_all(gen, [initial_seed: seed_tuple(seed), max_runs: runs], fn value ->
          case safe_save(mod, name, value) do
            {:ok, bytes} ->
              bytes = if plant == "roundtrip", do: corrupt(bytes), else: bytes

              case safe_load(mod, name, bytes) do
                {:ok, loaded, report} ->
                  # Value equality under the spec (docs/SPEC-TABLES.md §2.3,
                  # §2.4, §3, §3.2, §4):
                  # - Root `load/2` merges `defaults(desc)` at the top level,
                  #   while nested `decode/4` returns only the wire-present
                  #   fields for nested tables (`:table`, `:pointer`, list
                  #   elements of kind 13/17, union table arms, keyed table
                  #   slots, and map entry values), and an all-default
                  #   non-optional nested table elides on save (§3) and reads
                  #   back as `%{}` (§4).
                  # - A non-optional fixed array `[N]T` (`fixed_array: n`,
                  #   `elem_kind != 13`) elides when all N elements equal
                  #   `elem_default` (§3) and reads back as `[]`, which pads
                  #   back to N copies of `elem_default`.
                  # - An enum-keyed array `[Enum]T` (`:keyed`, §2.4, §3.2)
                  #   elides slots at `item.default` (or an all-default nested
                  #   table); expanding `key_names` with `item.default`
                  #   restores elided slots.
                  # - Every scalar (`:int`, `:float`, `:bool`, `:string`,
                  #   `:wstring`, `:bytes`, `:enum`, `:blob`) compares by exact
                  #   identity (`==`), so any idempotent lossy encoding (such
                  #   as clamping `int_bytes` with `min(v, 100)`) fails here
                  #   even though `save(load(save(v))) == save(v)` would pass.
                  norm_loaded = spec_normalize(desc, loaded, mod, unions)
                  norm_value = spec_normalize(desc, value, mod, unions)

                  cond do
                    report != @clean_report ->
                      {:error,
                       roundtrip_tag(plant, name) <>
                         "load report not clean: #{inspect(report)} (value #{inspect(value)})"}

                    norm_loaded != norm_value ->
                      {:error,
                       roundtrip_tag(plant, name) <>
                         "loaded value != input value " <>
                         "(loaded #{inspect(norm_loaded)}, expected #{inspect(norm_value)})"}

                    true ->
                      case safe_save(mod, name, loaded) do
                        {:ok, again} ->
                          if again == bytes do
                            {:ok, value}
                          else
                            {:error,
                             roundtrip_tag(plant, name) <>
                               "loaded value saved to different bytes " <>
                               "(sizes #{byte_size(bytes)}/#{byte_size(again)}, " <>
                               "first #{first_diff(bytes, again)}, value #{inspect(value)})"}
                          end

                        other ->
                          {:error, "re-save #{name} answered #{inspect(other)}"}
                      end
                  end

                other ->
                  {:error,
                   if(plant,
                     do:
                       "PLANT: elixir-streamdata round-trip corruption on #{name}: " <>
                         "load of the corrupted save answered #{inspect(other)}",
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
          shrunk = Map.get(failure, :shrunk_failure, failure)

          fail(
            "round-trip on #{inspect(mod)}.#{name} failed (seed=#{seed}): " <>
              inspect(shrunk)
          )
      end
    end)
  end

  defp roundtrip_tag(nil, name), do: "round-trip mismatch on #{name}: "

  defp roundtrip_tag(_plant, name),
    do: "PLANT: elixir-streamdata round-trip corruption on #{name}: "

  defp mutations(tables, unions, runtimes, runs, seed) do
    :rand.seed(:exsss, {seed, seed + 1, seed + 2})

    Enum.each(runtimes, fn {mod, rt_mod} ->
      if rt_mod && function_exported?(rt_mod, :leb_decode, 1) do
        for overlong <- [<<0x80, 0x00>>, <<0x81, 0x00>>] do
          if rt_mod.leb_decode(overlong) != :malformed do
            fail(
              "clean-refusal on #{inspect(mod)} (seed=#{seed}): " <>
                "#{inspect(rt_mod)}.leb_decode accepted non-canonical LEB128 #{inspect(overlong)}"
            )
          end
        end
      end
    end)

    Enum.each(tables, fn {mod, name, desc} ->
      gen =
        StreamData.bind(gen_desc(desc, 3, mod, unions), fn value ->
          case safe_save(mod, name, value) do
            {:ok, <<>>} -> StreamData.constant({value, <<>>})
            {:ok, bytes} -> StreamData.constant({value, bytes})
            _ -> StreamData.constant({value, :save_failed})
          end
        end)

      result =
        StreamData.check_all(gen, [initial_seed: seed_tuple(seed), max_runs: runs], fn {value,
                                                                                        bytes} ->
          cond do
            bytes == :save_failed ->
              {:error, "save #{name} failed before mutation"}

            byte_size(bytes) == 0 ->
              {:ok, :empty}

            true ->
              case check_non_canonical_leb(mod, name, bytes) do
                :ok ->
                  mutated = mutate(bytes, value)

                  case safe_load(mod, name, mutated) do
                    {:ok, _loaded, _report} -> {:ok, :read}
                    {:error, _reason, _report} -> {:ok, :refused}
                    other -> {:error, "load #{name} on a mutation answered #{inspect(other)}"}
                  end

                {:error, _} = err ->
                  err
              end
          end
        end)

      case result do
        {:ok, _} ->
          :ok

        {:error, failure} ->
          shrunk = Map.get(failure, :shrunk_failure, failure)

          fail(
            "clean-refusal on #{inspect(mod)}.#{name} failed (seed=#{seed}): " <>
              inspect(shrunk)
          )
      end
    end)
  end

  # Every length, count, index and id reference on the variable wire is a
  # canonical unsigned LEB128 written with the fewest bytes that carry it, and
  # a non-minimal spelling with a trailing zero group is malformed
  # (docs/SPEC-TABLES.md §3). From any valid save `bytes`:
  # 1. Replace the root body's 1-byte zero terminator `<<0>>` (right before the
  #    id table) with the 2-byte overlong zero `<<0x80, 0x00>>`.
  # 2. When the body carries at least one field, its first byte after the form
  #    byte is the first interned reference `b = 1 < 0x80`; replace `<<b>>`
  #    with the 2-byte overlong spelling `<<b ||| 0x80, 0x00>>`.
  # Both wires must be strictly refused with `{:error, :malformed, _}`.
  defp check_non_canonical_leb(mod, name, bytes) when byte_size(bytes) >= 10 do
    size = byte_size(bytes)
    count = :binary.decode_unsigned(binary_part(bytes, size - 8, 8), :little)
    term_pos = size - 8 - count * 8 - 1

    wires =
      if term_pos >= 1 and :binary.at(bytes, term_pos) == 0 do
        prefix = binary_part(bytes, 0, term_pos)
        trailer = binary_part(bytes, term_pos + 1, size - term_pos - 1)
        overlong_term = <<prefix::binary, 0x80, 0x00, trailer::binary>>

        if term_pos > 1 and :binary.at(bytes, 1) < 0x80 do
          b = :binary.at(bytes, 1)
          rest = binary_part(bytes, 2, size - 2)
          overlong_ref = <<1, b ||| 0x80, 0x00, rest::binary>>
          [{"terminator 0x80 0x00", overlong_term}, {"field ref 0x80 0x00", overlong_ref}]
        else
          [{"terminator 0x80 0x00", overlong_term}]
        end
      else
        []
      end

    Enum.find_value(wires, :ok, fn {label, nc_bytes} ->
      case safe_load(mod, name, nc_bytes) do
        {:error, :malformed, _report} ->
          nil

        other ->
          {:error,
           "load #{name} failed to refuse non-canonical LEB128 (#{label}): answered #{inspect(other)}"}
      end
    end)
  end

  defp check_non_canonical_leb(_mod, _name, _bytes), do: :ok

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
    <<head::binary-size(^at), b, tail::binary>> = bytes
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
          :rand.bytes(byte_size(bytes))
        end
    end
  end

  # ---- spec-defined value normalization (docs/SPEC-TABLES.md §2.3, §2.4, §3, §3.2, §4) ----

  defp spec_normalize(nil, val, _mod, _unions), do: val

  defp spec_normalize(desc, val, mod, unions) when is_map(val) do
    merged = Map.merge(defaults(desc), val)

    Map.new(desc.fields, fn f ->
      {f.name, spec_normalize_field(f, Map.get(merged, f.name), mod, unions)}
    end)
  end

  defp spec_normalize(_desc, val, _mod, _unions), do: val

  defp spec_normalize_field(%{optional: true}, nil, _mod, _unions), do: nil

  defp spec_normalize_field(%{t: kind} = f, val, mod, unions) when kind in [:table, :record] do
    elem_desc = Map.get(f, :elem) && table_desc(mod, f.elem)
    spec_normalize(elem_desc, val || %{}, mod, unions)
  end

  defp spec_normalize_field(%{t: :pointer} = f, val, mod, unions) do
    if val == nil do
      nil
    else
      elem_desc = Map.get(f, :elem) && table_desc(mod, f.elem)
      spec_normalize(elem_desc, val, mod, unions)
    end
  end

  defp spec_normalize_field(%{t: :list} = f, val, mod, unions) do
    elems = val || []

    padded =
      case Map.get(f, :fixed_array) do
        n when is_integer(n) ->
          kept = Enum.take(elems, n)
          kept ++ List.duplicate(Map.get(f, :elem_default), n - length(kept))

        _ ->
          elems
      end

    Enum.map(padded, &spec_normalize_elem(f, &1, mod, unions))
  end

  defp spec_normalize_field(%{t: :keyed} = f, val, mod, unions) do
    slots = val || %{}
    item = Map.put(f.item, :elem_kind, f.elem_kind)

    Map.new(f.key_names, fn key ->
      v = Map.get(slots, key, f.item.default)
      {key, spec_normalize_field(item, v, mod, unions)}
    end)
  end

  defp spec_normalize_field(%{t: :union} = f, val, mod, unions) do
    case val do
      nil ->
        nil

      {arm_name, payload} ->
        u = unions |> Map.get(mod, %{}) |> Map.get(f.elem)
        arm_desc = u && Map.get(u.by_name, arm_name)

        cond do
          arm_desc == nil ->
            {arm_name, payload}

          arm_desc.t == :void ->
            {arm_name, nil}

          true ->
            v = if payload == nil, do: Map.get(arm_desc, :default), else: payload
            {arm_name, spec_normalize_field(arm_desc, v, mod, unions)}
        end
    end
  end

  defp spec_normalize_field(%{t: :map} = f, val, mod, unions) do
    case Map.get(f, :elem) && table_desc(mod, f.elem) do
      %{fields: [_kf, vf | _]} ->
        Map.new(val || %{}, fn {k, v} ->
          key_str = if is_binary(k), do: k, else: Integer.to_string(k)
          {key_str, spec_normalize_field(vf, v, mod, unions)}
        end)

      _ ->
        val || %{}
    end
  end

  defp spec_normalize_field(_f, val, _mod, _unions), do: val

  defp spec_normalize_elem(%{elem_kind: 13} = f, e, mod, unions) do
    elem_desc = Map.get(f, :elem) && table_desc(mod, f.elem)
    spec_normalize(elem_desc, e || %{}, mod, unions)
  end

  defp spec_normalize_elem(%{elem_kind: 17} = f, e, mod, unions) do
    if e == nil do
      nil
    else
      elem_desc = Map.get(f, :elem) && table_desc(mod, f.elem)
      spec_normalize(elem_desc, e, mod, unions)
    end
  end

  defp spec_normalize_elem(%{elem_kind: 15} = f, e, mod, unions) do
    spec_normalize_field(%{t: :union, elem: f.elem}, e, mod, unions)
  end

  defp spec_normalize_elem(_f, e, _mod, _unions), do: e

  # ---- value generators ---------------------------------------------------

  defp gen_desc(desc, depth, mod, unions) do
    if depth <= 0 do
      StreamData.constant(defaults(desc))
    else
      desc.fields
      |> Map.new(fn f -> {f.name, gen_maybe_optional(f, depth - 1, mod, unions)} end)
      |> StreamData.fixed_map()
    end
  end

  defp defaults(desc) do
    Map.new(desc.fields, fn f -> {f.name, Map.get(f, :default)} end)
  end

  defp gen_maybe_optional(%{optional: true} = f, depth, mod, unions) do
    StreamData.one_of([
      StreamData.constant(nil),
      gen_field(Map.delete(f, :optional), depth, mod, unions)
    ])
  end

  defp gen_maybe_optional(f, depth, mod, unions), do: gen_field(f, depth, mod, unions)

  defp gen_field(%{t: :int} = f, _depth, _mod, _unions) do
    # width and signedness bound the value as tightly as min/max do: a value
    # outside the declared storage cannot round-trip, it is only truncated.
    {wlo, whi} = int_width_bounds(f)
    lo = f |> Map.get(:min) |> then(&if(is_integer(&1), do: max(&1, wlo), else: wlo))
    hi = f |> Map.get(:max) |> then(&if(is_integer(&1), do: min(&1, whi), else: whi))

    if lo <= hi, do: StreamData.integer(lo..hi), else: StreamData.constant(lo)
  end

  defp gen_field(%{t: :bool}, _depth, _mod, _unions), do: StreamData.boolean()

  defp gen_field(%{t: :float} = f, _depth, _mod, _unions) do
    # values that are exact in the fixed-point and float32 encodings the
    # corpus uses: n / 1024 needs at most 21 bits, inside float32's mantissa
    lo = -4096
    hi = 4096
    _ = f
    StreamData.map(StreamData.integer(lo..hi), fn n -> n / 1024 end)
  end

  defp gen_field(%{t: :string} = f, _depth, _mod, _unions) do
    max = min(Map.get(f, :size) || 16, 16)
    StreamData.string(:ascii, max_length: max)
  end

  defp gen_field(%{t: :wstring} = f, _depth, _mod, _unions) do
    max = min(Map.get(f, :size) || 16, 16)
    StreamData.string(:ascii, max_length: max)
  end

  defp gen_field(%{t: :bytes} = f, _depth, _mod, _unions) do
    case Map.get(f, :size) do
      n when is_integer(n) ->
        StreamData.map(
          StreamData.list_of(StreamData.integer(0..255), length: n),
          &:erlang.list_to_binary/1
        )

      _ ->
        StreamData.binary(max_length: 8)
    end
  end

  defp gen_field(%{t: :enum} = f, _depth, _mod, _unions) do
    f.by_name |> Map.keys() |> Enum.sort() |> StreamData.member_of()
  end

  defp gen_field(%{t: kind} = f, depth, mod, unions) when kind in [:table, :record] do
    name = Map.get(f, :elem)

    case name && table_desc(mod, name) do
      nil -> StreamData.constant(Map.get(f, :default) || %{})
      desc -> gen_desc(desc, depth, mod, unions)
    end
  end

  defp gen_field(%{t: :pointer} = f, depth, mod, unions) do
    name = Map.get(f, :elem)

    case depth > 0 && name && table_desc(mod, name) do
      %{} = desc ->
        StreamData.one_of([
          StreamData.constant(nil),
          gen_desc(desc, depth - 1, mod, unions)
        ])

      _ ->
        StreamData.constant(nil)
    end
  end

  defp gen_field(%{t: :blob} = f, _depth, _mod, _unions) do
    payload =
      case Map.get(f, :blob) do
        :string -> StreamData.string(:ascii, max_length: 8)
        _ -> StreamData.binary(max_length: 8)
      end

    StreamData.one_of([StreamData.constant(nil), payload])
  end

  defp gen_field(%{t: :union} = f, depth, mod, unions) do
    gen_union(Map.get(f, :elem), depth, mod, unions)
  end

  defp gen_field(%{t: :map} = f, depth, mod, unions) do
    case depth > 0 && Map.get(f, :elem) && table_desc(mod, f.elem) do
      %{fields: [kf, vf | _]} ->
        key_gen =
          StreamData.map(gen_field(kf, depth - 1, mod, unions), fn k ->
            if is_binary(k), do: k, else: Integer.to_string(k)
          end)

        val_gen = gen_field(vf, depth - 1, mod, unions)
        StreamData.map_of(key_gen, val_gen, max_length: 3)

      _ ->
        StreamData.constant(%{})
    end
  end

  defp gen_field(%{t: :list} = f, depth, mod, unions) do
    element = gen_element(f, depth, mod, unions)

    case Map.get(f, :fixed_array) do
      n when is_integer(n) ->
        StreamData.list_of(element, length: n)

      _ ->
        max = f |> Map.get(:max_count) |> then(&if(is_integer(&1), do: min(&1, 4), else: 4))
        StreamData.list_of(element, max_length: max)
    end
  end

  defp gen_field(%{t: :keyed} = f, depth, mod, unions) do
    item = gen_field(f.item, depth, mod, unions)

    f.key_names
    |> Map.new(fn key -> {key, item} end)
    |> StreamData.fixed_map()
  end

  defp gen_field(%{t: :void}, _depth, _mod, _unions), do: StreamData.constant(nil)

  defp gen_field(f, _depth, _mod, _unions), do: StreamData.constant(Map.get(f, :default))

  defp gen_union(union_name, depth, mod, unions) do
    u = unions |> Map.get(mod, %{}) |> Map.get(union_name)

    if depth <= 0 or u == nil or map_size(u.by_name) == 0 do
      StreamData.constant(nil)
    else
      arms =
        u.by_name
        |> Enum.sort_by(&elem(&1, 0))
        |> Enum.map(fn {arm_name, arm_desc} ->
          if arm_desc.t == :void do
            StreamData.constant({arm_name, nil})
          else
            StreamData.map(gen_field(arm_desc, depth - 1, mod, unions), fn v ->
              {arm_name, v}
            end)
          end
        end)

      StreamData.one_of([StreamData.constant(nil) | arms])
    end
  end

  defp gen_element(%{elem_kind: 15} = f, depth, mod, unions) do
    gen_union(Map.get(f, :elem), depth, mod, unions)
  end

  defp gen_element(%{elem_kind: 17} = f, depth, mod, unions) do
    name = Map.get(f, :elem)

    case depth > 0 && name && table_desc(mod, name) do
      %{} = desc ->
        StreamData.one_of([
          StreamData.constant(nil),
          gen_desc(desc, depth - 1, mod, unions)
        ])

      _ ->
        StreamData.constant(nil)
    end
  end

  defp gen_element(f, depth, mod, unions) do
    case Map.get(f, :elem_t) do
      :record ->
        desc = f |> Map.get(:elem) |> then(&table_desc(mod, &1))

        if desc, do: gen_desc(desc, depth, mod, unions), else: StreamData.constant(%{})

      :enum ->
        f.by_name |> Map.keys() |> Enum.sort() |> StreamData.member_of()

      :void ->
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
        max = min(Map.get(f, :size) || 8, 8)
        StreamData.string(:ascii, max_length: max)

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
      width when width in [1, 2, 4, 8, 16] ->
        bits = width * 8

        if Map.get(f, :signed, true) do
          {-(1 <<< (bits - 1)), (1 <<< (bits - 1)) - 1}
        else
          {0, (1 <<< bits) - 1}
        end

      _ ->
        {0, 0}
    end
  end

  defp bound({lo, _hi}, 0, _), do: lo
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
