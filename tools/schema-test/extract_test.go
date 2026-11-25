package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// extractRoot is a repository of one Go test, with the CLI tests' registry
// and a ledger that counts the test and gives it a tier line, written to a
// temporary directory: the test writes nothing in the tree.
func extractRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	reg, err := os.ReadFile(cliRegistry)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"go.mod":            "module example.com/fx\n\ngo 1.26\n",
		"pkg/a_test.go":     "package pkg\n\nimport \"testing\"\n\nfunc TestNeeds(t *testing.T) {}\n",
		testreg.DefaultPath: string(reg),
		extract.LedgerPath:  "# h\ngo-test\tpkg\t1\tL3\twaits\ntier\tpkg:TestNeeds\t1\tL3\tfunctional: needs cc\n",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestWriteLedgerKeepsTheTierLines: --write-ledger counts the unmoved tests
// again and carries every tier line over as it is, so the account still
// passes, counts the tiers, and --json gives the test its tier.
func TestWriteLedgerKeepsTheTierLines(t *testing.T) {
	t.Parallel()
	root := extractRoot(t)
	var out bytes.Buffer
	if err := extractCmd([]string{"--root", root, "--write-ledger"}, &out); err != nil {
		t.Fatal(err)
	}
	lines, err := extract.LoadLedger(filepath.Join(root, extract.LedgerPath))
	if err != nil {
		t.Fatal(err)
	}
	tiers := extract.TierLines(lines)
	if len(tiers) != 1 || tiers[0].Dir != "pkg:TestNeeds" || tiers[0].Count != 1 || tiers[0].Reason != "functional: needs cc" {
		t.Fatalf("the rewritten ledger's tier lines: %+v", tiers)
	}
	out.Reset()
	if err := extractCmd([]string{"--root", root}, &out); err != nil {
		t.Fatalf("the rewritten ledger fails its own check: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "tier lines (unmoved tests that are not unit): 1 functional, 0 nightly") {
		t.Errorf("the counts do not name the tier lines:\n%s", out.String())
	}
	out.Reset()
	if err := extractCmd([]string{"--root", root, "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"name":"TestNeeds"`) || !strings.Contains(out.String(), `"tier":"functional"`) {
		t.Errorf("--json does not give the test its tier line's tier:\n%s", out.String())
	}
}

// TestExtractNamesANewTestSinceMain: with --since naming main's tree, a Go
// test added without a row fails the command by its own name.
func TestExtractNamesANewTestSinceMain(t *testing.T) {
	t.Parallel()
	main, root := extractRoot(t), extractRoot(t)
	var out bytes.Buffer
	if err := extractCmd([]string{"--root", root, "--since", main}, &out); err != nil {
		t.Fatalf("the same tree: %v\n%s", err, out.String())
	}
	planted := "package pkg\n\nimport \"testing\"\n\nfunc TestPlantedNew(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(root, "pkg", "z_test.go"), []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err := extractCmd([]string{"--root", root, "--since", main}, &out)
	if err == nil || !strings.Contains(out.String(), "go-test pkg TestPlantedNew at pkg/z_test.go:5 is a new test with no registry row") {
		t.Fatalf("a new test without a row was not named (err %v):\n%s", err, out.String())
	}
}

// TestExtractRefusesATestThatSkips: a Go test that reaches a Skip fails the
// command by name; no ledger line permits one.
func TestExtractRefusesATestThatSkips(t *testing.T) {
	t.Parallel()
	root := extractRoot(t)
	var out bytes.Buffer
	if err := extractCmd([]string{"--root", root}, &out); err != nil {
		t.Fatalf("the tree without a skip: %v\n%s", err, out.String())
	}
	planted := "package pkg\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestNeeds(t *testing.T) {\n\tif os.Getenv(\"X\") == \"\" {\n\t\tt.Skip(\"x\")\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(root, "pkg", "a_test.go"), []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err := extractCmd([]string{"--root", root}, &out)
	if err == nil || !strings.Contains(out.String(), `pkg TestNeeds reaches a skip at pkg/a_test.go:10 if os.Getenv("X") == "": no test skips`) {
		t.Fatalf("a test that skips passed (err %v):\n%s", err, out.String())
	}
}

// TestExtractHoldsTheLedgerToTheTree: the command fails on a ledger whose
// count disagrees with the tree, by the line.
func TestExtractHoldsTheLedgerToTheTree(t *testing.T) {
	t.Parallel()
	root := extractRoot(t)
	ledger := filepath.Join(root, extract.LedgerPath)
	if err := os.WriteFile(ledger, []byte("# h\ngo-test\tpkg\t2\tL3\twaits\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := extractCmd([]string{"--root", root}, &out)
	if err == nil || !strings.Contains(out.String(), "FAIL extract: ") || !strings.Contains(out.String(), "go-test pkg") {
		t.Fatalf("a ledger that miscounts passed (err %v):\n%s", err, out.String())
	}
}

// TestExtractRefusesAGrownLedger: against main's ledger, a count line that
// is new fails the command by name.
func TestExtractRefusesAGrownLedger(t *testing.T) {
	t.Parallel()
	root := extractRoot(t)
	main := filepath.Join(t.TempDir(), "unmoved-main.tsv")
	if err := os.WriteFile(main, []byte("# h\ngo-test\tother\t1\tL3\twaits\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := extractCmd([]string{"--root", root, "--shrinks-from", main}, &out)
	if err == nil || !strings.Contains(out.String(), "go-test pkg is a new ledger line") {
		t.Fatalf("a grown ledger passed (err %v):\n%s", err, out.String())
	}
}

// TestWriteLedgerRefusesAnUnreadableTree: --write-ledger writes nothing while
// a file does not parse, and says which.
func TestWriteLedgerRefusesAnUnreadableTree(t *testing.T) {
	t.Parallel()
	root := extractRoot(t)
	if err := os.WriteFile(filepath.Join(root, "pkg", "bad_test.go"), []byte("package pkg\n\nfunc {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(root, extract.LedgerPath)
	before, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = extractCmd([]string{"--root", root, "--write-ledger"}, &out)
	if err == nil || !strings.Contains(err.Error(), "pkg/bad_test.go") {
		t.Fatalf("a write over an unreadable tree: %v", err)
	}
	after, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("the ledger was rewritten:\n%s", after)
	}
}

// TestNameBuildabilityIsL4s: the name-buildability build, whole or split by
// target, is L4's to give rows, so --move L3 never gives it one; the
// package's other tests are L3's. The plant is the routing removed: the
// build reads as L3 and is named.
func TestNameBuildabilityIsL4s(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, want string }{
		{"TestNameBuildability", "L4"},
		{"TestNameBuildability_rust", "L4"},
		{"TestPlanCoversTheCorpus", "L3"},
	} {
		got, _ := goTestLayer(t.TempDir(), extract.Item{Kind: extract.KindGoTest, Dir: "tools/namebuild", Name: c.name})
		if got != c.want {
			t.Errorf("tools/namebuild %s is layer %s, want %s", c.name, got, c.want)
		}
	}
}

// TestExtractRefusesAStaleTierLine: a test renamed without its tier line
// fails the command: the line names a test the tree does not hold.
func TestExtractRefusesAStaleTierLine(t *testing.T) {
	t.Parallel()
	root := extractRoot(t)
	src := filepath.Join(root, "pkg", "a_test.go")
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, bytes.Replace(body, []byte("TestNeeds"), []byte("TestRenamed"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = extractCmd([]string{"--root", root}, &out)
	if err == nil || !strings.Contains(out.String(), "tier pkg:TestNeeds names a Go test the repository does not hold") {
		t.Fatalf("a stale tier line passed (err %v):\n%s", err, out.String())
	}
}

// TestMoveCarriesTheTierAndDropsItsLine: --move gives a test with a tier line
// a row of that tier, and the rewritten ledger drops the line, since the row
// carries the tier from then on; the account passes its own check. The plant
// is the rewrite carrying every tier line over: Check then names the line.
func TestMoveCarriesTheTierAndDropsItsLine(t *testing.T) {
	t.Parallel()
	root := extractRoot(t)
	// a second test, with no tier line: its row is unit
	if err := os.WriteFile(filepath.Join(root, "pkg", "b_test.go"), []byte("package pkg\n\nimport \"testing\"\n\nfunc TestPlainUnit(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ledgerFile := filepath.Join(root, extract.LedgerPath)
	if err := os.WriteFile(ledgerFile, []byte("# h\ngo-test\tpkg\t2\tL3\twaits\ntier\tpkg:TestNeeds\t1\tL3\tfunctional: needs cc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// --move gives a Go test outside tools/ the compiler type; the CLI
	// fixture declares no unit tier
	regPath := filepath.Join(root, testreg.DefaultPath)
	regData, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(regPath, append(regData, `{"rec":"type","name":"compiler","what":"the compiler itself"}`+"\n"+`{"rec":"tier","name":"unit","what":"no toolchain"}`+"\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := extractCmd([]string{"--root", root, "--move", "L3"}, &out); err != nil {
		t.Fatalf("--move L3: %v\n%s", err, out.String())
	}
	reg, err := testreg.Load(filepath.Join(root, testreg.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	tiers := map[string]string{}
	for _, tt := range reg.Tests {
		for _, name := range []string{"TestNeeds", "TestPlainUnit"} {
			if strings.Contains(string(tt.Inputs), `"`+name+`"`) {
				tiers[name] = tt.Tier
			}
		}
	}
	if tiers["TestNeeds"] != "functional" || tiers["TestPlainUnit"] != "unit" {
		t.Fatalf("the moved rows' tiers are %v, want TestNeeds functional (its tier line) and TestPlainUnit unit", tiers)
	}
	lines, err := extract.LoadLedger(filepath.Join(root, extract.LedgerPath))
	if err != nil {
		t.Fatal(err)
	}
	if tiers := extract.TierLines(lines); len(tiers) != 0 {
		t.Fatalf("the moved test's tier line stayed: %+v", tiers)
	}
	out.Reset()
	if err := extractCmd([]string{"--root", root}, &out); err != nil {
		t.Fatalf("the account after --move fails its own check: %v\n%s", err, out.String())
	}
	// the plant: the old tier line carried over beside the row
	ledger := filepath.Join(root, extract.LedgerPath)
	data, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledger, append(data, "tier\tpkg:TestNeeds\t1\tL3\tfunctional: needs cc\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := extractCmd([]string{"--root", root}, &out); err == nil || !strings.Contains(out.String(), "tier pkg:TestNeeds names a test registry row") {
		t.Fatalf("a tier line beside its test's row passed (err %v):\n%s", err, out.String())
	}
}

// TestALanguagePackagesBenchmarksWaitForTheBenchAdapter: the Go tests of a
// language package wait for that language (L5), but when all that is left of
// it is benchmarks, they wait for the bench adapter (L6) like any other.
func TestALanguagePackagesBenchmarksWaitForTheBenchAdapter(t *testing.T) {
	t.Parallel()
	p := plan(t.TempDir(), &testreg.Registry{Langs: []testreg.Lang{{Name: "lua"}}})
	bench := extract.Item{Kind: extract.KindGoTest, Dir: "internal/codegen/lua", Name: "BenchmarkLuaBlock"}
	test := extract.Item{Kind: extract.KindGoTest, Dir: "internal/codegen/lua", Name: "TestLuaBlock"}
	if layer, reason := p(extract.KindGoTest, bench.Dir, []extract.Item{bench}); layer != "L6" || !strings.Contains(reason, "bench adapter") {
		t.Errorf("benchmarks only: %s %q, want L6, the bench adapter", layer, reason)
	}
	if layer, reason := p(extract.KindGoTest, bench.Dir, []extract.Item{bench, test}); layer != "L5" || !strings.Contains(reason, "per-language (lua)") {
		t.Errorf("a test and a benchmark: %s %q, want L5 per-language", layer, reason)
	}
}
