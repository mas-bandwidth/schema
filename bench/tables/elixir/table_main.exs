# the tables bench — the Elixir runner.
#
# Measures ONE thing: a representative fixed table written and read on the
# TOLERANT WIRE (docs/SPEC-TABLES.md §3), through the generated table codec.
# That is the number a reader who knows protobuf or flatbuffers already has a
# comparison for, and it is the per-language release gate for the tables layer
# (bench/tables/README.md).
#
# It is a port of bench/tables/cpp/table_main.cpp — the reference — and follows
# the same contract (BENCH-STANDARD.md): the committed variant corpus drives it,
# the golden gate runs before the clock, and the report is 1 warmup + 7 measured
# runs with the median beside min/max/spread.
#
# THIS FILE IS SHAPE-BLIND. It names the generated codec's module and its one
# verb suffix at a single call site and nothing else: no field, no pinned value,
# no wire size.
#
# WHAT ELIXIR SPELLS DIFFERENTLY, and the reason at each site.
#
#   * THERE IS NO CALLER-OWNED BUFFER. `save` returns a binary, because a BEAM
#     term is immutable and there is nothing for a caller to write into. The
#     write arm therefore measures the codec AND the allocation of its result,
#     which is what a consumer actually pays; there is no configuration of this
#     language in which it pays less.
#   * THERE IS NO SEPARATE RESET. `load` starts from the declared defaults by
#     construction (there is no reused instance to clear), so the reset the
#     other legs keep inside the clock is inside this one's `load` — same work,
#     one call.
#   * THE SINK IS A PROCESS-DICTIONARY FOLD. The BEAM's compiler cannot remove a
#     call whose result reaches `:erlang.put/2`, and the dictionary write is a
#     constant the arms share, so it is the barrier every arm carries equally.
#   * OPT IS `default`: the BEAM has one optimization configuration and no flag
#     to name.

defmodule TablesBench do
  import Bitwise

  @max_num_runs 7
  @num_variants 64

  # the CSV's own columns (BENCH-STANDARD.md §5.1). family `table` (§1.9): the
  # tolerant table wire, a DIFFERENT wire over a different corpus, so a tools
  # refusal to divide it against a `gen` row is correct and automatic. linkage
  # mod — the generated table codec is ordinary module code in this release and
  # names no runtime at all. checks contract — the BEAM bounds-checks every
  # binary match in every configuration and the reader's wire-contract
  # validation is unconditional, which is §3.4's word for exactly this.
  @csv_suffix "mod,contract,default,unknown"

  def main(argv) do
    opts =
      parse(argv, %{
        csv: false,
        gate: false,
        runs: @max_num_runs,
        wire: "testdata/wire",
        variants: "bench/corpus/variants"
      })
    IO.puts(:stderr, "schema tables bench (elixir)")

    if opts.csv do
      IO.puts(
        "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec," <>
          "max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline"
      )
    end

    # The one measured shape, named once — the generated module and its verb
    # suffix at the call site, and nothing else about it
    # (bench/SHAPE-GATE.allow).
    state = bench_table(opts, "bench_table", 400_000, Benchtable.BenchTableTable, "table_mixed")

    if state.failed do
      IO.puts(:stderr, "TABLES BENCH FAILED (corpus_id #{corpus_id(state)})")
      System.halt(1)
    end

    flush_csv(opts, state)
    IO.puts(:stderr, "OK (corpus_id #{corpus_id(state)})")
  end

  defp parse([], opts), do: opts
  defp parse(["--csv" | rest], opts), do: parse(rest, %{opts | csv: true})
  # --gate runs the GOLDEN GATE and stops, without starting a clock. It is this
  # leg's own verb, for the release gate the certification job runs: the gate is
  # a correctness check — variant 0 against the pinned instance, and all 64
  # loading, re-saving at the same length and coming back byte-identical — and a
  # certification job does not need eight timed runs to learn that.
  defp parse(["--gate" | rest], opts), do: parse(rest, %{opts | gate: true})
  defp parse(["--round", _k | rest], opts), do: parse(rest, %{opts | runs: 1})
  defp parse(["--wire-dir", d | rest], opts), do: parse(rest, %{opts | wire: d})
  defp parse(["--variant-dir", d | rest], opts), do: parse(rest, %{opts | variants: d})

  defp parse([bad | _], _opts) do
    IO.puts(:stderr, "usage: table_main.exs [--csv] [--round K] [--wire-dir <dir>] [--variant-dir <dir>] (got #{bad})")
    System.halt(1)
  end

  # ---- the golden gate, which runs BEFORE the clock (§1.5) ----

  defp bench_table(opts, name, iters, mod, verb) do
    load = String.to_atom("load_" <> verb)
    save = String.to_atom("save_" <> verb <> "!")
    state = %{failed: false, rows: [], goldens: %{}}

    packed = File.read!(Path.join(opts.variants, "#{name}.variants.bin"))

    if byte_size(packed) == 0 or rem(byte_size(packed), @num_variants) != 0 do
      abort(state, "variant data is #{byte_size(packed)} bytes, not a multiple of #{@num_variants} records")
    else
      record = div(byte_size(packed), @num_variants)
      variants = for k <- 0..(@num_variants - 1), do: binary_part(packed, k * record, record)
      state = %{state | goldens: Map.put(state.goldens, "#{name}.variants.bin", packed)}

      golden = File.read!(Path.join(opts.wire, "#{name}.bin"))
      state = %{state | goldens: Map.put(state.goldens, "#{name}.bin", golden)}

      cond do
        # gate 1 (§1.5): variant 0 IS the pinned instance
        hd(variants) != golden ->
          abort(state, "WIRE GOLDEN MISMATCH: variant 0 is not #{name}.bin — refusing to bench code that does not match the corpus")

        true ->
          # gate 2: every variant loads, re-saves at the same length, and comes
          # back byte-identical — before any clock starts
          instances = Enum.map(variants, fn v -> elem(apply(mod, load, [v]), 0) end)

          bad =
            Enum.zip(instances, variants)
            |> Enum.find(fn {inst, v} -> apply(mod, save, [inst]) != v end)

          cond do
            bad != nil ->
              abort(state, "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus")

            opts.gate ->
              IO.puts(:stderr, "#{name}: the golden gate passes — variant 0 is the pinned instance and " <>
                                 "all #{@num_variants} round-trip at #{record} bytes")
              state

            true ->
              measure(opts, state, name, iters, record, mod, load, save, instances, variants)
          end
      end
    end
  end

  defp abort(state, why) do
    IO.puts(:stderr, "FAILED: #{why}")
    %{state | failed: true}
  end

  # ---- the clock ----

  defp measure(opts, state, name, iters, record, mod, load, save, instances, variants) do
    # tuples, so the round-robin pick is O(1) and the pick is not part of what
    # is being measured
    inst = List.to_tuple(instances)
    vars = List.to_tuple(variants)

    # WRITE: save the 64 pre-loaded instances round-robin. Rotating them is the
    # §2.7 variation: the encoder never sees the same input twice in a row, and
    # bytes/op is constant by construction rather than by assertion.
    write = runs(opts.runs, iters, fn i -> apply(mod, save, [elem(inst, i &&& 63)]) end)

    # ROUND-TRIP: load a variant buffer, then re-save what came out. The load
    # needs no sink discipline of its own — its output IS the save's input, so
    # every loaded field is observed by construction.
    round_trip =
      runs(opts.runs, iters, fn i ->
        {value, _report} = apply(mod, load, [elem(vars, i &&& 63)])
        apply(mod, save, [value])
      end)

    state = report(opts, state, name, "write", iters, record, stats(write))
    state = report(opts, state, name, "round_trip", iters, record, stats(round_trip))

    # READ is DERIVED, never measured: round-trip time minus write time. It
    # prints for continuity and is NOT a CSV row — a derived number in the CSV
    # would be divided as if it had been measured (§2.9).
    w = stats(write)
    rt = stats(round_trip)
    read_time = 1.0 / rt.median - 1.0 / w.median

    if read_time > 0 do
      IO.puts(:stderr, pad(name) <> pad11("read") <> :erlang.float_to_binary(1.0e-6 / read_time, decimals: 3) <>
        " M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)")
    end

    state
  end

  # 1 discarded warmup run, then `runs` measured ones
  defp runs(count, iters, work) do
    spin(work, iters)

    Enum.map(1..count, fn _ ->
      t0 = System.monotonic_time(:microsecond)
      spin(work, iters)
      iters / ((System.monotonic_time(:microsecond) - t0) / 1.0e6)
    end)
  end

  # THE SINK: the BEAM's compiler cannot remove a call whose result reaches
  # :erlang.put/2, so folding the produced binary's size into the process
  # dictionary is the barrier. It is a constant every arm carries equally.
  defp spin(work, 0), do: :erlang.put(:sink, work)

  defp spin(work, n) do
    :erlang.put(:sink, byte_size(work.(n)))
    spin(work, n - 1)
  end

  defp stats(rates) do
    sorted = Enum.sort(rates)
    median = Enum.at(sorted, div(length(sorted), 2))
    min = hd(sorted)
    max = List.last(sorted)
    %{median: median, min: min, max: max, spread: (max - min) / median * 100.0}
  end

  defp report(opts, state, bench, path, iters, bytes_per_op, s) do
    mbps = s.median * bytes_per_op / (1024.0 * 1024.0)

    IO.puts(
      :stderr,
      pad(bench) <> pad11(path) <> fmt(s.median / 1.0e6, 3) <> " M msg/s " <> fmt(mbps, 1) <>
        " MB/s   (min " <> fmt(s.min / 1.0e6, 3) <> ", max " <> fmt(s.max / 1.0e6, 3) <>
        ", spread " <> fmt(s.spread, 1) <> "%)"
    )

    if opts.csv do
      row =
        "elixir,#{bench},#{path},#{iters},#{bytes_per_op},#{opts.runs}," <>
          "#{fmt(s.median, 0)},#{fmt(s.min, 0)},#{fmt(s.max, 0)},#{fmt(mbps, 2)},#{fmt(s.spread, 2)}"

      %{state | rows: state.rows ++ [row]}
    else
      state
    end
  end

  defp flush_csv(%{csv: false}, _state), do: :ok

  defp flush_csv(_opts, state) do
    id = corpus_id(state)
    Enum.each(state.rows, fn row -> IO.puts("#{row},#{id},table,#{@csv_suffix}") end)
  end

  # a run's corpus_id is FNV-1a-64 over the goldens THAT RUN LOADED (§1.6), in
  # sorted basename order — the C++ leg's std::map order
  defp corpus_id(state) do
    h =
      state.goldens
      |> Enum.sort_by(fn {name, _} -> name end)
      |> Enum.reduce(0xCBF29CE484222325, fn {name, data}, h ->
        h |> fnv1a64(name) |> fnv1a64(<<0>>) |> fnv1a64(data)
      end)

    String.pad_leading(String.downcase(Integer.to_string(h, 16)), 16, "0")
  end

  defp fnv1a64(h, data) do
    :binary.bin_to_list(data)
    |> Enum.reduce(h, fn b, acc -> band(bxor(acc, b) * 0x100000001B3, 0xFFFFFFFFFFFFFFFF) end)
  end

  defp fmt(v, places), do: :erlang.float_to_binary(v * 1.0, decimals: places)
  defp pad(s), do: String.pad_trailing(s, 19)
  defp pad11(s), do: String.pad_trailing(s, 12)
end

TablesBench.main(System.argv())
