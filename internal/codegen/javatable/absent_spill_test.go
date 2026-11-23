package javatable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestJavaAbsentSpillEveryMarkerGoesThroughTheAbsenceWriters holds the
// conformance driver's one absence path: a case this leg cannot answer is
// written by spillAbsent or its named sibling spillAbsentReason, and no
// surface writes a `.absent` marker itself. The strict blob_shared refusal
// (docs/SPEC-TABLES.md §2.5) is no absence: ruling R13 answers it `refused`
// in <case>, gated on the runtime's own refusal reason.
func TestJavaAbsentSpillEveryMarkerGoesThroughTheAbsenceWriters(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	src, err := os.ReadFile(filepath.Join(repo, "test", "conformance", "java", "src", "Driver.java"))
	if err != nil {
		t.Fatal(err)
	}
	driver := string(src)

	for _, helper := range []string{"spillAbsent", "spillAbsentReason"} {
		if !strings.Contains(driver, "private static void "+helper+"(") {
			t.Errorf("the driver has no %s absence writer", helper)
		}
	}
	if got := strings.Count(driver, `Files.write(Path.of(out, name + ".absent")`); got != 2 {
		t.Errorf("the two absence writers hold %d marker writes, want 2", got)
	}

	body := driverFunc(t, driver, "surfaceWire")
	if strings.Contains(body, ".absent") {
		t.Error("surfaceWire writes an absence marker itself instead of calling an absence writer")
	}
	if !regexp.MustCompile(`reason != null && reason\.startsWith\("blob_shared"\)\) \{\s*spill\(out, f\[1\], "refused\\n"`).MatchString(body) {
		t.Error("surfaceWire does not answer the runtime's strict blob_shared refusal `refused`")
	}

	surfaces := regexp.MustCompile(`private static int (surface[A-Za-z]+)\(`).FindAllStringSubmatch(driver, -1)
	if len(surfaces) == 0 {
		t.Fatal("the driver has no surfaces")
	}
	for _, match := range surfaces {
		name := match[1]
		if strings.Contains(driverFunc(t, driver, name), ".absent") {
			t.Errorf("%s writes an absence marker itself instead of calling an absence writer", name)
		}
	}
}

// TestJavaBlobSharedConformanceDriverAnswersTheWireRefusal runs the real
// conformance driver over a manifest with one strict blob_shared instance and
// one instance whose unit is off the classpath. The first must answer
// `refused` (ruling R13), the second the unnamed absence, and the wire
// surface must be green: the strict read's refusal (docs/SPEC-TABLES.md §2.5)
// is the wire-refused row's answer, never a failed case.
func TestJavaBlobSharedConformanceDriverAnswersTheWireRefusal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	repo := filepath.Join("..", "..", "..")
	build := t.TempDir()
	classes := filepath.Join(build, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}

	var sources []string
	units := []struct{ name, path string }{
		{"blobs", filepath.Join(repo, "tables", "blobs")},
		{"maps", filepath.Join(repo, "tables", "maps")},
		{"block", filepath.Join(repo, "tables", "block")},
		{"pointers", filepath.Join(repo, "tables", "pointers")},
		{"rt1", filepath.Join(repo, "test", "tables", "RT1.schema")},
		{"tblvarevo1", filepath.Join(repo, "test", "tables", "varevo", "VarEvo1.schema")},
		{"tblvarevo2", filepath.Join(repo, "test", "tables", "varevo", "VarEvo2.schema")},
		{"cooktorture", filepath.Join(repo, "test", "tables", "cooktorture", "CookTorture.schema")},
	}
	for _, unit := range units {
		u := unitFromDir(t, unit.path)
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
		sub := filepath.Join(build, unit.name)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if !strings.HasSuffix(name, ".java") {
				continue
			}
			path := filepath.Join(sub, name)
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, path)
		}
	}

	driverPath := filepath.Join(repo, "test", "conformance", "java", "src", "Driver.java")
	sources = append(sources, driverPath)
	javac, javaBin := javaTools(t)
	if out, err := testproc.Command(t, javac, append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	wire, err := filepath.Abs(filepath.Join(repo, "testdata", "wire", "tables", "blob_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(build, "MANIFEST.txt")
	text := "instance blob_shared blobdemo Catalog " + wire + " no-text wire-refused\n" +
		"instance missing_leg nothere Root " + wire + "\n"
	if err := os.WriteFile(manifest, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(build, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	run, err := testproc.Command(t, javaBin, "-ea", "-cp", classes, "Driver", manifest, "wire", out).CombinedOutput()
	if err != nil {
		t.Fatalf("the wire surface failed: %v\n%s", err, run)
	}

	refused, err := os.ReadFile(filepath.Join(out, "blob_shared"))
	if err != nil {
		t.Fatalf("the strict blob_shared refusal wrote no answer: %v", err)
	}
	if string(refused) != "refused\n" {
		t.Errorf("blob_shared = %q, want %q", refused, "refused\n")
	}
	if _, err := os.Stat(filepath.Join(out, "blob_shared.absent")); err == nil {
		t.Error("blob_shared wrote an absence beside its refusal")
	}
	missing, err := os.ReadFile(filepath.Join(out, "missing_leg.absent"))
	if err != nil {
		t.Fatalf("the missing unit spilled no absence: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("missing_leg.absent = %q, want an unnamed absence", missing)
	}
}
