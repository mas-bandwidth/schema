// cellmerge resolves diff3 conflict hunks in a markdown file whose three sides
// are each a one-row language table (header, separator, row), as
// docs/PORTING.md's register rows are: it merges the row cell by cell by
// column name, three-way against the base. A cell both sides changed
// differently, or a hunk that is not such a table, is left in place, and the
// exit status is 1.
//
//	git checkout --conflict=diff3 docs/PORTING.md
//	go run ./tools/cellmerge docs/PORTING.md
package main

import (
	"fmt"
	"os"
	"strings"
)

type table struct {
	cols  []string
	cells map[string]string
}

func splitRow(line string) ([]string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "| ") || !strings.HasSuffix(line, " |") {
		return nil, false
	}
	parts := strings.Split(line[2:len(line)-2], " | ")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts, true
}

func parse(lines []string, above []string) (*table, bool) {
	var nonblank []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonblank = append(nonblank, l)
		}
	}
	if len(nonblank) == 1 && len(above) >= 2 && strings.HasPrefix(above[len(above)-1], "|---") {
		nonblank = []string{above[len(above)-2], above[len(above)-1], nonblank[0]}
	}
	if len(nonblank) != 3 || !strings.HasPrefix(nonblank[1], "|---") {
		return nil, false
	}
	cols, ok := splitRow(nonblank[0])
	if !ok {
		return nil, false
	}
	row, ok := splitRow(nonblank[2])
	if !ok || len(row) != len(cols) {
		return nil, false
	}
	t := &table{cols: cols, cells: map[string]string{}}
	for i, c := range cols {
		t.cells[c] = row[i]
	}
	return t, true
}

func merge(base, ours, theirs *table) ([]string, bool) {
	cols := theirs.cols
	for _, c := range ours.cols {
		if _, ok := theirs.cells[c]; !ok {
			cols = append(cols, c)
		}
	}
	var row []string
	for _, c := range cols {
		b, inBase := base.cells[c]
		o, inOurs := ours.cells[c]
		t, inTheirs := theirs.cells[c]
		var v string
		switch {
		case !inOurs:
			v = t
		case !inTheirs:
			v = o
		case o == t:
			v = o
		case inBase && o == b:
			v = t
		case inBase && t == b:
			v = o
		default:
			return nil, false
		}
		row = append(row, v)
	}
	sep := make([]string, len(cols))
	for i := range sep {
		sep[i] = "---"
	}
	return []string{
		"| " + strings.Join(cols, " | ") + " |",
		"|" + strings.Join(sep, "|") + "|",
		"| " + strings.Join(row, " | ") + " |",
	}, true
}

// Resolve rewrites every mergeable diff3 hunk in src. It returns the new text,
// how many hunks it resolved, and the 1-based line of each hunk it left.
func Resolve(src string) (string, int, []int) {
	lines := strings.Split(src, "\n")
	var out []string
	resolved := 0
	var left []int
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "<<<<<<< ") {
			out = append(out, lines[i])
			continue
		}
		start := i
		var ours, base, theirs []string
		section := &ours
		j := i + 1
		for ; j < len(lines); j++ {
			l := lines[j]
			switch {
			case strings.HasPrefix(l, "||||||| "):
				section = &base
				continue
			case l == "=======":
				section = &theirs
				continue
			case strings.HasPrefix(l, ">>>>>>> "):
			default:
				*section = append(*section, l)
				continue
			}
			break
		}
		bt, ok1 := parse(base, out)
		ot, ok2 := parse(ours, out)
		tt, ok3 := parse(theirs, out)
		rowOnly := len(ours) == 1
		var merged []string
		ok := ok1 && ok2 && ok3
		if ok {
			merged, ok = merge(bt, ot, tt)
		}
		if ok && rowOnly {
			if len(out) < 2 || merged[0] != out[len(out)-2] {
				ok = false
			} else {
				merged = merged[2:]
			}
		}
		if ok {
			out = append(out, merged...)
			resolved++
		} else {
			end := min(j+1, len(lines))
			out = append(out, lines[start:end]...)
			left = append(left, start+1)
		}
		i = j
	}
	return strings.Join(out, "\n"), resolved, left
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/cellmerge <file with diff3 conflict hunks>")
		os.Exit(2)
	}
	path := os.Args[1]
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	text, resolved, left := Resolve(string(src))
	for _, l := range left {
		fmt.Fprintf(os.Stderr, "%s:%d: hunk not a mergeable table row\n", path, l)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Printf("%s: %d resolved, %d left\n", path, resolved, len(left))
	if len(left) > 0 {
		os.Exit(1)
	}
}
