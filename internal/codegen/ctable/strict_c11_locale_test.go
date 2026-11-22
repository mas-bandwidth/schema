// The generated <Name>Table.c converts numbers in the "C" locale with
// newlocale and uselocale, which a strict -std=c11 declares only under
// _POSIX_C_SOURCE 200809L. The generated file sets that macro itself, but a
// translation unit that includes the file after its own system headers sees
// the macro too late (main went red on tables/cards/c/retain_bridge.c).
// This test compiles generated output under the strict flags the cards
// driver uses, both ways a unit can include it, and requires the late case
// to refuse with the named #error rather than an unknown type name.
package ctable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
)

func TestCGeneratedTableCompilesUnderStrictC11(t *testing.T) {
	t.Parallel()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Fatalf("the strict C11 compile check needs cc: %v", err)
	}
	u := unitFrom(t, "package probe\n\ntable Probe\n{\n    ratio float64\n    name  string(8)\n}\n")
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatalf("cgen: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	for _, set := range []map[string][]byte{files, tables} {
		for name, data := range set {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	compile := func(name, src string) (string, error) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(cc, "-std=c11", "-Wall", "-Wextra", "-Werror", "-fsyntax-only", "-I", dir, path).CombinedOutput()
		return string(out), err
	}
	// The generated file alone, then as the first thing a unit that sets the
	// macro itself includes after a system header (the retain bridge's shape).
	if out, err := compile("alone.c", "#include \"ProbeTable.c\"\n"); err != nil {
		t.Fatalf("generated file alone under -std=c11 -Werror: %v\n%s", err, out)
	}
	if out, err := compile("bridge.c", "#define _POSIX_C_SOURCE 200809L\n#include <stdlib.h>\n#include \"ProbeTable.c\"\n"); err != nil {
		t.Fatalf("unit with the macro first under -std=c11 -Werror: %v\n%s", err, out)
	}
	// A unit that includes system headers first and never sets the macro must
	// be refused by name (glibc only: other libcs declare locale_t anyway).
	out, err := compile("late.c", "#include <stdlib.h>\n#include \"ProbeTable.c\"\n")
	if _, glibc := compile("glibc.c", "#include <stdlib.h>\n#ifndef __GLIBC__\n#error not glibc\n#endif\n"); glibc != nil {
		if err != nil {
			t.Fatalf("late include failed on a libc that declares locale_t:\n%s", out)
		}
		return
	}
	if err == nil {
		t.Fatal("late include compiled without the macro on glibc")
	}
	if !strings.Contains(out, "locale_t needs POSIX.1-2008") {
		t.Fatalf("late include failed without the named refusal:\n%s", out)
	}
}
