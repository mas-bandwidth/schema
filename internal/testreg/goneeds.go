package testreg

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// GoNeedsPath declares the builds a Go test reads and does not make itself:
// one line "needs ./<package> <top-level test regexp> <make target>" each.
// The regexp matches the whole test name. Whatever runs such a test builds
// the target first: the gotest adapter and the union harness in Prepare
// (Preparer), and tools/fullrun before a Go test the registry does not hold
// yet. What the test reads is then built before it runs, never left to
// whatever ran in the checkout before it.
const GoNeedsPath = "test/registry/go-needs.txt"

// GoNeed is one line of GoNeedsPath.
type GoNeed struct {
	Package string
	Test    *regexp.Regexp
	Target  string
}

// LoadGoNeeds reads a needs table. A line that is not a needs line, or whose
// regexp does not compile, is an error naming the line.
func LoadGoNeeds(path string) ([]GoNeed, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []GoNeed
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 4 || fields[0] != "needs" || !strings.HasPrefix(fields[1], "./") {
			return nil, fmt.Errorf("%s:%d: want \"needs ./<package> <test regexp> <make target>\", got %q", path, n, line)
		}
		re, err := regexp.Compile("^(?:" + fields[2] + ")$")
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		out = append(out, GoNeed{Package: fields[1], Test: re, Target: fields[3]})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// GoNeedsOf is the make targets the Go test pkg/test reads, sorted.
func GoNeedsOf(needs []GoNeed, pkg, test string) []string {
	set := map[string]bool{}
	for _, n := range needs {
		if n.Package == pkg && n.Test.MatchString(test) {
			set[n.Target] = true
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
