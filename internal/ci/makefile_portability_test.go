package ci

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

var (
	// Matches bare GNU `timeout <duration> ...` in command position (after
	// start-of-line, shell control keywords, subshells, pipes/chains, or
	// leading VAR=val assignments), while never matching `--timeout` or
	// `-timeout` flags.
	bareTimeoutCmdRe = regexp.MustCompile(
		`(?:^|[;&|({]\s*|\b(?:if|then|else|elif|do)\s+|!\s+)` +
			`[@+-]*` +
			`(?:[A-Za-z_][A-Za-z0-9_]*=(?:"[^"]*"|'[^']*'|[^\s;&|]+)\s+)*` +
			`(?:env\s+)?timeout\s+\S`,
	)

	makeTargetRuleRe   = regexp.MustCompile(`^([^\s:#=][^:=]*?)\s*:\s*([^=].*)?$`)
	makeSimpleVarRe    = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*(?::=|\?=|=)\s*(.*)$`)
	makeVarRefRe       = regexp.MustCompile(`\$\(([A-Za-z_][A-Za-z0-9_]*)\)`)
	makeDefineHeaderRe = regexp.MustCompile(`^define\s+([A-Za-z0-9_.-]+)`)

	harnessRunRe = regexp.MustCompile(
		`(?:conformance-harness(?:-vm)?|\$\([A-Za-z0-9_]*HARNESS[A-Za-z0-9_]*\))\s+run\b([^;&|>\n]*)`,
	)
	workFlagRe = regexp.MustCompile(`(?:^|\s)--work(?:=|\s+)(?:"([^"]+)"|'([^']+)'|([^\s"'\x60;&|>]+))`)

	cardsGenTargetRe = regexp.MustCompile(`^cards(?:-[a-z0-9]+)?-generate$`)
	ncScriptCallRe   = regexp.MustCompile(`\b(?:sh|bash)\s+(test/[^\s;&|"']*negative-control[^\s;&|"']*)\s*([^\s;&|"']*)`)
	cdBuildTreeRe    = regexp.MustCompile(`(?:^|[;&|(\s])cd\s+"?(?:\$\([A-Za-z0-9_]+\)/)?(build/tables-generated[A-Za-z0-9_-]*(?:/[A-Za-z0-9_.-]+)?|build/cards-[A-Za-z0-9_-]+)(?:/[^\s;&|)"']*)?"?`)
	readBuildTreeRe  = regexp.MustCompile(`\b(?:overlay_one|sed\b[^;&|]*|cp\b[^;&|]*|grep\b[^;&|]*)\s+"?(?:\$\([A-Za-z0-9_]+\)/)?(build/tables-generated[A-Za-z0-9_-]*(?:/[A-Za-z0-9_.-]+)?)(?:/[^\s;&|)"']*)?"?`)
	createBuildDirRe = regexp.MustCompile(`(?:\bmkdir\s+-p|--out|cp\s+-[rR]\s+\S+)\s+"?(?:\$\([A-Za-z0-9_]+\)/)?(build/[A-Za-z0-9_./-]+)"?`)
)

type makeLogicalLine struct {
	lineNum  int
	rawFirst string
	joined   string
}

func joinContinuations(content string) []makeLogicalLine {
	rawLines := strings.Split(content, "\n")
	out := make([]makeLogicalLine, 0, len(rawLines))
	for i := 0; i < len(rawLines); {
		start := i + 1
		first := rawLines[i]
		var b strings.Builder
		b.WriteString(strings.TrimRight(first, "\r"))
		for strings.HasSuffix(b.String(), `\`) && i+1 < len(rawLines) {
			cur := b.String()
			b.Reset()
			b.WriteString(cur[:len(cur)-1])
			b.WriteByte(' ')
			i++
			b.WriteString(strings.TrimSpace(strings.TrimRight(rawLines[i], "\r")))
		}
		i++
		out = append(out, makeLogicalLine{
			lineNum:  start,
			rawFirst: first,
			joined:   b.String(),
		})
	}
	return out
}

// stripUnquotedComment removes a `# ...` comment when `#` occurs outside single
// or double quotes and after start-of-line or whitespace.
func stripUnquotedComment(s string) string {
	var quote byte
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t' || s[i-1] == ';') {
			return s[:i]
		}
	}
	return s
}

type harnessWorkSite struct {
	file  string
	line  int
	scope string
	work  string
}

type makeTargetDef struct {
	file    string
	line    int
	prereqs []string
	recipes []makeLogicalLine
}

func expandMakeVars(s string, vars map[string]string) string {
	cur := s
	for range 4 {
		next := makeVarRefRe.ReplaceAllStringFunc(cur, func(m string) string {
			sub := makeVarRefRe.FindStringSubmatch(m)
			if len(sub) == 2 {
				if val, ok := vars[sub[1]]; ok {
					return val
				}
			}
			return m
		})
		if next == cur {
			break
		}
		cur = next
	}
	return cur
}

// extractScriptModeSection returns the lines relevant to `mode` inside a shell
// negative-control script: top-level helper functions / preamble outside `case
// "$mode" in ... esac` that are invoked, plus the matching `mode)` case arm (or
// the whole script when `mode == ""` or no `case` dispatch exists).
func extractScriptModeSection(content, mode string) []makeLogicalLine {
	lines := joinContinuations(content)
	if mode == "" || !strings.Contains(content, "case ") {
		return lines
	}
	var preamble []makeLogicalLine
	var armLines []makeLogicalLine
	helpers := map[string][]makeLogicalLine{}
	curHelper := ""
	helperDepth := 0
	inCase := false
	inMatchingArm := false
	caseDepth := 0
	armHeaderRe := regexp.MustCompile(`^([A-Za-z0-9_|*-]+)\)\s*(.*)$`)
	funcHeaderRe := regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*\(\)\s*\{$`)

	for _, ll := range lines {
		code := strings.TrimSpace(stripUnquotedComment(ll.joined))
		if code == "" {
			continue
		}
		if curHelper != "" {
			helperDepth += strings.Count(code, "{") - strings.Count(code, "}")
			if helperDepth <= 0 {
				curHelper = ""
				helperDepth = 0
				continue
			}
			helpers[curHelper] = append(helpers[curHelper], ll)
			continue
		}
		if !inCase {
			if fm := funcHeaderRe.FindStringSubmatch(code); fm != nil {
				curHelper = fm[1]
				helperDepth = 1
				continue
			}
		}
		if strings.HasPrefix(code, "case ") && strings.Contains(code, " in") {
			if !inCase {
				inCase = true
				caseDepth = 1
				inMatchingArm = false
				continue
			}
			caseDepth++
		}
		if inCase {
			if code == "esac" {
				caseDepth--
				if caseDepth == 0 {
					inCase = false
					inMatchingArm = false
				}
				continue
			}
			if caseDepth == 1 {
				if m := armHeaderRe.FindStringSubmatch(code); m != nil {
					inMatchingArm = false
					for pat := range strings.SplitSeq(m[1], "|") {
						if strings.TrimSpace(pat) == mode {
							inMatchingArm = true
						}
					}
					if inMatchingArm && m[2] != "" {
						armLines = append(armLines, makeLogicalLine{lineNum: ll.lineNum, rawFirst: ll.rawFirst, joined: m[2]})
					}
					continue
				}
				if inMatchingArm && strings.HasSuffix(code, ";;") {
					trimmed := strings.TrimSpace(strings.TrimSuffix(code, ";;"))
					if trimmed != "" {
						armLines = append(armLines, makeLogicalLine{lineNum: ll.lineNum, rawFirst: ll.rawFirst, joined: trimmed})
					}
					inMatchingArm = false
					continue
				}
			}
			if inMatchingArm {
				armLines = append(armLines, ll)
			}
			continue
		}
		preamble = append(preamble, ll)
	}

	var armText strings.Builder
	for _, al := range armLines {
		armText.WriteString(al.joined)
		armText.WriteByte(10)
	}
	joinedArm := armText.String()
	out := append([]makeLogicalLine(nil), preamble...)
	for hname, hbody := range helpers {
		if strings.Contains(joinedArm, hname) {
			out = append(out, hbody...)
		}
	}
	out = append(out, armLines...)
	return out
}

func collectRequiredPreexistingBuildTrees(lines []makeLogicalLine, vars map[string]string) []string {
	created := map[string]bool{}
	var needed []string
	seen := map[string]bool{}

	recordCreated := func(p string) {
		p = strings.TrimRight(p, "/")
		created[p] = true
	}
	isCreated := func(p string) bool {
		p = strings.TrimRight(p, "/")
		for c := range created {
			if p == c || strings.HasPrefix(p, c+"/") || strings.HasPrefix(c, p+"/") {
				return true
			}
		}
		return false
	}

	for _, ll := range lines {
		code := expandMakeVars(strings.TrimSpace(stripUnquotedComment(ll.joined)), vars)
		if code == "" {
			continue
		}
		for _, m := range createBuildDirRe.FindAllStringSubmatch(code, -1) {
			recordCreated(m[1])
		}
		for _, re := range []*regexp.Regexp{cdBuildTreeRe, readBuildTreeRe} {
			for _, m := range re.FindAllStringSubmatch(code, -1) {
				tree := strings.TrimRight(m[1], "/")
				if strings.Contains(tree, "$") || isCreated(tree) {
					continue
				}
				if !seen[tree] {
					seen[tree] = true
					needed = append(needed, tree)
				}
			}
		}
	}
	return needed
}

func targetCoversBuildTree(
	target string,
	tree string,
	defs map[string]*makeTargetDef,
	phony map[string]bool,
	vars map[string]string,
	visited map[string]bool,
) bool {
	if visited[target] {
		return false
	}
	visited[target] = true

	// A non-.PHONY file/stamp target whose path is inside `tree` or its parent
	// generated root (`build/tables-generated-go/.stamp` covers
	// `build/tables-generated-go/pointers`, and
	// `build/tables-generated-go/cards/.stamp` covers
	// `build/tables-generated-go/cards`) satisfies the prerequisite.
	if !phony[target] && strings.HasPrefix(target, "build/") {
		cleanTargetDir := strings.TrimSuffix(target, "/.stamp")
		cleanTargetDir = strings.TrimRight(cleanTargetDir, "/")
		if tree == cleanTargetDir || strings.HasPrefix(tree, cleanTargetDir+"/") {
			// Ensure `build/tables-generated-go/.stamp` only covers `cards` if its
			// own recipe actually builds `cards`!
			if strings.HasSuffix(tree, "/cards") && cleanTargetDir != tree {
				if def := defs[target]; def != nil {
					builtCards := false
					for _, r := range def.recipes {
						if strings.Contains(expandMakeVars(r.joined, vars), tree) {
							builtCards = true
							break
						}
					}
					if builtCards {
						return true
					}
				}
			} else {
				return true
			}
		}
	}

	def := defs[target]
	if def == nil {
		return false
	}
	for _, p := range def.prereqs {
		ep := expandMakeVars(p, vars)
		for tok := range strings.FieldsSeq(ep) {
			if targetCoversBuildTree(tok, tree, defs, phony, vars, visited) {
				return true
			}
		}
	}
	return false
}

func checkNegativeControlMakePrereqs(
	makeFiles map[string]string,
	ncFiles map[string]string,
) []string {
	var errs []string
	vars := map[string]string{}
	phony := map[string]bool{}
	defs := map[string]*makeTargetDef{}

	var makeNames []string
	for name := range makeFiles {
		makeNames = append(makeNames, name)
	}
	sort.Strings(makeNames)

	// Pass 1: collect simple variables, `.PHONY` declarations, and target DAG.
	for _, name := range makeNames {
		lines := joinContinuations(makeFiles[name])
		inDefine := false
		var curTargets []string

		for _, ll := range lines {
			trimmed := strings.TrimSpace(stripUnquotedComment(ll.joined))
			if trimmed == "" {
				continue
			}
			if trimmed == "endef" {
				inDefine = false
				curTargets = nil
				continue
			}
			if makeDefineHeaderRe.MatchString(trimmed) {
				inDefine = true
				curTargets = nil
				continue
			}
			if inDefine {
				continue
			}
			if strings.HasPrefix(ll.rawFirst, "\t") {
				for _, tname := range curTargets {
					if d := defs[tname]; d != nil {
						d.recipes = append(d.recipes, ll)
					}
				}
				continue
			}
			curTargets = nil
			if vm := makeSimpleVarRe.FindStringSubmatch(trimmed); vm != nil {
				vars[vm[1]] = strings.TrimSpace(vm[2])
				continue
			}
			if rm := makeTargetRuleRe.FindStringSubmatch(trimmed); rm != nil {
				lhs := expandMakeVars(strings.TrimSpace(rm[1]), vars)
				rhs := strings.TrimSpace(rm[2])
				if lhs == ".PHONY" {
					for tok := range strings.FieldsSeq(expandMakeVars(rhs, vars)) {
						phony[tok] = true
					}
					continue
				}
				// Strip order-only `|` separator while keeping all prerequisites.
				rhs = strings.ReplaceAll(rhs, "|", " ")
				prereqs := strings.Fields(rhs)
				for tname := range strings.FieldsSeq(lhs) {
					if strings.ContainsAny(tname, "$%()") {
						continue
					}
					d := defs[tname]
					if d == nil {
						d = &makeTargetDef{file: name, line: ll.lineNum}
						defs[tname] = d
					}
					d.prereqs = append(d.prereqs, prereqs...)
					curTargets = append(curTargets, tname)
				}
			}
		}
	}

	var targetNames []string
	for tname := range defs {
		targetNames = append(targetNames, tname)
	}
	slices.Sort(targetNames)

	// Rule A: No `cards*generate` target may carry a direct `rm -rf` / `schema
	// generate` recipe; it must alias a real `.stamp` file target so sub-makes and
	// `-j` never wipe `build/.../cards` under a sibling target.
	for _, tname := range targetNames {
		if !cardsGenTargetRe.MatchString(tname) {
			continue
		}
		d := defs[tname]
		if len(d.recipes) > 0 {
			errs = append(errs, fmt.Sprintf(
				"%s:%d (%s): `cards*generate` target runs commands directly instead of aliasing a `.stamp` file target (sub-makes and `make -j` re-run `.PHONY` recipes and wipe `build/.../cards` mid-flight)",
				d.file, d.line, tname,
			))
		}
		hasStamp := false
		for _, p := range d.prereqs {
			ep := expandMakeVars(p, vars)
			if strings.HasSuffix(ep, "/.stamp") || strings.HasSuffix(ep, ".stamp") {
				hasStamp = true
				break
			}
		}
		if !hasStamp {
			errs = append(errs, fmt.Sprintf(
				"%s:%d (%s): `cards*generate` target lacks a `.stamp` file prerequisite",
				d.file, d.line, tname,
			))
		}
	}

	// Rule B: Every `*-negative-control` target that reads or `cd`s into a
	// pre-existing `build/tables-generated*` or `build/cards-*` tree (directly or
	// via `sh test/.../negative-control* <mode>`) must have a transitive
	// non-`.PHONY` file/stamp prerequisite building that tree.
	for _, tname := range targetNames {
		if !strings.HasSuffix(tname, "-negative-control") {
			continue
		}
		d := defs[tname]
		neededTrees := collectRequiredPreexistingBuildTrees(d.recipes, vars)

		for _, r := range d.recipes {
			code := expandMakeVars(strings.TrimSpace(stripUnquotedComment(r.joined)), vars)
			for _, sm := range ncScriptCallRe.FindAllStringSubmatch(code, -1) {
				scriptPath := sm[1]
				mode := sm[2]
				if scriptBody, ok := ncFiles[scriptPath]; ok {
					sec := extractScriptModeSection(scriptBody, mode)
					for _, tree := range collectRequiredPreexistingBuildTrees(sec, vars) {
						if !slices.Contains(neededTrees, tree) {
							neededTrees = append(neededTrees, tree)
						}
					}
				}
			}
		}

		for _, tree := range neededTrees {
			if !targetCoversBuildTree(tname, tree, defs, phony, vars, map[string]bool{}) {
				errs = append(errs, fmt.Sprintf(
					"%s:%d (%s): negative-control accesses pre-existing `%s` without a non-`.PHONY` file/stamp Make prerequisite building `%s` (fails cold standalone or races sub-makes)",
					d.file, d.line, tname, tree, tree,
				))
			}
		}
	}

	return errs
}

func checkMakefileAndScriptPortability(
	makeFiles map[string]string,
	shFiles map[string]string,
	ncFiles ...map[string]string,
) ([]string, int) {
	var errs []string
	var workSites []harnessWorkSite

	var makeNames []string
	for name := range makeFiles {
		makeNames = append(makeNames, name)
	}
	sort.Strings(makeNames)

	for _, name := range makeNames {
		lines := joinContinuations(makeFiles[name])
		scope := "<top>"
		inDefine := false
		scopeHasWildcards := false
		scopeWildcardsLine := 0
		scopeHasBsdtar := false

		flushTarScope := func() {
			if scopeHasWildcards && !scopeHasBsdtar {
				errs = append(errs, fmt.Sprintf(
					"%s:%d (%s): uses GNU-only `tar --wildcards` without a `bsdtar` fallback branch",
					name, scopeWildcardsLine, scope,
				))
			}
			scopeHasWildcards = false
			scopeWildcardsLine = 0
			scopeHasBsdtar = false
		}

		for _, ll := range lines {
			trimmed := strings.TrimSpace(ll.joined)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if trimmed == "endef" {
				flushTarScope()
				inDefine = false
				scope = "<top>"
				continue
			}
			if m := makeDefineHeaderRe.FindStringSubmatch(trimmed); m != nil {
				flushTarScope()
				inDefine = true
				scope = "define:" + m[1]
				continue
			}
			isRecipe := strings.HasPrefix(ll.rawFirst, "\t") || inDefine
			if !isRecipe {
				if m := makeTargetRuleRe.FindStringSubmatch(ll.joined); m != nil {
					flushTarScope()
					scope = strings.TrimSpace(m[1])
				}
				continue
			}

			code := strings.TrimSpace(stripUnquotedComment(ll.joined))
			if code == "" {
				continue
			}

			if bareTimeoutCmdRe.MatchString(code) {
				errs = append(errs, fmt.Sprintf(
					"%s:%d (%s): invokes bare GNU `timeout` CLI (fails on macOS runners without coreutils; use a tool flag such as `--timeout` or Go context deadlines): %s",
					name, ll.lineNum, scope, code,
				))
			}

			if strings.Contains(code, "bsdtar") {
				scopeHasBsdtar = true
			}
			if strings.Contains(code, "tar") && strings.Contains(code, "--wildcards") {
				scopeHasWildcards = true
				if scopeWildcardsLine == 0 {
					scopeWildcardsLine = ll.lineNum
				}
			}

			for _, hm := range harnessRunRe.FindAllStringSubmatch(code, -1) {
				args := hm[1]
				wm := workFlagRe.FindStringSubmatch(args)
				work := ""
				if wm != nil {
					for _, g := range wm[1:] {
						if g != "" {
							work = strings.TrimRight(g, "/")
							break
						}
					}
				}
				workSites = append(workSites, harnessWorkSite{
					file:  name,
					line:  ll.lineNum,
					scope: scope,
					work:  work,
				})
			}
		}
		flushTarScope()
	}

	// Validate `--work` invariants across all conformance-harness run invocations.
	byConcreteWork := map[string][]harnessWorkSite{}
	for _, s := range workSites {
		if s.work == "" {
			errs = append(errs, fmt.Sprintf(
				"%s:%d (%s): `conformance-harness run` omits `--work` (falls back to shared default `build/conformance` and races under `make -j`)",
				s.file, s.line, s.scope,
			))
			continue
		}
		if s.work == "build/conformance/fixtures" || strings.HasSuffix(s.work, "/conformance/fixtures") {
			errs = append(errs, fmt.Sprintf(
				"%s:%d (%s): `conformance-harness run` specifies shared fixtures path `--work %s`",
				s.file, s.line, s.scope, s.work,
			))
			continue
		}
		if strings.Contains(s.work, "$(1)") || strings.Contains(s.work, "$$") || strings.Contains(s.work, "$@") {
			continue
		}
		byConcreteWork[s.work] = append(byConcreteWork[s.work], s)
	}

	var concretePaths []string
	for w := range byConcreteWork {
		concretePaths = append(concretePaths, w)
	}
	sort.Strings(concretePaths)
	for _, w := range concretePaths {
		sites := byConcreteWork[w]
		distinctTargets := map[string]harnessWorkSite{}
		for _, s := range sites {
			key := s.file + "::" + s.scope
			if _, exists := distinctTargets[key]; !exists {
				distinctTargets[key] = s
			}
		}
		if len(distinctTargets) > 1 {
			var parts []string
			for _, s := range sites {
				parts = append(parts, fmt.Sprintf("%s:%d (%s)", s.file, s.line, s.scope))
			}
			sort.Strings(parts)
			errs = append(errs, fmt.Sprintf(
				"multiple Make targets share `--work %s` (races under `make -j`): %s",
				w, strings.Join(parts, ", "),
			))
		}
	}

	// Shell scripts (`tools/**/*.sh`, `make/**/*.sh`).
	var shNames []string
	for name := range shFiles {
		shNames = append(shNames, name)
	}
	sort.Strings(shNames)

	for _, name := range shNames {
		content := shFiles[name]
		hasBsdtar := strings.Contains(content, "bsdtar")
		for _, ll := range joinContinuations(content) {
			code := strings.TrimSpace(stripUnquotedComment(ll.joined))
			if code == "" {
				continue
			}
			if bareTimeoutCmdRe.MatchString(code) {
				errs = append(errs, fmt.Sprintf(
					"%s:%d: invokes bare GNU `timeout` CLI (fails on macOS runners without coreutils): %s",
					name, ll.lineNum, code,
				))
			}
			if strings.Contains(code, "tar") && strings.Contains(code, "--wildcards") && !hasBsdtar {
				errs = append(errs, fmt.Sprintf(
					"%s:%d: uses GNU-only `tar --wildcards` without a `bsdtar` fallback branch",
					name, ll.lineNum,
				))
			}
		}
	}

	mergedNC := map[string]string{}
	for _, m := range ncFiles {
		maps.Copy(mergedNC, m)
	}
	errs = append(errs, checkNegativeControlMakePrereqs(makeFiles, mergedNC)...)

	sort.Strings(errs)
	return errs, len(workSites)
}

func loadRepoPortabilityInputs(t *testing.T, root string) (map[string]string, map[string]string, map[string]string) {
	t.Helper()
	makeFiles := map[string]string{}
	shFiles := map[string]string{}
	ncFiles := map[string]string{}

	mfBytes, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makeFiles["Makefile"] = string(mfBytes)

	for _, dir := range []string{"make", "tools"} {
		absDir := filepath.Join(root, dir)
		err := filepath.WalkDir(absDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			switch filepath.Ext(path) {
			case ".mk":
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				makeFiles[rel] = string(b)
			case ".sh":
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				shFiles[rel] = string(b)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}

	testDir := filepath.Join(root, "test")
	err = filepath.WalkDir(testDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() || !strings.Contains(d.Name(), "negative-control") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		ncFiles[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walk test negative-control scripts: %v", err)
	}

	return makeFiles, shFiles, ncFiles
}

func TestMakefilePortability(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	makeFiles, shFiles, ncFiles := loadRepoPortabilityInputs(t, root)
	if len(makeFiles) < 20 {
		t.Fatalf("loaded only %d Make files; expected Makefile + make/*.mk", len(makeFiles))
	}
	if len(shFiles) < 10 {
		t.Fatalf("loaded only %d shell scripts under make/ and tools/", len(shFiles))
	}
	if len(ncFiles) < 20 {
		t.Fatalf("loaded only %d negative-control scripts under test/", len(ncFiles))
	}

	errs, runSites := checkMakefileAndScriptPortability(makeFiles, shFiles, ncFiles)
	if runSites < 100 {
		t.Fatalf("scanned only %d conformance-harness run invocations; expected >= 100", runSites)
	}
	for _, e := range errs {
		t.Error(e)
	}
}

func TestMakefilePortabilityNegativeControls(t *testing.T) {
	t.Parallel()

	cleanMake := map[string]string{
		"Makefile": strings.Join([]string{
			"CARDS_GO_STAMP := build/tables-generated-go/cards/.stamp",
			".PHONY: cards-generate cards-conformance-go-negative-control",
			"cards-generate: $(CARDS_GO_STAMP)",
			"$(CARDS_GO_STAMP): bin/schema",
			"\t./bin/schema generate --lang go --out build/tables-generated-go/cards tables/cards",
			"\t@touch $@",
			"cards-conformance-go-negative-control: build/conformance-harness $(CARDS_GO_STAMP)",
			"\tsh test/go-tables/negative-control cards",
			"",
			"target-a:",
			"\t@./build/conformance-harness wire-fuzz --timeout 30s --seed 1 --n 10",
			"\tgo test -timeout 60s ./...",
			"\t@if command -v bsdtar >/dev/null 2>&1; then bsdtar -xf a.tar '*.so'; else tar --wildcards -xf a.tar '*.so'; fi",
			"\t./build/conformance-harness run --only cpp --work build/work-a",
			"",
			"target-b:",
			"\t./build/conformance-harness-vm run --only js --work build/work-b",
		}, "\n"),
	}
	cleanSh := map[string]string{
		"tools/ok.sh": "#!/bin/sh\n# timeout in comment is fine\nif command -v bsdtar >/dev/null 2>&1; then bsdtar -xf x.tar '*.a'; else tar --wildcards -xf x.tar '*.a'; fi\n./run --timeout 10s\n",
	}
	cleanNC := map[string]string{
		"test/go-tables/negative-control": "#!/bin/sh\ncase \"$mode\" in\ncards)\n  (cd build/tables-generated-go/cards/cmd/cards-driver && go build .)\n  ;;\nesac\n",
	}
	if errs, sites := checkMakefileAndScriptPortability(cleanMake, cleanSh, cleanNC); len(errs) != 0 || sites != 2 {
		t.Fatalf("expected clean baseline (2 sites, 0 errs), got sites=%d errs=%v", sites, errs)
	}

	cases := []struct {
		name      string
		makeFiles map[string]string
		shFiles   map[string]string
		ncFiles   map[string]string
		wantSub   string
	}{
		{
			name: "bare timeout in make recipe",
			makeFiles: map[string]string{
				"make/bad.mk": "fuzz-target:\n\t@timeout 30s ./build/conformance-harness wire-fuzz\n",
			},
			wantSub: "invokes bare GNU `timeout` CLI",
		},
		{
			name: "env-prefixed bare timeout in make if-guard",
			makeFiles: map[string]string{
				"make/bad.mk": "fuzz-nc:\n\t@if CARDS_DROP=1 timeout 20s ./build/conformance-harness wire-fuzz; then exit 1; fi\n",
			},
			wantSub: "invokes bare GNU `timeout` CLI",
		},
		{
			name: "bare timeout in tools shell script",
			shFiles: map[string]string{
				"tools/bad.sh": "#!/bin/sh\ntimeout 15 python3 check.py\n",
			},
			wantSub: "tools/bad.sh:2: invokes bare GNU `timeout` CLI",
		},
		{
			name: "tar --wildcards without bsdtar in make recipe",
			makeFiles: map[string]string{
				"make/bad.mk": "dist-tool:\n\ttar -xzf dist/tool.tar.gz --wildcards '*/bin/tool'\n",
			},
			wantSub: "uses GNU-only `tar --wildcards` without a `bsdtar` fallback",
		},
		{
			name: "tar --wildcards without bsdtar in shell script",
			shFiles: map[string]string{
				"tools/unpack.sh": "#!/bin/sh\ntar --wildcards -xf pkg.tar '*.bin'\n",
			},
			wantSub: "tools/unpack.sh:2: uses GNU-only `tar --wildcards` without a `bsdtar` fallback",
		},
		{
			name: "conformance-harness run missing --work",
			makeFiles: map[string]string{
				"Makefile": "bad-conf:\n\t./build/conformance-harness run --only cpp\n",
			},
			wantSub: "omits `--work`",
		},
		{
			name: "conformance-harness run with bare build/conformance/fixtures",
			makeFiles: map[string]string{
				"make/bad.mk": "bad-fixtures:\n\t./build/conformance-harness run --drivers a.txt --work build/conformance/fixtures\n",
			},
			wantSub: "specifies shared fixtures path `--work build/conformance/fixtures`",
		},
		{
			name: "two distinct targets sharing concrete --work path",
			makeFiles: map[string]string{
				"make/a.mk": "target-one:\n\t./build/conformance-harness run --only c --work build/shared-c-work\n",
				"make/b.mk": "target-two:\n\t./build/conformance-harness run --only c --work build/shared-c-work\n",
			},
			wantSub: "multiple Make targets share `--work build/shared-c-work`",
		},
		{
			name: "pre-fix phony cards-generate and cards-conformance-go-negative-control without stamp",
			makeFiles: map[string]string{
				"make/go.mk": strings.Join([]string{
					".PHONY: cards-generate cards-driver-go cards-conformance-go-negative-control",
					"cards-generate: bin/schema",
					"\t@rm -rf build/tables-generated-go/cards",
					"\t./bin/schema generate --lang go --out build/tables-generated-go/cards tables/cards",
					"cards-driver-go: cards-generate",
					"\t@mkdir -p build/tables-generated-go/cards/cmd/cards-driver",
					"\tcd build/tables-generated-go/cards/cmd/cards-driver && go build -o build/cards-driver-go .",
					"cards-conformance-go-negative-control: build/conformance-harness cards-driver-go",
					"\tCARDS_REQUIRE=load,render sh test/go-tables/negative-control cards",
				}, "\n"),
			},
			ncFiles: map[string]string{
				"test/go-tables/negative-control": "#!/bin/sh\ncase \"$mode\" in\ncards)\n  overlay_one build/tables-generated-go/cards/render.go 's/a/b/'\n  (cd build/tables-generated-go/cards/cmd/cards-driver && go build .)\n  ;;\nesac\n",
			},
			wantSub: "cards-conformance-go-negative-control): negative-control accesses pre-existing `build/tables-generated-go/cards` without a non-`.PHONY` file/stamp Make prerequisite",
		},
		{
			name: "cold negative-control recipe cd into build/tables-generated-foo without prerequisite",
			makeFiles: map[string]string{
				"make/foo.mk": "tables-foo-negative-control:\n\tcd build/tables-generated-foo/unit && go test .\n",
			},
			wantSub: "tables-foo-negative-control): negative-control accesses pre-existing `build/tables-generated-foo/unit` without a non-`.PHONY` file/stamp Make prerequisite",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs, _ := checkMakefileAndScriptPortability(tc.makeFiles, tc.shFiles, tc.ncFiles)
			joined := strings.Join(errs, "\n")
			if !strings.Contains(joined, tc.wantSub) {
				t.Fatalf("planted case %q did not report %q; got:\n%s", tc.name, tc.wantSub, joined)
			}
		})
	}
}
