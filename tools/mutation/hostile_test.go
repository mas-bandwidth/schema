package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Each target's hostile stage proves it tested by what its runner really
// prints. testdata/hostile/<lang>.txt is the whole output of the stage's own
// command on a bench, the unmutated tree, green: the proof must meet it, and
// must not meet the same output with every count zero and no case passed by
// name, which is what a runner that ran nothing prints. Lobster's summary,
// `192 pass, ... of 192 cases`, met no proof the sweep had, and its baseline
// was judged to have tested nothing.
func TestEveryHostileProofMeetsItsRunnersOutput(t *testing.T) {
	all, err := scopes(repo(t))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, s := range all {
		for _, st := range s.Stages {
			if st.Name != "hostile" {
				continue
			}
			lang := strings.TrimPrefix(s.Name, "target-")
			seen[lang] = true
			out, err := os.ReadFile(filepath.Join("testdata", "hostile", lang+".txt"))
			if err != nil {
				t.Errorf("target %s runs a hostile stage and has no recording of its runner's output: %v", lang, err)
				continue
			}
			proof := regexp.MustCompile(st.Proof)
			if !proof.Match(out) {
				t.Errorf("target %s: the hostile proof %q does not meet what its runner prints when it ran (testdata/hostile/%s.txt), so the stage's baseline is judged to have tested nothing", lang, st.Proof, lang)
			}
			if st.Counted && len(passedTests(out)) == 0 {
				t.Errorf("target %s: the hostile stage is counted and its recorded output passed no test by name", lang)
			}
			nothing := ranNothing(out)
			if proof.Match(nothing) && (!st.Counted || len(passedTests(nothing)) > 0) {
				t.Errorf("target %s: the hostile proof %q is met by a run of no case", lang, st.Proof)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("no target has a hostile stage: this test checked nothing")
	}
	// a recording with no stage is a target whose hostile stage went away
	files, err := os.ReadDir(filepath.Join("testdata", "hostile"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("testdata/hostile holds no recording: this check read nothing")
	}
	for _, f := range files {
		if lang, ok := strings.CutSuffix(f.Name(), ".txt"); !ok || !seen[lang] {
			t.Errorf("testdata/hostile/%s records a runner no hostile stage runs", f.Name())
		}
	}
}

var digits = regexp.MustCompile(`[0-9]+`)

// ranNothing is a runner's output as a run of no case prints it: every count
// zero, and no case or test passed by name.
func ranNothing(out []byte) []byte {
	var kept []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if passedTest.MatchString(line) || strings.HasPrefix(line, "PASS ") {
			continue
		}
		kept = append(kept, digits.ReplaceAllString(line, "0"))
	}
	return []byte(strings.Join(kept, "\n"))
}
