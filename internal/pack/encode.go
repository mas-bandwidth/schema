package pack

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/ir"
)

// Encoder packs JSON instance values (decoded via encoding/json: map[string]any,
// []any, float64, string, bool, nil) into schema wire bytes for one unit.
type Encoder struct {
	Unit *ir.Unit
	// UnionLower rewrites flatbuffers-JSON union shapes onto bool-guarded
	// schema arms before encoding — the transition adapter (see manifest.go).
	UnionLower map[string][]UnionRule // schema type name -> rules
	// Renames maps JSON keys onto schema field names per owner type (see
	// manifest.go — `type` is a schema keyword).
	Renames map[string]map[string]string
	// ClampBounds reproduces the game's historical LevelInfo semantics for a
	// collection that opts in: an out-of-range integer CLAMPS to its bound
	// with a loud warning instead of refusing — the level runs exactly as it
	// does today, and the authoring discrepancy becomes visible instead of
	// silent. Strict refusal stays the default.
	ClampBounds bool
	// Warn receives clamp warnings; nil discards them.
	Warn func(format string, args ...any)
}

func (e *Encoder) warnf(format string, args ...any) {
	if e.Warn != nil {
		e.Warn(format, args...)
	}
}

// EncodeInstance encodes one JSON object as schema type typeName, returning
// the wire bytes (byte-aligned at the end, GetBytesProcessed shape).
func (e *Encoder) EncodeInstance(typeName string, obj map[string]any) ([]byte, error) {
	st, ok := e.Unit.Structs[typeName]
	if !ok {
		return nil, fmt.Errorf("schema type %s: not declared in the unit", typeName)
	}
	w := &bitWriter{}
	if err := e.encodeStruct(w, st, obj, typeName); err != nil {
		return nil, err
	}
	return w.bytes(), nil
}

func (e *Encoder) encodeStruct(w *bitWriter, st *ir.Struct, obj map[string]any, path string) error {
	obj, err := e.lowerUnions(st.Name, obj, path)
	if err != nil {
		return err
	}
	// unknown keys are AUTHORING ERRORS — a typo'd field name silently becoming
	// a default is exactly the failure class the compile step exists to refuse
	allowed := map[string]bool{}
	for _, f := range st.Fields {
		allowed[f.Name] = true
	}
	for k := range obj {
		if !allowed[k] {
			return fmt.Errorf("%s: unknown field %q (schema type %s declares no such field)", path, k, st.Name)
		}
	}
	return e.encodeItems(w, st.Items, obj, path)
}

func (e *Encoder) encodeItems(w *bitWriter, items []ir.Item, obj map[string]any, path string) error {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			if err := e.encodeField(w, item.F, obj, path); err != nil {
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
				if err := e.encodeItems(w, taken, obj, path); err != nil {
					return err
				}
			}
		case *ir.ConstItem:
			w.writeBits(item.Value.Uint64(), item.Bits)
		case *ir.ReservedItem:
			w.writeBits(0, item.Bits)
		case *ir.AlignItem:
			w.align()
		}
	}
	return nil
}

// boolField reads a previously-encoded branch guard's value from the object.
func boolField(obj map[string]any, name, path string) (bool, error) {
	v, present := obj[name]
	if !present || v == nil {
		return false, nil // absent bool = zero value = false (defaults on guards
		// would make branch-taking invisible in the JSON; the schema checker
		// does not currently default guards in the corpus)
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%s.%s: branch guard must be a JSON boolean, got %T", path, name, v)
	}
	return b, nil
}

func (e *Encoder) encodeField(w *bitWriter, f *ir.Field, obj map[string]any, path string) error {
	val := obj[f.Name]
	fpath := path + "." + f.Name

	switch f.Array {
	case ir.ArrayFixed, ir.ArrayCounted:
		arr, err := jsonArray(val, fpath)
		if err != nil {
			return err
		}
		n := int64(len(arr))
		if f.Array == ir.ArrayCounted {
			if n < f.ArrayMin || n > f.ArrayBound {
				return fmt.Errorf("%s: %d elements, wire count range is [%d, %d]", fpath, n, f.ArrayMin, f.ArrayBound)
			}
			w.writeBits(uint64(n-f.ArrayMin), ir.BitsRequired(big.NewInt(f.ArrayMin), big.NewInt(f.ArrayBound)))
		} else {
			if n > f.ArrayBound {
				return fmt.Errorf("%s: %d elements exceeds fixed bound %d", fpath, n, f.ArrayBound)
			}
			// fixed arrays serialize all Bound elements; short JSON pads zeros
			for int64(len(arr)) < f.ArrayBound {
				arr = append(arr, nil)
			}
		}
		for i, elem := range arr {
			if err := e.encodeScalar(w, f, elem, fmt.Sprintf("%s[%d]", fpath, i)); err != nil {
				return err
			}
		}
		return nil
	}
	return e.encodeScalar(w, f, val, fpath)
}

func (e *Encoder) encodeScalar(w *bitWriter, f *ir.Field, val any, fpath string) error {
	switch f.Type.Kind {
	case ir.TBool:
		b := false
		if val != nil {
			var ok bool
			if b, ok = val.(bool); !ok {
				return fmt.Errorf("%s: expected JSON boolean, got %T", fpath, val)
			}
		} else if f.HasDefault {
			b = f.DefBool
		}
		bit := uint64(0)
		if b {
			bit = 1
		}
		w.writeBits(bit, 1)
		return nil

	case ir.TInt:
		iv, err := intValue(val, f, fpath)
		if err != nil {
			return err
		}
		if f.HasIntRange {
			iv, err = e.boundInt(iv, f.IntMin, f.IntMax, fpath)
			if err != nil {
				return err
			}
			// value - min, unsigned, in bits_required(min, max) bits — one law
			// at every width. A 128-bit range is the same encoding needing more
			// than one 32-bit group, and where it fits 64 bits the bytes ARE
			// serialize_int64's over the same bounds (SPEC §4.3, STANDARD.md).
			w.writeBigBits(new(big.Int).Sub(iv, f.IntMin), ir.BitsRequired(f.IntMin, f.IntMax))
			return nil
		}
		if f.Type.Width == 128 {
			// uint128 is the raw field: 128 bits, the low 64-bit half first —
			// which is what LSB-first bit order writes (SPEC §4.3). Bare
			// int128 does not exist; the checker refuses it, and a ranged
			// uint128 took the branch above.
			if iv.Sign() < 0 || iv.BitLen() > 128 {
				return fmt.Errorf("%s: %s does not fit uint128", fpath, iv)
			}
			w.writeBigBits(iv, 128)
			return nil
		}
		w.writeBits(twosComplement(iv, f.Type.Width), int64(f.Type.Width))
		return nil

	case ir.TBits:
		iv, err := intValue(val, f, fpath)
		if err != nil {
			return err
		}
		if iv.Sign() < 0 || iv.BitLen() > f.Type.Width {
			return fmt.Errorf("%s: %s does not fit bits(%d)", fpath, iv, f.Type.Width)
		}
		w.writeBits(iv.Uint64(), int64(f.Type.Width))
		return nil

	case ir.TFloat32:
		fv, err := floatValue(val, f, fpath)
		if err != nil {
			return err
		}
		if f.HasFloatRange {
			// serialize_compressed_float's writer, arithmetic for arithmetic:
			// every step is float32 because every runtime narrows the triple to
			// float32 at the call, and the quantized integer must match theirs
			// bit for bit (SPEC §4.3). Out-of-range values CLAMP here rather
			// than refuse — the runtimes clamp, and the wire this encoder
			// speaks for is theirs — but the clamp is a data problem, so it
			// warns.
			maxIntegerValue, wireBits := ir.CompressedFloatParams(f.FMin, f.FMax, f.Resolution)
			fmin, fmax := float32(f.FMin), float32(f.FMax)
			normalized := (float32(fv) - fmin) / (fmax - fmin)
			// the !>= / !<= form is the runtime's own: it forces NaN into range
			// instead of letting it reach the integer conversion
			if !(normalized >= 0.0) {
				normalized = 0.0
				e.warnf("%s: %v is below the declared float range [%v, %v] — CLAMPED (serialize_compressed_float's own write behaviour; fix the data)", fpath, fv, f.FMin, f.FMax)
			} else if !(normalized <= 1.0) {
				normalized = 1.0
				e.warnf("%s: %v is above the declared float range [%v, %v] — CLAMPED (serialize_compressed_float's own write behaviour; fix the data)", fpath, fv, f.FMin, f.FMax)
			}
			// The inner float32() conversion is LOAD BEARING, not decoration. Go
			// permits fusing a multiply and an add into a single FMA unless an
			// explicit conversion forces the intermediate rounding, and arm64
			// takes that permission: without it this line compiles to one FMADDS
			// and rounds ONCE where the runtimes round twice. That is not a
			// rounding nicety — it changes the bytes. compressed_float_value
			// 0.005 over [0, 10] resolution 0.01 quantizes to 0 fused and 1
			// unfused, so the same source packed on arm64 and amd64 would emit
			// different wire. Keep the conversion.
			quantized := uint64(math.Floor(float64(float32(normalized*float32(maxIntegerValue)) + 0.5)))
			w.writeBits(quantized, wireBits)
			return nil
		}
		w.writeBits(uint64(math.Float32bits(float32(fv))), 32)
		return nil

	case ir.TFloat64:
		fv, err := floatValue(val, f, fpath)
		if err != nil {
			return err
		}
		w.writeBits(math.Float64bits(fv), 64)
		return nil

	case ir.TString:
		s := ""
		if val != nil {
			var ok bool
			if s, ok = val.(string); !ok {
				return fmt.Errorf("%s: expected JSON string, got %T", fpath, val)
			}
		}
		if int64(len(s)) > f.Type.Size {
			return fmt.Errorf("%s: %d bytes exceeds string(%d)", fpath, len(s), f.Type.Size)
		}
		for i := 0; i < len(s); i++ {
			if s[i] == 0 {
				return fmt.Errorf("%s: interior 0x00 byte (SPEC §4.7 rejects it)", fpath)
			}
		}
		w.writeBits(uint64(len(s)), ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)))
		w.align()
		for i := 0; i < len(s); i++ {
			w.writeBits(uint64(s[i]), 8)
		}
		return nil

	case ir.TBytes:
		data, err := bytesValue(val, fpath)
		if err != nil {
			return err
		}
		if int64(len(data)) > f.Type.Size {
			return fmt.Errorf("%s: %d bytes exceeds bytes(%d)", fpath, len(data), f.Type.Size)
		}
		w.writeBits(uint64(len(data)), ir.BitsRequired(big.NewInt(0), big.NewInt(f.Type.Size)))
		w.align()
		for _, b := range data {
			w.writeBits(uint64(b), 8)
		}
		return nil

	case ir.TFixed:
		if !f.HasIntRange {
			// only a [local] field reaches no wire, and no [local] field is
			// ever encoded — so this is a compiler bug, not a data error
			spelling := "fixed"
			if !f.Type.Signed {
				spelling = "ufixed"
			}
			return fmt.Errorf("%s: %s(%d, %d) carries no [min, max] — the whole-unit bounds are part of the wire format (SPEC §4.3)",
				fpath, spelling, f.Type.IntBits, f.Type.FracBits)
		}
		raw, err := e.fixedRaw(f, val, fpath)
		if err != nil {
			return err
		}
		// the raw (scaled) bounds are the whole-unit bounds shifted by F, and
		// the wire is the raw offset in bitlen(rawMax - rawMin) bits — which is
		// bitlen(max - min) + F, the width the backends advertise (SPEC §4.3,
		// STANDARD.md fixed). F = 0 makes this literally a ranged integer.
		rawMin := new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits))
		rawMax := new(big.Int).Lsh(f.IntMax, uint(f.Type.FracBits))
		w.writeBigBits(new(big.Int).Sub(raw, rawMin), ir.BitsRequired(rawMin, rawMax))
		return nil

	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			idx, err := enumValue(ref, f, val, fpath)
			if err != nil {
				return err
			}
			w.writeBits(uint64(idx), ir.BitsRequired(big.NewInt(0), big.NewInt(ref.Max)))
			return nil
		case *ir.Flags:
			mask, err := flagsValue(ref, val, fpath)
			if err != nil {
				return err
			}
			w.writeBits(mask, int64(ref.WireBits))
			return nil
		case *ir.Struct:
			sub := map[string]any{}
			if val != nil {
				var ok bool
				if sub, ok = val.(map[string]any); !ok {
					return fmt.Errorf("%s: expected JSON object for %s, got %T", fpath, f.Type.Name, val)
				}
			}
			return e.encodeStruct(w, ref, sub, fpath)
		case *ir.Union:
			return e.encodeUnion(w, ref, val, fpath)
		}
	}
	return fmt.Errorf("%s: unhandled field kind", fpath)
}

// encodeUnion packs a union value (SPEC §4.8): a single-key JSON object, the
// key naming the variant in its source spelling; null or absent is None. The
// key's value must be an object — null under a key is a refusal, as is zero
// keys or more than one. A one-of holds one thing, and the encoder does not
// guess which.
func (e *Encoder) encodeUnion(w *bitWriter, u *ir.Union, val any, fpath string) error {
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(u.Max))
	if val == nil {
		if bits > 0 {
			w.writeBits(0, bits) // None — the tag is the whole wire
		}
		return nil
	}
	obj, ok := val.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: union %s takes a single-key object { \"<variant>\": { ... } } or null, got %T (SPEC §4.8)", fpath, u.Name, val)
	}
	if len(obj) != 1 {
		return fmt.Errorf("%s: union %s takes exactly one variant key, got %d — a one-of holds one thing (SPEC §4.8)", fpath, u.Name, len(obj))
	}
	for key, payload := range obj {
		for i, v := range u.Variants {
			if v.Name != key {
				continue
			}
			sub, ok := payload.(map[string]any)
			if !ok {
				return fmt.Errorf("%s.%s: a variant's value must be a JSON object; null spells None only at the union itself (SPEC §4.8)", fpath, key)
			}
			w.writeBits(uint64(i+1), bits)
			return e.encodeStruct(w, v.Ref, sub, fpath+"."+key)
		}
		return fmt.Errorf("%s: %q is not a variant of union %s", fpath, key, u.Name)
	}
	return nil // unreachable: len(obj) == 1
}

// boundInt enforces a declared wire range, honouring ClampBounds: strict
// refusal by default, clamp-with-a-warning for a collection that opts into the
// historical LevelInfo semantics.
func (e *Encoder) boundInt(v, min, max *big.Int, fpath string) (*big.Int, error) {
	if v.Cmp(min) >= 0 && v.Cmp(max) <= 0 {
		return v, nil
	}
	if !e.ClampBounds {
		return nil, fmt.Errorf("%s: %s outside wire range [%s, %s]", fpath, v, min, max)
	}
	clamped := max
	if v.Cmp(min) < 0 {
		clamped = min
	}
	e.warnf("%s: %s outside wire range [%s, %s] — CLAMPED to %s (the historical LevelInfo semantics; fix the data)", fpath, v, min, max, clamped)
	return clamped, nil
}

// fixedRaw resolves a fixed(I, F) field's JSON value to its RAW scaled
// integer — the value the wire carries an offset of.
//
// A JSON value is in WHOLE UNITS, the same domain as the field's [min, max]
// and its specified default (SPEC §4.6: "declared in WHOLE UNITS ... so no
// raw/units confusion is possible"). Units × 2^F is rounded to nearest, half
// away from zero — the ONE fixed-point rounding rule (SPEC §4.8, decided
// 2026-08-15), which the generated shallow-narrowing Quantize implements as
// the sign-mirrored shift since the same ruling (before it, the emitters
// shipped ties-toward-+infinity and this sentence was wrong about them —
// the 08-14 audit's fork 6). A DEFAULT must scale exactly and the
// checker enforces that, because a default is source text whose author can
// always pick a representable value; data is data, and a fixed field's Q
// format IS its declared precision, exactly as float32 storage is a float
// field's.
func (e *Encoder) fixedRaw(f *ir.Field, val any, fpath string) (*big.Int, error) {
	if val == nil && f.HasDefault && f.DefInt != nil {
		return f.DefInt, nil // already the raw scaled integer, already in range
	}
	units, err := ratValue(val, fpath)
	if err != nil {
		return nil, err
	}
	units, err = e.boundFixedUnits(units, f, fpath)
	if err != nil {
		return nil, err
	}
	scaled := new(big.Rat).Mul(units, new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.FracBits))))
	return ratRoundHalfAway(scaled), nil
}

// boundFixedUnits range-checks a fixed value in the whole-unit domain, where
// the bounds are written and where an error message means something. Checking
// before the scaling is sound: min and max scale to the raw bounds exactly, so
// a value inside the unit range cannot round outside the raw range.
func (e *Encoder) boundFixedUnits(v *big.Rat, f *ir.Field, fpath string) (*big.Rat, error) {
	min := new(big.Rat).SetInt(f.IntMin)
	max := new(big.Rat).SetInt(f.IntMax)
	if v.Cmp(min) >= 0 && v.Cmp(max) <= 0 {
		return v, nil
	}
	if !e.ClampBounds {
		return nil, fmt.Errorf("%s: %s outside wire range [%s, %s] (whole units)", fpath, v.FloatString(6), f.IntMin, f.IntMax)
	}
	clamped := max
	if v.Cmp(min) < 0 {
		clamped = min
	}
	e.warnf("%s: %s outside wire range [%s, %s] (whole units) — CLAMPED to %s (the historical LevelInfo semantics; fix the data)", fpath, v.FloatString(6), f.IntMin, f.IntMax, clamped.FloatString(6))
	return clamped, nil
}

// ratRoundHalfAway rounds a rational to the nearest integer, halves away from
// zero: floor((2|n| + d) / 2d), sign restored.
func ratRoundHalfAway(r *big.Rat) *big.Int {
	num := new(big.Int).Abs(r.Num())
	den := r.Denom()
	q := new(big.Int).Lsh(num, 1)
	q.Add(q, den)
	q.Div(q, new(big.Int).Lsh(den, 1))
	if r.Sign() < 0 {
		q.Neg(q)
	}
	return q
}

// ratValue reads a JSON number EXACTLY: json.Number carries the literal text,
// so a decimal that a float64 could only approximate still scales exactly onto
// the fixed-point grid. float64 is accepted for programmatic callers.
func ratValue(val any, fpath string) (*big.Rat, error) {
	switch v := val.(type) {
	case nil:
		return new(big.Rat), nil
	case json.Number:
		r, ok := new(big.Rat).SetString(v.String())
		if !ok {
			return nil, fmt.Errorf("%s: %s is not a number", fpath, v)
		}
		return r, nil
	case float64:
		r := new(big.Rat).SetFloat64(v)
		if r == nil {
			return nil, fmt.Errorf("%s: %v is not a finite number", fpath, v)
		}
		return r, nil
	}
	return nil, fmt.Errorf("%s: expected JSON number, got %T", fpath, val)
}

// twosComplement maps a big.Int onto width bits, negative values in two's
// complement — the raw storage-width wire (SPEC §4.3).
func twosComplement(v *big.Int, width int) uint64 {
	u := new(big.Int).Set(v)
	if u.Sign() < 0 {
		u.Add(u, new(big.Int).Lsh(big.NewInt(1), uint(width)))
	}
	if width >= 64 {
		return u.Uint64()
	}
	return u.Uint64() & ((1 << uint(width)) - 1)
}

func jsonArray(val any, fpath string) ([]any, error) {
	if val == nil {
		return nil, nil
	}
	arr, ok := val.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected JSON array, got %T", fpath, val)
	}
	return arr, nil
}

// intValue resolves a JSON value (or the field's default, or zero) to an
// integer, rejecting non-integral numbers — 1.5 in an int field is an
// authoring error, never a truncation. json.Number is the decode form
// (loadJSON uses UseNumber), so full-range uint64 values survive exactly;
// float64 is accepted for programmatic callers.
func intValue(val any, f *ir.Field, fpath string) (*big.Int, error) {
	switch v := val.(type) {
	case nil:
		if f.HasDefault && f.DefInt != nil {
			return f.DefInt, nil
		}
		return big.NewInt(0), nil
	case json.Number:
		iv, ok := new(big.Int).SetString(v.String(), 10)
		if !ok {
			return nil, fmt.Errorf("%s: %s is not an integer", fpath, v)
		}
		return iv, nil
	case float64:
		iv := int64(v)
		if float64(iv) != v {
			return nil, fmt.Errorf("%s: %v is not an integer", fpath, v)
		}
		return big.NewInt(iv), nil
	}
	return nil, fmt.Errorf("%s: expected JSON number, got %T", fpath, val)
}

func floatValue(val any, f *ir.Field, fpath string) (float64, error) {
	switch v := val.(type) {
	case nil:
		if f.HasDefault {
			return f.DefFloat, nil
		}
		return 0, nil
	case json.Number:
		n, err := v.Float64()
		if err != nil {
			return 0, fmt.Errorf("%s: %s is not a number: %w", fpath, v, err)
		}
		return n, nil
	case float64:
		return v, nil
	}
	return 0, fmt.Errorf("%s: expected JSON number, got %T", fpath, val)
}

// bytesValue accepts an array of byte-valued numbers or a base64 string.
func bytesValue(val any, fpath string) ([]byte, error) {
	switch v := val.(type) {
	case nil:
		return nil, nil
	case string:
		data, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, fmt.Errorf("%s: not valid base64: %w", fpath, err)
		}
		return data, nil
	case []any:
		out := make([]byte, len(v))
		for i, elem := range v {
			n, err := numberValue(elem)
			if err != nil || n < 0 || n > 255 {
				return nil, fmt.Errorf("%s[%d]: not a byte value", fpath, i)
			}
			out[i] = byte(n)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s: expected byte array or base64 string, got %T", fpath, val)
}

// numberValue reads a small non-negative integer from either decode form.
func numberValue(val any) (int64, error) {
	switch v := val.(type) {
	case json.Number:
		return v.Int64()
	case float64:
		iv := int64(v)
		if float64(iv) != v {
			return 0, fmt.Errorf("not an integer")
		}
		return iv, nil
	}
	return 0, fmt.Errorf("not a number")
}

// enumValue resolves a variant name string (the flatbuffers-JSON idiom), a
// number, or the field default to the wire value: None = 0, variants from 1.
func enumValue(ref *ir.Enum, f *ir.Field, val any, fpath string) (int64, error) {
	switch v := val.(type) {
	case nil:
		if f.HasDefault && f.DefVariant != "" {
			for i, name := range ref.Variants {
				if name == f.DefVariant {
					return int64(i + 1), nil
				}
			}
		}
		return 0, nil // None
	case string:
		if v == "None" {
			return 0, nil
		}
		for i, name := range ref.Variants {
			if name == v {
				return int64(i + 1), nil
			}
		}
		return 0, fmt.Errorf("%s: %q is not a variant of enum %s", fpath, v, ref.Name)
	case json.Number, float64:
		iv, err := numberValue(v)
		if err != nil || iv < 0 || iv > ref.Max {
			return 0, fmt.Errorf("%s: %v is not a valid %s wire value [0, %d]", fpath, v, ref.Name, ref.Max)
		}
		return iv, nil
	}
	return 0, fmt.Errorf("%s: expected enum variant name or number, got %T", fpath, val)
}

// flagsValue resolves an array of variant names or a number to the mask.
func flagsValue(ref *ir.Flags, val any, fpath string) (uint64, error) {
	switch v := val.(type) {
	case nil:
		return 0, nil
	case json.Number, float64:
		iv, err := numberValue(v)
		if err != nil || iv < 0 {
			return 0, fmt.Errorf("%s: %v is not an integer mask", fpath, v)
		}
		return uint64(iv), nil
	case []any:
		var mask uint64
		for _, elem := range v {
			name, ok := elem.(string)
			if !ok {
				return 0, fmt.Errorf("%s: flags array elements must be variant names", fpath)
			}
			found := false
			for i, variant := range ref.Variants {
				if variant == name {
					mask |= 1 << uint(i)
					found = true
					break
				}
			}
			if !found {
				return 0, fmt.Errorf("%s: %q is not a variant of flags %s", fpath, name, ref.Name)
			}
		}
		return mask, nil
	}
	return 0, fmt.Errorf("%s: expected flags mask or variant-name array, got %T", fpath, val)
}
