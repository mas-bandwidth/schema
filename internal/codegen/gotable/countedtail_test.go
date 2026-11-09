package gotable

import "testing"

// TestCountedStructTailResetWork is the positive gate for the
// go-counted-tail-work negative control: Reset of a counted struct array
// resets every element, including the tail past the live count, so a
// reused value carries no stale element into its next load.
func TestCountedStructTailResetWork(t *testing.T) {
	runGenerated(t, `package probe
fixed table Child { n int32 }
table Root { items [..3]Child }
`, `package probe
import("testing")
func TestCountedStructTail(t *testing.T){
 var v Root
 v.Items[2].N=99; v.ItemsCount=1
 RootReset(&v)
 if v.Items[2].N!=0 { t.Fatalf("counted tail reset work: stale struct element past live count: %+v", v.Items[2]) }
 if v.ItemsCount!=0 { t.Fatalf("counted tail reset work: count not reset: %d", v.ItemsCount) }
}
`)
}

// TestCountedUnionTailReset is the positive gate for the
// go-counted-tail-reset negative control: Reset of a counted union array
// returns every element's tag to None, including the tail past the live
// count.
func TestCountedUnionTailReset(t *testing.T) {
	runGenerated(t, `package probe
fixed table Child { n int32 }
union Choice { child Child
 empty }
table Root { choices [..3]Choice }
`, `package probe
import("testing")
func TestCountedUnionTail(t *testing.T){
 var v Root
 v.Choices[2].Type=ChoiceTypeChild; v.ChoicesCount=1
 RootReset(&v)
 if v.Choices[2].Type!=ChoiceTypeNone { t.Fatalf("counted tail not reset: stale union tag past live count: %+v", v.Choices[2].Type) }
 if v.ChoicesCount!=0 { t.Fatalf("counted tail not reset: count not reset: %d", v.ChoicesCount) }
}
`)
}
