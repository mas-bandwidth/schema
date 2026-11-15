package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// entriesTree writes a manifest and the given entry files into a fresh
// directory and returns its root.
func entriesTree(t *testing.T, entries map[string]string) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(manifestPath, `{"groups": [
  {"name": "base", "when": "pull-request", "targets": ["a-negative-control"]},
  {"name": "elixir", "when": "pull-request", "targets": ["b-negative-control"]}
]}`)
	for name, body := range entries {
		write(entriesDir+"/"+name, body)
	}
	return root
}

// TestEntriesJoinTheirGroup holds the per-control files: each entry's targets
// are appended to the group it names, in file-name order, and a file that is
// not an entry (no .json suffix) is not read.
func TestEntriesJoinTheirGroup(t *testing.T) {
	t.Parallel()
	root := entriesTree(t, map[string]string{
		"d-negative-control.json": `{"group": "elixir", "targets": ["d-negative-control"]}`,
		"c-negative-control.json": `{"group": "elixir", "targets": ["c-negative-control", "c2-negative-control"]}`,
		"e-negative-control.json": `{"group": "base", "targets": ["e-negative-control"]}`,
		"README":                  `not an entry`,
	})
	m, err := loadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"base":   {"a-negative-control", "e-negative-control"},
		"elixir": {"b-negative-control", "c-negative-control", "c2-negative-control", "d-negative-control"},
	}
	for _, g := range m.Groups {
		if !slices.Equal(g.Targets, want[g.Name]) {
			t.Errorf("group %s holds %v, want %v", g.Name, g.Targets, want[g.Name])
		}
	}
	targets, err := m.targetsOf("elixir")
	if err != nil {
		t.Fatal(err)
	}
	if targets != "b-negative-control c-negative-control c2-negative-control d-negative-control" {
		t.Errorf("targets elixir = %q", targets)
	}
}

// TestABadEntryIsRefused is the reversed witness: an entry naming no group
// the manifest holds, carrying a field an entry does not have, naming no
// target, or not parsing is refused by name, never dropped.
func TestABadEntryIsRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body, want string }{
		{"unknown group", `{"group": "elixr", "targets": ["x-negative-control"]}`, `no group named "elixr"`},
		{"unknown field", `{"group": "elixir", "target": ["x-negative-control"]}`, `unknown field`},
		{"no target", `{"group": "elixir", "targets": []}`, `names no target`},
		{"not json", `{"group": "elixir",`, `x-negative-control.json`},
		{"group and exclusion", `{"group": "elixir", "targets": ["x-negative-control"], "excluded": [{"target": "y-negative-control", "reason": "r"}]}`, `one or the other`},
		{"exclusion without reason", `{"excluded": [{"target": "x-negative-control", "reason": " "}]}`, `with no reason`},
		{"exclusion without target", `{"excluded": [{"reason": "r"}]}`, `with no target`},
		{"exclusion unknown field", `{"excluded": [{"target": "x-negative-control", "why": "r"}]}`, `unknown field`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := entriesTree(t, map[string]string{"x-negative-control.json": tc.body})
			_, err := loadManifest(root)
			if err == nil {
				t.Fatalf("the entry %s was accepted", tc.body)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "x-negative-control.json") {
				t.Errorf("the refusal %q does not name %q and the file", err, tc.want)
			}
		})
	}
}

// TestAnEntryDuplicatingAControlIsRefused: an entry that adds a control the
// manifest already places is two placements, which covered refuses.
func TestAnEntryDuplicatingAControlIsRefused(t *testing.T) {
	t.Parallel()
	root := entriesTree(t, map[string]string{
		"b-negative-control.json": `{"group": "base", "targets": ["b-negative-control"]}`,
	})
	m, err := loadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.covered(); err == nil || !strings.Contains(err.Error(), "b-negative-control") {
		t.Fatalf("covered accepted a control placed twice: %v", err)
	}
}

// TestAnEntryExcludesAControl holds the second entry form: a file carrying
// excluded entries adds each, target and reason, to the exclusion list, and
// the excluded control is accounted for by covered like one of the shared
// file's own exclusions.
func TestAnEntryExcludesAControl(t *testing.T) {
	t.Parallel()
	root := entriesTree(t, map[string]string{
		"x-negative-control.json": `{"excluded": [{"target": "x-negative-control", "reason": "needs a toolchain no tier installs"}]}`,
	})
	m, err := loadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Skipped) != 1 || m.Skipped[0].Target != "x-negative-control" || m.Skipped[0].Reason != "needs a toolchain no tier installs" {
		t.Fatalf("exclusions = %+v, want x-negative-control with its reason", m.Skipped)
	}
	covered, err := m.covered()
	if err != nil {
		t.Fatal(err)
	}
	if covered["x-negative-control"] != "excluded" {
		t.Errorf("x-negative-control is %q in the plan, want excluded", covered["x-negative-control"])
	}
	dup := entriesTree(t, map[string]string{
		"b-negative-control.json": `{"excluded": [{"target": "b-negative-control", "reason": "r"}]}`,
	})
	m, err = loadManifest(dup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.covered(); err == nil || !strings.Contains(err.Error(), "b-negative-control") {
		t.Fatalf("covered accepted a control both grouped and excluded: %v", err)
	}
}
