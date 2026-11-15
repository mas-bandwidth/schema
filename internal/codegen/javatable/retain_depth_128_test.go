package javatable

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJavaRetainKeepsDepth128 holds the retaining walk to §4's one nesting
// bound (docs/SPEC-TABLES.md §4, §6.6): the root body is depth 0 and each
// structural payload is one level deeper, so 128 levels pass and 129 refuse.
// A depth is not a retention capacity: every READ depth vector keeps its
// unknown field through the retaining load and writes the file back byte for
// byte, and every REFUSED depth vector is refused by the retaining load too.
func TestJavaRetainKeepsDepth128(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it builds and runs the generated Java)")
	javac, javaBin := javaToolsRequired(t)

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	files := cookUnitFiles(t, filepath.Join(root, "tables", "pointers"))
	dir := compileCookUnit(t, javac, files, "graphdemo", map[string]string{"RetainDepthDriver": retainDepthDriver})

	framing := filepath.Join(root, "testdata", "wire", "tables", "framing")
	reads := []string{"depth_tables_128", "depth_arrays_128", "depth_union_tables_64", "depth_keyed_128"}
	refused := []string{"depth_tables_129", "depth_arrays_129", "depth_union_tables_65", "depth_keyed_129", "depth_tables_5000"}

	args := []string{"-ea", "-cp", dir, "RetainDepthDriver", strconv.Itoa(len(reads))}
	for _, name := range reads {
		args = append(args, filepath.Join(framing, name+".bin"))
	}
	for _, name := range refused {
		args = append(args, filepath.Join(framing, name+".bin"))
	}
	out, err := exec.Command(javaBin, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("the retaining depth probe failed: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "FAIL") {
		t.Fatalf("the retaining walk did not hold §4's 128 levels:\n%s", out)
	}
	t.Logf("%s", out)
}

const retainDepthDriver = `import graphdemo.SceneRetain;
import graphdemo.SceneVariable;
import graphdemo.TableRetain;
import graphdemo.TableVariable;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class RetainDepthDriver {
    static int bad = 0;

    static void fail(String m) {
        System.out.println("FAIL " + m);
        bad++;
    }

    public static void main(String[] a) throws Exception {
        int reads = Integer.parseInt(a[0]);
        for (int i = 1; i <= reads; i++) {
            String name = Path.of(a[i]).getFileName().toString();
            byte[] wire = Files.readAllBytes(Path.of(a[i]));
            TableRetain store = new TableRetain(1 << 20, 1 << 17);
            TableVariable.Report load = new TableVariable.Report();
            SceneVariable.Value v = SceneRetain.loadRetain(wire, store, load);
            if (v == null || load.damaged || load.refused) {
                fail(name + ": retaining load refused a readable depth");
                continue;
            }
            if (load.retainLost != 0) {
                fail(name + ": retaining load lost an unknown field: " + load.retainLost);
            }
            TableVariable.Report save = new TableVariable.Report();
            byte[] saved = SceneRetain.saveRetain(v, store, save);
            if (saved == null) {
                fail(name + ": retaining save refused");
                continue;
            }
            if (!Arrays.equals(saved, wire)) {
                fail(name + ": retained save changed the file bytes (" + saved.length + " vs " + wire.length + ")");
            }
            if (save.retainLost != 0) {
                fail(name + ": retaining save lost an unknown field: " + save.retainLost);
            }
        }
        for (int i = 1 + reads; i < a.length; i++) {
            String name = Path.of(a[i]).getFileName().toString();
            byte[] wire = Files.readAllBytes(Path.of(a[i]));
            TableRetain store = new TableRetain(1 << 20, 1 << 17);
            TableVariable.Report load = new TableVariable.Report();
            SceneVariable.Value v = SceneRetain.loadRetain(wire, store, load);
            if (v != null) {
                fail(name + ": retaining load accepted a refused depth");
            }
        }
        if (bad != 0) {
            System.out.println("BAD " + bad);
            System.exit(1);
        }
        System.out.println("OK");
    }
}
`
