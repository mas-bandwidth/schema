package c_test

// THE NAME CORPUS, C LEG: every case of testdata/name-buildability-emit that
// cases.tsv names for c generates and compiles, every header included, against
// the serialize.c sibling, under -Wall -Wextra -Werror. The names are the ones
// the emitter once rewrote inside its own code (a type named uint8_t, memcpy or
// NULL) and the file bases its include guard could not spell (my-file).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/nameemit"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

func TestNameCorpusBuildsC(t *testing.T) {
	slowtest.Gate(t, "the C compiler and the serialize.c sibling")
	serialize := os.Getenv("SERIALIZE_C")
	if serialize == "" {
		serialize = filepath.Join("..", "..", "..", "..", "serialize.c")
	}
	serialize, err := filepath.Abs(serialize)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(serialize, "serialize.h")); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("serialize.h is not staged at %s (SERIALIZE_C, else the sibling checkout); SCHEMA_REQUIRE_CORPUS says the corpus must build", serialize)
		}
		t.Skipf("serialize.h is not staged at %s (SERIALIZE_C, else the sibling checkout)", serialize)
	}
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	for _, c := range nameemit.For(t, "c") {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			files := nameemit.Generate(t, c, "c")
			dir := t.TempDir()
			headers := nameemit.Write(t, files, dir, ".h")
			sources := nameemit.Write(t, files, dir, ".c")
			var main strings.Builder
			for _, h := range headers {
				fmt.Fprintf(&main, "#include \"%s\"\n", h)
			}
			main.WriteString("int main(void) { return 0; }\n")
			if err := os.WriteFile(filepath.Join(dir, "zz_main.c"), []byte(main.String()), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-I" + dir, "-I" + serialize,
				filepath.Join(dir, "zz_main.c")}
			for _, s := range sources {
				args = append(args, filepath.Join(dir, s))
			}
			args = append(args, filepath.Join(serialize, "serialize.c"), "-lm", "-o", filepath.Join(dir, "zz_main"))
			cmd := testproc.Command(t, cc, args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s (%s): the generated C does not build: %v\n%s", c.Name, c.Note, err, out)
			}
		})
	}
}
