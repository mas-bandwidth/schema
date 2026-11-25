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
// tests' registry and a ledger that counts the test and its skip-red line,
// written to a temporary directory: the test writes nothing in the tree.
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
		extract.LedgerPath:  "# h\ngo-test\tpkg\t1\tL3\twaits\nskip-red\tpkg:TestHides\t1\tL3\tmeasured red\n",
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

// TestWriteLedgerKeepsTheSkipRedLines: --write-ledger counts the unmoved
// tests again and carries every skip-red line over as it is, so the account
// still passes and still names the skip that hides a red.
func TestWriteLedgerKeepsTheSkipRedLines(t *testing.T) {
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
	reds := extract.SkipReds(lines)
	if len(reds) != 1 || reds[0].Dir != "pkg:TestHides" || reds[0].Count != 1 || reds[0].Reason != "measured red" {
		t.Fatalf("the rewritten ledger's skip-red lines: %+v", reds)
	}
	out.Reset()
	if err := extractCmd([]string{"--root", root}, &out); err != nil {
		t.Fatalf("the rewritten ledger fails its own check: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "skips that hide a red (skip-red lines): 1 tests, 1 skip sites") {
		t.Errorf("the counts do not name the skip-red account:\n%s", out.String())
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

// TestExtractRefusesADroppedSkipRedLine: against main's ledger, a skip-red
// line that left while its test still skips fails the command by name.
func TestExtractRefusesADroppedSkipRedLine(t *testing.T) {
	t.Parallel()
	root := extractRoot(t)
	main := filepath.Join(t.TempDir(), "unmoved-main.tsv")
	ledger := filepath.Join(root, extract.LedgerPath)
	data, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := extractCmd([]string{"--root", root, "--shrinks-from", main}, &out); err != nil {
		t.Fatalf("the unchanged ledger: %v\n%s", err, out.String())
	}
	if err := os.WriteFile(ledger, []byte("# h\ngo-test\tpkg\t1\tL3\twaits\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err = extractCmd([]string{"--root", root, "--shrinks-from", main}, &out)
	if err == nil || !strings.Contains(out.String(), "skip-red pkg:TestHides left the ledger, and the test still has 1 skips") {
		t.Fatalf("a dropped skip-red line passed (err %v):\n%s", err, out.String())
	}
}
