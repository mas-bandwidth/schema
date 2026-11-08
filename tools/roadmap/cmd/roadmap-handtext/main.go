// Command roadmap-handtext gates ROADMAP.md's hand-written text.
//
//	roadmap-handtext -page ROADMAP.md
//	    every nova-work block holds only a table and its source link
//	roadmap-handtext -before OLD -after NEW -author NAME
//	    NEW changes nothing outside the markers, unless NAME is Glenn Fiedler
//	roadmap-handtext -base REV [-head REV]
//	    every non-merge commit in REV..HEAD touching ROADMAP.md passes the
//	    -before/-after check against its parent
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mas-bandwidth/schema/v2/tools/roadmap"
)

func main() {
	page := flag.String("page", "", "check that every block of this page holds only a table")
	before := flag.String("before", "", "the page before a change")
	after := flag.String("after", "", "the page after a change")
	author := flag.String("author", "", "the author of the change")
	base := flag.String("base", "", "check every commit in base..head that touches ROADMAP.md")
	head := flag.String("head", "HEAD", "the head of the range for -base")
	flag.Parse()

	var err error
	switch {
	case *page != "":
		var raw []byte
		if raw, err = os.ReadFile(*page); err == nil {
			err = roadmap.CheckBlocksHoldOnlyTables(string(raw))
		}
	case *before != "" && *after != "":
		var b, a []byte
		if b, err = os.ReadFile(*before); err == nil {
			if a, err = os.ReadFile(*after); err == nil {
				err = roadmap.CheckHandTextChange(*after, *author, string(b), string(a))
			}
		}
	case *base != "":
		err = checkRange(*base, *head)
	default:
		flag.Usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "roadmap-handtext REFUSES:", err)
		os.Exit(1)
	}
	fmt.Println("OK: roadmap-handtext")
}

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, exit.Stderr)
	}
	return string(out), err
}

// pageAt is ROADMAP.md at rev, or "" where it does not exist.
func pageAt(rev string) string {
	out, err := git("show", rev+":ROADMAP.md")
	if err != nil {
		return ""
	}
	return out
}

func checkRange(base, head string) error {
	log, err := git("log", "--no-merges", "--format=%H%x1f%an", base+".."+head, "--", "ROADMAP.md")
	if err != nil {
		return err
	}
	commits := 0
	for line := range strings.SplitSeq(strings.TrimSpace(log), "\n") {
		if line == "" {
			continue
		}
		hash, author, _ := strings.Cut(line, "\x1f")
		commits++
		if err := roadmap.CheckHandTextChange(hash[:12], author, pageAt(hash+"^"), pageAt(hash)); err != nil {
			return err
		}
	}
	fmt.Printf("checked %d commit(s) in %s..%s touching ROADMAP.md\n", commits, base, head)
	return nil
}
