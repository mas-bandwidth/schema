// Resource bounds for the shared port of schema: the engine's nesting cap and
// refusal name, and the allocation factor of a read, held to the Go engine
// first (docs/SPEC-TABLES.md, resource bounds). This is the shared card; the
// per-port cards apply the same bounds in each language.
package ci

import (
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

// TestEngineReadAllocationFactor is the engine's half of the allocation-bound
// card: parsing an input of size N allocates at most a small constant factor of
// N (F, documented in docs/SPEC-TABLES.md). It measures with runtime.MemStats,
// no real time.
func TestEngineReadAllocationFactor(t *testing.T) {
	t.Parallel()
	src := []byte(nestedSchema(parser.MaxNestingDepth / 2))
	const F = 4.0
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	const iters = 20
	for i := 0; i < iters; i++ {
		parser.Parse("alloc.schema", src)
	}
	runtime.ReadMemStats(&after)
	objsPerRun := float64(after.Mallocs-before.Mallocs) / iters
	if objsPerRun > F*float64(len(src)) {
		t.Fatalf("parse of %d input bytes allocated %.0f objects per run, above factor F=%g", len(src), objsPerRun, F)
	}
}
