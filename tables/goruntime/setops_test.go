package goruntime

import "testing"

// setVocabulary is a vocabulary of n distinct ids, as AnnounceRead leaves one:
// every entry its own first, so a reference names exactly one mark.
func setVocabulary(n int) *TableVocabulary {
	v := &TableVocabulary{}
	v.Init(make([]TableMessageEntry, n))
	for i := range n {
		v.Entries[i].ID = uint64(1000 + i)
	}
	v.Count = int64(n)
	return v
}

// TestGeneratedMessageSetDuplicateIsLinear counts the looks the generated
// reader's keyed and skipped-body duplicate test takes (GO-1, schema review of
// 2026-11-21). This is the shared runtime the compiler emits into every Go
// unit, byte for byte (TestGoSharedRuntimePackageIsCurrent), so the count is
// the generated reader's. Over sets of growing size the looks a slot are a
// fixed number, so the count grows with the slot count and never with its
// square; the old test was slices.Contains over the keys seen so far. A count,
// not a clock: the bound is the work, whatever the machine.
func TestGeneratedMessageSetDuplicateIsLinear(t *testing.T) {
	t.Parallel()
	var perSlot uint64
	for _, n := range []int{64, 512, 4000} {
		v := setVocabulary(n)
		level := TableMessageSetOpen(v)
		if level < 0 {
			t.Fatalf("n=%d: the set would not open", n)
		}
		for ref := 1; ref <= n; ref++ {
			if TableMessageSetRepeat(v, level, uint64(ref)) {
				t.Fatalf("n=%d: ref %d called a repeat on its first naming", n, ref)
			}
		}
		if v.setProbes%uint64(n) != 0 {
			t.Fatalf("n=%d: %d looks, not a whole number a slot", n, v.setProbes)
		}
		k := v.setProbes / uint64(n)
		if perSlot == 0 {
			perSlot = k
		}
		if k != perSlot || k > 2 {
			t.Fatalf("n=%d: %d looks a slot, want %d (at most 2)", n, k, perSlot)
		}
		// the last slot named twice is caught in one more look
		before := v.setProbes
		if !TableMessageSetRepeat(v, level, uint64(n)) {
			t.Fatalf("n=%d: the repeat of the last slot was not caught", n)
		}
		if v.setProbes-before > 2 {
			t.Fatalf("n=%d: catching the repeat took %d looks", n, v.setProbes-before)
		}
		TableMessageSetClose(v)
	}
}

// TestGeneratedMessageSetsKeepOuterMarks: a set nested inside another, at
// depth, does not erase the outer set's marks, and a closed set's marks do not
// leak into a sibling. The outer level's looks stay bounded too.
func TestGeneratedMessageSetsKeepOuterMarks(t *testing.T) {
	t.Parallel()
	v := setVocabulary(8)
	outer := TableMessageSetOpen(v)
	if TableMessageSetRepeat(v, outer, 3) {
		t.Fatal("outer: first naming of 3 called a repeat")
	}
	for depth := range 40 {
		inner := TableMessageSetOpen(v)
		if TableMessageSetRepeat(v, inner, 3) {
			t.Fatalf("depth %d: the inner set's first naming of 3 called a repeat", depth)
		}
	}
	for range 40 {
		TableMessageSetClose(v)
	}
	if !TableMessageSetRepeat(v, outer, 3) {
		t.Fatal("outer: 3 named again after the inner sets closed was not caught")
	}
	sibling := TableMessageSetOpen(v)
	if TableMessageSetRepeat(v, sibling, 3) {
		t.Fatal("a fresh sibling set saw the closed sets' mark of 3")
	}
}
