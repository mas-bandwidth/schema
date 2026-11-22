package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Each initial sidecar and payload is a real pair from the old unit. Both
// members change for the new unit, so an incorrectly replaced side is visible.
func TestPairedPublishCorpus(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	corpus := filepath.Join("testdata", "paired_publish")
	var steps []struct {
		Name, Source, Expected string
		Blocked                bool
		Exit                   int
	}
	if err := json.Unmarshal([]byte(mustRead(t, filepath.Join(corpus, "operations.json"))), &steps); err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 {
		t.Fatalf("wanted the initial, blocked and retry operations, got %d", len(steps))
	}

	for _, verb := range []string{"pack", "cook"} {
		t.Run(verb, func(t *testing.T) {
			pair := t.TempDir()
			blocked := filepath.Join(t.TempDir(), "blocked-payload")
			sentinel := filepath.Join(blocked, "keep")
			writeTestFile(t, sentinel, "occupied")
			for _, name := range []string{"side.bin", "payload.bin"} {
				if mustRead(t, filepath.Join(corpus, "initial", verb, name)) == mustRead(t, filepath.Join(corpus, "expected", verb, name)) {
					t.Fatalf("fixture %s must change between the old and new pair", name)
				}
			}
			for _, step := range steps {
				t.Run(step.Name, func(t *testing.T) {
					side := filepath.Join(pair, "side.bin")
					payload := filepath.Join(pair, "payload.bin")
					if step.Blocked {
						payload = blocked // a nonempty directory refuses a file rename on every platform
					}
					tree := filepath.Join(corpus, step.Source, "tree")
					unit := filepath.Join(corpus, step.Source, "Player.schema")
					args := []string{"pack", "--root", "Player", "--message", "--announce", side, "--out", payload, tree, unit}
					if verb == "cook" {
						args = []string{"cook", "--root", "Player", "--in", tree, "--attribution", side, "--out", payload, unit}
					}
					code, output := runExit(t, bin, args...)
					if code != step.Exit || (step.Blocked && !strings.Contains(output, blocked)) {
						t.Fatalf("schema %s exited %d, wanted %d: %s", verb, code, step.Exit, output)
					}
					compareTrees(t, verb, filepath.Join(corpus, step.Expected, verb), pair)
					if mustRead(t, sentinel) != "occupied" {
						t.Fatal("the destination obstruction changed")
					}
					if code == 0 {
						if verb == "pack" {
							run(t, bin, "unpack", "--root", "Player", "--in", payload, "--announce", side, t.TempDir(), unit)
						} else {
							run(t, bin, "cook-check", "--root", "Player", "--attribution", side, payload, unit)
						}
					}
				})
			}
		})
	}
}
