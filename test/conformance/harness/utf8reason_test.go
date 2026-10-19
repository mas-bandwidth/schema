package main

import "testing"

// TestHostileUtf8ReasonScoresWrongReasonRed pins the reason str-illformed-at-bound
// owes its carrying port (docs/SPEC-TABLES.md §16.3): the key whose value held
// the ill-formed bytes is `name`, and a port that answers the old `<root>`
// scores red. A port the manifest does not name is not asked, so the C++ leg
// keeps its own verdict until its port takes the rule.
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
	if !m.HostileReasonOK(caseName, "go", want) {
		t.Fatalf("%s: the right Go reason scored red", caseName)
	}
	if m.HostileReasonOK(caseName, "go", "string_not_utf8: <root>") {
		t.Fatalf("%s: a Go reason of string_not_utf8: <root> scored green; the key is name", caseName)
	}
	if _, asked := m.HostileReasonFor(caseName, "cpp"); asked {
		t.Fatalf("%s: the manifest asks C++ for a reason it does not carry", caseName)
	}
	if !m.HostileReasonOK(caseName, "cpp", "anything the unported leg answers") {
		t.Fatalf("%s: the port outside the carrying set must be unaffected", caseName)
	}
}
