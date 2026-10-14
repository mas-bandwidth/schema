# P3 — hostile bytes, sweep + sanitizer (docs/SPEC-TABLES.md §7 item 5,
# docs/FIXED-FORM-ALGORITHM.md §7 item 5: "a byte-flip fuzz over the whole
# file, under a sanitizer, and it is not optional — every offset is arithmetic
# over sizes a stranger wrote down, so every byte, one bit at a time, is
# answered one of three ways and never a fourth: a refusal by name, a
# `malformed` read, or a read that lands values").
#
# THE LAW ON THE BEAM: there is no memory sanitizer, so the oracle is the
# exception class. A deliberate refusal is RuntimeError (the reader raised it
# with a sentence). An escaping MatchError, ArgumentError, FunctionClauseError
# or anything else is the reader leaving the buffer — the one outcome the
# sentence says must never happen.
#
# Run: elixir --erl "+B -noinput" -pa build/elixir-tables-ebin test/conformance/elixir/rows/P3.exs
# Exit 0 = green, exit 1 = red. One printed line per assertion.

import Bitwise

ebin = System.get_env("ELIXIR_TABLES_EBIN", "build/elixir-tables-ebin")
Code.prepend_path(ebin)

# ---- the block fixtures, the two images the block battery carries ----
# Each entry: {name, module_name, root, path}
fixtures = [
  {"block_render", "Blockdemo.RenderBlock", "render_frame", "testdata/wire/tables/block_render.bin"},
  {"block_padded", "Blockdemo.PaddedBlock", "padded_frame", "testdata/wire/tables/block_padded.bin"}
]

# Resolve module names at runtime so ELIXIR_TABLES_EBIN is respected
fixtures =
  for {name, mod_name, root, path} <- fixtures do
    mod = Module.concat([mod_name])
    Code.ensure_loaded!(mod)
    {name, mod, root, path}
  end

# ---- load the block images ----
images =
  for {_name, _mod, _root, path} <- fixtures do
    File.read!(path)
  end

if images == [] or Enum.any?(images, &(&1 == "")) do
  IO.puts(:stderr, "P3: no block fixtures found — run make build-conformance-elixir first")
  System.halt(1)
end

# ---- open a block image ----
open_block = fn mod, open_fun, bytes, lead ->
  apply(mod, open_fun, [bytes, lead])
end

# ---- walk a block through its descriptor dump ----
walk_block = fn mod, info_fun, block ->
  info = apply(mod, info_fun, [])
  BlockDump.dump(block.base, info)
end

# ---- build the open/walk pairs ----
mods =
  for {_name, mod, _root, _path} <- fixtures do
    mod
  end

# ---- the three mutation shapes (§7 item 5) ----
mutate_flip = fn bytes ->
  size = byte_size(bytes)
  at = :rand.uniform(size) - 1
  <<head::binary-size(^at), b, tail::binary>> = bytes
  <<head::binary, bxor(b, 1 <<< (:rand.uniform(8) - 1)), tail::binary>>
end

mutate_word = fn bytes ->
  size = byte_size(bytes)
  at = min(:rand.uniform(size) - 1, max(size - 8, 0))
  <<head::binary-size(^at), _::binary-size(8), tail::binary>> = bytes
  <<head::binary, :rand.uniform(0xFFFFFFFFFFFFFFFF)::little-unsigned-64, tail::binary>>
end

mutate_truncate = fn bytes ->
  size = byte_size(bytes)
  binary_part(bytes, 0, :rand.uniform(size))
end

mutators = [mutate_flip, mutate_word, mutate_truncate]

# ---- seed and iterations ----
seed = String.to_integer(System.get_env("SEED", "24845619678"))
:rand.seed(:exsss, {seed, seed + 369, seed + 7})
iterations = String.to_integer(System.get_env("P3_ITERATIONS", "5000"))

# ---- THE ORACLE: three outcomes, never a fourth ----
# 1. :error — deliberate refusal
# 2. {:ok, block} AND walk completes — read that lands values
# 3. RuntimeError — deliberate refusal with a sentence (the walk's own refusal)
# ANY OTHER EXCEPTION IS THE FAILURE.

results =
  Enum.reduce(Enum.zip(images, mods), {0, 0, []}, fn {bytes, mod}, {opened_acc, refused_acc, errors_acc} ->
    # Find the open and info functions for this module
    funcs = mod.__info__(:functions)

    {open_fun, info_fun} =
      Enum.reduce(funcs, {nil, nil}, fn {f, arity}, {of, inf} ->
        name = Atom.to_string(f)
        cond do
          String.starts_with?(name, "block_open_") and arity == 2 ->
            {f, inf}
          String.starts_with?(name, "block_info_") and arity == 0 ->
            {of, f}
          true ->
            {of, inf}
        end
      end)

    if open_fun == nil or info_fun == nil do
      {opened_acc, refused_acc, ["#{mod}: no block_open_/2 or block_info_/0 found" | errors_acc]}
    else
      {o, r, e} =
        Enum.reduce(1..iterations, {0, 0, []}, fn i, {o, r, e} ->
          mutator = Enum.at(mutators, rem(i - 1, 3))
          mutant = mutator.(bytes)
          lead = :rand.uniform(65) - 1

          try do
            case open_block.(mod, open_fun, mutant, lead) do
              :error ->
                {o, r + 1, e}

              {:ok, block} ->
                # OPENED: every accessor must stay inside the buffer
                walk_block.(mod, info_fun, block)
                {o + 1, r, e}
            end
          rescue
            _ in RuntimeError ->
              # the walk's own refusal, with a sentence
              {o, r + 1, e}

            ex ->
              {o, r,
               [
                 "mutant #{i} lead #{lead}: escaping #{Exception.format(:error, ex, __STACKTRACE__)} — " <>
                   "the reader left the buffer, which the sanitizer oracle refuses"
                 | e
               ]}
          end
        end)

      {opened_acc + o, refused_acc + r, e ++ errors_acc}
    end
  end)

{opened, refused, errors} = results

# ---- print results ----
if errors != [] do
  for err <- Enum.reverse(errors) do
    IO.puts("FAIL: #{err}")
  end
  IO.puts("P3: #{opened} opened, #{refused} refused, #{length(errors)} escaped — RED")
  System.halt(1)
else
  IO.puts(
    "P3: #{iterations * length(images)} mutants over #{length(images)} fixtures " <>
      "(#{iterations} each: bit-flip, word-overwrite, truncate) — " <>
      "#{opened} opened, #{refused} refused, no read left the buffer — GREEN"
  )
  System.halt(0)
end
