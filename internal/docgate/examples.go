package docgate

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// THE CODE EXAMPLES OF THE PAGES (review finding F-173). A fenced block whose
// info string names a target language is an EXAMPLE, and every example on the
// reader-facing pages is in exactly one of three classes, stated on the page:
//
//   - COMPILE: `<!-- example: compile unit=<dir> -->` on the line above the
//     fence. The slow gate compiles the block (or parses it, where the target
//     has no compiler step) against the generated output of the unit named.
//     With `harness=<name>` the block is spliced into
//     testdata/harness/<name>.<ext> at its EXAMPLE line first: that is how a
//     few lines lifted out of a program are still held to the real names.
//   - LEG: the block sits between a leg's own `<!-- <name>-usage -->` markers,
//     and that leg's gate extracts, builds and runs it. The registry below
//     names the file that does, and the gate checks that file still reads
//     the marker.
//   - FRAGMENT: `<!-- example: fragment -->` on the line above the fence. The
//     block is deliberately not a compilable unit: generated output quoted to
//     show its shape, a signature, a line of a larger program. It is counted,
//     so a page cannot drift into fragments without the count moving.
//
// An example in none of the three is a finding, and so is a fence in a
// language the gate has never heard of: silence is never the passing state.

// Example is one fenced target-language block of a page.
type Example struct {
	Page    string // repository-relative slash path
	Line    int    // 1-based line of the opening fence
	Lang    string // the canonical target language
	Body    string // the block's text, newline-terminated
	Class   string // ClassCompile, ClassLeg, ClassFragment, or "" when unmarked
	Unit    string // compile: the unit whose generated output it is held to
	Harness string // compile: the harness it is spliced into, or ""
	Leg     string // leg: the marker name
	File    string // schema: the unit's file the fence is held equal to
}

// The three classes.
const (
	ClassCompile  = "compile"
	ClassLeg      = "leg"
	ClassFragment = "fragment"
	// ClassSchema is a DECLARATION fence a compile example's unit is made of:
	// `<!-- example: schema unit=<dir> file=<name> -->` holds the fence
	// byte-equal to that file of the unit, so the page and the unit the gate
	// generates from cannot drift apart.
	ClassSchema = "schema"
)

// exampleLangs maps a fence's info string to the target language it shows.
var exampleLangs = map[string]string{
	"c": "c", "cpp": "cpp", "c++": "cpp", "csharp": "csharp", "cs": "csharp",
	"dart": "dart", "elixir": "elixir", "go": "go", "goose": "goose",
	"java": "java", "js": "js", "javascript": "js", "lobster": "lobster",
	"lua": "lua", "luau": "luau", "moo": "moo", "python": "python",
	"ruby": "ruby", "rust": "rust", "squirrel": "squirrel",
}

// proseLangs are the info strings that are not target code: a shell
// transcript, data, a declaration (the parser holds those, compiler/docs_test.go).
var proseLangs = map[string]bool{
	"": true, "sh": true, "bash": true, "console": true, "text": true,
	"json": true, "diff": true, "schema": true, "sexp": true, "yaml": true,
	"make": true, "tla": true,
}

// LegMarkers is the registry of the legs' own usage markers: the marker name
// and the file whose gate extracts, builds and runs the block between them.
var LegMarkers = map[string]string{
	"c-table-usage":                  "make/c.mk",
	"cpp-cook-usage-read":            "Makefile",
	"cpp-cook-usage-write":           "Makefile",
	"cpp-cook-usage-write-pointered": "Makefile",
	"cs-table-usage":                 "test/cs-cook/usage",
	"dart-table-usage":               "test/dart-tables/usage",
	"elixir-table-usage":             "make/elixir.mk",
	"go-table-usage":                 "test/go-tables/usage_test.go",
	"goose-table-usage":              "make/goose-blockcook.mk",
	"java-table-usage":               "make/java.mk",
	"js-table-usage":                 "make/js.mk",
	"lobster-table-usage":            "make/lobster-open-gates.mk",
	"lua-table-usage":                "make/lua.mk",
	"luau-table-usage":               "make/luau-open-gates.mk",
	"moo-table-usage":                "make/moo-open-gates.mk",
	"python-usage":                   "internal/ci/python_usage_fragments_test.go",
	"ruby-usage":                     "internal/ci/ruby_usage_fragments_test.go",
	"rust-table-usage":               "internal/codegen/rusttable/usage_rust_test.go",
}

var (
	exampleMarkRe = regexp.MustCompile(`^<!--\s*example:\s*(.*?)\s*-->$`)
	legOpenRe     = regexp.MustCompile(`^<!--\s*([a-z][a-z0-9-]*-usage)(?:\s+[^>]*?)?\s*-->$`)
	legCloseRe    = regexp.MustCompile(`^<!--\s*/([a-z][a-z0-9-]*-usage)\s*-->$`)
	unitRe        = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./-]*$`)
	harnessRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// ExamplePages lists the pages whose examples the gate owns: the README,
// every page under docs/, and every page under skills/.
func ExamplePages(root string) ([]string, error) {
	pages := []string{}
	if _, err := os.Stat(filepath.Join(root, "README.md")); err == nil {
		pages = append(pages, "README.md")
	}
	for _, dir := range []string{"docs", "skills"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".md") {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			pages = append(pages, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(pages)
	return pages, nil
}

// Examples returns every example of every page ExamplePages lists, in page
// order, and the problems the pages' own marking has: a fence in a language
// the gate does not know, a marker that does not parse, a marker with no
// fence under it, an unclosed fence or leg region.
func Examples(root string) ([]Example, []Problem, error) {
	pages, err := ExamplePages(root)
	if err != nil {
		return nil, nil, err
	}
	var examples []Example
	var problems []Problem
	for _, rel := range pages {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, nil, err
		}
		found, bad := pageExamples(rel, string(data))
		examples = append(examples, found...)
		problems = append(problems, bad...)
	}
	return examples, problems, nil
}

// pageExamples scans one page.
func pageExamples(rel, text string) ([]Example, []Problem) {
	var examples []Example
	var problems []Problem
	bad := func(line int, ref, reason string) {
		problems = append(problems, Problem{File: rel, Line: line, Ref: ref, Reason: reason})
	}
	lines := strings.Split(text, "\n")
	var (
		inFence  bool
		fence    string // the opening fence's run of backticks or tildes
		open     Example
		body     []string
		tracked  bool   // the open fence is an example
		mark     string // an example marker waiting for its fence
		markLine int
		leg      string // the open leg region
		legLine  int
	)
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if inFence {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
				if tracked {
					open.Body = strings.Join(body, "\n") + "\n"
					examples = append(examples, open)
				}
				inFence, tracked, body = false, false, nil
				continue
			}
			if tracked {
				body = append(body, line)
			}
			continue
		}
		if run := fenceRun(trimmed); run != "" {
			info := strings.ToLower(strings.TrimSpace(trimmed[len(run):]))
			inFence, fence = true, run
			lang, isExample := exampleLangs[info]
			switch {
			case isExample:
				tracked = true
				open = Example{Page: rel, Line: i + 1, Lang: lang}
				switch {
				case mark != "":
					if err := open.applyMark(mark); err != nil {
						bad(markLine, mark, "example-marker: "+err.Error())
					} else if open.Class == ClassSchema {
						bad(markLine, mark, "example-marker: a schema marker over target code")
					}
				case leg != "":
					open.Class, open.Leg = ClassLeg, leg
				}
			case !proseLangs[info]:
				bad(i+1, info, "example-language: a fence in a language the gate does not know")
			case mark != "":
				tracked = true
				open = Example{Page: rel, Line: i + 1, Lang: "schema"}
				if err := open.applyMark(mark); err != nil {
					bad(markLine, mark, "example-marker: "+err.Error())
				} else if open.Class != ClassSchema {
					bad(markLine, mark, "example-marker: the fence under it is not target code")
				}
			}
			mark = ""
			continue
		}
		if trimmed == "" {
			continue
		}
		if mark != "" {
			bad(markLine, mark, "example-marker: no fence under it")
			mark = ""
		}
		if m := exampleMarkRe.FindStringSubmatch(trimmed); m != nil {
			mark, markLine = m[1], i+1
			continue
		}
		if m := legCloseRe.FindStringSubmatch(trimmed); m != nil {
			if m[1] != leg {
				bad(i+1, m[1], "example-leg: a closing marker with no opening one")
			}
			leg = ""
			continue
		}
		if m := legOpenRe.FindStringSubmatch(trimmed); m != nil {
			if leg != "" {
				bad(legLine, leg, "example-leg: the region is not closed")
			}
			leg, legLine = m[1], i+1
			if _, ok := LegMarkers[leg]; !ok {
				bad(i+1, leg, "example-leg: a marker no gate is registered for")
			}
		}
	}
	if inFence {
		bad(open.Line, fence, "example-fence: the fence is not closed")
	}
	if mark != "" {
		bad(markLine, mark, "example-marker: no fence under it")
	}
	if leg != "" {
		bad(legLine, leg, "example-leg: the region is not closed")
	}
	return examples, problems
}

// fenceRun returns the run of three or more backticks or tildes a line opens
// with, or "".
func fenceRun(trimmed string) string {
	for _, c := range []string{"`", "~"} {
		if strings.HasPrefix(trimmed, c+c+c) {
			n := len(trimmed) - len(strings.TrimLeft(trimmed, c))
			return trimmed[:n]
		}
	}
	return ""
}

// applyMark reads the words of an example marker onto the example.
func (e *Example) applyMark(mark string) error {
	err := e.readMark(mark)
	if err != nil {
		e.Class, e.Unit, e.Harness, e.File = "", "", "", ""
	}
	return err
}

func (e *Example) readMark(mark string) error {
	words := strings.Fields(mark)
	if len(words) == 0 {
		return fmt.Errorf("it names no class")
	}
	switch words[0] {
	case ClassFragment:
		if len(words) != 1 {
			return fmt.Errorf("fragment takes no words")
		}
		e.Class = ClassFragment
		return nil
	case ClassCompile, ClassSchema:
		e.Class = words[0]
		for _, word := range words[1:] {
			key, value, ok := strings.Cut(word, "=")
			switch {
			case !ok:
				return fmt.Errorf("%q is not key=value", word)
			case key == "unit" && unitRe.MatchString(value) && !strings.Contains(value, ".."):
				e.Unit = value
			case key == "harness" && e.Class == ClassCompile && harnessRe.MatchString(value):
				e.Harness = value
			case key == "file" && e.Class == ClassSchema && unitRe.MatchString(value) && !strings.Contains(value, "/"):
				e.File = value
			default:
				return fmt.Errorf("%q is not a word %s takes", word, e.Class)
			}
		}
		if e.Unit == "" {
			return fmt.Errorf("%s names no unit", e.Class)
		}
		if e.Class == ClassSchema && e.File == "" {
			return fmt.Errorf("schema names no file")
		}
		return nil
	}
	return fmt.Errorf("%q is not compile, fragment or schema", words[0])
}

// Count is how many examples of one page, one language and one class there are.
type Count struct {
	Page, Lang, Class string
}

// CountExamples tallies the examples by page, language and class. An unmarked
// example tallies under the empty class.
func CountExamples(examples []Example) map[Count]int {
	counts := map[Count]int{}
	for _, e := range examples {
		counts[Count{e.Page, e.Lang, e.Class}]++
	}
	return counts
}

// CheckExamples is the fast gate over the marking: every problem Examples
// reports, every example in no class, every compile example whose unit or
// harness is not on disk, and every leg marker whose gate file no longer
// reads it.
func CheckExamples(root string) ([]Example, []Problem, error) {
	examples, problems, err := Examples(root)
	if err != nil {
		return nil, nil, err
	}
	legs := map[string]bool{}
	for _, e := range examples {
		switch e.Class {
		case "":
			problems = append(problems, Problem{File: e.Page, Line: e.Line, Ref: e.Lang,
				Reason: "example-unmarked: mark it compile (the slow gate builds it) or fragment"})
		case ClassCompile:
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(e.Unit))); err != nil {
				problems = append(problems, Problem{File: e.Page, Line: e.Line, Ref: e.Unit, Reason: "example-unit: no such directory"})
			}
			if e.Harness != "" {
				if _, err := os.Stat(HarnessPath(root, e)); err != nil {
					problems = append(problems, Problem{File: e.Page, Line: e.Line, Ref: e.Harness, Reason: "example-harness: no such harness for " + e.Lang})
				}
			}
		case ClassSchema:
			want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(e.Unit), e.File))
			switch {
			case err != nil:
				problems = append(problems, Problem{File: e.Page, Line: e.Line, Ref: e.Unit + "/" + e.File, Reason: "example-schema: no such file"})
			case string(want) != e.Body:
				problems = append(problems, Problem{File: e.Page, Line: e.Line, Ref: e.Unit + "/" + e.File, Reason: "example-schema: the fence and the unit's file differ"})
			}
		case ClassLeg:
			legs[e.Leg] = true
		}
	}
	for leg := range legs {
		gate, ok := LegMarkers[leg]
		if !ok {
			continue // reported where the marker opens
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(gate)))
		if err != nil || !strings.Contains(string(data), leg) {
			problems = append(problems, Problem{File: gate, Line: 1, Ref: leg, Reason: "example-leg: the registered gate does not read this marker"})
		}
	}
	sort.SliceStable(problems, func(i, j int) bool {
		if problems[i].File != problems[j].File {
			return problems[i].File < problems[j].File
		}
		return problems[i].Line < problems[j].Line
	})
	return examples, problems, nil
}

// exampleExt is the source extension of each target language.
var exampleExt = map[string]string{
	"c": ".c", "cpp": ".cpp", "csharp": ".cs", "dart": ".dart", "elixir": ".exs",
	"go": ".go", "goose": ".goose", "java": ".java", "js": ".mjs",
	"lobster": ".lobster", "lua": ".lua", "luau": ".luau", "moo": ".moo",
	"python": ".py", "ruby": ".rb", "rust": ".rs", "squirrel": ".nut",
}

// harnessSplice is the line of a harness the example replaces.
const harnessSplice = "EXAMPLE"

// HarnessPath is where a compile example's harness lives.
func HarnessPath(root string, e Example) string {
	return filepath.Join(root, "internal", "docgate", "testdata", "harness", e.Harness+exampleExt[e.Lang])
}

// Source is the text the slow gate hands the compiler for a compile example:
// the block itself, or its harness with the block spliced in at the line that
// holds only the word EXAMPLE in a comment.
func (e Example) Source(root string) (string, error) {
	if e.Harness == "" {
		return e.Body, nil
	}
	data, err := os.ReadFile(HarnessPath(root, e))
	if err != nil {
		return "", err
	}
	var out strings.Builder
	spliced := 0
	for line := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
		word := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "/#-"))
		if word == harnessSplice {
			out.WriteString(e.Body)
			spliced++
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	if spliced != 1 {
		return "", fmt.Errorf("harness %s has %d EXAMPLE lines, and one is owed", e.Harness, spliced)
	}
	return out.String(), nil
}
