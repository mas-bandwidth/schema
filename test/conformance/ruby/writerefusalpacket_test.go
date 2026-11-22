package rubywriterefusalpacket

import (
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	wrpacket "github.com/mas-bandwidth/schema/v2/test/conformance/write-refusal-packet"
)

// TestWriteRefusalPacketRuby holds docs/SPEC.md section 5.1's packet writer on
// the Ruby leg: every caller-holdable value the declaration does not admit is
// refused with Packetrefusal::InvalidValue (a raise, in every build), and the
// lawful value at the same boundary is written. The negative control moves the
// emitter's inline upper bound one past the maximum and requires the constant row, whose only guard is that bound, to go red.
func TestWriteRefusalPacketRuby(t *testing.T) {
	runWriteRefusalPacket(t)

	out, failed := slowtest.RunSabotaged(t, ".", "./test/conformance/ruby",
		"^TestWriteRefusalPacketRubySurface$",
		slowtest.Sabotage{
			File: "internal/codegen/ruby/codec.go",
			From: `c.line("unless %s.is_a?(::Integer) && %s >= %s && %s <= %s", v, v, min, v, max)`,
			To:   `c.line("unless %s.is_a?(::Integer) && %s >= %s && %s <= %s + 1", v, v, min, v, max)`,
		})
	if !failed {
		t.Fatalf("negative control: the upper-bound comparison moved and the packet surface stayed green\n%s", out)
	}
	if !strings.Contains(out, "FAILED: const_wrong") {
		t.Fatalf("negative control: the packet surface went red, but not at the row it moved\n%s", out)
	}
}

// TestWriteRefusalPacketRubySurface is the surface alone, so the negative
// control can run it under an overlay without re-entering the control.
func TestWriteRefusalPacketRubySurface(t *testing.T) {
	runWriteRefusalPacket(t)
}

// runWriteRefusalPacket generates PacketWriteRefusal for Ruby, runs the probe
// under the pinned interpreter over rows.txt and holds its verdicts.
func runWriteRefusalPacket(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "the pinned Ruby interpreter (it runs the generated packet writer)")
	t.Chdir("../../..")
	root := "."
	rows := wrpacket.Rows(t, root)
	dir := wrpacket.Generate(t, root, "ruby", compiler.Options{})
	probe, err := os.ReadFile("test/conformance/ruby/writerefusalpacket.rb")
	if err != nil {
		t.Fatal(err)
	}
	out := rubyrun.RunArgs(t, rubyrun.Interpreter(t), dir, "probe.rb",
		[]string{wrpacket.RowsPath(root)}, map[string][]byte{"probe.rb": probe})
	wrpacket.Check(t, string(out), rows, nil)
}
