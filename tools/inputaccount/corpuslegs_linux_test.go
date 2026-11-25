package main

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The conformance-corpus input-family checks (the retired ci-fast.yml
// input-family job): one reader leg of the conformance corpus under
// tools/inputaccount, first with -check-extra (every genuine input
// opened, and the planted extra input unread) and then with the wrong
// canary (one input byte flipped, which the leg must judge wrong).
// Each leg is one Go test so the registry holds one gotest row per
// leg (test/registry/retired.tsv); the commands are the old job's,
// with the same make variables the job set for the leg.

func corpusLegVars(t *testing.T, leg string) []string {
	t.Helper()
	dir := func(prog string) string {
		p, err := exec.LookPath(prog)
		if err != nil {
			t.Fatalf("conformance-corpus %s: %v", leg, err)
		}
		return filepath.Dir(p)
	}
	switch leg {
	case "rust":
		return []string{"RUSTUP_BIN=" + dir("cargo")}
	case "dart":
		return []string{"DART=dart"}
	case "java":
		return []string{"JAVA=java", "JAVAC=javac"}
	case "js":
		return []string{"NODE=node"}
	case "elixir":
		return []string{"BEAM_PATH=" + dir("erl") + ":" + dir("elixir")}
	}
	return nil
}

func runConformanceCorpusLeg(t *testing.T, leg string) {
	t.Helper()
	root := filepath.Join("..", "..")
	canary := filepath.Join(root, "testdata", "conformance", "tables", "zz-inputaccount-canary.txt")
	if err := os.Remove(canary); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("conformance-corpus %s: %v", leg, err)
	}
	vars := append(corpusLegVars(t, leg), "VERIFY_JOBS=4")
	for _, mode := range []string{"-check-extra", "-canary"} {
		args := []string{"run", "./tools/inputaccount", "-family", "conformance-corpus", "-leg", leg, mode}
		if mode == "-canary" {
			args = append(args, "wrong")
		}
		args = append(args, vars...)
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("inputaccount %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

func TestInputAccountConformanceCorpusCpp(t *testing.T) { runConformanceCorpusLeg(t, "cpp") }

func TestInputAccountConformanceCorpusC(t *testing.T) { runConformanceCorpusLeg(t, "c") }

func TestInputAccountConformanceCorpusGo(t *testing.T) { runConformanceCorpusLeg(t, "go") }

func TestInputAccountConformanceCorpusRust(t *testing.T) { runConformanceCorpusLeg(t, "rust") }

func TestInputAccountConformanceCorpusJs(t *testing.T) { runConformanceCorpusLeg(t, "js") }

func TestInputAccountConformanceCorpusCs(t *testing.T) { runConformanceCorpusLeg(t, "cs") }

func TestInputAccountConformanceCorpusUnity(t *testing.T) { runConformanceCorpusLeg(t, "unity") }

func TestInputAccountConformanceCorpusJava(t *testing.T) { runConformanceCorpusLeg(t, "java") }

func TestInputAccountConformanceCorpusDart(t *testing.T) { runConformanceCorpusLeg(t, "dart") }

func TestInputAccountConformanceCorpusElixir(t *testing.T) { runConformanceCorpusLeg(t, "elixir") }

func TestInputAccountConformanceCorpusLua(t *testing.T) { runConformanceCorpusLeg(t, "lua") }

func TestInputAccountConformanceCorpusLuau(t *testing.T) { runConformanceCorpusLeg(t, "luau") }

func TestInputAccountConformanceCorpusMoo(t *testing.T) { runConformanceCorpusLeg(t, "moo") }

func TestInputAccountConformanceCorpusSquirrel(t *testing.T) { runConformanceCorpusLeg(t, "squirrel") }

func TestInputAccountConformanceCorpusGoose(t *testing.T) { runConformanceCorpusLeg(t, "goose") }

func TestInputAccountConformanceCorpusLobster(t *testing.T) { runConformanceCorpusLeg(t, "lobster") }

func TestInputAccountConformanceCorpusPython(t *testing.T) { runConformanceCorpusLeg(t, "python") }

func TestInputAccountConformanceCorpusRuby(t *testing.T) { runConformanceCorpusLeg(t, "ruby") }
