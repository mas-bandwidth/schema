package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A TABLE VALUE WHOSE POINTERS FORM A LONG CHAIN IS A LAWFUL VALUE
// (docs/SPEC-TABLES.md §3.1), and every writer handles every graph the reader
// loads: save writes it, and toJson writes it or refuses past the text cap
// (§16.3) — never an Error escaping. The graph walk that numbers the nodes
// before both writers is a work list, not the Java call stack, so its stack
// use does not grow with the chain.
const javaLongChainSchema = `package javachain

table Chain
{
    value int32 = 0
    next  *Chain
}
`

// javaLongChainWire builds the same chain in the engine and answers its save,
// the bytes the Java save must match.
func javaLongChainWire(t *testing.T, m *tabletext.Model, n int) []byte {
	t.Helper()
	root := m.New(m.Lookup("Chain"))
	cur := root
	for i := 0; i < n; i++ {
		value, ok := cur.FieldByKey("value")
		if !ok {
			t.Fatal("Chain has no field value")
		}
		value.Cell.I = int64(i)
		value.Cell.U = uint64(i)
		if i+1 == n {
			break
		}
		next, ok := cur.FieldByKey("next")
		if !ok {
			t.Fatal("Chain has no field next")
		}
		child := m.New(m.Lookup("Chain"))
		next.Cell.Node = child
		cur = child
	}
	wire, err := tablewire.Encode(m, root)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// TestJavaSaveLongPointerChain holds the graph walk on the Java leg: a chain
// of 200,000 nodes built in memory saves to the engine's bytes, loads back as
// the chain, and writes as text only by refusing past the cap; a 128-node
// chain round-trips through the text form. The child runs on the default
// thread stack, so a walk that recurses per pointer cannot pass.
func TestJavaSaveLongPointerChain(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chain.schema"), []byte(javaLongChainSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := unitFromDir(t, dir)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	big := javaLongChainWire(t, m, 200000)
	small := javaLongChainWire(t, m, 128)
	for name, data := range map[string][]byte{"big.wire": big, "small.wire": small} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	srcdir := filepath.Join(dir, "src", "javachain")
	classes := filepath.Join(dir, "classes")
	for _, d := range []string{srcdir, classes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(srcdir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	driver := filepath.Join(dir, "src", "ChainDriver.java")
	if err := os.WriteFile(driver, []byte(javaLongChainDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	srcs, err := filepath.Glob(filepath.Join(srcdir, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, srcs...)
	args = append(args, driver)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "ChainDriver",
		filepath.Join(dir, "big.wire"), filepath.Join(dir, "small.wire")).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("the Java long-chain driver failed: %v\n%s", err, out)
	}
}

const javaLongChainDriver = `import javachain.ChainVariable;
import javachain.TableVariable;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.Arrays;

public final class ChainDriver {
    private ChainDriver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    static ChainVariable.Value chain(int n) {
        ChainVariable.Value root = new ChainVariable.Value();
        ChainVariable.Value cur = root;
        for (int i = 0; i < n; i++) {
            cur.value = i;
            if (i + 1 < n) {
                cur.next = new ChainVariable.Value();
                cur = cur.next;
            }
        }
        return root;
    }

    static int length(ChainVariable.Value root) {
        int n = 0;
        ChainVariable.Value cur = root;
        while (cur != null) {
            if (cur.value != n) {
                fail("node " + n + " carries " + cur.value);
            }
            n++;
            cur = cur.next;
        }
        return n;
    }

    public static void main(String[] args) throws Exception {
        byte[] engine = Files.readAllBytes(Paths.get(args[0]));
        byte[] engineSmall = Files.readAllBytes(Paths.get(args[1]));

        ChainVariable.Value big = chain(200000);
        byte[] saved;
        try {
            saved = ChainVariable.save(big);
        } catch (Throwable t) {
            fail("save threw " + t);
            return;
        }
        if (saved == null) { fail("save refused the chain"); }
        if (!Arrays.equals(engine, saved)) {
            int d = 0;
            while (d < engine.length && d < saved.length && engine[d] == saved[d]) { d++; }
            fail("save bytes differ from the engine at " + d);
        }

        TableVariable.Report report = new TableVariable.Report();
        ChainVariable.Value back;
        try {
            back = ChainVariable.load(saved, report);
        } catch (Throwable t) {
            fail("load threw " + t);
            return;
        }
        if (back == null || report.damaged || report.refused) { fail("load of the saved chain did not round-trip"); }
        if (length(back) != 200000) { fail("load gave back another chain length"); }

        byte[] text;
        try {
            text = ChainVariable.toJson(big);
        } catch (Throwable t) {
            fail("toJson threw " + t);
            return;
        }
        if (text != null) { fail("toJson wrote a chain past the text cap"); }

        ChainVariable.Value small = chain(128);
        byte[] smallSaved;
        try {
            smallSaved = ChainVariable.save(small);
        } catch (Throwable t) {
            fail("small save threw " + t);
            return;
        }
        if (smallSaved == null) { fail("save refused the 128-node chain"); }
        if (!Arrays.equals(engineSmall, smallSaved)) { fail("128-node save bytes differ from the engine"); }

        byte[] smallText;
        try {
            smallText = ChainVariable.toJson(small);
        } catch (Throwable t) {
            fail("small toJson threw " + t);
            return;
        }
        if (smallText == null) { fail("toJson refused the 128-node chain"); }
        TableVariable.Report sr = new TableVariable.Report();
        ChainVariable.Value fromText = ChainVariable.fromJson(smallText, sr);
        if (fromText == null || sr.damaged || sr.refused) { fail("fromJson of the 128-node text did not round-trip"); }
        if (length(fromText) != 128) { fail("fromJson gave back another chain length"); }
        byte[] resaved = ChainVariable.save(fromText);
        if (resaved == null || !Arrays.equals(smallSaved, resaved)) { fail("the 128-node text round-trip changed the wire"); }

        System.out.println("ok");
    }
}
`
