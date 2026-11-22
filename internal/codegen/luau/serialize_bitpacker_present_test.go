package luau

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE VENDORED BITPACKER FAMILY ALWAYS LOADS (SF-33). serialize_bytes.luau,
// vendored verbatim and hash-pinned (SERIALIZE_PIN), probes its bitpacker
// family with a pcall and falls back to its own bit operations when the probe
// fails, whatever the failure was. That fallback is meant for a unit that does
// not carry the family; every unit this generator emits carries it, so a probe
// that fails here means a broken family, and it must be loud rather than a
// silent switch of path. The pinned file cannot be edited in this repository
// (docs/PORT-LUAU.md: copied verbatim; the sha256 test refuses any change), so
// the guard is that the family loads and the bytes module reaches it.
func TestVendoredLuauBitpackerFamilyLoads(t *testing.T) {
	t.Parallel()
	unit := &ir.Unit{
		Package:    "hello",
		ProtocolId: 0x11,
		Files:      []*ir.File{{Base: "Hello"}},
		DeclFile:   map[string]string{},
	}
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["serialize_bitpacker.luau"]; !ok {
		t.Fatal("Generate omitted serialize_bitpacker.luau: serialize_bytes would fall back silently")
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.luau", `local Stream = require("./serialize_stream")
local ok, family = pcall(function()
	return Stream.family("bitpacker")
end)
if not ok then
	error("the bitpacker family does not load: " .. tostring(family), 0)
end
print("ok")
`)
	out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil)))
	if out != "ok" {
		t.Fatalf("main.luau printed %q, want ok", out)
	}
}
