package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	mathbits "math/bits"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// This scanner reads the announcement and batch directly. It uses declaration
// metadata for identity/defaults, but no tablewire reader or decoded instance.
// Its errors distinguish damage and unsupported layouts from a clean spelling.
type sharedMessageDamage struct{ Cause error }

func (e *sharedMessageDamage) Error() string { return e.Cause.Error() }
func (e *sharedMessageDamage) Unwrap() error { return e.Cause }

type sharedMessageUnsupported struct{ Detail string }

func (e *sharedMessageUnsupported) Error() string { return e.Detail }

func sharedMessageUnsupportedf(format string, args ...any) error {
	return &sharedMessageUnsupported{fmt.Sprintf(format, args...)}
}

type sharedMessageBytes struct {
	data []byte
	at   int
}

func (r *sharedMessageBytes) take(n int) ([]byte, error) {
	if n < 0 || n > len(r.data)-r.at {
		return nil, fmt.Errorf("truncated announcement at byte %d", r.at)
	}
	p := r.data[r.at : r.at+n]
	r.at += n
	return p, nil
}

func (r *sharedMessageBytes) leb() (uint64, error) {
	v, n := binary.Uvarint(r.data[r.at:])
	if n <= 0 || (n > 1 && r.data[r.at+n-1] == 0) {
		return 0, fmt.Errorf("invalid announcement LEB128 at byte %d", r.at)
	}
	r.at += n
	return v, nil
}

func (r *sharedMessageBytes) octet() (byte, error) {
	p, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return p[0], nil
}

// sharedMessageShape implements the metadata grammar in SPEC-TABLES §3.3,
// independently of ir.DecodeShape and the engine's announcement reader.
func sharedMessageShape(r *sharedMessageBytes, kind uint8, depth int) (ir.TableMessageShape, error) {
	var s ir.TableMessageShape
	if depth > 8 {
		return s, sharedMessageUnsupportedf("unsupported recursive announcement shape")
	}
	numeric := kind >= 2 && kind <= 10 || kind >= 18 && kind <= 29
	if numeric {
		packing, err := r.octet()
		if err != nil {
			return s, err
		}
		s.Packing = packing
		switch packing {
		case 0:
			return s, nil
		case 1:
			if kind == ir.TableKindF32 {
				return s, sharedMessageUnsupportedf("unsupported ranged f32 shape")
			}
			n, err := r.leb()
			if err != nil || n > uint64(ir.TableKindWidth(int(kind))*8) {
				return s, fmt.Errorf("invalid numeric width for kind %d: %v", kind, err)
			}
			s.Bits = int64(n)
			if kind >= 18 {
				p, err := r.take(16)
				if err != nil {
					return s, err
				}
				be := append([]byte(nil), p...)
				for i, j := 0, 15; i < j; i, j = i+1, j-1 {
					be[i], be[j] = be[j], be[i]
				}
				s.Base = new(big.Int).SetBytes(be)
				if ir.TableKindSigned(int(kind)) && p[15]&128 != 0 {
					s.Base.Sub(s.Base, new(big.Int).Lsh(big.NewInt(1), 128))
				}
			} else {
				v, err := r.leb()
				if err != nil {
					return s, err
				}
				s.Base = new(big.Int).SetUint64(v)
				if kind <= 5 {
					s.Base.SetInt64(int64(v>>1) ^ -int64(v&1))
				}
			}
			return s, nil
		case 2:
			if kind != ir.TableKindF32 {
				return s, sharedMessageUnsupportedf("unsupported packing 2 for kind %d", kind)
			}
			p, err := r.take(12)
			if err != nil {
				return s, err
			}
			s.QMin = math.Float32frombits(binary.LittleEndian.Uint32(p))
			s.QMax = math.Float32frombits(binary.LittleEndian.Uint32(p[4:]))
			s.QRes = math.Float32frombits(binary.LittleEndian.Uint32(p[8:]))
			count, err := sharedMessageQuantization(s)
			if err != nil {
				return s, err
			}
			s.Bits = int64(mathbits.Len64(count))
			return s, nil
		default:
			return s, sharedMessageUnsupportedf("unsupported numeric packing %d", packing)
		}
	}
	switch kind {
	case 0, 1, 11, 13, 15, 17, 30, 31, 32, 36:
		return s, nil
	case 12:
		n, err := r.leb()
		if err != nil || n > math.MaxInt32 {
			return s, fmt.Errorf("invalid string capacity: %v", err)
		}
		s.Max = int64(n)
		return s, nil
	case 14, 16:
		if kind == 14 {
			n, err := r.leb()
			if err != nil || n > math.MaxUint32 {
				return s, fmt.Errorf("invalid array minimum: %v", err)
			}
			s.Min = int64(n)
		}
		n, err := r.leb()
		if err != nil || n > math.MaxUint32 || int64(n) < s.Min {
			return s, fmt.Errorf("invalid collection bound: %v", err)
		}
		s.Max = int64(n)
		s.Elem, err = r.octet()
		if err != nil {
			return s, err
		}
		if s.Elem == 14 || s.Elem == 16 {
			return s, sharedMessageUnsupportedf("unsupported collection element %d", s.Elem)
		}
		inner, err := sharedMessageShape(r, s.Elem, depth+1)
		s.Inner = &inner
		return s, err
	default:
		return s, sharedMessageUnsupportedf("unsupported announced kind %d", kind)
	}
}

func sharedMessageQuantization(s ir.TableMessageShape) (uint64, error) {
	delta := float32(s.QMax - s.QMin)
	ratio := float32(delta / s.QRes)
	if !(s.QMin < s.QMax) || !(s.QRes > 0) || math.IsInf(float64(delta), 0) || math.IsInf(float64(ratio), 0) || math.IsNaN(float64(ratio)) {
		return 0, fmt.Errorf("invalid quantized shape")
	}
	return uint64(math.Min(4294967040, math.Max(1, math.Ceil(float64(ratio))))), nil
}

// An announcement is an ordinary file: resolve its two reserved fields by the
// trailer ids, not their usual slot numbers, and permit additional fields.
func sharedMessageAnnouncement(data []byte) (uint64, []ir.TableVocabularyEntry, error) {
	if len(data) < 10 || data[0] != ir.TableWireForm {
		return 0, nil, fmt.Errorf("not a file-form announcement")
	}
	n := binary.LittleEndian.Uint64(data[len(data)-8:])
	if n > uint64((len(data)-9)/8) {
		return 0, nil, fmt.Errorf("announcement trailer crosses body")
	}
	end := len(data) - 8 - int(n)*8
	r := &sharedMessageBytes{data: data[1:end]}
	var version uint64
	var vocabulary []byte
	haveVersion, haveVocabulary := false, false
	for {
		ref, err := r.leb()
		if err != nil {
			return 0, nil, err
		}
		if ref == 0 {
			break
		}
		if ref > n {
			return 0, nil, fmt.Errorf("announcement reference above trailer")
		}
		kind, err := r.octet()
		if err != nil {
			return 0, nil, err
		}
		var p []byte
		switch {
		case kind == 17:
			_, err = r.leb()
		case ir.TableKindWidth(int(kind)) > 0:
			p, err = r.take(ir.TableKindWidth(int(kind)))
		case kind >= 12 && kind <= 16 || kind == 31:
			var size uint64
			size, err = r.leb()
			if err == nil && size <= uint64(len(r.data)-r.at) {
				p, err = r.take(int(size))
			} else {
				err = fmt.Errorf("announcement payload crosses extent")
			}
		case kind != 32:
			err = sharedMessageUnsupportedf("unsupported announcement field kind %d", kind)
		}
		if err != nil {
			return 0, nil, err
		}
		id := binary.LittleEndian.Uint64(data[end+int(ref-1)*8:])
		switch id {
		case ir.TableBuildVersionWireId:
			if haveVersion || kind != 9 || len(p) != 8 {
				return 0, nil, fmt.Errorf("invalid announcement version field")
			}
			version, haveVersion = binary.LittleEndian.Uint64(p), true
		case ir.TableMessageVocabularyWireId:
			v := &sharedMessageBytes{data: p}
			elem, e := v.octet()
			count, e2 := v.leb()
			if haveVocabulary || kind != 14 || e != nil || e2 != nil || elem != 6 || count != uint64(len(p)-v.at) {
				return 0, nil, fmt.Errorf("invalid announcement vocabulary field")
			}
			vocabulary, haveVocabulary = p[v.at:], true
		}
	}
	if !haveVersion || !haveVocabulary || r.at != len(r.data) {
		return 0, nil, fmt.Errorf("announcement lacks a required field or has trailing bytes")
	}
	v := &sharedMessageBytes{data: vocabulary}
	var entries []ir.TableVocabularyEntry
	seen := map[string]bool{}
	for v.at < len(v.data) {
		p, err := v.take(9)
		if err != nil {
			return 0, nil, err
		}
		e := ir.TableVocabularyEntry{Id: binary.LittleEndian.Uint64(p), Kind: p[8]}
		e.Shape, err = sharedMessageShape(v, e.Kind, 0)
		if err != nil {
			return 0, nil, err
		}
		if seen[e.Key()] {
			return 0, nil, fmt.Errorf("duplicate announcement triple")
		}
		seen[e.Key()] = true
		entries = append(entries, e)
	}
	return version, entries, nil
}

type sharedMessageBits struct {
	data []byte
	at   int
}

func (r *sharedMessageBits) number(n int) (*big.Int, error) {
	if n < 0 || n > 128 || n > len(r.data)*8-r.at {
		return nil, fmt.Errorf("truncated message value at bit %d (width %d)", r.at, n)
	}
	v := new(big.Int)
	for i := range n {
		v.SetBit(v, i, uint(r.data[r.at/8]>>uint(r.at%8)&1))
		r.at++
	}
	return v, nil
}

func (r *sharedMessageBits) small(n int) (uint64, error) {
	v, err := r.number(n)
	if err != nil {
		return 0, err
	}
	return v.Uint64(), nil
}

func (r *sharedMessageBits) align() error {
	for r.at%8 != 0 {
		v, err := r.small(1)
		if err != nil || v != 0 {
			return fmt.Errorf("nonzero or missing message padding at bit %d", r.at)
		}
	}
	return nil
}

func (r *sharedMessageBits) bytes(n uint64) ([]byte, error) {
	if err := r.align(); err != nil {
		return nil, err
	}
	if n > uint64(len(r.data)-r.at/8) {
		return nil, fmt.Errorf("message bytes cross batch")
	}
	p := r.data[r.at/8 : r.at/8+int(n)]
	r.at += int(n) * 8
	return p, nil
}

type sharedMessageWalk struct {
	r       *sharedMessageBits
	entries []ir.TableVocabularyEntry
	refBits int
	same    bool
	shapes  map[string]bool
}

func (w *sharedMessageWalk) reference() (uint64, error) {
	v, err := w.r.small(w.refBits)
	if err != nil || v > uint64(len(w.entries)) {
		return 0, fmt.Errorf("invalid message reference at bit %d: %v", w.r.at, err)
	}
	return v, nil
}

func (w *sharedMessageWalk) body(st *ir.Struct, depth int) (bool, error) {
	if depth > 256 {
		return false, sharedMessageUnsupportedf("unsupported message nesting past 256")
	}
	fields, order := map[uint64]*ir.Field{}, map[uint64]int{}
	if st != nil {
		for i, f := range st.Fields {
			if f.Required {
				return false, sharedMessageUnsupportedf("unsupported required field in a fixed message declaration")
			}
			if f.Guard != "" {
				return false, sharedMessageUnsupportedf("unsupported guarded message field %s.%s", st.Name, f.Name)
			}
			fields[ir.TableFieldWireId(f)], order[ir.TableFieldWireId(f)] = f, i
		}
	}
	seen := map[uint64]bool{}
	last, count := -1, 0
	for {
		ref, err := w.reference()
		if err != nil {
			return false, err
		}
		if ref == 0 {
			break
		}
		e := w.entries[ref-1]
		if seen[e.Id] {
			return false, fmt.Errorf("duplicate message field %016x", e.Id)
		}
		seen[e.Id], count = true, count+1
		f := fields[e.Id]
		if w.same && st != nil {
			if f == nil {
				w.shapes["same-build-unknown-field"] = true
			} else {
				if order[e.Id] < last {
					w.shapes["fields-out-of-order"] = true
				}
				last = order[e.Id]
			}
		}
		matching := f != nil && ir.TableFieldEntry(f).Key() == e.Key()
		bound := f
		if !matching || f.Deprecated {
			bound = nil
		}
		atDefault, err := w.payload(e, bound, depth+1)
		if err != nil {
			return false, err
		}
		// None is forbidden only in a FIELD position, including a field
		// unknown to an older build. Fixed message declarations cannot be
		// required. Array elements and union-arm payloads use payload directly
		// and do not pass this field-only check.
		if e.Kind == ir.TableKindUnion && atDefault {
			w.shapes["union-arm-zero"] = true
		} else if w.same && matching && !f.Deprecated && !f.Type.Optional && atDefault {
			w.shapes["field-at-default"] = true
		}
	}
	return count == 0, nil
}

func (w *sharedMessageWalk) payload(e ir.TableVocabularyEntry, f *ir.Field, depth int) (bool, error) {
	s := e.Shape
	width := -1
	switch {
	case e.Kind == 1:
		width = 1
	case e.Kind == 11 || e.Kind == 36:
		width = 64
	case e.Kind == 10:
		width = 32
		if s.Packing == 2 {
			width = int(s.Bits)
		}
	case e.Kind >= 2 && e.Kind <= 9 || e.Kind >= 18 && e.Kind <= 29:
		width = ir.TableKindWidth(int(e.Kind)) * 8
		if s.Packing == 1 {
			width = int(s.Bits)
		}
	}
	if width >= 0 {
		value, err := w.r.number(width)
		if err != nil || f == nil {
			return false, err
		}
		want := new(big.Int)
		switch f.Type.Kind {
		case ir.TBool:
			if f.DefBool {
				want.SetInt64(1)
			}
		case ir.TFloat32:
			if s.Packing == 2 {
				count, err := sharedMessageQuantization(s)
				if err != nil {
					return false, err
				}
				if !value.IsUint64() || value.Uint64() > count {
					return false, fmt.Errorf("quantized message index above count")
				}
				delta := float32(s.QMax - s.QMin)
				q := float32(float32(value.Uint64()) / float32(count))
				decoded := float32(float32(q*delta) + s.QMin)
				return decoded == float32(f.DefFloat), nil
			} else {
				want.SetUint64(uint64(math.Float32bits(float32(f.DefFloat))))
			}
		case ir.TFloat64:
			want.SetUint64(math.Float64bits(f.DefFloat))
		default:
			if f.DefInt != nil {
				want.Set(f.DefInt)
			}
			if s.Packing == 1 {
				want.Sub(want, s.Base)
			} else if want.Sign() < 0 {
				want.Add(want, new(big.Int).Lsh(big.NewInt(1), uint(width)))
			}
		}
		return value.Cmp(want) == 0, nil
	}
	switch e.Kind {
	case 12:
		n, err := w.r.small(mathbits.Len64(uint64(s.Max)))
		if err != nil || n > uint64(s.Max) {
			return false, fmt.Errorf("invalid message string length: %v", err)
		}
		p, err := w.r.bytes(n)
		return f != nil && bytes.Equal(p, f.DefBytes), err
	case 13:
		var st *ir.Struct
		if f != nil {
			st, _ = f.Type.Ref.(*ir.Struct)
		}
		return w.body(st, depth)
	case 14, 16:
		n, err := w.r.small(mathbits.Len64(uint64(s.Max - s.Min)))
		n += uint64(s.Min)
		if err != nil || n > uint64(s.Max) {
			return false, fmt.Errorf("invalid message collection count %d: %v", n, err)
		}
		if n > 1<<20 {
			return false, sharedMessageUnsupportedf("unsupported message collection count %d past walk limit", n)
		}
		var element *ir.Field
		if f != nil {
			copy := *f
			copy.Array, copy.KeyEnum, copy.KeyEnumRef = ir.ArrayNone, "", nil
			copy.Type.Optional = false
			if f.Type.Kind == ir.TBytes {
				copy.Type = ir.FieldType{Kind: ir.TInt, Width: 8}
			}
			element = &copy
		}
		if e.Kind == 14 && s.Elem == 6 {
			if err := w.r.align(); err != nil {
				return false, err
			}
		}
		allDefault := true
		for range n {
			if e.Kind == 16 {
				ref, err := w.reference()
				if err != nil || ref == 0 || w.entries[ref-1].Kind != 0 {
					return false, fmt.Errorf("invalid keyed message reference: %v", err)
				}
			}
			dflt, err := w.payload(ir.TableVocabularyEntry{Kind: s.Elem, Shape: *s.Inner}, element, depth+1)
			if err != nil {
				return false, err
			}
			allDefault = allDefault && dflt
			if e.Kind == ir.TableKindKeyed && f != nil && w.same && dflt {
				w.shapes["field-at-default"] = true
			}
		}
		if f == nil {
			return false, nil
		}
		if f.Type.Kind == ir.TBytes {
			// Byte buffers have an independent specified default, not an
			// element default. Compare the payload without decoding a value.
			// The loop above already checked the framing; inspect its bytes.
			end := w.r.at / 8
			return bytes.Equal(w.r.data[end-int(n):end], f.DefBytes), nil
		}
		if e.Kind == 16 || f.Array != ir.ArrayFixed {
			return n == 0, nil
		}
		return s.Elem != ir.TableKindTable && allDefault, nil
	case 15:
		ref, err := w.reference()
		if err != nil || ref == 0 {
			return ref == 0, err
		}
		arm := w.entries[ref-1]
		var field *ir.Field
		if f != nil {
			if union, ok := f.Type.Ref.(*ir.Union); ok {
				for _, v := range union.Variants {
					if ir.TableArmEntry(v).Key() == arm.Key() {
						field = v.F
					}
				}
			}
		}
		if arm.Kind == 0 {
			return false, fmt.Errorf("union arm reference names a variant")
		}
		_, err = w.payload(arm, field, depth+1)
		return false, err
	case 30:
		ref, err := w.reference()
		if err != nil || ref != 0 && w.entries[ref-1].Kind != 0 {
			return false, fmt.Errorf("invalid enum reference: %v", err)
		}
		if f == nil {
			return false, nil
		}
		if f.DefVariant == "" || f.DefVariant == "None" {
			return ref == 0, nil
		}
		return ref != 0 && w.entries[ref-1].Id == ir.TableWireId(f.DefVariant), nil
	case 31:
		if err := w.r.align(); err != nil {
			return false, err
		}
		n, err := w.r.small(32)
		if err != nil {
			return false, err
		}
		_, err = w.r.bytes(n)
		return false, err
	case 32:
		return false, nil
	case 17:
		return false, sharedMessageUnsupportedf("unsupported pointer in a message body: fixed roots number no nodes")
	default:
		return false, sharedMessageUnsupportedf("unsupported message payload kind %d", e.Kind)
	}
}

func messageStructuralSpellings(unit *ir.Unit, root *ir.Struct, announcement, batch []byte) (shapes map[string]bool, err error) {
	// A damaged suffix can masquerade as field references before the walk
	// detects damage. Never certify any provisional spelling from a failed walk.
	defer func() {
		if err != nil {
			shapes = nil
			var unsupported *sharedMessageUnsupported
			if !errors.As(err, &unsupported) {
				err = &sharedMessageDamage{err}
			}
		}
	}()
	version, entries, err := sharedMessageAnnouncement(announcement)
	if err != nil {
		return nil, err
	}
	if root == nil || !root.FixedDeclared {
		return nil, sharedMessageUnsupportedf("unsupported variable root on message wire")
	}
	if len(batch) < 2 || batch[0] != ir.TableWireMessageForm {
		return nil, fmt.Errorf("not a message batch")
	}
	w := &sharedMessageWalk{r: &sharedMessageBits{data: batch, at: 16}, entries: entries,
		refBits: max(1, mathbits.Len(uint(len(entries)))), same: version == ir.BuildVersion(unit), shapes: map[string]bool{}}
	for range int(batch[1]) + 1 {
		if _, err := w.body(root, 0); err != nil {
			return w.shapes, err
		}
	}
	if err := w.r.align(); err != nil {
		return w.shapes, err
	}
	if w.r.at != len(batch)*8 {
		return w.shapes, fmt.Errorf("bytes after message batch")
	}
	return w.shapes, nil
}

func TestSharedMessageManifestSpellings(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, path := range m.Units[i].Paths {
			if !filepath.IsAbs(path) {
				m.Units[i].Paths[j] = filepath.Join(root, path)
			}
		}
	}
	units := newUnits(m)
	read, absent := 0, 0
	for _, row := range m.Messages {
		connection, err := m.LookupConnection(row.Connection)
		if err != nil {
			t.Fatal(err)
		}
		u, err := units.get(connection.Unit)
		if err != nil {
			t.Fatal(err)
		}
		st := u.Tables[row.Root]
		if row.Refused() {
			if st == nil || st.FixedDeclared {
				t.Errorf("%s: no-wire refusal does not name a variable root", row.Name)
			}
			absent++
			continue
		}
		announcement, err := os.ReadFile(filepath.Join(root, connection.Wire))
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(root, row.MessageWire))
		if err != nil {
			t.Fatal(err)
		}
		shapes, err := messageStructuralSpellings(u, st, announcement, data)
		if err != nil || len(shapes) != 0 {
			t.Errorf("%s (%s): spellings=%v error=%v", row.Name, row.MessageWire, shapes, err)
		}
		read++
	}
	t.Logf("classified %d readable message batches and accounted for %d no-wire refusals", read, absent)
	if read == 0 || absent == 0 {
		t.Fatalf("message coverage: %d readable batches, %d explicit no-wire refusals", read, absent)
	}
}

// A damaged bytes suffix can resemble another vocabulary reference. Markers
// observed before structural validation finishes are not certified spellings.
func TestSharedMessageDamageDiscardsProvisionalSpellings(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, path := range m.Units[i].Paths {
			if !filepath.IsAbs(path) {
				m.Units[i].Paths[j] = filepath.Join(root, path)
			}
		}
	}
	connection, err := m.LookupConnection("backend_conn")
	if err != nil {
		t.Fatal(err)
	}
	u, err := newUnits(m).get(connection.Unit)
	if err != nil {
		t.Fatal(err)
	}
	announcement, err := os.ReadFile(filepath.Join(root, connection.Wire))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/fuzz-vectors/message_bytes_truncated.bin"))
	if err != nil {
		t.Fatal(err)
	}
	shapes, err := messageStructuralSpellings(u, u.Tables["LoginRequest"], announcement, batch)
	var damage *sharedMessageDamage
	var unsupported *sharedMessageUnsupported
	if !errors.As(err, &damage) || errors.As(err, &unsupported) || shapes != nil {
		t.Fatalf("damaged suffix certified spellings or lacked damage type: shapes=%v error=%T %v", shapes, err, err)
	}
}

func TestSharedMessagePurposeSpellings(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	for _, corpus := range []struct {
		schema, table, dir string
		cases              map[string]string
	}{
		{"test/message-one-encoding/One.schema", "One", "testdata/wire/message-one-encoding", map[string]string{
			"canonical.bin": "", "explicit-default.bin": "field-at-default", "fields-reordered.bin": "fields-out-of-order", "union-none.bin": "union-arm-zero"}},
		{"test/non-canonical/NonCanonical.schema", "Message", "testdata/wire/non-canonical", map[string]string{"message-canonical.bin": ""}},
	} {
		u, err := compiler.New().Load([]string{filepath.Join(root, corpus.schema)})
		if err != nil {
			t.Fatal(err)
		}
		announcement, err := os.ReadFile(filepath.Join(root, corpus.dir, "announcement.bin"))
		if err != nil {
			t.Fatal(err)
		}
		for name, want := range corpus.cases {
			data, err := os.ReadFile(filepath.Join(root, corpus.dir, name))
			if err != nil {
				t.Fatal(err)
			}
			got, err := messageStructuralSpellings(u, u.Tables[corpus.table], announcement, data)
			if err != nil || want == "" && len(got) != 0 || want != "" && (len(got) != 1 || !got[want]) {
				t.Errorf("%s: spellings=%v want=%s error=%v", name, got, want, err)
			}
		}
	}
}

func TestSharedMessageClassifierControls(t *testing.T) {
	t.Parallel()
	u := tempUnit(t, `package messagecontrols
union Pick { plain int32 }
union Outer { inner Pick }
type Leaf { value uint8 }
fixed table Root
{
    mode uint8 = 7
    count uint8
    pick Pick
    maybe ?uint8
    picks [2]Pick
    outer Outer
    child Leaf
}
`)
	st := u.Tables["Root"]
	var entries []ir.TableVocabularyEntry
	for _, f := range st.Fields {
		entries = append(entries, ir.TableFieldEntry(f))
	}
	plain := ir.TableArmEntry(st.Fields[2].Type.Ref.(*ir.Union).Variants[0])
	inner := ir.TableArmEntry(st.Fields[5].Type.Ref.(*ir.Union).Variants[0])
	entries = append(entries, plain, inner,
		ir.TableVocabularyEntry{Id: ir.TableWireId("future"), Kind: ir.TableKindUnion},
		ir.TableVocabularyEntry{Id: ir.TableWireId("future_arm"), Kind: ir.TableKindNoPayload})
	refBits := mathbits.Len(uint(len(entries)))
	build := func(mode uint64, fields ...uint64) []byte {
		w := &bitw{}
		for _, ref := range fields {
			w.put(ref, refBits)
			switch ref {
			case 1:
				w.put(mode, 8)
			case 2:
				w.put(5, 8)
			case 3:
				w.put(0, refBits)
			case 4:
				w.put(0, 8)
			case 5:
				w.put(0, refBits) // None is a value at an array position.
				w.put(8, refBits)
				w.put(1, 32)
			case 6:
				w.put(9, refBits)
				w.put(0, refBits) // A nested-union arm payload may be None.
			case 7:
				w.put(0, refBits) // An empty nested record body.
			case 10:
				w.put(11, refBits) // Unknown, nonempty, payload-free arm.
			}
		}
		w.put(0, refBits)
		return batchOf(w)
	}
	for _, tc := range []struct {
		name          string
		version, mode uint64
		fields        []uint64
		want          string
	}{
		{"canonical", ir.BuildVersion(u), 7, nil, ""},
		{"optional-default", ir.BuildVersion(u), 7, []uint64{4}, ""},
		{"array-none", ir.BuildVersion(u), 7, []uint64{5}, ""},
		{"nested-union-none", ir.BuildVersion(u), 7, []uint64{6}, ""},
		{"default", ir.BuildVersion(u), 7, []uint64{1}, "field-at-default"},
		{"nested-default", ir.BuildVersion(u), 7, []uint64{7}, "field-at-default"},
		{"field-none", ir.BuildVersion(u), 7, []uint64{3}, "union-arm-zero"},
		{"order", ir.BuildVersion(u), 8, []uint64{2, 1}, "fields-out-of-order"},
		{"unknown-same-build", ir.BuildVersion(u), 7, []uint64{10}, "same-build-unknown-field"},
		{"unknown-evolution", ir.BuildVersion(u) + 1, 7, []uint64{10}, ""},
		{"default-evolution", ir.BuildVersion(u) + 1, 7, []uint64{1}, ""},
		{"order-evolution", ir.BuildVersion(u) + 1, 8, []uint64{2, 1}, ""},
		{"field-none-evolution", ir.BuildVersion(u) + 1, 7, []uint64{3}, "union-arm-zero"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := messageStructuralSpellings(u, st, announce(tc.version, entries), build(tc.mode, tc.fields...))
			if err != nil || tc.want == "" && len(got) != 0 || tc.want != "" && (len(got) != 1 || !got[tc.want]) {
				t.Errorf("spellings=%v want=%s error=%v", got, tc.want, err)
			}
		})
	}
	other := append([]ir.TableVocabularyEntry(nil), entries...)
	other[0].Kind = ir.TableKindU16
	w := &bitw{}
	w.put(1, refBits)
	w.put(7, 16)
	w.put(0, refBits)
	if got, err := messageStructuralSpellings(u, st, announce(ir.BuildVersion(u), other), batchOf(w)); err != nil || len(got) != 0 {
		t.Errorf("changed shape: spellings=%v error=%v", got, err)
	}
	// An unknown FIELD's None is still a forbidden field spelling at an
	// evolved version; it is not an array element or an arm payload.
	w = &bitw{}
	w.put(10, refBits)
	w.put(0, refBits)
	w.put(0, refBits)
	if got, err := messageStructuralSpellings(u, st, announce(ir.BuildVersion(u)+1, entries), batchOf(w)); err != nil || len(got) != 1 || !got["union-arm-zero"] {
		t.Errorf("unknown field None: spellings=%v error=%v", got, err)
	}
	if _, err := messageStructuralSpellings(u, st, announce(ir.BuildVersion(u), entries), []byte{2, 0}); err == nil {
		t.Error("truncated batch was accepted")
	}
	badPad := build(7)
	badPad[len(badPad)-1] |= 128
	if _, err := messageStructuralSpellings(u, st, announce(ir.BuildVersion(u), entries), badPad); err == nil {
		t.Error("nonzero final padding was accepted")
	}
	if _, err := messageStructuralSpellings(u, st, announce(ir.BuildVersion(u), entries), append(build(7), 0)); err == nil {
		t.Error("bytes after a batch were accepted")
	}
	// Resolve required announcement fields by their trailer ids even after
	// a later version adds a header field before them.
	vf := versionField(ir.BuildVersion(u))
	vf[0] = 2
	vocab := vocabularyField(ir.TableVocabularyBytes(entries))
	vocab[0] = 3
	header := append([]byte{1, ir.TableKindU8, 42}, vf...)
	header = append(header, vocab...)
	header = append(header, 0)
	futureHeader := forgeAnnouncement(header, ir.TableWireId("future_header"), ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId)
	if got, err := messageStructuralSpellings(u, st, futureHeader, build(7)); err != nil || len(got) != 0 {
		t.Errorf("evolved announcement: spellings=%v error=%v", got, err)
	}
	pointer := append([]ir.TableVocabularyEntry(nil), entries...)
	pointer[9].Kind = ir.TableKindPointer
	got, err := messageStructuralSpellings(u, st, announce(ir.BuildVersion(u)+1, pointer), build(7, 10))
	var unsupported *sharedMessageUnsupported
	var damage *sharedMessageDamage
	if !errors.As(err, &unsupported) || errors.As(err, &damage) || got != nil || !strings.Contains(err.Error(), "pointer") {
		t.Errorf("unsupported pointer was not reported explicitly: %v", err)
	}

}

func TestSharedMessageNumericDefaultControls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, declaration, value string
		width                    int
		atDefault                bool
	}{
		{"bool", "value bool = true", "1", 1, true},
		{"signed-range", "value int8 = -4 | min = -5, max = 2", "1", 3, true},
		{"unsigned-high-base", "value uint64 = 18446744073709551614 | min = 18446744073709551614, max = 18446744073709551615", "0", 1, true},
		{"wide-negative", "value int128 = -1 | min = -2, max = 1", "1", 2, true},
		{"wide-raw", "value uint128 = 340282366920938463463374607431768211455", "340282366920938463463374607431768211455", 128, true},
		{"fixed-raw-base", "value fixed(4, 4) = 1 | min = -2, max = 2", "48", 7, true},
		{"positive-zero", "value float32", "0", 32, true},
		{"negative-zero", "value float32", "2147483648", 32, false},
		{"quantized-grid-default", "value float32 = 0.2 | min = 0, max = 1, resolution = 0.1", "2", 4, true},
		{"quantized-off-grid-default", "value float32 = 0.15 | min = 0, max = 1, resolution = 0.1", "1", 4, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := tempUnit(t, "package numericmessage\nfixed table Root\n{\n"+tc.declaration+"\n}\n")
			st := u.Tables["Root"]
			entry := ir.TableFieldEntry(st.Fields[0])
			value, ok := new(big.Int).SetString(tc.value, 10)
			if !ok {
				t.Fatal("bad test integer")
			}
			body := &bitw{}
			body.put(1, 1)
			for i := range tc.width {
				body.put(uint64(value.Bit(i)), 1)
			}
			body.put(0, 1)
			got, err := messageStructuralSpellings(u, st, announce(ir.BuildVersion(u), []ir.TableVocabularyEntry{entry}), batchOf(body))
			if err != nil || got["field-at-default"] != tc.atDefault || len(got) > 1 {
				t.Errorf("spellings=%v atDefault=%v error=%v", got, tc.atDefault, err)
			}
		})
	}
}

func TestSharedMessageContinuousBodiesAndPadding(t *testing.T) {
	t.Parallel()
	u := tempUnit(t, "package messagealignment\nfixed table Root\n{\nvalue string(3)\n}\n")
	st := u.Tables["Root"]
	announcement := announce(ir.BuildVersion(u), []ir.TableVocabularyEntry{ir.TableFieldEntry(st.Fields[0])})
	stream := &bitw{}
	stream.put(1, 8) // two bodies, with no alignment between their terminators
	for _, value := range []byte{'a', 'b'} {
		stream.put(1, 1)
		stream.put(1, 2)
		stream.align()
		stream.put(uint64(value), 8)
		stream.put(0, 1)
	}
	stream.align()
	batch := append([]byte{ir.TableWireMessageForm}, stream.b...)
	if got, err := messageStructuralSpellings(u, st, announcement, batch); err != nil || len(got) != 0 {
		t.Fatalf("continuous bodies: spellings=%v error=%v", got, err)
	}
	// Bit36 is the second string's internal align, relative to batch byte0.
	// Its body begins at bit33, so resetting alignment at a body is wrong.
	bad := append([]byte(nil), batch...)
	bad[4] |= 1 << 4
	if _, err := messageStructuralSpellings(u, st, announcement, bad); err == nil || !strings.Contains(err.Error(), "padding") {
		t.Errorf("nonzero interior padding: %v", err)
	}
}

func TestSharedMessageTextAndBytesDefaults(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		declaration, payload string
		want                 bool
	}{
		{`value string(3) = "a"`, "a", true},
		{`value string(3) = "a"`, "b", false},
		{`value bytes(3)`, "", true},
		{`value bytes(3)`, "a", false},
	} {
		u := tempUnit(t, "package messagetext\nfixed table Root\n{\n"+tc.declaration+"\n}\n")
		st := u.Tables["Root"]
		body := &bitw{}
		body.put(1, 1)
		body.put(uint64(len(tc.payload)), 2)
		body.align()
		body.bytes([]byte(tc.payload))
		body.put(0, 1)
		got, err := messageStructuralSpellings(u, st, announce(ir.BuildVersion(u), []ir.TableVocabularyEntry{ir.TableFieldEntry(st.Fields[0])}), batchOf(body))
		if err != nil || got["field-at-default"] != tc.want || len(got) > 1 {
			t.Errorf("%s payload=%q: spellings=%v atDefault=%v error=%v", tc.declaration, tc.payload, got, tc.want, err)
		}
	}
}
