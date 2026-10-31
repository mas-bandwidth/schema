package mootable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/jsontestsuite"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE OUTSIDE ORACLE (docs/SPEC-TABLES.md §16.12): json.moo's JSON layer over
// every JSONTestSuite file, against the table the standard carries, with
// PENDING's files allowed to disagree and logged — the engine's own gate
// (internal/tabletext's TestJSONTestSuiteEngine) asked of moo.

func mooSuiteVerdicts(t *testing.T, s *jsontestsuite.Suite, runtime map[string][]byte) map[string]bool {
	t.Helper()
	names := s.Names()
	inputs := map[string][]byte{}
	for i, name := range names {
		inputs[suiteInput(i)] = s.Files[name]
	}
	var b strings.Builder
	b.WriteString(probePrelude)
	b.WriteString("for i = 1, " + itoa(len(names)) + " do\n\thexs(if J.jsonLayerAccepts(buffer.tostring(inputs[\"f\" .. (i - 1)])) then \"accept\" else \"refuse\")\nend\n")
	lines := jsonProbe(t, b.String(), inputs, runtime)
	if len(lines) != len(names) {
		t.Fatalf("the probe printed %d verdicts for %d files", len(lines), len(names))
	}
	got := map[string]bool{}
	for i, name := range names {
		got[name] = lines[i] == "accept"
	}
	return got
}

func suiteInput(i int) string { return "f" + itoa(i) }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for ; i > 0; i /= 10 {
		d = append([]byte{byte('0' + i%10)}, d...)
	}
	return string(d)
}

func TestMooJSONTestSuite(t *testing.T) {
	slowtest.Gate(t, "moo (it runs json.moo's JSON layer)")
	t.Parallel()
	s, err := jsontestsuite.Load(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	mismatches, pending := jsontestsuite.Check(s, mooSuiteVerdicts(t, s, nil))
	for _, p := range pending {
		t.Logf("pending: %s", p)
	}
	for _, m := range mismatches {
		t.Errorf("mismatch: %s", m)
	}
}

// TestMooJSONTestSuiteNegativeControl lets the layer ignore data after the
// root value (§16.1, THE ROOT OBJECT IS THE WHOLE TEXT) and requires the suite
// to name the files that see it.
func TestMooJSONTestSuiteNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a sabotaged json.moo)")
	t.Parallel()
	s, err := jsontestsuite.Load(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	src := string(Runtime()["json.moo"])
	const site = "\tlocal ok = skipValue(r, 0)\n\tspace(r)\n\treturn ok and not r.bad and r.pos == r.n + 1\n"
	if !strings.Contains(src, site) {
		t.Fatal("the control's site moved: json.moo's jsonLayerAccepts changed")
	}
	// the sabotage: data after the root value is ignored
	broken := strings.Replace(src, site, "\tlocal ok = skipValue(r, 0)\n\tspace(r)\n\treturn ok and not r.bad\n", 1)
	mismatches, _ := jsontestsuite.Check(s, mooSuiteVerdicts(t, s, map[string][]byte{"json.moo": []byte(broken)}))
	if len(mismatches) == 0 {
		t.Fatal("ignoring data after the root value passed JSONTestSuite: the gate cannot see the end-of-text rule")
	}
	t.Logf("ignoring trailing data: %d files named", len(mismatches))
}
