package ci

// The moo wire-fuzz pinned replay (make/moo-fuzz.mk tables-moo-wire-fuzz-pinned)
// read its list with a shell `read` loop, which drops a last line that has no
// newline: the replay reported one mutant fewer and passed (finding F-055, the
// same shape as the Luau replay registry). make/checks/moo-pinned-list.sh now
// reads the list whole before anything replays, and its controls are the
// committed fixtures under testdata/moo-wire-fuzz-pinned-controls. This test
// runs in milliseconds with no toolchain: it holds the recipe to the script,
// applies the script's rules to the real list and to every committed fixture
// (good accepted, each broken one refused), and holds the fixture set to the
// recipe's list both ways.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const mooPinnedControlsDir = "testdata/moo-wire-fuzz-pinned-controls"

// mooPinnedListProblems is make/checks/moo-pinned-list.sh's rule: the list
// ends in a newline, names at least one mutant, every named file is saved, and
// every saved file is named (its first field, exactly).
func mooPinnedListProblems(list string, saved []string) []string {
	var problems []string
	if list != "" && !strings.HasSuffix(list, "\n") {
		problems = append(problems, "the list does not end in a newline, so its last line would not be read")
	}
	named := map[string]bool{}
	have := map[string]bool{}
	for _, f := range saved {
		have[f] = true
	}
	for line := range strings.SplitSeq(list, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		named[fields[0]] = true
		if !have[fields[0]] {
			problems = append(problems, fields[0]+" is listed and not saved")
		}
	}
	if len(named) == 0 {
		problems = append(problems, "the list names no mutant")
	}
	for _, f := range saved {
		if !named[f] {
			problems = append(problems, f+" is saved and the list does not name it")
		}
	}
	sort.Strings(problems)
	return problems
}

// savedMutants lists dir's files as the script's `"$dir"/*` does: dir/<name>.
func savedMutants(t *testing.T, root, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, dir+"/"+e.Name())
	}
	return out
}

func TestMooPinnedListIsReadWhole(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk := readCI(t, root, "make/moo-fuzz.mk")
	for _, want := range []string{
		"tables-moo-wire-fuzz-pinned: tables-moo-wire-fuzz-pinned-list-controls ",
		"out=$$(sh make/checks/moo-pinned-list.sh $(MOO_WIRE_FUZZ_PINNED) $(MOO_WIRE_FUZZ_PINNED_DIR)) || exit 1",
		`[ $$n -eq "$$listed" ]`,
		"MOO_PINNED_LIST_CONTROLS := " + mooPinnedControlsDir,
		`[ $$dirs -eq 5 ]`,
		`[ $$refused -eq 4 ]`,
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("make/moo-fuzz.mk no longer holds %q", want)
		}
	}
	script := readCI(t, root, "make/checks/moo-pinned-list.sh")
	for _, want := range []string{
		`[ -z "$(tail -c 1 "$list")" ] || fail "the list does not end in a newline`,
		`[ "$n" -gt 0 ] || fail "the list names no mutant"`,
		`is listed and not saved`,
		`is saved and the list does not name it`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("make/checks/moo-pinned-list.sh no longer holds %q", want)
		}
	}

	// the real list
	list := readCI(t, root, "test/conformance/moo/wire-fuzz-pinned.txt")
	if problems := mooPinnedListProblems(list, savedMutants(t, root, "test/conformance/moo/wire-fuzz-pinned")); len(problems) != 0 {
		t.Errorf("test/conformance/moo/wire-fuzz-pinned.txt: %q", problems)
	}

	// the committed controls: the set is the recipe's, and each reads as the
	// recipe expects
	m := regexp.MustCompile(`(?m)^MOO_PINNED_LIST_REFUSED\s*:=\s*(.*)$`).FindStringSubmatch(mk)
	if m == nil {
		t.Fatal("make/moo-fuzz.mk does not list MOO_PINNED_LIST_REFUSED")
	}
	refused := strings.Fields(m[1])
	entries, err := os.ReadDir(filepath.Join(root, mooPinnedControlsDir))
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	want := append([]string{"good"}, refused...)
	sort.Strings(want)
	if fmt.Sprint(dirs) != fmt.Sprint(want) {
		t.Errorf("%s holds %v; the recipe runs %v", mooPinnedControlsDir, dirs, want)
	}
	reasons := map[string]string{
		"good":             "",
		"no-final-newline": "does not end in a newline",
		"unlisted-saved":   "is saved and the list does not name it",
		"listed-not-saved": "is listed and not saved",
		"names-nothing":    "the list names no mutant",
	}
	for _, name := range dirs {
		dir := mooPinnedControlsDir + "/" + name
		problems := mooPinnedListProblems(readCI(t, root, dir+"/PINNED"), savedMutants(t, root, dir+"/pins"))
		reason, known := reasons[name]
		switch {
		case !known:
			t.Errorf("%s: a control this test does not know", dir)
		case reason == "" && len(problems) != 0:
			t.Errorf("%s: the good fixture is refused: %q", dir, problems)
		case reason != "" && (len(problems) != 1 || !strings.Contains(problems[0], reason)):
			// one condition per broken fixture, so the verdict names the one
			// thing it breaks
			t.Errorf("%s: problems %q, want exactly one holding %q", dir, problems, reason)
		}
	}
}
