package gotable

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestTextFormUnboundedTextListsAndMapKeys holds docs/SPEC-TABLES.md
// §2.13.1 and §16.9.2: the hand-editable text form reads and writes
// unbounded text-list elements ([]string, []?string, [..N]string,
// [..N]?string) and map[string]V keys with no schema length bound, while the
// [..N] count bound still holds. It runs through runTextFormUnit on a
// generated unit compiled in a t.TempDir(), exactly as TestCommentedTextForm
// does, and reads the unbounded conformance vector.
func TestTextFormUnboundedTextListsAndMapKeys(t *testing.T) {
	t.Parallel()
	vectors, err := filepath.Abs("../../../test/conformance/textform")
	if err != nil {
		t.Fatal(err)
	}
	probe := strings.Replace(unboundedTextFormProbe, "__VECTORS__", vectors, 1)
	runTextFormUnit(t, unboundedTextFormSchema, map[string]string{"unbounded_test.go": probe})
}

const unboundedTextFormSchema = `package probe

table Tags
{
    rules []string
    first [..4]?string
    capped [..2]string
    byname map[string]int32
}

table Bounded
{
    rules [..4]string(8)
}
`

const unboundedTextFormProbe = `package probe

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const vectors = "__VECTORS__"

func vector(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(vectors, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func loadTags(t testing.TB, text []byte) (*TableTextFormDocument, *Tags) {
	t.Helper()
	doc, v, err := TagsLoadTextForm(text, TableTextFormDefaultLimits())
	if err != nil || doc == nil || v == nil {
		t.Fatalf("TagsLoadTextForm %q: %v", text, err)
	}
	return doc, v
}

func saveTags(t testing.TB, doc *TableTextFormDocument) []byte {
	t.Helper()
	out, err := TagsSaveTextForm(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func tagsJSON(t testing.TB, v *Tags) map[string]any {
	t.Helper()
	n := TagsToJsonMeasure(v)
	if n < 0 {
		t.Fatal("TagsToJsonMeasure refused")
	}
	buf := make([]byte, n)
	if TagsToJson(v, buf) != n {
		t.Fatal("TagsToJson")
	}
	var m map[string]any
	if err := json.Unmarshal(buf, &m); err != nil {
		t.Fatalf("tags json: %v", err)
	}
	return m
}

func TestUnboundedRedInputs(t *testing.T) {
	for _, input := range []string{
		"(:rules (\"bob\" \"al\"))",
		"(:first (\"x\"))",
		"(:byname ((\"k\" 1)))",
	} {
		doc, _ := loadTags(t, []byte(input))
		if out := saveTags(t, doc); !bytes.Equal(out, []byte(input)) {
			t.Fatalf("rewrite of %q is not the input byte for byte:\n%q", input, out)
		}
	}
}

func TestUnboundedVector(t *testing.T) {
	input := vector(t, "unbounded.input.sexp")
	doc, v := loadTags(t, input)
	if out := saveTags(t, doc); !bytes.Equal(out, vector(t, "unbounded.rewrite.sexp")) {
		t.Fatalf("the vector rewrite is not byte for byte:\n%q", out)
	}
	m := tagsJSON(t, v)
	rules, ok := m["rules"].([]any)
	if !ok || len(rules) != 2 {
		t.Fatalf("rules: %v", m["rules"])
	}
	if s := rules[0].(string); len(s) != 5000 {
		t.Fatalf("rules[0] length %d, want 5000", len(s))
	}
	byname, ok := m["byname"].(map[string]any)
	if !ok || len(byname) != 2 {
		t.Fatalf("byname: %v", m["byname"])
	}
	for k := range byname {
		if len(k) != 5000 {
			t.Fatalf("byname key length %d, want 5000", len(k))
		}
	}
}

func TestUnboundedCappedBound(t *testing.T) {
	_, _, err := TagsLoadTextForm([]byte("(:capped (\"a\" \"b\" \"c\"))"), TableTextFormDefaultLimits())
	var e *TableTextFormError
	if !errors.As(err, &e) || e.Name != TableTextFormBound || e.Path != "capped" {
		t.Fatalf("capped refusal %v, want %s at capped", err, TableTextFormBound)
	}
}

func TestUnboundedEdit(t *testing.T) {
	doc, _ := loadTags(t, []byte("(:rules (\"bob\" \"al\"))"))
	big := strings.Repeat("z", 6000)
	edoc, _, err := TagsEditTextForm(doc, TableTextFormEdit{Path: "rules[1]", Text: "\"" + big + "\""})
	if err != nil {
		t.Fatalf("edit rules[1]: %v", err)
	}
	want := bytes.Replace([]byte("(:rules (\"bob\" \"al\"))"), []byte("\"al\""), []byte("\""+big+"\""), 1)
	if got := saveTags(t, edoc); !bytes.Equal(got, want) {
		t.Fatalf("edited bytes are not exactly the new value's:\n%q", got)
	}
}

func TestBoundedPathUnchanged(t *testing.T) {
	_, _, err := BoundedLoadTextForm([]byte("(:rules (\"123456789\"))"), TableTextFormDefaultLimits())
	var e *TableTextFormError
	if !errors.As(err, &e) || e.Name != TableTextFormBound {
		t.Fatalf("bounded 9-byte element refusal %v, want %s", err, TableTextFormBound)
	}
}
`
