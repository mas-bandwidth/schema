package ccommon

import (
	"strings"
	"testing"
)

// The format gates read a generated header once per branch an #if chain can
// take; SinglePassConditionals leaves every chain one branch long, with the
// same branch kept under the same conditions. These pin the rewrite's text
// and its refusals.
func TestSinglePassConditionals(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"a file with no chain is unchanged",
			"#ifndef GUARD\n#define GUARD\nint a;\n#ifdef X\nint x;\n#endif\n#endif /* GUARD */\n",
			"#ifndef GUARD\n#define GUARD\nint a;\n#ifdef X\nint x;\n#endif\n#endif /* GUARD */\n"},
		{"a lone #if is kept whatever it tests",
			"#if __has_builtin( x ) && V > 2\nint a;\n#endif\n",
			"#if __has_builtin( x ) && V > 2\nint a;\n#endif\n"},
		{"an #else is the negation of the #if",
			"#if defined( A )\nint a;\n#else\nint b;\n#endif\n",
			"#if defined( A )\nint a;\n#endif\n#if !( defined( A ) )\nint b;\n#endif\n"},
		{"an #elif is taken under every earlier condition false",
			"#if defined( _MSC_VER )\n#define I __forceinline\n#elif defined( __GNUC__ ) || defined( __clang__ )\n#define I inline __attribute__( ( always_inline ) )\n#else\n#define I inline\n#endif\n",
			"#if defined( _MSC_VER )\n#define I __forceinline\n#endif\n#if !( defined( _MSC_VER ) ) && ( defined( __GNUC__ ) || defined( __clang__ ) )\n#define I inline __attribute__( ( always_inline ) )\n#endif\n#if !( defined( _MSC_VER ) ) && !( defined( __GNUC__ ) || defined( __clang__ ) )\n#define I inline\n#endif\n"},
		{"a compiler-predefined name may be compared by value, with a hex literal",
			"#if defined( __BYTE_ORDER__ ) && __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__\nint le;\n#elif _MSC_VER >= 0x76C\nint msvc;\n#endif\n",
			"#if defined( __BYTE_ORDER__ ) && __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__\nint le;\n#endif\n#if !( defined( __BYTE_ORDER__ ) && __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__ ) && ( _MSC_VER >= 0x76C )\nint msvc;\n#endif\n"},
		{"a system header in a branch is kept",
			"#if defined( _MSC_VER )\n#include <intrin.h>\n#else\nint b;\n#endif\n",
			"#if defined( _MSC_VER )\n#include <intrin.h>\n#endif\n#if !( defined( _MSC_VER ) )\nint b;\n#endif\n"},
		{"a literal's suffix is not a name the chain reads",
			"#if defined( __STDC_VERSION__ ) && __STDC_VERSION__ >= 201112L && !defined( __x86_64__ )\n#include <stdatomic.h>\n#else\nint b;\n#endif\n",
			"#if defined( __STDC_VERSION__ ) && __STDC_VERSION__ >= 201112L && !defined( __x86_64__ )\n#include <stdatomic.h>\n#endif\n#if !( defined( __STDC_VERSION__ ) && __STDC_VERSION__ >= 201112L && !defined( __x86_64__ ) )\nint b;\n#endif\n"},
		{"#ifdef and #ifndef are spelled as defined()",
			"#ifdef A\nint a;\n#else\nint b;\n#endif\n#ifndef B // why\nint c;\n#else\nint d;\n#endif\n",
			"#ifdef A\nint a;\n#endif\n#if !( defined( A ) )\nint b;\n#endif\n#ifndef B // why\nint c;\n#endif\n#if !( !defined( B ) )\nint d;\n#endif\n"},
		{"defined without parentheses is a defined() test",
			"#if defined A && !defined B\nint a;\n#else\nint b;\n#endif\n",
			"#if defined A && !defined B\nint a;\n#endif\n#if !( defined A && !defined B )\nint b;\n#endif\n"},
		{"a chain inside a branch is rewritten in place",
			"#ifndef G\n#if defined( A )\nint a;\n#else\n#if defined( B )\nint b;\n#else\nint c;\n#endif\n#endif\n#endif\n",
			"#ifndef G\n#if defined( A )\nint a;\n#endif\n#if !( defined( A ) )\n#if defined( B )\nint b;\n#endif\n#if !( defined( B ) )\nint c;\n#endif\n#endif\n#endif\n"},
		{"the directive's indentation is kept",
			"    #if defined( A )\n    int a;\n    #else\n    int b;\n    #endif\n",
			"    #if defined( A )\n    int a;\n    #endif\n    #if !( defined( A ) )\n    int b;\n    #endif\n"},
		{"a continued condition is joined",
			"#if defined( A ) \\\n    && defined( B )\nint a;\n#else\nint b;\n#endif\n",
			"#if defined( A ) \\\n    && defined( B )\nint a;\n#endif\n#if !( defined( A ) && defined( B ) )\nint b;\n#endif\n"},
		{"a directive inside a block comment is text",
			"/* a note:\n#else\n */\n#if defined( A )\nint a;\n#endif\n",
			"/* a note:\n#else\n */\n#if defined( A )\nint a;\n#endif\n"},
		{"a comment opener inside a string opens nothing",
			"const char * s = \"/*\";\n#if defined( A )\nint a;\n#else\nint b;\n#endif\n",
			"const char * s = \"/*\";\n#if defined( A )\nint a;\n#endif\n#if !( defined( A ) )\nint b;\n#endif\n"},
		{"a continued #define is carried whole",
			"#if defined( A )\n#define F( x ) \\\n    do { x; } while ( 0 )\n#else\n#define F( x )\n#endif\n",
			"#if defined( A )\n#define F( x ) \\\n    do { x; } while ( 0 )\n#endif\n#if !( defined( A ) )\n#define F( x )\n#endif\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SinglePassConditionals(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tc.want)
			}
			if strings.Contains(got, "#else") && !strings.Contains(tc.in, "/* a note:") || strings.Contains(got, "#elif") {
				t.Fatalf("an #else or #elif survived:\n%s", got)
			}
		})
	}
}

func TestSinglePassConditionalsRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"a branch that changes a name its chain reads",
			"#ifndef X\n#define X 1\n#else\nint again;\n#endif\n", "changes X"},
		{"a nested branch that changes a name its outer chain reads",
			"#if defined( A )\n#ifdef Q\n#undef A\n#endif\n#elif defined( B )\nint b;\n#endif\n", "changes A"},
		{"a branch that includes a project header",
			"#if defined( A )\n#include \"a.h\"\n#else\nint b;\n#endif\n", "#includes a project file"},
		{"a branch that includes a header named by a macro",
			"#if defined( A )\n#include HEADER\n#else\nint b;\n#endif\n", "#includes a project file"},
		{"a branch that includes the next header of the same name",
			"#if defined( A )\n#include_next <x.h>\n#else\nint b;\n#endif\n", "#includes a project file"},
		{"a system header in a chain that reads a name a header may define",
			"#ifndef __BYTE_ORDER\n#include <endian.h>\n#else\nint b;\n#endif\n", "#includes a system header, and the chain reads __BYTE_ORDER"},
		{"a reserved name that is not the compiler's, tested by value",
			"#if _POSIX_C_SOURCE >= 200809L\nint a;\n#else\nint b;\n#endif\n", "_POSIX_C_SOURCE is tested by value"},
		{"a chain that tests a macro's value",
			"#if V > 2\nint a;\n#else\nint b;\n#endif\n", "V is tested by value"},
		{"a chain that tests a macro which may expand to defined()",
			"#define ON defined( B )\n#if ON\nint a;\n#undef B\n#else\nint b;\n#endif\n", "ON is tested by value"},
		{"an #elif that might not evaluate where the #if is true",
			"#if !defined( __has_builtin )\nint a;\n#elif __has_builtin( x )\nint b;\n#endif\n", "__has_builtin( ... ) is called"},
		{"a comment that runs past a condition's line",
			"#if defined( A ) /* two\n line */\nint a;\n#else\nint b;\n#endif\n", "not part of a defined() test"},
		{"a defined with no name", "#if defined()\nint a;\n#else\nint b;\n#endif\n", "a defined with no name"},
		{"#elifdef", "#if defined( A )\nint a;\n#elifdef B\nint b;\n#else\nint c;\n#endif\n", "#elifdef is not one the rewrite knows"},
		{"#elifndef", "#if defined( A )\nint a;\n#elifndef B\nint b;\n#endif\n", "#elifndef is not one the rewrite knows"},
		{"a carriage return", "#if defined( A )\r\nint a;\r\n#else\r\nint b;\r\n#endif\r\n", "carriage return"},
		{"a chain never closed", "#if defined( A )\nint a;\n#else\nint b;\n", "never closed"},
		{"an #else with no #if", "int a;\n#else\nint b;\n", "with no #if"},
		{"an #endif with no #if", "int a;\n#endif\n", "with no #if"},
		{"an #elif after #else", "#if defined( A )\n#else\n#elif defined( B )\n#endif\n", "after #else"},
		{"an #if with no condition", "#if\nint a;\n#endif\n", "no condition"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SinglePassConditionals(tc.in)
			if err == nil {
				t.Fatalf("rewrote it, want a refusal naming %q:\n%s", tc.want, got)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refused with %v, want %q", err, tc.want)
			}
		})
	}
}
