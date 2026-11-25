package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// makeDB is what make's own database (make -p) says of the checkout's make
// files, read without running a recipe: each rule's prerequisites and
// recipe, the pattern rules, and every variable's value as written. A dry
// run (make -n) is no way to read a recipe here: make runs every recipe
// line that names $(MAKE) even under -n, and this Makefile's recursive
// lines build and test for real.
type makeDB struct {
	rules    map[string]*makeRule
	patterns []*makeRule // in the database's order
	vars     map[string]string
}

type makeRule struct {
	target  string
	prereqs []string
	recipe  []string
}

// makeDBSentinel is the goal the database is printed for: no rule makes
// it, so make prints the database, says so and stops, running nothing.
const makeDBSentinel = "__schema_test_make_database__"

// makeDBArgv is the command that prints the database.
var makeDBArgv = []string{"make", "--no-print-directory", "-p", "-q", makeDBSentinel}

// targetVarLine is a target-specific variable in the database
// ("pins-source-cpp: PINS_CPP_GENERATE = ..."), which writes no
// prerequisite.
var targetVarLine = regexp.MustCompile(`^\s*(export\s+|override\s+|private\s+)*[A-Za-z_][A-Za-z0-9_.]*\s*(\+|:|::|\?|!)?=`)

// parseMakeDB reads make -p's output. Output that is not a whole database
// (no Files section, or a stop other than the sentinel's missing rule) is
// an error: a database read in part would answer "not reached" for what it
// never read.
func parseMakeDB(out string) (*makeDB, error) {
	if !strings.Contains(out, "\n# Files\n") || !strings.Contains(out, "\n# Variables\n") || !strings.Contains(out, "\n# finished make data base") && !strings.Contains(out, "# VPATH Search Paths") {
		return nil, fmt.Errorf("make -p printed no whole database:\n%s", tail(out, 20))
	}
	if !strings.Contains(out, "No rule to make target '"+makeDBSentinel+"'") {
		return nil, fmt.Errorf("make -p did not stop at the sentinel goal %s, so the database may be partial:\n%s", makeDBSentinel, tail(out, 20))
	}
	db := &makeDB{rules: map[string]*makeRule{}, vars: map[string]string{}}
	section := ""
	var cur *makeRule
	var def string // the variable a define block is collecting
	lines := strings.Split(out, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch line {
		case "# Variables", "# Implicit Rules", "# Files", "# VPATH Search Paths", "# Directories", "# Pattern-specific Variable Values":
			section = line
			cur = nil
			continue
		}
		if def != "" {
			if line == "endef" {
				def = ""
				continue
			}
			db.vars[def] += line + "\n"
			continue
		}
		switch section {
		case "# Variables", "# Pattern-specific Variable Values":
			if name, ok := strings.CutPrefix(line, "define "); ok && section == "# Variables" {
				def = strings.TrimSpace(name)
				db.vars[def] = ""
				continue
			}
			if section != "# Variables" || line == "" || line[0] == '#' {
				continue
			}
			for _, op := range []string{" := ", " = ", " ::= ", " ?= ", " += "} {
				if name, val, ok := strings.Cut(line, op); ok && !strings.ContainsAny(name, " \t$") {
					db.vars[name] = val
					break
				}
			}
		case "# Implicit Rules", "# Files":
			if line == "" || line[0] == '#' {
				if line == "" {
					cur = nil
				}
				continue
			}
			if line[0] == '\t' {
				if cur != nil {
					cur.recipe = append(cur.recipe, line[1:])
				}
				continue
			}
			target, rest, ok := strings.Cut(line, ":")
			if !ok || strings.HasPrefix(rest, "=") || targetVarLine.MatchString(rest) {
				continue
			}
			rest = strings.TrimPrefix(rest, ":") // a double-colon rule
			r := &makeRule{target: target}
			for p := range strings.FieldsSeq(rest) {
				if p != "|" {
					r.prereqs = append(r.prereqs, p)
				}
			}
			if section == "# Implicit Rules" {
				if strings.Count(target, "%") == 1 {
					db.patterns = append(db.patterns, r)
					cur = r
				}
				continue
			}
			if prev, dup := db.rules[target]; dup {
				prev.prereqs = append(prev.prereqs, r.prereqs...)
				cur = prev
				continue
			}
			db.rules[target] = r
			cur = r
		}
	}
	if len(db.rules) == 0 {
		return nil, fmt.Errorf("make -p printed a database with no rules")
	}
	return db, nil
}

// pattern is the pattern rule that makes file, its stem substituted, or
// nil: the matching rule with the shortest stem, the first such in the
// database's order (make's own choice, but for whether a prerequisite can
// be made, which the database cannot say).
func (db *makeDB) pattern(file string) *makeRule {
	var best *makeRule
	bestStem := -1
	for _, p := range db.patterns {
		pre, suf, _ := strings.Cut(p.target, "%")
		if len(file) < len(pre)+len(suf) || !strings.HasPrefix(file, pre) || !strings.HasSuffix(file, suf) {
			continue
		}
		stem := file[len(pre) : len(file)-len(suf)]
		if bestStem >= 0 && len(stem) >= bestStem {
			continue
		}
		r := &makeRule{target: file, recipe: p.recipe}
		for _, q := range p.prereqs {
			r.prereqs = append(r.prereqs, strings.ReplaceAll(q, "%", stem))
		}
		best, bestStem = r, len(stem)
	}
	return best
}

// makeVarRef is a variable a recipe expands: $(NAME), ${NAME}, $(call
// NAME,...).
var makeVarRef = regexp.MustCompile(`\$[({](?:call\s+)?([A-Za-z_][A-Za-z0-9_]*)`)

// reach is everything make would make for target: its prerequisites, all
// the way down, through the pattern rules and the targets its recipes make
// again by $(MAKE); and the text of every recipe it would run, with the
// value of every variable those recipes expand, all the way down. named is
// what those recipes name as make runs them: each recipe with the
// variables it expands put in place (extract.SubstMakeVars), and the body
// of each function it calls so; never a variable's value on its own,
// where a directory that only starts a longer path (X := test/x, used as
// $(X)/a.schema) would read as a word of its own.
func (db *makeDB) reach(target string) (files map[string]bool, text, named []string) {
	files = map[string]bool{}
	seenVar := map[string]bool{}
	seenCall := map[string]bool{}
	var expand func(s string)
	expand = func(s string) {
		text = append(text, s)
		for _, m := range makeVarRef.FindAllStringSubmatch(s, -1) {
			if v, ok := db.vars[m[1]]; ok && !seenVar[m[1]] {
				seenVar[m[1]] = true
				expand(v)
			}
		}
	}
	var name func(s string)
	name = func(s string) {
		named = append(named, extract.SubstMakeVars(s, db.vars))
		for _, m := range makeVarRef.FindAllStringSubmatch(s, -1) {
			if !strings.Contains(m[0], "call") {
				continue
			}
			if v, ok := db.vars[m[1]]; ok && !seenCall[m[1]] {
				seenCall[m[1]] = true
				name(v)
			}
		}
	}
	queue := []string{target}
	files[target] = true
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		r := db.rules[f]
		if r == nil || len(r.recipe) == 0 && len(r.prereqs) == 0 {
			if p := db.pattern(f); p != nil {
				if r == nil {
					r = p
				} else {
					r = &makeRule{target: f, prereqs: append(slices0(r.prereqs), p.prereqs...), recipe: p.recipe}
				}
			}
		}
		if r == nil {
			continue
		}
		next := slices0(r.prereqs)
		recipe := strings.Join(r.recipe, "\n")
		if recipe != "" {
			expand(recipe)
			name(recipe)
			next = append(next, remade(recipe, db)...)
		}
		for _, p := range next {
			if !files[p] {
				files[p] = true
				queue = append(queue, p)
			}
		}
	}
	return files, text, named
}

func slices0(s []string) []string { return append([]string(nil), s...) }

// remade is the targets a recipe makes again with $(MAKE): each word after
// $(MAKE) on its line that is a rule of the database.
func remade(recipe string, db *makeDB) []string {
	var out []string
	for line := range strings.SplitSeq(recipe, "\n") {
		for _, mk := range []string{"$(MAKE)", "${MAKE}"} {
			_, after, ok := strings.Cut(line, mk)
			if !ok {
				continue
			}
			for w := range strings.FieldsSeq(after) {
				if w == ";" || w == "&&" || w == "||" || w == "|" {
					break
				}
				if _, ok := db.rules[w]; ok {
					out = append(out, w)
				}
			}
		}
	}
	return out
}
