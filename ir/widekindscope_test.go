// §15'S WIDE-KIND REFUSAL, SCOPED (docs/SPEC-TABLES.md §15, §3.4). The refusal
// is owed by the FORM-1 ACCELERATORS — the block form and the cooked form,
// which must name a kind's storage column and its reflection descriptor — and
// not by the wire: the FIXED FORM names no kind at all on its emitted path, so
// a port that lays a 128-bit field down as a sixteen-byte store at a constant
// offset carries it whether or not it can name the kind.
//
// The rule lives in ir because a rule five ports each spell for themselves is
// five rules. What is measured here is the SPLIT: a unit with no wide kind is
// untouched, a unit with them and no form-3 codec is refused WHOLE, and the
// same unit with a form-3 codec loses only the accelerators — and the refusal
// still names every field either way, because a port that quietly emitted a
// second wire for a kind it cannot name is the thing §15 exists to prevent.
package ir

import (
	"strings"
	"testing"
)

func wideScopeUnit(t *testing.T, wide bool) *Unit {
	t.Helper()
	f := &Field{Name: "plain", Type: FieldType{Kind: TInt, Width: 32}}
	st := &Struct{Name: "Probe", IsTable: true, Fields: []*Field{f}}
	if wide {
		st.Fields = append(st.Fields, &Field{Name: "big", Type: FieldType{Kind: TInt, Width: 128}})
	}
	file := &File{Base: "Probe", Tables: []*Struct{st}}
	return &Unit{Package: "probe", Files: []*File{file}, Tables: map[string]*Struct{"Probe": st}}
}

// NO WIDE KIND, NOTHING SAID, whatever the backend's form-3 state is: the scope
// is the wide kinds' and never a gate on the fixed form.
func TestWideTableKindsSilentWithoutWideKinds(t *testing.T) {
	u := wideScopeUnit(t, false)
	for _, fixed := range []bool{false, true} {
		s := WideTableKinds(u, "Probe", fixed)
		if s.Refusal != nil || s.Accelerators || s.Unit {
			t.Errorf("a unit with no wide kind says nothing (fixedForm=%v): %+v", fixed, s)
		}
	}
}

// WIDE KINDS AND NO FORM-3 CODEC: the unit is refused WHOLE, because there is
// nothing left for the backend to emit.
func TestWideTableKindsRefuseTheUnitWithoutTheFixedForm(t *testing.T) {
	u := wideScopeUnit(t, true)
	s := WideTableKinds(u, "Probe", false)
	if s.Refusal == nil {
		t.Fatal("a unit that declares the wide kinds is refused")
	}
	if !s.Unit || !s.Accelerators {
		t.Errorf("with no form-3 codec there is nothing left to emit: %+v", s)
	}
	for _, want := range []string{"Probe", "big", "§15"} {
		if !strings.Contains(s.Refusal.Error(), want) {
			t.Errorf("the refusal must name %q: %s", want, s.Refusal)
		}
	}
}

// WIDE KINDS AND A FORM-3 CODEC: only the ACCELERATORS stand down, and the
// refusal is still the sentence a port prints about them.
func TestWideTableKindsScopeToTheAcceleratorsWithTheFixedForm(t *testing.T) {
	u := wideScopeUnit(t, true)
	s := WideTableKinds(u, "Probe", true)
	if s.Refusal == nil {
		t.Fatal("the fields are still named")
	}
	if s.Unit {
		t.Error("a unit with a form-3 codec is not refused whole")
	}
	if !s.Accelerators {
		t.Error("but the block form and the cooked form stand down")
	}
}
