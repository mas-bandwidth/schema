package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestFixtureProducerFailureIsNotMaskedByAnEarlierRun is review finding
// F-194. make/go.mk's tables-go-module (and tables-go-fuzz and
// goose-accel-fixtures before it) wrote conformance fixtures into a build
// directory that persists, with the producer's exit status hidden by
// `|| true`. After one good run a failed producer changed nothing: the
// consumer read the earlier run's Scene.cook and the gate went green.
//
// The cases are testdata/freshfixtures (its README names them): a make rule of
// the real rules' shape, a producer this test controls, and a consumer.
func TestFixtureProducerFailureIsNotMaskedByAnEarlierRun(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	dir := t.TempDir()
	for dst, src := range map[string]string{
		"Makefile":    filepath.Join("testdata", "freshfixtures", "Makefile"),
		"producer.sh": filepath.Join("testdata", "freshfixtures", "producer.sh"),
		"fixtures":    filepath.Join(root, "test", "slowgate", "fixtures"),
	} {
		body, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, dst), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(target, producer string) (string, error) {
		cmd := exec.Command("make", "--no-print-directory", target, "PRODUCER="+producer)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "MAKEFLAGS=", "MAKELEVEL=")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	cook := filepath.Join(dir, "work", "fixtures", "Scene.cook")

	// F-194-fresh
	if out, err := run("fresh", "sh producer.sh ok one"); err != nil || !strings.Contains(out, "\none\n") {
		t.Fatalf("F-194-fresh: a working producer: err=%v\n%s", err, out)
	}
	// F-194: her repro. One good run happened; now the producer fails.
	out, err := run("fresh", "sh producer.sh fail")
	if err == nil {
		t.Errorf("F-194: the producer exited 17 and make exited 0, reading the earlier run's fixtures:\n%s", out)
	}
	if !strings.Contains(out, "exited 17") {
		t.Errorf("F-194: the failure does not name the producer's exit status:\n%s", out)
	}
	if _, err := os.Stat(cook); err == nil {
		t.Errorf("F-194: the earlier run's Scene.cook is still on disk after a failed producer")
	}
	// F-194-empty
	if out, err := run("fresh", "sh producer.sh empty"); err == nil || !strings.Contains(out, "wrote no file") {
		t.Errorf("F-194-empty: a producer that wrote nothing: err=%v\n%s", err, out)
	}
	// F-194-again
	if out, err := run("fresh", "sh producer.sh ok two"); err != nil || !strings.Contains(out, "\ntwo\n") || strings.Contains(out, "\none\n") {
		t.Errorf("F-194-again: a working producer after the failures: err=%v\n%s", err, out)
	}
	// F-194-unnamed: a producer command that does not say it writes where
	// the consumer reads is refused before it runs, and leaves the last
	// good run's files alone.
	if out, err := run("unnamed", "sh producer.sh fail"); err == nil || !strings.Contains(out, "does not name --work work") || strings.Contains(out, "refusing before any write") {
		t.Errorf("F-194-unnamed: a producer command with no --work: err=%v\n%s", err, out)
	}
	// F-194-old-shape: the control. The `|| true` recipe reads green on the
	// previous run's file, which is what makes the cases above mean something.
	if out, err := run("stale-shape", "sh producer.sh fail"); err != nil || !strings.Contains(out, "two") {
		t.Errorf("F-194-old-shape: the old recipe shape no longer shows the defect (err=%v), so this test cannot tell the fix from nothing:\n%s", err, out)
	}
}

// TestNoRecipeHidesAFixtureProducersFailure holds the class: every recipe
// that has the conformance harness write a --work directory goes through
// test/slowgate/fixtures, and none hides the harness's status.
func TestNoRecipeHidesAFixtureProducersFailure(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk, err := laneReadMake(root)
	if err != nil {
		t.Fatal(err)
	}
	producer := regexp.MustCompile(`conformance-harness\S* run\b`)
	through, seen := 0, 0
	for _, r := range mk.rules {
		for _, line := range r.recipe {
			text := strings.Join(strings.Fields(line.text), " ")
			if !producer.MatchString(text) {
				continue
			}
			seen++
			if strings.Contains(text, "sh test/slowgate/fixtures ") {
				through++
				if strings.Contains(text, "|| true") || strings.Contains(text, "|| :") {
					t.Errorf("%s: a fixtures line that still hides its status: %s", line.src, text)
				}
				continue
			}
			if regexp.MustCompile(`--drivers /dev/null`).MatchString(text) {
				t.Errorf("%s: the harness writes fixtures (--drivers /dev/null) outside test/slowgate/fixtures, so a failure can leave an earlier run's files to be read: %s", line.src, text)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no recipe runs the conformance harness: this test read no recipe")
	}
	if through < 3 {
		t.Errorf("%d recipes go through test/slowgate/fixtures, want the 3 that write fixture-only work directories (tables-go-module, tables-go-fuzz, goose-accel-fixtures)", through)
	}
}
