package gotable

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// TestTextFormTimestampStrictRFC3339 holds the hand-editable text form to the
// same table the JSON walk and the engine read: every spelling
// strictTimestampCases names, with the document spelled `(:at "<text>")` in
// the text form's own string escapes (docs/SPEC-TABLES.md §2.11, §16.9.4).
func TestTextFormTimestampStrictRFC3339(t *testing.T) {
	t.Parallel()
	cases := strictTimestampCases()

	var source strings.Builder
	source.WriteString("package probe\n\nimport (\n\t\"errors\"\n\t\"testing\"\n)\n\n")
	source.WriteString("var textFormTimestampCases = []struct {\n\tname  string\n\tdoc   string\n\tnanos int64\n\tok    bool\n}{\n")
	for _, tc := range cases {
		doc := "(:at " + strconv.Quote(tc.text) + ")"
		fmt.Fprintf(&source, "\t{%q, %q, %d, %t},\n", tc.name, doc, tc.nanos, tc.ok)
	}
	source.WriteString(`}

func TestTextFormTimestampStrictRFC3339Spellings(t *testing.T) {
	for _, c := range textFormTimestampCases {
		t.Run(c.name, func(t *testing.T) {
			_, v, err := RootLoadTextForm([]byte(c.doc), TableTextFormDefaultLimits())
			if c.ok {
				if err != nil || v == nil {
					t.Fatalf("%s: %v", c.doc, err)
				}
				if v.At != c.nanos {
					t.Fatalf("%s: landed %d, want %d", c.doc, v.At, c.nanos)
				}
				return
			}
			var e *TableTextFormError
			if !errors.As(err, &e) || e.Name != "text_timestamp" {
				t.Fatalf("%s: %v, want text_timestamp", c.doc, err)
			}
			if v != nil && v.At != 0 {
				t.Fatalf("%s: the refused spelling landed %d", c.doc, v.At)
			}
		})
	}
}
`)

	runTextFormUnit(t, strictTimestampSchema, map[string]string{"textformtimestamp_test.go": source.String()})
}
