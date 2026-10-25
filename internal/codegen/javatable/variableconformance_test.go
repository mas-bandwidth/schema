package javatable

import (
	"os"
	"path/filepath"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
)

// This is deliberately a backend staging hook, not public compiler admission.
// The fixture units contain arm aliases; refuseWasRows stays authoritative for
// CLI generation until the fixed-class alias card lands. The matrix can still
// measure these native variable codecs against the shared committed fixtures.
func TestStageJavaVariableConformance(t *testing.T) {
	t.Parallel()
	out := os.Getenv("SCHEMA_JAVA_NATIVE_CONFORMANCE_OUT")
	if out == "" {
		t.Skip("test-only native conformance staging is requested by make build-conformance-java")
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
		dest := filepath.Join(out, row.name)
		if err := os.MkdirAll(dest, 0755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dest, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("staged native backend codecs in %s; public CLI arm-alias admission remains refused", dest)
	}
}
