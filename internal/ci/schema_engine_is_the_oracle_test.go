package ci

import (
	"go/ast"
	"go/parser"
	"go/scanner"
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
	enginePackImport = "github.com/mas-bandwidth/schema/v2/internal/tablepack"
)

// scanEngineImportViolations walks root and returns every non-test Go source file
// under internal/codegen/, generated/, tables/, or runtime/ that (a) imports
// internal/tablewire or internal/tabletext in a Go import declaration, or (b)
// embeds either engine import path inside a string literal of its source — the
// way an emitter would bake the path into a template it emits into generated
// code. Comments are documentation, not dependencies, and are ignored.
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
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, parseErr := parser.ParseFile(fset, path, data, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		flagged := false
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == engineWireImport || p == engineTextImport {
				violations = append(violations, slashed+" imports "+p)
				flagged = true
			}
		}
		if !flagged {
			for _, s := range stringLiteralsOf(data) {
				if strings.Contains(s, engineWireImport) || strings.Contains(s, engineTextImport) {
					violations = append(violations, slashed+" embeds an engine import path")
					break
				}
			}
		}
		return nil
	})
	return violations, err
}

// stringLiteralsOf returns the text of every string literal in src, skipping
// comments. It uses the Go scanner so a raw string, an interpreted string and
// a backquoted source template are all seen, and a comment that merely cites an
// engine file is not.
func stringLiteralsOf(src []byte) []string {
	var out []string
	var s scanner.Scanner
	s.Init(token.NewFileSet().AddFile("", -1, len(src)), src, nil, 0)
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return out
		}
		if tok == token.STRING {
			out = append(out, lit)
		}
	}
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
	// The emitter plant hides the engine import inside a string literal (the way
	// a template would emit it), not in an import declaration, to prove the scan
	// catches embedded source imports and not only real Go imports.
	if err := os.WriteFile(plantedEmitter, []byte("package gotable\n\nconst generatedPreamble = `\nimport _ \""+engineTextImport+"\"\n`\n"), 0o644); err != nil {
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
	if !strings.Contains(joined, "internal/codegen/gotable/planted_emitter.go embeds an engine import path") {
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

// engineOracleHarnesses are the cross-port differential/conformance harnesses
// that derive their expected values AT TEST TIME from the Go engine
// (internal/tablewire, internal/tabletext, internal/tablepack). Each is
// verified below to import AND reference the engine, so a harness that drifts
// to committed pins or port-vs-port comparison cannot pass this gate.
//
// This is a deliberate, bounded inventory — not an "every port" claim. The
// conformance matrix (test/conformance/harness) is excluded on purpose: its
// expected values are committed goldens written by the REFERENCE leg (C++, by
// registry order) via `harness pin`, or by `harness generate` from the engine,
// and `harness run` reads those committed pins rather than calling the engine
// live. It is a shared-pin matrix, not an engine-runtime oracle, so it is not
// listed here.
var engineOracleHarnesses = []string{
	"internal/codegen/gotable/textdifferential_test.go",
	"internal/codegen/gotable/cookwrite_test.go",
	"internal/codegen/gotable/message_test.go",
	"internal/codegen/gotable/retain_test.go",
	"internal/codegen/jstable/variableclass_test.go",
	"internal/codegen/jstable/cardsretain_test.go",
	"internal/codegen/rusttable/variableclass_test.go",
	"internal/codegen/rusttable/cardsretain_test.go",
}

// checkHarnessUsesEngine verifies that every harness in harnesses derives its
// expected values from the Go engine. A harness passes only if at least one of
// its Go files both imports an engine package AND references an engine package
// identifier in its body; a file that merely cites the engine in a comment, or
// imports it without calling into it, is not proof of an engine oracle.
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
			if usesEngineForOracle(fset, file) {
				foundEngine = true
				break
			}
		}
		if !foundEngine {
			missing = append(missing, rel)
		}
	}
	return missing, nil
}

// usesEngineForOracle reports whether the Go source file imports an engine
// package and references its exported identifiers in its body.
func usesEngineForOracle(fset *token.FileSet, file string) bool {
	f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
	if err != nil {
		return false
	}
	importsEngine := false
	for _, imp := range f.Imports {
		switch strings.Trim(imp.Path.Value, `"`) {
		case engineWireImport, engineTextImport, enginePackImport:
			importsEngine = true
		}
	}
	if !importsEngine {
		return false
	}
	usesEngine := false
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		switch ident.Name {
		case "tablewire", "tabletext", "tablepack":
			usesEngine = true
		}
		return true
	})
	return usesEngine
}

// TestSchemaEngineIsTheOracleDifferentialHarnesses verifies that every Go
// differential harness in engineOracleHarnesses derives its expected values from
// the Go engine (internal/tablewire, internal/tabletext, internal/tablepack),
// and includes a planted-bad-pattern control for a harness that compares ports
// only against each other without using the engine. The conformance matrix is
// deliberately not listed here: it is a committed-pin matrix (reference leg =
// C++) rather than an engine-runtime oracle.
func TestSchemaEngineIsTheOracleDifferentialHarnesses(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	missing, err := checkHarnessUsesEngine(root, engineOracleHarnesses)
	if err != nil {
		t.Fatalf("check harnesses: %v", err)
	}
	if len(missing) > 0 {
		t.Fatalf("engine-oracle harnesses missing a Go engine import/use: %v", missing)
	}

	// Planted bad-pattern control: a harness that imports and uses no engine
	// package fails.
	tmp := t.TempDir()
	badHarness := filepath.Join(tmp, "remote_only_diff_test.go")
	if err := os.WriteFile(badHarness, []byte("package diff\nimport \"testing\"\nfunc TestRemoteOnly(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plantedMissing, err := checkHarnessUsesEngine(tmp, []string{"remote_only_diff_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plantedMissing) != 1 || plantedMissing[0] != "remote_only_diff_test.go" {
		t.Fatalf("planted endpoint-only harness must be flagged, got: %v", plantedMissing)
	}

	// A harness that imports the engine but never calls into it is also not an
	// engine oracle: the import alone must not be taken as proof.
	unusedImport := filepath.Join(tmp, "unused_engine_import_test.go")
	if err := os.WriteFile(unusedImport, []byte("package diff\nimport \"testing\"\nimport _ \""+engineWireImport+"\"\nfunc TestUnused(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plantedUnused, err := checkHarnessUsesEngine(tmp, []string{"unused_engine_import_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plantedUnused) != 1 || plantedUnused[0] != "unused_engine_import_test.go" {
		t.Fatalf("a harness with an unused engine import must be flagged, got: %v", plantedUnused)
	}
}
