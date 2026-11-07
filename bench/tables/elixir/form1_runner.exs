# schema tables bench — the Elixir leg, measuring THE VARIABLE FORM
# (docs/SPEC-TABLES.md §3, form byte 1) over the board's own corpus.
#
# The Elixir twin of bench/tables/cpp/table_main.cpp's `bench_table` arm, the
# reference implementation: ONE representative table (the root of
# bench/corpus/BenchTable.schema) saved and loaded on the tolerant wire over
# testdata/wire/bench_table.bin and its 64 rotating variants
# (bench/corpus/variants/bench_table.variants.bin; bench/tables/README.md).
#
# WHY THIS IS NOT runner.exs. That runner is the FIXED form's leg for
# bench/paired: it answers to the paired corpus's id and the paired generated
# unit. The variable form's corpus is a different corpus under a different id
# (§1.6) and a different generated unit (generated/bench/tables/elixir), as the
# C, C++, C# and Go legs keep them: two passes, two boards. runner.exs is
# untouched, so no paired row moves.
#
# THE GATES RUN BEFORE THE CLOCK and a failing gate emits NO rows (§1.5):
#   1. variant 0 IS the pinned instance, byte for byte;
#   2. every one of the 64 variants loads clean (nothing unknown, mismatched,
#      clamped, shared or malformed) and re-saves to the SAME bytes;
#   3. two complete rotations load and re-save byte-identically, including the
#      last-to-first transition. Elixir terms are immutable, so there is no
#      caller-owned target to reuse and no reset to omit; the rotation proves
#      the answer does not depend on what was loaded before.
#
# WHAT THE ROWS MEASURE. An op is one record. WRITE saves the 64 pre-loaded
# values round-robin; ROUND-TRIP loads a variant's bytes and saves what came
# out. Both carry the immutable-term value model: Load builds the whole map
# graph and Save a fresh binary, which is the shipped API. READ is derived
# (round-trip minus write), printed to stderr, never a row (§2.9).
#
# Constants are the fixed leg's: family `table`, linkage `beam`, checks
# `contract`, opt `default`, inline `unknown`, one codec so no `codec` column.
#
# THIS FILE IS SHAPE-BLIND: it names the generated root once, as the string the
# generated Save/Load take, and nothing else: no field, no pinned value, no
# wire size (`make shape-gate`; bench/tables/elixir/SHAPE-GATE.allow).
defmodule SchemaTablesBenchElixirForm1 do
  import Bitwise

  @num_records 64
  @max_iterations 2_147_483_584
  # The BEAM's variable-form Load and Save build the whole map graph and a
  # fresh binary per call: about 6 thousand records a second written and 2
  # thousand round-tripped on a fleet bench. The standard 400000 would take
  # over an hour a sitting, so 12800 keeps seven measured runs near a minute.
  # The count is a column of every row.
  @iters 12_800
  # --quick only, the iteration instrument and never the certification one
  @quick_iters 3_200
  @name "bench_table"
  @csv_suffix "table,beam,contract,default,unknown"
  @mask64 0xFFFFFFFFFFFFFFFF

  defp gate_fail(what) do
    IO.write(:stderr, "GOLDEN GATE FAILED: #{@name} #{what}\nreporting nothing.\n")
    System.halt(1)
  end

  defp fmt(value, decimals), do: :erlang.float_to_binary(value * 1.0, decimals: decimals)
  defp pad(value, width), do: String.pad_leading(value, width)

  # corpus_id (§1.6): FNV-1a-64 over the files this run loaded, in sorted
  # basename order: basename bytes, a 0x00 byte, the contents; 16 hex digits.
  defp fnv1a64(h, bytes) do
    for <<b <- bytes>>, reduce: h do
      acc -> bxor(acc, b) * 0x100000001B3 &&& @mask64
    end
  end

  defp corpus_id(goldens) do
    goldens
    |> Enum.sort_by(fn {name, _} -> name end)
    |> Enum.reduce(0xCBF29CE484222325, fn {name, bytes}, h ->
      h |> fnv1a64(name) |> fnv1a64(<<0>>) |> fnv1a64(bytes)
    end)
    |> Integer.to_string(16)
    |> String.downcase()
    |> String.pad_leading(16, "0")
  end

  defp corpus_file(dir, name) do
    case File.read(Path.join(dir, name)) do
      {:ok, bytes} ->
        {name, bytes}

      {:error, _} ->
        IO.write(
          :stderr,
          "missing corpus #{dir}/#{name} — run from the schema repo root, " <>
            "or pass --wire-dir/--variant-dir\n"
        )

        System.halt(1)
    end
  end

  # the generated Save and Load, named once each
  defp save!(value) do
    {:ok, bytes} = Benchtable.Table.save("TableMixed", value)
    bytes
  end

  defp load(bytes), do: Benchtable.Table.load("TableMixed", bytes)

  defp clean?(report) do
    report.unknown == 0 and report.kind_mismatch == 0 and report.clamped == 0 and
      report.malformed == false and Map.get(report, :blob_shared, 0) == 0
  end

  defp check_loads(variants) do
    for {bytes, k} <- Enum.with_index(variants) do
      case load(bytes) do
        {:ok, value, report} ->
          if not clean?(report), do: gate_fail("load of variant #{k} was not clean")

          case Benchtable.Table.save("TableMixed", value) do
            {:ok, ^bytes} ->
              value

            _ ->
              gate_fail(
                "variant #{k} round-trip bytes differ — refusing to bench a codec " <>
                  "that does not reproduce the corpus"
              )
          end

        _ ->
          gate_fail("load of variant #{k} failed")
      end
    end
  end

  defp check_rotation(variants) do
    t = List.to_tuple(variants)

    for k <- 0..(2 * @num_records - 1) do
      bytes = elem(t, k &&& @num_records - 1)

      case load(bytes) do
        {:ok, value, report} ->
          if not clean?(report) or save!(value) != bytes,
            do: gate_fail("rotation round-trip bytes differ")

        _ ->
          gate_fail("rotation load failed")
      end
    end
  end

  # per (bench, path): 1 discarded warmup run then num_runs measured runs
  defp timed_runs(num_runs, run_fn) do
    Enum.reduce(-1..(num_runs - 1)//1, [], fn run, rates ->
      t0 = System.monotonic_time(:nanosecond)
      iters = run_fn.()
      elapsed = (System.monotonic_time(:nanosecond) - t0) * 1.0e-9

      if run >= 0, do: [iters / elapsed | rates], else: rates
    end)
    |> Enum.reverse()
  end

  defp stats(rates) do
    sorted = Enum.sort(rates)
    n = length(sorted)
    median = Enum.at(sorted, div(n, 2))
    min = hd(sorted)
    max = List.last(sorted)
    {median, min, max, (max - min) / median * 100.0}
  end

  # WRITE: save the 64 pre-loaded values round-robin (§2.7 variation). The
  # accumulated size is the sink.
  defp write_loop(n, _i, _values, acc) when n <= 0, do: acc

  defp write_loop(n, i, values, acc) do
    bytes = save!(elem(values, i &&& @num_records - 1))
    write_loop(n - 1, i + 1, values, acc + byte_size(bytes))
  end

  # ROUND-TRIP: load a variant's bytes, then save what came out. The load needs
  # no sink of its own: its output IS the save's input.
  defp roundtrip_loop(n, _i, _variants, acc) when n <= 0, do: acc

  defp roundtrip_loop(n, i, variants, acc) do
    {:ok, value, _report} = load(elem(variants, i &&& @num_records - 1))
    roundtrip_loop(n - 1, i + 1, variants, acc + byte_size(save!(value)))
  end

  defp report(path, iters, bytes_per_op, rates) do
    {median, min, max, spread} = stats(rates)
    mbps = median * bytes_per_op / (1024.0 * 1024.0)

    IO.write(
      :stderr,
      "#{String.pad_trailing(@name, 18)} #{String.pad_trailing(path, 11)} " <>
        "#{pad(fmt(median / 1.0e6, 3), 10)} M msg/s #{pad(fmt(mbps, 1), 10)} MB/s   " <>
        "(min #{fmt(min / 1.0e6, 3)}, max #{fmt(max / 1.0e6, 3)}, spread #{fmt(spread, 1)}%)\n"
    )

    "elixir,#{@name},#{path},#{iters},#{bytes_per_op},#{length(rates)}," <>
      "#{fmt(median, 0)},#{fmt(min, 0)},#{fmt(max, 0)},#{fmt(mbps, 2)},#{fmt(spread, 2)}"
  end

  defp parse_args(argv) do
    parse_args(argv, %{
      csv: false,
      gate: false,
      quick: false,
      num_runs: 7,
      iterations: 0,
      wire_dir: "testdata/wire",
      variant_dir: "bench/corpus/variants"
    })
  end

  defp parse_args([], opts), do: opts
  defp parse_args(["--csv" | rest], opts), do: parse_args(rest, %{opts | csv: true})
  defp parse_args(["--gate" | rest], opts), do: parse_args(rest, %{opts | gate: true})
  defp parse_args(["--quick" | rest], opts), do: parse_args(rest, %{opts | quick: true})

  defp parse_args(["--round", k | rest], opts) do
    case Integer.parse(k) do
      {v, ""} when v >= 0 ->
        parse_args(rest, %{opts | num_runs: 1})

      _ ->
        IO.write(:stderr, "--round requires a non-negative integer\n")
        System.halt(1)
    end
  end

  defp parse_args(["--wire-dir", dir | rest], opts), do: parse_args(rest, %{opts | wire_dir: dir})

  defp parse_args(["--variant-dir", dir | rest], opts),
    do: parse_args(rest, %{opts | variant_dir: dir})

  defp parse_args(["--iterations", n | rest], opts) do
    case Integer.parse(n) do
      {v, ""} when v > 0 and v <= @max_iterations and rem(v, @num_records) == 0 ->
        parse_args(rest, %{opts | iterations: v})

      _ ->
        IO.write(
          :stderr,
          "--iterations requires a positive multiple of #{@num_records} up to #{@max_iterations}\n"
        )

        System.halt(1)
    end
  end

  defp parse_args([arg | _], _opts) do
    IO.write(
      :stderr,
      "usage: form1_main.exs [--gate] [--csv] [--round K] [--quick] [--iterations N] " <>
        "[--wire-dir <dir>] [--variant-dir <dir>] (got #{arg})\n"
    )

    System.halt(1)
  end

  def main(argv) do
    opts = parse_args(argv)

    IO.write(
      :stderr,
      "schema tables bench (elixir, the VARIABLE FORM" <>
        if(opts.quick, do: ", --quick: iteration instrument, not certification", else: "") <>
        ")\n"
    )

    {_, packed} = packed_file = corpus_file(opts.variant_dir, "#{@name}.variants.bin")
    {_, pinned} = pinned_file = corpus_file(opts.wire_dir, "#{@name}.bin")
    id = corpus_id([packed_file, pinned_file])

    if byte_size(packed) == 0 or rem(byte_size(packed), @num_records) != 0,
      do: gate_fail("the variant corpus is not 64 equal records")

    record = div(byte_size(packed), @num_records)
    variants = for k <- 0..(@num_records - 1), do: binary_part(packed, k * record, record)

    # gate 1 (§1.5): variant 0 IS the pinned instance.
    if hd(variants) != pinned,
      do: gate_fail("WIRE GOLDEN MISMATCH: variant 0 is not the pinned instance")

    # gates 2 and 3
    values = variants |> check_loads() |> List.to_tuple()
    check_rotation(variants)

    IO.write(
      :stderr,
      "#{@name}: gates passed before the clock — golden, load, round-trip, rotation (4 of 4)\n"
    )

    if opts.gate do
      IO.write(:stderr, "OK (gate only, corpus_id #{id})\n")
      System.halt(0)
    end

    iters =
      cond do
        opts.iterations > 0 -> opts.iterations
        opts.quick -> @quick_iters
        true -> @iters
      end

    variants_t = List.to_tuple(variants)

    write_rates =
      timed_runs(opts.num_runs, fn -> sink(write_loop(iters, 0, values, 0), iters) end)

    roundtrip_rates =
      timed_runs(opts.num_runs, fn -> sink(roundtrip_loop(iters, 0, variants_t, 0), iters) end)

    rows = [
      report("write", iters, record * 1.0, write_rates),
      report("round_trip", iters, record * 1.0, roundtrip_rates)
    ]

    {w_median, _, _, _} = stats(write_rates)
    {rt_median, _, _, _} = stats(roundtrip_rates)
    read_time = 1.0 / rt_median - 1.0 / w_median

    if read_time > 0 do
      IO.write(
        :stderr,
        "#{String.pad_trailing(@name, 18)} #{String.pad_trailing("read", 11)} " <>
          "#{pad(fmt(1.0e-6 / read_time, 3), 10)} M msg/s   " <>
          "(DERIVED: round-trip minus write, informational — not a measured row)\n"
      )
    end

    if opts.csv do
      IO.write(
        "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec," <>
          "max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n"
      )

      for row <- rows, do: IO.write("#{row},#{id},#{@csv_suffix}\n")
    end

    IO.write(:stderr, "OK (corpus_id #{id})\n")

    if System.get_env("SERIALIZE_BENCH_SINK") do
      IO.write(:stderr, "sink: #{Process.get(:bench_sink, 0)}\n")
    end
  end

  defp sink(bytes, iters) do
    Process.put(:bench_sink, Process.get(:bench_sink, 0) + bytes)
    iters
  end
end

SchemaTablesBenchElixirForm1.main(System.argv())
