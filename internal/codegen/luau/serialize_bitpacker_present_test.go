package luau

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// SF-33: serialize_bytes.luau used to probe its bitpacker family with a pcall
// whose failure it discarded, switching to a fallback path whatever the
// failure was. Upstream (serialize.luau v2.1.4) removed the probe and
// SERIALIZE_PIN now names that commit. hasDiscardedFamilyProbe is the check a
// planted old file must trip; the vendored file must not.
func hasDiscardedFamilyProbe(src string) bool {
	return strings.Contains(src, "pcall(function()\n\treturn Stream.family(\"bitpacker\")")
}

func TestVendoredLuauHasNoDiscardedBitpackerProbe(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("runtime", "serialize_bytes.luau"))
	if err != nil {
		t.Fatal(err)
	}
	if hasDiscardedFamilyProbe(string(data)) {
		t.Fatal("serialize_bytes.luau probes its bitpacker family with a discarded pcall (SF-33)")
	}
	old := "local hasBitpacker = pcall(function()\n\treturn Stream.family(\"bitpacker\")\nend)\n"
	if !hasDiscardedFamilyProbe(old) {
		t.Fatal("the check does not see the old probe: it would pass vacuously")
	}
}

// The family loads from the vendored set, so no fallback is ever needed.
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
		t.Fatal("Generate omitted serialize_bitpacker.luau")
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.luau", "local Stream = require(\"./serialize_stream\")\nlocal ok, family = pcall(function()\n\treturn Stream.family(\"bitpacker\")\nend)\nif not ok then\n\terror(\"the bitpacker family does not load: \" .. tostring(family), 0)\nend\nprint(\"ok\")\n")
	out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil)))
	if out != "ok" {
		t.Fatalf("main.luau printed %q, want ok", out)
	}
}
