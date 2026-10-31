package compiler

import (
	"slices"
	"strings"
	"testing"
)

// The cross-target carrier gates name the Goose leg only where the target
// is built in (`-tags schema_leg_goose`, compiler/target_goose.go): its table
// wire carries the constructs compiler/carries_goose_tablewire.go registers.
// gooseLegBuilt is that tag's answer (gooseleg_on_test.go, gooseleg_off_test.go).

// withGooseLeg is a sorted carrier list with goose in it when the leg is built.
func withGooseLeg(carriers []string) []string {
	if !gooseLegBuilt {
		return carriers
	}
	return slices.Sorted(slices.Values(append(slices.Clone(carriers), "goose")))
}

// gooseLegOrdered is a registry as the carrier gates compare it: sorted when
// the goose leg is built (its init registers before the targets' own files),
// in registration order otherwise.
func gooseLegOrdered(registered []string) []string {
	if !gooseLegBuilt {
		return registered
	}
	return slices.Sorted(slices.Values(registered))
}

// gooseLegSpelled is a refusal's carrier list as it reads with the goose leg
// built: goose sorts right after go.
func gooseLegSpelled(s string) string {
	if !gooseLegBuilt {
		return s
	}
	return strings.Replace(s, " go, ", " go, goose, ", 1)
}

// gooseCarriesTableWire holds the goose target to carrying a construct on the
// table wire: it generates, and it emits the form-1 runtime beside a table
// module (docs/PORT-GOOSE.md, THE TABLE WIRE).
func gooseCarriesTableWire(t *testing.T, files map[string][]byte, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("--lang goose refused a construct its table wire carries: %v", err)
	}
	table := false
	for name := range files {
		table = table || strings.HasSuffix(name, "Table.goose")
	}
	if !table || files["wire_load.goose"] == nil || files["wire_save.goose"] == nil {
		t.Fatalf("--lang goose emitted no table module and table-wire runtime")
	}
}
