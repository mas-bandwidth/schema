package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// THE POINTER GRAPH'S IDENTITY ON THE JAVA LEG (docs/PORTING.md M6 and M14;
// docs/SPEC-TABLES.md §3.1, §16). Three properties of the save-side numbering,
// each held by a driver over the generated tables/pointers unit:
//
//   - A NODE NAMED BY TWO POINTERS IS WRITTEN ONCE (M6): graph_shared, whose
//     `head`, `alias` and `ground.head` name one ListNode and whose tree names
//     one leaf twice, loads and re-saves to the committed bytes the C++
//     reference and the Go engine write.
//   - A CYCLE IS REFUSED AT AN OPEN ENTRY (M6): a TreeNode whose own pointer
//     names it, and a two-node loop, save to null; a diamond, which shares a
//     node and is no cycle, saves, and its load hands back one object at both
//     pointers.
//   - THE TEXT FORM LABELS A SHARED NODE (M14): graph_shared's text is the
//     committed golden, `"&node": N` at the first appearance and the bare
//     label after.
//
// Each property has a REVERSED WITNESS, held apart in
// TestJavaPointerGraphNegativeControl: the same driver over a copy of the
// generated runtime with that property's one check planted out must fail and
// name the property. A witness that stays green is a test that cannot see.
func TestJavaPointerGraphSharesOnceRefusesCyclesAndLabelsNodes(t *testing.T) {
	t.Parallel()
	javac, javaBin, files, wire, golden := graphFixture(t)
	if out, ok := runGraphDriver(t, javac, javaBin, files, wire, golden); !ok {
		t.Fatalf("the Java pointer graph driver failed:\n%s", out)
	}
}

// TestJavaPointerGraphNegativeControl is the reversed witness of each property
// above: one check planted out of the generated runtime, and the driver must go
// red on that property by name.
func TestJavaPointerGraphNegativeControl(t *testing.T) {
	t.Parallel()
	javac, javaBin, files, wire, golden := graphFixture(t)
	witnesses := []struct {
		name, file, from, to, want string
	}{
		{"cycle_open_check", "TableVariable.java", "if (keyOpen[i]) {", "if (false) {", "FAILED: cycle"},
		{"shared_node_once", "TableVariable.java", "if (keyIndex[i] == 0) {", "if (true) {", "FAILED: re-save"},
		{"node_label", "", "if (graph.useCount(child) <= 1) {", "if (true) {", "FAILED: toJson"},
	}
	for _, w := range witnesses {
		t.Run("witness_"+w.name, func(t *testing.T) {
			t.Parallel()
			planted := map[string][]byte{}
			hits := 0
			for name, data := range files {
				s := string(data)
				if (w.file == "" || name == w.file) && strings.Contains(s, w.from) {
					hits += strings.Count(s, w.from)
					s = strings.ReplaceAll(s, w.from, w.to)
				}
				planted[name] = []byte(s)
			}
			if hits == 0 || (w.file != "" && hits != 1) {
				t.Fatalf("the %s witness planted %d sites; it must find its one check", w.name, hits)
			}
			out, ok := runGraphDriver(t, javac, javaBin, planted, wire, golden)
			if ok {
				t.Fatalf("the %s witness left the driver green: the test cannot see the property", w.name)
			}
			if !strings.Contains(out, w.want) {
				t.Fatalf("the %s witness failed for another reason (want %q):\n%s", w.name, w.want, out)
			}
		})
	}
}

// runGraphDriver compiles files beside graphDriver and runs it over the
// shared wire and its golden text. It answers the driver's output and whether
// it printed ok.
func runGraphDriver(t *testing.T, javac, javaBin string, files map[string][]byte, wire, golden []byte) (string, bool) {
	t.Helper()
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	srcdir := filepath.Join(dir, "src", "graphdemo")
	for _, d := range []string{classes, srcdir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range files {
		if strings.HasSuffix(name, ".java") {
			if err := os.WriteFile(filepath.Join(srcdir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, data := range map[string][]byte{"shared.wire": wire, "shared.json": golden, "Driver.java": []byte(graphDriver)} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srcs, err := filepath.Glob(filepath.Join(srcdir, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, srcs...)
	args = append(args, filepath.Join(dir, "Driver.java"))
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Driver",
		filepath.Join(dir, "shared.wire"), filepath.Join(dir, "shared.json")).CombinedOutput()
	return string(out), err == nil && strings.Contains(string(out), "ok")
}

const graphDriver = `import graphdemo.SceneVariable;
import graphdemo.TableVariable;
import graphdemo.TreeNodeVariable;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.Arrays;

public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    static TreeNodeVariable.Value node(String label) {
        TreeNodeVariable.Value n = new TreeNodeVariable.Value();
        n.label = label;
        return n;
    }

    static SceneVariable.Value scene(TreeNodeVariable.Value tree) {
        SceneVariable.Value s = new SceneVariable.Value();
        s.name = "graph";
        s.tree = tree;
        return s;
    }

    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        byte[] golden = Files.readAllBytes(Paths.get(args[1]));

        // M6: a shared node is written once, byte for byte with the reference
        TableVariable.Report report = new TableVariable.Report();
        SceneVariable.Value shared = SceneVariable.load(wire, report);
        if (shared == null || report.malformed || report.refused) { fail("graph_shared did not load"); }
        if (shared.head == null || shared.head != shared.alias || shared.head != shared.ground.head) {
            fail("load: the three pointers to one ListNode are not one object");
        }
        byte[] saved = SceneVariable.save(shared);
        if (saved == null || !Arrays.equals(wire, saved)) {
            fail("re-save of graph_shared differs from the reference bytes (" + wire.length + " vs "
                    + (saved == null ? -1 : saved.length) + "): a shared node was not written once");
        }

        // M14: the text form labels the shared node and names it after
        byte[] text = SceneVariable.toJson(shared);
        if (!Arrays.equals(golden, text)) {
            fail("toJson of graph_shared differs from the golden: the &node label is not carried");
        }

        // M6: a cycle is refused at an OPEN entry, and a diamond is not a cycle
        TreeNodeVariable.Value self = node("self");
        self.left = self;
        if (SceneVariable.save(scene(self)) != null) { fail("cycle: a node naming itself saved"); }
        TreeNodeVariable.Value a = node("a");
        TreeNodeVariable.Value b = node("b");
        a.left = b;
        b.right = a;
        if (SceneVariable.save(scene(a)) != null) { fail("cycle: a two-node loop saved"); }
        TreeNodeVariable.Value top = node("top");
        TreeNodeVariable.Value leaf = node("leaf");
        top.left = leaf;
        top.right = leaf;
        byte[] diamond = SceneVariable.save(scene(top));
        if (diamond == null) { fail("a diamond, which is no cycle, was refused"); }
        TableVariable.Report again = new TableVariable.Report();
        SceneVariable.Value back = SceneVariable.load(diamond, again);
        if (back == null || again.malformed || back.tree == null || back.tree.left == null
                || back.tree.left != back.tree.right) {
            fail("re-save: the diamond's shared leaf did not load back as one node");
        }

        System.out.println("ok");
    }
}
`

// graphFixture is what both graph tests run over: the JDK, the generated
// tables/pointers unit, graph_shared's wire and its golden text.
func graphFixture(t *testing.T) (javac, javaBin string, files map[string][]byte, wire, golden []byte) {
	t.Helper()
	javac, javaBin = javaTools(t)
	files, err := Generate(unitFromDir(t, filepath.Join("..", "..", "..", "tables", "pointers")))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..", "..")
	if wire, err = os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "graph_shared.bin")); err != nil {
		t.Fatal(err)
	}
	if golden, err = os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", "graph_shared.json")); err != nil {
		t.Fatal(err)
	}
	// the golden carries the property: a label, and the label named again
	if !strings.Contains(string(golden), `"&node": 1,`) || !strings.Contains(string(golden), "{\n    \"&node\": 1\n  }") {
		t.Fatal("graph_shared.json carries no shared-node label; the fixture no longer holds M14")
	}
	return javac, javaBin, files, wire, golden
}
