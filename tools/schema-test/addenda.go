package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// addendaCmd writes the spec addenda from the registry (internal/testreg,
// addenda.go): one page per tier and the adapter-contract page, under docs/.
// A page in the addenda's place that the registry no longer generates is
// removed. With --check it writes nothing and fails, naming each page, when
// the committed pages are not what the registry generates.
func addendaCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("schema-test addenda", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("registry", testreg.DefaultPath, "")
	root := fs.String("root", ".", "")
	check := fs.Bool("check", false, "")
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Sprintf("addenda takes no names (got %q)", fs.Args())}
	}
	reg, err := testreg.Load(*path)
	if err != nil {
		return err
	}
	if *check {
		if err := testreg.AddendaDrift(reg, os.DirFS(*root)); err != nil {
			return err
		}
		_, err := fmt.Fprintf(stdout, "schema-test addenda: the spec addenda under %s are what %s generates\n", filepath.Join(*root, testreg.AddendaDir), *path)
		return err
	}
	files, err := testreg.Addenda(reg)
	if err != nil {
		return err
	}
	stale, err := testreg.StaleAddenda(os.DirFS(*root), files)
	if err != nil {
		return err
	}
	at := func(p string) string { return filepath.Join(*root, filepath.FromSlash(p)) }
	return writeAddenda(stdout, files, stale,
		func(p string, data []byte) error { return os.WriteFile(at(p), data, 0o644) },
		func(p string) error { return os.Remove(at(p)) })
}

// writeAddenda removes the stale pages and writes the generated ones through
// write and remove, saying each; the first failure stops it, naming the page.
func writeAddenda(stdout io.Writer, files []testreg.File, stale []string, write func(string, []byte) error, remove func(string) error) error {
	for _, p := range stale {
		if err := remove(p); err != nil {
			return fmt.Errorf("removing %s: %w", p, err)
		}
		if _, err := fmt.Fprintf(stdout, "removed %s\n", p); err != nil {
			return err
		}
	}
	for _, f := range files {
		if err := write(f.Path, f.Data); err != nil {
			return fmt.Errorf("writing %s: %w", f.Path, err)
		}
		if _, err := fmt.Fprintf(stdout, "wrote %s\n", f.Path); err != nil {
			return err
		}
	}
	return nil
}
