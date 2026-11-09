package javatable_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The Java conformance driver reads strict and writes back: an accepted
// read owes `read\n`, the complete text value and the canonical
// write-back wire in `<case>.wire` (a fixed root's own form 3, a variable
// root's variable save), a refused read owes only `refused\n` and no
// wire, and retain answers with the known value in the same shape.

func javaConformanceRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func javaConformanceBuild(t *testing.T, root string) {
	t.Helper()
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", "build/conformance-harness", "build-conformance-java")
	cmd.Dir = root
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build the Java conformance leg: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "build", "conformance-java", "Driver.class")); err != nil {
		t.Fatalf("built driver not found: %v", err)
	}
}

func javaDriverSurface(t *testing.T, root, surface, manifest string) string {
	t.Helper()
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.txt")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(root, "test", "conformance", "java", "driver"), manifestPath, surface, outDir)
	cmd.Dir = root
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driver %s surface: %v\n%s", surface, err, out)
	}
	return outDir
}

func TestJavaDriverSurfacesStrict(t *testing.T) {
	slowtest.Gate(t, "the JDK and the conformance corpus")
	root := javaConformanceRoot(t)
	javaConformanceBuild(t, root)

	// The whole matrix over the real corpus.
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(root, "build", "conformance-harness"), "run", "--only", "java", "--work", t.TempDir())
	cmd.Dir = root
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("conformance harness --only java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "tables conformance: every registered surface passes") {
		t.Fatalf("conformance harness did not pass every surface:\n%s", out)
	}

	// A readable report row: read, text and canonical write-back wire.
	wirePath := filepath.Join(root, "testdata", "wire", "tables", "fuzz-vectors", "map_key_widen.bin")
	outDir := javaDriverSurface(t, root, "report", "report map_key_widen mapdemo Chunks "+wirePath+"\n")
	got, err := os.ReadFile(filepath.Join(outDir, "map_key_widen"))
	if err != nil {
		t.Fatal(err)
	}
	wantText, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "report-values", "map_key_widen.json"))
	if err != nil {
		t.Fatal(err)
	}
	if want := append([]byte("read\n"), wantText...); !bytes.Equal(got, want) {
		t.Fatalf("map_key_widen report answer is not read + canonical text:\ngot  %q\nwant %q", got, want)
	}
	gotWire, err := os.ReadFile(filepath.Join(outDir, "map_key_widen.wire"))
	if err != nil {
		t.Fatalf("accepted read wrote no write-back wire: %v", err)
	}
	wantWire, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "save-wire", "map_key_widen.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotWire, wantWire) {
		t.Fatalf("map_key_widen write-back wire (%d bytes) differs from the canonical wire (%d bytes)", len(gotWire), len(wantWire))
	}

	// A fixed root's positive (form-3) wire, read through framing by its
	// variable form: the write-back is the root's own form-3 bytes.
	framingWire := filepath.Join(root, "testdata", "wire", "tables", "framing", "frame_unreferenced_entry.bin")
	outDir = javaDriverSurface(t, root, "framing", "framing frame_unreferenced_entry tblr1 Cfg "+framingWire+"\n")
	gotWire, err = os.ReadFile(filepath.Join(outDir, "frame_unreferenced_entry.wire"))
	if err != nil {
		t.Fatalf("accepted framing read wrote no write-back wire: %v", err)
	}
	wantWire, err = os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "save-wire", "frame_unreferenced_entry.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotWire, wantWire) {
		t.Fatalf("frame_unreferenced_entry write-back wire (%d bytes) differs from the canonical fixed wire (%d bytes)", len(gotWire), len(wantWire))
	}

	// Retain: `read\n` plus the complete known value, and the retained
	// save beside it as the write-back wire.
	retainWire := filepath.Join(root, "testdata", "wire", "tables", "retain_rt2.bin")
	outDir = javaDriverSurface(t, root, "retain", "retain retain_round_trip tblrt1 Node "+retainWire+" full full\n")
	got, err = os.ReadFile(filepath.Join(outDir, "retain_round_trip"))
	if err != nil {
		t.Fatal(err)
	}
	wantText, err = os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "retain-values", "retain_round_trip.json"))
	if err != nil {
		t.Fatal(err)
	}
	if want := append([]byte("read\n"), wantText...); !bytes.Equal(got, want) {
		t.Fatalf("retain answer is not read + known value:\ngot  %q\nwant %q", got, want)
	}
	gotWire, err = os.ReadFile(filepath.Join(outDir, "retain_round_trip.wire"))
	if err != nil {
		t.Fatalf("retain read wrote no write-back wire: %v", err)
	}
	wantWire, err = os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "retain_rt1_save.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotWire, wantWire) {
		t.Fatalf("retain write-back wire (%d bytes) differs from the retained save (%d bytes)", len(gotWire), len(wantWire))
	}
}

func TestJavaDriverSurfacesStrictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the JDK and the conformance corpus")
	root := javaConformanceRoot(t)
	javaConformanceBuild(t, root)

	// Plant the fault in a copy of the real positive artifact: its first
	// byte becomes the previous (variable) form's, which a fixed root's
	// one read must refuse, with no value and no write-back wire.
	positive, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "wire", "root_full.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(positive) == 0 || positive[0] != 3 {
		t.Fatalf("root_full positive wire does not start with the fixed form byte: %v", positive[:1])
	}
	sabotaged := append([]byte(nil), positive...)
	sabotaged[0] = 1
	dir := t.TempDir()
	sabotagedPath := filepath.Join(dir, "root_full.bin")
	if err := os.WriteFile(sabotagedPath, sabotaged, 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := javaDriverSurface(t, root, "report", "report root_full tabledemo RootConfig "+sabotagedPath+"\n")
	got, err := os.ReadFile(filepath.Join(outDir, "root_full"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "refused\n" {
		t.Fatalf("previous-form wire at a fixed root was not refused: %q", got)
	}
	if wire, err := os.ReadFile(filepath.Join(outDir, "root_full.wire")); err == nil && len(wire) > 0 {
		t.Fatalf("refused read left a %d-byte write-back wire", len(wire))
	}

	// The real historical form-1 artifact refuses the same way.
	previousPath := filepath.Join(root, "testdata", "wire", "tables", "root_full.bin")
	outDir = javaDriverSurface(t, root, "report", "report root_full tabledemo RootConfig "+previousPath+"\n")
	got, err = os.ReadFile(filepath.Join(outDir, "root_full"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "refused\n" {
		t.Fatalf("historical form-1 wire at a fixed root was not refused: %q", got)
	}

	// A real damaged framing vector (a repeated field id is damage)
	// refuses through the variable-form framing read.
	framingPath := filepath.Join(root, "testdata", "wire", "tables", "framing", "frame_repeat_last_wins.bin")
	outDir = javaDriverSurface(t, root, "framing", "framing frame_repeat_last_wins tblr1 Cfg "+framingPath+"\n")
	got, err = os.ReadFile(filepath.Join(outDir, "frame_repeat_last_wins"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "refused\n" {
		t.Fatalf("repeated-id framing wire was not refused: %q", got)
	}
	if wire, err := os.ReadFile(filepath.Join(outDir, "frame_repeat_last_wins.wire")); err == nil && len(wire) > 0 {
		t.Fatalf("refused framing read left a %d-byte write-back wire", len(wire))
	}
}
