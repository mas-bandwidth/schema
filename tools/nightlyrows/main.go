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
	"os"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/nightlyrows"
)

func main() {
	registry := flag.String("rows", nightlyrows.RegistryPath, "the registry")
	filesPath := flag.String("files", "", "a file naming the changed paths, one per line (required)")
	legs := flag.String("legs", "", "the legs the diff touched, space-separated")
	broad := flag.String("broad", "", "yes or no: the plan broad rule (required)")
	all := flag.Bool("all", false, "print every row of the registry; -files, -legs and -broad are refused")
	flag.Parse()
	if *all && (*filesPath != "" || *legs != "" || *broad != "") {
		fmt.Fprintln(os.Stderr, "nightlyrows: -all takes every row; -files, -legs and -broad select some, so they are refused with it")
		os.Exit(2)
	}
	if !*all && (*filesPath == "" || (*broad != "yes" && *broad != "no")) {
		fmt.Fprintln(os.Stderr, "nightlyrows: -files and -broad yes|no are required (or -all)")
		os.Exit(2)
	}
	f, err := os.Open(*registry)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nightlyrows:", err)
		os.Exit(1)
	}
	rows, err := nightlyrows.Parse(f)
	_ = f.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "nightlyrows:", err)
		os.Exit(1)
	}
	selected := rows
	if !*all {
		raw, err := os.ReadFile(*filesPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "nightlyrows:", err)
			os.Exit(1)
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
		fmt.Fprintln(os.Stderr, "nightlyrows:", err)
		os.Exit(1)
	}
	fmt.Println(string(b))
}
