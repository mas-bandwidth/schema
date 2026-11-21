// The committed `was` rename corpus (ir/testdata/was-rename): one schema file
// per step of two renames, the bool field a branch names and the default enum
// variant, each beside its unrenamed base and its bare-rename control, with the
// protocol id every file must hold (docs/SPEC-TABLES.md §5). The baseline's
// half of the same files is internal/baseline/wasrename_corpus_test.go.
package ir_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wasRenameCase is one row of ir/testdata/was-rename/cases.tsv.
type wasRenameCase struct {
	group, file, relation, id, retired string
}

func wasRenameCases(t *testing.T) []wasRenameCase {
	t.Helper()
	data, err := os.ReadFile("testdata/was-rename/cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	var out []wasRenameCase
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("cases.tsv line %d: %d fields, want 5", n+1, len(f))
		}
		out = append(out, wasRenameCase{f[0], f[1], f[2], f[3], f[4]})
	}
	return out
}

// TestWasRenameCorpus holds every committed file to its committed protocol id,
// and each id to its relation with its group's base: a rename under `was`
// keeps the base's id, a bare rename or a moved default does not. A `retired`
// id is one an earlier compiler emitted for that file and no compiler may emit
// again.
func TestWasRenameCorpus(t *testing.T) {
	t.Parallel()
	cases := wasRenameCases(t)
	base := map[string]string{}
	relations := map[string]int{}
	for _, c := range cases {
		if c.relation == "base" {
			base[c.group] = c.id
		}
		relations[c.group+"/"+c.relation]++
	}
	for _, group := range []string{"guard", "variant"} {
		for _, relation := range []string{"base", "same", "moves"} {
			if relations[group+"/"+relation] == 0 {
				t.Errorf("group %s has no %s row: a rename is only proven beside its base and its bare-rename control", group, relation)
			}
		}
	}
	for _, c := range cases {
		t.Run(c.group+"/"+c.file, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("testdata/was-rename", c.group, c.file))
			if err != nil {
				t.Fatal(err)
			}
			got := fmt.Sprintf("0x%016x", unitFrom(t, string(src)).ProtocolId)
			if got != c.id {
				t.Errorf("protocol id %s, the corpus commits %s", got, c.id)
			}
			switch c.relation {
			case "base":
			case "same":
				if got != base[c.group] {
					t.Errorf("a rename under was moved the protocol id: base %s, this file %s", base[c.group], got)
				}
			case "moves":
				if got == base[c.group] {
					t.Errorf("the control did not move the protocol id off the base's %s", got)
				}
			default:
				t.Fatalf("unknown relation %q", c.relation)
			}
			if c.retired != "-" && got == c.retired {
				t.Errorf("protocol id %s is the retired one", got)
			}
			if c.retired != "-" && c.relation != "same" {
				t.Errorf("only a rename under was has a retired id")
			}
		})
	}
}
