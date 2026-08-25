// Tests for the public expression-rendering surface: an external
// generator holding a checked unit must be able to render every kept bound,
// size, and default expression as the author's own source spelling — the
// same capability the built-in backends use for symbolic emission. The
// corpus goes in as schema text and comes out through the public IR only,
// the way an external consumer sees it.
package ir_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/parser"
	"github.com/mas-bandwidth/schema/ir"
)

const exprCorpus = `package exprtest

const MaxObjects = 256
const MaxUnits = 1024

// every operator, literal spelling, and nesting the grammar allows
const HalfObjects = MaxObjects / 2
const Remainder = MaxObjects % 5
const Area = MaxObjects * MaxObjects
const Grouped = (MaxObjects + 1) * 2
const Negated = -MaxUnits
const DoubleNeg = --3
const DoubleNegIdent = --MaxUnits

// enum max references — no twin in any generated target
enum Team { Red, Blue, Green }
const NumTeams = Team.Max
const TeamsPlus = Team.Max + 1

type Bounds {
    object_id int32 | min = 0, max = MaxObjects - 1
    delta     int32 | min = -MaxUnits, max = MaxUnits
    hexed     int32 | min = 0, max = 0x10
    counted   [..MaxObjects]uint8
    name      string(MaxUnits)
    retries   int32 = HalfObjects | min = 0, max = MaxObjects
}
`

func loadExprCorpus(t *testing.T) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Expr.schema", []byte(exprCorpus))
	if len(perrs) > 0 {
		t.Fatalf("test corpus does not parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  "Expr.schema",
		Name:  "Expr.schema",
		Base:  "Expr",
		Bytes: []byte(exprCorpus),
		AST:   f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("test corpus does not check: %v", cerrs[0])
	}
	return u
}

// TestRenderConstExprs pins the rendering of every constant declaration's
// kept expression: the neutral schema form, the target form under the nil
// (keep-the-spelling) and RustConstName identifier hooks, the referenced
// constant names, and the enum-max fact.
func TestRenderConstExprs(t *testing.T) {
	u := loadExprCorpus(t)
	tests := []struct {
		name      string   // const declaration
		neutral   string   // RenderExpr
		ident     string   // RenderExprIdent with nil (schema spelling)
		screaming string   // RenderExprIdent with ir.RustConstName
		consts    []string // ExprConsts, source order, per occurrence
		hasMax    bool     // ExprHasEnumMax
	}{
		{"MaxObjects", "256", "256", "256", nil, false},
		{"HalfObjects", "MaxObjects / 2", "MaxObjects / 2", "MAX_OBJECTS / 2", []string{"MaxObjects"}, false},
		{"Remainder", "MaxObjects % 5", "MaxObjects % 5", "MAX_OBJECTS % 5", []string{"MaxObjects"}, false},
		{"Area", "MaxObjects * MaxObjects", "MaxObjects * MaxObjects", "MAX_OBJECTS * MAX_OBJECTS", []string{"MaxObjects", "MaxObjects"}, false},
		{"Grouped", "(MaxObjects + 1) * 2", "(MaxObjects + 1) * 2", "(MAX_OBJECTS + 1) * 2", []string{"MaxObjects"}, false},
		{"Negated", "-MaxUnits", "-MaxUnits", "-MAX_UNITS", []string{"MaxUnits"}, false},
		// the neutral form keeps the author's doubled minus (it is a comment
		// spelling); the target form parenthesizes, since "--" is a decrement
		// operator in the C family and a parse error in Go and Rust
		{"DoubleNeg", "--3", "-(-3)", "-(-3)", nil, false},
		{"DoubleNegIdent", "--MaxUnits", "-(-MaxUnits)", "-(-MAX_UNITS)", []string{"MaxUnits"}, false},
		// E.Max renders neutrally for comments and refuses a target form
		{"NumTeams", "Team.Max", "?", "?", nil, true},
		{"TeamsPlus", "Team.Max + 1", "? + 1", "? + 1", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, ok := u.Consts[tt.name]
			if !ok {
				t.Fatalf("const %s missing from the unit", tt.name)
			}
			if got := ir.RenderExpr(c.Expr); got != tt.neutral {
				t.Errorf("RenderExpr = %q, want %q", got, tt.neutral)
			}
			if got := ir.RenderExprIdent(c.Expr, nil); got != tt.ident {
				t.Errorf("RenderExprIdent(nil) = %q, want %q", got, tt.ident)
			}
			if got := ir.RenderExprIdent(c.Expr, ir.RustConstName); got != tt.screaming {
				t.Errorf("RenderExprIdent(RustConstName) = %q, want %q", got, tt.screaming)
			}
			got := ir.ExprConsts(c.Expr)
			if len(got) != len(tt.consts) {
				t.Errorf("ExprConsts = %v, want %v", got, tt.consts)
			} else {
				for i := range got {
					if got[i] != tt.consts[i] {
						t.Errorf("ExprConsts = %v, want %v", got, tt.consts)
						break
					}
				}
			}
			if got := ir.ExprHasEnumMax(c.Expr); got != tt.hasMax {
				t.Errorf("ExprHasEnumMax = %v, want %v", got, tt.hasMax)
			}
		})
	}
}

// TestRenderFieldExprs proves every kept expression slot on a field renders
// the author's spelling: range bounds, the array bound, the string size, and
// the specified default.
func TestRenderFieldExprs(t *testing.T) {
	u := loadExprCorpus(t)
	st, ok := u.Structs["Bounds"]
	if !ok {
		t.Fatal("type Bounds missing from the unit")
	}
	fields := map[string]*ir.Field{}
	for _, f := range st.Fields {
		fields[f.Name] = f
	}
	tests := []struct {
		field string
		expr  func(f *ir.Field) ir.Expr
		want  string
	}{
		{"object_id", func(f *ir.Field) ir.Expr { return f.IntMaxExpr }, "MaxObjects - 1"},
		{"object_id", func(f *ir.Field) ir.Expr { return f.IntMinExpr }, "0"},
		{"delta", func(f *ir.Field) ir.Expr { return f.IntMinExpr }, "-MaxUnits"},
		{"hexed", func(f *ir.Field) ir.Expr { return f.IntMaxExpr }, "0x10"}, // source spelling survives
		{"counted", func(f *ir.Field) ir.Expr { return f.ArrayExpr }, "MaxObjects"},
		{"name", func(f *ir.Field) ir.Expr { return f.Type.SizeExpr }, "MaxUnits"},
		{"retries", func(f *ir.Field) ir.Expr { return f.DefExpr }, "HalfObjects"},
	}
	for _, tt := range tests {
		f, ok := fields[tt.field]
		if !ok {
			t.Errorf("field %s missing from Bounds", tt.field)
			continue
		}
		e := tt.expr(f)
		if e == nil {
			t.Errorf("%s: kept expression is nil, want %q", tt.field, tt.want)
			continue
		}
		if got := ir.RenderExpr(e); got != tt.want {
			t.Errorf("%s: RenderExpr = %q, want %q", tt.field, got, tt.want)
		}
	}
}

// TestRenderExprHostile pins the behavior on inputs no checked unit hands
// out: nil renders as the "?" poison — a spelling no target compiles — and
// the fact functions answer without panicking.
func TestRenderExprHostile(t *testing.T) {
	if got := ir.RenderExpr(nil); got != "?" {
		t.Errorf("RenderExpr(nil) = %q, want %q", got, "?")
	}
	if got := ir.RenderExprIdent(nil, nil); got != "?" {
		t.Errorf("RenderExprIdent(nil, nil) = %q, want %q", got, "?")
	}
	if got := ir.ExprConsts(nil); got != nil {
		t.Errorf("ExprConsts(nil) = %v, want nil", got)
	}
	if ir.ExprHasEnumMax(nil) {
		t.Error("ExprHasEnumMax(nil) = true, want false")
	}
}
