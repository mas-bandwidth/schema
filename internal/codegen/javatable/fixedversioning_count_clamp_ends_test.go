package javatable

// count_clamp_ends, the two ENDS of the count clamp, on the java leg. The
// roadmap's array-bounds work set carries two java task nodes, quoted here
// verbatim: java/C1 "count clamp v<0" and java/C2 "count clamp v>Max". Row 4,
// writer_bound_count, forges the count word to 7 — over the PLAN's bound of 4,
// but neither negative nor past the READER's own 8, so it exercises neither
// end. On 2026-09-19 the negative arm was measured deleted from five legs'
// runtime (c, cs, dart, elixir, js) with not one of their landed tests red:
// it is guarded by nothing, and this row is the guard. `clamped` is asserted
// EXACTLY == 1 and never >= 1, because a leg that also counts inside the
// bounds pass lands 2. C2's landed count of 4 is the WRITER's bound carried by
// the plan, never the reader's own 8 and never the forged 9.
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

	// PROBE C1 — the negative count clamps to ZERO, never -1, never 4, never 8.
	r := runProbe(t, classes, "Probe_reads", negFile)
	if r.n != 1 {
		t.Errorf("n=%d, want 1", r.n)
	}
	if r.refused || r.malformed {
		t.Errorf("a clean hostile read refused: refused=%v reason=%s malformed=%v", r.refused, r.reason, r.malformed)
	}
	if got := r.value["lead"]; got != "-1431655766" {
		t.Errorf("lead=%s, want 0xAAAAAAAA", got)
	}
	if got := r.value["trail"]; got != "-1145324613" {
		t.Errorf("trail=%s, want 0xBBBBBBBB", got)
	}
	if got := r.value["valsCount"]; got != "0" {
		t.Errorf("valsCount=%s, want 0 — a negative count clamps to ZERO, never -1, never the writer's 4, never the reader's 8", got)
	}
	if r.clamped != 1 {
		t.Errorf("clamped=%d, want 1 exactly — the negative arm fires once per record and never double-counts (§5.4)", r.clamped)
	}
	if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 {
		t.Errorf("counters moved on a clean hostile read: unknown=%d kindMismatch=%d widened=%d, want all 0",
			r.unknown, r.kindMismatch, r.widened)
	}

	// PROBE C2 — the count past the reader's own bound clamps to the WRITER's 4.
	r = runProbe(t, classes, "Probe_reads", overFile)
	if r.n != 1 {
		t.Errorf("n=%d, want 1", r.n)
	}
	if r.refused || r.malformed {
		t.Errorf("a clean hostile read refused: refused=%v reason=%s malformed=%v", r.refused, r.reason, r.malformed)
	}
	if got := r.value["lead"]; got != "-1431655766" {
		t.Errorf("lead=%s, want 0xAAAAAAAA", got)
	}
	if got := r.value["trail"]; got != "-1145324613" {
		t.Errorf("trail=%s, want 0xBBBBBBBB", got)
	}
	if got := r.value["valsCount"]; got != "4" {
		t.Errorf("valsCount=%s, want the WRITER's 4 — never the reader's own 8 and never the forged 9", got)
	}
	if got := r.value["vals"]; got != "list:1000,1001,1002,1003,0,0,0,0" {
		t.Errorf("vals=%s, want list:1000,1001,1002,1003 then the reader's declared default 0 in the grown slots", got)
	}
	if r.clamped != 1 {
		t.Errorf("clamped=%d, want 1 exactly — the high arm fires once per record and never double-counts (§5.4)", r.clamped)
	}
	if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 {
		t.Errorf("counters moved on a clean hostile read: unknown=%d kindMismatch=%d widened=%d, want all 0",
			r.unknown, r.kindMismatch, r.widened)
	}
}
