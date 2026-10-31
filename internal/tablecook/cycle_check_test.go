package tablecook_test

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A COOK IS NEVER CYCLIC (docs/SPEC-TABLES.md §3.1, §7.4): `Lock` refuses a
// data cycle and a cook is a locked region written verbatim, so references
// that close a cycle are a cook no writer produced, and `schema cook-check`
// refuses one by name. The forgery takes the chain fixture, a -> b, and points
// b back at a; every other check it makes still passes, so the cycle is the
// whole event.
func TestCookCheckRefusesACycleByName(t *testing.T) {
	u := unit(t, "../../tables/pointers")
	m := tabletext.NewModel(u)
	var chain graph
	for _, g := range graphs() {
		if g.name == "chain-and-alias" {
			chain = g
		}
	}
	valid, err := tablecook.Cook(m, chain.make(m), tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tablecook.Check(m, valid); err != nil {
		t.Fatalf("the chain fixture checks clean before the forgery: %v", err)
	}
	h, err := tablecook.ReadHeader(valid, ir.BuildVersion(u), ir.RecordLayout(u, m.Lookup("Scene")).Align)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := h.Directory(valid)
	if err != nil {
		t.Fatal(err)
	}
	listNode := ir.TableTypeId(m.Lookup("ListNode").WireName())
	var lists []int64
	for _, e := range dir {
		if e.TypeId == listNode {
			lists = append(lists, e.Offset)
		}
	}
	if len(lists) != 2 {
		t.Fatalf("the chain fixture holds %d ListNode nodes, and this forgery needs two", len(lists))
	}
	next := refSlot(t, u, m, "ListNode", "next")
	forged := append([]byte(nil), valid...)
	ord := binary.LittleEndian
	for i, from := range lists {
		to := lists[1-i]
		at := from + next
		ord.PutUint64(forged[h.DataOffset()+at:], uint64(to-at))
	}
	_, err = tablecook.Check(m, forged)
	var cycle *tablecook.CycleRefusal
	if !errors.As(err, &cycle) {
		t.Fatalf("a cyclic cook was not refused as a cycle: %v", err)
	}
	if len(cycle.Nodes) != 2 {
		t.Fatalf("the refusal names %v, and the cycle is two nodes", cycle.Nodes)
	}
}
