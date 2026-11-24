// Package wirealone reads testdata/conformance/tables/WIRE-ALONE.txt: the
// framing rows whose condition docs/SPEC-TABLES.md §6.5 lists as DECIDABLE
// FROM THE WIRE ALONE, each tagged with its class. A port's wire-walk control
// derives the rows its walk alone refuses from this one file and states only
// its own exceptions, each with a reason, so no port keeps a hand list.
package wirealone

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Path is the file, relative to the repository root.
const Path = "testdata/conformance/tables/WIRE-ALONE.txt"

// Classes is the closed set of condition classes, one per clause of §6.5's
// wire-alone list that a framing row breaks.
var Classes = map[string]bool{
	"id-table":     true,
	"inert-bytes":  true,
	"element-kind": true,
	"depth":        true,
	"node-table":   true,
	"graph":        true,
	"blob-shared":  true,
	"text":         true,
}

// Read answers every row of the file under root, keyed by row name and
// valued by its class. A malformed line, an unknown class, a repeated row or
// an empty file is an error naming it.
func Read(root string) (rows map[string]string, err error) {
	f, err := os.Open(filepath.Join(root, Path))
	if err != nil {
		return nil, err
	}
	// a close that fails is an error, never dropped
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			rows, err = nil, cerr
		}
	}()
	rows = map[string]string{}
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		w := strings.Fields(text)
		if len(w) != 2 {
			return nil, fmt.Errorf("%s:%d: want <class> <row>, got %q", Path, line, text)
		}
		if !Classes[w[0]] {
			return nil, fmt.Errorf("%s:%d: unknown class %q", Path, line, w[0])
		}
		if _, dup := rows[w[1]]; dup {
			return nil, fmt.Errorf("%s:%d: row %s is listed twice", Path, line, w[1])
		}
		rows[w[1]] = w[0]
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s lists no row", Path)
	}
	return rows, nil
}

// Owned answers the rows a port's walk alone refuses: every row of the file
// but the port's exceptions, sorted. Each exception must name a row of the
// file and carry a reason; one that does not is an error, so an exception
// can neither outlive its row nor stand unexplained.
func Owned(root string, exceptions map[string]string) ([]string, error) {
	rows, err := Read(root)
	if err != nil {
		return nil, err
	}
	for name, why := range exceptions {
		if _, ok := rows[name]; !ok {
			return nil, fmt.Errorf("the exception %s names no row of %s", name, Path)
		}
		if strings.TrimSpace(why) == "" {
			return nil, fmt.Errorf("the exception %s gives no reason", name)
		}
	}
	var out []string
	for name := range rows {
		if _, skip := exceptions[name]; !skip {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}
