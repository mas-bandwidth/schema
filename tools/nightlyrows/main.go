// Command nightlyrows prints, as the JSON array a matrix include reads, the
// rows of .github/nightly-fast-rows.txt a diff selects (internal/nightlyrows).
//
//	go run ./tools/nightlyrows -files diff-files.txt -legs "ruby lua" -broad no
//
// With -all it prints every row of the -rows registry instead.
//
//	go run ./tools/nightlyrows -all -rows .github/nightly-fast-rows.txt
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/nightlyrows"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// refuse prints a refusal on stderr and returns its exit status. A refusal
// whose text cannot be written still fails, with status 3, so the cause is
// never lost to a broken stderr behind a status that looks the same.
func refuse(stderr io.Writer, status int, format string, args ...any) int {
	if _, err := fmt.Fprintf(stderr, format, args...); err != nil {
		return 3
	}
	return status
}

// run is the command: its exit status, the matrix on stdout, every refusal
// named on stderr.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nightlyrows", flag.ContinueOnError)
	fs.SetOutput(stderr)
	registry := fs.String("rows", nightlyrows.RegistryPath, "the registry")
	filesPath := fs.String("files", "", "a file naming the changed paths, one per line (required)")
	legs := fs.String("legs", "", "the legs the diff touched, space-separated")
	broad := fs.String("broad", "", "yes or no: the plan broad rule (required)")
	all := fs.Bool("all", false, "print every row of the registry; -files, -legs and -broad are refused")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		return refuse(stderr, 2, "nightlyrows: unexpected arguments %q\n", fs.Args())
	}
	if *all && (*filesPath != "" || *legs != "" || *broad != "") {
		return refuse(stderr, 2, "nightlyrows: -all takes every row; -files, -legs and -broad select some, so they are refused with it\n")
	}
	if !*all && (*filesPath == "" || (*broad != "yes" && *broad != "no")) {
		return refuse(stderr, 2, "nightlyrows: -files and -broad yes|no are required (or -all)\n")
	}
	f, err := os.Open(*registry)
	if err != nil {
		return refuse(stderr, 1, "nightlyrows: %v\n", err)
	}
	rows, err := nightlyrows.Parse(f)
	_ = f.Close()
	if err != nil {
		return refuse(stderr, 1, "nightlyrows: %s: %v\n", *registry, err)
	}
	selected := rows
	if !*all {
		raw, err := os.ReadFile(*filesPath)
		if err != nil {
			return refuse(stderr, 1, "nightlyrows: %v\n", err)
		}
		var files []string
		for l := range strings.SplitSeq(string(raw), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				files = append(files, l)
			}
		}
		selected = nightlyrows.Select(rows, files, strings.Fields(*legs), *broad == "yes")
	}
	type out struct {
		Name string `json:"name"`
		Leg  string `json:"leg"`
		Run  string `json:"run"`
	}
	picked := []out{}
	for _, r := range selected {
		picked = append(picked, out{r.Name, r.Leg, r.Run})
	}
	b, err := json.Marshal(picked)
	if err != nil {
		return refuse(stderr, 1, "nightlyrows: %v\n", err)
	}
	if _, err := fmt.Fprintln(stdout, string(b)); err != nil {
		return refuse(stderr, 1, "nightlyrows: the matrix could not be written: %v\n", err)
	}
	return 0
}
