// Command nightlyrows prints, as the JSON array a matrix include reads, the
// rows of .github/nightly-fast-rows.txt a diff selects (internal/nightlyrows).
//
//	go run ./tools/nightlyrows -files diff-files.txt -legs "ruby lua" -broad no
//
// With -all it prints every row of the -rows registry instead: certify.yml's
// nightly-gates job runs all of .github/nightly-gates.txt that way.
//
//	go run ./tools/nightlyrows -all -rows .github/nightly-gates.txt
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
		fmt.Fprintf(stderr, "nightlyrows: unexpected arguments %q\n", fs.Args())
		return 2
	}
	if *all && (*filesPath != "" || *legs != "" || *broad != "") {
		fmt.Fprintln(stderr, "nightlyrows: -all takes every row; -files, -legs and -broad select some, so they are refused with it")
		return 2
	}
	if !*all && (*filesPath == "" || (*broad != "yes" && *broad != "no")) {
		fmt.Fprintln(stderr, "nightlyrows: -files and -broad yes|no are required (or -all)")
		return 2
	}
	f, err := os.Open(*registry)
	if err != nil {
		fmt.Fprintln(stderr, "nightlyrows:", err)
		return 1
	}
	rows, err := nightlyrows.Parse(f)
	_ = f.Close()
	if err != nil {
		fmt.Fprintf(stderr, "nightlyrows: %s: %v\n", *registry, err)
		return 1
	}
	selected := rows
	if !*all {
		raw, err := os.ReadFile(*filesPath)
		if err != nil {
			fmt.Fprintln(stderr, "nightlyrows:", err)
			return 1
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
		fmt.Fprintln(stderr, "nightlyrows:", err)
		return 1
	}
	fmt.Fprintln(stdout, string(b))
	return 0
}
