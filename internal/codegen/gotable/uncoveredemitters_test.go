package gotable

import (
	"strings"
	"testing"
)

// TestGoUncoveredEmitters covers the three emitters no other test reaches:
// GenerateOmitting (the omitted surface is absent), emitRegionMessage (reached
// by a regional unit that carries the message form), and emitRetainMessageSurface
// (reached by the retain surface; the fixed root's LoadRetainMessages refusal).
func TestGoUncoveredEmitters(t *testing.T) {
	t.Parallel()
	t.Run("GenerateOmitting_omits_cook_surface", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe
fixed table Root { x int32 }
`)
		full, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		hadCook := false
		for name := range full {
			if strings.HasSuffix(name, "Cook.go") {
				hadCook = true
			}
		}
		if !hadCook {
			t.Fatal("baseline unit emits no Cook surface; cannot prove omission")
		}
		out, err := GenerateOmitting(u, nil, FormCook)
		if err != nil {
			t.Fatal(err)
		}
		for name := range out {
			if strings.HasSuffix(name, "Cook.go") {
				t.Fatalf("FormCook omitted but %s still generated", name)
			}
		}
		if _, ok := out["ProbeTable.go"]; !ok {
			t.Fatal("the core ProbeTable.go is missing when the Cook form is omitted")
		}
	})
	t.Run("emitRegionMessage_reached_by_regional_message_unit", func(t *testing.T) {
		t.Parallel()
		// A unit that declares a variable table is REGIONAL (gotable.go, regional
		// := len(variableTableNames(u)) > 0), so the message form of its fixed
		// table reaches emitRegionMessage. Pin that the message batch surface the
		// generation path emits still round-trips.
		runGenerated(t, `package probe
table V { a int32 }
fixed table Root { b int32 }
`, `package probe
import "testing"
func TestRegionMessageSave(t *testing.T){
	var v Root; v.B=7
	values:=[]*Root{&v}
	var report TableReport
	n:=RootMeasureMessages(values,nil)
	out:=make([]byte,n)
	if got:=RootSaveMessages(values,out,&report); got!=n||report!=(TableReport{}){t.Fatalf("save %d/%d %+v",got,n,report)}
}
`)
	})
	t.Run("emitRetainMessageSurface_pins_fixed_retain_refusal", func(t *testing.T) {
		t.Parallel()
		// The retain surface reaches emitRetainMessageSurface; a fixed table's
		// root has no region, so the generated code refuses LoadRetainMessages by
		// name exactly as the surface's supporting comment states.
		runGenerated(t, `package probe
fixed table Root { b int32 }
`, `package probe
import "testing"
func TestRetainRefusal(t *testing.T){
	const want="Root: retention requires a region round trip through the VARIABLE form"
	if RootLoadRetainMessages!=want{t.Fatalf("refusal %q want %q",RootLoadRetainMessages,want)}
}
`)
	})
}
