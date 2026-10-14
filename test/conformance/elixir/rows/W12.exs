# W12 — elixir/W12: THE LAYOUT HASH INCLUDES THE 4-BYTE COUNT.
#
# Law (docs/FIXED-FORM-ALGORITHM.md:56-59, docs/SPEC-TABLES.md §3.4 / §5.2):
#   The hash is fnv1a64 — h := 0xcbf29ce484222325; for each byte v: h ^= v;
#   h *= 0x100000001b3 — over the layout's bytes AS WRITTEN, THE 4-BYTE COUNT
#   INCLUDED, and then over the DEFINITIONS DIGEST. An empty digest leaves the
#   hash the layout's alone. It is a wire identity, never a security claim.
#
# The layout "as written" is `ir.TableFixedLayoutBytes` (ir/fixedform.go:582):
# a u32 entry count FIRST, little-endian, then the seventeen-byte entries — so
# a hash computed over the entries without the count is NOT the wire hash.
#
# VECTOR: no hand vector is constructed here. The test reads the layout the
# compiler itself minted — the generated constant the production writer puts
# behind the header and the production reader matches lineage on — for
# GunnerConfig (tables/examples/Keyed.schema), a fixed table whose fields
# (float32, bool) state no range, no bits(N), no fixed(I,F) and no flags, so
# its DEFINITIONS DIGEST is EMPTY and the law reduces to
#   hash == fnv1a64(<<count::little-32>> ++ entries)
# which makes the count's inclusion observable in both directions:
# with the count the equation holds, without it it cannot.
# Cell (test/tables/V1.schema) carries a range, so its digest is NOT empty;
# its hash is asserted only against the count-less walk, which the law makes
# wrong for every digest content.
#
# REACH: the same compiled image the driver runs. make build-conformance-elixir
# compiles build/tables-generated-elixir/*/*.ex and the driver into
# build/elixir-tables-ebin (make/elixir.mk:98-105); this test prepends that
# ebin and loads the generated modules from it. Run from the repository root:
#   elixir test/conformance/elixir/rows/W12.exs
# exit 0 green, exit 1 red, one printed line per assertion.

ebin = "build/elixir-tables-ebin"

unless File.dir?(ebin) do
  IO.puts("FAIL W12 #{ebin} missing — run: make build/conformance-harness build-conformance-elixir")
  System.halt(1)
end

Code.prepend_path(Path.expand(ebin))

defmodule W12 do
  @moduledoc false

  import Bitwise

  # fnv1a64, spelled from the law's own sentence. The generated runtime keeps
  # its own copy (FixedRuntime.hash/1); assertion 1 below pins the two together
  # so this file tests the count, not the multiplier.
  def fnv(bin, h \\ 0xCBF29CE484222325)
  def fnv(<<>>, h), do: h
  def fnv(<<b, rest::binary>>, h), do: fnv(rest, bxor(h, b) * 0x100000001B3 &&& 0xFFFFFFFFFFFFFFFF)

  # The layout AS WRITTEN: a u32 count in front, entries behind it.
  def split(layout) do
    <<count::little-unsigned-32, entries::binary>> = layout
    {count, entries}
  end

  def run do
    results = [
      production_helper(),
      law_with_count(Tabledemo.KeyedFixed, :gunner_config),
      control_count_less(Tabledemo.KeyedFixed, :gunner_config, 3),
      control_count_less(Tblv1.V1Fixed, :cell, 4),
      wire_write_path(),
      wire_read_path()
    ]

    failed = Enum.count(results, &(&1 != :ok))

    if failed == 0 do
      System.halt(0)
    else
      System.halt(1)
    end
  end

  # 1. The production fnv helper and the law's formula agree on the layout.
  defp production_helper do
    layout = Tabledemo.KeyedFixed.gunner_config_fixed_layout()

    if Tabledemo.FixedRuntime.hash(layout) == fnv(layout) do
      line(true,
        "1 runtime fnv1a64 == the law's formula over the layout as written"
      )
    else
      line(false, "1 runtime fnv1a64 != the law's formula over the layout as written")
    end
  end

  # 2. THE LAW: the hash the compiler minted — the number every record
  # carries and the header writes — is fnv1a64 over the layout's bytes with
  # the 4-byte count INCLUDED, plus the definitions digest. GunnerConfig's
  # digest is empty, so the full equation is checkable from the wire side;
  # Cell's is not (it carries a range), so Cell appears only in the control
  # below, whose negative half the law makes true for every digest content.
  defp law_with_count(mod, table) do
    hash = apply(mod, :"#{table}_fixed_hash", [])
    layout = apply(mod, :"#{table}_fixed_layout", [])
    {count, entries} = split(layout)

    cond do
      fnv(<<count::little-unsigned-32, entries::binary>>) == hash ->
        line(true,
          "2 #{mod}.#{table}: hash 0x#{Integer.to_string(hash, 16)} == " <>
            "fnv1a64(<<count=#{count}::little-32>> ++ entries) — the 4-byte count is in the hash"
        )

      true ->
        line(false,
          "2 #{mod}.#{table}: hash 0x#{Integer.to_string(hash, 16)} != " <>
            "fnv1a64(<<count=#{count}::little-32>> ++ entries) — the 4-byte count is NOT in the hash"
        )
    end
  end

  # 4/5. THE BITE, inside the test: the count-less walk of the SAME layout
  # must NOT be the minted hash. If the emitter ever hashed the entries alone
  # (the bug this cell is about), assertion 2 fails and this one passes —
  # the pair localises the break to the count.
  defp control_count_less(mod, table, n) do
    hash = apply(mod, :"#{table}_fixed_hash", [])
    {_count, entries} = split(apply(mod, :"#{table}_fixed_layout", []))

    if fnv(entries) != hash do
      line(true,
        "#{n} #{mod}.#{table}: fnv1a64(entries alone) 0x#{Integer.to_string(fnv(entries), 16)} " <>
          "!= hash — the count-less walk is not the wire hash"
      )
    else
      line(false,
        "#{n} #{mod}.#{table}: fnv1a64(entries alone) == hash — the count is NOT in the hash"
      )
    end
  end

  # 6. THE PRODUCTION WRITE: the emitted saver puts the minted hash into the
  # header at offset 8 (form byte + seven reserved zeros) and into every
  # record's first eight bytes (KeyedFixed.ex, gunner_config_fixed_save/1 and
  # gunner_config_fixed_record/1). The bytes on the wire must be the
  # count-inclusive walk, not the count-less one.
  defp wire_write_path do
    value = %Tabledemo.GunnerConfig{reaction: 0.25, tracking: true}
    file = Tabledemo.KeyedFixed.gunner_config_fixed_save([value])
    {:ok, hash, _layout, _records} = Tabledemo.FixedRuntime.read_file_header(file)
    minted = Tabledemo.KeyedFixed.gunner_config_fixed_hash()
    layout = Tabledemo.KeyedFixed.gunner_config_fixed_layout()
    {count, entries} = split(layout)
    derived = fnv(<<count::little-unsigned-32, entries::binary>>)
    <<record_hash::little-unsigned-64, _::binary>> = Tabledemo.KeyedFixed.gunner_config_fixed_record(value)

    cond do
      hash == minted and minted == derived and record_hash == minted ->
        line(true,
          "5 write path: header@8 and record@0 carry 0x#{Integer.to_string(minted, 16)} " <>
            "== fnv1a64(count ++ entries) — the wire bytes are the count-inclusive walk"
        )

      true ->
        line(false,
          "5 write path: header@8 0x#{Integer.to_string(hash, 16)} / record@0 " <>
            "0x#{Integer.to_string(record_hash, 16)} vs derived 0x#{Integer.to_string(derived, 16)} " <>
            "— the wire bytes are NOT the count-inclusive walk"
        )
    end
  end

  # 7. THE PRODUCTION READ: the file the writer minted loads back through the
  # emitted loader, whose lineage is keyed on the same hash — identity on
  # this build's own layout, one value in one value out.
  defp wire_read_path do
    value = %Tabledemo.GunnerConfig{reaction: 0.25, tracking: true}
    file = Tabledemo.KeyedFixed.gunner_config_fixed_save([value])

    case Tabledemo.KeyedFixed.gunner_config_fixed_load(file) do
      {:ok, [^value], _report} ->
        line(true, "6 read path: save/load round trip through the hash-keyed lineage is identity")

      other ->
        line(false, "6 read path: save/load round trip is NOT identity: #{inspect(other)}")
    end
  end

  defp line(ok, text) do
    IO.puts("#{if(ok, do: "ok", else: "FAIL")} W12 #{text}")
    if(ok, do: :ok, else: :fail)
  end
end

W12.run()
