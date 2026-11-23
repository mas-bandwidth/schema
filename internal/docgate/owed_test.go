package docgate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestOwedRowsAreLinked holds docs/SPEC-CONFORMANCE.md §7 honest: every
// owed rule names the PR that carries its code and row and at least one id
// it adds, an entry whose PR has merged can no longer say owed, and neither
// can one any id of which is in the tree. It reads git and the tree, never
// the network.
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
		// A shallow clone outside CI has no history to read. The tree half
		// below still runs; the merged half is named here as NOT RUN.
		t.Logf("NOT RUN: the merged-PR half of this check: %v. Run it in a full clone.", err)
		merged = nil
	case err != nil:
		t.Fatal(err)
	}
	tree, err := OpenTree(root, "")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range entries {
		for _, id := range e.IDs {
			if _, err := IDKind(id); err == nil {
				ids = append(ids, id)
			}
		}
	}
	inTree, err := tree.Present(ids)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := CheckOwed(entries, func(pr int) bool { return merged[pr] }, func(id string) (bool, error) {
		v, ok := inTree[id]
		if !ok {
			return false, errors.New("Present did not answer for this id")
		}
		return v, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Error(f)
	}
	t.Logf("%d owed entries, %d ids checked", len(entries), len(ids))
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
	present := func(id string) (bool, error) { return id == "row_landed", nil }
	cases := []struct {
		name, row, want string
	}{
		{"clean", clean, ""},
		{"no PR named", "| SF-01 | a rule | `row_b` | `row_b` | none yet | owed |", "names no PR"},
		{"PR merged, still owed", "| SF-02 | a rule | `row_c` | `row_c` | #4740 | owed |", "#4740 has merged"},
		{"one of two PRs merged", "| SF-03 | a rule | `row_d` | `row_d` | #4740, #4757 | owed |", "#4740 has merged"},
		{"one id of two in the tree", "| FL-02 | a rule | `row_landed` | `row_landed`, `row_not_yet` | #4757 | owed |", "row_landed is in the tree"},
		{"no id owed", "| FL-05 | a rule | prose | the verdict changes | #4757 | owed |", "owes no id"},
		{"a plain word as an id", "| FL-06 | a rule | `refused` | `refused`, `row_f` | #4757 | owed |", "malformed id \"refused\""},
		{"call-site text as an id", "| FL-07 | a rule | x | `tablewire.AnnounceSchema` | #4757 | owed |", "malformed id"},
		{"a wildcard as an id", "| FL-08 | a rule | x | `sp-count-min-*` | #4757 | owed |", "malformed id"},
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

// plantRepo builds a small git repository in a temporary directory: the
// row registries with a decoy row, a comment naming an owed id, a Test func
// outside a _test.go file, and a history whose first-parent line holds a PR
// merge and a squash while a merged branch carries a "(#20)" commit.
func plantRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(p, body string) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	write(ManifestPath, "# rr_commented is a comment, not a row\nreport old_rr_fixed_zero_records_v0 unit Root wire\nframing real_row unit Root wire\n")
	write(HostilePath, "conformance-hostile-row\ttext\tunit\n")
	write("test/conformance/write-refusal/rows.txt", "wref_one refused count 1\n")
	write("notes.go", "package x\n\n// owed: rr_fixed_zero_records\nfunc TestNotInTestFile(t *testing.T) {}\n")
	write("a_test.go", "package x\n\nfunc TestReal(t *testing.T) {}\n\n// func TestIndented(t *testing.T) {}\n")
	write("dir/sub/a.txt", "x\n")
	run("add", "-A")
	run("commit", "-q", "-m", "Merge pull request #10 from x/y")
	run("checkout", "-q", "-b", "side")
	write("side.txt", "x\n")
	run("add", "-A")
	run("commit", "-q", "-m", "a branch commit (#20)")
	run("checkout", "-q", "main")
	run("merge", "-q", "--no-ff", "-m", "merge the side branch", "side")
	write("squash.txt", "x\n")
	run("add", "-A")
	run("commit", "-q", "-m", "a squash merge (#30)")
	return dir
}

// TestOwedLookupIsExact plants every near miss a substring match would take
// for the id, and every PR number off the first-parent line.
func TestOwedLookupIsExact(t *testing.T) {
	t.Parallel()
	dir := plantRepo(t)
	tree, err := OpenTree(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"rr_fixed_zero_records":        false, // a decoy row holds it as a substring; a comment names it
		"old_rr_fixed_zero_records_v0": true,
		"real_row":                     true,
		"rr_commented":                 false, // a comment line of the manifest is no row
		"conformance-hostile-row":      true,
		"wref_one":                     true,
		"TestReal":                     true,
		"TestRe":                       false, // a prefix of a Test func
		"TestNotInTestFile":            false, // declared outside a _test.go file
		"TestIndented":                 false, // not at the start of a line
		"dir/sub":                      true,
		"dir/sub/a.txt":                true,
		"dir/su":                       false,
		"ir/sub/a.txt":                 false,
	}
	var ids []string
	for id := range want {
		ids = append(ids, id)
	}
	got, err := tree.Present(ids)
	if err != nil {
		t.Fatal(err)
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s: present = %v, want %v", id, got[id], w)
		}
	}
	for _, bad := range []string{"refused", "tablewire.AnnounceSchema", "sp-count-min-*", "dir/*"} {
		if _, err := tree.Present([]string{bad}); err == nil {
			t.Errorf("%q: a malformed id was looked up", bad)
		}
	}
	merged, err := MergedPRs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !merged[10] || !merged[30] || merged[20] || len(merged) != 2 {
		t.Errorf("merged = %v, want #10 and #30 only: #20 is a branch commit off the first-parent line", merged)
	}
}
