// The text form's READ half (docs/SPEC-TABLES.md §16.2): one JSON text into one
// instance, over the IR. Every rule here is §16's and mirrors the generated
// C++ walk — unknown keys skipped and counted, duplicates last-wins and
// counted, a wrong JSON type skipped rather than coerced, numbers clamped at
// the declared bounds and then at the storage width, strings clamped at a code
// point boundary, trailing commas accepted, comments refused.
package tabletext

import (
	"errors"
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
	// graph is the VARIABLE class's label map (docs/SPEC-TABLES.md §16.7): the
	// node each `&node` defined, keyed by the label. nil for a fixed root,
	// which is what makes the reserved prefix a refusal there.
	graph map[uint64]labelEntry
}

// labelEntry is what an `&node` label defined: its node and the node's table,
// or neither for a definition the walk dropped — a value past an array's bound, an
// unknown key's value — whose label still has to exist so a reference to it reads
// null rather than refusing the text (§16.7).
type labelEntry struct {
	node *Instance
	st   *ir.Struct
	open bool // the definition has not closed yet: a reference here is a cycle
}

// Read fills one instance from one JSON text (docs/SPEC-TABLES.md §16.1). The
// instance holds what was placed before any stop; false means the text is not
// JSON or a value could not be placed at all, and report.Malformed says so.
func (m *Model) Read(inst *Instance, text []byte, report *Report) bool {
	in := &reader{text: text, report: report, m: m}
	if m.IsVariable(inst.Def.Name) {
		in.graph = map[uint64]labelEntry{}
	}
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
		// reading a dialect nobody wrote down (docs/SPEC-TABLES.md §16.2)
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

// TakesNull reports whether a field reads `null` as a VALUE rather than as a
// kind mismatch: the two kinds where absence is a value — a `?T`, which reads
// `null` as ABSENT, and a pointer, which reads it as null (docs/SPEC-TABLES.md
// §16.2).
func TakesNull(f *ir.Field) bool { return f.Type.Optional || f.Type.Pointer }

// Shape is the JSON shape a field's declaration takes, the classifier the read
// side matches a value against before placing it (docs/SPEC-TABLES.md §16.2). A
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
						in.pos = mark // a lone lead surrogate never found its partner
					}
				}
				// a surrogate half that never found its partner has no UTF-8
				// encoding: encoding it anyway would manufacture CESU-8 —
				// invalid UTF-8 — out of input that was valid JSON, so it
				// reads as the replacement character (docs/SPEC-TABLES.md §16.3)
				if code >= 0xd800 && code <= 0xdfff {
					code = 0xfffd
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

// walkNumber steps the cursor over ONE RFC 8259 number and reports whether the
// token was integral in FORM — no fraction and no exponent:
//
//	number = [ "-" ] int [ frac ] [ exp ]
//	int    = "0" / ( digit1-9 *digit )
//	frac   = "." 1*digit
//	exp    = ( "e" / "E" ) [ "-" / "+" ] 1*digit
//
// Scanning the production is what makes a typo in an authoring file a
// DIAGNOSTIC rather than a value: "1-2" scans as 1 and leaves "-2" where the
// object expects a comma, so the text is malformed — which is what §16.2
// promises. A permissive scan would hand "1-2" to a digit loop and report a
// clamp, and a config pipeline would never hear about it. Leading "+", leading
// zeros, ".5" and "3." are not JSON either.
func (in *reader) walkNumber() (integral, ok bool) {
	in.space()
	integral = true
	if in.pos < len(in.text) && in.text[in.pos] == '-' {
		in.pos++
	}
	if in.pos >= len(in.text) {
		return false, false
	}
	switch c := in.text[in.pos]; {
	case c == '0':
		in.pos++
	case c >= '1' && c <= '9':
		for in.pos < len(in.text) && in.text[in.pos] >= '0' && in.text[in.pos] <= '9' {
			in.pos++
		}
	default:
		return false, false
	}
	if in.pos < len(in.text) && in.text[in.pos] == '.' {
		in.pos++
		if in.pos >= len(in.text) || in.text[in.pos] < '0' || in.text[in.pos] > '9' {
			return false, false
		}
		for in.pos < len(in.text) && in.text[in.pos] >= '0' && in.text[in.pos] <= '9' {
			in.pos++
		}
		integral = false
	}
	if in.pos < len(in.text) && (in.text[in.pos] == 'e' || in.text[in.pos] == 'E') {
		in.pos++
		if in.pos < len(in.text) && (in.text[in.pos] == '-' || in.text[in.pos] == '+') {
			in.pos++
		}
		if in.pos >= len(in.text) || in.text[in.pos] < '0' || in.text[in.pos] > '9' {
			return false, false
		}
		for in.pos < len(in.text) && in.text[in.pos] >= '0' && in.text[in.pos] <= '9' {
			in.pos++
		}
		integral = false
	}
	return integral, true
}

// scanNumber is the same production with the token kept for conversion.
func (in *reader) scanNumber() (token string, integral bool, ok bool) {
	in.space()
	start := in.pos
	integral, ok = in.walkNumber()
	if !ok {
		return "", false, false
	}
	n := in.pos - start
	if n <= 0 || n > maxJsonNumber {
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
		_, ok := in.walkNumber()
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
	first := true
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
			// the key is kept, because a skipped OBJECT may still be a
			// pointer's: an `&node` opening it names a node the storage could
			// not hold, and the numbering has to survive the drop (§16.7).
			// Anywhere but first, the prefix is the reserved key out of place
			// — in a pointered root; a fixed root skips the value whole.
			key, _, ok := in.scanString(maxJsonKey)
			if !ok {
				return false
			}
			if in.peek() != ':' {
				in.bad = true
				return false
			}
			in.pos++
			if len(key) > 0 && key[0] == '&' && in.graph != nil {
				if !first || !in.skippedAmpersand(string(key)) {
					in.bad = true
					return false
				}
				first = false
				c = in.peek()
				if c == ',' {
					in.pos++
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
		first = false
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
	return in.readTableKeys(inst, depth, nil)
}

// readTableKeys places the keys of an object whose brace is already consumed.
// A pointer's object opens the same way a table's does, but its FIRST key may
// be `&node` (§16.7) and the reader of a pointer has to scan the key to know —
// so it hands the key it scanned in as firstKey, with the colon consumed, and
// this places it before scanning the rest.
func (in *reader) readTableKeys(inst *Instance, depth int, firstKey []byte) bool {
	seen := make(map[int]bool, len(inst.Fields))
	for {
		var key []byte
		var c byte
		if firstKey != nil {
			key, firstKey = firstKey, nil
		} else {
			c = in.peek()
			if c == '}' {
				in.pos++
				return true
			}
			if c == 0 {
				in.bad = true
				return false
			}
			var ok bool
			key, _, ok = in.scanString(maxJsonKey)
			if !ok {
				return false
			}
			if in.peek() != ':' {
				in.bad = true
				return false
			}
			in.pos++
		}
		if len(key) > 0 && key[0] == '&' {
			// THE AMPERSAND PREFIX IS RESERVED TO THE FORM (docs/SPEC-TABLES.md
			// §16.7). No declaration may take a key beginning with it, so this
			// is never a field this build lacks — it is the sharing construct
			// somewhere it cannot stand: `&node` opens a pointer's
			// object and nothing else, and readPointer has consumed them
			// before these keys are read. Malformed; never unknown, never
			// skipped.
			in.bad = true
			return false
		}
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
			switch shape := in.valueShape(); {
			case shape == 'z' && TakesNull(fv.Def):
				// `null` is the absence, not a value: a `?T` reads it as
				// ABSENT and a pointer as null (docs/SPEC-TABLES.md §16.2). It is
				// the ONE key that puts a field back at its defaults, so a
				// repeated key whose last occurrence is null cannot leave an
				// earlier value standing.
				if !in.literal("null") {
					return false
				}
				in.m.reset(fv)
			default:
				if shape != Shape(fv.Def) {
					// the wrong JSON type for the kind: skipped, never coerced
					in.report.KindMismatch++
					if !in.skipValue(depth + 1) {
						return false
					}
				} else if !in.readField(fv, depth) {
					return false
				}
				// PRESENCE of the KEY is presence (docs/SPEC-TABLES.md §16.2):
				// reaching this line is the key being there, whatever the
				// value turned out to be — a value the walk would not place
				// still makes the field present, because it is the KEY that
				// says so. Only `null`, handled above, is the absence.
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
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		return in.readPointer(fv, depth)
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
	// a SCALAR is not re-established: it is written only when a value is
	// actually placed, so a repeated key whose repeat the walk refuses leaves
	// the first occurrence's value standing (docs/SPEC-TABLES.md §16.2 — a value
	// with the wrong shape is skipped, never coerced, and re-establishment is
	// tied to placing)
	return in.readScalar(&fv.Cell, f, depth)
}

// scanLabel reads `&node`'s value, the label: a positive integer spelled as one —
// digits, no sign, no fraction, no exponent, no leading zero (§16.7). Anything
// else is malformed.
func (in *reader) scanLabel() (uint64, bool) {
	in.space()
	if in.pos >= len(in.text) || in.text[in.pos] < '1' || in.text[in.pos] > '9' {
		in.bad = true
		return 0, false
	}
	var value uint64
	for in.pos < len(in.text) && in.text[in.pos] >= '0' && in.text[in.pos] <= '9' {
		digit := uint64(in.text[in.pos] - '0')
		if value > (math.MaxUint64-digit)/10 {
			in.bad = true
			return 0, false
		}
		value = value*10 + digit
		in.pos++
	}
	return value, true
}

// readPointer places a pointer's object (docs/SPEC-TABLES.md §16.7). Its FIRST
// key decides what it is: `&node` naming a label not yet defined, with fields
// after it, is a DEFINITION; `&node` naming one already defined, alone, is a
// REFERENCE; any other key is a node named once, its object in place. The same walk in the generated C++ allocates the node in
// the builder's arena; here it is an instance.
func (in *reader) readPointer(fv *Field, depth int) bool {
	return in.readPointerCell(&fv.Cell, StructOf(fv.Def), depth)
}

// readPointerCell places one pointer SLOT — a field's cell, or an element of an
// array of pointers (§2.1) — from its object or null.
func (in *reader) readPointerCell(cell *Cell, st *ir.Struct, depth int) bool {
	if in.graph == nil || st == nil {
		in.bad = true
		return false
	}
	// the pointee nests one level down, exactly as a by-value table does, and
	// takes the same cap: a chain nests as deep as it is long (§16.7)
	if depth+1 > maxJsonDepth {
		in.bad = true
		return false
	}
	if in.peek() != '{' {
		in.bad = true
		return false
	}
	in.pos++
	c := in.peek()
	if c == '}' {
		// an empty object: a node at its defaults, named once
		in.pos++
		cell.Node = in.m.New(st)
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
	if string(key) != "&node" {
		// a node named once: the pointee's object in place, and this key is
		// its first field — unless it is the reserved prefix under a spelling
		// this form does not have, which readTableKeys refuses
		cell.Node = in.m.New(st)
		return in.readTableKeys(cell.Node, depth+1, key)
	}
	label, ok := in.scanLabel()
	if !ok {
		return false
	}
	entry, defined := in.graph[label]
	// ONE SPELLING, and what follows the label says which half it is: fields
	// after a label the text has not defined DEFINE it, and a label alone
	// that the text has defined REFERS to it. The other two are malformed — a
	// label alone that the text never defined, which would otherwise read as a
	// default node under a silent report, and a field after a label already
	// defined, which would be a second definition. That is what keeps a typo
	// loud.
	c = in.peek()
	if c == ',' {
		in.pos++
		c = in.peek()
	}
	bare := c == '}'
	if bare != defined {
		in.bad = true
		return false
	}
	if bare {
		// A REFERENCE. A label is defined when its object CLOSES, so a
		// reference met inside its own definition — at any depth of by-value
		// nesting — names a node whose descent is still open: the cycle the
		// wire refuses (§3.1), refused here where it is written. A definition
		// the reader dropped names no node, so the slot stays null with
		// nothing more counted — the drop was counted where it happened. A
		// node of another table than the slot declares is a kind mismatch, as
		// it is on the wire.
		in.pos++
		if entry.open {
			in.bad = true
			return false
		}
		cell.Node = nil
		switch {
		case entry.st == nil:
		case entry.st != st:
			in.report.KindMismatch++
		default:
			cell.Node = entry.node
		}
		return true
	}
	// A DEFINITION: the node takes the label, and the keys after `&node` are
	// its fields. The entry is OPEN until the object closes, so a reference to
	// the label from inside the node's own fields is refused as the cycle it
	// is; the node and its table are filled in at the close.
	cell.Node = in.m.New(st)
	in.graph[label] = labelEntry{open: true}
	if !in.readTableKeys(cell.Node, depth+1, nil) {
		return false
	}
	in.graph[label] = labelEntry{node: cell.Node, st: st}
	return true
}

// skippedAmpersand handles an `&`-prefixed key opening an object the walk is
// SKIPPING — a value past an array's bound, an unknown key's value, a value of
// the wrong shape. A definition in there still takes its label, so the numbering
// survives whatever the storage could not hold (§16.7): the label is registered
// with no node, and a reference to it reads null. Any other prefixed key is
// the reserved prefix out of place. A FIXED root never reaches this: what a
// reader does not place it does not police, and the value is skipped whole.
func (in *reader) skippedAmpersand(key string) bool {
	if in.graph == nil || key != "&node" {
		return false
	}
	label, ok := in.scanLabel()
	if !ok {
		return false
	}
	if _, defined := in.graph[label]; !defined {
		in.graph[label] = labelEntry{}
	}
	return true
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
	// bytes RE-ESTABLISH before decoding: a repeated key places a whole value,
	// and six-bits-at-a-time decoding cannot overlay one
	fv.Cell.Str = nil
	fv.Count = 0
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
	// an ARRAY re-establishes before placing: a fixed array writes every slot,
	// so a second, shorter occurrence overlaying a prefix would leave the
	// first occurrence's tail standing (docs/SPEC-TABLES.md §16.2, last-wins as a
	// WHOLE value)
	for i := range fv.Elems {
		fv.Elems[i] = in.m.elementZero(f)
	}
	fv.Count = 0
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
		case f.Type.Pointer && in.valueShape() == 'z':
			// a null element of an array of pointers is a null slot (§16.2)
			if !in.literal("null") {
				return false
			}
			fv.Elems[placed] = Cell{}
			placed++
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
// (docs/SPEC-TABLES.md §2.4, §16.2). An absent key keeps that slot's defaults; an
// unknown key is skipped and counted, and `"None"` is such a key because None
// keys no slot (§2.4).
//
// A KEYED OBJECT'S KEYS ARE KEYS: a variant named twice is a duplicate key
// like any other, last-wins and counted (§16.2). The count is taken on the
// RESOLVED SLOT and before the shape check, so a repeat the walk then refuses
// is still a repeat; a key that names no slot — an unknown variant, or `"None"`
// — is `unknown` EACH TIME and is never a duplicate, because there is no slot
// for it to be a repeat of.
func (in *reader) readKeyed(fv *Field, depth int) bool {
	f := fv.Def
	if in.peek() != '{' {
		in.bad = true
		return false
	}
	in.pos++
	// every slot back to its declared defaults ONCE, so a key the text omits
	// keeps them and a repeated field key cannot leave an earlier occurrence's
	// slots standing. Per-KEY the slot is not re-established: a repeated slot
	// key whose repeat the walk refuses leaves the first value, exactly as a
	// repeated scalar field key does.
	for i := range fv.Elems {
		fv.Elems[i] = in.m.elementZero(f)
	}
	shape := ElementShape(f)
	seen := map[int]bool{}
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
		if slot >= 0 {
			if seen[slot] {
				in.report.Duplicate++
			}
			seen[slot] = true
		}
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
		if f.Type.Pointer {
			return in.readPointerCell(cell, st, depth) // an element of an array of pointers (§2.1)
		}
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
	kind := ir.TableScalarKind(f)
	if kind == ir.TableKindF32 || kind == ir.TableKindF64 {
		return in.placeFloat(cell, f, token, kind == ir.TableKindF32)
	}
	if ir.TableKindWide(kind) {
		return in.placeWide(cell, f, token, kind)
	}
	if kind == ir.TableKindU16 || kind == ir.TableKindU32 || kind == ir.TableKindU64 ||
		kind == ir.TableKindI8 || kind == ir.TableKindI16 || kind == ir.TableKindI32 || kind == ir.TableKindI64 ||
		kind == ir.TableKindU8 {
		return in.placeInteger(cell, f, token, integral, kind)
	}
	in.bad = true
	return false
}

// placeFloat converts a number token into a float field. A magnitude the
// field's format cannot hold is the WRONG SHAPE for the kind and never reaches
// storage: storing the infinity the conversion produced would leave an
// instance this walk called CLEAN that the writer then refuses forever, and
// §16.1's invariant is that a text which reads clean writes back
// (docs/SPEC-TABLES.md §16.2, §16.3).
func (in *reader) placeFloat(cell *Cell, f *ir.Field, token string, single bool) bool {
	// ONE correctly-rounded conversion, at the field's OWN width: a float32
	// field converts with bitSize 32, which is what `strtof` is, and reading
	// through a float64 first would round twice. The two roundings part
	// company across a whole band — a decimal between FLT_MAX and the float32
	// rounding midpoint that lands ON the midpoint as a double — and again at
	// the subnormal end, where the double rounds to zero and the single does
	// not. The page's rule is exact conversion at both float widths (§16.2).
	bits := 64
	if single {
		bits = 32
	}
	value, err := strconv.ParseFloat(token, bits)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		in.bad = true
		return false
	}
	if math.IsInf(value, 0) || math.IsNaN(value) {
		in.report.KindMismatch++
		return true
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
		narrow := float32(value)
		if math.IsInf(float64(narrow), 0) {
			in.report.KindMismatch++
			return true
		}
		value = float64(narrow)
	}
	cell.F = value
	return true
}

// placeInteger converts a number token into an integer field. JSON HAS ONE
// NUMBER TYPE, so an integer field takes any token whose VALUE is integral,
// however it was spelled — 2, 2.0 and 1e3 are all the integers this walk
// places — and only a genuinely fractional value is the wrong shape for it
// (docs/SPEC-TABLES.md §16.2).
func (in *reader) placeInteger(cell *Cell, f *ir.Field, token string, integral bool, kind int) bool {
	signed := kind >= ir.TableKindI8 && kind <= ir.TableKindI64
	var value int64
	var saturated bool
	if integral {
		value, saturated = tokenInteger(token, signed)
	} else {
		d, err := strconv.ParseFloat(token, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			in.bad = true
			return false
		}
		if math.IsInf(d, 0) || math.IsNaN(d) {
			in.report.KindMismatch++
			return true
		}
		switch {
		case signed && d >= 9223372036854775808.0:
			value, saturated = math.MaxInt64, true
		case signed && d < -9223372036854775808.0:
			value, saturated = math.MinInt64, true
		case signed:
			if d != float64(int64(d)) {
				in.report.KindMismatch++
				return true
			}
			value = int64(d)
		case d < 0.0:
			// a negative for an unsigned field clamps to zero, as the exact
			// digit path already does
			if d != float64(int64(d)) {
				in.report.KindMismatch++
				return true
			}
			value, saturated = 0, true
		case d >= 18446744073709551616.0:
			value, saturated = -1, true // UINT64_MAX in the int64 both sides carry it in
		default:
			if d != float64(uint64(d)) {
				in.report.KindMismatch++
				return true
			}
			value = int64(uint64(d))
		}
	}
	if saturated {
		in.report.Clamped++
	}
	lo, hi, implied := ImpliedRange(f)
	if f.HasIntRange {
		lo, hi, implied = bigToFloat(f.IntMin), bigToFloat(f.IntMax), true
	}
	if implied {
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
		// -0 IS zero, and clamping it would report an event that did not
		// happen; only a real negative magnitude is out of range here
		if negative {
			return 0, magnitude != 0
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

// bigToFloat renders an IR range bound as the double the descriptors carry, so
// the text form's clamp compares the same two numbers the generated walk does.
func bigToFloat(v *big.Int) float64 {
	f, _ := new(big.Float).SetInt(v).Float64()
	return f
}

// ---- the packer's two entry points (docs/SPEC-TABLES.md §17.1) ----

// ReadValue places ONE FIELD's value from one text — a plain `<field>.json` at
// any level is that field's value verbatim. Every rule is §16's: the shape is
// checked before the value is placed, presence of the FILE is presence for a
// `?T`, and nothing is coerced.
func (m *Model) ReadValue(fv *Field, text []byte, report *Report) bool {
	in := &reader{text: text, report: report, m: m}
	m.reset(fv)
	var ok bool
	switch shape := in.valueShape(); {
	case shape == 'z' && TakesNull(fv.Def):
		// `null` is the absence, not a value (docs/SPEC-TABLES.md §16.2)
		ok = in.literal("null")
	case shape != Shape(fv.Def):
		report.KindMismatch++
		ok = in.skipValue(0)
	default:
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

// placeWide converts a number token into a wide kind's raw storage
// (docs/SPEC-TABLES.md §16.2): a 128-bit integer takes any token whose value
// is integral, a fixed field any token whose value is EXACTLY representable
// in its Q I.F — a finer fraction is the wrong shape for the field, counted
// as a kind mismatch and never rounded, the rule SPEC.md §4.6 gives a fixed
// default. A magnitude past the storage saturates and counts as a clamp;
// the declared range clamps after it, as it does for every bounded scalar.
func (in *reader) placeWide(cell *Cell, f *ir.Field, token string, kind int) bool {
	raw, exact, saturated := ParseWide(token, kind, f.Type.FracBits)
	if !exact {
		in.report.KindMismatch++
		return true
	}
	if saturated {
		in.report.Clamped++
	}
	raw, clamped := WideClamp(raw, f)
	if clamped {
		in.report.Clamped++
	}
	cell.Wide = raw
	return true
}
