# schema tables bench — the Elixir leg's entry point for THE VARIABLE FORM
# (docs/SPEC-TABLES.md §3, form byte 1). Its runner is form1_runner.exs; this
# file only loads the generated modules, for the reason main.exs gives (an
# `.exs` compiles as a whole before it runs).
#
# The board's leg script (bench/tables/elixir/leg) precompiles the generated
# unit into build/schema_tables_bench_elixir/ebin, the form the BEAM ships, and
# puts it on the code path. When it is not there the unit is compiled here in
# one parallel pass, so a bare `elixir bench/tables/elixir/form1_main.exs` also
# runs from the repository root.
gen =
  case System.get_env("SCHEMA_ELIXIR_TABLES_GEN_DIR") do
    nil -> Path.join(__DIR__, "../../../generated/bench/tables/elixir")
    "" -> Path.join(__DIR__, "../../../generated/bench/tables/elixir")
    dir -> dir
  end

if not Code.ensure_loaded?(Benchtable.Table) do
  if not File.dir?(gen) do
    IO.write(:stderr, "missing #{gen} — run `make generate`\n")
    System.halt(1)
  end

  files = gen |> Path.join("*.ex") |> Path.wildcard() |> Enum.sort()

  case Kernel.ParallelCompiler.compile(files, return_diagnostics: true) do
    {:ok, _modules, _diagnostics} ->
      :ok

    {:error, errors, _diagnostics} ->
      IO.write(:stderr, "the generated Elixir did not compile: #{inspect(errors)}\n")
      System.halt(1)
  end
end

Code.require_file("form1_runner.exs", __DIR__)
