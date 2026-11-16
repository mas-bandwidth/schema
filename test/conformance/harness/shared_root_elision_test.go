package main

import (
	"bytes"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// rootStoredDefaults checks scalar, string, empty array/keyed/map and empty
// by-value table fields at the root. It is a byte walk, not a value read. It
// also judges fixed scalar arrays; nested bodies are checked separately.
func rootStoredDefaults(st *ir.Struct, data []byte) map[string]bool {
	return bodyStoredDefaults(st, data, frameWire(data), -1)
}

// bodyStoredDefaults applies the same byte checks to one known body scope.
func bodyStoredDefaults(st *ir.Struct, data []byte, f *wireFrame, scope int) map[string]bool {
	lebAt := func(off, end int) (uint64, int, bool) {
		value, width := readLeb(data, off, end)
		return value, off + width, width > 0
	}
	out := map[string]bool{}
	fields := map[uint64]*ir.Field{}
	for _, fd := range st.Fields {
		fields[ir.TableFieldWireId(fd)] = fd
	}
	for i, sp := range f.spots {
		if sp.kind != spotRef || sp.value == 0 || i+1 >= len(f.spots) {
			continue
		}
		key := -1
		if len(sp.enclosing) > 0 {
			key = sp.enclosing[len(sp.enclosing)-1]
		}
		if key != scope {
			continue
		}
		id, ok := f.entryOf(sp.value)
		if !ok {
			continue
		}
		fd := fields[id]
		if fd == nil || fd.Required || fd.Deprecated || fd.Type.Optional || (fd.Type.Pointer && fd.Array == ir.ArrayNone) || (st.MapEntryOf != "" && (fd.Name == "key" || fd.Name == "value")) {
			continue
		}
		k := f.spots[i+1]
		if k.kind != spotKind || k.role != roleField || int(k.value) != ir.TableWireFieldKind(fd) {
			continue
		}
		off := k.off + 1
		end := sp.limit
		switch {
		case fd.Type.Kind == ir.TBytes:
			if k.value != ir.TableKindArray {
				continue
			}
			n, after, ok := lebAt(off, end)
			if !ok || n < 2 || n > uint64(end-after) || data[after] != ir.TableKindU8 {
				continue
			}
			count, payload, ok := lebAt(after+1, after+int(n))
			if ok && count == uint64(after+int(n)-payload) && bytes.Equal(data[payload:after+int(n)], fd.DefBytes) {
				out[fd.Name] = true
			}
		case fd.Array != ir.ArrayNone || fd.IsMap():
			if k.value != ir.TableKindArray && k.value != ir.TableKindKeyed {
				continue
			}
			n, after, ok := lebAt(off, end)
			if !ok || n < 2 || n > uint64(end-after) {
				continue
			}
			count, payload, ok := lebAt(after+1, after+int(n))
			if !ok {
				continue
			}
			// A fixed positional table array always rides, even if every
			// element is at its default. A keyed array is different.
			if fd.Array == ir.ArrayFixed && fd.KeyEnum == "" && ir.TableWireElemKind(fd) == ir.TableKindTable {
				continue
			}
			if count == 0 && fd.ArrayMin == 0 {
				out[fd.Name] = true
				continue
			}
			if fd.Array == ir.ArrayFixed && fd.KeyEnum == "" && int(data[after]) == ir.TableWireElemKind(fd) && count <= uint64(fd.ArrayBound) && fixedArrayAtDefault(fd, f, data, payload, after+int(n), count) {
				out[fd.Name] = true
			}
		case k.value == ir.TableKindString && fd.Type.Kind == ir.TString:
			n, after, ok := lebAt(off, end)
			if ok && n <= uint64(end-after) && bytes.Equal(data[after:after+int(n)], fd.DefBytes) {
				out[fd.Name] = true
			}
		case k.value == ir.TableKindTable:
			n, after, ok := lebAt(off, end)
			if ok && n == 1 && after < end && data[after] == 0 {
				out[fd.Name] = true
			}
		case k.value == ir.TableKindEnum:
			ref, _, ok := lebAt(off, end)
			if !ok {
				continue
			}
			if !fd.HasDefault || fd.DefVariant == "" || fd.DefVariant == "None" {
				if ref == 0 {
					out[fd.Name] = true
				}
			} else if id, ok := f.entryOf(ref); ok && id == defaultEnumWireID(fd) {
				out[fd.Name] = true
			}
		default:
			width := kindWidth(int(k.value))
			if width > 0 && off+width <= end && wireScalarDefault(fd, int(k.value), data[off:off+width]) {
				out[fd.Name] = true
			}
		}
	}
	return out
}

// defaultEnumWireID honors a variant's wire-name alias.
func defaultEnumWireID(fd *ir.Field) uint64 {
	name := fd.DefVariant
	if e, ok := fd.Type.Ref.(*ir.Enum); ok {
		name = e.VariantWireNameOf(name)
	}
	return ir.TableWireId(name)
}

// wireScalarDefault compares one directly encoded scalar to its declaration.
func wireScalarDefault(fd *ir.Field, kind int, data []byte) bool {
	if kind == ir.TableKindF32 && len(data) == 4 {
		v := math.Float32frombits(uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24)
		return v == float32(fd.DefFloat)
	}
	if kind == ir.TableKindF64 && len(data) == 8 {
		var bits uint64
		for j := 0; j < 8; j++ {
			bits |= uint64(data[j]) << uint(8*j)
		}
		return math.Float64frombits(bits) == fd.DefFloat
	}
	switch fd.Type.Kind {
	case ir.TBool, ir.TInt, ir.TBits, ir.TFixed, ir.TNamed:
	default:
		return false
	}
	if fd.Type.Kind == ir.TNamed {
		if _, ok := fd.Type.Ref.(*ir.Flags); !ok {
			return false
		}
	}
	if len(data) == 0 || len(data) > 16 {
		return false
	}
	raw := make([]byte, len(data))
	for j := range data {
		raw[len(data)-1-j] = data[j]
	}
	got := new(big.Int).SetBytes(raw)
	want := new(big.Int)
	if fd.Type.Kind == ir.TBool {
		if fd.HasDefault && fd.DefBool {
			want.SetInt64(1)
		}
	} else if fd.HasDefault && fd.DefInt != nil {
		want.Set(fd.DefInt)
	}
	if want.Sign() < 0 {
		want.Add(want, new(big.Int).Lsh(big.NewInt(1), uint(len(data)*8)))
	}
	return got.Cmp(want) == 0
}

func fixedArrayAtDefault(fd *ir.Field, f *wireFrame, data []byte, off, end int, count uint64) bool {
	kind := ir.TableWireElemKind(fd)
	for range count {
		switch kind {
		case ir.TableKindPointer, ir.TableKindUnion, ir.TableKindEnum:
			value, width := readLeb(data, off, end)
			if width == 0 {
				return false
			}
			off += width
			if kind == ir.TableKindEnum && fd.HasDefault && fd.DefVariant != "" && fd.DefVariant != "None" {
				id, ok := f.entryOf(value)
				if !ok || id != defaultEnumWireID(fd) {
					return false
				}
			} else if value != 0 {
				return false
			}
		default:
			width := kindWidth(kind)
			if width == 0 || off+width > end || !wireScalarDefault(fd, kind, data[off:off+width]) {
				return false
			}
			off += width
		}
	}
	return off == end
}

// TestRootStoredDefaultsKnownCases checks an explicit scalar default and a
// counted-array default against nondefault controls, with no engine decode.
func TestRootStoredDefaultsKnownCases(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	u, err := compiler.New().Load([]string{filepath.Join(root, "test/non-canonical/NonCanonical.schema")})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, field string }{{"variable-canonical.bin", ""}, {"variable-field-at-default.bin", "mode"}} {
		data, err := os.ReadFile(filepath.Join(root, "testdata/wire/non-canonical", tc.name))
		if err != nil {
			t.Fatal(err)
		}
		got := rootStoredDefaults(u.Tables["Root"], data)
		if tc.field == "" {
			if len(got) != 0 {
				t.Errorf("%s: got defaults %v", tc.name, got)
			}
			continue
		}
		if len(got) != 1 || !got[tc.field] {
			t.Errorf("%s: got %v, want %s", tc.name, got, tc.field)
		}
	}
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	unit, err := newUnits(m).get("tblr1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, framingDir, "array_L2_empty.bin"))
	if err != nil {
		t.Fatal(err)
	}
	got := rootStoredDefaults(unit.Tables["Cfg"], data)
	if len(got) != 1 || !got["grades"] {
		t.Errorf("empty array purpose: got %v", got)
	}
}

// TestSharedReadableRootElision checks the supported root elision shapes on readable file rows.
func TestSharedReadableRootElision(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	reports, err := readReports(filepath.Join(root, defaultReports))
	if err != nil {
		t.Fatal(err)
	}
	units := newUnits(m)
	type item struct{ name, unit, table, path string }
	var cases []item
	for _, x := range m.Instances {
		if !x.WireRefused {
			cases = append(cases, item{x.Name, x.Unit, x.Root, x.Wire})
		}
	}
	for _, x := range m.Reports {
		if reports[x.Name] == "read" {
			cases = append(cases, item{x.Name, x.Unit, x.Root, x.Wire})
		}
	}
	for _, x := range m.Framings {
		if !x.Refused {
			cases = append(cases, item{x.Name, x.Unit, x.Root, x.Wire})
		}
	}
	for _, x := range m.Rewrites {
		cases = append(cases, item{x.Name, x.Unit, x.Root, x.Wire})
	}
	for _, x := range m.Retains {
		cases = append(cases, item{x.Name, x.Unit, x.Root, x.Wire})
	}
	for _, x := range m.JSONWriteRefusals {
		cases = append(cases, item{x.Name, x.Unit, x.Root, x.Wire})
	}
	seen := map[string]bool{}
	for _, x := range cases {
		key := x.unit + "/" + x.table + "/" + x.path
		if seen[key] {
			continue
		}
		seen[key] = true
		data, err := os.ReadFile(filepath.Join(root, x.path))
		if err != nil {
			t.Errorf("%s: %v", x.name, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("%s: empty readable wire", x.name)
			continue
		}
		if data[0] == 3 {
			continue
		}
		if data[0] != 1 {
			t.Errorf("%s: unexpected form %d", x.name, data[0])
			continue
		}
		u, err := units.get(x.unit)
		if err != nil {
			t.Fatalf("%s: %v", x.name, err)
		}
		st := u.Tables[x.table]
		if st == nil {
			t.Fatalf("%s: no %s root", x.name, x.table)
		}
		if st.Integrity != "" {
			if len(data) < 5 {
				t.Errorf("%s: missing integrity trailer", x.name)
				continue
			}
			data = data[:len(data)-4]
		}
		shapes := rootStoredDefaults(st, data)
		want := ""
		if filepath.Base(x.path) == "array_L2_empty.bin" {
			want = "grades"
		}
		if want == "" {
			if len(shapes) != 0 {
				t.Errorf("%s (%s): root fields stored at default %v", x.name, x.path, shapes)
			}
		} else if len(shapes) != 1 || !shapes[want] {
			t.Errorf("%s: got %v, want only %s", x.name, shapes, want)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no readable file cases")
	}
}

// TestRootStoredDefaultsScalarAndCollectionControls exercises direct default
// comparisons and verifies a default-valued array element or map key rides.
func TestRootStoredDefaultsScalarAndCollectionControls(t *testing.T) {
	t.Parallel()
	schema := []byte("package rootelision\nenum Kind { New | was = \"Old\"\n Other }\ntable Node { x uint8 }\ntable Root {\n n int32 = 7\n choice Kind = New\n f float32 = 1.5\n title string(8) = \"abc\"\n values [..4]uint8\n nums [2]uint8\n ptrs [2]*Node\n nodes [2]Node\n blob bytes(4) = \"ab\"\n entries map[int32]int32\n keep uint8 | required\n}\n")
	path := filepath.Join(t.TempDir(), "Root.schema")
	if err := os.WriteFile(path, schema, 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name         string
		ids          []string
		body         []byte
		defaultField string
	}{
		{"int default", []string{"n"}, cat(b(1, 4), i32(7), b(0)), "n"},
		{"int nondefault", []string{"n"}, cat(b(1, 4), i32(8), b(0)), ""},
		{"aliased enum default", []string{"choice", "Old"}, b(1, 30, 2, 0), "choice"},
		{"enum nondefault", []string{"choice", "Other"}, b(1, 30, 2, 0), ""},
		{"float default", []string{"f"}, cat(b(1, 10), f32(1.5), b(0)), "f"},
		{"float nondefault", []string{"f"}, cat(b(1, 10), f32(2.5), b(0)), ""},
		{"text default", []string{"title"}, cat(b(1, 12, 3), []byte("abc"), b(0)), "title"},
		{"text nondefault", []string{"title"}, cat(b(1, 12, 3), []byte("def"), b(0)), ""},
		{"empty counted array", []string{"values"}, b(1, 14, 2, 6, 0, 0), "values"},
		{"default array element", []string{"values"}, b(1, 14, 3, 6, 1, 0, 0), ""},
		{"default map key", []string{"entries", "key"}, b(1, 14, 10, 13, 1, 7, 2, 4, 0, 0, 0, 0, 0, 0), ""},
		{"required default", []string{"keep"}, b(1, 6, 0, 0), ""},
		{"fixed array default", []string{"nums"}, b(1, 14, 4, 6, 2, 0, 0, 0), "nums"},
		{"fixed array nondefault", []string{"nums"}, b(1, 14, 4, 6, 2, 0, 1, 0), ""},
		{"fixed pointer array default", []string{"ptrs"}, b(1, 14, 4, 17, 2, 0, 0, 0), "ptrs"},
		{"fixed table array always rides", []string{"nodes"}, b(1, 14, 6, 13, 2, 1, 0, 1, 0, 0), ""},
		{"bytes default", []string{"blob"}, b(1, 14, 4, 6, 2, 'a', 'b', 0), "blob"},
		{"bytes nondefault", []string{"blob"}, b(1, 14, 4, 6, 2, 'a', 'c', 0), ""},
	} {
		v := framingVector{ids: tc.ids, body: tc.body}
		wire, err := v.bytes(nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := rootStoredDefaults(u.Tables["Root"], wire)
		if tc.defaultField == "" {
			if len(got) != 0 {
				t.Errorf("%s: got %v", tc.name, got)
			}
			continue
		}
		if len(got) != 1 || !got[tc.defaultField] {
			t.Errorf("%s: got %v, want %s", tc.name, got, tc.defaultField)
		}
	}
}
