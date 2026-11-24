package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWireFuzzSplitIsDerivedFromTheRoster holds make/checks/wire-fuzz-split.sh,
// the check that replaced the outside and absent seed counts the Lobster and
// Squirrel wire-fuzz recipes registered by hand (219, then 315, outside the
// builder arm: stale each time the corpus grew). The check reads the harness's
// own summary lines and holds the arms to each other, so a root that drops out
// of an arm is red and a corpus that grows is not. The logs are the harness's
// summary format, written into the test's temporary directory; no harness, no
// leg and no clock.
func TestWireFuzzSplitIsDerivedFromTheRoster(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	const (
		plainAll     = "wire-fuzz: seed 1, 520 seeds over 87 roots, 401392 enumerated + 200 random = 401592 mutants, 0 divergences, 517.4 s, 776 mutants/s\n"
		plainGrown   = "wire-fuzz: seed 1, 540 seeds over 88 roots, 401392 enumerated + 200 random = 401592 mutants, 0 divergences, 517.4 s, 776 mutants/s\n"
		plainAbsent  = "wire-fuzz: seed 1, 517 seeds over 87 roots, 3 seeds absent (roots the leg has no codec for), 1 enumerated + 2 random = 3 mutants, 0 divergences, 1.0 s, 3 mutants/s\n"
		builderAll   = "wire-fuzz-builder: seed 1, 205 seeds over 38 roots, 315 seeds outside the arm, 287504 enumerated + 200 random = 287704 mutants, 0 divergences, 202.4 s, 1422 mutants/s\n"
		builderGrown = "wire-fuzz-builder: seed 1, 210 seeds over 39 roots, 330 seeds outside the arm, 287504 enumerated + 200 random = 287704 mutants, 0 divergences, 202.4 s, 1422 mutants/s\n"
		builderHole  = "wire-fuzz-builder: seed 1, 203 seeds over 37 roots, 2 seeds absent (roots the leg has no codec for), 315 seeds outside the arm, 1 enumerated + 2 random = 3 mutants, 0 divergences, 1.0 s, 3 mutants/s\n"
		retainAll    = "wire-fuzz retain: seed 1, 205 seeds over 38 roots, 315 seeds outside the arm, 1 enumerated + 2 random = 3 mutants, 0 divergences, 1.0 s, 3 mutants/s\n"
		// the squirrel split: the plain arm answers the 315 the builder arm
		// leaves outside, and leaves absent the 205 the builder arm answers
		sqPlain     = "wire-fuzz: seed 1, 315 seeds over 87 roots, 205 seeds absent (roots the leg has no codec for), 1 enumerated + 2 random = 3 mutants, 0 divergences, 1.0 s, 3 mutants/s\n"
		sqPlainHole = "wire-fuzz: seed 1, 293 seeds over 87 roots, 227 ledgered seeds absent (test/conformance/wire-absent/squirrel-plain.tsv), 1 enumerated + 2 random = 3 mutants, 0 divergences, 1.0 s, 3 mutants/s\n"
		noSummary   = "harness: FAILED after 3 mutants: the leg died on the mutant (EOF)\n"
	)
	for _, c := range []struct {
		name, mode string
		logs       []string
		ok         bool
		want       string
	}{
		{"complete: every arm answers its roster", "complete", []string{plainAll, builderAll, retainAll}, true, "answers 205 seeds and 315 are outside it, the plain arm's 520"},
		{"complete: the corpus grows and nothing is registered", "complete", []string{plainGrown, builderGrown}, true, "the plain arm's 540"},
		{"complete: a root drops out of the builder arm", "complete", []string{plainAll, builderHole}, false, "leaves 2 seeds absent: the leg answers every root of the arm"},
		{"complete: a one-arm leg answers the whole corpus", "complete", []string{plainAll}, true, "the plain arm answers all 520 seeds of the corpus, none absent"},
		{"complete: a one-arm leg leaves seeds absent", "complete", []string{plainAbsent}, false, "the plain arm (plain.log) leaves 3 seeds absent"},
		{"complete: a root drops out of the plain arm", "complete", []string{plainAbsent, builderAll}, false, "the plain arm (plain.log) leaves 3 seeds absent"},
		{"complete: the arm's split does not add up to the plain arm's", "complete", []string{plainAll, builderGrown}, false, "540 in all, where the plain arm answers 520"},
		{"complete: an arm with no summary proved nothing", "complete", []string{plainAll, noSummary}, false, "carries no wire-fuzz summary line"},
		{"complement: the two arms split the corpus", "complement", []string{sqPlain, builderAll}, true, "the plain arm answers 315 seeds and the builder arm the other 205"},
		{"complement: seeds in neither arm", "complement", []string{sqPlainHole, builderAll}, false, "leaves 227 seeds absent where the builder arm (arm1.log) answers 205"},
		{"complement: a root drops out of the builder arm", "complement", []string{sqPlain, builderHole}, false, "leaves 2 seeds absent: the builder arm answers every root of its roster"},
		{"complement: a plain arm with no summary proved nothing", "complement", []string{noSummary, builderAll}, false, "plain.log carries no wire-fuzz summary line"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			args := []string{filepath.Join(root, "make/checks/wire-fuzz-split.sh"), c.mode, "test-recipe"}
			for i, body := range c.logs {
				name := "plain.log"
				if i > 0 {
					name = "arm" + string(rune('0'+i)) + ".log"
				}
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, []byte("./build/conformance-harness wire-fuzz ...\n"+body), 0o644); err != nil {
					t.Fatal(err)
				}
				args = append(args, name)
			}
			cmd := exec.Command("sh", args...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if (err == nil) != c.ok || !strings.Contains(string(out), c.want) {
				t.Fatalf("passed=%v, want %v and output carrying %q:\n%s", err == nil, c.ok, c.want, out)
			}
		})
	}
}
