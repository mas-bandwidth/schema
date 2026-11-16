package lockfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// These private DTOs are a versioned format, not JSON of the evolving IR.
// Expressions, paths and inert annotations are deliberately absent. Named
// references form a closed graph; declaration and field order are retained.
// Missing LineageEntry.Writer means legacy-unknown, never unconstrained.
type writerSnapshot struct {
	Version int            `json:"version"`
	Root    string         `json:"root"`
	Records []writerRecord `json:"records"`
	Enums   []writerEnum   `json:"enums,omitempty"`
	Flags   []writerFlags  `json:"flags,omitempty"`
	Unions  []writerUnion  `json:"unions,omitempty"`
}
type writerRecord struct {
	Name   string        `json:"name"`
	Was    string        `json:"was,omitempty"`
	Table  bool          `json:"table,omitempty"`
	Fixed  bool          `json:"fixed,omitempty"`
	Fields []writerField `json:"fields"`
}
type writerEnum struct {
	Name       string   `json:"name"`
	Variants   []string `json:"variants"`
	Was        []string `json:"was,omitempty"`
	Max        int64    `json:"max"`
	Storage    int      `json:"storage"`
	Deprecated []bool   `json:"deprecated,omitempty"`
}
type writerFlags struct {
	Name     string   `json:"name"`
	Variants []string `json:"variants"`
	Bits     int      `json:"bits"`
}
type writerUnion struct {
	Name    string      `json:"name"`
	Max     int64       `json:"max"`
	Storage int         `json:"storage"`
	Arms    []writerArm `json:"arms"`
}
type writerArm struct {
	Name  string       `json:"name"`
	Was   string       `json:"was,omitempty"`
	Type  string       `json:"type,omitempty"`
	Field *writerField `json:"field,omitempty"`
}
type writerField struct {
	Name          string `json:"name"`
	Was           string `json:"was,omitempty"`
	Kind          string `json:"kind"`
	Signed        bool   `json:"signed,omitempty"`
	Width         int    `json:"width,omitempty"`
	I             int    `json:"i,omitempty"`
	F             int    `json:"f,omitempty"`
	Size          int64  `json:"size,omitempty"`
	Type          string `json:"type,omitempty"`
	Optional      bool   `json:"optional,omitempty"`
	Timestamp     bool   `json:"timestamp,omitempty"`
	Array         string `json:"array,omitempty"`
	Bound         int64  `json:"bound,omitempty"`
	Minimum       int64  `json:"minimum,omitempty"`
	Key           string `json:"key,omitempty"`
	Deprecated    bool   `json:"deprecated,omitempty"`
	Required      bool   `json:"required,omitempty"`
	HasDefault    bool   `json:"has_default,omitempty"`
	DefInt        string `json:"def_int,omitempty"`
	DefFloat      uint64 `json:"def_float,omitempty"`
	DefBool       bool   `json:"def_bool,omitempty"`
	DefBytes      []byte `json:"def_bytes,omitempty"`
	DefVariant    string `json:"def_variant,omitempty"`
	HasIntRange   bool   `json:"has_int_range,omitempty"`
	Lo            string `json:"lo,omitempty"`
	Hi            string `json:"hi,omitempty"`
	HasFloatRange bool   `json:"has_float_range,omitempty"`
	FLo           uint64 `json:"flo,omitempty"`
	FHi           uint64 `json:"fhi,omitempty"`
	Resolution    uint64 `json:"resolution,omitempty"`
	Round         string `json:"round,omitempty"`
	Steps         int64  `json:"steps,omitempty"`
}

var writerKinds = map[ir.FieldTypeKind]string{ir.TInt: "int", ir.TBits: "bits", ir.TBool: "bool", ir.TFloat32: "float32", ir.TFloat64: "float64", ir.TString: "string", ir.TBytes: "bytes", ir.TFixed: "fixed", ir.TNamed: "named"}

func intString(n *big.Int) string {
	if n == nil {
		return ""
	}
	return n.String()
}
func snapshotField(f *ir.Field) writerField {
	out := writerField{Name: f.Name, Was: f.WasName, Kind: writerKinds[f.Type.Kind], Signed: f.Type.Signed, Width: f.Type.Width, I: f.Type.IntBits, F: f.Type.FracBits, Size: f.Type.Size, Type: f.Type.Name, Optional: f.Type.Optional, Timestamp: f.Type.Timestamp, Bound: f.ArrayBound, Minimum: f.ArrayMin, Key: f.KeyEnum, Deprecated: f.Deprecated, Required: f.Required, HasDefault: f.HasDefault, DefInt: intString(f.DefInt), DefFloat: math.Float64bits(f.DefFloat), DefBool: f.DefBool, DefBytes: f.DefBytes, DefVariant: f.DefVariant, HasIntRange: f.HasIntRange, Lo: intString(f.IntMin), Hi: intString(f.IntMax), HasFloatRange: f.HasFloatRange, FLo: math.Float64bits(f.FMin), FHi: math.Float64bits(f.FMax), Resolution: math.Float64bits(f.Resolution), Round: f.Round, Steps: f.Steps}
	switch f.Array {
	case ir.ArrayFixed:
		out.Array = "fixed"
	case ir.ArrayCounted:
		out.Array = "counted"
	}
	return out
}
func snapshotWriter(root *ir.Struct) writerSnapshot {
	out := writerSnapshot{Version: 1, Root: root.Name}
	seen := map[ir.Decl]bool{}
	var visit func(ir.Decl)
	field := func(f *ir.Field) {
		if f == nil {
			return
		}
		if f.KeyEnumRef != nil {
			visit(f.KeyEnumRef)
		}
		if f.Type.Ref != nil {
			visit(f.Type.Ref)
		}
	}
	visit = func(d ir.Decl) {
		if d == nil || seen[d] {
			return
		}
		seen[d] = true
		switch x := d.(type) {
		case *ir.Struct:
			rec := writerRecord{Name: x.Name, Was: x.WasName, Table: x.IsTable, Fixed: x.FixedDeclared}
			for _, f := range x.Fields {
				rec.Fields = append(rec.Fields, snapshotField(f))
			}
			out.Records = append(out.Records, rec)
			for _, f := range x.Fields {
				field(f)
			}
		case *ir.Enum:
			dep := append([]bool(nil), x.VariantDeprecated...)
			for i, tags := range x.VariantTags {
				for _, tag := range tags {
					if tag == "deprecated" {
						for len(dep) <= i {
							dep = append(dep, false)
						}
						dep[i] = true
					}
				}
			}
			out.Enums = append(out.Enums, writerEnum{Name: x.Name, Variants: x.Variants, Was: x.Was, Max: x.Max, Storage: x.StorageBits, Deprecated: dep})
		case *ir.Flags:
			out.Flags = append(out.Flags, writerFlags{Name: x.Name, Variants: x.Variants, Bits: x.WireBits})
		case *ir.Union:
			un := writerUnion{Name: x.Name, Max: x.Max, Storage: x.StorageBits}
			for _, v := range x.Variants {
				a := writerArm{Name: v.Name, Was: v.WasName, Type: v.Type}
				if v.F != nil {
					f := snapshotField(v.F)
					a.Field = &f
				}
				un.Arms = append(un.Arms, a)
			}
			out.Unions = append(out.Unions, un)
			for _, v := range x.Variants {
				field(v.F)
			}
		}
	}
	visit(root)
	return out
}
func writerBytes(root *ir.Struct) []byte {
	if root == nil {
		return nil
	}
	data, err := json.Marshal(snapshotWriter(root))
	if err != nil {
		panic(fmt.Sprintf("non-serializable compiler writer facts: %v", err))
	}
	return data
}
func parseWriter(data []byte, maxEntries int) (*ir.Struct, error) {
	var snap writerSnapshot
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&snap); err != nil {
		return nil, fmt.Errorf("invalid writer snapshot: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("writer snapshot carries trailing data")
	}
	if snap.Version != 1 {
		return nil, fmt.Errorf("writer snapshot version %d is not supported", snap.Version)
	}
	decls := map[string]ir.Decl{}
	add := func(name string, d ir.Decl) error {
		if name == "" || decls[name] != nil {
			return fmt.Errorf("writer snapshot repeats or omits declaration name %q", name)
		}
		decls[name] = d
		return nil
	}
	for _, r := range snap.Records {
		if err := add(r.Name, &ir.Struct{Name: r.Name, WasName: r.Was, IsTable: r.Table, FixedDeclared: r.Fixed}); err != nil {
			return nil, err
		}
	}
	for _, e := range snap.Enums {
		tags := make([][]string, len(e.Variants))
		for i, d := range e.Deprecated {
			if i >= len(tags) {
				return nil, fmt.Errorf("writer snapshot enum %s has too many deprecation marks", e.Name)
			}
			if d {
				tags[i] = []string{"deprecated"}
			}
		}
		if err := add(e.Name, &ir.Enum{Name: e.Name, Variants: e.Variants, Was: e.Was, Max: e.Max, StorageBits: e.Storage, VariantDeprecated: e.Deprecated, VariantTags: tags}); err != nil {
			return nil, err
		}
	}
	for _, f := range snap.Flags {
		if err := add(f.Name, &ir.Flags{Name: f.Name, Variants: f.Variants, WireBits: f.Bits}); err != nil {
			return nil, err
		}
	}
	for _, u := range snap.Unions {
		if err := add(u.Name, &ir.Union{Name: u.Name, Max: u.Max, StorageBits: u.Storage}); err != nil {
			return nil, err
		}
	}
	integer := func(s string) (*big.Int, error) {
		if s == "" {
			return nil, nil
		}
		if len(s) > 40 {
			return nil, fmt.Errorf("writer snapshot integer exceeds the 128-bit domain")
		}
		n, ok := new(big.Int).SetString(s, 10)
		if !ok || n.String() != s {
			return nil, fmt.Errorf("writer snapshot integer %q is not canonical decimal", s)
		}
		return n, nil
	}
	restore := func(raw writerField) (*ir.Field, error) {
		kind := ir.FieldTypeKind(-1)
		for i, k := range writerKinds {
			if k == raw.Kind {
				kind = i
				break
			}
		}
		if kind < 0 {
			return nil, fmt.Errorf("writer snapshot field %s has unknown kind %s", raw.Name, raw.Kind)
		}
		f := &ir.Field{Name: raw.Name, WasName: raw.Was, Type: ir.FieldType{Kind: ir.FieldTypeKind(kind), Signed: raw.Signed, Width: raw.Width, IntBits: raw.I, FracBits: raw.F, Size: raw.Size, Name: raw.Type, Optional: raw.Optional, Timestamp: raw.Timestamp}, ArrayBound: raw.Bound, ArrayMin: raw.Minimum, KeyEnum: raw.Key, Deprecated: raw.Deprecated, Required: raw.Required, HasDefault: raw.HasDefault, DefFloat: math.Float64frombits(raw.DefFloat), DefBool: raw.DefBool, DefBytes: raw.DefBytes, DefVariant: raw.DefVariant, HasIntRange: raw.HasIntRange, HasFloatRange: raw.HasFloatRange, FMin: math.Float64frombits(raw.FLo), FMax: math.Float64frombits(raw.FHi), Resolution: math.Float64frombits(raw.Resolution), Round: raw.Round, Steps: raw.Steps}
		switch raw.Array {
		case "":
		case "fixed":
			f.Array = ir.ArrayFixed
		case "counted":
			f.Array = ir.ArrayCounted
		default:
			return nil, fmt.Errorf("writer snapshot field %s has unknown array shape", raw.Name)
		}
		var err error
		f.DefInt, err = integer(raw.DefInt)
		if err != nil {
			return nil, err
		}
		f.IntMin, err = integer(raw.Lo)
		if err != nil {
			return nil, err
		}
		f.IntMax, err = integer(raw.Hi)
		if err != nil {
			return nil, err
		}
		if f.Type.Kind == ir.TNamed {
			f.Type.Ref = decls[f.Type.Name]
			if f.Type.Ref == nil {
				return nil, fmt.Errorf("writer snapshot field %s names missing type %s", f.Name, f.Type.Name)
			}
		}
		if f.KeyEnum != "" {
			e, ok := decls[f.KeyEnum].(*ir.Enum)
			if !ok {
				return nil, fmt.Errorf("writer snapshot field %s names missing key enum %s", f.Name, f.KeyEnum)
			}
			f.KeyEnumRef = e
		}
		if f.HasIntRange && (f.IntMin == nil || f.IntMax == nil || f.IntMin.Cmp(f.IntMax) > 0) {
			return nil, fmt.Errorf("writer snapshot field %s has invalid integer bounds", f.Name)
		}
		if f.HasFloatRange && (math.IsNaN(f.FMin) || math.IsNaN(f.FMax) || math.IsInf(f.FMin, 0) || math.IsInf(f.FMax, 0) || f.FMin >= f.FMax || math.IsNaN(f.Resolution) || math.IsInf(f.Resolution, 0) || f.Resolution <= 0) {
			return nil, fmt.Errorf("writer snapshot field %s has invalid float bounds", f.Name)
		}
		if f.Array == ir.ArrayCounted && (f.ArrayMin < 0 || f.ArrayMin > f.ArrayBound) {
			return nil, fmt.Errorf("writer snapshot field %s has invalid count bounds", f.Name)
		}
		return f, nil
	}
	for _, r := range snap.Records {
		st := decls[r.Name].(*ir.Struct)
		for _, raw := range r.Fields {
			f, err := restore(raw)
			if err != nil {
				return nil, err
			}
			st.Fields = append(st.Fields, f)
			st.Items = append(st.Items, &ir.FieldItem{F: f})
		}
	}
	for _, u := range snap.Unions {
		un := decls[u.Name].(*ir.Union)
		for _, raw := range u.Arms {
			v := ir.UnionVariant{Name: raw.Name, WasName: raw.Was, Type: raw.Type}
			if raw.Field != nil {
				f, err := restore(*raw.Field)
				if err != nil {
					return nil, err
				}
				v.F = f
				v.Ref, _ = f.Type.Ref.(*ir.Struct)
			}
			un.Variants = append(un.Variants, v)
		}
	}
	root, ok := decls[snap.Root].(*ir.Struct)
	if !ok || !root.IsTable || !root.FixedDeclared {
		return nil, fmt.Errorf("writer snapshot root %s is not a fixed table", snap.Root)
	}
	if err := validateWriterGraph(root, maxEntries); err != nil {
		return nil, err
	}
	if !bytes.Equal(data, writerBytes(root)) {
		return nil, fmt.Errorf("writer snapshot is not the canonical complete closure")
	}
	return root, nil
}
func validateWriter(e LineageEntry, table string) error {
	if e.Writer == nil {
		return nil
	}
	if e.Writer.WireName() != table {
		return fmt.Errorf("writer snapshot root %s does not name fixed table %s", e.Writer.WireName(), table)
	}
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(e.Writer))
	if !bytes.Equal(layout, e.Layout) || !bytes.Equal(ir.TableFixedDefinitionsDigest(e.Writer), e.Digest) || ir.TableFixedTypeBytes(e.Writer) != e.Record || lineageWireHash(layout, ir.TableFixedDefinitionsDigest(e.Writer)) != e.Wire {
		return fmt.Errorf("fixed table %s, lineage wire=0x%016x: writer snapshot does not match its root/layout/digest/record/hash", table, e.Wire)
	}
	return nil
}

// sameWriterConstraints compares the source checks, not inert annotations or
// declared spellings changed through a wire-name alias. Layout equality is
// checked separately; paths below are positions within that already equal tree.
func sameWriterConstraints(a, b *ir.Struct) bool {
	if a == nil || b == nil {
		return a == b
	}
	type bound struct {
		Path                     []int
		Kind, Width, Frac, Flags int
		Array                    ir.ArrayKind
		Minimum, Maximum         int64
		HasInt, HasFloat         bool
		Lo, Hi                   string
		FLo, FHi, Resolution     uint64
	}
	var rows func(*ir.Struct) []bound
	rows = func(root *ir.Struct) []bound {
		var out []bound
		var record func(*ir.Struct, []int)
		var field func(*ir.Field, []int)
		field = func(f *ir.Field, path []int) {
			r := bound{Path: append([]int(nil), path...), Kind: ir.TableScalarKind(f), Width: f.Type.Width, Frac: f.Type.FracBits, Array: f.Array, Minimum: f.ArrayMin, Maximum: f.ArrayBound, HasInt: f.HasIntRange, HasFloat: f.HasFloatRange}
			if f.IntMin != nil {
				r.Lo = f.IntMin.String()
			}
			if f.IntMax != nil {
				r.Hi = f.IntMax.String()
			}
			if f.HasFloatRange {
				r.FLo = math.Float64bits(f.FMin)
				r.FHi = math.Float64bits(f.FMax)
				r.Resolution = math.Float64bits(f.Resolution)
			}
			if fl, ok := f.Type.Ref.(*ir.Flags); ok {
				r.Flags = fl.WireBits
			}
			out = append(out, r)
			switch d := f.Type.Ref.(type) {
			case *ir.Struct:
				record(d, path)
			case *ir.Union:
				for i, v := range d.Variants {
					if v.F != nil {
						field(v.F, append(append([]int(nil), path...), i))
					}
				}
			}
		}
		record = func(st *ir.Struct, path []int) {
			for i, f := range st.Fields {
				field(f, append(append([]int(nil), path...), i))
			}
		}
		record(root, nil)
		return out
	}
	left, _ := json.Marshal(rows(a))
	right, _ := json.Marshal(rows(b))
	return bytes.Equal(left, right)
}
