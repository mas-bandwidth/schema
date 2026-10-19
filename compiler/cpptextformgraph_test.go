package compiler

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestCPPTextFormGraphVector drives the C++ hand-editable text form's read on
// a VARIABLE root whose Graph document names shared nodes by label
// (docs/SPEC-TABLES.md §16.9, §16.7). The schema is the variable contract
// (test/conformance/textform/variable-contract.json) as Go declares it in
// internal/codegen/gotable/textform_test.go textFormVariableSchema, and every
// expected answer is the one Go pins there: TestGraphVector for the positive
// vector and TestGraphRefusals for the sharing edges. This port computes no
// expectation of its own.
func TestCPPTextFormGraphVector(t *testing.T) {
	t.Parallel()
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated read is C++: no c++ on PATH")
	}
	files, err := New().Generate(unitFromSource(t, cppTextFormGraphSchema()), "cpp", Options{})
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
	if !strings.Contains(string(files["ProbeTable.h"]), "GraphLoadTextForm(") {
		t.Fatal("the generated header carries no GraphLoadTextForm")
	}
	if _, err := os.Stat(filepath.Join("..", "serialize", "serialize.h")); err != nil {
		stub := "#pragma once\n#include <stdint.h>\nnamespace serialize { using int128_t = __int128; using uint128_t = unsigned __int128; }\n"
		if err := os.WriteFile(filepath.Join(dir, "serialize.h"), []byte(stub), 0600); err != nil {
			t.Fatal(err)
		}
	}
	compile := func(t *testing.T, main string, bin string) {
		t.Helper()
		args := []string{"-std=c++17", "-O1", "-Wall", "-Wextra", "-Werror", "-I", dir, main, filepath.Join(dir, "ProbeTable.cpp"), "-o", bin}
		if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
			t.Fatalf("compile: %v\n%s", err, out)
		}
	}
	runText := func(t *testing.T, input string) string {
		t.Helper()
		main := filepath.Join(dir, filepath.Base(t.Name())+".cpp")
		d := strings.ReplaceAll(cppGraphDriver, "DOCUMENT", cppEscape(input))
		if err := os.WriteFile(main, []byte(d), 0600); err != nil {
			t.Fatal(err)
		}
		bin := strings.TrimSuffix(main, ".cpp")
		compile(t, main, bin)
		out, err := exec.Command(bin).CombinedOutput()
		if err != nil {
			if _, ok := err.(*exec.ExitError); !ok {
				t.Fatalf("run: %v\n%s", err, out)
			}
		}
		return strings.TrimSpace(string(out))
	}

	positive := func(t *testing.T) {
		text := runText(t, string(cppMustRead(t, "variable.input.sexp")))
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		var got any
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("the graph read did not write JSON: %v\n%s", err, text)
		}
		// Go internal/codegen/gotable/textform_test.go TestGraphVector builds
		// this from variable.value.json: one node defined under the document's
		// label 7, named again by reference, and the null item.
		want := map[string]any{
			"items": []any{
				map[string]any{"&node": json.Number("1"), "id": json.Number("6")},
				map[string]any{"&node": json.Number("1")},
				nil,
			},
			"lookup": map[string]any{"west": nil, "east": json.Number("2")},
			"blob":   "",
		}
		if !reflect.DeepEqual(got, want) {
			g, _ := json.Marshal(got)
			w, _ := json.Marshal(want)
			t.Fatalf("variable value:\n got %s\nwant %s", g, w)
		}
	}
	t.Run("variable", func(t *testing.T) { t.Parallel(); positive(t) })

	// The corpus refusals Go's TestGraphRefusals reads, each .sexp against its
	// .reason, and the sharing edges beyond the corpus that the same Go test
	// pins (text_reference for a second definition, a non-label spelling, a
	// label that is not the first pair and an unknown sharing name; the
	// duplicate map key is the ordered map's own text_duplicate_key).
	refusal := func(t *testing.T, input, want string) {
		t.Helper()
		if got := runText(t, input); got != "refused "+want {
			t.Fatalf("%q:\n got %q, want %q", input, got, "refused "+want)
		}
	}
	reason := func(stem string) string {
		b, err := os.ReadFile(filepath.Join("..", "test", "conformance", "textform", "refusals", stem+".reason"))
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(b))
	}
	for _, stem := range []string{"duplicate-map", "forward-reference"} {
		stem := stem
		t.Run(stem, func(t *testing.T) {
			t.Parallel()
			refusal(t, string(cppMustRead(t, filepath.Join("refusals", stem+".sexp"))), reason(stem))
		})
	}
	for i, tc := range []struct{ input, want string }{
		{`(:items (("&node" 1 :id 1) ("&node" 1 :id 2)))`, "text_reference"},
		{`(:items (("&node" 1.0 :id 1)))`, "text_reference"},
		{`(:items (("&node" 01 :id 1)))`, "text_syntax"},
		{`(:items ((:id 1 "&node" 1)))`, "text_reference"},
		{`(:items (("&ref" 1 :id 1)))`, "text_reference"},
	} {
		i, tc := i, tc
		t.Run("edge"+string(rune('0'+i)), func(t *testing.T) {
			t.Parallel()
			refusal(t, tc.input, tc.want)
		})
	}
}

// cppTextFormGraphSchema is the variable form of
// test/conformance/textform/variable-contract.json, declared as Go's
// internal/codegen/gotable/textform_test.go textFormVariableSchema declares it
// (its GNode is the document's Node).
func cppTextFormGraphSchema() string {
	return `package probe

table GNode
{
    id int32
}

table Graph
{
    items  []*GNode
    lookup map[string(8)]?int32
    blob   *bytes
}
`
}

// cppGraphDriver reads a Graph document on a variable root, locks the builder
// and prints the value the generated JSON walk writes. That is the same walk
// Go's graphJSON (internal/codegen/gotable/textform_test.go) compares.
const cppGraphDriver = `#include <stdio.h>
#include <string.h>
#include <vector>
#include "ProbeTable.h"

using namespace probe;

int main( void )
{
    static const char text[] = "DOCUMENT";
    GraphBuilder builder;
    const char * refusal = NULL;
    if ( !GraphLoadTextForm( text, (int64_t) strlen( text ), builder, &refusal ) )
    {
        printf( "refused %s\n", refusal == NULL ? "?" : refusal );
        return 1;
    }
    if ( !builder.Lock() ) { printf( "lock failed\n" ); return 1; }
    const Graph * value = builder.AsConst();
    std::vector<char> buffer( (size_t) GraphToJsonMeasure( value ) + 1 );
    const int64_t n = GraphToJson( value, buffer.data(), (int64_t) buffer.size() );
    fwrite( buffer.data(), 1, (size_t) n, stdout );
    return 0;
}
`
