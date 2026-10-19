// Resource bounds for the shared port of schema: the engine's nesting cap and
// refusal name, and the allocation factor of a read, held to the Go engine
// first (docs/SPEC-TABLES.md, resource bounds). This is the shared card; the
// per-port cards apply the same bounds in each language.
package ci

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// nestedSchema builds a schema whose bodies nest `depth` levels deep through
// the `if` item — the only body-nesting construct — so the engine's recursive
// block parsing has to descend `depth` frames.
func nestedSchema(depth int) string {
	var b strings.Builder
	b.WriteString("package p\n")
	b.WriteString("type T {\n")
	for i := 0; i < depth; i++ {
		b.WriteString("  a uint8\n")
		b.WriteString("  if a {\n")
	}
	b.WriteString("    a uint8\n")
	for i := 0; i < depth; i++ {
		b.WriteString("  }\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// TestEngineNestingDepthCap is the engine's half of the resource-bounds card:
// a schema nested past the documented cap is REFUSED by name, not recursed
// into, so a hostile file cannot drive the engine's stack or memory past a
// fixed size. This test is red at BASE: before the guard landed the deep
// schema parsed to a clean AST and no refusal appeared.
func TestEngineNestingDepthCap(t *testing.T) {
	t.Parallel()
	deep := nestedSchema(parser.MaxNestingDepth + 50)
	_, errs := parser.Parse("hostile.schema", []byte(deep))
	refused := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "nesting_depth") {
			refused = true
			break
		}
	}
	if !refused {
		t.Fatalf("schema nested %d levels past the cap was not refused by name: %v", 50, errs)
	}

	shallow := nestedSchema(parser.MaxNestingDepth / 2)
	if _, errs := parser.Parse("fine.schema", []byte(shallow)); len(errs) > 0 {
		t.Fatalf("schema under the cap refused: %v", errs)
	}
}

// TestEngineReadAllocationFactor records byte amplification for real parser-root reads.
// It is a measurement checkpoint; the normative factor is selected only after
// these per-shape byte ratios are available.
func TestEngineReadAllocationFactor(t *testing.T) {
	t.Parallel()
	// Measure independent parser workloads after warmup. TotalAlloc measures bytes
	// rather than allocation-object count; each read receives the same immutable
	// input and its parser result is discarded before the next iteration.
	shapes := []struct {
		name string
		src  []byte
	}{
		{"depth-near-cap", []byte(nestedSchema(parser.MaxNestingDepth - 1))},
		{"depth-over-cap", []byte(nestedSchema(parser.MaxNestingDepth + 50))},
		{"many-fields", []byte(manyFieldSchema(2048))},
		{"giant-string-default", []byte(giantStringSchema(64 * 1024))},
		{"huge-array-bound", []byte("package p\ntype T { values [18446744073709551615]uint8 }\n")},
	}
	for _, shape := range shapes {
		measureParserReadBytes(t, shape.name, shape.src)
	}
}

func manyFieldSchema(n int) string {
	var b strings.Builder
	b.WriteString("package p\ntype T {\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "field%04d uint8\n", i)
	}
	b.WriteString("}\n")
	return b.String()
}

func giantStringSchema(n int) string {
	return "package p\ntype T { label string(" + fmt.Sprint(n) + ") = \"" + strings.Repeat("x", n) + "\" }\n"
}

func measureParserReadBytes(t *testing.T, name string, src []byte) {
	t.Helper()
	_, _ = parser.Parse("measure.schema", src) // warm parser paths and caches
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	const iters = 10
	for i := 0; i < iters; i++ {
		_, _ = parser.Parse("measure.schema", src)
	}
	runtime.ReadMemStats(&after)
	bytesPerRead := float64(after.TotalAlloc-before.TotalAlloc) / iters
	t.Logf("MEASURE engine-root shape=%s input_bytes=%d allocated_bytes_per_read=%.0f ratio=%.3f", name, len(src), bytesPerRead, bytesPerRead/float64(len(src)))
}
