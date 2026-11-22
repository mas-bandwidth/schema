package luawriterefusalpacket

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	wrpacket "github.com/mas-bandwidth/schema/v2/test/conformance/write-refusal-packet"
)

// TestWriteRefusalPacketLua holds docs/SPEC.md section 5.1's packet writer on
// the Lua leg: every caller-holdable value the declaration does not admit is
// refused by the generated Write returning false (rows 1-6, 8, 10, 11; row 7
// raises and is not in rows.txt), and the lawful twin at the same boundary is
// written. The negative control relaxes the emitter's upper bound by one and
// requires the packet surface to go red at the scalar's refused row.
func TestWriteRefusalPacketLua(t *testing.T) {
	runWriteRefusalPacketLua(t)

	out, failed := slowtest.RunSabotaged(t, "../../..", "./test/conformance/lua",
		"^TestWriteRefusalPacketLuaSurface$",
		slowtest.Sabotage{
			File: "internal/codegen/lua/write.go",
			From: `indent, val, val, lo, val, hi)`,
			To:   `indent, val, val, lo, val, IntLiteral(new(big.Int).Add(f.IntMax, big.NewInt(1))))`,
		})
	if !failed {
		t.Fatalf("negative control: the upper bound was relaxed and the packet surface stayed green\n%s", out)
	}
	if !strings.Contains(out, "FAILED: scalar_high") {
		t.Fatalf("negative control: the packet surface went red, but not at the scalar's refused row\n%s", out)
	}
}

// TestWriteRefusalPacketLuaSurface is the surface alone, so the negative
// control can run it under an overlay without re-entering the control.
func TestWriteRefusalPacketLuaSurface(t *testing.T) {
	runWriteRefusalPacketLua(t)
}

func runWriteRefusalPacketLua(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "the pinned Lua interpreter (it runs the generated packet writer)")
	root := "../../.."
	rows := wrpacket.Rows(t, root)
	dir := wrpacket.Generate(t, root, "lua", compiler.Options{})
	probe, err := os.ReadFile("writerefusalpacket.lua")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.lua"), probe, 0o600); err != nil {
		t.Fatal(err)
	}
	rowsText, err := os.ReadFile(wrpacket.RowsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := luarun.Try(t, dir, "probe.lua", map[string][]byte{"rows": rowsText})
	if err != nil {
		t.Fatalf("the Lua probe failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr, stdout)
	}
	if stderr != "" {
		t.Logf("probe stderr:\n%s", stderr)
	}
	wrpacket.Check(t, stdout, rows, nil)
}
