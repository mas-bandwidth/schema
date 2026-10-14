// The tutorial's second language is Go and NOT C (schema#553). Parts 12 and 13
// cross from the C++ reference to exactly one other language, and the owner's
// rule is that the pair must be two DIFFERENT languages for structs in memory:
// "C and C++ is weak sauce ... anybody reading that would be 'Why?!'". Go is
// the one that carries the packet wire, the cook open and the block read the
// crossings need, so the page names Go and drops C.
//
// This gate pulls the fenced blocks out of the two parts and holds the rule
// mechanically rather than by eye: no C block survives in either part, each
// part shows Go, and every Go block PARSES as Go — a snippet that stops being
// Go fails here, and so does a C block that creeps back. A section heading or
// the closing summary that moves fails with the page and the line to look at.
package compiler

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// tutorialHeading locates one `## Part N:` section in the page.
const (
	tutorialPart12 = "## Part 12:"
	tutorialPart13 = "## Part 13:"
)

// tutorialPart returns the lines of one `## Part N:` section, up to the next
// `## Part` heading or the end of the page.
func tutorialPart(t *testing.T, page, heading string) []string {
	t.Helper()
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	var body []string
	inside := false
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "## Part ") {
			if inside {
				break
			}
			if strings.HasPrefix(line, heading) {
				inside = true
			}
		}
		if inside {
			body = append(body, line)
		}
	}
	if len(body) == 0 {
		t.Fatalf("%s has no section %q — this gate names a part that has moved", page, heading)
	}
	return body
}

// tutorialFences returns (lang, body) for every fenced block in the lines.
// lang is the info string on the opening ``` line, which is empty for an
// untagged block (a command transcript or a go.mod).
func tutorialFences(lines []string) [][2]string {
	var out [][2]string
	inside := false
	var lang string
	var body []string
	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if inside {
				out = append(out, [2]string{lang, strings.Join(body, "\n")})
				body = nil
			} else {
				lang = strings.TrimSpace(strings.TrimPrefix(line, "```"))
			}
			inside = !inside
			continue
		}
		if inside {
			body = append(body, line)
		}
	}
	return out
}

// TestTutorialSecondLanguageIsGo is the gate (schema#553).
func TestTutorialSecondLanguageIsGo(t *testing.T) {
	const page = "../docs/TUTORIAL.md"
	for _, heading := range []string{tutorialPart12, tutorialPart13} {
		lines := tutorialPart(t, page, heading)
		fences := tutorialFences(lines)

		goBlocks, cBlocks := 0, 0
		for _, fence := range fences {
			lang, body := fence[0], fence[1]
			switch lang {
			case "c":
				cBlocks++
				t.Errorf("%s still shows a C block; the second language is Go, not C (schema#553):\n%s",
					heading, body)
			case "go":
				goBlocks++
				if _, err := parser.ParseFile(token.NewFileSet(), "tutorial.go", body, parser.AllErrors); err != nil {
					t.Errorf("%s: a Go block does not parse as Go: %v\n%s", heading, err, body)
				}
			}
		}
		if cBlocks == 0 && goBlocks == 0 {
			t.Errorf("%s crosses to no language at all: no Go block and no C block", heading)
		}
		if goBlocks == 0 {
			t.Errorf("%s shows no Go block; the second language is Go (schema#553)", heading)
		}
	}

	// The part's own title and its closing sentence name the rule, so a silent
	// revert of the prose fails here too.
	part13 := strings.Join(tutorialPart(t, page, tutorialPart13), "\n")
	if !strings.Contains(part13, "### Three wires cross to Go") {
		t.Error("Part 13 no longer says its three wires cross to Go (schema#553)")
	}
	if prose := strings.Join(strings.Fields(part13), " "); !strings.Contains(prose, "Go tools reading its packets") {
		t.Error("Part 13's closing summary no longer names Go tools (schema#553)")
	}
}
