package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCIUnsupportedSelectionMustNotCredit(t *testing.T) {
	for name, body := range map[string]string{
		"if-after-env-is-disabled":              "env:\n          SCHEMA_SLOW: '1'\n        if: false\n        run: go test ./compiler",
		"workflow-go-flags-change-execution":    "env:\n          SCHEMA_SLOW: '1'\n          GOFLAGS: '-list=.'\n        run: go test ./compiler",
		"directory-changes-package":             "env:\n          SCHEMA_SLOW: '1'\n        working-directory: test\n        run: go test ./compiler",
		"folded-block-is-not-separate-commands": "env:\n          SCHEMA_SLOW: '1'\n        run: >\n          go test ./compiler\n          go test ./unrelated",
		"list-does-not-execute":                 "env:\n          SCHEMA_SLOW: '1'\n        run: go test ./compiler -list .",
		"skip-excludes-required-test":           "env:\n          SCHEMA_SLOW: '1'\n        run: go test ./compiler -skip .",
		"run-pattern-is-not-package-selector":   "env:\n          SCHEMA_SLOW: '1'\n        run: go test ./unrelated -run './...'",
		"echo-is-not-execution":                 "env:\n          SCHEMA_SLOW: '1'\n        run: echo go test ./compiler",
		"unset-disables-following-command":      "env:\n          SCHEMA_SLOW: '1'\n        run: |\n          unset SCHEMA_SLOW\n          go test ./compiler",
		"true-is-not-one":                       "env:\n          SCHEMA_SLOW: 'true'\n        run: go test ./compiler",
		"comment-is-not-command":                "run: |\n          # SCHEMA_SLOW=1 go test ./compiler\n          true",
		"cd-changes-package":                    "env:\n          SCHEMA_SLOW: '1'\n        run: cd test && go test ./compiler",
		"ten-is-not-one":                        "run: SCHEMA_SLOW=10 go test ./compiler",
	} {
		t.Run(name, func(t *testing.T) {
			steps, err := parseWorkflowContent("ci.yml", "jobs:\n  test:\n    steps:\n      - name: witness\n        "+body+"\n")
			if err != nil {
				return
			} // An unsupported shape may refuse explicitly.
			if got := coveredByCI(steps, gatedTest{pkg: "./compiler", name: "TestRequiredGate"}); got != "" {
				t.Fatalf("credited unsupported/disabled selection to %s", got)
			}
		})
	}
}

func TestMalformedEventsAreNotSilentlyDropped(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	data := `{"Action":"output","Package":"github.com/mas-bandwidth/schema/compiler","Test":"TestA","Output":"SCHEMA_SLOW: this test shells out"}` + "\n" + `{"Action":"output","Package":"github.com/mas-bandwidth/schema/compiler","Test":"TestB","Output":"SCHEMA_SLOW: this test shells out"` + "\n"
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := gateSkips(p); err == nil {
		t.Fatalf("accepted malformed stream, keeping only %d of two gate events", len(got))
	}
}

func TestCIExplicitCommandsSelectOnlyTheirPackagesAndTests(t *testing.T) {
	for _, command := range []string{"go test ./compiler -run '^TestRequiredGate$'", "SCHEMA_SLOW=1 go test ./compiler -run '^TestRequiredGate$'", "env SCHEMA_SLOW=1 go test ./compiler -count=1 -run='^TestRequiredGate$'"} {
		selector, slow, ok := parseSupportedCommandLine(command, true)
		if !ok || !slow || !selector("./compiler", "TestRequiredGate") {
			t.Fatalf("supported command refused: %s", command)
		}
		if selector("./unrelated", "TestRequiredGate") || selector("./compiler", "TestOther") {
			t.Fatalf("selected outside command scope: %s", command)
		}
	}
}
