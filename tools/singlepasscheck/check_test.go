package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/ccommon"
)

// A functional test: it runs the host's gcc as the preprocessor.

const emitted = `#ifndef ONCE
#define ONCE
#if defined( _MSC_VER )
#define I __forceinline
#elif defined( __GNUC__ ) || defined( __clang__ )
#define I inline __attribute__( ( always_inline ) )
#else
#define I inline
#endif
int first;
#endif
#include <string.h>
#if defined( _MSC_VER )
#error "not for MSVC"
#else
int not_msvc;
#endif
#ifndef ONCE
#define ONCE
int second;
#endif
`

// The real rewrite of a file passes; so the check is not red on everything.
func TestSinglePassCheckPassesTheRewrite(t *testing.T) {
	for _, lang := range []string{"c", "c++"} {
		after, err := ccommon.SinglePassConditionals(emitted)
		if err != nil {
			t.Fatal(err)
		}
		if after == emitted {
			t.Fatal("the rewrite changed nothing; the case compares nothing")
		}
		if err := Compare(emitted, after, lang); err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
	}
}

// Each sabotage below is a rewrite that is wrong; the check must say so,
// under the configuration where the wrong text is kept.
func TestSinglePassCheckRefusesAWrongRewrite(t *testing.T) {
	right, err := ccommon.SinglePassConditionals(emitted)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, from, to, want string }{
		{"a rewrite that drops an #else branch",
			"#if !( defined( _MSC_VER ) ) && !( defined( __GNUC__ ) || defined( __clang__ ) )\n#define I inline\n#endif\n", "",
			"config 2"},
		{"a rewrite that hoists a guarded #if past the #define between it and its twin",
			"#ifndef ONCE\n#define ONCE\n#if defined( _MSC_VER )", "#ifndef ONCE\n#if defined( _MSC_VER )",
			"config 0"},
		{"a rewrite that drops an #error",
			"#error \"not for MSVC\"\n", "",
			"config 1"},
		{"a rewrite that moves an #include",
			"int first;\n#endif\n#include <string.h>\n", "#include <string.h>\nint first;\n#endif\n",
			"config 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Count(right, tc.from) != 1 {
				t.Fatalf("the sabotage's anchor is not in the rewrite exactly once:\n%s", right)
			}
			wrong := strings.Replace(right, tc.from, tc.to, 1)
			err := Compare(emitted, wrong, "c")
			if err == nil {
				t.Fatalf("the check passed a wrong rewrite:\n%s", wrong)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("red as %v, want it red under %s", err, tc.want)
			}
		})
	}
}

// A preprocessor error is red, never two equal empty streams.
func TestSinglePassCheckRefusesAPreprocessorError(t *testing.T) {
	bad := "#if defined( A\nint a;\n#endif\n"
	if err := Compare(bad, bad, "c"); err == nil || !strings.Contains(err.Error(), "the form as emitted") {
		t.Fatalf("got %v, want a preprocessor error on the form as emitted", err)
	}
}

// Over trees: a file that differs, or is on one side only, is named; a tree
// whose directory names no language is refused.
func TestSinglePassCheckNamesTheFile(t *testing.T) {
	right, err := ccommon.SinglePassConditionals(emitted)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("before/tables-c/u/Same.h", "int same;\n")
	write("after/tables-c/u/Same.h", "int same;\n")
	write("before/tables-c/u/Right.h", emitted)
	write("after/tables-c/u/Right.h", right)
	write("before/bench-cpp/Wrong.h", emitted)
	write("after/bench-cpp/Wrong.h", strings.Replace(right, "#define ONCE\n#if", "#if", 1))
	write("before/bench-cpp/Gone.h", "int gone;\n")
	write("after/bench-cpp/New.h", "int added;\n")
	problems, files, rewritten, err := checkTrees(filepath.Join(root, "before"), filepath.Join(root, "after"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(problems, "\n")
	for _, want := range []string{
		"bench-cpp/Gone.h: written as emitted, missing rewritten",
		"bench-cpp/New.h: written rewritten, missing as emitted",
		"bench-cpp/Wrong.h: config 0",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("problems do not name %q:\n%s", want, got)
		}
	}
	if len(problems) != 3 || files != 3 || rewritten != 2 {
		t.Errorf("got %d problems, %d files, %d rewritten; want 3, 3, 2:\n%s", len(problems), files, rewritten, got)
	}
	write("after/tables/X.h", "int x;\n")
	if _, _, _, err := checkTrees(filepath.Join(root, "before"), filepath.Join(root, "after")); err == nil || !strings.Contains(err.Error(), "ends in -c or -cpp") {
		t.Fatalf("got %v, want the unnamed tree refused", err)
	}
}
