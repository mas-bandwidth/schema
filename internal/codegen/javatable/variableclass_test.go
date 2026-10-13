package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestVariableClassBeyondCardConstructsJava is the variable-class half of this
// card: the Java variable form (docs/SPEC-TABLES.md §3, form byte 1) carries
// the constructs beyond the card record — *bytes blobs (§2.5), unbounded
// arrays of scalars, strings and enums (§2.9), arrays of pointers and a
// pointer to a table outside a list element (§2.1, §2.9), the 64-bit id-table
// escape kind (§3) and the rest of the scalar kinds (§3) — byte for byte
// against the committed reference bytes. Each subtest generates the unit,
// compiles its variable codecs beside the runtime, and holds a load/save and
// toJson round trip of one committed instance to those bytes.
func TestVariableClassBeyondCardConstructsJava(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		schemaDir string // relative to the repo root, like tables/blobs
		pkg       string
		root      string // the table whose <Root>Variable.java is the codec
		wire      string // committed reference wire, relative to repo root
		json      string // committed reference text, relative to repo root
		wantFiles []string
	}{
		{
			name:      "blobs",
			schemaDir: filepath.Join("..", "..", "..", "tables", "blobs"),
			pkg:       "blobdemo",
			root:      "Catalog",
			wire:      filepath.Join("..", "..", "..", "testdata", "wire", "tables", "blob_small.bin"),
			json:      filepath.Join("..", "..", "..", "testdata", "conformance", "tables", "json", "blob_small.json"),
			wantFiles: []string{"AssetVariable.java", "CatalogVariable.java"},
		},
		{
			name:      "lists",
			schemaDir: filepath.Join("..", "..", "..", "tables", "lists"),
			pkg:       "listdemo",
			root:      "Save",
			wire:      filepath.Join("..", "..", "..", "testdata", "wire", "tables", "list_scalars.bin"),
			json:      filepath.Join("..", "..", "..", "testdata", "conformance", "tables", "json", "list_scalars.json"),
			wantFiles: []string{"SaveVariable.java", "MixedVariable.java"},
		},
		{
			name:      "pointers",
			schemaDir: filepath.Join("..", "..", "..", "tables", "pointers"),
			pkg:       "graphdemo",
			root:      "Scene",
			wire:      filepath.Join("..", "..", "..", "testdata", "wire", "tables", "graph_tree.bin"),
			json:      filepath.Join("..", "..", "..", "testdata", "conformance", "tables", "json", "graph_tree.json"),
			wantFiles: []string{"ListNodeVariable.java", "TreeNodeVariable.java", "LayerVariable.java", "SceneVariable.java", "AlbumVariable.java"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			javac, javaBin := javaTools(t)
			files, err := Generate(unitFromDir(t, tc.schemaDir))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.wantFiles {
				if _, ok := files[want]; !ok {
					t.Fatalf("%s not emitted for %s: the variable class beyond the card constructs is not carried", want, tc.schemaDir)
				}
			}
			roundTripVariable(t, javac, javaBin, files, tc.pkg, tc.root, tc.wire, tc.json)
		})
	}
}

// unitFromDir parses and checks every *.schema in dir into one unit, the way
// `schema generate` reads a directory (docs/SPEC-TABLES.md §2: a unit is a
// package, which may span files).
func unitFromDir(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var sources []check.SourceFile
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f, perrs := parser.Parse(p, b)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", p, perrs[0])
		}
		sources = append(sources, check.SourceFile{
			Path: p, Name: e.Name(), Base: strings.TrimSuffix(e.Name(), ".schema"), Bytes: b, AST: f,
		})
	}
	u, cerrs := check.Unit(sources)
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", dir, cerrs[0])
	}
	return u
}

// roundTripVariable compiles the generated Java beside one driver, then holds
// the load/save and toJson round trips to the committed reference bytes.
func roundTripVariable(t *testing.T, javac, javaBin string, files map[string][]byte, pkg, root, wirePath, jsonPath string) {
	t.Helper()
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	srcdir := filepath.Join(dir, "src", pkg)
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(srcdir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if strings.HasSuffix(name, ".java") {
			if err := os.WriteFile(filepath.Join(srcdir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	wire, err := os.ReadFile(wirePath)
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instance.wire"), wire, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instance.json"), golden, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Driver.java"), []byte(variableRoundTripDriver(pkg, root)), 0o644); err != nil {
		t.Fatal(err)
	}
	srcs, err := filepath.Glob(filepath.Join(srcdir, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, srcs...)
	args = append(args, filepath.Join(dir, "Driver.java"))
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Driver",
		filepath.Join(dir, "instance.wire"), filepath.Join(dir, "instance.json")).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("driver: %s", out)
	}
}

// variableRoundTripDriver loads a committed wire, re-saves it byte for byte,
// and writes its JSON byte for byte against the committed text.
func variableRoundTripDriver(pkg, root string) string {
	return `import java.nio.file.Files;
import java.nio.file.Paths;
import java.nio.charset.StandardCharsets;

public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        byte[] golden = Files.readAllBytes(Paths.get(args[1]));

        ` + pkg + `.TableVariable.Report report = new ` + pkg + `.TableVariable.Report();
        ` + pkg + `.` + root + `Variable.Value v = ` + pkg + `.` + root + `Variable.load(wire, report);
        if (v == null) { fail("load returned null"); }
        if (report.malformed || report.refused) { fail("load reported malformed/refused"); }

        byte[] saved = ` + pkg + `.` + root + `Variable.save(v);
        if (!java.util.Arrays.equals(wire, saved)) {
            int d = 0;
            while (d < wire.length && d < saved.length && wire[d] == saved[d]) { d++; }
            fail("re-save differs at byte " + d + " (wire " + wire.length + " saved " + saved.length + ")");
        }

        byte[] text = ` + pkg + `.` + root + `Variable.toJson(v);
        if (!java.util.Arrays.equals(golden, text)) {
            int d = 0;
            while (d < golden.length && d < text.length && golden[d] == text[d]) { d++; }
            fail("toJson differs from the Go leg at byte " + d);
        }

        ` + pkg + `.TableVariable.Report read = new ` + pkg + `.TableVariable.Report();
        ` + pkg + `.` + root + `Variable.Value from = ` + pkg + `.` + root + `Variable.fromJson(golden, read);
        if (read.malformed || read.unknown != 0 || read.kindMismatch != 0 || read.clamped != 0) {
            fail("fromJson reports loss");
        }
        byte[] round = ` + pkg + `.` + root + `Variable.save(from);
        if (!java.util.Arrays.equals(wire, round)) {
            fail("fromJson then save differs from the wire");
        }

        System.out.println("ok");
    }
}
`
}
