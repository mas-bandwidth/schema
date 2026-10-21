package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const schemaJSGateMk = "schema-js-generated-checkjs-strict-eslint-gate.mk"

func readSchemaJSGateMk(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "make", schemaJSGateMk))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The card's make file is the lens, not a comment. It pins both tools, reads
// the whole corpus make/js.mk generates (the build tree included), and holds
// each refusal that keeps a gate from checking nothing: a missing directory, a
// tool off its pin, a file a tool did not analyse, eslint's ignored-file
// warnings and unused directives, and the inline switches that turn a checker
// off for a file. This reads the file only; the make targets are the proof
// that the refusals fire, and the workflow runs them.
func TestSchemaJSGeneratedCheckJSEslintGatePins(t *testing.T) {
	t.Parallel()
	mk := readSchemaJSGateMk(t)
	for _, want := range []string{
		"SCHEMA_JS_CHECKJS_TS_VERSION  ?= 5.9.3",
		"SCHEMA_JS_GATE_ESLINT_VERSION ?= 9.39.5",
		"--checkJs --strict",
		"override SCHEMA_JS_CHECKJS_BASELINE := 18047:1095,2322:303,2339:674,2345:202,2353:25,2365:50,2367:3,2554:94,4104:26,7005:447,7006:36708,7008:103,7022:26,7023:113,7034:213,7053:1000",
		"internal/ci/schema_js_checkjs_ratchet.mjs",
		"ratchet-self-test",
		"@$(SCHEMA_JS_GATE_OUT)/tsc.rsp",
		"empty corpus file",
		"--listFiles",
		"--max-warnings 0",
		"--report-unused-disable-directives-severity error",
		"NOT ANALYSED",
		"tool missing: eslint",
		"tool missing: tsc",
		"holds no .js file",
		"is missing",
		"@ts-nocheck|@ts-ignore|eslint-env",
		"SCHEMA_JS_GATE_FULL_DIRS := $(SCHEMA_JS_GATE_FAST_DIRS) build/tables-generated-js",
		"generated/js generated/js-ludicrous generated/bench/js generated/bench/paired/js",
		"find \"$$d\" -name '*.js' -type f",
		`"no-var":"error"`,
		`"eqeqeq"`,
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("make/%s does not name %q", schemaJSGateMk, want)
		}
	}
	// skipLibCheck would leave the gate's own .d.ts unchecked, and npx would
	// fetch the checker at gate time instead of probing the pinned install.
	for _, never := range []string{"--skipLibCheck", "npx", "xargs $(SCHEMA_JS_GATE_TSC)"} {
		if strings.Contains(mk, never) {
			t.Errorf("make/%s names %q", schemaJSGateMk, never)
		}
	}
}

// The plant goes into a REAL generated file with its header kept, and the
// control runs the same gate targets, not a copy of their flags.
func TestSchemaJSCheckJSRatchetSelfTestCoversPartialFailureAndGrowth(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "ci", "schema_js_checkjs_ratchet.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, want := range []string{
		"partial baseline diagnostics plus a tool failure must fail closed",
		"growth in a baseline diagnostic code must fail",
		"a signal-like status must not be treated as a diagnostics completion",
		"unlocated diagnostic-shaped output must fail",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("checkJs ratchet self-test does not cover %q", want)
		}
	}
}

func TestSchemaJSGeneratedCheckJSEslintGatePlantIsInARealFile(t *testing.T) {
	t.Parallel()
	mk := readSchemaJSGateMk(t)
	for _, want := range []string{
		"SCHEMA_JS_GATE_PLANT_FILE := generated/js/Degenerate.js",
		"^// Code generated .* DO NOT EDIT",
		"trap 'cp $(SCHEMA_JS_GATE_NC)/original.js $(SCHEMA_JS_GATE_PLANT_FILE)' EXIT",
		"$(MAKE) --no-print-directory schema-js-generated-checkjs-strict-eslint-gate-eslint SCHEMA_JS_GATE_DIRS",
		"$(MAKE) --no-print-directory schema-js-generated-checkjs-strict-eslint-gate-checkjs SCHEMA_JS_GATE_DIRS",
		"error no-var ",
		"error eqeqeq ",
		"error no-dupe-keys ",
		"): error TS2741",
		"the plant file was not restored",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("make/%s negative control does not name %q", schemaJSGateMk, want)
		}
	}
	header, err := os.ReadFile(filepath.Join(repoRoot(t), "generated", "js", "Degenerate.js"))
	if err != nil {
		t.Fatal(err)
	}
	first := strings.SplitN(string(header), "\n", 2)[0]
	if !strings.HasPrefix(first, "// Code generated ") || !strings.HasSuffix(first, "DO NOT EDIT.") {
		t.Fatalf("generated/js/Degenerate.js first line %q is not the generated header the plant keeps", first)
	}
}

// The control is registered in the repository's negative-control registry. It
// is listed as excluded from the shared legs, with its reason, because its own
// workflow runs it with the pinned eslint and TypeScript those legs do not
// install; tools/negativecontrols refuses a target that is in neither list.
func TestSchemaJSGeneratedCheckJSEslintGateControlIsRegistered(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "negative-controls.json"))
	if err != nil {
		t.Fatal(err)
	}
	var reg struct {
		Groups []struct {
			Targets []string `json:"targets"`
		} `json:"groups"`
		Excluded []struct {
			Target string `json:"target"`
			Reason string `json:"reason"`
		} `json:"excluded"`
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatal(err)
	}
	const target = "schema-js-generated-checkjs-strict-eslint-gate-negative-control"
	found := false
	for _, e := range reg.Excluded {
		if e.Target == target {
			found = true
			if !strings.Contains(e.Reason, "schema-js-generated-checkjs-strict-eslint-gate.yml") {
				t.Errorf("the exclusion of %s does not name the workflow that runs it: %q", target, e.Reason)
			}
		}
	}
	if !found {
		t.Fatalf("make/negative-controls.json does not register %s", target)
	}
	for _, g := range reg.Groups {
		for _, tg := range g.Targets {
			if strings.HasPrefix(tg, "schema-js-generated-checkjs-strict-eslint-gate") {
				t.Errorf("%s is in a shared leg that does not install the pinned tools", tg)
			}
		}
	}
	wf, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "schema-js-generated-checkjs-strict-eslint-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wf), "make "+target) {
		t.Fatalf("the card's workflow does not run %s", target)
	}
}
