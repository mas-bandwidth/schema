package elixirtable

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE PREFILL, as a compile-time constant of the type (docs/SPEC-TABLES.md
// §3.4): the DECLARED DEFAULTS, laid out as RECORD BYTES.
//
// "THE PREFILL IS WHAT ANSWERS 'ABSENT FIELD'." A field this record does not
// carry has no plan entry, so the assembler splices these bytes over the gap
// and the loop never learns the field existed. In this port that makes the
// prefill a BINARY rather than a `Reset()` call, for the reason Rust's is a
// defaults image: the value surface here has no by-value reset to call, and a
// constant run of bytes is both cheaper and exactly the same answer.
//
// It is also the WRITE's template. Where the C++ reference memcpy's a constant
// template of the hash and zeros and then stores over it, an Elixir writer
// builds the record in one construction whose slack segments are literal
// zeros — the same bytes, and nothing to memset.

// fixedDefaultsImage is C(T) bytes of this type's declared defaults.
func fixedDefaultsImage(st *ir.Struct) []byte {
	out := make([]byte, 0, fixedTypeBytes(st))
	for _, f := range st.Fields {
		out = append(out, fixedFieldDefault(f)...)
	}
	return out
}

func fixedFieldDefault(f *ir.Field) []byte {
	out := make([]byte, 0, fixedFieldBytes(f))
	if f.Type.Optional {
		// A FRESH VALUE'S OPTIONAL IS ABSENT, and its payload rides WHOLE
		// behind the flag whether or not it is present (§3.4).
		out = append(out, 0)
	}
	switch {
	case f.KeyEnum != "":
		out = append(out, fixedRepeat(fixedElementDefault(f), f.KeyEnumRef.Max)...)
	case f.Array == ir.ArrayFixed:
		out = append(out, fixedRepeat(fixedElementDefault(f), f.ArrayBound)...)
	case f.Array == ir.ArrayCounted:
		// A [A..B] COUNT IS BORN AT A, the one wire-legal count a fresh value
		// can carry (SPEC §4.6), which is the count the value surface's own
		// construction default carries too.
		out = appendFixedU32(out, uint32(f.BornCount()))
		out = append(out, fixedRepeat(fixedElementDefault(f), f.ArrayBound)...)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		out = appendFixedU32(out, uint32(len(f.DefBytes)))
		out = append(out, fixedPadTo(f.DefBytes, f.Type.Size)...)
	case f.Type.Kind == ir.TWString:
		out = appendFixedU32(out, uint32(len(f.DefBytes)/2))
		out = append(out, fixedPadTo(f.DefBytes, 2*f.Type.Size)...)
	default:
		out = append(out, fixedElementDefault(f)...)
	}
	return out
}

func fixedRepeat(one []byte, n int64) []byte {
	out := make([]byte, 0, int64(len(one))*n)
	for range n {
		out = append(out, one...)
	}
	return out
}

func fixedPadTo(b []byte, n int64) []byte {
	out := make([]byte, n)
	copy(out, b)
	return out
}

func appendFixedU32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

// fixedElementDefault is ONE element's default bytes.
func fixedElementDefault(f *ir.Field) []byte {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedDefaultsImage(r)
		case *ir.Union:
			// THE ARMS OVERWRITE EACH OTHER, IN DECLARED ORDER, AT THEIR ONE
			// OVERLAY STORAGE, AND THE TAG LANDS None LAST
			// (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9 #38). Zeroing the union
			// whole is a DIFFERENT ANSWER: it makes every arm's declared
			// default zero for every field the plan leaves alone, silently —
			// which is exactly what an appended field inside an arm is.
			out := make([]byte, fixedElementBytes(f))
			tagBytes := ir.TableFixedUnionTagBytes(r)
			for _, v := range r.Variants {
				if v.F == nil {
					continue // a payload-free arm has no storage to land
				}
				copy(out[tagBytes:], fixedFieldDefault(v.F))
			}
			// THE TAG IS None AND IT IS WRITTEN LAST, over whatever an arm's
			// overlay would have reached into it.
			for i := int64(0); i < tagBytes; i++ {
				out[i] = 0
			}
			return out
		case *ir.Enum:
			return fixedLeafBytes(fixedEnumDefault(f, r), int64(r.StorageBits/8))
		case *ir.Flags:
			v := big.NewInt(0)
			if f.HasDefault && f.DefInt != nil {
				v = f.DefInt
			}
			return fixedLeafBytes(v, 8)
		}
	}
	width := ir.TableFixedStorageBytes(f.Type)
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return []byte{1}
		}
		return []byte{0}
	case ir.TFloat32:
		bits := math.Float32bits(0)
		if f.HasDefault {
			bits = math.Float32bits(float32(f.DefFloat))
		}
		return fixedLeafBytes(new(big.Int).SetUint64(uint64(bits)), 4)
	case ir.TFloat64:
		bits := math.Float64bits(0)
		if f.HasDefault {
			bits = math.Float64bits(f.DefFloat)
		}
		return fixedLeafBytes(new(big.Int).SetUint64(bits), 8)
	}
	v := big.NewInt(0)
	if f.HasDefault && f.DefInt != nil {
		v = f.DefInt
	}
	return fixedLeafBytes(v, width)
}

// fixedEnumDefault is the ORDINAL a defaulted enum field lands at: the
// variant's POSITION IN THE LAYOUT, from 1, and 0 is None (§3.4).
func fixedEnumDefault(f *ir.Field, e *ir.Enum) *big.Int {
	if f.HasDefault && f.DefVariant != "" {
		for i, name := range e.Variants {
			if name == f.DefVariant {
				return big.NewInt(int64(i + 1))
			}
		}
	}
	if f.HasDefault && f.DefInt != nil {
		return f.DefInt
	}
	return big.NewInt(0)
}

// fixedLeafBytes is a value's LITTLE-ENDIAN image at its declared width, two's
// complement for a negative one.
func fixedLeafBytes(v *big.Int, width int64) []byte {
	if width <= 0 {
		return nil
	}
	m := new(big.Int).Lsh(big.NewInt(1), uint(width*8))
	n := new(big.Int).Mod(v, m)
	out := make([]byte, width)
	bs := n.Bytes()
	for i, b := range bs {
		out[len(bs)-1-i] = b
	}
	return out
}

// fixedBinaryLiteral spells a run of bytes as an Elixir binary.
//
// IT IS A STRING OF `+"`"+`\\xNN`+"`"+` ESCAPES AND NOT A `+"`"+`<<0x08, ...>>`+"`"+` LITERAL, and the
// reason is the formatter rather than the language: `+"`"+`mix format`+"`"+` breaks a
// bitstring that outgrows the line ONE SEGMENT PER LINE, which would spell a
// layout of thirteen hundred bytes over thirteen hundred lines. A string
// literal it leaves exactly as written, and the two are the same binary.
func fixedBinaryLiteral(b []byte, _ int) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, v := range b {
		fmt.Fprintf(&sb, "\\x%02X", v)
	}
	sb.WriteByte('"')
	return sb.String()
}

// ---------------------------------------------------------------------------
// THE VALUE SURFACE's construction defaults
// ---------------------------------------------------------------------------

// fixedElixirDefault is a field's construction default — the specified default
// where declared, else SPEC §5's zero form, always folded to a literal because
// defstruct defaults evaluate at compile time.
func (g *fixedGen) fixedElixirDefault(f *ir.Field) string {
	switch {
	case f.KeyEnum != "":
		return fmt.Sprintf("List.duplicate(%s, %d)", g.fixedElemDefaultTerm(f), f.KeyEnumRef.Max)
	case f.Array == ir.ArrayFixed:
		return fmt.Sprintf("List.duplicate(%s, %d)", g.fixedElemDefaultTerm(f), f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		if n := f.BornCount(); n > 0 {
			return fmt.Sprintf("List.duplicate(%s, %d)", g.fixedElemDefaultTerm(f), n)
		}
		return "[]"
	}
	return g.fixedElemDefaultTerm(f)
}

func (g *fixedGen) fixedElemDefaultTerm(f *ir.Field) string {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fmt.Sprintf("%%%s.%s{}", g.ns, r.Name)
		case *ir.Union:
			return fmt.Sprintf("%%%s.%s{}", g.ns, r.Name)
		case *ir.Enum:
			return fixedEnumDefault(f, r).String()
		case *ir.Flags:
			if f.HasDefault && f.DefInt != nil {
				return f.DefInt.String()
			}
			return "0"
		}
	}
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TString, ir.TBytes, ir.TWString:
		if len(f.DefBytes) == 0 {
			return "<<>>"
		}
		return fixedBinaryLiteral(f.DefBytes, 0)
	case ir.TFloat32, ir.TFloat64:
		if f.HasDefault {
			return fixedFloatLiteral(f.DefFloat)
		}
		return "0.0"
	}
	if f.HasDefault && f.DefInt != nil {
		return fixedIntLiteral(f.DefInt)
	}
	return "0"
}

// fixedFloatLiteral spells a float the way `mix format` does — with a decimal
// point, and never in a form the parser would read as an integer.
func fixedFloatLiteral(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// fixedIntLiteral spells an integer the way `mix format` does: underscores
// every three digits past four digits, which is what the packet emitter's own
// literals carry.
func fixedIntLiteral(v *big.Int) string {
	s := v.String()
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	if len(s) <= 4 {
		if neg {
			return "-" + s
		}
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	out := strings.Join(parts, "_")
	if neg {
		return "-" + out
	}
	return out
}
