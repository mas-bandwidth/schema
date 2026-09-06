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

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/internal/scanner"
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
	case scanner.Doc:
		return "doc comment"
	default:
		return t.Text
	}
}

// takeDoc consumes a `///` DOC COMMENT standing before an item and returns
// its text (SPEC §4.1). The scanner has already held the block to its own
// lines and to touching what follows; what remains is BINDING, which is the
// caller's: a declaration, a field, a variant or an arm takes the text, and
// anything else refuses it by name through refuseDoc.
func (p *parser) takeDoc() (string, ast.Pos, bool) {
	if p.kind() != scanner.Doc {
		return "", ast.Pos{}, false
	}
	t := p.advance()
	return t.Text, t.Pos, true
}

// refuseDoc is the diagnostic for a `///` block above something that has no
// descriptor row to carry a doc comment (SPEC §4.1, SPEC-TABLES.md §8): the
// block was an opt-in, and dropping it silently is the outcome opt-in exists
// to prevent, so it is refused naming the spelling that works.
func (p *parser) refuseDoc(pos ast.Pos, what string) {
	p.errf(pos, "nothing here carries a doc comment — a /// block above %s reaches no descriptor; write // for a comment there (SPEC §4.1)", what)
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
		doc, docPos, hasDoc := p.takeDoc()
		if hasDoc && p.kind() == scanner.EOF {
			p.refuseDoc(docPos, "the end of the file")
			break
		}
		p.parseDecl(doc, docPos, hasDoc)
		if p.i == before {
			p.skipDecl() // ensure progress on a parse error
		}
	}
	if p.file.Package == "" && len(p.errs) == 0 {
		p.errf(ast.Pos{File: p.file.Path, Line: 1, Col: 1},
			"missing package declaration (package appears once per file, as its first declaration — SPEC §3.2)")
	}
}

// parseDecl parses one file-scope declaration. doc is the `///` block above
// it, when one stands there (SPEC §4.1): a const, enum, flags, type, table or
// union declaration takes it, and `package` refuses it by name.
func (p *parser) parseDecl(doc string, docPos ast.Pos, hasDoc bool) {
	t := p.tok()
	switch t.Kind {
	case scanner.KwPackage:
		if hasDoc {
			p.refuseDoc(docPos, "package")
		}
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
		d := &ast.ConstDecl{Name: name.Text, Pos: t.Pos, Doc: doc}
		if typ, ok := p.constType(); ok {
			d.Type = typ
		}
		p.expect(scanner.Assign, "=")
		d.Expr = p.parseExpr()
		if p.kind() == scanner.Pipe {
			// the qualification section carries TAGS and nothing else
			// (SPEC §4.2); | is never an operator, the language has no
			// bitwise-or, and the checker refuses anything valued there
			d.Attrs = p.parsePipeAttrs()
		}
		p.expectTerminator("constant declaration")
		p.file.Decls = append(p.file.Decls, d)

	case scanner.KwEnum:
		p.advance()
		name := p.expect(scanner.Ident, "enum name")
		d := &ast.EnumDecl{Name: name.Text, Pos: t.Pos, Doc: doc}
		d.Attrs = p.declQualifiers("enum")
		d.Variants = p.parseVariantList("enum")
		p.expectTerminator("enum declaration")
		p.file.Decls = append(p.file.Decls, d)

	case scanner.KwType:
		p.advance()
		name := p.expect(scanner.Ident, "type name")
		d := &ast.TypeDecl{Name: name.Text, Pos: t.Pos, Doc: doc}
		d.Attrs = p.declQualifiers("type")
		d.Body = p.parseBlock()
		p.expectTerminator("type declaration")
		p.file.Decls = append(p.file.Decls, d)

	case scanner.KwTable:
		// `table` declares a data type on the evolution-tolerant TABLE wire
		// (docs/SPEC-TABLES.md): field identity by name hash, unknown fields
		// skipped, absent fields defaulted. The body grammar is the type
		// body's; the qualification carries tags and the `was` rename
		// (docs/SPEC-TABLES.md §5). The BLOCK FORM
		// (docs/SPEC-TABLES.md §19) declares nothing at all: every fixed table has
		// one, emitted on the side, so there is no marker to parse.
		p.advance()
		name := p.expect(scanner.Ident, "table name")
		d := &ast.TableDecl{Name: name.Text, Pos: t.Pos, Doc: doc}
		d.Attrs = p.declQualifiers("table")
		d.Body = p.parseBlock()
		p.expectTerminator("table declaration")
		p.file.Decls = append(p.file.Decls, d)

	case scanner.KwMessage:
		// `message` is reserved and refused: messages are not part of the
		// language. A message set is a union of payload types plus your own
		// framing — declare the payloads as `type` and tag them with a
		// `union` (SPEC §4.8).
		p.errf(t.Pos, "messages are not part of the language — declare a `type` and tag a set with a `union` (SPEC §4.8)")
		p.skipDecl()

	case scanner.KwObject:
		// `object` is reserved and refused: objects are not part of the
		// language. Wire types are `type`.
		p.errf(t.Pos, "objects are not part of the language — declare a `type` (SPEC §4.2)")
		p.skipDecl()

	case scanner.KwSwitch, scanner.KwCase:
		p.errf(t.Pos, "switch is cut from v1 (SPEC §4.4); the keyword stays reserved")
		p.skipDecl()

	case scanner.KwInt, scanner.KwUint:
		p.errf(t.Pos, "%q is reserved — did you mean %s32?", t.Text, t.Text)
		p.skipDecl()

	case scanner.Ident:
		// contextual keywords at file scope: flags, union (SPEC §4.2)
		switch t.Text {
		case "flags":
			p.advance()
			name := p.expect(scanner.Ident, "flags name")
			d := &ast.FlagsDecl{Name: name.Text, Pos: t.Pos, Doc: doc}
			d.Attrs = p.declQualifiers("flags")
			d.Variants = p.parseVariantList("flags")
			p.expectTerminator("flags declaration")
			p.file.Decls = append(p.file.Decls, d)
		case "contexts":
			// contexts is refused at file scope: build contexts are not
			// part of the language — every field of a type is identical
			// on every side of the wire.
			p.errf(t.Pos, "contexts are not part of the language — a type's fields are identical on every peer (SPEC §4.2)")
			p.skipDecl()
		case "union":
			p.advance()
			name := p.expect(scanner.Ident, "union name")
			d := &ast.UnionDecl{Name: name.Text, Pos: t.Pos, Doc: doc}
			// the qualification carries TAGS and nothing else (SPEC §4.2)
			d.Attrs = p.declQualifiers("union")
			d.Variants = p.parseUnionBody()
			p.expectTerminator("union declaration")
			p.file.Decls = append(p.file.Decls, d)
		default:
			p.errf(t.Pos, "unexpected %q at file scope (declarations begin with package, const, enum, flags, type, table or union)", t.Text)
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

// parseVariantList parses an `enum` or `flags` body (SPEC §4.2): variant
// names separated by a COMMA or by a NEWLINE, a trailing separator allowed.
// Each variant may carry a `///` doc comment above it and a qualification
// section after it. The section runs to the end of the line, so a qualified
// variant ends its line and the newline is its separator; a `}` on that same
// line is refused by name, because the pipe claims the rest of the line and
// the brace would be arguing with it.
func (p *parser) parseVariantList(what string) []ast.Name {
	var names []ast.Name
	p.skipNewlineBeforeBrace()
	p.expect(scanner.LBrace, "{")
	for p.kind() != scanner.RBrace && p.kind() != scanner.EOF {
		doc, docPos, hasDoc := p.takeDoc()
		if hasDoc && (p.kind() == scanner.RBrace || p.kind() == scanner.EOF) {
			p.refuseDoc(docPos, "a closing brace")
			break
		}
		t := p.expect(scanner.Ident, what+" variant name")
		if t.Kind != scanner.Ident {
			break
		}
		n := ast.Name{Text: t.Text, Pos: t.Pos, Doc: doc}
		if p.kind() == scanner.Pipe {
			// A QUALIFIED VARIANT (SPEC §4.2): the section runs to the end of
			// the line, so the variant ends its line and the newline is its
			// separator. A trailing comma inside the section is the section's
			// own, and the list goes on below it.
			n.Attrs = p.parsePipeAttrs()
			if p.kind() == scanner.RBrace && p.tok().Pos.Line == t.Pos.Line {
				p.errf(p.tok().Pos, "a qualified variant ends its own line: write the list one variant per line, with the closing brace on a line of its own (SPEC §4.1)")
			}
		}
		names = append(names, n)
		switch p.kind() {
		case scanner.Comma:
			p.advance() // a newline after a comma is whitespace (SPEC §4.1)
		case scanner.Newline:
			for p.kind() == scanner.Newline {
				p.advance()
			}
		case scanner.Ident:
			// two names on one line with nothing between them: read on as
			// if the separator were there, so the rest of the body still
			// diagnoses
			p.errf(p.tok().Pos, "%s variants are separated by a comma or a newline — %s follows %s with neither (SPEC §4.2)", what, p.tok().Text, t.Text)
		}
	}
	p.expect(scanner.RBrace, "}")
	return names
}

// parseUnionBody parses a union's arm rows (SPEC §4.8): EACH ROW IS A FIELD
// LINE — an arm name, then any field type with the value-shaping attributes
// that type takes. What an arm may not carry (a specified default, `?`, `was`,
// `json`) parses here and is refused by the CHECKER at the arm, the way §2.3's
// `?` is: the grammar accepts the spelling and the diagnostic names the real
// problem.
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
			doc, docPos, hasDoc := p.takeDoc()
			if hasDoc && (p.kind() == scanner.RBrace || p.kind() == scanner.EOF) {
				p.refuseDoc(docPos, "a closing brace")
				continue
			}
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
			typePos := p.tok().Pos
			if p.kind() == scanner.Newline || p.kind() == scanner.RBrace || p.kind() == scanner.EOF || p.kind() == scanner.Pipe {
				// A PAYLOAD-FREE ARM is a bare name (SPEC §4.8): the arm has
				// no storage, the packet wire carries the tag alone and the
				// table wire the arm id with L = 0 (docs/SPEC-TABLES.md §2.6).
				// Its qualification section, when it has one, follows the name.
				v := ast.UnionVariant{Name: name.Text, Pos: name.Pos, TypePos: typePos, Doc: doc}
				if p.kind() == scanner.Pipe {
					v.Attrs = p.parsePipeAttrs()
				}
				variants = append(variants, v)
				p.expectTerminator("union arm")
				continue
			}
			item := p.parseFieldLine(name)
			arm, ok := item.(*ast.Field)
			if !ok {
				continue
			}
			arm.Doc = doc
			v := ast.UnionVariant{Name: arm.Name, Pos: arm.Pos, TypePos: typePos, Arm: arm, Doc: doc}
			if arm.Array == nil && !arm.Type.Optional && !arm.Type.Pointer && arm.Type.Kind == ast.ScalarNamed {
				// the arm names a bare declaration: the spelling every arm had
				// before an arm could be any field type
				v.Type = arm.Type.Name
			}
			variants = append(variants, v)
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
			doc, docPos, hasDoc := p.takeDoc()
			if hasDoc && (p.kind() == scanner.RBrace || p.kind() == scanner.EOF) {
				p.refuseDoc(docPos, "a closing brace")
				continue
			}
			if item := p.parseItem(doc, docPos, hasDoc); item != nil {
				b.Items = append(b.Items, item)
			}
			if p.i == before {
				p.skipToTerminator()
			}
		}
	}
}

// parseItem parses one body item. doc is the `///` block above it (SPEC
// §4.1): a FIELD takes it, and every other item — const( ), reserved( ),
// align, if — has no descriptor row to carry one and refuses it by name.
func (p *parser) parseItem(doc string, docPos ast.Pos, hasDoc bool) ast.Item {
	t := p.tok()
	if hasDoc && t.Kind != scanner.Ident {
		what := describe(t)
		switch t.Kind {
		case scanner.KwConst:
			what = "a const( ) item"
		case scanner.KwReserved:
			what = "a reserved( ) item"
		case scanner.KwAlign:
			what = "an align item"
		case scanner.KwIf:
			what = "an if branch"
		}
		p.refuseDoc(docPos, what)
	}
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
		item := p.parseFieldLine(t)
		if f, ok := item.(*ast.Field); ok {
			f.Doc = doc
		}
		return item

	default:
		p.errf(t.Pos, "unexpected %q inside block", describe(t))
		p.skipToTerminator()
		return nil
	}
}

// parseFieldLine parses a field row whose NAME token is already consumed:
// `name [?][bound]Type [= default] [| attrs] NL`. A union ARM is the same
// production (SPEC §4.8, docs/SPEC-TABLES.md §2.6) — an arm is a field line —
// so the two callers share it and no spelling can be legal in one and unknown
// in the other.
func (p *parser) parseFieldLine(t scanner.Token) ast.Item {
	{
		f := &ast.Field{Name: t.Text, Pos: t.Pos}
		// `settings ?GunnerSettings` — the OPTIONAL prefix (docs/SPEC-TABLES.md
		// §2.3). It binds to the whole field type, so it precedes an array
		// bound too; what may carry it is the CHECKER's business (a table
		// body, and not a pointer, an array, a string or bytes), so the
		// grammar accepts the spelling and the diagnostic names the real
		// problem.
		optional := false
		if p.kind() == scanner.Question {
			p.advance()
			optional = true
		}
		if p.kind() == scanner.LBrack {
			f.Array = p.parseArrayBound()
			p.refuseArrayOfArrays()
		}
		if p.kind() == scanner.KwMap {
			// `ships map[string(32)]ShipConfig` — a MAP (docs/SPEC-TABLES.md
			// §2.8). What may carry one is the CHECKER's business (a table
			// body, a bounded-string or integer key, no bound and no
			// attribute), so the grammar takes the spelling wherever a field
			// type stands and the diagnostic names the real problem.
			f.Map = p.parseMapType()
			f.Type.Optional = optional
		} else {
			f.Type = p.parseScalar()
			f.Type.Optional = optional
		}
		if p.kind() == scanner.Assign {
			// optional specified default: `invulnerable bool = true | local`
			// — the default DEFINES the fresh value, so it precedes the
			// qualification (SPEC §4.2). A brace list is a FLAGS default,
			// `caps Caps = { Jump, Crouch }`: the variant names whose bits
			// the fresh mask holds.
			p.advance()
			if p.kind() == scanner.LBrace {
				f.Default = p.parseSetLit()
			} else {
				f.Default = p.parseExpr()
			}
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
	}
}

// parseMapType reads `map[K]V` from the `map` keyword (docs/SPEC-TABLES.md
// §2.8). The VALUE is a whole field spelling — an array bound, an optional
// prefix, or another map — so `map[string(16)]map[uint8]Item` and
// `map[uint32]?[..4]Slot` are one production.
func (p *parser) parseMapType() *ast.MapType {
	kw := p.expect(scanner.KwMap, "map")
	m := &ast.MapType{Pos: kw.Pos}
	p.expect(scanner.LBrack, "[ after map")
	// the `?` prefix binds to the key exactly as it binds to a field type: what
	// may carry one is the CHECKER's business, so the grammar takes the
	// spelling and the diagnostic names the real problem — a key is an
	// identity and every entry has one (docs/SPEC-TABLES.md §2.8)
	keyOptional := false
	if p.kind() == scanner.Question {
		p.advance()
		keyOptional = true
	}
	m.Key = p.parseScalar()
	m.Key.Optional = keyOptional
	// a DEFAULT and a QUALIFICATION on the key, on the same terms as the `?`:
	// the grammar takes the spelling wherever a field's would stand, so the
	// diagnostic is the checker's sentence about clamping an identity rather
	// than a parse error about a bracket (docs/SPEC-TABLES.md §2.8, §11)
	if p.kind() == scanner.Assign {
		p.advance()
		m.KeyDefault = p.parseExpr()
	}
	if p.kind() == scanner.Pipe {
		m.KeyAttrs = p.parsePipeAttrs()
	}
	p.expect(scanner.RBrack, "] after a map key")
	value := &ast.Field{Name: "value", Pos: p.tok().Pos}
	if p.kind() == scanner.Question {
		p.advance()
		value.Type.Optional = true
	}
	if p.kind() == scanner.LBrack {
		value.Array = p.parseArrayBound()
		p.refuseArrayOfArrays()
	}
	if p.kind() == scanner.KwMap {
		optional := value.Type.Optional
		value.Map = p.parseMapType()
		value.Type.Optional = optional
	} else {
		optional := value.Type.Optional
		value.Type = p.parseScalar()
		value.Type.Optional = optional
	}
	m.Value = value
	return m
}

// refuseArrayOfArrays refuses a SECOND bracket where the element type stands
// — `[][]T`, `[][..N]T`, `[..N][]T` and `[N][]T` — by name, and then consumes
// the inner bound so the rest of the file's diagnostics still land. Arrays of
// arrays are not in v1 (SPEC §4.3), and the fix an unbounded array's element
// takes is a TABLE wrapper rather than a `type` wrapper, because a `type` body
// refuses a `[]T` (docs/SPEC-TABLES.md §2.9, §11).
func (p *parser) refuseArrayOfArrays() {
	if p.kind() != scanner.LBrack {
		return
	}
	p.errf(p.tok().Pos, "an array of arrays is not supported in v1 — wrap the inner array in a TABLE and declare an array of that table, which is the wrapper an unbounded array's element takes because a `type` body refuses a []T (SPEC §4.3, docs/SPEC-TABLES.md §2.9, §11)")
	p.parseArrayBound() // consumed so the rest of the file's diagnostics land
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
	case scanner.RBrack:
		// [] — an UNBOUNDED ARRAY (docs/SPEC-TABLES.md §2.9). The bracket is
		// the one every extent uses and an EMPTY bracket is the absence of an
		// extent, which is what the construct is.
		b.Kind = ast.ArrayList
	case scanner.DotDot:
		p.advance()
		if p.kind() == scanner.RBrack {
			// [..]T — refused by name (docs/SPEC-TABLES.md §2.9): `[..N]` is a
			// BOUND, so dropping its N reads as a bound someone failed to
			// finish rather than a bound nobody declared, and the grammar's own
			// Bound production has no such form
			p.errf(p.tok().Pos, "[..]T is not the unbounded array — `[..N]` is a BOUND, so dropping its N reads as a bound left unfinished rather than a bound nobody declared, and a count bound is a range LITERAL and never a truncated one; spell an unbounded array [] (docs/SPEC-TABLES.md §2.9, SPEC §4.2)")
			b.Kind = ast.ArrayList
			break
		}
		// [..N] — sugar for [0..N], reads "up to N" (SPEC §4.3)
		b.Kind = ast.ArrayUpTo
		b.Hi = p.parseExpr()
	default:
		first := p.parseExpr()
		if p.kind() == scanner.DotDot {
			p.advance()
			if p.kind() == scanner.RBrack {
				// [0..]T — refused by name (docs/SPEC-TABLES.md §2.9): it
				// states a minimum and hides the missing maximum behind it
				p.errf(p.tok().Pos, "[Min..]T is not the unbounded array — it states a minimum and hides the missing maximum behind it, and a count bound is a range LITERAL and never a truncated one; spell an unbounded array [], or complete the bound as [Min..N] (docs/SPEC-TABLES.md §2.9, SPEC §4.2)")
				b.Kind = ast.ArrayList
				break
			}
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
		// closed)
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
	case scanner.KwBits, scanner.KwString, scanner.KwWString, scanner.KwBytes:
		p.advance()
		p.expect(scanner.LParen, "(")
		arg := p.parseExpr()
		p.expect(scanner.RParen, ")")
		kind := ast.ScalarBits
		switch t.Kind {
		case scanner.KwString:
			kind = ast.ScalarString
		case scanner.KwWString:
			kind = ast.ScalarWString
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
	case scanner.Star:
		// `next *Node` — a POINTER to a table (docs/SPEC-TABLES.md). The C-like
		// spelling is deliberate: it reads as what it is. What may sit on the
		// right of the star is the CHECKER's business (a table, inside a table
		// body); the grammar accepts any name so the diagnostic names the real
		// problem instead of "expected a field type".
		p.advance()
		if k := p.kind(); k == scanner.KwBytes || k == scanner.KwString || k == scanner.KwWString {
			// `data *bytes`, `caption *string` — a BYTE BUFFER at its used
			// size (docs/SPEC-TABLES.md §2.5): a pointer to a blob node. It
			// takes no bound, because a buffer at its used size has none to
			// declare, so `*bytes(N)` is refused where the paren stands.
			word := p.advance()
			kind := ast.ScalarBytes
			switch k {
			case scanner.KwString:
				kind = ast.ScalarString
			case scanner.KwWString:
				kind = ast.ScalarWString
			}
			if p.kind() == scanner.LParen {
				p.errf(p.tok().Pos, "*%s takes no bound — a byte buffer is exactly the size it was given, and null when absent; write *%s, or bytes(N) for inline storage at a declared bound (docs/SPEC-TABLES.md §2.5)", word.Text, word.Text)
			}
			return ast.ScalarType{Kind: kind, Pointer: true, Pos: t.Pos}
		}
		name := p.expect(scanner.Ident, "the table a pointer targets")
		return ast.ScalarType{Kind: ast.ScalarNamed, Pointer: true, Name: name.Text, Pos: t.Pos}
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
// left for the caller. An empty section is a parse error. A valueless entry
// is a TAG, an identifier in an open namespace, and a RESERVED WORD is not an
// identifier (SPEC §4.1), so `| table` is refused with the word named rather
// than becoming a tag.
func (p *parser) parsePipeAttrs() []ast.Attr {
	var attrs []ast.Attr
	pipe := p.expect(scanner.Pipe, "|")
	refused := false
	if k := p.kind(); k == scanner.Int || k == scanner.Float || k == scanner.LParen || k == scanner.Minus {
		// `const X = 1 | 2`: | is never an operator — the language has no
		// bitwise-or. It opens the qualification section, whose entries are
		// identifiers (SPEC §4.2).
		p.errf(pipe.Pos, "| is never an operator — the language has no bitwise-or; | opens a qualification section, whose entries are identifiers (SPEC §4.2)")
		for p.kind() != scanner.Newline && p.kind() != scanner.RBrace && p.kind() != scanner.EOF {
			p.advance() // the terminator stays for the caller
		}
		return nil
	}
	for {
		if p.kind().IsKeyword() {
			word := p.advance()
			refused = true
			p.errf(word.Pos, "%s is a reserved word and cannot be a tag — a tag is an identifier, and reserved words are not identifiers (SPEC §4.1, §4.2)", word.Text)
			if p.kind() == scanner.Comma {
				p.advance()
				continue
			}
			break
		}
		if p.kind() != scanner.Ident {
			break
		}
		key := p.advance()
		a := ast.Attr{Key: key.Text, Pos: key.Pos}
		if p.kind() == scanner.Assign {
			p.advance()
			a.Value = p.parseExpr() // a bare word value (cpp_native = VMath) parses as an IdentExpr
		}
		attrs = append(attrs, a)
		if p.kind() == scanner.Comma {
			p.advance()
			continue
		}
		break
	}
	if len(attrs) == 0 && !refused {
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
		// E.Max / E.Count — contextual after '.' (SPEC §4.2)
		if p.kind() == scanner.Dot {
			dot := p.advance()
			m := p.expect(scanner.Ident, `"Max" or "Count"`)
			if m.Text != "Max" && m.Text != "Count" {
				p.errf(dot.Pos, "only .Max (enums and generated sets) and .Count (enums and flags) are legal after a name (found .%s)", m.Text)
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

// parseSetLit reads a FLAGS default, `{ Jump, Crouch }`, from the opening
// brace (SPEC §4.2): variant names separated by commas, a trailing comma
// allowed as in a variant list, and the closing brace on the same line. An
// empty list `{}` is the zero mask, exactly as `= 0` is on an integer.
func (p *parser) parseSetLit() *ast.SetLit {
	open := p.expect(scanner.LBrace, "{")
	lit := &ast.SetLit{Pos: open.Pos}
	for p.kind() == scanner.Ident {
		t := p.advance()
		lit.Names = append(lit.Names, ast.Name{Text: t.Text, Pos: t.Pos})
		if p.kind() != scanner.Comma {
			break
		}
		p.advance()
	}
	p.expect(scanner.RBrace, "}")
	return lit
}
