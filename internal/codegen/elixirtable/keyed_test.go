package elixirtable

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixir"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
  |> KeyedConfig.put_teams_at(Team.to_int(:red), %TeamConfig{spawn_count: 10, banner: "red"})
  |> KeyedConfig.put_teams_at(Team.to_int(:blue), %TeamConfig{spawn_count: 20, banner: "blue"})
  |> KeyedConfig.put_teams_at(Team.to_int(:green), %TeamConfig{spawn_count: 30, banner: "green"})

KeyedCheck.check(KeyedConfig.teams_at(cfg, Team.to_int(:red)).spawn_count == 10, "teams_at Red")
KeyedCheck.check(KeyedConfig.teams_at(cfg, Team.to_int(:blue)).spawn_count == 20, "teams_at Blue")
KeyedCheck.check(KeyedConfig.teams_at(cfg, Team.to_int(:green)).spawn_count == 30, "teams_at Green")
KeyedCheck.check(
  Enum.map(KeyedConfig.teams_entries(cfg), fn {k, v} -> {k, v.spawn_count, v.banner} end) ==
    [{1, 10, "red"}, {2, 20, "blue"}, {3, 30, "green"}],
  "teams_entries yields 1-based {key, element} pairs"
)

hull2 =
  %HullConfig{health: 250.0, mass: 5.0}
  |> HullConfig.put_turrets_at(Weapon.to_int(:cannon), %TurretConfig{damage: 11.0, cooldown: 0.25})
  |> HullConfig.put_turrets_at(Weapon.to_int(:missile), %TurretConfig{damage: 22.0, cooldown: 0.5})
  |> HullConfig.put_turrets_at(Weapon.to_int(:mine), %TurretConfig{damage: 33.0, cooldown: 1.0})

KeyedCheck.check(HullConfig.turrets_at(hull2, Weapon.to_int(:cannon)).damage == 11.0, "turrets_at Cannon")
KeyedCheck.check(HullConfig.turrets_at(hull2, Weapon.to_int(:missile)).damage == 22.0, "turrets_at Missile")
KeyedCheck.check(HullConfig.turrets_at(hull2, Weapon.to_int(:mine)).damage == 33.0, "turrets_at Mine")
KeyedCheck.check(
  Enum.map(HullConfig.turrets_entries(hull2), fn {k, t} -> {k, t.damage} end) ==
    [{1, 11.0}, {2, 22.0}, {3, 33.0}],
  "turrets_entries yields 1-based {key, element} pairs"
)

cfg = KeyedConfig.put_hulls_at(cfg, Hull.to_int(:gunship), hull2)
KeyedCheck.check(KeyedConfig.hulls_at(cfg, Hull.to_int(:interceptor)).health == 100.0, "hulls_at Interceptor untouched")
KeyedCheck.check(KeyedConfig.hulls_at(cfg, Hull.to_int(:gunship)).health == 250.0, "hulls_at Gunship updated")
KeyedCheck.check(KeyedConfig.hulls_at(cfg, Hull.to_int(:freighter)).health == 100.0, "hulls_at Freighter untouched")
KeyedCheck.check(
  Enum.map(KeyedConfig.hulls_entries(cfg), fn {k, h} -> {k, h.health} end) ==
    [{1, 100.0}, {2, 250.0}, {3, 100.0}],
  "hulls_entries yields 1-based {key, element} pairs"
)

scores =
  %ScoreBoard{}
  |> ScoreBoard.put_per_team_at(Team.to_int(:red), 1000)
  |> ScoreBoard.put_per_team_at(Team.to_int(:blue), 2000)
  |> ScoreBoard.put_per_team_at(Team.to_int(:green), 3000)

KeyedCheck.check(ScoreBoard.per_team_at(scores, Team.to_int(:red)) == 1000, "ScoreBoard.per_team_at Red")
KeyedCheck.check(ScoreBoard.per_team_at(scores, Team.to_int(:blue)) == 2000, "ScoreBoard.per_team_at Blue")
KeyedCheck.check(ScoreBoard.per_team_at(scores, Team.to_int(:green)) == 3000, "ScoreBoard.per_team_at Green")
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
    KeyedConfig.hulls_at(loaded, Hull.to_int(:gunship)).health == 250.0 and
    HullConfig.turrets_at(KeyedConfig.hulls_at(loaded, Hull.to_int(:gunship)), Weapon.to_int(:missile)).damage == 22.0 and
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
  |> SlotBundle.put_pairs_at(Slot.to_int(:alpha), %SlotPair{a: 11, b: 12})
  |> SlotBundle.put_pairs_at(Slot.to_int(:beta), %SlotPair{a: 21, b: 22})
  |> SlotBundle.put_pairs_at(Slot.to_int(:gamma), %SlotPair{a: 31, b: 32})

KeyedCheck.check(SlotBundle.pairs_at(bundle, Slot.to_int(:alpha)) == %SlotPair{a: 11, b: 12}, "SlotBundle.pairs_at Alpha")
KeyedCheck.check(SlotBundle.pairs_at(bundle, Slot.to_int(:beta)) == %SlotPair{a: 21, b: 22}, "SlotBundle.pairs_at Beta")
KeyedCheck.check(SlotBundle.pairs_at(bundle, Slot.to_int(:gamma)) == %SlotPair{a: 31, b: 32}, "SlotBundle.pairs_at Gamma")
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
  |> PConfig.put_tokens_at(Slot.to_int(:alpha), 101)
  |> PConfig.put_tokens_at(Slot.to_int(:gamma), 303)
  |> PConfig.put_tiers_at(Tier.to_int(:low), 7)
  |> PConfig.put_tiers_at(Tier.to_int(:high), 9)
  |> PConfig.put_per_slot_at(Slot.to_int(:beta), %SlotPair{a: 55, b: 66})

sholder =
  %SparseHolder{}
  |> SparseHolder.put_sparse_at(Spare.to_int(:one), 111)
  |> SparseHolder.put_sparse_at(Spare.to_int(:two), 222)
  |> SparseHolder.put_sparse_at(Spare.max(), 444)

spkt =
  %SparsePacket{}
  |> SparsePacket.put_sparse_at(Keyedpkt.Spare.to_int(:one), 111)
  |> SparsePacket.put_sparse_at(Keyedpkt.Spare.to_int(:two), 222)
  |> SparsePacket.put_sparse_at(Keyedpkt.Spare.max(), 444)

KeyedCheck.check(PConfig.tokens_at(pcfg, Slot.to_int(:alpha)) == 101, "tokens_at Alpha")
KeyedCheck.check(PConfig.tokens_at(pcfg, Slot.to_int(:beta)) == 0, "tokens_at Beta untouched")
KeyedCheck.check(PConfig.tokens_at(pcfg, Slot.to_int(:gamma)) == 303, "tokens_at Gamma")
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
KeyedCheck.check_raises_invalid(fn -> SlotBundle.pairs_at(bundle, :omega) end, "pairs", "Slot")
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

// An unknown enum-keyed array slot in variable form must skip its length-framed
// element bytes without decoding them, and a damaged slot key ref must stop the
// keyed-array body immediately (docs/SPEC-TABLES.md §3.2, §4, §6.6).
func TestUnknownKeyedSlotSkipsElementByLengthElixir(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	v2Path := filepath.Join("..", "..", "..", "test", "tables", "V2.schema")
	u := loadUnit(t, v2Path)
	root := u.Tables["Cfg"]
	if root == nil {
		t.Fatal("V2.schema declares no Cfg root")
	}
	model := tabletext.NewModel(u)

	const mutantHex = "010104030000000210220d03030c04040b000000050c0261310006070404160000000007000404210000000008101404030304650000000604660000000904680000000a10081e0203010b07010c0d0e0e04030700000000000000090000000e0d070f0a0000c84000100429000000111e0b008cec01864cdc63afb7f401aa9bdf1a9a8be145c2cb04da4fe6b4e78a35d1f9ee3d62cb8fecfcf7398794b696a762b5a0ca487f91235e50e2132b7121f0b8fa9321f3285edc4dc7115c80b0b211ca9752b318320d7ec916c491c5c4aa397792c46cbd988a923b0d4a69cb79a912ee29fda6ecee9a99eb45458959b62eef846f1ec0ca7ecea23f3b1f1100000000000000"
	const mutantSHA256 = "2f94fd221fa801921b3c2014429d8e4acdb257424efdeaeba4ebf1059792d660"

	mutant, err := hex.DecodeString(mutantHex)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(mutant)); got != mutantSHA256 {
		t.Fatalf("mutant sha256 = %s, want %s", got, mutantSHA256)
	}
	if size, whole, refused := tablewire.FileRegionMeasure(u, root, mutant); size != 320 || !whole || refused {
		t.Fatalf("FileRegionMeasure(mutant) = (%d, %v, %v), want (320, true, false)", size, whole, refused)
	}

	val := model.New(root)
	var report tabletext.Report
	ok, err := tablewire.Decode(model, val, mutant, &report)
	if !ok || err != nil || report.Malformed || report.Unknown != 2 || report.KindMismatch != 2 {
		t.Fatalf("Decode(mutant) = (%v, %v, %+v)", ok, err, report)
	}
	wantSave, err := tablewire.Encode(model, val)
	if err != nil || len(wantSave) != 192 {
		t.Fatalf("Encode(mutant) = (%d bytes, %v), want 192 bytes", len(wantSave), err)
	}

	v2Src, err := os.ReadFile(v2Path)
	if err != nil {
		t.Fatal(err)
	}
	retainSchema := strings.Replace(
		strings.Replace(string(v2Src), "package tblv2", "package tblv2retain", 1),
		"fixed table Cfg", "table Cfg", 1,
	)
	retainUnit := unitFrom(t, retainSchema)
	retainModel := tabletext.NewModel(retainUnit)
	rval := retainModel.New(retainUnit.Tables["Cfg"])
	var rreport tabletext.Report
	rt := tablewire.Retain{Capacity: 4096, IdCapacity: 512}
	ok, err = tablewire.DecodeRetain(retainModel, rval, mutant, &rt, &rreport)
	if !ok || err != nil || rreport.Malformed || rreport.Unknown != 2 || rreport.KindMismatch != 2 {
		t.Fatalf("DecodeRetain(mutant) = (%v, %v, %+v)", ok, err, rreport)
	}
	var sreport tabletext.Report
	wantRetainSave, err := tablewire.EncodeRetain(retainModel, rval, &rt, &sreport)
	if err != nil || len(wantRetainSave) != 192 {
		t.Fatalf("EncodeRetain(mutant) = (%d bytes, %v), want 192 bytes", len(wantRetainSave), err)
	}

	badKey := append([]byte(nil), mutant...)
	if badKey[12] != 0x03 {
		t.Fatalf("bank pair 1 key ref moved: byte 12 = 0x%02x, want 0x03", badKey[12])
	}
	badKey[12] = 0x00
	var badReport tabletext.Report
	if ok, err := tablewire.Decode(model, model.New(root), badKey, &badReport); ok || err != nil || !badReport.Malformed || badReport.Unknown != 1 {
		t.Fatalf("Decode(badKey) = (%v, %v, %+v), want ok=false malformed=true unknown=1", ok, err, badReport)
	}

	files := generateElixir(t, u)
	retainFiles := generateElixir(t, retainUnit)
	files["RetainTable.ex"] = retainFiles["Table.ex"]

	probe := fmt.Sprintf(`
mutant = Base.decode16!(%q, case: :lower)
want_save = Base.decode16!(%q, case: :lower)
want_retain_save = Base.decode16!(%q, case: :lower)
bad_key = Base.decode16!(%q, case: :lower)

if Tblv2.Table.load_measure("Cfg", mutant) != 320 do
  raise("expected 320 for mutant load_measure, got " <> inspect(Tblv2.Table.load_measure("Cfg", mutant)))
end

case Tblv2.Table.load("Cfg", mutant) do
  {:ok, v, r} ->
    if r.unknown != 2 or r.kind_mismatch != 2 or r.widened != 0 or r.clamped != 0 or r.malformed do
      raise("unexpected plain report: " <> inspect(r))
    end
    case Tblv2.Table.save("Cfg", v) do
      {:ok, ^want_save} -> :ok
      other -> raise("plain save mismatch: " <> inspect(other))
    end
  other ->
    raise("unexpected plain load result: " <> inspect(other))
end

empty = %%{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0}
store = %%{records: [], used: 0, cap: 4096, ids: [], id_cap: 512}
case Tblv2retain.Table.load_retain("Cfg", mutant, store, empty) do
  {:ok, rv, rr, retain} ->
    if rr.unknown != %d or rr.kind_mismatch != %d or rr.retained != %d or rr.retain_lost != %d or rr.malformed do
      raise("unexpected retain load report: " <> inspect(rr))
    end
    case Tblv2retain.Table.save_retain("Cfg", rv, retain, empty) do
      {:ok, ^want_retain_save, sr, _retain} ->
        if sr.retained != %d or sr.retain_lost != %d or sr.malformed do
          raise("unexpected retain save report: " <> inspect(sr))
        end
      other ->
        raise("retain save mismatch: " <> inspect(other))
    end
  other ->
    raise("unexpected retain load result: " <> inspect(other))
end

case Tblv2.Table.load("Cfg", bad_key) do
  {:error, :malformed, r} when r.malformed == true and r.unknown == 1 -> :ok
  other -> raise("expected bad_key plain refusal with unknown=1, got " <> inspect(other))
end

case Tblv2retain.Table.load_retain("Cfg", bad_key, store, empty) do
  {:error, :malformed, r, _retain} when r.malformed == true and r.unknown == 1 -> :ok
  other -> raise("expected bad_key retain refusal with unknown=1, got " <> inspect(other))
end

IO.puts("UNKNOWN_KEYED_SLOT_OK")
`,
		mutantHex,
		hex.EncodeToString(wantSave),
		hex.EncodeToString(wantRetainSave),
		hex.EncodeToString(badKey),
		rreport.Unknown, rreport.KindMismatch, rreport.Retained, rreport.RetainLost,
		sreport.Retained, sreport.RetainLost,
	)

	out, err := runArmProbe(t, elixirBinary(t), files, probe)
	if err != nil || !strings.Contains(out, "UNKNOWN_KEYED_SLOT_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("unknown keyed slot probe failed: %v\n%s", err, out)
	}

	// Negative control: decoding the slot element before checking whether its key
	// names a known slot contaminates the report with malformed=true on unknown
	// slots whose body was mutated inside the outer keyed-array frame.
	const target = `      case Map.fetch(f.key_by_id, kid) do
        {:ok, name} ->
          {v, report} = read_keyed_elem_plain(f, item, elem, nodes, ids, report)
          read_keyed_pairs(f, rest, n - 1, nodes, ids, Map.put(acc, name, v), report)

        :error ->
          read_keyed_pairs(
            f,
            rest,
            n - 1,
            nodes,
            ids,
            acc,
            %{report | unknown: report.unknown + 1}
          )
      end`
	const sabotage = `      {v, report} = read_keyed_elem_plain(f, item, elem, nodes, ids, report)
      case Map.fetch(f.key_by_id, kid) do
        {:ok, name} ->
          read_keyed_pairs(f, rest, n - 1, nodes, ids, Map.put(acc, name, v), report)

        :error ->
          read_keyed_pairs(
            f,
            rest,
            n - 1,
            nodes,
            ids,
            acc,
            %{report | unknown: report.unknown + 1}
          )
      end`
	tableEx := string(files["Table.ex"])
	if !strings.Contains(tableEx, target) {
		t.Fatal("negative control target not found in generated Table.ex")
	}
	badFiles := make(map[string][]byte, len(files))
	for k, v := range files {
		badFiles[k] = v
	}
	badFiles["Table.ex"] = []byte(strings.Replace(tableEx, target, sabotage, 1))
	if badOut, badErr := runArmProbe(t, elixirBinary(t), badFiles, probe); badErr == nil {
		t.Fatalf("negative control passed unexpectedly:\n%s", badOut)
	}
}
