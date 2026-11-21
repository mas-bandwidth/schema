package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResultsPublishedOnlyAfterRestoredBaseline(t *testing.T) {
	for _, name := range []string{"clean", "invalidated"} {
		t.Run(name, func(t *testing.T) {
			tree := t.TempDir()
			if err := os.CopyFS(tree, os.DirFS("testdata/corpus/invalidated")); err != nil {
				t.Fatal(err)
			}
			const old, replacement = "a+b == 5", "a+b != 5"
			src, err := os.ReadFile(filepath.Join(tree, "value.go"))
			if err != nil {
				t.Fatal(err)
			}
			at := strings.Index(string(src), old)
			if at < 0 {
				t.Fatal("corpus has no comparison to mutate")
			}
			mutant := Mutant{ID: "comparison", File: "value.go", Old: old, New: replacement, Start: at, End: at + len(old)}
			scope := Scope{Name: "check", Stages: []Stage{{Name: "arithmetic", Sh: "go test -v -count=1 .", Proof: "--- PASS: TestTwoPlusThree", Counted: true}}}
			out := filepath.Join(t.TempDir(), "results.jsonl")
			t.Setenv("MUTATION_CORPUS_PHASE", "")
			if name == "invalidated" {
				t.Setenv("MUTATION_CORPUS_PHASE", filepath.Join(tree, "phase"))
			}
			results, err := sweepToOutput(scope, tree, []Mutant{mutant}, sweepOptions{BaselineLimit: time.Minute}, shell, io.Discard, out)
			if name == "invalidated" {
				if err == nil || !strings.Contains(err.Error(), "every verdict of this run is void") || len(results) != 0 {
					t.Fatalf("restored baseline must void its verdict: results=%v err=%v", results, err)
				}
				if _, err := os.Stat(out); !os.IsNotExist(err) {
					t.Fatalf("invalidated run published %s: %v", out, err)
				}
			} else if err != nil || len(results) != 1 || results[0].Outcome != outcomeKilled {
				t.Fatalf("clean run should publish a kill: results=%v err=%v", results, err)
			}
			for _, entry := range []struct {
				name string
				args []string
			}{
				{"report", []string{out}},
				{"run", []string{"-scope", "check", "-work", t.TempDir(), "-retry", out}},
			} {
				if err := command(entry.name, entry.args); (err != nil) != (name == "invalidated") {
					t.Errorf("%s after %s: %v", entry.name, name, err)
				}
			}
		})
	}
}

func TestRetryRejectsAnEmptyResultsFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(out, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := command("run", []string{"-scope", "check", "-work", t.TempDir(), "-retry", out}); err == nil || !strings.Contains(err.Error(), "no result") {
		t.Fatalf("retry accepted an empty file: %v", err)
	}
}
