# The Elixir plan-read gate (bench/tables/elixir/runner.exs, gate P2) holds the
# plan read of the lineage corpus to its PINNED answer: every OLD value, the
# appended field's declared default, the widened bound's OLD elements and
# §5.4's exact `widened` count. Each refusal below is a read the gate it
# replaced accepted (any `widened > 0`, every other counter zero, and a second
# read agreeing with the first), so this file is red against that gate.
#
# Run from the repository root, against the PLAN build the paired driver makes
# (`go run ./bench/paired -mode build -langs elixir`):
#
#   SCHEMA_ELIXIR_GEN_DIR=build/paired/plan-elixir-unit/new \
#     elixir -pa build/paired/plan-elixir/ebin bench/tables/elixir/plan_gate_test.exs
#
# No clock, no socket, no network: it reads two committed corpus files.
ExUnit.start(autorun: false)
System.put_env("SCHEMA_TABLES_BENCH_NO_MAIN", "1")
Code.require_file("main.exs", __DIR__)

defmodule SchemaTablesBenchElixir.PlanGateTest do
  use ExUnit.Case, async: true

  alias SchemaTablesBenchElixir, as: Leg

  @lineage "bench/tables/elixir/lineage/bench_lineage.bin"
  @identity "bench/paired/corpus/bench_fixed.bin"

  setup_all do
    if tuple_size(Bench.WrapFixed.fixed_table_fixed_known()) != 2 do
      raise "not the plan build: run against build/paired/plan-elixir-unit/new (see this file's header)"
    end

    {:ok, identity, _} = Bench.WrapFixed.fixed_table_fixed_load(File.read!(@identity))
    {:ok, plan, report} = Bench.WrapFixed.fixed_table_fixed_load(File.read!(@lineage))
    %{identity: identity, plan: plan, report: report, defaults: Leg.plan_defaults()}
  end

  # The gate this one replaced, verbatim in its conditions.
  defp old_gate_accepts?(plan, report) do
    length(plan) == 64 and report.malformed == false and report.unknown == 0 and
      report.kind_mismatch == 0 and report.clamped == 0 and report.duplicate == 0 and
      report.widened > 0
  end

  defp at(values, k, fun),
    do: List.update_at(values, k, fn row -> %{row | value: fun.(row.value)} end)

  # `slipped`: the replaced gate let this read through, so it is this gate's to catch.
  defp refused(ctx, plan, report, needle, slipped \\ true) do
    assert old_gate_accepts?(plan, report) == slipped
    assert {:error, why} = Leg.plan_read_verdict(ctx.identity, plan, report, ctx.defaults)
    assert why =~ needle
  end

  test "the committed lineage reads to the pinned answer", ctx do
    assert ctx.report.widened == 64 * 64
    assert Leg.plan_read_verdict(ctx.identity, ctx.plan, ctx.report, ctx.defaults) == :ok
  end

  test "an OLD value that did not land is refused", ctx do
    plan = at(ctx.plan, 5, fn v -> %{v | sequence: Bitwise.bxor(v.sequence, 1)} end)
    refused(ctx, plan, ctx.report, "record 5: an OLD value did not land exactly ([:sequence])")

    plan =
      at(ctx.plan, 9, fn v ->
        %{v | stats: List.update_at(v.stats, 63, &%{&1 | delta: &1.delta + 1})}
      end)

    refused(ctx, plan, ctx.report, "record 9: an OLD value did not land exactly ([:stats])")

    plan = at(ctx.plan, 0, fn v -> %{v | entities: Enum.map(v.entities, &%{&1 | weapon: 0})} end)
    refused(ctx, plan, ctx.report, "record 0: an OLD value did not land exactly ([:entities])")
  end

  test "an appended field that does not hold its declared default is refused", ctx do
    plan = at(ctx.plan, 0, fn v -> %{v | idle_ticks: hd(ctx.identity).value.idle_ticks} end)
    assert hd(ctx.identity).value.idle_ticks != ctx.defaults.value.idle_ticks

    refused(
      ctx,
      plan,
      ctx.report,
      "record 0: the appended field does not hold its declared default"
    )
  end

  test "a widened bound carrying more than the OLD elements is refused", ctx do
    plan = at(ctx.plan, 2, fn v -> %{v | stats: Enum.at(ctx.identity, 2).value.stats} end)

    refused(
      ctx,
      plan,
      ctx.report,
      "record 2: the widened bound carries 80 elements, not the OLD 64"
    )
  end

  test "a widened count that is not §5.4's exact number is refused", ctx do
    for n <- [4095, 4097, 64 * 80, 1] do
      report = %{ctx.report | widened: n}
      refused(ctx, ctx.plan, report, "the counters are not §5.4's")
    end

    refused(ctx, ctx.plan, %{ctx.report | clamped: 1}, "the counters are not §5.4's", false)
  end

  test "a lineage file with one OLD value flipped reads past the replaced gate and is refused",
       ctx do
    file = File.read!(@lineage)
    # record 5's first field, one bit: past the header and layout, five whole
    # records and their 8-byte hash. Every size is the file's own.
    {:ok, _hash, layout, _records} = Bench.FixedRuntime.read_file_header(file)
    record = div(byte_size(file) - 20 - byte_size(layout), 64)
    at = 20 + byte_size(layout) + 5 * record + 8
    <<pre::binary-size(^at), b, post::binary>> = file

    {:ok, forged, report} =
      Bench.WrapFixed.fixed_table_fixed_load(<<pre::binary, Bitwise.bxor(b, 1), post::binary>>)

    refused(ctx, forged, report, "record 5: an OLD value did not land exactly ([:sequence])")
  end

  test "two identically wrong reads do not agree with the answer", ctx do
    wrong = at(ctx.plan, 31, fn v -> %{v | nonce: v.nonce + 1} end)
    saved = Bench.WrapFixed.fixed_table_fixed_save(wrong)
    assert saved == Bench.WrapFixed.fixed_table_fixed_save(wrong)
    refused(ctx, wrong, ctx.report, "record 31: an OLD value did not land exactly ([:nonce])")
  end
end

ExUnit.run() |> then(fn %{failures: f} -> System.halt(if f == 0, do: 0, else: 1) end)
