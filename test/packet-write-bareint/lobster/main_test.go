// The bare-integer packet Write driver for the Lobster leg (SPEC §5.1 row 11,
// docs/PACKET-WIRE-ALGORITHM.md §2.2 and §2.4, test/packet-write-bareint). It
// generates BareInt.schema through internal/codegen/lobster, runs main.lobster
// beside the shared cases, and requires every refused case to trip the
// generated writer's assert and every written boundary twin to read back
// exactly at its declared width.
//
// Lobster's int is 64 bits (docs/PORT-LOBSTER.md, THE NUMBER MODEL), so a
// field narrower than 64 can hold a value its declaration does not admit: that
// is the assert this gate holds. At 64 the storage and the field are the same
// width, so an out-of-width value cannot be built: the driver names those
// cases unbuildable and the test requires that set exactly.
package lobsterdriver

import (
	"math"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

type bareIntCase struct {
	name      string
	typ       string
	valueText string
	verdict   string
	width     int
	signed    bool
}

// The shared cases at the base: a case here is unbuildable when the value is
// not a value of the field's 64-bit storage (the storage is the width).
var unbuildableBareIntCases = []string{
	"int64-above-max",
	"uint64-above-max",
	"uint64-negative",
}

var (
	two63      = new(big.Int).Lsh(big.NewInt(1), 63)
	two64      = new(big.Int).Lsh(big.NewInt(1), 64)
	maxUint64  = new(big.Int).Sub(new(big.Int).Set(two64), big.NewInt(1))
	minInt64   = big.NewInt(math.MinInt64)
	maxInt64   = big.NewInt(math.MaxInt64)
	maskUint64 = new(big.Int).Sub(new(big.Int).Set(two64), big.NewInt(1))
)

func readBareIntCases(t *testing.T) []bareIntCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "cases.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []bareIntCase
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 4 {
			t.Fatalf("cases.txt: %q is not <name> <type> <value> <verdict>", line)
		}
		width, signed, ok := parseBareIntType(fields[1])
		if !ok {
			t.Fatalf("cases.txt: %q is not a bare integer type", fields[1])
		}
		cases = append(cases, bareIntCase{
			name: fields[0], typ: fields[1], valueText: fields[2], verdict: fields[3],
			width: width, signed: signed,
		})
	}
	return cases
}

func parseBareIntType(name string) (width int, signed, ok bool) {
	if len(name) < 2 || (name[0] != 'I' && name[0] != 'U') {
		return 0, false, false
	}
	width, err := strconv.Atoi(name[1:])
	if err != nil || width <= 0 || width > 64 {
		return 0, false, false
	}
	return width, name[0] == 'I', true
}

// lobsterValue is the argument main.lobster receives: the field's value as
// Lobster's signed 64-bit int. A 64-bit unsigned value is its two's-complement
// bit pattern, and a value no 64-bit int holds is not buildable.
func lobsterValue(c bareIntCase) (string, bool) {
	v, ok := new(big.Int).SetString(c.valueText, 10)
	if !ok {
		return "", false
	}
	if c.width < 64 {
		if !v.IsInt64() {
			return "", false
		}
		return v.String(), true
	}
	if c.signed {
		if v.Cmp(minInt64) < 0 || v.Cmp(maxInt64) > 0 {
			return "", false
		}
		return v.String(), true
	}
	if v.Sign() < 0 || v.Cmp(maxUint64) > 0 {
		return "", false
	}
	bits := new(big.Int).And(v, maskUint64)
	if bits.Cmp(two63) >= 0 {
		bits.Sub(bits, two64)
	}
	return bits.String(), true
}

func generateBareInt(t *testing.T) map[string][]byte {
	t.Helper()
	path := filepath.Join("..", "BareInt.schema")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	name := "BareInt.schema"
	tree, errs := parser.Parse(name, src)
	if len(errs) != 0 {
		t.Fatalf("parse %s: %v", name, errs)
	}
	u, errs := check.Unit([]check.SourceFile{{Path: path, Name: name, Base: "BareInt", Bytes: src, AST: tree}})
	if len(errs) != 0 {
		t.Fatalf("check %s: %v", name, errs)
	}
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func writeBareIntFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"main.lobster", "../cases.txt"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLobsterPacketWriteBareInt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeBareIntFiles(t, dir, generateBareInt(t))

	cases := readBareIntCases(t)
	var unbuildable []string
	for _, c := range cases {
		value, ok := lobsterValue(c)
		if !ok {
			unbuildable = append(unbuildable, c.name)
			continue
		}
		out, code := lobsterrun.RunExit(t, dir, "main.lobster", nil, c.name, value)
		switch c.verdict {
		case "refused":
			if code == 0 {
				t.Errorf("FAILED: %s: wrote a value the width refuses", c.name)
			}
		case "written":
			if code != 0 {
				t.Errorf("FAILED: %s: refused a value the width carries (exit %d)", c.name, code)
				continue
			}
			fields := strings.Fields(strings.TrimSpace(string(out)))
			if len(fields) != 2 {
				t.Errorf("FAILED: %s: driver printed %q, want <bits> <value>", c.name, strings.TrimSpace(string(out)))
				continue
			}
			bits, err := strconv.Atoi(fields[0])
			if err != nil {
				t.Errorf("FAILED: %s: wire bit count %q is not a number", c.name, fields[0])
			} else if bits != c.width {
				t.Errorf("FAILED: %s: wrote %d bits, want %d", c.name, bits, c.width)
			}
			if fields[1] != value {
				t.Errorf("FAILED: %s: read back %s, want %s", c.name, fields[1], value)
			}
		default:
			t.Fatalf("cases.txt: %s has verdict %q", c.name, c.verdict)
		}
	}

	slices.Sort(unbuildable)
	want := slices.Clone(unbuildableBareIntCases)
	slices.Sort(want)
	if !slices.Equal(unbuildable, want) {
		t.Errorf("FAILED: unbuildable cases are %v, want %v", unbuildable, want)
	}
}
