package javatable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func generateJavaUnitFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	u := unitFromDir(t, dir)
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
	return files
}

func runJavaBlobSharedStrict(t *testing.T, mutate func(unitName, fileName, src string) string) error {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	censusDir := filepath.Join(repo, "testdata", "conformance", "feature-witnesses", "blob-shared-report")
	censusUnit := unitFromDir(t, censusDir)
	rawCases, err := os.ReadFile(filepath.Join(censusDir, "cases.json"))
	if err != nil {
		return err
	}
	var cases []blobSharedReportCase
	if err := json.Unmarshal(rawCases, &cases); err != nil {
		return err
	}
	if len(cases) != 9 {
		return fmt.Errorf("expected 9 shared blob-report cases, got %d", len(cases))
	}

	type oracleVerdict struct {
		refused bool
		damaged bool
		reason  string
	}
	verdicts := map[string]oracleVerdict{}
	m := tabletext.NewModel(censusUnit)
	for _, row := range cases {
		wire, err := hex.DecodeString(row.Wire)
		if err != nil {
			return err
		}
		v := m.New(censusUnit.Tables["Root"])
		for _, retaining := range []bool{false, true} {
			store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
			r := tabletext.Report{}
			var ok bool
			if retaining {
				ok, err = tablewire.DecodeRetain(m, v, wire, store, &r)
			} else {
				ok, err = tablewire.Decode(m, v, wire, &r)
			}
			if err != nil && !tablewire.ReadRefusal(err) {
				return fmt.Errorf("%s retain=%v: oracle: %w", row.Name, retaining, err)
			}
			reason, _, _ := strings.Cut(r.Reason, ":")
			damaged := (!ok || r.Refused) && reason == "wire_damaged"
			refused := (!ok || r.Refused) && !damaged
			ov := oracleVerdict{
				refused: refused,
				damaged: damaged,
				reason:  reason,
			}
			if first, seen := verdicts[row.Name]; seen && first != ov {
				return fmt.Errorf("%s retain=%v: oracle verdict moved: %+v vs %+v", row.Name, retaining, first, ov)
			}
			verdicts[row.Name] = ov
		}
	}

	build := t.TempDir()
	var sources []string
	units := []struct {
		name string
		dir  string
	}{
		{"census", censusDir},
		{"blobs", filepath.Join(repo, "tables", "blobs")},
		{"maps", filepath.Join(repo, "tables", "maps")},
	}
	for _, item := range units {
		files := generateJavaUnitFiles(t, item.dir)
		sub := filepath.Join(build, item.name)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return err
		}
		for name, data := range files {
			if !strings.HasSuffix(name, ".java") {
				continue
			}
			src := string(data)
			if mutate != nil {
				src = mutate(item.name, name, src)
			}
			path := filepath.Join(sub, name)
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
				return err
			}
			sources = append(sources, path)
		}
	}

	var censusBody strings.Builder
	for _, tc := range cases {
		wire, err := hex.DecodeString(tc.Wire)
		if err != nil {
			return err
		}
		var elems []string
		for _, b := range wire {
			elems = append(elems, fmt.Sprintf("(byte) 0x%02x", b))
		}
		ov := verdicts[tc.Name]
		fmt.Fprintf(&censusBody, "        checkCensus(%q, new byte[]{%s}, %q, %t, %t, %q);\n",
			tc.Name, strings.Join(elems, ", "), tc.Saved, ov.refused, ov.damaged, ov.reason)
	}

	censusPkg := javaPackageOf(censusUnit)
	driver := fmt.Sprintf(`import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.Arrays;
import java.util.HexFormat;

public final class BlobSharedStrictDriver {
    private static int checks = 0;

    private static void require(boolean ok, String why) {
        if (!ok) {
            throw new RuntimeException("FAIL: " + why);
        }
        checks++;
    }

    private static void checkCensus(String name, byte[] wire, String savedHex, boolean wantRefused, boolean wantDamaged, String wantReason) {
        for (boolean retaining : new boolean[]{false, true}) {
            %s.TableRetain retain = new %s.TableRetain(1 << 16, 1024);
            %s.TableVariable.Report report = new %s.TableVariable.Report();
            %s.RootVariable.Value v = retaining
                    ? %s.RootRetain.loadRetain(wire, retain, report)
                    : %s.RootVariable.load(wire, report);
            String why = name + " retain=" + retaining;
            if (wantRefused || wantDamaged) {
                require(v == null, "strict refusal returned value: " + why);
                require(report.refused == wantRefused && report.damaged == wantDamaged,
                        "strict flags mismatch: " + why + " refused=" + report.refused + " damaged=" + report.damaged);
                if (wantRefused) {
                    require(wantReason.equals(report.reason),
                            "strict reason mismatch: " + why + " got=" + report.reason + " want=" + wantReason);
                }
                if (retaining) {
                    require(retain.used == 0 && retain.idUsed == 0,
                            "refused retaining load kept a tail: " + why);
                }
                continue;
            }
            require(v != null && !report.refused && !report.damaged, "expected clean read: " + why);
            if (name.startsWith("distinct_")) {
                require(v.a != null && v.b != null && v.c != null && v.d != null && v.c != v.d && Arrays.equals(v.c, v.d),
                        "distinct equal blob identities: " + why);
            }
            byte[] saved = %s.RootVariable.save(v);
            require(saved != null && HexFormat.of().formatHex(saved).equals(savedHex), "plain save: " + why);
            if (retaining) {
                byte[] rsaved = %s.RootRetain.saveRetain(v, retain, report);
                require(rsaved != null && HexFormat.of().formatHex(rsaved).equals(savedHex), "retaining save: " + why);
            }
        }
    }

    public static void main(String[] args) throws Exception {
%s
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        byte[] swire = Files.readAllBytes(Paths.get(args[1]));

        blobdemo.TableVariable.Report report = new blobdemo.TableVariable.Report();
        blobdemo.CatalogVariable.Value loaded = blobdemo.CatalogVariable.load(wire, report);
        require(loaded == null && report.refused && "blob_shared".equals(report.reason), "blob_shared.bin plain load");

        blobdemo.TableRetain retain = new blobdemo.TableRetain(1 << 20, 1024);
        blobdemo.TableVariable.Report rreport = new blobdemo.TableVariable.Report();
        blobdemo.CatalogVariable.Value rloaded = blobdemo.CatalogRetain.loadRetain(wire, retain, rreport);
        require(rloaded == null && rreport.refused && "blob_shared".equals(rreport.reason) && retain.used == 0, "blob_shared.bin retain load");

        mapdemo.TableVariable.Report sreport = new mapdemo.TableVariable.Report();
        mapdemo.DocsVariable.Value sloaded = mapdemo.DocsVariable.load(swire, sreport);
        require(sloaded == null && sreport.refused && "blob_shared".equals(sreport.reason), "blob_shared_string.bin plain load");

        mapdemo.TableRetain sretain = new mapdemo.TableRetain(1 << 20, 1024);
        mapdemo.TableVariable.Report srreport = new mapdemo.TableVariable.Report();
        mapdemo.DocsVariable.Value srloaded = mapdemo.DocsRetain.loadRetain(swire, sretain, srreport);
        require(srloaded == null && srreport.refused && "blob_shared".equals(srreport.reason) && sretain.used == 0, "blob_shared_string.bin retain load");

        blobdemo.CatalogVariable.Value fresh = new blobdemo.CatalogVariable.Value();
        fresh.thumb = new byte[]{1, 2, 3};
        fresh.alias = fresh.thumb;
        require(blobdemo.CatalogVariable.save(fresh) == null, "shared byte[] plain save refused");
        require(blobdemo.CatalogVariable.toJson(fresh) == null, "shared byte[] toJson refused");
        blobdemo.TableRetain fretain = new blobdemo.TableRetain(1 << 20, 1024);
        blobdemo.TableVariable.Report freport = new blobdemo.TableVariable.Report();
        require(blobdemo.CatalogRetain.saveRetain(fresh, fretain, freport) == null, "shared byte[] saveRetain refused");
        require(blobdemo.CatalogRetain.measureRetain(fresh, fretain) < 0, "shared byte[] measureRetain refused");

        fresh.alias = new byte[]{1, 2, 3};
        require(blobdemo.CatalogVariable.save(fresh) != null, "distinct equal byte[] plain save allowed");

        blobdemo.CatalogVariable.Value words = new blobdemo.CatalogVariable.Value();
        words.note = "the same literal";
        words.head = new blobdemo.AssetVariable.Value();
        words.head.caption = "the same literal";
        byte[] literalWire = blobdemo.CatalogVariable.save(words);
        byte[] literalRetainWire = blobdemo.CatalogRetain.saveRetain(words, new blobdemo.TableRetain(1 << 20, 1024), new blobdemo.TableVariable.Report());
        byte[] literalJson = blobdemo.CatalogVariable.toJson(words);
        long literalCookLen = blobdemo.CatalogCook.cookMeasure(words);
        byte[] literalCook = literalCookLen > 0 ? new byte[(int) literalCookLen] : null;
        if (literalCook != null) {
            blobdemo.CatalogCook.cookFrom(words, literalCook, blobdemo.TableCookWrite.LITTLE);
        }

        words.head.caption = new String("the same literal");
        byte[] distinctWire = blobdemo.CatalogVariable.save(words);
        long distinctCookLen = blobdemo.CatalogCook.cookMeasure(words);
        byte[] distinctCook = distinctCookLen > 0 ? new byte[(int) distinctCookLen] : null;
        if (distinctCook != null) {
            blobdemo.CatalogCook.cookFrom(words, distinctCook, blobdemo.TableCookWrite.LITTLE);
        }
        blobdemo.TableVariable.Measure m = new blobdemo.TableVariable.Measure();
        if (literalWire != null) {
            blobdemo.CatalogVariable.loadMeasure(literalWire, m);
        }
        require(literalWire != null
                && Arrays.equals(literalWire, distinctWire)
                && Arrays.equals(literalRetainWire, distinctWire)
                && literalJson != null
                && literalCook != null
                && Arrays.equals(literalCook, distinctCook)
                && m.nodeRecords == 3, "interned String literal numbered by value as two blob records");

        System.out.println("BLOB SHARED STRICT OK " + checks);
    }
}
`, censusPkg, censusPkg, censusPkg, censusPkg, censusPkg, censusPkg, censusPkg, censusPkg, censusPkg, censusBody.String())

	driverPath := filepath.Join(build, "BlobSharedStrictDriver.java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		return err
	}
	sources = append(sources, driverPath)
	classes := filepath.Join(build, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		return err
	}
	javac, javaBin := javaTools(t)
	if out, err := exec.Command(javac, append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)...).CombinedOutput(); err != nil {
		return fmt.Errorf("javac: %w\n%s", err, out)
	}
	wire, err := filepath.Abs(filepath.Join(repo, "testdata", "wire", "tables", "blob_shared.bin"))
	if err != nil {
		return err
	}
	swire, err := filepath.Abs(filepath.Join(repo, "testdata", "wire", "tables", "blob_shared_string.bin"))
	if err != nil {
		return err
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "BlobSharedStrictDriver", wire, swire).CombinedOutput()
	if err != nil {
		return fmt.Errorf("java: %w\n%s", err, out)
	}
	if !strings.Contains(string(out), "BLOB SHARED STRICT OK ") {
		return fmt.Errorf("unexpected output: %s", out)
	}
	return nil
}

// TestJavaBlobSharedStrict certifies vt/blobs/java and java/per-value-shared-marker
// under the strict read and write rules (docs/SPEC-TABLES.md §2.5, docs/plans/strict-only.md).
func TestJavaBlobSharedStrict(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	if err := runJavaBlobSharedStrict(t, nil); err != nil {
		t.Fatal(err)
	}
}

// TestJavaBlobSharedStrictNegativeControl plants real faults in the emitted Java
// artifacts for read-side shared-blob detection, write-side shared-bytes refusal,
// and write-side per-slot *string value numbering, and verifies each fault fails.
func TestJavaBlobSharedStrictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()

	t.Run("read-wirecheck-shared-blob", func(t *testing.T) {
		t.Parallel()
		replaced := false
		err := runJavaBlobSharedStrict(t, func(unitName, fileName, src string) string {
			if fileName != "TableVariable.java" {
				return src
			}
			const target = "if (blob[k - 1] && names[k] > 1) {"
			if strings.Contains(src, target) {
				replaced = true
				src = strings.Replace(src, target, "if (false && blob[k - 1] && names[k] > 1) {", 1)
			}
			src = strings.Replace(src, "if (rec.judged) {", "if (false && rec.judged) {", 1)
			return src
		})
		if !replaced {
			t.Fatal("negative control did not find WireCheck blob_shared guard in TableVariable.java")
		}
		if err == nil {
			t.Fatal("negative control failed: disabling read-side blob_shared check left the test green")
		}
	})

	t.Run("write-shared-bytes-blob", func(t *testing.T) {
		t.Parallel()
		replaced := false
		err := runJavaBlobSharedStrict(t, func(unitName, fileName, src string) string {
			if unitName != "blobs" || fileName != "TableVariable.java" {
				return src
			}
			const target = "if (isBlob && keyIndex[i] != 0) {"
			if strings.Contains(src, target) {
				replaced = true
				return strings.Replace(src, target, "if (false && isBlob && keyIndex[i] != 0) {", 1)
			}
			return src
		})
		if !replaced {
			t.Fatal("negative control did not find NodeGraph recordPointer blob guard in TableVariable.java")
		}
		if err == nil {
			t.Fatal("negative control failed: disabling write-side shared byte[] refusal left the test green")
		}
	})

	t.Run("write-interned-string-identity", func(t *testing.T) {
		t.Parallel()
		replaced := 0
		err := runJavaBlobSharedStrict(t, func(unitName, fileName, src string) string {
			if unitName != "blobs" || (fileName != "CatalogVariable.java" && fileName != "AssetVariable.java") {
				return src
			}
			for _, field := range []string{"note", "caption"} {
				target := fmt.Sprintf("v.%sIdx = g.recordBlob(v.%s, TableVariable.STRING_ID);", field, field)
				replacement := fmt.Sprintf("if (!g.recordPointer(v.%s, TableVariable.STRING_ID, true)) return false; v.%sIdx = g.index(v.%s);", field, field, field)
				if strings.Contains(src, target) {
					replaced++
					src = strings.Replace(src, target, replacement, 1)
				}
			}
			return src
		})
		if replaced != 2 {
			t.Fatalf("negative control replaced %d *string recordBlob calls, want 2", replaced)
		}
		if err == nil {
			t.Fatal("negative control failed: numbering *string slots by reference identity instead of value left the test green")
		}
	})
}
