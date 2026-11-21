package cpp_test

// THE NAME CORPUS, C++ LEG: every case of testdata/name-buildability-emit that
// cases.tsv names for cpp generates and compiles, every header included and
// every generated .cpp beside them, against the serialize sibling, as C++17
// under -Wall -Wextra -Werror. The names are a libc type, function or macro
// declared as a schema type (uint8_t, memcpy, NULL, INT32_MAX), which the
// type's own namespace once hid from the emitter's unqualified uses.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cqualify"
	"github.com/mas-bandwidth/schema/v2/internal/nameemit"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

func TestNameCorpusBuildsCpp(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler and the serialize sibling")
	serialize := os.Getenv("SERIALIZE")
	if serialize == "" {
		serialize = filepath.Join("..", "..", "..", "..", "serialize")
	}
	serialize, err := filepath.Abs(serialize)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(serialize, "serialize.h")); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("serialize.h is not staged at %s (SERIALIZE, else the sibling checkout); SCHEMA_REQUIRE_CORPUS says the corpus must build", serialize)
		}
		t.Skipf("serialize.h is not staged at %s (SERIALIZE, else the sibling checkout)", serialize)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	header, err := os.ReadFile(filepath.Join(serialize, "serialize.h"))
	if err != nil {
		t.Fatal(err)
	}
	macros := macroBodies(string(header))
	for _, c := range nameemit.For(t, "cpp") {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			files := nameemit.Generate(t, c, "cpp")
			// the packet codecs expand the runtime's macros inside the unit's
			// namespace, so a runtime whose macros spell the case's name bare
			// captures the user's type whatever the emitter writes
			// (a macro's name is the platform's in any case: the user's type is
			// spelled past it)
			if name, ok := strings.CutPrefix(c.Name, "type_"); ok && !cqualify.CppMacro(name) && bareIn(macros, name) {
				t.Skipf("%s: the serialize.h at %s spells %s bare inside its macros; the case builds once the pinned runtime spells it ::%s (mas-bandwidth/serialize#188)", c.Name, serialize, name, name)
			}
			dir := t.TempDir()
			headers := nameemit.Write(t, files, dir, ".h")
			sources := nameemit.Write(t, files, dir, ".cpp")
			var main strings.Builder
			for _, h := range headers {
				fmt.Fprintf(&main, "#include \"%s\"\n", h)
			}
			main.WriteString("int main() { return 0; }\n")
			if err := os.WriteFile(filepath.Join(dir, "zz_main.cpp"), []byte(main.String()), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"-std=c++17", "-Wall", "-Wextra", "-Werror", "-ffp-contract=off",
				"-I" + dir, "-I" + serialize, filepath.Join(dir, "zz_main.cpp")}
			for _, s := range sources {
				args = append(args, filepath.Join(dir, s))
			}
			args = append(args, "-o", filepath.Join(dir, "zz_main"))
			cmd := testproc.Command(t, cxx, args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s (%s): the generated C++ does not build: %v\n%s", c.Name, c.Note, err, out)
			}
		})
	}
}

// macroBodies is every line of src inside a #define, continuation lines
// included, with // comments cut.
func macroBodies(src string) []string {
	var out []string
	in := false
	for line := range strings.SplitSeq(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !in && !strings.HasPrefix(strings.ReplaceAll(trimmed, " ", ""), "#define") {
			continue
		}
		code, _, _ := strings.Cut(line, "//")
		out = append(out, code)
		in = strings.HasSuffix(strings.TrimRight(line, " \t"), "\\")
	}
	return out
}

// bareIn reports whether name appears in a line as a whole identifier with no
// :: . or -> before it.
func bareIn(lines []string, name string) bool {
	re := regexp.MustCompile(`(^|[^A-Za-z0-9_:.>])` + regexp.QuoteMeta(name) + `($|[^A-Za-z0-9_])`)
	return slices.ContainsFunc(lines, re.MatchString)
}
