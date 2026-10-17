package lua

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaTypeWireWritesTheGoldens generates examples/*.schema into a
// t.TempDir, builds each instance the Go cross-language test builds
// (test/go/main.go) before a goldenWire call, writes it in Lua, and
// byte-compares against testdata/wire/<name>.bin (docs/PORT-LUA.md; the C++
// reference's bytes are the judge, never the Lua leg's own output).
func TestLuaTypeWireWritesTheGoldens(t *testing.T) {
	t.Parallel()
	u := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	luarun.WriteHexProbe(t, dir)

	var probe strings.Builder
	probe.WriteString("local hexline = require(\"hexline\")\n")
	probe.WriteString("local serialize = require(\"serialize\")\n")
	type golden struct {
		name  string
		probe string
		bits  int64
	}
	goldens := []golden{
		{"chat", chatProbe, 0},
		{"probe_header", probeHeaderProbe, 0},
	}
	for _, g := range goldens {
		probe.WriteString("do\n")
		probe.WriteString(g.probe)
		probe.WriteString("  print(hexline(stream:flush()))\n")
		probe.WriteString("end\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luarun.Run(t, dir, "main.lua", nil)
	lines := luarun.HexLines(t, out)
	if len(lines) != len(goldens) {
		t.Fatalf("probe produced %d wire lines, want %d", len(lines), len(goldens))
	}
	for i, g := range goldens {
		golden, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", g.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(lines[i], golden) {
			t.Errorf("wire golden %s: lua bytes %x != C++-pinned %x", g.name, lines[i], golden)
		}
	}
}

const chatProbe = `
  local Chat = require("Chat")
  local inst = Chat.NewChat()
  inst.Text = "wire parity"
  local stream = serialize.WriteStream()
  Chat.WriteChat(stream, in)
`

const probeHeaderProbe = `
  local ProbeHeader = require("ProbeHeader")
  local inst = ProbeHeader.NewProbeHeader()
  inst.Version = 5
  inst.ProbeId = 0x1122334455667788
  local stream = serialize.WriteStream()
  ProbeHeader.WriteProbeHeader(stream, in)
`

// TestLuaTypeWireWriteNegativeControl patches the emitted write in memory (one
// anchor, matched exactly once) so one bool writes two bits, and requires the
// byte comparison to go red.
func TestLuaTypeWireWriteNegativeControl(t *testing.T) {
	t.Parallel()
	u := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	luarun.WriteHexProbe(t, dir)

	types := files["Types.lua"]
	anchor := "serializeBool(stream, v.HasFlags)"
	idx := strings.Index(string(types), anchor)
	if idx < 0 {
		t.Fatalf("negative-control anchor %q not found in emitted Types.lua", anchor)
	}
	if strings.Count(string(types), anchor) != 1 {
		t.Fatalf("negative-control anchor %q matched more than once", anchor)
	}
	patched := string(types[:idx]) + "serializeBool(stream, v.HasFlags); serializeBool(stream, v.HasFlags)" + string(types[idx+len(anchor):])
	files["Types.lua"] = []byte(patched)
	if err := os.WriteFile(filepath.Join(dir, "Types.lua"), files["Types.lua"], 0o644); err != nil {
		t.Fatal(err)
	}

	probe := "local hexline = require(\"hexline\")\n" +
		"local serialize = require(\"serialize\")\n" +
		"local Types = require(\"Types\")\n" +
		"local inst = Types.NewShipCreate()\n" +
		"inst.HasFlags = true\n" +
		"inst.Flags = Types.ShipFlags.Boosting | Types.ShipFlags.Aiming\n" +
		"local stream = serialize.WriteStream()\n" +
		"Types.WriteShipCreate(stream, inst)\n" +
		"print(hexline(stream:flush()))\n"
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luarun.Run(t, dir, "main.lua", nil)
	lines := luarun.HexLines(t, out)
	golden, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "shipcreate_flags.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(lines[0], golden) {
		t.Errorf("negative control: patched write produced the same bytes as the golden")
	}
}
