package c

// MaxBits/MaxBytes constants, the message dispatch surface, and the
// specified-default constructors.

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/internal/ir"
)

// emitMaxBits emits a type's worst-case wire size. Callers size their buffers
// from these, so their absence is not cosmetic — it is the difference between
// a caller sizing a buffer correctly and guessing.
func (g *gen) emitMaxBits(st *ir.Struct) {
	bits := ir.MaxBitsStruct(st)
	g.pf("#define %s_MAX_BITS %d   /* longest wire path; align pads at worst case (SPEC §6.1) */\n",
		screaming(st.Name), bits)
	g.pf("#define %s_MAX_BYTES %d  /* rounded up to the 8-byte write-buffer granularity */\n\n",
		screaming(st.Name), ir.MaxBytes(bits))
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
				g.pf("    {\n        int32_t i;\n        for ( i = 0; i < %d; i++ )\n        {\n", f.ArrayBound)
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
				g.pf("    value.%s = %s;\n", f.Name, g.int128Literal(f.DefInt))
				return
			}
			g.pf("    value.%s = %s;\n", f.Name, uint128Literal(f.DefInt))
			return
		}
		g.pf("    value.%s = %s;\n", f.Name, f.DefInt.String())
	}
}

// ---- message dispatch ----

// emitMessageTypes emits the MessageType tag enum. Only the file that owns the
// message surface emits it (ir.MessageOwner), so a unit has exactly one.
func (g *gen) emitMessageTypes() {
	g.pf("\n/* The message tag: the discriminant for a heterogeneous stream. None = 0 is\n")
	g.pf("   the stream terminator (SPEC §4.8). */\n")
	g.pf("typedef uint8_t MessageType;\n")
	g.pf("#define MESSAGE_TYPE_NONE 0\n")
	for i, m := range g.unit.Messages {
		g.pf("#define MESSAGE_TYPE_%s %d\n", screaming(m), i+1)
	}
	g.pf("#define MESSAGE_TYPE_MAX %d\n\n", len(g.unit.Messages))

	g.pf("/* Debug/log name for any MessageType value, out-of-set included. */\n")
	g.pf("static SCHEMA_UNUSED const char * enum_name_message_type( MessageType value )\n{\n")
	g.pf("    switch ( value )\n    {\n")
	g.pf("        case MESSAGE_TYPE_NONE: return \"None\";\n")
	for i, m := range g.unit.Messages {
		g.pf("        case %d: return %q;\n", i+1, m)
	}
	g.pf("        default: return \"???\";\n    }\n}\n\n")

	// the tagged union
	g.pf("/* The message union. C has no variant, so this is the tag plus a union of\n")
	g.pf("   the arms — the same shape the C++ target's default representation uses.\n")
	g.pf("   The selected arm is established ZEROED at selection (SPEC §5): read_message\n")
	g.pf("   zeroes before decoding. Bytes of unselected arms are indeterminate. */\n")
	g.pf("typedef struct Message {\n")
	g.pf("    MessageType type;\n")
	g.pf("    union {\n")
	for _, m := range g.unit.Messages {
		g.pf("        %s %s;\n", m, snake(m))
	}
	g.pf("    } as;\n")
	g.pf("} Message;\n\n")
}

func (g *gen) emitMessageDispatch() {
	g.pf("/* The tag itself, over [0, MESSAGE_TYPE_MAX]. */\n")
	g.pf("static SCHEMA_UNUSED int write_message_type( serialize_write_stream_t * stream, MessageType value )\n{\n")
	bits := ir.BitsRequired(bigZero(), bigInt64(int64(len(g.unit.Messages))))
	g.pf("    if ( value > MESSAGE_TYPE_MAX )\n    {\n        return 0;\n    }\n")
	g.call("    ", fmt.Sprintf("serialize_write_bits( stream, (serialize_uint32_t) value, %d )", bits))
	g.pf("    return 1;\n}\n\n")

	g.pf("static SCHEMA_UNUSED int read_message_type( serialize_read_stream_t * stream, MessageType * value )\n{\n")
	g.pf("    serialize_uint32_t raw = 0;\n")
	g.call("    ", fmt.Sprintf("serialize_read_bits( stream, &raw, %d )", bits))
	g.pf("    if ( raw > MESSAGE_TYPE_MAX )\n    {\n        return 0;\n    }\n")
	g.pf("    *value = (MessageType) raw;\n    return 1;\n}\n\n")

	g.pf("/* Writes the tag and then the selected arm. */\n")
	g.pf("static SCHEMA_UNUSED int write_message( serialize_write_stream_t * stream, const Message * message )\n{\n")
	g.pf("    switch ( message->type )\n    {\n")
	g.pf("        case MESSAGE_TYPE_NONE:\n")
	g.pf("            return write_message_type( stream, MESSAGE_TYPE_NONE ); /* the stream terminator */\n")
	for _, m := range g.unit.Messages {
		g.pf("        case MESSAGE_TYPE_%s:\n", screaming(m))
		g.pf("            if ( !write_message_type( stream, MESSAGE_TYPE_%s ) )\n            {\n                return 0;\n            }\n", screaming(m))
		g.pf("            return write_%s( stream, &message->as.%s );\n", snake(m), snake(m))
	}
	g.pf("        default:\n            return 0;\n    }\n}\n\n")

	g.pf("/* Reads the tag, ZEROES the selected arm, then decodes into it (SPEC §5). */\n")
	g.pf("static SCHEMA_UNUSED int read_message( serialize_read_stream_t * stream, Message * message )\n{\n")
	g.pf("    MessageType type = MESSAGE_TYPE_NONE;\n")
	g.call("    ", "read_message_type( stream, &type )")
	g.pf("    message->type = type;\n")
	g.pf("    switch ( type )\n    {\n")
	g.pf("        case MESSAGE_TYPE_NONE:\n            return 1;\n")
	for _, m := range g.unit.Messages {
		g.pf("        case MESSAGE_TYPE_%s:\n", screaming(m))
		g.pf("            memset( &message->as.%s, 0, sizeof( message->as.%s ) );\n", snake(m), snake(m))
		g.pf("            return read_%s( stream, &message->as.%s );\n", snake(m), snake(m))
	}
	g.pf("        default:\n            return 0;\n    }\n}\n\n")
}

func bigZero() *big.Int          { return big.NewInt(0) }
func bigInt64(v int64) *big.Int  { return big.NewInt(v) }


// uint128Literal renders an unsigned 128-bit default from its two lanes.
func uint128Literal(v *big.Int) string {
	mod := new(big.Int).Lsh(big.NewInt(1), 128)
	u := new(big.Int).Mod(v, mod)
	lo := new(big.Int).And(u, new(big.Int).SetUint64(^uint64(0)))
	hi := new(big.Int).Rsh(u, 64)
	return fmt.Sprintf("serialize_uint128_make( %sULL, %sULL )", hi.String(), lo.String())
}
