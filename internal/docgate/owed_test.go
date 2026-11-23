package docgate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOwedRowsAreLinked holds docs/SPEC-CONFORMANCE.md §7 honest: every
// owed rule names the PR that carries its code and row, and an entry whose
// PR has merged, or whose ids are all in the tree, can no longer say owed.
// It reads git and the tree, never the network.
func TestOwedRowsAreLinked(t *testing.T) {
	t.Parallel()
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(root, OwedPage))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ParseOwed(string(text))
	if err != nil {
		t.Fatal(err)
	}
	// No entry is no pass: an empty table means every rule is linked, and
	// then the section and this test go, together.
	if len(entries) == 0 {
		t.Fatalf("%s §7 holds no owed entry: every rule is linked, so delete the table and this test", OwedPage)
	}
	merged, err := MergedPRs(root)
	switch {
	case errors.Is(err, ErrShallow) && os.Getenv("GITHUB_ACTIONS") == "true":
		// CI fetches the history for this check (ci-fast.yml and
		// ci-full.yml, "the history the owed-table check reads"); a CI run
		// without it is a broken job, never a pass.
		t.Fatalf("%v: in CI the job must fetch the history before this test", err)
	case errors.Is(err, ErrShallow):
		// A shallow checkout (a CI job at fetch-depth 1) has no history to
		// read. The tree half below still runs; the merged half is named
		// here as NOT RUN, never passed.
		t.Logf("NOT RUN: the merged-PR half of this check: %v. Run it in a full clone.", err)
		merged = nil
	case err != nil:
		t.Fatal(err)
	}
	var ids []string
	for _, e := range entries {
		ids = append(ids, e.IDs...)
	}
	inTree, err := TreeIDs(root, ids)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := CheckOwed(entries, func(pr int) bool { return merged[pr] }, func(id string) (bool, error) {
		v, ok := inTree[id]
		if !ok {
			return false, errors.New("TreeIDs did not answer for this id")
		}
		return v, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Error(f)
	}
	t.Logf("%d owed entries checked", len(entries))
}

// TestOwedCheckGoesRed plants each failure the check exists to catch and
// asserts it is caught, so the gate above cannot pass by checking nothing.
func TestOwedCheckGoesRed(t *testing.T) {
	t.Parallel()
	page := func(rows ...string) string {
		return "# x\n\n" + OwedHeading + "\n\nprose\n\n" + OwedHeader + "\n|---|---|---|---|---|---|\n" + strings.Join(rows, "\n") + "\n\n## 8. next\n"
	}
	clean := "| FL-01 | a rule (SPEC §1) | `row_a` | `row_a` | #4757 | owed |"
	merged := func(pr int) bool { return pr == 4740 }
	present := func(id string) (bool, error) { return id == "row_landed" || id == "row_landed_too", nil }
	cases := []struct {
		name, row, want string
	}{
		{"clean", clean, ""},
		{"no PR named", "| SF-01 | a rule | `row_b` | `row_b` | none yet | owed |", "names no PR"},
		{"PR merged, still owed", "| SF-02 | a rule | `row_c` | `row_c` | #4740 | owed |", "#4740 has merged"},
		{"one of two PRs merged", "| SF-03 | a rule | `row_d` | `row_d` | #4740, #4757 | owed |", "#4740 has merged"},
		{"every id in the tree", "| FL-02 | a rule | `row_landed` | `row_landed`, `row_landed_too` | #4757 | owed |", "every id the entry owes is in the tree"},
		{"a state that is not owed", "| FL-03 | a rule | `row_e` | `row_e` | #4757 | linked |", "state \"linked\""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			entries, err := ParseOwed(page(c.row))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("parsed %d entries, want 1", len(entries))
			}
			got, err := CheckOwed(entries, merged, present)
			if err != nil {
				t.Fatal(err)
			}
			if c.want == "" {
				if len(got) != 0 {
					t.Fatalf("a clean entry drew %q", got)
				}
				return
			}
			if len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), c.want) {
				t.Fatalf("planted %q: findings %q, want one containing %q", c.name, got, c.want)
			}
		})
	}
	// A table the parser cannot read is an error, never an empty pass.
	for name, bad := range map[string]string{
		"no heading":     "# x\n\n" + OwedHeader + "\n|---|\n" + clean + "\n",
		"header changed": "# x\n\n" + OwedHeading + "\n\n| entry | rule | PR | state |\n|---|---|---|---|\n",
		"short row":      page("| FL-04 | a rule | #4757 | owed |"),
	} {
		if _, err := ParseOwed(bad); err == nil {
			t.Errorf("%s: ParseOwed accepted a table it cannot read", name)
		}
	}
}
