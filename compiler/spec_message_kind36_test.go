// THE STANDARD SAYS WHAT GO WRITES (docs/SPEC-TABLES.md §3.3, §2.11).
//
// A timestamp field is announced at kind `36` on the message form: its payload
// is the `int64`'s sixty-four raw bits, which is the nanosecond count §2.11
// stores, and its shape carries no bytes because the width is the kind's own.
// This pin holds the §3.3 announcement shape table to that row and keeps the
// §2.11 sentence that folded the message form in with the untested cook and
// block forms out.
package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestSpecMessageFormHasKind36Row(t *testing.T) {
	t.Parallel()
	const page = "../docs/SPEC-TABLES.md"
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)

	section := func(heading, next string) string {
		t.Helper()
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

	announcement := section("#### The announcement", "#### Elision")

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			row:  "§3.3 announcement shape table: kind 36",
			text: announcement,
			wants: []string{
				"`36` timestamp",
				"sixty-four bits",
				"the `int64` count §2.11 stores",
			},
			// §2.11 used to lump the message form in with the cook and block
			// forms as untested; the row now states it, so that clause is gone.
			stales: []string{"the cook, the block and the message form"},
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
				if strings.Contains(text, stale) {
					t.Errorf("%s: the page still states the stale sentence %q", r.row, stale)
				}
			}
		})
	}
}
