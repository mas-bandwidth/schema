package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	luaOverlayIDRx   = regexp.MustCompile(`^\(:id\s+"([^"]+)"`)
	luaOverlayStRx   = regexp.MustCompile(`:state\s+:([A-Za-z0-9_-]+)`)
	luaOverlayRevRx  = regexp.MustCompile(`:tested-revision\s+"([^"]*)"`)
	luaOverlayStrRx  = regexp.MustCompile(`"([^"]*)"`)
	luaRoadmapTaskRx = regexp.MustCompile(`\(:id "([^"]+)" :type :task`)
	luaTestFuncRx    = regexp.MustCompile(`(?m)^func\s+(Test[A-Za-z0-9_]+)\s*\(`)
)

// luaEvidenceStrings returns the quoted strings of the :evidence list of one
// overlay entry (the text between :evidence and the next keyword).
func luaEvidenceStrings(entry string) []string {
	_, after, ok := strings.Cut(entry, ":evidence")
	if !ok {
		return nil
	}
	rest := after
	open := strings.Index(rest, "(")
	if open < 0 {
		return nil
	}
	end := strings.Index(rest, ":tested-revision")
	if end < 0 || end < open {
		return nil
	}
	var out []string
	for _, m := range luaOverlayStrRx.FindAllStringSubmatch(rest[open:end], -1) {
		out = append(out, m[1])
	}
	return out
}

// validateLuaEvidenceOverlay holds docs/roadmap-evidence/lua.sexp to its shape:
// one entry per line, sorted by :id, each id a task of the roadmap, each entry
// :done with a 40-hex :tested-revision, and each evidence string naming a Go
// test func that exists in the named file or a make target that exists.
func validateLuaEvidenceOverlay(root, overlay string, tasks map[string]bool) error {
	var ids []string
	seen := map[string]bool{}
	for line := range strings.SplitSeq(overlay, "\n") {
		entry := strings.TrimSpace(line)
		if entry == "" || strings.HasPrefix(entry, ";") {
			continue
		}
		m := luaOverlayIDRx.FindStringSubmatch(entry)
		if m == nil {
			return fmt.Errorf("malformed overlay line: %.60s", entry)
		}
		id := m[1]
		if seen[id] {
			return fmt.Errorf("duplicate overlay entry %s", id)
		}
		seen[id] = true
		ids = append(ids, id)
		if !strings.HasPrefix(id, "port/lua/") {
			return fmt.Errorf("%s: not a lua task", id)
		}
		if !tasks[id] {
			return fmt.Errorf("%s: not a task in docs/roadmap.sexp", id)
		}
		if st := luaOverlayStRx.FindStringSubmatch(entry); st == nil || st[1] != "done" {
			return fmt.Errorf("%s: every entry must be :state :done", id)
		}
		if rev := luaOverlayRevRx.FindStringSubmatch(entry); rev == nil || !isFortyHexSHA(rev[1]) {
			return fmt.Errorf("%s: :tested-revision must be 40 lowercase hex", id)
		}
		evidence := luaEvidenceStrings(entry)
		if len(evidence) == 0 {
			return fmt.Errorf("%s: no :evidence strings", id)
		}
		for _, ev := range evidence {
			if err := checkSquirrelEvidenceCitation(root, id, ev); err != nil {
				return err
			}
		}
	}
	if len(ids) == 0 {
		return fmt.Errorf("overlay holds no entries")
	}
	if !sort.StringsAreSorted(ids) {
		return fmt.Errorf("overlay entries are not sorted by :id")
	}
	return nil
}

// TestLuaEvidenceOverlayNamesRealTests verifies docs/roadmap-evidence/lua.sexp:
// the overlay parses, every id is a task in the roadmap, every entry is :done
// with a 40-hex :tested-revision, and every evidence string names a Go test
// that exists in the cited file or a make target that exists. Later lua
// evidence cards add entries and rely on this test.
func TestLuaEvidenceOverlayNamesRealTests(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(b)
	}
	tasks := map[string]bool{}
	for _, m := range luaRoadmapTaskRx.FindAllStringSubmatch(read("docs/roadmap.sexp"), -1) {
		tasks[m[1]] = true
	}
	overlay := read("docs/roadmap-evidence/lua.sexp")
	if err := validateLuaEvidenceOverlay(root, overlay, tasks); err != nil {
		t.Fatal(err)
	}

	// The entries whose tests ran and passed: the packet row and six table
	// rows. block-build (its builder test needs the sibling Go serialize
	// runtime) and cook-write (red at the base) are owed, not listed.
	for _, id := range []string{
		"port/lua/packet",
		"port/lua/vt/block-read", "port/lua/vt/build-version", "port/lua/vt/cook-open",
		"port/lua/vt/doc-tags", "port/lua/vt/reflection", "port/lua/vt/unit-registry",
	} {
		if !strings.Contains(overlay, `(:id "`+id+`" `) {
			t.Errorf("overlay carries no entry for %s", id)
		}
	}

	// Negative controls: each defect must be refused.
	lines := strings.Split(strings.TrimSpace(overlay), "\n")
	lines[0], lines[1] = lines[1], lines[0]
	for name, bad := range map[string]string{
		"missing-test": strings.Replace(overlay, "TestLuaTypeWireReadsTheGoldens", "TestLuaNoSuchTest", 1),
		"missing-make": strings.Replace(overlay, "packet-utf8-lua", "packet-no-such-target-lua", 1),
		"bad-revision": strings.Replace(overlay, ":tested-revision", ":tested-revision \"abc\" :x", 1),
		"todo-state":   strings.Replace(overlay, ":state :done", ":state :todo", 1),
		"unknown-id":   strings.Replace(overlay, `(:id "port/lua/packet"`, `(:id "port/lua/no-such-task"`, 1),
		"unsorted":     strings.Join(lines, "\n") + "\n",
	} {
		if bad == overlay {
			t.Errorf("negative control %s changed nothing", name)
			continue
		}
		if err := validateLuaEvidenceOverlay(root, bad, tasks); err == nil {
			t.Errorf("negative control %s: the validator accepted the defect", name)
		}
	}
}
