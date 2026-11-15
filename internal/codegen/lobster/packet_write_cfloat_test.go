// The packet Write compressed-float range rule on the Lobster wire (SPEC §5.1
// row 10; PACKET-WIRE-ALGORITHM.md §2.5): a NaN, an infinity, or a finite value
// outside [min, max] is checked before quantizing. This leg holds a finite value
// off its range with a debug assert (--runtime-no-asserts removes it) and keeps
// the runtime's non-finite trap; a lawful value writes and reads back to its
// quantized value. The shared cases are test/packet-write-cfloat/cases.txt and
// the probe beside them is test/packet-write-cfloat/lobster/main.lobster.
package lobster

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

type packetWriteCFloatCase struct {
	name    string
	value   uint32
	refused bool
	read    uint32
}

func parsePacketWriteCFloatValue(t *testing.T, token string) uint32 {
	t.Helper()
	switch token {
	case "NaN":
		return 0x7fc00000
	case "+Inf":
		return 0x7f800000
	case "-Inf":
		return 0xff800000
	}
	v, err := strconv.ParseFloat(token, 32)
	if err != nil {
		t.Fatalf("packet-write-cfloat value %q: %v", token, err)
	}
	return math.Float32bits(float32(v))
}

func readPacketWriteCFloatCases(t *testing.T) []packetWriteCFloatCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "packet-write-cfloat", "cases.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []packetWriteCFloatCase
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			t.Fatalf("packet-write-cfloat case %q is not <name> <value> <verdict>", line)
		}
		c := packetWriteCFloatCase{name: fields[0], value: parsePacketWriteCFloatValue(t, fields[1])}
		switch fields[2] {
		case "refused":
			c.refused = true
		case "written":
		default:
			t.Fatalf("packet-write-cfloat case %q has verdict %q", line, fields[2])
		}
		c.read = c.value
		if len(fields) > 3 {
			if len(fields) != 5 || fields[3] != "reads-back" {
				t.Fatalf("packet-write-cfloat case %q is not <name> <value> written reads-back <value>", line)
			}
			c.read = parsePacketWriteCFloatValue(t, fields[4])
		}
		cases = append(cases, c)
	}
	if len(cases) == 0 {
		t.Fatal("packet-write-cfloat holds no cases")
	}
	return cases
}

func generatePacketWriteCFloat(t *testing.T) map[string][]byte {
	t.Helper()
	src := t.TempDir()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "packet-write-cfloat", "CFloat.schema"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, src, "CFloat.schema", string(data))
	files, err := Generate(unitFromDir(t, src))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func packetWriteCFloatReadBits(out string) (uint32, bool) {
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		rest, ok := strings.CutPrefix(line, "wrote ")
		if !ok {
			continue
		}
		bits, err := strconv.ParseUint(strings.TrimSpace(rest), 16, 32)
		if err != nil {
			return 0, false
		}
		return uint32(bits), true
	}
	return 0, false
}

func TestLobsterPacketWriteCFloatRange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeAll(t, dir, generatePacketWriteCFloat(t))
	driver, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "packet-write-cfloat", "lobster", "main.lobster"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "main.lobster", string(driver))
	for _, c := range readPacketWriteCFloatCases(t) {
		out, code := lobsterrun.RunExit(t, dir, "main.lobster", nil, fmt.Sprintf("%08x", c.value))
		text := strings.TrimSpace(string(out))
		if c.refused {
			if code == 0 {
				t.Errorf("FAILED: %s: a value the declaration does not admit was written: %s", c.name, text)
			}
			continue
		}
		if code != 0 {
			t.Errorf("FAILED: %s: a lawful value was refused (exit %d): %s", c.name, code, text)
			continue
		}
		got, ok := packetWriteCFloatReadBits(text)
		if !ok {
			t.Errorf("FAILED: %s: the driver printed %q", c.name, text)
			continue
		}
		if got != c.read {
			t.Errorf("FAILED: %s: read 0x%08x, want 0x%08x", c.name, got, c.read)
		}
	}
}
