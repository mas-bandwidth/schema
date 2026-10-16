# The Elixir table allocation audit (docs/PORTING.md I1, I14; issue #420).
#
# THE PIN COMES FIRST: test/elixir/runtime_pin.exs runs before a single module
# is loaded or a single case is measured. On any OTP or Elixir other than the
# pinned pair in make/elixir.mk it prints FAILED and exits non-zero without
# measuring; with SCHEMA_ELIXIR_ANY_OTP=1 it prints NOT CERTIFIED and reports
# the numbers without gating.
Code.require_file("test/elixir/runtime_pin.exs")

defmodule ElixirAllocAudit do
  @iterations 40

  # Per-iteration slack above the pinned budget:
  # - heap words: 64 words + 0.01% of the case's word budget for minor GC
  #   boundary rounding over @iterations iterations.
  # - binary_alloc calls: 8 calls per iteration because
  #   :erlang.system_info({:allocator, :binary_alloc}) is VM-wide.
  # - reductions: printed in the table and recorded in alloc-budget.txt, not gated.
  @binary_slack 8
  defp word_slack(w), do: 64 + div(w, 10_000)

  def main([_otp_want, _elixir_want, manifest, budget_file | rest]) do
    pin? = rest == ["pin"]
    any_otp? = System.get_env("SCHEMA_ELIXIR_ANY_OTP") == "1"

    if pin? and any_otp? do
      IO.puts(:stderr, "FAILED: refusing to write #{budget_file} under SCHEMA_ELIXIR_ANY_OTP=1")
      System.halt(1)
    end

    ebin = System.get_env("ELIXIR_TABLES_EBIN", "build/elixir-tables-ebin")
    Code.prepend_path(ebin)
    load_beams(ebin)

    cases = load_cases(manifest)

    if cases == [] do
      IO.puts(:stderr, "FAILED: no carried <Unit>.Table instances found in #{manifest}")
      System.halt(1)
    end

    measured =
      for c <- cases do
        {w, b, r} = measure_in_fresh_process(c.work)
        {c.name, w, b, r}
      end

    cond do
      pin? ->
        write_budget(budget_file, measured)
        IO.puts("elixir allocation audit: pinned #{length(measured)} cases to #{budget_file}")

      any_otp? ->
        IO.puts(
          "elixir allocation audit (NOT CERTIFIED, #{@iterations} iterations/case, #{length(measured)} cases):"
        )

        for {name, w, b, r} <- measured do
          IO.puts(
            "  #{String.pad_trailing(name, 24)} words=#{pad(w, 9)}  bin=#{pad(b, 4)}  reds=#{pad(r, 9)}  NOT CERTIFIED"
          )
        end

      true ->
        check_budget(budget_file, measured)
    end
  end

  def main(_) do
    IO.puts(
      :stderr,
      "usage: elixir test/elixir-alloc/main.exs <otp-pin> <elixir-pin> <manifest> <budget-file> [pin]"
    )

    System.halt(64)
  end

  defp load_beams(ebin) do
    for path <- Path.wildcard(Path.join(ebin, "*.beam")) do
      mod = path |> Path.basename(".beam") |> String.to_atom()
      Code.ensure_loaded(mod)
    end
  end

  defp table_module(unit) do
    mod = Module.concat([Macro.camelize(unit), "Table"])

    if Code.ensure_loaded?(mod) and function_exported?(mod, :load, 2) and
         function_exported?(mod, :save, 2) do
      mod
    else
      nil
    end
  end

  defp carried?(mod, root) do
    not function_exported?(mod, :carried?, 1) or mod.carried?(root)
  end

  defp load_cases(manifest) do
    manifest
    |> File.read!()
    |> String.split("\n")
    |> Enum.map(&String.trim/1)
    |> Enum.reject(fn line -> line == "" or String.starts_with?(line, "#") end)
    |> Enum.map(&String.split(&1, ~r/[ \t]+/))
    |> Enum.flat_map(fn
      ["instance", name, unit, root, wire | rest] ->
        case table_module(unit) do
          nil ->
            []

          mod ->
            if carried?(mod, root) do
              wire_bytes = File.read!(wire)

              json_bytes =
                if rest == [] do
                  File.read!(
                    Path.join(["testdata", "conformance", "tables", "json", name <> ".json"])
                  )
                else
                  nil
                end

              work = build_work(mod, root, wire_bytes, json_bytes)
              [%{name: name, work: work}]
            else
              []
            end
        end

      _ ->
        []
    end)
  end

  defp build_work(mod, root, wire_bytes, nil) do
    fn ->
      {:ok, value, _report} = mod.load(root, wire_bytes)
      {:ok, _saved} = mod.save(root, value)
      :ok
    end
  end

  defp build_work(mod, root, wire_bytes, json_bytes) do
    fn ->
      {:ok, value, _report} = mod.load(root, wire_bytes)
      {:ok, _saved} = mod.save(root, value)
      {:ok, jval, _jreport} = mod.from_json(root, json_bytes)
      {:ok, _text} = mod.to_json(root, jval)
      :ok
    end
  end

  defp measure_in_fresh_process(work) do
    parent = self()

    _pid =
      spawn_link(fn ->
        send(parent, {:measured, measure(work)})
      end)

    receive do
      {:measured, res} -> res
    end
  end

  defp measure(work) do
    # Warm the code path and module literal pools once before tracing.
    work.()
    me = self()
    tracer = spawn_link(fn -> trace_loop(0) end)
    :erlang.garbage_collect(me)
    :erlang.trace(me, true, [:garbage_collection, {:tracer, tracer}])
    bin0 = binary_alloc_calls()
    {_, red0} = :erlang.process_info(me, :reductions)

    for _ <- 1..@iterations, do: work.()

    # Collect inside the traced window while keeping `work` live on the stack
    # so only words allocated by the loop iterations are counted as reclaimed.
    :erlang.garbage_collect(me)
    :erlang.trace(me, false, [:garbage_collection])
    _ = :erlang.phash2(work)
    {_, red1} = :erlang.process_info(me, :reductions)
    bin1 = binary_alloc_calls()
    send(tracer, {:done, me})

    reclaimed =
      receive do
        {:reclaimed, n} -> n
      end

    {
      div(reclaimed, @iterations),
      max(0, div(bin1 - bin0, @iterations)),
      div(red1 - red0, @iterations)
    }
  end

  defp trace_loop(reclaimed) do
    receive do
      {:trace, _pid, :gc_minor_start, info} ->
        trace_loop(reclaimed + heap_words(info))

      {:trace, _pid, :gc_major_start, info} ->
        trace_loop(reclaimed + heap_words(info))

      {:trace, _pid, :gc_minor_end, info} ->
        trace_loop(reclaimed - heap_words(info))

      {:trace, _pid, :gc_major_end, info} ->
        trace_loop(reclaimed - heap_words(info))

      {:done, from} ->
        send(from, {:reclaimed, reclaimed})
    end
  end

  defp heap_words(info) do
    Keyword.get(info, :heap_size, 0) +
      Keyword.get(info, :old_heap_size, 0) +
      Keyword.get(info, :mbuf_size, 0)
  end

  defp binary_alloc_calls do
    case :erlang.system_info({:allocator, :binary_alloc}) do
      false ->
        0

      instances when is_list(instances) ->
        Enum.reduce(instances, 0, fn {:instance, _id, props}, acc ->
          calls = Keyword.get(props, :calls, [])

          case List.keyfind(calls, :binary_alloc, 0) do
            {:binary_alloc, giga, units} -> acc + giga * 1_000_000_000 + units
            _ -> acc
          end
        end)
    end
  end

  defp write_budget(path, measured) do
    header = """
    # Pinned per-iteration allocation budgets for the Elixir variable-form table
    # corpus (docs/PORTING.md I1, I14; issue #420).
    #
    # One iteration per carried instance row is:
    #   1. <Unit>.Table.load/2      (wire load)
    #   2. <Unit>.Table.save/2      (wire save)
    #   3. <Unit>.Table.from_json/2 (text read, when the instance carries text)
    #   4. <Unit>.Table.to_json/2   (text write, when the instance carries text)
    #
    # Columns: <case> <heap_words> <binary_calls> <reductions>
    # Written by `make tables-elixir-alloc-pin` under the pinned OTP/Elixir only;
    # checked by `make tables-elixir-alloc-audit`.
    """

    body =
      Enum.map_join(measured, "\n", fn {name, w, b, r} ->
        "#{String.pad_trailing(name, 24)} #{pad(w, 9)} #{pad(b, 4)} #{pad(r, 9)}"
      end) <> "\n"

    File.write!(path, header <> body)
  end

  defp read_budget(path) do
    if not File.exists?(path) do
      IO.puts(:stderr, "FAILED: missing budget file #{path} (run `make tables-elixir-alloc-pin`)")
      System.halt(1)
    end

    path
    |> File.read!()
    |> String.split("\n")
    |> Enum.map(&String.trim/1)
    |> Enum.reject(fn line -> line == "" or String.starts_with?(line, "#") end)
    |> Map.new(fn line ->
      [name, w, b, r] = String.split(line, ~r/[ \t]+/)
      {name, {String.to_integer(w), String.to_integer(b), String.to_integer(r)}}
    end)
  end

  defp check_budget(path, measured) do
    budgets = read_budget(path)

    IO.puts(
      "elixir allocation audit (#{@iterations} iterations/case, #{length(measured)} cases):"
    )

    failures =
      Enum.flat_map(measured, fn {name, w, b, r} ->
        case Map.get(budgets, name) do
          nil ->
            IO.puts(
              "  #{String.pad_trailing(name, 24)} words=#{pad(w, 9)}  bin=#{pad(b, 4)}  reds=#{pad(r, 9)}  unpinned"
            )

            []

          {want_w, want_b, want_r} ->
            w_limit = want_w + word_slack(want_w)
            b_limit = want_b + @binary_slack

            over =
              Enum.reject(
                [
                  if(w > w_limit,
                    do: "heap words #{w} > budget #{want_w} (+#{word_slack(want_w)})"
                  ),
                  if(b > b_limit,
                    do: "binary calls #{b} > budget #{want_b} (+#{@binary_slack})"
                  )
                ],
                &is_nil/1
              )

            if over == [] do
              IO.puts(
                "  #{String.pad_trailing(name, 24)} words=#{pad(w, 9)}/#{pad(want_w, 9)}  " <>
                  "bin=#{pad(b, 4)}/#{pad(want_b, 4)}  reds=#{pad(r, 9)}/#{pad(want_r, 9)}  ok"
              )

              []
            else
              reason = Enum.join(over, "; ")

              IO.puts(
                "  #{String.pad_trailing(name, 24)} words=#{pad(w, 9)}/#{pad(want_w, 9)}  " <>
                  "bin=#{pad(b, 4)}/#{pad(want_b, 4)}  reds=#{pad(r, 9)}/#{pad(want_r, 9)}  OVER BUDGET: #{reason}"
              )

              ["#{name}: #{reason}"]
            end
        end
      end)

    if failures != [] do
      IO.puts(
        :stderr,
        "ELIXIR ALLOCATION AUDIT FAILED (#{length(failures)} case(s) over budget):\n  " <>
          Enum.join(failures, "\n  ")
      )

      System.halt(1)
    end

    IO.puts("elixir allocation audit: all #{length(measured)} cases within pinned budget")
  end

  defp pad(n, width), do: String.pad_leading(Integer.to_string(n), width)
end

ElixirAllocAudit.main(System.argv())
