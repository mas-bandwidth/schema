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
	cppClangGateMakeFile = "make/checks/schema-cpp-generated-clang-tidy-weverything-gate.mk"
	cppClangGateWorkflow = ".github/workflows/schema-cpp-generated-clang-tidy-weverything-gate.yml"
	cppClangGateCheck    = "schema-cpp-generated-clang-tidy-weverything-gate-check"
)

func TestSchemaCppGeneratedClangTidyWeverythingGateIsWired(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	// The root Makefile treats every make/*.mk as a language port when it
	// probes wide-scalar refusal. Keep this checker under make/checks and
	// include it from the real C++ port so it is never counted as a port.
	if _, err := os.Stat(filepath.Join(root, "make", "schema-cpp-generated-clang-tidy-weverything-gate.mk")); !os.IsNotExist(err) {
		t.Fatalf("C++ checker is in make/*.mk and would be probed as a port: %v", err)
	}
	cppMakeFile, err := os.ReadFile(filepath.Join(root, "make", "cpp.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cppMakeFile), "include "+cppClangGateMakeFile) {
		t.Fatalf("make/cpp.mk does not include the C++ checker")
	}

	makeFile, err := os.ReadFile(filepath.Join(root, cppClangGateMakeFile))
	if err != nil {
		t.Fatalf("the C++ clang lens has no make file: %v", err)
	}
	for _, want := range []string{
		"schema-cpp-generated-clang-tidy-weverything-gate:",
		"SCHEMA_CPP_CTW_TIDY_PIN",
		"TOOL-MISSING clang-tidy",
		"TOOL-MISSING clang++",
		"TOOL-MISSING clang-tidy %s %s RUNNER_NAME=%s",
		"TOOL-MISSING clang++ %s %s RUNNER_NAME=%s",
		"RUNNER_NAME",
		"tool verified: clang-tidy",
		"tool verified: clang++",
		"bugprone-*,cert-*,cppcoreguidelines-*",
		"-Weverything -Werror",
		// The generated readers and writers are headers: without the filter
		// clang-tidy drops every header finding as non-user code and the gate
		// is green over a corpus it never read.
		`--header-filter="$(SCHEMA_CPP_CTW_HEADER_FILTER)"`,
	} {
		if !strings.Contains(string(makeFile), want) {
			t.Errorf("%s does not carry %q: the lens is not the checker the card names", cppClangGateMakeFile, want)
		}
	}

	workflow, err := os.ReadFile(filepath.Join(root, cppClangGateWorkflow))
	if err != nil {
		t.Fatalf("the C++ clang lens has no workflow: %v", err)
	}
	if strings.Count(string(workflow), "runs-on: [self-hosted, Linux, X64, fleet, schema]") != 2 {
		t.Fatal("both C++ lens jobs must use the repository fleet schema runners")
	}
	if strings.Count(string(workflow), "- name: verify the fleet Go pin") != 2 {
		t.Fatal("both C++ lens jobs must fail closed unless the runner has the Go 1.26 toolchain")
	}
	for _, forbidden := range []string{"ubuntu-latest", "sudo apt-get", "apt-get install"} {
		if strings.Contains(string(workflow), forbidden) {
			t.Errorf("workflow must use the fleet toolchain, found hosted/global install route %q", forbidden)
		}
	}
	for _, required := range []string{
		"SCHEMA_CPP_CTW_CLANG: clang++-18",
		"SCHEMA_CPP_CTW_CLANG_TIDY: clang-tidy-18",
		"SCHEMA_CPP_CTW_TIDY_PIN: '18.1'",
		"SCHEMA_CPP_CTW_CLANG_PIN: '18.1'",
		"github.event.pull_request.head.repo.full_name == github.repository",
		"Fork pull requests are excluded from the repository fleet runners below.",
		"- name: verify the fleet Go pin",
		"command -v go",
		"TOOL-MISSING go 1.26.x missing RUNNER_NAME=%s",
		"TOOL-MISSING go 1.26.x %s RUNNER_NAME=%s",
		"go1.26.*)",
		"tool verified: go %s RUNNER_NAME=%s",
		"GOTOOLCHAIN=local go env GOVERSION",
	} {
		if !strings.Contains(string(workflow), required) {
			t.Errorf("workflow does not pin or safely route the fleet checker: missing %q", required)
		}
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
// every file argument for the plant. Like the real clang-tidy, it reports from
// an included header (an -I directory) only when --header-filter is given and
// is not the match-nothing `^$`, so a recipe that drops the filter goes green
// over a planted header and this test goes red. It is a stand-in for the real
// binary in the PLUMBING test only; the workflow runs the real pin.
func fakeTool(t *testing.T, dir, name, version, plant string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	var body strings.Builder
	body.WriteString("#!/bin/sh\n")
	body.WriteString("if [ \"$1\" = --version ]; then echo \"" + version + "\"; exit 0; fi\n")
	if plant != "" {
		body.WriteString("hf=\"\"\n")
		body.WriteString("for a in \"$@\"; do case \"$a\" in --header-filter=^\\$) ;; --header-filter=?*) hf=1 ;; esac; done\n")
		body.WriteString("for a in \"$@\"; do\n")
		body.WriteString("  case \"$a\" in -I*) inc=\"${a#-I}\" ;; *) inc=\"\" ;; esac\n")
		body.WriteString("  if [ -f \"$a\" ] && grep -q '" + plant + "' \"$a\"; then\n")
		body.WriteString("    echo \"$a:1:1: warning: " + plant + " used after it was moved [bugprone-use-after-move]\"; exit 1\n")
		body.WriteString("  fi\n")
		body.WriteString("  if [ -n \"$hf\" ] && [ -n \"$inc\" ] && [ -d \"$inc\" ] && grep -Rqs '" + plant + "' \"$inc\"; then\n")
		body.WriteString("    echo \"$inc/unit.h:1:1: warning: " + plant + " used after it was moved [bugprone-use-after-move]\"; exit 1\n")
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
	// A zero pin makes one new unique diagnostic an exact +1 regression.
	ratchet := filepath.Join(dir, "ratchet.tsv")
	if err := os.WriteFile(ratchet, []byte("bugprone-use-after-move\t0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

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
			"SCHEMA_CPP_CTW_RATCHET="+ratchet,
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
	// The gate reports the growth, while the retained checker log names the
	// planted header diagnostic that produced it.
	raw, readErr := os.ReadFile(filepath.Join(dir, "bad-work", "logs", "1.tidy"))
	if readErr != nil || !strings.Contains(string(raw), "schema_ctw_plant") {
		t.Fatalf("negative control did not retain the named checker diagnostic: %v\n%s", readErr, raw)
	}
	if !strings.Contains(out, "bugprone-use-after-move pinned 0 measured 1") {
		t.Fatalf("the +1 pinned-count control did not name the check and both counts:\n%s", out)
	}

	// RED, the second way: the same planted header with the header filter set
	// to match nothing must go green, which proves the plant is seen only
	// through the filter. If this stays red the fake is not modelling the
	// real checker and the RED above proves nothing about headers.
	cmd := exec.Command("make", "--no-print-directory", "-C", root, cppClangGateCheck,
		"SCHEMA_CPP_CTW_CORPUS="+bad,
		"SCHEMA_CPP_CTW_WORK="+filepath.Join(dir, "bad-nofilter-work"),
		"SCHEMA_CPP_CTW_CLANG_TIDY="+tidy,
		"SCHEMA_CPP_CTW_CLANG="+clang,
		"SCHEMA_CPP_CTW_RATCHET="+ratchet,
		"SCHEMA_CPP_CTW_HEADER_FILTER=^$$",
	)
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly", "NOVA_TEST_NO_HOST=1")
	if nf, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("with the header filter matching nothing the planted header was still reported, so the fake does not model clang-tidy's header rule:\n%s", nf)
	}

	// GREEN: the clean twin passes through the same recipe.
	clean := writeUnit("clean", "inline int schema_ctw_clean( int * value ) { return value == nullptr ? 0 : *value; }\n")
	out, err = run(clean, filepath.Join(dir, "clean-work"))
	if err != nil {
		t.Fatalf("the clean twin did not pass, so the red above was not the plant:\n%s", out)
	}
}
