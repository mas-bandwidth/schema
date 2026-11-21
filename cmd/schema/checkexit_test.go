// `schema check` over a saved file: a refused form byte is a FAILING check
// (docs/SPEC-TABLES.md §3), and a schema source spelled `.Schema` is refused
// by name rather than read as a saved file whose `p` is a refused form byte.
// In-process and file-free: the CLI's own helpers, no binary is built.
package main

import (
	"strings"
	"testing"
)

func TestWireFormLinesReportARefusal(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		head    []byte
		refused bool
	}{
		{[]byte{1}, false},
		{[]byte{3}, false},
		{[]byte{5}, false}, // PLANNED is a form, not a refusal
		{[]byte{0}, true},
		{[]byte{6}, true},
		{[]byte{'p'}, true},
		{nil, true}, // an empty file
	} {
		lines, got := wireFormLines([]wireFile{{path: "saved.bin", head: row.head}})
		if got != row.refused {
			t.Errorf("head %v: refused = %v, want %v (%q)", row.head, got, row.refused, lines)
		}
		if len(lines) != 1 || !strings.HasPrefix(lines[0], "saved.bin: ") {
			t.Errorf("head %v: one line naming its file, got %q", row.head, lines)
		}
	}
	if _, refused := wireFormLines([]wireFile{{path: "a.bin", head: []byte{1}}, {path: "b.bin", head: []byte{0}}}); !refused {
		t.Error("one refused file among good ones must make the check fail")
	}
}

func TestSplitWireFilesRefusesAMisspelledSchemaExtension(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Ships.Schema", "ships.SCHEMA", "dir/Ships.sChEmA"} {
		if _, _, err := splitWireFiles([]string{name}); err == nil || !strings.Contains(err.Error(), ".schema in lowercase") {
			t.Errorf("%s: want a refusal naming the spelling, got %v", name, err)
		}
	}
	schemas, wires, err := splitWireFiles([]string{"Ships.schema"})
	if err != nil || len(schemas) != 1 || len(wires) != 0 {
		t.Errorf("Ships.schema: got schemas=%v wires=%v err=%v", schemas, wires, err)
	}
}
