package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCPPTextFormDocumentVectors drives the C++ hand-editable text form's
// DOCUMENT read (docs/SPEC-TABLES.md §16.9.1 grammar and identity, §16.9.2
// mapping field kind by field kind). The schema is the conformance corpus's
// own contract (test/conformance/textform/contract.json) declared (§16.4 for
// the aliased names), with exactly the rows this C++ build cannot yet carry
// removed: timestamp (the cpp target refuses the kind until its own card,
// cpp-carries-timestamp-on-the-binary-forms) and the int128/uint128/fixed
// rows (their generated storage is serialize.h's sibling, not staged here).
// Every expected value is the one Go pins in
// internal/codegen/gotable/textform_test.go TestDocumentVectors for the same
// schema text and the same input bytes (kinds.value.json). This port computes
// no expectation of its own; the refusals assert §16.9.4's stable names
// against the whole refusals corpus (test/conformance/textform/refusals),
// each .sexp against its .reason, as Go's TestDocumentRefusals does.
func TestCPPTextFormDocumentVectors(t *testing.T) {
	t.Parallel()
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

	text, err := os.ReadFile(filepath.Join("..", "test", "conformance", "textform", "kinds.input.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	drop := []string{
		` :signed -170141183460469231731687303715884105728`,
		` :unsigned 340282366920938463463374607431768211455`,
		` :fraction 0.0000152587890625`,
		` :time "2026-10-15T08:34:05-04:00"`,
	}
	stripped := string(text)
	for _, d := range drop {
		stripped = strings.ReplaceAll(stripped, d, "")
	}
	t.Run("kinds", func(t *testing.T) {
		t.Parallel()
		main, bin := writeDriver(t, stripped, cppKindsDriver)
		compile(t, main, bin)
		if got, want := run(t, bin), "2 2 1 -2 😀é AP8= 3 2 4 7"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

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
	skip := map[string]string{
		"timestamp": "timestamp is the cpp-carries-timestamp-on-the-binary-forms card",
		"fraction":  "the fixed(16,16) field is not staged in this C++ build (its storage is serialize.h's sibling)",
	}
	for _, name := range names {
		stem := strings.TrimSuffix(filepath.Base(name), ".sexp")
		if stem == "duplicate-map" || stem == "forward-reference" {
			continue // Graph's, run by the variable unit
		}
		input, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if why, ok := skip[stem]; ok {
			t.Run(stem, func(t *testing.T) {
				t.Parallel()
				t.Skip(why)
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
	if ran < 20 {
		t.Fatalf("only %d Document refusal vectors ran", ran)
	}
}

func cppEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

func cppTextFormKindsSchema() string {
	// test/conformance/textform/contract.json declared (§16.4 for the aliased
	// names). The timestamp field is omitted: the cpp target refuses the kind
	// until cpp-carries-timestamp lands; the int128/uint128/fixed fields are
	// omitted because their storage is serialize.h's sibling, not staged here.
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
    wide     wstring(4)
    blob     bytes(4) | json = "bytes"
    real     float32 | json = "float"
    flags    Features
    keyed    [State]int32
    choice   Choice
}
`
}

// cppKindsDriver prints the typed value as a single line; the expected line is
// transcribed from internal/codegen/gotable/textform_test.go TestDocumentVectors
// and kinds.value.json asset schema 2, state done, pairs [1,-2], wide "😀é",
// bytes base64 "AP8=", flags Fast|Safe, keyed {unknown:4, done:7}, choice ping.
const cppKindsDriver = `#include <stdio.h>
#include <string.h>
#include <string>
#include "ProbeTable.h"

using namespace probe;

static void put8( std::string & o, unsigned c )
{
    if ( c < 0x80 ) o += (char)c;
    else if ( c < 0x800 ) { o += (char)(0xC0|(c>>6)); o += (char)(0x80|(c&0x3F)); }
    else if ( c < 0x10000 ) { o += (char)(0xE0|(c>>12)); o += (char)(0x80|((c>>6)&0x3F)); o += (char)(0x80|(c&0x3F)); }
    else { o += (char)(0xF0|(c>>18)); o += (char)(0x80|((c>>12)&0x3F)); o += (char)(0x80|((c>>6)&0x3F)); o += (char)(0x80|(c&0x3F)); }
}

static std::string wide8( const char16_t * u, int len )
{
    std::string o;
    for ( int i = 0; i < len; i++ )
    {
        uint32_t c = u[i];
        if ( c >= 0xD800 && c <= 0xDBFF && i + 1 < len )
        {
            uint32_t lo = u[i+1];
            c = 0x10000 + ((c-0xD800)<<10) + (lo-0xDC00); i++;
        }
        put8( o, c );
    }
    return o;
}

static std::string b64( const uint8_t * b, int len )
{
    static const char a[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    std::string o;
    int i = 0;
    for ( ; i + 2 < len; i += 3 )
    {
        uint32_t v = (b[i]<<16)|(b[i+1]<<8)|b[i+2];
        o += a[(v>>18)&63]; o += a[(v>>12)&63]; o += a[(v>>6)&63]; o += a[v&63];
    }
    if ( i + 1 == len ) { uint32_t v = b[i]<<16; o += a[(v>>18)&63]; o += a[(v>>12)&63]; o += '='; o += '='; }
    else if ( i + 2 == len ) { uint32_t v = (b[i]<<16)|(b[i+1]<<8); o += a[(v>>18)&63]; o += a[(v>>12)&63]; o += a[(v>>6)&63]; o += '='; }
    return o;
}

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
    std::string w = wide8( value.wide, value.wide_length );
    std::string by = b64( value.blob, value.blob_length );
    printf( "%u %d %d %d %s %s %d %d %d %d\n",
        (unsigned) value.schema, (int) value.state,
        (int) value.pairs[0], (int) value.pairs[1],
        w.c_str(), by.c_str(),
        (int) value.flags, (int) value.choice.type,
        (int) value.keyed[State::unknown], (int) value.keyed[State::done] );
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
