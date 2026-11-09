package roadmap

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// A leg whose generator and conformance driver are both on main must not
// carry a :note that says the leg was never started. The base sexp and the
// evidence overlay are both read: an overlay that hides a stale base note
// does not make the base note true.

var (
	sexpIDRx   = regexp.MustCompile(`\(:id\s+"([^"]+)"`)
	sexpNoteRx = regexp.MustCompile(`(?s):note\s+"((?:\\.|[^"\\])*)"`)

	notStartedNoteRx = regexp.MustCompile(`(?i)\bnot started\b`)
)

func parseSexpNotes(raw string) map[string]string {
	out := make(map[string]string)
	for _, loc := range sexpIDRx.FindAllStringSubmatchIndex(raw, -1) {
		start := loc[0]
		id := raw[loc[2]:loc[3]]
		out[id] = noteInForm(raw, start)
	}
	return out
}

func noteInForm(raw string, start int) string {
	depth := 0
	inStr := false
	esc := false
	end := len(raw)
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if inStr {
			if esc {
				esc = false
			} else if ch == '\\' {
				esc = true
			} else if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = i + 1
				i = len(raw)
			}
		}
	}
	if m := sexpNoteRx.FindStringSubmatch(raw[start:end]); m != nil {
		return m[1]
	}
	return ""
}

func loadRoadmapNotes(t *testing.T, repoRoot string) (base, merged map[string]string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "roadmap.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	base = parseSexpNotes(string(raw))
	merged = maps.Clone(base)
	evFiles, err := filepath.Glob(filepath.Join(repoRoot, "docs", "roadmap-evidence", "*.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(evFiles)
	for _, path := range evFiles {
		if strings.HasPrefix(filepath.Base(path), ".") {
			continue
		}
		evRaw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for id, note := range parseSexpNotes(string(evRaw)) {
			if note == "" {
				continue
			}
			merged[id] = note
		}
	}
	return base, merged
}

// legsWithGeneratorAndDriver is the set of conformance drivers that also have
// a generator tree on main: test/conformance/<leg> plus internal/codegen/<leg>
// or internal/codegen/<leg>table.
func legsWithGeneratorAndDriver(repoRoot string) map[string]bool {
	legs := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(repoRoot, "test", "conformance"))
	if err != nil {
		return legs
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		leg := e.Name()
		codegen := filepath.Join(repoRoot, "internal", "codegen", leg)
		table := filepath.Join(repoRoot, "internal", "codegen", leg+"table")
		if dirExists(codegen) || dirExists(table) {
			legs[leg] = true
		}
	}
	return legs
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func portLeg(id string) (string, bool) {
	rest, ok := strings.CutPrefix(id, "port/")
	if !ok || rest == "" {
		return "", false
	}
	leg, _, _ := strings.Cut(rest, "/")
	if leg == "" || strings.Contains(leg, " ") {
		return "", false
	}
	return leg, true
}

func checkNotStartedOnBuiltLegs(base, merged map[string]string, active map[string]bool) error {
	ids := make([]string, 0, len(base)+len(merged))
	seen := map[string]bool{}
	for id := range base {
		ids = append(ids, id)
		seen[id] = true
	}
	for id := range merged {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	var b strings.Builder
	for _, id := range ids {
		leg, ok := portLeg(id)
		if !ok || !active[leg] {
			continue
		}
		for _, src := range []struct {
			name string
			note string
		}{
			{"base", base[id]},
			{"merged", merged[id]},
		} {
			if notStartedNoteRx.MatchString(src.note) {
				fmt.Fprintf(&b, "%s :note on %s (leg %s has its generator and driver on main) says \"Not started\": %q\n", src.name, id, leg, src.note)
			}
		}
	}
	if b.Len() == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
}

// TestRoadmapNoteHygiene (X2) refuses a :note that says "Not started" on a
// port leg whose generator and conformance driver are already on main.
func TestRoadmapNoteHygiene(t *testing.T) {
	root := "../.."
	base, merged := loadRoadmapNotes(t, root)
	active := legsWithGeneratorAndDriver(root)
	for _, leg := range []string{"lua", "luau", "squirrel", "moo", "lobster", "goose"} {
		if !active[leg] {
			t.Fatalf("expected %q to have a generator and a conformance driver on main", leg)
		}
	}
	if err := checkNotStartedOnBuiltLegs(base, merged, active); err != nil {
		t.Fatal(err)
	}
}

// TestRoadmapNoteHygieneNegativeControl plants the fault the gate exists to
// catch, and a note the gate must not catch.
func TestRoadmapNoteHygieneNegativeControl(t *testing.T) {
	active := map[string]bool{"lua": true}
	clean := map[string]string{"port/lua/packet": "The generator and driver are on main."}
	if err := checkNotStartedOnBuiltLegs(clean, clean, active); err != nil {
		t.Fatalf("a note that does not say Not started was refused: %v", err)
	}

	planted := maps.Clone(clean)
	planted["port/lua/packet"] = "Not started."
	err := checkNotStartedOnBuiltLegs(planted, planted, active)
	if err == nil || !strings.Contains(err.Error(), "port/lua/packet") {
		t.Fatalf("planted \"Not started.\" was not refused, err=%v", err)
	}

	shout := maps.Clone(clean)
	shout["port/lua/packet"] = "NOT STARTED: no generator exists."
	if err := checkNotStartedOnBuiltLegs(shout, shout, active); err == nil {
		t.Fatal("uppercase \"NOT STARTED\" was not refused")
	}

	// A leg with no generator and no driver is outside the gate, even when
	// its note says the work has not started.
	idle := map[string]string{"port/notaleg/packet": "Not started."}
	if err := checkNotStartedOnBuiltLegs(idle, idle, active); err != nil {
		t.Fatalf("a leg that is not on main was refused: %v", err)
	}

	// The same plant on the real graph must be named, including when other
	// notes are already clean.
	root := "../.."
	base, merged := loadRoadmapNotes(t, root)
	realActive := legsWithGeneratorAndDriver(root)
	n := base["port/lua/packet"]
	if n == "" || notStartedNoteRx.MatchString(n) {
		t.Fatalf("port/lua/packet note = %q, want a clean note to plant into", n)
	}
	base = maps.Clone(base)
	base["port/lua/packet"] = "Not started."
	err = checkNotStartedOnBuiltLegs(base, merged, realActive)
	if err == nil || !strings.Contains(err.Error(), "port/lua/packet") {
		t.Fatalf("planted note on the real graph was not refused, err=%v", err)
	}
}
