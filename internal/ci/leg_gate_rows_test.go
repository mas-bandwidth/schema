package ci

import (
	"strings"
	"testing"
)

// legGateTargets are the make targets the retired leg-gates-nightly.yml's
// leg-gate jobs ran (leg-gate, leg-gate-lobster, leg-gate-rust,
// leg-gate-squirrel, leg-gate-cpp): the fixed form and versioning gates of
// every leg. The workflow was retired into the registry; each target is a
// registry row now (test/registry/retired.tsv names the row).
var legGateTargets = strings.Fields(`tables-fixedform-plain tables-go-fixed-form tables-go-versioning
	tables-c-fixedform tables-c-versioning tables-c-clean tables-c-format-negative-control
	tables-rust-fixedform tables-rust-versioning-t12 tables-js-fixed-form tables-js-versioning
	tables-cs-leg-debug tables-cs-versioning tables-java-fixedform tables-dart-fixed-form tables-dart-versioning
	tables-elixir-fixed-form tables-elixir-versioning tables-luau-fixed-form tables-luau-versioning bench-luau-gate
	test-goose tables-goose-versioning tables-goose-message-form tables-lobster-clean bench-lobster-gate
	cards-squirrel-test
	conformance-negative-control-lobster conformance-negative-control-lobster-walk
	conformance-negative-control-lobster-framing conformance-negative-control-lobster-cycle
	conformance-negative-control-lobster-write-refusal conformance-negative-control-lobster-write-refusal-message
	tables-lobster-wire-fuzz test-lobster-packages test-lobster
	tables-rust-versioning-probes
	cards-squirrel-conformance cards-squirrel-conformance-negative-control
	cards-squirrel-conformance-upgrade-negative-control cards-squirrel-conformance-hash-negative-control
	tables-fixedform-asan tables-cpp-clean tables-cpp-format-negative-control tables-cpp-warnings-negative-control`)

// TestTheLegGatesAreRegistryRows holds every leg gate the retired workflow
// ran to a registry row that runs it (the nightly group, or a faster tier):
// a gate dropped from the registry is red here by name, where before
// checkWorkflowPolicy held it to a leg-gates-nightly.yml job.
func TestTheLegGatesAreRegistryRows(t *testing.T) {
	rows := registryMakeRows(t, repoRoot(t))
	for _, target := range legGateTargets {
		row, ok := rows[target]
		if !ok {
			t.Errorf("no registry row runs make %s: the leg gate leg-gates-nightly.yml ran is run by nothing", target)
			continue
		}
		switch row.Tier {
		case "unit", "functional", "nightly":
		default:
			t.Errorf("row %s runs make %s at tier %q, which no CI lane runs", row.Key, target, row.Tier)
		}
	}
}
