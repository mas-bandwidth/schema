package gotable

// THE ROADMAP'S INTEGER AND FLOAT ROWS ON THE GO LEG (docs/roadmap.sexp node
// `fixed-tables`, ROADMAP.md "NEW Fixed Tables"): signed-integers,
// unsigned-integers, signed-128, unsigned-128, ranged-integers, bits-values,
// float32-values, float64-values, compressed-floats, signed-fixed-point and
// unsigned-fixed-point. Law: docs/SPEC-TABLES.md §3.4 (THE FIXED FORM: every
// field at its DECLARED STORAGE WIDTH) and docs/FIXED-FORM-ALGORITHM.md §5.2
// (C(T)'s constant-size table), §5.9's compressed-float rulings ("it RIDES AS
// THE FLOAT ... no canonicalisation"). One subtest per task id, table-driven,
// t.Parallel() first.
//
// Every task is a [capability]: the leg WRITES and READS the construct with
// non-default and BOUNDARY values, asserted. Each subtest therefore does two
// things. First it holds the emitted layout to the declared width — the layout
// entry's kind and constant size, the Go storage type, and the emitted layout
// bytes equal to the ir walk's — which RUNS under a bare
// `go test ./internal/codegen/gotable/ -run TestFixedRoadmapGoT09IntegersFloats`.
// Then it actually WRITES a record with the field at its non-default boundary
// and READS it back through the leg's own emitted `TFixedSave`/`TFixedLoad`,
// compiling and running the generated unit in a throwaway module. That half
// goes through slowtest.Gate (it shells out to the Go toolchain) and so runs
// where a gate promises it: SCHEMA_REQUIRE_CORPUS=1 (the card's own STEP 13
// gate) or SCHEMA_SLOW=1. A width whose storage is the sibling `serialize.go`
// runtime (128-bit) names that runtime and skips rather than fakes it.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t09Field is one declared field's expected fixed-form facts: the layout entry's
// kind and constant size, and the Go storage spelling (the declared storage
// width in bytes, the identity plan's copy of exactly this).
type t09Field struct {
	name   string
	kind   int
	size   int64
	goType string
}

// t09Case is one roadmap row: the static fixture (every declared width), the
// fields it declares, the schema the write/read probe is compiled against, and
// the probe source itself.
type t09Case struct {
	id     string
	schema string
	fields []t09Field
	rt     string
	probe  string
}

// t09Roundtrip is the probe's one helper: write ONE T through the leg's emitted
// combined writer, read it back through the leg's emitted combined reader, and
// insist the load was clean (docs/SPEC-TABLES.md §3.4 record form).
const t09Roundtrip = `
func roundtrip(t *testing.T, v T) T {
	t.Helper()
	buf := make([]byte, TFixedMeasure(1))
	if n := TFixedSave([]T{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save returned %d, want %d", n, len(buf))
	}
	back := make([]T, 1)
	plan := make([]TableFixedEntry, 256)
	var r TableReport
	if n := TFixedLoad(back, buf, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("load returned %d, report %+v", n, r)
	}
	return back[0]
}
`

// t09Source assembles a probe file: the package clause, the imports, the one
// round-trip helper and the case's own tests.
func t09Source(imports, tests string) string {
	return "package probe\n\nimport (\n" + imports + ")\n" + t09Roundtrip + "\n" + tests + "\n"
}

// t09Check holds one schema's fixed table T to the facts its fields declare.
// Every fact is read off the LAW (ir.TableFixedWalkRoot, the one walk that
// produces the layout and the plan) and then off the EMITTED module, so the leg
// cannot disagree with the page: the layout entry's kind and size, the Go
// storage type, and the emitted module's own layout bytes equal to the walk's.
func t09Check(t *testing.T, schema string, fields []t09Field) {
	t.Helper()
	u := unitFrom(t, schema)
	st := u.Tables["T"]
	if st == nil {
		t.Fatal("the fixture declares no fixed table T")
	}
	walk := ir.TableFixedWalkRoot(st)
	byNote := map[string]ir.TableFixedLayoutEntry{}
	for _, e := range walk {
		byNote[e.Note] = e
	}
	for _, want := range fields {
		e, ok := byNote[want.name]
		if !ok {
			t.Errorf("the layout walk has no entry for %q", want.name)
			continue
		}
		if e.Kind != want.kind {
			t.Errorf("%s: the layout kind = %d, want %d (§3.4: the field rides under its declared kind)", want.name, e.Kind, want.kind)
		}
		if e.Size != want.size {
			t.Errorf("%s: the layout size = %d, want %d (§3.4: the declared storage width)", want.name, e.Size, want.size)
		}
		f := fieldByName(st, want.name)
		if f == nil {
			t.Errorf("the table declares no field %q", want.name)
			continue
		}
		if got := goFieldType(f.Type); got != want.goType {
			t.Errorf("%s: the Go storage type = %s, want %s (the declared storage width)", want.name, got, want.goType)
		}
	}
	// THE EMITTED MODULE CARRIES THE SAME LAYOUT (the writer seals it, the
	// reader walks it) and answers both entry points.
	src := tableFile(generate(t, schema))
	if !strings.Contains(src, "func TFixedSave(") || !strings.Contains(src, "func TFixedLoad(") {
		t.Errorf("the module does not carry both TFixedSave and TFixedLoad")
	}
	emitted := t02GoLayoutBytes(t, src)
	law := ir.TableFixedLayoutBytes(walk)
	if string(emitted) != string(law) {
		t.Errorf("the emitted layout is % x, want the walk's % x", emitted, law)
	}
}

func fieldByName(st *ir.Struct, name string) *ir.Field {
	for _, f := range st.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func TestFixedRoadmapGoT09IntegersFloats(t *testing.T) {
	t.Parallel()
	for _, tc := range t09Cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			t09Check(t, tc.schema, tc.fields)
			t.Run("write-read", func(t *testing.T) {
				t.Parallel()
				schema := tc.rt
				if schema == "" {
					schema = tc.schema
				}
				t09Run(t, schema, tc.probe)
			})
		})
	}
}

// t09Run generates the schema's table unit, writes it with the probe into a
// throwaway module and runs `go test` there. A schema whose emitted module
// imports the sibling `serialize.go` runtime (a 128-bit storage) names that
// runtime and skips where it is not staged, never faking the read.
func t09Run(t *testing.T, schema, probe string) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	needSerialize := false
	for _, data := range files {
		// The emitted unit IMPORTS the sibling runtime only for a 128-bit
		// storage; every module mentions it in the header comment, so match the
		// import path, not the bare word.
		if strings.Contains(string(data), `"github.com/mas-bandwidth/serialize.go"`) {
			needSerialize = true
		}
	}
	mod := "module probe\n\ngo 1.26\n"
	if needSerialize {
		runtime, ok := goSerializeDir(t)
		if !ok {
			if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
				t.Fatalf("the sibling runtime serialize.go is absent at %s (SPEC-TABLES §3.4: the 128-bit store is serialize.go's)", runtime)
			}
			t.Skipf("the 128-bit store reads through the sibling serialize.go runtime (SPEC-TABLES §3.4), not staged at %s", runtime)
		}
		mod += fmt.Sprintf("\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime)
	}
	out := map[string][]byte{"go.mod": []byte(mod), "values_test.go": []byte(probe)}
	maps.Copy(out, files)
	dir := t.TempDir()
	for name, data := range out {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated fixed-table runtime: %v\n%s", err, b)
	}
}

// goSerializeDir resolves the sibling serialize.go checkout whose 128-bit
// storage type the wide rows' generated module imports (SPEC-TABLES §3.4: the
// 128-bit store is serialize.go's). It takes the directory the Makefile names by
// the SERIALIZE_GO variable (make/go.mk:7) when the environment sets it, so the
// bench can stage the sibling anywhere; otherwise it is the
// `../../../../serialize.go` checkout beside this one. ok is false when the
// checkout is not staged at the resolved path.
func goSerializeDir(t *testing.T) (string, bool) {
	t.Helper()
	dir := os.Getenv("SERIALIZE_GO")
	if dir == "" {
		var err error
		dir, err = filepath.Abs("../../../../serialize.go")
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(dir); err != nil {
		return dir, false
	}
	return dir, true
}

// t09CaseByID returns the t09 case with the given roadmap id.
func t09CaseByID(id string) (t09Case, bool) {
	for _, tc := range t09Cases {
		if tc.id == id {
			return tc, true
		}
	}
	return t09Case{}, false
}

// TestFixed128BitRowsRunWithTheSerializeSibling resolves the sibling
// serialize.go runtime as t09Run does and runs the signed-128 and unsigned-128
// valid-data rows. Those rows compile generated code against the sibling, so
// this is a functional test behind slowtest.Gate, never a unit test. With
// SCHEMA_REQUIRE_CORPUS set and the sibling absent the row fails by name;
// unset, it skips by name.
func TestFixed128BitRowsRunWithTheSerializeSibling(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain and the serialize.go sibling")
	for _, id := range []string{"signed-128/go/valid-data", "unsigned-128/go/valid-data"} {
		tc, ok := t09CaseByID(id)
		if !ok {
			t.Fatalf("the t09 table names no case %q", id)
		}
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			schema := tc.rt
			if schema == "" {
				schema = tc.schema
			}
			t09Run(t, schema, tc.probe)
		})
	}
}

var t09Cases = []t09Case{
	{
		id: "signed-integers/go/valid-data",
		schema: `package probe
fixed table T {
    a int8 = -128
    b int16 = -32768
    c int32 = -2147483648
    d int64 = -9223372036854775808
}
`,
		fields: []t09Field{
			{"a", ir.TableKindI8, 1, "int8"},
			{"b", ir.TableKindI16, 2, "int16"},
			{"c", ir.TableKindI32, 4, "int32"},
			{"d", ir.TableKindI64, 8, "int64"},
		},
		// Signed integers, 8/16/32/64 bits: each width at its MINIMUM and its
		// MAXIMUM and a cross-sign pair, written and read at its declared
		// storage (SPEC-TABLES §3.4).
		probe: t09Source("\t\"testing\"\n", `
func TestSignedIntegers(t *testing.T) {
	for _, v := range []T{
		{A: -128, B: -32768, C: -2147483648, D: -9223372036854775808},
		{A: 127, B: 32767, C: 2147483647, D: 9223372036854775807},
		{A: -1, B: 0, C: 1, D: -2},
	} {
		if got := roundtrip(t, v); got != v {
			t.Fatalf("round trip: got %+v want %+v", got, v)
		}
	}
}`),
	},
	{
		id: "unsigned-integers/go/valid-data",
		schema: `package probe
fixed table T {
    a uint8 = 255
    b uint16 = 65535
    c uint32 = 4294967295
    d uint64 = 18446744073709551615
}
`,
		fields: []t09Field{
			{"a", ir.TableKindU8, 1, "uint8"},
			{"b", ir.TableKindU16, 2, "uint16"},
			{"c", ir.TableKindU32, 4, "uint32"},
			{"d", ir.TableKindU64, 8, "uint64"},
		},
		probe: t09Source("\t\"testing\"\n", `
func TestUnsignedIntegers(t *testing.T) {
	for _, v := range []T{
		{A: 255, B: 65535, C: 4294967295, D: 18446744073709551615},
		{A: 0, B: 0, C: 0, D: 0},
		{A: 1, B: 256, C: 65536, D: 4294967296},
	} {
		if got := roundtrip(t, v); got != v {
			t.Fatalf("round trip: got %+v want %+v", got, v)
		}
	}
}`),
	},
	{
		id: "signed-128/go/valid-data",
		schema: `package probe
fixed table T {
    a int128 = -170141183460469231731687303715884105728 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
}
`,
		fields: []t09Field{
			{"a", ir.TableKindI128, 16, "serialize.Int128"},
		},
		// Signed 128: two's complement, 16 bytes, the high lane set at the type
		// minimum and all ones at the maximum.
		probe: t09Source("\t\"testing\"\n\n\tserialize \"github.com/mas-bandwidth/serialize.go\"\n", `
func TestSigned128(t *testing.T) {
	for _, v := range []T{
		{A: serialize.Int128{Lo: 0, Hi: 0x8000000000000000}},
		{A: serialize.Int128{Lo: 0xFFFFFFFFFFFFFFFF, Hi: 0x7FFFFFFFFFFFFFFF}},
		{A: serialize.Int128{Lo: 1, Hi: 0}},
	} {
		if got := roundtrip(t, v); got != v {
			t.Fatalf("round trip: got %+v want %+v", got, v)
		}
	}
}`),
	},
	{
		id: "unsigned-128/go/valid-data",
		schema: `package probe
fixed table T {
    a uint128 = 340282366920938463463374607431768211455
}
`,
		fields: []t09Field{
			{"a", ir.TableKindU128, 16, "serialize.Uint128"},
		},
		probe: t09Source("\t\"testing\"\n\n\tserialize \"github.com/mas-bandwidth/serialize.go\"\n", `
func TestUnsigned128(t *testing.T) {
	for _, v := range []T{
		{A: serialize.Uint128{Lo: 0xFFFFFFFFFFFFFFFF, Hi: 0xFFFFFFFFFFFFFFFF}},
		{A: serialize.Uint128{Lo: 0, Hi: 0}},
		{A: serialize.Uint128{Lo: 1, Hi: 0}},
	} {
		if got := roundtrip(t, v); got != v {
			t.Fatalf("round trip: got %+v want %+v", got, v)
		}
	}
}`),
	},
	{
		id: "ranged-integers/go/valid-data",
		schema: `package probe
fixed table T {
    a int32 | min = -5, max = 5
    b int64 | min = -1000000000000, max = 1000000000000
    c uint16 | min = 0, max = 1000
}
`,
		fields: []t09Field{
			{"a", ir.TableKindI32, 4, "int32"},
			{"b", ir.TableKindI64, 8, "int64"},
			{"c", ir.TableKindU16, 2, "uint16"},
		},
		// A range narrows the value, never the storage: each field at the
		// range's own ends, written and read at its width's kind.
		probe: t09Source("\t\"testing\"\n", `
func TestRangedIntegers(t *testing.T) {
	for _, v := range []T{
		{A: -5, B: -1000000000000, C: 0},
		{A: 5, B: 1000000000000, C: 1000},
		{A: 0, B: 0, C: 7},
	} {
		if got := roundtrip(t, v); got != v {
			t.Fatalf("round trip: got %+v want %+v", got, v)
		}
	}
}`),
	},
	{
		id: "bits-values/go/valid-data",
		schema: `package probe
fixed table T {
    a bits(3)
    b bits(12)
    c bits(20)
    d bits(40)
}
`,
		fields: []t09Field{
			{"a", ir.TableKindU8, 4, "uint32"},
			{"b", ir.TableKindU16, 4, "uint32"},
			{"c", ir.TableKindU32, 4, "uint32"},
			{"d", ir.TableKindU64, 8, "uint64"},
		},
		// bits(N) at its top value (all N bits set) and zero.
		probe: t09Source("\t\"testing\"\n", `
func TestBitsValues(t *testing.T) {
	for _, v := range []T{
		{A: 7, B: 4095, C: 1048575, D: 1099511627775},
		{A: 0, B: 0, C: 0, D: 0},
		{A: 1, B: 2, C: 3, D: 4},
	} {
		if got := roundtrip(t, v); got != v {
			t.Fatalf("round trip: got %+v want %+v", got, v)
		}
	}
}`),
	},
	{
		id: "float32-values/go/valid-data",
		schema: `package probe
fixed table T {
    a float32 = 3.5
    b float32 = -0.5
}
`,
		fields: []t09Field{
			{"a", ir.TableKindF32, 4, "float32"},
			{"b", ir.TableKindF32, 4, "float32"},
		},
		// A float32 rides as its IEEE-754 bit pattern, four bytes, no
		// canonicalisation (SPEC-TABLES §3.4, ALGORITHM:182): the top of the
		// range and the subnormal floor both round-trip.
		probe: t09Source("\t\"math\"\n\t\"testing\"\n", `
func TestFloat32(t *testing.T) {
	for _, v := range []T{
		{A: 3.5, B: -0.5},
		{A: math.MaxFloat32, B: math.SmallestNonzeroFloat32},
		{A: math.SmallestNonzeroFloat32, B: math.MaxFloat32},
	} {
		if got := roundtrip(t, v); got != v {
			t.Fatalf("round trip: got %+v want %+v", got, v)
		}
	}
}`),
	},
	{
		id: "float64-values/go/valid-data",
		schema: `package probe
fixed table T {
    a float64 = 2.5
    b float64 = -1234.5
}
`,
		fields: []t09Field{
			{"a", ir.TableKindF64, 8, "float64"},
			{"b", ir.TableKindF64, 8, "float64"},
		},
		probe: t09Source("\t\"math\"\n\t\"testing\"\n", `
func TestFloat64(t *testing.T) {
	for _, v := range []T{
		{A: 2.5, B: -1234.5},
		{A: math.MaxFloat64, B: math.SmallestNonzeroFloat64},
		{A: math.SmallestNonzeroFloat64, B: math.MaxFloat64},
	} {
		if got := roundtrip(t, v); got != v {
			t.Fatalf("round trip: got %+v want %+v", got, v)
		}
	}
}`),
	},
	{
		id: "compressed-floats/go/valid-data",
		schema: `package probe
fixed table T {
    a float32 = 0.5 | min = 0.0, max = 1.0, resolution = 0.01
    b float32 = 0.5 | min = -1.0, max = 1.0, resolution = 0.001
}
`,
		fields: []t09Field{
			{"a", ir.TableKindF32, 4, "float32"},
			{"b", ir.TableKindF32, 4, "float32"},
		},
		// A compressed float RIDES AS THE FLOAT (ALGORITHM:423) — min, max and
		// resolution are definitions, never wire — so the value moves as the
		// float32 bit pattern and lands bit-exact, including the range ends.
		probe: t09Source("\t\"testing\"\n", `
func TestCompressedFloats(t *testing.T) {
	for _, v := range []T{
		{A: 0.25, B: -0.5},
		{A: 0.0, B: -1.0},
		{A: 1.0, B: 1.0},
	} {
		if got := roundtrip(t, v); got != v {
			t.Fatalf("round trip: got %+v want %+v", got, v)
		}
	}
}`),
	},
	{
		id: "signed-fixed-point/go/valid-data",
		schema: `package probe
fixed table T {
    a fixed(16, 16) | min = -8, max = 8
    b fixed(48, 16) | min = -100, max = 100
    c fixed(112, 16) | min = -1000, max = 1000
}
`,
		rt: `package probe
fixed table T {
    a fixed(16, 16) | min = -8, max = 8
    b fixed(48, 16) | min = -100, max = 100
}
`,
		fields: []t09Field{
			{"a", ir.TableKindFixed32, 4, "int32"},
			{"b", ir.TableKindFixed64, 8, "int64"},
			{"c", ir.TableKindFixed128, 16, "serialize.Int128"},
		},
		// fixed(I,F) is the RAW SCALED INTEGER two's complement
		// (ALGORITHM:183): the field at its width's kind, the raw value at the
		// range's own ends (value * 2^F).
		probe: t09Source("\t\"testing\"\n", `
func TestSignedFixedPoint(t *testing.T) {
	for _, v := range []T{
		{A: -8 << 16, B: -100 << 16},
		{A: 8 << 16, B: 100 << 16},
		{A: 0, B: 0},
		{A: -1, B: 1},
	} {
		if got := roundtrip(t, v); got != v {
			t.Fatalf("round trip: got %+v want %+v", got, v)
		}
	}
}`),
	},
	{
		id: "unsigned-fixed-point/go/valid-data",
		schema: `package probe
fixed table T {
    a ufixed(16, 16) | min = 0, max = 16
    b ufixed(48, 16) | min = 0, max = 100
    c ufixed(112, 16) | min = 0, max = 1000
}
`,
		rt: `package probe
fixed table T {
    a ufixed(16, 16) | min = 0, max = 16
    b ufixed(48, 16) | min = 0, max = 100
}
`,
		fields: []t09Field{
			{"a", ir.TableKindUFixed32, 4, "uint32"},
			{"b", ir.TableKindUFixed64, 8, "uint64"},
			{"c", ir.TableKindUFixed128, 16, "serialize.Uint128"},
		},
		probe: t09Source("\t\"testing\"\n", `
func TestUnsignedFixedPoint(t *testing.T) {
	for _, v := range []T{
		{A: 0, B: 0},
		{A: 16 << 16, B: 100 << 16},
		{A: 1, B: 1},
	} {
		if got := roundtrip(t, v); got != v {
			t.Fatalf("round trip: got %+v want %+v", got, v)
		}
	}
}`),
	},
}
