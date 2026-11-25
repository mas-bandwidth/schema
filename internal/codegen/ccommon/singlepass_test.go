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
		{"an #else is the negation of the #if",
			"#if defined( A )\nint a;\n#else\nint b;\n#endif\n",
			"#if defined( A )\nint a;\n#endif\n#if !( defined( A ) )\nint b;\n#endif\n"},
		{"an #elif is taken under every earlier condition false",
			"#if defined( _MSC_VER )\n#define I __forceinline\n#elif defined( __GNUC__ ) || defined( __clang__ )\n#define I inline __attribute__( ( always_inline ) )\n#else\n#define I inline\n#endif\n",
			"#if defined( _MSC_VER )\n#define I __forceinline\n#endif\n#if !( defined( _MSC_VER ) ) && ( defined( __GNUC__ ) || defined( __clang__ ) )\n#define I inline __attribute__( ( always_inline ) )\n#endif\n#if !( defined( _MSC_VER ) ) && !( defined( __GNUC__ ) || defined( __clang__ ) )\n#define I inline\n#endif\n"},
		{"#ifdef and #ifndef are spelled as defined()",
			"#ifdef A\nint a;\n#else\nint b;\n#endif\n#ifndef B // why\nint c;\n#else\nint d;\n#endif\n",
			"#ifdef A\nint a;\n#endif\n#if !( defined( A ) )\nint b;\n#endif\n#ifndef B // why\nint c;\n#endif\n#if !( !defined( B ) )\nint d;\n#endif\n"},
		{"a chain inside a branch is rewritten in place",
			"#ifndef G\n#if A\nint a;\n#else\n#if B\nint b;\n#else\nint c;\n#endif\n#endif\n#endif\n",
			"#ifndef G\n#if A\nint a;\n#endif\n#if !( A )\n#if B\nint b;\n#endif\n#if !( B )\nint c;\n#endif\n#endif\n#endif\n"},
		{"the directive's indentation is kept",
			"    #if A\n    int a;\n    #else\n    int b;\n    #endif\n",
			"    #if A\n    int a;\n    #endif\n    #if !( A )\n    int b;\n    #endif\n"},
		{"a continued condition is joined",
			"#if defined( A ) \\\n    && defined( B )\nint a;\n#else\nint b;\n#endif\n",
			"#if defined( A ) \\\n    && defined( B )\nint a;\n#endif\n#if !( defined( A ) && defined( B ) )\nint b;\n#endif\n"},
		{"a directive inside a block comment is text",
			"/* a note:\n#else\n */\n#if A\nint a;\n#endif\n",
			"/* a note:\n#else\n */\n#if A\nint a;\n#endif\n"},
		{"a comment opener inside a string opens nothing",
			"const char * s = \"/*\";\n#if A\nint a;\n#else\nint b;\n#endif\n",
			"const char * s = \"/*\";\n#if A\nint a;\n#endif\n#if !( A )\nint b;\n#endif\n"},
		{"a continued #define is carried whole",
			"#if A\n#define F( x ) \\\n    do { x; } while ( 0 )\n#else\n#define F( x )\n#endif\n",
			"#if A\n#define F( x ) \\\n    do { x; } while ( 0 )\n#endif\n#if !( A )\n#define F( x )\n#endif\n"},
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
			"#if A\n#ifdef Q\n#undef A\n#endif\n#elif B\nint b;\n#endif\n", "changes A"},
		{"a chain never closed", "#if A\nint a;\n#else\nint b;\n", "never closed"},
		{"an #else with no #if", "int a;\n#else\nint b;\n", "with no #if"},
		{"an #endif with no #if", "int a;\n#endif\n", "with no #if"},
		{"an #elif after #else", "#if A\n#else\n#elif B\n#endif\n", "after #else"},
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
