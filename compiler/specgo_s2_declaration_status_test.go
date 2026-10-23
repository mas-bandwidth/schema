// The Go declaration-status rows of docs/SPEC-TABLES.md, held to the page:
// each row the card folds in is stated where the section lives, and the stale
// sentence it replaces is gone. The pages are read as text, as docs_test.go
// reads its examples.
package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestSpecGoS2DeclarationStatus(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)

	rows := []struct {
		name  string
		holds []string
		gone  []string
	}{
		{
			name:  "WIRE FORM STATUS names Go as a form-3 carrier",
			holds: []string{"3.4's FIXED form is carried by the C++ reference, by Go", "FixedLoadTolerant"},
			gone:  []string{"Its file and announced message codecs use §3's id-table wire form."},
		},
		{
			name:  "§2.5 Go carries both blob kinds",
			holds: []string{"Go carries `*bytes` and `*string`"},
			gone:  []string{"the other backends refuse a unit that declares one"},
		},
		{
			name:  "§2.9 every registered list target carries []T",
			holds: []string{"Go, C, C#, Rust, Dart, Java, JavaScript and Elixir"},
			gone:  []string{"the tool's COOK and UNCOOK halves do not lay out the element arrays yet"},
		},
		{
			name:  "§2.9 TableList.At returns nil out of range",
			holds: []string{"`TableList.At(i)` returns nil for `i < 0` or `i >= Count`"},
			gone:  []string{"Rust panics, Go panics"},
		},
		{
			name:  "§2.8 and §2.9 the arena alignment floors at 8 in Go",
			holds: []string{"`ir.TableRegionAlign`, the closure's maximum alignment floored at 8"},
		},
		{
			name: "§2.13 the three text-list refusals",
			holds: []string{
				"a specified default: a fresh list is empty",
				"`[Min..N]string(M)`",
				"any valued attribute other than `was`, `json` and `total`",
			},
		},
		{
			name:  "§2.13.1 the message form does not apply",
			holds: []string{"THE MESSAGE FORM DOES NOT APPLY"},
			gone:  []string{"EMITS NOTHING for a table whose closure holds"},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			for _, want := range row.holds {
				if !strings.Contains(page, want) {
					t.Errorf("SPEC-TABLES.md does not hold %q", want)
				}
			}
			for _, old := range row.gone {
				if strings.Contains(page, old) {
					t.Errorf("SPEC-TABLES.md still holds the stale %q", old)
				}
			}
		})
	}
}
