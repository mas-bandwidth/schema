package pythontable

import (
	"strings"
	"testing"
)

func TestPythonReflection(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, `package core
/// A record.
table Record
{
    /// A value.
    value int32 | deprecated
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	view, ok := files["core/_schema/unit_view.py"]
	if !ok {
		t.Fatal("unit view module core/_schema/unit_view.py is missing")
	}
	if !strings.Contains(string(view), "UnitView") {
		t.Fatal("unit view has no UnitView entry point")
	}
}
