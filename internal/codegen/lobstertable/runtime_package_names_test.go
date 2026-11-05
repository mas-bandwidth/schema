package lobstertable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestLobsterRuntimePackageBindingScan(t *testing.T) {
	t.Parallel()
	source := `// let comment_name = 1
class Holder:
    field_name:int
/* def hidden(comment_arg:int): */
def sample(argument:int, generic, callback:fun(nested:int)):
    let first, second = pair()
    var mutable = 1
    let text = "let literal_name = 1"
    for(items) element, index:
        print(element)
def extra():
    let future_local = 0
let top_level = 1
var other_top:int = 2
`
	got := runtimeBindings(source)
	for _, name := range []string{"Holder", "sample", "extra", "top_level", "other_top"} {
		if !got[name] {
			t.Errorf("missed namespace binding %s", name)
		}
	}
	for _, name := range []string{"argument", "generic", "callback", "nested", "first", "second", "mutable", "text", "element", "index", "future_local", "comment_name", "comment_arg", "field_name", "literal_name", "hidden"} {
		if got[name] {
			t.Errorf("non-namespace binding %s reserved", name)
		}
	}
	// A new namespace declaration must automatically become reserved.
	if got["new_global"] {
		t.Fatal("control already present")
	}
	if !runtimeBindings(source + "\nlet new_global = 0\n")["new_global"] {
		t.Fatal("new namespace declaration escaped the scan")
	}
	// A new local must not.
	if runtimeBindings(source + "\ndef later():\n    let new_local = 0\n")["new_local"] {
		t.Fatal("function local reserved as a package name")
	}
}

func TestLobsterRuntimePackageBindingRefusal(t *testing.T) {
	t.Parallel()
	// Namespace-scope runtime symbols still collide and are refused.
	for _, name := range []string{"unit_view", "view_types", "view_package", "set_unit", "ViewType"} {
		if err := checkRuntimePackageName(&ir.Unit{Package: name, Tables: map[string]*ir.Struct{"Root": {}}}); err == nil {
			t.Errorf("runtime namespace %s accepted", name)
		}
	}
	// Ordinary parameters and locals are not namespace collisions.
	tableFree := &ir.Unit{Package: "tag"}
	if err := checkRuntimePackageName(tableFree); err != nil {
		t.Errorf("package tag refused: %v", err)
	}
	withTable := &ir.Unit{Package: "t", Tables: map[string]*ir.Struct{"Root": {}}}
	if err := checkRuntimePackageName(withTable); err != nil {
		t.Errorf("package t refused: %v", err)
	}
	if err := checkRuntimePackageName(&ir.Unit{Package: "custom_registry_probe"}); err != nil {
		t.Fatal(err)
	}
}
