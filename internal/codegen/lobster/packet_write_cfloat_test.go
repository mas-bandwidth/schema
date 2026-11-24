// The packet Write compressed-float range rule on the Lobster wire (SPEC §5.1
// row 10; PACKET-WIRE-ALGORITHM.md §2.5): a NaN, an infinity, or a finite value
// outside [min, max] is checked before quantizing. This leg holds a finite value
// off its range with a debug assert (--runtime-no-asserts removes it) and keeps
// the runtime's non-finite trap; a lawful value writes and reads back to its
// quantized value. The shared cases are test/packet-write-cfloat/cases.txt and
// the probe beside them is test/packet-write-cfloat/lobster/main.lobster. Each
// case answers "case <name> ok|FAILED: <why>", and internal/casegate judges the
// answers against the case file and test/packet-write-cfloat/owed.txt, as
// tools/casegate does for every other leg.
package lobster

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/casegate"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

type packetWriteCFloatCase struct {
	name    string
	value   uint32
	refused bool
	read    uint32
	typ     string // CFloat, RatioEndpoint or RatioPair
	wire    string // the written bytes in hex; "" when the case pins none
	bits    int64  // the bits the write emits before the flush; -1 when unpinned
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
		c := packetWriteCFloatCase{name: fields[0], value: parsePacketWriteCFloatValue(t, fields[1]), bits: -1}
		switch fields[2] {
		case "refused":
			c.refused = true
		case "written":
		default:
			t.Fatalf("packet-write-cfloat case %q has verdict %q", line, fields[2])
		}
		c.read = c.value
		c.typ = "CFloat"
		if len(fields)%2 != 1 {
			t.Fatalf("packet-write-cfloat case %q has a modifier with no operand", line)
		}
		for i := 3; i+1 < len(fields); i += 2 {
			switch fields[i] {
			case "reads-back":
				c.read = parsePacketWriteCFloatValue(t, fields[i+1])
			case "type":
				if fields[i+1] != "RatioEndpoint" && fields[i+1] != "RatioPair" {
					t.Fatalf("packet-write-cfloat case %q names an unknown type", line)
				}
				c.typ = fields[i+1]
			case "wire":
				c.wire = strings.ToLower(fields[i+1])
			case "bits":
				n, err := strconv.ParseInt(fields[i+1], 10, 64)
				if err != nil || n < 0 {
					t.Fatalf("packet-write-cfloat case %q has bits %q", line, fields[i+1])
				}
				c.bits = n
			default:
				t.Fatalf("packet-write-cfloat case %q has an unknown modifier %q", line, fields[i])
			}
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
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "packet-write-cfloat", "PacketCFloat.schema"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, src, "PacketCFloat.schema", string(data))
	files, err := Generate(unitFromDir(t, src))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// packetWriteCFloatReadBits reads the probe's "wrote <float32 bits> wire <hex>
// bits <n>" line: the value read back, the bytes, and the bits the write
// emitted before the flush.
func packetWriteCFloatReadBits(out string) (uint32, string, int64, bool) {
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		rest, ok := strings.CutPrefix(line, "wrote ")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) != 5 || fields[1] != "wire" || fields[3] != "bits" {
			return 0, "", 0, false
		}
		bits, err := strconv.ParseUint(fields[0], 16, 32)
		if err != nil {
			return 0, "", 0, false
		}
		written, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			return 0, "", 0, false
		}
		return uint32(bits), fields[2], written, true
	}
	return 0, "", 0, false
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
	allReadPacketWriteCFloatCases := readPacketWriteCFloatCases(t)
	if len(allReadPacketWriteCFloatCases) == 0 {
		t.Fatal("readPacketWriteCFloatCases(t) is empty: the loop below would check nothing")
	}
	// Each case's answer, the lines casegate.Verdict reads; a FAILED case
	// is also a t.Errorf naming it (the negative control reads those).
	var answers strings.Builder
	fail := func(name, format string, args ...any) {
		why := fmt.Sprintf(format, args...)
		t.Errorf("FAILED: %s: %s", name, why)
		fmt.Fprintf(&answers, "case %s FAILED: %s\n", name, why)
	}
	for _, c := range allReadPacketWriteCFloatCases {
		out, code := lobsterrun.RunExit(t, dir, "main.lobster", nil, fmt.Sprintf("%08x", c.value), c.typ)
		text := strings.TrimSpace(string(out))
		if c.refused {
			if code == 0 {
				fail(c.name, "a value the declaration does not admit was written: %s", text)
				continue
			}
			fmt.Fprintf(&answers, "case %s ok\n", c.name)
			continue
		}
		if code != 0 {
			fail(c.name, "a lawful value was refused (exit %d): %s", code, text)
			continue
		}
		got, wire, bits, ok := packetWriteCFloatReadBits(text)
		if !ok {
			fail(c.name, "the driver printed %q", text)
			continue
		}
		if c.bits >= 0 && bits != c.bits {
			fail(c.name, "wrote %d bits before the flush, want %d", bits, c.bits)
			continue
		}
		if c.wire != "" && wire != c.wire {
			fail(c.name, "wrote %s, want %s", wire, c.wire)
			continue
		}
		if got != c.read {
			fail(c.name, "read 0x%08x, want 0x%08x", got, c.read)
			continue
		}
		fmt.Fprintf(&answers, "case %s ok\n", c.name)
	}
	// The case list comes from the case file, never from this loop: a case
	// the loop skips is red here, naming it.
	root := filepath.Join("..", "..", "..", "test", "packet-write-cfloat")
	caseText, err := os.ReadFile(filepath.Join(root, "cases.txt"))
	if err != nil {
		t.Fatal(err)
	}
	owedText, err := os.ReadFile(filepath.Join(root, "owed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	cases, err := casegate.ReadCases(string(caseText))
	if err != nil {
		t.Fatal(err)
	}
	owed, err := casegate.ReadOwed(string(owedText), "lobster")
	if err != nil {
		t.Fatal(err)
	}
	red, summary := casegate.Verdict("packet-write-cfloat-lobster", "lobster", 0, []byte(answers.String()), cases, owed)
	for _, r := range red {
		if !strings.Contains(r, ": FAILED: ") {
			t.Errorf("casegate: %s", r)
		}
	}
	t.Log(summary)
}
