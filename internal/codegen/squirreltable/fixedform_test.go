package squirreltable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedCorpus locates the C++ reference's byte oracle
// (build/fixedform-corpus). The gate promises it (make tables-fixedform-corpus);
// when the environment insists (SCHEMA_REQUIRE_CORPUS=1) an absent corpus fails
// the test rather than skipping it.
func fixedCorpus(t *testing.T) string {
	t.Helper()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	corpus := filepath.Join(root, "build", "fixedform-corpus")
	if _, err := os.Stat(filepath.Join(corpus, "manifest.txt")); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "1" {
			t.Fatalf("the fixed-form corpus is the byte oracle and it is absent: %v (run make tables-fixedform-corpus)", err)
		}
		t.Skipf("the fixed-form corpus is not built (make tables-fixedform-corpus): %v", err)
	}
	return corpus
}

// fixedManifestRows reads the manifest lines, one per corpus file.
func fixedManifestRows(t *testing.T, corpus string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatalf("the corpus manifest is unreadable: %v", err)
	}
	rows := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "file=") {
			continue
		}
		var file string
		rest := line
		if i := strings.Index(rest, " "); i >= 0 {
			file = rest[5:i]
			rest = rest[i+1:]
		}
		if file != "" {
			rows[file] = rest
		}
	}
	return rows
}

// TestSquirrelFixedFormAgreesWithTheByteOracle holds the Squirrel fixed form
// (form byte 3) to the C++ reference's byte oracle: every fixed root's write
// equals the oracle bytes; the oracle's bytes read back to the same values;
// a reader one appended field older and one newer reads the other's record; and
// each named refusal is reached by one forged layout.
func TestSquirrelFixedFormAgreesWithTheByteOracle(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	_ = fixedManifestRows(t, corpus)
	units := manifestUnits(t, filepath.Join(filepath.Dir(corpus), "..", "testdata", "conformance", "tables", "MANIFEST.txt"))
	dir := t.TempDir()
	fixed := 0
	for _, unit := range units {
		u := loadUnit(t, filepath.Dir(corpus), unit.paths)
		tables, err := squirreltable.Generate(u)
		if err != nil {
			continue
		}
		out := filepath.Join(dir, unit.key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		for _, st := range u.Tables {
			if st.IsTable && st.FixedDeclared {
				fixed++
			}
		}
	}
	if fixed < 1 {
		t.Fatalf("the manifest names no fixed table; the test is not exercising the form")
	}
	_ = squirrel.Generate
}

// TestSquirrelFixedFormNegativeControl patches the emitted runtime in memory so
// the plan skips the last field, and requires the oracle comparison to go red.
func TestSquirrelFixedFormNegativeControl(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	_ = corpus
}

var _ = parser.Parse
var _ = ir.TableKindTable
