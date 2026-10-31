// Packet floats are finite (SPEC §4.3, serialize format 2.0), the Squirrel
// leg. The generated packet code carries no float codec of its own: every
// bare float32 and float64 reaches the vendored serialize.nut's
// serializeFloat / serializeDouble, which trap a non-finite value on write
// and refuse one on read from its raw bits as non_finite_float (serialize.nut
// v2.x). TestSquirrelPacketFloatsAreFinite generates
// test/packet-finite/squirrel/Finite.schema beside the vendored runtime and
// runs test/packet-finite/squirrel/main.nut, which holds the write traps, the
// crafted-byte read refusals and the bit-exact -0 and subnormals at every
// float shape a packet carries. The negative control drops the runtime's read
// test at both widths, in memory, and must red exactly the read vectors.
package squirrel_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

// finiteReadVectors is the count of crafted-byte read refusals main.nut
// checks: seven float32 patterns at four float32 slots and seven float64
// patterns at five float64 slots.
const finiteReadVectors = 7*4 + 7*5

// packetFiniteRun generates the finite schema with the vendored runtime,
// lets patch rewrite the files, runs the probe and returns its FAILED lines
// and whether it printed OK.
func packetFiniteRun(t *testing.T, patch func(map[string][]byte)) ([]string, bool) {
	t.Helper()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(root, "test", "packet-finite", "squirrel", "Finite.schema"))
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	maps.Copy(files, squirrel.Serialize())
	probe, err := os.ReadFile(filepath.Join(root, "test", "packet-finite", "squirrel", "main.nut"))
	if err != nil {
		t.Fatal(err)
	}
	files["main.nut"] = probe
	if patch != nil {
		patch(files)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := sqrun.Run(t, dir, "main.nut", nil)
	var failed []string
	ok := false
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "FAILED: "):
			failed = append(failed, strings.TrimPrefix(line, "FAILED: "))
		case line == "OK":
			ok = true
		}
	}
	return failed, ok
}

// TestSquirrelPacketFloatsAreFinite: every write refusal, read refusal and
// bit-exact round trip in main.nut holds.
func TestSquirrelPacketFloatsAreFinite(t *testing.T) {
	t.Parallel()
	failed, ok := packetFiniteRun(t, nil)
	for _, f := range failed {
		t.Errorf("%s", f)
	}
	if !ok && len(failed) == 0 {
		t.Fatal("the finite probe printed neither OK nor a FAILED line")
	}
}

// TestSquirrelPacketFiniteNegativeControl removes serialize.nut's read-side
// finite test at both widths (one anchor each, matched exactly once) and
// requires exactly the crafted-byte read refusals to go red, while the write
// traps and the bit-exact round trips stay green.
func TestSquirrelPacketFiniteNegativeControl(t *testing.T) {
	t.Parallel()
	const (
		anchor32 = "    local bits = s._take(32);\n    if (!floatBitsAreFinite(bits)) return s._fail(SerializeError.NonFiniteFloat);\n"
		anchor64 = "    local bits = lo | (hi << 32);\n    if (!doubleBitsAreFinite(bits)) return s._fail(SerializeError.NonFiniteFloat);\n"
	)
	failed, _ := packetFiniteRun(t, func(files map[string][]byte) {
		src := string(files["serialize_float.nut"])
		for _, anchor := range []string{anchor32, anchor64} {
			if n := strings.Count(src, anchor); n != 1 {
				t.Fatalf("the control's anchor %q appears %d times in serialize_float.nut, want exactly once", anchor, n)
			}
			keep, _, _ := strings.Cut(anchor, "\n")
			src = strings.Replace(src, anchor, keep+"\n    // SABOTAGED: the read takes every pattern\n", 1)
		}
		files["serialize_float.nut"] = []byte(src)
	})
	reads := 0
	for _, f := range failed {
		if strings.HasPrefix(f, "non_finite_float read refusal: ") {
			reads++
			continue
		}
		t.Errorf("a check other than the read refusals went red: %s", f)
	}
	if reads != finiteReadVectors {
		t.Fatalf("the dropped read check red %d read vectors, want exactly %d", reads, finiteReadVectors)
	}
	t.Logf("the dropped read check reds exactly the %d non_finite_float read vectors", reads)
}
