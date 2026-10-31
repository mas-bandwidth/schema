package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// mooControls are the moo port's negative controls, each a make target that
// make/negative-controls.json must run on a moo row.
var mooControls = []string{
	"packet-analyze-moo-negative-control",
	"packet-wire-moo-emitter-negative-control",
	"packet-wire-moo-negative-control",
	"schema-moo-generated-analyze-strict-gate-negative-control",
	"schema-moo-generated-analyze-strict-gate-pin-negative-control",
}

// TestSchemaMooGeneratedAnalyzeStrictGateWiring holds the moo strict-analyze
// gate (docs/PORT-MOO.md, layer 7) to what makes it a gate rather than a green
// line: the make targets run their tests through test/slowgate/proof behind the
// toolchain gate, the test reads output (moo-analyze exits 0 on a lint
// warning) in strict mode over a corpus that names every Makefile corpus
// directory, the analyzer is held to the pin, every moo control is registered
// on a row that installs the moo pin, and the workflows run it. Pure text over
// tracked files: it needs no moo, so it rides `go test ./...` anywhere; the
// make targets are what run the analyzer.
func TestSchemaMooGeneratedAnalyzeStrictGateWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	read := func(parts ...string) string {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Join(parts...), err)
		}
		return string(body)
	}

	// THE MAKE FRAGMENT: each target behind toolchain-moo, each through the
	// proof script naming the tests it proves.
	mk := read("make", "schema-moo-generated-analyze-strict-gate.mk")
	for target, proves := range map[string]string{
		"schema-moo-generated-analyze-strict-gate":                      "'TestMooGeneratedAnalyzeStrictGate'",
		"schema-moo-generated-analyze-strict-gate-negative-control":     "'TestMooGeneratedAnalyzeStrictGatePlantedPattern/type-error TestMooGeneratedAnalyzeStrictGatePlantedPattern/lint-warning'",
		"schema-moo-generated-analyze-strict-gate-pin-negative-control": "'TestMooGeneratedAnalyzeStrictGatePinNegativeControl'",
		"packet-wire-moo-negative-control":                              "'TestMooTypeWireWriteNegativeControl'",
		"packet-analyze-moo-negative-control":                           "'TestMooAnalyzerGateNegativeControl'",
	} {
		for _, want := range []string{
			".PHONY: " + target + "\n",
			"\n" + target + ": toolchain-moo\n",
			"sh test/slowgate/proof " + target + " \\\n\t\t" + proves + " \\\n\t\t./internal/codegen/moo -run ",
		} {
			if !strings.Contains(mk, want) {
				t.Errorf("make/schema-moo-generated-analyze-strict-gate.mk does not carry %q", want)
			}
		}
	}

	// THE EMITTER CONTROL: a sabotaged copy of the emitter through an overlay,
	// red on exactly the goldens that write a bool.
	for _, want := range []string{
		".PHONY: packet-wire-moo-emitter-negative-control\n",
		"\npacket-wire-moo-emitter-negative-control: toolchain-moo\n",
		"go run ./tools/sabotage -name packet-wire-moo-bool-twice",
		"go test -overlay=$(MOO_WIRE_NC_DIR)/overlay.json",
		"MOO_WIRE_NC_RED := inputpacket joins probearray rigidbody_at_rest rigidbody_moving shipcreate_flags testdata\n",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("make/schema-moo-generated-analyze-strict-gate.mk does not carry %q", want)
		}
	}

	// THE GATE'S TEST: strict mode, output read (not the exit code alone),
	// --!strict required, the runtime in the analyzed set, the pin checked, and
	// a corpus that names every directory the Makefile's SCHEMAS* wildcards
	// read (bench/corpus is per file, held below).
	gate := read("internal", "codegen", "moo", "strictgate_test.go")
	for _, want := range []string{
		`"--mode=strict"`,
		"len(bytes.TrimSpace(out)) != 0",
		`[]byte("--!strict\n")`,
		"the runtime module %s is not in the analyzed set",
		"moorun.Pin",
		`filepath.Join(dir, "PIN")`,
		"strictGateKnownRefusals",
		`{"type-error", `,
		`{"lint-warning", `,
		`"bench/corpus/Bench.schema"`,
		`"bench/corpus/RealWorld.schema"`,
		`"test/packet-defaults/*.schema"`,
		`"test/packet-wide/*.schema"`,
		`"test/tables/*.schema"`,
	} {
		if !strings.Contains(gate, want) {
			t.Errorf("internal/codegen/moo/strictgate_test.go does not carry %q", want)
		}
	}
	corpus := regexp.MustCompile(`\$\(wildcard ([^)*]+)/\*\.schema\)`)
	makefile := read("Makefile")
	dirs := 0
	for line := range strings.SplitSeq(makefile, "\n") {
		if !strings.HasPrefix(line, "SCHEMAS") || !strings.Contains(line, ":=") {
			continue
		}
		for _, m := range corpus.FindAllStringSubmatch(line, -1) {
			if m[1] == "bench/corpus" {
				continue
			}
			dirs++
			if !strings.Contains(gate, "\t\""+m[1]+"\",\n") {
				t.Errorf("the Makefile's corpus directory %s is not in the moo strict gate's strictGateDirs", m[1])
			}
		}
	}
	if dirs < 10 {
		t.Fatalf("read %d corpus directories out of the Makefile's SCHEMAS* lines: the reader is not reading them", dirs)
	}

	// THE PLAN: every moo control on a pull-request row that installs the moo
	// pin, and the pin field a marker, never a commit.
	var manifest struct {
		Groups []struct {
			Name    string   `json:"name"`
			When    string   `json:"when"`
			Moo     string   `json:"moo"`
			Targets []string `json:"targets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(read("make", "negative-controls.json")), &manifest); err != nil {
		t.Fatalf("parse make/negative-controls.json: %v", err)
	}
	for _, control := range mooControls {
		found := false
		for _, g := range manifest.Groups {
			for _, target := range g.Targets {
				if target != control {
					continue
				}
				found = true
				if g.When != "pull-request" {
					t.Errorf("%s runs in group %q on %q, want pull-request", control, g.Name, g.When)
				}
				if g.Moo != "pin" {
					t.Errorf("%s runs in group %q, which does not install the moo pin (moo %q, want \"pin\")", control, g.Name, g.Moo)
				}
			}
		}
		if !found {
			t.Errorf("%s is not registered in make/negative-controls.json", control)
		}
	}

	// THE CONTROL JOBS install the pin on exactly the rows that ask for it.
	for _, wf := range []string{"ci-full.yml", "certify.yml"} {
		body := read(".github", "workflows", wf)
		if !strings.Contains(body, "      - uses: ./.github/actions/moo\n        if: matrix.moo != ''\n") {
			t.Errorf(".github/workflows/%s: the negative-control job does not install the moo pin on `matrix.moo != ''` rows", wf)
		}
		// The java formatter keeps its own condition: a moo step spliced
		// between it and its `if` takes java's condition and leaves the
		// formatter running on every row.
		if strings.Contains(body, "      - uses: ./.github/actions/moo\n        if: matrix.java != ''\n") {
			t.Errorf(".github/workflows/%s: the moo step carries the java rows' condition", wf)
		}
	}

	// THE WORKFLOW runs the gate and both of its controls on the pin.
	wf := read(".github", "workflows", "schema-moo-generated-analyze-strict-gate.yml")
	for _, want := range []string{
		"push:",
		"pull_request:",
		"schedule:",
		"workflow_dispatch:",
		"uses: ./.github/actions/moo",
		"run: make schema-moo-generated-analyze-strict-gate\n",
		"run: make schema-moo-generated-analyze-strict-gate-negative-control\n",
		"run: make schema-moo-generated-analyze-strict-gate-pin-negative-control\n",
		"- 'internal/codegen/moo/**'",
		"- 'make/moo.mk'",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("the moo strict-gate workflow does not carry %q", want)
		}
	}

	// THE TOOLCHAIN CONTROL covers both moo pins: make/moo.mk registers them,
	// and toolchain-moo probes each by name, which is what the Makefile's
	// toolchain-negative-control points at an absent path one at a time.
	moo := read("make", "moo.mk")
	for _, want := range []string{
		"TOOLCHAIN_PINS_moo := MOO MOO_ANALYZE",
		"$(call toolchain_probe,moo,MOO,$(MOO))",
		"$(call toolchain_probe,moo,MOO_ANALYZE,$(MOO_ANALYZE))",
	} {
		if !strings.Contains(moo, want) {
			t.Errorf("make/moo.mk does not carry %q", want)
		}
	}
}
