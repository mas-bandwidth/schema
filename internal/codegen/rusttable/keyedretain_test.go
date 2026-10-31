package rusttable

import (
	"strings"
	"testing"
)

// A KEYED SLOT WRITTEN AGAIN DISCARDS WHAT WAS RETAINED UNDER IT
// (docs/SPEC-TABLES.md §6.6; the fuzz vector keyed_slot_rewrite_discards_retained
// holds the behaviour against the oracle). The oracle forgets a keyed table
// slot's instance before refilling it, so records retained under the body the
// slot loses go with it. This is the toolchain-free witness that the emitter
// still spells it, with the REVERSED WITNESS taking the discard out.
func TestKeyedSlotRewriteDiscardsRetainedRust(t *testing.T) {
	t.Parallel()
	const probe = `package probe

enum Slot { Left, Right }

table Inner
{
    x int32
}

table Holder
{
    slots [Slot]Inner
}
`
	var all strings.Builder
	for _, src := range generate(t, probe) {
		all.Write(src)
	}
	body := rustFn(all.String(), "holder_load_body")
	if body == "" {
		t.Fatal("no generated holder_load_body")
	}
	want := "if let Some(ret) = retain.as_deref_mut() { retain_discard_body(ret, path.step(0, slot as u32)); }"
	if !strings.Contains(body, want) {
		t.Errorf("a keyed table slot is refilled without discarding the records retained under it")
	}
	if strings.Contains(strings.Replace(body, want, "", 1), want) {
		t.Errorf("the reversed witness still passes, so the check cannot go red")
	}
}
