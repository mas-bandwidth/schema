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
// by one slot. The strict load of testdata/wire/tables/blob_shared.bin and
// testdata/wire/tables/blob_shared_string.bin refuses the whole load, on load
// and loadRetain (docs/plans/strict-only.md); a fresh value whose two *bytes
// slots hold one byte[] is refused by save, saveRetain, measureRetain and
// toJson and saves after Descriptor.reset(), while two equal but distinct byte[] arrays are two records and
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
        // §2.5 on the read: a blob owned by two slots refuses the whole load
        // (docs/plans/strict-only.md), on load and on loadRetain, for *bytes
        // and *string slots alike, and the retaining load keeps no tail.
        blobdemo.TableVariable.Report report = new blobdemo.TableVariable.Report();
        blobdemo.CatalogVariable.Value loaded = blobdemo.CatalogVariable.load(wire, report);
        System.out.println("loaded " + (loaded == null && report.refused ? "refused" : "read"));
        blobdemo.TableRetain retain = new blobdemo.TableRetain(1 << 20, 1024);
        blobdemo.TableVariable.Report rreport = new blobdemo.TableVariable.Report();
        blobdemo.CatalogVariable.Value rloaded = blobdemo.CatalogRetain.loadRetain(wire, retain, rreport);
        System.out.println("loaded-retain " + (rloaded == null && rreport.refused && retain.used == 0 ? "refused" : "read"));
        mapdemo.TableVariable.Report sreport = new mapdemo.TableVariable.Report();
        mapdemo.DocsVariable.Value sloaded = mapdemo.DocsVariable.load(swire, sreport);
        System.out.println("str-loaded " + (sloaded == null && sreport.refused ? "refused" : "read"));
        mapdemo.TableRetain sretain = new mapdemo.TableRetain(1 << 20, 1024);
        mapdemo.TableVariable.Report srreport = new mapdemo.TableVariable.Report();
        mapdemo.DocsVariable.Value srloaded = mapdemo.DocsRetain.loadRetain(swire, sretain, srreport);
        System.out.println("str-loaded-retain " + (srloaded == null && srreport.refused && sretain.used == 0 ? "refused" : "read"));

        // §2.5 on the write: a value whose two *bytes slots hold one byte[]
        // is refused by save, toJson, saveRetain and measureRetain; after
        // Descriptor.reset() it saves.
        blobdemo.CatalogVariable.Value fresh = new blobdemo.CatalogVariable.Value();
        fresh.thumb = new byte[] {1, 2, 3};
        fresh.alias = fresh.thumb;
        System.out.println("fresh-save " + (blobdemo.CatalogVariable.save(fresh) == null ? "refused" : "written"));
        System.out.println("fresh-json " + (blobdemo.CatalogVariable.toJson(fresh) == null ? "refused" : "written"));
        blobdemo.TableRetain fretain = new blobdemo.TableRetain(1 << 20, 1024);
        blobdemo.TableVariable.Report freport = new blobdemo.TableVariable.Report();
        System.out.println("fresh-retain-save " + (blobdemo.CatalogRetain.saveRetain(fresh, fretain, freport) == null ? "refused" : "written"));
        System.out.println("fresh-retain-measure " + (blobdemo.CatalogRetain.measureRetain(fresh, fretain) < 0 ? "refused" : "written"));
        blobdemo.CatalogDescriptor.tableType().reset.accept(fresh);
        System.out.println("reset-bytes-save " + (blobdemo.CatalogVariable.save(fresh) == null ? "refused" : "written"));
        fresh.thumb = new byte[] {1, 2, 3};
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
        // the same two keys with one *string node cook shorter: the literal
        // in two slots cooks as two nodes, never one shared record
        mapdemo.DocsVariable.Value oneNode = new mapdemo.DocsVariable.Value();
        mapdemo.DocsPagesEntryVariable.Value o1 = new mapdemo.DocsPagesEntryVariable.Value();
        o1.key = "a";
        o1.value = "the same literal";
        mapdemo.DocsPagesEntryVariable.Value o2 = new mapdemo.DocsPagesEntryVariable.Value();
        o2.key = "b";
        oneNode.pages.add(o1);
        oneNode.pages.add(o2);
        long oneNodeCookLen = mapdemo.DocsCook.cookMeasure(oneNode);
        System.out.println("map-literal-two-nodes " + (docsWire != null && dm.nodeRecords == 2 && oneNodeCookLen > 0 && docsCookLen > oneNodeCookLen));

        // Large populated map (2,000 entries) where every entry's *string slot
        // holds the same String literal: each slot gets its own cooked node via
        // O(1) Plan.slotNode lookup.
        int n500 = 500;
        int n2000 = 2000;
        mapdemo.DocsVariable.Value docs500 = new mapdemo.DocsVariable.Value();
        for (int i = 0; i < n500; i++) {
            mapdemo.DocsPagesEntryVariable.Value e = new mapdemo.DocsPagesEntryVariable.Value();
            e.key = String.format("k%04d", i);
            e.value = "the same literal";
            docs500.pages.add(e);
        }
        mapdemo.DocsVariable.Value docs2000 = new mapdemo.DocsVariable.Value();
        for (int i = 0; i < n2000; i++) {
            mapdemo.DocsPagesEntryVariable.Value e = new mapdemo.DocsPagesEntryVariable.Value();
            e.key = String.format("k%04d", i);
            e.value = "the same literal";
            docs2000.pages.add(e);
        }
        long len500 = mapdemo.DocsCook.cookMeasure(docs500);
        byte[] buf500 = new byte[(int) len500];
        mapdemo.DocsCook.cookFrom(docs500, buf500, mapdemo.TableCookWrite.LITTLE);
        long len2000 = mapdemo.DocsCook.cookMeasure(docs2000);
        byte[] buf2000 = new byte[(int) len2000];
        boolean ok2000 = mapdemo.DocsCook.cookFrom(docs2000, buf2000, mapdemo.TableCookWrite.LITTLE);
        mapdemo.DocsCook cook2000 = mapdemo.DocsCook.open(buf2000);
        int root2000 = cook2000 != null ? cook2000.root() : -1;
        int count2000 = root2000 >= 0 ? mapdemo.DocsRow.pagesCount(buf2000, root2000) : -1;
        int pages2000 = count2000 >= 0 ? cook2000.at(mapdemo.DocsRow.pagesSlot(root2000), count2000 * mapdemo.DocsRow.pagesStride) : -1;
        int at0 = pages2000 >= 0 ? mapdemo.DocsRow.pagesFind(buf2000, pages2000, count2000, "k0000".getBytes("UTF-8")) : -1;
        int at1999 = pages2000 >= 0 ? mapdemo.DocsRow.pagesFind(buf2000, pages2000, count2000, "k1999".getBytes("UTF-8")) : -1;
        boolean readOk = count2000 == n2000 && at0 >= 0 && at1999 > at0;
        System.out.println("map-large-2000 " + (ok2000 && readOk && len2000 > len500 + 1500L * 32L));
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
		"loaded refused",
		"loaded-retain refused",
		"str-loaded refused",
		"str-loaded-retain refused",
		"fresh-save refused",
		"fresh-json refused",
		"fresh-retain-save refused",
		"fresh-retain-measure refused",
		"reset-bytes-save written",
		"equal-save written",
		"literal-save written",
		"distinct-save written",
		"literal-two-nodes true",
		"map-literal-two-nodes true",
		"map-large-2000 true",
	}, "\n")
	if got != want {
		t.Fatalf("the Java writers on a shared blob (§2.5):\n%s\nwant:\n%s", got, want)
	}
}
