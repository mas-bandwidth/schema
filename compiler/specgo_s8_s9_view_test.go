// The Go view's spelling is the standard's spelling (docs/SPEC-TABLES.md §8.1,
// §8.2, §8.3, §8.5). Each row below is one thing Go already does that the
// page now states: a row's `want` is the identifier or sentence the page must
// carry, and a row's `stale` is the sentence the page must no longer carry.
package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestSpecGoS8S9View(t *testing.T) {
	t.Parallel()

	page, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(page)

	rows := []struct {
		name  string
		want  []string
		stale []string
	}{
		{
			name:  "8.1 and 8.3 port status: Go emits the registry and runs the corpus gate",
			want:  []string{"TestGoUnitViewCorpus", "all six sets"},
			stale: []string{"the eight ports do not yet"},
		},
		{
			name:  "8.5 Go's view file is <Package>View.go",
			want:  []string{"<Package>View.go"},
			stale: []string{"`<Package>View.cs` in C#."},
		},
		{
			name:  "8.1 pointer columns are on every Go descriptor",
			want:  []string{"declares `Pointer` and `TargetId`", "declares `Variable`"},
			stale: []string{"none carries not one of them"},
		},
		{
			name:  "8.1 Go has no place column",
			want:  []string{"mapPlace", "listAdd"},
			stale: []string{"ONE FUNCTION COLUMN SERVES BOTH, and it is `place`"},
		},
		{
			name:  "8.1 and 8.2 zero is the id the wire reserves for None",
			want:  []string{"reserves for `None`"},
			stale: []string{"value no declared name folds to"},
		},
		{
			name:  "8.1 and 8.3 Go's extra columns",
			want:  []string{"PayloadOffset", "DeclaredTypeName", "TextList", "every non-void arm"},
			stale: []string{"The two descriptors are the arm's"},
		},
		{
			name:  "8.1 and 8.3 Go's descriptors are filled in init()",
			want:  []string{"filled in `init()`", "immutable by convention", "variable root's `<Name>Save`", "fixed root's typed `SaveBody`"},
			stale: []string{"CONSTANT-INITIALISED"},
		},
	}

	for _, row := range rows {
		for _, want := range row.want {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the page does not carry %q", row.name, want)
			}
		}
		for _, stale := range row.stale {
			if strings.Contains(text, stale) {
				t.Errorf("%s: the page still carries the stale sentence %q", row.name, stale)
			}
		}
	}
}
