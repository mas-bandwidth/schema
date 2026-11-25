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

// extractRoot is a repository of one Go test holding one skip, with the CLI
// tests' registry and a ledger that counts the test, written to a temporary
// directory: the test writes nothing in the tree.
func extractRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	reg, err := os.ReadFile(cliRegistry)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"go.mod":            "module example.com/fx\n\ngo 1.26\n",
		"pkg/a_test.go":     "package pkg\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestHides(t *testing.T) {\n\tif os.Getenv(\"X\") == \"\" {\n\t\tt.Skip(\"x\")\n\t}\n}\n",
		testreg.DefaultPath: string(reg),
		extract.LedgerPath:  "# h\ngo-test\tpkg\t1\tL3\twaits\n",
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

// TestWriteLedgerGivesASkippingTestToL3: --write-ledger counts a test that
// still skips like any other language-free Go test, on L3's line (the gotest
// adapter runs it, and its skip fails), never on a later layer's; the account
// it writes passes its own check.
func TestWriteLedgerGivesASkippingTestToL3(t *testing.T) {
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
	if len(lines) != 1 || lines[0].Kind != extract.KindGoTest || lines[0].Dir != "pkg" || lines[0].Count != 1 || lines[0].Layer != "L3" {
		t.Fatalf("the rewritten ledger: %+v", lines)
	}
	out.Reset()
	if err := extractCmd([]string{"--root", root}, &out); err != nil {
		t.Fatalf("the rewritten ledger fails its own check: %v\n%s", err, out.String())
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

// TestExtractRefusesAGrownLedger: against main's ledger, a line that is new
// fails the command by name.
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
