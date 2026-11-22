// Command inputfamilies is the fast lane's input-reader selection
// (internal/inputfamilies).
//
//	inputfamilies select -changed FILE [-all] [-pr]
//	inputfamilies check
//
// select reads the changed paths (one per line) and prints, for
// $GITHUB_OUTPUT, the families they touch, one row per reader leg of each
// (input_rows, a JSON array of {leg, family, target}) and the Go packages
// that name a changed input (input_packages). A changed file under
// testdata/ that nothing is known to read is a ::warning naming it. check
// refuses a family with no inputs, no reader, or a driver that never runs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/inputfamilies"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: inputfamilies select -changed FILE [-all] [-pr] | check")
		return 2
	}
	t, err := inputfamilies.Load(".")
	if err != nil {
		fmt.Fprintf(stderr, "inputfamilies: %v\n", err)
		return 2
	}
	switch args[0] {
	case "check":
		bad := inputfamilies.Check(t)
		for _, b := range bad {
			fmt.Fprintf(stderr, "inputfamilies: %s\n", b)
		}
		if len(bad) > 0 {
			return 1
		}
		fmt.Fprintf(stdout, "inputfamilies: %d families, every input present, every driver runs\n", len(inputfamilies.Families))
		return 0
	case "select":
		fs := flag.NewFlagSet("select", flag.ContinueOnError)
		fs.SetOutput(stderr)
		changed := fs.String("changed", "", "file of changed paths, one per line")
		all := fs.Bool("all", false, "select every family (a push to main)")
		pr := fs.Bool("pr", false, "keep only the families that run in the pull-request tier")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		var paths []string
		if *changed != "" {
			b, err := os.ReadFile(*changed)
			if err != nil {
				fmt.Fprintf(stderr, "inputfamilies: %v\n", err)
				return 2
			}
			for _, l := range strings.Split(string(b), "\n") {
				if l = strings.TrimSpace(l); l != "" {
					paths = append(paths, l)
				}
			}
		} else if !*all {
			fmt.Fprintln(stderr, "inputfamilies select: -changed or -all is required")
			return 2
		}
		s := inputfamilies.Select(t, paths, *all, *pr)
		rows := s.Rows
		if rows == nil {
			rows = []inputfamilies.Row{}
		}
		j, _ := json.Marshal(rows)
		fmt.Fprintf(stdout, "families=%s\n", strings.Join(s.Families, " "))
		fmt.Fprintf(stdout, "input_rows=%s\n", j)
		fmt.Fprintf(stdout, "input_packages=%s\n", strings.Join(s.Packages, " "))
		for _, u := range s.Unread {
			fmt.Fprintf(stderr, "::warning::%s changed and no input family or Go file names it: nothing is known to read it\n", u)
		}
		return 0
	}
	fmt.Fprintf(stderr, "inputfamilies: unknown command %q\n", args[0])
	return 2
}
