// Package luauwriterefusalpacket is the Luau leg of the write-refusal-packet
// surface (docs/SPEC.md §5.1, test/conformance/README.md).
package luauwriterefusalpacket

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	wrpacket "github.com/mas-bandwidth/schema/v2/test/conformance/write-refusal-packet"
)

const root = "../../.."

// TestWriteRefusalPacketLuau holds §5.1 on the Luau leg: every value the
// declaration does not admit is refused by the generated WritePacketWriteRefusal
// (false, in every build) and each lawful twin is written. The negative control
// relaxes the emitter's upper-bound comparison and requires the boundary row
// scalar_at_max to go red.
func TestWriteRefusalPacketLuau(t *testing.T) {
	runSurface(t)

	out, failed := slowtest.RunSabotaged(t, root, "./test/conformance/luau",
		"^TestWriteRefusalPacketLuauSurface$",
		slowtest.Sabotage{
			File: "internal/codegen/luau/write.go",
			From: `%s < %s or %s > %s then return false end%s`,
			To:   `%s < %s or %s >= %s then return false end%s`,
		})
	if !failed {
		t.Fatalf("negative control: the upper-bound comparison moved and the packet surface stayed green\n%s", out)
	}
	if !strings.Contains(out, "FAILED: scalar_at_max") {
		t.Fatalf("negative control: the packet surface went red, but not at the boundary row\n%s", out)
	}
}

// TestWriteRefusalPacketLuauSurface is the surface alone, so the negative
// control can run it under an overlay without re-entering the control.
func TestWriteRefusalPacketLuauSurface(t *testing.T) {
	runSurface(t)
}

func runSurface(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "the pinned Luau interpreter (it runs the generated packet writer)")
	rows := wrpacket.Rows(t, root)
	dir := wrpacket.Generate(t, root, "luau", compiler.Options{})

	probe, err := os.ReadFile("writerefusalpacket.luau")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), probe, 0o600); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("--!strict\nreturn {\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "\t{ name = %q, op = %q, arg = %q },\n", r.Name, r.Op, r.Arg)
	}
	b.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "rows.luau"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	out := luaurun.Run(t, dir, "main.luau", map[string][]byte{})
	wrpacket.Check(t, string(out), rows, nil)
}
