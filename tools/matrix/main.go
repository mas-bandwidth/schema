package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const usage = `usage: go run ./tools/matrix <command>

  page     print the generated page to stdout
  write    write docs/MATRIX.md from docs/matrix.json
  check    exit nonzero when the committed page differs from the manifest
  states   print the state line per language, in manifest order (release notes)
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
	m, err := LoadManifest(root)
	if err != nil {
		fail(err)
	}
	switch os.Args[1] {
	case "page":
		fmt.Print(Render(m))
	case "write":
		if err := os.WriteFile(filepath.Join(root, pagePath), []byte(Render(m)), 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("wrote %s from %s\n", pagePath, manifestPath)
	case "check":
		want := Render(m)
		got, err := os.ReadFile(filepath.Join(root, pagePath))
		if err != nil {
			fail(fmt.Errorf("%s is missing; run `go run ./tools/matrix write`: %w", pagePath, err))
		}
		if string(got) != want {
			fail(fmt.Errorf("%s is stale; run `go run ./tools/matrix write`", pagePath))
		}
		fmt.Printf("%s is in step with %s\n", pagePath, manifestPath)
	case "states":
		fmt.Print(StateLines(m))
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

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
	fmt.Fprintln(os.Stderr, "matrix:", err)
	os.Exit(1)
}
