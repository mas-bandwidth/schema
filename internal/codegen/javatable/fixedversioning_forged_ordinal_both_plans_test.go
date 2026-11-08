package javatable

// `forged_ordinal_both_plans`, §5.8 row 12 of docs/FIXED-FORM-VERSIONING-TESTS.md:
// `VOLD_/VNEW_enum_append` as they stand — `Tier { Bronze, Silver, Gold }` → `+ Platinum` —
// plus a forged `old_enum_append.bin` with `r0.tier` set to 4, an ordinal PAST the
// writer's three variants but a name the reader has. The SAME bytes are read TWICE:
// the NEW build (compiled plan) and the OLD build (identity plan). Neither may land
// Platinum, nor any value: under the strict read (docs/plans/strict-only.md) BOTH
// refuse the whole load as outOfRange, n = -1, with no counter moved.
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
	if bytes.Contains(raw[at+1:], needle) {
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
		if probe.r.n != -1 {
			t.Errorf("%s: n=%d, want -1: an ordinal past the writer's variants refuses the whole load", probe.which, probe.r.n)
		}
		if !probe.r.refused || probe.r.reason != "outOfRange" {
			t.Errorf("%s: refused=%v reason=%s, want a refusal named outOfRange", probe.which, probe.r.refused, probe.r.reason)
		}
		if probe.r.damaged {
			t.Errorf("%s: damaged=%v, want false: a forged value is a refusal, never framing damage", probe.which, probe.r.damaged)
		}
		if probe.r.unknown != 0 || probe.r.widened != 0 {
			t.Errorf("%s: counters moved: unknown=%d widened=%d, want all 0",
				probe.which, probe.r.unknown, probe.r.widened)
		}
	}
	if compiled.refused != identity.refused || compiled.reason != identity.reason {
		t.Errorf("the two plans disagree: compiled refused=%v reason=%s, identity refused=%v reason=%s, want the same verdict on both",
			compiled.refused, compiled.reason, identity.refused, identity.reason)
	}
}
