package compiler

import (
	"os"
	"strings"
	"testing"
)

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

// The rows below are registered through the hooks, from a test-only init, the
// same way a future card's own file adds a row. They exercise a construct the
// literals already cover with a second schema, so they are real rows the table
// checks run, not markers only this test can see. The refusal row is a PACKET
// value default: the table-wire default is carried by the tagged Squirrel
// compiler now (compiler/target_squirrel_defaults.go), while the packet-wire
// default is not, so refuseUnported still refuses squirrel here.
const (
	hookCarriesConstruct = carryRequired
	hookCarriesSchema    = "package probe\ntable Crate\n{\n    label string(16) | required\n}\n"
	hookRefusalRefuser   = "refuseUnported"
	hookRefusalField     = "L.payload"
)

func init() {
	extraCarriesRows = append(extraCarriesRows, carriesRow{
		construct: hookCarriesConstruct,
		schema:    hookCarriesSchema,
		refusal:   "refuses | required",
	})
	extraRefusalRows = append(extraRefusalRows, refusalRow{
		refuser: hookRefusalRefuser,
		name:    "hook packet value default",
		src:     "package probe\n\ntype L\n{\n    payload string(8) = \"hi\"\n}\n",
		field:   hookRefusalField,
		carried: false,
	})
}

// carriesTable is the table the real carries test iterates: the row literal
// followed by the rows registered through the hook.
func carriesTable(literal []carriesRow) []carriesRow {
	return append(append([]carriesRow(nil), literal...), extraCarriesRows...)
}

// refusalTable is the table the real refusal test iterates: the row literal
// followed by the rows registered through the hook.
func refusalTable(literal []refusalRow) []refusalRow {
	return append(append([]refusalRow(nil), literal...), extraRefusalRows...)
}

// TestCarriesRowsAppendHooksAreUsed pins the append-hook mechanism: the rows
// registered by init are in the tables the real tests iterate, and both real
// tests iterate through carriesTable/refusalTable, so an extras row reaches
// every per-construct check without an edit to the row literals.
func TestCarriesRowsAppendHooksAreUsed(t *testing.T) {
	t.Parallel()
	if !slicesContainsCarriesRow(carriesTable(nil), hookCarriesConstruct, hookCarriesSchema) {
		t.Fatal("the carries row registered by init() was not iterated")
	}
	if !slicesContainsRefusalRow(refusalTable(nil), hookRefusalRefuser, hookRefusalField) {
		t.Fatal("the refusal row registered by init() was not iterated")
	}
	for _, want := range []struct{ file, call string }{
		{"carries_test.go", "carriesTable(cases)"},
		{"tablessquirrel_test.go", "refusalTable(cases)"},
	} {
		src, err := os.ReadFile(want.file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), want.call) {
			t.Errorf("%s does not iterate through %s", want.file, want.call)
		}
	}
}

func slicesContainsCarriesRow(rows []carriesRow, construct, schema string) bool {
	for _, tc := range rows {
		if tc.construct == construct && tc.schema == schema {
			return true
		}
	}
	return false
}

func slicesContainsRefusalRow(rows []refusalRow, refuser, field string) bool {
	for _, tc := range rows {
		if tc.refuser == refuser && tc.field == field {
			return true
		}
	}
	return false
}
