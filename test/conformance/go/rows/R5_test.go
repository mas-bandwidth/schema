package rows

import (
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestRowR5 asserts: "the digest carries every range, every resolution
// (tag 'Q') and every reader limit (tag 'L'), and a flags type deduped
// by name, once."  (docs/FIXED-FORM-ALGORITHM.md:1130 row 10;
// docs/SPEC-TABLES.md §5.2, bill §13, `ir/fixedform.go` TableFixedLayoutHash).
//
// We build one struct that exercises ALL four facets at once:
//   - HasIntRange on an int field  → tag 'R' + i64 min + i64 max
//   - HasFloatRange+Resolution    → tag 'R' + f64bits min + f64bits max + tag 'Q' + f64bits step
//   - Two fields reference the SAME Flags type               → tag 'F' ONCE (deduped by bare name)
//
// Three assertions follow:
//  1. Every tag appears exactly once — 'R' twice (int range + float range),
//     'Q' once (float resolution), 'F' once (flags, deduplicated).
//  2. A change to ANY facet moves the fnv1a64 layout hash
//     (TableFixedLayoutHash), which is the binding mechanism that lets an
//     old reader refuse a widened peer.
//  3. If no reader-side limit field exists in the IR types (checked via
//     reflection), then tag 'L' must NOT appear in the digest — the row
//     is RESERVED until a table-level limit spelling exists (bill §13).
func TestRowR5(t *testing.T) {
	// --- Build the production schema under test --------------------

	fl := &ir.Flags{
		Name:     "Perms",
		Variants: []string{"Read", "Write"},
		WireBits: 2,
	}
	st := &ir.Struct{
		Name: "Root",
		Fields: []*ir.Field{
			{
				Name:        "id",
				Type:        ir.FieldType{Kind: ir.TInt, Width: 32},
				HasIntRange: true,
				IntMin:      big.NewInt(0),
				IntMax:      big.NewInt(100),
			},
			{
				Name:          "temperature",
				Type:          ir.FieldType{Kind: ir.TFloat32},
				HasFloatRange: true,
				FMin:          -40.0,
				FMax:          85.0,
				Resolution:    0.5,
			},
			{
				Name: "mode_a",
				Type: ir.FieldType{Kind: ir.TNamed, Name: "Perms", Ref: fl},
			},
			{
				Name: "mode_b",
				Type: ir.FieldType{Kind: ir.TNamed, Name: "Perms", Ref: fl},
			},
		},
	}

	digest := ir.TableFixedDefinitionsDigest(st)
	layout := make([]byte, 32) // placeholder layout bytes

	hashA := ir.TableFixedLayoutHash(layout, st)

	// --- Assertion 1: tags present ----------------------------------

	count := func(tag byte) int {
		n := 0
		for _, b := range digest {
			if b == tag {
				n++
			}
		}
		return n
	}

	if count('R') != 2 {
		t.Fatalf("two ranges (int + float) expected, got %d 'R' in digest %x", count('R'), digest)
	}
	if count('Q') != 1 {
		t.Fatalf("one resolution tag 'Q' expected, got %d in digest %x", count('Q'), digest)
	}
	if count('F') != 1 {
		t.Fatalf("one flags entry (deduped by name, emitted once) expected, got %d 'F' in digest %x", count('F'), digest)
	}

	// --- Assertion 2: every facet moves the wire hash ---------------

	// 2a — widen the int range → hash must change
	stWide := &ir.Struct{
		Name: "Root",
		Fields: []*ir.Field{
			{
				Name:        "id",
				Type:        ir.FieldType{Kind: ir.TInt, Width: 32},
				HasIntRange: true,
				IntMin:      big.NewInt(0),
				IntMax:      big.NewInt(9999), // widened from 100
			},
			{
				Name:          "temperature",
				Type:          ir.FieldType{Kind: ir.TFloat32},
				HasFloatRange: true,
				FMin:          -40.0,
				FMax:          85.0,
				Resolution:    0.5,
			},
			{Name: "mode_a", Type: ir.FieldType{Kind: ir.TNamed, Name: "Perms", Ref: fl}},
			{Name: "mode_b", Type: ir.FieldType{Kind: ir.TNamed, Name: "Perms", Ref: fl}},
		},
	}
	hashWide := ir.TableFixedLayoutHash(layout, stWide)
	if hashA == hashWide {
		t.Fatal("widened int range must move the wire hash: this is the interop break (§5.2)")
	}

	// 2b — coarsen the float resolution → hash must change
	stCoarse := &ir.Struct{
		Name: "Root",
		Fields: []*ir.Field{
			{Name: "id", Type: ir.FieldType{Kind: ir.TInt, Width: 32}, HasIntRange: true, IntMin: big.NewInt(0), IntMax: big.NewInt(100)},
			{
				Name:          "temperature",
				Type:          ir.FieldType{Kind: ir.TFloat32},
				HasFloatRange: true,
				FMin:          -40.0,
				FMax:          85.0,
				Resolution:    1.0, // coarsened from 0.5
			},
			{Name: "mode_a", Type: ir.FieldType{Kind: ir.TNamed, Name: "Perms", Ref: fl}},
			{Name: "mode_b", Type: ir.FieldType{Kind: ir.TNamed, Name: "Perms", Ref: fl}},
		},
	}
	hashCoarse := ir.TableFixedLayoutHash(layout, stCoarse)
	if hashA == hashCoarse {
		t.Fatal("coarsened float resolution (tag 'Q') must move the wire hash")
	}

	// 2c — rename the flags variants → hash must change
	flChanged := &ir.Flags{
		Name:     "Perms",
		Variants: []string{"Execute", "Delete"}, // different names
		WireBits: 2,
	}
	stFl := &ir.Struct{
		Name: "Root",
		Fields: []*ir.Field{
			{Name: "id", Type: ir.FieldType{Kind: ir.TInt, Width: 32}, HasIntRange: true, IntMin: big.NewInt(0), IntMax: big.NewInt(100)},
			{Name: "temperature", Type: ir.FieldType{Kind: ir.TFloat32}, HasFloatRange: true, FMin: -40.0, FMax: 85.0, Resolution: 0.5},
			{Name: "mode_a", Type: ir.FieldType{Kind: ir.TNamed, Name: "Perms", Ref: flChanged}},
			{Name: "mode_b", Type: ir.FieldType{Kind: ir.TNamed, Name: "Perms", Ref: flChanged}},
		},
	}
	hashFl := ir.TableFixedLayoutHash(layout, stFl)
	if hashA == hashFl {
		t.Fatal("changed flags variants must move the wire hash")
	}

	// --- Assertion 3: tag 'L' is reserved ---------------------------

	hasLimit := irTypeHasLimitField(ir.Field{}) || irTypeHasLimitField(*st)
	emitsL := false
	for _, b := range digest {
		if b == 'L' {
			emitsL = true
			break
		}
	}
	switch {
	case hasLimit && !emitsL:
		t.Fatal("'L' is reserved until a reader-side limit field exists; if the IR has one, the digest must emit it")
	case !hasLimit && emitsL:
		t.Fatal("'L' is reserved; when the IR has no limit field, the digest must not contain 'L'")
	}
}

// irTypeHasLimitField returns true when t (or its element, if pointer) has a
// struct field whose name contains "limit" (case-insensitive), signalling that
// a reader-side limit may someday be a first-class fact.
func irTypeHasLimitField(v any) bool {
	typeOf := reflect.TypeOf(v)
	if typeOf.Kind() == reflect.Ptr {
		typeOf = typeOf.Elem()
	}
	if typeOf.Kind() != reflect.Struct {
		return false
	}
	for i := 0; i < typeOf.NumField(); i++ {
		name := strings.ToLower(typeOf.Field(i).Name)
		if strings.Contains(name, "limit") {
			return true
		}
	}
	return false
}
