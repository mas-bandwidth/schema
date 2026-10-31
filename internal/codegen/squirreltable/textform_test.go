package squirreltable_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// textFormDocumentSchema is the Document contract of test/conformance/textform
// (contract.json) as Go's oracle declares it (internal/codegen/gotable
// textform_test.go textFormDocumentBody), field for field, `wide wstring(4)`
// included. Four keys meet the compiler through the json attribute, as Go's
// do.
const textFormDocumentSchema = `
package probe
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
fixed table Document {
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
 wide wstring(4)
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

// documentPrelude is the probe's vocabulary. Every line it prints is a case
// name, a verb and its operands, bytes as lowercase hex.
const documentPrelude = `local inputs = dofile("inputs.nut", true);
local schema = dofile("probe/schema_module.nut", true);
local T = schema.require("ProbeTables");
local J = schema.require("schema_table_json");
local hex = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%02x", b[i] & 0xff);
  return s == "" ? "-" : s;
};
local refused = function(key, report) {
  print(key + " refuse " + report.name + " " + report.offset + "\n");
};
// ok: the rewrite, then the value's JSON
local ok = function(key, doc) {
  local js = J.ToJson(T, "Document", doc.value);
  print(key + " ok " + hex(T.DocumentSaveTextForm(doc)) + " " + (js == null ? "null" : hex(js)) + "\n");
};
local load = function(key, text, limits) {
  local report = {};
  local doc = T.DocumentLoadTextForm(text, limits, report);
  if (doc == null) { refused(key, report); return null; }
  ok(key, doc);
  return doc;
};
local edit = function(key, doc, edits) {
  local report = {};
  local e = T.DocumentEditTextForm(doc, edits, report);
  if (e == null) { refused(key, report); return; }
  ok(key, e);
};
local comments = function(key, doc) {
  local paths = [];
  foreach (c in doc.Comments()) paths.append(c.path == "" ? "." : c.path);
  local s = "";
  foreach (i, p in paths) s += (i > 0 ? " " : "") + p;
  print(key + " comments " + s + "\n");
};
local span = function(key, doc, path) {
  local sp = doc.Span(path);
  print(key + " span " + sp[0] + " " + sp[1] + "\n");
};
`

// probeLine is one line the probe printed.
type probeLine struct {
	verb string
	args []string
}

func probeBytes(t *testing.T, s string) []byte {
	t.Helper()
	if s == "-" {
		return []byte{}
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("probe hex %q: %v", s, err)
	}
	return b
}

func decodeJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var m map[string]any
	if err := d.Decode(&m); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return m
}

// normalJSON makes numbers comparable exactly and every instant comparable
// as UTC, as Go's oracle probe does.
func normalJSON(v any, key string) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = normalJSON(e, k)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalJSON(e, key)
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

// oracleJSON reads the value oracle's wrapper objects into the JSON form's
// values, as Go's oracle probe does.
func oracleJSON(v any) any {
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
			out[k] = oracleJSON(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = oracleJSON(e)
		}
		return out
	}
	return v
}

func setJSONPath(m map[string]any, path string, value any) {
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

// TestSquirrelDocumentVectors runs the §16.9 document vectors through the
// Squirrel text form, held to the Go oracle (internal/codegen/gotable
// textform_test.go): TestDocumentVectors (every positive reads to its typed-
// value oracle and rewrites byte for byte), TestDocumentEdits (both leaf
// edits land only their own bytes and value, a semantic no-op keeps the old
// spelling, a bound is refused and the old document stays, every structural
// edit is text_edit_conflict, disjoint edits apply by original span),
// TestDocumentRefusals (every Document refusal vector refuses by its stable
// name, offset inside the input), TestDocumentAttachment and
// TestDocumentLimits. It prints the number of vectors exercised.
func TestSquirrelDocumentVectors(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	vectors := filepath.Join(root, "test", "conformance", "textform")
	vector := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(vectors, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	u := unitFromSchema(t, textFormDocumentSchema)
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unitFromSchema(t, textFormDocumentSchema))
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	module := string(tables["ProbeTables.nut"])
	for _, fn := range []string{"_M.DocumentLoadTextForm <-", "_M.DocumentSaveTextForm <-", "_M.DocumentEditTextForm <-"} {
		if !strings.Contains(module, fn) {
			t.Errorf("ProbeTables.nut lacks %s", fn)
		}
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "probe")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}

	inputs := map[string][]byte{}
	var p strings.Builder
	p.WriteString(documentPrelude)
	lit := func(s string) string { return squirrel.StringLit([]byte(s)) }

	// TestDocumentVectors: the positives, the kinds vector's astral wide
	// pair included
	kinds := vector("kinds.input.sexp")
	positives := map[string][]byte{
		"roadmap": vector("roadmap.input.sexp"),
		"layout":  vector("layout.input.sexp"),
		"kinds":   kinds,
		"empty":   []byte("()"),
	}
	for _, stem := range []string{"roadmap", "layout", "kinds", "empty"} {
		inputs[stem] = positives[stem]
		fmt.Fprintf(&p, "load(%q, inputs[%q], null);\n", stem, stem)
	}
	if !bytes.Equal(vector("roadmap.rewrite.sexp"), positives["roadmap"]) || !bytes.Equal(vector("layout.rewrite.sexp"), positives["layout"]) || !bytes.Equal(vector("kinds.rewrite.sexp"), kinds) {
		t.Fatal("a rewrite vector is not its input: the corpus moved")
	}

	// TestDocumentEdits
	type editCase struct {
		key   string
		doc   string
		edits [][2]string
	}
	editJSON := map[string]map[string]any{}
	var edits []editCase
	for _, stem := range []string{"roadmap", "layout"} {
		ed := decodeJSON(t, vector(stem+".edit.json"))
		if ed["operation"] != "replace_scalar" {
			t.Fatalf("%s: operation %v", stem, ed["operation"])
		}
		spelling, _ := json.Marshal(ed["value"])
		editJSON[stem] = ed
		edits = append(edits, editCase{"edit-" + stem, stem, [][2]string{{ed["path"].(string), string(spelling)}}})
	}
	for _, s := range []string{"2", "2.0", "20e-1"} {
		edits = append(edits, editCase{"noop-" + s, "layout", [][2]string{{"count", s}}})
	}
	edits = append(edits, editCase{"bound", "layout", [][2]string{{"count", "11"}}})
	for i, e := range [][2]string{
		{"nodes", "()"},     // a compound is edited through its children
		{"nodes[0]", "1"},   // the same, as a scalar
		{"enabled", "true"}, // absent: an insert is structural
		{"count", "(1)"},    // a compound spelling is not a leaf edit
		{"count", "1 2"},    // nor two tokens
	} {
		edits = append(edits, editCase{fmt.Sprintf("conflict-%d", i), "roadmap", [][2]string{e}})
	}
	edits = append(edits, editCase{"conflict-twice", "roadmap", [][2]string{{"count", "1"}, {"count", "2"}}})
	edits = append(edits, editCase{"disjoint", "roadmap", [][2]string{{"count", "7"}, {"nodes[0].title", "\"a much longer title\""}}})
	for _, e := range edits {
		list := make([]string, len(e.edits))
		for i, ed := range e.edits {
			list[i] = fmt.Sprintf("{ path = %s, text = %s }", lit(ed[0]), lit(ed[1]))
		}
		fmt.Fprintf(&p, "edit(%q, docs[%q], [%s]);\n", e.key, e.doc, strings.Join(list, ", "))
	}
	// an edit moves no original document
	fmt.Fprintf(&p, "ok(\"after-roadmap\", docs.roadmap);\nok(\"after-layout\", docs.layout);\n")
	fmt.Fprintf(&p, "span(\"span-title\", docs.roadmap, \"nodes[0].title\");\nspan(\"span-count\", docs.roadmap, \"count\");\n")

	// TestDocumentAttachment
	fmt.Fprintf(&p, "comments(\"attach-roadmap\", docs.roadmap);\ncomments(\"attach-layout\", docs.layout);\n")

	// TestDocumentLimits
	inputs["limits"] = []byte("(:nodes ((:id \"a\")))")
	inputs["alloc"] = []byte("(:title \"abcd\")")
	for i, l := range []string{"{ bytes = 4 }", "{ tokens = 3 }", "{ nodes = 2 }", "{ allocation = 0, depth = 2 }"} {
		fmt.Fprintf(&p, "load(\"limit-%d\", inputs.limits, %s);\n", i, l)
	}
	fmt.Fprintf(&p, "load(\"limit-alloc\", inputs.alloc, { allocation = 3 });\nload(\"limit-default\", inputs.limits, null);\n")

	// TestDocumentRefusals: every Document vector (duplicate-map and
	// forward-reference are Graph's)
	names, err := filepath.Glob(filepath.Join(vectors, "refusals", "*.sexp"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no refusal vectors: %v", err)
	}
	reasons := map[string]string{}
	for _, name := range names {
		stem := strings.TrimSuffix(filepath.Base(name), ".sexp")
		if stem == "duplicate-map" || stem == "forward-reference" {
			continue
		}
		reasons["r-"+stem] = strings.TrimSpace(string(vector("refusals/" + stem + ".reason")))
		inputs["r-"+stem] = vector("refusals/" + stem + ".sexp")
		fmt.Fprintf(&p, "load(%q, inputs[%q], null);\n", "r-"+stem, "r-"+stem)
	}
	if len(reasons) < 20 {
		t.Fatalf("only %d Document refusal vectors", len(reasons))
	}

	script := strings.Replace(p.String(), "edit(", "local docs = { roadmap = load(\"base-roadmap\", inputs.roadmap, null), layout = load(\"base-layout\", inputs.layout, null) };\nedit(", 1)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script))
	stdout := sqrun.Run(t, dir, "main.nut", inputs)

	got := map[string]probeLine{}
	for line := range strings.SplitSeq(strings.TrimRight(string(stdout), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			t.Fatalf("probe line %q is not a case and a result", line)
		}
		got[f[0]] = probeLine{f[1], f[2:]}
	}
	count := 0
	value := func(key string) ([]byte, map[string]any) {
		t.Helper()
		l, ok := got[key]
		if !ok || l.verb != "ok" || len(l.args) != 2 {
			t.Fatalf("%s: %v %v, want a document", key, l.verb, l.args)
		}
		js := probeBytes(t, l.args[1])
		return probeBytes(t, l.args[0]), decodeJSON(t, js)
	}
	refusal := func(key, want string, input []byte) {
		t.Helper()
		l := got[key]
		if l.verb != "refuse" || len(l.args) != 2 || l.args[0] != want {
			t.Errorf("%s: %s %v, want refuse %s", key, l.verb, l.args, want)
			return
		}
		if off, err := strconv.Atoi(l.args[1]); err != nil || off < 0 || (input != nil && off > len(input)) {
			t.Errorf("%s: offset %s outside the input", key, l.args[1])
		}
		count++
	}
	same := func(what string, g, w map[string]any) {
		t.Helper()
		if gn, wn := normalJSON(g, ""), normalJSON(w, ""); !reflect.DeepEqual(gn, wn) {
			gb, _ := json.Marshal(gn)
			wb, _ := json.Marshal(wn)
			t.Errorf("%s:\n got %s\nwant %s", what, gb, wb)
		}
	}

	// the positives: the input byte for byte, and the full typed value
	_, defaults := value("empty")
	for _, stem := range []string{"roadmap", "layout", "kinds"} {
		rewrite, v := value(stem)
		if !bytes.Equal(rewrite, positives[stem]) {
			t.Errorf("%s: the rewrite is not the input byte for byte:\n%q", stem, rewrite)
		}
		want := map[string]any{}
		maps.Copy(want, defaults)
		o := decodeJSON(t, vector(stem+".value.json"))
		for k, e := range o["explicit_fields"].(map[string]any) {
			if e == nil {
				delete(want, k)
				continue
			}
			want[k] = oracleJSON(e)
		}
		same(stem+" value", v, want)
		count++
	}
	// the one value JSON cannot hold through a rational: the negative zero
	if l := got["kinds"]; len(l.args) == 2 {
		var compact bytes.Buffer
		if err := json.Compact(&compact, probeBytes(t, l.args[1])); err != nil || !strings.Contains(compact.String(), `"float":-0,`) {
			t.Errorf("kinds float: the negative zero lost its sign: %s", compact.String())
		}
	}

	// the edits
	for _, stem := range []string{"roadmap", "layout"} {
		edited, ev := value("edit-" + stem)
		if !bytes.Equal(edited, vector(stem+".edited.sexp")) {
			t.Errorf("%s: the edit changed more than its value:\n%q", stem, edited)
		}
		_, want := value(stem)
		setJSONPath(want, editJSON[stem]["path"].(string), editJSON[stem]["value"])
		same(stem+" edited value", ev, want)
		if after, _ := value("after-" + stem); !bytes.Equal(after, positives[stem]) {
			t.Errorf("%s: an edit moved the original document", stem)
		}
		count++
	}
	for _, s := range []string{"2", "2.0", "20e-1"} {
		if b, _ := value("noop-" + s); !bytes.Equal(b, positives["layout"]) {
			t.Errorf("no-op edit %s rewrote the document:\n%q", s, b)
		}
		count++
	}
	refusal("bound", "text_bound", nil)
	for i := range 5 {
		refusal(fmt.Sprintf("conflict-%d", i), "text_edit_conflict", nil)
	}
	refusal("conflict-twice", "text_edit_conflict", nil)
	src := positives["roadmap"]
	spanOf := func(key string) (int, int) {
		l := got[key]
		if l.verb != "span" || len(l.args) != 2 {
			t.Fatalf("%s: %v", key, l)
		}
		s, _ := strconv.Atoi(l.args[0])
		e, _ := strconv.Atoi(l.args[1])
		return s, e
	}
	ts, te := spanOf("span-title")
	cs, ce := spanOf("span-count")
	if string(src[ts:te]) != `"First"` || string(src[cs:ce]) != "2" {
		t.Errorf("spans name %q and %q", src[ts:te], src[cs:ce])
	}
	wantBoth := string(src[:ts]) + "\"a much longer title\"" + string(src[te:cs]) + "7" + string(src[ce:])
	if both, _ := value("disjoint"); string(both) != wantBoth {
		t.Errorf("two disjoint edits:\n%s", both)
	}
	count++

	// the attachment oracle
	for key, want := range map[string]string{
		"attach-roadmap": ". . . title nodes nodes[0] nodes[1] count . .",
		"attach-layout":  ". title .",
	} {
		if l := got[key]; l.verb != "comments" || strings.Join(l.args, " ") != want {
			t.Errorf("%s: %v, want %s", key, l.args, want)
		}
		count++
	}

	// the limits
	for i := range 4 {
		refusal(fmt.Sprintf("limit-%d", i), "text_limit", nil)
	}
	refusal("limit-alloc", "text_limit", nil)
	value("limit-default")

	// the refusals by name
	for key, want := range reasons {
		refusal(key, want, inputs[key])
	}
	t.Logf("ran %d document vectors", count)
}
