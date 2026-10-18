// THE STANDARD SAYS WHAT GO WRITES (docs/SPEC-TABLES.md §2.10, §21.1, §21.4).
//
// The language ports are unheld and read the spec, so every rule Go already
// keeps has to be the rule the page states. This pin reads the sections the
// card edits and holds each row to the Go spelling it folded in, and holds the
// stale sentence it replaced out: internal/lockfile/lockfile.go:110 (Version =
// 7), :1002-1014, :1073, :1124; cmd/schema/main.go:202-203;
// internal/lockfile/monotone.go:205, :340, :363; internal/lockfile/diff.go:238;
// compiler/compiler.go:249; internal/check/check_fixed.go:47; ir.FixedClosureBreaks.
package compiler

import (
	"os"
	"strings"
	"testing"
)

// specSection returns the page from heading up to (not including) the next
// heading, so a literal is found in the section the row names and not
// somewhere else on a very long page.
func specSection(t *testing.T, page, heading, next string) string {
	t.Helper()
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	from := strings.Index(text, heading)
	if from < 0 {
		t.Fatalf("%s has no heading %q", page, heading)
	}
	to := strings.Index(text[from:], next)
	if to < 0 {
		t.Fatalf("%s has no heading %q after %q", page, next, heading)
	}
	return text[from : from+to]
}

func TestSpecGoS210S21LockV7Lineage(t *testing.T) {
	t.Parallel()
	const page = "../docs/SPEC-TABLES.md"
	lock := specSection(t, page, "### 2.10 ", "### 2.11 ")
	law := specSection(t, page, "### 21.1 ", "### 21.2 ")
	closure := specSection(t, page, "### 21.4 ", "### 21.5 ")

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			// §2.10: rendering version 7, lineage lines, bits=/ibits=,
			// v6 salvage, a newer compiler's own refusal, --retire.
			row:  "§2.10 lock v7 and lineage",
			text: lock,
			wants: []string{
				"schema-lock 7",
				"lineage wire=",
				"bits=",
				"ibits=",
				"SALVAGED",
				"--retire",
				"NEWER compiler",
				"may then be dropped",
			},
			stales: []string{
				"schema-lock 5",
				"delete it and write it again",
			},
		},
		{
			// §21.1 opening: the monotone check in schema.lock, run by
			// schema check, is the instrument — not the baseline.
			row:    "§21.1 opening",
			text:   law,
			wants:  []string{"monotone check"},
			stales: []string{"The baseline (§18) refuses any commit that narrows"},
		},
		{
			// §21.1 union row: an arm renamed without was is refused; one
			// renamed under was passes.
			row:    "§21.1 union row",
			text:   law,
			wants:  []string{"an arm renamed under `was`"},
			stales: []string{"an arm removed, reordered, or its payload changed"},
		},
		{
			// §21.1 compressed float: a removed range widens, as it does for
			// any ranged scalar (monotone.go:205).
			row:    "§21.1 compressed-float row",
			text:   law,
			wants:  []string{"moved OUTWARD or REMOVED"},
			stales: []string{"moved OUTWARD like any ranged scalar"},
		},
		{
			// §21.4: a type is not fixed and has no lineage of its own.
			row:    "§21.4 type carries no lineage",
			text:   closure,
			wants:  []string{"a nested record has no file of its own"},
			stales: []string{"Every table or type a fixed table reaches by value is itself declared"},
		},
		{
			// §21.4: the six constructs a fixed closure refuses
			// (ir.FixedClosureBreaks, internal/check/check_fixed.go:47).
			row:    "§21.4 closure refusals",
			text:   closure,
			wants:  []string{"a byte buffer at its used size"},
			stales: []string{"A pointer, a map, or an unbounded array in the closure is refused"},
		},
	}

	for _, r := range rows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			for _, want := range r.wants {
				if !strings.Contains(r.text, want) {
					t.Errorf("%s: the section no longer states %q", r.row, want)
				}
			}
			for _, stale := range r.stales {
				if strings.Contains(r.text, stale) {
					t.Errorf("%s: the section still states the stale sentence %q", r.row, stale)
				}
			}
		})
	}
}
