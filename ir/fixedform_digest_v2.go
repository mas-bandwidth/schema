package ir

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
)

// The first byte cannot begin a legacy digest (R, B, X or F). Prefixing even
// roots with no definition rows prevents intentional layout-only key reuse.
const tableFixedDigestV2Prefix = "\x00schema-fixed-definitions\x02"

// TableFixedDefinitionsDigest computes the current fixed-file definitions
// identity from a checked, acyclic writer closure. Rows are framed and addressed
// by wire paths, so omitting another field's range cannot reassign a bound.
// Declaration spelling and shared-type deduplication never determine a path.
// Nil is retained as the legacy raw-hash compatibility operation.
func TableFixedDefinitionsDigest(st *Struct) []byte {
	if st == nil {
		return nil
	}
	w := tableFixedDigestV2{bytes: []byte(tableFixedDigestV2Prefix)}
	path := tableFixedDigestPath(nil, 't', TableKindTable, TableWireId(st.WireName()))
	w.record(st, path)
	return w.bytes
}

// TableFixedDefinitionsDigestForRecorded computes the writer digest under the
// scheme selected by recorded. Callers must compare the result with recorded
// as well as checking the layout, record size, root and raw wire hash. An empty
// recorded digest selects legacy-v1; it does NOT waive that equality check for
// a historical source containing constraints. Unknown epochs are refused.
func TableFixedDefinitionsDigestForRecorded(st *Struct, recorded []byte) ([]byte, error) {
	version, err := TableFixedDefinitionsDigestVersion(recorded)
	if err != nil {
		return nil, err
	}
	if version == 2 {
		return TableFixedDefinitionsDigest(st), nil
	}
	return TableFixedDefinitionsDigestLegacy(st), nil
}

// TableFixedDefinitionsDigestVersion identifies a recorded identity scheme for
// lock rendering validation. It does not validate the row payloads; matching a
// writer requires recomputing and comparing its digest under that scheme.
func TableFixedDefinitionsDigestVersion(recorded []byte) (int, error) {
	if bytes.HasPrefix(recorded, []byte(tableFixedDigestV2Prefix)) {
		return 2, nil
	}
	if len(recorded) == 0 || recorded[0] == 'R' || recorded[0] == 'B' || recorded[0] == 'X' || recorded[0] == 'F' {
		return 1, nil
	}
	return 0, fmt.Errorf("unknown fixed definitions digest scheme")
}

type tableFixedDigestV2 struct{ bytes []byte }

// A segment is role:u8, fixed-wire kind:u32 LE, wire id:u64 LE. The path's
// byte length frames it in each row; anonymous wrapper IDs are zero.
func tableFixedDigestPath(parent []byte, role byte, kind int, id uint64) []byte {
	p := append([]byte(nil), parent...)
	p = append(p, role)
	p = tableFixedDigestU32(p, uint32(kind))
	return tableFixedDigestU64(p, id)
}

func (w *tableFixedDigestV2) row(path []byte, tag byte, payload []byte) {
	w.bytes = append(w.bytes, tag)
	w.bytes = tableFixedDigestU32(w.bytes, uint32(len(path)))
	w.bytes = append(w.bytes, path...)
	w.bytes = tableFixedDigestU32(w.bytes, uint32(len(payload)))
	w.bytes = append(w.bytes, payload...)
}

func (w *tableFixedDigestV2) record(st *Struct, path []byte) {
	for _, f := range st.Fields {
		w.field(f, path, 'f', TableFieldWireId(f))
	}
}

func (w *tableFixedDigestV2) field(f *Field, parent []byte, role byte, id uint64) {
	if f == nil {
		return
	}
	kind := TableFixedLeafKind(f)
	if f.KeyEnum != "" {
		kind = TableKindKeyed
	} else if f.Array != ArrayNone || f.Type.Kind == TBytes {
		kind = TableKindArray
	}
	path := tableFixedDigestPath(parent, role, kind, id)
	if f.Type.Optional {
		path = tableFixedDigestPath(path, '?', TableKindOptional, 0)
	}
	if f.Array == ArrayCounted {
		w.row(path, 'C', tableFixedDigestU64(nil, uint64(f.ArrayMin)))
	}
	if f.Array != ArrayNone || f.KeyEnum != "" {
		path = tableFixedDigestPath(path, 'e', TableFixedLeafKind(f), 0)
	}
	if f.HasIntRange {
		p := tableFixedDigestU32(nil, uint32(TableFixedLeafKind(f)))
		p = append(p, tableFixedDigestBool(f.Type.Signed))
		p = tableFixedDigestU32(p, uint32(f.Type.FracBits))
		p = tableFixedDigestInteger(p, f.IntMin)
		p = tableFixedDigestInteger(p, f.IntMax)
		w.row(path, 'R', p)
	} else if f.HasFloatRange {
		p := tableFixedDigestU32(nil, uint32(TableFixedLeafKind(f)))
		p = tableFixedDigestU64(p, math.Float64bits(f.FMin))
		p = tableFixedDigestU64(p, math.Float64bits(f.FMax))
		w.row(path, 'D', p)
		w.row(path, 'Q', tableFixedDigestU64(nil, math.Float64bits(f.Resolution)))
	}
	switch f.Type.Kind {
	case TBits:
		w.row(path, 'B', tableFixedDigestU32(nil, uint32(f.Type.Width)))
	case TFixed:
		p := tableFixedDigestU32(nil, uint32(f.Type.IntBits))
		p = tableFixedDigestU32(p, uint32(f.Type.FracBits))
		w.row(path, 'X', append(p, tableFixedDigestBool(f.Type.Signed)))
	}
	switch r := f.Type.Ref.(type) {
	case *Flags:
		n := r.WireBits
		if n == 0 {
			n = len(r.Variants)
		}
		p := tableFixedDigestU32(nil, uint32(n))
		p = tableFixedDigestU32(p, uint32(len(r.Variants)))
		for _, name := range r.Variants {
			p = tableFixedDigestU64(p, TableWireId(name))
		}
		w.row(path, 'F', p)
	case *Struct:
		w.record(r, path)
	case *Union:
		for _, v := range r.Variants {
			w.field(v.F, path, 'a', TableWireId(v.WireName()))
		}
	}
}

func tableFixedDigestBool(v bool) byte {
	if v {
		return 1
	}
	return 0
}

// Sign plus length-prefixed minimal big-endian magnitude is canonical and
// lossless throughout signed/unsigned 128-bit domains and fixed raw scales.
// Nil has its own marker, so an invalid unchecked IR cannot alias zero.
func tableFixedDigestInteger(dst []byte, n *big.Int) []byte {
	if n == nil {
		return tableFixedDigestU32(append(dst, 2), 0)
	}
	magnitude := n.Bytes()
	dst = append(dst, tableFixedDigestBool(n.Sign() < 0))
	dst = tableFixedDigestU32(dst, uint32(len(magnitude)))
	return append(dst, magnitude...)
}
