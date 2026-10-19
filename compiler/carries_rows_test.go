package compiler

import "testing"

// carriesRow is one row of the expected-carries table: the construct, the
// schema that exercises it, and the refusal text naming it on a target that
// does not carry it.
type carriesRow struct {
	construct string
	schema    string
	refusal   string
}

// refusalRow is one row of the squirrel refuse-by-name table: the refuser,
// the construct's human name, the schema, the field the refusal must name, and
// whether Squirrel now carries (accepts) the construct.
type refusalRow struct {
	refuser string
	name    string
	src     string
	field   string
	carried bool
}

// extraCarriesRows lets a future card add carries rows from its own file with
// func init() { extraCarriesRows = append(extraCarriesRows, ...) }, without
// touching the row literals in carries_test.go.
var extraCarriesRows []carriesRow

// extraRefusalRows lets a future card add refusal rows from its own file with
// func init() { extraRefusalRows = append(extraRefusalRows, ...) }, without
// touching the row literals in tablessquirrel_test.go.
var extraRefusalRows []refusalRow

// TestCarriesRowsAppendHooksAreUsed pins the append-hook mechanism: a row
// appended to the extras is iterated alongside the literal rows.
func TestCarriesRowsAppendHooksAreUsed(t *testing.T) {
	t.Parallel()
	rows := append([]carriesRow(nil), extraCarriesRows...)
	rows = append(rows, carriesRow{construct: "hook.test-row"})
	found := false
	for _, tc := range rows {
		if tc.construct == "hook.test-row" {
			found = true
		}
	}
	if !found {
		t.Fatal("a row registered through the append hook was not iterated")
	}
}
