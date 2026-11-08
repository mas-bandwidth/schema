package javatable

// count_clamp_ends, the two ENDS of the count clamp, on the java leg. The
// roadmap's array-bounds work set carries two java task nodes, quoted here
// verbatim: java/C1 "count clamp v<0" and java/C2 "count clamp v>Max". Row 4,
// writer_bound_count, forges the count word to 7 — over the PLAN's bound of 4,
// but neither negative nor past the READER's own 8, so it exercises neither
// end. On 2026-09-19 the negative arm was measured deleted from five legs'
// runtime (c, cs, dart, elixir, js) with not one of their landed tests red:
// it is guarded by nothing, and this row is the guard. Under the strict read
// (docs/plans/strict-only.md, ALG §4.5) BOTH ends REFUSE the whole load as
// countOverBound, n = -1, no counter moved; C2's bound is the WRITER's 4
// carried by the plan, never the reader's own 8.
import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningCountClampEnds(t *testing.T) {
	corpus := corpusDir(t)
	oldFile := filepath.Join(corpus, "old_array_bounded_grow.bin")
	raw, err := os.ReadFile(oldFile)
	if err != nil {
		t.Fatalf("read %s: %v", oldFile, err)
	}

	// THE FORGE: the count word is located by its neighbour `lead` = 0xAAAAAAAA
	// and its own lawful value 4 (the manifest's numbers for this writer). The
	// needle must occur exactly once or the forge has no single locator.
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	at := bytes.Index(raw, needle)
	if at < 0 {
		t.Fatalf("the forge needle % x is not in %s", needle, oldFile)
	}
	if bytes.Contains(raw[at+1:], needle) {
		t.Fatalf("the forge needle % x occurs more than once in %s", needle, oldFile)
	}

	// PROBE C1 — the count word forged to -1 (0xFFFFFFFF, little-endian int32).
	negative := append([]byte(nil), raw...)
	negative[at+4] = 0xFF
	negative[at+5] = 0xFF
	negative[at+6] = 0xFF
	negative[at+7] = 0xFF
	negFile := filepath.Join(t.TempDir(), "negative_array_bounded_grow.bin")
	if err := os.WriteFile(negFile, negative, 0o644); err != nil {
		t.Fatal(err)
	}

	// PROBE C2 — the count word forged to 9, past the writer's 4 and the reader's 8.
	over := append([]byte(nil), raw...)
	over[at+4] = 0x09
	overFile := filepath.Join(t.TempDir(), "over_array_bounded_grow.bin")
	if err := os.WriteFile(overFile, over, 0o644); err != nil {
		t.Fatal(err)
	}

	_, classes := buildRow(t, t.TempDir(), "array_bounded_grow", []sideSpec{
		{key: "reads", schema: "VNEW_array_bounded_grow.schema", older: []string{"VOLD_array_bounded_grow.schema"}},
		{key: "refuses", schema: "VOLD_array_bounded_grow.schema"},
	})

	// PROBE C1 — a negative count refuses the whole load as countOverBound: never
	// a clamp to zero, never -1 landed, never the writer's 4 or the reader's 8.
	requireNamedRefusal(t, "C1 count -1", runProbe(t, classes, "Probe_reads", negFile), "countOverBound")

	// PROBE C2 — a count past the WRITER's bound (the plan's 4) refuses the whole
	// load as countOverBound; it never lands 4, the reader's own 8 or the forged 9.
	requireNamedRefusal(t, "C2 count 9", runProbe(t, classes, "Probe_reads", overFile), "countOverBound")

	// The unforged file still reads: each refusal above is a refusal OF A FORGE.
	r := runProbe(t, classes, "Probe_reads", oldFile)
	if r.n != 1 || r.refused || r.damaged {
		t.Errorf("the unforged file does not read: n=%d refused=%v reason=%s damaged=%v", r.n, r.refused, r.reason, r.damaged)
	}
	if got := r.value["valsCount"]; got != "4" {
		t.Errorf("valsCount=%s, want the writer's 4 on the unforged file", got)
	}
}
