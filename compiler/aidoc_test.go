// docs/AI.md is the page an AI assistant reads before it writes a .schema file
// in somebody else's project, and it is read as instructions: an example on it
// that does not compile teaches a syntax the language does not have, and the
// assistant repeats it. This gate pulls every ```schema block out of the page,
// adds the `package` line the page leaves out, and puts the unit through the
// same front end `schema check` runs — parse, check, the fixed-table bounds —
// and then through Generate for every live target, so an example that some
// language cannot carry fails here rather than in a user's build.
//
// The gate needs no file on disk and no toolchain: the blocks are parsed from
// memory and generation stops at the emitted text.
package compiler

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const aiPage = "../docs/AI.md"

// aiSchemaBlock is one ```schema fence and the page line its body starts on.
type aiSchemaBlock struct {
	line int
	body string
}

// aiSchemaBlocks returns every fence whose info string is exactly "schema". A
// fence left open at the end of the page is a failure, because everything after
// it would silently stop being prose.
func aiSchemaBlocks(t *testing.T, page string, data []byte) []aiSchemaBlock {
	t.Helper()
	var blocks []aiSchemaBlock
	inside, isSchema, start := false, false, 0
	var body []string
	for i, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "```") {
			if inside && isSchema {
				body = append(body, line)
			}
			continue
		}
		if inside {
			if isSchema {
				blocks = append(blocks, aiSchemaBlock{line: start, body: strings.Join(body, "\n") + "\n"})
			}
			inside, isSchema, body = false, false, nil
			continue
		}
		inside = true
		isSchema = strings.TrimSpace(strings.TrimPrefix(line, "```")) == "schema"
		start = i + 2
	}
	if inside {
		t.Fatalf("%s: a fenced block opened at line %d is never closed", page, start-1)
	}
	return blocks
}

// aiCheckUnit is `schema check` over one in-memory file: the parser, the
// checker, and the fixed-table bounds Load applies after them.
func aiCheckUnit(src string) (*ir.Unit, []error) {
	f, errs := parser.Parse("Example.schema", []byte(src))
	if len(errs) > 0 {
		return nil, errs
	}
	u, errs := check.Unit([]check.SourceFile{{
		Path:  "Example.schema",
		Name:  "Example.schema",
		Base:  "Example",
		Bytes: []byte(src),
		AST:   f,
	}})
	if len(errs) > 0 {
		return nil, errs
	}
	if _, errs := ir.TableFixedRecordBounds(u, 0); len(errs) > 0 {
		return nil, errs
	}
	if _, errs := ir.TableFixedLeafCapRefusals(u); len(errs) > 0 {
		return nil, errs
	}
	return u, nil
}

func TestAIPageSchemaExamplesCheckAndGenerate(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(aiPage)
	if err != nil {
		t.Fatal(err)
	}
	blocks := aiSchemaBlocks(t, aiPage, data)
	// The page teaches the whole language, so it has many blocks; a count this
	// low means the fence spelling changed and the gate stopped seeing them.
	if len(blocks) < 15 {
		t.Fatalf("%s has %d ```schema blocks; the page carries more than 15, so the extractor has stopped finding them", aiPage, len(blocks))
	}
	c := New()
	targets := c.Targets()
	for _, b := range blocks {
		t.Run(fmt.Sprintf("line%d", b.line), func(t *testing.T) {
			t.Parallel()
			src := b.body
			if !declaresPackage(src) {
				src = "package example\n\n" + src
			}
			u, errs := aiCheckUnit(src)
			if len(errs) > 0 {
				t.Fatalf("%s:%d: the example does not check: %v\n%s", aiPage, b.line, errs[0], b.body)
			}
			for _, target := range targets {
				if _, err := c.Generate(u, target, Options{}); err != nil {
					t.Errorf("%s:%d: the example does not generate for %s: %v\n%s", aiPage, b.line, target, err, b.body)
				}
			}
		})
	}
}

// TestAIPageExtractorFindsOnlySchemaFences is the gate's own control: a page
// with one schema fence, one fence of another language and one bare fence
// yields exactly the schema one, and a broken example fails the check.
func TestAIPageExtractorFindsOnlySchemaFences(t *testing.T) {
	t.Parallel()
	text := "intro\n\n```schema\nconst A = 1\n```\n\n```go\nx := 1\n```\n\n```\nplain\n```\n"
	blocks := aiSchemaBlocks(t, "page.md", []byte(text))
	if len(blocks) != 1 || blocks[0].body != "const A = 1\n" || blocks[0].line != 4 {
		t.Fatalf("got %+v, want one block at line 4 holding the const", blocks)
	}
	// A broken example must fail the check half, not pass by accident.
	if _, errs := aiCheckUnit("package example\n\ntype T { x int }\n"); len(errs) == 0 {
		t.Fatal("a field of type int checked clean; the gate would wave a broken example through")
	}
}
