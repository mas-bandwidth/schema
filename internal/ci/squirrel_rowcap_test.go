package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// squirrelPerCommitCapSeconds is the fleet rule a runner enforces by killing
// a per-commit job (exit 137): two minutes. The recorded times below are the
// measured walls of the squirrel card rows after the split, not a live timer.
const squirrelPerCommitCapSeconds = 120

// recordedPartOverCap is the predicate the squirrel rows are held to. A
// planted 132 must trip it; a planted 104 must not.
func recordedPartOverCap(seconds float64) bool {
	return seconds > squirrelPerCommitCapSeconds
}

// TestSquirrelCardRowsFitTheCap holds the fixed-form squirrel card work to
// the per-commit cap. One leg-gate row ran dist-squirrel, the facade tests,
// the card corpus and the three negative controls together and measured 132 s
// wall (run 42599413205, job 129101577381). The three parts each record a
// time, and a part recorded past the cap fails here.
func TestSquirrelCardRowsFitTheCap(t *testing.T) {
	if !recordedPartOverCap(132) {
		t.Fatal("PLANTED: a recorded 132 s per-commit part must exceed the cap")
	}
	if recordedPartOverCap(104) {
		t.Fatal("PLANTED: a recorded 104 s part fits the cap")
	}

	body, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	re := regexp.MustCompile(`(?m)^[ \t]*# row-seconds (\S+) ([0-9]+(?:\.[0-9]+)?)\s*$`)
	got := map[string]float64{}
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		n, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			t.Fatalf("row-seconds %s: %v", m[1], err)
		}
		if _, dup := got[m[1]]; dup {
			t.Errorf("row-seconds %s is recorded twice", m[1])
		}
		got[m[1]] = n
		if recordedPartOverCap(n) {
			t.Errorf("row %s records %.0f s, past the %d s per-commit cap", m[1], n, squirrelPerCommitCapSeconds)
		}
	}
	for _, part := range []string{"test", "conformance", "negative"} {
		if _, ok := got[part]; !ok {
			t.Errorf("ci-fast.yml records no row-seconds for the squirrel %s part", part)
		}
	}

	const marker = "squirrel) make dist-squirrel"
	i := strings.Index(text, marker)
	if i < 0 {
		t.Fatal("ci-fast.yml has no squirrel fixed-form arm")
	}
	arm := text[i:]
	if end := strings.Index(arm, ";;"); end >= 0 {
		arm = arm[:end]
	}
	negatives := []string{
		"cards-squirrel-conformance-negative-control",
		"cards-squirrel-conformance-upgrade-negative-control",
		"cards-squirrel-conformance-hash-negative-control",
	}
	inArm := 0
	for _, target := range negatives {
		if strings.Contains(arm, target) {
			inArm++
		}
		if !strings.Contains(text, target) {
			t.Errorf("ci-fast.yml no longer runs %s", target)
		}
	}
	if inArm == len(negatives) {
		t.Error("the leg-gate squirrel arm still runs all three card negative controls in one row")
	}
	if !strings.Contains(text, "leg-gate-squirrel:") {
		t.Error("ci-fast.yml has no leg-gate-squirrel job for the split rows")
	}
}
