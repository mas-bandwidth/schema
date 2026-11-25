package main

import (
	"fmt"
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
		cov, err := Compare(emitted, after, lang)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		// two chains: the inline spelling (3 branches) and the #error (2)
		if cov != (Coverage{Chains: 2, Branches: 5}) {
			t.Fatalf("%s: coverage %+v, want 2 chains, 5 branches", lang, cov)
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
			_, err := Compare(emitted, wrong, "c")
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
	bad := "#if defined( A\nint a;\n#else\nint b;\n#endif\n"
	if _, err := Compare(bad, bad, "c"); err == nil || !strings.Contains(err.Error(), "the form as emitted") {
		t.Fatalf("got %v, want a preprocessor error on the form as emitted", err)
	}
}

// A branch no configuration keeps is never compared, so it is red by line;
// a chain whose own context no configuration keeps is counted, not red.
func TestSinglePassCheckNamesABranchNoConfigurationKeeps(t *testing.T) {
	in := "#if defined( __schema_never__ )\nint a;\n#else\nint b;\n#endif\n#ifndef G\n#define G\n#endif\n#ifndef G\n#if defined( A )\nint c;\n#else\nint d;\n#endif\n#endif\n"
	after, err := ccommon.SinglePassConditionals(in)
	if err != nil {
		t.Fatal(err)
	}
	cov, err := Compare(in, after, "c")
	if err == nil || !strings.Contains(err.Error(), "line 1: #if defined( __schema_never__ )") || strings.Contains(err.Error(), "line 3:") {
		t.Fatalf("got %v, want line 1's branch named as never compared and nothing else", err)
	}
	if cov != (Coverage{Chains: 2, Branches: 2, Unreached: 1}) {
		t.Fatalf("coverage %+v, want 2 chains, 2 branches reached, 1 chain unreached", cov)
	}
}

// A name another header or the build defines is defined in configuration 4,
// so both sides of a chain on it are compared; a name the file defines, or
// the compiler's, is left to the configuration.
func TestSinglePassCheckDefinesTheProjectNames(t *testing.T) {
	in := "#ifndef GUARD\n#define GUARD\n#ifdef OTHER_RUNTIME\nint shared;\n#else\nint own;\n#endif\n#if defined( GUARD ) && defined( __GNUC__ )\nint g;\n#else\nint n;\n#endif\n#endif\n"
	after, err := ccommon.SinglePassConditionals(in)
	if err != nil {
		t.Fatal(err)
	}
	marked, chains := markChains(in)
	if got := ProjectNames(in, chains); strings.Join(got, ",") != "OTHER_RUNTIME" {
		t.Fatalf("project names %q, want OTHER_RUNTIME alone", got)
	}
	if !strings.Contains(marked, markerPrefix+"0_1\n") {
		t.Fatalf("the #else is not marked:\n%s", marked)
	}
	if _, err := Compare(in, after, "c"); err != nil {
		t.Fatal(err)
	}
}

// Over trees: a file that differs, or is on one side only, is named; a tree
// the rewrite changed nothing in is red; the trees must be exactly the ones
// named, each named for its language.
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
	before, after := filepath.Join(root, "before"), filepath.Join(root, "after")
	write("before/tables-c/u/Same.h", "int same;\n")
	write("after/tables-c/u/Same.h", "int same;\n")
	write("before/tables-c/u/Right.h", emitted)
	write("after/tables-c/u/Right.h", right)
	write("before/bench-cpp/Wrong.h", emitted)
	write("after/bench-cpp/Wrong.h", strings.Replace(right, "#define ONCE\n#if", "#if", 1))
	write("before/bench-cpp/Gone.h", "int gone;\n")
	write("after/bench-cpp/New.h", "int added;\n")
	write("before/still-c/Same.h", "int same;\n")
	write("after/still-c/Same.h", "int same;\n")
	res, err := checkTrees(before, after, []string{"tables-c", "bench-cpp", "still-c"})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(res.problems, "\n")
	for _, want := range []string{
		"bench-cpp/Gone.h: written as emitted, missing rewritten",
		"bench-cpp/New.h: written rewritten, missing as emitted",
		"bench-cpp/Wrong.h: config 0",
		"still-c: the rewrite changed no file of this tree",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("problems do not name %q:\n%s", want, got)
		}
	}
	if len(res.problems) != 4 || res.files != 4 || res.rewritten != 2 {
		t.Errorf("got %d problems, %d files, %d rewritten; want 4, 4, 2:\n%s", len(res.problems), res.files, res.rewritten, got)
	}
	for _, tc := range []struct {
		trees []string
		want  string
	}{
		{[]string{"tables-c", "bench-cpp"}, "still-c is not one of the trees named"},
		{[]string{"tables-c", "bench-cpp", "still-c", "gone-c"}, "the tree gone-c is missing"},
		{[]string{"tables-c", "bench-cpp", "still"}, "ends in -c or -cpp"},
		{[]string{"tables-c", "tables-c"}, "each tree is named once"},
	} {
		if _, err := checkTrees(before, after, tc.trees); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("-trees %v: got %v, want a refusal naming %q", tc.trees, err, tc.want)
		}
	}
}

// A chain no configuration reaches is red unless it sits in a guarded twin:
// clang-cl (_WIN32 and __clang__ together) is no configuration, so a chain
// only it reaches is never compared, and that is named, not counted. A twin
// in another branch of its parent, or after an #undef of its guard, is not
// proven dead.
func TestSinglePassCheckNamesAChainNoConfigurationReaches(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"a chain under a combination no configuration takes",
			"#if defined( _WIN32 ) && defined( __clang__ )\n#if defined( A )\nint a;\n#else\nint b;\n#endif\n#endif\n",
			"line 2: #if defined( A ): a chain no configuration reaches"},
		{"a twin after an #undef of its guard",
			"#if defined( _WIN32 ) && defined( __clang__ )\n#ifndef G\n#define G\n#endif\n#undef G\n#ifndef G\n#if defined( A )\nint a;\n#else\nint b;\n#endif\n#endif\n#endif\n",
			"line 7: #if defined( A ): a chain no configuration reaches"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after, err := ccommon.SinglePassConditionals(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Compare(tc.in, after, "c")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
	_, chains := markChains("#if defined( Q )\n#ifndef G\n#define G\n#endif\n#else\n#ifndef G\n#if defined( A )\nint a;\n#else\nint b;\n#endif\n#endif\n#endif\n")
	for _, c := range chains {
		if c.dead {
			t.Fatalf("the chain at line %d is called dead, but its twin's guard was defined in another branch", c.line)
		}
	}
}

// A configuration is run again only where it differs from an earlier one on
// a name the file's directives name; one that differs only elsewhere keeps
// the same text and is not run.
func TestSinglePassCheckRunsEachDistinctConfigurationOnce(t *testing.T) {
	configs := [][]string{
		{},
		{"-U__x86_64__"},
		{"-D_MSC_VER=1930", "-U__x86_64__"},
		{"-D_MSC_VER=1930"},
		{"-D__cplusplus=201703L"},
		{"-DB=1"},
	}
	src := "#if defined( _MSC_VER )\nint a;\n#endif\n#if defined( __cplusplus )\nint c;\n#endif\n#define X B\n"
	if got, want := fmt.Sprint(sameAs(src, "c", configs)), "[-1 0 -1 2 -1 -1]"; got != want {
		t.Errorf("c: got %s, want %s", got, want)
	}
	if got, want := fmt.Sprint(sameAs(src, "c++", configs)), "[-1 0 -1 2 0 -1]"; got != want {
		t.Errorf("c++: got %s, want %s", got, want)
	}
}
