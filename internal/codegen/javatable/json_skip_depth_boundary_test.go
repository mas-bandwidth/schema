package javatable

// A skipped JSON value nests at most as deep as a placed value
// (docs/SPEC-TABLES.md §16.2). The cap is 128 containers, root object
// included in the count the Go reader uses: 128 under an unknown key are
// read and 129 refuse, for arrays and for objects. The verdict is the
// contract; the refusal's reason is not.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const javaSkipDepthSchema = `package probe
table Root {
 value int32 = 1
}
`

const javaSkipDepthProbe = `package probe;
import java.nio.file.Files;
import java.nio.file.Path;

public final class SkipDepthProbe {
    private SkipDepthProbe() {}

    public static void main(String[] args) throws Exception {
        for (String path : args) {
            byte[] text = Files.readAllBytes(Path.of(path));
            boolean threw = false;
            boolean read = false;
            try {
                TableVariable.Report report = new TableVariable.Report();
                RootVariable.Value v = RootVariable.fromJson(text, report);
                read = v != null && !report.damaged && !report.refused;
            } catch (Throwable e) {
                threw = true;
            }
            System.out.println("read=" + (read ? 1 : 0) + " threw=" + (threw ? 1 : 0));
        }
    }
}
`

// nestedContainers is n containers around a leaf. n counts only those
// containers, not the root object the caller wraps them in.
func nestedContainers(n int, open, close, leaf string) string {
	var b strings.Builder
	for range n {
		b.WriteString(open)
	}
	b.WriteString(leaf)
	for range n {
		b.WriteString(close)
	}
	return b.String()
}

// underUnknownKey is a root object whose one key names nothing the reader
// places. The root is depth 0; the value is the next container.
func underUnknownKey(value string) string {
	return `{"extra":` + value + `}`
}

func TestJavaJsonSkippedValueDepthBoundary(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it builds and runs the generated Java)")
	javac, javaBin := javaToolsRequired(t)

	u := unitFrom(t, javaSkipDepthSchema)
	m := tabletext.NewModel(u)
	root := m.Lookup("Root")
	if root == nil {
		t.Fatal("missing Root")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	dir := t.TempDir()
	var sources []string
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, src, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".java") {
			sources = append(sources, p)
		}
	}
	probe := filepath.Join(dir, "SkipDepthProbe.java")
	if err := os.WriteFile(probe, []byte(javaSkipDepthProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, probe)
	args := append([]string{"--release", "17", "-d", dir}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	// Depth is the engine's: the root object counts, and so does every
	// array or object under the unknown key. 128 of those are read, 129
	// refuse. One object holding 127 arrays is 128 containers under the
	// key, which is still read.
	cases := []struct {
		name string
		text string
		read bool
	}{
		{"arrays-128", underUnknownKey(nestedContainers(128, "[", "]", "null")), true},
		{"arrays-129", underUnknownKey(nestedContainers(129, "[", "]", "null")), false},
		{"objects-128", underUnknownKey(nestedContainers(128, `{"z":`, "}", "null")), true},
		{"objects-129", underUnknownKey(nestedContainers(129, `{"z":`, "}", "null")), false},
		{"object-then-127-arrays", underUnknownKey(`{"z":` + nestedContainers(127, "[", "]", "null") + `}`), true},
	}

	var caseFiles []string
	goRead := make([]bool, len(cases))
	for i, c := range cases {
		p := filepath.Join(dir, c.name+".json")
		if err := os.WriteFile(p, []byte(c.text), 0o644); err != nil {
			t.Fatal(err)
		}
		caseFiles = append(caseFiles, p)
		inst := m.New(root)
		var report tabletext.Report
		goRead[i] = m.Read(inst, []byte(c.text), &report)
		if goRead[i] != c.read {
			t.Fatalf("%s: Go reader verdict is %v, want %v", c.name, goRead[i], c.read)
		}
	}

	runArgs := append([]string{"-ea", "-cp", dir, "probe.SkipDepthProbe"}, caseFiles...)
	out, err := exec.Command(javaBin, runArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		fields := map[string]string{}
		for field := range strings.FieldsSeq(lines[i]) {
			k, v, ok := strings.Cut(field, "=")
			if ok {
				fields[k] = v
			}
		}
		if fields["threw"] != "0" {
			t.Fatalf("%s: Java text read threw: %s", c.name, lines[i])
		}
		javaRead := fields["read"] == "1"
		if javaRead != goRead[i] {
			t.Errorf("%s: Java read=%v, Go read=%v", c.name, javaRead, goRead[i])
		}
	}
}
