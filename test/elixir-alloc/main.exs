# THE ELIXIR LEG'S ALLOCATION AUDIT (docs/PORTING.md I1 and I14, issue #420).
#
# usage: elixir -pa <ebin> test/elixir-alloc/main.exs \
#          <manifest> <pinned OTP> <pinned Elixir> audit|pin [<budget file>]
#
# THE PIN COMES FIRST. One major's JIT can allocate where another does not, so
# a figure measured on whatever runtime a PATH lookup found says nothing about
# the runtime the claim is for. The first act of this script is the leg's own
# runtime pin (test/elixir/runtime_pin.exs) with the two versions make/elixir.mk
# compares against: off them it prints FAILED and halts before a single module
# below is even compiled, so nothing is measured. SCHEMA_ELIXIR_ANY_OTP=1 is the
# escape hatch: the numbers are printed under NOT CERTIFIED and the audit exits
# 0, and `pin` refuses to write a budget at all.
runtime_pin = fn [_manifest, otp_want, elixir_want | _] = argv ->
  System.argv([otp_want, elixir_want])
  Code.eval_file("test/elixir/runtime_pin.exs")
  System.argv(argv)
end

runtime_pin.(System.argv())

# ---------------------------------------------------------------------------
# The instrument, because the number is worth what the instrument is.
#
# ONE ITERATION is one wire load, one wire save, one text read and one text
# write of an instance — the calls the conformance driver's wire, json-read and
# json-write surfaces make (test/conformance/elixir/driver_impl.ex) on
# <Unit>.Table. A `no-text` instance has no text form, so its iteration is the
# wire half alone, and the output says so.
#
# THREE COUNTERS:
#
#   HEAP WORDS   per process. Each case runs in a FRESH process, traced with
#                :erlang.trace/3 on :garbage_collection: the words its
#                collections reclaimed between two full collections, plus what
#                is still live at the end, is exactly what the loop allocated —
#                and nothing another process does can reach it. Gated at 64
#                words plus one in ten thousand of the pinned figure: the
#                collector's own jitter with room, far under what the negative
#                control adds (a thousand-cell list, two thousand words).
#   BINARY CALLS :erlang.system_info({:allocator, :binary_alloc}), the refc
#                binary's own cumulative call counter. A payload over 64 bytes
#                lives off the process heap and is counted here and nowhere
#                else. It is VM-WIDE (there is no per-process one), so it
#                carries a stated slack of 8 calls per iteration for another
#                process's handful, under the control's sixteen.
#   REDUCTIONS   the scheduler's own accounting, PRINTED AND PINNED, NOT GATED:
#                a different build counts them differently, and the two columns
#                above are the allocation claim.
#
# THE FLOOR IS NOT ZERO AND IS NOT CLAIMED TO BE. A decoded BEAM term is an
# allocation and no buffer is caller-owned. What the gate holds is that the
# figure does not MOVE: the budget is pinned per case in
# test/conformance/elixir/alloc-budget.txt by `make tables-elixir-alloc-pin`
# and re-pinned deliberately, exactly as a wire golden is. A case with no
# pinned line is listed `unpinned` and does not fail, so a unit another card
# adds does not turn this gate red; a pinned case the leg no longer measures
# DOES fail, because that is a budget nobody checks.
defmodule AllocAudit do
  # the budget is per iteration over THIS many: a fixed per-case cost amortizes
  # over it, so it is a constant of the pinned file and not a knob
  @iterations 200
  @pin_runs 5
  @heap_slack 64
  @binary_slack 8

  def main([manifest, otp_want, elixir_want, mode | rest]) do
    budget_path = List.first(rest) || "test/conformance/elixir/alloc-budget.txt"
    any_otp = System.get_env("SCHEMA_ELIXIR_ANY_OTP") == "1"
    ebin = System.get_env("ELIXIR_TABLES_EBIN") || "build/elixir-tables-ebin"
    Code.prepend_path(ebin)

    {cases, skipped} = cases(parse(File.read!(manifest)))

    Enum.each(skipped, fn {name, why} ->
      IO.puts("  #{String.pad_trailing(name, 24)} not measured: #{why}")
    end)

    if cases == [] do
      IO.puts(
        "FAILED: alloc-audit found no instance with a <Unit>.Table codec in #{manifest} (ebin #{ebin})"
      )

      System.halt(1)
    end

    # THE PIN TAKES THE TOP OF THE JITTER. One measurement of a small case
    # moves by about fifty words between runs of the same code on the same
    # runtime (chain_value read 2367 and 2417 under two pins), so a budget
    # pinned off one run can sit at the bottom of that spread and leave the
    # audit's 64-word slack nearly spent. The pin measures every case
    # #{@pin_runs} times and keeps the largest figure per column; the audit
    # measures once against it.
    runs = if mode == "pin", do: @pin_runs, else: 1

    IO.puts(
      "alloc-audit: measuring #{length(cases)} cases, #{@iterations} iterations each" <>
        if(runs > 1, do: ", #{runs} runs per case (the pin keeps the largest)", else: "")
    )

    measured = Enum.map(cases, fn c -> {c, top(for _ <- 1..runs, do: measure(c))} end)

    case mode do
      "pin" -> pin(measured, budget_path, any_otp, otp_want, elixir_want)
      "audit" -> audit(measured, budget_path, any_otp, otp_want, elixir_want)
    end
  end

  def main(_) do
    IO.puts("usage: main.exs <manifest> <pinned OTP> <pinned Elixir> audit|pin [<budget file>]")
    System.halt(2)
  end

  # ---- the corpus ----

  defp parse(text) do
    text
    |> String.split("\n")
    |> Enum.map(&String.trim/1)
    |> Enum.reject(fn line -> line == "" or String.starts_with?(line, "#") end)
    |> Enum.map(&String.split(&1, ~r/[ \t]+/))
    |> Enum.filter(&(hd(&1) == "instance"))
    |> Enum.map(&tl/1)
  end

  # every `instance` whose unit has a <Unit>.Table module at the moment this
  # runs, and whose root that codec loads and saves back (a blob two slots name
  # refuses the save, docs/SPEC-TABLES.md §2.5); the rest are named, not measured
  defp cases(rows) do
    Enum.reduce(rows, {[], []}, fn [name, unit, root, wire | flags], {cs, sk} ->
      mod = Module.concat([Macro.camelize(unit), "Table"])

      if Code.ensure_loaded?(mod) and function_exported?(mod, :load, 2) and
           function_exported?(mod, :save, 2) do
        bytes = File.read!(wire)
        json = Path.join("testdata/conformance/tables/json", name <> ".json")
        text = if flags == [] and File.exists?(json), do: File.read!(json), else: nil

        with {:ok, value, _report} <- mod.load(root, bytes),
             {:ok, _saved} <- mod.save(root, value) do
          c = %{name: name, mod: mod, root: root, wire: bytes, text: text}
          {cs ++ [c], sk}
        else
          other ->
            {cs,
             sk ++
               [
                 {name,
                  "#{inspect(mod)} does not carry #{root} (load or save answers #{inspect(elem(other, 1))})"}
               ]}
        end
      else
        {cs, sk}
      end
    end)
  end

  # ---- one iteration, and the reference it is held to ----

  # the reference outputs, taken once: every iteration must reproduce them, so a
  # run that drifted stops rather than measuring something else
  defp reference(c) do
    {:ok, value, _} = c.mod.load(c.root, c.wire)
    {:ok, saved} = c.mod.save(c.root, value)

    case c.text do
      nil ->
        %{saved: saved, from_text: nil, text_out: nil}

      text ->
        {:ok, from_text, _} = c.mod.from_json(c.root, text)
        {:ok, from_text_saved} = c.mod.save(c.root, from_text)
        {:ok, text_out} = c.mod.to_json(c.root, value)
        %{saved: saved, from_text: from_text_saved, text_out: text_out}
    end
  end

  defp once(c, ref) do
    {:ok, value, _} = c.mod.load(c.root, c.wire)
    {:ok, saved} = c.mod.save(c.root, value)
    if saved != ref.saved, do: raise("#{c.name}: the wire round trip drifted")

    if c.text != nil do
      {:ok, from_text, _} = c.mod.from_json(c.root, c.text)
      {:ok, from_text_saved} = c.mod.save(c.root, from_text)
      if from_text_saved != ref.from_text, do: raise("#{c.name}: the text read drifted")
      {:ok, text_out} = c.mod.to_json(c.root, value)
      if text_out != ref.text_out, do: raise("#{c.name}: the text write drifted")
    end

    :ok
  end

  defp spin(_c, _ref, 0), do: :ok

  defp spin(c, ref, n),
    do:
      (
        once(c, ref)
        spin(c, ref, n - 1)
      )

  # ---- the three counters ----

  # ONE case, in a fresh process, so its heap is nobody else's
  defp measure(c) do
    parent = self()

    {pid, mon} =
      spawn_monitor(fn ->
        ref = reference(c)
        once(c, ref)
        # binary_calls/0 builds one {:instance, id, stats} term per allocator
        # instance (one per scheduler) on THIS heap: read it before the
        # collection that opens the window and after live1 closes it, so the
        # heap-word figure does not carry a scheduler-count-dependent term
        binary0 = binary_calls()
        :erlang.garbage_collect()
        live0 = used()
        {:reductions, r0} = :erlang.process_info(self(), :reductions)
        reclaimed = traced(fn -> spin(c, ref, @iterations) end)
        {:reductions, r1} = :erlang.process_info(self(), :reductions)
        live1 = used()
        binary1 = binary_calls()

        send(
          parent,
          {:measured, self(),
           {div(reclaimed + (live1 - live0), @iterations), div(binary1 - binary0, @iterations),
            div(r1 - r0, @iterations)}}
        )
      end)

    receive do
      {:measured, ^pid, figures} ->
        Process.demonitor(mon, [:flush])
        figures

      {:DOWN, ^mon, :process, ^pid, reason} ->
        IO.puts("FAILED: #{c.name}: the measuring process died: #{inspect(reason)}")
        System.halt(1)
    end
  end

  # the heap's USED words, which after a full collection are exactly the live
  # terms and nothing about how the heap was sized
  defp used do
    {:garbage_collection_info, info} = :erlang.process_info(self(), :garbage_collection_info)
    heap_of(info)
  end

  # the heap words THIS process's collections reclaimed while `work` ran, the
  # full collection that closes the window included: a collection after the
  # trace stops would reclaim the last young generation and count none of it,
  # and where that boundary falls moves the figure by a whole generation
  defp traced(work) do
    me = self()
    tracer = spawn(fn -> collect(0, nil) end)
    :erlang.trace(me, true, [:garbage_collection, {:tracer, tracer}])

    try do
      work.()
      :erlang.garbage_collect()
    after
      :erlang.trace(me, false, [:garbage_collection])
    end

    send(tracer, {:reclaimed, me})

    receive do
      {:reclaimed, words} -> words
    end
  end

  defp collect(sum, before) do
    receive do
      {:trace, _pid, kind, info} when kind in [:gc_minor_start, :gc_major_start] ->
        collect(sum, heap_of(info))

      {:trace, _pid, kind, info} when kind in [:gc_minor_end, :gc_major_end] ->
        collect(sum + max((before || heap_of(info)) - heap_of(info), 0), nil)

      {:reclaimed, from} ->
        send(from, {:reclaimed, sum})
    end
  end

  defp heap_of(info) do
    Keyword.get(info, :heap_size, 0) + Keyword.get(info, :old_heap_size, 0) +
      Keyword.get(info, :mbuf_size, 0)
  end

  # binary_alloc's cumulative, VM-wide call counter, summed over its instances
  defp binary_calls do
    Enum.reduce(:erlang.system_info({:allocator, :binary_alloc}), 0, fn
      {:instance, _id, stats}, acc ->
        with {:calls, calls} <- List.keyfind(stats, :calls, 0),
             {:binary_alloc, giga, n} <- List.keyfind(calls, :binary_alloc, 0) do
          acc + giga * 1_000_000_000 + n
        else
          _ -> acc
        end

      _, acc ->
        acc
    end)
  end

  defp top(figures) do
    {Enum.max(for({w, _, _} <- figures, do: w)), Enum.max(for({_, b, _} <- figures, do: b)),
     Enum.max(for({_, _, r} <- figures, do: r))}
  end

  # ---- the two modes ----

  defp pin(measured, path, any_otp, otp_want, elixir_want) do
    if any_otp do
      IO.puts(
        "FAILED: alloc-pin refuses to write #{path} under SCHEMA_ELIXIR_ANY_OTP=1: a budget " <>
          "is pinned on OTP #{otp_want} with Elixir #{elixir_want} or not at all"
      )

      System.halt(1)
    end

    lines =
      Enum.map_join(measured, fn {c, {w, b, r}} ->
        print(c, {w, b, r}, "")
        "#{c.name} #{w} #{b} #{r}\n"
      end)

    File.write!(path, header(otp_want, elixir_want) <> lines)

    IO.puts(
      "alloc-pin: pinned #{length(measured)} budgets into #{path} on OTP #{otp_want} with Elixir #{elixir_want}"
    )
  end

  defp audit(measured, path, any_otp, otp_want, elixir_want) do
    budget = read_budget(path)
    names = MapSet.new(measured, fn {c, _} -> c.name end)

    verdicts =
      Enum.map(measured, fn {c, {w, b, r}} ->
        case Map.fetch(budget, c.name) do
          {:ok, {pw, pb, pr}} ->
            heap_ceiling = pw + @heap_slack + div(pw, 10_000)
            binary_ceiling = pb + @binary_slack

            over =
              Enum.reject(
                [
                  w > heap_ceiling and "heap words #{w} over #{pw} + #{heap_ceiling - pw}",
                  b > binary_ceiling and "binary calls #{b} over #{pb} + #{@binary_slack}"
                ],
                &(&1 == false)
              )

            print(c, {w, b, r}, "  (budget #{pw} words, #{pb} binary calls, #{pr} reductions)")
            {c.name, over}

          :error ->
            print(c, {w, b, r}, "  unpinned")
            {c.name, :unpinned}
        end
      end)

    over = for {name, cols} <- verdicts, is_list(cols), cols != [], do: {name, cols}
    unpinned = for {name, :unpinned} <- verdicts, do: name
    lost = budget |> Map.keys() |> Enum.reject(&MapSet.member?(names, &1)) |> Enum.sort()
    pinned = length(verdicts) - length(unpinned)

    Enum.each(over, fn {name, cols} ->
      IO.puts(
        "#{name}: over its pinned budget per iteration on " <>
          Enum.join(cols, ", ") <>
          " — the generated code allocates more than it did; re-pin deliberately " <>
          "(make tables-elixir-alloc-pin) or find what was added"
      )
    end)

    Enum.each(lost, fn name ->
      IO.puts(
        "#{name}: pinned in #{path} but not measured — the leg no longer carries it, or the corpus lost it"
      )
    end)

    if unpinned != [],
      do:
        IO.puts(
          "alloc-audit: #{length(unpinned)} unpinned (not gated): #{Enum.join(unpinned, " ")}"
        )

    cond do
      any_otp ->
        IO.puts(
          "alloc-audit: NOT CERTIFIED — the figures above were read under SCHEMA_ELIXIR_ANY_OTP=1, " <>
            "not on the pinned runtime alone (the pin wants OTP #{otp_want}, Elixir #{elixir_want}); " <>
            "#{length(over)} over, #{length(lost)} lost, and nothing is certified"
        )

      over == [] and lost == [] ->
        IO.puts(
          "alloc-audit: every case is inside its pinned budget (#{pinned} pinned, " <>
            "#{length(unpinned)} unpinned) on OTP #{otp_want} with Elixir #{elixir_want}"
        )

      true ->
        IO.puts(
          "FAILED: alloc-audit: #{length(over)} cases over budget, #{length(lost)} pinned cases not measured"
        )

        System.halt(1)
    end
  end

  defp print(c, {w, b, r}, tail) do
    shape = if c.text == nil, do: " (wire only: no text form)", else: ""

    IO.puts(
      "  #{String.pad_trailing(c.name, 24)} #{w} heap words  #{b} binary calls  " <>
        "#{r} reductions#{shape}#{tail}"
    )
  end

  defp read_budget(path) do
    case File.read(path) do
      {:ok, text} ->
        text
        |> String.split("\n")
        |> Enum.map(&String.trim/1)
        |> Enum.reject(fn l -> l == "" or String.starts_with?(l, "#") end)
        |> Map.new(fn l ->
          [name, w, b, r] = String.split(l, ~r/[ \t]+/)
          {name, {String.to_integer(w), String.to_integer(b), String.to_integer(r)}}
        end)

      {:error, reason} ->
        IO.puts(
          "FAILED: alloc-audit cannot read its budget #{path}: #{inspect(reason)}; make tables-elixir-alloc-pin writes it"
        )

        System.halt(1)
    end
  end

  defp header(otp_want, elixir_want) do
    """
    # THE ALLOCATION BUDGET of the Elixir table leg (docs/PORTING.md I1, I14),
    # pinned by `make tables-elixir-alloc-pin` on OTP #{otp_want} with Elixir #{elixir_want}
    # and read by `make tables-elixir-alloc-audit`. Re-pinning is deliberate, as a
    # wire golden is. Written by test/elixir-alloc/main.exs; do not hand-edit.
    #
    # <case> <heap words> <binary calls> <reductions>, all per iteration
    #
    # One iteration is one wire load, one wire save, one text read and one text
    # write of the instance through its <Unit>.Table codec (the wire half alone
    # for a no-text instance), averaged over 200; each figure is the largest of
    # five measurements. Heap words: per process, from :erlang.trace/3 on
    # :garbage_collection, gated at 64 + 1/10000 of the figure. Binary calls:
    # binary_alloc's VM-wide call counter, gated at + 8. Reductions: printed and
    # pinned, not gated. The floor is not zero and is not claimed to be.
    """
  end
end

AllocAudit.main(System.argv())
