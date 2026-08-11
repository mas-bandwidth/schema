// The table wire (notes/table-wire.md): evolution-tolerant TLV encoding for
// `type` declarations — field identity by name hash, unknown fields skipped,
// absent fields defaulted, changed kinds skipped-and-counted, nothing
// crashes on data from a different schema version. The realtime message
// wire is untouched; this wire is for data that outlives builds.
package pack

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/internal/ir"
)

// FieldId is the stable wire identity of a field: fold16(fnv1a32(name)),
// rebounding 0 (the terminator) to 1.
func FieldId(name string) uint16 {
	h := uint32(0x811C9DC5)
	for i := 0; i < len(name); i++ {
		h ^= uint32(name[i])
		h *= 0x01000193
	}
	id := uint16((h ^ (h >> 16)) & 0xFFFF)
	if id == 0 {
		id = 1
	}
	return id
}

// wire kinds (notes/table-wire.md)
const (
	kEnd    = 0
	kBool   = 1
	kI8     = 2
	kI16    = 3
	kI32    = 4
	kI64    = 5
	kU8     = 6
	kU16    = 7
	kU32    = 8
	kU64    = 9
	kF32    = 10
	kF64    = 11
	kString = 12
	kTable  = 13
	kArray  = 14
)

// scalarKind maps a field's SCALAR type (array-ness aside) to its wire kind.
func scalarKind(f *ir.Field) (byte, error) {
	switch f.Type.Kind {
	case ir.TBool:
		return kBool, nil
	case ir.TInt:
		if f.Type.Width == 128 {
			return 0, fmt.Errorf("int128 has no table-wire kind")
		}
		if f.Type.Signed {
			switch f.Type.Width {
			case 8:
				return kI8, nil
			case 16:
				return kI16, nil
			case 32:
				return kI32, nil
			case 64:
				return kI64, nil
			}
		}
		switch f.Type.Width {
		case 8:
			return kU8, nil
		case 16:
			return kU16, nil
		case 32:
			return kU32, nil
		case 64:
			return kU64, nil
		}
		return 0, fmt.Errorf("unsupported int width %d", f.Type.Width)
	case ir.TBits:
		switch {
		case f.Type.Width <= 8:
			return kU8, nil
		case f.Type.Width <= 16:
			return kU16, nil
		case f.Type.Width <= 32:
			return kU32, nil
		case f.Type.Width <= 64:
			return kU64, nil
		}
		return 0, fmt.Errorf("bits(%d) exceeds the widest table-wire kind (u64)", f.Type.Width)
	case ir.TFloat32:
		return kF32, nil
	case ir.TFloat64:
		return kF64, nil
	case ir.TString:
		return kString, nil
	case ir.TBytes:
		return kArray, nil // array of u8
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			switch ref.StorageBits {
			case 8:
				return kU8, nil
			case 16:
				return kU16, nil
			case 32:
				return kU32, nil
			default:
				return kU64, nil
			}
		case *ir.Flags:
			return kU64, nil
		case *ir.Struct:
			return kTable, nil
		}
	}
	return 0, fmt.Errorf("field kind %v has no table-wire mapping", f.Type.Kind)
}

func kindSize(kind byte) int {
	switch kind {
	case kBool, kI8, kU8:
		return 1
	case kI16, kU16:
		return 2
	case kI32, kU32, kF32:
		return 4
	case kI64, kU64, kF64:
		return 8
	}
	return -1 // variable
}

// CheckTableIds refuses field-id collisions per type — loud, at compile time.
func CheckTableIds(u *ir.Unit) error {
	for name, st := range u.Structs {
		seen := map[uint16]string{}
		for _, f := range st.Fields {
			id := FieldId(f.Name)
			if prev, dup := seen[id]; dup {
				return fmt.Errorf("type %s: fields %s and %s collide on table-wire id 0x%04x — rename one (notes/table-wire.md)", name, prev, f.Name, id)
			}
			seen[id] = f.Name
		}
	}
	return nil
}

// ---- writer ----

type tableWriter struct {
	buf []byte
}

func (w *tableWriter) u8(v byte)    { w.buf = append(w.buf, v) }
func (w *tableWriter) u16(v uint16) { w.buf = binary.LittleEndian.AppendUint16(w.buf, v) }
func (w *tableWriter) u32(v uint32) { w.buf = binary.LittleEndian.AppendUint32(w.buf, v) }
func (w *tableWriter) u64(v uint64) { w.buf = binary.LittleEndian.AppendUint64(w.buf, v) }
func (w *tableWriter) raw(b []byte) { w.buf = append(w.buf, b...) }

// EncodeTable encodes a JSON object as typeName on the TABLE wire.
// Fields at their declared default (or zero) are elided — absent means
// default on this wire, by contract. typeName must be in the unit's table
// closure (a `table` declaration or something one references).
func (e *Encoder) EncodeTable(typeName string, obj map[string]any) ([]byte, error) {
	st, ok := e.Unit.Structs[typeName]
	if !ok {
		return nil, fmt.Errorf("schema type %s: not declared in the unit", typeName)
	}
	if !ir.TableClosure(e.Unit)[typeName] {
		return nil, fmt.Errorf("%s is not in the table closure — declare it (or a root that references it) with `table` instead of `type`", typeName)
	}
	return e.encodeTableStruct(st, obj, typeName)
}

func (e *Encoder) encodeTableStruct(st *ir.Struct, obj map[string]any, path string) ([]byte, error) {
	obj, err := e.lowerUnions(st.Name, obj, path)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, f := range st.Fields {
		allowed[f.Name] = true
	}
	for k := range obj {
		if !allowed[k] {
			return nil, fmt.Errorf("%s: unknown field %q (schema type %s declares no such field)", path, k, st.Name)
		}
	}
	w := &tableWriter{}
	if err := e.encodeTableItems(w, st.Items, obj, path); err != nil {
		return nil, err
	}
	w.u16(0) // terminator
	return w.buf, nil
}

// encodeTableItems walks the wire tree so branch guards are honored: fields on
// an untaken branch stay off the wire entirely — TLV's native optionality
// carries the branch (notes/table-wire.md), and the reader's prefilled
// defaults stand in for the untaken side. const/reserved/align are dense-wire
// constructs; the table wire carries named fields only.
func (e *Encoder) encodeTableItems(w *tableWriter, items []ir.Item, obj map[string]any, path string) error {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			if err := e.encodeTableField(w, item.F, obj, path); err != nil {
				return err
			}
		case *ir.Branch:
			cond, err := boolField(obj, item.Cond, path)
			if err != nil {
				return err
			}
			taken := item.Then
			if cond == item.Neg { // if !cond with cond true, or if cond with cond false
				taken = item.Else
			}
			if taken != nil {
				if err := e.encodeTableItems(w, taken, obj, path); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// encodeTableField writes one field, eliding defaults/absence.
func (e *Encoder) encodeTableField(w *tableWriter, f *ir.Field, obj map[string]any, path string) error {
	val := obj[f.Name]
	fpath := path + "." + f.Name
	kind, err := scalarKind(f)
	if err != nil {
		return fmt.Errorf("%s: %v", fpath, err)
	}

	if f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes {
		var arr []any
		if f.Type.Kind == ir.TBytes {
			data, err := bytesValue(val, fpath)
			if err != nil {
				return err
			}
			for _, b := range data {
				arr = append(arr, float64(b))
			}
			kind = kU8
		} else {
			arr, err = jsonArray(val, fpath)
			if err != nil {
				return err
			}
		}
		if len(arr) == 0 {
			return nil // empty array elides
		}
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		if int64(len(arr)) > bound {
			return fmt.Errorf("%s: %d elements exceeds bound %d", fpath, len(arr), bound)
		}
		if f.Array == ir.ArrayFixed {
			// fixed arrays are positional: pad to the bound (absent trailing
			// elements encode as the element default), and elide entirely when
			// EVERY element is default — parity with the C++ writer and the
			// reader's prefill. Table elements always ride (no cheap compare).
			for int64(len(arr)) < bound {
				arr = append(arr, nil)
			}
			if kind != kTable {
				allDefault := true
				for _, elem := range arr {
					def, derr := e.isDefault(f, elem, fpath)
					if derr != nil {
						return derr
					}
					if !def {
						allDefault = false
						break
					}
				}
				if allDefault {
					return nil
				}
			}
		}
		body := &tableWriter{}
		body.u8(kind)
		body.u16(uint16(len(arr)))
		for i, elem := range arr {
			if err := e.encodeTableScalar(body, f, kind, elem, fmt.Sprintf("%s[%d]", fpath, i)); err != nil {
				return err
			}
		}
		w.u16(FieldId(f.Name))
		w.u8(kArray)
		w.u32(uint32(len(body.buf)))
		w.raw(body.buf)
		return nil
	}

	// scalar / string / nested table: compute, elide default
	elide, err := e.isDefault(f, val, fpath)
	if err != nil {
		return err
	}
	if elide {
		return nil
	}
	body := &tableWriter{}
	if err := e.encodeTableScalar(body, f, kind, val, fpath); err != nil {
		return err
	}
	if kind == kTable && len(body.buf) <= 6 { // len prefix + bare terminator: all-default nested
		return nil
	}
	w.u16(FieldId(f.Name))
	w.u8(kind)
	w.raw(body.buf)
	return nil
}

// isDefault reports whether a scalar JSON value equals the field's declared
// default (or zero), so it can elide. Composites and strings report default
// only when absent/empty.
func (e *Encoder) isDefault(f *ir.Field, val any, fpath string) (bool, error) {
	switch f.Type.Kind {
	case ir.TBool:
		if val == nil {
			return true, nil
		}
		b, ok := val.(bool)
		if !ok {
			return false, fmt.Errorf("%s: expected JSON boolean, got %T", fpath, val)
		}
		def := f.HasDefault && f.DefBool
		return b == def, nil
	case ir.TInt, ir.TBits:
		if val == nil {
			return true, nil
		}
		iv, err := intValue(val, f, fpath)
		if err != nil {
			return false, err
		}
		def := big.NewInt(0)
		if f.HasDefault && f.DefInt != nil {
			def = f.DefInt
		}
		return iv.Cmp(def) == 0, nil
	case ir.TFloat32, ir.TFloat64:
		if val == nil {
			return true, nil
		}
		fv, err := floatValue(val, f, fpath)
		if err != nil {
			return false, err
		}
		def := 0.0
		if f.HasDefault {
			def = f.DefFloat
		}
		return fv == def, nil
	case ir.TString:
		s, _ := val.(string)
		return val == nil || s == "", nil
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			if val == nil {
				return true, nil // absent -> reader defaults it to the same value
			}
			ref := f.Type.Ref.(*ir.Enum)
			idx, err := enumValue(ref, f, val, fpath)
			if err != nil {
				return false, err
			}
			var def int64
			if f.HasDefault && f.DefVariant != "" {
				for i, name := range ref.Variants {
					if name == f.DefVariant {
						def = int64(i + 1)
					}
				}
			}
			return idx == def, nil
		case *ir.Flags:
			if val == nil {
				return true, nil
			}
			mask, err := flagsValue(f.Type.Ref.(*ir.Flags), val, fpath)
			return mask == 0, err
		case *ir.Struct:
			return val == nil, nil // nested: encode when present; body may still collapse
		}
	}
	return false, nil
}

// encodeTableScalar writes ONE value's payload (no id/kind header for fixed
// kinds — the caller wrote them; strings/tables carry their own framing).
func (e *Encoder) encodeTableScalar(w *tableWriter, f *ir.Field, kind byte, val any, fpath string) error {
	switch kind {
	case kBool:
		b := false
		if val != nil {
			var ok bool
			if b, ok = val.(bool); !ok {
				return fmt.Errorf("%s: expected JSON boolean, got %T", fpath, val)
			}
		} else if f.HasDefault {
			b = f.DefBool
		}
		if b {
			w.u8(1)
		} else {
			w.u8(0)
		}
	case kI8, kI16, kI32, kI64, kU8, kU16, kU32, kU64:
		var iv *big.Int
		var err error
		if _, isEnum := f.Type.Ref.(*ir.Enum); f.Type.Kind == ir.TNamed && isEnum {
			idx, eerr := enumValue(f.Type.Ref.(*ir.Enum), f, val, fpath)
			if eerr != nil {
				return eerr
			}
			iv = big.NewInt(idx)
		} else if _, isFlags := f.Type.Ref.(*ir.Flags); f.Type.Kind == ir.TNamed && isFlags {
			mask, ferr := flagsValue(f.Type.Ref.(*ir.Flags), val, fpath)
			if ferr != nil {
				return ferr
			}
			iv = new(big.Int).SetUint64(mask)
		} else {
			iv, err = intValue(val, f, fpath)
			if err != nil {
				return err
			}
			if f.HasIntRange && (iv.Cmp(f.IntMin) < 0 || iv.Cmp(f.IntMax) > 0) {
				if !e.ClampBounds {
					return fmt.Errorf("%s: %s outside declared range [%s, %s]", fpath, iv, f.IntMin, f.IntMax)
				}
				clamped := f.IntMax
				if iv.Cmp(f.IntMin) < 0 {
					clamped = f.IntMin
				}
				e.warnf("%s: %s outside declared range [%s, %s] — CLAMPED to %s (the historical LevelInfo semantics; fix the data)", fpath, iv, f.IntMin, f.IntMax, clamped)
				iv = clamped
			}
		}
		width := kindSize(kind)
		u := twosComplement(iv, width*8)
		switch width {
		case 1:
			w.u8(byte(u))
		case 2:
			w.u16(uint16(u))
		case 4:
			w.u32(uint32(u))
		case 8:
			w.u64(u)
		}
	case kF32:
		fv, err := floatValue(val, f, fpath)
		if err != nil {
			return err
		}
		w.u32(math.Float32bits(float32(fv)))
	case kF64:
		fv, err := floatValue(val, f, fpath)
		if err != nil {
			return err
		}
		w.u64(math.Float64bits(fv))
	case kString:
		s := ""
		if val != nil {
			var ok bool
			if s, ok = val.(string); !ok {
				return fmt.Errorf("%s: expected JSON string, got %T", fpath, val)
			}
		}
		if f.Type.Kind == ir.TString && int64(len(s)) > f.Type.Size {
			return fmt.Errorf("%s: %d bytes exceeds string(%d)", fpath, len(s), f.Type.Size)
		}
		w.u16(uint16(len(s)))
		w.raw([]byte(s))
	case kTable:
		sub := map[string]any{}
		if val != nil {
			var ok bool
			if sub, ok = val.(map[string]any); !ok {
				return fmt.Errorf("%s: expected JSON object for %s, got %T", fpath, f.Type.Name, val)
			}
		}
		body, err := e.encodeTableStruct(f.Type.Ref.(*ir.Struct), sub, fpath)
		if err != nil {
			return err
		}
		w.u32(uint32(len(body)))
		w.raw(body)
	default:
		return fmt.Errorf("%s: unencodable kind %d", fpath, kind)
	}
	return nil
}

// ---- decoder (the evolution harness, and any Go reader) ----

// TableReport counts the permissive contract's events.
type TableReport struct {
	Unknown      int // unknown field ids skipped
	KindMismatch int // known id, wrong kind — skipped
	Clamped      int // out-of-range values clamped
	Malformed    bool
}

// DecodeTable decodes TABLE-wire bytes as typeName into a generic map
// (field name -> bool / *big.Int / float64 / string / []any / map).
// Absent fields appear with their DECLARED defaults — the reader's
// prefill-then-overlay contract.
// DecodeTable decodes typeName's table wire generically (the evolution
// harness and any Go tooling). Like EncodeTable, typeName must be in the
// unit's table closure.
func DecodeTable(u *ir.Unit, typeName string, data []byte) (map[string]any, *TableReport, error) {
	st, ok := u.Structs[typeName]
	if !ok {
		return nil, nil, fmt.Errorf("schema type %s: not declared in the unit", typeName)
	}
	if !ir.TableClosure(u)[typeName] {
		return nil, nil, fmt.Errorf("%s is not in the table closure — declare it (or a root that references it) with `table` instead of `type`", typeName)
	}
	rep := &TableReport{}
	out, rest := decodeTableStruct(u, st, data, rep)
	if rest == nil {
		rep.Malformed = true
	}
	return out, rep, nil
}

func decodeTableStruct(u *ir.Unit, st *ir.Struct, data []byte, rep *TableReport) (map[string]any, []byte) {
	byId := map[uint16]*ir.Field{}
	for _, f := range st.Fields {
		byId[FieldId(f.Name)] = f
	}
	out := map[string]any{}
	for _, f := range st.Fields {
		out[f.Name] = defaultValue(u, f)
	}
	for {
		if len(data) < 2 {
			return out, nil
		}
		id := binary.LittleEndian.Uint16(data)
		data = data[2:]
		if id == 0 {
			return out, data
		}
		if len(data) < 1 {
			return out, nil
		}
		kind := data[0]
		data = data[1:]
		f := byId[id]
		var expect byte
		if f != nil {
			expect, _ = scalarKind(f)
			if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
				expect = kArray
			}
		}
		if f == nil || kind != expect {
			if f == nil {
				rep.Unknown++
			} else {
				rep.KindMismatch++
			}
			var ok bool
			data, ok = skipPayload(kind, data)
			if !ok {
				return out, nil
			}
			continue
		}
		var ok bool
		out[f.Name], data, ok = decodePayload(u, f, kind, data, rep)
		if !ok {
			return out, nil
		}
	}
}

func skipPayload(kind byte, data []byte) ([]byte, bool) {
	if n := kindSize(kind); n > 0 {
		if len(data) < n {
			return nil, false
		}
		return data[n:], true
	}
	switch kind {
	case kString:
		if len(data) < 2 {
			return nil, false
		}
		n := int(binary.LittleEndian.Uint16(data))
		if len(data) < 2+n {
			return nil, false
		}
		return data[2+n:], true
	case kTable, kArray:
		if len(data) < 4 {
			return nil, false
		}
		n := int(binary.LittleEndian.Uint32(data))
		if len(data) < 4+n {
			return nil, false
		}
		return data[4+n:], true
	}
	return nil, false
}

func decodePayload(u *ir.Unit, f *ir.Field, kind byte, data []byte, rep *TableReport) (any, []byte, bool) {
	if kind == kArray {
		if len(data) < 4 {
			return nil, nil, false
		}
		n := int(binary.LittleEndian.Uint32(data))
		if len(data) < 4+n {
			return nil, nil, false
		}
		body, rest := data[4:4+n], data[4+n:]
		if len(body) < 3 {
			return defaultValue(u, f), rest, true // no element header: an empty array
		}
		elemKind := body[0]
		count := int(binary.LittleEndian.Uint16(body[1:]))
		body = body[3:]
		expected, err := scalarKind(f)
		if err != nil {
			return nil, nil, false
		}
		if f.Type.Kind == ir.TBytes {
			expected = kU8 // bytes travel as an array of u8
		}
		if elemKind != expected {
			rep.KindMismatch++ // element type changed: keep the default
			return defaultValue(u, f), rest, true
		}
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		if int64(count) > bound {
			rep.Clamped++
			count = int(bound)
		}
		var out []any
		for i := 0; i < count; i++ {
			var v any
			var ok bool
			v, body, ok = decodeScalarPayload(u, f, elemKind, body, rep)
			if !ok {
				return out, rest, true // truncated array: keep what decoded
			}
			out = append(out, v)
		}
		return out, rest, true
	}
	v, rest, ok := decodeScalarPayload(u, f, kind, data, rep)
	return v, rest, ok
}

func decodeScalarPayload(u *ir.Unit, f *ir.Field, kind byte, data []byte, rep *TableReport) (any, []byte, bool) {
	if n := kindSize(kind); n > 0 {
		if len(data) < n {
			return nil, nil, false
		}
		raw := data[:n]
		rest := data[n:]
		clampFloat := func(v float64) float64 {
			if f != nil && f.HasFloatRange {
				if v < f.FMin {
					rep.Clamped++
					return f.FMin
				}
				if v > f.FMax {
					rep.Clamped++
					return f.FMax
				}
			}
			return v
		}
		switch kind {
		case kBool:
			return raw[0] != 0, rest, true
		case kF32:
			return clampFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(raw)))), rest, true
		case kF64:
			return clampFloat(math.Float64frombits(binary.LittleEndian.Uint64(raw))), rest, true
		default:
			var u64 uint64
			switch n {
			case 1:
				u64 = uint64(raw[0])
			case 2:
				u64 = uint64(binary.LittleEndian.Uint16(raw))
			case 4:
				u64 = uint64(binary.LittleEndian.Uint32(raw))
			case 8:
				u64 = binary.LittleEndian.Uint64(raw)
			}
			v := new(big.Int).SetUint64(u64)
			if kind == kI8 || kind == kI16 || kind == kI32 || kind == kI64 {
				width := uint(kindSize(kind) * 8)
				if v.Bit(int(width-1)) == 1 {
					v.Sub(v, new(big.Int).Lsh(big.NewInt(1), width))
				}
			}
			if f != nil && f.HasIntRange {
				if v.Cmp(f.IntMin) < 0 {
					rep.Clamped++
					v = new(big.Int).Set(f.IntMin)
				} else if v.Cmp(f.IntMax) > 0 {
					rep.Clamped++
					v = new(big.Int).Set(f.IntMax)
				}
			}
			if f != nil && f.Type.Kind == ir.TBits {
				max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
				if v.Cmp(max) > 0 { // wire kind is wider than bits(W): width overflow clamps
					rep.Clamped++
					v = max
				}
			}
			if f != nil {
				if enum, isEnum := f.Type.Ref.(*ir.Enum); f.Type.Kind == ir.TNamed && isEnum {
					if v.Sign() < 0 || v.Cmp(big.NewInt(enum.Max)) > 0 { // out-of-set -> None
						rep.Clamped++
						v = big.NewInt(0)
					}
				}
			}
			return v, rest, true
		}
	}
	switch kind {
	case kString:
		if len(data) < 2 {
			return nil, nil, false
		}
		n := int(binary.LittleEndian.Uint16(data))
		if len(data) < 2+n {
			return nil, nil, false
		}
		s := string(data[2 : 2+n])
		if f != nil && f.Type.Kind == ir.TString && int64(len(s)) > f.Type.Size {
			rep.Clamped++
			s = s[:f.Type.Size]
		}
		return s, data[2+n:], true
	case kTable:
		if len(data) < 4 {
			return nil, nil, false
		}
		n := int(binary.LittleEndian.Uint32(data))
		if len(data) < 4+n {
			return nil, nil, false
		}
		var sub map[string]any
		if f != nil {
			if st, ok := f.Type.Ref.(*ir.Struct); ok {
				var rest []byte
				sub, rest = decodeTableStruct(u, st, data[4:4+n], rep)
				if rest == nil {
					rep.Malformed = true
				}
			}
		}
		return sub, data[4+n:], true
	}
	return nil, nil, false
}

// defaultValue builds a field's declared-default generic value.
func defaultValue(u *ir.Unit, f *ir.Field) any {
	if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
		return []any(nil)
	}
	switch f.Type.Kind {
	case ir.TBool:
		return f.HasDefault && f.DefBool
	case ir.TInt, ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			return new(big.Int).Set(f.DefInt)
		}
		return big.NewInt(0)
	case ir.TFloat32, ir.TFloat64:
		if f.HasDefault {
			return f.DefFloat
		}
		return 0.0
	case ir.TString:
		return ""
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				for i, name := range ref.Variants {
					if name == f.DefVariant {
						return big.NewInt(int64(i + 1))
					}
				}
			}
			return big.NewInt(0)
		case *ir.Flags:
			return big.NewInt(0)
		case *ir.Struct:
			sub := map[string]any{}
			for _, sf := range ref.Fields {
				sub[sf.Name] = defaultValue(u, sf)
			}
			return sub
		}
	}
	return nil
}
