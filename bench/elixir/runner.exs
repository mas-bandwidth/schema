# schema bench — the Elixir runner: a conforming run.sh leg per
# bench/README.md's runner contract, measuring the generated monomorphic
# Elixir codecs over bench/corpus/Bench.schema's BenchMixed.
#
# CONTRACT (BENCH-STANDARD.md): fixed iteration counts identical to every
# other runner's rows for these benches (--quick's reduced count is the one
# deliberate exception, recorded in the iters column); 1 discarded warmup
# run then 7 measured runs per (bench, path) — or exactly one measured run
# under --round K, where the interleaved driver aggregates across rounds
# (§2.4); 64 rotating variant buffers on both paths;
# median/min/max/spread over the measured runs;
# CSV v2 rows on stdout under --csv, human table on stderr.
#
# WHAT THE ROWS MEASURE: the generated codec — schema's Elixir backend
# emits self-contained accumulator-threaded write/read functions with zero
# runtime dependencies, so the generated code IS the Elixir serialize path.
# The rows carry family=gen — the estate's one benchmark subject
# (schema#196). Peak-style numbers
# from this runner's earlier serialize-family form (tight loops,
# best-of-five) are NOT comparable to these rows — different measurement
# contract, and the statistic alone moves the number.
#
# GOLDEN GATED (§1.5): before any timing, variant 0 of the committed
# variant corpus is byte-compared against the C++-pinned testdata/wire
# golden, and every variant is decoded and re-encoded byte-identically.
# A runner that mismatches REFUSES to bench.
# corpus_id is FNV-1a-64 over the goldens this run actually loaded (§1.6).
#
# Invocation is main.exs, from bench/elixir, under the repo's pinned
# BEAM/Elixir toolchain (the Makefile's $(ELIXIR) PATH form):
#
#   cd bench/elixir && PATH="<dist otp>:<dist elixir>:$PATH" \
#       elixir main.exs [--csv] [--round K] [--quick]
#
# --quick: 3 measured runs at a reduced iteration count
# (the BEAM is ~2 orders behind the native legs; the full count would blow
# quick mode's one-minute-per-language bound) — run.sh's iteration
# instrument, never the certification instrument.
defmodule SchemaBenchElixir do
  import Bitwise

  @num_variants 64

  # §1.2/§2.1: fixed per-benchmark iteration counts, identical across every
  # language's rows for these benches, recorded in the iters column
  @mixed_iters 4_000_000
  # --quick only (PROPOSED, BENCH-STANDARD quick-mode scaling)
  @quick_mixed_iters 400_000

  # CSV v2 (§5.1) per-runner constants: family gen (the rows measure
  # generated code), linkage beam (codec modules compiled beside the caller
  # into one VM, no library boundary), checks contract (no caller-error
  # checks in the writer, wire-contract validation unconditional in the
  # reader), opt default (no level), inline unknown (no §4 elixir branch).
  @csv_suffix "gen,beam,contract,default,unknown"

  # ------------------------------------------------------------------
  # harness
  # ------------------------------------------------------------------

  # The 64-bit mask the corpus_id fold works under. The C bench's LCG itself
  # is gone with the hand-written shape code — the data-driven leg's variation
  # is the committed corpus, not a generator — but timed_runs still threads a
  # seed value through the runs so its shape matches the other runners'.
  @mask64 0xFFFFFFFFFFFFFFFF

  defp lcg_seed, do: 1

  defp gate_fail(row, what) do
    IO.write(:stderr, "GOLDEN GATE FAILED: #{row} #{what}\nreporting nothing.\n")
    System.halt(1)
  end

  defp golden(name) do
    path = "../../testdata/wire/#{name}.bin"

    case File.read(path) do
      {:ok, bytes} ->
        bytes

      {:error, _} ->
        IO.write(:stderr, "missing wire golden #{path} — run from bench/elixir\n")
        System.halt(1)
    end
  end

  defp fmt(value, decimals), do: :erlang.float_to_binary(value * 1.0, decimals: decimals)
  defp pad(value, width), do: String.pad_leading(value, width)

  # corpus_id (§1.6): FNV-1a-64 over the goldens this run loaded — for each
  # file in sorted basename order, the basename bytes, a 0x00 byte, the
  # contents — rendered as 16 lowercase hex digits
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

  # per (bench, path): 1 discarded warmup run then num_runs measured runs;
  # the rng threads across runs (the write stream keeps varying, never
  # replaying one sequence the branch predictor could memorize)
  defp timed_runs(num_runs, run_fn) do
    {rates, _} =
      Enum.reduce(-1..(num_runs - 1), {[], lcg_seed()}, fn run, {rates, rng} ->
        t0 = System.monotonic_time(:nanosecond)
        {rng, iters} = run_fn.(rng)
        elapsed = (System.monotonic_time(:nanosecond) - t0) * 1.0e-9

        if run >= 0 do
          {[iters / elapsed | rates], rng}
        else
          {rates, rng}
        end
      end)

    Enum.reverse(rates)
  end

  defp stats(rates) do
    sorted = Enum.sort(rates)
    n = length(sorted)
    median = Enum.at(sorted, div(n, 2))
    min = hd(sorted)
    max = List.last(sorted)
    {median, min, max, (max - min) / median * 100.0}
  end

  defp report(bench, path, iters, bytes_per_op, rates) do
    {median, min, max, spread} = stats(rates)
    mbps = median * bytes_per_op / (1024.0 * 1024.0)

    IO.write(
      :stderr,
      "#{String.pad_trailing(bench, 18)} #{String.pad_trailing(path, 5)} " <>
        "#{pad(fmt(median / 1.0e6, 2), 10)} M msg/s #{pad(fmt(mbps, 1), 10)} MB/s   " <>
        "(min #{fmt(min / 1.0e6, 2)}, max #{fmt(max / 1.0e6, 2)}, spread #{fmt(spread, 1)}%)\n"
    )

    "elixir,#{bench},#{path},#{iters},#{bytes_per_op},#{length(rates)}," <>
      "#{fmt(median, 0)},#{fmt(min, 0)},#{fmt(max, 0)},#{fmt(mbps, 2)},#{fmt(spread, 2)}"
  end

  # ------------------------------------------------------------------
  # the DATA-DRIVEN driver for bench_mixed (issue #191)
  #
  # THE PROPERTY: nothing below names a field of the shape it measures. Shape
  # knowledge lives in the committed variant DATA (bench/corpus/variants,
  # emitted by bench/tools/variantgen) and in the generated codec, and nowhere
  # else — so this driver cannot drift from another language's driver in what
  # it measures, which is the whole reason the design exists. If a change here
  # ever needs a field name, the design has failed and that is the finding.
  #
  # It is the ONLY driver in this runner: bench_mixed is the whole leg, and
  # no hand-written pin, vary, field-check or sink function survives here.
  # ------------------------------------------------------------------

  defp variant_data(row) do
    path = "../../bench/corpus/variants/#{row}.variants.bin"

    case File.read(path) do
      {:ok, bytes} ->
        bytes

      {:error, _} ->
        IO.write(
          :stderr,
          "missing variant data #{path} — run `make bench-variants`, and run the bench from bench/elixir\n"
        )

        System.halt(1)
    end
  end

  defp gate_data_driven(row, golden_name, write, read) do
    packed = variant_data(row)

    # The records are fixed-width by construction (§2.7 pins every structure
    # field), so the file needs no index: the record size IS file size /
    # @num_variants, and a file that does not divide evenly is a refusal.
    if byte_size(packed) == 0 or rem(byte_size(packed), @num_variants) != 0 do
      gate_fail(
        row,
        "variant data is #{byte_size(packed)} bytes, not a multiple of #{@num_variants} " <>
          "records — refusing to bench data whose stride is not the record size"
      )
    end

    record = div(byte_size(packed), @num_variants)
    variants = for <<chunk::binary-size(^record) <- packed>>, do: chunk

    # gate 1 (§1.5): variant 0 IS the pinned instance, so the whole variant
    # file is bound to the wire golden by one byte-compare.
    golden_bytes = golden(golden_name)

    if hd(variants) != golden_bytes do
      gate_fail(row, "variant 0 vs testdata/wire/#{golden_name}.bin")
    end

    # gate 2: every variant decodes, re-encodes, and comes back byte-identical
    # at the same length. This is stronger than the pinned-instance-only gate
    # the retired hand-written shapes applied — §1.5's named residual (the 64
    # varied buffers
    # length-checked but never value-checked) closes here, for every variant.
    instances =
      variants
      |> Enum.with_index()
      |> Enum.map(fn {variant, k} ->
        case read.(variant, record * 8) do
          {:ok, decoded} ->
            if write.(decoded) != variant do
              gate_fail(
                row,
                "variant #{k} round-trip bytes differ — refusing to bench a codec " <>
                  "that does not reproduce the corpus"
              )
            end

            decoded

          :error ->
            gate_fail(row, "decode of variant #{k} failed")
        end
      end)

    {List.to_tuple(instances), List.to_tuple(variants), record,
     [{golden_name <> ".bin", golden_bytes}, {row <> ".variants.bin", packed}]}
  end

  # WRITE: encode the 64 pre-decoded instances round-robin. Rotating the
  # instances is what §2.7's per-iteration LCG mutation bought — the encoder
  # never sees the same input twice in a row — with none of the per-language
  # mutation code, and with bytes/op constant by construction.
  defp dd_write_loop(0, _i, _instances, _write, acc), do: acc

  defp dd_write_loop(n, i, instances, write, acc) do
    instance = elem(instances, i &&& @num_variants - 1)
    dd_write_loop(n - 1, i + 1, instances, write, acc + byte_size(write.(instance)))
  end

  # ROUND-TRIP: decode a variant, then re-encode what came out. The decode
  # needs no sink discipline of its own — its output IS the encode's input, so
  # every decoded field is observed by construction, with no per-language fold
  # to audit (§2.7's read-side sink problem dissolved rather than equalized).
  defp dd_roundtrip_loop(0, _i, _variants, _read, _write, _num_bits, acc), do: acc

  defp dd_roundtrip_loop(n, i, variants, read, write, num_bits, acc) do
    variant = elem(variants, i &&& @num_variants - 1)
    {:ok, decoded} = read.(variant, num_bits)

    dd_roundtrip_loop(
      n - 1,
      i + 1,
      variants,
      read,
      write,
      num_bits,
      acc + byte_size(write.(decoded))
    )
  end

  defp bench_data_driven(row, gated, iters, num_runs, write, read) do
    {instances, variants, bytes_per_packet, _goldens} = gated
    num_bits = bytes_per_packet * 8

    write_rates =
      timed_runs(num_runs, fn rng ->
        sink_w = dd_write_loop(iters, 0, instances, write, 0)
        Process.put(:bench_sink, Process.get(:bench_sink, 0) + sink_w)
        {rng, iters}
      end)

    roundtrip_rates =
      timed_runs(num_runs, fn rng ->
        sink_r = dd_roundtrip_loop(iters, 0, variants, read, write, num_bits, 0)
        Process.put(:bench_sink, Process.get(:bench_sink, 0) + sink_r)
        {rng, iters}
      end)

    rows = [
      report(row, "write", iters, bytes_per_packet, write_rates),
      report(row, "round_trip", iters, bytes_per_packet, roundtrip_rates)
    ]

    # READ is DERIVED, never measured: round-trip time minus write time. It
    # prints for continuity with the read rows the rest of the corpus still
    # reports and is NOT a CSV row — a derived number in the CSV would be
    # divided as if it had been measured.
    {w_median, _, _, _} = stats(write_rates)
    {rt_median, _, _, _} = stats(roundtrip_rates)
    read_time = 1.0 / rt_median - 1.0 / w_median

    if read_time > 0 do
      IO.write(
        :stderr,
        "#{String.pad_trailing(row, 18)} #{String.pad_trailing("read", 5)} " <>
          "#{pad(fmt(1.0e-6 / read_time, 2), 10)} M msg/s   " <>
          "(DERIVED: round-trip minus write, informational — not a measured row)\n"
      )
    end

    rows
  end

  defp parse_args(argv) do
    parse_args(argv, %{csv: false, quick: false, num_runs: 7})
  end

  defp parse_args([], opts), do: opts
  defp parse_args(["--csv" | rest], opts), do: parse_args(rest, %{opts | csv: true})
  defp parse_args(["--quick" | rest], opts), do: parse_args(rest, %{opts | quick: true})

  defp parse_args(["--round", _k | rest], opts),
    do: parse_args(rest, %{opts | num_runs: 1})

  defp parse_args([arg | _], _opts) do
    IO.write(:stderr, "usage: main.exs [--csv] [--round K] [--quick] (got #{arg})\n")
    System.halt(1)
  end

  def main(argv) do
    opts = parse_args(argv)
    quick = opts.quick
    num_runs = if quick and opts.num_runs == 7, do: 3, else: opts.num_runs

    IO.write(
      :stderr,
      "schema bench (elixir, generated codecs" <>
        if(quick, do: ", --quick: iteration instrument, not certification", else: "") <> ")\n"
    )

    # every measured row's golden gate runs before any row is timed: a
    # runner that fails its goldens reports nothing at all (§1.5)
    gated_mixed =
      gate_data_driven(
        "bench_mixed",
        "bench_mixed",
        &Bench.Bench.write_bench_mixed/1,
        &Bench.Bench.read_bench_mixed/2
      )

    {_, _, _, goldens_mixed} = gated_mixed

    rows =
      bench_data_driven(
        "bench_mixed",
        gated_mixed,
        if(quick, do: @quick_mixed_iters, else: @mixed_iters),
        num_runs,
        &Bench.Bench.write_bench_mixed/1,
        &Bench.Bench.read_bench_mixed/2
      )

    goldens = goldens_mixed

    id = corpus_id(goldens)

    if opts.csv do
      IO.write(
        "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec," <>
          "max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n"
      )

      for row <- rows do
        IO.write("#{row},#{id},#{@csv_suffix}\n")
      end
    end

    IO.write(:stderr, "OK (corpus_id #{id})\n")

    # the g_sink escape: the runtime cannot prove the env var absent, so the
    # accumulated sink is observable and no loop's work can be deleted
    if System.get_env("SERIALIZE_BENCH_SINK") do
      IO.write(:stderr, "sink: #{Process.get(:bench_sink, 0)}\n")
    end
  end
end

SchemaBenchElixir.main(System.argv())
