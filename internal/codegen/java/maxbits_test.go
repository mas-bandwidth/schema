package java

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A packet type whose MaxBits exceeds 2^31 - 1 must still emit valid Java:
// the MAX_BITS constant (and the bound that uses it) are literals javac
// accepts. The storage `[300000000]uint8` is 2,400,000,000 bits, past
// Integer.MAX_VALUE.
const giantMaxBitsSchema = `package giant

type Giant
{
    raw [300000000]uint8
}
`

func TestJavaGiantMaxBitsLiteralCompiles(t *testing.T) {
	t.Parallel()

	src := giantMaxBitsSchema
	f, errs := parser.Parse("Big.schema", []byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Big.schema", Name: "Big.schema", Base: "Big", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) != 0 {
		t.Fatal(cerrs)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	text := string(files["Big.java"])
	if len(text) == 0 {
		t.Fatal("Big.java not emitted")
	}

	slowtest.Gate(t, "javac")
	javac, _ := compressedFloatJavaTools(t)
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src", "giant")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	javaFile := filepath.Join(srcDir, "Big.java")
	if err := os.WriteFile(javaFile, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(javac, "--release", "17", "-nowarn", "-d", classes, javaFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
}
