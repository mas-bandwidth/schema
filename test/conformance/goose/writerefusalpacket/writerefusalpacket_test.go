// Package goosewriterefusalpacket is the Goose leg of the
// write-refusal-packet surface (docs/SPEC.md §5.1; test/conformance/README.md).
package goosewriterefusalpacket

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	wrpacket "github.com/mas-bandwidth/schema/v2/test/conformance/write-refusal-packet"
)

// probe is the Goose side: it reads rows.txt, applies each row's op to a fresh
// lawful value, runs the generated packet write and prints one verdict per row.
//
//go:embed probe.goose
var probe []byte

// root is the repository root relative to this package's directory.
const root = "../../../.."

// TestWriteRefusalPacketGoose holds docs/SPEC.md §5.1's packet writer on the
// Goose leg (rows 1-6 and 8 every build; row 11 is not a packet row here and
// float rows 7 and 10 are not in rows.txt): every caller-holdable value the
// declaration does not admit is refused with the contract's refusal kind, and
// the lawful twin at the same boundary is written. The negative control moves
// the Goose emitter's upper-bound comparison and requires the boundary case to
// go red.
func TestWriteRefusalPacketGoose(t *testing.T) {
	runWriteRefusalPacketGoose(t)

	out, failed := slowtest.RunSabotaged(t, root, "./test/conformance/goose/writerefusalpacket",
		"^TestWriteRefusalPacketGooseSurface$",
		slowtest.Sabotage{
			File: "internal/codegen/goose/write.go",
			From: `cond += fmt.Sprintf("%s > %s", expr, IntLit(hi))`,
			To:   `cond += fmt.Sprintf("%s >= %s", expr, IntLit(hi))`,
		})
	if !failed {
		t.Fatalf("negative control: the upper-bound comparison moved and the packet surface stayed green\n%s", out)
	}
	if !strings.Contains(out, "FAILED: scalar_at_max") {
		t.Fatalf("negative control: the packet surface went red, but not at the boundary case\n%s", out)
	}
}

// TestWriteRefusalPacketGooseSurface is the surface alone, so the negative
// control can run it under an overlay without re-entering the control.
func TestWriteRefusalPacketGooseSurface(t *testing.T) {
	runWriteRefusalPacketGoose(t)
}

// runWriteRefusalPacketGoose generates PacketWriteRefusal.schema for Goose,
// runs the probe beside the generated files with the pinned compiler, and
// holds its verdicts against rows.txt. Behind slowtest.Gate (gooserun.Command
// gates too): it compiles and runs Goose. No row is exempt: every value the
// rows name is representable in the Goose struct (an enum and a union tag are
// u8, bits(7) is u32, counts and lengths are i32).
func runWriteRefusalPacketGoose(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "the Goose toolchain (it compiles and runs the generated packet writer)")
	if goose, why := gooserun.Compiler(); goose == "" {
		t.Fatalf("the Goose compiler does not resolve: %s", why)
	}
	dir := wrpacket.Generate(t, root, "goose", nil)
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), probe, 0o600); err != nil {
		t.Fatal(err)
	}
	out := gooserun.RunArgs(t, dir, "main.goose", nil, wrpacket.RowsPath(root))
	wrpacket.Check(t, string(out), wrpacket.Rows(t, root), nil)
}
