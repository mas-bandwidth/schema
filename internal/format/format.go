// Package format is schemafmt (SPEC §7.4): gofmt's philosophy — one style,
// no options — run by `schema fmt`, the one command that writes a schema
// file.
//
// Safety is built in, not promised: Format re-parses its own output and
// structurally compares it against the input's AST, and verifies its own
// idempotence — on any mismatch it returns an error and no output. A
// formatter that could change meaning, or that never settles, refuses
// instead.
package format

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/scanner"
)

// Format canonicalizes one schema file. The returned bytes may equal the
// input (already canonical).
func Format(path string, src []byte) ([]byte, error) {
	before, errs := parser.Parse(path, src)
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s does not parse; not formatting it: %w", path, errs[0])
	}

	out, err := render(path, src)
	if err != nil {
		return nil, err
	}

	after, errs := parser.Parse(path, out)
	if len(errs) > 0 {
		return nil, fmt.Errorf("formatter defect (output does not parse — refusing): %w", errs[0])
	}
	if fingerprint(before) != fingerprint(after) {
		return nil, fmt.Errorf("formatter defect (output parses differently from input — refusing to change meaning) in %s", path)
	}

	again, err := render(path, out)
	if err != nil {
		return nil, fmt.Errorf("formatter defect (second pass failed): %w", err)
	}
	if string(again) != string(out) {
		return nil, fmt.Errorf("formatter defect (not idempotent — refusing) in %s", path)
	}
	return out, nil
}

// ---- rendering ----

type line struct {
	blank   bool
	tokens  []scanner.Token // code tokens (comments excluded)
	comment string          // trailing or standalone comment text
}

func render(path string, src []byte) ([]byte, error) {
	raw, errs := scanner.ScanRaw(path, src)
	if len(errs) > 0 {
		return nil, errs[0]
	}

	// assemble physical lines
	var lines []line
	cur := line{}
	flush := func() {
		if len(cur.tokens) == 0 && cur.comment == "" {
			cur.blank = true
		}
		lines = append(lines, cur)
		cur = line{}
	}
	for _, t := range raw {
		switch t.Kind {
		case scanner.Newline:
			flush()
		case scanner.EOF:
			if len(cur.tokens) > 0 || cur.comment != "" {
				flush()
			}
		case scanner.Comment:
			cur.comment = strings.TrimRight(t.Text, " \t")
		default:
			cur.tokens = append(cur.tokens, t)
		}
	}

	// Allman normalization (SPEC §4.1, §7.4 rule 1): a MULTI-LINE block's
	// opening brace moves to its own line, and `else` stands alone between
	// its braces. One-line { ... } groups — an inline enum list, a one-line
	// type body — close on their own line and stay whole. Both placements
	// parse; this is the one canonical output.
	var normalized []line
	for _, l := range lines {
		work := l
		// a closing } followed by more tokens (`} else {`) splits after the }
		for len(work.tokens) > 1 && work.tokens[0].Kind == scanner.RBrace {
			normalized = append(normalized, line{tokens: work.tokens[:1]})
			work = line{tokens: work.tokens[1:], comment: work.comment}
		}
		// a line ENDING in { opens a multi-line block: the brace stands alone
		// (a group that closes on the same line ends in }, not {, and passes
		// through untouched)
		if n := len(work.tokens); n > 1 && work.tokens[n-1].Kind == scanner.LBrace {
			normalized = append(normalized, line{tokens: work.tokens[:n-1], comment: work.comment})
			work = line{tokens: work.tokens[n-1:]}
		}
		normalized = append(normalized, work)
	}
	lines = normalized

	// render each line at its depth, collapsing blank runs and dropping
	// blanks at the file start, after an opener and before a closer
	var rendered []string
	depth := 0
	pendingBlank := false
	for _, l := range lines {
		if l.blank {
			if len(rendered) > 0 {
				pendingBlank = true
			}
			continue
		}
		opens, closes := braceDelta(l.tokens)
		lineDepth := depth
		if closes > 0 && len(l.tokens) > 0 && l.tokens[0].Kind == scanner.RBrace {
			lineDepth-- // a closing } dedents its own line
			pendingBlank = false
		}
		if pendingBlank {
			rendered = append(rendered, "")
			pendingBlank = false
		}
		text := renderLine(l)
		rendered = append(rendered, indent(lineDepth)+text)
		depth += opens - closes
		if depth < 0 {
			depth = 0
		}
		if len(l.tokens) > 0 && l.tokens[len(l.tokens)-1].Kind == scanner.LBrace {
			pendingBlank = false
		}
	}

	aligned := alignColumns(rendered)
	return []byte(strings.Join(aligned, "\n") + "\n"), nil
}

func indent(depth int) string {
	if depth <= 0 {
		return ""
	}
	return strings.Repeat("    ", depth)
}

func braceDelta(tokens []scanner.Token) (opens, closes int) {
	for _, t := range tokens {
		switch t.Kind {
		case scanner.LBrace:
			opens++
		case scanner.RBrace:
			closes++
		}
	}
	return
}

// renderLine renders one line's tokens with canonical spacing, the attribute
// reorder (valueless markers first — SPEC §7.4 rule 3), and the trailing
// comment.
func renderLine(l line) string {
	tokens := reorderAttrs(dropTrailingCommaOneLine(l.tokens))
	var b strings.Builder
	for i, t := range tokens {
		if i > 0 && needSpace(tokens[i-1], t, tokens, i) {
			b.WriteByte(' ')
		}
		b.WriteString(t.Text)
	}
	code := b.String()
	if l.comment != "" {
		if code == "" {
			return l.comment
		}
		return code + " " + l.comment
	}
	return code
}

// dropTrailingCommaOneLine removes a trailing comma inside a one-line
// variant list: `{ Red, Blue, }` -> `{ Red, Blue }`.
func dropTrailingCommaOneLine(tokens []scanner.Token) []scanner.Token {
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].Kind == scanner.Comma && tokens[i+1].Kind == scanner.RBrace {
			return append(append([]scanner.Token{}, tokens[:i]...), tokens[i+1:]...)
		}
	}
	return tokens
}

// reorderAttrs rewrites a line's | qualification section with valueless
// markers first, then valued keys, both in stable order (SPEC §7.4 rule 3).
// The section runs from the first top-level | to the end of the line, so
// there is at most one and nothing can follow it.
func reorderAttrs(tokens []scanner.Token) []scanner.Token {
	for i, t := range tokens {
		if t.Kind != scanner.Pipe {
			continue
		}
		entries := splitTop(tokens[i+1:], scanner.Comma)
		var valueless, valued [][]scanner.Token
		for _, e := range entries {
			if containsKind(e, scanner.Assign) {
				valued = append(valued, e)
			} else {
				valueless = append(valueless, e)
			}
		}
		out := append([]scanner.Token{}, tokens[:i+1]...)
		first := true
		for _, e := range append(valueless, valued...) {
			if !first {
				out = append(out, scanner.Token{Kind: scanner.Comma, Text: ","})
			}
			first = false
			out = append(out, e...)
		}
		return out
	}
	return tokens
}

// isAttrOpen reports whether the [ at index i opens an ATTRIBUTE list rather
// than an array bound. An array bound is a PREFIX of a type: its ] is
// immediately followed by a scalar (ident or type keyword). An attribute
// list's ] is followed by nothing, `=`, `{`, or a comment.
func isAttrOpen(tokens []scanner.Token, i int) bool {
	j := i + 1
	depth := 1
	for j < len(tokens) && depth > 0 {
		switch tokens[j].Kind {
		case scanner.LBrack:
			depth++
		case scanner.RBrack:
			depth--
		}
		j++
	}
	if j >= len(tokens) {
		return true // ] ends the line -> attributes
	}
	next := tokens[j]
	if next.Kind == scanner.Ident || next.Kind.IsKeyword() {
		return false // ]Type -> an array bound
	}
	return true
}

func splitTop(tokens []scanner.Token, sep scanner.Kind) [][]scanner.Token {
	var out [][]scanner.Token
	var cur []scanner.Token
	depth := 0
	for _, t := range tokens {
		switch t.Kind {
		case scanner.LParen, scanner.LBrack, scanner.LBrace:
			depth++
		case scanner.RParen, scanner.RBrack, scanner.RBrace:
			depth--
		}
		if t.Kind == sep && depth == 0 {
			out = append(out, cur)
			cur = nil
			continue
		}
		cur = append(cur, t)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

func containsKind(tokens []scanner.Token, k scanner.Kind) bool {
	for _, t := range tokens {
		if t.Kind == k {
			return true
		}
	}
	return false
}

// needSpace decides the separator between adjacent tokens (SPEC §7.4 rules
// 3 and 4, codified from the corpus's own formatting).
func needSpace(prev, cur scanner.Token, tokens []scanner.Token, i int) bool {
	switch cur.Kind {
	case scanner.RParen, scanner.RBrack, scanner.Comma, scanner.Dot, scanner.DotDot:
		return false
	case scanner.LBrack:
		// THE MAP's bracket binds to its keyword — `map[K]V`, never
		// `map [K]V` (docs/SPEC-TABLES.md §2.8). Every other `[` opens an
		// array bound at the head of a type and takes the ordinary answer
		// below.
		if prev.Kind == scanner.KwMap {
			return false
		}
	case scanner.LParen:
		// tight after a callee (bits(, string(, const(, reserved() and after
		// unary minus; spaced after operators and separators
		switch prev.Kind {
		case scanner.Ident, scanner.KwBits, scanner.KwString, scanner.KwBytes,
			scanner.KwFixed, scanner.KwUfixed, scanner.KwConst, scanner.KwReserved:
			return false
		case scanner.Minus:
			return isUnary(tokens, i-1)
		}
		return true
	case scanner.RBrace:
		return true // one-line lists close with ` }`
	}
	switch prev.Kind {
	case scanner.LParen, scanner.Dot, scanner.DotDot, scanner.Not:
		return false
	case scanner.Question:
		// the OPTIONAL prefix binds to the type it qualifies — `settings
		// ?GunnerSettings`, never `settings ? GunnerSettings`
		// (docs/SPEC-TABLES.md §2.3)
		return false
	case scanner.Star:
		// the POINTER star binds to the type it targets — `next *Node`, never
		// `next * Node` (docs/SPEC-TABLES.md). A star in TYPE POSITION is the
		// pointer spelling; every other star is multiplication and keeps its
		// spaces.
		return !isPointerStar(tokens, i-1)
	case scanner.LBrack:
		return false
	case scanner.Minus:
		if isUnary(tokens, i-1) {
			return false
		}
		return true
	case scanner.RBrack:
		// `]Type` is the junction that closes an array bound or a MAP KEY,
		// and what follows it is a TYPE, tight against the bracket: an ident,
		// a type keyword (`map` included), another bound, the optional `?` or
		// the pointer `*` (docs/SPEC-TABLES.md §2.1, §2.3, §2.8). No
		// expression in this grammar ends in `]`, so anything else is a line
		// that has left the type and gets a space.
		switch cur.Kind {
		case scanner.Ident, scanner.Question, scanner.Star, scanner.LBrack:
			return false
		}
		return !cur.Kind.IsKeyword()
	case scanner.LBrace:
		return true // one-line lists open with `{ `
	}
	return true
}

// isPointerStar reports whether the star at index i is the POINTER spelling
// rather than multiplication. TYPE POSITION is three places and no others: a
// field line opens with its name, so index 1 is one; the `]` that closes an
// array bound or a map key is another (`[..4]*Node`,
// `map[uint32]*ShipConfig`); and the optional `?` is the third. Multiplication
// never appears in any of them, because no expression in this grammar ends in
// `]` or `?`.
func isPointerStar(tokens []scanner.Token, i int) bool {
	if i <= 0 || tokens[i].Kind != scanner.Star {
		return false
	}
	if i == 1 {
		return tokens[0].Kind == scanner.Ident
	}
	switch tokens[i-1].Kind {
	case scanner.RBrack, scanner.Question:
		return true
	}
	return false
}

// isUnary reports whether the minus at index i is unary.
func isUnary(tokens []scanner.Token, i int) bool {
	if tokens[i].Kind != scanner.Minus {
		return false
	}
	if i == 0 {
		return true
	}
	switch tokens[i-1].Kind {
	case scanner.Ident, scanner.Int, scanner.Float, scanner.RParen, scanner.RBrack:
		return false
	}
	return true
}

// ---- column alignment (SPEC §7.4 rule 2) ----

// alignColumns pads names and types within contiguous runs of field lines
// (broken by blank lines and comment lines), and aligns `=` within const
// runs. Operates on the rendered lines; alignment is the only thing that
// distinguishes these lines from their minimal one-space form.
func alignColumns(lines []string) []string {
	out := make([]string, len(lines))
	copy(out, lines)

	type run struct{ start, end int }
	flushField := func(r run) {
		if r.end-r.start < 2 {
			return
		}
		maxName, maxType := 0, 0
		anyTail := false
		for i := r.start; i < r.end; i++ {
			name, typ, tail, ok := splitField(out[i])
			if !ok {
				continue
			}
			if len(name) > maxName {
				maxName = len(name)
			}
			if len(typ) > maxType {
				maxType = len(typ)
			}
			if tail != "" {
				anyTail = true
			}
		}
		for i := r.start; i < r.end; i++ {
			name, typ, tail, ok := splitField(out[i])
			if !ok {
				continue
			}
			ind := out[i][:len(out[i])-len(strings.TrimLeft(out[i], " "))]
			s := ind + pad(name, maxName) + " " + typ
			if tail != "" {
				s = ind + pad(name, maxName) + " " + pad(typ, maxType) + " " + tail
			} else if anyTail {
				s = ind + pad(name, maxName) + " " + typ
			}
			out[i] = s
		}
	}
	flushConst := func(r run) {
		if r.end-r.start < 2 {
			return
		}
		maxHead := 0
		for i := r.start; i < r.end; i++ {
			head, _, ok := splitConst(out[i])
			if ok && len(head) > maxHead {
				maxHead = len(head)
			}
		}
		for i := r.start; i < r.end; i++ {
			head, rest, ok := splitConst(out[i])
			if !ok {
				continue
			}
			out[i] = pad(head, maxHead) + " = " + rest
		}
	}

	inField, inConst := -1, -1
	for i := 0; i <= len(out); i++ {
		isF, isC := false, false
		if i < len(out) {
			_, _, _, isF = splitField(out[i])
			_, _, isC = splitConst(out[i])
		}
		if !isF && inField >= 0 {
			flushField(run{inField, i})
			inField = -1
		}
		if !isC && inConst >= 0 {
			flushConst(run{inConst, i})
			inConst = -1
		}
		if isF && inField < 0 {
			inField = i
		}
		if isC && inConst < 0 {
			inConst = i
		}
	}
	return out
}

func pad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

// splitField recognizes an indented `name type...` field line and splits it
// into name, type and the attribute/default tail. Lines that are not plain
// fields (declarations, braces, comments) return ok = false.
func splitField(s string) (name, typ, tail string, ok bool) {
	ind := len(s) - len(strings.TrimLeft(s, " "))
	if ind == 0 {
		return "", "", "", false // fields are always indented
	}
	body := s[ind:]
	if strings.HasPrefix(body, "//") || strings.HasPrefix(body, "}") {
		return "", "", "", false
	}
	fields := strings.SplitN(body, " ", 2)
	if len(fields) != 2 {
		return "", "", "", false
	}
	name = fields[0]
	switch name {
	// non-field items that can open an indented line; `flags` and `contexts`
	// are contextual at FILE scope only — indented, they are ordinary field
	// names (the corpus's Ship.flags) and align with their run
	case "const", "reserved", "align", "if", "else":
		return "", "", "", false
	}
	rest := strings.TrimLeft(fields[1], " ")
	if rest == "" || strings.HasPrefix(rest, "//") {
		return "", "", "", false
	}
	// the DEFINITION (type plus any `= default`) runs to the first ` |` —
	// the qualification column is padded past the longest definition in the
	// run (SPEC §7.4 rule 2)
	typ = rest
	tail = ""
	if k := strings.Index(rest, " |"); k >= 0 {
		typ, tail = rest[:k], strings.TrimLeft(rest[k:], " ")
	}
	if strings.HasSuffix(typ, "{") {
		return "", "", "", false // a declaration opener, not a field
	}
	return name, typ, tail, true
}

// splitConst recognizes a top-level `const Name [type] = expr` line.
func splitConst(s string) (head, rest string, ok bool) {
	if !strings.HasPrefix(s, "const ") {
		return "", "", false
	}
	before, after, ok := strings.Cut(s, " = ")
	if !ok {
		return "", "", false
	}
	return strings.TrimRight(before, " "), after, true
}

// ---- the structural fingerprint ----

// fingerprint renders an AST to a canonical string with positions stripped
// and attribute lists in the formatter's own canonical order, so a
// meaning-preserving reformat fingerprints identically and anything else
// does not.
func fingerprint(f *ast.File) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n", f.Package)
	for _, d := range f.Decls {
		fpDecl(&b, d)
	}
	return b.String()
}

func fpDecl(b *strings.Builder, d ast.Decl) {
	switch d := d.(type) {
	case *ast.ConstDecl:
		fmt.Fprintf(b, "const %s %s = %s\n", d.Name, d.Type, fpExpr(d.Expr))
	case *ast.EnumDecl:
		fmt.Fprintf(b, "enum %s %s {%s}\n", d.Name, fpAttrs(d.Attrs), fpNames(d.Variants))
	case *ast.FlagsDecl:
		fmt.Fprintf(b, "flags %s %s {%s}\n", d.Name, fpAttrs(d.Attrs), fpNames(d.Variants))
	case *ast.TypeDecl:
		fmt.Fprintf(b, "type %s %s\n", d.Name, fpAttrs(d.Attrs))
		fpBlock(b, d.Body)
	case *ast.TableDecl:
		fmt.Fprintf(b, "table %s\n", d.Name)
		fpBlock(b, d.Body)
	case *ast.UnionDecl:
		// AN ARM IS A FIELD LINE (docs/SPEC-TABLES.md §2.6), so an arm's
		// fingerprint is a FIELD's: the type, the bound and the attributes,
		// or the formatter could move one and the safety net would not see it
		fmt.Fprintf(b, "union %s\n{\n", d.Name)
		for _, v := range d.Variants {
			b.WriteString("variant ")
			fpField(b, v.Arm)
		}
		b.WriteString("}\n")
	}
}

func fpBlock(b *strings.Builder, blk *ast.Block) {
	b.WriteString("{\n")
	for _, item := range blk.Items {
		switch item := item.(type) {
		case *ast.Field:
			b.WriteString("field ")
			fpField(b, item)
		case *ast.ConstField:
			fmt.Fprintf(b, "const(%s,%s)\n", fpExpr(item.Value), fpExpr(item.Bits))
		case *ast.ReservedItem:
			fmt.Fprintf(b, "reserved(%s)\n", fpExpr(item.Bits))
		case *ast.AlignItem:
			b.WriteString("align\n")
		case *ast.IfItem:
			neg := ""
			if item.Neg {
				neg = "!"
			}
			fmt.Fprintf(b, "if %s%s\n", neg, item.Cond.Text)
			fpBlock(b, item.Then)
			if item.Else != nil {
				b.WriteString("else\n")
				fpBlock(b, item.Else)
			}
		}
	}
	b.WriteString("}\n")
}

// fpFieldType renders a field's WHOLE type spelling: its array bound and
// either its `map[K]V` or its scalar. A map is spelled here and not in
// fpScalar because a map's VALUE is a whole field spelling rather than a type
// (docs/SPEC-TABLES.md §2.8) — and it has to be spelled somewhere, or the
// structural comparison that makes this formatter safe would be blind to
// every map it reformats.
func fpFieldType(f *ast.Field) string {
	if f.Map == nil {
		return fpBound(f.Array) + fpScalar(f.Type)
	}
	// the map's own `?` rides outside the spelling, as the value's does: the
	// checker refuses `?map`, and the fingerprint has to see the difference
	// before the checker ever runs
	opt := ""
	if f.Type.Optional {
		opt = "?"
	}
	key := f.Map.Key
	attrs := ""
	if len(f.Map.KeyAttrs) > 0 || f.Map.KeyDefault != nil {
		attrs = fpAttrs(f.Map.KeyAttrs)
		if f.Map.KeyDefault != nil {
			attrs += "=" + fpExpr(f.Map.KeyDefault)
		}
	}
	return opt + fpBound(f.Array) + "map[" + fpScalar(key) + attrs + "]" + fpFieldType(f.Map.Value)
}

func fpBound(a *ast.ArrayBound) string {
	if a == nil {
		return ""
	}
	switch a.Kind {
	case ast.ArrayFixed:
		return fmt.Sprintf("[%s]", fpExpr(a.Hi))
	case ast.ArrayUpTo:
		return fmt.Sprintf("[..%s]", fpExpr(a.Hi))
	case ast.ArrayRange:
		return fmt.Sprintf("[%s..%s]", fpExpr(a.Lo), fpExpr(a.Hi))
	}
	return ""
}

func fpScalar(t ast.ScalarType) string {
	if t.Optional {
		// `?T` is a different field from `T`: presence rides beside the value
		// (docs/SPEC-TABLES.md §2.3), so the marker belongs in the fingerprint
		inner := t
		inner.Optional = false
		return "?" + fpScalar(inner)
	}
	switch t.Kind {
	case ast.ScalarInt:
		sign := "uint"
		if t.Signed {
			sign = "int"
		}
		return fmt.Sprintf("%s%d", sign, t.Width)
	case ast.ScalarBool:
		return "bool"
	case ast.ScalarFloat32:
		return "float32"
	case ast.ScalarFloat64:
		return "float64"
	case ast.ScalarBits:
		return fmt.Sprintf("bits(%s)", fpExpr(t.Arg))
	case ast.ScalarString:
		if t.Pointer {
			return "*string" // a byte buffer at its used size (docs/SPEC-TABLES.md §2.5)
		}
		return fmt.Sprintf("string(%s)", fpExpr(t.Arg))
	case ast.ScalarBytes:
		if t.Pointer {
			return "*bytes"
		}
		return fmt.Sprintf("bytes(%s)", fpExpr(t.Arg))
	case ast.ScalarFixed:
		if t.Signed {
			return fmt.Sprintf("fixed(%s,%s)", fpExpr(t.Arg), fpExpr(t.Arg2))
		}
		return fmt.Sprintf("ufixed(%s,%s)", fpExpr(t.Arg), fpExpr(t.Arg2))
	default:
		if t.Pointer {
			// `next *Node` — the pointer binds to the type, not the name, so
			// the star sits against the target (docs/SPEC-TABLES.md)
			return "*" + t.Name
		}
		return t.Name
	}
}

// fpAttrs canonicalizes the attribute order exactly as the formatter does
// (valueless first, both classes stable), so the reorder fingerprints as
// meaning-preserving and any other change does not.
func fpAttrs(attrs []ast.Attr) string {
	var valueless, valued []string
	for _, a := range attrs {
		// language-binding attrs (cpp_native/cpp_include) rename what one
		// target CALLS the storage — they cannot change a single wire bit, so
		// they stay out of the protocol id: two units differing only in
		// bindings speak the same protocol.
		if strings.HasPrefix(a.Key, "cpp_") {
			continue
		}
		if a.Value == nil {
			valueless = append(valueless, a.Key)
		} else {
			valued = append(valued, fmt.Sprintf("%s=%s", a.Key, fpExpr(a.Value)))
		}
	}
	return "[" + strings.Join(append(valueless, valued...), ",") + "]"
}

func fpNames(names []ast.Name) string {
	var out []string
	for _, n := range names {
		out = append(out, n.Text)
	}
	return strings.Join(out, ",")
}

func fpExpr(e ast.Expr) string {
	switch e := e.(type) {
	case nil:
		return ""
	case *ast.IntLit:
		return e.Value.String() // 0x10 and 16 fingerprint alike: same value
	case *ast.FloatLit:
		return fmt.Sprintf("%g", e.Value)
	case *ast.IdentExpr:
		return e.Name
	case *ast.StringLit:
		return `"` + e.Value + `"`
	case *ast.MaxExpr:
		return e.Enum + "." + e.Sel
	case *ast.UnaryExpr:
		return "(- " + fpExpr(e.X) + ")"
	case *ast.BinaryExpr:
		return "(" + e.Op + " " + fpExpr(e.X) + " " + fpExpr(e.Y) + ")"
	case *ast.ParenExpr:
		return "(paren " + fpExpr(e.X) + ")"
	}
	return "?"
}

// fpField fingerprints one FIELD LINE — a table or type field, or a union ARM
// (docs/SPEC-TABLES.md §2.6), which is the same production.
func fpField(b *strings.Builder, f *ast.Field) {
	if f == nil {
		b.WriteString("<none>\n")
		return
	}
	def := ""
	if f.Default != nil {
		def = " = " + fpExpr(f.Default)
	}
	fmt.Fprintf(b, "%s %s %s%s\n", f.Name, fpFieldType(f), fpAttrs(f.Attrs), def)
}
