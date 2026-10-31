package javatable

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A COOKED MAP HAS FIND (docs/PORTING.md M19; docs/SPEC-TABLES.md §2.8): a binary
// search over the ascending entries where they lie. The probe opens the golden
// cook of map_full (mapdemo.Fleet), and for its string-keyed `ships` and its
// SIGNED int16-keyed `tiers` finds every key the array holds at that entry's own
// offset, and answers -1 for a key it does not hold. The control plants a
// reversed order into each Find (the emitter's `order < 0` branch turned
// around), and the probe must go red.
const cookFindDriver = `import java.nio.file.Files;
import java.nio.file.Path;

public final class CookFindProbe {
    public static void main(String[] args) throws Exception {
        byte[] data = Files.readAllBytes(Path.of(args[0]));
        mapdemo.FleetCook cook = mapdemo.FleetCook.open(data);
        if (cook == null) throw new AssertionError("open");
        int root = cook.root();
        int n = mapdemo.FleetRow.shipsCount(data, root);
        int ships = cook.at(mapdemo.FleetRow.shipsSlot(root), n * mapdemo.FleetRow.shipsStride);
        if (n < 2 || ships < 0) throw new AssertionError("ships: " + n);
        for (int i = 0; i < n; i++) {
            int at = ships + i * mapdemo.FleetRow.shipsStride;
            int k = mapdemo.FleetShipsEntryRow.keyAt(at), len = mapdemo.FleetShipsEntryRow.keyLength(data, at);
            byte[] key = java.util.Arrays.copyOfRange(data, k, k + len);
            if (mapdemo.FleetRow.shipsFind(data, ships, n, key) != at) throw new AssertionError("ships find " + i);
        }
        if (mapdemo.FleetRow.shipsFind(data, ships, n, "ÿÿ no such ship".getBytes("UTF-8")) != -1) throw new AssertionError("ships absent");
        int m = mapdemo.FleetRow.tiersCount(data, root);
        int tiers = cook.at(mapdemo.FleetRow.tiersSlot(root), m * mapdemo.FleetRow.tiersStride);
        if (m < 2 || tiers < 0) throw new AssertionError("tiers: " + m);
        for (int i = 0; i < m; i++) {
            int at = tiers + i * mapdemo.FleetRow.tiersStride;
            short key = mapdemo.FleetTiersEntryRow.key(data, at);
            if (mapdemo.FleetRow.tiersFind(data, tiers, m, key) != at) throw new AssertionError("tiers find " + i);
        }
        if (mapdemo.FleetRow.tiersFind(data, tiers, m, Short.MIN_VALUE) != -1) throw new AssertionError("tiers absent");
        System.out.println("find ok " + n + " ships, " + m + " tiers");
    }
}
`

func runCookFind(t *testing.T, plant bool) (string, bool) {
	t.Helper()
	javac, javaBin := javaTools(t)
	files := cookUnitFiles(t, filepath.Join("..", "..", "..", "tables", "maps"))
	if plant {
		row := string(files["FleetRow.java"])
		from := "if (order < 0) {\n                low = mid + 1;\n            } else {\n                high = mid;\n            }"
		to := "if (order > 0) { // SABOTAGED\n                low = mid + 1;\n            } else {\n                high = mid;\n            }"
		if strings.Count(row, from) < 2 {
			t.Fatalf("the Find control must plant every Find in FleetRow.java, found %d sites", strings.Count(row, from))
		}
		files["FleetRow.java"] = []byte(strings.ReplaceAll(row, from, to))
	}
	dir := compileCookUnit(t, javac, files, "mapdemo", map[string]string{"CookFindProbe": cookFindDriver})
	golden := filepath.Join("..", "..", "..", "testdata", "conformance", "tables", "cook-write", "map_full.cook")
	out, err := exec.Command(javaBin, "-ea", "-cp", dir, "CookFindProbe", golden).CombinedOutput()
	return string(out), err == nil
}

func TestJavaCookMapFind(t *testing.T) {
	t.Parallel()
	if out, ok := runCookFind(t, false); !ok || !strings.Contains(out, "find ok") {
		t.Fatalf("Find over the cooked map_full:\n%s", out)
	}
}

func TestJavaCookMapFindNegativeControl(t *testing.T) {
	t.Parallel()
	if out, ok := runCookFind(t, true); ok {
		t.Fatalf("a Find with its order reversed left the probe green:\n%s", out)
	}
}
