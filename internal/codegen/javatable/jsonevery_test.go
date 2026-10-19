package javatable

// The text form for EVERY table (docs/SPEC-TABLES.md §16): the Go oracle
// (internal/tabletext) writes the canonical text of the all-default instance
// of every table a unit holds, the Java codec reads that text with fromJson
// and writes it back with toJson, and the two texts are byte-compared. A
// table absent from the emitted variable form, or a field the Java reader
// loses, is red here.

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const jsonEveryJavaProbe = `import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

public final class JsonEveryProbe {
    private JsonEveryProbe() {}

    public static void main(String[] args) throws Exception {
        String pkg = args[0];
        for (int i = 1; i + 2 < args.length; i += 3) {
            String table = args[i];
            byte[] text = Files.readAllBytes(Paths.get(args[i + 1]));
            Class<?> codec = Class.forName(pkg + "." + table + "Variable");
            Class<?> reportType = Class.forName(pkg + ".TableVariable$Report");
            Class<?> valueType = Class.forName(pkg + "." + table + "Variable$Value");
            Object report = reportType.getConstructor().newInstance();
            Object value = codec.getMethod("fromJson", byte[].class, reportType).invoke(null, text, report);
            boolean malformed = false;
            try { malformed = reportType.getField("malformed").getBoolean(report); } catch (NoSuchFieldException ignored) {}
            boolean refused = false;
            try { refused = reportType.getField("refused").getBoolean(report); } catch (NoSuchFieldException ignored) {}
            if (value == null || malformed || refused) {
                System.err.println(table + ": fromJson refused the oracle text");
                System.exit(1);
            }
            byte[] out = (byte[]) codec.getMethod("toJson", valueType).invoke(null, value);
            if (out == null) {
                System.err.println(table + ": toJson refused the value");
                System.exit(1);
            }
            Files.write(Path.of(args[i + 2]), out);
        }
    }
}
`

func TestTableJsonEveryTableJava(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)
	for _, dir := range []string{"examples", filepath.Join("tables", "arms"), filepath.Join("tables", "maps")} {
		dir := dir
		t.Run(strings.ReplaceAll(dir, string(os.PathSeparator), "-"), func(t *testing.T) {
			t.Parallel()
			u := unitFromDir(t, filepath.Join("..", "..", "..", dir))
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("generate %s: %v", dir, err)
			}
			root := t.TempDir()
			srcdir := filepath.Join(root, "src", u.Package)
			classes := filepath.Join(root, "classes")
			inst := filepath.Join(root, "inst")
			for _, d := range []string{srcdir, classes, inst} {
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
			probe := filepath.Join(root, "JsonEveryProbe.java")
			if err := os.WriteFile(probe, []byte(jsonEveryJavaProbe), 0o644); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--release", "17", "-d", classes}, srcs...)
			args = append(args, probe)
			if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
				t.Fatalf("javac %s: %v\n%s", dir, err, out)
			}

			model := tabletext.NewModel(u)
			run := []string{u.Package}
			for _, name := range sortedTableNames(u) {
				st := u.Tables[name]
				if _, ok := files[name+"Variable.java"]; !ok {
					t.Fatalf("%s: table %s has no Java text form (%sVariable.java)", dir, name, name)
				}
				value := model.New(st)
				model.Refill(value)
				text, err := model.Write(value)
				if err != nil {
					t.Fatalf("%s: oracle writes %s: %v", dir, name, err)
				}
				in := filepath.Join(inst, name+".json")
				if err := os.WriteFile(in, text, 0o644); err != nil {
					t.Fatal(err)
				}
				run = append(run, name, in, filepath.Join(root, name+".out"))
			}
			out, err := exec.Command(javaBin, append([]string{"-ea", "-cp", classes, "JsonEveryProbe"}, run...)...).CombinedOutput()
			if err != nil {
				t.Fatalf("java %s: %v\n%s", dir, err, out)
			}
			for _, name := range sortedTableNames(u) {
				st := u.Tables[name]
				value := model.New(st)
				model.Refill(value)
				want, err := model.Write(value)
				if err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(filepath.Join(root, name+".out"))
				if err != nil {
					t.Fatal(err)
				}
				// toJson closes the document with the newline the canonical
				// file form writes (§16.1); model.Write returns the document.
				if string(got) != string(want)+"\n" {
					t.Fatalf("%s: Java text for %s differs from the Go oracle\n got: %q\nwant: %q", dir, name, got, want)
				}
			}
		})
	}
}

func sortedTableNames(u *ir.Unit) []string {
	names := make([]string, 0, len(u.Tables))
	for name, st := range u.Tables {
		if st.IsMapEntry() {
			// A map entry is a codec body reached through its holder, never a
			// root with a public text form (docs/SPEC-TABLES.md §2.8).
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
