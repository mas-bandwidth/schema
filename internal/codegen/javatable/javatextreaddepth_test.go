package javatable

// THE TEXT READ'S DEPTH CAP (docs/SPEC-TABLES.md §16.2, §16.14 W2): a pointer,
// a nested table or an array of tables nests as deep as the text spells it.
// At 128 it reads and at 129 it refuses, the same depth the Go reader
// (internal/tabletext) and the Java text writer use, and no depth throws.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const javaTextReadDepthSchema = `package probe

table Node {
 next *Node
 kids []*Node
}
`

const javaTextReadDepthProbe = `import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

public final class DepthProbe {
    private DepthProbe() {}

    public static void main(String[] args) throws Exception {
        if (args[0].equals("read")) {
            byte[] text = Files.readAllBytes(Paths.get(args[1]));
            boolean threw = false;
            boolean read = false;
            boolean wrote = false;
            String thrown = "";
            try {
                probe.TableVariable.Report report = new probe.TableVariable.Report();
                probe.NodeVariable.Value v = probe.NodeVariable.fromJson(text, report);
                read = v != null;
                if (read) {
                    wrote = probe.NodeVariable.toJson(v) != null;
                }
            } catch (Throwable e) {
                threw = true;
                thrown = e.getClass().getName();
            }
            System.out.println("read=" + (read ? 1 : 0) + " write=" + (wrote ? 1 : 0) + " threw=" + (threw ? 1 : 0) + (threw ? " class=" + thrown : ""));
            return;
        }
        if (args[0].equals("build")) {
            int n = Integer.parseInt(args[1]);
            boolean threw = false;
            boolean wrote = false;
            boolean readBack = false;
            String thrown = "";
            try {
                probe.NodeVariable.Value root = new probe.NodeVariable.Value();
                probe.NodeVariable.Value cur = root;
                for (int i = 0; i < n; i++) {
                    probe.NodeVariable.Value child = new probe.NodeVariable.Value();
                    cur.next = child;
                    cur = child;
                }
                byte[] text = probe.NodeVariable.toJson(root);
                wrote = text != null;
                if (wrote) {
                    probe.TableVariable.Report report = new probe.TableVariable.Report();
                    readBack = probe.NodeVariable.fromJson(text, report) != null;
                }
            } catch (Throwable e) {
                threw = true;
                thrown = e.getClass().getName();
            }
            System.out.println("write=" + (wrote ? 1 : 0) + " read=" + (readBack ? 1 : 0) + " threw=" + (threw ? 1 : 0) + (threw ? " class=" + thrown : ""));
            return;
        }
        System.out.println("unknown mode");
        System.exit(2);
    }
}
`

// nextText spells a chain of n pointer objects under the root: the root, then
// n pointees linked by `next`.
func nextText(n int) string {
	var b strings.Builder
	for range n {
		b.WriteString(`{"next":`)
	}
	b.WriteString(`{}`)
	for range n {
		b.WriteByte('}')
	}
	return b.String()
}

// kidsText spells a chain of n arrays of tables under the root.
func kidsText(n int) string {
	var b strings.Builder
	for range n {
		b.WriteString(`{"kids":[`)
	}
	b.WriteString(`{}`)
	for range n {
		b.WriteString(`]}`)
	}
	return b.String()
}

// probeVerdicts reads one Java probe line. It fails on any Throwable, because
// the read must refuse rather than throw at every depth.
func probeVerdicts(t *testing.T, out []byte) map[string]string {
	t.Helper()
	line := strings.TrimSpace(string(out))
	got := map[string]string{}
	for field := range strings.FieldsSeq(line) {
		if k, v, ok := strings.Cut(field, "="); ok {
			got[k] = v
		}
	}
	if got["threw"] != "0" {
		t.Fatalf("the Java text read or write threw: %q", line)
	}
	return got
}

// TestJavaTextReadDepthCap holds the generated Java text read to the depth cap
// on the same texts the Go reader reads: 128 nests in, 129 refuses, and a
// 100000-deep text refuses without a Throwable. The writer counts every
// container the reader does (§16.14 W2): each Node writes its `kids` array,
// so a chain of 127 nodes under the root puts the last `kids` at depth 128
// and round-trips, and one of 128 puts it at 129 and does neither.
func TestJavaTextReadDepthCap(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)

	u := unitFrom(t, javaTextReadDepthSchema)
	m := tabletext.NewModel(u)
	node := m.Lookup("Node")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	root := t.TempDir()
	srcdir := filepath.Join(root, "src", "probe")
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
	probeMain := filepath.Join(root, "DepthProbe.java")
	if err := os.WriteFile(probeMain, []byte(javaTextReadDepthProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, srcs...)
	args = append(args, probeMain)
	if out, err := testproc.Command(t, javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	runProbe := func(args ...string) []byte {
		t.Helper()
		cmd := testproc.Command(t, javaBin, "-ea", "-cp", classes, "DepthProbe")
		cmd.Args = append(cmd.Args, args...)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("probe %v: %v\n%s", args, err, out)
		}
		return out
	}

	// readText answers the Go reader's verdict on the text, the Go writer's
	// verdict on the value it read (false when it did not read), and the
	// Java probe's verdicts on the same text.
	readText := func(name, text string) (bool, bool, map[string]string) {
		t.Helper()
		inst := m.New(node)
		var report tabletext.Report
		goRead := m.Read(inst, []byte(text), &report)
		goWrite := false
		if goRead {
			_, err := m.Write(inst)
			goWrite = err == nil
		}
		path := filepath.Join(root, name+".json")
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		got := probeVerdicts(t, runProbe("read", path))
		return goRead, goWrite, got
	}

	// The reference first: the Go reader's own verdicts on the same texts.
	// A pointer chain nests one table value per link under the root; an array
	// of tables nests a table and its element, so the engine's own spacing
	// decides where its cap falls. Writing back what was read is the Go
	// writer's verdict too (§16.14 W2): a chain of 128 nodes reads, since its
	// deepest object sits at 128, but every node writes its `kids` array, and
	// the last one's empty `[]` sits at 129, so the write refuses it; 127
	// writes back.
	for _, c := range []struct {
		name  string
		text  string
		want  *bool
		write *bool
	}{
		{"next-127", nextText(127), new(true), new(true)},
		{"next-128", nextText(128), new(true), new(false)},
		{"next-129", nextText(129), new(false), nil},
		{"next-100000", nextText(100000), new(false), nil},
		{"kids-128", kidsText(128), nil, nil},
		{"kids-129", kidsText(129), nil, nil},
		{"kids-100000", kidsText(100000), nil, nil},
	} {
		goRead, goWrite, got := readText(c.name, c.text)
		if c.want != nil && goRead != *c.want {
			t.Fatalf("%s: Go reader verdict is %v, want %v", c.name, goRead, *c.want)
		}
		javaRead := got["read"] == "1"
		if javaRead != goRead {
			t.Fatalf("%s: Java fromJson verdict is %v, Go reader is %v", c.name, javaRead, goRead)
		}
		if c.write != nil && goWrite != *c.write {
			t.Fatalf("%s: Go writer verdict on the value read is %v, want %v", c.name, goWrite, *c.write)
		}
		if javaWrite := got["write"] == "1"; goRead && javaWrite != goWrite {
			t.Fatalf("%s: Java toJson verdict on the value read is %v, Go writer is %v", c.name, javaWrite, goWrite)
		}
	}

	// W2: the writer carries the same cap in the same unit. A chain of 127
	// nodes under the root writes and reads; 128, whose last `kids` array sits
	// at depth 129, does neither. Build both in Go and Java.
	goWrites := func(n int) bool {
		rootInst := m.New(node)
		cur := rootInst
		for range n {
			child := m.New(node)
			var next *tabletext.Field
			for j := range cur.Fields {
				if cur.Fields[j].Def.Name == "next" {
					next = &cur.Fields[j]
					break
				}
			}
			if next == nil {
				t.Fatal("Node has no next field")
			}
			next.Cell.Node = child
			cur = child
		}
		_, err := m.Write(rootInst)
		return err == nil
	}
	for _, n := range []int{127, 128} {
		want := n == 127
		if got := goWrites(n); got != want {
			t.Fatalf("Go writer chain of %d nodes: write=%v, want %v", n, got, want)
		}
		got := probeVerdicts(t, runProbe("build", strconv.Itoa(n)))
		if got["write"] != boolDigit(want) {
			t.Fatalf("Java writer chain of %d nodes: write=%s, want %s", n, got["write"], boolDigit(want))
		}
		if want && got["read"] != "1" {
			t.Fatalf("Java writer chain of %d nodes wrote a text its own fromJson refuses", n)
		}
	}
}

func boolDigit(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
