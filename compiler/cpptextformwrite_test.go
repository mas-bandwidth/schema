package compiler

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestCPPTextFormDocumentEdits drives the C++ hand-editable text form's
// DOCUMENT writer and edit (docs/SPEC-TABLES.md §16.9.3). The document retains
// the source bytes, a read then a write is the input byte for byte, and an
// edit replaces only its scalar's token span. Every expected answer here is
// the one Go pins in internal/codegen/gotable/textform_test.go
// TestDocumentEdits (and TestDocumentVectors for the rewrite) for the same
// schema text and the same input bytes: the roadmap and layout .edited.sexp
// files, the same no-op spellings, the same structural refusals, and the same
// two disjoint edits applied by original span. The port computes no
// expectation of its own.
func TestCPPTextFormDocumentEdits(t *testing.T) {
	t.Parallel()
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated writer is C++: no c++ on PATH")
	}

	type harness struct {
		dir string
	}
	prepare := func(t *testing.T, schema, driverSource string) harness {
		t.Helper()
		files, err := New().Generate(unitFromSource(t, schema), "cpp", Options{})
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
		for _, fn := range []string{"DocumentSaveTextForm(", "DocumentEditTextForm(", "struct TableTextFormEdit"} {
			if !strings.Contains(header, fn) {
				t.Fatalf("the generated header carries no %s: the document writer was not emitted", fn)
			}
		}
		// A STUB OR THE SIBLING serialize HEADER, exactly as the read test does:
		// a 128-bit or fixed-point field names serialize::int128_t/uint128_t.
		if _, err := os.Stat(filepath.Join("..", "serialize", "serialize.h")); err != nil {
			stub := "#pragma once\n#include <stdint.h>\nnamespace serialize { using int128_t = __int128; using uint128_t = unsigned __int128; }\n"
			if err := os.WriteFile(filepath.Join(dir, "serialize.h"), []byte(stub), 0600); err != nil {
				t.Fatal(err)
			}
		}
		driver := filepath.Join(dir, "probe.cpp")
		if err := os.WriteFile(driver, []byte(driverSource), 0600); err != nil {
			t.Fatal(err)
		}
		args := []string{"-std=c++17", "-O1", "-Wall", "-Wextra", "-Werror", "-I", dir, driver, filepath.Join(dir, "ProbeTable.cpp"), "-o", filepath.Join(dir, "probe")}
		if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
			t.Fatalf("compile: %v\n%s", err, out)
		}
		return harness{dir: dir}
	}
	run := func(t *testing.T, h harness, args ...string) ([]byte, error) {
		t.Helper()
		return exec.Command(filepath.Join(h.dir, "probe"), args...).CombinedOutput()
	}
	write := func(t *testing.T, h harness, name string, data []byte) string {
		t.Helper()
		path := filepath.Join(h.dir, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	read := func(t *testing.T, name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("..", "test", "conformance", "textform", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	decode := func(t *testing.T, b []byte) map[string]any {
		t.Helper()
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.UseNumber()
		var got any
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("decode %q: %v", b, err)
		}
		m, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("edited value is %T, not an object", got)
		}
		return m
	}
	same := func(t *testing.T, what string, got, want map[string]any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			g, _ := json.Marshal(got)
			w, _ := json.Marshal(want)
			t.Fatalf("%s:\n got %s\nwant %s", what, g, w)
		}
	}
	setPath := func(m map[string]any, path string, value any) {
		var cur any = m
		parts := strings.Split(strings.ReplaceAll(path, "[", ".["), ".")
		for i, p := range parts {
			last := i == len(parts)-1
			if strings.HasPrefix(p, "[") {
				n := 0
				for _, c := range strings.Trim(p, "[]") {
					n = n*10 + int(c-'0')
				}
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

	// The value oracle's roadmap and layout typed values, transcribed from
	// internal/codegen/gotable/textform_test.go TestDocumentVectors through the
	// same contract defaults the read test uses.
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

	fixed := prepare(t, cppTextFormKindsSchema(), cppTextFormWriteDriver)
	for _, tc := range []struct {
		stem string
		want map[string]any
	}{
		{"roadmap", roadmap},
		{"layout", layout},
	} {
		tc := tc
		t.Run(tc.stem+"/rewrite", func(t *testing.T) {
			t.Parallel()
			input := read(t, tc.stem+".input.sexp")
			inPath := write(t, fixed, tc.stem+".input.sexp", input)
			outPath := filepath.Join(fixed.dir, tc.stem+".rewrite.sexp")
			if out, err := run(t, fixed, "save", inPath, outPath); err != nil {
				t.Fatalf("save: %v\n%s", err, out)
			}
			out, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatal(err)
			}
			// Go's TestDocumentVectors: save(doc) == .rewrite.sexp == input.
			if !reflect.DeepEqual(out, read(t, tc.stem+".rewrite.sexp")) || !reflect.DeepEqual(out, input) {
				t.Fatalf("%s: the rewrite is not the input byte for byte:\n%q", tc.stem, out)
			}
		})
		t.Run(tc.stem+"/edit", func(t *testing.T) {
			t.Parallel()
			input := read(t, tc.stem+".input.sexp")
			var edit struct {
				Operation string `json:"operation"`
				Path      string `json:"path"`
				Value     any    `json:"value"`
			}
			dec := json.NewDecoder(bytes.NewReader(read(t, tc.stem+".edit.json")))
			dec.UseNumber()
			if err := dec.Decode(&edit); err != nil {
				t.Fatal(err)
			}
			if edit.Operation != "replace_scalar" {
				t.Fatalf("%s: operation %q", tc.stem, edit.Operation)
			}
			spelling, _ := json.Marshal(edit.Value)
			inPath := write(t, fixed, tc.stem+".input.sexp", input)
			docPath := filepath.Join(fixed.dir, tc.stem+".edited.sexp")
			jsonPath := filepath.Join(fixed.dir, tc.stem+".edited.json")
			out, err := run(t, fixed, "edit", inPath, edit.Path, string(spelling), docPath, jsonPath)
			if err != nil {
				t.Fatalf("%s edit: %v\n%s", tc.stem, err, out)
			}
			gotDoc, err := os.ReadFile(docPath)
			if err != nil {
				t.Fatal(err)
			}
			// Go's TestDocumentEdits: save(edited) == .edited.sexp.
			if !reflect.DeepEqual(gotDoc, read(t, tc.stem+".edited.sexp")) {
				t.Fatalf("%s: the edit changed more than its value:\n%q", tc.stem, gotDoc)
			}
			gotJSON, err := os.ReadFile(jsonPath)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.want
			setPath(want, edit.Path, edit.Value)
			same(t, tc.stem+" edited value", decode(t, gotJSON), want)
			// the original document is untouched
			if out, err := run(t, fixed, "save", inPath, docPath); err != nil {
				t.Fatalf("%s: save after edit: %v\n%s", tc.stem, err, out)
			}
			if got, err := os.ReadFile(inPath); err != nil || !reflect.DeepEqual(got, input) {
				t.Fatalf("%s: an edit moved the original document: %v", tc.stem, err)
			}
		})
	}

	t.Run("noop", func(t *testing.T) {
		t.Parallel()
		input := read(t, "layout.input.sexp")
		inPath := write(t, fixed, "layout.input.sexp", input)
		for _, spelling := range []string{"2", "2.0", "20e-1"} {
			// Go's TestDocumentEdits: a semantic no-op keeps the old spelling,
			// so count stays 2e0 and the document does not move.
			docPath := filepath.Join(fixed.dir, "layout.noop.sexp")
			jsonPath := filepath.Join(fixed.dir, "layout.noop.json")
			if out, err := run(t, fixed, "edit", inPath, "count", spelling, docPath, jsonPath); err != nil {
				t.Fatalf("no-op edit %s: %v\n%s", spelling, err, out)
			}
			got, err := os.ReadFile(docPath)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, input) {
				t.Fatalf("no-op edit %s rewrote the document:\n%q", spelling, got)
			}
		}
	})

	t.Run("typed-noop", func(t *testing.T) {
		t.Parallel()
		// Go internal/codegen/gotable/textform.go float: a float kind's canon
		// is the LANDED bit pattern, so float32 1.00000001 rounds back to 1.0
		// and the edit is a semantic no-op that keeps the source spelling
		// (§16.9.3). Go integer gives fraction fixed(16,16) the exact scaled
		// value, so 1.500000 and 15e-1 are the same typed value as 1.5. The
		// rows are cppTextFormKindsSchema's own `real float32` (json "float")
		// and `fraction fixed(16,16)`; TestDocumentEdits pins the same no-op
		// rule on layout's count.
		input := []byte("(:float 1.0 :fraction 1.5)")
		inPath := write(t, fixed, "typed.input.sexp", input)
		for _, e := range []struct{ path, spelling string }{
			{"float", "1.00000001"},
			{"fraction", "1.500000"},
			{"fraction", "15e-1"},
		} {
			docPath := filepath.Join(fixed.dir, "typed.noop.sexp")
			jsonPath := filepath.Join(fixed.dir, "typed.noop.json")
			if out, err := run(t, fixed, "edit", inPath, e.path, e.spelling, docPath, jsonPath); err != nil {
				t.Fatalf("typed no-op %s %s: %v\n%s", e.path, e.spelling, err, out)
			}
			got, err := os.ReadFile(docPath)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, input) {
				t.Fatalf("typed no-op %s %s rewrote the document:\n%q", e.path, e.spelling, got)
			}
		}
		// A neighbouring float32 value is a real change and replaces only its
		// token span.
		docPath := filepath.Join(fixed.dir, "typed.changed.sexp")
		jsonPath := filepath.Join(fixed.dir, "typed.changed.json")
		if out, err := run(t, fixed, "edit", inPath, "float", "2.5", docPath, jsonPath); err != nil {
			t.Fatalf("typed change: %v\n%s", err, out)
		}
		got, err := os.ReadFile(docPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "(:float 2.5 :fraction 1.5)" {
			t.Fatalf("typed change replaced more than its token:\n%q", got)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		input := read(t, "roadmap.input.sexp")
		inPath := write(t, fixed, "roadmap.input.sexp", input)
		docPath := filepath.Join(fixed.dir, "refused.sexp")
		jsonPath := filepath.Join(fixed.dir, "refused.json")
		refusal := func(t *testing.T, path, text, want string) {
			t.Helper()
			out, err := run(t, fixed, "edit", inPath, path, text, docPath, jsonPath)
			if err == nil {
				t.Fatalf("edit %q %q was accepted", path, text)
			}
			if got := strings.TrimSpace(string(out)); got != want {
				t.Fatalf("edit %q %q refused %q, want %q", path, text, got, want)
			}
		}
		// Go's TestDocumentEdits: an out-of-bound edit is refused by name and
		// the old pair stays.
		refusal(t, "count", "11", "text_bound")
		if out, err := run(t, fixed, "save", inPath, docPath); err != nil {
			t.Fatalf("save after refusal: %v\n%s", err, out)
		}
		if got, err := os.ReadFile(docPath); err != nil || !reflect.DeepEqual(got, input) {
			t.Fatalf("a refused edit moved the document: %v", err)
		}
		// Go's TestDocumentEdits: a compound through its children, an absent
		// insert, a compound spelling and two tokens are all edit conflicts.
		for _, e := range []struct{ path, text string }{
			{"nodes", "()"},
			{"nodes[0]", "1"},
			{"enabled", "true"},
			{"count", "(1)"},
			{"count", "1 2"},
		} {
			refusal(t, e.path, e.text, "text_edit_conflict")
		}
		// Two edits of one span: Go's TestDocumentEdits refuses the pair.
		out, err := run(t, fixed, "edit2", inPath, "count", "1", "count", "2", docPath, jsonPath)
		if err == nil || strings.TrimSpace(string(out)) != "text_edit_conflict" {
			t.Fatalf("two edits of one span: %v %q", err, out)
		}
	})

	t.Run("disjoint", func(t *testing.T) {
		t.Parallel()
		input := read(t, "roadmap.input.sexp")
		inPath := write(t, fixed, "roadmap.input.sexp", input)
		docPath := filepath.Join(fixed.dir, "roadmap.disjoint.sexp")
		jsonPath := filepath.Join(fixed.dir, "roadmap.disjoint.json")
		// Go's TestDocumentEdits applies two disjoint edits by ORIGINAL span, so
		// no offset drifts; its expected bytes are the same concatenation.
		src := string(input)
		ts := strings.Index(src, `"First"`)
		cs := strings.Index(src, ":count 2") + len(":count ")
		want := src[:ts] + `"a much longer title"` + src[ts+len(`"First"`):cs] + "7" + src[cs+len("2"):]
		out, err := run(t, fixed, "edit2", inPath, "count", "7", "nodes[0].title", `"a much longer title"`, docPath, jsonPath)
		if err != nil {
			t.Fatalf("two disjoint edits: %v\n%s", err, out)
		}
		got, err := os.ReadFile(docPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("two disjoint edits:\n got %q\nwant %q", got, want)
		}
	})

	// THE SAME WRITER ON A VARIABLE ROOT (§16.9.1: the form is selected by the
	// root declaration). The edit and rewrite are the same oracle bytes.
	variable := prepare(t, strings.Replace(cppTextFormKindsSchema(), "fixed table Document", "table Document", 1), cppTextFormWriteVariableDriver)
	t.Run("variable", func(t *testing.T) {
		t.Parallel()
		input := read(t, "roadmap.input.sexp")
		inPath := write(t, variable, "roadmap.input.sexp", input)
		docPath := filepath.Join(variable.dir, "roadmap.edited.sexp")
		jsonPath := filepath.Join(variable.dir, "roadmap.edited.json")
		out, err := run(t, variable, "edit", inPath, "nodes[0].title", `"Changed"`, docPath, jsonPath)
		if err != nil {
			t.Fatalf("variable edit: %v\n%s", err, out)
		}
		got, err := os.ReadFile(docPath)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, read(t, "roadmap.edited.sexp")) {
			t.Fatalf("variable edit changed more than its value:\n%q", got)
		}
		want := roadmap
		setPath(want, "nodes[0].title", "Changed")
		gotJSON, err := os.ReadFile(jsonPath)
		if err != nil {
			t.Fatal(err)
		}
		same(t, "variable edited value", decode(t, gotJSON), want)
	})
}

// cppTextFormWriteDriver drives the retained document and the leaf edit. All
// output is written to files so the bytes (trailing spaces, CRLF, the absence
// of a last newline) survive.
const cppTextFormWriteDriver = `#include <stdio.h>
#include <string.h>
#include <string>
#include <vector>
#include "ProbeTable.h"

using namespace probe;

static bool slurp( const char * path, std::string & out )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) { return false; }
    char buffer[4096];
    size_t n;
    while ( ( n = fread( buffer, 1, sizeof( buffer ), f ) ) > 0 ) { out.append( buffer, n ); }
    fclose( f );
    return true;
}

static bool spit( const char * path, const std::string & bytes )
{
    FILE * f = fopen( path, "wb" );
    if ( f == NULL ) { return false; }
    if ( !bytes.empty() ) { fwrite( bytes.data(), 1, bytes.size(), f ); }
    fclose( f );
    return true;
}

static void print_json( const Document & value, const char * path )
{
    std::vector<char> buffer( (size_t) DocumentToJsonMeasure( value ) + 1 );
    const int64_t n = DocumentToJson( value, buffer.data(), (int64_t) buffer.size() );
    std::string out( buffer.data(), (size_t) n );
    spit( path, out );
}

int main( int argc, char ** argv )
{
    if ( argc < 2 ) { return 2; }
    const std::string mode = argv[1];
    std::string text;
    if ( argc < 3 || !slurp( argv[2], text ) ) { printf( "input missing\n" ); return 2; }
    const char * document = NULL;
    int64_t document_bytes = 0;
    Document value;
    const char * refusal = NULL;
    if ( !DocumentLoadTextForm( text.data(), (int64_t) text.size(), &document, &document_bytes, value, &refusal ) )
    {
        printf( "%s\n", refusal == NULL ? "?" : refusal );
        return 3;
    }
    const int64_t room = 1 << 20;
    std::vector<char> out( (size_t) room );
    if ( mode == "save" )
    {
        if ( argc < 4 ) { return 2; }
        const int64_t n = DocumentSaveTextForm( document, document_bytes, out.data(), room );
        if ( n < 0 ) { printf( "save failed\n" ); return 2; }
        return spit( argv[3], std::string( out.data(), (size_t) n ) ) ? 0 : 2;
    }
    if ( mode == "edit" && argc >= 7 )
    {
        TableTextFormEdit edit;
        edit.path = argv[3];
        edit.text = argv[4];
        int64_t edited_bytes = 0;
        if ( !DocumentEditTextForm( document, document_bytes, &edit, 1, out.data(), room, &edited_bytes, value, &refusal ) )
        {
            printf( "%s\n", refusal == NULL ? "buffer" : refusal );
            return 3;
        }
        if ( !spit( argv[5], std::string( out.data(), (size_t) edited_bytes ) ) ) { return 2; }
        print_json( value, argv[6] );
        return 0;
    }
    if ( mode == "edit2" && argc >= 9 )
    {
        TableTextFormEdit edits[2];
        edits[0].path = argv[3]; edits[0].text = argv[4];
        edits[1].path = argv[5]; edits[1].text = argv[6];
        int64_t edited_bytes = 0;
        if ( !DocumentEditTextForm( document, document_bytes, edits, 2, out.data(), room, &edited_bytes, value, &refusal ) )
        {
            printf( "%s\n", refusal == NULL ? "buffer" : refusal );
            return 3;
        }
        if ( !spit( argv[7], std::string( out.data(), (size_t) edited_bytes ) ) ) { return 2; }
        print_json( value, argv[8] );
        return 0;
    }
    printf( "bad mode\n" );
    return 2;
}
`

// cppTextFormWriteVariableDriver is the same probe on a VARIABLE root: the
// read and edit land in a builder, Lock packs it, and AsConst is the value the
// JSON walk prints.
const cppTextFormWriteVariableDriver = `#include <stdio.h>
#include <string.h>
#include <string>
#include <vector>
#include "ProbeTable.h"

using namespace probe;

static bool slurp( const char * path, std::string & out )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) { return false; }
    char buffer[4096];
    size_t n;
    while ( ( n = fread( buffer, 1, sizeof( buffer ), f ) ) > 0 ) { out.append( buffer, n ); }
    fclose( f );
    return true;
}

static bool spit( const char * path, const std::string & bytes )
{
    FILE * f = fopen( path, "wb" );
    if ( f == NULL ) { return false; }
    if ( !bytes.empty() ) { fwrite( bytes.data(), 1, bytes.size(), f ); }
    fclose( f );
    return true;
}

static void print_json( const Document * value, const char * path )
{
    std::vector<char> buffer( (size_t) DocumentToJsonMeasure( value ) + 1 );
    const int64_t n = DocumentToJson( value, buffer.data(), (int64_t) buffer.size() );
    spit( path, std::string( buffer.data(), (size_t) n ) );
}

int main( int argc, char ** argv )
{
    if ( argc < 2 ) { return 2; }
    const std::string mode = argv[1];
    std::string text;
    if ( argc < 3 || !slurp( argv[2], text ) ) { printf( "input missing\n" ); return 2; }
    const char * document = NULL;
    int64_t document_bytes = 0;
    DocumentBuilder builder;
    const char * refusal = NULL;
    if ( !DocumentLoadTextForm( text.data(), (int64_t) text.size(), &document, &document_bytes, builder, &refusal ) )
    {
        printf( "%s\n", refusal == NULL ? "?" : refusal );
        return 3;
    }
    const int64_t room = 1 << 20;
    std::vector<char> out( (size_t) room );
    if ( mode == "save" )
    {
        if ( argc < 4 ) { return 2; }
        const int64_t n = DocumentSaveTextForm( document, document_bytes, out.data(), room );
        if ( n < 0 ) { printf( "save failed\n" ); return 2; }
        return spit( argv[3], std::string( out.data(), (size_t) n ) ) ? 0 : 2;
    }
    if ( mode == "edit" && argc >= 7 )
    {
        TableTextFormEdit edit;
        edit.path = argv[3];
        edit.text = argv[4];
        int64_t edited_bytes = 0;
        if ( !DocumentEditTextForm( document, document_bytes, &edit, 1, out.data(), room, &edited_bytes, builder, &refusal ) )
        {
            printf( "%s\n", refusal == NULL ? "buffer" : refusal );
            return 3;
        }
        if ( !builder.Lock() ) { printf( "lock failed\n" ); return 2; }
        if ( !spit( argv[5], std::string( out.data(), (size_t) edited_bytes ) ) ) { return 2; }
        print_json( builder.AsConst(), argv[6] );
        return 0;
    }
    if ( mode == "edit2" && argc >= 9 )
    {
        TableTextFormEdit edits[2];
        edits[0].path = argv[3]; edits[0].text = argv[4];
        edits[1].path = argv[5]; edits[1].text = argv[6];
        int64_t edited_bytes = 0;
        if ( !DocumentEditTextForm( document, document_bytes, edits, 2, out.data(), room, &edited_bytes, builder, &refusal ) )
        {
            printf( "%s\n", refusal == NULL ? "buffer" : refusal );
            return 3;
        }
        if ( !builder.Lock() ) { printf( "lock failed\n" ); return 2; }
        if ( !spit( argv[7], std::string( out.data(), (size_t) edited_bytes ) ) ) { return 2; }
        print_json( builder.AsConst(), argv[8] );
        return 0;
    }
    printf( "bad mode\n" );
    return 2;
}
`
