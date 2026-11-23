package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// siblingRuntime is a leg's sibling runtime checkout: the directory `make
// deps` stages beside the worktree, the environment variable that names
// another one, and a file that proves the checkout is there.
type siblingRuntime struct {
	env, dir, marker string
}

// siblingRuntimes is every leg that compiles or runs against a sibling. A leg
// absent from it needs none (the scripting legs carry their runtime inside
// the generated tree).
var siblingRuntimes = map[string]siblingRuntime{
	"cpp":   {"SERIALIZE", "../serialize", "serialize.h"},
	"c":     {"SERIALIZE_C", "../serialize.c", "serialize.c"},
	"cs":    {"SERIALIZE_CS", "../serialize.cs", "src/Serialize.cs"},
	"unity": {"SERIALIZE_CS", "../serialize.cs", "src/Serialize.cs"},
	"go":    {"SERIALIZE_GO", "../serialize.go", "go.mod"},
	"rust":  {"SERIALIZE_RS", "../serialize.rs", "Cargo.toml"},
	"js":    {"SERIALIZE_JS", "../serialize.js", "src/index.js"},
}

// requireSiblings stops a run at once, with one line naming the command to
// run, when a leg it is about to exercise has no sibling runtime checkout. Left
// to itself that leg fails in the middle of a compile (CS2001, a missing
// include, a cargo path) and the message names a symptom, never the cause.
func requireSiblings(langs []string) error {
	for _, lang := range langs {
		s, ok := siblingRuntimes[lang]
		if !ok {
			continue
		}
		dir := s.dir
		if v := os.Getenv(s.env); v != "" {
			dir = v
		}
		dir, err := fromRepoRoot(dir)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(dir, s.marker)); err != nil {
			return fmt.Errorf("the %s leg needs the sibling checkout %s (%s is missing): run 'make deps'", lang, dir, s.marker)
		}
	}
	return nil
}

// fromRepoRoot resolves a relative sibling path against the repository root
// (the directory holding go.mod, found by walking up from the working
// directory). make deps stages the siblings beside the repository root, so a
// relative path is read from there, never from the directory the test or the
// harness was started in. An absolute path is returned unchanged.
func fromRepoRoot(dir string) (string, error) {
	if filepath.IsAbs(dir) {
		return dir, nil
	}
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return filepath.Join(root, dir), nil
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", fmt.Errorf("no go.mod above the working directory: cannot find the repository root to resolve the sibling checkout %s", dir)
		}
		root = parent
	}
}
