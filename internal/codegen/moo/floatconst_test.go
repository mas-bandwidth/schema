package moo

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestMooFloatConstOneValue: a float const holds the one value every language
// holds (SPEC §4.3, docs/rulings.json strict-ieee-arithmetic). The module
// carries it through the boundary a float field already rides — serialize.Float32
// over the binary32 pattern, serialize.Uint64 over the binary64 pattern — so the
// bits are the Go leg's float32/float64 bits, subnormals and the largest finite
// included.
func TestMooFloatConstOneValue(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the generated modules under the pinned interpreter)")

	f32 := []float64{1.0 / 3.0, 0.1, float64(math.SmallestNonzeroFloat32), math.MaxFloat32}
	f64 := []float64{1.0 / 3.0, 0.1, math.SmallestNonzeroFloat64, math.MaxFloat64}
	names32 := []string{"Third", "Tenth", "Subnormal", "Largest"}
	names64 := []string{"ThirdD", "TenthD", "SubnormalD", "LargestD"}

	var schema strings.Builder
	schema.WriteString("package probe\n\n")
	for i, v := range f32 {
		fmt.Fprintf(&schema, "const %s float32 = %s\n", names32[i], strconv.FormatFloat(v, 'g', -1, 64))
	}
	schema.WriteString("\n")
	for i, v := range f64 {
		fmt.Fprintf(&schema, "const %s float64 = %s\n", names64[i], strconv.FormatFloat(v, 'g', -1, 64))
	}

	src := t.TempDir()
	p := filepath.Join(src, "Probe.schema")
	if err := os.WriteFile(p, []byte(schema.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, _ := generateInto(t, unitFromFiles(t, []string{p}))

	var probe strings.Builder
	probe.WriteString("--!strict\nlocal P = require(\"./Probe\")\n")
	for _, n := range names32 {
		fmt.Fprintf(&probe, "print(string.format(\"%%08x\", P.%s.bits))\n", n)
	}
	for _, n := range names64 {
		fmt.Fprintf(&probe, "print(string.format(\"%%08x%%08x\", P.%s[2], P.%s[1]))\n", n, n)
	}
	writeFile(t, dir, "main.moo", probe.String())
	lines := moorun.HexLines(t, moorun.Run(t, dir, "main.moo", nil))
	if len(lines) != len(f32)+len(f64) {
		t.Fatalf("probe produced %d lines, want %d", len(lines), len(f32)+len(f64))
	}
	for i, v := range f32 {
		got := binary.BigEndian.Uint32(lines[i])
		if want := math.Float32bits(float32(v)); got != want {
			t.Errorf("float32 const %s: 0x%08x, want Go's 0x%08x", names32[i], got, want)
		}
	}
	for i, v := range f64 {
		got := binary.BigEndian.Uint64(lines[len(f32)+i])
		if want := math.Float64bits(v); got != want {
			t.Errorf("float64 const %s: 0x%016x, want Go's 0x%016x", names64[i], got, want)
		}
	}
}
