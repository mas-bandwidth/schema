package gotable

// nested_union_forged_tag: when an inner union sits inside an outer union arm,
// the compiled Form-3 plan's None constant entry for the inner union is GUARDED
// by the outer union's tag (guard != tableFixedNoGuard). A forged inner tag
// past the writer's inner arm count must still refuse the strict load as
// out_of_range on both the compiled lineage plan and the identity plan.

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedVersioningNestedUnionForgedTag(t *testing.T) {
	const older = `package nestedforged

fixed table Leaf
{
    m int32
}

union Inner
{
    alpha Leaf
    beta  int32
}

fixed table Wrap
{
    inner Inner
}

union Outer
{
    wrap  Wrap
    other int32
}

fixed table Lineage
{
    outer Outer
    seq   int32
}
`
	const newer = `package nestedforged

fixed table Leaf
{
    m int32
}

union Inner
{
    alpha Leaf
    beta  int32
    gamma int64
}

fixed table Wrap
{
    inner Inner
}

union Outer
{
    wrap  Wrap
    other int32
}

fixed table Lineage
{
    outer Outer
    seq   int32
}
`
	tmp := t.TempDir()
	wirePath := filepath.Join(tmp, "old_nested_union.bin")

	saveSrc := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestSaveOld(t *testing.T) {
	var v Lineage
	LineageReset(&v)
	v.Outer.Type = OuterTypeWrap
	v.Outer.Wrap.Inner.Type = InnerTypeAlpha
	v.Outer.Wrap.Inner.Alpha.M = 7
	v.Seq = 15
	buf := make([]byte, LineageFixedMeasure(1))
	n := LineageFixedSave([]Lineage{v}, buf)
	if n != int64(len(buf)) {
		t.Fatalf("save: got %%d, want %%d", n, len(buf))
	}
	if err := os.WriteFile(%q, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}
`, wirePath)
	if out, err := runVersionProbe(t, older, nil, saveSrc); err != nil {
		t.Fatalf("save old nested union fixture: %v\n%s", err, out)
	}

	checkSrc := fmt.Sprintf(`package probe

import ("bytes"; "os"; "testing")

func TestNestedUnionForgedTag(t *testing.T) {
	raw, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{0x01, 0x01, 0x07, 0x00, 0x00, 0x00, 0x0f, 0x00, 0x00, 0x00}
	at := bytes.Index(raw, needle)
	if at < 0 {
		t.Fatal("nested_union_forged_tag: record needle not found")
	}
	if bytes.Contains(raw[at+1:], needle) {
		t.Fatal("nested_union_forged_tag: record needle matched more than once")
	}
	for _, forgedTag := range []byte{3, 0xc5} {
		data := append([]byte(nil), raw...)
		data[at+1] = forgedTag
		clear(data[at+2 : at+6])

		back := make([]Lineage, 2)
		back[0].Seq = 99
		var r TableReport
		plan := make([]TableFixedEntry, 1024)
		n := LineageFixedLoad(back, data, plan, &r)
		if n != -1 {
			t.Fatalf("tag 0x%%02x: expected refusal (-1), got n=%%d %%+v", forgedTag, n, r)
		}
		if r.Verdict != TableOpenRefused || r.Reason != "out_of_range" || !r.FixedRaised {
			t.Fatalf("tag 0x%%02x: expected out_of_range refusal, got %%+v", forgedTag, r)
		}
		if r.Unknown != 0 || r.Widened != 0 {
			t.Fatalf("tag 0x%%02x: counters moved on refusal: %%+v", forgedTag, r)
		}
	}
}
`, wirePath)

	if out, err := runVersionProbe(t, newer, []string{older}, checkSrc); err != nil {
		t.Fatalf("nested_union_forged_tag (compiled plan): %v\n%s", err, out)
	}
	if out, err := runVersionProbe(t, older, nil, checkSrc); err != nil {
		t.Fatalf("nested_union_forged_tag (identity plan): %v\n%s", err, out)
	}
}
