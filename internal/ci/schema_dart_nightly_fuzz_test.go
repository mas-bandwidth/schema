package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaDartNightlyFuzzWiring holds the card's artefacts to the shape that
// makes the nightly fuzz a gate rather than a script: a make fragment with a
// time budget, a workflow that fuzzes only on the schedule, a replay gate on
// pushes, a registry built from the generated corpus, saved regression inputs,
// and the planted-pattern control registered in the negative-control manifest.
//
// It reads files only. The heavy work is the make target the nightly workflow
// runs; this test is the cheap structural half that a push can afford.
func TestSchemaDartNightlyFuzzWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	makeFile := filepath.Join(root, "make", "schema-dart-nightly-fuzz.mk")
	makeBody, err := os.ReadFile(makeFile)
	if err != nil {
		t.Fatalf("the card's make fragment is missing: %v", err)
	}
	makeText := string(makeBody)
	for _, want := range []string{
		"DART_NIGHTLY_FUZZ_SECONDS ?=",
		"schema-dart-nightly-fuzz:",
		"schema-dart-nightly-fuzz-replay:",
		"schema-dart-nightly-fuzz-negative-control:",
		"--seconds $(DART_NIGHTLY_FUZZ_SECONDS)",
		"--seed $(DART_NIGHTLY_FUZZ_SEED)",
		"nightly-fuzz-regressions",
	} {
		if !strings.Contains(makeText, want) {
			t.Errorf("make/schema-dart-nightly-fuzz.mk does not carry %q", want)
		}
	}

	// The corpus is the set of Dart generate directories, not a second list.
	for _, unit := range []string{
		"tables/arms", "tables/backend", "tables/blobs", "tables/block",
		"tables/blockhome", "tables/examples", "tables/lists", "tables/maps",
		"tables/messages", "tables/pointers", "tables/scalars", "tables/stream",
		"tables/vocab", "tables/vocab9", "examples-wide",
		"test/tables/V1.schema", "test/tables/V2.schema",
		"test/tables/W1.schema", "test/tables/W2.schema",
		"test/tables/R1.schema", "test/tables/R2.schema",
		"test/tables/P1.schema", "test/tables/P3.schema",
		"test/tables/G1.schema", "test/tables/M1.schema", "test/tables/M2.schema",
	} {
		if !strings.Contains(makeText, unit) {
			t.Errorf("the nightly corpus omits %s, which a Dart generate target reads", unit)
		}
	}

	wf, ok := workflows(t, root)["schema-dart-nightly-fuzz.yml"]
	if !ok {
		t.Fatal("no .github/workflows/schema-dart-nightly-fuzz.yml")
	}
	if !strings.Contains(wf, "schedule:") || !strings.Contains(wf, "workflow_dispatch:") {
		t.Error("the nightly workflow needs both schedule: and workflow_dispatch:")
	}
	if !strings.Contains(wf, "pull_request:") || !strings.Contains(wf, "push:") {
		t.Error("the replay gate needs push: and pull_request: triggers")
	}
	if !strings.Contains(wf, "schema-dart-nightly-fuzz-replay") {
		t.Error("the workflow does not run the fast replay target")
	}
	if !strings.Contains(wf, "schema-dart-nightly-fuzz ") &&
		!strings.Contains(wf, "make schema-dart-nightly-fuzz") {
		t.Error("the scheduled job does not run the fuzz target")
	}

	// Saved regression inputs and the seed that reads them.
	regDir := filepath.Join(root, "testdata", "wire", "tables", "nightly-fuzz-regressions")
	entries, err := os.ReadDir(regDir)
	if err != nil {
		t.Fatalf("the saved regression corpus is missing: %v", err)
	}
	saved := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() == 0 {
			t.Errorf("%s is an empty regression input", e.Name())
			continue
		}
		if !strings.Contains(e.Name(), ".") {
			t.Errorf("%s does not name its table type before the first dot, so replay cannot select a reader", e.Name())
		}
		saved++
	}
	if saved == 0 {
		t.Fatal("the regression corpus holds no input — the replay gate would replay nothing")
	}

	// The planted control is registered with a reason, as the manifest requires.
	manifest, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Groups []struct {
			Name    string   `json:"name"`
			When    string   `json:"when"`
			Why     string   `json:"why"`
			Targets []string `json:"targets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range m.Groups {
		for _, target := range g.Targets {
			if target == "schema-dart-nightly-fuzz-negative-control" {
				if g.Name != "dart-nightly" {
					t.Errorf("the Dart fuzz control runs in group %q, want its own dart-nightly group", g.Name)
				}
				if g.When != "nightly" {
					t.Errorf("the fuzz control runs on %q; the one-nightly-budget plant belongs nightly", g.When)
				}
				if strings.TrimSpace(g.Why) == "" {
					t.Error("the fuzz control is registered without a justification")
				}
				if !strings.Contains(g.Why, "Dart 3.13.2") {
					t.Error("the Dart fuzz group's toolchain justification does not pin Dart 3.13.2")
				}
				found = true
			}
		}
	}
	if !found {
		t.Error("schema-dart-nightly-fuzz-negative-control is not in make/negative-controls.json")
	}

	// Pin the actual generated-reader count guard and its mutation, not the
	// explanatory comment, so a stale anchor cannot make the control inert.
	if !strings.Contains(makeText, "grep -rl 'if (count < 0 || count > (data.length - 9) ~/ 8) return 2;'") ||
		!strings.Contains(makeText, "if (count > (data.length - 9) ~/ 8) return 2; // SABOTAGED") {
		t.Error("the planted-pattern control does not remove the generated open() count bound")
	}

	// The control must make the removed bound reachable with a crafted
	// negative-count trailer. A random 64-bit write landing in the last eight
	// bytes is too rare for a 30-second budget, so the control carries a seed.
	if !strings.Contains(makeText, `printf '\001\177\376\377\377\377\377\377\377\377'`) ||
		!strings.Contains(makeText, "--seeds $(DART_NIGHTLY_FUZZ_BUILD)-nc/seeds") {
		t.Error("the planted-pattern control does not seed a reachable negative-count trailer")
	}
	if !strings.Contains(makeText, "schema-dart-nightly-fuzz-analyze:") ||
		!strings.Contains(makeText, "$(DART) analyze $(DART_NIGHTLY_FUZZ_BUILD)/analyze") {
		t.Error("the card's reader corpus is not analyzed by strict mode")
	}

	fuzzSource, err := os.ReadFile(filepath.Join(root, "test", "dart-tables", "nightly_fuzz.dart"))
	if err != nil {
		t.Fatal(err)
	}
	fuzzText := string(fuzzSource)
	if !strings.Contains(fuzzText, "strict save round-trip was refused") {
		t.Error("strict save round-trip ignores the second wire report")
	}
	if !strings.Contains(fuzzText, "if (target == 2) return null") ||
		!strings.Contains(fuzzText, "wire reader threw FormatException") {
		t.Error("FormatException is not distinguished between JSON refusal and wire-reader failure")
	}
}
