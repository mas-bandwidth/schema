# schema tables bench — the Elixir leg's entry point, for THE FIXED FORM
# (docs/SPEC-TABLES.md §3.4, form byte 3).
#
# It only loads. An `.exs` compiles as a whole before it runs, so the generated
# modules have to be required from a file the runner is required AFTER — the
# same split bench/elixir/main.exs makes for the packet leg, and for the same
# reason.
#
# THE GENERATED MODULES COME FROM THE PAIRED UNIT, which is the bench corpus's
# two schema files compiled together. The second is generated under another
# basename (`Wrap.schema`) because a declaration whose name is its own file's
# basename collides with the module this backend writes for that file (§11) —
# that is the checker working, and no declaration moves, so no id and no layout
# byte moves either. bench/paired/main.go's build stage makes the copy;
# make/elixir.mk's `tables-elixir-fixed-bench` makes the same one.
#
# The paired driver spawns it from the REPOSITORY ROOT:
#
#   PATH="<dist otp>:<dist elixir>:$PATH" elixir bench/tables/elixir/main.exs \
#       --indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus \
#       [--csv] [--gate] [--round K] [--iterations N]
gen = Path.join(__DIR__, "../../../generated/bench/paired/elixir")

if not File.dir?(gen) do
  IO.write(
    :stderr,
    "missing #{gen} — run `go run ./bench/paired -mode build -langs elixir`\n"
  )

  System.halt(1)
end

# ONE PARALLEL COMPILE, not a required order. The generated modules reference
# each other at COMPILE time — a struct default of another module's struct, a
# runtime constant in a default argument — so the dependency order is the
# emitter's business and not this file's, and Kernel.ParallelCompiler is what
# resolves it. Requiring them one by one would encode an order that a new
# emitted module could silently invalidate.
#
# THE RUNTIME MODULE GOES FIRST, AND IT IS THE ONE EXCEPTION. Every fixed root
# carries an `@on_load` that builds its lineage's plans out of the lock's bytes
# (internal/codegen/elixirtable/fixedelixir.go), and that hook calls the
# runtime. `@on_load` runs at LOAD time, not at compile time, so the parallel
# compiler cannot see the edge: it is free to load a root before it has
# compiled FixedRuntime, and the hook then dies with an UndefinedFunctionError
# and the root never loads at all. Compiling the runtime by itself first is the
# whole of the ordering this file encodes — it depends on no generated module,
# so there is nothing for a new emitted module to invalidate.
compile = fn files ->
  case Kernel.ParallelCompiler.compile(files, return_diagnostics: true) do
    {:ok, _modules, _diagnostics} ->
      :ok

    {:error, errors, _diagnostics} ->
      IO.write(:stderr, "the generated Elixir did not compile: #{inspect(errors)}\n")
      System.halt(1)
  end
end

{runtime, rest} =
  gen
  |> Path.join("*.ex")
  |> Path.wildcard()
  |> Enum.sort()
  |> Enum.split_with(&(Path.basename(&1) == "FixedRuntime.ex"))

compile.(runtime)
compile.(rest)

Code.require_file("runner.exs", __DIR__)
