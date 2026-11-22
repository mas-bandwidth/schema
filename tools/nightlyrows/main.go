// Command nightlyrows prints, as the JSON array a matrix include reads, the
// rows of .github/nightly-fast-rows.txt a diff selects (internal/nightlyrows).
//
//	go run ./tools/nightlyrows -files diff-files.txt -legs "ruby lua" -broad no
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
	flag.Parse()
	if *filesPath == "" || (*broad != "yes" && *broad != "no") {
		fmt.Fprintln(os.Stderr, "nightlyrows: -files and -broad yes|no are required")
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
	raw, err := os.ReadFile(*filesPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nightlyrows:", err)
		os.Exit(1)
	}
	var files []string
	for _, l := range strings.Split(string(raw), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	type out struct {
		Name string `json:"name"`
		Leg  string `json:"leg"`
		Run  string `json:"run"`
	}
	picked := []out{}
	for _, r := range nightlyrows.Select(rows, files, strings.Fields(*legs), *broad == "yes") {
		picked = append(picked, out{r.Name, r.Leg, r.Run})
	}
	b, err := json.Marshal(picked)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nightlyrows:", err)
		os.Exit(1)
	}
	fmt.Println(string(b))
}
