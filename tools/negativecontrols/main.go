package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const usage = `usage: go run ./tools/negativecontrols <command>

  list      every negative-control target the Makefile and its includes define
  check     hold the manifest against the Makefile; exit nonzero on a difference
  matrix    the manifest's groups as a GitHub Actions matrix, one line of JSON
  targets   the make targets of one group, space separated: targets <group>
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	root, err := repoRoot()
	if err != nil {
		fail(err)
	}
	switch os.Args[1] {
	case "list":
		defs, err := enumerate(root)
		if err != nil {
			fail(err)
		}
		for _, d := range defs {
			fmt.Printf("%s\t%s\n", d.Target, d.File)
		}
	case "check":
		defs, err := enumerate(root)
		if err != nil {
			fail(err)
		}
		m, err := loadManifest(root)
		if err != nil {
			fail(err)
		}
		missing, stale, err := reconcile(defs, m)
		if err != nil {
			fail(err)
		}
		bad := false
		for _, t := range missing {
			fmt.Fprintf(os.Stderr, "NEGATIVE CONTROL UNCOVERED: %s is defined and the pull-request leg does not run it; add it to a group in %s, or to the exclusion list with a reason\n", t, manifestPath)
			bad = true
		}
		for _, t := range stale {
			fmt.Fprintf(os.Stderr, "NEGATIVE CONTROL STALE: %s is named in %s and no makefile defines it\n", t, manifestPath)
			bad = true
		}
		for _, e := range m.Skipped {
			if strings.TrimSpace(e.Reason) == "" {
				fmt.Fprintf(os.Stderr, "NEGATIVE CONTROL EXCLUDED WITHOUT A REASON: %s\n", e.Target)
				bad = true
			}
		}
		if bad {
			os.Exit(1)
		}
		fmt.Printf("every one of the %d negative controls is in a group or in an explained exclusion\n", len(defs))
	case "matrix":
		m, err := loadManifest(root)
		if err != nil {
			fail(err)
		}
		out, err := m.matrix()
		if err != nil {
			fail(err)
		}
		fmt.Println(string(out))
	case "targets":
		if len(os.Args) != 3 {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		m, err := loadManifest(root)
		if err != nil {
			fail(err)
		}
		targets, err := m.targetsOf(os.Args[2])
		if err != nil {
			fail(err)
		}
		fmt.Println(targets)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
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
	fmt.Fprintln(os.Stderr, "negativecontrols:", err)
	os.Exit(1)
}
