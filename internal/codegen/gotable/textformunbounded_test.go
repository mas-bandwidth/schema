package gotable

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestTextFormUnboundedTextListsAndMapKeys is the §2.13.1 text-form gate: an
// unbounded text-list element or map key is read and rewritten with no length
// bound (docs/SPEC-TABLES.md §16.9.2), the [..N] count bound still holds, and
// the bounded element path is unchanged.
func TestTextFormUnboundedTextListsAndMapKeys(t *testing.T) {
	t.Parallel()
	vectors, err := filepath.Abs("../../../test/conformance/textform")
	if err != nil {
		t.Fatal(err)
	}
	runTextFormUnit(t, unboundedTagsSchema, map[string]string{
		"textformunbounded_test.go": strings.ReplaceAll(unboundedTagsProbe, "__VECTORS__", strconv.Quote(vectors)),
	})
	runTextFormUnit(t, boundedTagsSchema, map[string]string{
		"textformunbounded_test.go": strings.ReplaceAll(unboundedBoundedProbe, "__VECTORS__", strconv.Quote(vectors)),
	})
}

// unboundedTagsSchema is the §2.13.1 text-form unit: unbounded list elements,
// optional elements, a bounded count and an unbounded map key.
const unboundedTagsSchema = `package probe
table Tags {
 rules []string
 first [..4]?string
 capped [..2]string
 byname map[string]int32
}
`

// boundedTagsSchema pins the bounded element path: a [..4]string(8) element is
// still length-bound, so the text-form bound check is unchanged.
const boundedTagsSchema = `package probe
table Tags {
 rules [..4]string(8)
}
`

// unboundedTagsProbe runs inside the generated unit; %[1]q is the vector
// directory.
const unboundedTagsProbe = `package probe

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const vectors = __VECTORS__

func vector(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(vectors, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func tagsJSON(t *testing.T, v *Tags) map[string]any {
	t.Helper()
	n := TagsToJsonMeasure(v)
	if n < 0 {
		t.Fatal("json measure")
	}
	b := make([]byte, n)
	if TagsToJson(v, b) != n {
		t.Fatal("json write")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var m map[string]any
	if err := d.Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func TestTextFormUnbounded(t *testing.T) {
	// the three RED inputs at BASE now load and rewrite byte for byte
	for _, input := range []string{
		"(:rules (\"bob\" \"al\"))",
		"(:first (\"x\"))",
		"(:byname ((\"k\" 1)))",
	} {
		doc, v, err := TagsLoadTextForm([]byte(input), TableTextFormDefaultLimits())
		if err != nil || doc == nil || v == nil {
			t.Fatalf("unbounded input %q: %v", input, err)
		}
		out, err := TagsSaveTextForm(doc)
		if err != nil || !bytes.Equal(out, []byte(input)) {
			t.Fatalf("unbounded input %q rewrite: %v\n%q", input, err, out)
		}
	}

	// the vector loads, rewrites byte for byte, and its value is the plain shape
	input := vector(t, "unbounded.input.sexp")
	doc, v, err := TagsLoadTextForm(input, TableTextFormDefaultLimits())
	if err != nil || doc == nil || v == nil {
		t.Fatalf("unbounded vector: %v", err)
	}
	out, err := TagsSaveTextForm(doc)
	if err != nil || !bytes.Equal(out, input) || !bytes.Equal(out, vector(t, "unbounded.rewrite.sexp")) {
		t.Fatalf("unbounded vector rewrite: %v\n%q", err, out)
	}
	j := tagsJSON(t, v)
	rules := j["rules"].([]any)
	if len(rules) != 2 || rules[0] != "x" || len(rules[1].(string)) != 5000 {
		t.Fatalf("rules shape: %v", rules)
	}
	first := j["first"].([]any)
	if len(first) != 2 || first[0] != "x" || first[1] != nil {
		t.Fatalf("first shape: %v", first)
	}
	k1 := strings.Repeat("a", 4999) + "X"
	k2 := strings.Repeat("a", 4999) + "Y"
	byname := j["byname"].(map[string]any)
	if len(byname) != 2 || byname[k1] != json.Number("1") || byname[k2] != json.Number("2") {
		t.Fatalf("byname shape: %v", byname)
	}

	// the [..N] count bound still holds
	_, _, err = TagsLoadTextForm([]byte("(:capped (\"a\" \"b\" \"c\"))"), TableTextFormDefaultLimits())
	var e *TableTextFormError
	if !errors.As(err, &e) || e.Name != "text_bound" || e.Path != "capped" {
		t.Fatalf("capped count: %v", err)
	}

	// an edit of rules[1] to a 6000-byte value lands only that value's bytes
	newval := strings.Repeat("e", 6000)
	spelling := "\"" + newval + "\""
	edited, ev, err := TagsEditTextForm(doc, TableTextFormEdit{Path: "rules[1]", Text: spelling})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	start, end, _ := doc.Span("rules[1]")
	replaced := string(input[:start]) + spelling + string(input[end:])
	if got, _ := TagsSaveTextForm(edited); !bytes.Equal(got, []byte(replaced)) {
		t.Fatalf("edit touched other bytes:\n%q", got)
	}
	jv := tagsJSON(t, ev)
	if jv["rules"].([]any)[1].(string) != newval {
		t.Fatalf("edit value: %v", jv["rules"])
	}
}
`

// unboundedBoundedProbe runs inside the bounded-element unit.
const unboundedBoundedProbe = `package probe

import (
	"errors"
	"testing"
)

const vectors = __VECTORS__

func TestTextFormBoundedElement(t *testing.T) {
	if _, _, err := TagsLoadTextForm([]byte("(:rules (\"x\"))"), TableTextFormDefaultLimits()); err != nil {
		t.Fatalf("8-byte element: %v", err)
	}
	_, _, err := TagsLoadTextForm([]byte("(:rules (\"123456789\"))"), TableTextFormDefaultLimits())
	var e *TableTextFormError
	if !errors.As(err, &e) || e.Name != "text_bound" {
		t.Fatalf("9-byte element: %v", err)
	}
}
`
