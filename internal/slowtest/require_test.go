package slowtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProgramFailsNamingAMissingProgram: a program found through its
// variable or on PATH is returned; one found nowhere fails the test, naming
// the program and the variable. No program is started: PATH is a scratch
// directory holding an empty file with the program's name, or nothing.
func TestProgramFailsNamingAMissingProgram(t *testing.T) {
	for _, c := range []struct{ name, found string }{
		{"path", "path"}, {"env", "env"}, {"stale-env", "stale-env"}, {"none", "none"},
	} {
		t.Run(c.name, func(t *testing.T) {
			onPath, elsewhere := t.TempDir(), t.TempDir()
			fake := func(dir string) string {
				p := filepath.Join(dir, "tsc")
				if err := os.WriteFile(p, nil, 0o755); err != nil { // an executable stand-in, never run
					t.Fatal(err)
				}
				return p
			}
			t.Setenv("PATH", onPath)
			t.Setenv("TSC", "")
			want := ""
			switch c.found {
			case "path":
				want = fake(onPath)
			case "env":
				want = fake(elsewhere)
				t.Setenv("TSC", want)
			case "stale-env":
				want = fake(onPath)
				t.Setenv("TSC", filepath.Join(elsewhere, "absent", "tsc"))
			}
			var got string
			v := verdictOf(func(tb testing.TB) { got = Program(tb, "tsc", "TSC") })
			if want == "" {
				if !strings.Contains(v.failed, "no tsc on PATH: set TSC") {
					t.Errorf("tsc found nowhere: failed %q; want a failure naming tsc and TSC", v.failed)
				}
				return
			}
			if got != want || v.failed != "" {
				t.Errorf("want %q; got %q (failed %q)", want, got, v.failed)
			}
		})
	}
}
