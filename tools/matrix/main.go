package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const usage = `usage: go run ./tools/matrix <command>

  page     print the generated page to stdout
  write    write docs/MATRIX.md from docs/matrix.json
  check    exit nonzero when the committed page differs from the manifest
  states   print the state line per language, in manifest order (release notes)
  proof    compute the 3.0.0 proof table from conformance results:
             -results <file|dir>  the harness's --results file, or a directory of per-leg files
             -write               rewrite docs/proof.json and docs/MATRIX.md from it
             -report <file>       write the release report (the commit, the red cells)
             -commit <sha>        the commit the report is for
             -cell <row/lang>     print one cell and exit 0 only if it is proven
           without -write it fails when docs/proof.json is stale or any cell is red,
           printing the red list
  release-check -report <file> -commit <sha>
           the release lock: refuse unless the report is green at that exact commit
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
	case "proof":
		os.Exit(proofCommand(root, os.Args[2:]))
	case "release-check":
		os.Exit(releaseCheckCommand(os.Args[2:]))
	}
	m, err := LoadManifest(root)
	if err != nil {
		fail(err)
	}
	switch os.Args[1] {
	case "page":
		fmt.Print(page(root, m))
	case "write":
		if err := os.WriteFile(filepath.Join(root, pagePath), []byte(page(root, m)), 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("wrote %s from %s\n", pagePath, manifestPath)
	case "check":
		want := page(root, m)
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

// page is the whole generated page: the manifest's tables and, when the tree
// carries the snapshot, the proof section from docs/proof.json.
func page(root string, m Manifest) string {
	p, err := LoadProof(root)
	if err != nil {
		fail(err)
	}
	return RenderPage(m, p)
}

func proofCommand(root string, args []string) int {
	fs := flag.NewFlagSet("proof", flag.ExitOnError)
	results := fs.String("results", "build/conformance/results.json", "the harness's --results file, or a directory of per-leg files")
	write := fs.Bool("write", false, "rewrite docs/proof.json and docs/MATRIX.md")
	report := fs.String("report", "", "write the release report here")
	commit := fs.String("commit", "", "the commit the report is for")
	cell := fs.String("cell", "", "print one cell, row/lang, and exit 0 only if it is proven")
	_ = fs.Parse(args)
	r, err := LoadResults(*results)
	if err != nil {
		fail(err)
	}
	in, err := LoadInputs(root, r)
	if err != nil {
		fail(err)
	}
	p := Compute(in)
	if *cell != "" {
		i := strings.LastIndex(*cell, "/")
		if i < 0 {
			fail(fmt.Errorf("-cell takes row/lang"))
		}
		c, ok := p.Cell((*cell)[:i], (*cell)[i+1:])
		if !ok {
			fail(fmt.Errorf("no cell %s", *cell))
		}
		fmt.Printf("%s / %s: %s %s%s\n", c.Row, c.Lang, c.Status, c.Why, c.Gate)
		if c.Status != StatusProven {
			return 1
		}
		return 0
	}
	if *write {
		if err := os.WriteFile(filepath.Join(root, proofPath), MarshalProof(p), 0o644); err != nil {
			fail(err)
		}
		m, err := LoadManifest(root)
		if err != nil {
			fail(err)
		}
		if err := os.WriteFile(filepath.Join(root, pagePath), []byte(RenderPage(m, &p)), 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("wrote %s and %s: %d of %d cells red\n", proofPath, pagePath, len(p.Red()), len(p.Cells))
		return 0
	}
	stale := ""
	committed, err := os.ReadFile(filepath.Join(root, proofPath))
	if err != nil || string(committed) != string(MarshalProof(p)) {
		stale = proofPath + " is stale: it is not the proof these results compute; run `go run ./tools/matrix proof -write` and commit it"
	}
	if *report != "" {
		rep := Report{Commit: *commit, Cells: len(p.Cells), Red: p.Red(), Stale: stale}
		data, _ := json.MarshalIndent(rep, "", "  ")
		if err := os.MkdirAll(filepath.Dir(*report), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(*report, append(data, '\n'), 0o644); err != nil {
			fail(err)
		}
	}
	fmt.Print(RedList(p))
	code := 0
	if stale != "" {
		fmt.Fprintln(os.Stderr, "proof:", stale)
		code = 1
	}
	if red := len(p.Red()); red > 0 {
		fmt.Fprintf(os.Stderr, "proof: RED, %d of %d cells (listed above)\n", red, len(p.Cells))
		code = 1
	} else {
		fmt.Printf("proof: green, %d cells\n", len(p.Cells))
	}
	return code
}

func releaseCheckCommand(args []string) int {
	fs := flag.NewFlagSet("release-check", flag.ExitOnError)
	report := fs.String("report", "", "the proof report the ci-full proof job wrote at the commit")
	commit := fs.String("commit", "", "the commit being released")
	_ = fs.Parse(args)
	if *report == "" {
		fmt.Fprintln(os.Stderr, "release REFUSES: no proof report (PROOF=<file>): the release needs the ci-full proof job's report at the commit")
		return 1
	}
	r, err := LoadReport(*report)
	if err != nil {
		fmt.Fprintln(os.Stderr, "release REFUSES: the proof report cannot be read:", err)
		return 1
	}
	if err := VerifyRelease(r, *commit); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("proof: green at %s, %d cells\n", *commit, r.Cells)
	return 0
}
