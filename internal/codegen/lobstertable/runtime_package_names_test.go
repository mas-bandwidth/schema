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
`
	got := runtimeBindings(source)
	for _, name := range []string{"argument", "generic", "callback", "nested", "first", "second", "mutable", "text", "element", "index"} {
		if !got[name] {
			t.Errorf("missed binding %s", name)
		}
	}
	for _, name := range []string{"comment_name", "comment_arg", "field_name", "literal_name", "sample", "Holder", "int", "fun"} {
		if got[name] {
			t.Errorf("nonbinding %s claimed", name)
		}
	}
	// A newly introduced local must automatically become reserved.
	if got["future_local"] {
		t.Fatal("control already present")
	}
	if !runtimeBindings(source + "\ndef extra():\n    let future_local = 0\n")["future_local"] {
		t.Fatal("new local escaped scanner")
	}
}

func TestLobsterRuntimePackageBindingRefusal(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"report", "cell", "rec", "w", "v", "r", "s"} {
		if err := checkRuntimePackageName(&ir.Unit{Package: name, Tables: map[string]*ir.Struct{"Root": {}}}); err == nil {
			t.Errorf("runtime package %s accepted", name)
		}
	}
	if err := checkRuntimePackageName(&ir.Unit{Package: "custom_registry_probe"}); err != nil {
		t.Fatal(err)
	}
}
