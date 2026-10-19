package compiler

import (
	"path/filepath"
	"runtime"
	"testing"
)

// carriesCase is one row of the expected-carries table: the construct, the
// schema that exercises it, and the refusal text naming it on a target that
// does not carry it. Every construct registers its own row from its own file,
// so landing one construct never edits another construct's row.
type carriesCase struct {
	construct string
	schema    string
	refusal   string
	file      string // basename of the *_test.go file that registered it
}

// carriesCases is the append-only expected-carries table.
var carriesCases []carriesCase

func addCarriesCase(c carriesCase) {
	c.file = callerTestFile()
	carriesCases = append(carriesCases, c)
}

// refusalCase is one row of the squirrel refuse-by-name table: the refuser,
// the construct's human name, the schema, the field the refusal must name, and
// whether Squirrel now carries (accepts) the construct.
type refusalCase struct {
	construct string
	refuser   string
	name      string
	src       string
	field     string
	carried   bool
	file      string // basename of the *_test.go file that registered it
}

// refusalCases is the append-only squirrel refuse-by-name table.
var refusalCases []refusalCase

func addRefusalCase(c refusalCase) {
	c.file = callerTestFile()
	refusalCases = append(refusalCases, c)
}

// callerTestFile returns the basename of the test file whose init() registered
// the row.
func callerTestFile() string {
	_, file, _, ok := runtime.Caller(2)
	if !ok {
		return ""
	}
	return filepath.Base(file)
}

// expectedRowFile names the one file a construct's rows must live in.
func expectedRowFile(construct string) string {
	return "carries_" + construct + "_test.go"
}

// TestCarriesRowsAreRegisteredPerFile guards the append-only tables: every row
// is registered from the construct's own file, and no row is registered twice.
func TestCarriesRowsAreRegisteredPerFile(t *testing.T) {
	t.Parallel()
	for _, c := range carriesCases {
		if want := expectedRowFile(c.construct); c.file != want {
			t.Errorf("construct %q rows are defined in %s, want %s", c.construct, c.file, want)
		}
	}
	seen := map[string]bool{}
	for _, c := range carriesCases {
		if seen[c.construct] {
			t.Errorf("construct %q is registered twice", c.construct)
		}
		seen[c.construct] = true
	}
	rseen := map[string]bool{}
	seenRefusers := map[string]bool{}
	for _, c := range refusalCases {
		if want := expectedRowFile(c.construct); c.file != want {
			t.Errorf("refusal %q rows are defined in %s, want %s", c.construct, c.file, want)
		}
		if rseen[c.construct] {
			t.Errorf("refusal construct %q is registered twice", c.construct)
		}
		rseen[c.construct] = true
		if seenRefusers[c.refuser] {
			t.Errorf("refuser %q is registered twice", c.refuser)
		}
		seenRefusers[c.refuser] = true
	}
}
