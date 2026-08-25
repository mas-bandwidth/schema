package c

// MaxBits/MaxBytes constants and the specified-default constructors.

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitMaxBits emits a type's worst-case wire size. Callers size their buffers
// from these, so their absence is not cosmetic — it is the difference between
// a caller sizing a buffer correctly and guessing. The MAX_BYTES comment
// carries the read half of serialize.c's buffer contract (the
// align-up ruling): the allocation backing a read buffer must extend at
// least 8 bytes past the data, because the reader loads unconditional
// 64-bit windows — a caller sizing a receive allocation at exactly
// MAX_BYTES would be handing the reader undefined behavior.
func (g *gen) emitMaxBits(st *ir.Struct) {
	bits := ir.MaxBitsStruct(st)
	g.pf("#define %s_MAX_BITS %d   /* longest wire path; align pads at worst case (SPEC §6.1) */\n",
		screaming(st.Name), bits)
	g.pf("#define %s_MAX_BYTES %d  %s\n\n",
		screaming(st.Name), ir.MaxBytes(bits), g.maxBytesTail())
}

// ---- specified defaults ----

// structHasDefaults reports whether a type (or a type it composes) carries a
// specified default, which is what makes a new_X() constructor worth emitting.
func structHasDefaults(st *ir.Struct) bool {
	seen := map[string]bool{}
	var walk func(*ir.Struct) bool
	walk = func(s *ir.Struct) bool {
		if seen[s.Name] {
			return false
		}
		seen[s.Name] = true
		for _, f := range s.Fields {
			if f.HasDefault {
				return true
			}
			if f.Type.Kind == ir.TNamed {
				if inner, ok := f.Type.Ref.(*ir.Struct); ok && walk(inner) {
					return true
				}
			}
		}
		return false
	}
	return walk(st)
}

// emitConstructor emits new_X(), which applies the SPECIFIED defaults. The
// all-zero form is the schema's default (SPEC §4.2), so a type with no
// specified defaults needs no constructor — memset is already correct.
func (g *gen) emitConstructor(st *ir.Struct) {
	if !structHasDefaults(st) {
		return
	}
	g.pf("/* Returns a %s with its SPECIFIED defaults applied. A memset to zero is\n", st.Name)
	g.pf("   the schema's own default (SPEC §4.2: zero initialization unless a\n")
	g.pf("   specified default overrides it), so only types carrying one get this. */\n")
	g.pf("static SCHEMA_UNUSED %s new_%s( void )\n{\n", st.Name, snake(st.Name))
	g.pf("    %s value;\n", st.Name)
	g.pf("    memset( &value, 0, sizeof( value ) );\n")
	for _, f := range st.Fields {
		g.emitDefaultInit(f)
	}
	g.pf("    return value;\n}\n\n")
}

func (g *gen) emitDefaultInit(f *ir.Field) {
	if f.Type.Kind == ir.TNamed {
		if inner, ok := f.Type.Ref.(*ir.Struct); ok && structHasDefaults(inner) {
			if f.Array != ir.ArrayNone {
				// an array of a defaulted type: every element carries them, and
				// C cannot assign to an array
				g.pf("    {\n        int32_t i;\n        for ( i = 0; i < %s; i++ )\n        {\n", g.renderInt(f.ArrayExpr, big.NewInt(f.ArrayBound)))
				g.pf("            value.%s[i] = new_%s();\n        }\n    }\n", f.Name, snake(f.Type.Name))
				return
			}
			g.pf("    value.%s = new_%s();\n", f.Name, snake(f.Type.Name))
			return
		}
	}
	if f.Array != ir.ArrayNone && f.HasDefault {
		g.unsupported("field %s is an array with a specified default, which has no C emission", f.Name)
		return
	}
	if !f.HasDefault {
		return
	}
	switch {
	case f.Type.Kind == ir.TBool:
		if f.DefBool {
			g.pf("    value.%s = 1;\n", f.Name)
		}
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		g.pf("    value.%s = %s;\n", f.Name, formatFloat(f.DefFloat))
	case f.Type.Kind == ir.TNamed && f.DefVariant != "":
		g.pf("    value.%s = %s_%s;\n", f.Name, screaming(f.Type.Name), screaming(f.DefVariant))
	case f.DefInt != nil:
		// C has no 128-bit literal, so a 128-bit default is built from its
		// lanes; TFixed carries its own Signed since ufixed landed, so the
		// storage's signedness alone picks the literal family
		if f.Type.Width == 128 {
			if f.Type.Signed {
				g.pf("    value.%s = %s;\n", f.Name, g.int128Literal(f.DefExpr, f.DefInt))
				return
			}
			g.pf("    value.%s = %s;\n", f.Name, uint128Literal(f.DefInt))
			return
		}
		// f.DefExpr is nil for a fixed field — the checker never hands the
		// whole-units expression out as the raw initializer
		g.pf("    value.%s = %s;\n", f.Name, g.renderInt(f.DefExpr, f.DefInt))
	}
}

// uint128Literal renders an unsigned 128-bit default from its two lanes.
func uint128Literal(v *big.Int) string {
	mod := new(big.Int).Lsh(big.NewInt(1), 128)
	u := new(big.Int).Mod(v, mod)
	lo := new(big.Int).And(u, new(big.Int).SetUint64(^uint64(0)))
	hi := new(big.Int).Rsh(u, 64)
	return fmt.Sprintf("serialize_uint128_make( %sULL, %sULL )", hi.String(), lo.String())
}
