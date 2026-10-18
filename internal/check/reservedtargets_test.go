// The §4.6 target-name safety rule, proven against the targets being
// generated: generate --lang L checks L's reserved words only, and every
// command that names no target checks every target's — one diagnostic per
// name listing every checked target it collides with.
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func TestReservedWordsOnlyForGeneratedTargets(t *testing.T) {
	t.Parallel()
	src := "package t\ntable Words {\n  func int32\n  class int32\n  select int32\n  self int32\n}\n"
	f, perrs := parser.Parse("t.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	files := []SourceFile{{
		Path:  "t.schema",
		Name:  "t.schema",
		Base:  "t",
		Bytes: []byte(src),
		AST:   f,
	}}
	reserved := func(errs []error) map[string]bool {
		out := map[string]bool{}
		for _, e := range errs {
			s := e.Error()
			for _, w := range []string{"func", "class", "select", "self"} {
				if strings.Contains(s, w+"\" is a reserved word") {
					out[w] = true
				}
			}
		}
		return out
	}

	// target go only: func and select (Go words) are refused; class and self
	// (C++/C# and Rust) are not checked.
	_, errs := UnitForTargets(files, []string{"Go"})
	r := reserved(errs)
	for _, w := range []string{"func", "select"} {
		if !r[w] {
			t.Errorf("target go should refuse %q, got errs=%v", w, errs)
		}
	}
	for _, w := range []string{"class", "self"} {
		if r[w] {
			t.Errorf("target go must not check %q (reserved on other targets), got errs=%v", w, errs)
		}
	}

	// target cpp only: class alone.
	_, errs = UnitForTargets(files, []string{"C++"})
	r = reserved(errs)
	if !r["class"] {
		t.Errorf("target cpp should refuse class, got errs=%v", errs)
	}
	for _, w := range []string{"func", "select", "self"} {
		if r[w] {
			t.Errorf("target cpp must not check %q, got errs=%v", w, errs)
		}
	}

	// no target: every target's words, all four, and the class diagnostic
	// names every target it collides with in one line.
	_, errs = Unit(files)
	r = reserved(errs)
	for _, w := range []string{"func", "class", "select", "self"} {
		if !r[w] {
			t.Errorf("no target should refuse %q, got errs=%v", w, errs)
		}
	}
	var classDiag string
	for _, e := range errs {
		if strings.Contains(e.Error(), "class\" is a reserved word") {
			classDiag = e.Error()
			break
		}
	}
	if !strings.Contains(classDiag, "C++") || !strings.Contains(classDiag, "C#") {
		t.Errorf("the class diagnostic should name every target it collides with in one line, got %q", classDiag)
	}
}
