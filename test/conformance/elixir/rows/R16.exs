# Card cell3-elixir-r16 — the R16 cell of the elixir row group: §5.4's counters
# exactly (docs/FIXED-FORM-ALGORITHM.md:939-947).
#
# THE LAW (§5.4, docs/FIXED-FORM-ALGORITHM.md:939):
#   | where | counter | when |
#   | MATCH, at COMPILE | unknown | once per writer field no reader field names,
#   |                   |         | ONCE PER PEER AND NEVER PER RECORD |
#   | widen, widenf | widened | once per entry per record; a folded element run
#   |                |         | is ONE and an unfolded one is min(their_n, my_n),
#   |                |         | and a widened run is never folded (§5.9 #33) |
#   | count, text | clamped | once per entry per record, when the length or count
#   |             |         | was out of range |
#   | the bounds pass | clamped | a FORGED ordinal remapped to None, on the
#   |                 |         | COMPILED plan exactly as on the identity one |
#   | copy, const, present, ordinal | none | the op lands its value and moves
#   |                               | nothing; the remap is the BOUNDS PASS's |
#
# Every clause is asserted with the EXACT number, never >= : a reader that
# censused per record, folded a widened run, or counted the forged ordinal in
# the ordinal op as well lands a different number and goes red.
#
# THE VECTORS ARE THE GENERATED WRITERS' OWN OUTPUT (the production path, not a
# hand-derivation — the cpp leg's R16.cpp is the same shape). The corpus oracle
# (build/fixedform-corpus) is not built on this bench, and the versioning pairs
# live in the tree as SCHEMAS (test/tables/VOLD_<row>.schema,
# VNEW_<row>.schema), so this file compiles what it needs:
#
#   * the OLD side of each pair is generated straight from its committed
#     schema; its `_fixed_save/1` lays down the OLD layout's form-3 file;
#   * the NEW side is generated against a schema.lock this file BUILDS, so the
#     NEW reader's module carries the OLD layout as a lineage entry and the
#     read goes through the COMPILED plan. For a LAWFUL pair (every row but
#     unknown_census) the lock is the CLI's own two-step: lock the OLD content,
#     lock the NEW content, the monotone law (§5.1) accepts the append/widen.
#     unknown_census is §5.8 row 11, a DELIBERATELY UNLAWFUL pair (a field
#     removal, which §5.1 refuses and `schema lock` refuses too — see the
#     schema's own header), so its lock is SPLICED here: the OLD print's
#     lineage lines are folded into the NEW print ahead of the NEW entries and
#     the `lineage=` roll-up is recomputed (fnv1a64 over the roll-up text,
#     internal/lockfile/lineage.go's LineageHash). The OLD schema's package is
#     renamed to the NEW package wherever a lock is involved, because a lock
#     belongs to ONE package and the layout bytes the lock records are
#     package-independent (the wire hash is over layout bytes alone).
#
# THE PRODUCTION PATH UNDER TEST: `<root>_fixed_save/1` and
# `<root>_fixed_load/2` in the generated fixed surfaces, reached exactly the
# way test/conformance/elixir/driver_impl.ex reaches generated code — the .ex
# files are required, and the module exporting the root's reader is found BY
# SEARCH. A load runs R.read_file_header → R.select (the lineage match, §5.3)
# → R.lineage_lane (the plan compiled at MODULE LOAD from the lock's bytes,
# §5.9 #3) → R.run per record → R.census ONCE after the loop (§5.9 #6, the
# COMPILE half of the unknown clause).
#
# Run from the repository root:  elixir test/conformance/elixir/rows/R16.exs
# Exit 0 green, 1 red, one printed line per assertion. Depends on no other
# rows/ file and edits no shared file. Generation output goes to
# build/tmp-r16/ and is regenerated each run; R16_CACHE=1 reuses it (the
# card's control: break the one generated-code constant the law governs —
# a byte of the baked lineage in build/tmp-r16 — and this file goes red).

repo = File.cwd!()
bin = Path.join(repo, "bin/schema")
tmp = Path.join(repo, "build/tmp-r16")

unless File.exists?(bin) do
  IO.puts(:stderr, "FAIL: #{bin} is not built — run `make build/conformance-harness` first")
  System.halt(1)
end

defmodule R16.Lock do
  @moduledoc false

  # fnv1a64 over the text: the lock's one hash (ir.TableWireId,
  # internal/lockfile/lineage.go's LineageHash digests the roll-up text with it).
  @basis 0xCBF29CE484222325
  @prime 0x00000100000001B3
  @mask 0xFFFFFFFFFFFFFFFF

  def fnv1a64(text) do
    for(<<b <- text>>, do: b)
    |> Enum.reduce(@basis, fn b, h -> rem(Bitwise.bxor(h, b) * @prime, @mask + 1) end)
  end

  def hex16(v), do: v |> Integer.to_string(16) |> String.downcase() |> String.pad_leading(16, "0")

  # one lock text's per-table blocks: {table_name, header_line, body_lines}
  def blocks(text) do
    text
    |> String.split("\n")
    |> Enum.drop_while(fn l -> not String.starts_with?(l, "fixed table ") end)
    |> Enum.chunk_while([], fn
      "fixed table " <> _ = l, [] -> {:cont, [l]}
      "fixed table " <> _ = l, acc -> {:cont, Enum.reverse(acc), [l]}
      l, acc -> {:cont, [l | acc]}
    end, fn
      [] -> {:cont, []}
      acc -> {:cont, Enum.reverse(acc), []}
    end)
    |> Enum.reject(&(&1 == []))
    |> Enum.map(fn [head | body] ->
      [_, _, name | _] = String.split(head, " ")
      {name, head, body}
    end)
  end

  def lineage_lines(body), do: Enum.filter(body, &String.match?(&1, ~r/^\s+lineage wire=/))

  def wires(body) do
    for l <- lineage_lines(body) do
      [_, "wire=0x" <> hex | _] = String.split(String.trim(l), " ")
      String.downcase(hex)
    end
  end

  # internal/lockfile/lineage.go's LineageHash: one fnv1a64 over
  # `lineage wire=0x%016x[ retired]\n` per entry, in order.
  def rollup(wire_hexes) do
    fnv1a64(Enum.map_join(wire_hexes, "", fn hex -> "lineage wire=0x" <> hex <> "\n" end))
  end

  # The splice for §5.8 row 11's UNLAWFUL pair: the NEW print is the live
  # projection and stays verbatim but for the roll-up; the OLD print's lineage
  # lines go in AHEAD of the NEW ones (oldest first, the current layout last —
  # §5.2), per fixed table. A v7 lock's self-consistency checks (layout= over
  # the field lines, lineage= over the lineage lines, the live layout the LAST
  # entry) all hold of the result by construction.
  def splice(old_print, new_print) do
    old_by_name = Map.new(blocks(old_print), fn {name, head, body} -> {name, {head, body}} end)

    out_blocks =
      for {name, head, body} <- blocks(new_print) do
        {_old_head, old_body} = Map.fetch!(old_by_name, name)
        old_lins = lineage_lines(old_body)
        new_lins = lineage_lines(body)
        roll = rollup(wires(old_body) ++ wires(body))
        head = String.replace(head, ~r/lineage=0x[0-9a-f]+/, "lineage=0x" <> hex16(roll))
        fields = Enum.reject(body, &(&1 in new_lins or String.trim(&1) == ""))
        [head | fields ++ old_lins ++ new_lins]
      end

    header =
      new_print
      |> String.split("\n")
      |> Enum.take_while(fn l -> not String.starts_with?(l, "fixed table ") end)
      |> Enum.reject(fn l -> String.trim(l) == "" end)

    Enum.join(header ++ Enum.flat_map(out_blocks, fn block -> ["" | block] end), "\n") <> "\n"
  end
end

defmodule R16.Gen do
  @moduledoc false

  def run!(cmd, args) do
    case System.cmd(cmd, args, stderr_to_stdout: true) do
      {out, 0} -> out
      {out, rc} -> raise "FAIL: #{cmd} #{Enum.join(args, " ")} exited #{rc}: #{out}"
    end
  end

  def read!(path), do: File.read!(path)

  # The OLD schema's package renamed to the NEW package: a lock belongs to one
  # package, and the layout bytes the lock records are package-independent.
  def repackaged(text, new_package) do
    String.replace(text, ~r/^package \S+$/m, "package " <> new_package)
  end

  # the OLD writer: generated straight from the committed schema, no lock
  def old(bin, tmp, row) do
    out = Path.join(tmp, row <> "/old")
    run!(bin, ["generate", "--lang", "elixir", "--out", out,
               Path.join("test/tables", "VOLD_" <> row <> ".schema")])
    out
  end

  # the NEW reader for a LAWFUL pair: the CLI's own two-step lock — lock the
  # OLD content, lock the NEW content; §5.1's monotone law accepts the
  # append/widen, so the second lock appends the NEW entry to the OLD one.
  def new_lawful(bin, tmp, row) do
    dir = Path.join(tmp, row <> "/lock")
    File.mkdir_p!(dir)
    schema = Path.join(dir, "pair.schema")
    File.write!(schema, repackaged(read!(Path.join("test/tables", "VOLD_" <> row <> ".schema")), "vnew_" <> row))
    run!(bin, ["lock", schema])
    File.write!(schema, read!(Path.join("test/tables", "VNEW_" <> row <> ".schema")))
    run!(bin, ["lock", schema])
    out = Path.join(tmp, row <> "/new")
    run!(bin, ["generate", "--lang", "elixir", "--out", out, schema])
    out
  end

  # the NEW reader for §5.8 row 11's UNLAWFUL pair: the lock is spliced (R16.Lock).
  def new_unlawful(bin, tmp, row) do
    dir = Path.join(tmp, row <> "/lock")
    File.mkdir_p!(dir)
    old_schema = Path.join(dir, "old.schema")
    schema = Path.join(dir, "pair.schema")
    File.write!(old_schema, repackaged(read!(Path.join("test/tables", "VOLD_" <> row <> ".schema")), "vnew_" <> row))
    File.write!(schema, read!(Path.join("test/tables", "VNEW_" <> row <> ".schema")))
    old_print = run!(bin, ["lock", "--print", old_schema])
    new_print = run!(bin, ["lock", "--print", schema])
    File.write!(Path.join(dir, "schema.lock"), R16.Lock.splice(old_print, new_print))
    out = Path.join(tmp, row <> "/new")
    run!(bin, ["generate", "--lang", "elixir", "--out", out, schema])
    out
  end

  # require one generated dir's .ex files, runtime first (a module a later one
  # calls at COMPILE time must already be loaded, and the lineage's plans are
  # built at module load — the Go probe's sortedRuntimeFirst, §5.9 #3).
  def require!(dir) do
    files = Path.wildcard(Path.join(dir, "*.ex"))
    {runtime, rest} = Enum.split_with(files, &(Path.basename(&1) in ["FixedRuntime.ex", "BuildVersion.ex"]))
    {fixed, packet} = Enum.split_with(rest, &String.ends_with?(&1, "Fixed.ex"))

    for f <- Enum.sort(runtime) ++ Enum.sort(packet) ++ Enum.sort(fixed) do
      Code.require_file(f)
    end
  end

  # the module exporting the root's reader, found BY SEARCH under the unit's
  # namespace — the driver's contract (driver_impl.ex: "dispatch is by SEARCH,
  # not by a generated index").
  def fixed_mod!(ns_camel, fn_prefix) do
    want = String.to_atom(fn_prefix <> "_fixed_load")

    mod =
      Enum.find_value(:code.all_loaded(), fn {mod, _} ->
        name = Atom.to_string(mod)

        if String.starts_with?(name, "Elixir." <> ns_camel <> ".") and
             function_exported?(mod, want, 2) do
          mod
        end
      end)

    mod || raise "FAIL: no module under #{ns_camel} exports #{want}/2"
  end
end

# ---- the corpus this file compiles -----------------------------------------
#
# Every pair: the OLD writer's module (identity lineage) and the NEW reader's
# module (the OLD layout in its lineage, so the read is the COMPILED plan).

rows = [
  # %{row, root, fn_prefix, unlawful?}
  %{row: "unknown_census", root: "Census", fun: "census", unlawful: true},
  %{row: "int_widen", root: "IntWiden", fun: "int_widen", unlawful: false},
  %{row: "array_elem_widen", root: "ArrayElemWiden", fun: "array_elem_widen", unlawful: false},
  %{row: "enum_width", root: "Lineage", fun: "lineage", unlawful: false},
  %{row: "array_bounded_grow", root: "ArrayBoundedGrow", fun: "array_bounded_grow", unlawful: false},
  %{row: "string_grow", root: "StringGrow", fun: "string_grow", unlawful: false},
  %{row: "enum_append", root: "Lineage", fun: "lineage", unlawful: false},
  %{row: "field_append", root: "Lineage", fun: "lineage", unlawful: false},
  %{row: "union_append", root: "Lineage", fun: "lineage", unlawful: false},
  %{row: "optional_add", root: "OptionalAdd", fun: "optional_add", unlawful: false}
]

if System.get_env("R16_CACHE") != "1" do
  File.rm_rf!(tmp)
end

File.mkdir_p!(tmp)

mods =
  Map.new(rows, fn %{row: row, root: root, fun: fun, unlawful: unlawful} ->
    old_dir = Path.join(tmp, row <> "/old")
    new_dir = Path.join(tmp, row <> "/new")

    if not File.dir?(old_dir) do
      R16.Gen.old(bin, tmp, row)
      if unlawful, do: R16.Gen.new_unlawful(bin, tmp, row), else: R16.Gen.new_lawful(bin, tmp, row)
    end

    R16.Gen.require!(old_dir)
    R16.Gen.require!(new_dir)

    old_ns = Macro.camelize("vold_" <> row)
    new_ns = Macro.camelize("vnew_" <> row)

    {row,
     %{
       root: root,
       old_mod: R16.Gen.fixed_mod!(old_ns, fun),
       new_mod: R16.Gen.fixed_mod!(new_ns, fun),
       old_ns: old_ns,
       new_ns: new_ns
     }}
  end)

# ---- the assertion harness (R13's shape): one printed line per assertion ----

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

# struct construction without compile-time struct expansion (the modules are
# required at runtime): struct(Module, fields).
struct_of = fn ns, name, fields -> struct(Module.concat([ns, name]), fields) end

save = fn mod, fun, values -> apply(mod, String.to_atom(fun <> "_fixed_save"), [values]) end
load = fn mod, fun, data -> apply(mod, String.to_atom(fun <> "_fixed_load"), [data]) end

# every counter but the one a clause names, at zero, and malformed clear
quiet = fn report, except ->
  report
  |> Map.drop([:malformed, :layout_hash | except])
  |> Enum.all?(fn {_k, v} -> v == 0 end) and report.malformed == false
end

# EVERY needle this file forges is one field's lawful writer bytes; a needle
# that does not occur exactly the expected number of times is a broken locator
# and says so (the cpp leg's find_once rule).
forge = fn data, needle, hits_wanted, write ->
  hits =
    for at <- 0..(byte_size(data) - byte_size(needle)),
        binary_part(data, at, byte_size(needle)) == needle,
        do: at

  if length(hits) != hits_wanted do
    {:error, "the locator #{Base.encode16(needle)} occurs #{length(hits)} times, want #{hits_wanted}"}
  else
    {:ok, Enum.reduce(hits, data, fn at, acc -> write.(acc, at) end)}
  end
end

put32 = fn data, at, v ->
  binary_part(data, 0, at) <> <<v::little-unsigned-32>> <> binary_part(data, at + 4, byte_size(data) - at - 4)
end

# ============================================================================
# unknown: once per PEER at COMPILE, never per record (§5.8 row 11's pair).
# THREE records, each FOUR Items, each carrying the dropped `drop`: the
# per-record divergence would read 3 and the per-element one 12.
# ============================================================================

uc = mods["unknown_census"]

item = fn a, d -> struct_of.(uc.old_ns, "Item", a: a, drop: d) end

census_rec = fn k ->
  struct_of.(uc.old_ns, "Census",
    lead: 1,
    items: [item.(10 * k + 0, 100), item.(10 * k + 1, 101), item.(10 * k + 2, 102), item.(10 * k + 3, 103)],
    trail: 2
  )
end

file = save.(uc.old_mod, "census", [census_rec.(0), census_rec.(1), census_rec.(2)])

case load.(uc.new_mod, "census", file) do
  {:ok, values, report} ->
    check.("unknown_census: the NEW reader returned all three records", length(values) == 3)

    check.(
      "unknown is once per PEER at COMPILE (== 1, never 3 per record, never 12 per element)",
      report.unknown == 1
    )

    check.("unknown_census: no other counter moved on the counted peer", quiet.(report, [:unknown]))

    landed =
      Enum.zip(values, [[0, 1, 2, 3], [10, 11, 12, 13], [20, 21, 22, 23]])
      |> Enum.all?(fn {v, want} ->
        v.lead == 1 and v.trail == 2 and Enum.map(v.items, & &1.a) == want
      end)

    check.("unknown_census: every named field landed exactly", landed)

  other ->
    check.("unknown_census: the NEW reader returned all three records — got #{inspect(other)}", false)
end

# ============================================================================
# widened: once per ENTRY per RECORD — three widened scalars read 3, not 1.
# ============================================================================

iw = mods["int_widen"]

iw_rec = fn v -> struct_of.(iw.old_ns, "IntWiden", lead: 0xAAAAAAAA, v: v, trail: 0xBBBBBBBB) end
file = save.(iw.old_mod, "int_widen", [iw_rec.(-1), iw_rec.(-32768), iw_rec.(32767)])

case load.(iw.new_mod, "int_widen", file) do
  {:ok, values, report} ->
    check.("int_widen: the NEW reader returned all three records", length(values) == 3)

    check.(
      "widened is once per ENTRY per RECORD (== 3 for three widened scalars, not 1)",
      report.widened == 3
    )

    check.("int_widen: only widened moved", quiet.(report, [:widened]))

    check.(
      "int_widen: the sign-extended values landed exactly",
      Enum.map(values, & &1.v) == [-1, -32768, 32767] and
        Enum.all?(values, &(&1.lead == 0xAAAAAAAA and &1.trail == 0xBBBBBBBB))
    )

  other ->
    check.("int_widen: the NEW reader returned all three records — got #{inspect(other)}", false)
end

# ============================================================================
# widened: a widened element run is UNFOLDED — [..4]int16 into [..4]int32 with
# FOUR elements sent reads min(their_n, my_n) == 4; a fold would read 1.
# ============================================================================

ae = mods["array_elem_widen"]

aew_rec =
  struct_of.(ae.old_ns, "ArrayElemWiden",
    lead: 0xAAAAAAAA,
    vals: [-1, -32768, 32767, 0],
    trail: 0xBBBBBBBB
  )

file = save.(ae.old_mod, "array_elem_widen", [aew_rec])

case load.(ae.new_mod, "array_elem_widen", file) do
  {:ok, [v], report} ->
    check.(
      "a widened element run is UNFOLDED: widened == min(their_n, my_n) == 4 (a fold would read 1)",
      report.widened == 4
    )

    check.("array_elem_widen: only widened moved", quiet.(report, [:widened]))

    check.(
      "array_elem_widen: each element widened element by element, sign and all, and the brackets did not move",
      v.vals == [-1, -32768, 32767, 0] and v.lead == 0xAAAAAAAA and v.trail == 0xBBBBBBBB
    )

  other ->
    check.("array_elem_widen: the NEW reader returned the record — got #{inspect(other)}", false)
end

# ============================================================================
# widened: a grown enum ordinal width — 255 variants (one byte) into 256 (two).
# ============================================================================

ew = mods["enum_width"]
ew_rec = struct_of.(ew.old_ns, "Lineage", tier: 255, seq: 12)
file = save.(ew.old_mod, "lineage", [ew_rec])

case load.(ew.new_mod, "lineage", file) do
  {:ok, [v], report} ->
    check.("a grown enum ordinal width counts widened once (== 1)", report.widened == 1)
    check.("enum_width: only widened moved", quiet.(report, [:widened]))

    check.(
      "enum_width: the widened ordinal and the scalar behind it landed exactly",
      v.tier == 255 and v.seq == 12
    )

  other ->
    check.("enum_width: the NEW reader returned the record — got #{inspect(other)}", false)
end

# ============================================================================
# clamped: once per ENTRY per RECORD, the COUNT lane — [..4] into [..8], the
# count word of EACH of TWO records forged from the writer's 4 to 7, between
# the two bounds (§5.8 row 4's bound).
# ============================================================================

ab = mods["array_bounded_grow"]

abg_rec = fn ->
  struct_of.(ab.old_ns, "ArrayBoundedGrow",
    lead: 0xAAAAAAAA,
    vals: [1000, 1001, 1002, 1003],
    trail: 0xBBBBBBBB
  )
end

file = save.(ab.old_mod, "array_bounded_grow", [abg_rec.(), abg_rec.()])

case forge.(file, <<0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00>>, 2, fn acc, at ->
       put32.(acc, at + 4, 7)
     end) do
  {:ok, forged} ->
    case load.(ab.new_mod, "array_bounded_grow", forged) do
      {:ok, values, report} ->
        check.("array_bounded_grow: the NEW reader returned both records", length(values) == 2)

        check.(
          "array_bounded_grow: the count clamps to the WRITER's 4, never the reader's 8 nor the forged 7",
          Enum.all?(values, &(length(&1.vals) == 4 and &1.vals == [1000, 1001, 1002, 1003]))
        )

        check.("clamped is once per ENTRY per RECORD (== 2 for two records, not 1)", report.clamped == 2)
        check.("array_bounded_grow: only clamped moved", quiet.(report, [:clamped]))

        check.(
          "array_bounded_grow: the brackets did not move",
          Enum.all?(values, &(&1.lead == 0xAAAAAAAA and &1.trail == 0xBBBBBBBB))
        )

      other ->
        check.("array_bounded_grow: the NEW reader returned both records — got #{inspect(other)}", false)
    end

  {:error, why} ->
    check.("array_bounded_grow: #{why}", false)
end

# ============================================================================
# clamped: the TEXT lane — string(8) into string(16), the length word forged
# from the writer's FULL 8 to 12, past the writer's bound and inside the
# reader's (the lane only the writer's bound catches).
# ============================================================================

sg = mods["string_grow"]
sg_rec = struct_of.(sg.old_ns, "StringGrow", lead: 0xAAAAAAAA, text: "abcdefgh", trail: 0xBBBBBBBB)
file = save.(sg.old_mod, "string_grow", [sg_rec])

case forge.(file, <<0xAA, 0xAA, 0xAA, 0xAA, 0x08, 0x00, 0x00, 0x00>>, 1, fn acc, at ->
       put32.(acc, at + 4, 12)
     end) do
  {:ok, forged} ->
    case load.(sg.new_mod, "string_grow", forged) do
      {:ok, [v], report} ->
        check.(
          "string_grow: the text length clamps to the WRITER's 8 and the bytes land",
          v.text == "abcdefgh"
        )

        check.("text clamps once per entry per record (== 1, not >= 1)", report.clamped == 1)
        check.("string_grow: only clamped moved", quiet.(report, [:clamped]))

        check.(
          "string_grow: the brackets did not move",
          v.lead == 0xAAAAAAAA and v.trail == 0xBBBBBBBB
        )

      other ->
        check.("string_grow: the NEW reader returned the record — got #{inspect(other)}", false)
    end

  {:error, why} ->
    check.("string_grow: #{why}", false)
end

# ============================================================================
# the bounds pass counts a forged ordinal remapped to None on BOTH plans —
# tier forged from Gold (3), the WRITER's top variant, to 4.
# ============================================================================

ea = mods["enum_append"]
ea_rec = struct_of.(ea.old_ns, "Lineage", tier: 3, seq: 9)
file = save.(ea.old_mod, "lineage", [ea_rec])

case forge.(file, <<0x03, 0x09, 0x00, 0x00, 0x00>>, 1, fn acc, at ->
       binary_part(acc, 0, at) <> <<4>> <> binary_part(acc, at + 1, byte_size(acc) - at - 1)
     end) do
  {:ok, forged} ->
    # THE COMPILED PLAN: the NEW build reads the forged OLD file through the
    # lineage, so the plan is compiled from the OLD (three-variant) layout.
    case load.(ea.new_mod, "lineage", forged) do
      {:ok, [v], report} ->
        check.("forged ordinal, compiled plan: one past the WRITER's top lands None", v.tier == 0)
        check.("forged ordinal, compiled plan: the scalar behind it landed", v.seq == 9)

        check.(
          "forged ordinal, compiled plan: clamped == 1 (the bounds pass, not the op too)",
          report.clamped == 1
        )

        check.("forged ordinal, compiled plan: only clamped moved", quiet.(report, [:clamped]))

      other ->
        check.("forged ordinal, compiled plan: the record read — got #{inspect(other)}", false)
    end

    # THE IDENTITY PLAN: the OLD build reads its own forged file on its own
    # hash, so the plan is the baked identity one.
    case load.(ea.old_mod, "lineage", forged) do
      {:ok, [v], report} ->
        check.("forged ordinal, identity plan: one past the WRITER's top lands None", v.tier == 0)
        check.("forged ordinal, identity plan: the scalar behind it landed", v.seq == 9)
        check.("forged ordinal, identity plan: clamped == 1 on the SAME forged bytes", report.clamped == 1)
        check.("forged ordinal, identity plan: only clamped moved", quiet.(report, [:clamped]))

      other ->
        check.("forged ordinal, identity plan: the record read — got #{inspect(other)}", false)
    end

  {:error, why} ->
    check.("enum_append: #{why}", false)
end

# ============================================================================
# copy, const, present, ordinal move nothing: a clean NEW-READS-OLD of an
# appended field, variant, arm or `?` is not an event — every counter at zero.
# ============================================================================

# COPY: one field appended (field_append).
fa = mods["field_append"]
fa_rec = struct_of.(fa.old_ns, "Lineage", x: 11, y: 22, z: 33)
file = save.(fa.old_mod, "lineage", [fa_rec])

case load.(fa.new_mod, "lineage", file) do
  {:ok, [v], report} ->
    check.(
      "field_append: an appended field is not an event — the old values landed, the tail is its declared default",
      v.x == 11 and v.y == 22 and v.z == 33 and v.w == 77
    )

    check.("field_append: the COPY op moved every counter not at all", quiet.(report, []))

  other ->
    check.("field_append: the NEW reader returned the record — got #{inspect(other)}", false)
end

# ORDINAL: one variant appended (enum_append), a lawful ordinal remap.
file = save.(ea.old_mod, "lineage", [ea_rec])

case load.(ea.new_mod, "lineage", file) do
  {:ok, [v], report} ->
    check.("enum_append clean: the appended variant is not an event — Gold landed", v.tier == 3 and v.seq == 9)
    check.("enum_append clean: the ORDINAL op moved every counter not at all", quiet.(report, []))

  other ->
    check.("enum_append clean: the NEW reader returned the record — got #{inspect(other)}", false)
end

# CONST: one arm appended (union_append), a lawful tag remap.
ua = mods["union_append"]

ua_rec =
  struct_of.(ua.old_ns, "Lineage",
    pick: struct_of.(ua.old_ns, "Pick", type: 1, alpha: struct_of.(ua.old_ns, "Alpha", m: 7)),
    seq: 5
  )

file = save.(ua.old_mod, "lineage", [ua_rec])

case load.(ua.new_mod, "lineage", file) do
  {:ok, [v], report} ->
    check.(
      "union_append: an appended arm is not an event — the tag and payload landed",
      v.pick.type == 1 and v.pick.alpha.m == 7 and v.seq == 5
    )

    check.("union_append: the CONST op moved every counter not at all", quiet.(report, []))

  other ->
    check.("union_append: the NEW reader returned the record — got #{inspect(other)}", false)
end

# PRESENT: T into ?T (optional_add), every old value lands present.
oa = mods["optional_add"]

oa_rec =
  struct_of.(oa.old_ns, "OptionalAdd",
    lead: 1,
    link: struct_of.(oa.old_ns, "OptLeaf", value: 42),
    trail: 2
  )

file = save.(oa.old_mod, "optional_add", [oa_rec])

case load.(oa.new_mod, "optional_add", file) do
  {:ok, [v], report} ->
    check.(
      "optional_add: T into ?T lands present, and the value with it",
      v.link_present == true and v.link.value == 42 and v.lead == 1 and v.trail == 2
    )

    check.("optional_add: the PRESENT op moved every counter not at all", quiet.(report, []))

  other ->
    check.("optional_add: the NEW reader returned the record — got #{inspect(other)}", false)
end

# ---- the verdict ----

{count, fails} = Agent.get(checker, & &1)
Agent.stop(checker)

if fails == [] do
  IO.puts("#{count} assertions, all green — §5.4's counters exactly, on the elixir leg")
  System.halt(0)
else
  IO.puts(:stderr, "#{count} assertions, #{length(fails)} failed: #{Enum.join(fails, "; ")}")
  System.halt(1)
end
