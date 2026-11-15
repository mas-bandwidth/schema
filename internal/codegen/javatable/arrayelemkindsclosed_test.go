package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// arrayElemKindsDriverJava loads each wire through this leg's strict load as
// graphdemo's Scene and prints one verdict per wire: `read` when the load
// hands back a value, `refused` when it hands back none with the report
// refusing or damaged. It compares no reason (docs/rulings.json,
// refusal-verdict-only): the verdict is the contract.
const arrayElemKindsDriverJava = `import java.nio.file.*;

public final class ArrayElemKindsDriver {
    public static void main(String[] a) throws Exception {
        int bad = 0;
        for (int i = 0; i < a.length; i += 2) {
            String path = a[i], want = a[i + 1];
            byte[] data = Files.readAllBytes(Paths.get(path));
            graphdemo.TableVariable.Report report = new graphdemo.TableVariable.Report();
            graphdemo.SceneVariable.Value value = graphdemo.SceneVariable.load(data, report);
            String got = value != null ? "read" : "refused";
            if (value != null && (report.damaged || report.refused)) {
                System.out.println("FAIL " + path + ": handed back a value on a refusal");
                bad++;
                continue;
            }
            if (value == null && !(report.damaged || report.refused)) {
                System.out.println("FAIL " + path + ": no verdict");
                bad++;
                continue;
            }
            System.out.println("VERDICT " + path + " " + got);
            if (!got.equals(want)) {
                System.out.println("FAIL " + path + ": " + got + ", want " + want);
                bad++;
            }
        }
        System.out.println(bad == 0 ? "OK" : "BAD " + bad);
        System.exit(bad == 0 ? 0 : 1);
    }
}
`

// TestJavaArrayElementKindsClosed holds every pinned array-element-kind wire
// (docs/SPEC-TABLES.md §3, THE ELEMENT KINDS ARE CLOSED) to its strict-load
// verdict through graphdemo's Scene: an element kind outside the closed list
// refuses under any count, a count of 0 included, and the on-list controls
// read. The vectors ride an unknown root field, so the wire-alone walk must
// judge the element kind before any declaration.
func TestJavaArrayElementKindsClosed(t *testing.T) {
	t.Parallel()
	javac, java := javaToolsRequired(t)
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	files := generateJavaUnitFiles(t, filepath.Join(repo, "tables", "pointers"))

	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(dir, filepath.Base(name))
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	driver := filepath.Join(dir, "ArrayElemKindsDriver.java")
	if err := os.WriteFile(driver, []byte(arrayElemKindsDriverJava), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	if b, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, b)
	}

	cases := []struct {
		name string
		want string
	}{
		{"array_elem_kind_32_count_2.bin", "refused"},
		{"array_elem_kind_31_count_1.bin", "refused"},
		{"array_elem_kind_16_count_1.bin", "refused"},
		{"array_elem_unassigned_count_0.bin", "refused"},
		{"array_elem_reserved_count_0.bin", "refused"},
		{"array_elem_zero_count_0.bin", "refused"},
		{"array_elem_kind_17_count_0.bin", "read"},
		{"array_elem_kind_17_count_1.bin", "read"},
	}
	jargs := []string{"-ea", "-cp", classes, "ArrayElemKindsDriver"}
	for _, c := range cases {
		jargs = append(jargs, filepath.Join(repo, "testdata", "wire", "tables", "framing", c.name), c.want)
	}
	out, err := exec.Command(java, jargs...).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "FAIL") {
		t.Fatalf("an array element kind answered the wrong verdict:\n%s", out)
	}
	t.Logf("%s", out)
}
