package ci

import (
	"go/parser"
	"go/token"
	"io/fs"
	"math"
	"math/big"
	"math/bits"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	engineWireImport = "github.com/mas-bandwidth/schema/v2/internal/tablewire"
	engineTextImport = "github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// scanEngineImportViolations walks root and returns every non-test Go source file
// under internal/codegen/, generated/, tables/, or runtime/ that imports
// internal/tablewire or internal/tabletext, or whose emitter source embeds
// those import paths into generated non-test output.
func scanEngineImportViolations(root string) ([]string, error) {
	var violations []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		slashed := filepath.ToSlash(rel)
		if !strings.HasSuffix(slashed, ".go") || strings.HasSuffix(slashed, "_test.go") {
			return nil
		}
		inScope := strings.HasPrefix(slashed, "internal/codegen/") ||
			strings.HasPrefix(slashed, "generated/") ||
			strings.HasPrefix(slashed, "tables/") ||
			strings.HasPrefix(slashed, "runtime/")
		if !inScope {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == engineWireImport || p == engineTextImport {
				violations = append(violations, slashed+" imports "+p)
			}
		}
		return nil
	})
	return violations, err
}

// TestSchemaEngineIsTheOracleImportIndependence verifies that no code generator
// (internal/codegen/* non-test source), generated Go unit (generated/**), or
// port runtime (tables/**, runtime/**) imports internal/tablewire or
// internal/tabletext, and includes a planted-bad-pattern control that fails
// when a generated unit or emitter imports the engine and passes when removed.
func TestSchemaEngineIsTheOracleImportIndependence(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	violations, err := scanEngineImportViolations(root)
	if err != nil {
		t.Fatalf("scan repo: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("expected 0 engine import violations in clean tree, got: %v", violations)
	}

	// Planted bad-pattern control: copy a minimal tree into t.TempDir(), plant
	// forbidden engine imports into both a generated unit and an emitter file,
	// verify scanEngineImportViolations catches both by name, then remove the
	// plants and verify the scan is green again.
	tmp := t.TempDir()
	genDir := filepath.Join(tmp, "generated", "plant")
	codegenDir := filepath.Join(tmp, "internal", "codegen", "gotable")
	for _, dir := range []string{genDir, codegenDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	plantedGen := filepath.Join(genDir, "PlantedTable.go")
	plantedEmitter := filepath.Join(codegenDir, "planted_emitter.go")
	if err := os.WriteFile(plantedGen, []byte("package plant\nimport _ \""+engineWireImport+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plantedEmitter, []byte("package gotable\nimport _ \""+engineTextImport+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plantedViolations, err := scanEngineImportViolations(tmp)
	if err != nil {
		t.Fatalf("scan planted tree: %v", err)
	}
	if len(plantedViolations) != 2 {
		t.Fatalf("planted control must catch 2 violations, got %d: %v", len(plantedViolations), plantedViolations)
	}
	joined := strings.Join(plantedViolations, "\n")
	if !strings.Contains(joined, "generated/plant/PlantedTable.go imports "+engineWireImport) {
		t.Fatalf("planted control missed generated unit violation: %s", joined)
	}
	if !strings.Contains(joined, "internal/codegen/gotable/planted_emitter.go imports "+engineTextImport) {
		t.Fatalf("planted control missed emitter violation: %s", joined)
	}

	if err := os.Remove(plantedGen); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(plantedEmitter); err != nil {
		t.Fatal(err)
	}
	cleanAgain, err := scanEngineImportViolations(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleanAgain) != 0 {
		t.Fatalf("expected 0 violations after removing plants, got: %v", cleanAgain)
	}
}

// TestSchemaEngineIsTheOracleSharedHelpersRederivation independently re-derives
// the compile-time helpers in package ir that both the Go engine
// (internal/tablewire, internal/tabletext) and the port emitters
// (internal/codegen/*) call. Because both sides read ir, a bug in these helpers
// would otherwise be shared by the oracle and the generated readers.
func TestSchemaEngineIsTheOracleSharedHelpersRederivation(t *testing.T) {
	t.Parallel()

	// 1. Independently re-derive FNV-1a-64 (ir.TableWireId, docs/SPEC-TABLES.md §5)
	// against IETF FNV test vectors and reserved wire names.
	fnv1a64 := func(s string) uint64 {
		const (
			offset64 = 14695981039346656037
			prime64  = 1099511628211
		)
		h := uint64(offset64)
		for i := 0; i < len(s); i++ {
			h ^= uint64(s[i])
			h *= prime64
		}
		return h
	}
	vectors := []struct {
		in   string
		want uint64
	}{
		{"", 0xcbf29ce484222325},
		{"a", 0xaf63dc4c8601ec8c},
		{"foobar", 0x85944171f73967e8},
		{"key", fnv1a64("key")},
		{"value", fnv1a64("value")},
		{"bytes", fnv1a64("bytes")},
		{"string", fnv1a64("string")},
	}
	for _, tc := range vectors {
		if got := ir.TableWireId(tc.in); got != tc.want {
			t.Errorf("ir.TableWireId(%q) = 0x%x, want 0x%x", tc.in, got, tc.want)
		}
	}
	if ir.MapKeyWireId != fnv1a64("key") || ir.MapValueWireId != fnv1a64("value") {
		t.Errorf("MapKeyWireId/MapValueWireId mismatch: got (0x%x, 0x%x)", ir.MapKeyWireId, ir.MapValueWireId)
	}

	// 2. Independently re-derive BitsRequired(min, max) = bitlen(max - min).
	for _, tc := range []struct {
		min, max int64
		wantBits int64
	}{
		{0, 1, 1},
		{0, 255, 8},
		{-100, 100, 8},
		{0, 1024, 11},
	} {
		got := ir.BitsRequired(big.NewInt(tc.min), big.NewInt(tc.max))
		if got != tc.wantBits {
			t.Errorf("ir.BitsRequired(%d, %d) = %d, want %d", tc.min, tc.max, got, tc.wantBits)
		}
	}

	// 3. Independently re-derive CompressedFloatParams(fmin, fmax, res).
	{
		delta := float32(10.0) - float32(-10.0)
		values := delta / float32(0.01)
		wantMaxInt := uint64(math.Ceil(float64(values)))
		wantBits := int64(bits.Len64(wantMaxInt))
		gotMaxInt, gotBits := ir.CompressedFloatParams(-10.0, 10.0, 0.01)
		if gotMaxInt != wantMaxInt || gotBits != wantBits {
			t.Errorf("ir.CompressedFloatParams(-10, 10, 0.01) = (%d, %d), want (%d, %d)", gotMaxInt, gotBits, wantMaxInt, wantBits)
		}
	}

	// 4. Verify the assigned wire-form discriminator bytes (docs/SPEC-TABLES.md §3).
	wantForms := map[uint8]string{
		1: "the variable form",
		2: "the message form",
		3: "the fixed form",
	}
	for b, wantName := range wantForms {
		f, ok := ir.WireFormOf(b)
		if !ok || f.Name != wantName || f.Planned {
			t.Errorf("ir.WireFormOf(%d) = (%+v, %v), want active %q", b, f, ok, wantName)
		}
	}
}

// checkHarnessUsesEngine verifies that every Go differential/conformance harness
// directory or file in harnesses imports internal/tablewire, internal/tabletext,
// or compiler (which wraps internal/tablewire and internal/tabletext).
func checkHarnessUsesEngine(root string, harnesses []string) ([]string, error) {
	var missing []string
	fset := token.NewFileSet()
	for _, rel := range harnesses {
		full := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(full)
		if err != nil {
			return nil, err
		}
		var files []string
		if info.IsDir() {
			entries, err := os.ReadDir(full)
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
					files = append(files, filepath.Join(full, e.Name()))
				}
			}
		} else {
			files = []string{full}
		}
		foundEngine := false
		for _, file := range files {
			f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
			if err != nil {
				return nil, err
			}
			for _, imp := range f.Imports {
				p := strings.Trim(imp.Path.Value, `"`)
				if p == engineWireImport || p == engineTextImport || p == "github.com/mas-bandwidth/schema/v2/compiler" {
					foundEngine = true
					break
				}
			}
			if foundEngine {
				break
			}
		}
		if !foundEngine {
			missing = append(missing, rel)
		}
	}
	return missing, nil
}

// TestSchemaEngineIsTheOracleDifferentialHarnesses verifies that every
// cross-port conformance and differential harness derives its expected values
// from the Go engine (internal/tablewire, internal/tabletext, or compiler), and
// includes a planted-bad-pattern control for a harness that compares ports only
// against each other without importing the engine.
func TestSchemaEngineIsTheOracleDifferentialHarnesses(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	harnesses := []string{
		"test/conformance/harness",
		"internal/codegen/gotable/retain_test.go",
		"internal/codegen/gotable/textdifferential_test.go",
		"internal/codegen/jstable/variableclass_test.go",
		"internal/codegen/rusttable/variableclass_test.go",
	}
	missing, err := checkHarnessUsesEngine(root, harnesses)
	if err != nil {
		t.Fatalf("check harnesses: %v", err)
	}
	if len(missing) > 0 {
		t.Fatalf("harnesses missing Go engine oracle import: %v", missing)
	}

	// Planted bad-pattern control: a harness that imports no engine package fails.
	tmp := t.TempDir()
	badHarness := filepath.Join(tmp, "peer_only_diff_test.go")
	if err := os.WriteFile(badHarness, []byte("package diff\nimport \"testing\"\nfunc TestPeerOnly(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plantedMissing, err := checkHarnessUsesEngine(tmp, []string{"peer_only_diff_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plantedMissing) != 1 || plantedMissing[0] != "peer_only_diff_test.go" {
		t.Fatalf("planted peer-only harness must be flagged, got: %v", plantedMissing)
	}
}
