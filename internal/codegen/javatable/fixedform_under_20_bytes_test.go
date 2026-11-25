package javatable

// fixed_form_under_20_bytes — schema#876 F7, the under-20-bytes row (was the
// malformed row), on the java leg. F7 was a GAP on all nine legs; five of them
// (go, c, dart, js, cpp) are now closed and merged, and java is one of four
// left. A file shorter than the header REFUSES BY NAME, `truncated`
// (docs/FIXED-FORM-ALGORITHM.md §5.3 refusal table; there is no `malformed`
// flag on this form's report) — asserting Reason.layoutMalformed here would
// be wrong, because a short FILE is truncated, not a malformed layout.
// k == 0 is guarded by the empty guard (data == null || data.length < 1) and
// k = 1..19 by the length guard (data.length < fileHeaderBytes + 4): two
// clauses, one answer. The destination is proved untouched with assertFresh on
// the non-poisoned probe: every field still equals a fresh value. CONTROL 2
// deletes the length guard and the reserved-byte loop walks off a short array.

import (
	"os"
	"path/filepath"
	"testing"
)

// fileHeaderBytes mirrors the emitter's own constant at fixedruntime.go:189 —
// the header is 16 bytes and the layout's own u32 length is the next 4, so a
// file is truncated below fileHeaderBytes + 4. The loop bound is written from
// the constant, never a hardcoded 20.
const fileHeaderBytes = 16

func TestFixedFormUnder20Bytes(t *testing.T) {
	corpus := corpusDir(t)
	oldFile := filepath.Join(corpus, "old_array_bounded_grow.bin")
	raw, err := os.ReadFile(oldFile)
	if err != nil {
		t.Fatalf("read %s: %v", oldFile, err)
	}

	dir := t.TempDir()
	_, classes := buildRow(t, dir, "fixed_form_under_20_bytes", []sideSpec{
		{key: "reads", schema: "VNEW_array_bounded_grow.schema", older: []string{"VOLD_array_bounded_grow.schema"}},
		{key: "refuses", schema: "VOLD_array_bounded_grow.schema"},
	})

	// The full file must read cleanly FIRST, so a broken fixture cannot pass
	// this row by accident.
	full := runProbe(t, classes, "Probe_reads", oldFile)
	if full.n < 1 || full.refused {
		t.Fatalf("the full file does not read cleanly: n=%d refused=%v reason=%s",
			full.n, full.refused, full.reason)
	}

	for k := range fileHeaderBytes + 4 {
		shortFile := filepath.Join(t.TempDir(), "short.bin")
		if err := os.WriteFile(shortFile, raw[:k], 0o644); err != nil {
			t.Fatal(err)
		}
		r := runProbe(t, classes, "Probe_reads", shortFile)
		if !r.refused {
			t.Errorf("k=%d: refused=%v, want true — a %d-byte file is under the header", k, r.refused, k)
		}
		if r.reason != "truncated" {
			t.Errorf("k=%d: reason=%s, want truncated — a short file refuses by name (§5.3)", k, r.reason)
		}
		if r.n != -1 {
			t.Errorf("k=%d: n=%d, want -1", k, r.n)
		}
		if r.unknown != 0 || r.widened != 0 {
			t.Errorf("k=%d: counters moved: unknown=%d widened=%d, want all 0",
				k, r.unknown, r.widened)
		}
		if r.layoutHash != "0x0" {
			t.Errorf("k=%d: layoutHash=%s, want 0x0 — only the layout refusals report a hash", k, r.layoutHash)
		}

		// The destination is proved untouched on the NON-poisoned probe, where
		// "untouched" is "still every field of a fresh value" — assertFresh. The
		// reads probe is poisoned (0x5A through its non-final fields), so a
		// value==fresh comparison cannot serve there; the refuses probe is not.
		ref := runProbe(t, classes, "Probe_refuses", shortFile)
		if !ref.refused || ref.reason != "truncated" || ref.n != -1 {
			t.Fatalf("k=%d: the refuses probe did not refuse truncated: n=%d refused=%v reason=%s", k, ref.n, ref.refused, ref.reason)
		}
		assertFresh(t, ref)
	}
}
