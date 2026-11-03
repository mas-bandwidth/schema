package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// This is deliberately a backend staging hook, not public compiler admission.
// The fixture units contain arm aliases; refuseWasRows stays authoritative for
// CLI generation until the fixed-class alias card lands. The matrix can still
// measure these native variable codecs against the shared committed fixtures.
func TestStageJavaVariableConformance(t *testing.T) {
	t.Parallel()
	out := os.Getenv("SCHEMA_JAVA_NATIVE_CONFORMANCE_OUT")
	verifyCompile := false
	if out == "" {
		out = t.TempDir()
		verifyCompile = true
	}
	var javac string
	if verifyCompile {
		slowtest.Gate(t, "javac")
		javac, _ = javaTools(t)
	}
	for _, row := range []struct{ name, dir string }{{"native-messages", "tables/messages"}, {"native-wide", "examples-wide"}} {
		u := unitFromDir(t, filepath.Join("..", "..", "..", row.dir))
		files, err := javacodegen.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range tables {
			if _, exists := files[name]; exists {
				t.Fatalf("duplicate generated Java file %s", name)
			}
			files[name] = data
		}
		if len(files) == 0 {
			t.Fatalf("%s: no generated Java files", row.name)
		}
		dest := filepath.Join(out, row.name)
		if err := os.MkdirAll(dest, 0755); err != nil {
			t.Fatal(err)
		}
		var javaFiles []string
		for name, data := range files {
			if len(data) == 0 {
				t.Fatalf("%s: empty generated Java file %s", row.name, name)
			}
			path := filepath.Join(dest, name)
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			javaFiles = append(javaFiles, path)
		}
		for _, req := range []string{"TableVariable.java", "TableFixed.java"} {
			if st, err := os.Stat(filepath.Join(dest, req)); err != nil || st.Size() == 0 {
				t.Fatalf("%s: missing or empty staged runtime %s: %v", row.name, req, err)
			}
		}
		if verifyCompile {
			classes := filepath.Join(out, row.name+"-classes")
			if err := os.MkdirAll(classes, 0755); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--release", "17", "-Xlint:all", "-Werror", "-d", classes}, javaFiles...)
			if outBytes, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
				t.Fatalf("%s: javac staged conformance files: %v\n%s", row.name, err, outBytes)
			}
		}
		t.Logf("staged native backend codecs in %s; public CLI arm-alias admission remains refused", dest)
	}
}
