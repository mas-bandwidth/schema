// Package cqualify gives every name a C unit declares at file scope its
// package, so two units' headers can share one translation unit.
//
// C has no namespace. The C emitters (internal/codegen/c and ctable) write
// a unit in its schema spellings — Vec3, write_vec3, MAX_OBJECTS,
// TableWriter — and this pass is the namespace: it collects every
// identifier the unit's own files DECLARE at file scope (types and tags,
// functions, file-scope objects, enumerators, macros) and respells each one
// with the package, uniformly across every file of the unit:
//
//	Vec3 (a type)             -> example_vec3_t
//	write_vec3 (a function)   -> example_write_vec3
//	MAX_OBJECTS (a macro)     -> EXAMPLE_MAX_OBJECTS
//	kTableAnnounce (a value) -> EXAMPLE_K_TABLE_ANNOUNCE
//	SCHEMA_TABLE_X / schema_x -> SCHEMA_EXAMPLE_TABLE_X / schema_example_x
//
// Comments, string literals and member names (anything after . or ->) are
// never touched, so reflection names and the text form keep the schema's
// spellings. Names that already carry the package (SCHEMA_<PKG>_…,
// schema_<pkg>_…), the shared runtime header's names (ccommon.RuntimeHeader,
// the same text in every unit) and the caller-overridable hooks
// (schema_assert and friends, a #define under its own #ifndef) are left as
// they are.
//
// A unit whose names would collide after qualification is refused by name:
// the pass never emits two declarations of one spelling.
package cqualify

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/ccommon"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Qualify returns files with every unit-declared file-scope name qualified
// by pkg. The shared runtime header passes through untouched.
func Qualify(files map[string][]byte, pkg string) (map[string][]byte, error) {
	rename, err := Names(files, pkg)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(files))
	for name, data := range files {
		if name == ccommon.RuntimeHeaderName || !isC(name) {
			out[name] = data
			continue
		}
		out[name] = []byte(Rewrite(string(data), rename))
	}
	return out, nil
}

func isC(name string) bool {
	return strings.HasSuffix(name, ".h") || strings.HasSuffix(name, ".c")
}

// Names is the unit's rename map: every file-scope name its files declare,
// to its qualified spelling.
func Names(files map[string][]byte, pkg string) (map[string]string, error) {
	shared := map[string]bool{}
	for n := range declared(string(ccommon.RuntimeHeader())).all() {
		shared[n] = true
	}
	names := make([]string, 0, len(files))
	for name := range files {
		if name != ccommon.RuntimeHeaderName && isC(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	decls := &decls{kind: map[string]kind{}, hooks: map[string]bool{}}
	for _, name := range names {
		decls.merge(declared(string(files[name])))
	}
	// SCHEMA_ and schema_ are the generator's own (SPEC §6.3), so a name
	// under them is the unit's even where the unit only TESTS it — an
	// #ifdef SCHEMA_TABLE_SEQUENCE_RUNTIME in a unit with no sequences must
	// ask about its own package's runtime, never another unit's
	for _, name := range names {
		for _, t := range tokenize(string(files[name])) {
			if t.kind != tIdent || !(strings.HasPrefix(t.text, "SCHEMA_") || strings.HasPrefix(t.text, "schema_")) {
				continue
			}
			if _, ok := decls.kind[t.text]; !ok {
				decls.add(t.text, kindValue)
			}
		}
	}
	rename := map[string]string{}
	from := map[string]string{}
	keys := make([]string, 0, len(decls.kind))
	for n := range decls.kind {
		keys = append(keys, n)
	}
	sort.Strings(keys)
	for _, n := range keys {
		if shared[n] || decls.hooks[n] || strings.HasPrefix(n, "__") {
			continue
		}
		q, ok := qualify(n, decls.kind[n], pkg)
		if !ok {
			continue
		}
		if prev, dup := from[q]; dup {
			return nil, fmt.Errorf("C backend: %s and %s both qualify to %s in package %s; rename one at the source", prev, n, q, pkg)
		}
		if _, clash := decls.kind[q]; clash && q != n {
			return nil, fmt.Errorf("C backend: %s qualifies to %s, which the unit already declares; rename one at the source", n, q)
		}
		from[q] = n
		rename[n] = q
	}
	return rename, nil
}

type kind int

const (
	kindValue kind = iota // a function, object, enumerator or macro
	kindType              // a typedef name or a struct/union/enum tag
)

// qualify spells n in package pkg.
func qualify(n string, k kind, pkg string) (string, bool) {
	lower := strings.ToLower(pkg)
	upper := strings.ToUpper(pkg)
	switch {
	case strings.HasPrefix(n, "SCHEMA_"+upper+"_"), strings.HasPrefix(n, "schema_"+lower+"_"):
		return n, false // already the package's
	case strings.HasPrefix(n, "SCHEMA_"):
		return "SCHEMA_" + upper + "_" + n[len("SCHEMA_"):], true
	case strings.HasPrefix(n, "schema_"):
		return "schema_" + lower + "_" + n[len("schema_"):], true
	case k == kindType && !screaming(n):
		s := ir.RustSnake(n)
		if !strings.HasSuffix(s, "_t") {
			s += "_t"
		}
		return lower + "_" + s, true
	case screaming(n):
		return upper + "_" + n, true
	case strings.ToLower(n) != n:
		// a mixed-case value (kTableAnnounce, TableByteOrder_Big) is a
		// constant: it takes the SCREAMING form, the Rust constant spelling
		// the checker's name registry already claims
		return upper + "_" + ir.RustConstName(n), true
	default:
		return lower + "_" + n, true
	}
}

func screaming(n string) bool {
	return strings.ToUpper(n) == n && strings.ContainsAny(n, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
}

// ---- rewriting ----

// Rewrite respells every identifier token of src found in rename, skipping
// comments, literals, member names and a function-like macro's own
// parameters. Exported for the harness migration and the tests.
func Rewrite(src string, rename map[string]string) string {
	toks := tokenize(src)
	var b strings.Builder
	b.Grow(len(src) + len(src)/8)
	var params map[string]bool
	directive := -1
	var braces []bool // per open brace: true for a struct or union body
	for i, t := range toks {
		if t.directive != directive {
			directive = t.directive
			params = nil
			if directive >= 0 {
				params = macroParams(toks, i)
			}
		}
		if t.directive < 0 && t.kind == tPunct {
			switch t.text {
			case "{":
				braces = append(braces, opensRecord(toks, i))
			case "}":
				if len(braces) > 0 {
					braces = braces[:len(braces)-1]
				}
			}
		}
		if t.kind == tIdent && !params[t.text] {
			inRecord := t.directive < 0 && len(braces) > 0 && braces[len(braces)-1]
			if q, ok := rename[t.text]; ok && !afterMember(toks, i) && !(inRecord && declaresMember(toks, i)) {
				b.WriteString(q)
				continue
			}
		}
		b.WriteString(t.text)
	}
	return b.String()
}

// opensRecord reports whether the { at i opens a struct or union body: the
// keyword stands right before it, or before the tag right before it.
func opensRecord(toks []token, i int) bool {
	j := prevSignificant(toks, i)
	if j >= 0 && toks[j].kind == tIdent && toks[j].text != "struct" && toks[j].text != "union" {
		j = prevSignificant(toks, j)
	}
	return j >= 0 && (toks[j].text == "struct" || toks[j].text == "union")
}

// declaresMember reports whether the identifier at i, inside a record body,
// is a member's own name: followed by ; , [ or : (a bit-field), or the NAME
// of a ( * NAME ) function-pointer member. A type name is followed by
// another identifier or a *, so it never matches.
func declaresMember(toks []token, i int) bool {
	n := nextSignificant(toks, i)
	if n < 0 {
		return false
	}
	switch toks[n].text {
	case ";", ",", "[", ":":
		return true
	case ")":
		p := prevSignificant(toks, i)
		return p >= 0 && toks[p].text == "*" && prevSignificant(toks, p) >= 0 && toks[prevSignificant(toks, p)].text == "("
	}
	return false
}

func prevSignificant(toks []token, i int) int {
	for j := i - 1; j >= 0; j-- {
		if toks[j].kind != tSpace && toks[j].kind != tComment && toks[j].directive < 0 {
			return j
		}
	}
	return -1
}

func nextSignificant(toks []token, i int) int {
	for j := i + 1; j < len(toks); j++ {
		if toks[j].kind != tSpace && toks[j].kind != tComment && toks[j].directive < 0 {
			return j
		}
	}
	return -1
}

// afterMember reports whether the identifier at i names a member: the
// nearest significant token before it is . or ->.
func afterMember(toks []token, i int) bool {
	for j := i - 1; j >= 0; j-- {
		switch toks[j].kind {
		case tSpace, tComment:
			continue
		case tPunct:
			return toks[j].text == "." || toks[j].text == "->"
		default:
			return false
		}
	}
	return false
}

// macroParams returns the parameters of a function-like #define starting at
// token i (the directive's first token), or nil.
func macroParams(toks []token, i int) map[string]bool {
	sig := significant(toks, i, toks[i].directive)
	// # define NAME ( params )
	if len(sig) < 4 || sig[0].text != "#" || sig[1].text != "define" || sig[2].kind != tIdent {
		return nil
	}
	nameEnd := sig[2].end
	open := sig[3]
	if open.text != "(" || open.start != nameEnd {
		return nil // object-like: the ( is not adjacent to the name
	}
	params := map[string]bool{}
	for _, t := range sig[4:] {
		if t.text == ")" {
			break
		}
		if t.kind == tIdent {
			params[t.text] = true
		}
	}
	return params
}

// significant returns the non-space, non-comment tokens of directive d from i.
func significant(toks []token, i, d int) []token {
	var out []token
	for j := i; j < len(toks) && toks[j].directive == d; j++ {
		if toks[j].kind != tSpace && toks[j].kind != tComment {
			out = append(out, toks[j])
		}
	}
	return out
}

// ---- declaration scan ----

type decls struct {
	kind  map[string]kind
	hooks map[string]bool
}

func (d *decls) add(n string, k kind) {
	if n == "" {
		return
	}
	if prev, ok := d.kind[n]; ok && prev == kindType {
		return
	}
	d.kind[n] = k
}

func (d *decls) merge(o *decls) {
	for n, k := range o.kind {
		d.add(n, k)
	}
	for n := range o.hooks {
		d.hooks[n] = true
	}
}

func (d *decls) all() map[string]kind { return d.kind }

var keywords = map[string]bool{
	"auto": true, "break": true, "case": true, "char": true, "const": true, "continue": true,
	"default": true, "do": true, "double": true, "else": true, "enum": true, "extern": true,
	"float": true, "for": true, "goto": true, "if": true, "inline": true, "int": true, "long": true,
	"register": true, "restrict": true, "return": true, "short": true, "signed": true, "sizeof": true,
	"static": true, "struct": true, "switch": true, "typedef": true, "union": true, "unsigned": true,
	"void": true, "volatile": true, "while": true, "_Bool": true, "_Static_assert": true,
	"_Alignas": true, "_Alignof": true, "bool": true, "true": true, "false": true,
}

// declared scans one file's file-scope declarations.
func declared(src string) *decls {
	d := &decls{kind: map[string]kind{}, hooks: map[string]bool{}}
	toks := tokenize(src)

	// preprocessor: #define NAME, and the hook shape — a lowercase NAME
	// #defined directly under its own #ifndef, which a caller overrides
	lastIfndef := ""
	for i := 0; i < len(toks); i++ {
		if toks[i].directive < 0 || (i > 0 && toks[i-1].directive == toks[i].directive) {
			continue
		}
		sig := significant(toks, i, toks[i].directive)
		if len(sig) >= 3 && sig[0].text == "#" && sig[2].kind == tIdent {
			switch sig[1].text {
			case "ifndef":
				lastIfndef = sig[2].text
				continue
			case "define":
				n := sig[2].text
				d.add(n, kindValue)
				if n == lastIfndef && strings.ToLower(n) == n {
					d.hooks[n] = true
				}
			}
		}
		lastIfndef = ""
	}

	// code: file-scope statements
	var code []token
	for _, t := range toks {
		if t.directive < 0 && t.kind != tSpace && t.kind != tComment {
			code = append(code, t)
		}
	}
	macros := map[string]bool{}
	for n := range d.kind {
		macros[n] = true
	}
	var stmt []token
	for i := 0; i < len(code); i++ {
		t := code[i]
		switch {
		case t.text == "{" && isExternC(stmt):
			stmt = nil // extern "C" { … } is transparent
		case t.text == "}" && len(stmt) == 0:
			// the extern "C" block's close
		case t.text == "{" && isFunctionHead(stmt):
			d.add(functionName(stmt, macros), kindValue)
			i = skipBlock(code, i)
			stmt = nil
		case t.text == "{":
			end := skipBlock(code, i)
			stmt = append(stmt, code[i:end+1]...)
			i = end
		case t.text == ";":
			statement(d, stmt, macros)
			stmt = nil
		default:
			stmt = append(stmt, t)
		}
	}
	return d
}

func isExternC(stmt []token) bool {
	return len(stmt) == 2 && stmt[0].text == "extern" && stmt[1].kind == tString
}

// isFunctionHead: the statement so far ends in a parameter list's ) (or an
// attribute after it), and it has no = at the top level.
func isFunctionHead(stmt []token) bool {
	if len(stmt) == 0 {
		return false
	}
	depth := 0
	sawParen := false
	for _, t := range stmt {
		switch t.text {
		case "(":
			depth++
			sawParen = true
		case ")":
			depth--
		case "=":
			if depth == 0 {
				return false
			}
		}
	}
	return sawParen && stmt[len(stmt)-1].text == ")"
}

// functionName: the identifier before the first top-level ( that is not an
// attribute spelling or a macro.
func functionName(stmt []token, macros map[string]bool) string {
	for i := 1; i < len(stmt); i++ {
		if stmt[i].text != "(" || stmt[i-1].kind != tIdent {
			continue
		}
		n := stmt[i-1].text
		if strings.HasPrefix(n, "__") || keywords[n] || macros[n] || screaming(n) {
			// skip the attribute's own parenthesized argument
			i = skipParens(stmt, i)
			continue
		}
		return n
	}
	return ""
}

func skipParens(toks []token, i int) int {
	depth := 0
	for j := i; j < len(toks); j++ {
		switch toks[j].text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return len(toks) - 1
}

func skipBlock(toks []token, i int) int {
	depth := 0
	for j := i; j < len(toks); j++ {
		switch toks[j].text {
		case "{":
			depth++
		case "}":
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return len(toks) - 1
}

// statement records the names one file-scope statement (without its ;)
// declares.
func statement(d *decls, stmt []token, macros map[string]bool) {
	if len(stmt) == 0 {
		return
	}
	// a file-scope macro invocation (SCHEMA_TABLE_STATIC_ASSERT( … )) declares
	// nothing this pass owns
	if stmt[0].kind == tIdent && (macros[stmt[0].text] || screaming(stmt[0].text)) && len(stmt) > 1 && stmt[1].text == "(" {
		return
	}
	// tags and enumerators, wherever they stand
	for i := 0; i+1 < len(stmt); i++ {
		t := stmt[i]
		if t.text != "struct" && t.text != "union" && t.text != "enum" {
			continue
		}
		j := i + 1
		if stmt[j].kind == tIdent {
			if j+1 < len(stmt) && stmt[j+1].text == "{" || len(stmt) == 2 {
				d.add(stmt[j].text, kindType)
			}
			j++
		}
		if t.text == "enum" && j < len(stmt) && stmt[j].text == "{" {
			enumerators(d, stmt, j)
		}
	}
	top := topLevel(stmt)
	if len(top) == 0 {
		return
	}
	if top[0].text == "typedef" {
		braces := 0
		for i := 0; i+3 < len(stmt); i++ {
			switch stmt[i].text {
			case "{":
				braces++
			case "}":
				braces--
			}
			// typedef R (*NAME)(…), outside any struct body it defines
			if braces == 0 && stmt[i].text == "(" && stmt[i+1].text == "*" && stmt[i+2].kind == tIdent && stmt[i+3].text == ")" {
				d.add(stmt[i+2].text, kindType)
				return
			}
		}
		for i, t := range top {
			if t.kind == tIdent && !keywords[t.text] && (i+1 == len(top) || top[i+1].text == "," || top[i+1].text == "[") {
				d.add(t.text, kindType)
			}
		}
		return
	}
	// a prototype: the name before the first top-level (
	for i := 1; i < len(top); i++ {
		if top[i].text == "(" && top[i-1].kind == tIdent {
			n := top[i-1].text
			if strings.HasPrefix(n, "__") || keywords[n] || macros[n] || screaming(n) {
				continue
			}
			beforeEq := true
			for _, u := range top[:i] {
				if u.text == "=" {
					beforeEq = false
				}
			}
			if beforeEq {
				d.add(n, kindValue)
				return
			}
		}
	}
	// objects: declarators before = [ , or the end
	inInit := false
	for i, t := range top {
		if t.text == "=" {
			inInit = true
			continue
		}
		if t.text == "," {
			inInit = false
			continue
		}
		if inInit || t.kind != tIdent || keywords[t.text] {
			continue
		}
		if i+1 == len(top) || top[i+1].text == "=" || top[i+1].text == "[" || top[i+1].text == "," {
			d.add(t.text, kindValue)
		}
	}
}

// topLevel is stmt without anything inside braces, brackets or parentheses
// (the opening token of each group is kept, so a declarator's [ still shows).
func topLevel(stmt []token) []token {
	var out []token
	depth := 0
	for _, t := range stmt {
		switch t.text {
		case "{", "[", "(":
			if depth == 0 {
				out = append(out, t)
			}
			depth++
			continue
		case "}", "]", ")":
			depth--
			continue
		}
		if depth == 0 {
			out = append(out, t)
		}
	}
	return out
}

// enumerators records the constants of the enum body opening at stmt[open].
func enumerators(d *decls, stmt []token, open int) {
	depth := 0
	expect := true
	for i := open; i < len(stmt); i++ {
		switch stmt[i].text {
		case "{", "(", "[":
			depth++
			continue
		case "}", ")", "]":
			depth--
			if depth == 0 {
				return
			}
			continue
		case ",":
			if depth == 1 {
				expect = true
			}
			continue
		}
		if depth == 1 && expect && stmt[i].kind == tIdent {
			d.add(stmt[i].text, kindValue)
			expect = false
		}
	}
}

// ---- tokens ----

type tokKind int

const (
	tSpace tokKind = iota
	tComment
	tString
	tNumber
	tIdent
	tPunct
)

type token struct {
	kind       tokKind
	text       string
	start, end int
	directive  int // index of the preprocessor directive it belongs to, or -1
}

func identStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func identPart(c byte) bool { return identStart(c) || c >= '0' && c <= '9' }

// tokenize splits C source into tokens whose texts concatenate back to src.
func tokenize(src string) []token {
	var toks []token
	directive := -1
	directives := 0
	lineStart := true
	i := 0
	emit := func(k tokKind, j int) {
		toks = append(toks, token{kind: k, text: src[i:j], start: i, end: j, directive: directive})
		i = j
	}
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\n':
			// a directive ends at a newline not escaped by a backslash
			if directive >= 0 && !(i > 0 && src[i-1] == '\\') {
				directive = -1
			}
			lineStart = true
			emit(tSpace, i+1)
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v' || c == '\\':
			emit(tSpace, i+1)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				j = len(src) - i
			}
			emit(tComment, i+j)
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				emit(tComment, len(src))
			} else {
				emit(tComment, i+2+j+2)
			}
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(src) && src[j] != c && src[j] != '\n' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j < len(src) && src[j] == c {
				j++
			}
			lineStart = false
			emit(tString, j)
		case c == '#' && lineStart:
			directive = directives
			directives++
			lineStart = false
			emit(tPunct, i+1)
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			j := i + 1
			for j < len(src) && (identPart(src[j]) || src[j] == '.' ||
				(src[j] == '+' || src[j] == '-') && (src[j-1] == 'e' || src[j-1] == 'E' || src[j-1] == 'p' || src[j-1] == 'P')) {
				j++
			}
			lineStart = false
			emit(tNumber, j)
		case identStart(c):
			j := i + 1
			for j < len(src) && identPart(src[j]) {
				j++
			}
			lineStart = false
			emit(tIdent, j)
		case c == '-' && i+1 < len(src) && src[i+1] == '>':
			lineStart = false
			emit(tPunct, i+2)
		default:
			lineStart = false
			emit(tPunct, i+1)
		}
	}
	return toks
}
