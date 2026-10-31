# THE ELIXIR TABLES SOAK (docs/PORTING.md I9).
#
# usage: elixir -pa <ebin> test/elixir-soak/main.exs \
#          <manifest> <pinned OTP> <pinned Elixir> <passes> [<budget file>]
#
# THE LENGTH IS A PASS COUNT, NEVER A CLOCK: the count is what the gate reads
# across, a count runs the same loop on every machine, and `make shape-gate`
# refuses a timing primitive outside the sanctioned bench. ONE PASS is one
# iteration of every pinned case of the allocation audit (test/elixir-alloc/
# main.exs): one wire load, one wire save, one text read and one text write of
# the instance through its <Unit>.Table codec, the wire half alone for a
# no-text instance, each held to the reference outputs taken once at the start.
#
# THE GATE IS THE ALLOCATION COUNT, RE-MEASURED EVERY SAMPLE. The passes are cut
# into samples of 200, the audit's own window: in a sample every case runs 200
# times back to back and the audit's two counters are read over that window in
# ONE long-lived process: heap words (the words this process's collections
# reclaimed, traced with :erlang.trace/3 on :garbage_collection, plus the live
# change across the window) and binary calls (binary_alloc's VM-wide call
# counter). The window is the audit's because a collection's heap resizing is
# a cost per window, so a figure averaged over another length is a different
# measurement than the budget. Every case's per-pass figure must sit inside its
# pinned budget in test/conformance/elixir/alloc-budget.txt with the audit's
# own slack, at every sample: a path that allocates and frees the same bytes
# every pass reds at the first sample and at every one after it, naming the
# case. ELIXIR_SOAK_CASES, a comma-separated list of case names, narrows the
# soak to those cases (the negative control's short run).
#
# DRIFT IS THE WEAKER INSTRUMENT, KEPT BESIDE IT: the live heap words after a
# full collection and the VM's binary memory, read at the first sample and at
# the last. It sees retention, and it reads +0 forever for a matched
# allocate-and-free, which is why it does not stand alone.
#
# THE PIN COMES FIRST, as it does in the audit: the budget is pinned on one
# runtime, so the leg's runtime pin runs before anything is measured, and
# SCHEMA_ELIXIR_ANY_OTP=1 prints the figures under NOT CERTIFIED and exits 0.
runtime_pin = fn [_manifest, otp_want, elixir_want | _] = argv ->
  System.argv([otp_want, elixir_want])
  Code.eval_file("test/elixir/runtime_pin.exs")
  System.argv(argv)
end

runtime_pin.(System.argv())

defmodule Soak do
  # a sample is the audit's own window: every case run this many times back to
  # back, the length its pinned per-iteration budget is averaged over, so a
  # sample's figure and the budget are the same measurement
  @window 200
  # the audit's slack per case and per iteration (test/elixir-alloc/main.exs)
  @heap_slack 64
  @binary_slack 8
  # retention the drift line tolerates: the collector's own bookkeeping, far
  # under one retained instance of the corpus
  @live_drift_words 4096
  @binary_drift_bytes 1_048_576

  def main([manifest, otp_want, elixir_want, passes | rest]) do
    budget_path = List.first(rest) || "test/conformance/elixir/alloc-budget.txt"
    any_otp = System.get_env("SCHEMA_ELIXIR_ANY_OTP") == "1"
    ebin = System.get_env("ELIXIR_TABLES_EBIN") || "build/elixir-tables-ebin"
    Code.prepend_path(ebin)
    passes = String.to_integer(passes)

    if passes < @window do
      IO.puts("SOAK FAILED: #{passes} passes cannot fill one sample of #{@window}")
      System.halt(1)
    end

    budget = read_budget(budget_path)
    cases = cases(parse(File.read!(manifest)), budget)

    cases =
      case System.get_env("ELIXIR_SOAK_CASES") do
        nil -> cases
        "" -> cases
        names -> Enum.filter(cases, &(&1.name in String.split(names, ",")))
      end

    if cases == [] do
      IO.puts(
        "SOAK FAILED: no pinned case has a <Unit>.Table codec in #{manifest} (ebin #{ebin})"
      )

      System.halt(1)
    end

    samples = div(passes, @window)
    lead = passes - samples * @window

    IO.puts(
      "soak: #{passes} passes over #{length(cases)} pinned cases, #{samples} samples of " <>
        "#{@window} passes, each case held to its pinned budget at every sample"
    )

    parent = self()
    {pid, mon} =
      spawn_monitor(fn -> send(parent, {:soaked, self(), run(parent, cases, lead, window)}) end)

    {samples, ran, drift} = collect_samples(pid, mon, [])

    by_name = Map.new(cases, &{&1.name, &1})

    over =
      for {i, figures} <- samples do
        cols =
          for {name, {w, b}} <- figures,
              c <- [Map.fetch!(by_name, name)],
              w > c.words + @heap_slack + div(c.words, 10_000) or b > c.binary + @binary_slack do
            "#{c.name} #{w} words (pinned #{c.words}), #{b} binary calls (pinned #{c.binary})"
          end

        total_w = Enum.sum(for {_c, {w, _b}} <- figures, do: w)
        total_b = Enum.sum(for {_c, {_w, b}} <- figures, do: b)

        IO.puts(
          "soak: sample #{i}: #{total_w} heap words, #{total_b} binary calls per pass, " <>
            "#{length(cols)} cases over their pinned budget"
        )

        if cols != [] do
          IO.puts(
            "SOAK FAILED: sample #{i}: #{length(cols)} cases allocate more per pass than " <>
              "their pinned budget, first #{hd(cols)}"
          )
        end

        cols
      end
      |> Enum.reject(&(&1 == []))

    IO.puts("soak: worker ran #{ran} passes, driver took #{length(samples)} samples")

    {live_drift, binary_drift} = drift
    drift_ok = live_drift <= @live_drift_words and binary_drift <= @binary_drift_bytes

    IO.puts(
      "soak: drift #{live_drift} live heap words, #{binary_drift} binary bytes " <>
        if(drift_ok, do: "[PASS]", else: "[FAIL]")
    )

    cond do
      any_otp ->
        IO.puts(
          "soak: NOT CERTIFIED — read under SCHEMA_ELIXIR_ANY_OTP=1 (the pin wants OTP " <>
            "#{otp_want}, Elixir #{elixir_want}); #{length(over)} samples over, and nothing is certified"
        )

      over != [] ->
        System.halt(1)

      not drift_ok ->
        IO.puts("SOAK FAILED: the live heap or the binary memory drifted past its bound")
        System.halt(1)

      true ->
        IO.puts(
          "soak: every sample is inside the pinned allocation budget on OTP #{otp_want} " <>
            "with Elixir #{elixir_want}"
        )
    end
  end

  def main(_) do
    IO.puts("usage: main.exs <manifest> <pinned OTP> <pinned Elixir> <passes> [<budget file>]")

    System.halt(2)
  end

  # the samples as the soaking process sends them, in order, then its end
  defp collect_samples(pid, mon, acc) do
    receive do
      {:sample, ^pid, i, figures} ->
        collect_samples(pid, mon, [{i, figures} | acc])

      {:soaked, ^pid, {ran, drift}} ->
        Process.demonitor(mon, [:flush])
        {Enum.reverse(acc), ran, drift}

      {:DOWN, ^mon, :process, ^pid, reason} ->
        IO.puts("SOAK FAILED: the soaking process died: #{inspect(reason)}")
        System.halt(1)
    end
  end

  # ---- the soaking process ----

  defp run(parent, cases, lead, samples) do
    cases = Enum.map(cases, &Map.put(&1, :ref, reference(&1)))
    pass(cases)
    spin(cases, lead)
    :erlang.garbage_collect()
    live_first = used()
    binary_first = :erlang.memory(:binary)

    # each sample's figures go to the driver as they are taken, so nothing the
    # soak itself keeps grows the live heap the drift line reads
    for i <- 1..samples do
      send(parent, {:sample, self(), i, Enum.map(cases, &{&1.name, window(&1)})})
    end

    :erlang.garbage_collect()
    drift = {used() - live_first, :erlang.memory(:binary) - binary_first}
    {lead + samples * @window, drift}
  end

  # ONE CASE'S WINDOW in a sample: @window iterations back to back, measured
  # on their own exactly as the audit measures them, so the count names the
  # case that moved
  defp window(c) do
    binary0 = binary_calls()
    :erlang.garbage_collect()
    live0 = used()
    reclaimed = traced(fn -> repeat(c, @window) end)
    live1 = used()
    binary1 = binary_calls()
    {div(reclaimed + (live1 - live0), @window), div(binary1 - binary0, @window)}
  end

  defp repeat(_c, 0), do: :ok

  defp repeat(c, n) do
    once(c)
    repeat(c, n - 1)
  end

  defp spin(_cases, 0), do: :ok

  defp spin(cases, n) do
    pass(cases)
    spin(cases, n - 1)
  end

  defp pass(cases), do: Enum.each(cases, &once/1)

  # ---- the corpus, as the audit reads it ----

  defp parse(text) do
    text
    |> String.split("\n")
    |> Enum.map(&String.trim/1)
    |> Enum.reject(fn line -> line == "" or String.starts_with?(line, "#") end)
    |> Enum.map(&String.split(&1, ~r/[ \t]+/))
    |> Enum.filter(&(hd(&1) == "instance"))
    |> Enum.map(&tl/1)
  end

  # every pinned `instance` whose unit has a <Unit>.Table module that loads
  # and saves it back; an unpinned case has no budget to hold a sample to
  defp cases(rows, budget) do
    Enum.flat_map(rows, fn [name, unit, root, wire | flags] ->
      mod = Module.concat([Macro.camelize(unit), "Table"])

      with {:ok, {words, binary}} <- Map.fetch(budget, name),
           true <-
             Code.ensure_loaded?(mod) and function_exported?(mod, :load, 2) and
               function_exported?(mod, :save, 2),
           bytes = File.read!(wire),
           {:ok, value, _} <- mod.load(root, bytes),
           {:ok, _saved} <- mod.save(root, value) do
        json = Path.join("testdata/conformance/tables/json", name <> ".json")
        text = if flags == [] and File.exists?(json), do: File.read!(json), else: nil

        [
          %{
            name: name,
            mod: mod,
            root: root,
            wire: bytes,
            text: text,
            words: words,
            binary: binary
          }
        ]
      else
        _ -> []
      end
    end)
  end

  # the reference outputs, taken once: every pass must reproduce them, so a
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

  defp once(%{ref: ref} = c) do
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

  # ---- the counters, the audit's own ----

  defp used do
    {:garbage_collection_info, info} = :erlang.process_info(self(), :garbage_collection_info)
    heap_of(info)
  end

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

  defp read_budget(path) do
    case File.read(path) do
      {:ok, text} ->
        text
        |> String.split("\n")
        |> Enum.map(&String.trim/1)
        |> Enum.reject(fn l -> l == "" or String.starts_with?(l, "#") end)
        |> Map.new(fn l ->
          [name, w, b, _r] = String.split(l, ~r/[ \t]+/)
          {name, {String.to_integer(w), String.to_integer(b)}}
        end)

      {:error, reason} ->
        IO.puts("SOAK FAILED: cannot read the budget #{path}: #{inspect(reason)}")
        System.halt(1)
    end
  end
end

Soak.main(System.argv())
