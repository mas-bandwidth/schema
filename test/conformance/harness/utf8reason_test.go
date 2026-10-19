package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHostileUtf8ReasonScoresWrongReasonRed pins the LIVE path the harness
// takes for str-illformed-at-bound (docs/SPEC-TABLES.md §16.3): the manifest
// asks Go for `string_not_utf8: name`, hostileExpectations carries that beside
// the verdict, and the driver's <case>.reason sidecar is what scoreAnswer
// compares. A Go leg that still answers `string_not_utf8: <root>` therefore
// scores red even though its verdict is `refused`. A port the line does not
// name is not asked, so C++ keeps its own verdict until its port takes the rule.
func TestHostileUtf8ReasonScoresWrongReasonRed(t *testing.T) {
	t.Parallel()

	const root = "../../../"
	m, err := ReadManifest(root+"testdata/conformance/tables/MANIFEST.txt", root+"testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}

	const caseName = "str-illformed-at-bound"

	want, asked := m.HostileReasonFor(caseName, "go")
	if !asked {
		t.Fatalf("%s: the manifest asks Go for no reason; §16.3 names the key", caseName)
	}
	if want != "string_not_utf8: name" {
		t.Fatalf("%s: Go owes reason %q, got %q", caseName, "string_not_utf8: name", want)
	}
	if _, asked := m.HostileReasonFor(caseName, "cpp"); asked {
		t.Fatalf("%s: the manifest asks C++ for a reason it does not carry", caseName)
	}

	// THE REAL EXPECTATION LIST, not a hand-built one: if the run loop stops
	// emitting the reason expectation this test loses the case and says so.
	expectations, _ := hostileExpectations(m, "go")
	var got *expectation
	for i := range expectations {
		if expectations[i].name == caseName+".reason" {
			got = &expectations[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("%s: hostileExpectations asks Go for no .reason sidecar", caseName)
	}
	if string(got.want) != want+"\n" {
		t.Fatalf("%s: reason expectation %q, want %q", caseName, got.want, want+"\n")
	}

	dir := t.TempDir()
	sidecar := filepath.Join(dir, caseName+".reason")
	if err := os.WriteFile(sidecar, []byte("string_not_utf8: <root>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wrong := &result{}
	scoreAnswer("go", "json-hostile", dir, *got, "", wrong)
	if len(wrong.failures) == 0 {
		t.Fatalf("%s: the old <root> reason scored green; the key is name", caseName)
	}

	if err := os.WriteFile(sidecar, got.want, 0o644); err != nil {
		t.Fatal(err)
	}
	right := &result{}
	scoreAnswer("go", "json-hostile", dir, *got, "", right)
	if len(right.failures) != 0 || right.pass != 1 {
		t.Fatalf("%s: the right reason scored red: pass=%d failures=%v", caseName, right.pass, right.failures)
	}
}
