package lobster

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// The authored constants are checked as values in the native runtime, not
// inferred from their use in a field bound or a golden packet.
func TestLobsterPacketConstants(t *testing.T) {
	t.Parallel()
	if out := runPacketConstants(t, ""); out != "constants ok\n" {
		t.Fatalf("constant values: %q", out)
	}
}

func TestLobsterPacketConstantsNegativeControl(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"HalfTurn", "MaxPositionUnits", "NumTeams"} {
		t.Run(name, func(t *testing.T) {
			if out := runPacketConstants(t, name); out != "constant "+name+" mismatch\n" {
				t.Fatalf("mutated constant: %q", out)
			}
		})
	}
}

func runPacketConstants(t *testing.T, sabotage string) string {
	t.Helper()
	files := generateExamples(t)
	if sabotage != "" {
		file := "Constants.lobster"
		if sabotage == "HalfTurn" {
			file = "Wire.lobster"
		}
		src := string(files[file])
		anchor := "let " + sabotage + " = "
		if strings.Count(src, anchor) != 1 {
			t.Fatal("HalfTurn constant anchor must occur once")
		}
		start := strings.Index(src, anchor)
		end := strings.IndexByte(src[start:], '\n')
		if end < 0 {
			t.Fatal("HalfTurn declaration has no newline")
		}
		replacement := "0"
		if sabotage == "HalfTurn" {
			replacement = "181.0"
		}
		files[file] = []byte(src[:start] + anchor + replacement + src[start+end:])
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", `import .Wire
import .Constants
if example.ProbeId != 0xDEADBEEFCAFE: print("constant ProbeId mismatch")
elif example.HalfTurn != 180.0: print("constant HalfTurn mismatch")
elif example.TickRate != 60.0: print("constant TickRate mismatch")
elif example.SpinRate != 1.5: print("constant SpinRate mismatch")
elif example.MaxPositionUnits != 8388608: print("constant MaxPositionUnits mismatch")
elif example.MaxVelocityUnits != 2097152: print("constant MaxVelocityUnits mismatch")
elif example.NumTeams != 2: print("constant NumTeams mismatch")
else: print("constants ok")
`)
	return string(lobsterrun.Run(t, dir, "main.lobster", nil))
}
