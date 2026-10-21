package ci

// TestTablesCppMemaccessDestinationsAreVoidCast holds the C++ test sources to
// the one spelling gcc will accept for a byte-wise memory operation whose
// destination is a generated struct.
//
// gcc 13's -Wclass-memaccess (under -Wall, which TABLES_CXXFLAGS promotes to an
// error with -Werror) refuses a std::memset / std::memcpy / std::memmove whose
// destination is a non-trivial class: the generated fixed-form roots and
// reports are exactly such types, and `make tables-fixedform` at the bench's
// default compiler failed with 45 of these. clang does not raise the warning,
// so the C++ fixed-form tests compiled for one compiler and not the other, and
// every card in the stream that ran the target without naming CXX went red.
//
// The fix is in the source, never in the flags: each such destination is cast
// to (void *), the form schema#1889 (landed f055b3f2) uses in refuses(). The
// rule is SYNTACTIC on purpose — every memset/memcpy/memmove destination in
// test/tables/*.cpp starts with (void *) — so a byte buffer is cast too even
// though this compiler would not warn about it: the next destination that
// grows a constructor is then already spelled right. This gate reads text; it
// runs no compiler, no clock and no socket.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// memaccessCallRe finds a memset/memcpy/memmove invocation, with or without the
// std:: qualifier, at a token boundary so `FooMemset(` is not a match.
var memaccessCallRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])(?:std::)?(?:memset|memcpy|memmove)\s*\(`)

// memaccessOffenders returns the 1-based line numbers in src at which a
// memset/memcpy/memmove destination is not spelled `(void *) ...`.
//
// Each call is matched on a single line (every call in test/tables/*.cpp is one
// line); the first argument runs from the opening parenthesis to the first
// top-level comma, tracking nested parentheses so `sizeof( v )` and a cast's
// own parenthesis do not end it early. A `//` comment is cut first so a call
// quoted in prose is not an offender. It is a pure function over a string, so
// the test can drive it on in-memory controls without touching a file.
func memaccessOffenders(src string) []int {
	var offenders []int
	for i, raw := range strings.Split(src, "\n") {
		line := raw
		if cut := strings.Index(line, "//"); cut >= 0 {
			line = line[:cut]
		}
		for _, loc := range memaccessCallRe.FindAllStringIndex(line, -1) {
			// The match starts at the boundary character (or ^), so the
			// opening parenthesis is the last byte of the match.
			open := loc[1] - 1
			depth := 0
			var arg strings.Builder
			for j := open + 1; j < len(line); j++ {
				c := line[j]
				if c == '(' {
					depth++
				} else if c == ')' {
					if depth == 0 {
						break
					}
					depth--
				} else if c == ',' && depth == 0 {
					break
				}
				arg.WriteByte(c)
			}
			if !strings.HasPrefix(strings.TrimSpace(arg.String()), "(void *)") {
				offenders = append(offenders, i+1)
			}
		}
	}
	return offenders
}

func TestTablesCppMemaccessDestinationsAreVoidCast(t *testing.T) {
	t.Parallel()

	// THE IN-MEMORY NEGATIVE CONTROL. The checker is a function over a string,
	// so a wrong checker is caught here before it ever reads a file: an uncast
	// call is flagged and the same call cast to (void *) is not.
	const uncast = "    std::memset( &v, 0, sizeof( v ) );"
	const cast = "    std::memset( (void *) &v, 0, sizeof( v ) );"
	if got := memaccessOffenders(uncast); len(got) != 1 || got[0] != 1 {
		t.Fatalf("negative control: the checker must flag the uncast %q at line 1, got %v", uncast, got)
	}
	if got := memaccessOffenders(cast); len(got) != 0 {
		t.Fatalf("negative control: the checker must accept the cast %q, got %v", cast, got)
	}

	root := repoRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "test", "tables", "*.cpp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no .cpp under test/tables — this gate would pass over an empty set")
	}
	sort.Strings(files)

	scanned := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			rel = file
		}
		for _, ln := range memaccessOffenders(string(data)) {
			t.Errorf("%s:%d: the destination of a memset/memcpy/memmove is not cast to (void *). "+
				"gcc's -Werror=class-memaccess refuses such a call on a non-trivial class (the generated "+
				"fixed-form roots and reports are exactly that), and clang is silent, so the test compiles "+
				"under one compiler and not the other. Cast the destination — std::memset( (void *) &v, ... ) — "+
				"the form schema#1889 uses, and never add a warning flag.",
				rel, ln)
		}
		scanned++
	}
	if scanned == 0 {
		t.Fatal("scanned no .cpp files — this gate would pass over an empty set")
	}
	t.Logf("scanned %d C++ test sources under test/tables", scanned)
}
