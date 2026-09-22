# Card cell-elixir-r13 — the R13 cell of the elixir row group "plan-selection:
# Select known layouts and refuse unsupported input" (docs/roadmap.sexp).
#
# THE LAW (docs/FIXED-FORM-ALGORITHM.md:899, with its outcome table at :906):
#   REFUSE is total: no counter moves, nothing is decoded, and not one
#   destination byte is written — the prefill included. `malformed` is the
#   residue and not a bucket a named rule falls into.
# and the joint assertion every refusal asserts:
#   `refused` plus `reason` is one answer; `malformed` is the other; they are
#   NEVER both set, and the counters are a third thing again.
#
# On this leg the two answers are the reason atom and the report's `malformed`
# flag: a refusal by name comes back `{:error, name, report}` with
# `report.malformed == false`, and a malformed read comes back
# `{:error, :malformed, report}` with `report.malformed == true` — a set
# `malformed` beside a named reason, or a named reason on a malformed read, is
# the failure §5.3 row 8 names. The destination is the returned values term, so
# "not one destination byte written" is the SHAPE of every refusal here: a
# 3-tuple `{:error, _, _}` with no values in it, the prefill (`R.run`'s third
# argument) never reached.
#
# THE VECTORS are built from the law, not from a fixture — the conformance
# corpus (testdata/conformance/tables) carries no form-3 file. The header is 20
# bytes: the form byte 3, the seven reserved zeros, the file's hash (little
# endian 64) and the layout's byte length (little endian 32); the layout bytes
# follow, then records of one 8-byte per-record hash plus the body. The hash,
# the layout bytes and the body size are read from the generated module's own
# published constants (`root_config_fixed_hash/0`, `_layout/0`, `_body_bytes/0`)
# — a runtime never derives one (§5.3 step 4). Two rows of the law's table are
# not constructible from this corpus and are named here rather than faked:
# `layout_unsupported` (every table in the corpus has floor 0 and no retired
# hashes, so no lineage entry sits below a floor) and the `record_bytes <= 8`
# malformation (the smallest body in the corpus is 22 bytes, so no record is
# its bare hash).
#
# Run from the repository root:  elixir test/conformance/elixir/rows/R13.exs
# Exit 0 green, 1 red, one printed line per assertion. Depends on no other
# rows/ file and edits no shared file.

ebin = System.get_env("EBIN", "build/elixir-tables-ebin")
:code.add_path(String.to_charlist(ebin))

# The beams the Makefile's build-conformance-elixir compiled, loaded the way
# test/conformance/elixir/driver_impl.ex loads them — explicitly, because an
# .exs script runs with no application to load the beams for it.
mods =
  Path.wildcard(ebin <> "/*.beam")
  |> Enum.map(fn path ->
    mod = path |> Path.basename(".beam") |> String.to_atom()
    Code.ensure_loaded(mod)
    mod
  end)

# Dispatch is by SEARCH, not by a generated index (the driver's contract): the
# module whose reader this cell tests is whichever one exports the family read
# verdict for the corpus's RootConfig root.
root = "root_config"
want_load = String.to_atom(root <> "_fixed_load")
mod = Enum.find(mods, &function_exported?(&1, want_load, 1))

unless mod do
  IO.puts(
    :stderr,
    "FAIL: no module exports #{want_load}/1 — run make build-conformance-elixir"
  )

  System.halt(1)
end

hash = apply(mod, :"#{root}_fixed_hash", [])
layout = apply(mod, :"#{root}_fixed_layout", [])
body_bytes = apply(mod, :"#{root}_fixed_body_bytes", [])
known = apply(mod, :"#{root}_fixed_known", [])

# The 20-byte file header (§5.3 step 1): form 3, seven reserved zeros, the
# file's hash, the layout's length.
header = fn h, lay ->
  <<3, 0, 0, 0, 0, 0, 0, 0, h::little-unsigned-64, byte_size(lay)::little-unsigned-32>>
end

# One record: the per-record hash — THE FILE'S, or the read refuses :no_layout
# — over a zero body, which is a lawful RootConfig image.
record = fn h -> <<h::little-unsigned-64>> <> :binary.copy(<<0>>, body_bytes) end

load = fn data -> apply(mod, want_load, [data]) end

# Every counter stays at zero: the report's integer lanes, `layout_hash`
# excluded (it is the LAST member and zero on every path but the two layout
# refusals, §5.9 #7, #15).
counters_zero? = fn report ->
  report |> Map.drop([:malformed, :layout_hash]) |> Map.values() |> Enum.all?(&(&1 == 0))
end

{:ok, checker} = Agent.start_link(fn -> {0, []} end)

check = fn label, pass? ->
  Agent.get_and_update(checker, fn {n, fails} ->
    if pass? do
      IO.puts("ok #{label}")
      {n, {n + 1, fails}}
    else
      IO.puts("FAIL #{label}")
      {n, {n + 1, fails ++ [label]}}
    end
  end)
end

named_refusal = fn label, data, want, want_layout_hash ->
  case load.(data) do
    {:error, ^want, report} ->
      check.(
        label,
        not report.malformed and counters_zero?.(report) and
          report.layout_hash == want_layout_hash
      )

    other ->
      check.(label <> " — wanted {:error, #{inspect(want)}, _}, got #{inspect(other)}", false)
  end
end

malformed_read = fn label, data ->
  case load.(data) do
    {:error, :malformed, report} ->
      check.(label, report.malformed and counters_zero?.(report) and report.layout_hash == 0)

    other ->
      check.(
        label <>
          " — wanted {:error, :malformed, _} with malformed set, got #{inspect(other)}",
        false
      )
  end
end

# ---- the positive control: a lawful read lands, malformed clear ----

own_file = header.(hash, layout) <> layout <> record.(hash)

check.(
  "lawful own-file read lands {:ok, values, report}",
  match?({:ok, values, _} when is_list(values), load.(own_file))
)

case load.(own_file) do
  {:ok, _values, report} ->
    check.("lawful own-file read: malformed clear", not report.malformed)

  other ->
    check.(
      "lawful own-file read: malformed clear — got #{inspect(other)}",
      false
    )
end

# ---- REFUSAL BY NAME: refused true, reason the name, malformed FALSE ----
# (docs/FIXED-FORM-ALGORITHM.md:906, the table's first row)

# form byte 1 — a committed form-1 file, whatever its length
named_refusal.(
  "form byte 1 refuses previous_form, malformed clear, no counter",
  <<1, 0, 0>>,
  :previous_form,
  0
)

# form byte 2 — a committed form-2 batch
named_refusal.(
  "form byte 2 refuses message_form_as_file, malformed clear, no counter",
  <<2, 0, 0>>,
  :message_form_as_file,
  0
)

# a form byte no form defines
named_refusal.(
  "form byte 9 refuses newer_form, malformed clear, no counter",
  <<9, 0, 0>>,
  :newer_form,
  0
)

# a hash no lineage entry holds — layout_newer reports THE FILE'S HASH and
# nothing else (§5.9 #7)
stranger = Bitwise.bxor(hash, 0xDEADBEEF)

check.(
  "the stranger hash is in no lineage entry (vector soundness)",
  not Enum.any?(Tuple.to_list(known), &(&1.hash == stranger))
)

named_refusal.(
  "a hash outside the lineage refuses layout_newer, layout_hash the file's hash, no counter",
  header.(stranger, layout) <> layout <> record.(stranger),
  :layout_newer,
  stranger
)

# a KNOWN hash whose layout bytes differ — layout_malformed, the name, and
# layout_hash stays 0 (zero on every path but the two layout refusals, §5.9 #15)
corrupt =
  binary_part(layout, 0, byte_size(layout) - 1) <>
    <<Bitwise.bxor(:binary.at(layout, byte_size(layout) - 1), 1)>>

named_refusal.(
  "a known hash over different layout bytes refuses layout_malformed, layout_hash clear",
  header.(hash, corrupt) <> corrupt <> record.(hash),
  :layout_malformed,
  0
)

# a record whose hash names no layout this reader holds
named_refusal.(
  "a record hash that is not the file's refuses no_layout, malformed clear, no counter",
  header.(hash, layout) <> layout <> record.(Bitwise.bxor(hash, 1)),
  :no_layout,
  0
)

# ---- MALFORMED: the residue, malformed TRUE, no reason named ----
# (docs/FIXED-FORM-ALGORITHM.md:906, the table's second row)

# no first byte — there is no form byte to name
malformed_read.("an empty file is malformed, counters zero", <<>>)

# under 20 bytes, the form byte having already answered
malformed_read.("a 5-byte form-3 file is malformed, counters zero", <<3, 0, 0, 0, 0>>)

# any reserved byte 1..7 nonzero — the reserved clause stands before the length
# clause, so eight bytes are enough to name it
malformed_read.("a nonzero reserved byte is malformed, counters zero", <<3, 0, 0, 0, 0, 0, 0, 1>>)

# a ragged tail: rest mod record_bytes != 0
malformed_read.("a ragged tail is malformed, counters zero", own_file <> <<1, 2, 3>>)

{count, fails} = Agent.get(checker, & &1)
Agent.stop(checker)

if fails == [] do
  IO.puts("#{count} assertions, all green — REFUSE is total on the #{mod} read")
  System.halt(0)
else
  IO.puts(:stderr, "#{count} assertions, #{length(fails)} failed: #{Enum.join(fails, "; ")}")
  System.halt(1)
end
