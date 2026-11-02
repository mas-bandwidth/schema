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
// by one slot. The tolerant load of testdata/wire/tables/blob_shared.bin and
// testdata/wire/tables/blob_shared_string.bin keeps the value in both slots
// and marks the second slot as shared (cleared by Descriptor.reset()), so a
// later save, saveRetain, measureRetain and toJson of that loaded value
// refuse; a fresh value whose two *bytes slots hold one byte[] is refused the
// same way, while two equal but distinct byte[] arrays are two records and
// save. On *string slots, caller-built equal strings — including one interned
// literal in two *string fields, union arms or map values — number each slot
// by value as its own blob record on save, saveRetain, measureRetain, toJson
// and Cook (docs/USAGE.md).
func TestJavaWritersRefuseASharedBlob(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	build := t.TempDir()
	var sources []string
	for _, dir := range []string{"blobs", "maps"} {
		u := unitFromDir(t, filepath.Join(repo, "tables", dir))
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
		sub := filepath.Join(build, dir)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			path := filepath.Join(sub, name)
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, path)
		}
	}
	driver := `import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.Arrays;

public final class BlobSharedDriver {
    private BlobSharedDriver() {}

    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        byte[] swire = Files.readAllBytes(Paths.get(args[1]));
        blobdemo.TableVariable.Report report = new blobdemo.TableVariable.Report();
        blobdemo.CatalogVariable.Value loaded = blobdemo.CatalogVariable.load(wire, report);
        System.out.println("loaded " + (loaded != null) + " one-node " + (loaded != null && loaded.thumb == loaded.alias));
        System.out.println("loaded-save " + (blobdemo.CatalogVariable.save(loaded) == null ? "refused" : "written"));
        System.out.println("loaded-json " + (blobdemo.CatalogVariable.toJson(loaded) == null ? "refused" : "written"));

        blobdemo.TableRetain retain = new blobdemo.TableRetain(1 << 20, 1024);
        blobdemo.TableVariable.Report rreport = new blobdemo.TableVariable.Report();
        blobdemo.CatalogVariable.Value rloaded = blobdemo.CatalogRetain.loadRetain(wire, retain, rreport);
        System.out.println("loaded-retain-save " + (blobdemo.CatalogRetain.saveRetain(rloaded, retain, rreport) == null ? "refused" : "written"));
        System.out.println("loaded-retain-measure " + (blobdemo.CatalogRetain.measureRetain(rloaded, retain) < 0 ? "refused" : "written"));

        blobdemo.CatalogDescriptor.tableType().reset.accept(loaded);
        System.out.println("reset-bytes-save " + (blobdemo.CatalogVariable.save(loaded) == null ? "refused" : "written"));

        mapdemo.TableVariable.Report sreport = new mapdemo.TableVariable.Report();
        mapdemo.DocsVariable.Value sloaded = mapdemo.DocsVariable.load(swire, sreport);
        System.out.println("str-loaded " + (sloaded != null)
                + " save " + (mapdemo.DocsVariable.save(sloaded) == null ? "refused" : "written")
                + " json " + (mapdemo.DocsVariable.toJson(sloaded) == null ? "refused" : "written"));
        mapdemo.TableRetain sretain = new mapdemo.TableRetain(1 << 20, 1024);
        mapdemo.DocsVariable.Value srloaded = mapdemo.DocsRetain.loadRetain(swire, sretain, sreport);
        System.out.println("str-retain-save " + (mapdemo.DocsRetain.saveRetain(srloaded, sretain, sreport) == null ? "refused" : "written")
                + " measure " + (mapdemo.DocsRetain.measureRetain(srloaded, sretain) < 0 ? "refused" : "written"));
        long scookLen = mapdemo.DocsCook.cookMeasure(sloaded);
        System.out.println("str-loaded-cook " + (scookLen > 0 ? "packed" : "refused"));
        mapdemo.DocsDescriptor.tableType().reset.accept(sloaded);
        System.out.println("str-reset-save " + (mapdemo.DocsVariable.save(sloaded) == null ? "refused" : "written"));

        blobdemo.CatalogVariable.Value fresh = new blobdemo.CatalogVariable.Value();
        fresh.thumb = new byte[] {1, 2, 3};
        fresh.alias = fresh.thumb;
        System.out.println("fresh-save " + (blobdemo.CatalogVariable.save(fresh) == null ? "refused" : "written"));
        fresh.alias = new byte[] {1, 2, 3};
        System.out.println("equal-save " + (blobdemo.CatalogVariable.save(fresh) == null ? "refused" : "written"));

        // Caller-built equal Strings — including one interned literal in two
        // *string slots — number each slot by value as its own blob record.
        blobdemo.CatalogVariable.Value words = new blobdemo.CatalogVariable.Value();
        words.note = "the same literal";
        words.head = new blobdemo.AssetVariable.Value();
        words.head.caption = "the same literal";
        byte[] literalWire = blobdemo.CatalogVariable.save(words);
        System.out.println("literal-save " + (literalWire == null ? "refused" : "written"));
        blobdemo.TableVariable.Report lrep = new blobdemo.TableVariable.Report();
        byte[] literalRetainWire = blobdemo.CatalogRetain.saveRetain(words, new blobdemo.TableRetain(1 << 20, 1024), lrep);
        byte[] literalJson = blobdemo.CatalogVariable.toJson(words);
        long literalCookLen = blobdemo.CatalogCook.cookMeasure(words);
        byte[] literalCook = literalCookLen > 0 ? new byte[(int) literalCookLen] : null;
        if (literalCook != null) {
            blobdemo.CatalogCook.cookFrom(words, literalCook, blobdemo.TableCookWrite.LITTLE);
        }

        words.head.caption = new String("the same literal");
        byte[] distinctWire = blobdemo.CatalogVariable.save(words);
        System.out.println("distinct-save " + (distinctWire == null ? "refused" : "written"));
        long distinctCookLen = blobdemo.CatalogCook.cookMeasure(words);
        byte[] distinctCook = distinctCookLen > 0 ? new byte[(int) distinctCookLen] : null;
        if (distinctCook != null) {
            blobdemo.CatalogCook.cookFrom(words, distinctCook, blobdemo.TableCookWrite.LITTLE);
        }
        blobdemo.TableVariable.Measure m = new blobdemo.TableVariable.Measure();
        if (literalWire != null) {
            blobdemo.CatalogVariable.loadMeasure(literalWire, m);
        }
        System.out.println("literal-two-nodes " + (literalWire != null
                && Arrays.equals(literalWire, distinctWire)
                && Arrays.equals(literalRetainWire, distinctWire)
                && literalJson != null
                && literalCook != null
                && Arrays.equals(literalCook, distinctCook)
                && m.nodeRecords == 3));

        // Container (*string in map entries) with the same interned literal:
        mapdemo.DocsVariable.Value docs = new mapdemo.DocsVariable.Value();
        mapdemo.DocsPagesEntryVariable.Value e1 = new mapdemo.DocsPagesEntryVariable.Value();
        e1.key = "a";
        e1.value = "the same literal";
        mapdemo.DocsPagesEntryVariable.Value e2 = new mapdemo.DocsPagesEntryVariable.Value();
        e2.key = "b";
        e2.value = "the same literal";
        docs.pages.add(e1);
        docs.pages.add(e2);
        byte[] docsWire = mapdemo.DocsVariable.save(docs);
        mapdemo.TableVariable.Measure dm = new mapdemo.TableVariable.Measure();
        if (docsWire != null) {
            mapdemo.DocsVariable.loadMeasure(docsWire, dm);
        }
        long docsCookLen = mapdemo.DocsCook.cookMeasure(docs);
        System.out.println("map-literal-two-nodes " + (docsWire != null && dm.nodeRecords == 2 && docsCookLen > scookLen));
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
	swire, err := filepath.Abs(filepath.Join(repo, "testdata", "wire", "tables", "blob_shared_string.bin"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "BlobSharedDriver", wire, swire).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	want := strings.Join([]string{
		"loaded true one-node true",
		"loaded-save refused",
		"loaded-json refused",
		"loaded-retain-save refused",
		"loaded-retain-measure refused",
		"reset-bytes-save written",
		"str-loaded true save refused json refused",
		"str-retain-save refused measure refused",
		"str-loaded-cook packed",
		"str-reset-save written",
		"fresh-save refused",
		"equal-save written",
		"literal-save written",
		"distinct-save written",
		"literal-two-nodes true",
		"map-literal-two-nodes true",
	}, "\n")
	if got != want {
		t.Fatalf("the Java writers on a shared blob (§2.5):\n%s\nwant:\n%s", got, want)
	}
}
