package check

import (
	"strings"
	"testing"
)

// TestBasenameCaseFolding: the case controls of File-name safety. A reserved
// word or a hidden module is refused in the spelling the target reads, and
// a spelling no target folds to it stays legal; two basenames that differ only
// by case are refused together, whatever else they spell.
func TestBasenameCaseFolding(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		base string
		want string // "" = legal
	}{
		{"Enum", "Rust"},      // Rust lowercases into mod enum
		{"enum", "Goose"},     // every spelling-preserving target too
		{"Def", ""},           // def is Lobster's, Python's and Ruby's, and none of them folds
		{"Main", "Rust"},      // main.rs
		{"Io", ""},            // require("Io") is not Lua's io
		{"io", "Lua"},         // require("io") is
		{"Init", ""},          // require("./Init") is the file
		{"SERIALIZE", "Rust"}, // folds
		{"Schema_x", "Squirrel"},
		{"serialize2", ""},
		{"x_testing", ""},
		{"x_test", "Go"},
		{"Holder_windows", "Go"},
		{"Probe", ""},
	} {
		got := basenameRefusal(tc.base)
		if tc.want == "" && got != "" {
			t.Errorf("%s: refused (%s), want legal", tc.base, got)
		}
		if tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q, want a refusal naming %s", tc.base, got, tc.want)
		}
	}
}

// TestInlineBufferNames: the C# packet emitter declares InlineBuffer<N> for
// each fixed length N >= 1 a unit uses, so every such spelling is refused as
// a declaration name (SPEC §4.6, Platform-name safety), and the spellings
// that are not one stay legal.
func TestInlineBufferNames(t *testing.T) {
	for name, want := range map[string]bool{
		"InlineBuffer4": true, "InlineBuffer12": true, "InlineBuffer1": true,
		"InlineBuffer": false, "InlineBuffer0": false, "InlineBuffer04": false,
		"InlineBuffer4x": false, "InlineBufferX": false, "inlineBuffer4": false,
	} {
		if got := isInlineBufferName(name); got != want {
			t.Errorf("isInlineBufferName(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestSerializeMacrosAreRefusedAsDeclarations: every macro serialize.h defines
// is refused as a declaration name for every target, naming C++ and the header
// (SPEC §4.6, Platform-name safety; n_internal_66, 80 and 88 found
// serialize_fixed, write_float, write_int and write_string), and the same name
// as a table field or a union arm is a member, which no macro reaches, so it
// takes no such refusal.
func TestSerializeMacrosAreRefusedAsDeclarations(t *testing.T) {
	t.Parallel()
	if len(serializeMacros) != 94 {
		t.Fatalf("%d serialize.h macros, want the 94 serialize v2.1.6 defines", len(serializeMacros))
	}
	for _, name := range serializeMacros {
		decl := checkerErrors(t, "package probe\n\nenum "+name+"\n{ A, B }\n\ntable T\n{\n    k "+name+"\n}\n")
		found := false
		for _, e := range decl {
			if strings.Contains(e, "serialize.h") && strings.Contains(e, "(C++)") && strings.Contains(e, `"`+name+`"`) {
				found = true
			}
		}
		if !found {
			t.Errorf("enum %s: %q, want the serialize.h refusal naming C++", name, decl)
		}
		member := checkerErrors(t, "package probe\n\ntable A\n{\n    x uint8\n}\n\nunion U\n{\n    "+name+" A\n}\n\ntable T\n{\n    "+name+" uint8\n    u U\n}\n")
		for _, e := range member {
			if strings.Contains(e, "serialize.h") {
				t.Errorf("field and arm %s: refused as a macro (%s), want accepted: a member is no declaration", name, e)
			}
		}
	}
}
