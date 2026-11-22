package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestTimestampKindIsReadAndScoped pins the front end of the `timestamp` kind
// (SPEC.md §4.13, docs/SPEC-TABLES.md §2.11): it is read as a kind in a table
// body, it stays an ordinary name everywhere else, and every placement the
// first major does not carry is refused BY NAME, once, in the checker.
func TestTimestampKindIsReadAndScoped(t *testing.T) {
	t.Parallel()

	// READ: a bare timestamp field of a table, and a field still NAMED timestamp
	good := `package probe
table Event
{
    timestamp   int64
    opened_at   timestamp
    closed_at   timestamp | json = "closed"
}
fixed table Session
{
    started     timestamp
}
`
	u := checkUnit(t, good)
	var seen []string
	for _, f := range u.Files {
		for _, st := range f.Tables {
			for _, fld := range st.Fields {
				if ir.TableScalarKind(fld) == 36 { // the timestamp kind's wire number
					if fld.Type.Kind != ir.TInt {
						t.Errorf("%s.%s: a timestamp is an int64 with a flag, got kind %v", st.Name, fld.Name, fld.Type.Kind)
					}
					seen = append(seen, st.Name+"."+fld.Name)
					if fld.Type.Width != 64 || !fld.Type.Signed {
						t.Errorf("%s.%s: a timestamp is a signed 64 bit count, got width %d signed %t", st.Name, fld.Name, fld.Type.Width, fld.Type.Signed)
					}
				}
			}
		}
	}
	if got := strings.Join(seen, " "); got != "Event.opened_at Event.closed_at Session.started" {
		t.Fatalf("the timestamp fields read as %q", got)
	}

	// SCOPED: each refusal names the construct
	for _, c := range []struct{ name, schema, want string }{
		{"a packet type", "package probe\ntype P\n{\n    at timestamp\n}\n", "timestamp is a TABLE field type"},
		{"an array", "package probe\ntable T\n{\n    at [4]timestamp\n}\n", "an array of timestamp"},
		{"a counted array", "package probe\ntable T\n{\n    at [..4]timestamp\n}\n", "an array of timestamp"},
		{"an unbounded array", "package probe\ntable T\n{\n    at []timestamp\n}\n", "an array of timestamp"},
		{"an optional", "package probe\ntable T\n{\n    at ?timestamp\n}\n", "?timestamp is a named follow-on"},
		{"a default", "package probe\ntable T\n{\n    at timestamp = 5\n}\n", "a timestamp takes no specified default"},
		{"a map value", "package probe\ntable T\n{\n    m map[uint8]timestamp\n}\n", "a map of timestamp values"},
		{"a union arm", "package probe\nunion U\n{\n    at timestamp\n}\ntable T\n{\n    u U\n}\n", "not a union arm payload"},
		{"a range", "package probe\ntable T\n{\n    at timestamp | min = 0, max = 5\n}\n", "min/max are not valid on timestamp"},
	} {
		if got := refuse(t, c.schema); !strings.Contains(got, c.want) {
			t.Errorf("%s: refused, but not by name (want %q): %s", c.name, c.want, got)
		}
	}

	// an undeclared name is still an undefined type, and a declaration of that
	// name still wins: timestamp is a contextual word, not a reserved one
	if got := refuse(t, "package probe\ntable T\n{\n    at stamp\n}\n"); !strings.Contains(got, "undefined type stamp") {
		t.Errorf("an undefined type: %s", got)
	}
	checkUnit(t, "package probe\ntype timestamp\n{\n    ns int64\n}\ntable Holder\n{\n    at timestamp\n}\n")
}
