// Command emit writes one unit's <Ns>.UnitView module (docs/SPEC-TABLES.md
// §8.3) and nothing else, for the VIEW_CORPUS units whose full
// `schema generate --lang elixir` this leg still refuses by name (make
// tables-elixir-view, make/elixir.mk). The registry is data about the IR and
// names no codec, so the listing gate covers every corpus unit; the refusal
// of the codecs is unchanged and is named in the gate's own comment.
//
//	go run ./test/elixir-view/emit <schema dir> <out file>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: emit <schema dir> <out file>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "emit:", err)
		os.Exit(1)
	}
}

func run(dir, out string) error {
	paths, err := filepath.Glob(filepath.Join(dir, "*.schema"))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no .schema files in %s", dir)
	}
	sort.Strings(paths)
	var files []check.SourceFile
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		ast, errs := parser.Parse(filepath.Base(p), src)
		if len(errs) != 0 {
			return fmt.Errorf("parse %s: %v", p, errs)
		}
		files = append(files, check.SourceFile{Path: p, Name: filepath.Base(p), Base: strings.TrimSuffix(filepath.Base(p), ".schema"), Bytes: src, AST: ast})
	}
	u, errs := check.Unit(files)
	if len(errs) != 0 {
		return fmt.Errorf("check %s: %v", dir, errs)
	}
	return os.WriteFile(out, elixirtable.UnitView(u), 0o644)
}
