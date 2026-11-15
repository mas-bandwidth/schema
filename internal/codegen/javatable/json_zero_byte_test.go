package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const javaJsonZeroByteSchema = `package probe

table Root {
    name   string(8)
    lines  []string
    labels map[string]string(8)
}
`

const javaJsonZeroByteProbe = `package probe;

import java.nio.charset.StandardCharsets;

public final class ZeroByteProbe {
    private ZeroByteProbe() {}

    static RootVariable.Value value(int site, boolean zero) {
        RootVariable.Value v = new RootVariable.Value();
        v.name = site == 0 && zero ? String.valueOf(new char[]{'a', 0, 'b'}) : "plain";
        RootLinesTextVariable.Value line = new RootLinesTextVariable.Value();
        line.value = site == 1 && zero ? String.valueOf(new char[]{'a', 0, 'b'}) : "line";
        v.lines.add(line);
        RootLabelsEntryVariable.Value entry = new RootLabelsEntryVariable.Value();
        entry.key = site == 2 && zero ? String.valueOf(new char[]{'a', 0, 'b'}) : "key";
        entry.value = "value";
        v.labels.add(entry);
        return v;
    }

    static void fail(String message) {
        throw new AssertionError(message);
    }

    static void expectAssert(String name, int site) {
        try {
            RootVariable.toJson(value(site, true));
        } catch (AssertionError expected) {
            return;
        }
        fail("toJson did not assert for zero byte in " + name);
    }

    public static void main(String[] args) {
        if (args[0].equals("assert")) {
            expectAssert("string field", 0);
            expectAssert("text-list element", 1);
            expectAssert("map key", 2);
            System.out.println("probe: JSON writer asserted for all zero-byte text sites");
            return;
        }
        if (args[0].equals("lawful")) {
            byte[] lawful = RootVariable.toJson(value(-1, false));
            String text = new String(lawful, StandardCharsets.UTF_8);
            if (!text.equals("{\n  \"name\": \"plain\",\n  \"lines\": [\n    \"line\"\n  ],\n  \"labels\": {\n    \"key\": \"value\"\n  }\n}\n")) {
                fail("lawful JSON changed: " + text);
            }
            System.out.println("probe: lawful output stayed unchanged");
            return;
        }
        fail("unknown mode " + args[0]);
    }
}
`

// TestJavaJsonWriterRefusesZeroByte holds Java's debug-only text-write check
// to the no-zero-byte rule at string values, text-list elements, and map keys.
func TestJavaJsonWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	javac, javaBin := javaToolsRequired(t)

	files, err := Generate(unitFrom(t, javaJsonZeroByteSchema))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	srcdir := filepath.Join(dir, "src", "probe")
	classes := filepath.Join(dir, "classes")
	for _, path := range []string{srcdir, classes} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		path := filepath.Join(srcdir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	probe := filepath.Join(srcdir, "ZeroByteProbe.java")
	if err := os.WriteFile(probe, []byte(javaJsonZeroByteProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, probe)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	run := func(assertions bool, mode string) ([]byte, error) {
		args := []string{"-cp", classes}
		if assertions {
			args = append(args, "-ea")
		}
		args = append(args, "probe.ZeroByteProbe", mode)
		return exec.Command(javaBin, args...).CombinedOutput()
	}
	if out, err := run(true, "assert"); err != nil {
		t.Fatalf("debug writer did not assert: %v\n%s", err, out)
	}
	if out, err := run(false, "lawful"); err != nil {
		t.Fatalf("lawful output: %v\n%s", err, out)
	}
}
