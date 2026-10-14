// Command agentsmap regenerates the repository's AGENTS.md map: one root
// page under 3 KB, plus a page per big tree, each a table of directory →
// purpose → guarding test → one command. The catalog is catalog.go; the
// live tree is the other half. go test ./tools/agentsmap fails on drift.
//
//	make map
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const usage = `usage: go run ./tools/agentsmap

  regenerate AGENTS.md and the per-directory map pages from tools/agentsmap/catalog.go
  and the live tree. go test ./tools/agentsmap fails when they drift.
`

func main() {
	if len(os.Args) > 1 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	root, err := repoRoot()
	if err != nil {
		fail(err)
	}
	pages, issues := Render(root, catalog)
	if len(issues) > 0 {
		for _, s := range issues {
			fmt.Fprintf(os.Stderr, "agentsmap: %s\n", s)
		}
		os.Exit(1)
	}
	if err := Write(root, pages); err != nil {
		fail(err)
	}
	fmt.Printf("wrote %d pages\n", len(pages))
}

// repoRoot walks up from the working directory to the tree that holds the
// Makefile, so the tool runs the same from the root and from its own package.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no Makefile in any parent of the working directory")
		}
		dir = parent
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "agentsmap:", err)
	os.Exit(1)
}
