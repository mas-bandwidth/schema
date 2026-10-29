package elixirtable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixir"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// generateElixir lowers a unit through the packet and table emitters.
func generateElixir(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	files, err := elixir.Generate(u)
	if err != nil {
		t.Fatalf("generate packet: %v", err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("generate table: %v", err)
	}
	maps.Copy(files, tables)
	return files
}

// writeModuleDir writes every generated .ex to dir and returns the file names.
func writeModuleDir(t *testing.T, dir string, files map[string][]byte) []string {
	t.Helper()
	var names []string
	for name, data := range files {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".ex") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	// BuildVersion.ex must load FIRST: a Cook or Block module names its
	// build_version at COMPILE time through a module attribute, and `elixir -r`
	// loads files in the order given, so a sorted order that put <Base>Cook.ex
	// before BuildVersion.ex would fail to compile the corpus.
	// The packet codecs' support module loads first for the same reason: every
	// file module imports from it at COMPILE time.
	sort.Strings(names)
	first := []string{BuildVersionModule + ".ex"}
	rest := make([]string, 0, len(names))
	for _, n := range names {
		switch n {
		case BuildVersionModule + ".ex":
		case "PacketRuntime.ex":
			first = append(first, n)
		default:
			rest = append(rest, n)
		}
	}
	return append(first, rest...)
}

// runProbe writes probe.exs beside the generated modules and runs it with the
// leg's own toolchain, returning the combined output.
func runProbe(t *testing.T, elixirBin, dir string, names []string, probe string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{}
	for _, n := range names {
		args = append(args, "-r", n)
	}
	args = append(args, "probe.exs")
	cmd := exec.Command(elixirBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	return string(out)
}

// TestCardTextFormMatchesGo pins docs/SPEC-TABLES.md §16, §16.2, §16.3: the
// JSON text form of tables/cards. The loaded wire's text equals the committed
// card-populated.json byte for byte, and that text read back re-saves to the
// wire byte for byte; an unknown property, an out-of-range enum name and an
// over-bound string are counted, never silently truncated.
func TestCardTextFormMatchesGo(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "tables", "cards", "Card.schema"))
	files := generateElixir(t, u)
	dir := t.TempDir()
	names := writeModuleDir(t, dir, files)
	wire := filepath.Join(root, "tables", "cards", "fuzz", "card-populated.wire")
	json := filepath.Join(root, "tables", "cards", "fuzz", "card-populated.json")
	probe := `
defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
wire = File.read!(` + elixirString(wire) + `)
json = File.read!(` + elixirString(json) + `)
{:ok, value, report} = Cards.Table.load("Card", wire)
Probe.check(report.malformed == false, "load is clean")
{:ok, text} = Cards.Table.to_json("Card", value)
Probe.check(text == json, "to_json matches the committed JSON byte for byte")
{:ok, back, back_report} = Cards.Table.from_json("Card", json)
Probe.check(back_report.malformed == false, "from_json is clean")
{:ok, out} = Cards.Table.save("Card", back)
Probe.check(out == wire, "the text read back re-saves byte for byte")
unknown = String.replace(json, "\"id\":", "\"mystery\": 7,\n  \"id\":")
{:ok, _uv, ur} = Cards.Table.from_json("Card", unknown)
Probe.check(ur.unknown == 1, "an unknown property is counted: #{inspect(ur)}")
bad_enum = String.replace(json, "\"tier\": \"flash\"", "\"tier\": \"nonexistent\"")
{:ok, bv, br} = Cards.Table.from_json("Card", bad_enum)
Probe.check(br.unknown == 1 and bv["tier"] == nil, "an out-of-range enum reads None and is counted: #{inspect(br)} #{inspect(bv["tier"])}")
over = String.replace(json, "\"id\": \"populated\"", "\"id\": \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"")
{:ok, ov, orr} = Cards.Table.from_json("Card", over)
Probe.check(orr.clamped == 1 and byte_size(ov["id"]) == 64, "an over-bound string clamps and counts, never truncates: #{inspect(orr)} #{byte_size(ov["id"])}")
IO.puts("ok")
`
	runProbe(t, elixirBin, dir, names, probe)
}

// TestCardConstructsRoundTripTheVariableForm pins docs/SPEC-TABLES.md §3, §4,
// §5: tables/cards generates in Elixir, the committed wire loads to the values
// card-populated.json names and re-saves byte for byte, and an older declaration
// reads a newer wire finding the `was` rename, skipping and counting the added
// field, and defaulting the dropped one.
func TestCardConstructsRoundTripTheVariableForm(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	// ---- the card itself: byte for byte ----
	u := loadUnit(t, filepath.Join(root, "tables", "cards", "Card.schema"))
	files := generateElixir(t, u)
	if _, ok := files["Table.ex"]; !ok {
		names := make([]string, 0, len(files))
		for k := range files {
			names = append(names, k)
		}
		t.Fatalf("tables/cards generated no variable form module; got %v", names)
	}
	dir := t.TempDir()
	names := writeModuleDir(t, dir, files)
	wire := filepath.Join(root, "tables", "cards", "fuzz", "card-populated.wire")
	probe := `
defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
wire = File.read!(` + elixirString(wire) + `)
{:ok, value, report} = Cards.Table.load("Card", wire)
Probe.check(report == %{unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, malformed: false, retained: 0, retain_lost: 0, blob_shared: 0, reason: ""}, "report: #{inspect(report)}")
Probe.check(value["id"] == "populated", "id")
Probe.check(value["sha"] == "0123456789012345678901234567890123456789", "sha")
Probe.check(value["tier"] == "flash", "tier")
Probe.check(value["priority"] == "critical", "priority")
Probe.check(value["column"] == "review", "column")
Probe.check(value["deadline_minutes"] == 45, "deadline_minutes")
Probe.check(value["epoch"] == 7, "epoch")
Probe.check(value["attempt"] == 1, "attempt")
Probe.check(value["done"] == "done when", "done")
Probe.check(length(value["paths"]) == 2, "paths length")
Probe.check(String.starts_with?(value["body"], "body with multibyte"), "body")
Probe.check(value["extra"] in [nil, []], "extra")
{:ok, out} = Cards.Table.save("Card", value)
Probe.check(out == wire, "re-save is byte for byte")
IO.puts("ok")
`
	runProbe(t, elixirBin, dir, names, probe)

	// ---- the two-declaration pair: was, unknown, default ----
	td := filepath.Join("testdata", "cards")
	newU := loadUnit(t, filepath.Join(td, "new.schema"))
	newFiles := generateElixir(t, newU)
	newDir := t.TempDir()
	newNames := writeModuleDir(t, newDir, newFiles)
	wirePath := filepath.Join(t.TempDir(), "new.wire")
	saveProbe := `
wire = Cardpair.Table.save("Card", %{"caption" => "hello", "priority" => 7})
case wire do
  {:ok, bytes} -> File.write!(` + elixirString(wirePath) + `, bytes)
  other -> IO.inspect(other, label: "save"); System.halt(1)
end
IO.puts("saved")
`
	runProbe(t, elixirBin, newDir, newNames, saveProbe)

	oldU := loadUnit(t, filepath.Join(td, "old.schema"))
	oldFiles := generateElixir(t, oldU)
	oldDir := t.TempDir()
	oldNames := writeModuleDir(t, oldDir, oldFiles)
	loadProbe := `
defmodule Probe do
  def check(true, _), do: :ok
  def check(false, what) do
    IO.puts(:stderr, "FAIL: " <> what)
    System.halt(1)
  end
end
wire = File.read!(` + elixirString(wirePath) + `)
{:ok, value, report} = Cardpair.Table.load("Card", wire)
Probe.check(report.unknown == 1, "unknown is counted: #{inspect(report)}")
Probe.check(value["label"] == "hello", "was rename found: #{inspect(value["label"])}")
Probe.check(value["extra"] == "", "absent field defaults: #{inspect(value["extra"])}")
IO.puts("ok")
`
	runProbe(t, elixirBin, oldDir, oldNames, loadProbe)
}
