package rusttable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCardVariableModulesEmitTheForm1Framing pins docs/SPEC-TABLES.md §3, §3.1,
// §5: the variable wire's first stage emits the form byte, the tolerant read
// report and, per card field, the two facts §3's field header is made of — the
// field's wire id and its kind byte. The kind bytes it emits are the same
// bytes the committed card-populated.wire carries (string 0x0c, enum 0x1e,
// int32 0x04, int64 0x05), so the constants are pinned against the Go leg's
// evidence rather than against this backend's own arithmetic alone.
func TestCardVariableModulesEmitTheForm1Framing(t *testing.T) {
	t.Parallel()

	unit := unitFrom(t, cardShapeSrc)
	modules, err := cardVariableModules(unit)
	if err != nil {
		t.Fatalf("the card constructs were refused from the variable path: %v", err)
	}

	runtime, ok := modules[VariableRuntimeModule+".rs"]
	if !ok {
		t.Fatalf("the card constructs emitted no %s; got %v", VariableRuntimeModule+".rs", keys(modules))
	}
	if !strings.Contains(string(runtime), "pub const TABLE_WIRE_FORM: u8 = 1") {
		t.Error("the runtime does not carry the form byte 1 (docs/SPEC-TABLES.md §3)")
	}

	var body string
	for name, data := range modules {
		if strings.HasSuffix(name, "_variable.rs") {
			body = string(data)
		}
	}
	if body == "" {
		t.Fatalf("the card constructs emitted no *_variable.rs module; got %v", keys(modules))
	}

	card := unit.Tables["Card"]
	for _, want := range []struct {
		field string
		kind  int
	}{
		{"id", ir.TableKindString},            // 0x0c on the wire
		{"tier", ir.TableKindEnum},            // 0x1e on the wire
		{"deadline_minutes", ir.TableKindI32}, // 0x04 on the wire
		{"attempt", ir.TableKindI64},          // 0x05 on the wire
	} {
		f := findField(card, want.field)
		if f == nil {
			t.Fatalf("the card has no field %q", want.field)
		}
		if kind := ir.TableWireFieldKind(f); kind != want.kind {
			t.Errorf("%s: kind %d, want %d", want.field, kind, want.kind)
		}
		wantConst := fmt.Sprintf("(0x%016x, %d)", ir.TableFieldWireId(f), want.kind)
		if !strings.Contains(body, wantConst) {
			t.Errorf("%s: the variable module does not carry its field header %s", want.field, wantConst)
		}
	}
}

// TestCardVariablePathRefusesScoped pins the scoping the boundary owes: the
// refusal runs on the VARIABLE WIRE PATH alone (§11). A unit carrying the card
// slice is emitted; a unit carrying the slice plus one excluded construct is
// refused naming table, field and construct — never mis-encoded.
func TestCardVariablePathRefusesScoped(t *testing.T) {
	t.Parallel()

	unit := unitFrom(t, cardShapeSrc)
	if _, err := cardVariableModules(unit); err != nil {
		t.Errorf("the card slice was refused from the variable path: %v", err)
	}

	// wstring(N) rides the variable wire as a field (docs/SPEC-TABLES.md §3,
	// kind 33; TestWideTextOnTheTableWireRust), so it is no longer the
	// excluded construct here: the fixed-point kind still is (§15)
	withWide := cardUnit(&ir.Field{Name: "title", Type: ir.FieldType{Kind: ir.TWString, Size: 16}})
	if _, err := cardVariableModules(withWide); err != nil {
		t.Errorf("a unit with a wstring(N) field was refused from the variable path: %v", err)
	}

	withFixed := cardUnit(&ir.Field{Name: "rate", Type: ir.FieldType{Kind: ir.TFixed, Width: 32, Signed: true}})
	_, err := cardVariableModules(withFixed)
	if err == nil {
		t.Fatal("a unit with a fixed-point field was not refused from the variable path")
	}
	for _, want := range []string{"Card", "rate", "fixed-point"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err, want)
		}
	}
}

func findField(st *ir.Struct, name string) *ir.Field {
	for _, f := range st.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
