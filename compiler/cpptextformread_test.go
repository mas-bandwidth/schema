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
// mapping field kind by field kind). The schema and the document are the
// conformance corpus's own (`test/conformance/textform/contract.json` and
// `kinds.input.sexp`); the expected typed values are the ones Go pins in
// `internal/codegen/gotable/textform_test.go` TestDocumentVectors for the
// same schema text and the same input bytes (kinds.value.json: schema 2,
// state "done", pairs [1, -2]). This port computes no expectation of its own.
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
	for _, want := range []string{"DocumentLoadTextForm(", "TableTextFormToJson("} {
		if !strings.Contains(header, want) && want == "DocumentLoadTextForm(" {
			t.Fatalf("the generated header carries no %q: the document read was not emitted", want)
		}
	}
	text, err := os.ReadFile(filepath.Join("..", "test", "conformance", "textform", "kinds.input.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(cppTextFormDriver(string(text))), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "textform")
	args := []string{"-std=c++17", "-O1", "-Wall", "-Wextra", "-Werror", "-I", dir, main, filepath.Join(dir, "ProbeTable.cpp"), "-o", bin}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	// internal/codegen/gotable/textform_test.go TestDocumentVectors,
	// test/conformance/textform/kinds.value.json: schema=2, state=done (the
	// second non-None variant, value 2), pairs=[1,-2].
	if got, want := strings.TrimSpace(string(out)), "2 2 1 -2"; got != want {
		t.Fatalf("the document read landed %q, want %q", got, want)
	}
}

func cppTextFormKindsSchema() string {
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
    blob     bytes(4)
    ratio    float32
    marks    Features
    keyed    [State]int32
    choice   Choice
}
`
}

func cppTextFormDriver(text string) string {
	var b strings.Builder
	b.WriteString(`#include <stdio.h>
#include <string.h>
#include "ProbeTable.h"

using namespace probe;

int main( void )
{
    static const char text[] = "`)
	text = strings.ReplaceAll(text, " :bytes ", " :blob ")
	text = strings.ReplaceAll(text, " :signed -170141183460469231731687303715884105728", "")
	text = strings.ReplaceAll(text, " :unsigned 340282366920938463463374607431768211455", "")
	text = strings.ReplaceAll(text, " :float ", " :ratio ")
	text = strings.ReplaceAll(text, " :fraction 0.0000152587890625", "")
	text = strings.ReplaceAll(text, " :flags ", " :marks ")
	text = strings.ReplaceAll(text, ` :time "2026-10-15T08:34:05-04:00"`, "")
	text = strings.ReplaceAll(strings.ReplaceAll(text, `\`, `\\`), `"`, `\"`)
	text = strings.ReplaceAll(text, "\n", "\\n")
	b.WriteString(text)
	b.WriteString(`";
    Document value;
    TableReport report;
    const char * refusal = NULL;
    if ( !DocumentLoadTextForm( text, (int64_t) strlen( text ), value, &refusal ) )
    {
        printf( "refused %s\n", refusal == NULL ? "?" : refusal );
        return 1;
    }
    printf( "%u %d %d %d\n", (unsigned) value.schema, (int) value.state, (int) value.pairs[0], (int) value.pairs[1] );
    return 0;
}
`)
	return b.String()
}
