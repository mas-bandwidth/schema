package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCsGeneratedNullableCaAnalyzersGateWiring holds the card's own gate to
// its shape: the make target, the corpus it generates, the workflow triggers,
// the justified suppressions, and the negative control's registration. The
// property itself is the target's exit status and the planted control; this
// test keeps the wiring from disappearing without a red.
func TestCsGeneratedNullableCaAnalyzersGateWiring(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	mk := filepath.Join(root, "make/schema-cs-generated-nullable-ca-analyzers-gate.mk")
	mkText := readGateFile(t, mk)
	for _, want := range []string{
		"schema-cs-generated-nullable-ca-analyzers-gate:",
		"schema-cs-generated-nullable-ca-analyzers-gate-negative-control:",
		"<Nullable>enable</Nullable>",
		"<TreatWarningsAsErrors>true</TreatWarningsAsErrors>",
		"<AnalysisMode>All</AnalysisMode>",
		"--out $(CSG_CORPUS)/examples examples",
		"--out $(CSG_CORPUS)/bench bench/corpus/Bench.schema",
		"--out $(CSG_CORPUS)/bench/realworld bench/corpus/RealWorld.schema",
		"--out $(CSG_CORPUS)/ludicrous examples128",
		"PlantedNullableBug.cs",
		"CS8602",
	} {
		if !strings.Contains(mkText, want) {
			t.Errorf("%s: missing %q", mk, want)
		}
	}
	for _, line := range strings.Split(mkText, "\n") {
		if strings.Contains(line, "SuppressMessage") && !strings.Contains(line, "Justification") {
			t.Errorf("%s: suppression without a justification: %s", mk, strings.TrimSpace(line))
		}
		if strings.Contains(line, "NoWarn=") || strings.Contains(line, "<NoWarn>") {
			t.Errorf("%s: blanket NoWarn is not a justified suppression: %s", mk, strings.TrimSpace(line))
		}
	}

	wf := filepath.Join(root, ".github/workflows/schema-cs-generated-nullable-ca-analyzers-gate.yml")
	wfText := readGateFile(t, wf)
	for _, want := range []string{
		"schedule:",
		"workflow_dispatch:",
		"pull_request:",
		"make schema-cs-generated-nullable-ca-analyzers-gate\n",
		"make schema-cs-generated-nullable-ca-analyzers-gate-negative-control",
	} {
		if !strings.Contains(wfText, want) {
			t.Errorf("%s: missing %q", wf, want)
		}
	}

	manifest, err := os.ReadFile(filepath.Join(root, "make/negative-controls.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Groups []struct {
			Targets []string `json:"targets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range m.Groups {
		for _, target := range g.Targets {
			if target == "schema-cs-generated-nullable-ca-analyzers-gate-negative-control" {
				found = true
			}
		}
	}
	if !found {
		t.Error("make/negative-controls.json: the planted nullable control is in no group")
	}
}

func readGateFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
