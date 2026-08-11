// Package ast holds the parsed form of *.schema files (SPEC §4.2).
package ast

import (
	"math/big"

	"github.com/mas-bandwidth/schema/internal/scanner"
)

type Pos = scanner.Pos

type File struct {
	Path    string // as given on the command line
	Base    string // basename without extension, e.g. "Constants"
	Package string
	PkgPos  Pos
	Decls   []Decl
}

// Decl is a top-level declaration.
type Decl interface {
	DeclName() string // "" for contexts
	DeclPos() Pos
}

type ConstDecl struct {
	Name string
	Pos  Pos
	Type string // "" for an untyped (kind-inferred) constant; else e.g. "uint64"
	Expr Expr
}

type EnumDecl struct {
	Name     string
	Pos      Pos
	Attrs    []Attr
	Variants []Name
}

type FlagsDecl struct {
	Name     string
	Pos      Pos
	Attrs    []Attr
	Variants []Name
}

type ContextsDecl struct {
	Pos   Pos
	Names []Name
}

type TypeDecl struct {
	Name    string
	Pos     Pos
	Attrs   []Attr // type TAGS — user-chosen, inert in v1 (SPEC §4.2)
	Body    *Block
	IsTable bool // declared with `table` instead of `type`: a reflection/
	// table-wire root — it and everything it references get table codecs
	// and field descriptors
}

type MessageDecl struct {
	Name string
	Pos  Pos
	Body *Block
}

type ObjectDecl struct {
	Name string
	Pos  Pos
	Body *Block
}

func (d *ConstDecl) DeclName() string    { return d.Name }
func (d *EnumDecl) DeclName() string     { return d.Name }
func (d *FlagsDecl) DeclName() string    { return d.Name }
func (d *ContextsDecl) DeclName() string { return "" }
func (d *TypeDecl) DeclName() string     { return d.Name }
func (d *MessageDecl) DeclName() string  { return d.Name }
func (d *ObjectDecl) DeclName() string   { return d.Name }

func (d *ConstDecl) DeclPos() Pos    { return d.Pos }
func (d *EnumDecl) DeclPos() Pos     { return d.Pos }
func (d *FlagsDecl) DeclPos() Pos    { return d.Pos }
func (d *ContextsDecl) DeclPos() Pos { return d.Pos }
func (d *TypeDecl) DeclPos() Pos     { return d.Pos }
func (d *MessageDecl) DeclPos() Pos  { return d.Pos }
func (d *ObjectDecl) DeclPos() Pos   { return d.Pos }

type Name struct {
	Text string
	Pos  Pos
}

// Block is a { ... } body.
type Block struct {
	Items []Item
}

type Item interface{ ItemPos() Pos }

type Field struct {
	Name    string
	Pos     Pos
	Array   *ArrayBound // nil if not an array
	Type    ScalarType
	Attrs   []Attr
	Default Expr // optional "= expr" after the attributes (zero init otherwise)
}

type ArrayKind int

const (
	ArrayFixed ArrayKind = iota // [N]T
	ArrayUpTo                   // [<= N]T
	ArrayRange                  // [Min..N]T
)

type ArrayBound struct {
	Kind ArrayKind
	Lo   Expr // ArrayRange only
	Hi   Expr
}

type ScalarKind int

const (
	ScalarInt ScalarKind = iota // Bits field width — 8/16/32/64/128 — Signed flag
	ScalarBits
	ScalarBool
	ScalarFloat32
	ScalarFloat64
	ScalarString
	ScalarBytes
	ScalarFixed // fixed(I, F) — signed fixed point (SPEC §4.3)
	ScalarNamed
)

type ScalarType struct {
	Kind   ScalarKind
	Signed bool   // ScalarInt
	Width  int    // ScalarInt: 8/16/32/64/128
	Arg    Expr   // ScalarBits/ScalarString/ScalarBytes: the (N); ScalarFixed: I
	Arg2   Expr   // ScalarFixed: F
	Name   string // ScalarNamed
	Pos    Pos
}

type ConstField struct {
	Pos   Pos
	Value Expr
	Bits  Expr
}

type ReservedItem struct {
	Pos  Pos
	Bits Expr
}

type AlignItem struct {
	Pos Pos
}

type IfItem struct {
	Pos  Pos
	Neg  bool
	Cond Name
	Then *Block
	Else *Block // nil if absent
}

func (f *Field) ItemPos() Pos        { return f.Pos }
func (c *ConstField) ItemPos() Pos   { return c.Pos }
func (r *ReservedItem) ItemPos() Pos { return r.Pos }
func (a *AlignItem) ItemPos() Pos    { return a.Pos }
func (i *IfItem) ItemPos() Pos       { return i.Pos }

// Attr is one entry of a trailing [ ... ] attribute list.
// Valueless: Value == nil. Word-valued (round = up): Value is *IdentExpr.
type Attr struct {
	Key   string
	Pos   Pos
	Value Expr
}

// Expressions (SPEC §4.2 IntExpr / FloatExpr).

type Expr interface{ ExprPos() Pos }

type IntLit struct {
	Pos   Pos
	Value *big.Int
	Text  string // source spelling (decimal / 0x / 0b)
}

type FloatLit struct {
	Pos   Pos
	Value float64
	Text  string
}

type IdentExpr struct {
	Pos  Pos
	Name string
}

// MaxExpr is an enum max reference: E.Max (SPEC §4.2).
type MaxExpr struct {
	Pos  Pos
	Enum string
}

type UnaryExpr struct {
	Pos Pos
	Op  string // "-"
	X   Expr
}

type BinaryExpr struct {
	Pos Pos
	Op  string // + - * / %
	X   Expr
	Y   Expr
}

type ParenExpr struct {
	Pos Pos
	X   Expr
}

func (e *IntLit) ExprPos() Pos     { return e.Pos }
func (e *FloatLit) ExprPos() Pos   { return e.Pos }
func (e *IdentExpr) ExprPos() Pos  { return e.Pos }
func (e *MaxExpr) ExprPos() Pos    { return e.Pos }
func (e *UnaryExpr) ExprPos() Pos  { return e.Pos }
func (e *BinaryExpr) ExprPos() Pos { return e.Pos }
func (e *ParenExpr) ExprPos() Pos  { return e.Pos }
