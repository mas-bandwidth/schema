// The Squirrel all-port differential (docs/roadmap.sexp, group
// schema-per-language-lenses; ROADMAP.md). Squirrel has no static checker of
// its own over the generated output, so this is its lens: the conformance
// harness runs every registered port over the SAME corpus and the SAME
// vectors, and this test compares Squirrel's answer, case by case, with every
// other port's answer.
//
// THE CORPUS IS THE HARNESS'S, NOT A SECOND ONE. build/conformance-harness
// materialises testdata/conformance/tables (the corpus the Makefile's
// conformance targets read) and writes one file per (language, surface, case)
// under <work>/out/<lang>/<surface>. A case a port answers `.absent` is a
// feature that port does not carry, and it is NOT comparable: the differential
// reports it as skipped rather than turning absence into a verdict. Two
// present answers must be byte-identical — the same accept/refuse verdict, the
// same refusal reason in the report, and the same decoded value re-saved on the
// wire.
//
// The random-input arm is the harness's own wire-fuzz (make/squirrel-fuzz.mk):
// the same seed and the same mutants through Squirrel and through every port,
// each against the one independent engine (internal/tablewire), which is the
// cross-port comparison at the mutant grain. It runs in the card's nightly
// workflow, never per push.
package ci

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// differentialWorkEnv names the harness output tree the corpus test reads.
const differentialWorkEnv = "SCHEMA_SQUIRREL_DIFFERENTIAL_WORK"

// squirrelOutRoot is one port's output tree under <work>/out.
type squirrelOutRoot struct {
	lang string
	dir  string
}

// compareSquirrelAgainstPorts reads <work>/out and compares every file the
// Squirrel leg wrote with the same surface and case in every other port. It
// returns the number of comparisons made, the ports it compared against (the
// ports that answered at least one case Squirrel answered), and a sorted list
// of divergences.
//
// A case is comparable only when BOTH sides wrote the answer. A `.absent`
// marker on either side, or a file one side never wrote, is not a divergence:
// absence is a feature the port does not carry, and the conformance matrix
// already names it. A surface the Squirrel leg did not answer is not walked.
func compareSquirrelAgainstPorts(work string) (int, []string, []string, error) {
	out := filepath.Join(work, "out")
	entries, err := os.ReadDir(out)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("the differential has no harness output: %w (run make schema-squirrel-all-port-differential)", err)
	}
	var roots []squirrelOutRoot
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		roots = append(roots, squirrelOutRoot{lang: e.Name(), dir: filepath.Join(out, e.Name())})
	}
	if len(roots) == 0 {
		return 0, nil, nil, fmt.Errorf("%s holds no port", out)
	}
	var squirrel *squirrelOutRoot
	for i := range roots {
		if roots[i].lang == "squirrel" {
			squirrel = &roots[i]
		}
	}
	if squirrel == nil {
		return 0, nil, nil, fmt.Errorf("%s has no squirrel tree — the differential has nothing to be a lens on", out)
	}

	comparisons := 0
	compared := map[string]bool{}
	var divergences []string

	surfaces, err := os.ReadDir(squirrel.dir)
	if err != nil {
		return 0, nil, nil, err
	}
	for _, surface := range surfaces {
		if !surface.IsDir() {
			continue
		}
		cases, err := os.ReadDir(filepath.Join(squirrel.dir, surface.Name()))
		if err != nil {
			return 0, nil, nil, err
		}
		for _, c := range cases {
			name := c.Name()
			if c.IsDir() || strings.HasSuffix(name, ".absent") {
				continue
			}
			mine, err := os.ReadFile(filepath.Join(squirrel.dir, surface.Name(), name))
			if err != nil {
				return 0, nil, nil, err
			}
			for _, other := range roots {
				if other.lang == "squirrel" {
					continue
				}
				theirs, present := portAnswer(other.dir, surface.Name(), name)
				if !present {
					continue
				}
				comparisons++
				compared[other.lang] = true
				if !bytes.Equal(mine, theirs) {
					divergences = append(divergences, fmt.Sprintf(
						"squirrel vs %s: %s/%s differs (%d bytes vs %d bytes)%s",
						other.lang, surface.Name(), name, len(mine), len(theirs), firstDifference(mine, theirs)))
				}
			}
		}
	}
	sort.Strings(divergences)
	ports := make([]string, 0, len(compared))
	for lang := range compared {
		ports = append(ports, lang)
	}
	sort.Strings(ports)
	return comparisons, ports, divergences, nil
}

// portAnswer reads one port's answer for a case. It reports false when the
// port wrote neither the answer nor the `.absent` marker: that is the harness's
// failure to name, not a divergence this test invents.
func portAnswer(dir, surface, name string) ([]byte, bool) {
	answer, err := os.ReadFile(filepath.Join(dir, surface, name))
	if err == nil {
		return answer, true
	}
	if _, absent := os.Stat(filepath.Join(dir, surface, name+".absent")); absent == nil {
		return nil, false
	}
	return nil, false
}

// firstDifference names the first byte two answers disagree on, so the report
// localises the plant rather than printing two blobs.
func firstDifference(a, b []byte) string {
	n := min(len(b), len(a))
	for i := range n {
		if a[i] != b[i] {
			return fmt.Sprintf(": first difference at byte %d", i)
		}
	}
	if len(a) != len(b) {
		return fmt.Sprintf(": one is a prefix of the other at byte %d", n)
	}
	return ""
}

// writeAnswers writes one port's (surface, case) answers under dir.
func writeAnswers(t *testing.T, dir, lang string, answers map[string]map[string]string) {
	t.Helper()
	for surface, cases := range answers {
		path := filepath.Join(dir, "out", lang, surface)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range cases {
			if err := os.WriteFile(filepath.Join(path, name), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestSquirrelAllPortDifferentialDetectsADivergence is the planted-bad-pattern
// unit control: with the plant in (one byte of Squirrel's wire answer changed)
// the comparison is red and names the case; with the plant out the same tree
// is green. It runs on t.TempDir() and needs no toolchain.
func TestSquirrelAllPortDifferentialDetectsADivergence(t *testing.T) {
	t.Parallel()

	clean := map[string]map[string]string{
		"wire":   {"Scene": "AAAA", "Depot": "BBBB"},
		"report": {"Scene": "0,0,0,0,0,false,read\n"},
	}
	other := map[string]map[string]string{
		"wire":   {"Scene": "AAAA", "Depot": "BBBB"},
		"report": {"Scene": "0,0,0,0,0,false,read\n"},
	}

	t.Run("green when the plant is out", func(t *testing.T) {
		t.Parallel()
		work := t.TempDir()
		writeAnswers(t, work, "squirrel", clean)
		writeAnswers(t, work, "cpp", other)
		n, ports, divergences, err := compareSquirrelAgainstPorts(work)
		if err != nil {
			t.Fatal(err)
		}
		if len(divergences) != 0 {
			t.Fatalf("a clean tree diverged: %v", divergences)
		}
		if n != 3 {
			t.Fatalf("compared %d answers, want 3", n)
		}
		if len(ports) != 1 || ports[0] != "cpp" {
			t.Fatalf("compared ports = %v, want [cpp]", ports)
		}
	})

	t.Run("red when one wire byte is planted", func(t *testing.T) {
		t.Parallel()
		work := t.TempDir()
		planted := map[string]map[string]string{
			"wire":   {"Scene": "AAAB", "Depot": "BBBB"},
			"report": {"Scene": "0,0,0,0,0,false,read\n"},
		}
		writeAnswers(t, work, "squirrel", planted)
		writeAnswers(t, work, "cpp", other)
		_, _, divergences, err := compareSquirrelAgainstPorts(work)
		if err != nil {
			t.Fatal(err)
		}
		if len(divergences) != 1 {
			t.Fatalf("divergences = %v, want exactly the planted Scene wire answer", divergences)
		}
		if !strings.Contains(divergences[0], "squirrel vs cpp: wire/Scene") ||
			!strings.Contains(divergences[0], "byte 3") {
			t.Fatalf("the divergence does not name the plant: %q", divergences[0])
		}
	})

	t.Run("absence is not a divergence", func(t *testing.T) {
		t.Parallel()
		work := t.TempDir()
		writeAnswers(t, work, "squirrel", clean)
		writeAnswers(t, work, "cpp", map[string]map[string]string{
			"wire": {"Scene": "AAAA"}, // Depot is absent on this leg
		})
		if err := os.WriteFile(filepath.Join(work, "out", "cpp", "wire", "Depot.absent"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		n, _, divergences, err := compareSquirrelAgainstPorts(work)
		if err != nil {
			t.Fatal(err)
		}
		if len(divergences) != 0 {
			t.Fatalf("an absent case diverged: %v", divergences)
		}
		if n != 1 {
			t.Fatalf("compared %d answers, want the one both ports answer", n)
		}
	})
}

// TestSquirrelAllPortDifferentialRequiresComparisons refuses to pass on an
// empty lens: a tree with no squirrel output, no other port, or no shared
// answer proves nothing.
func TestSquirrelAllPortDifferentialRequiresComparisons(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	writeAnswers(t, work, "squirrel", map[string]map[string]string{"wire": {"Scene": "AAAA"}})
	writeAnswers(t, work, "cpp", map[string]map[string]string{"wire": {"Scene": "AAAA"}})
	if _, _, divergences, err := compareSquirrelAgainstPorts(work); err != nil || len(divergences) != 0 {
		t.Fatalf("one shared answer must be a green comparison, got %v %v", divergences, err)
	}
	only := t.TempDir()
	writeAnswers(t, only, "squirrel", map[string]map[string]string{"wire": {"Scene": "AAAA"}})
	n, ports, divergences, err := compareSquirrelAgainstPorts(only)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(ports) != 0 || len(divergences) != 0 {
		t.Fatalf("a lone squirrel tree compared %d answers across %v: %v", n, ports, divergences)
	}
	if _, _, _, err := compareSquirrelAgainstPorts(t.TempDir()); err == nil {
		t.Fatal("a work tree with no harness output must be refused, not passed")
	}
}

// TestSchemaSquirrelAllPortDifferential is the corpus gate. It compares
// Squirrel with every port the harness ran, over every case both answered.
// make schema-squirrel-all-port-differential runs the harness first; the test
// reads its output tree.
func TestSchemaSquirrelAllPortDifferential(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the generated conformance corpus and every port's conformance driver")

	root := repoRoot(t)
	work := os.Getenv(differentialWorkEnv)
	if work == "" {
		work = filepath.Join(root, "build", "conformance-differential")
	} else if !filepath.IsAbs(work) {
		work = filepath.Join(root, work)
	}
	if _, err := os.Stat(filepath.Join(work, "out")); errors.Is(err, fs.ErrNotExist) {
		// THE SKIP IS A FAILURE WHEN SOMETHING PROMISED THE CORPUS, the rule
		// the fixed-form byte-oracle suites keep: a bare `go test ./...` that
		// never ran the harness has nothing to compare and says so, while
		// make schema-squirrel-all-port-differential sets SCHEMA_REQUIRE_CORPUS
		// and a missing tree there is red.
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the differential has no harness output under %s", work)
		}
		t.Skip("the harness output is not built: make schema-squirrel-all-port-differential")
	}
	n, ports, divergences, err := compareSquirrelAgainstPorts(work)
	if err != nil {
		t.Fatalf("%v; run make schema-squirrel-all-port-differential", err)
	}
	if n == 0 || len(ports) == 0 {
		t.Fatalf("the differential compared no answer against any port under %s; a lens over nothing is not a pass", work)
	}
	for _, d := range divergences {
		t.Error(d)
	}
	t.Logf("squirrel agreed with %d port(s) on %d answer(s): %s", len(ports), n, strings.Join(ports, ", "))
}
