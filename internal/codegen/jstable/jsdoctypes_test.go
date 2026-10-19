package jstable

// TestJsEmitterWritesJsdocTypes holds the JavaScript emitters to the JSDoc
// contract the checkJs gate (`tsc --checkJs --strict`) reads: every generated
// function, method and constructor carries an `@param {T}` for each of its
// parameters, by name and in order; every function that returns a value
// carries an `@returns {T}`; every tuple return (the `[value, ok]` pairs) is
// typed by an `@typedef` of a tuple; no type is `any`, `*` or `Object`; and
// every <T>FixedDecode call passes exactly the arguments its declaration takes
// (the unused 4th `stage` argument is gone). It is pure Go over the emitted
// text of the corpus units: no node, no tsc, no time, no files written.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
)

// jsdocCorpus is the set of schema units the generated JavaScript corpus is
// built from (Makefile SCHEMAS, SCHEMAS128 and make/js.mk's
// build/tables-generated-js units, and the bench trees generated/bench/js and
// generated/bench/paired/js): a directory is one unit of every .schema file
// in it, a file is a unit by itself, and files joined by "+" are one unit.
var jsdocCorpus = []string{
	"examples", "examples128",
	"tables/examples", "tables/pointers", "tables/blobs", "tables/maps", "examples-wide",
	"tables/lists", "tables/block", "tables/blockhome", "tables/messages", "tables/stream",
	"tables/scalars", "tables/vocab", "tables/vocab9", "tables/backend",
	"test/tables/V1.schema", "test/tables/V2.schema", "test/tables/W1.schema", "test/tables/W2.schema",
	"test/tables/R1.schema", "test/tables/R2.schema", "test/tables/P1.schema", "test/tables/P3.schema",
	"test/tables/M1.schema", "test/tables/M2.schema", "test/tables/Scalars2.schema",
	"bench/corpus/Bench.schema", "bench/corpus/RealWorld.schema",
	"bench/corpus/Bench.schema+bench/corpus/FixedTable.schema",
}

// jsdocRepoRoot walks up from the package directory to the module root.
func jsdocRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			t.Fatal("go.mod not found above the package directory")
		}
		dir = up
	}
}

// jsdocCorpusModules generates every corpus unit with both JavaScript
// emitters (the packet modules of internal/codegen/js and the table modules
// of this package), keyed "<unit>/<file>".
func jsdocCorpusModules(t *testing.T) map[string]string {
	t.Helper()
	root := jsdocRepoRoot(t)
	out := map[string]string{}
	for _, entry := range jsdocCorpus {
		p := filepath.Join(root, entry)
		var paths []string
		if strings.HasSuffix(entry, ".schema") {
			for _, f := range strings.Split(entry, "+") {
				paths = append(paths, filepath.Join(root, f))
			}
		} else {
			matches, err := filepath.Glob(filepath.Join(p, "*.schema"))
			if err != nil {
				t.Fatal(err)
			}
			if len(matches) == 0 {
				t.Fatalf("corpus unit %s has no .schema files", entry)
			}
			sort.Strings(matches)
			paths = matches
		}
		u := loadUnit(t, paths...)
		packets, err := js.Generate(u)
		if err != nil {
			t.Fatalf("%s: js.Generate: %v", entry, err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: jstable.Generate: %v", entry, err)
		}
		for name, data := range packets {
			out[entry+"/"+name] = string(data)
		}
		for name, data := range tables {
			out[entry+"/"+name] = string(data)
		}
	}
	return out
}

func TestJsEmitterWritesJsdocTypes(t *testing.T) {
	t.Parallel()
	modules := jsdocCorpusModules(t)
	if len(modules) < 100 {
		t.Fatalf("the corpus generated only %d modules; the walk is broken", len(modules))
	}
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	var faults []string
	functions := 0
	for _, name := range names {
		fs, n := checkJsdoc(name, modules[name])
		faults = append(faults, fs...)
		functions += n
	}
	if functions < 1000 {
		t.Fatalf("only %d generated functions were found; the scanner is broken", functions)
	}
	if len(faults) > 0 {
		shown := faults
		if len(shown) > 40 {
			shown = shown[:40]
		}
		t.Fatalf("%d JSDoc faults over %d generated functions in %d modules; first %d:\n%s",
			len(faults), functions, len(modules), len(shown), strings.Join(shown, "\n"))
	}
	t.Logf("%d generated functions in %d modules carry their JSDoc types", functions, len(modules))
}

// TestJsEmitterWritesJsdocTypesReversedWitness plants the fault the check
// exists for: one `@param` line dropped from one generated function, in a
// copy of the emitted text. The check must go red naming that function.
func TestJsEmitterWritesJsdocTypesReversedWitness(t *testing.T) {
	t.Parallel()
	modules := jsdocCorpusModules(t)
	const unit = "examples/Types.js"
	src, ok := modules[unit]
	if !ok {
		t.Fatalf("%s was not generated", unit)
	}
	if fs, _ := checkJsdoc(unit, src); len(fs) != 0 {
		t.Fatalf("the unplanted module is not clean: %s", strings.Join(fs, "\n"))
	}
	lines := strings.Split(src, "\n")
	planted := -1
	for i := 0; i+1 < len(lines); i++ {
		if strings.Contains(lines[i], "@param") && strings.Contains(lines[i], " value") {
			// drop the @param of the first function that names `value`
			planted = i
			break
		}
	}
	if planted < 0 {
		t.Fatalf("%s has no `@param ... value` line to drop", unit)
	}
	fn := ""
	for j := planted + 1; j < len(lines); j++ {
		if m := jsFuncDecl.FindStringSubmatch(lines[j]); m != nil {
			fn = m[2]
			break
		}
	}
	if fn == "" {
		t.Fatal("no function follows the planted line")
	}
	plantedSrc := strings.Join(append(append([]string{}, lines[:planted]...), lines[planted+1:]...), "\n")
	fs, _ := checkJsdoc(unit, plantedSrc)
	named := false
	for _, f := range fs {
		if strings.Contains(f, " "+fn+":") && strings.Contains(f, "value") {
			named = true
		}
	}
	if !named {
		t.Fatalf("dropping the @param of %s's `value` stayed green or did not name %s: %v", fn, fn, fs)
	}
	// the other three checks, one plant each: a banned type, a dropped
	// @returns, and a suppression directive
	plants := []struct{ what, from, to, want string }{
		{"banned type", "@param {SerializeStream} stream", "@param {any} stream", "any, * or Object"},
		{"dropped @returns", " * @returns {boolean}\n", "", "no @returns type"},
		{"suppression", "@param {SerializeStream} stream", "@ts-ignore\n * @param {SerializeStream} stream", "suppresses the checker"},
	}
	for _, p := range plants {
		if !strings.Contains(src, p.from) {
			t.Fatalf("%s: %s holds no %q to plant on", p.what, unit, p.from)
		}
		fs, _ := checkJsdoc(unit, strings.Replace(src, p.from, p.to, 1))
		hit := false
		for _, f := range fs {
			if strings.Contains(f, p.want) {
				hit = true
			}
		}
		if !hit {
			t.Fatalf("planted %s stayed green: %v", p.what, fs)
		}
	}
}

var (
	// a function declaration, one per line as the emitters write them
	jsFuncDecl = regexp.MustCompile(`^\s*(?:export\s+)?(?:async\s+)?function\s*\*?\s*([*]?)([A-Za-z_$][\w$]*)\s*\(([^)]*)\)\s*\{`)
	// a class method or constructor inside a class body
	jsMethodDecl  = regexp.MustCompile(`^\s+(?:static\s+)?(?:get\s+|set\s+)?([A-Za-z_$][\w$]*)\s*\(([^)]*)\)\s*\{`)
	jsClassDecl   = regexp.MustCompile(`^\s*(?:export\s+)?class\s+([A-Za-z_$][\w$]*)`)
	jsParamTag    = regexp.MustCompile(`@param\s+\{`)
	jsTypedefTup  = regexp.MustCompile(`@typedef\s+\{\s*\[`)
	jsKeywords    = map[string]bool{"if": true, "for": true, "while": true, "switch": true, "catch": true, "function": true, "return": true, "with": true}
	jsBannedType  = regexp.MustCompile(`(^|[^\w$.])(any|Object|Function)([^\w$]|$)|\*`)
	jsTsDirective = regexp.MustCompile(`@ts-(ignore|nocheck|expect-error)`)
	jsFixedCall   = regexp.MustCompile(`([A-Za-z_$][\w$]*FixedDecode)\(`)
)

// jsdocBlock is the JSDoc comment that ends directly above line i (only `//`
// line comments and blank lines may sit between), or "" when none does.
func jsdocBlock(lines []string, i int) string {
	j := i - 1
	for j >= 0 {
		s := strings.TrimSpace(lines[j])
		if s == "" || strings.HasPrefix(s, "//") {
			j--
			continue
		}
		break
	}
	if j < 0 || !strings.HasSuffix(strings.TrimSpace(lines[j]), "*/") {
		return ""
	}
	end := j
	for j >= 0 && !strings.Contains(lines[j], "/**") {
		if strings.Contains(lines[j], "/*") && !strings.Contains(lines[j], "/**") {
			return ""
		}
		j--
	}
	if j < 0 {
		return ""
	}
	return strings.Join(lines[j:end+1], "\n")
}

// jsdocTags returns each tag's type text and the word after it: the
// balanced {...} after @param, @returns, @return, @type, @typedef, @property.
type jsdocTag struct{ tag, typ, name string }

func jsdocTags(block string) []jsdocTag {
	var out []jsdocTag
	for i := 0; i < len(block); i++ {
		if block[i] != '@' {
			continue
		}
		j := i + 1
		for j < len(block) && (block[j] >= 'a' && block[j] <= 'z') {
			j++
		}
		tag := block[i+1 : j]
		switch tag {
		case "param", "returns", "return", "type", "typedef", "property", "prop", "template", "this", "callback":
		default:
			continue
		}
		k := j
		for k < len(block) && (block[k] == ' ' || block[k] == '\t') {
			k++
		}
		typ := ""
		if k < len(block) && block[k] == '{' {
			depth := 0
			start := k
			for ; k < len(block); k++ {
				if block[k] == '{' {
					depth++
				} else if block[k] == '}' {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			if k >= len(block) {
				out = append(out, jsdocTag{tag: tag, typ: block[start:]})
				break
			}
			typ = block[start+1 : k]
			k++
		}
		for k < len(block) && (block[k] == ' ' || block[k] == '\t') {
			k++
		}
		n := k
		for n < len(block) && block[n] != ' ' && block[n] != '\n' && block[n] != '\t' {
			n++
		}
		out = append(out, jsdocTag{tag: tag, typ: typ, name: strings.Trim(block[k:n], "[]")})
		i = k - 1
	}
	return out
}

// jsParams splits a parameter list into names (defaults and rest dots dropped).
func jsParams(list string) []string {
	var out []string
	for _, p := range strings.Split(list, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if k := strings.Index(p, "="); k >= 0 {
			p = strings.TrimSpace(p[:k])
		}
		out = append(out, strings.TrimPrefix(p, "..."))
	}
	return out
}

// jsBody is the text of the block that opens on line i, from its first '{'
// to the matching '}', with nested function and arrow bodies removed so a
// `return` inside them is not counted for the outer function. Braces inside
// strings, regex literals and comments are rare in the generated code; string
// literals are skipped.
func jsBody(lines []string, i int) string {
	text := strings.Join(lines[i:], "\n")
	open := strings.Index(text, "{")
	if open < 0 {
		return ""
	}
	// the parameter list's own braces never appear (no destructuring), so the
	// first '{' after the ')' of the declaration opens the body
	if close := strings.Index(text, ")"); close >= 0 && close > open {
		open = strings.Index(text[close:], "{") + close
	}
	depth := 0
	var b strings.Builder
	skipDepth := -1
	for k := open; k < len(text); k++ {
		c := text[k]
		if c == '"' || c == '\'' || c == '`' {
			q := c
			for k++; k < len(text) && text[k] != q; k++ {
				if text[k] == '\\' {
					k++
				}
			}
			continue
		}
		if c == '/' && k+1 < len(text) && text[k+1] == '/' {
			for k < len(text) && text[k] != '\n' {
				k++
			}
			continue
		}
		if c == '{' {
			if skipDepth < 0 && depth > 0 && nestedFnOpens(text[:k]) {
				skipDepth = depth
			}
			depth++
		} else if c == '}' {
			depth--
			if skipDepth >= 0 && depth == skipDepth {
				skipDepth = -1
				continue
			}
			if depth == 0 {
				return b.String()
			}
		}
		if skipDepth < 0 {
			b.WriteByte(c)
		}
	}
	return b.String()
}

var nestedFnHead = regexp.MustCompile(`(=>\s*|\bfunction\s*[\w$]*\s*\([^()]*\)\s*)$`)

func nestedFnOpens(before string) bool {
	if len(before) > 200 {
		before = before[len(before)-200:]
	}
	return nestedFnHead.MatchString(before)
}

var jsReturnValue = regexp.MustCompile(`\breturn\s+[^;\s}]`)

// a tuple return: an array literal of two or more elements
var jsReturnTuple = regexp.MustCompile(`\breturn\s+\[[^\]\n]*,`)

// checkJsdoc returns the faults of one module and the number of functions,
// methods and constructors it holds. A fault names the module, the line and
// the function.
func checkJsdoc(module, src string) ([]string, int) {
	lines := strings.Split(src, "\n")
	var faults []string
	count := 0
	typedefs := map[string]string{}
	// the typedef names and whether each is a tuple
	for _, tg := range jsdocTags(src) {
		if tg.tag == "typedef" && tg.name != "" {
			typedefs[tg.name] = tg.typ
		}
	}
	// every type written anywhere in the module
	for _, tg := range jsdocTags(src) {
		if tg.typ != "" && jsBannedType.MatchString(tg.typ) {
			faults = append(faults, fmt.Sprintf("%s: @%s {%s} %s: the type uses any, * or Object", module, tg.tag, tg.typ, tg.name))
		}
	}
	if m := jsTsDirective.FindString(src); m != "" {
		faults = append(faults, fmt.Sprintf("%s: %s suppresses the checker", module, m))
	}
	// the fixed decode's arity, declaration against every call
	arity := map[string]int{}
	for _, l := range lines {
		if m := jsFuncDecl.FindStringSubmatch(l); m != nil && strings.HasSuffix(m[2], "FixedDecode") {
			arity[m[2]] = len(jsParams(m[3]))
		}
	}
	classDepth := -1
	depth := 0
	for i, l := range lines {
		trim := strings.TrimSpace(l)
		if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "*") || strings.HasPrefix(trim, "/*") {
			continue
		}
		for _, m := range jsFixedCall.FindAllStringSubmatchIndex(l, -1) {
			name := l[m[2]:m[3]]
			if strings.Contains(l[:m[0]], "function ") {
				continue
			}
			want, ok := arity[name]
			if !ok {
				continue
			}
			args := jsCallArgs(l[m[1]:])
			if args != want {
				faults = append(faults, fmt.Sprintf("%s:%d: %s is called with %d arguments, declared with %d", module, i+1, name, args, want))
			}
		}
		var fname, plist string
		isCtor := false
		if m := jsFuncDecl.FindStringSubmatch(l); m != nil {
			fname, plist = m[2], m[3]
		} else if classDepth >= 0 && depth == classDepth+1 {
			if m := jsMethodDecl.FindStringSubmatch(l); m != nil && !jsKeywords[m[1]] {
				fname, plist = m[1], m[2]
				isCtor = fname == "constructor"
			}
		}
		if jsClassDecl.MatchString(l) && classDepth < 0 {
			classDepth = depth
		}
		if fname != "" {
			count++
			params := jsParams(plist)
			block := jsdocBlock(lines, i)
			var got []string
			var returns *jsdocTag
			for _, tg := range jsdocTags(block) {
				tg := tg
				switch tg.tag {
				case "param":
					if tg.typ == "" {
						faults = append(faults, fmt.Sprintf("%s:%d: %s: @param %s has no type", module, i+1, fname, tg.name))
					}
					got = append(got, tg.name)
				case "returns", "return":
					returns = &tg
				}
			}
			if strings.Join(got, ",") != strings.Join(params, ",") {
				faults = append(faults, fmt.Sprintf("%s:%d: %s: @param names %v, the parameters are %v", module, i+1, fname, got, params))
			}
			if !isCtor {
				body := jsBody(lines, i)
				if jsReturnValue.MatchString(body) {
					if returns == nil || returns.typ == "" {
						faults = append(faults, fmt.Sprintf("%s:%d: %s: returns a value with no @returns type", module, i+1, fname))
					} else if jsReturnTuple.MatchString(body) && !tupleTyped(returns.typ, typedefs) {
						faults = append(faults, fmt.Sprintf("%s:%d: %s: returns a tuple; @returns {%s} names no tuple @typedef", module, i+1, fname, returns.typ))
					}
				}
			}
		}
		depth += strings.Count(stripJsStrings(l), "{") - strings.Count(stripJsStrings(l), "}")
		if classDepth >= 0 && depth <= classDepth {
			classDepth = -1
		}
	}
	return faults, count
}

// tupleTyped reports whether a @returns type names a tuple typedef: one
// alternative of the type must be a name whose @typedef is a tuple.
func tupleTyped(typ string, typedefs map[string]string) bool {
	for _, alt := range strings.Split(typ, "|") {
		alt = strings.TrimSpace(alt)
		if def, ok := typedefs[alt]; ok && jsTypedefTup.MatchString("@typedef {"+def+"}") {
			return true
		}
	}
	return false
}

// stripJsStrings blanks string literals so their braces are not counted.
func stripJsStrings(l string) string {
	var b strings.Builder
	for k := 0; k < len(l); k++ {
		c := l[k]
		if c == '"' || c == '\'' || c == '`' {
			q := c
			for k++; k < len(l) && l[k] != q; k++ {
				if l[k] == '\\' {
					k++
				}
			}
			continue
		}
		if c == '/' && k+1 < len(l) && l[k+1] == '/' {
			break
		}
		b.WriteByte(c)
	}
	return b.String()
}

// jsCallArgs counts the top-level arguments of a call whose '(' was just
// consumed; it reads to the matching ')'.
func jsCallArgs(rest string) int {
	depth, n, seen := 0, 0, false
	for _, c := range rest {
		switch c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				if seen {
					n++
				}
				return n
			}
			depth--
		case ',':
			if depth == 0 {
				n++
				seen = false
				continue
			}
		}
		if c != ' ' {
			seen = true
		}
	}
	return n
}
