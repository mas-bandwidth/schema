package elixirtable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The credo gate's targets, as make/elixir.mk names them (issue #547, the
// analyzer half).
const (
	credoGateTarget      = "tables-elixir-credo"
	credoPinTarget       = "tables-elixir-credo-pin"
	credoControlTarget   = "tables-elixir-credo-negative-control"
	credoToolchainTarget = "toolchain-elixir-credo"
)

// credoMake is make/elixir.mk read as a graph: each target's recipe lines,
// and the targets each one reaches, by prerequisite or by a `$(MAKE) <target>`
// line in its recipe. Targets with more than one rule (test-elixir is added to
// all over the file) collect the edges of every rule.
type credoMake struct {
	recipes map[string][]string
	edges   map[string][]string
}

var (
	credoRuleRE    = regexp.MustCompile(`^([A-Za-z0-9_./%-]+(?: [A-Za-z0-9_./%-]+)*):(?:[^=]|$)`)
	credoSubMakeRE = regexp.MustCompile(`\$\(MAKE\)((?: +-[-A-Za-z]+)*) +([A-Za-z0-9_.-]+)`)
)

func readCredoMake(t *testing.T) credoMake {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "make", "elixir.mk"))
	if err != nil {
		t.Fatal(err)
	}
	m := credoMake{recipes: map[string][]string{}, edges: map[string][]string{}}
	var current []string
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(line, "\t") {
			for _, target := range current {
				m.recipes[target] = append(m.recipes[target], line)
				for _, sub := range credoSubMakeRE.FindAllStringSubmatch(line, -1) {
					m.edges[target] = append(m.edges[target], sub[2])
				}
			}
			continue
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		current = nil
		match := credoRuleRE.FindStringSubmatch(line)
		if match == nil || strings.HasPrefix(line, ".PHONY") {
			continue
		}
		_, prereqs, _ := strings.Cut(line, ":")
		prereqs, _, _ = strings.Cut(prereqs, "#")
		for _, target := range strings.Fields(match[1]) {
			current = append(current, target)
			m.edges[target] = append(m.edges[target], strings.Fields(strings.ReplaceAll(prereqs, "|", " "))...)
		}
	}
	return m
}

// reachable is every target the named one reaches.
func (m credoMake) reachable(from string) map[string]bool {
	seen := map[string]bool{from: true}
	queue := []string{from}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, to := range m.edges[next] {
			if !seen[to] {
				seen[to] = true
				queue = append(queue, to)
			}
		}
	}
	return seen
}

// TestCredoGateRunsAtDefaultStrictness pins the credo gate's shape in
// make/elixir.mk (issue #547, the analyzer half): its `mix credo` line passes no
// --strict and no check override, so the gate is credo's DEFAULT strictness
// over credo's default checks; its first act is the credo pin; and neither the
// gate nor its control nor the fetch is reachable from test-elixir, because the
// per-pull-request image carries no credo. Both ride tables-elixir-release
// instead, after the toolchain target. It reads make/elixir.mk and nothing
// else: no Elixir, no clock, no network.
func TestCredoGateRunsAtDefaultStrictness(t *testing.T) {
	t.Parallel()

	m := readCredoMake(t)

	recipe := m.recipes[credoGateTarget]
	if len(recipe) == 0 {
		t.Fatalf("make/elixir.mk defines no recipe for %s", credoGateTarget)
	}
	var credoLines []string
	for _, r := range recipe {
		if strings.Contains(r, "$(MIX) credo") {
			credoLines = append(credoLines, r)
		}
	}
	if len(credoLines) != 1 {
		t.Fatalf("%s's recipe runs `$(MIX) credo` on %d lines, want exactly 1", credoGateTarget, len(credoLines))
	}
	// Every flag that raises the strictness or narrows, widens or drops the
	// check set. --all only lists every issue credo found at the default
	// priority (suggest otherwise elides some per category), and --format json
	// is how the recipe counts them; neither changes what is reported.
	for _, flag := range []string{
		"--strict", "--all-priorities", " -A", "--min-priority",
		"--checks", " -c ", "--only", "--checks-with-tag", "--checks-without-tag",
		"--ignore", " -i ", "--enable-disabled-checks", "--config-name", " -C ",
		"--config-file", "--mute-exit-status",
	} {
		if strings.Contains(credoLines[0], flag) {
			t.Errorf("%s's `mix credo` line passes %q, which moves it off credo's default strictness or default checks: %q",
				credoGateTarget, strings.TrimSpace(flag), strings.TrimSpace(credoLines[0]))
		}
	}

	if !strings.Contains(recipe[0], "$(MAKE)") || !strings.Contains(recipe[0], credoPinTarget) {
		t.Errorf("%s's first recipe line is %q; the pin (%s) must be checked first",
			credoGateTarget, strings.TrimSpace(recipe[0]), credoPinTarget)
	}
	pin := strings.Join(m.recipes[credoPinTarget], "\n")
	if !strings.Contains(pin, "$(ELIXIR_PIN_CREDO)") || !strings.Contains(pin, "exit 1") {
		t.Errorf("%s does not refuse a compiled credo that is not $(ELIXIR_PIN_CREDO)", credoPinTarget)
	}

	fromTest := m.reachable("test-elixir")
	if len(fromTest) < 10 {
		t.Fatalf("test-elixir reaches %d targets; the reader, not the property, broke", len(fromTest))
	}
	for _, target := range []string{credoGateTarget, credoControlTarget, credoToolchainTarget, credoPinTarget} {
		if fromTest[target] {
			t.Errorf("%s is reachable from test-elixir; the per-pull-request image carries no credo, so it rides tables-elixir-release only", target)
		}
	}
	fromRelease := m.reachable("tables-elixir-release")
	for _, target := range []string{credoToolchainTarget, credoGateTarget, credoControlTarget} {
		if !fromRelease[target] {
			t.Errorf("%s is not reachable from tables-elixir-release", target)
		}
	}
}
