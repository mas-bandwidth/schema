package darttable

import (
	"strings"
	"testing"
)

// A field whose accessor would take a row cursor's own member name (`at`,
// the cursor's offset) spells its accessors from `<name>$` instead: a schema
// name never holds a `$`, so the escape collides with nothing. Before it, a
// fixed table with a field named `at` (tables/messages InsertText) generated
// a Row that redeclared `at` and did not compile.
func TestRowAccessorEscapesTheCursorMembers(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Cursor { line int32 }

fixed table Edit
{
    at       Cursor
    view     int32
    row_size int32
    origin   int32
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var cook string
	for name, data := range files {
		if strings.HasSuffix(name, "Cook.dart") && strings.Contains(string(data), "final class EditRow") {
			cook = string(data)
		}
	}
	if cook == "" {
		t.Fatal("no EditRow in the cook output")
	}
	row := dartClassBody(t, cook, "EditRow")
	for _, want := range []string{
		"int at;", "CursorRow at$(CursorRow into)", "int get view$", "int get rowSize$", "int get origin",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("no %q in\n%s", want, row)
		}
	}
	if strings.Contains(row, "CursorRow at(") || strings.Contains(row, "int get view =>") {
		t.Errorf("an accessor still takes a cursor member's name:\n%s", row)
	}
}
