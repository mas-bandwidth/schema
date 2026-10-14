# R7 — THE IDENTITY LANE IS AN INDEX COMPARISON, NEVER A RECOMPUTED HASH
# (docs/roadmap.sexp elixir/R7, audit schema#898). The law is
# docs/FIXED-FORM-ALGORITHM.md:867-871: "the compiler hands every reader its own
# wire hash and every known hash as CONSTANTS — `R.own_hash` and
# `R.lineage[i]`, laid down by COMPILE — and a runtime NEVER computes a hash
# from layout bytes it holds, not for the IDENTITY LANE (step 8, where the
# selected entry being the reader's own is an INDEX COMPARISON and never a
# recomputation)". The refusal table (:884) pins the answer's shape: a hash in
# no lineage entry is `:layout_newer` carrying THE FILE'S HASH, and nothing
# else.
#
# The hurt the law exists for is THIS leg (#928): it hashed its own layout
# bytes to find itself in the lineage. A hash recomputed from layout bytes
# folds in no digest — it is not on the wire and not in the bytes — so it
# matched NOTHING, and the lane that is supposed to be free was the lane
# nothing could reach: every read of this build's own files ran a compiled
# plan, correct in its values and wrong in every way that matters.
#
# PRODUCTION PATH UNDER TEST. Tabledemo.TablesFixed.root_config_fixed_load/2,
# the generated fixed-form reader on the beams `make build-conformance-elixir`
# compiles into build/elixir-tables-ebin (emitted by
# internal/codegen/elixirtable/fixedelixir.go:1612): load ->
# FixedRuntime.read_file_header/1, which takes the header's hash AS GIVEN
# (step 4) -> FixedRuntime.select/4 (fixedruntime.go:1983), the FIRST index
# with lineage[i] == h against the constants COMPILE laid down (step 5) -> the
# floor (step 6) -> the lock's byte comparison (step 7) -> the lane dispatch
# (fixedelixir.go:1661), which reads FixedRuntime.lineage_lane/3 and, for the
# entry that is the reader's own, takes `:identity` — the clause
# `when h == own` (fixedruntime.go:2047), step 8's INDEX COMPARISON against
# the R.own_hash constant — and runs `@root_config_plan`, the identity plan
# literal, one whole-body move. Nothing on that path computes a hash from
# layout bytes. The reader is driven end to end; no helper is the witness.
#
# THE VECTOR is constructed from the law through the PRODUCTION WRITER,
# root_config_fixed_save/1, whose every byte the law fixes (testdata/conformance
# carries no form-3 file): the form byte 3 at 0; the seven reserved zeros at
# 1..7; the hash — the compile-time constant itself, since a runtime never
# derives one — at 8; the u32 layout length at 16; the layout bytes from 20;
# then one record per 8 + body_bytes, each record opening with the same eight
# hash bytes. The frame those bytes carry is asserted below, not trusted.
#
# Run from the repository root:  elixir test/conformance/elixir/rows/R7.exs
# Exit 0 green, 1 red, one printed line per assertion. Depends on no other
# rows/ file and edits no shared file.

import Bitwise

Code.prepend_path(System.get_env("EBIN", "build/elixir-tables-ebin"))

{:module, _} = Code.ensure_loaded(Tabledemo.TablesFixed)
{:module, _} = Code.ensure_loaded(Tabledemo.FixedRuntime)

alias Tabledemo.FixedRuntime, as: R
alias Tabledemo.TablesFixed, as: M

root = :root_config
hash = M.root_config_fixed_hash()
layout = M.root_config_fixed_layout()
body_bytes = M.root_config_fixed_body_bytes()
known = M.root_config_fixed_known()
last = tuple_size(known) - 1
plan = M.root_config_fixed_plan()

# ONE RECORD of default values, through the production writer — every byte the
# law fixes, none of them invented here. The struct is built at run time
# (struct/2), because this file compiles before the ebin is on the path and a
# `%Tabledemo.RootConfig{}` literal would expand at compile time.
written = struct(Tabledemo.RootConfig, version_note: "r7 identity")
file = M.root_config_fixed_save([written])

u = fn data, at, width ->
  :binary.decode_unsigned(binary_part(data, at, width), :little)
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

clean? = fn report ->
  report.malformed == false and report.layout_hash == 0 and
    report.unknown == 0 and report.kind_mismatch == 0 and report.clamped == 0 and
    report.widened == 0 and report.duplicate == 0
end

# ---- THE FRAME THE LAW FIXES, and the constants it is held to ----

check.(
  "the production writer wrote the whole file: #{byte_size(file)} == measure(1)",
  byte_size(file) == M.root_config_fixed_measure(1)
)

check.(
  "the file's frame is the law's: form byte 3 at 0, the seven reserved zeros at 1..7",
  :binary.at(file, 0) == 3 and binary_part(file, 1, 7) == <<0::56>>
)

check.(
  "the hash at 8 IS the compile-time constant R.own_hash (0x#{Integer.to_string(hash, 16)}), handed to the reader, never derived",
  u.(file, 8, 8) == hash
)

check.(
  "every record opens with the same handed constant, never a derived one",
  u.(file, 20 + byte_size(layout), 8) == hash
)

check.(
  "the lineage COMPILE laid down ends on the build's own entry: R.lineage[#{last}].hash == R.own_hash",
  elem(known, last).hash == hash
)

check.(
  "the u32 at 16 is the layout's length and the layout behind it is byte for byte the lock's own bytes, R.lineage[#{last}]",
  u.(file, 16, 4) == byte_size(layout) and
    binary_part(file, 20, byte_size(layout)) == layout and
    elem(known, last).layout == layout and
    elem(known, last).layout_bytes == byte_size(layout)
)

# ---- THE R7 BITE, first face: the selected entry being the reader's own is
# an INDEX COMPARISON — `known[i].hash == R.own_hash`, both COMPILE constants
# (fixedruntime.go:2047) — and never a recomputation over the layout bytes the
# runtime holds. A recomputed hash folds in no digest and matches NOTHING, so
# this clause would answer a COMPILED lane here: the #928 hurt, where the lane
# that is supposed to be free was the lane nothing could reach.

case R.select(known, M.root_config_fixed_floor(), hash, layout) do
  {:ok, i} ->
    check.(
      "select resolves the own hash to lineage index #{last} of #{last}",
      i == last
    )

    check.(
      "the lane of that entry is :identity — step 8's index comparison against R.own_hash, never a recomputed hash",
      R.lineage_lane(M, root, i) == :identity
    )

  other ->
    check.("select resolves the own hash — got #{inspect(other)}", false)
    check.("the lane of that entry is :identity", false)
end

# ---- and the own file READS through that lane, end to end: n records, the
# census lands zero (§5.9 #30 — the identity plan is the law's own shape, not a
# compiled one), and the written value round-trips ----

case M.root_config_fixed_load(file) do
  {:ok, values, report} ->
    check.(
      "the build's own file reads on the identity lane: n=#{length(values)}, every counter zero",
      length(values) == 1 and clean?.(report)
    )

    check.(
      "the written value round-trips",
      values == [written]
    )

  other ->
    check.("the build's own file reads on the identity lane — got #{inspect(other, limit: 8)}", false)
    check.("the written value round-trips", false)
end

# ---- THE R7 BITE, second face: the identity lane is ONE entry — one move, no
# compile — so the same read answers at a caller plan capacity of exactly the
# identity plan's own. A reader whose own entry resolved to a COMPILED plan
# (a recompute matching nothing) holds `made` — the pre-coalesce entry count a
# compile returns — over that one entry and refuses :plan_too_large here
# (§5.9 #4, #5), which is how a dead free lane shows even though the values it
# lands are right.

check.(
  "the identity plan is one whole-body run — the lane is one entry",
  plan == [{:copy, 0, 0, body_bytes}]
)

case M.root_config_fixed_load(file, plan_capacity: length(plan)) do
  {:ok, values, report} ->
    check.(
      "the own hash resolves to a plan of exactly the identity plan's size (#{length(plan)} entry): the same file reads at that caller plan capacity, so the lane it takes is the identity lane and never a compiled one",
      length(values) == 1 and clean?.(report)
    )

  other ->
    check.(
      "the own hash resolves to a plan of exactly the identity plan's size (#{length(plan)} entry) — got #{inspect(other, limit: 8)}",
      false
    )
end

# ---- THE HASH WAS TAKEN AS GIVEN, NEVER RECOMPUTED. A header hash no
# lineage entry holds is :layout_newer, and the report carries THE FILE'S HASH
# — the value the header held, not this build's constant and not a hash of the
# layout bytes behind it, which no runtime computes.

given = hash |> bxor(0xFFFFFFFFFFFFFFFF)

lying =
  <<3, 0::56, given::little-unsigned-64, byte_size(layout)::little-unsigned-32>> <>
    layout <> <<hash::little-unsigned-64, 0::size(body_bytes)-unit(8)>>

check.("the lying hash is in no lineage entry (vector soundness)",
  not Enum.any?(Tuple.to_list(known), &(&1.hash == given))
)

case M.root_config_fixed_load(lying) do
  {:error, :layout_newer, report} ->
    check.(
      "a header hash no lineage entry holds is :layout_newer carrying THE FILE'S HASH (0x#{Integer.to_string(given, 16)}) and nothing else: the hash is taken as given, never recomputed",
      report.layout_hash == given and clean?.(%{report | layout_hash: 0})
    )

  other ->
    check.(
      "a header hash no lineage entry holds is :layout_newer carrying THE FILE'S HASH and nothing else — got #{inspect(other, limit: 8)}",
      false
    )
end

{count, fails} = Agent.get(checker, & &1)
Agent.stop(checker)

if fails == [] do
  IO.puts("#{count} assertions, all green — the identity lane is an index comparison on #{M}")
  System.halt(0)
else
  IO.puts(:stderr, "#{count} assertions, #{length(fails)} failed: #{Enum.join(fails, "; ")}")
  System.halt(1)
end
