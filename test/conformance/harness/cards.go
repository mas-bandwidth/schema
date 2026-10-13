// `harness cards` — the card conformance check (tables/cards/conformance/FORMAT.md).
//
// It is the concrete side of tla/CardHops.tla: ImplementationsAgree and
// HopOrderIrrelevant say every language's leg reads, writes, renders, upgrades
// and hashes one card to the same bytes, and this check compares those bytes. It
// writes the derived manifest (the committed one, which already carries no
// expected answer) into build/, runs a driver over each surface into a fresh
// outdir, and compares every answer byte for byte against expected/. A driver is
// a command, and the comparison takes the driver as a Go function value
// (manifest, surface, outdir) returning its exit code, so a unit test drives an
// in-process fake with no subprocess.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// cardSurfaces is the nine card surfaces, in the order the check prints them.
var cardSurfaces = []string{"load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view"}

// cardCase is one manifest line: a surface and a case name. The input and edit
// are the driver's, never the check's.
type cardCase struct {
	surface string
	name    string
}

// cardsDriverFn runs one surface and returns the driver's exit code (0 ran,
// 2 absent, anything else failed). os/exec wraps it in runCards; a test wraps a
// function that writes into a temp dir.
type cardsDriverFn func(manifest, surface, outdir string) int

// cardResult is one surface's verdict.
type cardResult struct {
	pass, total, missing int
	absent               bool
	failures             []string
}

// runCards runs the driver over every surface in the corpus and compares
// against expected/. It returns false when any required surface fails or is
// answered absent, and when zero cases run.
func runCards(w io.Writer, corpus, expected, driverCmd, require string, work string, driverFn cardsDriverFn) (bool, error) {
	cases, err := readCardCases(filepath.Join(corpus, "MANIFEST.txt"))
	if err != nil {
		return false, err
	}
	required := requireSet(require)
	derived := filepath.Join(work, "manifest.txt")
	if err := writeFileAtomic(derived, manifestBytes(filepath.Join(corpus, "MANIFEST.txt"))); err != nil {
		return false, err
	}

	bySurface := map[string][]cardCase{}
	for _, c := range cases {
		bySurface[c.surface] = append(bySurface[c.surface], c)
	}

	driver := driverFn
	if driver == nil {
		driver = execCardsDriver(driverCmd)
	}

	results := map[string]*cardResult{}
	ran := 0
	for _, s := range cardSurfaces {
		cs := bySurface[s]
		r := &cardResult{total: len(cs)}
		results[s] = r
		if len(cs) == 0 {
			continue
		}
		ran += len(cs)
		outdir := filepath.Join(work, "out", s)
		if err := os.RemoveAll(outdir); err != nil {
			return false, err
		}
		if err := os.MkdirAll(outdir, 0o755); err != nil {
			return false, err
		}
		code := driver(derived, s, outdir)
		switch {
		case code == 2:
			r.absent = true
			if required[s] {
				r.failures = append(r.failures, "the driver answered the required surface absent (exit 2)")
			}
		case code != 0:
			r.failures = append(r.failures, fmt.Sprintf("the driver exited %d", code))
		default:
			for _, c := range cs {
				if _, err := os.Stat(filepath.Join(outdir, c.name+".absent")); err == nil {
					r.missing++
					continue
				}
				outs, err := cardOutputs(s, c.name, expected)
				if err != nil {
					return false, err
				}
				ok := true
				for _, o := range outs {
					want, err := os.ReadFile(filepath.Join(expected, o))
					if err != nil {
						return false, err
					}
					got, err := os.ReadFile(filepath.Join(outdir, o))
					if err != nil {
						r.failures = append(r.failures, fmt.Sprintf("%s: the driver wrote nothing", o))
						ok = false
						break
					}
					if !bytes.Equal(got, want) {
						r.failures = append(r.failures, describeDiff(o, want, got))
						ok = false
						break
					}
				}
				if ok {
					r.pass++
				}
			}
		}
		if required[s] && r.missing > 0 {
			r.failures = append(r.failures, fmt.Sprintf("%d case(s) answered absent on a required surface", r.missing))
		}
	}

	ok := printCards(w, results, required)
	if ran == 0 {
		return false, fmt.Errorf("the card corpus names no cases")
	}
	return ok, nil
}

// manifestBytes returns the committed manifest, which already carries no
// expected answer: the check copies it into build/ as the derived manifest a
// driver reads (test/conformance/README.md, "The shape").
func manifestBytes(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		panic(err) // the corpus manifest is committed; a missing one is a broken tree
	}
	return b
}

// readCardCases reads the committed manifest's surface/name pairs.
func readCardCases(path string) ([]cardCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []cardCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("malformed manifest line %q", line)
		}
		out = append(out, cardCase{surface: fields[0], name: fields[1]})
	}
	return out, sc.Err()
}

// cardOutputs names the files a case's surface owes, read from expected/: a
// refused case owes only <name>.refused; a success owes its answer files.
func cardOutputs(surface, name, expectedDir string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(expectedDir, name+".refused")); err == nil {
		return []string{name + ".refused"}, nil
	}
	switch surface {
	case "load":
		return []string{name + ".json", name + ".report"}, nil
	case "upgrade":
		return []string{name + ".wire", name + ".report"}, nil
	case "save", "bounds", "parse", "edit":
		return []string{name + ".wire"}, nil
	case "render":
		return []string{name + ".txt"}, nil
	case "taskhash":
		return []string{name + ".hex"}, nil
	case "view":
		return []string{"view.txt"}, nil
	}
	return nil, fmt.Errorf("%q is not a card surface", surface)
}

// requireSet parses --require into the set of surfaces an absence fails.
func requireSet(list string) map[string]bool {
	out := map[string]bool{}
	for _, s := range strings.Split(list, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out[s] = true
		}
	}
	return out
}

// execCardsDriver wraps os/exec around the driver command.
func execCardsDriver(command string) cardsDriverFn {
	return func(manifest, surface, outdir string) int {
		argv := append(strings.Fields(command), manifest, surface, outdir)
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		if err := cmd.Run(); err == nil {
			return 0
		} else if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		return -1
	}
}

// printCards prints one line per surface and returns the verdict. The line is
// `cards <surface> pass n/m +ka`, or FAIL with the first differing case and
// byte offset, or absent. A required surface answered absent is a failure; a
// non-required one is printed and not.
func printCards(w io.Writer, results map[string]*cardResult, required map[string]bool) bool {
	ok := true
	for _, s := range cardSurfaces {
		r := results[s]
		if r.total == 0 {
			continue
		}
		switch {
		case len(r.failures) > 0:
			fmt.Fprintf(w, "cards %s FAIL: %s\n", s, r.failures[0])
			ok = false
		case r.absent:
			fmt.Fprintf(w, "cards %s absent\n", s)
			if required[s] {
				ok = false
			}
		default:
			m := r.total - r.missing
			line := fmt.Sprintf("cards %s pass %d/%d", s, r.pass, m)
			if r.missing > 0 {
				line += fmt.Sprintf(" +%da", r.missing)
			}
			fmt.Fprintln(w, line)
		}
	}
	return ok
}
