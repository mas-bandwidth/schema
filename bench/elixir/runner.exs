# The timed rows — main.exs loads the generated modules first; see the note
# there on why the two files are split.
defmodule SchemaBenchElixir do
  import Bitwise

  @num_trials 5
  @num_variants 64

  # ------------------------------------------------------------------
  # the C bench's uint64 LCG, direct in BEAM integers under a 64-bit mask
  # (serialize.elixir bench/bench.exs, verbatim)
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

  defp env_int(name, fallback) do
    case System.get_env(name) do
      nil ->
        fallback

      raw ->
        case Integer.parse(raw) do
          {value, ""} when value >= 1 ->
            value

          _ ->
            IO.write(:stderr, "#{name} must be a positive integer\n")
            System.halt(1)
        end
    end
  end

  defp gate_fail(row, what) do
    IO.write(:stderr, "GOLDEN GATE FAILED: #{row} #{what}\nreporting nothing.\n")
    System.halt(1)
  end

  defp golden(name), do: File.read!("../../testdata/wire/#{name}.bin")

  defp fmt(value, decimals), do: :erlang.float_to_binary(value * 1.0, decimals: decimals)
  defp pad(value, width), do: String.pad_leading(value, width)

  # ------------------------------------------------------------------
  # the golden gate (§1.5), shared by every shape: the PINNED instance's
  # bytes must equal the C++-pinned testdata/wire golden, and all 64 variant
  # buffers must decode back field-perfect. A mismatch refuses to bench.
  # ------------------------------------------------------------------

  defp gate_shape(row, golden_name, init, vary, write, read) do
    packet = init.()
    wire = write.(packet)

    if wire != golden(golden_name) do
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

    {List.to_tuple(variants), bytes_per_packet}
  end

  # ------------------------------------------------------------------
  # the timed rows: write and read (and measure, where the family prints
  # it), best of five trials — serialize.elixir bench/bench.exs's loop, with
  # the generated monomorphic functions in place of the stream calls. Every
  # loop's work flows into the sink (published at exit under an env var the
  # runtime cannot rule out), and the LCG varies every written packet.
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

  defp measure_loop(0, _rng, _p, _vary, _measure, acc), do: acc

  defp measure_loop(n, rng, p, vary, measure, acc) do
    rng = lcg_step(rng)
    p = vary.(p, rng)
    measure_loop(n - 1, rng, p, vary, measure, acc + measure.(p))
  end

  defp best_of(nil, ns), do: ns
  defp best_of(best, ns), do: min(best, ns)

  defp bench_shape(row, label, gated, opts) do
    {variants, bytes_per_packet} = gated
    init = opts[:init]
    vary = opts[:vary]
    write = opts[:write]
    read = opts[:read]
    sink_of = opts[:sink_of]
    measure = opts[:measure]
    mb_row = opts[:mb_row] || false
    num_packets = opts[:num_packets]
    num_bits = bytes_per_packet * 8

    {best_write, best_read, best_measure, sink} =
      Enum.reduce(0..@num_trials, {nil, nil, nil, 0}, fn trial, {bw, br, bm, sink} ->
        t0 = System.monotonic_time(:nanosecond)
        {sink_w, rng} = write_loop(num_packets, lcg_seed(), init.(), vary, write, 0)
        write_ns = System.monotonic_time(:nanosecond) - t0

        t0 = System.monotonic_time(:nanosecond)
        sink_r = read_loop(num_packets, 0, variants, read, num_bits, sink_of, 0)
        read_ns = System.monotonic_time(:nanosecond) - t0

        {sink_m, measure_ns} =
          if measure do
            t0 = System.monotonic_time(:nanosecond)
            sink_m = measure_loop(num_packets, rng, init.(), vary, measure, 0)
            {sink_m, System.monotonic_time(:nanosecond) - t0}
          else
            {0, 0}
          end

        sink = sink + sink_w + sink_r + sink_m

        if trial == 0 do
          # the untimed warmup pass
          {bw, br, bm, sink}
        else
          {best_of(bw, write_ns), best_of(br, read_ns), best_of(bm, measure_ns), sink}
        end
      end)

    total_mb = bytes_per_packet * num_packets / (1024 * 1024)
    packets = num_packets / 1_000_000

    write_s = best_write * 1.0e-9
    read_s = best_read * 1.0e-9

    rows =
      if mb_row do
        IO.write(
          "#{label} write: #{pad(fmt(total_mb / write_s, 1), 8)} MB/s  (#{fmt(packets / write_s, 1)} M packets/s)\n" <>
            "#{label} read:  #{pad(fmt(total_mb / read_s, 1), 8)} MB/s  (#{fmt(packets / read_s, 1)} M packets/s)\n"
        )

        [
          {row, "write", "MB/s", total_mb / write_s},
          {row, "write", "Mpackets/s", packets / write_s},
          {row, "read", "MB/s", total_mb / read_s},
          {row, "read", "Mpackets/s", packets / read_s}
        ]
      else
        IO.write(
          "#{label} write: #{pad(fmt(packets / write_s, 1), 6)} M packets/s   read: #{pad(fmt(packets / read_s, 1), 6)} M packets/s\n"
        )

        [
          {row, "write", "Mpackets/s", packets / write_s},
          {row, "read", "Mpackets/s", packets / read_s}
        ]
      end

    rows =
      if measure do
        measure_s = best_measure * 1.0e-9

        IO.write(
          "#{label} measure: #{pad(fmt(packets / measure_s, 1), 6)} M packets/s (generation-time folded)\n"
        )

        rows ++ [{row, "measure", "Mpackets/s", packets / measure_s}]
      else
        rows
      end

    {rows, sink}
  end

  def main(argv) do
    csv = "--csv" in argv
    num_packets = env_int("BENCH_STREAM_PACKETS", 1_000_000)

    # every row's golden gate runs before any row is timed: a runner that
    # fails its goldens reports nothing at all (§1.5)
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

    gated_mixed =
      gate_shape(
        "bench_mixed",
        "bench_mixed",
        &init_bench_mixed/0,
        &vary_bench_mixed/2,
        &Bench.Bench.write_bench_mixed/1,
        &Bench.Bench.read_bench_mixed/2
      )

    IO.write("\n[schema bench — generated Elixir]\n\n")

    # the stream-comparable row: the same 12-op packet serialize.elixir's
    # stream rows measure, through the generated monomorphic codec
    {rows1, sink1} =
      bench_shape("bench_packet", "packet (generated):", gated_packet,
        init: &init_bench_packet/0,
        vary: &vary_bench_packet/2,
        write: &Bench.Bench.write_bench_packet/1,
        read: &Bench.Bench.read_bench_packet/2,
        sink_of: & &1.b,
        measure: &Bench.Bench.measure_bench_packet/1,
        mb_row: true,
        num_packets: num_packets
      )

    IO.write("\n")

    {rows2, sink2} =
      bench_shape("bench_ints", "int packet   (generated):", gated_ints,
        init: &init_bench_ints/0,
        vary: &vary_bench_ints/2,
        write: &Bench.Bench.write_bench_ints/1,
        read: &Bench.Bench.read_bench_ints/2,
        sink_of: & &1.f0,
        num_packets: num_packets
      )

    {rows3, sink3} =
      bench_shape("bench_bits", "bits packet  (generated):", gated_bits,
        init: &init_bench_bits/0,
        vary: &vary_bench_bits/2,
        write: &Bench.Bench.write_bench_bits/1,
        read: &Bench.Bench.read_bench_bits/2,
        sink_of: & &1.b7,
        num_packets: num_packets
      )

    {rows4, sink4} =
      bench_shape("bench_mixed", "mixed packet (generated):", gated_mixed,
        init: &init_bench_mixed/0,
        vary: &vary_bench_mixed/2,
        write: &Bench.Bench.write_bench_mixed/1,
        read: &Bench.Bench.read_bench_mixed/2,
        sink_of: & &1.sequence,
        num_packets: num_packets
      )

    IO.write("\n")

    if csv do
      IO.write("row,op,units,value\n")

      for {row, op, units, value} <- rows1 ++ rows2 ++ rows3 ++ rows4 do
        IO.write("#{row},#{op},#{units},#{fmt(value, 4)}\n")
      end
    end

    # the g_sink escape: the runtime cannot prove the env var absent, so the
    # accumulated sink is observable and no loop's work can be deleted
    if System.get_env("SERIALIZE_BENCH_SINK") do
      IO.write(:stderr, "sink: #{sink1 + sink2 + sink3 + sink4}\n")
    end
  end
end

SchemaBenchElixir.main(System.argv())
