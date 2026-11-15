package javatable

// A FIXED-FORM FILE HOLDS AT LEAST ONE RECORD (docs/FIXED-FORM-ALGORITHM.md
// §5.3 step 9, docs/SPEC-TABLES.md §3.4 and §22.6). The Java fixed load
// refuses a file whose header and layout are followed by no record, a file
// holding one record still reads, and the fixed save refuses an empty batch
// as the leg's debug-only write contract (docs/rulings.json write-refusal).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestJavaFixedBatchHoldsARecord(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "an external toolchain")

	repo := filepath.Join("..", "..", "..")
	vector := filepath.Join(repo, "testdata", "wire", "fixed-empty-batch")
	oneRecord := filepath.Join(vector, "one-record.bin")
	noRecord := filepath.Join(vector, "no-record.bin")
	for _, path := range []string{oneRecord, noRecord} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("the vector has no %s: %v", filepath.Base(path), err)
		}
	}

	u := unitFromPath(t, filepath.Join(repo, "test", "fixed-empty-batch", "Batch.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	pkg := javaPackageOf(u)
	if pkg == "" {
		t.Fatal("the schema declares no package")
	}

	javac, javaBin := javaTools(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixedJava(t, src, pkg, files)
	driver := fixedBatchDriver(pkg)
	if err := os.WriteFile(filepath.Join(src, "Driver.java"), []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}

	sources, err := filepath.Glob(filepath.Join(src, "*", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, filepath.Join(src, "Driver.java"))
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	// -ea is the debug build: the save contract is a debug assert.
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Driver", oneRecord, noRecord).CombinedOutput()
	if err != nil {
		t.Fatalf("java Driver: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("driver did not hold the rule:\n%s", out)
	}
}

func fixedBatchDriver(pkg string) string {
	return fmt.Sprintf(`import java.nio.file.Files;
import java.nio.file.Path;

public final class Driver {
    private Driver() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    static %[1]s.BatchFixed.Value[] fresh(int n) {
        %[1]s.BatchFixed.Value[] out = new %[1]s.BatchFixed.Value[n];
        for (int i = 0; i < n; i++) {
            out[i] = new %[1]s.BatchFixed.Value();
        }
        return out;
    }

    public static void main(String[] args) throws Exception {
        byte[] one = Files.readAllBytes(Path.of(args[0]));
        byte[] none = Files.readAllBytes(Path.of(args[1]));

        // EVERY FIXED READ ENTRY refuses a file with no record: a root read
        // (room for one) and a batch read (room for more).
        for (int cap : new int[]{1, 4}) {
            %[1]s.BatchFixed.Value[] values = fresh(cap);
            %[1]s.TableFixed.Report report = new %[1]s.TableFixed.Report();
            int n = %[1]s.BatchFixed.load(values, cap, none,
                    %[1]s.TableFixed.plan(1 << 12), new short[1 << 12],
                    %[1]s.BatchFixed.image(), report);
            if (n != -1) {
                fail("empty batch (capacity " + cap + ") read " + n + " records, want a refusal");
            }
            if (!report.refused && !report.damaged) {
                fail("empty batch (capacity " + cap + ") left no refusal");
            }
        }

        // A ONE-RECORD FILE STILL READS.
        %[1]s.BatchFixed.Value[] values = fresh(1);
        %[1]s.TableFixed.Report report = new %[1]s.TableFixed.Report();
        int n = %[1]s.BatchFixed.load(values, 1, one,
                %[1]s.TableFixed.plan(1 << 12), new short[1 << 12],
                %[1]s.BatchFixed.image(), report);
        if (n != 1) {
            fail("one-record file read " + n + " records, want 1");
        }
        if (report.refused || report.damaged) {
            fail("one-record file was refused");
        }

        // A SAVE OF ZERO RECORDS IS REFUSED, as the leg's debug assert.
        byte[] buffer = new byte[%[1]s.BatchFixed.measure(1)];
        try {
            int wrote = %[1]s.BatchFixed.save(fresh(1), 0, buffer);
            fail("save of zero records wrote " + wrote + " bytes, want a refusal");
        } catch (AssertionError expected) {
            // the debug write contract, active under -ea
        }

        System.out.println("ok");
    }
}
`, pkg)
}
