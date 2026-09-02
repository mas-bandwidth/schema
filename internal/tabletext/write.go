// The text form's WRITE half (SPEC-TABLES.md §16.1): one instance as one JSON
// text — every field, in declaration order, defaults included, because a text
// is for people and tools and a text that elides is a text a reader has to
// know the schema to complete.
//
// The BYTES match the generated C++ writer: two-space indent, `": "` between a
// key and its value, a comma closing the previous line, `{}` and `[]` for the
// empty forms, a float at the shortest precision that reads back the same
// value at the field's own width. That is what makes `schema unpack` -> C++
// `ToJson` a byte comparison rather than a structural one.
package tabletext

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

type writer struct {
	b strings.Builder
}

func (w *writer) raw(s string) { w.b.WriteString(s) }
func (w *writer) put(c byte)   { w.b.WriteByte(c) }
func (w *writer) line(depth int) {
	w.b.WriteByte('\n')
	for i := 0; i < depth; i++ {
		w.b.WriteString("  ")
	}
}

// Write renders one instance as one JSON text. It fails only where a value has
// no text spelling at all — a non-finite float, an enum value or union tag no
// variant names — which measure and save refuse for the same reason (§5).
func (m *Model) Write(inst *Instance) ([]byte, error) {
	w := &writer{}
	if err := m.writeValue(w, inst, 0); err != nil {
		return nil, err
	}
	return []byte(w.b.String()), nil
}

func (m *Model) writeValue(w *writer, inst *Instance, depth int) error {
	guards := Guards(inst.Def)
	any := false
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		if terms, guarded := guards[fv.Def.Name]; guarded && !inst.GuardHolds(terms) {
			continue
		}
		// an OPTIONAL field writes its key only when present: presence IS the
		// presence, and an absent key is the absence (SPEC-TABLES.md §16.2)
		if fv.Def.Type.Optional && !fv.Present {
			continue
		}
		if !any {
			w.put('{')
		} else {
			w.put(',')
		}
		any = true
		w.line(depth + 1)
		writeString(w, []byte(ir.TableFieldJsonKey(fv.Def)))
		w.raw(": ")
		if err := m.writeField(w, fv, depth+1); err != nil {
			return err
		}
	}
	if !any {
		w.raw("{}")
		return nil
	}
	w.line(depth)
	w.put('}')
	return nil
}

func (m *Model) writeField(w *writer, fv *Field, depth int) error {
	f := fv.Def
	switch {
	case f.Type.Kind == ir.TString:
		writeString(w, fv.Cell.Str)
		return nil
	case f.Type.Kind == ir.TBytes:
		writeBase64(w, fv.Cell.Str)
		return nil
	case f.KeyEnum != "":
		return m.writeKeyed(w, fv, depth)
	case f.Array != ir.ArrayNone:
		count := int(f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			count = fv.Count
		}
		if count == 0 {
			w.raw("[]")
			return nil
		}
		w.put('[')
		for i := 0; i < count; i++ {
			if i > 0 {
				w.put(',')
			}
			w.line(depth + 1)
			if err := m.writeScalar(w, &fv.Elems[i], f, depth+1); err != nil {
				return err
			}
		}
		w.line(depth)
		w.put(']')
		return nil
	}
	return m.writeScalar(w, &fv.Cell, f, depth)
}

// writeKeyed renders an enum-keyed array as an OBJECT keyed by variant name
// (SPEC-TABLES.md §2.4, §16.2). Every slot the array KEYS is written, as a
// fixed array writes every element — the slots ARE the value. Slot 0 is
// None's and is never one of them: None keys no record.
func (m *Model) writeKeyed(w *writer, fv *Field, depth int) error {
	f := fv.Def
	n := KeyedSlotCount(f)
	if n <= KeyedFirstSlot() {
		w.raw("{}")
		return nil
	}
	w.put('{')
	for slot := KeyedFirstSlot(); slot < n; slot++ {
		if slot > KeyedFirstSlot() {
			w.put(',')
		}
		w.line(depth + 1)
		name := EnumName(f.KeyEnumRef, KeyedSlotValue(f, slot))
		if name == "" {
			return fmt.Errorf("enum-keyed array %s: slot %d belongs to no variant of %s — a slot with no name has no text spelling (SPEC-TABLES.md §3.2)", f.Name, slot, f.KeyEnum)
		}
		writeString(w, []byte(name))
		w.raw(": ")
		if err := m.writeScalar(w, &fv.Elems[slot], f, depth+1); err != nil {
			return err
		}
	}
	w.line(depth)
	w.put('}')
	return nil
}

func (m *Model) writeScalar(w *writer, cell *Cell, f *ir.Field, depth int) error {
	if un := UnionOf(f); un != nil {
		// a union is an object with ONE key, the arm's name; None is {}
		if cell.U == 0 {
			w.raw("{}")
			return nil
		}
		if int(cell.U) > len(un.Variants) {
			return fmt.Errorf("union %s: tag %d names no arm — the writer refuses it, exactly as measure does (SPEC-TABLES.md §5)", un.Name, cell.U)
		}
		arm := un.Variants[cell.U-1]
		w.put('{')
		w.line(depth + 1)
		writeString(w, []byte(arm.Name))
		w.raw(": ")
		payload := cell.Tab
		if payload == nil {
			payload = m.New(arm.Ref)
		}
		if err := m.writeValue(w, payload, depth+1); err != nil {
			return err
		}
		w.line(depth)
		w.put('}')
		return nil
	}
	if st := StructOf(f); st != nil {
		inst := cell.Tab
		if inst == nil {
			inst = m.New(st)
		}
		return m.writeValue(w, inst, depth)
	}
	if e := EnumOf(f); e != nil {
		// a value no variant names has no text spelling, exactly as it has no
		// wire identity: the writer REFUSES rather than writing None over it
		name := EnumName(e, int64(cell.U))
		if name == "" {
			return fmt.Errorf("enum %s: value %d names no variant — the writer refuses it, exactly as measure does (SPEC-TABLES.md §5)", e.Name, cell.U)
		}
		writeString(w, []byte(name))
		return nil
	}
	if fl := FlagsOf(f); fl != nil {
		bits := cell.U
		if bits == 0 {
			w.raw("[]")
			return nil
		}
		w.put('[')
		first := true
		for bit := 0; bit < 64; bit++ {
			if bits&(uint64(1)<<uint(bit)) == 0 {
				continue
			}
			if bit >= len(fl.Variants) {
				return fmt.Errorf("flags %s: bit %d names no variant — a bit with no name has no text spelling (SPEC-TABLES.md §16.2)", fl.Name, bit)
			}
			if !first {
				w.put(',')
			}
			first = false
			w.line(depth + 1)
			writeString(w, []byte(fl.Variants[bit]))
		}
		w.line(depth)
		w.put(']')
		return nil
	}
	switch ScalarKind(f) {
	case KindBool:
		if cell.B {
			w.raw("true")
		} else {
			w.raw("false")
		}
		return nil
	case KindF32:
		return writeFloat(w, cell.F, true)
	case KindF64:
		return writeFloat(w, cell.F, false)
	case KindI8, KindI16, KindI32, KindI64:
		w.raw(strconv.FormatInt(cell.I, 10))
		return nil
	default:
		w.raw(strconv.FormatUint(cell.U, 10))
		return nil
	}
}

func writeString(w *writer, s []byte) {
	const hex = "0123456789abcdef"
	w.put('"')
	for _, c := range s {
		switch c {
		case '"':
			w.raw("\\\"")
		case '\\':
			w.raw("\\\\")
		case '\b':
			w.raw("\\b")
		case '\f':
			w.raw("\\f")
		case '\n':
			w.raw("\\n")
		case '\r':
			w.raw("\\r")
		case '\t':
			w.raw("\\t")
		default:
			if c < 0x20 {
				w.raw("\\u00")
				w.put(hex[c>>4])
				w.put(hex[c&0xf])
			} else {
				// UTF-8 rides as its own bytes: the wire imposes no encoding
				// and neither does the text (SPEC-TABLES.md §3)
				w.put(c)
			}
		}
	}
	w.put('"')
}

const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

func writeBase64(w *writer, data []byte) {
	w.put('"')
	i := 0
	for ; i+3 <= len(data); i += 3 {
		triple := uint32(data[i])<<16 | uint32(data[i+1])<<8 | uint32(data[i+2])
		w.put(base64Alphabet[triple>>18&0x3f])
		w.put(base64Alphabet[triple>>12&0x3f])
		w.put(base64Alphabet[triple>>6&0x3f])
		w.put(base64Alphabet[triple&0x3f])
	}
	if i < len(data) {
		left := len(data) - i
		triple := uint32(data[i]) << 16
		if left == 2 {
			triple |= uint32(data[i+1]) << 8
		}
		w.put(base64Alphabet[triple>>18&0x3f])
		w.put(base64Alphabet[triple>>12&0x3f])
		if left == 2 {
			w.put(base64Alphabet[triple>>6&0x3f])
		} else {
			w.put('=')
		}
		w.put('=')
	}
	w.put('"')
}

// writeFloat spells a float at the SHORTEST precision that reads back as the
// same value at the field's own width, so a round trip is exact and a text
// stays readable. Non-finite values have no JSON spelling at all and the
// writer REFUSES rather than losing one silently — the rule measure and save
// already apply to an enum value no variant names (§5).
func writeFloat(w *writer, value float64, single bool) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("float %v has no JSON spelling — the writer refuses it rather than losing it silently (SPEC-TABLES.md §16.2)", value)
	}
	low, high := 15, 17
	if single {
		low, high = 6, 9
	}
	for digits := low; ; digits++ {
		text := formatG(value, digits)
		if digits >= high {
			w.raw(text)
			return nil
		}
		back, err := strconv.ParseFloat(text, 64)
		if err == nil {
			if single {
				if float64(float32(back)) == value {
					w.raw(text)
					return nil
				}
			} else if back == value {
				w.raw(text)
				return nil
			}
		}
	}
}

// formatG is C's `%.*g` for a finite double: the exponent form when the
// decimal exponent is below -4 or at least the precision, the plain form
// otherwise, trailing zeros and a trailing point removed, and an exponent of at
// least two digits. The generated writer reaches it through snprintf; a port
// spells it out, because the two must agree byte for byte.
func formatG(value float64, prec int) string {
	if prec < 1 {
		prec = 1
	}
	sci := strconv.FormatFloat(value, 'e', prec-1, 64)
	at := strings.IndexByte(sci, 'e')
	exp, _ := strconv.Atoi(sci[at+1:])
	if exp < -4 || exp >= prec {
		return trimZeros(sci[:at]) + "e" + expDigits(exp)
	}
	return trimZeros(strconv.FormatFloat(value, 'f', prec-1-exp, 64))
}

func trimZeros(s string) string {
	if !strings.ContainsRune(s, '.') {
		return s
	}
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

func expDigits(exp int) string {
	sign := "+"
	if exp < 0 {
		sign = "-"
		exp = -exp
	}
	if exp < 10 {
		return sign + "0" + strconv.Itoa(exp)
	}
	return sign + strconv.Itoa(exp)
}

// ---- the packer's two entry points (SPEC-TABLES.md §17.1) ----

// WriteValue renders ONE FIELD's value as the text a `<field>.json` carries.
func (m *Model) WriteValue(fv *Field) ([]byte, error) {
	w := &writer{}
	if err := m.writeField(w, fv, 0); err != nil {
		return nil, err
	}
	return []byte(w.b.String()), nil
}

// WriteElement renders ONE ELEMENT of an array field as the text a
// `<Variant>.json` or a bounded array's element file carries.
func (m *Model) WriteElement(fv *Field, slot int) ([]byte, error) {
	w := &writer{}
	if err := m.writeScalar(w, &fv.Elems[slot], fv.Def, 0); err != nil {
		return nil, err
	}
	return []byte(w.b.String()), nil
}
