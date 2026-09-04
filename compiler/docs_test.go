// The pages' own schema examples have to be schema. A declaration a reader
// copies out of docs/SPEC.md, docs/SPEC-TABLES.md or docs/USAGE.md and pastes
// into a file is the first thing they run, and one that does not parse teaches
// a syntax the language does not have. This gate names each such block by an
// anchor line, pulls the fenced block that holds it out of the page, and puts
// it through the parser — so an example cannot go stale in either direction:
// a block that stops parsing fails, and an anchor that stops existing fails
// with the page and the line to look at.
//
// Only blocks that are DECLARATIONS are listed; a block of generated C++, Go
// or JSON is not schema at all. The gate is the PARSER's, so a block enters it
// the moment its construct has a grammar — §2.8's maps parse now, and their
// block is listed below even though no backend carries the codec yet.
package compiler

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// docExample is one fenced block, found by an anchor line and, where a page
// prints the same anchor more than once, by which occurrence.
type docExample struct {
	page   string // path relative to this package
	anchor string // a line inside the block, compared trimmed
	nth    int    // 1 = the first block holding it
}

// The declaration blocks the pages print. Each is a complete unit once a
// package line is supplied; a block that carries its own `package` keeps it.
var docExamples = []docExample{
	{"../docs/SPEC.md", "// Wire.schema", 1},                    // §4.9, the complete example
	{"../docs/SPEC-TABLES.md", "table OpenDocument", 1},         // §2.6, union arms as tables
	{"../docs/SPEC-TABLES.md", "table Node", 2},                 // §3.1, framing worked
	{"../docs/SPEC-TABLES.md", "table Item { count int32 }", 1}, // §2.8, maps
	{"../docs/SPEC-TABLES.md", "package demo", 1},               // §7.3, a cook worked to the byte
	{"../docs/SPEC-TABLES.md", "name    string(16)", 1},         // §16.7, one declaration, two texts
	{"../docs/USAGE.md", "table OpenDocument", 1},               // messages: a union whose arms are tables
	{"../docs/USAGE.md", "table Node", 1},                       // pointers
}

func TestDocExamplesParse(t *testing.T) {
	for _, ex := range docExamples {
		t.Run(fmt.Sprintf("%s/%s/%d", ex.page, ex.anchor, ex.nth), func(t *testing.T) {
			src := fencedBlockContaining(t, ex.page, ex.anchor, ex.nth)
			unit := src
			if !declaresPackage(unit) {
				unit = "package docs\n\n" + unit
			}
			if _, errs := parser.Parse("Probe.schema", []byte(unit)); len(errs) > 0 {
				t.Errorf("%s: the example anchored at %q does not parse: %v\n%s",
					ex.page, ex.anchor, errs[0], src)
			}
		})
	}
}

func declaresPackage(src string) bool {
	for line := range strings.SplitSeq(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "package ") {
			return true
		}
	}
	return false
}

// fencedBlockContaining returns the body of the nth ``` block in page that
// holds a line equal to anchor once trimmed. It fails the test when the page
// has no such block, because an anchor that no longer exists means this gate
// stopped covering the example it was written for.
func fencedBlockContaining(t *testing.T, page, anchor string, nth int) string {
	t.Helper()
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	inside, found, seen := false, false, 0
	var body []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "```") {
			if inside && found {
				if seen++; seen == nth {
					return strings.Join(body, "\n") + "\n"
				}
			}
			inside = !inside
			body, found = nil, false
			continue
		}
		if !inside {
			continue
		}
		body = append(body, line)
		if strings.TrimSpace(line) == anchor {
			found = true
		}
	}
	t.Fatalf("%s has no fenced block number %d holding the line %q — this gate names an example that has moved", page, nth, anchor)
	return ""
}
