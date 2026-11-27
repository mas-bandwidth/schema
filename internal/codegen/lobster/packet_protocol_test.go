package lobster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/lobster/lobsterrun"
)

// The committed C++ and Go surfaces pin both examples (low high-bit) and
// examples128/Ludicrous (high-bit >>63 == 1) after the shared identity epoch.
// The native probe exercises the Lobster hexadecimal literal path on the
// high-bit Ludicrous protocol ID.
func TestLobsterPacketProtocolID(t *testing.T)                { runPacketProtocolID(t, false) }
func TestLobsterPacketProtocolIDNegativeControl(t *testing.T) { runPacketProtocolID(t, true) }
func runPacketProtocolID(t *testing.T, sabotage bool) {
	t.Helper()
	const (
		examplesID  uint64 = 0x2bf51af687ac0abd
		ludicrousID uint64 = 0xd2ded2cc8a482aa1
	)
	for path, anchor := range map[string]string{
		"generated/go/Constants.go":                 "const ProtocolID uint64 = 0x2bf51af687ac0abd",
		"generated/cpp/Constants.h":                 "inline constexpr uint64_t ProtocolId = 0x2bf51af687ac0abdull",
		"generated/go-ludicrous/Ludicrous.go":       "const ProtocolID uint64 = 0xd2ded2cc8a482aa1",
		"testdata/golden/ludicrous/cpp/Ludicrous.h": "inline constexpr uint64_t ProtocolId = 0xd2ded2cc8a482aa1ull",
	} {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), anchor) {
			t.Fatalf("reference ID missing from %s", path)
		}
	}
	ex := unitFromDir(t, filepath.Join("..", "..", "..", "examples"))
	if ex.ProtocolId != examplesID {
		t.Fatalf("examples protocol ID: %016x", ex.ProtocolId)
	}
	u := unitFromDir(t, filepath.Join("..", "..", "..", "examples128"))
	if u.ProtocolId != ludicrousID || u.ProtocolId>>63 != 1 {
		t.Fatalf("examples128 protocol ID: %016x", u.ProtocolId)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	src := string(files["Ludicrous.lobster"])
	anchor := fmt.Sprintf("let ProtocolId = 0x%016x", u.ProtocolId)
	if strings.Count(src, anchor) != 1 {
		t.Fatal("high-bit protocol ID literal absent")
	}
	if sabotage {
		files["Ludicrous.lobster"] = []byte(strings.Replace(src, anchor, "let ProtocolId = 0xeb26c970fde3fd70", 1))
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", fmt.Sprintf(`import .Ludicrous
if ludicrous.ProtocolId == 0x%016x: print("protocol id ok")
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
