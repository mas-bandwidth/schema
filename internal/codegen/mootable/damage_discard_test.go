package mootable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The controls remove the runtime's final discard, not the damage detection
// or the assertions. Both inputs still report malformed, but return values.
func TestMooDamageDiscardNegativeControls(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted readers)")
	t.Parallel()
	t.Run("retaining load", func(t *testing.T) {
		g := generateFiles(t, map[string]string{"Occ.schema": occurrenceUnit})
		b := &wb{}
		b.outer(true, false)
		b.end()
		wire := b.finish()
		before := mooRetain(t, g, "Node", wire, retainFull, -1)
		if !before.malformed || before.value {
			t.Fatalf("control needs a refused damaged load: %+v", before)
		}
		removeMooDiscard(t, g, "table_load.moo", "\tif report.malformed then\n\t\treturn nil\n\tend\n")
		after := mooRetain(t, g, "Node", wire, retainFull, -1)
		if !after.malformed || !after.value {
			t.Fatalf("removing the runtime discard did not expose the partial load: %+v", after)
		}
	})
	t.Run("message batch", func(t *testing.T) {
		units, _ := readManifest(t)
		g := generateUnit(t, units, "backenddemo")
		if g.err != nil {
			t.Fatal(g.err)
		}
		conn := readRoot(t, "testdata/wire/tables/backend_conn.bin")
		wire := readRoot(t, "testdata/wire/tables/backend_batch_damaged_second_message.bin")
		inputs := map[string][]byte{"conn": conn, "cases": packCases([][]byte{wire})}
		probe := damageProbe(g, "LoginRequest", 256)
		before := g.run(t, probe, inputs)
		_, rest, _ := strings.Cut(before, " ")
		if mooAnswer(rest) != "malformed" {
			t.Fatalf("control needs a refused batch: %s", before)
		}
		removeMooDiscard(t, g, "table_message.moo", "\tif rep.malformed then\n\t\treturn 0, {}\n\tend\n")
		after := g.run(t, probe, inputs)
		if !strings.Contains(after, "DAMAGE RETURNED VALUES") {
			t.Fatalf("removing the runtime discard did not expose the partial batch: %s", after)
		}
	})
}

func removeMooDiscard(t *testing.T, g *generatedUnit, file, anchor string) {
	t.Helper()
	src := string(g.files[file])
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("%s: discard anchor must occur exactly once", file)
	}
	if err := os.WriteFile(filepath.Join(g.dir, file), []byte(strings.Replace(src, anchor, "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}
