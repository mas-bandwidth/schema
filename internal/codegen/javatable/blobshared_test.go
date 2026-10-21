package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJavaWritersRefuseASharedBlob holds §2.5 on the Java leg: a blob is owned
// by one slot. The tolerant load of testdata/wire/tables/blob_shared.bin keeps
// the one node for both slots, and a later save, measure-by-save and toJson of
// that value refuse; a fresh value whose two slots hold one byte[] is refused
// the same way, while two equal but distinct arrays are two records and save.
// The same holds for a String: one interned literal in two *string slots is
// one object and is refused, while distinct String objects of equal contents
// save (the documented Java behaviour, docs/USAGE.md).
func TestJavaWritersRefuseASharedBlob(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, "tables", "blobs"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range packet {
		if _, ok := files[name]; !ok && strings.HasSuffix(name, ".java") {
			files[name] = data
		}
	}
	pkg := javaPackageOf(u)
	build := t.TempDir()
	var sources []string
	for name, data := range files {
		path := filepath.Join(build, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	driver := `import java.nio.file.Files;
import java.nio.file.Paths;

public final class BlobSharedDriver {
    private BlobSharedDriver() {}

    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        ` + pkg + `.TableVariable.Report report = new ` + pkg + `.TableVariable.Report();
        ` + pkg + `.CatalogVariable.Value loaded = ` + pkg + `.CatalogVariable.load(wire, report);
        System.out.println("loaded " + (loaded != null) + " one-node " + (loaded != null && loaded.thumb == loaded.alias));
        System.out.println("loaded-save " + (` + pkg + `.CatalogVariable.save(loaded) == null ? "refused" : "written"));
        System.out.println("loaded-json " + (` + pkg + `.CatalogVariable.toJson(loaded) == null ? "refused" : "written"));
        ` + pkg + `.CatalogVariable.Value fresh = new ` + pkg + `.CatalogVariable.Value();
        fresh.thumb = new byte[] {1, 2, 3};
        fresh.alias = fresh.thumb;
        System.out.println("fresh-save " + (` + pkg + `.CatalogVariable.save(fresh) == null ? "refused" : "written"));
        fresh.alias = new byte[] {1, 2, 3};
        System.out.println("equal-save " + (` + pkg + `.CatalogVariable.save(fresh) == null ? "refused" : "written"));
        // DELIBERATE (docs/USAGE.md, Java): a blob is one OBJECT, and Java
        // interns literals, so one literal in two *string slots is one String
        // and is refused as blob_shared; distinct String objects of equal
        // contents are two records and save.
        ` + pkg + `.CatalogVariable.Value words = new ` + pkg + `.CatalogVariable.Value();
        words.note = "the same literal";
        words.head = new ` + pkg + `.AssetVariable.Value();
        words.head.caption = "the same literal";
        System.out.println("literal-save " + (` + pkg + `.CatalogVariable.save(words) == null ? "refused" : "written"));
        words.head.caption = new String("the same literal");
        System.out.println("distinct-save " + (` + pkg + `.CatalogVariable.save(words) == null ? "refused" : "written"));
    }
}
`
	driverPath := filepath.Join(build, "BlobSharedDriver.java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driverPath)
	classes := filepath.Join(build, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	javac, javaBin := javaTools(t)
	if out, err := exec.Command(javac, append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	wire, err := filepath.Abs(filepath.Join(repo, "testdata", "wire", "tables", "blob_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "BlobSharedDriver", wire).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	want := "loaded true one-node true\nloaded-save refused\nloaded-json refused\nfresh-save refused\nequal-save written\nliteral-save refused\ndistinct-save written"
	if got != want {
		t.Fatalf("the Java writers on a shared blob (§2.5):\n%s\nwant:\n%s", got, want)
	}
}
