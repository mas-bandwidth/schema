package gotable

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// The "Tables from Go" section of docs/USAGE.md is the Go adopter's whole
// recipe — the compiler pin, the module setup, and a save and a load of each
// class — and every Go block it prints is a COMPLETE TEST BODY. This gate
// takes the section's schema block and each fenced Go block out of the page,
// compiles the schema and runs each Go block against it through runGenerated,
// so the recipe cannot go stale in either direction: a block that stops
// compiling fails under its own name, and a section that stops existing fails
// naming the page. Each Go block runs against the schema fence nearest above
// it, because the recipe prints each unit's declaration beside the code that
// uses it: the two classes are two UNITS — a fixed table carries its
// fixed-form surface only in a unit that declares no variable table at all
// (hasFixedForm refuses a regional unit), so one declaration could never
// serve both blocks.
//
// The section's own declaration blocks are held to the parser separately, by
// compiler/docs_test.go (TestDocExamplesParse).
func TestUsageGoTablesRecipeRuns(t *testing.T) {
	t.Parallel()
	page, err := os.ReadFile("../../../docs/USAGE.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := usageTablesFromGoFences(t, string(page))
	var schemas []string
	type recipe struct {
		schema, body string
		test         string
	}
	var recipes []recipe
	for _, block := range blocks {
		if usageFenceIsSchema(block.body) {
			schemas = append(schemas, block.body)
			continue
		}
		if block.lang != "go" {
			continue
		}
		if !usageDeclaresPackage(block.body) {
			t.Fatalf("the Go block running %s is not a complete test body: no package line", usageTestName(block.body))
		}
		if name := usageTestName(block.body); name == "" {
			t.Fatal("a Go block in the section is not a complete test body: no Test function")
		}
		if len(schemas) == 0 {
			t.Fatalf("the Go block running %s has no schema block above it", usageTestName(block.body))
		}
		recipes = append(recipes, recipe{schema: schemas[len(schemas)-1], body: block.body, test: usageTestName(block.body)})
	}
	if len(schemas) == 0 {
		t.Fatal(`the "Tables from Go" section declares no schema`)
	}
	if len(recipes) < 2 {
		t.Fatalf(`the "Tables from Go" section runs %d Go block(s); both the variable and the fixed recipe are owed`, len(recipes))
	}
	for i, r := range recipes {
		t.Run(fmt.Sprintf("block%d/%s", i+1, r.test), func(t *testing.T) {
			runGenerated(t, r.schema, r.body)
		})
	}
}

// usageFence is one fenced block of the page: its language tag, empty for a
// plain fence, and its body.
type usageFence struct {
	lang string
	body string
}

// usageTablesFromGoFences cuts the "### Tables from Go" section out of the
// page and returns its fenced blocks in order. A heading outside a fence ends
// the section; a fence keeps one from ending it.
func usageTablesFromGoFences(t *testing.T, page string) []usageFence {
	t.Helper()
	lines := strings.Split(page, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "### Tables from Go" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf(`docs/USAGE.md has no "Tables from Go" section — the Go adopter's recipe (the pin, the module setup, and a save and a load of each class) is missing`)
	}
	var out []usageFence
	inside := false
	var lang string
	var body []string
	for _, line := range lines[start:] {
		if strings.HasPrefix(line, "```") {
			if inside {
				out = append(out, usageFence{lang: lang, body: strings.Join(body, "\n") + "\n"})
			}
			inside, lang, body = !inside, strings.TrimPrefix(line, "```"), nil
			continue
		}
		if !inside && strings.HasPrefix(line, "#") {
			return out
		}
		if inside {
			body = append(body, line)
		}
	}
	if inside {
		t.Fatal(`the "Tables from Go" section has an unclosed fence`)
	}
	return out
}

// usageFenceIsSchema is whether a fenced block is a declaration: a unit
// carries a package line, and a table unit declares a table.
func usageFenceIsSchema(body string) bool {
	hasPackage, hasTable := false, false
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		hasPackage = hasPackage || strings.HasPrefix(trimmed, "package ")
		hasTable = hasTable || strings.HasPrefix(trimmed, "table ") || strings.HasPrefix(trimmed, "fixed table ")
	}
	return hasPackage && hasTable
}

// usageDeclaresPackage is whether a Go block is a whole file rather than a
// fragment: the recipe's blocks are pasted into a _test.go beside the
// generated package, so the package line is the first thing each owes.
func usageDeclaresPackage(body string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "package ") {
			return true
		}
	}
	return false
}

// usageTestName is the block's first test function, for the subtest's name.
func usageTestName(body string) string {
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "func Test"); ok {
			if name, _, ok := strings.Cut(after, "("); ok {
				return "Test" + name
			}
		}
	}
	return ""
}
