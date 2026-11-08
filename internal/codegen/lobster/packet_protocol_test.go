package lobster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// The examples' independent committed C++ and Go surfaces pin this high-bit
// ID. The native probe exercises the Lobster hexadecimal literal path.
func TestLobsterPacketProtocolID(t *testing.T)                { runPacketProtocolID(t, false) }
func TestLobsterPacketProtocolIDNegativeControl(t *testing.T) { runPacketProtocolID(t, true) }
func runPacketProtocolID(t *testing.T, sabotage bool) {
	t.Helper()
	const id uint64 = 0xeb26c970fde3fd71
	for path, anchor := range map[string]string{
		"generated/go/Constants.go": "const ProtocolID uint64 = 0xeb26c970fde3fd71",
		"generated/cpp/Constants.h": "inline constexpr uint64_t ProtocolId = 0xeb26c970fde3fd71ull",
	} {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), anchor) {
			t.Fatalf("reference ID missing from %s", path)
		}
	}
	u := unitFromDir(t, filepath.Join("..", "..", "..", "examples"))
	if u.ProtocolId != id || u.ProtocolId>>63 != 1 {
		t.Fatalf("examples protocol ID: %016x", u.ProtocolId)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	src := string(files["Constants.lobster"])
	anchor := fmt.Sprintf("let ProtocolId = 0x%016x", u.ProtocolId)
	if strings.Count(src, anchor) != 1 {
		t.Fatal("high-bit protocol ID literal absent")
	}
	if sabotage {
		files["Constants.lobster"] = []byte(strings.Replace(src, anchor, "let ProtocolId = 0xeb26c970fde3fd70", 1))
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", fmt.Sprintf(`import .Constants
if example.ProtocolId == 0x%016x: print("protocol id ok")
else: print("protocol id mismatch")
`, u.ProtocolId))
	out := string(lobsterrun.Run(t, dir, "main.lobster", nil))
	want := "protocol id ok\n"
	if sabotage {
		want = "protocol id mismatch\n"
	}
	if out != want {
		t.Fatalf("protocol probe: got %q, want %q", out, want)
	}
}
