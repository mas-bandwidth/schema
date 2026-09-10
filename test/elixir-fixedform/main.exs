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
# THE PAYLOAD RIDES WHOLE WHETHER OR NOT IT IS PRESENT (§3.4), and when the flag
# is 0 what rides is ZERO. The reference's own storage for this record carries
# 99 and "still" behind the false flag ON PURPOSE — that is the stain the
# oracle plants — and the FILE carries none of it, so an absent optional is a
# hole in the record and never a window into the writer's memory.
Leg.eq("p3[1] absent", {absent.name, absent.link_present}, {"absent", false})

Leg.eq(
  "p3[1].link behind a false flag is the template's zeros, not the writer's storage",
  {absent.link.value, absent.link.tag},
  {0, ""}
)

# AND THIS PORT'S OWN WRITER DOES THE SAME. The stain is put in the VALUE this
# time, which the C++ oracle cannot reach from here, and the wire must carry
# none of it — the same record with the flag SET does carry it, so the check is
# about the flag and not about a payload never written.
stained = %Tblp3.Chain{
  name: "absent",
  link_present: false,
  link: %Tblp3.Link{value: 99, tag: "still"}
}

Leg.bytes_eq(
  "an absent optional this port writes carries the template's zeros",
  Tblp3.P3Fixed.chain_fixed_save([stained]),
  Tblp3.P3Fixed.chain_fixed_save([%{stained | link: %Tblp3.Link{value: 0, tag: ""}}])
)

Leg.check(
  "and the SAME payload PRESENT does put those bytes on the wire",
  Tblp3.P3Fixed.chain_fixed_save([%{stained | link_present: true}]) !=
    Tblp3.P3Fixed.chain_fixed_save([%{stained | link_present: true, link: %Tblp3.Link{}}])
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

# ---------------------------------------------------------------------------
# THE PLAN CACHE IS BOUNDED, AND A CACHED PLAN IS HELD TO THE CALLER'S CAPACITY
# ---------------------------------------------------------------------------
#
# A LAYOUT COMES FROM A PEER, so an unbounded cache is a peer's lever on this
# node: every new layout is one `:persistent_term.put/2`, and every put makes
# EVERY PROCESS IN THE VM scan its heap. The cache fills once and then stops —
# past the bound a new peer's plan is compiled per load and written nowhere, so
# minting layouts buys an attacker no global scans at all.
Leg.section("the plan cache: bounded, forgettable, and held to the caller's capacity")

Leg.eq(
  "the cache states its bound",
  is_integer(Tblfx2.FixedRuntime.plan_cache_max()) and Tblfx2.FixedRuntime.plan_cache_max() > 0,
  true
)

# MINT MORE LAYOUTS THAN THE BOUND, each a real one this reader will compile
# against: FX1's own layout with the ROOT'S NOTE-FREE id left alone and one
# entry's id moved, which is a layout that parses and compiles to a plan.
fx1_layout = Tblfx1.FX1Fixed.fx_root_fixed_layout()

mint = fn i ->
  # entry 1's id lives at 4 (the header) + 17 (the root entry), eight bytes wide
  <<head::binary-size(21), _id::little-unsigned-64, tail::binary>> = fx1_layout
  <<head::binary, 0xF000 + i::little-unsigned-64, tail::binary>>
end

Tblfx2.FixedRuntime.forget(Tblfx2.FX2Fixed)

Leg.eq(
  "forget/1 empties the module's cache",
  :persistent_term.get({Tblfx2.FX2Fixed, :fixed_plans}, []),
  []
)

records = fn layout ->
  IO.iodata_to_binary([
    Tblfx1.FixedRuntime.file_header(Tblfx1.FixedRuntime.hash(layout), byte_size(layout)),
    layout,
    <<Tblfx1.FixedRuntime.hash(layout)::little-unsigned-64,
      0::size(Tblfx1.FX1Fixed.fx_root_fixed_body_bytes())-unit(8)>>
  ])
end

bound = Tblfx2.FixedRuntime.plan_cache_max()

for i <- 1..(bound + 8) do
  {:ok, _, _} = Tblfx2.FX2Fixed.fx_root_fixed_load(records.(mint.(i)))
end

Leg.eq(
  "a peer minting layouts fills the cache and no further",
  length(:persistent_term.get({Tblfx2.FX2Fixed, :fixed_plans}, [])),
  bound
)

# AND A READ PAST THE BOUND STILL ANSWERS: the plan is compiled per load and
# written nowhere, so the cost is an attacker's and never this node's memory.
{:ok, [past | _], past_report} = Tblfx2.FX2Fixed.fx_root_fixed_load(records.(mint.(9_999)))
Leg.eq("a read past the bound still lands its values", past.keep, 7)
Leg.eq("and moves no `malformed`", past_report.malformed, false)

Tblfx2.FixedRuntime.forget(Tblfx2.FX2Fixed)

# A CACHED PLAN IS REFUSED BY THE NUMBER THE COMPILER MEASURED, not by the
# length of the coalesced list — which is smaller, and would admit a caller
# that compiling fresh would have been told `plan_too_large`.
{:ok, _, _} = Tblfx2.FX2Fixed.fx_root_fixed_load(read.("fx1.bin"))

Leg.eq(
  "a cached plan is held to THIS caller's capacity",
  Tblfx2.FX2Fixed.fx_root_fixed_load(read.("fx1.bin"), plan_capacity: 1) |> elem(1),
  :plan_too_large
)

# ---------------------------------------------------------------------------
# WHAT A LOADED VALUE HOLDS: `copy: true` detaches a record from its file
# ---------------------------------------------------------------------------
#
# A record whose plan is the identity plan needs no image built — the body IS
# the image, a REFERENCE into the file — and the projection then binds each text
# field as a sub-binary of that. On the BEAM a sub-binary keeps its whole parent
# alive, so a small field lifted out of a big file holds the big file. That is
# the right default for a caller that uses the values and drops them; `copy:
# true` is for the other shape, and it has to land the SAME VALUES.
Leg.section("`copy: true`: the same values, detached from the file")

{:ok, shared, _} = Tblp1.P1Fixed.chain_fixed_load(read.("p1.bin"))
{:ok, copied, _} = Tblp1.P1Fixed.chain_fixed_load(read.("p1.bin"), copy: true)
Leg.eq("copy: true lands the same values", copied, shared)

Leg.bytes_eq(
  "and saves back to the same bytes",
  Tblp1.P1Fixed.chain_fixed_save(copied),
  read.("p1.bin")
)

# ---------------------------------------------------------------------------
# THE WRITE SIDE'S OWN CONTRACT: a caller's bug RAISES rather than riding
# ---------------------------------------------------------------------------
#
# The C++ reference spends a DEBUG-ONLY `schema_assert` on the same questions,
# which NDEBUG compiles out; the BEAM has no compile-out assert, so this port
# does what its own packet codec does — an O(1) check that raises. It is the
# debug-assert equivalent here and never the thing that keeps the wire safe:
# the READ side checks the same numbers in every build and counts the clamp.
Leg.section("the write side's contract: a caller's bug raises, it does not ride")

raises = fn name, f ->
  Leg.check(name, match?({:error, _}, try(do: {:ok, f.()}, rescue: (e -> {:error, e}))))
end

fu_one = fn over -> %Tblfu1.FuRoot{tail: 3, mark: over, heat: 0.0} end

raises.("an integer past its DECLARED STORAGE WIDTH raises rather than truncating", fn ->
  Tblfu1.FU1Fixed.fu_root_fixed_save([fu_one.(70_000)])
end)

raises.("and so does one below it", fn ->
  Tblfu1.FU1Fixed.fu_root_fixed_save([fu_one.(-70_000)])
end)

# AND A DECLARED RANGE IS A RANGE. FX1's `renamed` is `| min = 0, max = 1000`,
# which is narrower than its int32 storage: the reader CLAMPS a peer's value to
# it and counts one, and the writer refuses its own caller's.
raises.("an integer past its DECLARED RANGE raises where a peer's would clamp", fn ->
  Tblfx1.FX1Fixed.fx_root_fixed_save([%Tblfx1.FxRoot{renamed: 1001}])
end)

raises.("text past its declared bound raises", fn ->
  Tblfx1.FX1Fixed.fx_root_fixed_save([%Tblfx1.FxRoot{label: "far too long for eight"}])
end)

raises.("a counted array past its declared bound raises", fn ->
  Tblfx1.FX1Fixed.fx_root_fixed_save([%Tblfx1.FxRoot{marks: [1, 2, 3, 4, 5]}])
end)

# A `{:nonfinite, bits}` CARRYING A FINITE PATTERN is a caller who tagged an
# ordinary number, and the packet codec has always refused it. The fixed form's
# runtime had dropped that half of the contract.
raises.("a {:nonfinite, _} carrying a FINITE float32 pattern raises", fn ->
  Tblfu1.FU1Fixed.fu_root_fixed_save([
    %Tblfu1.FuRoot{tail: 3, mark: 0, heat: {:nonfinite, 0x3F800000}}
  ])
end)

# A PLAN COMPILED FROM A LAYOUT THAT IS MINE, BYTE FOR BYTE, MUST LAND EXACTLY
# WHAT THE IDENTITY PLAN LANDS. That is what reaches `count`, `text`, `ordinal`
# and the keyed walk, which the cross-schema cases above do not touch.
compiled_is_identity = fn name, layout, dst, plan, prefill, decode, values ->
  {:ok, mine} = Tblp1.FixedRuntime.parse_layout(layout)

  {:ok, made, _n, _} =
    Tblp1.FixedRuntime.compile(mine, mine, dst, 4096, Tblp1.FixedRuntime.report())

  run_both = fn body ->
    {img_a, ra} = Tblp1.FixedRuntime.run(plan, body, prefill, Tblp1.FixedRuntime.report())
    {img_b, rb} = Tblp1.FixedRuntime.run(made, body, prefill, Tblp1.FixedRuntime.report())

    {{img_a, ra.clamped + elem(decode.(img_a, 0), 1)},
     {img_b, rb.clamped + elem(decode.(img_b, 0), 1)}}
  end

  both = Enum.map(values, run_both)

  Leg.check(
    "#{name}: a plan compiled from MY OWN layout lands what the identity plan lands",
    Enum.all?(both, fn {{a, _}, {b, _}} -> decode.(a, 0) == decode.(b, 0) end)
  )

  # THE COUNTER MOVES ON BOTH PATHS, and the identity path never moves it MORE.
  # THE IDENTITY PATH CHECKS IN THE PROJECTION, which walks only what a read can
  # have written — a counted array's LIVE elements and never its slack, the
  # reference's own rule (docs/SPEC-TABLES.md §3.4: slack is unspecified on
  # read and moves no counter). A COMPILED plan checks in the interpreter, whose
  # entries are static and so cover EVERY slot of a run, slack included; over a
  # clean body the two agree exactly, and over a hostile one the compiled plan
  # can only count more, never less.
  #
  # A HOSTILE BODY RIDES BESIDE THE REAL ONES: every byte 0xFF, so every
  # declared bound is crossed at once. A corpus whose values are all in range
  # would let both sides count nothing and call it agreement.
  hostile = :binary.copy(<<0xFF>>, byte_size(prefill))
  counted = both ++ [run_both.(hostile)]

  Leg.check(
    "#{name}: the identity path counts the `clamped` a compiled plan counts on a clean body",
    Enum.all?(both, fn {{_, a}, {_, b}} -> a == b end)
  )

  Leg.check(
    "#{name}: and never more than a compiled plan counts over slack",
    Enum.all?(counted, fn {{_, a}, {_, b}} -> a <= b end)
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
  &Tabledemo.PackFixed.pack_config_fixed_decode/2,
  bodies_of.(read.("pack.bin"), Tabledemo.PackFixed.pack_config_fixed_body_bytes())
)

compiled_is_identity.(
  "keyed",
  Tabledemo.KeyedFixed.keyed_config_fixed_layout(),
  Tabledemo.KeyedFixed.keyed_config_fixed_dst(),
  Tabledemo.KeyedFixed.keyed_config_fixed_plan(),
  Tabledemo.KeyedFixed.keyed_config_fixed_prefill(),
  &Tabledemo.KeyedFixed.keyed_config_fixed_decode/2,
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

# TEXT INSIDE A UNION ARM, read BOTH WAYS (docs/SPEC-TABLES.md §3.4, §15).
#
# A plan entry for a `string(N)` that sits inside a union arm carries TWO
# INDEPENDENT FACTS: which arm's tag guards it, and which flavour of text it is
# (utf8, wide, bytes). A port that spends ONE lane on both loses whichever was
# written second, and the loss is silent — no counter, no refusal. It went
# wrong exactly that way on another leg (reference-fix 12): a `string(8)` in a
# union's SECOND arm read as "" through a compiled plan while the identity read
# landed the text.
#
# THIS PORT KEEPS THEM APART BY SHAPE and not by convention: the arm's tag
# rides in the `{:guard, src, tag, inner}` wrapper the entry is nested in, and
# the flavour is the entry's own last element. FU1/FU2 is the pin that says so
# — the same two records read once through the IDENTITY plan and once through a
# plan COMPILED from FU1's layout, and they have to agree field for field.
Leg.section("text under a union arm: the identity read and the compiled read agree")

fu =
  Tblfu1.FU1Fixed.fu_root_fixed_save([
    %Tblfu1.FuRoot{
      pick: %Tblfu1.Pick{
        type: 2,
        labelled: %Tblfu1.Labelled{lead: 5, label: "hello", trail: 9}
      },
      tail: 3,
      mark: -12_345,
      # A SIGNALLING NaN: exponent all ones, payload 1, and the QUIET BIT CLEAR.
      # No BEAM float term holds it, which is why it travels as {:nonfinite, _}.
      heat: {:nonfinite, 0x7F800001}
    },
    %Tblfu1.FuRoot{
      pick: %Tblfu1.Pick{type: 1, plain: %Tblfu1.Plain{n: 7}},
      tail: 4,
      mark: 12_345,
      heat: 1.5
    }
  ])

{:ok, [i_arm2, i_arm1], fu_ident} = Tblfu1.FU1Fixed.fu_root_fixed_load(fu)
{:ok, [c_arm2, c_arm1], fu_comp} = Tblfu2.FU2Fixed.fu_root_fixed_load(fu)

# THE SECOND ARM, field by field. `lead` and `trail` sit either side of the
# text on purpose: a mislaid length or a mislaid guard moves a neighbour too.
Leg.eq("arm 2, identity: the tag", i_arm2.pick.type, 2)
Leg.eq("arm 2, compiled: the tag", c_arm2.pick.type, 2)
Leg.eq("arm 2, identity: lead before the text", i_arm2.pick.labelled.lead, 5)
Leg.eq("arm 2, compiled: lead before the text", c_arm2.pick.labelled.lead, 5)
Leg.eq("arm 2, identity: the text's length", byte_size(i_arm2.pick.labelled.label), 5)
Leg.eq("arm 2, compiled: the text's length", byte_size(c_arm2.pick.labelled.label), 5)
Leg.eq("arm 2, identity: the text", i_arm2.pick.labelled.label, "hello")
Leg.eq("arm 2, compiled: the text", c_arm2.pick.labelled.label, "hello")
Leg.eq("arm 2, identity: trail after the text", i_arm2.pick.labelled.trail, 9)
Leg.eq("arm 2, compiled: trail after the text", c_arm2.pick.labelled.trail, 9)
Leg.eq("arm 2, identity: the tail past the union", i_arm2.tail, 3)
Leg.eq("arm 2, compiled: the tail past the union", c_arm2.tail, 3)
# a field the writer does not carry has no plan entry, so the PREFILL lands it
Leg.eq("arm 2, compiled: `extra` takes its declared default", c_arm2.extra, 11)

# AND THE FIRST ARM, which is right by accident wherever the second is wrong:
# an arm ordinal of 1 survives being confused with a utf8 flavour of 1.
Leg.eq("arm 1, identity", {i_arm1.pick.type, i_arm1.pick.plain.n, i_arm1.tail}, {1, 7, 4})
Leg.eq("arm 1, compiled", {c_arm1.pick.type, c_arm1.pick.plain.n, c_arm1.tail}, {1, 7, 4})
Leg.eq("arm 1, compiled: `extra` takes its declared default", c_arm1.extra, 11)

Leg.eq("text under an arm moves no counter, identity", fu_ident.clamped, 0)
Leg.eq("text under an arm moves no counter, compiled", fu_comp.clamped, 0)
Leg.eq("nothing was damaged either way", {fu_ident.malformed, fu_comp.malformed}, {false, false})

# THE SIGNED WIDENING RUNG (SPEC §4). FU1 declares `mark` an `int16` and FU2 an
# `int32`, so FU2's COMPILED plan reads the writer's two bytes SIGN-EXTENDED and
# re-images them at four. A NEGATIVE value is the whole test: an unsigned read of
# the same two bytes lands 53_191 and a rung that only carried positives would
# never say so.
Leg.eq("the signed rung, identity: int16 stays int16", i_arm2.mark, -12_345)
Leg.eq("the signed rung, compiled: int16 -> int32 keeps the SIGN", c_arm2.mark, -12_345)
Leg.eq("the signed rung: and a positive value is unmoved", c_arm1.mark, 12_345)
# TWO RUNGS, TWO RECORDS: the signed one and the float one below, each counted.
Leg.eq("a rung is COUNTED, once per rung per record", fu_comp.widened, 4)
Leg.eq("the identity read widens nothing", fu_ident.widened, 0)

# AND THE FLOAT RUNG, f32 into f64 — where the reference is a `(double) f` and
# every hardware convert QUIETS A SIGNALLING NaN: the payload is carried into
# the wide mantissa and the quiet bit is SET. Preserving the signalling state
# would put a pattern on this side of the rung the reference cannot produce.
Leg.eq(
  "the float rung, identity: the sNaN pattern is untouched",
  i_arm2.heat,
  {:nonfinite, 0x7F800001}
)

Leg.eq(
  "the float rung, compiled: f32 -> f64 QUIETS an sNaN, as `(double) f` does",
  c_arm2.heat,
  {:nonfinite, 0x7FF8000020000000}
)

# THE CONTROL: carrying the signalling state instead would land this, which is
# the pattern the reference cannot produce and the one this rung used to make.
Leg.check(
  "the float rung: and it is NOT the signalling pattern",
  c_arm2.heat != {:nonfinite, 0x7FF0000020000000}
)

# A FINITE FLOAT IS NOT A NaN and keeps its zero payload: an infinity quieted
# would be a NaN nobody wrote, and an ordinary number just widens.
Leg.eq("the float rung: a finite value widens exactly", c_arm1.heat, 1.5)

# ---------------------------------------------------------------------------
# WHAT A HOSTILE ORDINAL, A HOSTILE FLAG AND A HOSTILE BOOL LAND AS
# ---------------------------------------------------------------------------
#
# A NUMBER OUTSIDE ITS DECLARED SET IS HELD, NOT REINTERPRETED, and the holding
# is COUNTED so the caller can see it happened. FE1's body is ten bytes — the
# grade ordinal, the union tag, the arm, the tail — so the two hostile bytes go
# in by hand rather than through a value surface that could not express them.
Leg.section("hostile ordinals, flags and bools: what each one lands as")

fe_file = fn body ->
  Tblfe1.FE1Fixed.fe_root_fixed_save([]) <>
    <<Tblfe1.FE1Fixed.fe_root_fixed_hash()::little-unsigned-64, body::binary>>
end

# grade, tag, the arm's int32, the tail
fe_body = fn grade, tag ->
  <<grade::unsigned-8, tag::unsigned-8, 41::little-signed-32, 99::little-signed-32>>
end

# A UNION TAG BEYOND THE DECLARED ARM COUNT lands None and counts one `clamped`
# on the identity path: the tag is an ordinal like any other and 0 is None.
{:ok, [t0], tag_report} = Tblfe1.FE1Fixed.fe_root_fixed_load(fe_file.(fe_body.(3, 9)))
Leg.eq("a tag past the last arm lands None", t0.effect.type, 0)
Leg.eq("and COUNTS one clamped", tag_report.clamped, 1)
Leg.eq("the tail past the union still lands", t0.tail, 99)
Leg.eq("a held tag is not damage", tag_report.malformed, false)

# ON A COMPILED PATH a tag that matches no arm OF THE WRITER'S fires no entry
# at all, so the prefill's None is what stays and nothing is counted. That is a
# different event from the clamp above, and it is the correct one.
{:ok, [t1], compiled_tag} = Tblfe2.FE2Fixed.fe_root_fixed_load(fe_file.(fe_body.(3, 9)))
Leg.eq("compiled: a tag matching no arm of the writer's lands None", t1.effect.type, 0)
Leg.eq("compiled: and fires no entry, so nothing is counted", compiled_tag.clamped, 0)

# AN ENUM ORDINAL PAST THE LAST VARIANT lands 0 (None) and counts one
# `clamped`, on BOTH paths — the identity plan holds it against the variant
# count it was built from, a compiled plan against the writer's own table.
{:ok, [e0], enum_report} = Tblfe1.FE1Fixed.fe_root_fixed_load(fe_file.(fe_body.(99, 2)))
Leg.eq("identity: an ordinal past the last variant lands None", e0.grade, 0)
Leg.eq("identity: and COUNTS one clamped", enum_report.clamped, 1)
Leg.eq("identity: the arm behind a good tag is undisturbed", e0.effect.ward.charge, 41)

{:ok, [e1], compiled_enum} = Tblfe2.FE2Fixed.fe_root_fixed_load(fe_file.(fe_body.(99, 2)))
Leg.eq("compiled: an ordinal past the last variant lands None", e1.grade, 0)
Leg.eq("compiled: and COUNTS one clamped", compiled_enum.clamped, 1)

# ORDINAL 0 IS None AND IS NOT A CLAMP, which is what keeps the counter honest.
{:ok, [z0], none_report} = Tblfe1.FE1Fixed.fe_root_fixed_load(fe_file.(fe_body.(0, 0)))
Leg.eq("None is a value and not a clamp", {z0.grade, z0.effect.type}, {0, 0})
Leg.eq("so nothing is counted", none_report.clamped, 0)

# A PRESENT BYTE AND A BOOL BYTE ARE BOTH `!= 0` AND NEVER `== 1`, which is
# what a form that writes 0 or 1 and reads anything owes a hostile writer. Both
# land through the PROJECTION, which the identity and compiled paths share, so
# planting the byte once reaches both. The offset is found by writing the same
# value twice with only that flag moved, so no offset is hard-coded here.
only_diff = fn a, b ->
  Enum.find(0..(byte_size(a) - 1)//1, fn i -> :binary.at(a, i) != :binary.at(b, i) end)
end

plant = fn data, at, byte ->
  <<binary_part(data, 0, at)::binary, byte::unsigned-8,
    binary_part(data, at + 1, byte_size(data) - at - 1)::binary>>
end

absent_bytes = Tblp3.P3Fixed.chain_fixed_save([%{present | link_present: false}])
present_bytes = Tblp3.P3Fixed.chain_fixed_save([%{present | link_present: true}])
present_at = only_diff.(absent_bytes, present_bytes)

{:ok, [seven], _} = Tblp3.P3Fixed.chain_fixed_load(plant.(absent_bytes, present_at, 7))
Leg.eq("a present byte of 7 LANDS PRESENT — the test is `!= 0`", seven.link_present, true)

{:ok, [zero], _} = Tblp3.P3Fixed.chain_fixed_load(plant.(present_bytes, present_at, 0))
Leg.eq("and only 0 is absent", zero.link_present, false)

track = fn v, on ->
  ships =
    List.update_at(v.ships, 0, fn ship -> %{ship | gunner: %{ship.gunner | tracking: on}} end)

  %{v | ships: ships}
end

bool_off = Tabledemo.PackFixed.pack_config_fixed_save([track.(p0, false)])
bool_on = Tabledemo.PackFixed.pack_config_fixed_save([track.(p0, true)])
bool_at = only_diff.(bool_off, bool_on)

{:ok, [two], _} = Tabledemo.PackFixed.pack_config_fixed_load(plant.(bool_off, bool_at, 2))
Leg.eq("a bool byte of 2 LANDS TRUE", Enum.at(two.ships, 0).gunner.tracking, true)

{:ok, [off], _} = Tabledemo.PackFixed.pack_config_fixed_load(plant.(bool_on, bool_at, 0))
Leg.eq("and only 0 is false", Enum.at(off.ships, 0).gunner.tracking, false)

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

{wrong, _} = Tblfx2.FX2Fixed.fx_root_fixed_decode(wrong_image, 0)

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
#
# THE BODY IS THE DECLARED ONE and its length is asked for rather than typed:
# a record short of `body_bytes` is a RAGGED TAIL and refuses as `malformed`
# before the hash is ever looked up, so a hand-written length is a test that
# silently stops testing `no_layout` the day a field joins FxRoot.
{:error, why, no_layout_report} =
  Tblfx1.FX1Fixed.fx_root_fixed_load(
    IO.iodata_to_binary([
      Tblfx1.FixedRuntime.file_header(stated, byte_size(layout)),
      layout,
      <<0::little-unsigned-64, 0::size(Tblfx1.FX1Fixed.fx_root_fixed_body_bytes())-unit(8)>>
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
Leg.check("the clamp is COUNTED", clamp_report.clamped >= 1)

# AND THEN THE CONTENT RULE FIRES ON WHAT THE CLAMP LEFT, which is the
# reference's own order: the length is held to the declared bound FIRST, and the
# used units are checked SECOND. `chain-one` padded out to sixteen bytes carries
# NULs, and a `string(N)`'s bytes have no zero among them (§3) — so the field is
# damage, it reads its DECLARED DEFAULT, and the rest of the record stands.
Leg.eq("the clamped span is then held to the CONTENT rule", clamped.name, "")
Leg.eq("ill-formed text fires the one damage flag", clamp_report.malformed, true)
Leg.eq("and the rest of the record stands", clamped.link.value, 77)

# A CLAMP ON ITS OWN IS NOT DAMAGE. The same hostile length over a field whose
# whole declared bound IS well-formed text: the clamp counts and nothing else
# moves, which is what makes the case above discriminating.
full =
  (fn ->
     saved =
       Tblp1.P1Fixed.chain_fixed_save([
         %Tblp1.Chain{name: "sixteen-bytes!!!", link: %Tblp1.Link{value: 77, tag: "tagged"}}
       ])

     head = binary_part(saved, 0, byte_size(saved) - Tblp1.P1Fixed.chain_fixed_body_bytes())
     body = binary_part(saved, byte_size(head), Tblp1.P1Fixed.chain_fixed_body_bytes())
     <<_::little-signed-32, rest::binary>> = body
     head <> <<9999::little-signed-32, rest::binary>>
   end).()

{:ok, [full_back], full_report} = Tblp1.P1Fixed.chain_fixed_load(full)
Leg.eq("a clamp alone lands the whole declared bound", full_back.name, "sixteen-bytes!!!")
Leg.check("and is COUNTED", full_report.clamped >= 1)
Leg.eq("a clamp is not `malformed`", full_report.malformed, false)

# ---------------------------------------------------------------------------
# THE ONE CONTENT RULE THE WIRE HAS (§3, §4), on this form's terms
# ---------------------------------------------------------------------------
#
# A `string(N)`'s used bytes are well-formed UTF-8 with no zero among them. A
# payload that is not the text its kind says it is is DAMAGE and not data, and
# the verdict here is not the packet wire's: SPEC.md §4.7 refuses the WHOLE
# read, because a packet has no position after a field that did not decode; a
# FIXED RECORD IS POSITIONAL, so the damage is ONE FIELD's — it reads its
# declared default, one `malformed` fires, and the rest of the record stands.
#
# THE POISON IS WRITTEN THROUGH THE WRITER, because it can be: the write side's
# bounds are the used length and the live count, and neither is a content rule.
Leg.section("ill-formed text: the field reads its default, the record stands")

poison = fn bytes ->
  Tblfx1.FX1Fixed.fx_root_fixed_save([
    %Tblfx1.FxRoot{keep: 1234, nested: %Tblfx1.FxNested{a: 7, b: 2}, label: bytes}
  ])
end

for {bytes, what} <- [
      {<<0xFF>>, "a lone 0xFF is not text"},
      {<<0xE2, 0x82>>, "a truncated three-byte sequence is not text"},
      {<<0xA9>>, "a bare continuation byte is not text"},
      {<<0xC0, 0x80>>, "an overlong NUL is not text"},
      {<<?a, 0, ?b>>, "an interior null is not text"}
    ] do
  {:ok, [back], r} = Tblfx1.FX1Fixed.fx_root_fixed_load(poison.(bytes))
  Leg.eq(what, r.malformed, true)
  Leg.eq("#{what}: the field reads its DECLARED DEFAULT", back.label, "fx")
  Leg.eq("#{what}: and the rest of the record stands", {back.keep, back.nested.a}, {1234, 7})
end

# AND WELL-FORMED TEXT IS UNTOUCHED, which is what makes the five above
# discriminating: multi-byte UTF-8 inside the bound reads back whole.
{:ok, [ete], ete_report} = Tblfx1.FX1Fixed.fx_root_fixed_load(poison.("été"))
Leg.eq("well-formed multi-byte UTF-8 rides whole", ete.label, "été")
Leg.eq("and moves no counter", {ete_report.malformed, ete_report.clamped}, {false, 0})

# THE NEGATIVE CONTROL: the read loop with the content rule taken out of the
# `text` op, which is what this leg did before. The same record, the same plan,
# and the byte that is not text stands in the image with nothing said.
loose_plan =
  Enum.map(Tblfx1.FX1Fixed.fx_root_fixed_plan(), fn
    {:text, src, dst, aux, size, _flavour} -> {:text, src, dst, aux, size, 3}
    entry -> entry
  end)

[poison_body | _] = bodies_of.(poison.(<<0xFF>>), Tblfx1.FX1Fixed.fx_root_fixed_body_bytes())

{loose_image, loose_report} =
  Tblfx1.FixedRuntime.run(
    loose_plan,
    poison_body,
    Tblfx1.FX1Fixed.fx_root_fixed_prefill(),
    Tblfx1.FixedRuntime.report()
  )

Leg.eq(
  "NEGATIVE CONTROL: the loop with no content rule leaves the byte standing",
  Tblfx1.FX1Fixed.fx_root_fixed_decode(loose_image).label,
  <<0xFF>>
)

Leg.eq("NEGATIVE CONTROL: and says nothing about it", loose_report.malformed, false)

# THE OVERLOADED LANE, PLANTED BY HAND. The bug this leg is pinned against is a
# plan entry that spends ONE lane on both the arm ordinal and the text flavour.
# This port cannot spell that — the two live in different tuples — so the
# control WRITES IT ANYWAY: the arm's tag copied over the flavour element of
# the `text` entry, which is what the overloaded encoding would have produced.
# A tag of 2 read as a flavour is `wide`, whose unit is two bytes, so the
# length is held to four and the text comes back short. IF THE TWO FACTS EVER
# SHARE A LANE AGAIN, THIS IS WHAT THE READ WOULD DO.
#
# THE PLAN SABOTAGED IS ONE COMPILED FROM FU1's OWN LAYOUT, not the identity
# plan: the identity plan is ONE copy and carries no `text` entry at all (the
# identity read is a projection and never a plan), so the entry with a
# flavour lane to overload only exists on the compiled side.
[fu_body | _] = bodies_of.(fu, Tblfu1.FU1Fixed.fu_root_fixed_body_bytes())
{:ok, fu_mine} = Tblfu1.FixedRuntime.parse_layout(Tblfu1.FU1Fixed.fu_root_fixed_layout())

{:ok, fu_compiled, _, _} =
  Tblfu1.FixedRuntime.compile(
    fu_mine,
    fu_mine,
    Tblfu1.FU1Fixed.fu_root_fixed_dst(),
    4096,
    Tblfu1.FixedRuntime.report()
  )

Leg.check(
  "the compiled plan carries the text entry under the arm's guard",
  Enum.any?(fu_compiled, &match?({:guard, _, _, {:text, _, _, _, _, _}}, &1))
)

overloaded =
  Enum.map(fu_compiled, fn
    {:guard, g, tag, {:text, src, dst, aux, size, _flavour}} ->
      {:guard, g, tag, {:text, src, dst, aux, size, tag}}

    entry ->
      entry
  end)

Leg.check(
  "an overloaded flavour lane does NOT reproduce the arm's text",
  (fn ->
     {image, _} =
       Tblfu1.FixedRuntime.run(
         overloaded,
         fu_body,
         Tblfu1.FU1Fixed.fu_root_fixed_prefill(),
         Tblfu1.FixedRuntime.report()
       )

     elem(Tblfu1.FU1Fixed.fu_root_fixed_decode(image, 0), 0).pick.labelled.label
   end).() != "hello"
)

# AND THE UNCHECKED ORDINAL, planted the same way: an ordinal that rides as a
# plain COPY is an ordinal the PLAN does not hold. The projection holds it
# anyway — a grade of 99 names no variant, so it lands None and counts one
# `clamped` — because the identity path has no plan op to lean on and the
# check has to live where both paths meet.
unchecked =
  Enum.map(Tblfe1.FE1Fixed.fe_root_fixed_plan(), fn
    {:ordinal, src, dst, size, _width, _variants} -> {:copy, src, dst, size}
    entry -> entry
  end)

{loose_image, loose_report} =
  Tblfe1.FixedRuntime.run(
    unchecked,
    fe_body.(99, 2),
    Tblfe1.FE1Fixed.fe_root_fixed_prefill(),
    Tblfe1.FixedRuntime.report()
  )

{loose, loose_count} = Tblfe1.FE1Fixed.fe_root_fixed_decode(loose_image, 0)

Leg.check(
  "an ordinal riding as a plain copy is held by the PROJECTION, not the plan",
  loose.grade == 0 and loose_report.clamped == 0 and loose_count == 1
)

Leg.verdict()
