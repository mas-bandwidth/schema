// A text list's total in the checker (docs/SPEC-TABLES.md §2.13, §11): the
// attribute accepted on the four spellings, its value recorded, and each
// refusal the section states matched by its diagnostic text.
package check

import (
	"strings"
	"testing"
)

func TestTextListTotalAccepted(t *testing.T) {
	t.Parallel()
	u := buildUnit(t, "package t\nconst N = 6\ntable L {\n    names [..8]string(8) | total = 12\n    maybe [..8]?string(8) | total = N * 2\n    repos []string(8) | total = 1\n    bases []?string(8) | total = 4096, json = \"b\"\n    plain [..8]string(8)\n}\n")
	l := u.Tables["L"]
	for i, want := range []int64{12, 12, 1, 4096, 0} {
		if got := l.Fields[i].TextTotal; got != want {
			t.Errorf("field %s: TextTotal %d, want %d", l.Fields[i].Name, got, want)
		}
	}
	if l.Fields[3].JsonKey != "b" {
		t.Error("json beside total was dropped")
	}
}

func TestTextListTotalRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, want, src string }{
		{"total on a string", "field s: total applies only to a text list",
			"package t\ntable L {\n    s string(8) | total = 4\n}\n"},
		{"total on an array of int32", "field xs: total applies only to a text list",
			"package t\ntable L {\n    xs [..4]int32 | total = 4\n}\n"},
		{"total on a type field", "field s: total applies only to a text list",
			"package t\ntype P {\n    s string(8) | total = 4\n}\n"},
		{"total below 1", "field names: total = 0 is below 1",
			"package t\ntable L {\n    names [..4]string(8) | total = 0\n}\n"},
		{"total negative", "field names: total = -3 is below 1",
			"package t\ntable L {\n    names [..4]string(8) | total = -3\n}\n"},
		{"total not an integer", "field names: total takes an integer",
			"package t\ntable L {\n    names [..4]string(8) | total = \"a\"\n}\n"},
		{"total repeated", "attribute total repeated",
			"package t\ntable L {\n    names [..4]string(8) | total = 4, total = 5\n}\n"},
		{"a text list with a total in a fixed table", "a text list is a VARIABLE-table construct",
			"package t\nfixed table F {\n    names [..4]string(8) | total = 4\n}\n"},
		{"a text list with a total in a type body", "a text list is a VARIABLE-table construct",
			"package t\ntype P {\n    names [..4]string(8) | total = 4\n}\n"},
		{"a text list with a total as a union arm", "a text list is not an arm",
			"package t\nunion U {\n    words [..4]string(8) | total = 4\n}\ntable H {\n    u U\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			errs := runUnit(t, map[string]string{"T.schema": tc.src})
			for _, e := range errs {
				if strings.Contains(e.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("no diagnostic contains %q; got: %v", tc.want, errs)
		})
	}
}
