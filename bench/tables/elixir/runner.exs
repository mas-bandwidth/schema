# schema tables bench — the Elixir leg, measuring THE FIXED FORM
# (docs/SPEC-TABLES.md §3.4, form byte 3) over the paired corpus.
#
# It is the sibling of bench/tables/cpp/table_main.cpp and bench/tables/c/table_main.c
# and follows the same contract as bench/elixir/runner.exs does on the packet
# wire (BENCH-STANDARD.md): fixed iteration counts identical to every other
# runner's rows for this bench; 1 discarded warmup run then 7 measured runs per
# (bench, path), or exactly one measured run under `--round K`, where the
# interleaved driver aggregates across rounds (§2.4); CSV v2 rows on stdout
# under `--csv`, a human table on stderr.
#
# WHAT THE ROWS MEASURE. An op is ONE RECORD and the file is the unit: WRITE is
# one `fixed_table_fixed_save/1` laying down all sixty-four, ROUND-TRIP is one
# `fixed_table_fixed_load/1` of the whole file and then one save of what came
# out. The rates are records per second, exactly as the C++ and C legs report
# them, so the three rows divide against each other and against the packet row.
#
# WHY THERE IS NO `bench_table` ROW HERE. This backend carries the FIXED form
# and no other table wire: form 1, the tolerant form, is deferred (schema#515)
# and nothing in the generated Elixir reads or writes one. So this leg names its
# rows `bench_fixed`, which is the name bench/paired/main.go's parser expects
# from a leg that measures form 3, and it emits no tolerant row at all rather
# than a row it cannot stand behind.
#
# THE TOLERANT HALF IS STILL LOADED, AND ONLY LOADED. The corpus id (§1.6) is a
# fold over the files a run actually read, and the paired driver's TABLE id
# folds all five: `bench_fixed.bin`, `bench_fixed.layout`, `bench_table.bin`,
# `bench_table.lengths`, `bench_table.variants.bin`. A row measured against one
# corpus is not divisible against a row measured against another, so this leg
# reads all five and says plainly what it did with each: the two `bench_fixed`
# files are GATED — the layout compared byte for byte, the file loaded, saved
# back and compared — and the three `bench_table` files are READ AND NOT
# DECODED, because there is no form-1 codec here to decode them with. That is
# this leg's one gap and it is named rather than hidden; it closes when
# schema#515 lands.
defmodule SchemaTablesBenchElixir do
  import Bitwise

  @num_records 64
  @max_iterations 2_147_483_584

  # §1.2/§2.1: the fixed per-benchmark iteration count, identical to the C++
  # and C legs' `bench_fixed` rows
  @fixed_iters 400_000
  # --quick only, run.sh's iteration instrument and never the certification one
  @quick_fixed_iters 40_000

  # CSV v2 (§5.1) per-runner constants: family `table` (§1.9 — the table wire
  # over the table corpus, which is what makes a tools refusal to divide it
  # against a `gen` row correct and automatic); linkage `beam` (the generated
  # codec modules are compiled beside the caller into one VM and name no
  # library boundary); checks `contract` (no caller-error checks in the writer,
  # wire-contract validation unconditional in the reader — §3.4's word for
  # exactly this, and the axis bench/paired/main.go requires of every table
  # leg); opt `default` (the BEAM takes no level from us); inline `unknown`
  # until the §4 verdict pass has an Elixir branch.
  @csv_suffix "table,beam,contract,default,unknown"
  @packet_csv_suffix "gen,beam,contract,default,unknown"

  @mask64 0xFFFFFFFFFFFFFFFF

  defp gate_fail(what) do
    IO.write(:stderr, "GOLDEN GATE FAILED: bench_fixed #{what}\nreporting nothing.\n")
    System.halt(1)
  end

  defp fmt(value, decimals), do: :erlang.float_to_binary(value * 1.0, decimals: decimals)
  defp pad(value, width), do: String.pad_leading(value, width)

  # corpus_id (§1.6): FNV-1a-64 over the files this run loaded — for each file
  # in sorted basename order, the basename bytes, a 0x00 byte, the contents —
  # rendered as 16 lowercase hex digits. Bit for bit the driver's own fold
  # (bench/paired/main.go's `corpusID`).
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

  # A corpus file, from the first of the two directories the driver named that
  # holds it. Under bench/paired/main.go both are bench/paired/corpus; the two
  # names are separate on the packet wire and this leg keeps the distinction
  # rather than assuming they collapsed.
  defp corpus_file(opts, name) do
    [opts.wire_dir, opts.variant_dir]
    |> Enum.map(&Path.join(&1, name))
    |> Enum.find(&File.exists?/1)
    |> case do
      nil ->
        IO.write(
          :stderr,
          "missing corpus #{name} in #{opts.wire_dir} or #{opts.variant_dir} — run from the " <>
            "schema repo root, or pass --wire-dir/--variant-dir\n"
        )

        System.halt(1)

      path ->
        {name, File.read!(path)}
    end
  end

  # ------------------------------------------------------------------
  # the gates, every one of them before any clock (§1.5)
  # ------------------------------------------------------------------

  # gate 1: THE LAYOUT IS THE CORPUS'S LAYOUT. Every other fact about the form
  # — the positions, the ids, the kinds, the record's size, the hash every
  # record carries — is settled by these bytes, so this one comparison is what
  # says this leg speaks the form and not a near miss.
  defp gate_layout(layout) do
    mine = Bench.WrapFixed.fixed_table_fixed_layout()

    if layout != mine do
      gate_fail(
        "this build's layout is not the corpus's, byte for byte " <>
          "(#{byte_size(mine)} bytes here, #{byte_size(layout)} in the corpus)"
      )
    end
  end

  # gate 2: the whole file loads clean, and a same-schema read MOVES NO COUNTER.
  defp gate_load(file) do
    case Bench.WrapFixed.fixed_table_fixed_load(file) do
      {:ok, values, report} ->
        if length(values) != @num_records do
          gate_fail("the corpus holds #{length(values)} records, not #{@num_records}")
        end

        if {report.unknown, report.kind_mismatch, report.clamped, report.widened,
            report.malformed} != {0, 0, 0, 0, false} do
          gate_fail("a same-schema read moved a counter: #{inspect(report)}")
        end

        values

      other ->
        gate_fail("the corpus did not load: #{inspect(other)}")
    end
  end

  # gate 3: writing them back reproduces the file, BYTE FOR BYTE. This is the
  # matched gate — the whole claim that this port and the C++ reference agree
  # about the form, and not merely that this port round-trips itself.
  defp gate_save(values, file, which) do
    got = Bench.WrapFixed.fixed_table_fixed_save(values)

    if got != file do
      n = min(byte_size(got), byte_size(file))
      at = Enum.find(0..max(n - 1, 0)//1, fn i -> :binary.at(got, i) != :binary.at(file, i) end)

      gate_fail(
        "#{which} round-trip bytes differ — refusing to bench a codec that does not " <>
          "reproduce the corpus; first byte differing from the C++ reference at offset " <>
          "#{inspect(at)}, #{byte_size(got)} bytes vs #{byte_size(file)}"
      )
    end
  end

  # gate 4: the load owns the prefill, so a SECOND load into a value the first
  # one already produced re-saves the same bytes. On a language whose terms are
  # immutable there is no reused buffer to get wrong; what there IS to get wrong
  # is the plan cache and the prefill, and two more full passes are what
  # exercise them.
  defp gate_reload(file) do
    for _ <- 1..2 do
      gate_save(gate_load(file), file, "reloaded")
    end
  end

  # the packet wire's gates for --read-arm: variant 0 IS the packet golden, and
  # every variant decodes, re-encodes, and comes back byte-identical.
  defp gate_mixed_read(opts) do
    variant_path = Path.join(opts.packet_variant_dir, "bench_mixed.variants.bin")

    packed =
      case File.read(variant_path) do
        {:ok, bytes} when byte_size(bytes) > 0 and rem(byte_size(bytes), @num_records) == 0 ->
          bytes

        _ ->
          IO.write(
            :stderr,
            "missing or malformed packet corpus #{variant_path} — pass --packet-variant-dir\n"
          )

          System.halt(1)
      end

    record = div(byte_size(packed), @num_records)
    variants = for <<chunk::binary-size(^record) <- packed>>, do: chunk

    golden_path = Path.join(opts.packet_wire_dir, "bench_mixed.bin")

    golden_bytes =
      case File.read(golden_path) do
        {:ok, bytes} ->
          bytes

        {:error, _} ->
          IO.write(
            :stderr,
            "missing packet wire golden #{golden_path} — pass --packet-wire-dir\n"
          )

          System.halt(1)
      end

    if hd(variants) != golden_bytes do
      gate_fail("packet variant 0 is not the packet wire golden")
    end

    Enum.with_index(variants)
    |> Enum.each(fn {variant, k} ->
      case Bench.Bench.read_bench_mixed(variant, record * 8) do
        {:ok, decoded} ->
          if Bench.Bench.write_bench_mixed(decoded) != variant do
            gate_fail(
              "packet variant #{k} round-trip bytes differ — refusing to bench a codec " <>
                "that does not reproduce the corpus"
            )
          end

        :error ->
          gate_fail("decode of packet variant #{k} failed")
      end
    end)

    {List.to_tuple(variants), record,
     [{"bench_mixed.bin", golden_bytes}, {"bench_mixed.variants.bin", packed}]}
  end

  # ------------------------------------------------------------------
  # THE PLAN READ'S ANSWER, pinned before any clock
  # ------------------------------------------------------------------
  #
  # THIS PART OF THE LEG IS NOT SHAPE-BLIND, AND ITS LEDGER SAYS SO
  # (bench/tables/elixir/SHAPE-GATE.allow). What the NEW reader must answer for
  # an OLD record is the logical record with exactly the six narrowings of
  # bench/tables/elixir/lineage/VOLD_bench_lineage.schema applied, and saying
  # which slots they touch names those slots. It is the Rust leg's `expected`
  # (bench/tables/rust/src/lineage.rs on sprint/schema-rust), the same rules:
  #
  #   appended field   idle_ticks takes this build's declared DEFAULT
  #   widened bound    stats keeps the first @old_stats_bound elements, no more
  #   widened text     player_name keeps the bytes the OLD capacity holds
  #   enum append      an ordinal past the OLD variant count was folded back by
  #                    the lineage's producer, ((v - 1) mod COUNT) + 1, and the
  #                    answer carries the same fold
  #   new union arm    no record selects it (the narrowing refuses one)
  #   integer widen    delta's range is unmoved, so every value lands as it was
  #
  # THE COUNTER. §5.4 (docs/FIXED-FORM-ALGORITHM.md): `widened` moves once per
  # widen entry per record, and a widened run is never folded, so the integer
  # widen inside the bound is min(their_n, my_n) = min(64, 80) = 64 entries per
  # record. BENCH-STANDARD.md's "80 `widened` per record" is the NEW bound, not
  # §5.4's multiple of it; the Rust leg's gate asserts the same 64 x 64.
  @old_stats_bound 64
  @old_text_cap 8
  @old_weapon_variants 12
  @plan_widened_per_record 64

  @doc false
  # This build's declared defaults: the prefill image, decoded through this
  # build's own projection — never written down here.
  def plan_defaults do
    {row, _clamped, _damaged} =
      Bench.WrapFixed.fixed_table_fixed_decode(
        Bench.WrapFixed.fixed_table_fixed_prefill(),
        0,
        false
      )

    row
  end

  defp old_ordinal(v) when v > @old_weapon_variants, do: rem(v - 1, @old_weapon_variants) + 1
  defp old_ordinal(v), do: v

  @doc false
  # The plan read's answer for the 64 identity records.
  def plan_expected(identity_values, defaults) do
    Enum.map(identity_values, fn row ->
      v = row.value
      name = v.player_name

      %{
        row
        | value: %{
            v
            | entities: Enum.map(v.entities, fn e -> %{e | weapon: old_ordinal(e.weapon)} end),
              stats: Enum.take(v.stats, @old_stats_bound),
              player_name: binary_part(name, 0, min(byte_size(name), @old_text_cap)),
              idle_ticks: defaults.value.idle_ticks
          }
      }
    end)
  end

  @doc false
  # :ok when the plan read of the lineage file is exactly the plan's answer,
  # else {:error, why} naming the first thing that is not.
  def plan_read_verdict(identity_values, plan_values, report, defaults) do
    want_widened = @num_records * @plan_widened_per_record
    expected = plan_expected(identity_values, defaults)

    counters =
      {report.malformed, report.unknown, report.kind_mismatch, report.clamped, report.duplicate,
       report.widened}

    cond do
      length(identity_values) != @num_records ->
        {:error,
         "the identity corpus holds #{length(identity_values)} records, not #{@num_records}"}

      length(plan_values) != @num_records ->
        {:error, "the plan read returned #{length(plan_values)} records, not #{@num_records}"}

      counters != {false, 0, 0, 0, 0, want_widened} ->
        {:error,
         "the counters are not §5.4's for these six classes: #{inspect(report)} " <>
           "(want widened #{want_widened} = #{@num_records} x #{@plan_widened_per_record}, every other counter zero)"}

      true ->
        case Enum.find_index(Enum.zip(plan_values, expected), fn {got, want} -> got != want end) do
          nil ->
            if Bench.WrapFixed.fixed_table_fixed_save(plan_values) ==
                 Bench.WrapFixed.fixed_table_fixed_save(expected) do
              :ok
            else
              {:error, "the plan read saves to bytes that are not the plan's answer"}
            end

          k ->
            got = Enum.at(plan_values, k).value
            want = Enum.at(expected, k).value

            fields =
              for {f, w} <- Map.from_struct(want), Map.get(got, f) != w, do: f

            cond do
              got.idle_ticks != defaults.value.idle_ticks ->
                {:error,
                 "record #{k}: the appended field does not hold its declared default " <>
                   "(#{inspect(got.idle_ticks)}, declared #{inspect(defaults.value.idle_ticks)})"}

              length(got.stats) != length(want.stats) ->
                {:error,
                 "record #{k}: the widened bound carries #{length(got.stats)} elements, " <>
                   "not the OLD #{length(want.stats)}"}

              true ->
                {:error, "record #{k}: an OLD value did not land exactly (#{inspect(fields)})"}
            end
        end
    end
  end

  # the plan lane's gates. P1, P3 and P4 are shape-blind: no field is named, only
  # the lineage the build laid down and the bytes the shipped calls return. P2
  # holds the read to its pinned answer (plan_read_verdict above).
  defp gate_plan_read(lineage_dir, identity_file) do
    bin_path = Path.join(lineage_dir, "bench_lineage.bin")
    layout_path = Path.join(lineage_dir, "bench_lineage.layout")

    {lineage_file, lineage_layout} =
      with {:ok, bin} when byte_size(bin) >= 20 <- File.read(bin_path),
           {:ok, lay} <- File.read(layout_path) do
        {bin, lay}
      else
        _ ->
          IO.write(:stderr, "missing lineage corpus in #{lineage_dir} — pass --lineage-dir\n")
          System.halt(1)
      end

    # gate P1: THE LINEAGE IS THE LOCK'S. This build knows two layouts, the
    # older one first, and the lineage file carries that older layout's hash
    # and bytes — so the read below runs a plan, never the identity.
    known = Bench.WrapFixed.fixed_table_fixed_known()

    if tuple_size(known) != 2 do
      gate_fail("this build's lineage does not carry the older layout")
    end

    old = elem(known, 0)
    new_hash = Bench.WrapFixed.fixed_table_fixed_hash()

    case Bench.FixedRuntime.read_file_header(lineage_file) do
      {:ok, file_hash, file_layout, _records}
      when old.hash != new_hash and
             file_hash == old.hash and
             old.layout_bytes == byte_size(lineage_layout) and
             old.layout == lineage_layout and
             file_layout == lineage_layout ->
        :ok

      _ ->
        gate_fail(
          "the lineage corpus is not the lock's older layout, or this build's lineage is not that lock's"
        )
    end

    # gate P2: NEW READS OLD, and the answer is the one the versioning law
    # says it must be (BENCH-STANDARD.md, "The gate before the clock"): every
    # OLD value landed exactly, the reader's appended field holds its DECLARED
    # default, the widened bound carries the OLD elements and nothing past
    # them, and the counters are §5.4's EXACT numbers. The answer is derived
    # from the 64 records the identity corpus decoded to (gates 1-4 above have
    # held those to the C++ reference byte for byte), never from a second read
    # of the lineage file — two identically wrong reads would agree with each
    # other, and they do not agree with this.
    identity_values = gate_load(identity_file)

    plan_values =
      case Bench.WrapFixed.fixed_table_fixed_load(lineage_file) do
        {:ok, loaded, report} ->
          case plan_read_verdict(identity_values, loaded, report, plan_defaults()) do
            :ok ->
              IO.write(
                :stderr,
                "#{String.pad_trailing("bench_plan_read", 18)} plan read of #{@num_records} records: " <>
                  "widened #{report.widened} (expected #{@num_records} x #{@plan_widened_per_record}), " <>
                  "clamped #{report.clamped}; every OLD value, the appended default and the " <>
                  "bound's #{@old_stats_bound} elements landed exactly\n"
              )

              loaded

            {:error, why} ->
              gate_fail("the lineage corpus did not read to the plan's answer: #{why}")
          end

        other ->
          gate_fail("the lineage corpus did not read clean through its plan: #{inspect(other)}")
      end

    # gate P3: the prefill owns every byte the plan does not land — a second
    # load saves to the same bytes byte for byte.
    saved_a = Bench.WrapFixed.fixed_table_fixed_save(plan_values)

    case Bench.WrapFixed.fixed_table_fixed_load(lineage_file) do
      {:ok, second_values, _report} ->
        saved_b = Bench.WrapFixed.fixed_table_fixed_save(second_values)

        if saved_a != saved_b do
          gate_fail("a second plan load saved to different bytes")
        end

      other ->
        gate_fail("second plan load failed: #{inspect(other)}")
    end

    # gate P4: the plan lane reads something the identity lane does not —
    # its records saved in today's layout are not the identity corpus.
    if saved_a == identity_file do
      gate_fail(
        "the lineage corpus reads to the identity corpus: the plan lane would measure nothing older"
      )
    end

    {lineage_file,
     [{"bench_lineage.bin", lineage_file}, {"bench_lineage.layout", lineage_layout}]}
  end

  # ------------------------------------------------------------------
  # the clock
  # ------------------------------------------------------------------

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

  # WRITE: one call lays down all @num_records records; an op is one record. The
  # accumulated size is the sink — the runtime cannot prove the env var absent,
  # so no loop's work can be deleted.
  defp write_loop(remaining, _values, acc) when remaining <= 0, do: acc

  defp write_loop(remaining, values, acc) do
    write_loop(
      remaining - @num_records,
      values,
      acc + byte_size(Bench.WrapFixed.fixed_table_fixed_save(values))
    )
  end

  # ROUND-TRIP: read the file back, then write what came out.
  defp roundtrip_loop(remaining, _file, acc) when remaining <= 0, do: acc

  defp roundtrip_loop(remaining, file, acc) do
    {:ok, values, _report} = Bench.WrapFixed.fixed_table_fixed_load(file)

    roundtrip_loop(
      remaining - @num_records,
      file,
      acc + byte_size(Bench.WrapFixed.fixed_table_fixed_save(values))
    )
  end

  # READ (fixed form, identity or plan): read the whole file alone, never
  # derived from a round trip minus a write (§2.9).
  defp read_loop(remaining, _file, acc) when remaining <= 0, do: acc

  defp read_loop(remaining, file, acc) do
    {:ok, values, _report} = Bench.WrapFixed.fixed_table_fixed_load(file)
    read_loop(remaining - @num_records, file, acc + length(values))
  end

  # READ (packet wire): decode the 64 rotating packet variants alone.
  defp packet_read_loop(0, _i, _variants, _num_bits, acc), do: acc

  defp packet_read_loop(n, i, variants, num_bits, acc) do
    variant = elem(variants, i &&& @num_records - 1)
    {:ok, decoded} = Bench.Bench.read_bench_mixed(variant, num_bits)
    packet_read_loop(n - 1, i + 1, variants, num_bits, acc + map_size(decoded))
  end

  defp report(bench, path, iters, bytes_per_op, rates) do
    {median, min, max, spread} = stats(rates)
    mbps = median * bytes_per_op / (1024.0 * 1024.0)

    IO.write(
      :stderr,
      "#{String.pad_trailing(bench, 18)} #{String.pad_trailing(path, 11)} " <>
        "#{pad(fmt(median / 1.0e6, 3), 10)} M rec/s #{pad(fmt(mbps, 1), 10)} MB/s   " <>
        "(min #{fmt(min / 1.0e6, 3)}, max #{fmt(max / 1.0e6, 3)}, spread #{fmt(spread, 1)}%)\n"
    )

    "elixir,#{bench},#{path},#{iters},#{bytes_per_op},#{length(rates)}," <>
      "#{fmt(median, 0)},#{fmt(min, 0)},#{fmt(max, 0)},#{fmt(mbps, 2)},#{fmt(spread, 2)}"
  end

  # ------------------------------------------------------------------
  # argv — bench/paired/main.go's `runner` and bench/tables/run.sh's leg
  # contract, which is the same set the C++ and C legs answer
  # ------------------------------------------------------------------

  defp parse_args(argv) do
    parse_args(argv, %{
      csv: false,
      gate: false,
      quick: false,
      read_arm: false,
      num_runs: 7,
      round: 0,
      iterations: 0,
      packet_iterations: 0,
      wire_dir: "bench/paired/corpus",
      variant_dir: "bench/paired/corpus",
      packet_wire_dir: "testdata/wire",
      packet_variant_dir: "bench/corpus/variants",
      lineage_dir: nil
    })
  end

  defp parse_args([], opts), do: opts
  defp parse_args(["--csv" | rest], opts), do: parse_args(rest, %{opts | csv: true})
  defp parse_args(["--gate" | rest], opts), do: parse_args(rest, %{opts | gate: true})
  defp parse_args(["--quick" | rest], opts), do: parse_args(rest, %{opts | quick: true})
  defp parse_args(["--read-arm" | rest], opts), do: parse_args(rest, %{opts | read_arm: true})

  # `--indexed` names the tolerant form's 64-entry length index. The fixed form
  # carries no lengths at all — a record's size is the layout's, so the count is
  # arithmetic — so the flag is accepted and means nothing here.
  defp parse_args(["--indexed" | rest], opts), do: parse_args(rest, opts)

  defp parse_args(["--round", k | rest], opts) do
    case Integer.parse(k) do
      {v, ""} when v >= 0 ->
        parse_args(rest, %{opts | num_runs: 1, round: v})

      _ ->
        IO.write(:stderr, "--round requires a non-negative integer\n")
        System.halt(1)
    end
  end

  defp parse_args(["--wire-dir", dir | rest], opts),
    do: parse_args(rest, %{opts | wire_dir: dir})

  defp parse_args(["--variant-dir", dir | rest], opts),
    do: parse_args(rest, %{opts | variant_dir: dir})

  defp parse_args(["--packet-wire-dir", dir | rest], opts),
    do: parse_args(rest, %{opts | packet_wire_dir: dir})

  defp parse_args(["--packet-variant-dir", dir | rest], opts),
    do: parse_args(rest, %{opts | packet_variant_dir: dir})

  defp parse_args(["--lineage-dir", dir | rest], opts),
    do: parse_args(rest, %{opts | lineage_dir: dir})

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

  defp parse_args(["--packet-iterations", n | rest], opts) do
    case Integer.parse(n) do
      {v, ""} when v > 0 and v <= @max_iterations and rem(v, @num_records) == 0 ->
        parse_args(rest, %{opts | packet_iterations: v})

      _ ->
        IO.write(
          :stderr,
          "--packet-iterations requires a positive multiple of #{@num_records} up to #{@max_iterations}\n"
        )

        System.halt(1)
    end
  end

  defp parse_args([arg | _], _opts) do
    IO.write(
      :stderr,
      "usage: main.exs [--indexed] [--gate] [--csv] [--round K] [--quick] [--iterations N] " <>
        "[--wire-dir <dir>] [--variant-dir <dir>] [--read-arm [--packet-iterations N] " <>
        "[--packet-wire-dir <dir>] [--packet-variant-dir <dir>] [--lineage-dir <dir>]] (got #{arg})\n"
    )

    System.halt(1)
  end

  defp run_read_arm(opts, file, table_goldens) do
    {packet_variants, packet_record, packet_goldens} = gate_mixed_read(opts)
    plan? = opts.lineage_dir != nil or tuple_size(Bench.WrapFixed.fixed_table_fixed_known()) > 1

    {lineage_file, lineage_goldens} =
      if plan? do
        gate_plan_read(opts.lineage_dir || "bench/tables/elixir/lineage", file)
      else
        {nil, []}
      end

    id = corpus_id(table_goldens ++ packet_goldens ++ lineage_goldens)

    if opts.gate do
      IO.write(:stderr, "OK (gate only, corpus_id #{id})\n")
      System.halt(0)
    end

    iters =
      cond do
        opts.iterations > 0 -> opts.iterations
        opts.quick -> @quick_fixed_iters
        true -> @fixed_iters
      end

    packet_iters = if opts.packet_iterations > 0, do: opts.packet_iterations, else: iters
    num_steps = if plan?, do: 3, else: 2
    fixed_bytes = byte_size(file) / @num_records
    num_bits = packet_record * 8

    rows =
      for step <- 0..(num_steps - 1)//1 do
        case rem(opts.round + step, num_steps) do
          0 ->
            rates =
              timed_runs(opts.num_runs, fn -> sink(read_loop(iters, file, 0), iters) end)

            "#{report("bench_fixed_read", "read", iters, fixed_bytes, rates)},#{id},#{@csv_suffix}"

          1 ->
            rates =
              timed_runs(opts.num_runs, fn ->
                sink(
                  packet_read_loop(packet_iters, 0, packet_variants, num_bits, 0),
                  packet_iters
                )
              end)

            "#{report("bench_mixed_read", "read", packet_iters, packet_record * 1.0, rates)},#{id},#{@packet_csv_suffix}"

          2 ->
            plan_bytes = byte_size(lineage_file) / @num_records

            rates =
              timed_runs(opts.num_runs, fn ->
                sink(read_loop(iters, lineage_file, 0), iters)
              end)

            "#{report("bench_plan_read", "read", iters, plan_bytes, rates)},#{id},#{@csv_suffix}"
        end
      end

    if opts.csv do
      IO.write(
        "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec," <>
          "max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n"
      )

      for row <- rows, do: IO.write("#{row}\n")
    end

    IO.write(:stderr, "OK (corpus_id #{id})\n")

    if System.get_env("SERIALIZE_BENCH_SINK") do
      IO.write(:stderr, "sink: #{Process.get(:bench_sink, 0)}\n")
    end
  end

  def main(argv) do
    opts = parse_args(argv)

    IO.write(
      :stderr,
      "schema tables bench (elixir, the fixed form" <>
        if(opts.quick, do: ", --quick: iteration instrument, not certification", else: "") <>
        ")\n"
    )

    {_, file} = fixed_bin = corpus_file(opts, "bench_fixed.bin")
    {_, layout} = fixed_layout = corpus_file(opts, "bench_fixed.layout")

    # READ AND NOT DECODED — see this file's header. They are in the fold
    # because the driver's table id folds them, and a row that claimed a corpus
    # it never opened would be a row answerable to nothing.
    tolerant =
      for name <- ["bench_table.bin", "bench_table.lengths", "bench_table.variants.bin"],
          do: corpus_file(opts, name)

    goldens = [fixed_bin, fixed_layout | tolerant]
    id = corpus_id(goldens)

    if byte_size(file) == 0, do: gate_fail("the corpus is empty")

    gate_layout(layout)
    values = gate_load(file)
    gate_save(values, file, "same-storage")
    gate_reload(file)

    if opts.read_arm do
      run_read_arm(opts, file, goldens)
      System.halt(0)
    end

    # `--gate` IS THE GATE AND NOTHING ELSE (bench/paired/main.go's `gate`): the
    # whole report is an exit status and an empty stdout.
    if opts.gate do
      IO.write(:stderr, "OK (gate only, corpus_id #{id})\n")
      System.halt(0)
    end

    iters =
      cond do
        opts.iterations > 0 -> opts.iterations
        opts.quick -> @quick_fixed_iters
        true -> @fixed_iters
      end

    bytes_per_op = byte_size(file) / @num_records

    write_rates = timed_runs(opts.num_runs, fn -> sink(write_loop(iters, values, 0), iters) end)

    roundtrip_rates =
      timed_runs(opts.num_runs, fn -> sink(roundtrip_loop(iters, file, 0), iters) end)

    rows = [
      report("bench_fixed", "write", iters, bytes_per_op, write_rates),
      report("bench_fixed", "round_trip", iters, bytes_per_op, roundtrip_rates)
    ]

    # READ is DERIVED, never measured: round-trip time minus write time. It
    # prints for continuity with the other legs' stderr and is NOT a CSV row —
    # a derived number in the CSV would be divided as if it had been measured.
    {w_median, _, _, _} = stats(write_rates)
    {rt_median, _, _, _} = stats(roundtrip_rates)
    read_time = 1.0 / rt_median - 1.0 / w_median

    if read_time > 0 do
      IO.write(
        :stderr,
        "#{String.pad_trailing("bench_fixed", 18)} #{String.pad_trailing("read", 11)} " <>
          "#{pad(fmt(1.0e-6 / read_time, 3), 10)} M rec/s   " <>
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

# bench/tables/elixir/plan_gate_test.exs loads this file for its functions alone.
if System.get_env("SCHEMA_TABLES_BENCH_NO_MAIN") != "1" do
  SchemaTablesBenchElixir.main(System.argv())
end
