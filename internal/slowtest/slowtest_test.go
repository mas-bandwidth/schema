package slowtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestEveryToolchainTestIsBehindTheGate is the red test for issue #1025: a
// package whose tests shell out to a foreign compiler costs seconds per test,
// every run, and a package full of them costs minutes. `go test ./compiler/`
// was 261 s and `go test ./internal/codegen/gotable/` 41 s on the card's own
// machine, against the owner's one-to-two-minute rule. The cause is not Go; it
// is cc, c++, dotnet and the Go toolchain, invoked by a test that never says
// it is about to spend somebody else's compiler.
//
// The gate the fix adds is internal/slowtest.Gate. This test holds the two
// lists together: every test source under compiler/ and internal/codegen/ that
// reaches a foreign toolchain must carry a Gate call, so a new expensive test
// cannot land ungated and quietly push the per-commit run back over the rule.
//
// The scan once covered only compiler/ and internal/codegen/gotable/ with a
// handful of named toolchains, and the message-depth landing (f2dcef6) put an
// ungated rustc, javac, dotnet, node, dart and elixir probe into seven other
// codegen packages without tripping it. It now covers every package under
// both roots and any exec.Command, exec.CommandContext or exec.LookPath.
// The sources that were ungated when it widened are named in
// testdata/ungated-toolchain-tests.txt. That list is a ratchet: a new source
// off it fails, and an entry that is gated, gone, or no longer reaches a
// toolchain fails too, so the list only shrinks.
func TestEveryToolchainTestIsBehindTheGate(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := readUngatedBaseline(filepath.Join("testdata", "ungated-toolchain-tests.txt"))
	if err != nil {
		t.Fatal(err)
	}
	scan, err := scanToolchainTests(root, toolchainScopes)
	if err != nil {
		t.Fatal(err)
	}
	if scan.scanned == 0 {
		t.Fatal("no toolchain test sources found — this gate is looking at the wrong text")
	}
	fresh, stale := ratchetUngated(scan, baseline)
	for _, rel := range fresh {
		t.Errorf("%s reaches a foreign toolchain but never calls slowtest.Gate: a default `go test` pays for that compiler on every run (issue #1025)", rel)
	}
	for _, rel := range stale {
		t.Errorf("%s is named in testdata/ungated-toolchain-tests.txt but is no longer an ungated toolchain test: remove it from the list so it cannot regress", rel)
	}
	t.Logf("toolchain test sources: %d scanned, %d behind the gate, %d on the ungated list", scan.scanned, scan.scanned-len(scan.ungated), len(baseline))
}

// TestToolchainScanNegativeControl plants the three verdicts the scan must
// tell apart in a scratch tree: a gated toolchain test passes, an ungated one
// off the list is named, a plain test is not counted, and a list entry that has
// since been gated is named as stale. A scan that matched nothing, or that
// trusted the list without checking it, fails here.
func TestToolchainScanNegativeControl(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := map[string]string{
		"internal/codegen/xtable/gated_test.go":      "package xtable\nfunc TestA(t *testing.T) {\n\tslowtest.Gate(t, \"cc\")\n\texec.Command(\"cc\")\n}\n",
		"internal/codegen/xtable/ungated_test.go":    "package xtable\nfunc TestB(t *testing.T) {\n\texec.Command(\"rustc\")\n}\n",
		"internal/codegen/xtable/listed_test.go":     "package xtable\nfunc TestC(t *testing.T) {\n\texec.LookPath(\"node\")\n}\n",
		"internal/codegen/xtable/nowgated_test.go":   "package xtable\nfunc TestD(t *testing.T) {\n\tslowtest.Gate(t, \"dart\")\n\texec.CommandContext(ctx, \"dart\")\n}\n",
		"internal/codegen/xtable/plain_test.go":      "package xtable\nfunc TestE(t *testing.T) {}\n",
		"internal/codegen/xtable/testdata/x_test.go": "package x\nfunc TestF(t *testing.T) {\n\texec.Command(\"cc\")\n}\n",
		"compiler/deep/sub/ungated_nested_test.go":   "package sub\nfunc TestG(t *testing.T) {\n\tfindDotnet(t)\n}\n",
	}
	for rel, text := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	scan, err := scanToolchainTests(root, toolchainScopes)
	if err != nil {
		t.Fatal(err)
	}
	if scan.scanned != 5 {
		t.Errorf("scanned %d toolchain sources, want 5 (plain and testdata/ excluded)", scan.scanned)
	}
	baseline := map[string]bool{
		"internal/codegen/xtable/listed_test.go":   true,
		"internal/codegen/xtable/nowgated_test.go": true,
		"internal/codegen/xtable/deleted_test.go":  true,
	}
	fresh, stale := ratchetUngated(scan, baseline)
	if want := []string{"compiler/deep/sub/ungated_nested_test.go", "internal/codegen/xtable/ungated_test.go"}; !slices.Equal(fresh, want) {
		t.Errorf("ungated off the list = %v, want %v", fresh, want)
	}
	if want := []string{"internal/codegen/xtable/deleted_test.go", "internal/codegen/xtable/nowgated_test.go"}; !slices.Equal(stale, want) {
		t.Errorf("stale list entries = %v, want %v", stale, want)
	}
}

var toolchainScopes = []string{"compiler", filepath.Join("internal", "codegen")}

// toolchainMarkers is any text by which a test source reaches a program
// outside the Go process: every exec entry point, and the dotnet finder that
// wraps one.
var toolchainMarkers = []string{
	"exec.Command(",
	"exec.CommandContext(",
	"exec.LookPath(",
	"findDotnet",
}

type toolchainScan struct {
	scanned int
	ungated []string // slash-separated, relative to the repository root, sorted
}

func scanToolchainTests(root string, scopes []string) (toolchainScan, error) {
	var scan toolchainScan
	for _, scope := range scopes {
		err := filepath.WalkDir(filepath.Join(root, scope), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(data)
			if !slices.ContainsFunc(toolchainMarkers, func(m string) bool { return strings.Contains(text, m) }) {
				return nil
			}
			scan.scanned++
			if !strings.Contains(text, "slowtest.Gate(") {
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				scan.ungated = append(scan.ungated, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return scan, err
		}
	}
	slices.Sort(scan.ungated)
	return scan, nil
}

// ratchetUngated splits the scan against the list: fresh is ungated and not
// listed, stale is listed and not ungated (gated since, or gone).
func ratchetUngated(scan toolchainScan, baseline map[string]bool) (fresh, stale []string) {
	ungated := map[string]bool{}
	for _, rel := range scan.ungated {
		ungated[rel] = true
		if !baseline[rel] {
			fresh = append(fresh, rel)
		}
	}
	for rel := range baseline {
		if !ungated[rel] {
			stale = append(stale, rel)
		}
	}
	slices.Sort(stale)
	return fresh, stale
}

func readUngatedBaseline(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	baseline := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		baseline[line] = true
	}
	return baseline, nil
}

// TestTheGotableToolchainTestsAreGated is the card's gate: the four gotable
// test sources that reach a foreign toolchain each carry a slowtest.Gate call,
// so a future ungated addition is caught here before it can push the
// per-commit run back over the rule.
func TestTheGotableToolchainTestsAreGated(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	files := []string{
		"internal/codegen/gotable/declaredforms_test.go",
		"internal/codegen/gotable/fixedplanmax_test.go",
		"internal/codegen/gotable/sharedruntime_test.go",
		"internal/codegen/gotable/subsets_test.go",
	}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "slowtest.Gate(") {
			t.Errorf("%s reaches a foreign toolchain but does not call slowtest.Gate", file)
		}
	}
}
