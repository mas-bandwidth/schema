package slowtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spawnPath is one function on a path to a subprocess: spawn is the text by
// which it starts one (directly, or through a helper that does), and gate is
// the call that must come before it. An empty spawn means the first exec entry
// point in toolchainMarkers; an empty gate means slowtest.Gate.
type spawnPath struct {
	file, fn, spawn, gate string
}

// spawnPaths are the shared runner doors every interpreted leg's tests go
// through, and the tests that reached a subprocess without passing a gate
// first because the spawn sat behind a helper or an argument the file-level
// scan cannot see. A runner door that gates covers every caller, present and
// future, so a test that reaches gooserun, luarun, luaurun, lobsterrun, moorun,
// pyrun, sqrun or fixedproof.CppOracle skips in the default tier.
var spawnPaths = []spawnPath{
	{file: "internal/codegen/goose/gooserun/command.go", fn: "Command"},
	{file: "internal/codegen/lua/luarun/luarun.go", fn: "Try"},
	{file: "internal/codegen/luau/luaurun/luaurun.go", fn: "Run"},
	{file: "internal/codegen/luau/luaurun/luaurun.go", fn: "Analyze"},
	{file: "internal/codegen/lobster/lobsterrun/lobsterrun.go", fn: "runFlags"},
	{file: "internal/codegen/moo/moorun/moorun.go", fn: "RunErr"},
	{file: "internal/codegen/moo/moorun/moorun.go", fn: "Analyze"},
	{file: "internal/codegen/python/pyrun/pyrun.go", fn: "Try"},
	{file: "internal/codegen/python/pyrun/pyrun.go", fn: "Analyze"},
	{file: "internal/codegen/squirrel/sqrun/sqrun.go", fn: "interpreterFor", spawn: "Interpreter()"}, // resolves the binary Run then spawns
	{file: "internal/codegen/squirrel/sqrun/sqrun.go", fn: "Run", gate: "interpreterFor(t)"},
	{file: "internal/fixedproof/fixedproof.go", fn: "CppOracle"},
	{file: "internal/codegen/goosetable/fixed_proof_strict_test.go", fn: "TestGooseFixedProofSuiteStrict", spawn: "buildAndRunCppFixedOracle("},
	{file: "internal/codegen/goosetable/fixed_proof_strict_test.go", fn: "TestGooseFixedProofSuiteStrictNegativeControl", spawn: "compileAndRunCppFixedOracleFiles("},
	{file: "internal/codegen/ctable/identity_test.go", fn: "identityCCFlags", spawn: "gccFortifySilence("},
	{file: "internal/codegen/gotable/fixedunionregional_test.go", fn: "TestGoRegionalUnitUnionCarriesFixedForm", spawn: "memceiling.Guard("},
	{file: "internal/codegen/gotable/fixedunionregional_test.go", fn: "TestGoRegionalUnionWriteRefusalNegativeControl", spawn: "memceiling.Guard("},
	{file: "compiler/newleg_test.go", fn: "TestNewLegBenchScaffoldIsUnavailable"},
	{file: "test/conformance/harness/announcegrown_test.go", fn: "TestLimitTheGrownRowsReachADriver", spawn: "materialise("},
	{file: "bench/paired/window_test.go", fn: "TestQuietWindowStopsOwnedRunnerAndRetainsRefusal", spawn: "w.capture("},
	{file: "test/conformance/go/rows/R6_test.go", fn: "TestRowR6", gate: "f8Gate(t)"},
	{file: "test/conformance/go/rows/W13_test.go", fn: "TestRowW13", gate: "f8Gate(t)"},
}

// TestSpawnPathsGateBeforeTheySpawn holds every function in spawnPaths to its
// gate: the gate is called, the spawn is still there (so the entry cannot pass
// by naming nothing), and the gate comes first.
func TestSpawnPathsGateBeforeTheySpawn(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range spawnPaths {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p.file)))
		if err != nil {
			t.Fatal(err)
		}
		if why := gatesBeforeSpawn(src, p); why != "" {
			t.Errorf("%s: %s %s", p.file, p.fn, why)
		}
	}
}

// TestSpawnPathsNegativeControl plants the verdicts gatesBeforeSpawn must
// tell apart: gated first passes; no gate, a gate after the spawn, a missing
// spawn and a missing function each fail.
func TestSpawnPathsNegativeControl(t *testing.T) {
	t.Parallel()
	cases := []struct {
		src  string
		p    spawnPath
		pass bool
	}{
		{"package x\nfunc F(t T) {\n\tslowtest.Gate(t, \"cc\")\n\texec.Command(\"cc\")\n}\n", spawnPath{fn: "F"}, true},
		{"package x\nfunc F(t T) {\n\texec.Command(\"cc\")\n}\n", spawnPath{fn: "F"}, false},
		{"package x\nfunc F(t T) {\n\thelper(t)\n\tslowtest.Gate(t, \"cc\")\n}\n", spawnPath{fn: "F", spawn: "helper("}, false},
		{"package x\nfunc F(t T) {\n\tslowtest.Gate(t, \"cc\")\n}\n", spawnPath{fn: "F", spawn: "helper("}, false},
		{"package x\nfunc G(t T) {\n\tslowtest.Gate(t, \"cc\")\n\texec.Command(\"cc\")\n}\n", spawnPath{fn: "F"}, false},
		{"package x\nfunc F(t T) {\n\tgate(t)\n\texec.Command(\"cc\")\n}\n", spawnPath{fn: "F", gate: "gate(t)"}, true},
	}
	for i, c := range cases {
		c.p.file = "x.go"
		why := gatesBeforeSpawn([]byte(c.src), c.p)
		if (why == "") != c.pass {
			t.Errorf("case %d: verdict %q, want pass=%v", i, why, c.pass)
		}
	}
}

// gatesBeforeSpawn returns "" when p.fn in src calls its gate before its
// spawn, and the reason otherwise.
func gatesBeforeSpawn(src []byte, p spawnPath) string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, p.file, src, 0)
	if err != nil {
		return err.Error()
	}
	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == p.fn && fn.Body != nil {
			body = fn.Body
			break
		}
	}
	if body == nil {
		return "is not found"
	}
	text := string(src[fset.Position(body.Lbrace).Offset:fset.Position(body.Rbrace).Offset])
	spawn := -1
	if p.spawn != "" {
		spawn = strings.Index(text, p.spawn)
	} else {
		for _, marker := range toolchainMarkers {
			if i := strings.Index(text, marker); i >= 0 && (spawn < 0 || i < spawn) {
				spawn = i
			}
		}
	}
	if spawn < 0 {
		return "no longer reaches the spawn this entry names: update spawnPaths"
	}
	gate := p.gate
	if gate == "" {
		gate = "slowtest.Gate("
	}
	at := strings.Index(text, gate)
	if at < 0 || at > spawn {
		return "starts a subprocess before calling " + gate + ": a default `go test` (SCHEMA_SLOW unset) pays for it"
	}
	return ""
}
