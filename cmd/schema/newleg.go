package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// parseNewLegArgs accepts flags before or after the language name, so
// `schema new-leg lua --root .` is one command with `schema new-leg --root . lua`.
func parseNewLegArgs(args []string) (lang, root string, verbose bool, err error) {
	root = "."
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--verbose" || a == "-verbose":
			verbose = true
		case a == "--root" || a == "-root":
			if i+1 >= len(args) {
				return "", "", false, fmt.Errorf("new-leg --root needs a directory")
			}
			i++
			root = args[i]
		case strings.HasPrefix(a, "--root="):
			root = strings.TrimPrefix(a, "--root=")
		case a == "--":
			pos = append(pos, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(a, "-"):
			return "", "", false, fmt.Errorf("new-leg: unknown flag %s", a)
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) != 1 {
		return "", "", false, fmt.Errorf("new-leg needs the language name: schema new-leg lua")
	}
	return pos[0], root, verbose, nil
}

// writeNewLeg writes compiler.NewLeg's files under root. It refuses to
// overwrite, and it refuses a directory that is not a schema tree, so a
// misplaced invocation cannot scatter a language leg into an unrelated
// folder. fmt remains the only command that writes a .schema file.
func writeNewLeg(root string, files map[string][]byte, verbose bool) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("new-leg --root %s is not a directory", root)
	}
	for _, need := range []string{"Makefile", "compiler", "make", "internal/codegen"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(need))); err != nil {
			return fmt.Errorf("new-leg writes a schema language leg: %s is not a schema tree (missing %s)", root, need)
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		if !filepath.IsLocal(name) || strings.Contains(name, "\\") {
			return fmt.Errorf("new-leg path %q is not a local path", name)
		}
		if strings.HasSuffix(name, ".schema") {
			return fmt.Errorf("new-leg must not write a .schema file (%s); fmt is the only command that writes one", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(root, filepath.FromSlash(name))
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("new-leg refuses to overwrite %s", path)
		}
	}
	for _, name := range names {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if compiler.NewLegExec(name) {
			mode = 0o755
		}
		if err := os.WriteFile(path, files[name], mode); err != nil {
			return err
		}
		if verbose {
			fmt.Printf("wrote %s\n", path)
		}
	}
	return nil
}
