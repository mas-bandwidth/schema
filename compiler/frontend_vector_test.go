package compiler

import (
	"fmt"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE FRONT-END VECTOR (docs/SPEC-CONFORMANCE.md §8, rows 26 and 27). A port
// that reads schema text resolves defaults, range ends and wire ids itself;
// this is the vector it checks them against. Two rules, each held two ways:
// by the committed vector.txt, and by a computation here that does not use the
// compiler.
//
//   - A float literal is a float64 first (SPEC.md §4.2): a float32 value is
//     float32(float64(literal)). The test also proves the vector discriminates:
//     the literal rounded ONCE to float32 is a different value.
//   - A `was` alias hashes its UTF-8 bytes (SPEC.md §4.2): every id below is
//     fnv1a64 over the alias's bytes, computed here by hand.
//
// FRONTEND_VECTOR_WRITE=1 rewrites vector.txt from the compiler.
func TestFrontEndVector(t *testing.T) {
	t.Parallel()
	const dir = "../testdata/conformance/frontend/"
	const lit = "1.00000005960464477550"
	u, err := New().Load([]string{dir + "Literals.schema"})
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables["Literals"]
	if st == nil {
		t.Fatal("no table Literals")
	}
	field := func(name string) *ir.Field {
		for _, f := range st.Fields {
			if f.Name == name {
				return f
			}
		}
		t.Fatalf("no field %s", name)
		return nil
	}

	// the float rule, without the compiler
	asF64, err := strconv.ParseFloat(lit, 64) // correctly rounded, once, to float64
	if err != nil {
		t.Fatal(err)
	}
	twice := float32(asF64)
	once, _, _ := new(big.Float).SetPrec(200).SetMode(big.ToNearestEven).Parse(lit, 10)
	onceF32, _ := once.Float32()
	if twice == onceF32 {
		t.Fatalf("the literal does not discriminate: once %v, twice %v", onceF32, twice)
	}
	tie32 := field("tie32")
	if got := float32(tie32.DefFloat); got != twice {
		t.Errorf("tie32's default is %v, want float32(float64(%s)) = %v", got, lit, twice)
	}
	if got := float32(tie32.FMin); got != twice {
		t.Errorf("tie32's min is %v, want %v", got, twice)
	}
	if got := field("tie64").DefFloat; got != asF64 {
		t.Errorf("tie64's default is %v, want %v", got, asF64)
	}
	if got := float32(field("sum32").DefFloat); got != float32(0.1+0.2) {
		t.Errorf("sum32's default is %v, want float32(0.1 + 0.2 in float64) = %v", got, float32(0.1+0.2))
	}

	// the was rule, without the compiler
	fnv := func(s string) uint64 {
		h := uint64(0xcbf29ce484222325)
		for _, b := range []byte(s) {
			h = (h ^ uint64(b)) * 0x100000001b3
		}
		return h
	}
	pick := u.TableUnions["Pick"]
	if pick == nil {
		pick = u.Unions["Pick"]
	}
	if pick == nil {
		t.Fatal("no union Pick")
	}
	ids := []struct {
		what  string
		alias string
		got   uint64
	}{
		{"table Literals", "Été", ir.TableWireId(st.WireName())},
		{"field renamed", "naïve", ir.TableFieldWireId(field("renamed"))},
		{"arm plain", "ärm", ir.TableWireId(pick.Variants[0].WireName())},
	}
	var b strings.Builder
	for _, id := range ids {
		if id.got != fnv(id.alias) {
			t.Errorf("%s: id %#016x, want fnv1a64 of the UTF-8 bytes of %q = %#016x", id.what, id.got, id.alias, fnv(id.alias))
		}
		fmt.Fprintf(&b, "id %s %x %#016x\n", strings.ReplaceAll(id.what, " ", "."), []byte(id.alias), id.got)
	}
	fmt.Fprintf(&b, "f32 tie32.default %#08x\n", math.Float32bits(float32(tie32.DefFloat)))
	fmt.Fprintf(&b, "f32 tie32.min %#08x\n", math.Float32bits(float32(tie32.FMin)))
	fmt.Fprintf(&b, "f64 tie64.default %#016x\n", math.Float64bits(field("tie64").DefFloat))
	fmt.Fprintf(&b, "f32 sum32.default %#08x\n", math.Float32bits(float32(field("sum32").DefFloat)))
	got := b.String()

	path := dir + "vector.txt"
	if os.Getenv("FRONTEND_VECTOR_WRITE") == "1" {
		head := "# THE FRONT-END VECTOR for Literals.schema (docs/SPEC-CONFORMANCE.md §8, rows 26-27).\n" +
			"# id <what> <alias UTF-8 bytes, hex> <wire id>: fnv1a64 over those bytes.\n" +
			"# f32/f64 <field>.<fact> <IEEE-754 bits>: the literal rounded to float64, then to the field's width.\n"
		if err := os.WriteFile(path, []byte(head+got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v — FRONTEND_VECTOR_WRITE=1 writes it", err)
	}
	var body []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			body = append(body, line)
		}
	}
	if want := strings.Join(body, "\n") + "\n"; want != got {
		t.Fatalf("the front-end vector moved:\n got %s\nwant %s", got, want)
	}
}
