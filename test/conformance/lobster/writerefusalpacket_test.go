// The write-refusal-packet surface for the Lobster leg (docs/SPEC.md §5.1,
// test/conformance/README.md): every caller-holdable value the declaration does
// not admit is refused by the generated packet Write, and the lawful value at
// the same boundary is written.
//
// Lobster's packet writer holds the schema's contracts with a build-removable
// assert (--runtime-no-asserts drops it; the default runtime keeps it), and a
// value wider than its bits(N) with the runtime's write trap, serialize_abort,
// which prints the rule and exits 70 in every build
// (internal/codegen/lobster/write.go, THE CHECK MODEL). Either ends the
// program, so the probe runs once per row and a refusal is read here: an
// assert that fails INSIDE the generated PacketWriteRefusal.lobster, or the
// runtime's `serialize: ` trap. Anything else (an assert in the probe, an
// index out of range, a writer that answers false) is no answer at all, and
// wrpacket.Check fails the row.
//
// Lobster's int is 64 bits, so every row's value is buildable: an enum or a
// union tag takes any int (Grade(4), ShotType(3)), and nothing is exempt.
package lobsterwriterefusalpacket

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	wrpacket "github.com/mas-bandwidth/schema/v2/test/conformance/write-refusal-packet"
)

const root = "../../.."

// generatedAssert is the VM's line for an assert that failed in the generated
// writer: the file it names is the generated unit, never the probe.
var generatedAssert = regexp.MustCompile(`^VM error \(PacketWriteRefusal\.lobster:[0-9]+\): assertion failed: `)

// TestWriteRefusalPacketLobster runs the surface, then a negative control that
// moves the emitter's upper-bound assert from <= to < and requires the
// boundary case to go red.
func TestWriteRefusalPacketLobster(t *testing.T) {
	runWriteRefusalPacket(t)

	out, failed := slowtest.RunSabotaged(t, root, "./test/conformance/lobster",
		"^TestWriteRefusalPacketLobsterSurface$",
		slowtest.Sabotage{
			File: "internal/codegen/lobster/write.go",
			From: `"%sassert %s >= %s and %s <= %s // the value is in [min, max] (SPEC §4.3)\n"`,
			To:   `"%sassert %s >= %s and %s < %s // the value is in [min, max] (SPEC §4.3)\n"`,
		})
	if !failed {
		t.Fatalf("negative control: the upper-bound assert moved and the packet surface stayed green\n%s", out)
	}
	if !strings.Contains(out, "FAILED: scalar_at_max") {
		t.Fatalf("negative control: the packet surface went red, but not at the boundary case\n%s", out)
	}
}

// TestWriteRefusalPacketLobsterSurface is the surface alone, so the negative
// control can run it under an overlay without re-entering the control.
func TestWriteRefusalPacketLobsterSurface(t *testing.T) {
	runWriteRefusalPacket(t)
}

func runWriteRefusalPacket(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "the Lobster toolchain (it runs the generated packet writer)")
	if _, why := lobsterrun.Interpreter(); why != "" {
		t.Fatalf("the Lobster toolchain does not resolve: %s", why)
	}
	dir := wrpacket.Generate(t, root, "lobster", compiler.Options{})
	probe, err := os.ReadFile("writerefusalpacket.lobster")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.lobster"), probe, 0o644); err != nil {
		t.Fatal(err)
	}

	rows := wrpacket.Rows(t, root)
	var answers strings.Builder
	for _, r := range rows {
		answers.WriteString(answer(t, dir, r) + "\n")
	}
	wrpacket.Check(t, answers.String(), rows, nil)
}

// answer runs the probe on one row and says what the writer did: `<name>
// wrote`, `<name> refused`, or a line that answers neither and carries what
// the program printed.
func answer(t *testing.T, dir string, r wrpacket.Row) string {
	t.Helper()
	stdout, code := lobsterrun.RunExit(t, dir, "probe.lobster", nil, r.Name, r.Op, r.Arg)
	out := strings.TrimSpace(string(stdout))
	switch {
	case code == 0 && out == r.Name+" wrote":
		return out
	case code == 1 && generatedAssert.MatchString(out):
		return r.Name + " refused"
	case code == 70 && strings.HasPrefix(out, "serialize: "):
		return r.Name + " refused"
	}
	return r.Name + ": not a refusal and not a write (exit " + strconv.Itoa(code) + "): " + out
}
