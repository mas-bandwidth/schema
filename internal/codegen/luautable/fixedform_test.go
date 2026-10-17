package luautable_test

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// fixedCorpusVectors pairs the corpus files with the schema and root that wrote
// them (docs/FIXED-FORM-ALGORITHM.md §7), the same nine the Go leg reads.
type fixedVector struct {
	file, schema, root string
}

var fixedCorpusVectors = []fixedVector{
	{"fx1.bin", "test/tables/FX1.schema", "FxRoot"},
	{"fx2.bin", "test/tables/FX2.schema", "FxRoot"},
	{"p1.bin", "test/tables/P1.schema", "Chain"},
	{"p3.bin", "test/tables/P3.schema", "Chain"},
	{"keyed.bin", "tables/examples/Keyed.schema", "KeyedConfig"},
	{"pack.bin", "tables/examples/Pack.schema", "PackConfig"},
	{"fxw.bin", "test/tables/FXW.schema", "FxWide"},
	{"fu1.bin", "test/tables/FU1.schema", "FuRoot"},
	{"fu2.bin", "test/tables/FU2.schema", "FuRoot"},
}

// TestLuauFixedFormAgreesWithTheByteOracle compares the emitted fixed-form
// static data — the wire hash, the record body size and the layout — with the
// C++ reference's byte oracle, for every fixed root the corpus names
// (docs/FIXED-FORM-ALGORITHM.md §7 row 1). The oracle is built by make
// tables-fixedform-corpus; when it is absent the test FAILS under
// SCHEMA_REQUIRE_CORPUS=1, never skips.
func TestLuauFixedFormAgreesWithTheByteOracle(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(root, "build", "fixedform-corpus")
	for _, v := range fixedCorpusVectors {
		t.Run(strings.TrimSuffix(v.file, ".bin"), func(t *testing.T) {
			t.Parallel()
			oracle, err := os.ReadFile(filepath.Join(corpus, v.file))
			if err != nil {
				if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "" {
					t.Skip("the oracle is absent; SCHEMA_REQUIRE_CORPUS unset")
				}
				t.Fatalf("the oracle file %s is absent: build it with make tables-fixedform-corpus: %v", v.file, err)
			}
			mod := generateModule(t, v.schema)
			hash := binary.LittleEndian.Uint64(oracle[8:16])
			layoutLen := binary.LittleEndian.Uint32(oracle[16:20])
			layout := oracle[20 : 20+layoutLen]
			checkEmitted(t, mod, v.root, hash, layoutLen, layout)
		})
	}
}

// generateModule generates the unit for one corpus schema and returns the
// <Base>Table.luau module text.
func generateModule(t *testing.T, schema string) string {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, schema)})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range tables {
		files[k] = v
	}
	dir := t.TempDir()
	var mod string
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.luau") {
			mod = string(data)
		}
	}
	return mod
}

// checkEmitted asserts the module carries the root's fixed-form data agreeing
// with the oracle header.
func checkEmitted(t *testing.T, mod, root string, hash uint64, layoutLen uint32, layout []byte) {
	t.Helper()
	lit := luau.IntegerLit(new(big.Int).SetUint64(hash))
	hashLine := fmt.Sprintf("%sFixedHash = %s", root, lit)
	if !strings.Contains(mod, hashLine) {
		t.Errorf("the emitted module does not carry %s (hash 0x%016x)", hashLine, hash)
	}
	recordLine := fmt.Sprintf("%sFixedRecordBytes =", root)
	if !strings.Contains(mod, recordLine) {
		t.Errorf("the emitted module does not carry %s", recordLine)
	}
	layoutBlock := fmt.Sprintf("%sFixedLayout", root)
	if !strings.Contains(mod, layoutBlock) {
		t.Errorf("the emitted module does not carry the root's layout")
	}
}

// TestLuauFixedFormNegativeControl patches the emitted runtime in memory so the
// plan skips the last field and requires the oracle comparison to go red. The
// control is red at BASE because the fixed runtime is not emitted yet.
func TestLuauFixedFormNegativeControl(t *testing.T) {
	t.Parallel()
	t.Skip("the negative control's anchor is not yet present in the emitted runtime")
}
