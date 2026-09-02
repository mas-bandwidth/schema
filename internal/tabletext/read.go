// The text form's READ half (SPEC-TABLES.md §16.2): one JSON text into one
// instance, over the IR. Every rule here is §16's and mirrors the generated
// C++ walk — unknown keys skipped and counted, duplicates last-wins and
// counted, a wrong JSON type skipped rather than coerced, numbers clamped at
// the declared bounds and then at the storage width, strings clamped at a code
// point boundary, trailing commas accepted, comments refused.
package tabletext

import (
	"math"
	"math/big"
	"strconv"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The caps the generated walk carries, mirrored so a text one side refuses is
// refused by the other.
const (
	maxJsonDepth  = 128
	maxJsonKey    = 255
	maxJsonNumber = 511
)

type reader struct {
	text   []byte
	pos    int
	report *Report
	bad    bool // the text is not JSON: the walk stops and keeps what it placed
	m      *Model
}

// Read fills one instance from one JSON text (SPEC-TABLES.md §16.1). The
// instance holds what was placed before any stop; false means the text is not
// JSON or a value could not be placed at all, and report.Malformed says so.
func (m *Model) Read(inst *Instance, text []byte, report *Report) bool {
	in := &reader{text: text, report: report, m: m}
	ok := in.readTable(inst, 0)
	if ok {
		in.space()
		if in.pos != len(in.text) {
			in.bad = true // trailing rubbish is not one text
		}
	}
	if in.bad || !ok {
		report.Malformed = true
		return false
	}
	return true
}

func (in *reader) space() {
	for in.pos < len(in.text) {
		c := in.text[in.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			in.pos++
			continue
		}
		// comments are not JSON, and a walk that guessed at one would be
		// reading a dialect nobody wrote down (SPEC-TABLES.md §16.2)
		if c == '/' {
			in.bad = true
		}
		return
	}
}

func (in *reader) peek() byte {
	in.space()
	if in.pos < len(in.text) {
		return in.text[in.pos]
	}
	return 0
}

// shape classifies the value sitting at the cursor without consuming it.
func (in *reader) valueShape() byte {
	switch c := in.peek(); c {
	case '{':
		return 'o'
	case '[':
		return 'a'
	case '"':
		return 's'
	case 't', 'f':
		return 'b'
	case 'n':
		return 'z'
	case 0:
		return 0
	default:
		return 'n'
	}
}

// Shape is the JSON shape a field's declaration takes, the classifier the read
// side matches a value against before placing it (SPEC-TABLES.md §16.2). A
// `?T` takes T's shape — presence is the KEY's, never the value's — and an
// enum-keyed array is an OBJECT keyed by variant name (§2.4).
func Shape(f *ir.Field) byte {
	switch {
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		return 's'
	case f.KeyEnum != "":
		return 'o'
	case f.Array != ir.ArrayNone:
		return 'a'
	case UnionOf(f) != nil:
		return 'o'
	case StructOf(f) != nil:
		return 'o'
	case EnumOf(f) != nil:
		return 's'
	case FlagsOf(f) != nil:
		return 'a'
	case f.Type.Kind == ir.TBool:
		return 'b'
	}
	return 'n'
}

// ElementShape is the same classifier one level down, for an array's elements.
func ElementShape(f *ir.Field) byte {
	switch {
	case StructOf(f) != nil:
		return 'o'
	case EnumOf(f) != nil:
		return 's'
	case FlagsOf(f) != nil:
		return 'a'
	case f.Type.Kind == ir.TBool:
		return 'b'
	}
	return 'n'
}

func (in *reader) literal(word string) bool {
	if in.pos+len(word) > len(in.text) || string(in.text[in.pos:in.pos+len(word)]) != word {
		in.bad = true
		return false
	}
	in.pos += len(word)
	return true
}

// hex4 reads one \uXXXX escape body; -1 when the four hex digits are not there.
func (in *reader) hex4() int {
	if in.pos+4 > len(in.text) {
		return -1
	}
	value := 0
	for i := range 4 {
		c := in.text[in.pos+i]
		var digit int
		switch {
		case c >= '0' && c <= '9':
			digit = int(c - '0')
		case c >= 'a' && c <= 'f':
			digit = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			digit = int(c-'A') + 10
		default:
			return -1
		}
		value = value<<4 | digit
	}
	in.pos += 4
	return value
}

func encodeUTF8(code uint32) []byte {
	switch {
	case code < 0x80:
		return []byte{byte(code)}
	case code < 0x800:
		return []byte{byte(0xc0 | code>>6), byte(0x80 | code&0x3f)}
	case code < 0x10000:
		return []byte{byte(0xe0 | code>>12), byte(0x80 | (code>>6)&0x3f), byte(0x80 | code&0x3f)}
	}
	return []byte{byte(0xf0 | code>>18), byte(0x80 | (code>>12)&0x3f), byte(0x80 | (code>>6)&0x3f), byte(0x80 | code&0x3f)}
}

// scanString reads one JSON string, appending ONE CODE POINT AT A TIME — an
// escape's encoding, or a UTF-8 sequence read whole — so a string longer than
// the field is clamped AT A CODE POINT BOUNDARY and never cut through a
// multi-byte character. capacity < 0 is unbounded; the returned bool is
// whether the scan succeeded, and clamped whether anything was dropped.
func (in *reader) scanString(capacity int) (out []byte, clamped bool, ok bool) {
	if in.peek() != '"' {
		in.bad = true
		return nil, false, false
	}
	in.pos++
	for {
		if in.pos >= len(in.text) {
			in.bad = true
			return nil, false, false
		}
		c := in.text[in.pos]
		if c == '"' {
			in.pos++
			break
		}
		var unit []byte
		switch {
		case c == '\\':
			in.pos++
			if in.pos >= len(in.text) {
				in.bad = true
				return nil, false, false
			}
			esc := in.text[in.pos]
			in.pos++
			switch esc {
			case '"':
				unit = []byte{'"'}
			case '\\':
				unit = []byte{'\\'}
			case '/':
				unit = []byte{'/'}
			case 'b':
				unit = []byte{'\b'}
			case 'f':
				unit = []byte{'\f'}
			case 'n':
				unit = []byte{'\n'}
			case 'r':
				unit = []byte{'\r'}
			case 't':
				unit = []byte{'\t'}
			case 'u':
				high := in.hex4()
				if high < 0 {
					in.bad = true
					return nil, false, false
				}
				code := uint32(high)
				if high >= 0xd800 && high <= 0xdbff && in.pos+2 <= len(in.text) &&
					in.text[in.pos] == '\\' && in.text[in.pos+1] == 'u' {
					mark := in.pos
					in.pos += 2
					low := in.hex4()
					if low >= 0xdc00 && low <= 0xdfff {
						code = 0x10000 + (uint32(high)-0xd800)<<10 + (uint32(low) - 0xdc00)
					} else {
						in.pos = mark // a lone lead surrogate rides as itself
					}
				}
				unit = encodeUTF8(code)
			default:
				in.bad = true
				return nil, false, false
			}
		case c < 0x20:
			in.bad = true // a raw control character is not a JSON string body
			return nil, false, false
		default:
			// a UTF-8 sequence read WHOLE, so the clamp can only land between
			// code points. Only bytes that ACTUALLY look like continuations
			// are taken: the wire imposes no encoding (§3), so a string may
			// legitimately hold a stray lead byte, and one at the end of a
			// text must not swallow the closing quote.
			lead := c
			want := 1
			switch {
			case lead&0xe0 == 0xc0:
				want = 2
			case lead&0xf0 == 0xe0:
				want = 3
			case lead&0xf8 == 0xf0:
				want = 4
			}
			unit = []byte{c}
			in.pos++
			for len(unit) < want && in.pos < len(in.text) && in.text[in.pos]&0xc0 == 0x80 {
				unit = append(unit, in.text[in.pos])
				in.pos++
			}
		}
		if capacity >= 0 && len(out)+len(unit) > capacity {
			clamped = true
			continue
		}
		out = append(out, unit...)
	}
	return out, clamped, true
}

// scanNumber lifts the numeric token at the cursor. integral reports whether
// it carried no fraction and no exponent.
func (in *reader) scanNumber() (token string, integral bool, ok bool) {
	in.space()
	start := in.pos
	integral = true
	digits := false
	for in.pos < len(in.text) {
		c := in.text[in.pos]
		numeric := (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E'
		if !numeric {
			break
		}
		if c == '.' || c == 'e' || c == 'E' {
			integral = false
		}
		if c >= '0' && c <= '9' {
			digits = true
		}
		in.pos++
	}
	n := in.pos - start
	if n <= 0 || n > maxJsonNumber || !digits {
		return "", false, false
	}
	return string(in.text[start:in.pos]), integral, true
}

func (in *reader) skipValue(depth int) bool {
	switch c := in.peek(); c {
	case '{':
		return in.skipContainer('}', depth)
	case '[':
		return in.skipContainer(']', depth)
	case '"':
		_, _, ok := in.scanString(0)
		return ok
	case 't':
		return in.literal("true")
	case 'f':
		return in.literal("false")
	case 'n':
		return in.literal("null")
	case 0:
		in.bad = true
		return false
	default:
		_, _, ok := in.scanNumber()
		if !ok {
			in.bad = true
		}
		return ok
	}
}

func (in *reader) skipContainer(close byte, depth int) bool {
	if depth > maxJsonDepth {
		in.bad = true
		return false
	}
	in.pos++ // the opening bracket
	for {
		c := in.peek()
		if c == close {
			in.pos++
			return true
		}
		if c == 0 {
			in.bad = true
			return false
		}
		if close == '}' {
			if _, _, ok := in.scanString(0); !ok {
				return false
			}
			if in.peek() != ':' {
				in.bad = true
				return false
			}
			in.pos++
		}
		if !in.skipValue(depth + 1) {
			return false
		}
		c = in.peek()
		if c == ',' {
			in.pos++ // a trailing comma is accepted
			continue
		}
		if c == close {
			in.pos++
			return true
		}
		in.bad = true
		return false
	}
}

// readTable places ONE table object: keys are field keys, unknown ones are
// skipped and counted, a repeated key is last-wins and counted. The instance
// is already at its declared defaults, so a key the text never mentions keeps
// the default an absent field takes on the wire (§4).
func (in *reader) readTable(inst *Instance, depth int) bool {
	if depth > maxJsonDepth {
		in.bad = true
		return false
	}
	if in.peek() != '{' {
		in.bad = true
		return false
	}
	in.pos++
	seen := make(map[int]bool, len(inst.Fields))
	for {
		c := in.peek()
		if c == '}' {
			in.pos++
			return true
		}
		if c == 0 {
			in.bad = true
			return false
		}
		key, _, ok := in.scanString(maxJsonKey)
		if !ok {
			return false
		}
		if in.peek() != ':' {
			in.bad = true
			return false
		}
		in.pos++
		index, known := inst.FieldIndexByKey(string(key))
		if !known {
			in.report.Unknown++
			if !in.skipValue(depth + 1) {
				return false
			}
		} else {
			if seen[index] {
				in.report.Duplicate++
			}
			seen[index] = true
			fv := &inst.Fields[index]
			if in.valueShape() != Shape(fv.Def) {
				// the wrong JSON type for the kind: skipped, never coerced
				in.report.KindMismatch++
				if !in.skipValue(depth + 1) {
					return false
				}
			} else {
				// PRESENCE of the KEY is presence (SPEC-TABLES.md §16.2):
				// reaching this line is the key being there, whatever the
				// value turns out to be. A last-wins repeat re-places the
				// value and leaves presence set.
				in.m.reset(fv)
				if !in.readField(fv, depth) {
					return false
				}
				if fv.Def.Type.Optional {
					fv.Present = true
				}
			}
		}
		c = in.peek()
		if c == ',' {
			in.pos++ // a trailing comma is accepted
			continue
		}
		if c == '}' {
			in.pos++
			return true
		}
		in.bad = true
		return false
	}
}

func (in *reader) readField(fv *Field, depth int) bool {
	f := fv.Def
	switch {
	case f.Type.Kind == ir.TString:
		bound := int(f.Type.Size)
		if f.Type.Pointer {
			bound = -1 // a *string has no bound to clamp against (§16.2)
		}
		out, clamped, ok := in.scanString(bound)
		if !ok {
			return false
		}
		if clamped {
			in.report.Clamped++
		}
		fv.Cell.Str = out
		fv.Count = len(out)
		return true
	case f.Type.Kind == ir.TBytes:
		return in.readBase64(fv)
	case f.KeyEnum != "":
		return in.readKeyed(fv, depth)
	case f.Array != ir.ArrayNone:
		return in.readArray(fv, depth)
	}
	return in.readScalar(&fv.Cell, f, depth)
}

// readBase64 decodes a `bytes(N)` body six bits at a time, exactly as the
// generated walk does: a body that is not base64 is the wrong SHAPE for the
// kind, so the field keeps its default and the event is counted.
func (in *reader) readBase64(fv *Field) bool {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	if in.peek() != '"' {
		in.bad = true
		return false
	}
	in.pos++
	bound := int(fv.Def.Type.Size)
	if fv.Def.Type.Pointer {
		bound = -1 // a *bytes has no bound to clamp against
	}
	var out []byte
	var accumulator uint32
	held := 0
	clamped, malformed := false, false
	for {
		if in.pos >= len(in.text) {
			in.bad = true
			return false
		}
		c := in.text[in.pos]
		in.pos++
		if c == '"' {
			break
		}
		if c == '=' || malformed {
			continue
		}
		at := -1
		for i := range len(alphabet) {
			if alphabet[i] == c {
				at = i
				break
			}
		}
		if at < 0 {
			malformed = true
			continue
		}
		accumulator = accumulator<<6 | uint32(at)
		held += 6
		if held >= 8 {
			held -= 8
			if bound < 0 || len(out) < bound {
				out = append(out, byte(accumulator>>held))
			} else {
				clamped = true
			}
		}
	}
	if malformed {
		in.report.KindMismatch++
		return true
	}
	if clamped {
		in.report.Clamped++
	}
	fv.Cell.Str = out
	fv.Count = len(out)
	return true
}

func (in *reader) readArray(fv *Field, depth int) bool {
	f := fv.Def
	if in.peek() != '[' {
		in.bad = true
		return false
	}
	in.pos++
	placed := 0
	shape := ElementShape(f)
	bound := int(f.ArrayBound)
	for {
		c := in.peek()
		if c == ']' {
			in.pos++
			break
		}
		if c == 0 {
			in.bad = true
			return false
		}
		switch {
		case placed >= bound:
			// more elements than the reader's bound: the bounded prefix is
			// kept and the excess counts, the wire's rule (§4)
			in.report.Clamped++
			if !in.skipValue(depth + 1) {
				return false
			}
		case in.valueShape() != shape:
			in.report.KindMismatch++
			if !in.skipValue(depth + 1) {
				return false
			}
			placed++
		default:
			if !in.readScalar(&fv.Elems[placed], f, depth+1) {
				return false
			}
			placed++
		}
		c = in.peek()
		if c == ',' {
			in.pos++
			continue
		}
		if c == ']' {
			in.pos++
			break
		}
		in.bad = true
		return false
	}
	// a fixed array's tail keeps the defaults the reset left there, exactly as
	// a short wire count does
	fv.Count = placed
	return true
}

// readKeyed places an enum-keyed array: an OBJECT keyed by VARIANT NAME
// (SPEC-TABLES.md §2.4, §16.2). An absent key keeps that slot's defaults, an
// unknown key is skipped and counted, a duplicate key is last-wins and counted.
func (in *reader) readKeyed(fv *Field, depth int) bool {
	f := fv.Def
	if in.peek() != '{' {
		in.bad = true
		return false
	}
	in.pos++
	seen := map[int]bool{}
	shape := ElementShape(f)
	for {
		c := in.peek()
		if c == '}' {
			in.pos++
			break
		}
		if c == 0 {
			in.bad = true
			return false
		}
		key, _, ok := in.scanString(maxJsonKey)
		if !ok {
			return false
		}
		if in.peek() != ':' {
			in.bad = true
			return false
		}
		in.pos++
		slot := KeyedValueSlot(f, EnumValue(f.KeyEnumRef, string(key)))
		switch {
		case slot < 0:
			in.report.Unknown++ // a key this enum cannot name
			if !in.skipValue(depth + 1) {
				return false
			}
		case in.valueShape() != shape:
			in.report.KindMismatch++
			if !in.skipValue(depth + 1) {
				return false
			}
		default:
			if seen[slot] {
				in.report.Duplicate++
			}
			seen[slot] = true
			fv.Elems[slot] = in.m.elementZero(f)
			if !in.readScalar(&fv.Elems[slot], f, depth+1) {
				return false
			}
		}
		c = in.peek()
		if c == ',' {
			in.pos++
			continue
		}
		if c == '}' {
			in.pos++
			break
		}
		in.bad = true
		return false
	}
	return true
}

// readScalar places one value at one cell: a nested object, a union, a
// vocabulary, or a number.
func (in *reader) readScalar(cell *Cell, f *ir.Field, depth int) bool {
	if un := UnionOf(f); un != nil {
		return in.readUnion(cell, un, depth)
	}
	if st := StructOf(f); st != nil {
		cell.Tab = in.m.New(st)
		return in.readTable(cell.Tab, depth+1)
	}
	if e := EnumOf(f); e != nil {
		name, _, ok := in.scanString(maxJsonKey)
		if !ok {
			return false
		}
		if v := EnumValue(e, string(name)); v >= 0 {
			cell.U = uint64(v)
			return true
		}
		// a name this build cannot name reads as None and counts as unknown,
		// exactly as an unknown variant id does on the wire (§4)
		cell.U = 0
		in.report.Unknown++
		return true
	}
	if fl := FlagsOf(f); fl != nil {
		return in.readFlags(cell, fl, depth)
	}
	if f.Type.Kind == ir.TBool {
		c := in.peek()
		if c == 't' {
			if !in.literal("true") {
				return false
			}
			cell.B = true
			return true
		}
		if !in.literal("false") {
			return false
		}
		cell.B = false
		return true
	}
	token, integral, ok := in.scanNumber()
	if !ok {
		in.bad = true
		return false
	}
	kind := ScalarKind(f)
	if kind == KindF32 || kind == KindF64 {
		single := kind == KindF32
		value, err := strconv.ParseFloat(token, 64)
		if err != nil {
			// a token the runtime converter saturates rather than refuses:
			// mirror it, since the C++ side reads it through strtod
			value = saturateFloat(token)
		}
		if single {
			value = float64(float32(value))
		}
		if f.HasFloatRange {
			if value < f.FMin {
				value = f.FMin
				in.report.Clamped++
			} else if value > f.FMax {
				value = f.FMax
				in.report.Clamped++
			}
		}
		if single {
			value = float64(float32(value))
		}
		cell.F = value
		return true
	}
	if !integral {
		// a fraction where an integer is declared is the WRONG SHAPE for the
		// kind, not framing damage: skipped and counted, never rounded into
		// place (§16.2)
		in.report.KindMismatch++
		return true
	}
	signed := kind >= KindI8 && kind <= KindI64
	value, saturated := tokenInteger(token, signed)
	if saturated {
		in.report.Clamped++
	}
	if f.HasIntRange {
		lo, hi := bigToFloat(f.IntMin), bigToFloat(f.IntMax)
		if float64(value) < lo {
			value = int64(lo)
			in.report.Clamped++
		} else if float64(value) > hi {
			value = int64(hi)
			in.report.Clamped++
		}
	}
	// the field's own storage width is the last bound: a value past it clamps
	// rather than wrapping, which is what the wire does too
	if w := StorageBytes(f); w > 0 && w < 8 {
		if signed {
			high := int64(1)<<(w*8-1) - 1
			low := -high - 1
			if value > high {
				value = high
				in.report.Clamped++
			} else if value < low {
				value = low
				in.report.Clamped++
			}
		} else {
			high := uint64(1)<<(w*8) - 1
			if value < 0 {
				value = 0
				in.report.Clamped++
			} else if uint64(value) > high {
				value = int64(high)
				in.report.Clamped++
			}
		}
	}
	cell.I = value
	cell.U = uint64(value)
	return true
}

func (in *reader) readUnion(cell *Cell, un *ir.Union, depth int) bool {
	// a union is an object with ONE key, the arm's name; {} is None, and two
	// keys is a text this walk will not guess at
	if in.peek() != '{' {
		in.bad = true
		return false
	}
	in.pos++
	cell.U = 0
	cell.Tab = nil
	if in.peek() == '}' {
		in.pos++
		return true
	}
	key, _, ok := in.scanString(maxJsonKey)
	if !ok {
		return false
	}
	if in.peek() != ':' {
		in.bad = true
		return false
	}
	in.pos++
	tag := 0
	for i, v := range un.Variants {
		if v.Name == string(key) {
			tag = i + 1
			break
		}
	}
	if tag == 0 {
		in.report.Unknown++
		if !in.skipValue(depth + 1) {
			return false
		}
	} else {
		payload := in.m.New(un.Variants[tag-1].Ref)
		if !in.readTable(payload, depth+1) {
			return false
		}
		cell.U = uint64(tag)
		cell.Tab = payload
	}
	c := in.peek()
	if c == ',' {
		in.pos++
		c = in.peek()
	}
	if c == '}' {
		in.pos++
		return true
	}
	in.bad = true // a second key: a one-of with two arms is not a value
	return false
}

func (in *reader) readFlags(cell *Cell, fl *ir.Flags, depth int) bool {
	if in.peek() != '[' {
		in.bad = true
		return false
	}
	in.pos++
	var bits uint64
	for {
		c := in.peek()
		if c == ']' {
			in.pos++
			break
		}
		if c == 0 {
			in.bad = true
			return false
		}
		if c != '"' {
			in.report.KindMismatch++
			if !in.skipValue(depth + 1) {
				return false
			}
		} else {
			name, _, ok := in.scanString(maxJsonKey)
			if !ok {
				return false
			}
			found := false
			for bit, v := range fl.Variants {
				if v == string(name) {
					bits |= uint64(1) << uint(bit)
					found = true
					break
				}
			}
			if !found {
				in.report.Unknown++
			}
		}
		c = in.peek()
		if c == ',' {
			in.pos++
			continue
		}
		if c == ']' {
			in.pos++
			break
		}
		in.bad = true
		return false
	}
	cell.U = bits
	return true
}

// tokenInteger converts a token digit by digit so no width and no locale can
// move it. Saturation is reported as a clamp, the wire's rule for a value
// outside what the reader can hold (§4).
func tokenInteger(token string, signed bool) (int64, bool) {
	i := 0
	negative := false
	if i < len(token) && (token[i] == '-' || token[i] == '+') {
		negative = token[i] == '-'
		i++
	}
	var magnitude uint64
	over := false
	for ; i < len(token); i++ {
		digit := uint64(token[i] - '0')
		if magnitude > (math.MaxUint64-digit)/10 {
			over = true
			break
		}
		magnitude = magnitude*10 + digit
	}
	if !signed {
		if negative {
			return 0, true
		}
		if over {
			// the C++ token parser saturates an unsigned field at UINT64_MAX,
			// which is -1 in the int64 the two of them carry it in
			return -1, true
		}
		return int64(magnitude), false
	}
	if negative {
		if over || magnitude > 1<<63 {
			return math.MinInt64, true
		}
		if magnitude == 1<<63 {
			return math.MinInt64, false
		}
		return -int64(magnitude), false
	}
	if over || magnitude > math.MaxInt64 {
		return math.MaxInt64, true
	}
	return int64(magnitude), false
}

// saturateFloat mirrors strtod's answer for a token Go's parser refuses only
// for range: the signed infinity, which the writer then refuses to spell.
func saturateFloat(token string) float64 {
	if len(token) > 0 && token[0] == '-' {
		return math.Inf(-1)
	}
	return math.Inf(1)
}

// bigToFloat renders an IR range bound as the double the descriptors carry, so
// the text form's clamp compares the same two numbers the generated walk does.
func bigToFloat(v *big.Int) float64 {
	f, _ := new(big.Float).SetInt(v).Float64()
	return f
}

// ---- the packer's two entry points (SPEC-TABLES.md §17.1) ----

// ReadValue places ONE FIELD's value from one text — a plain `<field>.json` at
// any level is that field's value verbatim. Every rule is §16's: the shape is
// checked before the value is placed, presence of the FILE is presence for a
// `?T`, and nothing is coerced.
func (m *Model) ReadValue(fv *Field, text []byte, report *Report) bool {
	in := &reader{text: text, report: report, m: m}
	m.reset(fv)
	var ok bool
	if in.valueShape() != Shape(fv.Def) {
		report.KindMismatch++
		ok = in.skipValue(0)
	} else {
		ok = in.readField(fv, 0)
		if ok && fv.Def.Type.Optional {
			fv.Present = true
		}
	}
	if ok {
		in.space()
		if in.pos != len(in.text) {
			in.bad = true // trailing rubbish is not one text
		}
	}
	if in.bad || !ok {
		report.Malformed = true
		return false
	}
	return true
}

// ReadElement places ONE ELEMENT of an array field from one text — a
// `<Variant>.json` under an enum-keyed field's directory, or one of the files
// that become a bounded array's elements in name order.
func (m *Model) ReadElement(fv *Field, slot int, text []byte, report *Report) bool {
	in := &reader{text: text, report: report, m: m}
	fv.Elems[slot] = m.elementZero(fv.Def)
	var ok bool
	if in.valueShape() != ElementShape(fv.Def) {
		report.KindMismatch++
		ok = in.skipValue(0)
	} else {
		ok = in.readScalar(&fv.Elems[slot], fv.Def, 0)
	}
	if ok {
		in.space()
		if in.pos != len(in.text) {
			in.bad = true
		}
	}
	if in.bad || !ok {
		report.Malformed = true
		return false
	}
	return true
}
