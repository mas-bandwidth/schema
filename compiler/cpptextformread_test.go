package compiler

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestCPPTextFormDocumentVectors drives the C++ hand-editable text form's
// DOCUMENT read (docs/SPEC-TABLES.md §16.9.1 grammar and identity, §16.9.2
// mapping field kind by field kind). The schema is the conformance corpus's
// own contract (test/conformance/textform/contract.json) declared (§16.4 for
// the aliased names), with only the timestamp row removed: the cpp target
// still refuses that kind at compiler/generate.go:101, until its own card
// cpp-carries-timestamp-on-the-binary-forms. Every expected value is the one Go pins in
// internal/codegen/gotable/textform_test.go TestDocumentVectors for the same
// schema text and the same input bytes (kinds.value.json). This port computes
// no expectation of its own; the refusals assert §16.9.4's stable names
// against the whole refusals corpus (test/conformance/textform/refusals),
// each .sexp against its .reason, as Go's TestDocumentRefusals does.
func TestCPPTextFormDocumentVectors(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler")
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated read is C++: no c++ on PATH")
	}
	files, err := New().Generate(unitFromSource(t, cppTextFormKindsSchema()), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	header := string(files["ProbeTable.h"])
	if !strings.Contains(header, "DocumentLoadTextForm(") {
		t.Fatal("the generated header carries no DocumentLoadTextForm: the document read was not emitted")
	}

	compile := func(t *testing.T, main string, bin string) {
		t.Helper()
		args := []string{"-std=c++17", "-O1", "-Wall", "-Wextra", "-Werror", "-I", dir, main, filepath.Join(dir, "ProbeTable.cpp"), "-o", bin}
		if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
			t.Fatalf("compile: %v\n%s", err, out)
		}
	}
	run := func(t *testing.T, bin string) string {
		t.Helper()
		out, err := exec.Command(bin).CombinedOutput()
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	writeDriver := func(t *testing.T, input string, driver string) (string, string) {
		t.Helper()
		name := filepath.Base(t.Name()) + ".cpp"
		main := filepath.Join(dir, name)
		d := strings.ReplaceAll(driver, "DOCUMENT", cppEscape(input))
		if err := os.WriteFile(main, []byte(d), 0600); err != nil {
			t.Fatal(err)
		}
		return main, strings.TrimSuffix(main, ".cpp")
	}

	// A STUB OR THE SIBLING serialize HEADER. A fixed-point field or a 128-bit
	// integer makes the generated header name serialize::int128_t/uint128_t
	// (internal/codegen/cpptable/cpptable.go unitHas128) and nothing else of
	// serialize. When the sibling checkout is staged we compile against it; when
	// it is not, these two aliases are the whole surface this unit names, so the
	// conformance rows still run rather than vanish behind an absent checkout.
	if _, err := os.Stat(filepath.Join("..", "serialize", "serialize.h")); err != nil {
		stub := "#pragma once\n#include <stdint.h>\nnamespace serialize { using int128_t = __int128; using uint128_t = unsigned __int128; }\n"
		if err := os.WriteFile(filepath.Join(dir, "serialize.h"), []byte(stub), 0600); err != nil {
			t.Fatal(err)
		}
	}

	jsonRun := func(t *testing.T, input string) map[string]any {
		t.Helper()
		main, bin := writeDriver(t, input, cppJSONDriver)
		compile(t, main, bin)
		dec := json.NewDecoder(strings.NewReader(run(t, bin)))
		dec.UseNumber()
		var got any
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("the document read did not write JSON: %v", err)
		}
		m, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("the document read wrote %T, not an object", got)
		}
		return m
	}
	same := func(t *testing.T, stem string, got, want map[string]any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			g, _ := json.Marshal(got)
			w, _ := json.Marshal(want)
			t.Fatalf("%s value:\n got %s\nwant %s", stem, g, w)
		}
	}

	// The three positive vectors' typed values, transcribed from
	// test/conformance/textform/*.value.json with the contract defaults Go's
	// internal/codegen/gotable/textform_test.go expect() lays under them. Every
	// value here is the one Go's TestDocumentVectors pins for the same bytes.
	kindsText, err := os.ReadFile(filepath.Join("..", "test", "conformance", "textform", "kinds.input.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	stripped := strings.ReplaceAll(string(kindsText), ` :time "2026-10-15T08:34:05-04:00"`, "")
	kinds := cppDocumentDefaults()
	kinds["schema"] = json.Number("2")
	kinds["state"] = "done"
	kinds["pairs"] = []any{json.Number("1"), json.Number("-2")}
	kinds["optional"] = []any{}
	kinds["bytes"] = "AP8="
	kinds["signed"] = json.Number("-170141183460469231731687303715884105728")
	kinds["unsigned"] = json.Number("340282366920938463463374607431768211455")
	kinds["fraction"] = json.Number("0.0000152587890625")
	kinds["float"] = json.Number("-0")
	kinds["flags"] = []any{"Fast", "Safe"}
	kinds["keyed"] = map[string]any{"unknown": json.Number("4"), "done": json.Number("7")}
	kinds["choice"] = map[string]any{"ping": nil}

	roadmap := cppDocumentDefaults()
	roadmap["schema"] = json.Number("1")
	roadmap["root"] = "schema/goal"
	roadmap["title"] = "Schema roadmap"
	roadmap["count"] = json.Number("2")
	roadmap["text"] = []any{
		map[string]any{"line": "a ; semicolon is data"},
		map[string]any{"line": "line\nwith escape"},
	}
	roadmap["nodes"] = []any{
		map[string]any{"id": "one", "title": "First", "state": "unknown"},
		map[string]any{"id": "two", "title": "Second", "state": "done"},
	}

	layout := cppDocumentDefaults()
	layout["title"] = "A B"
	layout["count"] = json.Number("2")
	layout["enabled"] = true

	for _, tc := range []struct {
		stem  string
		input string
		want  map[string]any
	}{
		{"kinds", stripped, kinds},
		{"roadmap", string(cppMustRead(t, "roadmap.input.sexp")), roadmap},
		{"layout", string(cppMustRead(t, "layout.input.sexp")), layout},
	} {
		t.Run(tc.stem, func(t *testing.T) {
			t.Parallel()
			same(t, tc.stem, jsonRun(t, tc.input), tc.want)
		})
	}

	// THE SAME DOCUMENT ON A VARIABLE ROOT (§16.9.1: the form is selected by
	// the root declaration). The read lands through a builder and locks it; the
	// typed value is the same oracle, so the same three vectors run again.
	variableSchema := strings.Replace(cppTextFormKindsSchema(), "fixed table Document", "table Document", 1)
	vfiles, err := New().Generate(unitFromSource(t, variableSchema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate variable: %v", err)
	}
	vdir := t.TempDir()
	for name, data := range vfiles {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(vdir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join("..", "serialize", "serialize.h")); err != nil {
		stub := "#pragma once\n#include <stdint.h>\nnamespace serialize { using int128_t = __int128; using uint128_t = unsigned __int128; }\n"
		if err := os.WriteFile(filepath.Join(vdir, "serialize.h"), []byte(stub), 0600); err != nil {
			t.Fatal(err)
		}
	}
	vcompile := func(t *testing.T, main string, bin string) {
		t.Helper()
		args := []string{"-std=c++17", "-O1", "-Wall", "-Wextra", "-Werror", "-I", vdir, main, filepath.Join(vdir, "ProbeTable.cpp"), "-o", bin}
		if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
			t.Fatalf("compile variable: %v\n%s", err, out)
		}
	}
	vjsonRun := func(t *testing.T, input string) map[string]any {
		t.Helper()
		main := filepath.Join(vdir, filepath.Base(t.Name())+".cpp")
		d := strings.ReplaceAll(cppVariableJSONDriver, "DOCUMENT", cppEscape(input))
		if err := os.WriteFile(main, []byte(d), 0600); err != nil {
			t.Fatal(err)
		}
		bin := strings.TrimSuffix(main, ".cpp")
		vcompile(t, main, bin)
		dec := json.NewDecoder(strings.NewReader(run(t, bin)))
		dec.UseNumber()
		var got any
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("the variable document read did not write JSON: %v", err)
		}
		m, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("the variable document read wrote %T, not an object", got)
		}
		return m
	}
	for _, tc := range []struct {
		stem  string
		input string
		want  map[string]any
	}{
		{"variable/kinds", stripped, kinds},
		{"variable/roadmap", string(cppMustRead(t, "roadmap.input.sexp")), roadmap},
		{"variable/layout", string(cppMustRead(t, "layout.input.sexp")), layout},
	} {
		t.Run(tc.stem, func(t *testing.T) {
			t.Parallel()
			same(t, tc.stem, vjsonRun(t, tc.input), tc.want)
		})
	}

	reason := func(stem string) string {
		b, err := os.ReadFile(filepath.Join("..", "test", "conformance", "textform", "refusals", stem+".reason"))
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(b))
	}
	names, err := filepath.Glob(filepath.Join("..", "test", "conformance", "textform", "refusals", "*.sexp"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no refusal vectors: %v", err)
	}
	ran := 0
	for _, name := range names {
		stem := strings.TrimSuffix(filepath.Base(name), ".sexp")
		if stem == "duplicate-map" || stem == "forward-reference" {
			continue // Graph's, run by the variable unit
		}
		input, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if stem == "timestamp" {
			t.Run(stem, func(t *testing.T) {
				t.Parallel()
				t.Skip("the cpp target refuses the timestamp kind at compiler/generate.go:101 until cpp-carries-timestamp-on-the-binary-forms lands")
			})
			continue
		}
		t.Run(stem, func(t *testing.T) {
			t.Parallel()
			main, bin := writeDriver(t, string(input), cppRefusalDriver)
			compile(t, main, bin)
			if got, want := run(t, bin), reason(stem); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
		ran++
	}
	if ran < 21 {
		t.Fatalf("only %d Document refusal vectors ran", ran)
	}
}

func cppMustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "test", "conformance", "textform", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// cppDocumentDefaults is the empty Document's JSON, the contract defaults
// internal/codegen/gotable/textform_test.go expect() starts from, in the
// generated C++ walk's own spelling (state None, count 0, fraction 0.0).
func cppDocumentDefaults() map[string]any {
	return map[string]any{
		"schema":   json.Number("0"),
		"root":     "",
		"title":    "",
		"state":    "None",
		"count":    json.Number("0"),
		"enabled":  false,
		"text":     []any{},
		"nodes":    []any{},
		"pairs":    []any{json.Number("0"), json.Number("0")},
		"bytes":    "",
		"signed":   json.Number("0"),
		"unsigned": json.Number("0"),
		"fraction": json.Number("0.0"),
		"float":    json.Number("0"),
		"flags":    []any{},
		"keyed":    map[string]any{"unknown": json.Number("0"), "done": json.Number("0")},
		"choice":   map[string]any{},
	}
}

func cppEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

func cppTextFormKindsSchema() string {
	// test/conformance/textform/contract.json declared (§16.4 for the aliased
	// names). The timestamp field is omitted: the cpp target refuses the kind
	// at compiler/generate.go:101 until cpp-carries-timestamp lands. The
	// 128-bit and fixed-point rows are carried; see the serialize stub above.
	return `package probe

enum State { unknown, done }
flags Features { Fast, Safe }
union Choice
{
    amount int32
    ping
}
type TextItem
{
    line string(80)
}
type Node
{
    id    string(16)
    title string(32)
    state State
}
fixed table Document
{
    schema   uint8 | min = 0, max = 3
    root     string(32)
    title    string(32)
    state    State
    count    int32 | min = 0, max = 10
    enabled  bool
    text     [..4]TextItem
    nodes    [..3]Node
    pairs    [2]int32
    optional ?[..4]int32
    blob     bytes(4) | json = "bytes"
    low      int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727, json = "signed"
    high     uint128 | json = "unsigned"
    fraction fixed(16,16) | min = -32768, max = 32767
    real     float32 | json = "float"
    flags    Features
    keyed    [State]int32
    choice   Choice
}
`
}

// cppJSONDriver prints the value the document read landed as the generated
// walk's canonical JSON. The Go oracle's docJSON (internal/codegen/gotable/
// textform_test.go) compares the same walk's JSON, so a field-by-field value
// comparison is the oracle's own comparison and not a digest of this port.
const cppJSONDriver = `#include <stdio.h>
#include <string.h>
#include <vector>
#include "ProbeTable.h"

using namespace probe;

int main( void )
{
    static const char text[] = "DOCUMENT";
    Document value;
    const char * refusal = NULL;
    if ( !DocumentLoadTextForm( text, (int64_t) strlen( text ), value, &refusal ) )
    {
        printf( "refused %s\n", refusal == NULL ? "?" : refusal );
        return 1;
    }
    std::vector<char> buffer( (size_t) DocumentToJsonMeasure( value ) + 1 );
    const int64_t n = DocumentToJson( value, buffer.data(), (int64_t) buffer.size() );
    fwrite( buffer.data(), 1, (size_t) n, stdout );
    return 0;
}
`

// cppVariableJSONDriver is cppJSONDriver over a VARIABLE root: the read lands
// into a builder, Lock packs it, and the same JSON walk prints the value.
const cppVariableJSONDriver = `#include <stdio.h>
#include <string.h>
#include <vector>
#include "ProbeTable.h"

using namespace probe;

int main( void )
{
    static const char text[] = "DOCUMENT";
    DocumentBuilder builder;
    const char * refusal = NULL;
    if ( !DocumentLoadTextForm( text, (int64_t) strlen( text ), builder, &refusal ) )
    {
        printf( "refused %s\n", refusal == NULL ? "?" : refusal );
        return 1;
    }
    if ( !builder.Lock() ) { printf( "lock failed\n" ); return 1; }
    const Document * value = builder.AsConst();
    std::vector<char> buffer( (size_t) DocumentToJsonMeasure( value ) + 1 );
    const int64_t n = DocumentToJson( value, buffer.data(), (int64_t) buffer.size() );
    fwrite( buffer.data(), 1, (size_t) n, stdout );
    return 0;
}
`

// cppRefusalDriver prints the stable refusal name (§16.9.4) for an ill-formed
// document, or the value when it is (wrongly) accepted.
const cppRefusalDriver = `#include <stdio.h>
#include <string.h>
#include "ProbeTable.h"

using namespace probe;

int main( void )
{
    static const char text[] = "DOCUMENT";
    Document value;
    const char * refusal = NULL;
    if ( !DocumentLoadTextForm( text, (int64_t) strlen( text ), value, &refusal ) )
    {
        printf( "%s\n", refusal == NULL ? "?" : refusal );
        return 0;
    }
    printf( "accepted\n" );
    return 0;
}
`
