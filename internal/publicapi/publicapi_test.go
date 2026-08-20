// Package publicapi is the acceptance gate on the public compiler API
// (issue #85): a Go module OUTSIDE this one, importing nothing but
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

// TestExternalModuleBuildsTheCLI is the acceptance test of issue #85.
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
		t.Fatal("cmd/schema imports an internal package — the CLI is supposed to be a client of the public API (issue #85)")
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
	// the projection the id hashes.
	for _, corpus := range []string{corpusDir, corpus128Dir} {
		for _, verb := range []string{"check", "id", "projection"} {
			want := run(t, inRepo, verb, corpus)
			got := run(t, external, verb, corpus)
			if got != want {
				t.Errorf("%s %s diverged\n in-repo: %q\nexternal: %q", verb, corpus, want, got)
			}
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
		run(t, inRepo, generateArgs(c, wantDir)...)
		run(t, external, generateArgs(c, gotDir)...)
		compareTrees(t, c.name, wantDir, gotDir)
	}

	// the data compiler: same manifest, same instance, same container bytes —
	// down to the content hash the two builds print.
	wantBin, wantLine := runPack(t, inRepo, filepath.Join(work, "pack-want"), root)
	gotBin, gotLine := runPack(t, external, filepath.Join(work, "pack-got"), root)
	if !bytes.Equal(wantBin, gotBin) {
		t.Errorf("pack: the container bytes differ between the in-repo and external builds")
	}
	if gotLine != wantLine {
		t.Errorf("pack: the report differs\n in-repo: %q\nexternal: %q", wantLine, gotLine)
	}
}

// packManifest packs one ProbeArray instance out of the main corpus: a table
// root that composes a defaulted type through a fixed array, so the container
// exercises defaults, nesting and a counted array rather than a single scalar.
const packManifest = `{
  "unit": %q,
  "outputs": [
    { "file": "probes.bin", "collections": [ { "type": "ProbeArray", "file": "probe.json" } ] }
  ]
}`

const packInstance = `{
  "samples": [
    { "active": true, "orientation": 1.25, "raw_delta": 7, "big_delta": 900, "samples": [1, 2, 3] },
    { "active": false, "orientation": -0.5, "raw_delta": -7, "big_delta": -900, "samples": [9] }
  ],
  "config": { "retries": 3 }
}`

// runPack writes the manifest and its instance into dir, packs it with bin,
// and returns the container bytes and the report line (with the run's own
// temporary paths removed, since only the numbers are comparable).
func runPack(t *testing.T, bin, dir, root string) ([]byte, string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "manifest.json")
	// Manifest paths resolve against the manifest's own directory, so the
	// corpus is named relative to the temporary directory this one sits in.
	unit, err := filepath.Rel(dir, filepath.Join(root, "examples"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(fmt.Sprintf(packManifest, unit)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.json"), []byte(packInstance), 0o644); err != nil {
		t.Fatal(err)
	}
	report := strings.ReplaceAll(run(t, bin, "pack", manifest), dir, "<dir>")
	data, err := os.ReadFile(filepath.Join(dir, "probes.bin"))
	if err != nil {
		t.Fatal(err)
	}
	return data, report
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
	return b.String()
}
