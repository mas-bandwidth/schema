# schema bench — the Elixir runner: a conforming run.sh leg per
# bench/README.md's runner contract, measuring the generated monomorphic
# Elixir codecs over the four bench/corpus/Bench.schema shapes.
#
# CONTRACT (BENCH-STANDARD.md): fixed iteration counts identical to every
# other runner's rows for these benches (--quick's reduced count is the one
# deliberate exception, recorded in the iters column); 1 discarded warmup
# run then 7 measured runs per (bench, path) — or exactly one measured run
# under --round K, where the interleaved driver aggregates across rounds
# (§2.4); per-iteration LCG variation on the write path; 64 rotating
# variant read buffers; median/min/max/spread over the measured runs;
# CSV v2 rows on stdout under --csv, human table on stderr.
#
# WHAT THE ROWS MEASURE: the generated codec — schema's Elixir backend
# emits self-contained accumulator-threaded write/read functions with zero
# runtime dependencies, so the generated code IS the Elixir serialize path.
# The rows carry family=gen: a ratio against another language's family=rt
# row (the serialize runtime API called by hand) is a subject difference,
# not a language difference, and the tools refuse it. Peak-style numbers
# from this runner's earlier serialize-family form (tight loops,
# best-of-five) are NOT comparable to these rows — different measurement
# contract, and the statistic alone moves the number.
#
# GOLDEN GATED (§1.5): before any timing, every measured shape's pinned
# instance is written and byte-compared against the C++-pinned
# testdata/wire golden, and all 64 LCG variant buffers are decoded back
# with every field verified. A runner that mismatches REFUSES to bench.
# corpus_id is FNV-1a-64 over the goldens this run actually loaded (§1.6).
#
# Invocation is main.exs, from bench/elixir, under the repo's pinned
# BEAM/Elixir toolchain (the Makefile's $(ELIXIR) PATH form):
#
#   cd bench/elixir && PATH="<dist otp>:<dist elixir>:$PATH" \
#       elixir main.exs [--csv] [--round K] [--quick]
#
# --quick: bench_mixed only, 3 measured runs at a reduced iteration count
# (the BEAM is ~2 orders behind the native legs; the full count would blow
# quick mode's one-minute-per-language bound) — run.sh's iteration
# instrument, never the certification instrument.
defmodule SchemaBenchElixir do
  import Bitwise

  @num_variants 64

  # §1.2/§2.1: fixed per-benchmark iteration counts, identical across every
  # language's rows for these benches, recorded in the iters column
  @packet_iters 32_000_000
  @ints_iters 40_000_000
  @bits_iters 48_000_000
  @mixed_iters 40_000_000
  # --quick only (PROPOSED, BENCH-STANDARD quick-mode scaling)
  @quick_mixed_iters 8_000_000

  # CSV v2 (§5.1) per-runner constants: family gen (the rows measure
  # generated code), linkage beam (codec modules compiled beside the caller
  # into one VM, no library boundary), checks contract (no caller-error
  # checks in the writer, wire-contract validation unconditional in the
  # reader), opt default (no level), inline unknown (no §4 elixir branch).
  @csv_suffix "gen,beam,contract,default,unknown"

  # ------------------------------------------------------------------
  # the C bench's uint64 LCG, direct in BEAM integers under a 64-bit mask
  # ------------------------------------------------------------------

  @mask64 0xFFFFFFFFFFFFFFFF
  @lcg_mul 6_364_136_223_846_793_005
  @lcg_add 1_442_695_040_888_963_407

  defp lcg_seed, do: 1
  defp lcg_step(rng), do: rng * @lcg_mul + @lcg_add &&& @mask64

  # the low 32 bits of (rng >> s)
  defp shr(rng, s), do: rng >>> s &&& 0xFFFFFFFF

  # ------------------------------------------------------------------
  # pinned instances and vary functions — the §1.3 shapes, field mappings
  # verbatim from the family benches (blob is the generated storage's list)
  # ------------------------------------------------------------------

  @blob_init Enum.map(0..16, fn i -> i * 31 &&& 0xFF end)
  @blob_tail tl(@blob_init)

  defp init_bench_packet do
    %Bench.BenchPacket{
      a: -37,
      b: 12_345,
      c: 987_654,
      bits7: 97,
      bits13: 5000,
      bits23: 1_234_567,
      flag: true,
      x: 1.5,
      y: -3.25,
      z: 100.125,
      big: 0x123456789ABCDEF0,
      blob: @blob_init
    }
  end

  defp vary_bench_packet(p, rng) do
    %{
      p
      | a: (shr(rng, 8) &&& 63) - 32,
        b: shr(rng, 16) &&& 65_535,
        c: (shr(rng, 24) &&& 0xFFFFF) - 500_000,
        bits7: rng &&& 127,
        bits13: shr(rng, 3) &&& 8191,
        bits23: shr(rng, 5) &&& 8_388_607,
        flag: (rng &&& 1) != 0,
        # exact in float32
        x: (rng &&& 0xFFFF) * 1.0,
        big: rng,
        blob: [rng >>> 32 &&& 0xFF | @blob_tail]
    }
  end

  defp init_bench_ints do
    %Bench.BenchInts{
      f0: -37,
      f1: 12_345,
      f2: 987_654,
      f3: 2,
      f4: -15,
      f5: 777,
      f6: -2048,
      f7: 200,
      f8: -543_210,
      f9: 99
    }
  end

  defp vary_bench_ints(p, rng) do
    %{
      p
      | f0: (shr(rng, 8) &&& 63) - 32,
        f1: shr(rng, 16) &&& 65_535,
        f2: (shr(rng, 24) &&& 0xFFFFF) - 500_000,
        f3: shr(rng, 2) &&& 3,
        f4: (shr(rng, 11) &&& 15) - 8,
        f5: shr(rng, 22) &&& 511,
        f6: (shr(rng, 33) &&& 2047) - 1024,
        f7: shr(rng, 40) &&& 255,
        f8: (shr(rng, 30) &&& 0xFFFFF) - 500_000,
        f9: shr(rng, 57) &&& 63
    }
  end

  defp init_bench_bits do
    %Bench.BenchBits{
      b7: 97,
      b13: 5000,
      b23: 1_234_567,
      b3: 5,
      b32: 0xDEADBEEF,
      b11: 1024,
      b19: 333_333,
      b48: 0xFEDCBA987654
    }
  end

  defp vary_bench_bits(p, rng) do
    %{
      p
      | b7: rng &&& 127,
        b13: shr(rng, 3) &&& 8191,
        b23: shr(rng, 5) &&& 8_388_607,
        b3: shr(rng, 29) &&& 7,
        b32: shr(rng, 16),
        b11: shr(rng, 37) &&& 2047,
        b19: shr(rng, 44) &&& 524_287,
        # the 48-bit field: low dword + 16-bit remainder, composed — the same
        # bits the family benches send as two lanes
        b48: (rng &&& 0xFFFFFFFF) ||| (shr(rng, 32) &&& 0xFFFF) <<< 32
    }
  end

  defp init_bench_mixed do
    %Bench.BenchMixed{
      sequence: 52_428,
      ack_bits: 0xA5A5A5A5,
      entity_id: 2049,
      pos_x: -16_384,
      pos_y: 16_383,
      pos_z: -1,
      yaw: 511,
      moving: true,
      firing: false,
      timestamp: 0x123456789ABC,
      weapon: 15
    }
  end

  defp vary_bench_mixed(p, rng) do
    %{
      p
      | sequence: shr(rng, 8) &&& 65_535,
        ack_bits: shr(rng, 16),
        entity_id: rng &&& 4095,
        pos_x: (shr(rng, 20) &&& 32_767) - 16_384,
        pos_y: (shr(rng, 25) &&& 32_767) - 16_384,
        pos_z: (shr(rng, 30) &&& 32_767) - 16_384,
        yaw: shr(rng, 3) &&& 511,
        moving: (rng &&& 1) != 0,
        firing: (rng &&& 2) != 0,
        timestamp: (rng &&& 0xFFFFFFFF) ||| (shr(rng, 32) &&& 0xFFFF) <<< 32,
        weapon: shr(rng, 60) &&& 15
    }
  end

  # ------------------------------------------------------------------
  # harness
  # ------------------------------------------------------------------

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

  # ------------------------------------------------------------------
  # the golden gate (§1.5), shared by every shape: the PINNED instance's
  # bytes must equal the C++-pinned testdata/wire golden, and all 64 variant
  # buffers must decode back field-perfect. A mismatch refuses to bench.
  # ------------------------------------------------------------------

  defp gate_shape(row, golden_name, init, vary, write, read) do
    packet = init.()
    wire = write.(packet)
    golden_bytes = golden(golden_name)

    if wire != golden_bytes do
      gate_fail(row, "pinned instance vs testdata/wire/#{golden_name}.bin")
    end

    # 64 LCG variants, written with the same sequence the write loop uses,
    # each decoded back and field-verified
    {variants, _rng, _p} =
      Enum.reduce(1..@num_variants, {[], lcg_seed(), init.()}, fn _, {variants, rng, p} ->
        rng = lcg_step(rng)
        p = vary.(p, rng)
        {[write.(p) | variants], rng, p}
      end)

    variants = Enum.reverse(variants)
    bytes_per_packet = byte_size(hd(variants))

    if Enum.any?(variants, fn v -> byte_size(v) != bytes_per_packet end) do
      gate_fail(row, "variant sizes diverge")
    end

    {_rng, _p} =
      variants
      |> Enum.with_index()
      |> Enum.reduce({lcg_seed(), init.()}, fn {variant, k}, {rng, p} ->
        rng = lcg_step(rng)
        p = vary.(p, rng)

        case read.(variant, bytes_per_packet * 8) do
          {:ok, decoded} ->
            if decoded != p do
              gate_fail(row, "variant #{k} field mismatch")
            end

            {rng, p}

          :error ->
            gate_fail(row, "variant #{k} read verdict")
        end
      end)

    {List.to_tuple(variants), bytes_per_packet, {golden_name <> ".bin", golden_bytes}}
  end

  # ------------------------------------------------------------------
  # the timed loops: write varies every packet through the LCG, read
  # rotates the 64 gated variant buffers; every loop's work flows into the
  # sink (published at exit under an env var the runtime cannot rule out)
  # ------------------------------------------------------------------

  defp write_loop(0, rng, _p, _vary, _write, acc), do: {acc, rng}

  defp write_loop(n, rng, p, vary, write, acc) do
    rng = lcg_step(rng)
    p = vary.(p, rng)
    wire = write.(p)
    write_loop(n - 1, rng, p, vary, write, acc + byte_size(wire))
  end

  defp read_loop(0, _i, _variants, _read, _num_bits, _sink_of, acc), do: acc

  defp read_loop(n, i, variants, read, num_bits, sink_of, acc) do
    variant = elem(variants, i &&& @num_variants - 1)
    {:ok, decoded} = read.(variant, num_bits)
    read_loop(n - 1, i + 1, variants, read, num_bits, sink_of, acc + sink_of.(decoded))
  end

  # read-side sink discipline (#175, equalized to the cpp/c reference): every
  # read loop observes the FULL decoded struct per iteration. The C/C++ legs
  # get this for free from an empty-asm memory clobber over the whole struct;
  # the BEAM has no zero-cost clobber, so the leg's idiom is a per-iteration
  # sum of every decoded field — floats truncated, booleans as 0/1, the blob
  # list element-by-element. The sink adds are real work the clobber
  # languages do not pay; the published number is an upper bound on the
  # observation cost.

  defp bool_bit(true), do: 1
  defp bool_bit(false), do: 0

  defp sink_of_bench_packet(%Bench.BenchPacket{
         a: a,
         b: b,
         c: c,
         bits7: b7,
         bits13: b13,
         bits23: b23,
         flag: flag,
         x: x,
         y: y,
         z: z,
         big: big,
         blob: blob
       }) do
    a + b + c + b7 + b13 + b23 + bool_bit(flag) +
      trunc(x) + trunc(y) + trunc(z) + big + Enum.sum(blob)
  end

  defp sink_of_bench_ints(%Bench.BenchInts{
         f0: f0,
         f1: f1,
         f2: f2,
         f3: f3,
         f4: f4,
         f5: f5,
         f6: f6,
         f7: f7,
         f8: f8,
         f9: f9
       }) do
    f0 + f1 + f2 + f3 + f4 + f5 + f6 + f7 + f8 + f9
  end

  defp sink_of_bench_bits(%Bench.BenchBits{
         b7: b7,
         b13: b13,
         b23: b23,
         b3: b3,
         b32: b32,
         b11: b11,
         b19: b19,
         b48: b48
       }) do
    b7 + b13 + b23 + b3 + b32 + b11 + b19 + b48
  end

  defp sink_of_bench_mixed(%Bench.BenchMixed{
         sequence: sequence,
         ack_bits: ack_bits,
         entity_id: entity_id,
         pos_x: pos_x,
         pos_y: pos_y,
         pos_z: pos_z,
         yaw: yaw,
         moving: moving,
         firing: firing,
         timestamp: timestamp,
         weapon: weapon
       }) do
    sequence + ack_bits + entity_id + pos_x + pos_y + pos_z +
      yaw + bool_bit(moving) + bool_bit(firing) + timestamp + weapon
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

  defp bench_shape(row, gated, iters, num_runs, opts) do
    {variants, bytes_per_packet, _golden} = gated
    init = opts[:init]
    vary = opts[:vary]
    write = opts[:write]
    read = opts[:read]
    sink_of = opts[:sink_of]
    num_bits = bytes_per_packet * 8

    write_rates =
      timed_runs(num_runs, fn rng ->
        {sink_w, rng} = write_loop(iters, rng, init.(), vary, write, 0)
        Process.put(:bench_sink, Process.get(:bench_sink, 0) + sink_w)
        {rng, iters}
      end)

    read_rates =
      timed_runs(num_runs, fn rng ->
        sink_r = read_loop(iters, 0, variants, read, num_bits, sink_of, 0)
        Process.put(:bench_sink, Process.get(:bench_sink, 0) + sink_r)
        {rng, iters}
      end)

    [
      report(row, "write", iters, bytes_per_packet, write_rates),
      report(row, "read", iters, bytes_per_packet, read_rates)
    ]
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
      gate_shape(
        "bench_mixed",
        "bench_mixed",
        &init_bench_mixed/0,
        &vary_bench_mixed/2,
        &Bench.Bench.write_bench_mixed/1,
        &Bench.Bench.read_bench_mixed/2
      )

    mixed_opts = [
      init: &init_bench_mixed/0,
      vary: &vary_bench_mixed/2,
      write: &Bench.Bench.write_bench_mixed/1,
      read: &Bench.Bench.read_bench_mixed/2,
      # full-struct observation (#175)
      sink_of: &sink_of_bench_mixed/1
    ]

    {rows, goldens} =
      if quick do
        {_, _, golden_mixed} = gated_mixed

        {bench_shape("bench_mixed", gated_mixed, @quick_mixed_iters, num_runs, mixed_opts),
         [golden_mixed]}
      else
        gated_packet =
          gate_shape(
            "bench_packet",
            "bench_packet",
            &init_bench_packet/0,
            &vary_bench_packet/2,
            &Bench.Bench.write_bench_packet/1,
            &Bench.Bench.read_bench_packet/2
          )

        gated_ints =
          gate_shape(
            "bench_ints",
            "bench_ints",
            &init_bench_ints/0,
            &vary_bench_ints/2,
            &Bench.Bench.write_bench_ints/1,
            &Bench.Bench.read_bench_ints/2
          )

        gated_bits =
          gate_shape(
            "bench_bits",
            "bench_bits",
            &init_bench_bits/0,
            &vary_bench_bits/2,
            &Bench.Bench.write_bench_bits/1,
            &Bench.Bench.read_bench_bits/2
          )

        {_, _, g_packet} = gated_packet
        {_, _, g_ints} = gated_ints
        {_, _, g_bits} = gated_bits
        {_, _, g_mixed} = gated_mixed

        rows =
          bench_shape("bench_packet", gated_packet, @packet_iters, num_runs,
            init: &init_bench_packet/0,
            vary: &vary_bench_packet/2,
            write: &Bench.Bench.write_bench_packet/1,
            read: &Bench.Bench.read_bench_packet/2,
            sink_of: &sink_of_bench_packet/1
          ) ++
            bench_shape("bench_ints", gated_ints, @ints_iters, num_runs,
              init: &init_bench_ints/0,
              vary: &vary_bench_ints/2,
              write: &Bench.Bench.write_bench_ints/1,
              read: &Bench.Bench.read_bench_ints/2,
              sink_of: &sink_of_bench_ints/1
            ) ++
            bench_shape("bench_bits", gated_bits, @bits_iters, num_runs,
              init: &init_bench_bits/0,
              vary: &vary_bench_bits/2,
              write: &Bench.Bench.write_bench_bits/1,
              read: &Bench.Bench.read_bench_bits/2,
              sink_of: &sink_of_bench_bits/1
            ) ++
            bench_shape("bench_mixed", gated_mixed, @mixed_iters, num_runs, mixed_opts)

        {rows, [g_packet, g_ints, g_bits, g_mixed]}
      end

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
