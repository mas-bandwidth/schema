package elixirtable

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRetainReadPadsEvolvedFixedArrayArm pins docs/SPEC-TABLES.md sections 3
// and 4: a lawful older fixed-array union arm reads with the newer build's
// element defaults, and that public value saves again through the retaining
// API without caller repair.
func TestRetainReadPadsEvolvedFixedArrayArm(t *testing.T) {
	t.Parallel()
	elixirBin := elixirBinary(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	peer := loadUnit(t, filepath.Join(root, "test", "tables", "widenbound", "WidenBoundPeer.schema"))
	peerDir := t.TempDir()
	peerNames := writeModuleDir(t, peerDir, generateElixir(t, peer))
	peerOut := runProbe(t, elixirBin, peerDir, peerNames, `
value = %{"sequence" => 41, "body" => {"spans", [5]}}
{:ok, wire} = Widenboundpeer.Table.save("ToolEnvelope", value)
IO.puts(Base.encode16(wire, case: :lower))
`)
	wireHex := strings.TrimSpace(peerOut)
	if wireHex == "" {
		t.Fatal("the older ordinary writer returned no wire")
	}

	current := loadUnit(t, filepath.Join(root, "tables", "messages", "Messages.schema"))
	currentDir := t.TempDir()
	currentNames := writeModuleDir(t, currentDir, generateElixir(t, current))
	probe := `
wire = Base.decode16!("` + wireHex + `", case: :lower)
retain = %{records: [], used: 0, cap: 1048576, ids: [], id_cap: 131072}
report = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
{:ok, plain, _plain_report} = Messagedemo.Table.load("ToolEnvelope", wire)
{:ok, value, report, retain} = Messagedemo.Table.load_retain("ToolEnvelope", wire, retain, report)
unless value == plain,
  do: raise("plain and retaining reads disagree: " <> inspect({plain, value}))
unless value["body"] == {"spans", [5, 0]},
  do: raise("retaining read did not fill the newer fixed-array slot: " <> inspect(value))
{:ok, plain_saved} = Messagedemo.Table.save("ToolEnvelope", plain)
{:ok, saved, _report, _retain} = Messagedemo.Table.save_retain("ToolEnvelope", value, retain, report)
unless byte_size(saved) > 0 and saved == plain_saved,
  do: raise("plain and retaining saves disagree")
unless Messagedemo.Table.measure_retain("ToolEnvelope", value, retain) == byte_size(saved),
  do: raise("retaining measure disagrees with its save")
IO.puts("ok")
`
	runProbe(t, elixirBin, currentDir, currentNames, probe)
}
