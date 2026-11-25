package cpptable

import (
	"strings"
	"testing"
)

// The force-inline macro is a hook: a build that defines <PKG>_TABLE_INLINE
// before the header (as plain `inline`, for a sanitizer build that crawls
// with every body forced inline) keeps its own spelling, as C's
// SCHEMA_<PKG>_TABLE_INLINE does. Every #define of it sits behind one
// #ifndef of the macro itself.
func TestCppTableInlineMacroIsAHook(t *testing.T) {
	t.Parallel()
	files := generateCPPAll(t, "package probe\n\ntable Config { level int32 }\n")
	header := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") {
			header = string(data)
		}
	}
	if header == "" {
		t.Fatal("the cpp target emitted no Table.h")
	}
	const macro = "PROBE_TABLE_INLINE"
	guard := "#ifndef " + macro + "\n"
	if n := strings.Count(header, guard); n != 1 {
		t.Fatalf("want one %q, found %d: a build cannot set the force-inline macro itself", strings.TrimSpace(guard), n)
	}
	open := strings.Index(header, guard)
	defines := strings.Count(header, "#define "+macro+" ")
	if defines == 0 {
		t.Fatal("the header defines no " + macro)
	}
	// the guard opens before the first #define and its #endif closes after
	// the last: count #if/#endif depth from the guard
	depth := 0
	seen := 0
	for _, line := range strings.Split(header[open:], "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "#if"):
			depth++
		case strings.HasPrefix(line, "#endif"):
			depth--
		case strings.HasPrefix(line, "#define "+macro+" "):
			if depth < 1 {
				t.Fatalf("a #define of %s sits outside its #ifndef", macro)
			}
			seen++
		}
		if depth == 0 {
			break
		}
	}
	if seen != defines {
		t.Fatalf("%d of %d #defines of %s sit inside its #ifndef", seen, defines, macro)
	}
}
