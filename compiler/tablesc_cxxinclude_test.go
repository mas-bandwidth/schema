package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestCTablePointerHeaderIncludesFromCxx holds the promise of a generated C
// table header's extern "C" block: a header whose unit declares node types
// (pointers, blobs, lists) compiles when a C++ translation unit includes it,
// and the same header still compiles as C.
func TestCTablePointerHeaderIncludesFromCxx(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C and C++ compilers, once per generated table header")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("generated C compilation requires cc")
	}
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("generated C++ inclusion requires c++")
	}
	for _, path := range []string{"../tables/pointers", "../tables/blobs"} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			paths, err := filepath.Glob(filepath.Join(path, "*.schema"))
			if err != nil {
				t.Fatal(err)
			}
			u, err := New().Load(paths)
			if err != nil {
				t.Fatal(err)
			}
			files, err := generateCUnqualified(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			var headers []string
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
				if strings.HasSuffix(name, "Table.h") {
					headers = append(headers, name)
				}
			}
			if len(headers) == 0 {
				t.Fatal("the unit generated no table header")
			}
			sort.Strings(headers)
			for _, header := range headers {
				stem := strings.TrimSuffix(header, ".h")
				include := []byte("#include \"" + header + "\"\n")
				cpp := filepath.Join(dir, stem+"_include.cpp")
				c := filepath.Join(dir, stem+"_include.c")
				if err := os.WriteFile(cpp, include, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(c, include, 0600); err != nil {
					t.Fatal(err)
				}
				// The C++ leg holds the compile the extern "C" block promises;
				// the C leg below holds the header warning-clean.
				if output, err := exec.Command(cxx, "-std=c++17", "-fsyntax-only", "-I", dir, cpp).CombinedOutput(); err != nil {
					t.Errorf("%s included from C++: %v\n%s", header, err, output)
				}
				if output, err := exec.Command(cc, "-std=c99", "-fsyntax-only", "-Wall", "-Wextra", "-Werror", "-I", dir, c).CombinedOutput(); err != nil {
					t.Errorf("%s included from C: %v\n%s", header, err, output)
				}
			}
		})
	}
}
