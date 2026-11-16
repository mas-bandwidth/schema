package pythontable

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
)

func TestPythonSurfaceSelection(t *testing.T) {
	u := loadUnit(t, `package surfaces
fixed table Root { value int32 }
`)
	original, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	all, err := ParseSurfaces("wire,fixed,json,cook,block,view,message,retain")
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := GenerateSurface(u, all)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, explicit) {
		t.Fatal("default and full set differ")
	}
	wire, err := ParseSurfaces("wire")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := GenerateSurface(u, wire)
	if err != nil {
		t.Fatal(err)
	}
	code := string(selected["surfaces/Core.py"])
	if !strings.Contains(code, "Root.block = None") || strings.Contains(code, "Root.block = BlockTable(") {
		t.Fatal("wire selection did not remove block descriptor")
	}
	if !strings.Contains(string(original["surfaces/Core.py"]), "Root.block = BlockTable(") {
		t.Fatal("default lost block descriptor")
	}
	if got := surfaceForms(wire, u.Tables["Root"]); !reflect.DeepEqual(got, []string{"wire"}) {
		t.Fatalf("forms = %v", got)
	}
	if u.Tables["Root"].DeclaredForms() {
		t.Fatal("generation mutated the unit")
	}
	if _, err := ParseSurfaces("wire,unknown"); err == nil {
		t.Fatal("unknown surface accepted")
	}
}

func TestPythonSurfaceDeclaredIntersection(t *testing.T) {
	u := loadUnit(t, `package surfaces
table Alpha | forms = "wire,json" { value int32 }
table Beta | forms = "wire" { value int32 }
`)
	for _, x := range []struct{ list, table, form string }{{"wire,cook", "Alpha", "cook"}, {"wire,json", "Beta", "json"}, {"wire,block", "Alpha", "block"}} {
		s, err := ParseSurfaces(x.list)
		if err != nil {
			t.Fatal(err)
		}
		err = ValidateSurfaces(u, s)
		if err == nil || !strings.Contains(err.Error(), x.table) || !strings.Contains(err.Error(), x.form) {
			t.Fatalf("%s refusal: %v", x.list, err)
		}
	}
	if err := ValidateSurfaces(u, gotable.DefaultSurfaces()); err != nil {
		t.Fatalf("default refused: %v", err)
	}
	if got := surfaceForms(gotable.DefaultSurfaces(), u.Tables["Alpha"]); !reflect.DeepEqual(got, []string{"wire", "json"}) {
		t.Fatalf("declared forms = %v", got)
	}
	if got := surfaceForms(gotable.DefaultSurfaces(), u.Tables["Beta"]); !reflect.DeepEqual(got, []string{"wire"}) {
		t.Fatalf("declared forms = %v", got)
	}
}
