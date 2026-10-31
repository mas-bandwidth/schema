package javatable

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A MAP-BEARING COOK IS REFUSED BY NAME (docs/PORTING.md M19; docs/SPEC-TABLES.md
// §2.8). The Java cook writer lays out no map entry array, so a root whose
// closure holds a map would be written with its map holders naming no entries:
// a cook that silently drops the map. Every write entry point of such a root
// refuses instead, naming the field, as the tool's cook refuses a map-bearing
// unit (compiler/tablesmaps.go). tables/maps' Cells holds `rows`, a map.
//
// Neither test is parallel: the control swaps the emitter's map finder, which
// the gate reads.
const cookMapRefusalDriver = `public final class CookMapProbe {
    public static void main(String[] args) {
        mapdemo.CellsVariable.Value v = new mapdemo.CellsVariable.Value();
        long bytes = mapdemo.CellsCook.cookMeasure(v);
        String reason = mapdemo.CellsCook.cookMeasureReason(v).reason();
        boolean wrote = mapdemo.CellsCook.cookFrom(v, new byte[1 << 16], 0);
        System.out.println("measure=" + bytes + " reason=" + reason + " wrote=" + wrote);
    }
}
`

func runCookMapProbe(t *testing.T) string {
	t.Helper()
	javac, javaBin := javaTools(t)
	files := cookUnitFiles(t, filepath.Join("..", "..", "..", "tables", "maps"))
	dir := compileCookUnit(t, javac, files, "mapdemo", map[string]string{"CookMapProbe": cookMapRefusalDriver})
	out, err := exec.Command(javaBin, "-cp", dir, "CookMapProbe").CombinedOutput()
	if err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}
	return string(out)
}

const cookMapRefused = "measure=-1 reason=map_bearing: Cells.rows wrote=false"

func TestJavaCookRefusesAMapBearingRoot(t *testing.T) {
	if out := runCookMapProbe(t); !strings.Contains(out, cookMapRefused) {
		t.Fatalf("Cells holds a map and its cook must refuse by name (%q), got:\n%s", cookMapRefused, out)
	}
}

func TestJavaCookMapRefusalNegativeControl(t *testing.T) {
	saved := cookMapFieldOf
	cookMapFieldOf = func(*ir.Unit, *ir.Struct) string { return "" }
	defer func() { cookMapFieldOf = saved }()
	out := runCookMapProbe(t)
	if strings.Contains(out, cookMapRefused) {
		t.Fatalf("with the refusal planted out of the emitter the probe still refuses: the gate cannot see the refusal\n%s", out)
	}
	if !strings.Contains(out, "wrote=true") {
		t.Fatalf("with the refusal planted out the cook should write (dropping the map), got:\n%s", out)
	}
}
