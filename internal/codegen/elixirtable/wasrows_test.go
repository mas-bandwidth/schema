package elixirtable_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The renamed unit: one enum variant, one packet-union arm and one type field
// each carry a `was` alias (docs/SPEC-TABLES.md §5). The variable form's
// descriptors hash each under its old name, so a writer from before the rename
// still reads as the new name.
const wasRowsRenamedSchema = `package wrows

enum Grade
{
    Bronze,
    Argent | was = "Silver"
    Gold
}

type Buff
{
    mult float32 = 1.0 | was = "multiplier"
}

type Ward
{
    charge float32 = 0.0
}

union Effect
{
    shield Ward | was = "ward"
    pong | was = "ping"
    tally int32
}

fixed table Cfg
{
    grade Grade
    effect Effect
    buff  Buff
}
`

const wasRowsOriginalSchema = `package wrows

enum Grade
{
    Bronze,
    Silver,
    Gold
}

type Buff
{
    multiplier float32 = 1.0
}

type Ward
{
    charge float32 = 0.0
}

union Effect
{
    ward Ward
    ping
    tally int32
}

fixed table Cfg
{
    grade Grade
    effect Effect
    buff  Buff
}
`

const wasRowsOldValue = `{"grade":"Silver","effect":{"ward":{"charge":1.5}},"buff":{"multiplier":2.0}}`

// loadWasRowsUnit is the compiler's path (GatherPaths, Load), so a refusal the
// target makes is the one the test sees: the elixirtable package's own tests
// cannot import compiler (target_elixir.go imports elixirtable), hence the
// external test package.
func loadWasRowsUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Wrows.schema")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatalf("gather paths: %v", err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load unit: %v", err)
	}
	return u
}

// TestElixirWasRowsCarried pins that the Elixir build carries the three `was`
// rows (docs/SPEC-TABLES.md §5): an enum variant's, a packet-union arm's and a
// type field's alias. An older writer's bytes — the engine over the OLD unit —
// are read by the NEW Elixir build and re-saved byte-for-byte, and the engine's
// own read of those bytes under the new unit names the new variants, arms and
// fields.
// Functional (elixirBinary): run with SCHEMA_REQUIRE_CORPUS=1.
func TestElixirWasRowsCarried(t *testing.T) {
	t.Parallel()
	elixirBin := elixirtable.ElixirBinaryForTest(t)
	newUnit := loadWasRowsUnit(t, wasRowsRenamedSchema)
	oldUnit := loadWasRowsUnit(t, wasRowsOriginalSchema)

	// THE COMPILER PATH (schema generate --lang elixir): the target's own
	// refuseWasRows runs here, so this is red at a base that does not register
	// elixir as a was-row carrier.
	files, err := compiler.New().Generate(newUnit, "elixir", compiler.Options{})
	if err != nil {
		t.Fatalf("schema generate --lang elixir refused the renamed unit: %v", err)
	}

	// the older writer's bytes from the engine over the old unit
	mOld := tabletext.NewModel(oldUnit)
	instOld := mOld.New(mOld.Lookup("Cfg"))
	var r tabletext.Report
	if !mOld.Read(instOld, []byte(wasRowsOldValue), &r) || !r.Silent() {
		t.Fatalf("old value did not place cleanly: %+v", r)
	}
	oldWire, err := tablewire.Encode(mOld, instOld)
	if err != nil {
		t.Fatalf("engine encode (old unit): %v", err)
	}

	// the engine's read of those bytes under the NEW unit is the expected value
	mNew := tabletext.NewModel(newUnit)
	instNew := mNew.New(mNew.Lookup("Cfg"))
	var r2 tabletext.Report
	if _, err := tablewire.Decode(mNew, instNew, oldWire, &r2); err != nil {
		t.Fatalf("engine decode (new unit): %v", err)
	}
	gotText, err := mNew.Write(instNew)
	if err != nil {
		t.Fatalf("engine write (new unit): %v", err)
	}
	for _, name := range []string{"Argent", "shield", "mult"} {
		if !strings.Contains(string(gotText), name) {
			t.Errorf("engine's read of the old bytes does not name the new %q: %s", name, gotText)
		}
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "old.wire"), oldWire, 0o600); err != nil {
		t.Fatal(err)
	}
	names := elixirtable.WriteModuleDirForTest(t, dir, files)
	probe := `
defmodule WasRowsProbe do
  def run(dir, want_hex) do
    {:ok, wire} = File.read(Path.join(dir, "old.wire"))
    {:ok, value, _report} = Wrows.Table.load("Cfg", wire)
    {:ok, resaved} = Wrows.Table.save("Cfg", value)
    got_hex = Base.encode16(resaved, case: :lower)
    if got_hex != want_hex do
      IO.puts(:stderr, "FAIL: resave differs: got " <> got_hex)
      IO.puts(:stderr, "FAIL: expected: " <> want_hex)
      System.halt(1)
    end
    IO.puts("RESAVE-OK")
  end
end

WasRowsProbe.run(` + strconv.Quote(dir) + `, ` + strconv.Quote(hex.EncodeToString(oldWire)) + `)
`
	out := elixirtable.RunProbeForTest(t, elixirBin, dir, names, probe)
	if !strings.Contains(out, "RESAVE-OK") {
		t.Errorf("new Elixir build did not re-save the old writer's bytes identically:\n%s", out)
	}
}
