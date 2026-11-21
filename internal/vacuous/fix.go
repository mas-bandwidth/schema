package vacuous

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// applyFixes inserts, before every class 2 loop whose collection has a name
// and whose function has a testing parameter in scope, the check the loop was
// missing:
//
//	if len(files) == 0 {
//		t.Fatal("files is empty: the loop below would check nothing")
//	}
//
// It returns how many checks it wrote. A loop it cannot fix this way (the
// collection is an unnamed call, the walk is a callback, or no test is in
// scope) is left for a person, and stays reported.
func applyFixes(root string, findings []Finding) (int, error) {
	byFile := map[string][]Finding{}
	for _, f := range findings {
		if f.fixKey != "" && f.fixT != "" {
			byFile[f.File] = append(byFile[f.File], f)
		}
	}
	fixed := 0
	for file, fs := range byFile {
		path := filepath.Join(root, filepath.FromSlash(file))
		data, err := os.ReadFile(path)
		if err != nil {
			return fixed, err
		}
		src := string(data)
		// last loop first, so earlier offsets stay true
		sort.Slice(fs, func(i, j int) bool { return fs[i].fixAt > fs[j].fixAt })
		last := -1
		for _, f := range fs {
			if f.fixAt == last || f.fixAt > len(src) {
				continue
			}
			last = f.fixAt
			lineStart := strings.LastIndexByte(src[:f.fixAt], '\n') + 1
			indent := src[lineStart:f.fixAt]
			if strings.TrimSpace(indent) != "" {
				continue // the loop does not start its line
			}
			what, bind := f.fixKey, ""
			if f.fixHoist {
				// name the ranged call, so that it can be counted
				call := src[f.fixX0:f.fixX1]
				if strings.ContainsAny(call, "\n\"`") {
					continue // a call over several lines, or holding a string: left for a person
				}
				what, bind = call, fmt.Sprintf("%s%s := %s\n", indent, f.fixKey, call)
				src = src[:f.fixX0] + f.fixKey + src[f.fixX1:]
			}
			check := fmt.Sprintf("%s%sif len(%s) == 0 {\n%s\t%s.Fatal(\"%s is empty: the loop below would check nothing\")\n%s}\n", bind, indent, f.fixKey, indent, f.fixT, what, indent)
			src = src[:lineStart] + check + src[lineStart:]
			fixed++
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			return fixed, err
		}
	}
	return fixed, nil
}
