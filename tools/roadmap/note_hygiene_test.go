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

type sexpNode struct {
	id       string
	nodeType string
	state    string
	note     string
	children []string
	cells    [][3]string
}

var (
	sexpIDRx       = regexp.MustCompile(`\(:id\s+"([^"]+)"`)
	sexpTypeRx     = regexp.MustCompile(`:type\s+(:[^\s\)]+)`)
	sexpStateRx    = regexp.MustCompile(`:state\s+(:[^\s\)]+)`)
	sexpNoteRx     = regexp.MustCompile(`(?s):note\s+"((?:\\.|[^"\\])*)"`)
	sexpCellRx     = regexp.MustCompile(`\(\s*"([^"]+)"\s+"([^"]+)"\s+"([^"]+)"\s*\)`)
	sexpChildrenRx = regexp.MustCompile(`(?s):children\s+\(([^)]*)\)`)
	sexpStringRx   = regexp.MustCompile(`"((?:\\.|[^"\\])*)"`)

	staleNotStartedRx = regexp.MustCompile(`(?i)\bnot started\b|no generator, runtime or conformance leg exists`)
	staleDoneNoteRx   = regexp.MustCompile(`(?i)\bnot started\b|no generator|not yet verified to the :done bar|^\s*partial\b`)
	staleLuaTagRx     = regexp.MustCompile(`\bschema_leg_lua\b`)
)

func parseSexpNodes(raw string) map[string]sexpNode {
	out := make(map[string]sexpNode)
	for _, loc := range sexpIDRx.FindAllStringSubmatchIndex(raw, -1) {
		start := loc[0]
		id := raw[loc[2]:loc[3]]
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
		chunk := raw[start:end]
		n := sexpNode{id: id}
		if m := sexpTypeRx.FindStringSubmatch(chunk); m != nil {
			n.nodeType = m[1]
		}
		if m := sexpStateRx.FindStringSubmatch(chunk); m != nil {
			n.state = m[1]
		}
		if m := sexpNoteRx.FindStringSubmatch(chunk); m != nil {
			n.note = strings.ReplaceAll(m[1], `\n`, " ")
		}
		if n.nodeType == ":work-set" {
			if m := sexpChildrenRx.FindStringSubmatch(chunk); m != nil {
				for _, sm := range sexpStringRx.FindAllStringSubmatch(m[1], -1) {
					n.children = append(n.children, sm[1])
				}
			}
		}
		if n.nodeType == ":roadmap" {
			if idx := strings.Index(chunk, ":cells"); idx >= 0 {
				for _, cm := range sexpCellRx.FindAllStringSubmatch(chunk[idx:], -1) {
					n.cells = append(n.cells, [3]string{cm[1], cm[2], cm[3]})
				}
			}
		}
		out[id] = n
	}
	return out
}

func loadRoadmapAndOverlays(t *testing.T, repoRoot string) (base map[string]sexpNode, merged map[string]sexpNode) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "roadmap.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	base = parseSexpNodes(string(raw))
	merged = make(map[string]sexpNode, len(base))
	maps.Copy(merged, base)

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
		for id, ov := range parseSexpNodes(string(evRaw)) {
			cur := merged[id]
			cur.id = id
			if ov.state != "" {
				cur.state = ov.state
			}
			if ov.note != "" {
				cur.note = ov.note
			}
			merged[id] = cur
		}
	}
	return base, merged
}

func legsWithCodeAndDriverOnMain(repoRoot string) map[string]bool {
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
		codegenDir := filepath.Join(repoRoot, "internal", "codegen", leg)
		tableDir := filepath.Join(repoRoot, "internal", "codegen", leg+"table")
		if dirExists(codegenDir) || dirExists(tableDir) {
			legs[leg] = true
		}
	}
	return legs
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func collectWorkLeaves(id string, nodes map[string]sexpNode, seen map[string]bool) []string {
	if seen[id] {
		return nil
	}
	seen[id] = true
	n, ok := nodes[id]
	if !ok {
		return nil
	}
	if n.nodeType == ":work-set" {
		var out []string
		for _, child := range n.children {
			out = append(out, collectWorkLeaves(child, nodes, seen)...)
		}
		return out
	}
	return []string{id}
}

func checkRoadmapNoteHygiene(base, merged map[string]sexpNode, activeLegs map[string]bool) error {
	// 1. Check every cell across the three roadmaps (both base and merged).
	for _, rmID := range []string{"packet-wire", "fixed-tables", "variable-tables"} {
		rm, ok := base[rmID]
		if !ok {
			return fmt.Errorf("missing roadmap node %q", rmID)
		}
		for _, cell := range rm.cells {
			row, col, workID := cell[0], cell[1], cell[2]
			for _, leafID := range collectWorkLeaves(workID, base, map[string]bool{}) {
				bNode := base[leafID]
				mNode := merged[leafID]
				if mNode.state == ":done" {
					if staleDoneNoteRx.MatchString(mNode.note) {
						return fmt.Errorf("roadmap %s cell (%s, %s) leaf %s is :done but carries stale note: %q", rmID, row, col, leafID, mNode.note)
					}
				}
				if activeLegs[col] {
					if staleNotStartedRx.MatchString(bNode.note) {
						return fmt.Errorf("roadmap %s cell (%s, %s) leaf %s on active leg %q carries stale base note: %q", rmID, row, col, leafID, col, bNode.note)
					}
					if staleNotStartedRx.MatchString(mNode.note) {
						return fmt.Errorf("roadmap %s cell (%s, %s) leaf %s on active leg %q carries stale merged note: %q", rmID, row, col, leafID, col, mNode.note)
					}
				}
			}
		}
	}

	// 2. Check all port/<leg>/* and non-sprint-out :done nodes.
	for id, mNode := range merged {
		if strings.HasPrefix(id, "sprint-out/") {
			continue
		}
		bNode := base[id]
		if mNode.state == ":done" && staleDoneNoteRx.MatchString(mNode.note) {
			return fmt.Errorf("done node %s carries stale note: %q", id, mNode.note)
		}
		if strings.HasPrefix(id, "port/") {
			rest := strings.TrimPrefix(id, "port/")
			leg, _, _ := strings.Cut(rest, "/")
			if activeLegs[leg] {
				if staleNotStartedRx.MatchString(bNode.note) {
					return fmt.Errorf("node %s on active leg %q carries stale base note: %q", id, leg, bNode.note)
				}
				if staleNotStartedRx.MatchString(mNode.note) {
					return fmt.Errorf("node %s on active leg %q carries stale merged note: %q", id, leg, mNode.note)
				}
			}
			if leg == "lua" && (staleLuaTagRx.MatchString(bNode.note) || staleLuaTagRx.MatchString(mNode.note)) {
				return fmt.Errorf("lua node %s carries retired build-tag note: %q", id, mNode.note)
			}
		}
	}
	return nil
}

// TestRoadmapNotesAreNotStale (X2 / L6 / U0 / S0) holds every roadmap cell and
// port/<leg>/* node to its leg's reality on main: a leg with its generator and
// conformance driver on main never carries a "Not started" or "no generator"
// note, a :done task never carries a "Not started", "not yet verified to the
// :done bar", or leading "Partial" note, and port/lua/* notes do not reference
// the retired schema_leg_lua build tag.
func TestRoadmapNotesAreNotStale(t *testing.T) {
	base, merged := loadRoadmapAndOverlays(t, "../..")
	activeLegs := legsWithCodeAndDriverOnMain("../..")
	for _, wantLeg := range []string{"lua", "luau", "squirrel", "moo", "lobster", "goose"} {
		if !activeLegs[wantLeg] {
			t.Fatalf("expected %q to be detected as an active leg on main", wantLeg)
		}
	}
	if err := checkRoadmapNoteHygiene(base, merged, activeLegs); err != nil {
		t.Fatal(err)
	}
}

// TestRoadmapNotesAreNotStaleNegativeControl proves that synthetic stale notes
// on :done cells and on active legs are rejected by checkRoadmapNoteHygiene.
func TestRoadmapNotesAreNotStaleNegativeControl(t *testing.T) {
	base, merged := loadRoadmapAndOverlays(t, "../..")
	activeLegs := legsWithCodeAndDriverOnMain("../..")

	cases := []struct {
		name   string
		mutate func(b, m map[string]sexpNode)
		want   string
	}{
		{
			name: "done-cell-not-started",
			mutate: func(b, m map[string]sexpNode) {
				n := m["port/moo/packet"]
				n.state = ":done"
				n.note = "Not started."
				m["port/moo/packet"] = n
			},
			want: "is :done but carries stale note",
		},
		{
			name: "done-cell-partial",
			mutate: func(b, m map[string]sexpNode) {
				n := m["port/moo/packet"]
				n.state = ":done"
				n.note = "Partial: reader still owed."
				m["port/moo/packet"] = n
			},
			want: "is :done but carries stale note",
		},
		{
			name: "active-leg-todo-not-started",
			mutate: func(b, m map[string]sexpNode) {
				n := b["port/luau/packet"]
				n.note = "Not started: no generator, runtime or conformance leg exists."
				b["port/luau/packet"] = n
			},
			want: "on active leg \"luau\" carries stale base note",
		},
		{
			name: "lua-retired-build-tag",
			mutate: func(b, m map[string]sexpNode) {
				n := m["port/lua/generator"]
				n.note = "Built behind schema_leg_lua build tag."
				m["port/lua/generator"] = n
			},
			want: "lua node port/lua/generator carries retired build-tag note",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bCopy := maps.Clone(base)
			mCopy := maps.Clone(merged)
			tc.mutate(bCopy, mCopy)
			err := checkRoadmapNoteHygiene(bCopy, mCopy, activeLegs)
			if err == nil {
				t.Fatalf("expected %s mutation to be rejected, but checkRoadmapNoteHygiene passed", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s: got error %q, want substring %q", tc.name, err.Error(), tc.want)
			}
		})
	}
}
