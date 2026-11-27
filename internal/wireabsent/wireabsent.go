// Package wireabsent is the one spelling of the wire-fuzz summary's absence
// clause and of the absent-root ledger's path (docs/SPEC-CONFORMANCE.md
// W-ROSTER). The harness prints them (test/conformance/harness) and the
// release gate reads them back (tools/wirefuzzrelease); both take them from
// here, so a change to the words or the path changes both or neither, and the
// gate can never read a reworded clause as "no absence".
package wireabsent

import (
	"fmt"
	"path/filepath"
	"regexp"
)

// Dir is where the ledgers live, from the repository root.
const Dir = "test/conformance/wire-absent"

// LedgerPath is the ledger for a leg and arm (plain, builder or retain).
func LedgerPath(dir, leg, arm string) string {
	return filepath.Join(dir, leg+"-"+arm+".tsv")
}

// Clause is the summary's absence clause: n seeds absent, every one held by
// name in ledger.
func Clause(n int, ledger string) string {
	return fmt.Sprintf(", %d ledgered seeds absent (%s)", n, ledger)
}

// ClauseRE reads a Clause back: the count, then the ledger.
var ClauseRE = regexp.MustCompile(`, (\d+) ledgered seeds absent \(([^)]+)\)`)
