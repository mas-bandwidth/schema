package main

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// maxRootBytes is the ceiling on the root AGENTS.md. A harness reads that
// page at the start of every session, so a map that grows without a cap
// stops being read. Nested pages are uncapped.
const maxRootBytes = 3 * 1024

const rootAgents = "AGENTS.md"

// skipNames are directories the map does not catalog: VCS, build output, and
// local toolchain unpacks. A new source tree is not one of these.
var skipNames = map[string]bool{
	".git":         true,
	"bin":          true,
	"build":        true,
	"dist":         true,
	"node_modules": true,
	"obj":          true,
	"runtimes":     true,
	"target":       true,
	"tending":      true,
}

type catalogIndex map[string]Entry

func indexCatalog(cat []Entry) (catalogIndex, []string) {
	idx := make(catalogIndex, len(cat))
	var issues []string
	for _, e := range cat {
		if e.Path == "" {
			issues = append(issues, "catalog row has an empty path")
			continue
		}
		if strings.Contains(e.Path, "\\") || strings.HasPrefix(e.Path, "/") || strings.HasSuffix(e.Path, "/") {
			issues = append(issues, fmt.Sprintf("catalog path %q must be slash-separated with no leading or trailing slash", e.Path))
		}
		if _, dup := idx[e.Path]; dup {
			issues = append(issues, fmt.Sprintf("catalog names %s twice", e.Path))
		}
		if strings.TrimSpace(e.Purpose) == "" || strings.TrimSpace(e.Guard) == "" || strings.TrimSpace(e.Command) == "" {
			issues = append(issues, fmt.Sprintf("catalog row %s is missing purpose, guard, or command", e.Path))
		}
		idx[e.Path] = e
	}
	return idx, issues
}

// Render builds every AGENTS.md page from the live tree and the catalog.
// Uncatalogued children still appear (as "-" rows) so a committed page goes
// stale the moment a mapped directory grows a child. Completeness issues are
// returned beside the pages; make map refuses to write when any are present.
func Render(root string, cat []Entry) (map[string]string, []string) {
	idx, issues := indexCatalog(cat)
	issues = append(issues, treeIssues(root, cat, idx)...)

	pages := make(map[string]string)
	pages[rootAgents] = renderPage(root, "", idx)
	for _, e := range cat {
		if !e.Page {
			continue
		}
		pages[e.Path+"/"+rootAgents] = renderPage(root, e.Path, idx)
	}
	if n := len(pages[rootAgents]); n >= maxRootBytes {
		issues = append(issues, fmt.Sprintf("%s is %d bytes, over the %d-byte cap; shorten the catalog rows", rootAgents, n, maxRootBytes))
	}
	return pages, issues
}

func treeIssues(root string, cat []Entry, idx catalogIndex) []string {
	var issues []string
	top, err := childDirs(root, "")
	if err != nil {
		return []string{err.Error()}
	}
	for _, name := range top {
		if _, ok := idx[name]; !ok {
			issues = append(issues, fmt.Sprintf("uncatalogued directory %s; add a row to tools/agentsmap/catalog.go and run: make map", name))
		}
	}
	for _, e := range cat {
		abs := filepath.Join(root, filepath.FromSlash(e.Path))
		st, err := os.Stat(abs)
		if err != nil || !st.IsDir() {
			issues = append(issues, fmt.Sprintf("catalog names %s which is not a directory", e.Path))
			continue
		}
		parent := parentPath(e.Path)
		if parent == "" {
			continue
		}
		p, ok := idx[parent]
		if !ok || !p.Page {
			issues = append(issues, fmt.Sprintf("%s is catalogued but %s is not a page", e.Path, parent))
		}
	}
	for _, e := range cat {
		if !e.Page {
			continue
		}
		kids, err := childDirs(root, e.Path)
		if err != nil {
			issues = append(issues, err.Error())
			continue
		}
		for _, name := range kids {
			path := e.Path + "/" + name
			if _, ok := idx[path]; !ok {
				issues = append(issues, fmt.Sprintf("uncatalogued directory %s; add a row to tools/agentsmap/catalog.go and run: make map", path))
			}
		}
	}
	return issues
}

func renderPage(root, dir string, idx catalogIndex) string {
	var b strings.Builder
	if dir == "" {
		b.WriteString("# AGENTS.md — generated map\n\n")
		b.WriteString("Do not edit. `make map` regenerates this file. Rules: [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md). Glenn, 2026-09-18: AGENTS.md alone — no `CLAUDE.md`, no pointer, no symlink.\n\n")
		b.WriteString("Schema is the data language for games. One definition compiles to bit-packed codecs in C, C++, C#, Dart, Elixir, Go, Java, JavaScript and Rust. The compiler is AGPL-3.0; generated code is yours ([LICENSE](LICENSE)). Public Go API: `compiler/`, `ir/`.\n\n")
		b.WriteString("```\nmake\nmake test\nbin/schema check <dir>\n```\n\n")
	} else {
		depth := strings.Count(dir, "/") + 1
		up := strings.Repeat("../", depth)
		b.WriteString("# AGENTS.md — generated map of ")
		b.WriteString(dir)
		b.WriteString("/\n\n")
		b.WriteString("Do not edit. `make map` regenerates this file. Root: [AGENTS.md](")
		b.WriteString(up)
		b.WriteString("AGENTS.md). Rules: [CONTRIBUTING.md](")
		b.WriteString(up)
		b.WriteString("docs/CONTRIBUTING.md).\n\n")
	}
	b.WriteString("| dir | purpose | guard | command |\n| --- | --- | --- | --- |\n")

	kids, err := childDirs(root, dir)
	if err != nil {
		b.WriteString("| - | ")
		b.WriteString(err.Error())
		b.WriteString(" | - | - |\n")
		return b.String()
	}
	for _, name := range kids {
		path := name
		if dir != "" {
			path = dir + "/" + name
		}
		e, ok := idx[path]
		if !ok {
			e = Entry{Path: path, Purpose: "-", Guard: "-", Command: "-"}
		}
		b.WriteString(renderRow(e))
	}
	return b.String()
}

func renderRow(e Entry) string {
	label := "`" + filepath.Base(e.Path) + "/`"
	if e.Page {
		rel := filepath.Base(e.Path) + "/AGENTS.md"
		label = "[" + filepath.Base(e.Path) + "/](" + rel + ")"
	}
	return fmt.Sprintf("| %s | %s | `%s` | `%s` |\n", label, e.Purpose, e.Guard, e.Command)
}

func childDirs(root, rel string) ([]string, error) {
	dir := root
	if rel != "" {
		dir = filepath.Join(root, filepath.FromSlash(rel))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if skipDir(name) {
			continue
		}
		if !e.IsDir() {
			if e.Type()&fs.ModeSymlink != 0 {
				st, err := os.Stat(filepath.Join(dir, name))
				if err != nil || !st.IsDir() {
					continue
				}
			} else {
				continue
			}
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

func skipDir(name string) bool {
	if skipNames[name] {
		return true
	}
	return strings.HasPrefix(name, ".") && name != ".github"
}

func parentPath(p string) string {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return ""
	}
	return p[:i]
}

// Check holds the committed pages against a fresh Render. A mapped directory
// that grew a child, a catalog row that was edited, or a hand-edit of a page
// all fail with "stale; run: make map".
func Check(root string, cat []Entry) []string {
	pages, issues := Render(root, cat)
	for _, path := range slices.Sorted(maps.Keys(pages)) {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s is missing; run: make map", path))
			continue
		}
		if string(got) != pages[path] {
			issues = append(issues, fmt.Sprintf("%s is stale; run: make map", path))
		}
	}
	issues = append(issues, extraAgentsPages(root, pages)...)
	return issues
}

// extraAgentsPages names every AGENTS.md the tree carries that Render did not
// produce, so a hand-written prefix cannot sit beside the generated map.
func extraAgentsPages(root string, pages map[string]string) []string {
	var extra []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipNames[d.Name()] {
				return filepath.SkipDir
			}
			if strings.HasPrefix(d.Name(), ".") && d.Name() != ".github" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != rootAgents {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if _, ok := pages[rel]; !ok {
			extra = append(extra, fmt.Sprintf("%s is not generated by tools/agentsmap; delete it or add a Page row and run: make map", rel))
		}
		return nil
	})
	if err != nil {
		return []string{err.Error()}
	}
	slices.Sort(extra)
	return extra
}

func Write(root string, pages map[string]string) error {
	for _, path := range slices.Sorted(maps.Keys(pages)) {
		abs := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, []byte(pages[path]), 0o644); err != nil {
			return err
		}
	}
	return nil
}
