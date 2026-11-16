package gotable

import "testing"

// Dead counted slots are zero regardless of their stored pointers (§7.6).
// The same rule applies when the unit also carries optional pointer closures.
func TestCookIgnoresDeadSlotPointers(t *testing.T) {
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

import ("bytes";"testing")

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

	// A cook with stale dead storage is the same artifact as cleared storage.
 deadRef:=v.Nodes[1]
 v.Nodes[1]=0
 size:=RootCookMeasure(v,&b.Arena)
 if size<=0 {t.Fatal("live pointer baseline measure")}
 for _,order:=range []TableByteOrder{TableByteOrderLittle,TableByteOrderBig} {
  want:=make([]byte,size)
  if !RootCookFrom(v,want,order,&b.Arena){t.Fatal("live pointer baseline cook")}
  v.Nodes[1]=deadRef
  if n,err:=RootCookMeasureReason(v,&b.Arena);n!=size||err!=nil{t.Fatalf("dead pointer changed measure: %d %v",n,err)}
  got:=bytes.Repeat([]byte{0xa5},int(size))
  if RootCookFrom(v,got[:len(got)-1],order,&b.Arena)||!bytes.Equal(got,bytes.Repeat([]byte{0xa5},len(got))){t.Fatal("short cook changed output")}
  if !RootCookFrom(v,got,order,&b.Arena)||!bytes.Equal(got,want){t.Fatal("dead pointer changed cook bytes")}
  v.Nodes[1]=0
 }
 v.Nodes[1]=deadRef

	// §2.3: count 2 reaches both slots, so both pointers ride and it cooks.
	v.NodesCount = 2
	cooked := make([]byte, RootCookMeasure(v, &b.Arena))
 if int64(len(cooked))<=size {t.Fatal("newly live node was not carried")}
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
