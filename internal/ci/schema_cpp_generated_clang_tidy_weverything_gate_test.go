package ci

// The C++ lens (ideas roadmap group schema-per-language-lenses) is a make
// target, so its planted-pattern proof has two halves. The static half holds
// the target, its pinned checker and its two workflow triggers in place: a
// deleted target or a workflow that never runs the full corpus is otherwise a
// silent absence. The behavioural half drives the SAME check recipe with fake
// checker binaries over a prepared corpus: the named plant must turn it red and
// the clean twin must stay green. That makes the plumbing red/green on every
// `go test`, without a checker on the host; the nightly workflow drives the
// same recipe with the real pinned clang-tidy, which is the half that proves
// the checker catches the plant. The test never generates the corpus, opens no
// socket and reads no clock.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	cppClangGateMakeFile = "make/schema-cpp-generated-clang-tidy-weverything-gate.mk"
	cppClangGateWorkflow = ".github/workflows/schema-cpp-generated-clang-tidy-weverything-gate.yml"
	cppClangGateCheck    = "schema-cpp-generated-clang-tidy-weverything-gate-check"
)

func TestSchemaCppGeneratedClangTidyWeverythingGateIsWired(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	makeFile, err := os.ReadFile(filepath.Join(root, cppClangGateMakeFile))
	if err != nil {
		t.Fatalf("the C++ clang lens has no make file: %v", err)
	}
	for _, want := range []string{
		"schema-cpp-generated-clang-tidy-weverything-gate:",
		"SCHEMA_CPP_CTW_TIDY_PIN",
		"bugprone-*,cert-*,cppcoreguidelines-*",
		"-Weverything -Werror",
		"tool missing: clang-tidy",
	} {
		if !strings.Contains(string(makeFile), want) {
			t.Errorf("%s does not carry %q: the lens is not the checker the card names", cppClangGateMakeFile, want)
		}
	}

	workflow, err := os.ReadFile(filepath.Join(root, cppClangGateWorkflow))
	if err != nil {
		t.Fatalf("the C++ clang lens has no workflow: %v", err)
	}
	for _, want := range []string{
		"schedule:",
		"workflow_dispatch:",
		"make schema-cpp-generated-clang-tidy-weverything-gate\n",
		"make schema-cpp-generated-clang-tidy-weverything-gate-plant",
	} {
		if !strings.Contains(string(workflow), want) {
			t.Errorf("%s does not carry %q: the heavy tier would never run the whole corpus", cppClangGateWorkflow, want)
		}
	}
}

// fakeTool writes one executable shell script and returns its path. The script
// answers --version with the Makefile's pin and, when it is the checker, scans
// every file argument for the plant. It is a stand-in for the real binary in
// the PLUMBING test only; the workflow runs the real pin.
func fakeTool(t *testing.T, dir, name, version, plant string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	var body strings.Builder
	body.WriteString("#!/bin/sh\n")
	body.WriteString("if [ \"$1\" = --version ]; then echo \"" + version + "\"; exit 0; fi\n")
	if plant != "" {
		body.WriteString("for a in \"$@\"; do\n")
		body.WriteString("  case \"$a\" in -I*) inc=\"${a#-I}\" ;; *) inc=\"\" ;; esac\n")
		body.WriteString("  if [ -f \"$a\" ] && grep -q '" + plant + "' \"$a\"; then\n")
		body.WriteString("    echo \"$a: error: " + plant + " is the planted pattern\"; exit 1\n")
		body.WriteString("  fi\n")
		body.WriteString("  if [ -n \"$inc\" ] && [ -d \"$inc\" ] && grep -Rqs '" + plant + "' \"$inc\"; then\n")
		body.WriteString("    echo \"$inc: error: " + plant + " is the planted pattern\"; exit 1\n")
		body.WriteString("  fi\n")
		body.WriteString("done\n")
	}
	body.WriteString("exit 0\n")
	if err := os.WriteFile(path, []byte(body.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSchemaCppGeneratedClangTidyWeverythingGatePlantedPattern(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	tidy := fakeTool(t, bin, "clang-tidy", "LLVM version 18.1.0", "schema_ctw_plant")
	clang := fakeTool(t, bin, "clang++", "clang version 18.1.0", "")

	writeUnit := func(name, header string) string {
		corpus := filepath.Join(dir, name)
		unit := filepath.Join(corpus, "unit")
		if err := os.MkdirAll(unit, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(unit, "unit.h"), []byte(header), 0o644); err != nil {
			t.Fatal(err)
		}
		return corpus
	}

	run := func(corpus, work string) (string, error) {
		cmd := exec.Command("make", "--no-print-directory", "-C", root, cppClangGateCheck,
			"SCHEMA_CPP_CTW_CORPUS="+corpus,
			"SCHEMA_CPP_CTW_WORK="+work,
			"SCHEMA_CPP_CTW_CLANG_TIDY="+tidy,
			"SCHEMA_CPP_CTW_CLANG="+clang,
		)
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly", "NOVA_TEST_NO_HOST=1")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// RED: the plant is in the corpus and the gate must exit non-zero naming it.
	bad := writeUnit("bad", "inline int schema_ctw_plant( int * value ) { return (int) sizeof( value ); }\n")
	out, err := run(bad, filepath.Join(dir, "bad-work"))
	if err == nil {
		t.Fatalf("negative control: the gate stayed green with the plant in:\n%s", out)
	}
	if !strings.Contains(out, "schema_ctw_plant") {
		t.Fatalf("negative control: the gate went red, but not on the named plant:\n%s", out)
	}

	// GREEN: the clean twin passes through the same recipe.
	clean := writeUnit("clean", "inline int schema_ctw_clean( int * value ) { return value == nullptr ? 0 : *value; }\n")
	out, err = run(clean, filepath.Join(dir, "clean-work"))
	if err != nil {
		t.Fatalf("the clean twin did not pass, so the red above was not the plant:\n%s", out)
	}
}
