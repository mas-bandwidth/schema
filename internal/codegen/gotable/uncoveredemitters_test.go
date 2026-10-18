package gotable

import (
	"reflect"
	"strings"
	"testing"
)

// TestGoUncoveredEmitters covers the emitter surface no other test reaches.
// GenerateOmitting is the omit path; emitRegionMessage and
// emitRetainMessageSurface are comment-only stubs, so a profile shows them at
// 0.0% however often their call sites run (a statement-less function cannot
// cover a statement). They are listed as removal candidates in the report; the
// message and retain forms below keep the behaviour their call sites frame
// under test.
func TestGoUncoveredEmitters(t *testing.T) {
	t.Parallel()
	t.Run("GenerateOmitting_omits_only_the_named_form", func(t *testing.T) {
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
	t.Run("GenerateOmitting_nothing_is_Generate", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe
fixed table Root { x int32 }
`)
		full, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		out, err := GenerateOmitting(u, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(full, out) {
			t.Fatalf("omitting no form changed the output: %d files vs %d", len(full), len(out))
		}
	})
	t.Run("message_form_round_trips", func(t *testing.T) {
		t.Parallel()
		// A unit that declares a variable table is REGIONAL, so the message
		// form of its fixed table is emitted beside the region surface. Pin
		// that the batch it writes measures and reads back.
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
	t.Run("fixed_root_refuses_retain_messages_by_name", func(t *testing.T) {
		t.Parallel()
		// A fixed table's root has no region, so the generated code refuses
		// LoadRetainMessages by name (docs/SPEC-TABLES.md §3.3, §6.6).
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
