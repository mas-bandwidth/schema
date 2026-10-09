# cell3-elixir-text-bytes-flags-defaults-valid-data —
# String, byte-buffer and flags defaults: valid-data write/read acceptance.
#
# THE LAW (docs/FIXED-FORM-ALGORITHM.md, ALGORITHM §3 prefill; PR847):
#   The prefill is the DECLARED DEFAULTS as record bytes. A field this record
#   does not carry has no plan entry at all, so these bytes are what lands there
#   and the loop never learns the field existed. A write/read round-trip of a
#   record whose string, byte-buffer and flags fields carry their declared
#   defaults (or non-default valid data) must land the same values.
#
# THE TABLE: Tabledemo.ProfileConfig (tables/examples/Tables.schema) carries:
#   name  string(32)         — default <<>>  (string)
#   icon  bytes(16)          — default <<>>  (byte-buffer)
#   loadout.perks Perks      — default 0     (flags)
#
# VECTOR DERIVATION: built from the law — the save/load round-trip. No
# conformance corpus fixture exercises this feature; the test constructs the
# smallest vectors from the generated module's own constants (hash, layout,
# body_bytes) and the save/load API.
#
# Run from the repository root: elixir test/conformance/elixir/rows/TextBytesFlagsDefaultsValidData.exs
# Exit 0 green, exit 1 red, one printed line per assertion.

ebin = System.get_env("EBIN", "build/elixir-tables-ebin")
Code.prepend_path(ebin)

Path.wildcard(ebin <> "/*.beam")
|> Enum.each(fn path ->
  path |> Path.basename(".beam") |> String.to_atom() |> Code.ensure_loaded!()
end)

mod = Tabledemo.TablesFixed
save = fn values -> mod.profile_config_fixed_save(values) end
load = fn data -> mod.profile_config_fixed_load(data) end

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

# ---- 1. default-values round-trip: string, bytes and flags at their defaults ----
default_profile = struct!(Tabledemo.ProfileConfig)
data = save.([default_profile])

case load.(data) do
  {:ok, [read], report} ->
    check.("default string lands empty", read.name == <<>>)
    check.("default bytes lands empty", read.icon == <<>>)
    check.("default flags lands zero", read.loadout.perks == 0)
    check.("default round-trip: malformed clear", not report.malformed)
    check.("default round-trip: no counters moved",
      report.unknown == 0 and report.kind_mismatch == 0 and
      report.clamped == 0 and report.widened == 0 and report.duplicate == 0)

  other ->
    check.("default round-trip: expected {:ok, [_], _}, got #{inspect(other)}", false)
end

# ---- 2. non-default values round-trip: string, bytes and flags with valid data ----
non_default = struct!(Tabledemo.ProfileConfig,
  name: "hello",
  icon: <<0x01, 0x02, 0x03, 0x04>>,
  loadout: struct!(Tabledemo.LoadoutConfig, perks: 5)
)

data2 = save.([non_default])

case load.(data2) do
  {:ok, [read2], report2} ->
    check.("non-default string survives", read2.name == "hello")
    check.("non-default bytes survives", read2.icon == <<0x01, 0x02, 0x03, 0x04>>)
    check.("non-default flags survives", read2.loadout.perks == 5)
    check.("non-default round-trip: malformed clear", not report2.malformed)

  other ->
    check.("non-default round-trip: expected {:ok, [_], _}, got #{inspect(other)}", false)
end

# ---- 3. string at max length, bytes at max length, all flag bits ----
max_profile = struct!(Tabledemo.ProfileConfig,
  name: :binary.copy("x", 32),
  icon: :binary.copy(<<0xFF>>, 16),
  loadout: struct!(Tabledemo.LoadoutConfig, perks: 7)
)

data3 = save.([max_profile])

case load.(data3) do
  {:ok, [read3], report3} ->
    check.("max-length string survives", read3.name == :binary.copy("x", 32))
    check.("max-length bytes survives", read3.icon == :binary.copy(<<0xFF>>, 16))
    check.("all flag bits survives", read3.loadout.perks == 7)
    check.("max round-trip: malformed clear", not report3.malformed)

  other ->
    check.("max round-trip: expected {:ok, [_], _}, got #{inspect(other)}", false)
end

{count, fails} = Agent.get(checker, & &1)
Agent.stop(checker)

if fails == [] do
  IO.puts("#{count} assertions, all green — String, byte-buffer and flags defaults: valid-data write/read acceptance")
  System.halt(0)
else
  IO.puts(:stderr, "#{count} assertions, #{length(fails)} failed: #{Enum.join(fails, "; ")}")
  System.halt(1)
end
