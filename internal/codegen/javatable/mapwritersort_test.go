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

// THE WRITER HOLDS A MAP'S SORT (docs/PORTING.md M19; docs/SPEC-TABLES.md
// §2.8): Save and ToJson each derive ascending key order from the entries the
// caller built, whatever order those are in. A round trip of a loaded value
// cannot see the sort, because a load hands back entries already ascending, so
// this test BUILDS the value in Java with its entries out of order (a string
// key, and an unsigned 64-bit key whose largest value is negative as a Java
// long) and holds Save and ToJson to the engine's bytes for the same value.
// The reversed witness hands back the caller's order unsorted and must go red.
func TestJavaMapWriterHoldsTheSort(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaTools(t)
	dir := t.TempDir()
	schema := "package probe\ntable Root {\n names map[string(16)]int32\n ids map[uint64]int32\n tail int32\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "Root.schema"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := unitFromDir(t, dir)
	var names, ids string
	for _, f := range u.Tables["Root"].Fields {
		switch f.Name {
		case "names":
			names = f.MapEntry.Name
		case "ids":
			ids = f.MapEntry.Name
		}
	}
	if names == "" || ids == "" {
		t.Fatal("the probe unit names no map entry tables")
	}

	// the engine's bytes and text for the value, which it writes ascending
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	report := new(tabletext.Report)
	if !model.Read(value, []byte(`{"names":{"b":2,"a":1,"ab":3},"ids":{"5":50,"1":10,"18446744073709551615":99,"3":30},"tail":9}`), report) {
		t.Fatalf("reference: %+v", report)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	text, err := model.Write(value)
	if err != nil {
		t.Fatal(err)
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	driver := strings.NewReplacer("NAMES", names, "IDS", ids).Replace(mapSortDriver)

	t.Run("carried", func(t *testing.T) {
		t.Parallel()
		if out, ok := runMapSortDriver(t, javac, javaBin, files, driver, wire, text); !ok {
			t.Fatalf("the Java map writer did not hold the sort:\n%s", out)
		}
	})
	t.Run("witness_unsorted", func(t *testing.T) {
		t.Parallel()
		const from = "return new ArrayList<>(ordered.values());"
		planted := map[string][]byte{}
		hits := 0
		for name, data := range files {
			hits += strings.Count(string(data), from)
			planted[name] = []byte(strings.ReplaceAll(string(data), from, "return entries;"))
		}
		if hits != 2 {
			t.Fatalf("the unsorted witness planted %d sites, want the two maps' order_", hits)
		}
		out, ok := runMapSortDriver(t, javac, javaBin, planted, driver, wire, text)
		if ok {
			t.Fatal("a writer that keeps the caller's order left the test green: it cannot see the sort")
		}
		if !strings.Contains(out, "FAILED: save") {
			t.Fatalf("the unsorted witness failed for another reason:\n%s", out)
		}
	})
}

func runMapSortDriver(t *testing.T, javac, javaBin string, files map[string][]byte, driver string, wire, text []byte) (string, bool) {
	t.Helper()
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	srcdir := filepath.Join(dir, "src", "probe")
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
	for name, data := range map[string][]byte{"want.wire": wire, "want.json": text, "Driver.java": []byte(driver)} {
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
		filepath.Join(dir, "want.wire"), filepath.Join(dir, "want.json")).CombinedOutput()
	return string(out), err == nil && strings.Contains(string(out), "ok")
}

const mapSortDriver = `import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.Arrays;
import probe.IDSVariable;
import probe.NAMESVariable;
import probe.RootVariable;

public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    static NAMESVariable.Value name(String key, int value) {
        NAMESVariable.Value e = new NAMESVariable.Value();
        e.key = key;
        e.value = value;
        return e;
    }

    static IDSVariable.Value id(long key, int value) {
        IDSVariable.Value e = new IDSVariable.Value();
        e.key = key;
        e.value = value;
        return e;
    }

    static RootVariable.Value build() {
        RootVariable.Value v = new RootVariable.Value();
        v.names.add(name("b", 2));
        v.names.add(name("ab", 3));
        v.names.add(name("a", 1));
        v.ids.add(id(5L, 50));
        v.ids.add(id(-1L, 99)); // 18446744073709551615, the largest uint64
        v.ids.add(id(1L, 10));
        v.ids.add(id(3L, 30));
        v.tail = 9;
        return v;
    }

    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        String text = new String(Files.readAllBytes(Paths.get(args[1])), StandardCharsets.UTF_8).strip();

        // save first, then toJson on a value built afresh: the graph pass
        // stores the ordered list back into the value it numbers
        byte[] saved = RootVariable.save(build());
        if (saved == null || !Arrays.equals(wire, saved)) {
            fail("save does not write the entries ascending (" + wire.length + " vs " + (saved == null ? -1 : saved.length) + ")");
        }
        String json = new String(RootVariable.toJson(build()), StandardCharsets.UTF_8).strip();
        if (!json.equals(text)) { fail("toJson does not write the entries ascending:\n" + json); }
        System.out.println("ok");
    }
}
`
