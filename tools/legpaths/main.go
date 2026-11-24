// Command legpaths prints the legs a list of changed paths selects
// (internal/legpaths): ci-fast.yml's plan and ci-full.yml's pr-select read
// their path-to-leg selection from it.
//
//	go run ./tools/legpaths [-root .] [-explain] -changed <file|->
//
// It prints the selected legs on one line, space-separated, in
// inputfamilies.Legs order (an empty line when none). -explain prints each
// path and its legs to stderr. A path list it cannot read is exit 2.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/legpaths"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("legpaths", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "the repository root")
	changed := fs.String("changed", "", "the changed paths, one per line (- for standard input)")
	explain := fs.Bool("explain", false, "print each path and the legs it selects to standard error")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *changed == "" || fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: legpaths [-root .] [-explain] -changed <file|->")
		return 2
	}
	in := stdin
	if *changed != "-" {
		f, err := os.Open(*changed)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "legpaths: %v\n", err)
			return 2
		}
		defer func() { _ = f.Close() }() // errdiscard: read-only; a read error is sc.Err's
		in = f
	}
	var paths []string
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		if p := strings.TrimSpace(sc.Text()); p != "" {
			paths = append(paths, p)
		}
	}
	if err := sc.Err(); err != nil {
		_, _ = fmt.Fprintf(stderr, "legpaths: %v\n", err)
		return 2
	}
	ix, err := legpaths.Load(*root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	if *explain {
		for _, p := range paths {
			_, _ = fmt.Fprintf(stderr, "%s: %s\n", p, strings.Join(ix.Legs(p), " "))
		}
	}
	if _, err := fmt.Fprintln(stdout, strings.Join(ix.Select(paths), " ")); err != nil {
		_, _ = fmt.Fprintf(stderr, "legpaths: %v\n", err)
		return 2
	}
	return 0
}
