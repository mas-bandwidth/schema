package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// THE GOOSE BATCH AND ITS FALL BACK (test/conformance/goose/driver,
// batch_answer). The driver compiles each unit once and answers every
// surface from that one run's sections. A section must end with its "#done"
// line to count. When a batch is torn, or exits non-zero, each of its
// surfaces is answered by its own single run instead, and the driver says so
// on stderr by name. Each case runs the committed driver in a scratch tree
// under a stand-in goose. The stand-in answers the batch call as the case
// says and answers a single run with the prior lines, so no conformance
// run's files are touched.
func TestGooseDriverBatchFallsBackByName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// batch is the stand-in's body for the batch call
		batch string
		// single is the stand-in's body for a single run
		single string
		// say is what stderr must hold, and quiet what it must not
		say, quiet []string
	}{
		{
			name:   "whole-batch-answers",
			batch:  "echo '#surface message-prior'; " + priorStandIn + "echo '#done message-prior'\n",
			single: "echo 'stand-in: a single run, though the batch was whole' >&2; exit 1\n",
			quiet:  []string{"single run", "exited non-zero"},
		},
		{
			name:   "torn-batch-falls-back",
			batch:  "echo '#surface message-prior'; printf 'p_past 00\\n'\n",
			single: priorStandIn,
			say:    []string{`batch has no "#done message-prior" for unit tblprior`, "single run"},
		},
		{
			name:   "failed-batch-says-so",
			batch:  "echo 'stand-in: batch boom' >&2; exit 3\n",
			single: priorStandIn,
			say:    []string{"unit tblprior's batch exited non-zero", "stand-in: batch boom", `batch has no "#done message-prior"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, manifest, out := gooseBatchTree(t)
			// run -O0 <main> -- <surface|batch> <manifest> <unit> [surfaces]
			goose := standIn(t, dir, "goose", `if [ "$5" = batch ]; then
[ "$7" = tblprior ] || { echo "stand-in: batch of unit $7" >&2; exit 1; }
`+tc.batch+`exit 0
fi
[ "$7" = tblprior ] || { echo "stand-in: ran unit $7" >&2; exit 1; }
`+tc.single)
			// testinputs:none — runs the copy of the goose driver that gooseBatchTree read through os, which go test records
			cmd := exec.Command("sh", filepath.Join("test", "conformance", "goose", "driver"), manifest, "message-prior", out)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOOSE="+goose, "SCHEMA_GOOSE_GENERATED=units")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if b, err := cmd.Output(); err != nil {
				t.Fatalf("the goose driver failed on the message-prior surface: %v\nstdout: %s\nstderr: %s", err, b, stderr.String())
			}
			mustPriorAnswers(t, "goose", out)
			for _, s := range tc.say {
				if !strings.Contains(stderr.String(), s) {
					t.Errorf("stderr does not say %q:\n%s", s, stderr.String())
				}
			}
			for _, s := range tc.quiet {
				if strings.Contains(stderr.String(), s) {
					t.Errorf("stderr says %q, and the batch was whole:\n%s", s, stderr.String())
				}
			}
		})
	}
}

// TestGooseDriverRefusesAnUnreadableBatchKey: a batch key input that cannot
// be read (here accel_lib.goose is missing) stops the driver by name. It
// never builds a cache keyed on part of the inputs.
func TestGooseDriverRefusesAnUnreadableBatchKey(t *testing.T) {
	t.Parallel()
	dir, manifest, out := gooseBatchTree(t)
	if err := os.Remove(filepath.Join(dir, "test", "conformance", "goose", "accel_lib.goose")); err != nil {
		t.Fatal(err)
	}
	goose := standIn(t, dir, "goose", priorStandIn)
	// testinputs:none — runs the copy of the goose driver that gooseBatchTree read through os, which go test records
	cmd := exec.Command("sh", filepath.Join("test", "conformance", "goose", "driver"), manifest, "message-prior", out)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOOSE="+goose, "SCHEMA_GOOSE_GENERATED=units")
	b, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the driver answered with a batch key input missing:\n%s", b)
	}
	if !strings.Contains(string(b), "the batch cache key's inputs cannot all be read") {
		t.Errorf("the driver failed without naming the key:\n%s", b)
	}
}

// gooseBatchTree is a scratch repository root like priorTree's (with no
// skip: sh, awk and perl missing fail the driver by name) holding everything
// the goose batch reads: the programs, the accelerators and every unit's text glue, and the
// one unit tblprior the prior lines name.
func gooseBatchTree(t *testing.T) (dir, manifest, out string) {
	t.Helper()
	extra := []string{"main.goose", "unit.goose", "accel.goose", "accel_lib.goose", filepath.Join("..", "answer-lines.pl")}
	texts, err := filepath.Glob(filepath.Join("..", "goose", "units", "*.goose"))
	if err != nil {
		t.Fatal(err)
	}
	if len(texts) == 0 {
		t.Fatal("test/conformance/goose/units holds no .goose text glue")
	}
	for _, p := range texts {
		extra = append(extra, filepath.Join("units", filepath.Base(p)))
	}
	root := filepath.Join("..", "..", "..")
	dir = t.TempDir()
	for _, rel := range append([]string{"driver"}, extra...) {
		src, err := os.ReadFile(filepath.Join(root, "test", "conformance", "goose", rel))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(dir, "test", "conformance", "goose", rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, src, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"ann.bin", "file.bin", "past.bin", "inside.bin"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte{1}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest = filepath.Join(dir, "manifest.txt")
	if err := os.WriteFile(manifest, []byte("unit tblprior x.schema\n"+priorManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	out = filepath.Join(dir, "out")
	unit := filepath.Join(dir, "units", "tblprior")
	if err := os.MkdirAll(unit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unit, "PriorNarrowTable.goose"), []byte("namespace tblprior;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, manifest, out
}
