package compiler

import (
	"os"
	"strings"
	"testing"
)

// TestIssue474ContributingDescribesCAA holds the contributing page to the
// workflow that gates every pull request. .github/workflows/cla.yml blocks a
// pull request until each human commit author signs the Contributor Assignment
// Agreement, an assignment stronger than the CLA that docs/CONTRIBUTING.md once
// denied. The page has to name the agreement and quote the exact sentence the
// action matches, and it must never claim there is no CLA.
func TestIssue474ContributingDescribesCAA(t *testing.T) {
	workflow, err := os.ReadFile("../.github/workflows/cla.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workflow), "I have read the CAA and I hereby sign it") {
		t.Fatalf(".github/workflows/cla.yml no longer gates on a CAA signature; this gate names the wrong file")
	}
	page, err := os.ReadFile("../docs/CONTRIBUTING.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(page)
	if strings.Contains(strings.ToLower(text), "there is no cla") {
		t.Errorf("docs/CONTRIBUTING.md claims there is no CLA while the workflow holds every pull request until the author signs a copyright assignment")
	}
	if !strings.Contains(text, "## The Contributor Assignment Agreement") {
		t.Errorf("docs/CONTRIBUTING.md has no Contributor Assignment Agreement section")
	}
	if !strings.Contains(text, "I have read the CAA and I hereby sign it, assigning copyright in my contributions to Más Bandwidth LLC.") {
		t.Errorf("docs/CONTRIBUTING.md does not quote the exact sentence .github/workflows/cla.yml matches")
	}
}
