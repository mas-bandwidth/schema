package javatable

// THE WRITE COUNTS EVERY CONTAINER THE READ DOES (docs/SPEC-TABLES.md §16.2,
// §16.14 W2), held on the generated Java toJson: a chain of nodes, each
// holding one EMPTY container of a kind the writer always emits (an array, a
// union's object, a map's object, an enum-keyed array's object, a flags
// value's array), writes and reads back with 127 nodes under the root, the
// last container at depth 128, and toJson refuses 128 nodes, the last one's
// container at 129. The unit is internal/tabletext/depth_write_test.go's
// depthWriteUnit; the chains are built by the Go engine and handed to Java
// as wire, so the probe needs no per-table builder.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// javaDepthWriteUnit is internal/tabletext/depth_write_test.go's
// depthWriteUnit, package and all.
const javaDepthWriteUnit = `package depthwrite

enum Side
{
    Left
    Right
}

union Pick
{
    a int32
    b int32
}

table ArrNode
{
    next *ArrNode
    vals [..4]int32
}

table UnionNode
{
    next *UnionNode
    pick Pick
}

table MapNode
{
    next *MapNode
    by   map[uint8]int32
}

table KeyedNode
{
    next  *KeyedNode
    sides [Side]int32
}

flags Mark
{
    Hot
    Cold
}

table FlagsNode
{
    next  *FlagsNode
    marks Mark
}
`

// javaDepthWriteProbe loads one wire file as the named table and answers
// whether toJson wrote it and whether fromJson read the text back.
const javaDepthWriteProbe = `import java.lang.reflect.Method;
import java.nio.file.Files;
import java.nio.file.Paths;

public final class DepthWriteProbe {
    private DepthWriteProbe() {}

    public static void main(String[] args) throws Exception {
        boolean loaded = false;
        boolean wrote = false;
        boolean readBack = false;
        boolean threw = false;
        String thrown = "";
        try {
            Class<?> table = Class.forName("depthwrite." + args[0] + "Variable");
            Class<?> valueClass = Class.forName("depthwrite." + args[0] + "Variable$Value");
            Method load = table.getMethod("load", byte[].class, depthwrite.TableVariable.Report.class);
            Method toJson = table.getMethod("toJson", valueClass);
            Method fromJson = table.getMethod("fromJson", byte[].class, depthwrite.TableVariable.Report.class);
            Object v = load.invoke(null, Files.readAllBytes(Paths.get(args[1])), new depthwrite.TableVariable.Report());
            loaded = v != null;
            if (loaded) {
                byte[] text = (byte[]) toJson.invoke(null, v);
                wrote = text != null;
                if (wrote) {
                    readBack = fromJson.invoke(null, text, new depthwrite.TableVariable.Report()) != null;
                }
            }
        } catch (Throwable e) {
            threw = true;
            thrown = e.getClass().getName();
        }
        System.out.println("load=" + (loaded ? 1 : 0) + " write=" + (wrote ? 1 : 0) + " read=" + (readBack ? 1 : 0) + " threw=" + (threw ? 1 : 0) + (threw ? " class=" + thrown : ""));
    }
}
`

// depthChainWire is the engine's wire for a chain of n nodes of the table
// under the root, each linked by `next`, every other field its default.
func depthChainWire(t *testing.T, m *tabletext.Model, table string, n int) []byte {
	t.Helper()
	st := m.Lookup(table)
	root := m.New(st)
	cur := root
	for range n {
		child := m.New(st)
		var next *tabletext.Field
		for j := range cur.Fields {
			if cur.Fields[j].Def.Name == "next" {
				next = &cur.Fields[j]
			}
		}
		if next == nil {
			t.Fatalf("%s has no next field", table)
		}
		next.Cell.Node = child
		cur = child
	}
	data, err := tablewire.Encode(m, root)
	if err != nil {
		t.Fatalf("%s: the engine could not save a chain of %d: %v", table, n, err)
	}
	return data
}

func TestJavaJsonWriteDepthCountsEveryContainer(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)

	u := unitFrom(t, javaDepthWriteUnit)
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	root := t.TempDir()
	srcdir := filepath.Join(root, "src", "depthwrite")
	classes := filepath.Join(root, "classes")
	for _, d := range []string{srcdir, classes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var srcs []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(srcdir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, p)
	}
	probeMain := filepath.Join(root, "DepthWriteProbe.java")
	if err := os.WriteFile(probeMain, []byte(javaDepthWriteProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, srcs...)
	args = append(args, probeMain)
	if out, err := testproc.Command(t, javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	for _, table := range []string{"ArrNode", "UnionNode", "MapNode", "KeyedNode", "FlagsNode"} {
		for _, n := range []int{127, 128} {
			path := filepath.Join(root, table+"-"+strconv.Itoa(n)+".bin")
			if err := os.WriteFile(path, depthChainWire(t, m, table, n), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := testproc.Command(t, javaBin, "-ea", "-cp", classes, "DepthWriteProbe", table, path).CombinedOutput()
			if err != nil {
				t.Fatalf("%s-%d probe: %v\n%s", table, n, err, out)
			}
			got := probeVerdicts(t, out)
			if got["load"] != "1" {
				t.Fatalf("%s-%d: the generated load refused the engine's wire: %s", table, n, out)
			}
			if n == 127 && (got["write"] != "1" || got["read"] != "1") {
				t.Fatalf("%s: 127 nodes under the root, the last container at depth 128, must write and read back: %s", table, out)
			}
			if n == 128 && got["write"] != "0" {
				t.Fatalf("%s: 128 nodes under the root put the last container at depth 129, and toJson wrote it: %s", table, out)
			}
		}
	}
}
