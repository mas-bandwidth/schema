package lobstertable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
)

// fixedCorpus is the reference dump's nine form-3 files and the schema/root
// each one was written from (docs/FIXED-FORM-ALGORITHM.md §7 item 1).
var fixedCorpus = []struct{ file, schema, root string }{
	{"fx1.bin", "test/tables/FX1.schema", "FxRoot"},
	{"fx2.bin", "test/tables/FX2.schema", "FxRoot"},
	{"p1.bin", "test/tables/P1.schema", "Chain"},
	{"p3.bin", "test/tables/P3.schema", "Chain"},
	{"keyed.bin", "tables/examples/Keyed.schema", "KeyedConfig"},
	{"pack.bin", "tables/examples/Pack.schema", "PackConfig"},
	{"fu1.bin", "test/tables/FU1.schema", "FuRoot"},
	{"fu2.bin", "test/tables/FU2.schema", "FuRoot"},
}

// TestLobsterFixedFormAgreesWithTheByteOracle reads every fixed root the
// reference dumped (docs/FIXED-FORM-ALGORITHM.md §7 item 1), saves it back
// through the Lobster form-3 writer, and holds the bytes to the C++ oracle's
// byte for byte. Both directions are exercised: Load reads the reference's
// bytes and Save writes them. docs/SPEC-TABLES.md §3.4.
func TestLobsterFixedFormAgreesWithTheByteOracle(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpusDir(t)
	compared := 0
	for _, c := range fixedCorpus {
		want, err := os.ReadFile(filepath.Join(corpus, c.file))
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		got := fixedRun(t, c.schema, c.root, want)
		if fixedOracleNonFinite(t, corpus, c.file) {
			// A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3): the C++ corpus
			// before finite-tables #2232 regenerates it still writes a NaN
			// into this file, and every writer refuses one: the save answers
			// nothing
			if len(got) != 0 {
				t.Errorf("%s: the oracle carries a NaN or an infinity and the save wrote %d bytes; every writer refuses one", c.file, len(got))
			}
			compared++
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%s: save after load is not the oracle's bytes (got %d, want %d)", c.file, len(got), len(want))
		}
		compared++
	}
	t.Logf("fixed roots compared byte for byte: %d", compared)
}

// TestLobsterFixedFormNegativeControl plants one flipped byte in a live
// record body and holds the gate red: the saved bytes must not equal the
// planted input. docs/FIXED-FORM-ALGORITHM.md §7 item 3.
func TestLobsterFixedFormNegativeControl(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpusDir(t)
	want, err := os.ReadFile(filepath.Join(corpus, "fx1.bin"))
	if err != nil {
		t.Fatalf("fx1.bin: %v", err)
	}
	corrupt := append([]byte(nil), want...)
	layout := int(corrupt[16]) | int(corrupt[17])<<8 | int(corrupt[18])<<16 | int(corrupt[19])<<24
	at := 20 + layout + 8 // the first body byte of the first record
	corrupt[at] ^= 0xff
	got := fixedRun(t, "test/tables/FX1.schema", "FxRoot", corrupt)
	if string(got) == string(want) {
		t.Fatalf("the negative control did not go red: a flipped offset %d still saved the oracle's bytes", at)
	}
}

// fixedCorpusDir is the C++ reference's byte oracle, built by
// `make tables-fixedform-corpus`. It follows every other leg's corpus-reading
// suite (cpptable's fixedversioning_test.go, and the contract ci-fast.yml's
// go-test-touched names): a bare `go test` on a tree that never built the
// oracle has nothing to read and says so, and under SCHEMA_REQUIRE_CORPUS —
// which `make test-lobster` sets after building the corpus — a missing oracle
// is a failure, never a quiet skip.
func fixedCorpusDir(t *testing.T) string {
	t.Helper()
	corpus := filepath.Join(root, "build", "fixedform-corpus")
	if _, err := os.Stat(filepath.Join(corpus, "manifest.txt")); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the reference's byte oracle is not in %s: %v", corpus, err)
		}
		t.Skip("the reference's byte oracle is not built: make tables-fixedform-corpus")
	}
	return corpus
}

// fixedRun generates one schema for Lobster, runs a main that loads the
// input and prints the saved file as hex, and returns those bytes.
func fixedRun(t *testing.T, schema, rootName string, input []byte) []byte {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, schema)})
	if err != nil {
		t.Fatalf("%s: %v", schema, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("%s does not load: %v", schema, err)
	}
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatalf("%s: lobster: %v", schema, err)
	}
	tables, err := lobstertable.Generate(u)
	if err != nil {
		t.Fatalf("%s: lobster tables: %v", schema, err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	base := ""
	for _, f := range u.Files {
		if f.Base != "" && strings.Contains(schema, f.Base+".schema") {
			base = f.Base
		}
	}
	if base == "" {
		for _, f := range u.Files {
			for _, st := range f.Tables {
				if st.Name == rootName {
					base = f.Base
				}
			}
		}
	}
	main := fmt.Sprintf(`import .%sTable
%s
def read_in() -> string:
    let f = read_file("in.bin")
    if f: return f
    return ""
let values = %s.%sFixedDecode(read_in())
hex(%s.%sFixedSave(values))
`, base, lobsterrun.HexProbe, u.Package, rootName, u.Package, rootName)
	if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := lobsterrun.Run(t, dir, "main.lobster", map[string][]byte{"in.bin": input})
	lines := lobsterrun.HexLines(t, out)
	if len(lines) != 1 {
		t.Fatalf("%s: main printed %d hex lines, want 1", schema, len(lines))
	}
	return lines[0]
}

// fixedOracleNonFinite reports whether the oracle's manifest says file
// carries a NaN or an infinity (`nan|0x…`, `inf|0x…`).
func fixedOracleNonFinite(t *testing.T, corpus, file string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "file="+file+" ") {
			return strings.Contains(line, "=nan|") || strings.Contains(line, "=inf|") || strings.Contains(line, "=-inf|")
		}
	}
	return false
}
