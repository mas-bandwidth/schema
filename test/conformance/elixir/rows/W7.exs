# W7.exs — "arg and meta are two lanes" (cell elixir/W7).
#
# THE LAW (docs/FIXED-FORM-ALGORITHM.md:245, fix 12; the card's pointer :240 lands
# inside the guard2 paragraph, the governing sentence is :245):
#   "**`arg` and `meta` ARE TWO LANES BECAUSE THEY ARE TWO FACTS, and must never
#   share one.** A `string(N)` under a union arm needs both, and one lane gives
#   whichever was stamped last: a byte string under arm `2` read as WIDE, or an
#   entry that runs only when the tag equals the flavour — **under the wrong arm**."
# and the struct's own split (docs/FIXED-FORM-ALGORITHM.md:231):
#   "arg is THE GUARD'S ORDINAL and nothing else; meta is THE OP'S OWN ARGUMENT
#   (a text flavour)."
# The `text` op's table row confirms the meta lane (docs/FIXED-FORM-ALGORITHM.md:254):
#   "`meta` = flavour" (1 utf8, 2 wide, 3 bytes).
#
# THIS LEG SPENDS THE TWO LANES BY SHAPE, in two different tuples of one plan
# entry: the ordinal rides the `{:guard, gsrc, tag, argw, inner}` wrapper (`tag` IS
# the arg lane) and the flavour is the text entry's own last element
# (`{:text, src, dst, aux, size, flavour}` — `flavour` IS the meta lane). The
# generated runtime consumes them in two different `step/4` clauses of
# FixedRuntime: the guard clause compares the record's tag to `tag` at `argw`
# width (build/tables-generated-elixir/tblp1/FixedRuntime.ex:1536), the text
# clause reads `flavour` for the unit and the content rule (same file, :1568,
# :1579). Two lanes, two clauses — neither can stamp over the other.
#
# THE VECTOR, CONSTRUCTED FROM THE LAW: the conformance corpus carries no
# `string(N)` under a union arm (tables/examples' Effect arms hold only scalars;
# there is no UT1 unit in ELIXIR_TABLE_UNITS), so the record is the smallest body
# the law names — a one-byte union tag at offset 0, then a `string(8)`'s
# four-byte little-endian used length and its eight declared content bytes; arm 2
# (ordinal 2, distinct from the utf8 flavour 1), "hello" (5 used bytes), slack
# zeroed:
#
#   [0]     tag = 2           the guard's ordinal fact
#   [1..5]  length = 5        the text op's length word
#   [5..13] "hello\0\0\0"     the declared 8-byte span
#
# The plan entries are built exactly as the plan compiler builds them for a text
# field inside a union's second arm (`guarded` wraps `{:text, ...}` with the
# writer's ordinal, FixedRuntime.ex:893-894, :1180-1181) and run through the
# production read loop `FixedRuntime.run/4`, reached the way the leg's driver
# reaches generated code: the beams under $EBIN / build/elixir-tables-ebin.
# Depends on no other rows/ file.
#
# Run from the repository root:  elixir test/conformance/elixir/rows/W7.exs
# Exit 0 green, exit 1 red, one printed line per assertion.

ebin = System.get_env("EBIN", "build/elixir-tables-ebin")
Code.prepend_path(ebin)

{:module, _} = Code.ensure_loaded(Tblp1.FixedRuntime)

alias Tblp1.FixedRuntime, as: RT

# THE VECTOR — derived in the header, built here.
record_utf8 = <<2, 5::little-signed-32, "hello", 0, 0, 0>>
# the wide twin: same arm-2 guard, length in CODE UNITS (2), payload four
# UTF-16 LE units across the same 8-byte declared span.
record_wide = <<2, 2::little-signed-32, "h", 0, "e", 0, "l", 0, "l", 0>>
prefill = :binary.copy(<<0>>, 13)

# THE TWO LANES, TWO TUPLES: the tag byte copied whole (unguarded), then the
# arm-2 guard wrapping the text entry. arg lane = 2 (the ordinal, nothing else);
# meta lane = 1 (utf8). The wide twin keeps the guard IDENTICAL and changes only
# the flavour element — that is the independence the law demands.
tag_copy = {:copy, 0, 0, 1}
guarded_utf8 = {:guard, 0, 2, 1, {:text, 1, 1, 5, 8, 1}}
guarded_wide = {:guard, 0, 2, 1, {:text, 1, 1, 5, 8, 2}}
plan_utf8 = [tag_copy, guarded_utf8]
plan_wide = [tag_copy, guarded_wide]

# THE OLD ONE-LANE ENCODING, PLANTED BY HAND: the flavour stamped over the
# arm ordinal in the guard's slot — what a port that shared one lane would have
# produced. A tag of 2 no longer matches ordinal 1, so under arm 2 the entry
# must drop the string.
plan_folded = [tag_copy, {:guard, 0, 1, 1, {:text, 1, 1, 5, 8, 1}}]

run = fn plan, body -> RT.run(plan, body, prefill, RT.report()) end

checks = [
  {"W7 arg and meta ride two different slots -- ordinal 2, flavour 1, not equal",
   fn ->
     {:guard, _g, ordinal, _argw, {:text, _s, _d, _a, _size, flavour}} = guarded_utf8
     ordinal == 2 and flavour == 1 and ordinal != flavour
   end},
  {"W7 changing the flavour leaves the ORDINAL lane untouched (guard identical, 2 in both)",
   fn ->
     {:guard, g1, t1, w1, {:text, _, _, _, _, f1}} = guarded_utf8
     {:guard, g2, t2, w2, {:text, _, _, _, _, f2}} = guarded_wide
     g1 == g2 and t1 == t2 and w1 == w2 and t1 == 2 and f1 == 1 and f2 == 2
   end},
  {"W7 both lanes together: the entry fires under arm 2 and lands the utf8 text, clean",
   fn ->
     {image, report} = run.(plan_utf8, record_utf8)

     image == <<2, 5::little-signed-32, "hello", 0, 0, 0>> and
       report.malformed == false and report.clamped == 0
   end},
  {"W7 the guard answers the ORDINAL lane, never the flavour: tag 1 (== the flavour) fires nothing",
   fn ->
     # record tag 1 equals the utf8 FLAVOUR but not the entry's ordinal 2: if
     # the guard read the meta lane, "hello" would land here. It must not.
     {image, report} = run.(plan_utf8, <<1, 5::little-signed-32, "hello", 0, 0, 0>>)

     image == <<1, 0::size(12)-unit(8)>> and report.malformed == false
   end},
  {"W7 the FLAVOUR lane alone moves the read: wide under the same arm-2 guard counts units",
   fn ->
     # meta 1 -> 2 turns the same bytes into wide: the length lands in UNITS
     # (2), not bytes (5), and the payload rides as UTF-16 — while the arg lane
     # still fires under tag 2.
     {image, report} = run.(plan_wide, record_wide)

     image == <<2, 2::little-signed-32, "h", 0, "e", 0, "l", 0, "l", 0>> and
       report.malformed == false and report.clamped == 0
   end},
  {"W7 NEGATIVE CONTROL: the old one-lane encoding (flavour stamped over the ordinal) drops the string under arm 2",
   fn ->
     {image, _report} = run.(plan_folded, record_utf8)
     image == <<2, 0::size(12)-unit(8)>>
   end}
]

results =
  Enum.map(checks, fn {name, probe} ->
    try do
      {name, probe.()}
    rescue
      e -> {name, {:raised, e}}
    end
  end)

Enum.each(results, fn
  {name, true} -> IO.puts("ok   #{name}")
  {name, false} -> IO.puts("FAIL #{name}")
  {name, {:raised, e}} -> IO.puts("FAIL #{name} (raised #{inspect(e)})")
end)

if Enum.all?(results, fn {_name, r} -> r == true end) do
  IO.puts("elixir/W7 arg and meta are two lanes: green")
  System.halt(0)
else
  IO.puts("elixir/W7 arg and meta are two lanes: RED")
  System.halt(1)
end
