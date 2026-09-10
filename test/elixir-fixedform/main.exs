# THE ELIXIR LEG of the FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3.
#
# THE BYTES ARE THE C++ REFERENCE'S, and that is measured here rather than
# asserted. `make tables-fixedform-corpus` has the reference write one form-3
# FILE per root — FX1, FX2, P1, P3, KeyedConfig and PackConfig — with values set
# by hand, and this leg proves itself against those files two ways:
#
#   THE WRITE   read the file, save the values back, and the bytes must be
#               IDENTICAL. That reaches every field, the layout, the hash and
#               every byte of declared slack, because a byte this port encodes
#               differently is a byte that does not come back.
#   THE READ    read a file written under ANOTHER schema's layout, which is the
#               plan path and the whole of what §3.4's versioning invariant is
#               worth.
#
# Beside them ride §4's counters and the NEGATIVE CONTROLS, of which the one
# §3.4 names by name is a reader given the WRONG PLAN for a record: "the plan is
# the whole of this form's safety, so a test that never watched a wrong plan
# fail is a test that never checked the right one worked."

defmodule Leg do
  def start do
    :persistent_term.put({__MODULE__, :fails}, [])
  end

  def check(name, true), do: pass(name)

  def check(name, false), do: fail(name, "expected true")

  def eq(name, got, want) when got == want, do: pass(name)

  def eq(name, got, want) do
    fail(name, "got #{inspect(got, limit: 12)}\n           want #{inspect(want, limit: 12)}")
  end

  def bytes_eq(name, got, want) when got == want, do: pass(name)

  def bytes_eq(name, got, want) do
    fail(name, "#{byte_size(got)} bytes vs #{byte_size(want)}; #{first_diff(got, want)}")
  end

  defp first_diff(a, b) do
    n = min(byte_size(a), byte_size(b))

    case Enum.find(0..(n - 1)//1, fn i -> :binary.at(a, i) != :binary.at(b, i) end) do
      nil ->
        "the shorter is a prefix of the longer"

      i ->
        "first byte differing from the C++ reference at offset #{i}: " <>
          "0x#{Integer.to_string(:binary.at(a, i), 16)} vs 0x#{Integer.to_string(:binary.at(b, i), 16)}"
    end
  end

  defp pass(name), do: IO.puts("  ok   #{name}")

  defp fail(name, why) do
    IO.puts("  FAIL #{name}: #{why}")

    :persistent_term.put({__MODULE__, :fails}, [name | :persistent_term.get({__MODULE__, :fails})])
  end

  def verdict do
    case :persistent_term.get({__MODULE__, :fails}) do
      [] ->
        IO.puts("\nfixed form: the Elixir leg matches the C++ reference")
        System.halt(0)

      fails ->
        IO.puts("\nFAILED: #{length(fails)} — #{Enum.join(Enum.reverse(fails), ", ")}")
        System.halt(1)
    end
  end

  def section(title), do: IO.puts("\n== #{title}")
end

[corpus] = System.argv()
read = fn name -> File.read!(Path.join(corpus, name)) end
Leg.start()

# ---------------------------------------------------------------------------
# THE WRITE: every file read and saved back has to come out BYTE FOR BYTE
# ---------------------------------------------------------------------------

Leg.section("the reference's own bytes, read and written back")

roundtrip = fn name, load, save ->
  want = read.(name)

  case load.(want) do
    {:ok, values, report} ->
      Leg.eq("#{name}: a clean read moves no counter", Map.delete(report, :malformed), %{
        unknown: 0,
        kind_mismatch: 0,
        clamped: 0,
        widened: 0,
        duplicate: 0
      })

      Leg.bytes_eq("#{name}: saved back byte for byte", save.(values), want)
      values

    other ->
      Leg.check("#{name}: read", false)
      IO.puts("     #{inspect(other)}")
      []
  end
end

fx1 =
  roundtrip.(
    "fx1.bin",
    &Tblfx1.FX1Fixed.fx_root_fixed_load/1,
    &Tblfx1.FX1Fixed.fx_root_fixed_save/1
  )

fx2 =
  roundtrip.(
    "fx2.bin",
    &Tblfx2.FX2Fixed.fx_root_fixed_load/1,
    &Tblfx2.FX2Fixed.fx_root_fixed_save/1
  )

p1 = roundtrip.("p1.bin", &Tblp1.P1Fixed.chain_fixed_load/1, &Tblp1.P1Fixed.chain_fixed_save/1)
p3 = roundtrip.("p3.bin", &Tblp3.P3Fixed.chain_fixed_load/1, &Tblp3.P3Fixed.chain_fixed_save/1)

keyed =
  roundtrip.(
    "keyed.bin",
    &Tabledemo.KeyedFixed.keyed_config_fixed_load/1,
    &Tabledemo.KeyedFixed.keyed_config_fixed_save/1
  )

pack =
  roundtrip.(
    "pack.bin",
    &Tabledemo.PackFixed.pack_config_fixed_load/1,
    &Tabledemo.PackFixed.pack_config_fixed_save/1
  )

# ---------------------------------------------------------------------------
# THE VALUES, so a round trip that agreed on the wrong bytes cannot pass
# ---------------------------------------------------------------------------

Leg.section("the values the reference set, field by field")

[a, b] = fx1
Leg.eq("fx1[0].keep", a.keep, 4242)
Leg.eq("fx1[0].narrow", a.narrow, 40000)
Leg.eq("fx1[0].renamed", a.renamed, 321)
Leg.eq("fx1[0].gone", a.gone, 654)
Leg.eq("fx1[0].nested", {a.nested.a, a.nested.b}, {111, 222})

Leg.eq(
  "fx1[1]",
  {b.keep, b.narrow, b.renamed, b.gone, b.nested.a, b.nested.b},
  {1, 2, 3, 4, 5, 6}
)

[f2] = fx2
Leg.eq("fx2.keep", f2.keep, 5150)
Leg.eq("fx2.narrow", f2.narrow, 70000)
Leg.eq("fx2.renamed_to", f2.renamed_to, 808)
Leg.eq("fx2.added", f2.added, 909)
Leg.eq("fx2.extra", {f2.extra.x, f2.extra.y}, {55, 66})

[c1] = p1
Leg.eq("p1.name", c1.name, "chain-one")
Leg.eq("p1.link", {c1.link.value, c1.link.tag}, {77, "tagged"})

[present, absent] = p3
Leg.eq("p3[0] present", {present.name, present.link_present}, {"present", true})
Leg.eq("p3[0].link", {present.link.value, present.link.tag}, {88, "here"})
# THE PAYLOAD RIDES WHOLE whether or not it is present (§3.4), which is why the
# value surface carries it beside the flag and why the bytes come back.
Leg.eq("p3[1] absent", {absent.name, absent.link_present}, {"absent", false})

Leg.eq(
  "p3[1].link rides behind a false flag",
  {absent.link.value, absent.link.tag},
  {99, "still"}
)

[k0, _k1] = keyed
Leg.eq("keyed[0].teams: every slot, in the key enum's order", length(k0.teams), 3)

Leg.eq(
  "keyed[0].teams[0]",
  {Enum.at(k0.teams, 0).spawn_count, Enum.at(k0.teams, 0).banner},
  {4, "red"}
)

Leg.eq("keyed[0].teams[2].banner", Enum.at(k0.teams, 2).banner, "green")
Leg.eq("keyed[0].scores.per_team", k0.scores.per_team, [1000, 2000, 3000])
Leg.eq("keyed[0].hulls[0].health", Enum.at(k0.hulls, 0).health, 100.0)

Leg.eq(
  "keyed[0].hulls[0].turrets[0] gunner present",
  Enum.at(Enum.at(k0.hulls, 0).turrets, 0).gunner_present,
  true
)

[p0, p1v] = pack
Leg.eq("pack[0].version", p0.version, 7)
Leg.eq("pack[0].global.tick_rate", p0.global.tick_rate, 120)
# THE ORDINAL IS THE VARIANT'S POSITION IN THE LAYOUT, from 1, and 0 is None.
Leg.eq("pack[0].global.difficulty = Hard", p0.global.difficulty, 3)
Leg.eq("pack[0].global.build_note", p0.global.build_note, "first build")
Leg.eq("pack[0].global.spawn_delays", p0.global.spawn_delays, [0.5, 1.0, 1.5])
Leg.eq("pack[0].ships[0].display_name", Enum.at(p0.ships, 0).display_name, "fighter")
Leg.eq("pack[0].ships[0].hardpoints (a COUNT, then MAX)", Enum.at(p0.ships, 0).hardpoints, [1])
Leg.eq("pack[0].ships[1].hardpoints", Enum.at(p0.ships, 1).hardpoints, [1, 2])
Leg.eq("pack[0].ships[2].gunner.callsign", Enum.at(p0.ships, 2).gunner.callsign, "ghost")
Leg.eq("pack[0].thresholds", p0.thresholds, [100, 200, 300])
Leg.eq("pack[0].reserves", length(p0.reserves), 2)
Leg.eq("pack[0].reserves[1].display_name", Enum.at(p0.reserves, 1).display_name, "spare-b")
Leg.eq("pack[1].global.difficulty = Easy", p1v.global.difficulty, 1)

# ---------------------------------------------------------------------------
# THE READ: ANOTHER WRITER'S LAYOUT, through a plan compiled from it
# ---------------------------------------------------------------------------

Leg.section("the plan path: §4's evolution table, and what each edit costs")

# AN FX2 READER OVER AN FX1 FILE. Every edit FX1.schema names lands here:
#   narrow   uint16 there, uint32 here — a WIDENED field, counting `widened`
#   renamed  arrives under `was = "renamed"` and resolves like any other id
#   gone     a field this reader cannot name — one `unknown`, stepped over
#   added    a field the writer does not carry — this reader's declared default
#   extra    a whole nested TYPE the writer does not carry — the default too
{:ok, [n0, n1], newer} = Tblfx2.FX2Fixed.fx_root_fixed_load(read.("fx1.bin"))
Leg.eq("newer over older: keep", n0.keep, 4242)
Leg.eq("newer over older: narrow WIDENED u16 -> u32", n0.narrow, 40000)
Leg.eq("newer over older: renamed arrives under `was`", n0.renamed_to, 321)
Leg.eq("newer over older: added takes its declared default", n0.added, 11)
Leg.eq("newer over older: an unknown TYPE leaves its default", {n0.extra.x, n0.extra.y}, {0, 0})
Leg.eq("newer over older: nested", {n0.nested.a, n0.nested.b}, {111, 222})
Leg.eq("newer over older: the second record too", {n1.narrow, n1.renamed_to}, {2, 3})
Leg.eq("newer over older: one `unknown`, counted ONCE per writer", newer.unknown, 1)
Leg.eq("newer over older: one `widened` per record", newer.widened, 2)
Leg.eq("newer over older: nothing else moved", {newer.kind_mismatch, newer.clamped}, {0, 0})
Leg.eq("newer over older: `malformed` does not fire", newer.malformed, false)

# AN FX1 READER OVER AN FX2 FILE. Coming back DOWN the ladder is a kind that
# MOVED and is reported rather than reinterpreted (§4).
{:ok, [o0], older} = Tblfx1.FX1Fixed.fx_root_fixed_load(read.("fx2.bin"))
Leg.eq("older over newer: keep", o0.keep, 5150)
Leg.eq("older over newer: u32 -> u16 is a kind that MOVED", o0.narrow, 3)
Leg.eq("older over newer: the rename resolves both ways", o0.renamed, 808)
Leg.eq("older over newer: a field the writer dropped defaults", o0.gone, 9)
Leg.eq("older over newer: nested", {o0.nested.a, o0.nested.b}, {33, 44})
Leg.eq("older over newer: one `kind_mismatch`", older.kind_mismatch, 1)
Leg.eq("older over newer: `added` and `extra` are two `unknown`", older.unknown, 2)
Leg.eq("older over newer: nothing was damaged", older.malformed, false)

# AN OPTIONAL AGAINST A VALUE. §2.3 makes `?T` and a plain `T` wire-identical in
# form 1; ON THIS FORM THEY ARE ONE BYTE APART, and the layout's kind 35 is what
# lets a reader SEE the edit — it reads as `kind_mismatch` and the field takes
# its declared default, rather than every byte after it sliding by one.
{:ok, [v0, _v1], value_side} = Tblp1.P1Fixed.chain_fixed_load(read.("p3.bin"))
Leg.eq("value reader over an optional writer: name still lands", v0.name, "present")
Leg.eq("value reader over an optional writer: the payload defaults", v0.link.value, 0)
Leg.eq("value reader over an optional writer: one `kind_mismatch`", value_side.kind_mismatch, 1)

{:ok, [w0], opt_side} = Tblp3.P3Fixed.chain_fixed_load(read.("p1.bin"))
Leg.eq("optional reader over a value writer: name still lands", w0.name, "chain-one")
Leg.eq("optional reader over a value writer: absent", w0.link_present, false)
Leg.eq("optional reader over a value writer: one `kind_mismatch`", opt_side.kind_mismatch, 1)

# THE PLAN IS COMPILED ONCE PER PEER AND CACHED BY HASH, and the second read of
# the same peer's file has to answer the same thing out of the cache.
{:ok, [again | _], _} = Tblfx2.FX2Fixed.fx_root_fixed_load(read.("fx1.bin"))
Leg.eq("the cached plan reads the same", {again.narrow, again.renamed_to}, {40000, 321})

Leg.check(
  "the plan is held by hash in :persistent_term",
  :persistent_term.get({Tblfx2.FX2Fixed, :fixed_plan, Tblfx1.FX1Fixed.fx_root_fixed_hash()}, nil) !=
    nil
)

# A PLAN COMPILED FROM A LAYOUT THAT IS MINE, BYTE FOR BYTE, MUST LAND EXACTLY
# WHAT THE IDENTITY PLAN LANDS. That is what reaches `count`, `text`, `ordinal`
# and the keyed walk, which the cross-schema cases above do not touch.
compiled_is_identity = fn name, layout, dst, plan, prefill, decode, clamped, values ->
  {:ok, mine} = Tblp1.FixedRuntime.parse_layout(layout)
  {:ok, made, _} = Tblp1.FixedRuntime.compile(mine, mine, dst, 4096, Tblp1.FixedRuntime.report())

  run_both = fn body ->
    {img_a, ra} = Tblp1.FixedRuntime.run(plan, body, prefill, Tblp1.FixedRuntime.report())
    {img_b, rb} = Tblp1.FixedRuntime.run(made, body, prefill, Tblp1.FixedRuntime.report())
    {{img_a, ra.clamped + clamped.(img_a)}, {img_b, rb.clamped + clamped.(img_b)}}
  end

  both = Enum.map(values, run_both)

  Leg.check(
    "#{name}: a plan compiled from MY OWN layout lands what the identity plan lands",
    Enum.all?(both, fn {{a, _}, {b, _}} -> decode.(a) == decode.(b) end)
  )

  # THE COUNTER MOVES THE SAME EITHER WAY, which is what the clamp fold owes
  # §4. THE IDENTITY PATH CHECKS IN THE PROJECTION — its plan is one copy and
  # carries no `clamp`, `count` or `text` op at all — and a COMPILED plan
  # checks in the interpreter, as it always did; the two are two spellings of
  # the same rule and a record has to be `clamped` the same number of times
  # under both.
  #
  # A HOSTILE BODY RIDES BESIDE THE REAL ONES: every byte 0xFF, so every
  # declared bound is crossed at once. A corpus whose values are all in range
  # would let both sides count nothing and call it agreement. It is counted and
  # not decoded, because an ENUM ORDINAL the writer does not carry is remapped
  # by a compiled plan and passed through by the identity plan, which is §3.4's
  # own rule and not this fold's business.
  hostile = :binary.copy(<<0xFF>>, byte_size(prefill))
  counted = both ++ [run_both.(hostile)]

  Leg.check(
    "#{name}: the identity path counts the `clamped` a compiled plan counts",
    Enum.all?(counted, fn {{_, a}, {_, b}} -> a == b end)
  )

  Leg.check(
    "#{name}: and a body of nothing but 0xFF actually crossed a bound",
    match?({{_, n}, {_, _}} when n > 0, List.last(counted))
  )
end

bodies_of = fn data, size ->
  {:ok, _stated, _layout, records} = Tblp1.FixedRuntime.read_file_header(data)

  for <<_h::little-unsigned-64, body::binary-size(^size) <- records>>, do: body
end

compiled_is_identity.(
  "pack",
  Tabledemo.PackFixed.pack_config_fixed_layout(),
  Tabledemo.PackFixed.pack_config_fixed_dst(),
  Tabledemo.PackFixed.pack_config_fixed_plan(),
  Tabledemo.PackFixed.pack_config_fixed_prefill(),
  &Tabledemo.PackFixed.pack_config_fixed_decode/1,
  &Tabledemo.PackFixed.pack_config_fixed_clamped/1,
  bodies_of.(read.("pack.bin"), Tabledemo.PackFixed.pack_config_fixed_body_bytes())
)

compiled_is_identity.(
  "keyed",
  Tabledemo.KeyedFixed.keyed_config_fixed_layout(),
  Tabledemo.KeyedFixed.keyed_config_fixed_dst(),
  Tabledemo.KeyedFixed.keyed_config_fixed_plan(),
  Tabledemo.KeyedFixed.keyed_config_fixed_prefill(),
  &Tabledemo.KeyedFixed.keyed_config_fixed_decode/1,
  &Tabledemo.KeyedFixed.keyed_config_fixed_clamped/1,
  bodies_of.(read.("keyed.bin"), Tabledemo.KeyedFixed.keyed_config_fixed_body_bytes())
)

# AN ORDINAL SLIDE, which is the one edit a record's bytes cannot show.
#
# FE2 inserts `Electrum` between Bronze and Silver, so Gold's ORDINAL slides
# from 3 to 4; and it inserts the `shield` arm between `boost` and `ward`, so
# `ward`'s tag slides from 2 to 3 and the arm behind the tag moves with it. A
# reader that took the number for its own would read Gold as Silver and a ward
# as a shield. THE PLAN REMAPS BOTH: the `ordinal` op resolves a variant through
# the plan's own table, and a union's tag is written as MY ordinal under THEIR
# tag's guard.
Leg.section("the ordinal slide: an enum and a union that both gained a variant in the middle")

slide =
  Tblfe1.FE1Fixed.fe_root_fixed_save([
    %Tblfe1.FeRoot{
      grade: 3,
      effect: %Tblfe1.Effect{type: 2, ward: %Tblfe1.Ward{charge: 41}},
      tail: 99
    },
    %Tblfe1.FeRoot{
      grade: 1,
      effect: %Tblfe1.Effect{type: 1, boost: %Tblfe1.Boost{power: 17}},
      tail: 100
    }
  ])

{:ok, [s0, s1], slide_report} = Tblfe2.FE2Fixed.fe_root_fixed_load(slide)
Leg.eq("the enum's Gold slid 3 -> 4 and was REMAPPED", s0.grade, 4)
Leg.eq("the union's ward tag slid 2 -> 3", s0.effect.type, 3)
Leg.eq("and the arm behind the slid tag landed", s0.effect.ward.charge, 41)
Leg.eq("Bronze did not slide, and neither did the boost arm", {s1.grade, s1.effect.type}, {1, 1})
Leg.eq("the arm's payload", s1.effect.boost.power, 17)
Leg.eq("the tail past the union is undisturbed", {s0.tail, s1.tail}, {99, 100})
Leg.eq("a slide is not a `kind_mismatch`", slide_report.kind_mismatch, 0)

Leg.eq(
  "`shield` is a field this writer does not carry, so nothing counts",
  slide_report.unknown,
  0
)

Leg.eq("a slide does not damage", slide_report.malformed, false)

# and BACK: FE1 reading FE2. Electrum and shield are variants this reader has no
# name for, so the ordinal resolves to NONE and the arm is simply never a source.
back =
  Tblfe2.FE2Fixed.fe_root_fixed_save([
    %Tblfe2.FeRoot{
      grade: 2,
      effect: %Tblfe2.Effect{type: 2, shield: %Tblfe2.Shield{plating: 5}},
      tail: 8
    }
  ])

{:ok, [b0], back_report} = Tblfe1.FE1Fixed.fe_root_fixed_load(back)
Leg.eq("a variant this reader cannot name resolves to None", b0.grade, 0)
Leg.eq("an arm this reader cannot name leaves the tag at None", b0.effect.type, 0)
Leg.eq("and the tail past it still lands", b0.tail, 8)
Leg.eq("nothing was damaged coming back", back_report.malformed, false)

# ---------------------------------------------------------------------------
# THE NEGATIVE CONTROLS
# ---------------------------------------------------------------------------

Leg.section("the negative controls: every refusal BY NAME, and no damage")

# THE ONE §3.4 NAMES: A READER GIVEN THE WRONG PLAN FOR A RECORD GOES RED.
[fx1_body | _] = bodies_of.(read.("fx1.bin"), Tblfx1.FX1Fixed.fx_root_fixed_body_bytes())

{wrong_image, _} =
  Tblfx2.FixedRuntime.run(
    Tblfx2.FX2Fixed.fx_root_fixed_plan(),
    <<fx1_body::binary, 0::size(10)-unit(8)>>,
    Tblfx2.FX2Fixed.fx_root_fixed_prefill(),
    Tblfx2.FixedRuntime.report()
  )

wrong = Tblfx2.FX2Fixed.fx_root_fixed_decode(wrong_image)

Leg.check(
  "the WRONG plan does NOT reproduce the record",
  {wrong.narrow, wrong.renamed_to} != {40000, 321}
)

# A FORM BYTE THIS BUILD DOES NOT CARRY, AND THE DIRECTION IT SITS IN. The
# registry is ORDERED (§3), so one word for both directions was one word too
# few: the VARIABLE form is older and a batch handed to a file reader is neither.
<<_form, tail::binary>> = read.("fx1.bin")

Leg.eq(
  "form byte 6 is `newer_form`",
  Tblfx1.FX1Fixed.fx_root_fixed_load(<<6, tail::binary>>) |> elem(1),
  :newer_form
)

Leg.eq(
  "form byte 1 is `previous_form` — the VARIABLE form is OLDER",
  Tblfx1.FX1Fixed.fx_root_fixed_load(<<1, tail::binary>>) |> elem(1),
  :previous_form
)

Leg.eq(
  "form byte 2 is `message_form_as_file`",
  Tblfx1.FX1Fixed.fx_root_fixed_load(<<2, tail::binary>>) |> elem(1),
  :message_form_as_file
)

# A LAYOUT WHOSE BYTES ARE NOT A LAYOUT, each rule under ITS OWN NAME.
{:ok, stated, layout, records} = Tblfx1.FixedRuntime.read_file_header(read.("fx1.bin"))
Leg.eq("the header names the layout it carries", stated, Tblfx1.FixedRuntime.hash(layout))

refuse = fn name, bad, want ->
  data =
    IO.iodata_to_binary([
      Tblfx1.FixedRuntime.file_header(Tblfx1.FixedRuntime.hash(bad), byte_size(bad)),
      bad,
      records
    ])

  Leg.eq(name, Tblfx1.FX1Fixed.fx_root_fixed_load(data) |> elem(1), want)
end

<<_count::little-unsigned-32, entries::binary>> = layout

refuse.(
  "an entry count that does not fit is `layout_count_mismatch`",
  <<99::little-unsigned-32, entries::binary>>,
  :layout_count_mismatch
)

refuse.("a truncated layout is `layout_malformed`", binary_part(layout, 0, 3), :layout_malformed)

# the ROOT's kind is checked first and by its own name, because a root that is
# not a TABLE is a layout whose every offset is a guess
bad_root =
  <<binary_part(layout, 0, 12)::binary, 14,
    binary_part(layout, 13, byte_size(layout) - 13)::binary>>

refuse.("a root that is not a table is `layout_kind_invalid`", bad_root, :layout_kind_invalid)

# and a CHILD's kind outside the closed set: entry 1's kind byte is at 4 + 17 + 8
bad_kind =
  <<binary_part(layout, 0, 29)::binary, 99,
    binary_part(layout, 30, byte_size(layout) - 30)::binary>>

refuse.("a kind outside the closed set is `layout_kind_unknown`", bad_kind, :layout_kind_unknown)

# a root whose size its children do not account for
bad_size =
  <<binary_part(layout, 0, 13)::binary, 0xFF, 0xFF, 0, 0,
    binary_part(layout, 17, byte_size(layout) - 17)::binary>>

refuse.(
  "a size its kind does not admit is `layout_size_mismatch`",
  bad_size,
  :layout_size_mismatch
)

# a child count the layout does not hold
bad_tree =
  <<binary_part(layout, 0, 17)::binary, 99, 0, 0, 0,
    binary_part(layout, 21, byte_size(layout) - 21)::binary>>

Leg.check(
  "a child count that does not close refuses by name",
  Tblfx1.FX1Fixed.fx_root_fixed_load(
    IO.iodata_to_binary([
      Tblfx1.FixedRuntime.file_header(Tblfx1.FixedRuntime.hash(bad_tree), byte_size(bad_tree)),
      bad_tree,
      records
    ])
  )
  |> elem(1)
  |> then(&(&1 in [:layout_tree_unclosed, :layout_size_mismatch, :layout_kind_invalid]))
)

# A HEADER WHOSE HASH IS NOT THE HASH OF THE LAYOUT BEHIND IT is refused, and
# CHECKED LAST so a broken layout is never reported as a lying header.
Leg.eq(
  "a header that names another layout is `layout_malformed`",
  Tblfx1.FX1Fixed.fx_root_fixed_load(
    IO.iodata_to_binary([
      Tblfx1.FixedRuntime.file_header(stated + 1, byte_size(layout)),
      layout,
      records
    ])
  )
  |> elem(1),
  :layout_malformed
)

Leg.eq(
  "and the layout's OWN rule is what fires when both are wrong",
  Tblfx1.FX1Fixed.fx_root_fixed_load(
    IO.iodata_to_binary([
      Tblfx1.FixedRuntime.file_header(stated + 1, byte_size(bad_kind)),
      bad_kind,
      records
    ])
  )
  |> elem(1),
  :layout_kind_unknown
)

# A PLAN THAT DOES NOT FIT THE CALLER'S STORAGE is a refusal by name and never a
# growth — §3.3's rule for a resolved vocabulary, holding here unchanged.
Leg.eq(
  "a plan past the caller's capacity is `plan_too_large`",
  Tblfx2.FX2Fixed.fx_root_fixed_load(read.("fx1.bin"), plan_capacity: 1) |> elem(1),
  :plan_too_large
)

# BYTES LEFT OVER ARE `malformed`: the two ends of the file have met.
Leg.eq(
  "a ragged tail is `malformed`",
  Tblfx1.FX1Fixed.fx_root_fixed_load(read.("fx1.bin") <> <<1, 2, 3>>) |> elem(1),
  :malformed
)

# A RECORD WHOSE HASH NAMES NO LAYOUT THIS READER HOLDS IS `no_layout`: nothing
# is decoded, no counter moves, `malformed` does not fire.
{:error, why, no_layout_report} =
  Tblfx1.FX1Fixed.fx_root_fixed_load(
    IO.iodata_to_binary([
      Tblfx1.FixedRuntime.file_header(stated, byte_size(layout)),
      layout,
      <<0::little-unsigned-64, 0::size(22)-unit(8)>>
    ])
  )

Leg.eq("a hash naming no layout is `no_layout`", why, :no_layout)
Leg.eq("`no_layout` moves no counter", no_layout_report.malformed, false)

# A COUNT AND A LENGTH ARE CLAMPED TO THIS READER'S OWN BOUND, counting one
# `clamped` each — the `count` and `text` ops, on the identity path as well as
# the compiled one.
hostile =
  (fn ->
     [body | _] = bodies_of.(read.("p1.bin"), Tblp1.P1Fixed.chain_fixed_body_bytes())
     # name_length, the first four bytes of Chain's body, made absurd
     <<_::little-signed-32, rest::binary>> = body

     Tblp1.P1Fixed.chain_fixed_save([]) <>
       <<Tblp1.P1Fixed.chain_fixed_hash()::little-unsigned-64, 9999::little-signed-32,
         rest::binary>>
   end).()

{:ok, [clamped], clamp_report} = Tblp1.P1Fixed.chain_fixed_load(hostile)
Leg.eq("a hostile length is clamped to the declared bound", byte_size(clamped.name), 16)
Leg.check("the clamp is COUNTED", clamp_report.clamped >= 1)
Leg.eq("a clamp is not `malformed`", clamp_report.malformed, false)

Leg.verdict()
