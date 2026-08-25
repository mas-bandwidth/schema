// Package parser builds the AST from tokens — hand-written recursive descent
// in the style of the Go toolchain's own parser (SPEC §7.1). The grammar is
// LL(2); the parser recovers at declaration boundaries so one error does not
// hide the rest.
package parser

import (
	"fmt"
	"math/big"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/internal/ast"
	"github.com/mas-bandwidth/schema/internal/scanner"
)

// Parse scans and parses one file.
func Parse(path string, src []byte) (*ast.File, []error) {
	toks, errs := scanner.Scan(path, src)
	if len(errs) > 0 {
		return nil, errs
	}
	base := strings.TrimSuffix(filepath.Base(path), ".schema")
	p := &parser{toks: toks, file: &ast.File{Path: path, Base: base}}
	p.parseFile()
	return p.file, p.errs
}

type parser struct {
	toks []scanner.Token
	i    int
	file *ast.File
	errs []error
}

func (p *parser) tok() scanner.Token { return p.toks[p.i] }
func (p *parser) kind() scanner.Kind { return p.toks[p.i].Kind }
func (p *parser) peek() scanner.Token {
	if p.i+1 < len(p.toks) {
		return p.toks[p.i+1]
	}
	return p.toks[len(p.toks)-1]
}

func (p *parser) advance() scanner.Token {
	t := p.toks[p.i]
	if p.kind() != scanner.EOF {
		p.i++
	}
	return t
}

func (p *parser) errf(pos ast.Pos, format string, args ...any) {
	p.errs = append(p.errs, fmt.Errorf("%s: %s", pos, fmt.Sprintf(format, args...)))
}

func (p *parser) expect(k scanner.Kind, what string) scanner.Token {
	if p.kind() != k {
		t := p.tok()
		p.errf(t.Pos, "expected %s, found %q", what, describe(t))
		return t
	}
	return p.advance()
}

func describe(t scanner.Token) string {
	switch t.Kind {
	case scanner.EOF:
		return "end of file"
	case scanner.Newline:
		return "newline"
	default:
		return t.Text
	}
}

// terminator: an actual newline, or the closing } of the enclosing block
// (not consumed), or EOF (SPEC §4.1).
func (p *parser) expectTerminator(what string) {
	switch p.kind() {
	case scanner.Newline:
		p.advance()
	case scanner.RBrace, scanner.EOF:
		// } terminates the item before it; EOF synthesizes a terminator
	default:
		t := p.tok()
		p.errf(t.Pos, "expected newline after %s, found %q", what, describe(t))
		p.skipToTerminator()
	}
}

func (p *parser) skipToTerminator() {
	for p.kind() != scanner.Newline && p.kind() != scanner.RBrace && p.kind() != scanner.EOF {
		p.advance()
	}
	if p.kind() == scanner.Newline {
		p.advance()
	}
}

// skipDecl recovers at a declaration boundary: skip to a top-level newline,
// balancing braces, so one bad declaration does not hide the rest.
func (p *parser) skipDecl() {
	depth := 0
	for p.kind() != scanner.EOF {
		switch p.kind() {
		case scanner.LBrace:
			depth++
		case scanner.RBrace:
			if depth > 0 {
				depth--
			}
		case scanner.Newline:
			if depth == 0 {
				p.advance()
				return
			}
		}
		p.advance()
	}
}

func (p *parser) parseFile() {
	for p.kind() != scanner.EOF {
		if p.kind() == scanner.Newline {
			p.advance()
			continue
		}
		before := p.i
		p.parseDecl()
		if p.i == before {
			p.skipDecl() // ensure progress on a parse error
		}
	}
	if p.file.Package == "" && len(p.errs) == 0 {
		p.errf(ast.Pos{File: p.file.Path, Line: 1, Col: 1},
			"missing package declaration (package appears once per file, as its first declaration — SPEC §3.2)")
	}
}

func (p *parser) parseDecl() {
	t := p.tok()
	switch t.Kind {
	case scanner.KwPackage:
		p.advance()
		name := p.expect(scanner.Ident, "package name")
		p.expectTerminator("package declaration")
		if p.file.Package != "" {
			p.errf(t.Pos, "duplicate package declaration (one per file — SPEC §3.2)")
			return
		}
		if len(p.file.Decls) > 0 {
			p.errf(t.Pos, "package must be the file's first declaration (SPEC §3.2)")
		}
		p.file.Package = name.Text
		p.file.PkgPos = t.Pos

	case scanner.KwConst:
		p.advance()
		name := p.expect(scanner.Ident, "constant name")
		d := &ast.ConstDecl{Name: name.Text, Pos: t.Pos}
		if typ, ok := p.constType(); ok {
			d.Type = typ
		}
		p.expect(scanner.Assign, "=")
		d.Expr = p.parseExpr()
		if p.kind() == scanner.Pipe {
			p.errf(p.tok().Pos, "a constant takes no qualification, and | is never an operator — the language has no bitwise-or (SPEC §4.2)")
			p.skipToTerminator()
		}
		p.expectTerminator("constant declaration")
		p.file.Decls = append(p.file.Decls, d)

	case scanner.KwEnum:
		p.advance()
		name := p.expect(scanner.Ident, "enum name")
		d := &ast.EnumDecl{Name: name.Text, Pos: t.Pos}
		d.Attrs = p.declQualifiers("enum")
		d.Variants = p.parseVariantList("enum")
		p.expectTerminator("enum declaration")
		p.file.Decls = append(p.file.Decls, d)

	case scanner.KwType:
		p.advance()
		name := p.expect(scanner.Ident, "type name")
		d := &ast.TypeDecl{Name: name.Text, Pos: t.Pos}
		d.Attrs = p.declQualifiers("type")
		d.Body = p.parseBlock()
		p.expectTerminator("type declaration")
		p.file.Decls = append(p.file.Decls, d)

	case scanner.KwTable:
		// `table` is reserved and refused: tables are not part of the
		// language. Realtime wire types are `type`; the id is exact-match
		// and content that outlives builds is not this compiler's subject.
		p.errf(t.Pos, "tables are not part of the language — declare a `type` (the id is exact-match: same protocol id or refuse, SPEC §3)")
		p.skipDecl()

	case scanner.KwMessage:
		p.advance()
		name := p.expect(scanner.Ident, "message name")
		d := &ast.MessageDecl{Name: name.Text, Pos: t.Pos}
		if p.kind() == scanner.Pipe {
			p.errf(p.tok().Pos, "a message declaration takes no qualification (SPEC §4.2)")
			p.skipToTerminator()
		}
		d.Body = p.parseBlock()
		p.expectTerminator("message declaration")
		p.file.Decls = append(p.file.Decls, d)

	case scanner.KwObject:
		p.advance()
		name := p.expect(scanner.Ident, "object name")
		d := &ast.ObjectDecl{Name: name.Text, Pos: t.Pos}
		if p.kind() == scanner.Pipe {
			p.errf(p.tok().Pos, "an object declaration takes no qualification (SPEC §4.2)")
			p.skipToTerminator()
		}
		d.Body = p.parseBlock()
		p.expectTerminator("object declaration")
		p.file.Decls = append(p.file.Decls, d)

	case scanner.KwSwitch, scanner.KwCase:
		p.errf(t.Pos, "switch is cut from v1 (SPEC §4.4); the keyword stays reserved")
		p.skipDecl()

	case scanner.KwInt, scanner.KwUint:
		p.errf(t.Pos, "%q is reserved — did you mean %s32?", t.Text, t.Text)
		p.skipDecl()

	case scanner.Ident:
		// contextual keywords at file scope: flags, contexts (SPEC §4.2)
		switch t.Text {
		case "flags":
			p.advance()
			name := p.expect(scanner.Ident, "flags name")
			d := &ast.FlagsDecl{Name: name.Text, Pos: t.Pos}
			d.Attrs = p.declQualifiers("flags")
			d.Variants = p.parseVariantList("flags")
			p.expectTerminator("flags declaration")
			p.file.Decls = append(p.file.Decls, d)
		case "contexts":
			p.advance()
			d := &ast.ContextsDecl{Pos: t.Pos}
			d.Names = p.parseVariantList("contexts")
			p.expectTerminator("contexts declaration")
			p.file.Decls = append(p.file.Decls, d)
		case "union":
			p.advance()
			name := p.expect(scanner.Ident, "union name")
			d := &ast.UnionDecl{Name: name.Text, Pos: t.Pos}
			if p.kind() == scanner.Pipe {
				p.errf(p.tok().Pos, "a union declaration takes no qualification (SPEC §4.2)")
				p.skipToTerminator()
			}
			d.Variants = p.parseUnionBody()
			p.expectTerminator("union declaration")
			p.file.Decls = append(p.file.Decls, d)
		default:
			p.errf(t.Pos, "unexpected %q at file scope (declarations begin with package, const, enum, flags, type, message, object, union or contexts)", t.Text)
			p.skipDecl()
		}

	default:
		p.errf(t.Pos, "unexpected %q at file scope", describe(t))
		p.skipDecl()
	}
}

// constType parses the optional explicit type of a const declaration.
func (p *parser) constType() (string, bool) {
	switch p.kind() {
	case scanner.KwInt8, scanner.KwInt16, scanner.KwInt32, scanner.KwInt64,
		scanner.KwUint8, scanner.KwUint16, scanner.KwUint32, scanner.KwUint64,
		scanner.KwFloat32, scanner.KwFloat64:
		return p.advance().Text, true
	}
	return "", false
}

func (p *parser) parseVariantList(what string) []ast.Name {
	var names []ast.Name
	p.skipNewlineBeforeBrace()
	p.expect(scanner.LBrace, "{")
	for p.kind() != scanner.RBrace && p.kind() != scanner.EOF {
		t := p.expect(scanner.Ident, what+" variant name")
		if t.Kind != scanner.Ident {
			break
		}
		names = append(names, ast.Name{Text: t.Text, Pos: t.Pos})
		if p.kind() == scanner.Comma {
			p.advance() // trailing comma allowed; newlines around commas are whitespace
			continue
		}
		break
	}
	p.expect(scanner.RBrace, "}")
	return names
}

// parseUnionBody parses a union's { variant Type ... } rows (SPEC §4.8): each
// row is a variant name then its payload type name, newline-terminated like a
// field. No attributes, no defaults, no bounds — a variant row names a thing,
// it does not describe a wire refinement.
func (p *parser) parseUnionBody() []ast.UnionVariant {
	var variants []ast.UnionVariant
	p.skipNewlineBeforeBrace()
	p.expect(scanner.LBrace, "{")
	for {
		switch p.kind() {
		case scanner.RBrace:
			p.advance()
			return variants
		case scanner.EOF:
			p.errf(p.tok().Pos, "unexpected end of file inside union body (missing } )")
			return variants
		case scanner.Newline:
			p.advance()
		default:
			if p.kind() == scanner.KwType {
				// `type` scans as a keyword, so this cannot reach the
				// checker: the named refusal lives here (SPEC §4.8 — it is
				// the tag member's own name in the C/C++ representations)
				p.errf(p.tok().Pos, "variant type is a compile error — it is the tag member's own name in the C and C++ union representations; rename at the source (SPEC §4.8)")
				p.advance()
				p.skipToTerminator()
				continue
			}
			name := p.expect(scanner.Ident, "union variant name")
			if name.Kind != scanner.Ident {
				p.skipToTerminator()
				continue
			}
			// the named refusal for non-type payloads the grammar can spot
			// here: a scalar keyword or an array bound (SPEC §4.8 — a
			// payload is a declared type; wrap scalars, no arrays)
			switch t := p.tok(); t.Kind {
			case scanner.LBrack, scanner.KwBits, scanner.KwBool, scanner.KwFloat32,
				scanner.KwFloat64, scanner.KwString, scanner.KwBytes, scanner.KwFixed,
				scanner.KwUfixed, scanner.KwInt8, scanner.KwInt16, scanner.KwInt32,
				scanner.KwInt64, scanner.KwUint8, scanner.KwUint16, scanner.KwUint32,
				scanner.KwUint64, scanner.KwInt128, scanner.KwUint128:
				p.errf(t.Pos, "a union variant's payload is a declared type — wrap a scalar or array in a type (SPEC §4.8)")
				p.skipToTerminator()
				continue
			}
			typ := p.expect(scanner.Ident, "union variant payload type")
			if typ.Kind != scanner.Ident {
				p.skipToTerminator()
				continue
			}
			variants = append(variants, ast.UnionVariant{
				Name: name.Text, Pos: name.Pos, Type: typ.Text, TypePos: typ.Pos,
			})
			p.expectTerminator("union variant")
		}
	}
}

// skipNewlineBeforeBrace tolerates the opening brace on its own line —
// Allman is the house style (SPEC §4.1); the formatter normalizes, the
// parser reads both placements.
func (p *parser) skipNewlineBeforeBrace() {
	if p.kind() == scanner.Newline && p.peek().Kind == scanner.LBrace {
		p.advance()
	}
}

func (p *parser) parseBlock() *ast.Block {
	b := &ast.Block{}
	p.skipNewlineBeforeBrace()
	p.expect(scanner.LBrace, "{")
	for {
		switch p.kind() {
		case scanner.RBrace:
			p.advance()
			return b
		case scanner.EOF:
			p.errf(p.tok().Pos, "unexpected end of file inside block (missing } )")
			return b
		case scanner.Newline:
			p.advance()
		default:
			before := p.i
			if item := p.parseItem(); item != nil {
				b.Items = append(b.Items, item)
			}
			if p.i == before {
				p.skipToTerminator()
			}
		}
	}
}

func (p *parser) parseItem() ast.Item {
	t := p.tok()
	switch t.Kind {
	case scanner.KwConst: // const(value, bits) — one token of lookahead disambiguates
		p.advance()
		p.expect(scanner.LParen, "(")
		v := p.parseExpr()
		p.expect(scanner.Comma, ",")
		b := p.parseExpr()
		p.expect(scanner.RParen, ")")
		p.expectTerminator("const field")
		return &ast.ConstField{Pos: t.Pos, Value: v, Bits: b}

	case scanner.KwReserved:
		p.advance()
		p.expect(scanner.LParen, "(")
		b := p.parseExpr()
		p.expect(scanner.RParen, ")")
		p.expectTerminator("reserved field")
		return &ast.ReservedItem{Pos: t.Pos, Bits: b}

	case scanner.KwAlign:
		p.advance()
		p.expectTerminator("align")
		return &ast.AlignItem{Pos: t.Pos}

	case scanner.KwIf:
		p.advance()
		item := &ast.IfItem{Pos: t.Pos}
		if p.kind() == scanner.Not {
			p.advance()
			item.Neg = true
		}
		cond := p.expect(scanner.Ident, "condition field name")
		item.Cond = ast.Name{Text: cond.Text, Pos: cond.Pos}
		item.Then = p.parseBlock()
		// a newline between } and else is tolerated; the formatter rewrites
		// it to `} else {` on one line (SPEC §4.1)
		if p.kind() == scanner.Newline && p.peek().Kind == scanner.KwElse {
			p.advance()
		}
		if p.kind() == scanner.KwElse {
			p.advance()
			item.Else = p.parseBlock()
		}
		p.expectTerminator("if")
		return item

	case scanner.KwSwitch, scanner.KwCase:
		p.errf(t.Pos, "switch is cut from v1 (SPEC §4.4); the keyword stays reserved")
		p.skipToTerminator()
		return nil

	case scanner.Ident:
		p.advance()
		f := &ast.Field{Name: t.Text, Pos: t.Pos}
		if p.kind() == scanner.LBrack {
			f.Array = p.parseArrayBound()
		}
		f.Type = p.parseScalar()
		if p.kind() == scanner.Assign {
			// optional specified default: `invulnerable bool = true | local`
			// — the default DEFINES the fresh value, so it precedes the
			// qualification (SPEC §4.2)
			p.advance()
			f.Default = p.parseExpr()
		}
		if p.kind() == scanner.LBrack {
			// the RETIRED trailing attribute block (SPEC §4.2) — refuse with
			// the replacement named, then parse the group so the rest of the
			// file's diagnostics still land
			p.errf(p.tok().Pos, "the [ ... ] attribute block is retired — qualifiers follow | to the end of the line (SPEC §4.2)")
			f.Attrs = p.parseBracketAttrs()
			if p.kind() == scanner.Assign { // the old default-after-attrs order
				p.advance()
				f.Default = p.parseExpr()
			}
		}
		if p.kind() == scanner.Pipe {
			f.Attrs = p.parsePipeAttrs()
		}
		p.expectTerminator("field")
		return f

	default:
		p.errf(t.Pos, "unexpected %q inside block", describe(t))
		p.skipToTerminator()
		return nil
	}
}

func (p *parser) parseArrayBound() *ast.ArrayBound {
	p.expect(scanner.LBrack, "[")
	b := &ast.ArrayBound{}
	switch p.kind() {
	case scanner.LessEq:
		// the comparison-operator spelling is retired (SPEC §4.3): a bound
		// is a range literal, not a truncated expression — refusal with the
		// replacement named, then parse on so one bound does not hide the
		// rest of the file's diagnostics
		p.errf(p.tok().Pos, "the [<= N] bound is retired — spell it [..N], the range literal for a count in [0, N] (SPEC §4.3)")
		p.advance()
		b.Kind = ast.ArrayUpTo
		b.Hi = p.parseExpr()
	case scanner.DotDot:
		// [..N] — sugar for [0..N], reads "up to N" (SPEC §4.3)
		p.advance()
		b.Kind = ast.ArrayUpTo
		b.Hi = p.parseExpr()
	default:
		first := p.parseExpr()
		if p.kind() == scanner.DotDot {
			p.advance()
			b.Kind = ast.ArrayRange
			b.Lo = first
			b.Hi = p.parseExpr()
		} else {
			b.Kind = ast.ArrayFixed
			b.Hi = first
		}
	}
	p.expect(scanner.RBrack, "]")
	return b
}

func (p *parser) parseScalar() ast.ScalarType {
	t := p.tok()
	switch t.Kind {
	case scanner.KwInt8, scanner.KwInt16, scanner.KwInt32, scanner.KwInt64,
		scanner.KwUint8, scanner.KwUint16, scanner.KwUint32, scanner.KwUint64,
		scanner.KwInt128, scanner.KwUint128:
		p.advance()
		signed := t.Text[0] == 'i'
		width, _ := strconv.Atoi(strings.TrimPrefix(strings.TrimPrefix(t.Text, "uint"), "int"))
		return ast.ScalarType{Kind: ast.ScalarInt, Signed: signed, Width: width, Pos: t.Pos}
	case scanner.KwFixed, scanner.KwUfixed:
		// fixed(I, F) / ufixed(I, F) — the Q format is the type's shape, so it
		// is positional like bits(N)/string(N) (SPEC §4.2, the
		// positional/attribute line); the u prefix names the storage's
		// signedness, the integer family's own int/uint precedent (§9 q17,
		// closed 2026-08-15)
		p.advance()
		p.expect(scanner.LParen, "(")
		i := p.parseExpr()
		p.expect(scanner.Comma, ",")
		f := p.parseExpr()
		p.expect(scanner.RParen, ")")
		return ast.ScalarType{Kind: ast.ScalarFixed, Signed: t.Kind == scanner.KwFixed, Arg: i, Arg2: f, Pos: t.Pos}
	case scanner.KwBool:
		p.advance()
		return ast.ScalarType{Kind: ast.ScalarBool, Pos: t.Pos}
	case scanner.KwFloat32:
		p.advance()
		return ast.ScalarType{Kind: ast.ScalarFloat32, Pos: t.Pos}
	case scanner.KwFloat64:
		p.advance()
		return ast.ScalarType{Kind: ast.ScalarFloat64, Pos: t.Pos}
	case scanner.KwBits, scanner.KwString, scanner.KwBytes:
		p.advance()
		p.expect(scanner.LParen, "(")
		arg := p.parseExpr()
		p.expect(scanner.RParen, ")")
		kind := ast.ScalarBits
		switch t.Kind {
		case scanner.KwString:
			kind = ast.ScalarString
		case scanner.KwBytes:
			kind = ast.ScalarBytes
		}
		return ast.ScalarType{Kind: kind, Arg: arg, Pos: t.Pos}
	case scanner.KwInt, scanner.KwUint:
		p.advance()
		p.errf(t.Pos, "%q is reserved — did you mean %s32?", t.Text, t.Text)
		return ast.ScalarType{Kind: ast.ScalarInt, Signed: t.Kind == scanner.KwInt, Width: 32, Pos: t.Pos}
	case scanner.Ident:
		p.advance()
		return ast.ScalarType{Kind: ast.ScalarNamed, Name: t.Text, Pos: t.Pos}
	default:
		p.errf(t.Pos, "expected a field type, found %q", describe(t))
		return ast.ScalarType{Kind: ast.ScalarNamed, Name: "<error>", Pos: t.Pos}
	}
}

// declQualifiers parses a declaration line's optional qualification: the |
// section (which runs to end of line, so the caller's body opens on the NEXT
// line — the newline is consumed here), or the RETIRED [ ... ] block,
// refused by name but parsed so diagnosis continues (SPEC §4.2).
func (p *parser) declQualifiers(what string) []ast.Attr {
	switch p.kind() {
	case scanner.LBrack:
		p.errf(p.tok().Pos, "the [ ... ] attribute block is retired — a %s's qualifiers follow | to the end of the line, and the body opens on the next line (SPEC §4.2)", what)
		return p.parseBracketAttrs()
	case scanner.Pipe:
		attrs := p.parsePipeAttrs()
		if p.kind() == scanner.Newline {
			p.advance() // the section claimed the line; the body opens below
		}
		return attrs
	}
	return nil
}

// parsePipeAttrs parses a | qualification section: comma-separated
// attributes running to the end of the line (SPEC §4.2). The terminator is
// left for the caller. An empty section is a parse error.
func (p *parser) parsePipeAttrs() []ast.Attr {
	var attrs []ast.Attr
	pipe := p.expect(scanner.Pipe, "|")
	for p.kind() == scanner.Ident {
		key := p.advance()
		a := ast.Attr{Key: key.Text, Pos: key.Pos}
		if p.kind() == scanner.Assign {
			p.advance()
			a.Value = p.parseExpr() // a bare word value (round = up) parses as an IdentExpr
		}
		attrs = append(attrs, a)
		if p.kind() == scanner.Comma {
			p.advance()
			continue
		}
		break
	}
	if len(attrs) == 0 {
		p.errf(pipe.Pos, "empty qualification section — write the qualifiers after | or drop it (SPEC §4.2)")
	}
	return attrs
}

// parseBracketAttrs parses the RETIRED [ ... ] attribute block — kept so the
// named refusal can recover and keep diagnosing the rest of the file, and so
// `schema fmt -migrate` can rewrite old sources (SPEC §7.4).
func (p *parser) parseBracketAttrs() []ast.Attr {
	var attrs []ast.Attr
	p.expect(scanner.LBrack, "[")
	for p.kind() != scanner.RBrack && p.kind() != scanner.EOF {
		key := p.expect(scanner.Ident, "attribute key")
		if key.Kind != scanner.Ident {
			break
		}
		a := ast.Attr{Key: key.Text, Pos: key.Pos}
		if p.kind() == scanner.Assign {
			p.advance()
			a.Value = p.parseExpr() // a bare word value (round = up) parses as an IdentExpr
		}
		attrs = append(attrs, a)
		if p.kind() == scanner.Comma {
			p.advance()
			continue
		}
		break
	}
	p.expect(scanner.RBrack, "]")
	return attrs
}

// Expressions — precedence climbing: unary minus binds tightest, then * / %,
// then + - (SPEC §4.2 IntExpr/FloatExpr).

func (p *parser) parseExpr() ast.Expr { return p.parseAdd() }

func (p *parser) parseAdd() ast.Expr {
	x := p.parseMul()
	for p.kind() == scanner.Plus || p.kind() == scanner.Minus {
		op := p.advance()
		y := p.parseMul()
		x = &ast.BinaryExpr{Pos: op.Pos, Op: op.Text, X: x, Y: y}
	}
	return x
}

func (p *parser) parseMul() ast.Expr {
	x := p.parseUnary()
	for p.kind() == scanner.Star || p.kind() == scanner.Slash || p.kind() == scanner.Percent {
		op := p.advance()
		y := p.parseUnary()
		x = &ast.BinaryExpr{Pos: op.Pos, Op: op.Text, X: x, Y: y}
	}
	return x
}

func (p *parser) parseUnary() ast.Expr {
	if p.kind() == scanner.Minus {
		t := p.advance()
		return &ast.UnaryExpr{Pos: t.Pos, Op: "-", X: p.parseUnary()}
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() ast.Expr {
	t := p.tok()
	switch t.Kind {
	case scanner.Int:
		p.advance()
		v := new(big.Int)
		if _, ok := v.SetString(t.Text, 0); !ok {
			p.errf(t.Pos, "malformed integer literal %q", t.Text)
		}
		return &ast.IntLit{Pos: t.Pos, Value: v, Text: t.Text}
	case scanner.Float:
		p.advance()
		v, err := strconv.ParseFloat(t.Text, 64)
		if err != nil {
			p.errf(t.Pos, "malformed float literal %q", t.Text)
		}
		return &ast.FloatLit{Pos: t.Pos, Value: v, Text: t.Text}
	case scanner.String:
		p.advance()
		return &ast.StringLit{Pos: t.Pos, Value: strings.Trim(t.Text, `"`)}
	case scanner.Ident:
		p.advance()
		// E.Max / F.Count — contextual after '.' (SPEC §4.2)
		if p.kind() == scanner.Dot {
			dot := p.advance()
			m := p.expect(scanner.Ident, `"Max" or "Count"`)
			if m.Text != "Max" && m.Text != "Count" {
				p.errf(dot.Pos, "only .Max (enums and generated sets) and .Count (flags) are legal after a name (found .%s)", m.Text)
			}
			return &ast.MaxExpr{Pos: t.Pos, Enum: t.Text, Sel: m.Text}
		}
		return &ast.IdentExpr{Pos: t.Pos, Name: t.Text}
	case scanner.LParen:
		p.advance()
		x := p.parseExpr()
		p.expect(scanner.RParen, ")")
		return &ast.ParenExpr{Pos: t.Pos, X: x}
	default:
		p.advance()
		p.errf(t.Pos, "expected an expression, found %q", describe(t))
		return &ast.IntLit{Pos: t.Pos, Value: big.NewInt(0), Text: "0"}
	}
}
