package pythontable

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/ir"
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

func TestPythonRetainSurfaceSelection(t *testing.T) {
	u := loadUnit(t, `package surfaces
 table Root { value int32 }
 fixed table Fixed { value int32 }
`)
	wire, err := ParseSurfaces("wire")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := GenerateSurface(u, wire)
	if err != nil {
		t.Fatal(err)
	}
	code, init := string(plain["surfaces/Core.py"]), string(plain["surfaces/__init__.py"])
	for _, text := range []string{"def load_retain(", "def save_retain(", "def measure_retain(", "TableRetain"} {
		if strings.Contains(code, text) || strings.Contains(init, text) {
			t.Fatalf("wire-only emitted %s", text)
		}
	}
	for _, name := range []string{"surfaces/_schema/retain.py", "surfaces/_schema/retain_known.py"} {
		if _, ok := plain[name]; ok {
			t.Fatalf("wire-only emitted %s", name)
		}
	}
	selected, err := ParseSurfaces("wire,retain")
	if err != nil {
		t.Fatal(err)
	}
	retained, err := GenerateSurface(u, selected)
	if err != nil {
		t.Fatal(err)
	}
	code, init = string(retained["surfaces/Core.py"]), string(retained["surfaces/__init__.py"])
	for _, text := range []string{"def load_retain(", "def save_retain(", "def measure_retain(", "def load_retain_messages(", "def save_retain_messages("} {
		if !strings.Contains(code, text) {
			t.Fatalf("retain selection omitted %s", text)
		}
	}
	if !strings.Contains(init, "TableRetain") {
		t.Fatal("retain selection omitted export")
	}
	for _, name := range []string{"surfaces/_schema/retain.py", "surfaces/_schema/retain_known.py"} {
		if _, ok := retained[name]; !ok {
			t.Fatalf("retain selection omitted %s", name)
		}
	}
	fieldName := loadUnit(t, "package names\ntable Root { ordinary int32 }\n")
	fieldName.Tables["Root"].Fields[0].Name = "load_retain" // exercise the emitter's own selection-aware name check
	typeName := loadUnit(t, "package names\ntable Alias { n int32 }\n")
	alias := typeName.Tables["Alias"]
	delete(typeName.Tables, "Alias")
	alias.Name = "TableRetain"
	typeName.Tables["TableRetain"] = alias
	for _, named := range []*ir.Unit{fieldName, typeName} {
		if _, err := GenerateSurface(named, wire); err != nil {
			t.Fatalf("wire-only name refused: %v", err)
		}
		if _, err := GenerateSurface(named, selected); err == nil {
			t.Fatal("retain collision accepted")
		}
	}
}
