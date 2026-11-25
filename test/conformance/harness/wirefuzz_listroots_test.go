package main

import (
	"strings"
	"testing"
)

// TestWireFuzzListRootsIsTheRoster: --list-roots prints exactly the roots the
// profile's roster drives, one line each with its class and form, so a caller
// that runs one leg per unit (make/goose-wirefuzz.sh) learns which arms a unit
// has from the corpus. The builder roster is variable-class file roots alone.
func TestWireFuzzListRootsIsTheRoster(t *testing.T) {
	m, _, _ := corpus(t)
	for _, opts := range []wireFuzzOptions{{}, {builder: true}, {builder: true, retain: true}} {
		_, roots, _, _, err := buildWireFuzzRoster(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		if err := writeRosterRoots(&out, roots); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
		if len(roots) == 0 || len(lines) != len(roots) {
			t.Fatalf("%+v: %d roots, %d lines", opts, len(roots), len(lines))
		}
		for i, line := range lines {
			f := strings.Split(line, "\t")
			r := roots[i]
			if len(f) != 4 || f[0] != r.unit || f[1] != r.root {
				t.Fatalf("%+v: line %q is not root %s.%s", opts, line, r.unit, r.root)
			}
			if (f[2] == "variable") != r.variable || (f[3] == "message") != r.message {
				t.Fatalf("%+v: line %q has the wrong class or form", opts, line)
			}
			if opts.builder && (f[2] != "variable" || f[3] != "file") {
				t.Fatalf("the builder roster holds %q, which is not a variable-class file root", line)
			}
		}
	}
}
