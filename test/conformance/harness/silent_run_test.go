// A RUN PROVES IT RAN (proveRan, run.go). `harness run` loops over legs,
// surfaces and cases, and each loop used to be able to run zero times, or
// fewer times than the manifest registers, under the success footer:
//
//   - no leg at all: an --only that names no registered leg, an empty
//     registry, a --skip that names every leg;
//   - a leg that answers nothing: every surface absent, or every case
//     `.absent`;
//   - a scoring loop that stops before the surface's last case.
//
// Each is red here, and each test carries its control: the same corpus with
// the run left whole stays green. A run with no leg, or no leg on any surface,
// is refused with an error (runWith's no-leg check and emptySelection); a
// surface the corpus gave 0 rows is strictRows' (zerorows_test.go).
package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func (c fakeCorpus) runOnly(t *testing.T, drivers, only, skip string, opts runOptions) (string, bool) {
	t.Helper()
	var out bytes.Buffer
	ok, err := runWith(&out, c.m, c.manifest, c.jsonDir, c.reports, drivers,
		filepath.Join(t.TempDir(), "work"), only, skip, opts)
	if err != nil {
		// a refused run is red: its error is what it says
		return out.String() + "\n" + err.Error(), false
	}
	return out.String(), ok
}

func TestRunWithNoLegIsRed(t *testing.T) {
	needShell(t)
	const says = "conformance ran no leg"

	t.Run("control: the leg --only names runs and is green", func(t *testing.T) {
		c := newFakeCorpus(t)
		out, ok := c.runOnly(t, c.committed(t, map[string]fakeLeg{referenceLang: {}, "zz": {}}), "zz", "", runOptions{})
		if !ok {
			t.Fatalf("CONTROL FAILED: a whole run of one leg is red:\n%s", out)
		}
		mustSay(t, out, []string{footer}, []string{says})
	})
	t.Run("--only names no registered leg", func(t *testing.T) {
		c := newFakeCorpus(t)
		out, ok := c.runOnly(t, c.committed(t, map[string]fakeLeg{referenceLang: {}, "zz": {}}), "zy", "", runOptions{})
		if ok {
			t.Errorf("a run of no leg passed:\n%s", out)
		}
		mustSay(t, out, []string{says, `--only "zy"`}, []string{footer})
	})
	t.Run("an empty registry", func(t *testing.T) {
		c := newFakeCorpus(t)
		empty := filepath.Join(t.TempDir(), "drivers.txt")
		writeFile(t, empty, "# no leg\n")
		out, ok := c.runOnly(t, empty, "", "", runOptions{})
		if ok {
			t.Errorf("a run over an empty registry passed:\n%s", out)
		}
		mustSay(t, out, []string{says}, []string{footer})
	})
	t.Run("--skip names every leg", func(t *testing.T) {
		c := newFakeCorpus(t)
		out, ok := c.runOnly(t, c.substituted(t, "zz", fakeLeg{}), "", "zz", runOptions{})
		if ok {
			t.Errorf("a run that skipped its only leg passed:\n%s", out)
		}
		mustSay(t, out, []string{says, "conformance SKIPS the zz leg"}, []string{footer})
	})
}

func TestLegThatAnswersNothingIsRed(t *testing.T) {
	needShell(t)
	const says = "the leg answered no case on any surface"
	// every grain at once: one surface left out of `list`, one exiting 2,
	// and the last case `.absent` — three lawful absences and no answer
	nothing := fakeLeg{unlisted: "json-write", exitTwo: "json-read", caseGone: true}

	t.Run("a port of the committed registry", func(t *testing.T) {
		c := newFakeCorpus(t)
		out, ok := c.runHarness(t, c.committed(t, map[string]fakeLeg{referenceLang: {}, "zz": nothing}))
		if ok {
			t.Errorf("a leg that answered nothing passed:\n%s", out)
		}
		mustSay(t, out, []string{"zz / wire: " + says}, []string{footer, "cpp / wire: " + says})
	})
	t.Run("the one leg of a substituted registry", func(t *testing.T) {
		c := newFakeCorpus(t)
		// emptySelection passes this run (its "pass 0/0" cells are not
		// absent); proveRan is what reads the leg answered nothing
		out, ok := c.runOnly(t, c.substituted(t, "zz", nothing), "", "", runOptions{})
		if ok {
			t.Errorf("a leg that answered nothing passed:\n%s", out)
		}
		mustSay(t, out, []string{says}, []string{footer})
	})
}

// TestRunStoppedShortIsRed is the count itself, with no driver: a surface
// that registers three cases and gave two a verdict is red, and so is a leg
// whose every verdict is absent; the whole run is green.
func TestRunStoppedShortIsRed(t *testing.T) {
	leg := func(wireCases map[string]string, total int) (map[string]map[string]*result, *result) {
		rs := map[string]*result{}
		for _, s := range surfaces {
			rs[s] = &result{}
		}
		rs["wire"] = &result{total: total, cases: wireCases}
		return map[string]map[string]*result{"zz": rs}, rs["wire"]
	}
	failures := func(results map[string]map[string]*result) string {
		var all []string
		for _, s := range surfaces {
			all = append(all, results["zz"][s].failures...)
		}
		return strings.Join(all, "\n")
	}

	whole, _ := leg(map[string]string{"a": "pass", "b": "absent", "c": "fail"}, 3)
	proveRan([]string{"zz"}, whole)
	if got := failures(whole); got != "" {
		t.Fatalf("CONTROL FAILED: a whole run is red: %s", got)
	}

	short, _ := leg(map[string]string{"a": "pass", "b": "pass"}, 3)
	proveRan([]string{"zz"}, short)
	if got := failures(short); !strings.Contains(got, "3 case(s) registered and 2 given a verdict") {
		t.Errorf("a surface that stopped one case short is not red: %q", got)
	}

	absent, _ := leg(map[string]string{"a": "absent", "b": "absent"}, 2)
	proveRan([]string{"zz"}, absent)
	if got := failures(absent); !strings.Contains(got, "the leg answered no case on any surface") {
		t.Errorf("a leg whose every verdict is absent is not red: %q", got)
	}
}
