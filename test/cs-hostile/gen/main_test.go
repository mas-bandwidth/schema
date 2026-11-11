package main

import (
	"strings"
	"testing"
)

var cleanDispatchRow = row{name: "clean", carrier: "announce", schema: "tables/examples", table: "PackConfig"}

func badDispatchRows() []row {
	return []row{
		{name: "table-quote", carrier: "announce", schema: "tables/examples", table: `Pack"Config`},
		{name: "table-backslash", carrier: "announce", schema: "tables/examples", table: `Pack\Config`},
		{name: "table-space", carrier: "announce", schema: "tables/examples", table: "Pack Config"},
		{name: "schema-quote", carrier: "announce", schema: `tables/"examples`, table: "PackConfig"},
		{name: "schema-backslash", carrier: "announce", schema: `tables\examples`, table: "PackConfig"},
		{name: "schema-space", carrier: "announce", schema: "tables examples", table: "PackConfig"},
		{name: "carrier-quote", carrier: `ann"ounce`, schema: "tables/examples", table: "PackConfig"},
		{name: "carrier-backslash", carrier: `ann\ounce`, schema: "tables/examples", table: "PackConfig"},
		{name: "carrier-space", carrier: "ann ounce", schema: "tables/examples", table: "PackConfig"},
	}
}

// renderDispatchRows runs the generator's dispatch rendering on in-memory rows,
// without a compiler or a corpus on disk.
func renderDispatchRows(rows []row, check func(row) error) (string, error) {
	u := &unit{
		schema:    "tables/examples",
		dir:       "u00_tables_examples",
		namespace: "Examples",
		source:    "public static TableWire.Verdict AnnounceRead(TableVocabulary vocabulary, ReadOnlySpan<byte> bytes, TableReport report)",
	}
	bySchema := map[string]*unit{}
	for _, r := range rows {
		bySchema[r.schema] = u
	}
	src, err := renderDispatch(rows, bySchema, check)
	if err != nil {
		return "", err
	}
	return string(src), nil
}

// dispatchFieldFailures returns every way the dispatch field rule failed.
func dispatchFieldFailures(check func(row) error) []string {
	var failures []string
	for _, bad := range badDispatchRows() {
		_, err := renderDispatchRows([]row{bad}, check)
		switch {
		case err == nil:
			failures = append(failures, bad.name)
		case !strings.Contains(err.Error(), bad.name):
			failures = append(failures, bad.name+": error does not name the row: "+err.Error())
		}
	}
	src, err := renderDispatchRows([]row{cleanDispatchRow}, check)
	const want = `case "tables/examples\tPackConfig\tannounce": return u00_tables_examples_announce(d, 0);`
	switch {
	case err != nil:
		failures = append(failures, cleanDispatchRow.name+": "+err.Error())
	case !strings.Contains(src, want):
		failures = append(failures, cleanDispatchRow.name+": case not emitted")
	}
	return failures
}

func TestDispatchFieldsAreIdentifiers(t *testing.T) {
	t.Parallel()
	if failures := dispatchFieldFailures(validateRow); len(failures) != 0 {
		t.Fatalf("dispatch fields are not all checked: %v", failures)
	}
}

func TestDispatchFieldsAreIdentifiersNegativeControl(t *testing.T) {
	t.Parallel()
	failures := dispatchFieldFailures(func(row) error { return nil })
	missing := map[string]bool{}
	for _, r := range badDispatchRows() {
		missing[r.name] = true
	}
	for _, f := range failures {
		for name := range missing {
			if strings.Contains(f, name) {
				delete(missing, name)
			}
		}
	}
	if len(missing) != 0 {
		t.Fatalf("dropping the field check did not go red by name; missing %v (failures %v)", missing, failures)
	}
}
