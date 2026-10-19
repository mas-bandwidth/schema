package gotable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const utf8ReasonKeySchema = `package probe

type Inner {
    tag string(16)
}
fixed table Root {
    name  string(16)
    inner Inner
}
`

func TestGoJsonUtf8RefusalNamesTheKey(t *testing.T) {
	t.Parallel()

	bad := "\xff"
	cases := []struct {
		name string
		text string
		want string
	}{
		{"field value", `{"name":"aa` + bad + `"}`, "string_not_utf8: name"},
		{"nested field value", `{"inner":{"tag":"aa` + bad + `"}}`, "string_not_utf8: tag"},
		{"object key", `{"na` + bad + `me":1}`, "string_not_utf8: na" + bad + "me"},
	}

	runGenerated(t, utf8ReasonKeySchema, `package probe
import "testing"
func TestGoJsonUtf8ReasonNamesTheKey(t *testing.T) {
 cases := []struct{ name, text, want string }{
  {"field value", "{\"name\":\"aa`+"\\xff"+`\"}", "string_not_utf8: name"},
  {"nested field value", "{\"inner\":{\"tag\":\"aa`+"\\xff"+`\"}}", "string_not_utf8: tag"},
  {"object key", "{\"na`+"\\xff"+`me\":1}", "string_not_utf8: na`+"\\xff"+`me"},
 }
 for _, c := range cases {
  var v Root
  var r TableReport
  if RootFromJson(&v, []byte(c.text), &r) {
   t.Fatalf("%s: accepted ill-formed UTF-8", c.name)
  }
  if !r.Malformed {
   t.Fatalf("%s: not malformed: %+v", c.name, r)
  }
  if r.Reason != c.want {
   t.Fatalf("%s: reason %q, want %q", c.name, r.Reason, c.want)
  }
 }
 var v Root
 var r TableReport
 if !RootFromJson(&v, []byte("{\"name\":\"ok\"}"), &r) || r.Malformed || r.Reason != "" {
  t.Fatalf("clean read: %+v", r)
 }
}
`)

	model := tabletext.NewModel(unitFrom(t, utf8ReasonKeySchema))
	root := model.Lookup("Root")
	for _, c := range cases {
		inst := model.New(root)
		var r tabletext.Report
		if model.Read(inst, []byte(c.text), &r) {
			t.Fatalf("%s: engine accepted ill-formed UTF-8", c.name)
		}
		if !r.Malformed || r.Reason != c.want {
			t.Fatalf("%s: engine report %+v, want Malformed reason %q", c.name, r, c.want)
		}
	}

	// A VALUE WITH NO KEY IS THE TOP (docs/SPEC-TABLES.md §16.3): the packer's
	// ReadValue hands the field's text without an enclosing key, so the reason
	// says <root> rather than inventing one.
	inst := model.New(root)
	fv, ok := inst.FieldByKey("name")
	if !ok {
		t.Fatal("Root has no name field")
	}
	var r tabletext.Report
	if model.ReadValue(fv, []byte(`"aa`+bad+`"`), &r) {
		t.Fatal("ReadValue accepted ill-formed UTF-8")
	}
	if !r.Malformed || r.Reason != "string_not_utf8: <root>" {
		t.Fatalf("ReadValue report %+v, want Malformed reason string_not_utf8: <root>", r)
	}
}
