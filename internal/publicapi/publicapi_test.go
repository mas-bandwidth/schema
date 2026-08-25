// Package publicapi is the acceptance gate on the public compiler API
// : a Go module OUTSIDE this one, importing nothing but
// github.com/mas-bandwidth/schema/compiler and .../ir, builds a schema
// compiler that emits the same bytes this repo's own binary emits.
//
// Two legs, because the claim has two halves.
//
// TestExternalModuleBuildsTheCLI takes cmd/schema/main.go VERBATIM into a
// throwaway module and builds it there. The build is the assertion: Go
// refuses an internal import from outside the module, so the day cmd/schema
// reaches past the public API this test fails with the compiler's own error
// and names the package. Then both binaries run the full command matrix over
// both corpora and every emitted file is compared byte for byte.
//
// TestExternalGeneratorRegisters builds testdata/client, a module that
// implements the [compiler.Generator] interface itself and registers it on the
// driver — the half the first leg cannot prove, since the built-in targets
// might have had a private path in. Its output is compared against the same
// computation run in-module here: outside and inside must agree exactly.
//
// The issue asked for a byte-identical BINARY. Binaries carry their own
// module path, build directory and linker stamps, so equal bytes there would
// measure the build environment rather than the API; equal OUTPUT — every
// generated file of six targets across two corpora, plus the protocol id and
// the wire projection — is the honest form of the same claim, and it is the
// property this repo already treats as ground truth.
package publicapi

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/compiler"
	"github.com/mas-bandwidth/schema/ir"
)

// the corpora, from this package's directory.
const (
	corpusDir    = "../../examples"
	corpus128Dir = "../../examples128"
)

// repoRoot is the module root, absolute — the external module replaces the
// schema requirement with it.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// goBuild builds one main package into binPath, failing the test with the
// toolchain's own message. That message is the point in the external case: an
// import of an internal package fails here, by name.
func goBuild(t *testing.T, dir, binPath string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build in %s: %v\n%s", dir, err, out)
	}
}

// run executes a built binary and returns its combined output, failing on a
// nonzero exit.
func run(t *testing.T, binPath string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", binPath, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// TestExternalModuleBuildsTheCLI is the acceptance test of the public-API boundary.
func TestExternalModuleBuildsTheCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two compilers")
	}
	root := repoRoot(t)
	work := t.TempDir()

	// The external module: the CLI's own source, a go.mod that requires this
	// module by its public path, and nothing else. No network — the
	// requirement is replaced with the checkout, and schema depends on no
	// third-party module.
	clientDir := filepath.Join(work, "external")
	if err := os.MkdirAll(clientDir, 0o755); err != nil {
		t.Fatal(err)
	}
	main, err := os.ReadFile(filepath.Join(root, "cmd", "schema", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(main, []byte("mas-bandwidth/schema/internal/")) {
		t.Fatal("cmd/schema imports an internal package — the CLI is supposed to be a client of the public API")
	}
	if err := os.WriteFile(filepath.Join(clientDir, "main.go"), main, 0o644); err != nil {
		t.Fatal(err)
	}
	gomod := fmt.Sprintf("module schemacli\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/schema v0.0.0\n\nreplace github.com/mas-bandwidth/schema => %s\n", root)
	if err := os.WriteFile(filepath.Join(clientDir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}

	inRepo := filepath.Join(work, "schema-inrepo")
	external := filepath.Join(work, "schema-external")
	goBuild(t, filepath.Join(root, "cmd", "schema"), inRepo)
	goBuild(t, clientDir, external)

	// the reporting commands: identical text, including the protocol id and
	// the projection the id hashes. check is silent on success (its answer is
	// the exit code); --verbose restores its ok line, so both shapes run.
	for _, corpus := range []string{corpusDir, corpus128Dir} {
		for _, verb := range [][]string{{"check"}, {"check", "--verbose"}, {"id"}, {"projection"}} {
			args := append(append([]string{}, verb...), corpus)
			want := run(t, inRepo, args...)
			got := run(t, external, args...)
			if got != want {
				t.Errorf("%s %s diverged\n in-repo: %q\nexternal: %q", strings.Join(verb, " "), corpus, want, got)
			}
		}
		if out := run(t, inRepo, "check", corpus); out != "" {
			t.Errorf("check %s: success is silent by default, printed %q", corpus, out)
		}
	}

	// every target, both corpora, both C++ message representations: the
	// emitted trees must match file for file and byte for byte.
	type genCase struct {
		name    string
		corpus  string
		target  string
		variant bool
	}
	var cases []genCase
	for _, corpus := range []string{corpusDir, corpus128Dir} {
		for _, target := range []string{"c", "cpp", "cs", "go", "js", "rust"} {
			cases = append(cases, genCase{
				name:   filepath.Base(corpus) + "-" + target,
				corpus: corpus,
				target: target,
			})
		}
		cases = append(cases, genCase{
			name:    filepath.Base(corpus) + "-cpp-variant",
			corpus:  corpus,
			target:  "cpp",
			variant: true,
		})
	}
	generateArgs := func(c genCase, outDir string) []string {
		args := []string{"generate", "--lang", c.target, "--out", outDir}
		if c.variant {
			args = append(args, "--cpp-message", "variant")
		}
		return append(args, c.corpus)
	}
	for _, c := range cases {
		wantDir := filepath.Join(work, "want", c.name)
		gotDir := filepath.Join(work, "got", c.name)
		if out := run(t, inRepo, generateArgs(c, wantDir)...); out != "" {
			t.Errorf("%s: generate is silent on success by default, printed %q", c.name, out)
		}
		if out := run(t, external, generateArgs(c, gotDir)...); out != "" {
			t.Errorf("%s: generate is silent on success by default (external build), printed %q", c.name, out)
		}
		compareTrees(t, c.name, wantDir, gotDir)
	}

	// --verbose restores the per-file report, identically on both sides (the
	// output directories differ per run, so they are normalized out).
	wantVDir := filepath.Join(work, "want-verbose")
	gotVDir := filepath.Join(work, "got-verbose")
	wantV := strings.ReplaceAll(run(t, inRepo, "generate", "--lang", "cpp", "--out", wantVDir, "--verbose", corpusDir), wantVDir, "<out>")
	gotV := strings.ReplaceAll(run(t, external, "generate", "--lang", "cpp", "--out", gotVDir, "--verbose", corpusDir), gotVDir, "<out>")
	if gotV != wantV {
		t.Errorf("generate --verbose diverged\n in-repo: %q\nexternal: %q", wantV, gotV)
	}
	if !strings.Contains(wantV, "wrote ") {
		t.Errorf("generate --verbose did not list the emitted files: %q", wantV)
	}

}

// compareTrees fails on any difference between two emitted directories: a
// missing file, an extra file, or one differing byte.
func compareTrees(t *testing.T, label, wantDir, gotDir string) {
	t.Helper()
	want := readTree(t, wantDir)
	got := readTree(t, gotDir)
	if len(want) == 0 {
		t.Fatalf("%s: the in-repo compiler emitted nothing", label)
	}
	for name, wantBytes := range want {
		gotBytes, ok := got[name]
		if !ok {
			t.Errorf("%s: the external build did not emit %s", label, name)
			continue
		}
		if !bytes.Equal(wantBytes, gotBytes) {
			t.Errorf("%s: %s differs between the in-repo and external builds", label, name)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s: the external build emitted an extra file %s", label, name)
		}
	}
}

// readTree reads a directory of emitted files, keyed by path relative to it.
func readTree(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out[rel] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestExternalGeneratorRegisters builds testdata/client — a module outside
// this one that writes its own [compiler.Generator] and registers it — and
// checks its output against the same walk performed here, in-module, over the
// same public API.
func TestExternalGeneratorRegisters(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a compiler")
	}
	root := repoRoot(t)
	work := t.TempDir()
	bin := filepath.Join(work, "client")
	goBuild(t, filepath.Join(root, "internal", "publicapi", "testdata", "client"), bin)

	got := run(t, bin, corpusDir)
	if got != wantClientOutput(t) {
		t.Errorf("the external generator's output diverged from the in-module walk\n want:\n%s\n  got:\n%s", wantClientOutput(t), got)
	}
}

// wantClientOutput reproduces testdata/client's report from inside this
// module. The two run the same public API from opposite sides of the module
// boundary, so any divergence is the boundary itself.
func wantClientOutput(t *testing.T) string {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{corpusDir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "package %s 0x%016x\n", u.Package, u.ProtocolId)
	// The external module registers one more target than the built-in six,
	// and it must appear in the driver's own list.
	targets := append(c.Targets(), "widths")
	sort.Strings(targets)
	fmt.Fprintf(&b, "targets %s\n", strings.Join(targets, " "))
	for _, name := range u.Messages {
		st := u.Structs[name]
		fmt.Fprintf(&b, "%s %d bits %d bytes %d fields\n",
			name, ir.MaxBitsStruct(st), ir.MaxBytes(ir.MaxBitsStruct(st)), len(st.Fields))
	}
	// the symbolic-rendering surface: declared bounds rendered as source expressions,
	// mirrored line for line in the external module's widths generator
	structs := make([]string, 0, len(u.Structs))
	for name := range u.Structs {
		structs = append(structs, name)
	}
	sort.Strings(structs)
	for _, name := range structs {
		for _, f := range u.Structs[name].Fields {
			if len(ir.ExprConsts(f.IntMaxExpr)) == 0 || ir.ExprHasEnumMax(f.IntMaxExpr) {
				continue
			}
			fmt.Fprintf(&b, "bound %s.%s max = %s | %s = %s\n", name, f.Name,
				ir.RenderExpr(f.IntMaxExpr),
				ir.RenderExprIdent(f.IntMaxExpr, ir.RustConstName), f.IntMax)
		}
	}
	return b.String()
}
