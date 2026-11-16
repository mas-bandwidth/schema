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
// does not yet judge all-default nonempty arrays or nested field defaults.
func rootStoredDefaults(st *ir.Struct, data []byte) map[string]bool {
	f := frameWire(data)
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
		if sp.kind != spotRef || len(sp.enclosing) != 0 || sp.value == 0 || i+1 >= len(f.spots) {
			continue
		}
		id, ok := f.entryOf(sp.value)
		if !ok {
			continue
		}
		fd := fields[id]
		if fd == nil || fd.Required || fd.Deprecated || fd.Type.Optional || fd.Type.Pointer {
			continue
		}
		k := f.spots[i+1]
		if k.kind != spotKind || k.role != roleField || int(k.value) != ir.TableWireFieldKind(fd) {
			continue
		}
		off := k.off + 1
		switch {
		case fd.Array != ir.ArrayNone || fd.IsMap():
			if k.value != ir.TableKindArray && k.value != ir.TableKindKeyed {
				continue
			}
			n, after, ok := lebAt(off, f.bodyEnd)
			if !ok || n < 2 || n > uint64(f.bodyEnd-after) {
				continue
			}
			count, _, ok := lebAt(after+1, after+int(n))
			if ok && count == 0 && fd.ArrayMin == 0 {
				out[fd.Name] = true
			}
		case k.value == ir.TableKindString && fd.Type.Kind == ir.TString:
			n, after, ok := lebAt(off, f.bodyEnd)
			if ok && n <= uint64(f.bodyEnd-after) && bytes.Equal(data[after:after+int(n)], fd.DefBytes) {
				out[fd.Name] = true
			}
		case k.value == ir.TableKindTable:
			n, after, ok := lebAt(off, f.bodyEnd)
			if ok && n == 1 && after < f.bodyEnd && data[after] == 0 {
				out[fd.Name] = true
			}
		case k.value == ir.TableKindEnum:
			ref, _, ok := lebAt(off, f.bodyEnd)
			if !ok {
				continue
			}
			if !fd.HasDefault || fd.DefVariant == "" || fd.DefVariant == "None" {
				if ref == 0 {
					out[fd.Name] = true
				}
			} else if id, ok := f.entryOf(ref); ok && id == ir.TableWireId(fd.DefVariant) {
				out[fd.Name] = true
			}
		case k.value == ir.TableKindF32:
			if off+4 <= f.bodyEnd {
				v := math.Float32frombits(uint32(data[off]) | uint32(data[off+1])<<8 | uint32(data[off+2])<<16 | uint32(data[off+3])<<24)
				if v == float32(fd.DefFloat) {
					out[fd.Name] = true
				}
			}
		case k.value == ir.TableKindF64:
			if off+8 <= f.bodyEnd {
				var bits uint64
				for j := 0; j < 8; j++ {
					bits |= uint64(data[off+j]) << uint(8*j)
				}
				if math.Float64frombits(bits) == fd.DefFloat {
					out[fd.Name] = true
				}
			}
		default:
			width := kindWidth(int(k.value))
			if width == 0 || width > 16 || off+width > f.bodyEnd {
				continue
			}
			switch fd.Type.Kind {
			case ir.TBool, ir.TInt, ir.TBits, ir.TFixed, ir.TNamed:
			default:
				continue
			}
			if fd.Type.Kind == ir.TNamed {
				if _, ok := fd.Type.Ref.(*ir.Flags); !ok {
					continue
				}
			}
			raw := make([]byte, width)
			for j := 0; j < width; j++ {
				raw[width-1-j] = data[off+j]
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
				want.Add(want, new(big.Int).Lsh(big.NewInt(1), uint(width*8)))
			}
			if got.Cmp(want) == 0 {
				out[fd.Name] = true
			}
		}
	}
	return out
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
	schema := []byte("package rootelision\ntable Root {\n n int32 = 7\n f float32 = 1.5\n title string(8) = \"abc\"\n values [..4]uint8\n entries map[int32]int32\n keep uint8 | required\n}\n")
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
		{"float default", []string{"f"}, cat(b(1, 10), f32(1.5), b(0)), "f"},
		{"float nondefault", []string{"f"}, cat(b(1, 10), f32(2.5), b(0)), ""},
		{"text default", []string{"title"}, cat(b(1, 12, 3), []byte("abc"), b(0)), "title"},
		{"text nondefault", []string{"title"}, cat(b(1, 12, 3), []byte("def"), b(0)), ""},
		{"empty counted array", []string{"values"}, b(1, 14, 2, 6, 0, 0), "values"},
		{"default array element", []string{"values"}, b(1, 14, 3, 6, 1, 0, 0), ""},
		{"default map key", []string{"entries", "key"}, b(1, 14, 10, 13, 1, 7, 2, 4, 0, 0, 0, 0, 0, 0), ""},
		{"required default", []string{"keep"}, b(1, 6, 0, 0), ""},
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
