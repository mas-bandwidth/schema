// Package ast holds the parsed form of *.schema files (SPEC §4.2).
package ast

import (
	"math/big"

	"github.com/mas-bandwidth/schema/v2/internal/scanner"
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
	DeclName() string
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

type TypeDecl struct {
	Name  string
	Pos   Pos
	Attrs []Attr // type TAGS — user-chosen, inert in v1 (SPEC §4.2)
	Body  *Block
}

// TableDecl is a `table` declaration: a data type on the evolution-tolerant
// TABLE wire (docs/SPEC-TABLES.md) rather than the packet wire. The body grammar
// is the type body's; the qualification carries tags and the `was` rename
// (docs/SPEC-TABLES.md §5).
type TableDecl struct {
	Name  string
	Pos   Pos
	Attrs []Attr
	Body  *Block
}

// UnionDecl is a first-class one-of type (SPEC §4.8): an implicit None row,
// then each arm as a FIELD LINE. The tag enum <Name>Type is generated, never
// declared.
type UnionDecl struct {
	Name     string
	Pos      Pos
	Variants []UnionVariant
}

// UnionVariant is one arm. An arm IS a field line (SPEC §4.8,
// docs/SPEC-TABLES.md §2.6), so Arm carries the whole of it — the type, the
// array bound, the attributes — parsed by the field production. Type is the
// arm's payload type NAME when the arm names a declaration by itself (no
// pointer, no array), "" otherwise: the spellings that predate general arms
// read it, and the checker resolves every arm through Arm.
type UnionVariant struct {
	Name    string // field-style lower_snake, unique within the union
	Pos     Pos
	Type    string // the payload type name, when the arm names a bare declaration
	TypePos Pos
	Arm     *Field
}

func (d *ConstDecl) DeclName() string { return d.Name }
func (d *EnumDecl) DeclName() string  { return d.Name }
func (d *FlagsDecl) DeclName() string { return d.Name }
func (d *TypeDecl) DeclName() string  { return d.Name }
func (d *TableDecl) DeclName() string { return d.Name }
func (d *UnionDecl) DeclName() string { return d.Name }

func (d *ConstDecl) DeclPos() Pos { return d.Pos }
func (d *EnumDecl) DeclPos() Pos  { return d.Pos }
func (d *FlagsDecl) DeclPos() Pos { return d.Pos }
func (d *TypeDecl) DeclPos() Pos  { return d.Pos }
func (d *TableDecl) DeclPos() Pos { return d.Pos }
func (d *UnionDecl) DeclPos() Pos { return d.Pos }

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

	// Map is the `map[K]V` spelling (docs/SPEC-TABLES.md §2.8): a lookup over
	// entries the wire carries as a sorted array of one generated
	// `{ key, value }` table. Set instead of Type, which the checker leaves at
	// its zero value on a map field. Table bodies only; the checker refuses
	// every other placement, every key that is not a bounded string or an
	// integer, and every bound or attribute on the map itself, by name.
	Map *MapType
}

// MapType is a `map[K]V` field's two halves (docs/SPEC-TABLES.md §2.8). The
// VALUE is a whole field spelling rather than a scalar type, so a map of
// arrays, of optionals and of maps are one production and the checker resolves
// a value with the same code that resolves any field: Value.Name is always
// "value", and it carries no attributes and no default because the grammar
// gives a value nowhere to spell one.
type MapType struct {
	Pos   Pos
	Key   ScalarType
	Value *Field

	// KeyAttrs and KeyDefault are a qualification and a default written ON
	// THE KEY — `map[uint32 | max = 4]int32`, `map[uint32 = 5]int32`. The
	// grammar takes them so the CHECKER can refuse them by name: a key is an
	// identity, and clamping an identity merges two entries
	// (docs/SPEC-TABLES.md §2.8, §11). Nothing downstream of the checker ever
	// sees a key that carries one.
	KeyAttrs   []Attr
	KeyDefault Expr
}

type ArrayKind int

const (
	ArrayFixed ArrayKind = iota // [N]T
	ArrayUpTo                   // [..N]T — sugar for [0..N] (SPEC §4.3)
	ArrayRange                  // [Min..N]T
	// ArrayList is `[]T` — an UNBOUNDED ARRAY, a counted array whose count
	// the data decides (docs/SPEC-TABLES.md §2.9). The bracket carries no
	// expression, so Lo and Hi are both nil.
	ArrayList
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
	ScalarWString // wstring(N) — UTF-16 code units (SPEC §4.12)
	ScalarBytes
	ScalarFixed // fixed(I, F) / ufixed(I, F) — fixed point, Signed flag (SPEC §4.3)
	ScalarNamed
)

type ScalarType struct {
	Kind   ScalarKind
	Signed bool // ScalarInt; ScalarFixed (fixed = true, ufixed = false)
	Width  int  // ScalarInt: 8/16/32/64/128
	Arg    Expr // ScalarBits/ScalarString/ScalarWString/ScalarBytes: the (N); ScalarFixed: I
	Arg2   Expr // ScalarFixed: F
	// Pointer marks the `*T` spelling (ScalarNamed only): a POINTER to a
	// table rather than a by-value nesting (docs/SPEC-TABLES.md). Types remain
	// value semantics; tables allow pointer semantics.
	Pointer bool
	// Optional marks the `?T` spelling: an OPTIONAL by-value field, present
	// or absent, with a generated presence companion beside the value
	// (docs/SPEC-TABLES.md §2.3). Table bodies only; the checker refuses every
	// other placement by name.
	Optional bool
	Name     string // ScalarNamed
	Pos      Pos
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

// StringLit is a "..." literal — legal only as an attribute value (SPEC §4.2).
type StringLit struct {
	Pos   Pos
	Value string // without the quotes
}

// SetLit is a FLAGS default, `= { Jump, Crouch }` (SPEC §4.2): the variant
// names whose bits the fresh mask holds. It stands only where a field default
// stands, and the checker resolves every name against the field's own flags
// declaration.
type SetLit struct {
	Pos   Pos
	Names []Name
}

// MaxExpr is a set reference: E.Max, the extent of an enum or a generated
// set, and E.Count, the declared variant count of an enum or a flags
// declaration (SPEC §4.2). Sel is "Max" or "Count".
type MaxExpr struct {
	Pos  Pos
	Enum string
	Sel  string
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
func (e *StringLit) ExprPos() Pos  { return e.Pos }
func (e *SetLit) ExprPos() Pos     { return e.Pos }
func (e *FloatLit) ExprPos() Pos   { return e.Pos }
func (e *IdentExpr) ExprPos() Pos  { return e.Pos }
func (e *MaxExpr) ExprPos() Pos    { return e.Pos }
func (e *UnaryExpr) ExprPos() Pos  { return e.Pos }
func (e *BinaryExpr) ExprPos() Pos { return e.Pos }
func (e *ParenExpr) ExprPos() Pos  { return e.Pos }
