package ci

// dist-ruby verified the analyzers' gem lock (make/ruby-gems.lock) with a
// shell `read` loop, which drops a last line that has no newline: that gem was
// neither fetched nor held to its sha256, and a copy already under
// dist/ruby-gems was installed unchecked by `gem install dist/ruby-gems/*.gem`
// (the same shape as finding F-055). make/checks/ruby-gems-lock.sh now reads
// the lock whole first, the install holds its verified count to the lock's,
// and its controls are the committed fixtures under
// testdata/ruby-gems-lock-controls. This test runs in milliseconds with no
// toolchain and no network: it holds the recipe to the script, applies the
// script's rule to the real lock and to each fixture, and holds the fixture set
// to the recipe's list both ways.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const rubyGemsLockControlsDir = "testdata/ruby-gems-lock-controls"

var rubyGemsLockLine = regexp.MustCompile(`^[A-Za-z0-9_.-]+==[0-9A-Za-z_.-]+ sha256:[0-9a-f]{64}$`)

// rubyGemsLockProblems is make/checks/ruby-gems-lock.sh's rule.
func rubyGemsLockProblems(lock string) []string {
	var problems []string
	if lock != "" && !strings.HasSuffix(lock, "\n") {
		problems = append(problems, "the lock does not end in a newline, so its last line would not be read")
	}
	gems := 0
	for line := range strings.SplitSeq(strings.TrimSuffix(lock, "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		gems++
		if !rubyGemsLockLine.MatchString(line) {
			problems = append(problems, fmt.Sprintf("%q is not name==version sha256:<64 hex>", line))
		}
	}
	if gems == 0 {
		problems = append(problems, "the lock names no gem")
	}
	return problems
}

func TestRubyGemsLockIsReadWhole(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk := readCI(t, root, "make/ruby-toolchain.mk")
	for _, want := range []string{
		"dist/ruby-%/GEMS: dist/ruby-%/PIN $(RUBY_GEMS_LOCK) | ruby-gems-lock-controls",
		"out=$$(sh make/checks/ruby-gems-lock.sh $(RUBY_GEMS_LOCK)) || exit 1",
		`[ $$verified -eq "$$listed" ]`,
		"RUBY_GEMS_LOCK_CONTROLS := " + rubyGemsLockControlsDir,
		`*": 2 gem(s)"`,
		`[ $$files -eq 4 ]`,
		`[ $$refused -eq 3 ]`,
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("make/ruby-toolchain.mk no longer holds %q", want)
		}
	}
	script := readCI(t, root, "make/checks/ruby-gems-lock.sh")
	for _, want := range []string{
		`[ -z "$(tail -c 1 "$lock")" ] || fail "the lock does not end in a newline`,
		`is not name==version sha256:<64 hex>`,
		`[ "$n" -gt 0 ] || fail "the lock names no gem"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("make/checks/ruby-gems-lock.sh no longer holds %q", want)
		}
	}

	if problems := rubyGemsLockProblems(readCI(t, root, "make/ruby-gems.lock")); len(problems) != 0 {
		t.Errorf("make/ruby-gems.lock: %q", problems)
	}

	m := regexp.MustCompile(`(?m)^RUBY_GEMS_LOCK_REFUSED\s*:=\s*(.*)$`).FindStringSubmatch(mk)
	if m == nil {
		t.Fatal("make/ruby-toolchain.mk does not list RUBY_GEMS_LOCK_REFUSED")
	}
	want := []string{"good"}
	want = append(want, strings.Fields(m[1])...)
	sort.Strings(want)
	entries, err := os.ReadDir(filepath.Join(root, rubyGemsLockControlsDir))
	if err != nil {
		t.Fatal(err)
	}
	var have []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".lock"); ok {
			have = append(have, name)
		}
	}
	if fmt.Sprint(have) != fmt.Sprint(want) {
		t.Errorf("%s holds %v; the recipe runs %v", rubyGemsLockControlsDir, have, want)
	}
	reasons := map[string]string{
		"good":             "",
		"no-final-newline": "does not end in a newline",
		"malformed-line":   "is not name==version sha256:<64 hex>",
		"names-nothing":    "the lock names no gem",
	}
	for _, name := range have {
		problems := rubyGemsLockProblems(readCI(t, root, rubyGemsLockControlsDir+"/"+name+".lock"))
		reason, known := reasons[name]
		switch {
		case !known:
			t.Errorf("%s/%s.lock: a control this test does not know", rubyGemsLockControlsDir, name)
		case reason == "" && len(problems) != 0:
			t.Errorf("%s/good.lock is refused: %q", rubyGemsLockControlsDir, problems)
		case reason != "" && (len(problems) != 1 || !strings.Contains(problems[0], reason)):
			// one broken condition per fixture, so each shows its own check
			// turns the lock red
			t.Errorf("%s/%s.lock: problems %q, want exactly one holding %q", rubyGemsLockControlsDir, name, problems, reason)
		}
	}
}
