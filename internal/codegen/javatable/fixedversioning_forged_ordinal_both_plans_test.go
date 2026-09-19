package javatable

// `forged_ordinal_both_plans`, §5.8 row 12 of docs/FIXED-FORM-VERSIONING-TESTS.md:
// `VOLD_/VNEW_enum_append` as they stand — `Tier { Bronze, Silver, Gold }` → `+ Platinum` —
// plus a forged `old_enum_append.bin` with `r0.tier` set to 4, an ordinal PAST the
// writer's three variants but a name the reader has. The SAME bytes are read TWICE:
// the NEW build (compiled plan) and the OLD build (identity plan). Each must land
// `tier` None (0) and never Platinum; the counter asserted EXACTLY is `clamped == 1`
// on BOTH, never `>= 1` — a leg that also counts the ordinal op lands 2 and passes.
import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedVersioningForgedOrdinalBothPlans(t *testing.T) {
	corpus := corpusDir(t)
	oldFile := filepath.Join(corpus, "old_enum_append.bin")
	raw, err := os.ReadFile(oldFile)
	if err != nil {
		t.Fatalf("read %s: %v", oldFile, err)
	}

	// THE FORGE: locate `r0.tier` by its declared value 3 and its neighbour
	// `r0.seq = 9` (the manifest's lawful values for the writer), assert the
	// needle occurs exactly once, and overwrite the tier byte with 4.
	needle := []byte{0x03, 0x09, 0x00, 0x00, 0x00}
	at := bytes.Index(raw, needle)
	if at < 0 {
		t.Fatalf("the forge needle % x is not in %s", needle, oldFile)
	}
	if bytes.Index(raw[at+1:], needle) >= 0 {
		t.Fatalf("the forge needle % x occurs more than once in %s", needle, oldFile)
	}
	forged := append([]byte(nil), raw...)
	forged[at] = 0x04 // the forge
	hostile := filepath.Join(t.TempDir(), "hostile_enum_append.bin")
	if err := os.WriteFile(hostile, forged, 0o644); err != nil {
		t.Fatal(err)
	}

	_, classes := buildRow(t, t.TempDir(), "enum_append", []sideSpec{
		{key: "reads", schema: "VNEW_enum_append.schema", older: []string{"VOLD_enum_append.schema"}},
		{key: "refuses", schema: "VOLD_enum_append.schema"},
	})

	compiled := runProbe(t, classes, "Probe_reads", hostile)
	identity := runProbe(t, classes, "Probe_refuses", hostile)

	for _, probe := range []struct {
		which string
		r     probeRun
	}{
		{"the NEW build (compiled plan)", compiled},
		{"the OLD build (identity plan)", identity},
	} {
		if probe.r.n != 1 {
			t.Errorf("%s: n=%d, want 1", probe.which, probe.r.n)
		}
		if probe.r.refused {
			t.Errorf("%s: refused=%v reason=%s, want no refusal", probe.which, probe.r.refused, probe.r.reason)
		}
		if probe.r.malformed {
			t.Errorf("%s: malformed=%v, want false", probe.which, probe.r.malformed)
		}
		if got := probe.r.value["tier"]; got != "0" {
			t.Errorf("%s: tier=%s, want None (0) and never Platinum", probe.which, got)
		}
		if got := probe.r.value["seq"]; got != "9" {
			t.Errorf("%s: seq=%s, want 9", probe.which, got)
		}
		if probe.r.clamped != 1 {
			t.Errorf("%s: clamped=%d, want 1 exactly", probe.which, probe.r.clamped)
		}
		if probe.r.unknown != 0 || probe.r.kindMismatch != 0 || probe.r.widened != 0 {
			t.Errorf("%s: counters moved: unknown=%d kindMismatch=%d widened=%d, want all 0",
				probe.which, probe.r.unknown, probe.r.kindMismatch, probe.r.widened)
		}
	}
	if compiled.clamped != identity.clamped {
		t.Errorf("the two plans disagree: compiled clamped=%d, identity clamped=%d, want the same number on both",
			compiled.clamped, identity.clamped)
	}
}
