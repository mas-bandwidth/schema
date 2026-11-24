package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// W-ROSTER (docs/SPEC-CONFORMANCE.md §2, docs/SPEC-TABLES.md §4.2): a
// whole-roster `harness wire-fuzz` run fails any seed whose root the leg has
// no codec for, unless test/conformance/wire-absent/<leg>-<arm>.tsv holds it.
// The harness derives that path from --leg and the run's arm, so a caller
// cannot pass the wrong ledger; it can only leave --leg off, and the harness
// refuses that. This test finds every wire-fuzz call the build makes
// (Makefile, make/*.mk, make/*.sh, make/checks/*.sh) and fails on one that
// would run the whole roster without naming its leg, so a nightly-only caller
// goes red here, on the PR, and not in the nightly.

// wireFuzzCall is one invocation of the harness's wire-fuzz command.
type wireFuzzCall struct {
	where string // file:line of the logical line's first physical line
	args  string // the words after `wire-fuzz`, up to the shell separator
}

// harnessWireFuzz matches the harness binary (by path, variable or the
// harness's own name) followed by its wire-fuzz command.
var harnessWireFuzz = regexp.MustCompile(`(conformance-harness(-vm)?|/harness|\$\{?harness\}?"?)\s+wire-fuzz\b`)

// wireFuzzCalls returns every wire-fuzz invocation in the build's recipes.
// wireFuzzBuildFiles are the files the build runs wire-fuzz from; each
// pattern must match, or the scan judged nothing there.
var wireFuzzBuildFiles = []string{"Makefile", "make/*.mk", "make/*.sh", "make/checks/*.sh"}

func wireFuzzCalls(t *testing.T, root string, patterns []string) []wireFuzzCall {
	t.Helper()
	var files []string
	for _, pat := range patterns {
		got, err := provesran.Glob(filepath.Join(root, pat))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, got...)
	}
	sort.Strings(files)
	var calls []wireFuzzCall
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, path)
		lines := strings.Split(string(data), "\n")
		for i := 0; i < len(lines); i++ {
			start := i
			logical := lines[i]
			for strings.HasSuffix(logical, `\`) && i+1 < len(lines) {
				i++
				logical = strings.TrimSuffix(logical, `\`) + " " + lines[i]
			}
			if strings.HasPrefix(strings.TrimSpace(logical), "#") {
				continue
			}
			for _, loc := range harnessWireFuzz.FindAllStringIndex(logical, -1) {
				rest := logical[loc[1]:]
				calls = append(calls, wireFuzzCall{where: rel + ":" + itoa(start+1), args: callArgs(rest)})
			}
		}
	}
	return calls
}

// callArgs cuts the words after `wire-fuzz` at the first shell separator
// outside quotes: a redirection, a pipe, `;`, `&&` or `||`.
func callArgs(rest string) string {
	var quote rune
	for i, r := range rest {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '>' || r == '|' || r == ';' || r == '&':
			return strings.TrimSpace(rest[:i])
		}
	}
	return strings.TrimSpace(rest)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// legFlag finds the --leg word of a call; a replay (one mutant, one named
// root) reads no ledger and needs none.
var legFlag = regexp.MustCompile(`(^|\s)--leg[ =](\S+)`)

// replayFlag marks a one-mutant replay.
var replayFlag = regexp.MustCompile(`(^|\s)--replay[ =]`)

func TestEveryWholeRosterWireFuzzCallNamesItsLeg(t *testing.T) {
	root := repoRoot(t)
	calls := wireFuzzCalls(t, root, wireFuzzBuildFiles)
	// the inventory is the proof the scan saw the build: a scan that finds
	// nothing, or loses a known caller, is a broken scan and not a pass
	if len(calls) < 100 {
		t.Fatalf("found %d wire-fuzz calls in the build; the scan is broken", len(calls))
	}
	seen := map[string]bool{}
	whole := 0
	for _, c := range calls {
		seen[strings.SplitN(c.where, ":", 2)[0]] = true
		if replayFlag.MatchString(c.args) {
			continue
		}
		whole++
		if m := legFlag.FindStringSubmatch(c.args); m == nil || m[2] == "" {
			t.Errorf("%s: a whole-roster wire-fuzz call names no --leg, so the harness reads no absent-root ledger for it: wire-fuzz %s", c.where, c.args)
		}
		if testing.Verbose() {
			t.Logf("%s: wire-fuzz %s", c.where, c.args)
		}
	}
	for _, must := range []string{"make/c.mk", "make/cs.mk", "make/verify-compiled.mk", "make/verify-vm.mk", "make/checks/wire-fuzz-release-run.sh", "make/goose-wirefuzz.sh", "Makefile"} {
		if !seen[must] {
			t.Errorf("the scan found no wire-fuzz call in %s, which has them; the scan is broken", must)
		}
	}
	if whole == 0 {
		t.Fatal("no whole-roster wire-fuzz call found; the scan is broken")
	}
}

// The scan itself: a planted call with no --leg is caught, a replay is not,
// and a call whose --leg sits on a continuation line is read whole.
func TestWireFuzzCallScanCatchesAMissingLeg(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "make"), 0o755); err != nil {
		t.Fatal(err)
	}
	mk := "x:\n" +
		"\t./build/conformance-harness wire-fuzz --driver 'a b' \\\n\t\t--leg c --seed 1 > log\n" +
		"\t./build/conformance-harness wire-fuzz --driver d --seed 1 --n 2\n" +
		"\t./build/conformance-harness wire-fuzz --driver d --replay f.bin --unit u --root R\n" +
		"# ./build/conformance-harness wire-fuzz --driver commented\n"
	if err := os.WriteFile(filepath.Join(dir, "make", "x.mk"), []byte(mk), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := wireFuzzCalls(t, dir, []string{"make/*.mk"})
	if len(calls) != 3 {
		t.Fatalf("scan found %d calls, want 3: %+v", len(calls), calls)
	}
	var missing []string
	for _, c := range calls {
		if replayFlag.MatchString(c.args) {
			continue
		}
		if legFlag.FindStringSubmatch(c.args) == nil {
			missing = append(missing, c.where)
		}
	}
	if len(missing) != 1 || missing[0] != "make/x.mk:4" {
		t.Fatalf("scan flagged %v, want exactly make/x.mk:4", missing)
	}
}
