package gotable

import (
	"fmt"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestCommentedTextForm is Aspect 14's Go gate: the textform surface emits
// <Name>LoadTextForm, <Name>SaveTextForm and <Name>EditTextForm for every
// table (docs/SPEC-TABLES.md §16.9, §8.8), importing the standard library
// only, and the compiled units are held to test/conformance/textform/: every
// positive vector reads to its typed-value oracle and rewrites byte for byte,
// both leaf edits land only their own bytes, every refusal vector refuses by
// its stable name, the attachment oracle holds, and a fuzzed edit of one value
// changes only that value's line and that value. The Document vectors run on a
// FIXED and a VARIABLE root; the Graph vectors on the variable one.
func TestCommentedTextForm(t *testing.T) {
	t.Parallel()
	vectors, err := filepath.Abs("../../../test/conformance/textform")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("emitted", func(t *testing.T) {
		t.Parallel()
		s, err := ParseSurfaces("textform")
		if err != nil || !s.TextForm || !s.JSON || !s.Wire {
			t.Fatalf("textform must imply json and wire: %+v %v", s, err)
		}
		if DefaultSurfaces().TextForm {
			t.Fatal("the textform surface is selected by name only; the default set must not carry it")
		}
		for _, schema := range []string{textFormFixedSchema, textFormVariableSchema} {
			u := unitFrom(t, schema)
			plain, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			for name := range plain {
				if strings.HasSuffix(name, "TableTextForm.go") {
					t.Fatalf("a default generate emitted %s", name)
				}
			}
			files, err := GenerateLineageSurface(u, nil, textFormSurfaces())
			if err != nil {
				t.Fatal(err)
			}
			src, ok := files["ProbeTableTextForm.go"]
			if !ok {
				t.Fatalf("the textform surface emitted no ProbeTableTextForm.go")
			}
			for _, fn := range []string{"func DocumentLoadTextForm(", "func DocumentSaveTextForm(", "func DocumentEditTextForm("} {
				if !strings.Contains(string(src), fn) {
					t.Errorf("ProbeTableTextForm.go lacks %s", fn)
				}
			}
			file, err := parser.ParseFile(token.NewFileSet(), "ProbeTableTextForm.go", src, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range file.Imports {
				path, _ := strconv.Unquote(spec.Path.Value)
				if strings.Contains(strings.Split(path, "/")[0], ".") {
					t.Errorf("the text form imports %s: the generated code imports the standard library only", path)
				}
			}
		}
	})
	t.Run("fixed", func(t *testing.T) {
		t.Parallel()
		runTextFormUnit(t, textFormFixedSchema, map[string]string{"textform_test.go": fmt.Sprintf(textFormProbe, vectors)})
	})
	t.Run("variable", func(t *testing.T) {
		t.Parallel()
		runTextFormUnit(t, textFormVariableSchema, map[string]string{
			"textform_test.go": fmt.Sprintf(textFormProbe, vectors),
			"graph_test.go":    textFormGraphProbe,
		})
	})
}

func textFormSurfaces() Surfaces {
	s := DefaultSurfaces()
	s.TextForm = true
	return s
}

// runTextFormUnit is runGeneratedUnit over the textform surface: the unit is
// generated with the surface named, compiled and run in a t.TempDir().
func runTextFormUnit(t *testing.T, schema string, tests map[string]string) {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineageSurface(u, nil, textFormSurfaces())
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	for name, src := range tests {
		files[name] = []byte(src)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated text-form unit: %v\n%s", err, out)
	}
}

// The Document contract (test/conformance/textform/contract.json), declared.
// The contract is notation, not a compiler input, so four of its keys meet
// the compiler through the json attribute (§16.4): bytes is a keyword and
// signed, unsigned and float are C++ reserved words (SPEC §4.6). int128 and
// fixed(16,16) require a declared range, which is the kind's whole domain.
const textFormDocumentBody = `
enum State { unknown, done }
flags Features { Fast, Safe }
type Node {
 id string(16)
 title string(32)
 state State
}
type TextItem {
 line string(80)
}
union Choice {
 amount int32
 ping
}
DOCUMENT Document {
 schema uint8 | min = 0, max = 3
 root string(32)
 title string(32)
 state State
 count int32 | min = 0, max = 10
 enabled bool
 text [..4]TextItem
 nodes [..3]Node
 pairs [2]int32
 optional ?[..4]int32
 blob bytes(4) | json = "bytes"
 low int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727, json = "signed"
 high uint128 | json = "unsigned"
 fraction fixed(16,16) | min = -32768, max = 32767
 real float32 | json = "float"
 time timestamp
 flags Features
 keyed [State]int32
 choice Choice
}
`

var textFormFixedSchema = "package probe\n" + strings.Replace(textFormDocumentBody, "DOCUMENT", "fixed table", 1)

// The Graph contract (variable-contract.json) beside a variable Document. Its
// node type is GNode here, since the Document's by-value Node is a type.
var textFormVariableSchema = "package probe\n" + strings.Replace(textFormDocumentBody, "DOCUMENT", "table", 1) + `
table GNode {
 id int32
}
table Graph {
 items []*GNode
 lookup map[string(8)]?int32
 blob *bytes
}
table Strict {
 on bool
 need int32 | required
 if on
 {
  extra int32
 }
}
`

// textFormProbe runs inside the generated unit; %[1]q is the vector directory.
const textFormProbe = `package probe

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

const vectors = %[1]q

func vector(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(vectors, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decode(t testing.TB, b []byte) map[string]any {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var m map[string]any
	if err := d.Decode(&m); err != nil {
		t.Fatalf("decode %%s: %%v", b, err)
	}
	return m
}

func docJSON(t testing.TB, v *Document) map[string]any {
	t.Helper()
	n := DocumentToJSONMeasure(v)
	if n < 0 {
		t.Fatal("the value read from a document does not write as JSON")
	}
	b := make([]byte, n)
	if DocumentToJSON(v, b) != n {
		t.Fatal("json write")
	}
	return decode(t, b)
}

// normal makes numbers comparable exactly (a rational's string) and every
// instant comparable as UTC.
func normal(v any, key string) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = normal(e, k)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normal(e, key)
		}
		return out
	case json.Number:
		r, ok := new(big.Rat).SetString(string(x))
		if !ok {
			return "bad number " + string(x)
		}
		return "num:" + r.RatString()
	case string:
		if key == "time" {
			if at, err := time.Parse(time.RFC3339Nano, x); err == nil {
				return "time:" + at.UTC().Format(time.RFC3339Nano)
			}
		}
	}
	return v
}

// oracle reads the value oracle's wrapper objects into the JSON form's values.
func oracle(t testing.TB, v any) any {
	switch x := v.(type) {
	case map[string]any:
		if s, ok := x["base64"]; ok && len(x) == 1 {
			return s
		}
		if s, ok := x["decimal"]; ok && len(x) == 1 {
			return json.Number(s.(string))
		}
		if raw, ok := x["raw"]; ok {
			bits, _ := strconv.Atoi(string(x["fraction_bits"].(json.Number)))
			r, _ := new(big.Rat).SetString(string(raw.(json.Number)))
			r.Quo(r, new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), uint(bits))))
			return json.Number(r.FloatString(bits))
		}
		if h, ok := x["ieee754_binary32_hex"]; ok {
			u, _ := strconv.ParseUint(h.(string), 16, 32)
			return json.Number(strconv.FormatFloat(float64(math.Float32frombits(uint32(u))), 'g', -1, 32))
		}
		if s, ok := x["rfc3339_utc"]; ok {
			return s
		}
		out := map[string]any{}
		for k, e := range x {
			out[k] = oracle(t, e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = oracle(t, e)
		}
		return out
	}
	return v
}

func load(t testing.TB, text []byte) (*TableTextFormDocument, *Document) {
	t.Helper()
	doc, v, err := DocumentLoadTextForm(text, TableTextFormDefaultLimits())
	if err != nil || doc == nil || v == nil {
		t.Fatalf("read %%q: %%v", text, err)
	}
	return doc, v
}

func save(t testing.TB, doc *TableTextFormDocument) []byte {
	t.Helper()
	out, err := DocumentSaveTextForm(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// expect is the oracle's full typed value: the contract defaults (the empty
// document) with the explicit fields over them; an explicit null is absent.
func expect(t testing.TB, stem string) map[string]any {
	t.Helper()
	_, defaults := load(t, []byte("()"))
	want := docJSON(t, defaults)
	o := decode(t, vector(t, stem+".value.json"))
	for k, v := range o["explicit_fields"].(map[string]any) {
		if v == nil {
			delete(want, k)
			continue
		}
		want[k] = oracle(t, v)
	}
	return want
}

func same(t testing.TB, what string, got, want map[string]any) {
	t.Helper()
	if g, w := normal(got, ""), normal(want, ""); !reflect.DeepEqual(g, w) {
		gb, _ := json.Marshal(g)
		wb, _ := json.Marshal(w)
		t.Fatalf("%%s:\n got %%s\nwant %%s", what, gb, wb)
	}
}

func TestDocumentVectors(t *testing.T) {
	for _, stem := range []string{"roadmap", "layout", "kinds"} {
		input := vector(t, stem+".input.sexp")
		doc, v := load(t, input)
		if out := save(t, doc); !bytes.Equal(out, vector(t, stem+".rewrite.sexp")) || !bytes.Equal(out, input) {
			t.Fatalf("%%s: the rewrite is not the input byte for byte:\n%%q", stem, out)
		}
		same(t, stem+" value", docJSON(t, v), expect(t, stem))
	}
	// the one value JSON cannot hold: the negative zero's sign bit
	_, v := load(t, vector(t, "kinds.input.sexp"))
	if math.Float32bits(v.Real) != 0x80000000 {
		t.Fatalf("kinds float: bits %%08x, want 80000000", math.Float32bits(v.Real))
	}
}

func setPath(m map[string]any, path string, value any) {
	var cur any = m
	parts := strings.Split(strings.ReplaceAll(path, "[", ".["), ".")
	for i, p := range parts {
		last := i == len(parts)-1
		if strings.HasPrefix(p, "[") {
			n, _ := strconv.Atoi(strings.Trim(p, "[]"))
			if last {
				cur.([]any)[n] = value
				return
			}
			cur = cur.([]any)[n]
			continue
		}
		if last {
			cur.(map[string]any)[p] = value
			return
		}
		cur = cur.(map[string]any)[p]
	}
}

func getPath(m map[string]any, path string) any {
	var cur any = m
	for _, p := range strings.Split(strings.ReplaceAll(path, "[", ".["), ".") {
		if strings.HasPrefix(p, "[") {
			n, _ := strconv.Atoi(strings.Trim(p, "[]"))
			cur = cur.([]any)[n]
			continue
		}
		cur = cur.(map[string]any)[p]
	}
	return cur
}

func refusal(t testing.TB, err error, name string) *TableTextFormError {
	t.Helper()
	var e *TableTextFormError
	if !errors.As(err, &e) || e.Name != name || err.Error() != name {
		t.Fatalf("refusal %%v, want %%s", err, name)
	}
	return e
}

func TestDocumentEdits(t *testing.T) {
	for _, stem := range []string{"roadmap", "layout"} {
		input := vector(t, stem+".input.sexp")
		doc, v := load(t, input)
		edit := decode(t, vector(t, stem+".edit.json"))
		if edit["operation"] != "replace_scalar" {
			t.Fatalf("%%s: operation %%v", stem, edit["operation"])
		}
		path := edit["path"].(string)
		spelling, _ := json.Marshal(edit["value"])
		edited, ev, err := DocumentEditTextForm(doc, TableTextFormEdit{Path: path, Text: string(spelling)})
		if err != nil {
			t.Fatalf("%%s edit: %%v", stem, err)
		}
		if out := save(t, edited); !bytes.Equal(out, vector(t, stem+".edited.sexp")) {
			t.Fatalf("%%s: the edit changed more than its value:\n%%q", stem, out)
		}
		want := docJSON(t, v)
		setPath(want, path, edit["value"])
		same(t, stem+" edited value", docJSON(t, ev), want)
		if !bytes.Equal(save(t, doc), input) {
			t.Fatalf("%%s: an edit moved the original document", stem)
		}
	}
	input := vector(t, "layout.input.sexp")
	doc, _ := load(t, input)
	// a semantic no-op keeps the old spelling: count stays 2e0
	for _, spelling := range []string{"2", "2.0", "20e-1"} {
		noop, _, err := DocumentEditTextForm(doc, TableTextFormEdit{Path: "count", Text: spelling})
		if err != nil || !bytes.Equal(save(t, noop), input) {
			t.Fatalf("no-op edit %%s rewrote the document: %%v", spelling, err)
		}
	}
	// an edit is validated before anything is returned, and the old pair stays
	if _, _, err := DocumentEditTextForm(doc, TableTextFormEdit{Path: "count", Text: "11"}); err == nil {
		t.Fatal("an out-of-bound edit was accepted")
	} else {
		refusal(t, err, "text_bound")
	}
	if !bytes.Equal(save(t, doc), input) {
		t.Fatal("a refused edit moved the document")
	}
	rdoc, _ := load(t, vector(t, "roadmap.input.sexp"))
	for _, e := range []TableTextFormEdit{
		{Path: "nodes", Text: "()"},              // a compound is edited through its children
		{Path: "nodes[0]", Text: "1"},            // the same, as a scalar
		{Path: "enabled", Text: "true"},          // absent: an insert is structural
		{Path: "count", Text: "(1)"},             // a compound spelling is not a leaf edit
		{Path: "count", Text: "1 2"},             // nor two tokens
	} {
		if _, _, err := DocumentEditTextForm(rdoc, e); err == nil {
			t.Fatalf("edit %%+v was accepted", e)
		} else {
			refusal(t, err, "text_edit_conflict")
		}
	}
	if _, _, err := DocumentEditTextForm(rdoc, TableTextFormEdit{Path: "count", Text: "1"}, TableTextFormEdit{Path: "count", Text: "2"}); err == nil {
		t.Fatal("two edits of one span were accepted")
	} else {
		refusal(t, err, "text_edit_conflict")
	}
	// disjoint edits are applied by ORIGINAL span: no offset drifts
	src := rdoc.Bytes()
	ts, te, _ := rdoc.Span("nodes[0].title")
	cs, ce, _ := rdoc.Span("count")
	want := string(src[:ts]) + "\"a much longer title\"" + string(src[te:cs]) + "7" + string(src[ce:])
	both, _, err := DocumentEditTextForm(rdoc, TableTextFormEdit{Path: "count", Text: "7"}, TableTextFormEdit{Path: "nodes[0].title", Text: "\"a much longer title\""})
	if err != nil || string(save(t, both)) != want {
		t.Fatalf("two disjoint edits: %%v\n%%s", err, save(t, both))
	}
}

func TestDocumentRefusals(t *testing.T) {
	names, err := filepath.Glob(filepath.Join(vectors, "refusals", "*.sexp"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no refusal vectors: %%v", err)
	}
	ran := 0
	for _, name := range names {
		stem := strings.TrimSuffix(filepath.Base(name), ".sexp")
		if stem == "duplicate-map" || stem == "forward-reference" {
			continue // Graph's, run by the variable unit
		}
		input := vector(t, "refusals/"+stem+".sexp")
		reason := strings.TrimSpace(string(vector(t, "refusals/"+stem+".reason")))
		doc, v, err := DocumentLoadTextForm(input, TableTextFormDefaultLimits())
		if doc != nil || v != nil {
			t.Fatalf("%%s: a refusal returned a document or a value", stem)
		}
		e := refusal(t, err, reason)
		if e.Offset < 0 || e.Offset > len(input) {
			t.Fatalf("%%s: offset %%d outside the input", stem, e.Offset)
		}
		ran++
	}
	if ran < 20 {
		t.Fatalf("only %%d Document refusal vectors ran", ran)
	}
}

func TestDocumentAttachment(t *testing.T) {
	cases := map[string][]string{
		"roadmap": {"", "", "", "title", "nodes", "nodes[0]", "nodes[1]", "count", "", ""},
		"layout":  {"", "title", ""},
	}
	for stem, want := range cases {
		doc, _ := load(t, vector(t, stem+".input.sexp"))
		comments := doc.Comments()
		if len(comments) != len(want) {
			t.Fatalf("%%s: %%d comments, want %%d: %%+v", stem, len(comments), len(want), comments)
		}
		for i, c := range comments {
			if c.Path != want[i] {
				t.Errorf("%%s comment %%q attaches to %%q, want %%q", stem, c.Text, c.Path, want[i])
			}
		}
	}
}

func TestDocumentLimits(t *testing.T) {
	text := []byte("(:nodes ((:id \"a\")))")
	for _, limits := range []TableTextFormLimits{{Bytes: 4}, {Tokens: 3}, {Nodes: 2}, {Allocation: 0, Depth: 2}} {
		if _, _, err := DocumentLoadTextForm(text, limits); err == nil {
			t.Fatalf("limits %%+v accepted the document", limits)
		} else {
			refusal(t, err, "text_limit")
		}
	}
	if _, _, err := DocumentLoadTextForm([]byte("(:title \"abcd\")"), TableTextFormLimits{Allocation: 3}); err == nil {
		t.Fatal("the decoded-allocation limit was not held")
	}
	load(t, text)
}

// TestDocumentRules holds §16.9's rules past the corpus: each document is
// refused by exactly the name given, or read when the name is empty.
func TestDocumentRules(t *testing.T) {
	for text, want := range map[string]string{
		"(:pairs (1 2 3))":                       "text_bound",
		"(:time 5)":                              "text_kind_mismatch",
		"(:float 1e40)":                          "text_bound",
		"(:float 1e-60)":                         "",
		"(:signed 1.5)":                          "text_kind_mismatch",
		"(:signed 170141183460469231731687303715884105728)": "text_bound",
		"(:unsigned -1)":                         "text_bound",
		"(:count 1e400)":                         "text_bound",
		"(:count 0e999999999)":                   "",
		"(:count 1e-400)":                        "text_kind_mismatch",
		"(:count 1-2)":                           "text_syntax",
		"(:count -0)":                            "",
		"(:fraction 15e-1)":                      "",
		"(:fraction 32768)":                      "text_bound",
		"(:choice (:nope 1))":                    "text_unknown_key",
		"(:choice ())":                           "",
		"(:choice (:ping 1))":                    "text_kind_mismatch",
		"(:choice (:amount 1 :amount 2))":        "text_duplicate_key",
		"(:keyed (:done 1 :done 2))":             "text_duplicate_key",
		"(:keyed (:done 1 \"done\" 2))":        "text_duplicate_key",
		"(:state \"None\" :flags ())":          "",
		"(:state :None)":                         "",
		"(:flags (1))":                           "text_kind_mismatch",
		"(:flags (:Slow))":                       "text_unknown_variant",
		"(:bytes \"AP8\")":                     "text_encoding",
		"(:bytes \"AP8=\\n\")":               "text_encoding",
		"(:bytes \"\")":                        "",
		"(:title \"a\",)":                      "text_syntax",
		"\xef\xbb\xbf()":                      "text_syntax",
		"(:title \"a\")\r":                    "text_syntax",
		"(:title \"a\") ; ok\r\n":            "",
		"(:title \"a\") ; \xff":               "text_encoding",
		"(:title \"\\x\")":                   "text_encoding",
		"(:title \"\\uDC00\")":               "text_encoding",
		"(:title \"\\uD83D\\uDE00\")":      "",
		"(:title \"\\u00e9\")":               "",
		"(:title \"a\tb\")":                   "text_syntax",
		"(:title \"a\\u12":                    "text_unterminated",
		"(:count 2 :count 3)":                    "text_duplicate_key",
		"(:nodes ((:id \"x\" :surprise 1)))":   "text_unknown_key",
		"(:nodes ((\"&node\" 1 :id \"x\")))": "text_reference",
		"(:title)":                               "text_syntax",
		"(1 2)":                                  "text_syntax",
		"(:enabled 1)":                           "text_kind_mismatch",
		"(:enabled null)":                        "text_kind_mismatch",
		"(:optional null)":                       "",
		"(:optional (1 2 3 4 5))":                "text_bound",
		"(:text ((:line \"a\" :line \"b\")))": "text_duplicate_key",
		"(:time \"2026-10-15T08:34:05.123456789+05:30\")": "",
		"(:time \"3000-01-01T00:00:00Z\")":     "text_timestamp",
		"\"x\"":                                "text_syntax",
		"":                                       "text_syntax",
		"; only a comment":                       "text_syntax",
		"(":                                      "text_unterminated",
		"(:root \"x\") )":                      "text_syntax",
	} {
		_, _, err := DocumentLoadTextForm([]byte(text), TableTextFormDefaultLimits())
		if want == "" {
			if err != nil {
				t.Errorf("%%q: %%v", text, err)
			}
			continue
		}
		var e *TableTextFormError
		if !errors.As(err, &e) || e.Name != want {
			t.Errorf("%%q: %%v, want %%s", text, err, want)
		}
	}
}

// the replacement generator, by the last segment of a path
func spell(rng *rand.Rand, path string) (string, bool) {
	last := path
	if i := strings.LastIndexAny(path, ".]"); i >= 0 && path[i] == '.' {
		last = path[i+1:]
	} else if strings.HasSuffix(path, "]") {
		last = "[]"
	}
	word := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = "abcdefghijklmnopqrstuvwxyz ;()ABC"[rng.IntN(33)]
		}
		s, _ := json.Marshal(string(b))
		return string(s)
	}
	switch {
	case last == "schema":
		return strconv.Itoa(rng.IntN(4)), true
	case last == "count":
		return strconv.Itoa(rng.IntN(11)), true
	case last == "title" || last == "root":
		return word(rng.IntN(17)), true
	case last == "id":
		return word(rng.IntN(17)), true
	case last == "line":
		return word(rng.IntN(81)), true
	case last == "state":
		return []string{":unknown", ":done", "\"done\"", ":None"}[rng.IntN(4)], true
	case last == "enabled":
		return []string{"true", "false"}[rng.IntN(2)], true
	case strings.HasPrefix(path, "pairs[") || last == "unknown" || last == "done":
		return strconv.Itoa(int(int32(rng.Uint32()))), true
	case last == "signed":
		v := new(big.Int).Lsh(big.NewInt(int64(rng.Uint64()>>1)), 64)
		v.Add(v, new(big.Int).SetUint64(rng.Uint64()))
		if rng.IntN(2) == 0 {
			v.Neg(v)
		}
		return v.String(), true
	case last == "unsigned":
		v := new(big.Int).Lsh(new(big.Int).SetUint64(rng.Uint64()), 64)
		return v.Add(v, new(big.Int).SetUint64(rng.Uint64())).String(), true
	case last == "fraction":
		return new(big.Rat).SetFrac64(rng.Int64N(65535*65536+1)-32768*65536, 65536).FloatString(16), true
	case last == "float":
		return strconv.FormatFloat(float64(math.Float32frombits(rng.Uint32()&0x7f7fffff)), 'g', -1, 32), true
	case last == "time":
		return "\"" + time.Unix(rng.Int64N(7258118400), int64(rng.IntN(1e9))).UTC().Format(time.RFC3339Nano) + "\"", true
	case last == "bytes":
		b := make([]byte, rng.IntN(5))
		for i := range b {
			b[i] = byte(rng.Uint32())
		}
		return "\"" + base64.StdEncoding.EncodeToString(b) + "\"", true
	}
	return "", false
}

func lineOf(src []byte, at int) int { return bytes.Count(src[:at], []byte("\n")) }

// FuzzTextFormEdit: a fuzzed edit of one value changes only that value's span
// (so only its line) and only that value. The seeds are a fixed PCG stream, so
// a plain go test runs the same corpus every time.
func FuzzTextFormEdit(f *testing.F) {
	seeds := rand.New(rand.NewPCG(1000, 14))
	for i := 0; i < 400; i++ {
		f.Add(uint8(seeds.IntN(3)), uint16(seeds.Uint32()), seeds.Uint64())
	}
	stems := []string{"roadmap", "layout", "kinds"}
	f.Fuzz(func(t *testing.T, stem uint8, pick uint16, seed uint64) {
		input := vector(t, stems[int(stem)%%len(stems)]+".input.sexp")
		doc, v := load(t, input)
		rng := rand.New(rand.NewPCG(seed, 1000))
		var paths []string
		for _, p := range doc.Paths() {
			if _, ok := spell(rng, p); ok && doc.Scalar(p) {
				paths = append(paths, p)
			}
		}
		if len(paths) == 0 {
			t.Fatal("no editable scalar")
		}
		path := paths[int(pick)%%len(paths)]
		text, _ := spell(rng, path)
		edited, ev, err := DocumentEditTextForm(doc, TableTextFormEdit{Path: path, Text: text})
		if err != nil {
			t.Fatalf("edit %%s to %%s: %%v", path, text, err)
		}
		out := save(t, edited)
		start, end, _ := doc.Span(path)
		replaced := string(input[:start]) + text + string(input[end:])
		if !bytes.Equal(out, input) && string(out) != replaced {
			t.Fatalf("edit %%s to %%s touched other bytes:\n%%q", path, text, out)
		}
		before, after := bytes.Split(input, []byte("\n")), bytes.Split(out, []byte("\n"))
		if len(before) != len(after) {
			t.Fatalf("edit %%s added or removed lines", path)
		}
		for i := range before {
			if i != lineOf(input, start) && !bytes.Equal(before[i], after[i]) {
				t.Fatalf("edit %%s at line %%d changed line %%d", path, lineOf(input, start), i)
			}
		}
		got, want := docJSON(t, ev), docJSON(t, v)
		if strings.Contains(path, "flags") {
			return
		}
		top := strings.Split(strings.Split(path, ".")[0], "[")[0]
		want[top] = got[top]
		same(t, "edit "+path+" moved another value", got, want)
	})
}
`

// textFormGraphProbe is the variable-only Graph contract.
const textFormGraphProbe = `package probe

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func graphJSON(t *testing.T, v *Graph) map[string]any {
	t.Helper()
	n := GraphToJSONMeasure(v)
	if n < 0 {
		t.Fatal("graph json measure")
	}
	b := make([]byte, n)
	if GraphToJSON(v, b) != n {
		t.Fatal("graph json write")
	}
	return decode(t, b)
}

func TestGraphVector(t *testing.T) {
	input := vector(t, "variable.input.sexp")
	doc, v, err := GraphLoadTextForm(input, TableTextFormDefaultLimits())
	if err != nil {
		t.Fatalf("variable: %v", err)
	}
	out, err := GraphSaveTextForm(doc)
	if err != nil || !bytes.Equal(out, vector(t, "variable.rewrite.sexp")) || !bytes.Equal(out, input) {
		t.Fatalf("variable: the rewrite is not the input byte for byte: %v\n%q", err, out)
	}
	if c := doc.Comments(); len(c) != 1 || c[0].Path != "" {
		t.Fatalf("the variable heading belongs to Graph: %+v", c)
	}
	// the oracle: items name nodes by label, and a label named twice is ONE
	// node, which the JSON writer says with &node (§16.7)
	o := decode(t, vector(t, "variable.value.json"))
	nodes := o["nodes"].(map[string]any)
	labels := map[string]int{}
	var items []any
	for _, item := range o["items"].([]any) {
		if item == nil {
			items = append(items, nil)
			continue
		}
		label := string(item.(map[string]any)["node"].(json.Number))
		if n, ok := labels[label]; ok {
			items = append(items, map[string]any{"&node": json.Number(itoa(n))})
			continue
		}
		labels[label] = len(labels) + 1
		def := map[string]any{"&node": json.Number(itoa(labels[label]))}
		for k, e := range nodes[label].(map[string]any) {
			def[k] = e
		}
		items = append(items, def)
	}
	want := map[string]any{"items": items, "lookup": o["lookup"], "blob": oracle(t, o["blob"])}
	same(t, "variable value", graphJSON(t, v), want)
	got := graphJSON(t, v)["items"].([]any)
	if !reflect.DeepEqual(got[0].(map[string]any)["&node"], got[1].(map[string]any)["&node"]) {
		t.Fatal("the two items do not name one node")
	}
}

// TestStrictRequiredAndGuards: a missing required field and a present field
// under a false guard are refused after the whole record, so order never
// decides (§16.9.1, §16.9.4).
func TestStrictRequiredAndGuards(t *testing.T) {
	for text, want := range map[string]string{
		"(:need 1)":                   "",
		"(:need 0)":                   "",
		"()":                          "text_missing_required",
		"(:on true)":                  "text_missing_required",
		"(:need 1 :extra 2)":          "text_inactive_field",
		"(:need 1 :on false :extra 2)": "text_inactive_field",
		"(:need 1 :on true :extra 2)": "",
		"(:extra 2 :on true :need 1)": "",
	} {
		_, _, err := StrictLoadTextForm([]byte(text), TableTextFormDefaultLimits())
		if want == "" {
			if err != nil {
				t.Errorf("%s: %v", text, err)
			}
			continue
		}
		var e *TableTextFormError
		if !errors.As(err, &e) || e.Name != want {
			t.Errorf("%s: %v, want %s", text, err, want)
		}
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestGraphRefusals(t *testing.T) {
	for _, stem := range []string{"duplicate-map", "forward-reference"} {
		input := vector(t, "refusals/"+stem+".sexp")
		reason := strings.TrimSpace(string(vector(t, "refusals/"+stem+".reason")))
		doc, v, err := GraphLoadTextForm(input, TableTextFormDefaultLimits())
		var e *TableTextFormError
		if doc != nil || v != nil || !errors.As(err, &e) || e.Name != reason {
			t.Fatalf("%s: %v, want %s", stem, err, reason)
		}
	}
	// the sharing rules beyond the corpus (§16.7, §16.9.2)
	for text, want := range map[string]string{
		"(:items ((\"&node\" 1 :id 1) (\"&node\" 1 :id 2)))": "text_reference", // a second definition
		"(:items ((\"&node\" 1.0 :id 1)))":                    "text_reference", // not a label spelling
		"(:items ((\"&node\" 01 :id 1)))":                     "text_syntax",    // not a number at all
		"(:items ((:id 1 \"&node\" 1)))":                      "text_reference", // the label is the first pair
		"(:items ((\"&ref\" 1 :id 1)))":                       "text_reference", // only &node
		"(:lookup ((\"&node\" 1)))":                           "",               // a map key is data
		"(:items ((\"&node\" 3 :id 1) (\"&node\" 3) (\"&node\" 3)))": "",
		"(:blob \"AB==\")":                                    "text_encoding",
		"(:lookup ((\"east\")))":                              "text_kind_mismatch",
	} {
		_, _, err := GraphLoadTextForm([]byte(text), TableTextFormDefaultLimits())
		if want == "" {
			if err != nil {
				t.Fatalf("%s: %v", text, err)
			}
			continue
		}
		var e *TableTextFormError
		if !errors.As(err, &e) || e.Name != want {
			t.Fatalf("%s: %v, want %s", text, err, want)
		}
	}
}
`
