// THE TICKED-CELL RULE (test/conformance/README.md, "The nightly"). A ✅ in
// ROADMAP.md says a leg carries that row's feature. A case whose surface and
// class read that row is then owed by the leg: answering it ABSENT — a
// `<case>.absent` file (empty or not), a surface left out of `list`, or exit
// code 2 on the surface (which is how a leg whose generator refused the unit
// says so) — fails the leg under --claims, the same way a case the carries
// registry claims does (claims.go). A row that is not ticked for the leg keeps
// the old meaning: the absence is the work left, counted and not red.
//
// THE ONE PIECE OF MAPPING DATA is caseRows: which roadmap rows a case of a
// surface reads, by the surface and the root's class (fixed or variable). The
// ticks themselves are read from the committed ROADMAP.md, and the row ids
// from docs/roadmap.sexp, where render.lisp takes them from; ticked_test.go
// holds every row caseRows names to the committed table, so a renamed row is
// a red test rather than a rule that quietly stops applying.
package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	roadmapTablePath = "ROADMAP.md"
	roadmapDataPath  = "docs/roadmap.sexp"
)

// tickSet is which legs each roadmap row is ticked for: "section/row" -> lang.
type tickSet map[string]map[string]bool

// caseRows is the roadmap rows one case reads: by its surface, and for a
// surface that reads a table root, by the root's class.
func caseRows(surface, class string) []string {
	byClass := func(fixed, variable string) []string {
		if class == "variable" {
			return []string{"variable-tables/" + variable}
		}
		return []string{"variable-tables/" + fixed}
	}
	switch surface {
	case "wire", "write-refusal":
		return byClass("fixed-class", "variable-class")
	case "json-read", "json-write", "json-hostile", "write-refusal-json":
		return byClass("text-form", "text-form-variable")
	case "message", "message-prior", "write-refusal-message":
		return []string{"variable-tables/message-form"}
	case "report":
		return []string{"variable-tables/widening"}
	case "framing":
		return []string{"variable-tables/id-table-wire"}
	case "retain", "retain-save":
		return []string{"variable-tables/retain-unknown"}
	case "cook", "cook-foreign", "cook-forgery":
		return []string{"variable-tables/cook-open"}
	case "cook-write":
		return []string{"variable-tables/cook-write"}
	case "block", "block-foreign", "block-dump", "forgery":
		return []string{"variable-tables/block-read"}
	case "write-refusal-fixed":
		return []string{"fixed-tables/writing"}
	}
	return nil
}

// tickedRow answers whether lang is ticked for any row the case reads, and
// which row.
func (t tickSet) tickedRow(lang, surface, class string) (string, bool) {
	for _, row := range caseRows(surface, class) {
		if t[row][lang] {
			return row, true
		}
	}
	return "", false
}

// readTicks reads the committed roadmap's ticks for every section caseRows
// names.
func readTicks(tablePath, dataPath string) (tickSet, error) {
	md, err := os.ReadFile(tablePath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, err
	}
	return parseTicks(string(md), string(data), []string{"variable-tables", "fixed-tables"})
}

var roadmapRowPair = regexp.MustCompile(`\("([a-z0-9][a-z0-9-]*)"\s+"((?:[^"\\]|\\.)*)"\)`)

// parseTicks reads, for each section, the row ids and titles from the data
// and the ✅ cells from the rendered table, matching rows by title.
func parseTicks(md, data string, sections []string) (tickSet, error) {
	out := tickSet{}
	for _, section := range sections {
		start := strings.Index(data, `(:id "`+section+`" :type :roadmap`)
		if start < 0 {
			return nil, fmt.Errorf("%s: no roadmap section %q", roadmapDataPath, section)
		}
		rest := data[start:]
		rowsAt := strings.Index(rest, ":rows")
		colsAt := strings.Index(rest, ":columns")
		if rowsAt < 0 || colsAt < rowsAt {
			return nil, fmt.Errorf("%s: section %q has no :rows before :columns", roadmapDataPath, section)
		}
		ids := map[string]string{} // title -> id
		for _, m := range roadmapRowPair.FindAllStringSubmatch(rest[rowsAt:colsAt], -1) {
			ids[strings.ReplaceAll(m[2], `\"`, `"`)] = m[1]
		}
		begin := "<!-- nova-work:" + section + ":start -->"
		end := "<!-- nova-work:" + section + ":end -->"
		b := strings.Index(md, begin)
		e := strings.Index(md, end)
		if b < 0 || e < b {
			return nil, fmt.Errorf("%s: no table for section %q", roadmapTablePath, section)
		}
		var columns []string
		for line := range strings.SplitSeq(md[b+len(begin):e], "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "|---") {
				continue
			}
			cells := strings.Split(strings.Trim(line, "|"), "|")
			for i := range cells {
				cells[i] = strings.TrimSpace(cells[i])
			}
			if columns == nil {
				columns = cells[1:]
				continue
			}
			title := cells[0]
			if title == "complete" {
				continue
			}
			id, ok := ids[title]
			if !ok {
				return nil, fmt.Errorf("%s: section %q row %q has no id in %s", roadmapTablePath, section, title, roadmapDataPath)
			}
			key := section + "/" + id
			out[key] = map[string]bool{}
			for i, cell := range cells[1:] {
				if i < len(columns) && cell == "✅" {
					out[key][columns[i]] = true
				}
			}
		}
		if columns == nil {
			return nil, fmt.Errorf("%s: section %q has no header row", roadmapTablePath, section)
		}
	}
	return out, nil
}
