# R14 — EVERY COMPILED ENTRY'S REACH IS IN THE WRITER'S COORDINATES: a union
# tag grown 1 -> 8 reads the writer's tag at the writer's width and compiles
# whole; `layout_record_too_large` is for an entry that really reaches past the
# writer's declared record.
#
# THE LAW: docs/FIXED-FORM-ALGORITHM.md §4.2 and §5.2 PLAN — every compiled
# entry is bounded by the WRITER'S OWN DECLARED RECORD SIZE, and one entry past
# it refuses the plan whole under layout_record_too_large. §5.2 EMIT kind 15
# (the ruling of 2026-11-25): the union's None LANDS my_tag bytes, and a leg
# that reads the writer's raw tag beside it reads it at THE WRITER'S TAG WIDTH,
# their_tag bytes, never my_tag: its source reach is src + their_tag, so a
# grown tag compiles whole and the tag never swallows the arm's payload.
#
# THE PRODUCTION PATH ON THIS LEG. The plan compiler is the generated runtime's
# `compile/5` (emitted from internal/codegen/elixirtable/fixedruntime.go into
# every unit's <Ns>.FixedRuntime; this row loads Tblp1's). This leg lays the
# union's None by the PREFILL (my tag's bytes are zero in the image before any
# write), and the op that reads the writer's raw tag beside it is `:tagcount`
# — `{:tagcount, src, their_tag, their_arms}`, which writes nothing and marks
# a tag past the writer's arm set forged (§4.6). `src_end` takes its reach as
# src + their_tag, and `any_past?` holds every entry to `size_at(theirs, 0)`.
# `lineage_init/7` lays a plan per lock lineage entry at module load through
# `lineage_lane_of`, which is the lane a file under a foreign hash selects.
#
# THE VECTOR (the same derivation test/conformance/rust/rows/R14.rs and
# js/rows/R14.mjs record; no corpus holds a forged lock layout, and a tag of 8
# bytes is impossible in a real schema). A layout block is a u32LE entry
# count, then 17-byte entries: id u64LE, kind u8, size u32LE, children u32LE.
# Kind 13 a table, 15 a union (size = tag + widest arm), 8 a u32.
#   THEIRS (writer): [1,13,5,1], [7,15,5,1], [9,8,4,0] — a 5-byte record.
#   MINE(tag) (reader): [1,13,tag+4,1], [7,15,tag+4,1], [9,8,4,0].
#   My image: the union's tag at 0 (tag bytes), the u32 arm at `tag`.
#
# Run from the repository root:
#   elixir -pa build/elixir-tables-ebin test/conformance/elixir/rows/R14.exs
# Exit 0 green, 1 red, one printed line per assertion. Depends on no other
# rows/ file and edits no shared file.

Code.prepend_path(System.get_env("EBIN", "build/elixir-tables-ebin"))
{:module, _} = Code.ensure_loaded(Tblp1.FixedRuntime)
alias Tblp1.FixedRuntime, as: RT

defmodule R14 do
  @moduledoc false

  def entry(id, kind, size, children),
    do: <<id::little-64, kind::8, size::little-32, children::little-32>>

  def block(entries), do: <<length(entries)::little-32>> <> Enum.join(entries)

  def theirs, do: block([entry(1, 13, 5, 1), entry(7, 15, 5, 1), entry(9, 8, 4, 0)])

  def mine(tag),
    do: block([entry(1, 13, tag + 4, 1), entry(7, 15, tag + 4, 1), entry(9, 8, 4, 0)])

  # MY SIDE, one row per entry of mine(tag): {dst, stride, aux, counted, arg}.
  # The root and the union sit at image 0 and the union's tag leads its
  # storage (aux 0); the arm's u32 overlays the byte past my tag, `tag`.
  def dst(tag), do: {{0, 0, 0, 0, 0}, {0, 0, 0, 0, 0}, {tag, 0, 0, 0, 0}}

  # every op of a plan, the guard chains peeled, outermost first
  def ops(plan), do: Enum.flat_map(plan, &peel/1)
  defp peel({:guard, at, tag, argw, inner}), do: [{:guard, at, tag, argw} | peel(inner)]
  defp peel(op), do: [op]
end

{:ok, pass} = Agent.start_link(fn -> {0, 0} end)

check = fn ok, msg ->
  if ok do
    IO.puts("PASS " <> msg)
    Agent.update(pass, fn {p, f} -> {p + 1, f} end)
  else
    IO.puts("FAIL " <> msg)
    Agent.update(pass, fn {p, f} -> {p, f + 1} end)
  end
end

compile = fn tag ->
  {:ok, t} = RT.parse_layout(R14.theirs())
  {:ok, m} = RT.parse_layout(R14.mine(tag))
  RT.compile(t, m, R14.dst(tag), 4096, RT.report())
end

# RUN a compiled plan over one writer record into my image of tag + 4 bytes:
# the prefill is my None (every byte zero), and a forged tag's mark is
# refused by the runtime's own forged_check, as the generated load does.
run = fn plan, tag, record ->
  {image, report} = RT.run(plan, :erlang.list_to_binary(record), :binary.copy(<<0>>, tag + 4), RT.report())

  try do
    RT.forged_check(report)
    {:ok, image}
  catch
    {:refuse, why} -> {:error, why}
  end
end

# ---- 0. every vector is a lawful layout: the compile is what is reached ------
{:ok, t} = RT.parse_layout(R14.theirs())
check.(RT.size_at(t, 0) == 5, "the writer's declared record is 5 bytes")

for tag <- [1, 2, 8] do
  check.(match?({:ok, _}, RT.parse_layout(R14.mine(tag))), "MINE(#{tag}) parses as a lawful layout")
end

# ---- 1. THE TWINS: THEIRS vs MINE(1) and MINE(2) compile whole --------------
for tag <- [1, 2] do
  res = compile.(tag)
  check.(match?({:ok, [_ | _], _, _}, res), "tag 1 -> #{tag} compiles whole, got #{inspect(elem(res, 0))} #{inspect(elem(res, 1))}")
end

# ---- 2. THE CELL: a tag grown 1 -> 8 reads the writer's 1-byte tag ----------
case compile.(8) do
  {:ok, plan, made, report} ->
    check.(true, "the grown tag compiles whole: #{made} entries, no refusal (not layout_record_too_large)")
    check.(Map.get(report, :unlawful, 0) == 0 and Map.get(report, :unlawful_kind, 0) == 0,
      "nothing marked the pair unlawful")
    ops = R14.ops(plan)

    case Enum.find(ops, &match?({:tagcount, _, _, _}, &1)) do
      {:tagcount, src, width, arms} ->
        check.(src == 0, "the None's tag read is at the writer's tag offset 0, got #{src}")
        check.(width == 1, "it reads the WRITER's 1-byte tag, never my 8: got #{width}")
        check.(arms == 1, "it holds the tag to the writer's arm count, 1: got #{arms}")
        check.(src + width <= 5, "its reach, #{src} + #{width}, is inside the writer's record of 5")

      nil ->
        check.(false, "the union's None tag read (:tagcount) is in the plan: #{inspect(plan)}")
    end

    check.(Enum.member?(ops, {:const, 0, 8, 1}),
      "the arm's tag const lands MY 8-byte tag (dst 0, size 8, value 1): #{inspect(plan)}")
    check.(Enum.member?(ops, {:guard, 0, 1, 1}),
      "the arm is guarded by the writer's tag at offset 0, width 1, ordinal 1: #{inspect(plan)}")
    check.(Enum.member?(ops, {:copy, 1, 8, 4}),
      "the u32 arm is a copy of 4 bytes from src 1, past the WRITER's tag, to my 8: #{inspect(plan)}")

    # THE READ IT LAYS: arm 1 carrying 0x11223344 lands my tag 1 in 8 bytes and
    # the u32 whole past it; the payload is never read as the tag.
    case run.(plan, 8, [1, 0x44, 0x33, 0x22, 0x11]) do
      {:ok, <<mytag::little-64, v::little-32>>} ->
        check.(mytag == 1, "my 8-byte tag lands exactly 1 (high bytes zero), got 0x#{Integer.to_string(mytag, 16)}")
        check.(v == 0x11223344, "the u32 lands whole past my tag, got 0x#{Integer.to_string(v, 16)}")

      other ->
        check.(false, "arm 1 reads with no refusal, got #{inspect(other)}")
    end

    # tag 0 is None: my whole tag lands 0
    case run.(plan, 8, [0, 0, 0, 0, 0]) do
      {:ok, <<mytag::little-64, _::little-32>>} -> check.(mytag == 0, "tag 0 reads as None in my 8-byte tag, got #{mytag}")
      other -> check.(false, "tag 0 reads with no refusal, got #{inspect(other)}")
    end

    # a forged tag past the writer's one arm refuses out_of_range (§4.6), read
    # at the writer's width: 2 is past an arm count of 1.
    res = run.(plan, 8, [2, 0, 0, 0, 0])
    check.(res == {:error, :out_of_range}, "a forged writer tag 2 refuses out_of_range, got #{inspect(res)}")

  other ->
    check.(false, "the grown tag compiles whole: THEIRS vs MINE(8) answered #{inspect(other)}")
end

# ---- 3. THE LINEAGE PATH the generated module runs lays a READY plan ----------
#
# A lock lineage entry whose hash is not this build's own is compiled at module
# load by lineage_init/7 into a lane; a lawful grown tag lays a ready lane with
# the lock's record size.
for tag <- [8, 2, 1] do
  module = :"r14_elixir_row_tag#{tag}"
  known = {
    %{hash: 0x4444444433333333, layout: R14.theirs(), layout_bytes: byte_size(R14.theirs()), record_bytes: 13},
    %{hash: 0x2222222211111111, layout: R14.mine(tag), layout_bytes: byte_size(R14.mine(tag)), record_bytes: tag + 12}
  }

  :ok = RT.lineage_init(module, :root, known, 0x2222222211111111, R14.mine(tag), R14.dst(tag), RT.plan_capacity())
  lane = RT.lineage_lane(module, :root, 0)

  check.(match?({:ok, [_ | _], 5, _, 0, 0}, lane),
    "the grown tag (1 -> #{tag}) lays a ready lineage lane with the lock's record of 5, got #{inspect(lane)}")
  check.(RT.lineage_lane(module, :root, 1) == :identity, "this build's own hash is the identity lane (tag #{tag})")
end

{p, f} = Agent.get(pass, & &1)

if f == 0 do
  IO.puts("elixir/R14 a grown union tag reads the writer's tag width: green (#{p} checks)")
  System.halt(0)
else
  IO.puts("elixir/R14 a grown union tag reads the writer's tag width: RED (#{f} failed, #{p} passed)")
  System.halt(1)
end
