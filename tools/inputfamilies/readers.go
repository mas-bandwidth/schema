package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/inputfamilies"
)

// legReaders is `inputfamilies readers -changed FILE`: the legs whose make
// targets, or whose verify-leg rows' steps, read a changed path
// (internal/inputfamilies TreeReaders), printed as reader_legs=<leg>... for
// the plan, with each changed path's readers and why named on stderr.
func legReaders(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("readers", flag.ContinueOnError)
	fs.SetOutput(stderr)
	changed := fs.String("changed", "", "file of changed paths, one per line")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *changed == "" {
		_, _ = fmt.Fprintln(stderr, "inputfamilies readers: -changed is required")
		return 2
	}
	b, err := os.ReadFile(*changed)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "inputfamilies:", err)
		return 2
	}
	root, err := filepath.Abs(".")
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "inputfamilies:", err)
		return 2
	}
	readers, err := inputfamilies.TreeReaders(root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "inputfamilies:", err)
		return 1
	}
	got := map[string]bool{}
	for l := range strings.SplitSeq(string(b), "\n") {
		if l = strings.TrimSpace(l); l == "" {
			continue
		}
		for _, w := range readers.Why(l) {
			_, _ = fmt.Fprintf(stderr, "inputfamilies readers: %s: %s\n", l, w)
		}
		for _, leg := range readers.Of(l) {
			got[leg] = true
		}
	}
	var legs []string
	for _, l := range inputfamilies.Legs {
		if got[l] {
			legs = append(legs, l)
		}
	}
	_, _ = fmt.Fprintf(stdout, "reader_legs=%s\n", strings.Join(legs, " "))
	return 0
}
