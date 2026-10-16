package elixirtable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixir"
)

// TestKeyedAccessorCoversBothEndsElixir pins docs/PORTING.md M5 (#407):
// every enum-keyed array in the Elixir backend — both fixed-table value structs
// (KeyedConfig, HullConfig) and plain type structs (ScoreBoard, SlotBundle,
// SparseHolder) — exposes a 1-based keyed accessor pair (<field>_at/2,
// put_<field>_at/3) and a key-element iterator (<field>_entries/1) on the
// struct's own module, refusing None (key 0), keys past E.Max (including
// headroom enums with | max = M), negative keys, and non-integer keys with an
// ArgumentError that names the field, the key, and the bound.
// Functional (elixirBinary): run with SCHEMA_REQUIRE_CORPUS=1.
func TestKeyedAccessorCoversBothEndsElixir(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	keyedUnit := loadUnit(t, filepath.Join(root, "tables", "examples", "Keyed.schema"))
	keyedFiles := generateElixir(t, keyedUnit)

	probeUnit := unitFrom(t, `package keyedprobe

enum Slot { Alpha, Beta, Gamma }
enum Tier { Low, High }

enum Spare | max = 4
{
    One
    Two
}

type SlotPair
{
    a int32 = 1
    b int32 = 2
}

type SlotBundle
{
    pairs [Slot]SlotPair
}

type SparseHolder
{
    sparse [Spare]int32
}

fixed table KeyedConfig
{
    tokens   [Slot]int32
    tiers    [Tier]int32
    per_slot [Slot]SlotPair
    nested   SlotBundle
}
`)
	probeFiles := generateElixir(t, probeUnit)

	pktUnit := unitFrom(t, `package keyedpkt

enum Spare | max = 4
{
    One
    Two
}

type SparsePacket
{
    sparse [Spare]int32
}
`)
	pktFiles, err := elixir.Generate(pktUnit)
	if err != nil {
		t.Fatalf("elixir.Generate: %v", err)
	}

	dir := t.TempDir()
	keyedDir := filepath.Join(dir, "keyed")
	probeDir := filepath.Join(dir, "probe")
	pktDir := filepath.Join(dir, "pkt")
	if err := os.MkdirAll(keyedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(probeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pktDir, 0o700); err != nil {
		t.Fatal(err)
	}
	keyedNames := writeKeyedModules(t, keyedDir, keyedFiles)
	probeNames := writeKeyedModules(t, probeDir, probeFiles)
	pktNames := writeKeyedModules(t, pktDir, pktFiles)

	probe := `
defmodule KeyedCheck do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end

  def check_raises_none(fun, field) do
    try do
      v = fun.()
      IO.puts(:stderr, "FAIL: #{field}: key 0 (None) was not refused, got #{inspect(v)}")
      System.halt(1)
    rescue
      e in ArgumentError ->
        msg = Exception.message(e)
        check(
          String.contains?(msg, field) and String.contains?(msg, "key 0 is None and names no slot"),
          "#{field}: None refusal message wrong: #{inspect(msg)}"
        )
    end
  end

  def check_raises_max(fun, field, bad_key, enum_name, max_val) do
    try do
      v = fun.()
      IO.puts(:stderr, "FAIL: #{field}: key past max (#{bad_key} > #{max_val}) was not refused, got #{inspect(v)}")
      System.halt(1)
    rescue
      e in ArgumentError ->
        msg = Exception.message(e)
        want = "key #{bad_key} is past #{enum_name}.max (#{max_val})"
        check(
          String.contains?(msg, field) and String.contains?(msg, want),
          "#{field}: max refusal message wrong: #{inspect(msg)} (wanted #{inspect(want)})"
        )
    end
  end

  def check_raises_invalid(fun, field, enum_name) do
    try do
      v = fun.()
      IO.puts(:stderr, "FAIL: #{field}: invalid key was not refused, got #{inspect(v)}")
      System.halt(1)
    rescue
      e in ArgumentError ->
        msg = Exception.message(e)
        check(
          String.contains?(msg, field) and String.contains?(msg, "is not a variant of #{enum_name}"),
          "#{field}: invalid key refusal message wrong: #{inspect(msg)}"
        )
    end
  end
end

# 1. tables/examples/Keyed.schema (Tabledemo.KeyedConfig, Tabledemo.HullConfig, Tabledemo.ScoreBoard)
alias Tabledemo.{Team, Hull, Weapon, TeamConfig, TurretConfig, HullConfig, ScoreBoard, KeyedConfig, KeyedFixed}

cfg = %KeyedConfig{}
KeyedCheck.check(Team.max() == 3 and Hull.max() == 3 and Weapon.max() == 3, "enum maxes")

# 1-based get/put on table struct fields (teams, hulls, turrets) and type struct field (ScoreBoard.per_team)
cfg =
  cfg
  |> KeyedConfig.put_teams_at(Team.red(), %TeamConfig{spawn_count: 10, banner: "red"})
  |> KeyedConfig.put_teams_at(Team.blue(), %TeamConfig{spawn_count: 20, banner: "blue"})
  |> KeyedConfig.put_teams_at(Team.green(), %TeamConfig{spawn_count: 30, banner: "green"})

KeyedCheck.check(KeyedConfig.teams_at(cfg, Team.red()).spawn_count == 10, "teams_at Red")
KeyedCheck.check(KeyedConfig.teams_at(cfg, Team.blue()).spawn_count == 20, "teams_at Blue")
KeyedCheck.check(KeyedConfig.teams_at(cfg, Team.green()).spawn_count == 30, "teams_at Green")
KeyedCheck.check(
  Enum.map(KeyedConfig.teams_entries(cfg), fn {k, v} -> {k, v.spawn_count, v.banner} end) ==
    [{1, 10, "red"}, {2, 20, "blue"}, {3, 30, "green"}],
  "teams_entries yields 1-based {key, element} pairs"
)

hull2 =
  %HullConfig{health: 250.0, mass: 5.0}
  |> HullConfig.put_turrets_at(Weapon.cannon(), %TurretConfig{damage: 11.0, cooldown: 0.25})
  |> HullConfig.put_turrets_at(Weapon.missile(), %TurretConfig{damage: 22.0, cooldown: 0.5})
  |> HullConfig.put_turrets_at(Weapon.mine(), %TurretConfig{damage: 33.0, cooldown: 1.0})

KeyedCheck.check(HullConfig.turrets_at(hull2, Weapon.cannon()).damage == 11.0, "turrets_at Cannon")
KeyedCheck.check(HullConfig.turrets_at(hull2, Weapon.missile()).damage == 22.0, "turrets_at Missile")
KeyedCheck.check(HullConfig.turrets_at(hull2, Weapon.mine()).damage == 33.0, "turrets_at Mine")
KeyedCheck.check(
  Enum.map(HullConfig.turrets_entries(hull2), fn {k, t} -> {k, t.damage} end) ==
    [{1, 11.0}, {2, 22.0}, {3, 33.0}],
  "turrets_entries yields 1-based {key, element} pairs"
)

cfg = KeyedConfig.put_hulls_at(cfg, Hull.gunship(), hull2)
KeyedCheck.check(KeyedConfig.hulls_at(cfg, Hull.interceptor()).health == 100.0, "hulls_at Interceptor untouched")
KeyedCheck.check(KeyedConfig.hulls_at(cfg, Hull.gunship()).health == 250.0, "hulls_at Gunship updated")
KeyedCheck.check(KeyedConfig.hulls_at(cfg, Hull.freighter()).health == 100.0, "hulls_at Freighter untouched")
KeyedCheck.check(
  Enum.map(KeyedConfig.hulls_entries(cfg), fn {k, h} -> {k, h.health} end) ==
    [{1, 100.0}, {2, 250.0}, {3, 100.0}],
  "hulls_entries yields 1-based {key, element} pairs"
)

scores =
  %ScoreBoard{}
  |> ScoreBoard.put_per_team_at(Team.red(), 1000)
  |> ScoreBoard.put_per_team_at(Team.blue(), 2000)
  |> ScoreBoard.put_per_team_at(Team.green(), 3000)

KeyedCheck.check(ScoreBoard.per_team_at(scores, Team.red()) == 1000, "ScoreBoard.per_team_at Red")
KeyedCheck.check(ScoreBoard.per_team_at(scores, Team.blue()) == 2000, "ScoreBoard.per_team_at Blue")
KeyedCheck.check(ScoreBoard.per_team_at(scores, Team.green()) == 3000, "ScoreBoard.per_team_at Green")
KeyedCheck.check(
  ScoreBoard.per_team_entries(scores) == [{1, 1000}, {2, 2000}, {3, 3000}],
  "ScoreBoard.per_team_entries yields 1-based {key, element} pairs"
)

cfg = %{cfg | scores: scores}
wire = KeyedFixed.keyed_config_fixed_save([cfg])
{:ok, [loaded], report} = KeyedFixed.keyed_config_fixed_load(wire)
KeyedCheck.check(report.clamped == 0 and report.unknown == 0, "KeyedFixed round-trip report clean")
KeyedCheck.check(
  KeyedConfig.teams_entries(loaded) == KeyedConfig.teams_entries(cfg) and
    KeyedConfig.hulls_at(loaded, Hull.gunship()).health == 250.0 and
    HullConfig.turrets_at(KeyedConfig.hulls_at(loaded, Hull.gunship()), Weapon.missile()).damage == 22.0 and
    ScoreBoard.per_team_entries(loaded.scores) == [{1, 1000}, {2, 2000}, {3, 3000}],
  "KeyedFixed round-trip preserves keyed slots"
)

# 2. Keyedprobe (tokens, tiers, per_slot, nested.pairs, SparseHolder.sparse | max = 4, and Keyedpkt.SparsePacket)
alias Keyedprobe.{Slot, Tier, Spare, SlotPair, SlotBundle, SparseHolder, ProbeFixed}
alias Keyedprobe.KeyedConfig, as: PConfig
alias Keyedpkt.SparsePacket

KeyedCheck.check(Slot.max() == 3 and Tier.max() == 2 and Spare.max() == 4, "probe enum maxes")

bundle =
  %SlotBundle{}
  |> SlotBundle.put_pairs_at(Slot.alpha(), %SlotPair{a: 11, b: 12})
  |> SlotBundle.put_pairs_at(Slot.beta(), %SlotPair{a: 21, b: 22})
  |> SlotBundle.put_pairs_at(Slot.gamma(), %SlotPair{a: 31, b: 32})

KeyedCheck.check(SlotBundle.pairs_at(bundle, Slot.alpha()) == %SlotPair{a: 11, b: 12}, "SlotBundle.pairs_at Alpha")
KeyedCheck.check(SlotBundle.pairs_at(bundle, Slot.beta()) == %SlotPair{a: 21, b: 22}, "SlotBundle.pairs_at Beta")
KeyedCheck.check(SlotBundle.pairs_at(bundle, Slot.gamma()) == %SlotPair{a: 31, b: 32}, "SlotBundle.pairs_at Gamma")
KeyedCheck.check(
  SlotBundle.pairs_entries(bundle) == [
    {1, %SlotPair{a: 11, b: 12}},
    {2, %SlotPair{a: 21, b: 22}},
    {3, %SlotPair{a: 31, b: 32}}
  ],
  "SlotBundle.pairs_entries yields 1-based {key, element} pairs"
)

pcfg =
  %PConfig{nested: bundle}
  |> PConfig.put_tokens_at(Slot.alpha(), 101)
  |> PConfig.put_tokens_at(Slot.gamma(), 303)
  |> PConfig.put_tiers_at(Tier.low(), 7)
  |> PConfig.put_tiers_at(Tier.high(), 9)
  |> PConfig.put_per_slot_at(Slot.beta(), %SlotPair{a: 55, b: 66})

sholder =
  %SparseHolder{}
  |> SparseHolder.put_sparse_at(Spare.one(), 111)
  |> SparseHolder.put_sparse_at(Spare.two(), 222)
  |> SparseHolder.put_sparse_at(Spare.max(), 444)

spkt =
  %SparsePacket{}
  |> SparsePacket.put_sparse_at(Keyedpkt.Spare.one(), 111)
  |> SparsePacket.put_sparse_at(Keyedpkt.Spare.two(), 222)
  |> SparsePacket.put_sparse_at(Keyedpkt.Spare.max(), 444)

KeyedCheck.check(PConfig.tokens_at(pcfg, Slot.alpha()) == 101, "tokens_at Alpha")
KeyedCheck.check(PConfig.tokens_at(pcfg, Slot.beta()) == 0, "tokens_at Beta untouched")
KeyedCheck.check(PConfig.tokens_at(pcfg, Slot.gamma()) == 303, "tokens_at Gamma")
KeyedCheck.check(PConfig.tokens_entries(pcfg) == [{1, 101}, {2, 0}, {3, 303}], "tokens_entries")
KeyedCheck.check(PConfig.tiers_entries(pcfg) == [{1, 7}, {2, 9}], "tiers_entries")
KeyedCheck.check(
  PConfig.per_slot_entries(pcfg) == [
    {1, %SlotPair{a: 1, b: 2}},
    {2, %SlotPair{a: 55, b: 66}},
    {3, %SlotPair{a: 1, b: 2}}
  ],
  "per_slot_entries"
)
KeyedCheck.check(
  SparseHolder.sparse_entries(sholder) == [{1, 111}, {2, 222}, {3, 0}, {4, 444}],
  "SparseHolder.sparse_entries covers 1..Spare.max (4)"
)
KeyedCheck.check(
  SparsePacket.sparse_entries(spkt) == [{1, 111}, {2, 222}, {3, 0}, {4, 444}],
  "SparsePacket.sparse_entries covers 1..Spare.max (4)"
)

pwire = ProbeFixed.keyed_config_fixed_save([pcfg])
{:ok, [ploaded], preport} = ProbeFixed.keyed_config_fixed_load(pwire)
KeyedCheck.check(preport.clamped == 0 and preport.unknown == 0, "ProbeFixed round-trip report clean")
KeyedCheck.check(ploaded == pcfg, "ProbeFixed round-trip preserves keyed slots")

# 3. Lower bound refusal: None (key 0) on every keyed field (table structs and type structs)
KeyedCheck.check_raises_none(fn -> KeyedConfig.teams_at(cfg, 0) end, "teams")
KeyedCheck.check_raises_none(fn -> KeyedConfig.put_teams_at(cfg, 0, %TeamConfig{}) end, "teams")
KeyedCheck.check_raises_none(fn -> KeyedConfig.hulls_at(cfg, 0) end, "hulls")
KeyedCheck.check_raises_none(fn -> KeyedConfig.put_hulls_at(cfg, 0, %HullConfig{}) end, "hulls")
KeyedCheck.check_raises_none(fn -> HullConfig.turrets_at(hull2, 0) end, "turrets")
KeyedCheck.check_raises_none(fn -> HullConfig.put_turrets_at(hull2, 0, %TurretConfig{}) end, "turrets")
KeyedCheck.check_raises_none(fn -> ScoreBoard.per_team_at(scores, 0) end, "per_team")
KeyedCheck.check_raises_none(fn -> ScoreBoard.put_per_team_at(scores, 0, 1) end, "per_team")
KeyedCheck.check_raises_none(fn -> SlotBundle.pairs_at(bundle, 0) end, "pairs")
KeyedCheck.check_raises_none(fn -> SlotBundle.put_pairs_at(bundle, 0, %SlotPair{}) end, "pairs")
KeyedCheck.check_raises_none(fn -> PConfig.tokens_at(pcfg, 0) end, "tokens")
KeyedCheck.check_raises_none(fn -> PConfig.put_tokens_at(pcfg, 0, 1) end, "tokens")
KeyedCheck.check_raises_none(fn -> PConfig.tiers_at(pcfg, 0) end, "tiers")
KeyedCheck.check_raises_none(fn -> PConfig.put_tiers_at(pcfg, 0, 1) end, "tiers")
KeyedCheck.check_raises_none(fn -> PConfig.per_slot_at(pcfg, 0) end, "per_slot")
KeyedCheck.check_raises_none(fn -> PConfig.put_per_slot_at(pcfg, 0, %SlotPair{}) end, "per_slot")
KeyedCheck.check_raises_none(fn -> SparseHolder.sparse_at(sholder, 0) end, "sparse")
KeyedCheck.check_raises_none(fn -> SparseHolder.put_sparse_at(sholder, 0, 1) end, "sparse")
KeyedCheck.check_raises_none(fn -> SparsePacket.sparse_at(spkt, 0) end, "sparse")
KeyedCheck.check_raises_none(fn -> SparsePacket.put_sparse_at(spkt, 0, 1) end, "sparse")

# 4. Upper bound refusal: E.Max + 1 on every keyed field (including Spare.max() + 1 == 5)
KeyedCheck.check_raises_max(fn -> KeyedConfig.teams_at(cfg, Team.max() + 1) end, "teams", 4, "Team", 3)
KeyedCheck.check_raises_max(fn -> KeyedConfig.put_teams_at(cfg, Team.max() + 1, %TeamConfig{}) end, "teams", 4, "Team", 3)
KeyedCheck.check_raises_max(fn -> KeyedConfig.hulls_at(cfg, Hull.max() + 1) end, "hulls", 4, "Hull", 3)
KeyedCheck.check_raises_max(fn -> KeyedConfig.put_hulls_at(cfg, Hull.max() + 1, %HullConfig{}) end, "hulls", 4, "Hull", 3)
KeyedCheck.check_raises_max(fn -> HullConfig.turrets_at(hull2, Weapon.max() + 1) end, "turrets", 4, "Weapon", 3)
KeyedCheck.check_raises_max(fn -> HullConfig.put_turrets_at(hull2, Weapon.max() + 1, %TurretConfig{}) end, "turrets", 4, "Weapon", 3)
KeyedCheck.check_raises_max(fn -> ScoreBoard.per_team_at(scores, Team.max() + 1) end, "per_team", 4, "Team", 3)
KeyedCheck.check_raises_max(fn -> ScoreBoard.put_per_team_at(scores, Team.max() + 1, 1) end, "per_team", 4, "Team", 3)
KeyedCheck.check_raises_max(fn -> SlotBundle.pairs_at(bundle, Slot.max() + 1) end, "pairs", 4, "Slot", 3)
KeyedCheck.check_raises_max(fn -> SlotBundle.put_pairs_at(bundle, Slot.max() + 1, %SlotPair{}) end, "pairs", 4, "Slot", 3)
KeyedCheck.check_raises_max(fn -> PConfig.tokens_at(pcfg, Slot.max() + 1) end, "tokens", 4, "Slot", 3)
KeyedCheck.check_raises_max(fn -> PConfig.put_tokens_at(pcfg, Slot.max() + 1, 1) end, "tokens", 4, "Slot", 3)
KeyedCheck.check_raises_max(fn -> PConfig.tiers_at(pcfg, Tier.max() + 1) end, "tiers", 3, "Tier", 2)
KeyedCheck.check_raises_max(fn -> PConfig.put_tiers_at(pcfg, Tier.max() + 1, 1) end, "tiers", 3, "Tier", 2)
KeyedCheck.check_raises_max(fn -> PConfig.per_slot_at(pcfg, Slot.max() + 1) end, "per_slot", 4, "Slot", 3)
KeyedCheck.check_raises_max(fn -> PConfig.put_per_slot_at(pcfg, Slot.max() + 1, %SlotPair{}) end, "per_slot", 4, "Slot", 3)
KeyedCheck.check_raises_max(fn -> SparseHolder.sparse_at(sholder, Spare.max() + 1) end, "sparse", 5, "Spare", 4)
KeyedCheck.check_raises_max(fn -> SparseHolder.put_sparse_at(sholder, Spare.max() + 1, 1) end, "sparse", 5, "Spare", 4)
KeyedCheck.check_raises_max(fn -> SparsePacket.sparse_at(spkt, Keyedpkt.Spare.max() + 1) end, "sparse", 5, "Spare", 4)
KeyedCheck.check_raises_max(fn -> SparsePacket.put_sparse_at(spkt, Keyedpkt.Spare.max() + 1, 1) end, "sparse", 5, "Spare", 4)

# 5. Negative and non-integer keys also raise ArgumentError
KeyedCheck.check_raises_invalid(fn -> KeyedConfig.teams_at(cfg, -1) end, "teams", "Team")
KeyedCheck.check_raises_invalid(fn -> KeyedConfig.put_teams_at(cfg, -1, %TeamConfig{}) end, "teams", "Team")
KeyedCheck.check_raises_invalid(fn -> KeyedConfig.teams_at(cfg, :alpha) end, "teams", "Team")
KeyedCheck.check_raises_invalid(fn -> ScoreBoard.per_team_at(scores, -1) end, "per_team", "Team")
KeyedCheck.check_raises_invalid(fn -> ScoreBoard.per_team_at(scores, :alpha) end, "per_team", "Team")
KeyedCheck.check_raises_invalid(fn -> SlotBundle.pairs_at(bundle, -1) end, "pairs", "Slot")
KeyedCheck.check_raises_invalid(fn -> SlotBundle.pairs_at(bundle, :alpha) end, "pairs", "Slot")
KeyedCheck.check_raises_invalid(fn -> SparseHolder.sparse_at(sholder, -1) end, "sparse", "Spare")
KeyedCheck.check_raises_invalid(fn -> SparseHolder.sparse_at(sholder, :alpha) end, "sparse", "Spare")
KeyedCheck.check_raises_invalid(fn -> SparsePacket.sparse_at(spkt, -1) end, "sparse", "Spare")
KeyedCheck.check_raises_invalid(fn -> SparsePacket.sparse_at(spkt, :alpha) end, "sparse", "Spare")

IO.puts("ok")
`
	probePath := filepath.Join(dir, "probe.exs")
	if err := os.WriteFile(probePath, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	var args []string
	for _, n := range keyedNames {
		args = append(args, "-r", filepath.Join(keyedDir, n))
	}
	for _, n := range probeNames {
		args = append(args, "-r", filepath.Join(probeDir, n))
	}
	for _, n := range pktNames {
		args = append(args, "-r", filepath.Join(pktDir, n))
	}
	args = append(args, probePath)
	cmd := exec.Command(elixirBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("keyed probe failed: %v\n%s", err, out)
	}
}

func writeKeyedModules(t *testing.T, dir string, files map[string][]byte) []string {
	t.Helper()
	var names []string
	for name, data := range files {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".ex") {
			continue
		}
		if name == "Table.ex" || name == "TableRuntime.ex" || name == "TableMessage.ex" ||
			name == "CookRuntime.ex" || name == "BlockRuntime.ex" ||
			strings.HasSuffix(name, "Cook.ex") || strings.HasSuffix(name, "Block.ex") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	return sortedRuntimeFirst(names)
}
