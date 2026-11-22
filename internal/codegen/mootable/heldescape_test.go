package mootable_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestMooKindHeldEscapeRefuses (R17: the newer reader reads old data, never
// the other way): kind 31 is held by name and never written, so a reader meets
// it only as damage, exactly as 33 and 34. The shared input carries a kind 31
// field the reader cannot name; the tolerant load, the strict read, the
// retaining load and the text carrier all discard the whole read as
// malformed. The old reader skipped it by its L and read the file.
func TestMooKindHeldEscapeRefuses(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted readers)")
	t.Parallel()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "tblvarevo1")
	if g.err != nil {
		t.Fatalf("tblvarevo1: %v", g.err)
	}
	wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing/varevo_kind31_escape.bin"))
	if err != nil {
		t.Fatal(err)
	}
	got := g.run(t, cycleProbe(g, "EvoDoc"), map[string][]byte{"wire": wire})
	if want := cycleWant(0, true); got != want {
		t.Errorf("kind 31 at an unnamed field: got\n%s\nwant\n%s", got, want)
	}
}
