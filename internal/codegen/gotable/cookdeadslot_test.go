package gotable

import "testing"

// TestCookRefusesADeadSlotPointer pins docs/SPEC-TABLES.md §7.6's refusal for a
// non-null pointer in storage the numbering walk did not reach, in a unit that
// also holds §2.3's `?T` over a variable closure: the writer's null-for-absent
// gate keys on presence and does not swallow a dead counted-array slot.
func TestCookRefusesADeadSlotPointer(t *testing.T) {
	t.Parallel()
	runGenerated(t, cookDeadSlotSchema, cookDeadSlotProbe)
}

const cookDeadSlotSchema = `package probe

table Inner
{
    text *string
}

table Node
{
    value int32
}

table Root
{
    inner ?Inner
    nodes [..2]*Node
}
`

const cookDeadSlotProbe = `package probe

import "testing"

func TestDeadSlot(t *testing.T) {
	t.Parallel()
	var b RootBuilder
	if !b.Init() {
		t.Fatal("init")
	}
	defer b.Shutdown()
	v := b.GetRoot()

	v.NodesCount = 1
	if NodeEmplace(&b.Main, &v.Nodes[0]) == nil {
		t.Fatal("emplace live")
	}
	dead := NodeEmplace(&b.Main, &v.Nodes[1])
	if dead == nil {
		t.Fatal("emplace dead")
	}
	dead.Value = 9
	if v.Nodes[1] == 0 {
		t.Fatal("the dead slot lost its pointer; the case proves nothing")
	}

	// §7.6: the walk did not reach slot 2, so the cook refuses it.
	if RootCookFrom(v, make([]byte, 1<<12), TableByteOrderLittle, &b.Arena) {
		t.Fatal("a dead counted slot holding a non-null pointer was cooked")
	}
	if n, err := RootCookMeasureReason(v, &b.Arena); n != -1 || err != TableRefuseInvalidValue {
		t.Fatalf("CookMeasureReason = %d, %v; want -1, %v", n, err, TableRefuseInvalidValue)
	}
	if n := RootCookMeasure(v, &b.Arena); n != -1 {
		t.Fatalf("CookMeasure = %d; want -1", n)
	}

	// §2.3: count 2 reaches both slots, so both pointers ride and it cooks.
	v.NodesCount = 2
	cooked := make([]byte, RootCookMeasure(v, &b.Arena))
	if len(cooked) == 0 || !RootCookFrom(v, cooked, TableByteOrderLittle, &b.Arena) {
		t.Fatal("count 2 with both pointers set was refused")
	}

	// §2.3: an absent ?T still cooks; its stale reference is written null.
	var a RootBuilder
	if !a.Init() {
		t.Fatal("init")
	}
	defer a.Shutdown()
	av := a.GetRoot()
	av.InnerPresent = true
	TableStringEmplace(&a.Main, &av.Inner.Text, []byte("stale"))
	if av.Inner.Text == 0 {
		t.Fatal("the absent value lost its stale pointer; the case proves nothing")
	}
	av.InnerPresent = false
	cooked = make([]byte, RootCookMeasure(av, &a.Arena))
	if len(cooked) == 0 || !RootCookFrom(av, cooked, TableByteOrderLittle, &a.Arena) {
		t.Fatal("an absent ?T over a variable closure was refused")
	}
}
`
