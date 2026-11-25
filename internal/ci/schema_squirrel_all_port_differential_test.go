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
// present answers must be byte-identical after the harness's own
// equivalences (normalizeDifferentialAnswer) — the same accept/refuse verdict,
// the same decoded value, and the same value re-saved on the wire (the
// `<case>.wire` files are compared raw).
//
// THE EQUIVALENCES ARE THE HARNESS'S, AND NOTHING ELSE IS NORMALISED.
// testdata/conformance/tables/FORMAT.md fixes each, and the harness's run.go
// applies each when it scores a leg against the oracle:
//
//   - report, framing, json-hostile, retain: the verdict line is `read` or
//     `refused`; the legacy seven-field counter line
//     (`unknown,kind_mismatch,widened,clamped,duplicate,malformed,verdict`)
//     is readable transport for the same one bit, and "no comparison uses"
//     the counters (FORMAT.md, reports.txt). Both sides are reduced to the
//     plain verdict line exactly as normalizeReadAnswer does; the value text
//     after a `read` line is kept byte for byte. An answer the harness would
//     reject (a refusal carrying value bytes, a malformed counter line) is
//     compared raw, so it still diverges.
//   - write-refusal*: `unrepresentable` "answers a `refused` row and no
//     other" (FORMAT.md, write-refusal) — the leg's own types refused the
//     value before its writer could. It is compared as `refused`; `wrote`
//     against either stays a divergence.
//   - message-prior: a `<case>.refused` file's contents "are the leg's own
//     diagnostic and are never compared" (FORMAT.md, message-prior;
//     docs/rulings.json refusal-verdict-only). Every one reads as `refused`;
//     the re-saved batch in `<case>` is compared raw.
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
	"strconv"
	"strings"
	"testing"
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
//
// One answer the harness credits without holding it to bytes is not
// comparable either, and is not counted: a read-into `unrepresentable` (the
// leg's fixed load always takes a capacity, so the row has no call to hold,
// FL-17; the harness credits it on a fixed-nocap or retain-fixed row and
// fails it on any other). Every retain-refusal answer is compared.
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
				if surface.Name() == "read-into" &&
					(string(mine) == "unrepresentable\n" || string(theirs) == "unrepresentable\n") {
					continue
				}

				comparisons++
				compared[other.lang] = true
				if !bytes.Equal(normalizeDifferentialAnswer(surface.Name(), name, mine),
					normalizeDifferentialAnswer(surface.Name(), name, theirs)) {
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

// normalizeDifferentialAnswer applies the harness's answer equivalences
// (see the file comment) and nothing else. A `<case>.wire` write-back file is
// returned untouched on every surface.
func normalizeDifferentialAnswer(surface, name string, answer []byte) []byte {
	if strings.HasSuffix(name, ".wire") {
		return answer
	}
	if surface == "message-prior" && strings.HasSuffix(name, ".refused") {
		// THE REFUSAL IS THE VERDICT; ITS CONTENTS ARE THE LEG'S OWN
		// DIAGNOSTIC and are never compared (FORMAT.md, message-prior;
		// docs/rulings.json refusal-verdict-only; SPEC-CONFORMANCE.md §4).
		// The harness's scorePrior reads only that the file exists. Squirrel
		// writes its reason name, C++ the word `refused`, Go its report:
		// every one of them is the same verdict.
		return []byte("refused\n")
	}
	switch {
	case surface == "retain-refusal":
		// THE UNBOUND STORE'S SAVE REFUSES (retainrefusal.go,
		// retainRefusalUnboundLegs): python and ruby bind the store to the
		// root it loaded, so the save after the refused read raises rather
		// than writing; the two read verdicts are the same, and the harness
		// rejects save-refused from any other leg. That leg writes no
		// `<case>.wire`, so there is no second save to compare.
		if string(answer) == "read\nrefused\nsave-refused\n" {
			return []byte("read\nrefused\nsaved\n")
		}
	case surface == "report" || surface == "framing" || surface == "json-hostile" || surface == "retain":
		if norm, ok := normalizeDifferentialReadAnswer(answer); ok {
			return norm
		}
	case strings.HasPrefix(surface, "write-refusal"):
		if string(answer) == "unrepresentable\n" {
			return []byte("refused\n")
		}
	}
	return answer
}

// normalizeDifferentialReadAnswer is test/conformance/harness's
// normalizeReadAnswer and ParseCounts (run.go, wirefuzz.go), which live in a
// main package this one cannot import. It reports false for an answer the
// harness refuses to parse, which the caller then compares raw.
func normalizeDifferentialReadAnswer(answer []byte) ([]byte, bool) {
	line, value, _ := bytes.Cut(answer, []byte{'\n'})
	trimmed := strings.TrimSpace(string(line))
	refused := false
	switch trimmed {
	case "refused":
		refused = true
	case "read":
	default:
		parts := strings.Split(trimmed, ",")
		if len(parts) != 7 {
			return nil, false
		}
		for _, p := range parts[:5] {
			n, err := strconv.Atoi(p)
			if err != nil || strconv.Itoa(n) != p {
				return nil, false
			}
		}
		var malformed bool
		switch parts[5] {
		case "true":
			malformed = true
		case "false":
		default:
			return nil, false
		}
		switch parts[6] {
		case "refused":
			refused = true
		case "read":
		default:
			return nil, false
		}
		refused = refused || malformed
	}
	if refused {
		if len(bytes.TrimSpace(value)) > 0 {
			return nil, false
		}
		return []byte("refused\n"), true
	}
	return append([]byte("read\n"), value...), true
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

// TestSquirrelAllPortDifferentialNormalizesOnlyTheHarnessEquivalences holds
// the equivalences to exactly their reach: a legacy counter line equals
// its plain verdict and `unrepresentable` equals `refused`, while a changed
// verdict, a changed value, a refusal carrying value bytes, `wrote` against
// `unrepresentable`, the same spellings on a surface the rule does not name,
// and a changed write-back wire all still diverge.
func TestSquirrelAllPortDifferentialNormalizesOnlyTheHarnessEquivalences(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, surface, file, squirrel, other string
		diverges                             bool
	}{
		{"counter line read", "report", "C", "read\n{}\n", "0,0,0,0,0,false,read\n{}\n", false},
		{"counter line refused", "framing", "C", "refused\n", "0,0,0,0,0,true,refused\n", false},
		{"malformed flag is refusal", "json-hostile", "C", "refused\n", "0,0,0,0,0,true,read\n", false},
		{"counters are not compared", "retain", "C", "read\nv\n", "3,1,0,2,0,false,read\nv\n", false},
		{"verdict differs", "report", "C", "read\n{}\n", "0,0,0,0,0,false,refused\n", true},
		{"value differs", "report", "C", "read\n{\"a\":1}\n", "0,0,0,0,0,false,read\n{\"a\":2}\n", true},
		{"refusal carrying a value", "framing", "C", "refused\n", "refused\n{}\n", true},
		{"malformed counter line", "report", "C", "read\n", "0,0,0,0,false,read\n", true},
		{"counter line off its surfaces", "wire", "C", "read\n", "0,0,0,0,0,false,read\n", true},
		{"write-back wire is raw", "report", "C.wire", "AAAA", "AAAB", true},
		{"unrepresentable answers refused", "write-refusal", "W", "refused\n", "unrepresentable\n", false},
		{"unrepresentable on json", "write-refusal-json", "W", "unrepresentable\n", "refused\n", false},
		{"unrepresentable is not wrote", "write-refusal-fixed", "W", "wrote\n", "unrepresentable\n", true},
		{"unrepresentable off its surfaces", "report", "W", "refused\n", "unrepresentable\n", true},
		{"prior refusal diagnostic is not compared", "message-prior", "P.refused", "out_of_range", "refused", false},
		{"prior re-saved batch is raw", "message-prior", "P", "AAAA", "AAAB", true},
		{"refusal diagnostic off message-prior", "report", "P.refused", "out_of_range", "refused", true},
		{"unbound save-refused is saved", "retain-refusal", "R", "read\nrefused\nsaved\n", "read\nrefused\nsave-refused\n", false},
		{"retain-refusal read verdict differs", "retain-refusal", "R", "read\nrefused\nsaved\n", "read\nread\nsave-refused\n", true},
		{"retain-refusal second save is raw", "retain-refusal", "R.wire", "AAAA", "AAAB", true},
		{"save-refused off retain-refusal", "retain", "R", "read\nrefused\nsaved\n", "read\nrefused\nsave-refused\n", true},
		{"read-into value differs", "read-into", "I", "read\n5000\n", "read\n4000\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			work := t.TempDir()
			writeAnswers(t, work, "squirrel", map[string]map[string]string{tc.surface: {tc.file: tc.squirrel}})
			writeAnswers(t, work, "go", map[string]map[string]string{tc.surface: {tc.file: tc.other}})
			n, _, divergences, err := compareSquirrelAgainstPorts(work)
			if err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Fatalf("compared %d answers, want 1", n)
			}
			if got := len(divergences) == 1; got != tc.diverges {
				t.Fatalf("%s/%s: %q vs %q: diverges = %v, want %v (%v)",
					tc.surface, tc.file, tc.squirrel, tc.other, got, tc.diverges, divergences)
			}
		})
	}
}

// TestSquirrelAllPortDifferentialLeavesUncomparableAnswersOut holds the one
// answer the harness credits without bytes to exactly its reach: a read-into
// `unrepresentable` on either side is not compared, while `unrepresentable`
// on another surface still diverges, and every retain-refusal answer is
// compared for every leg.
func TestSquirrelAllPortDifferentialLeavesUncomparableAnswersOut(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, lang, surface, file, squirrel, other string
		compared, diverges                         bool
	}{
		{"read-into unrepresentable there", "go", "read-into", "ri_retain_fixed", "read\nzeroed\n", "unrepresentable\n", false, false},
		{"read-into unrepresentable here", "go", "read-into", "ri_fixed_5000", "unrepresentable\n", "read\n5000\n", false, false},
		{"retain-refusal second save", "lobster", "retain-refusal", "retain_refusal_body.wire", "AAAA", "AAAABBBB", true, true},
		{"retain-refusal answer", "lobster", "retain-refusal", "retain_refusal_body", "read\nrefused\nsaved\n", "read\nread\nsaved\n", true, true},
		{"retain-refusal another leg", "goose", "retain-refusal", "retain_refusal_body.wire", "AAAA", "AAAABBBB", true, true},
		{"unrepresentable off read-into", "go", "report", "ri_fixed_5000", "unrepresentable\n", "read\n5000\n", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			work := t.TempDir()
			writeAnswers(t, work, "squirrel", map[string]map[string]string{tc.surface: {tc.file: tc.squirrel}})
			writeAnswers(t, work, tc.lang, map[string]map[string]string{tc.surface: {tc.file: tc.other}})
			n, _, divergences, err := compareSquirrelAgainstPorts(work)
			if err != nil {
				t.Fatal(err)
			}
			if got := n == 1; got != tc.compared {
				t.Fatalf("%s/%s: compared %d answers, want compared = %v", tc.surface, tc.file, n, tc.compared)
			}
			if got := len(divergences) == 1; got != tc.diverges {
				t.Fatalf("%s/%s: diverges = %v, want %v (%v)", tc.surface, tc.file, got, tc.diverges, divergences)
			}
		})
	}
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

	root := repoRoot(t)
	work := os.Getenv(differentialWorkEnv)
	if work == "" {
		work = filepath.Join(root, "build", "conformance-differential")
	} else if !filepath.IsAbs(work) {
		work = filepath.Join(root, work)
	}
	if _, err := os.Stat(filepath.Join(work, "out")); errors.Is(err, fs.ErrNotExist) {
		// a missing tree is red, never a pass and never a skip: a bare
		// `go test` that never ran the harness has nothing to compare
		t.Fatalf("the differential has no harness output under %s: run make schema-squirrel-all-port-differential", work)
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
