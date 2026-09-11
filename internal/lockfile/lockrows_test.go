// EVERY ROW OF THE LAW, at the lock: docs/FIXED-FORM-VERSIONING-TESTS.md's
// LOCK-REFUSES and LOCK-ALLOWS columns, one pair per row of
// docs/FIXED-FORM-BILL-READS-BACKWARD.md §2 and §6.
//
// Glenn's ruling (§6): "lock." The lock is the law's home, so every narrowing
// the bill forbids is refused HERE, by name, with the table, the definition,
// the rule and BOTH VALUES in the sentence — and every widening the bill
// allows is accepted by `schema lock`, which is the edit that moves the file.
//
// The idiom is lockfile_test.go's: an OLD LOCK against a NEW UNIT. Each row
// writes a `before` schema, locks it, writes the row's `after`, and asks the
// check what it says. A Refuses case wants one refusal naming the rule; an
// Allows case wants `schema lock` to take it and the table's layout hash to
// MOVE, which is the lineage's one new entry (the lineage itself is the next
// step of the bill, §6b, and is not asserted here).
package lockfile_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
)

// ---- the fixture shapes ----

// pkg wraps a body in this file's package line.
func pkg(body string) string { return "package lockrows\n\n" + body }

// rowTable is the one fixed table every row edits, `Row`.
func rowTable(fields string) string {
	return pkg("fixed table Row\n{\n" + fields + "\n}\n")
}

// rowWith is [rowTable] with declarations beside it — an enum, a flags mask, a
// union, a nested type, a constant.
func rowWith(decls, fields string) string {
	return pkg(decls + "\n\nfixed table Row\n{\n" + fields + "\n}\n")
}

// ---- the two columns ----

// lockThen is the whole path a row takes: write `before`, lock it, write
// `after`, and hand back what the check says over the committed lock.
func lockThen(t *testing.T, before, after string) []error {
	t.Helper()
	dir, paths := fixture(t, before)
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatalf("locking the `before` schema: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
	return lockfile.Check(load(t, paths), paths)
}

// rowRefuses is the LOCK-REFUSES column: the narrowing does not compile
// against the lock, and the one refusal names every phrase the row's line of
// the design table asks for.
func rowRefuses(t *testing.T, before, after string, want ...string) {
	t.Helper()
	refuses(t, lockThen(t, before, after), want...)
}

// rowRefusesAmong is [rowRefuses] for a row whose edit has CONSEQUENCES the
// lock reports beside the finding — an arm removed takes its payload type out
// of the closure, a keyed array brings its enum in — so the assertion is that
// one refusal names the rule, not that it is the only sentence.
func rowRefusesAmong(t *testing.T, before, after string, want ...string) {
	t.Helper()
	errs := lockThen(t, before, after)
	for _, err := range errs {
		got := err.Error()
		all := true
		for _, w := range want {
			if !strings.Contains(got, w) {
				all = false
				break
			}
		}
		if all {
			return
		}
	}
	t.Fatalf("no refusal names %q among %d: %v", want, len(errs), errs)
}

// rowAllows is the LOCK-ALLOWS column: the widening is not a break, so
// `schema lock` takes it — and the table's layout hash MOVES, because a
// widening is a new layout and the lineage is a list of layouts.
//
// The check still refuses the widening until the lock is written: a lock that
// trails the declaration is a record of a layout nobody has. That is the same
// refusal an append has always taken, and it names `schema lock`.
func rowAllows(t *testing.T, before, after, table string) {
	t.Helper()
	dir, paths := fixture(t, before)
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatalf("locking the `before` schema: %v", err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	was := readLock(t, path)
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
	errs := lockfile.Check(load(t, paths), paths)
	for _, err := range errs {
		if !strings.Contains(err.Error(), "write it with `schema lock`") {
			t.Fatalf("a widening is a change the record must carry, not a break: %v", err)
		}
	}
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
		t.Fatalf("`schema lock` must take the widening: rewrote=%v err=%v", rewrote, err)
	}
	now := readLock(t, path)
	before1, after1 := was.Table(table), now.Table(table)
	if before1 == nil || after1 == nil {
		t.Fatalf("the lock must carry %s both sides of the widening", table)
	}
	if before1.Layout == after1.Layout {
		t.Errorf("a widening is a new layout: %s still hashes to 0x%016x", table, after1.Layout)
	}
	if len(lockfile.Check(load(t, paths), paths)) != 0 {
		t.Errorf("the lock is current after `schema lock`: %v", lockfile.Check(load(t, paths), paths))
	}
}

// rowUnchanged is the other positive: an edit that is not a change at all —
// a rename through `was` — passes the check and moves no byte of the file.
func rowUnchanged(t *testing.T, before, after string) {
	t.Helper()
	dir, paths := fixture(t, before)
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatalf("locking the `before` schema: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Fatalf("this edit is not a change: %v", errs)
	}
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || rewrote {
		t.Fatalf("nothing to write: rewrote=%v err=%v", rewrote, err)
	}
}

// ---- field_append ----

func TestLockFieldAppendRefuses(t *testing.T) {
	before := rowTable("    x int32\n    y int32\n    z int32")
	t.Run("removed", func(t *testing.T) {
		rowRefuses(t, before, rowTable("    x int32\n    y int32"),
			"fixed table Row", "field z", "field removed")
	})
	t.Run("inserted not at the end", func(t *testing.T) {
		rowRefuses(t, before, rowTable("    x int32\n    w int32\n    y int32\n    z int32"),
			"fixed table Row", "field y", "field inserted not at the end")
	})
	t.Run("reordered", func(t *testing.T) {
		rowRefuses(t, before, rowTable("    y int32\n    x int32\n    z int32"),
			"fixed table Row", "field x", "fields reordered")
	})
}

func TestLockFieldAppendAllows(t *testing.T) {
	rowAllows(t, rowTable("    x int32\n    y int32\n    z int32"),
		rowTable("    x int32\n    y int32\n    z int32\n    w int32"), "Row")
}

// ---- field_deprecate ----

func TestLockFieldDeprecateRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    a int32\n    b int32\n    c int32"),
		rowTable("    a int32\n    c int32"),
		"fixed table Row", "field b", "field removed")
}

func TestLockFieldDeprecateAllows(t *testing.T) {
	rowAllows(t, rowTable("    a int32\n    b int32\n    c int32"),
		rowTable("    a int32\n    b int32 | deprecated\n    c int32"), "Row")
}

// ---- field_undeprecate: DEPRECATION IS ONE WAY (bill §12.3) ----
//
// The lock's own first answer (`TestUnDeprecateRefused`) was the right one, and
// §12.3 restores it: "§2.10's reason stands ('what would come back is not
// data'): undeprecating is refused; §2's row is corrected." Every writer that
// ran while the field was deprecated left the DEFAULT in the slot, so a reader
// that starts believing the field again believes a number nobody wrote — and a
// deprecated field is read on every plan, identity included, so there is
// nothing to turn back on.
func TestLockFieldUndeprecateRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    a int32\n    b int32 | deprecated"),
		rowTable("    a int32\n    b int32"),
		"fixed table Row", "field b", "undeprecated")
}

// ---- field_modify ----

func TestLockFieldModifyRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    a int32"), rowTable("    a string(8)"),
		"fixed table Row", "field a", "kind changed")
}

// ---- enum_append ----

const tierEnum = "enum Tier\n{\n    Bronze\n    Silver\n    Gold\n}"

func TestLockEnumAppendRefuses(t *testing.T) {
	before := rowWith(tierEnum, "    tier Tier")
	t.Run("reordered", func(t *testing.T) {
		rowRefuses(t, before, rowWith("enum Tier\n{\n    Bronze\n    Gold\n    Silver\n}", "    tier Tier"),
			"enum Tier", "Silver", "variant reordered")
	})
	t.Run("removed", func(t *testing.T) {
		rowRefuses(t, before, rowWith("enum Tier\n{\n    Bronze\n    Silver\n}", "    tier Tier"),
			"enum Tier", "Gold", "variant removed")
	})
	t.Run("inserted mid-list", func(t *testing.T) {
		rowRefuses(t, before, rowWith("enum Tier\n{\n    Bronze\n    Platinum\n    Silver\n    Gold\n}", "    tier Tier"),
			"enum Tier", "Silver", "variant inserted mid-list")
	})
	t.Run("renamed without was", func(t *testing.T) {
		rowRefuses(t, before, rowWith("enum Tier\n{\n    Bronze\n    Silver\n    Golden\n}", "    tier Tier"),
			"enum Tier", "Gold", "variant renamed without was")
	})
}

func TestLockEnumAppendAllows(t *testing.T) {
	rowAllows(t, rowWith(tierEnum, "    tier Tier"),
		rowWith("enum Tier\n{\n    Bronze\n    Silver\n    Gold\n    Platinum\n}", "    tier Tier"), "Row")
}

// ---- enum_width: the ordinal width grows with the list, at the boundary ----

// wideEnum is an enum of n variants, V1..Vn, which is the only way a width is
// set: "a width cannot be set by hand" (the design).
func wideEnum(n int) string {
	var b strings.Builder
	b.WriteString("enum Wide\n{\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "    V%d\n", i)
	}
	b.WriteString("}")
	return b.String()
}

func TestLockEnumWidthAllows(t *testing.T) {
	rowAllows(t, rowWith(wideEnum(255), "    w Wide"), rowWith(wideEnum(256), "    w Wide"), "Row")
}

// ---- union_append ----

const pickUnion = "type Av\n{\n    x int32\n}\n\ntype Bv\n{\n    y int32\n}\n\ntype Cv\n{\n    z int32\n}\n\nunion Pick\n{\n    a Av\n    b Bv\n}"

func TestLockUnionAppendRefuses(t *testing.T) {
	before := rowWith(pickUnion, "    pick Pick")
	arms := func(body string) string {
		return "type Av\n{\n    x int32\n}\n\ntype Bv\n{\n    y int32\n}\n\ntype Cv\n{\n    z int32\n}\n\nunion Pick\n{\n" + body + "\n}"
	}
	t.Run("reordered", func(t *testing.T) {
		rowRefuses(t, before, rowWith(arms("    b Bv\n    a Av"), "    pick Pick"),
			"union Pick", "a", "arm reordered")
	})
	t.Run("removed", func(t *testing.T) {
		rowRefusesAmong(t, before, rowWith(arms("    a Av"), "    pick Pick"),
			"union Pick", "arm 2, b", "arm removed")
	})
	t.Run("inserted mid-list", func(t *testing.T) {
		rowRefusesAmong(t, before, rowWith(arms("    a Av\n    c Cv\n    b Bv"), "    pick Pick"),
			"union Pick", "arm 2, b", "arm inserted mid-list")
	})
	t.Run("payload changed", func(t *testing.T) {
		rowRefusesAmong(t, before, rowWith(arms("    a Cv\n    b Bv"), "    pick Pick"),
			"union Pick", "arm 1, a", "arm payload changed (Av -> Cv)")
	})
}

func TestLockUnionAppendAllows(t *testing.T) {
	rowAllows(t, rowWith(pickUnion, "    pick Pick"),
		rowWith("type Av\n{\n    x int32\n}\n\ntype Bv\n{\n    y int32\n}\n\ntype Cv\n{\n    z int32\n}\n\nunion Pick\n{\n    a Av\n    b Bv\n    c Cv\n}", "    pick Pick"), "Row")
}

// ---- union_arm_payload_widen ----

func armsWith(av string) string {
	return "type Av\n{\n" + av + "\n}\n\ntype Bv\n{\n    y int32\n}\n\nunion Pick\n{\n    a Av\n    b Bv\n}"
}

func TestLockUnionArmPayloadWidenRefuses(t *testing.T) {
	rowRefuses(t, rowWith(armsWith("    x int32\n    y int32"), "    pick Pick"),
		rowWith(armsWith("    x int32"), "    pick Pick"),
		"type Av", "field y", "field removed")
}

func TestLockUnionArmPayloadWidenAllows(t *testing.T) {
	rowAllows(t, rowWith(armsWith("    x int32\n    y int32"), "    pick Pick"),
		rowWith(armsWith("    x int32\n    y int32\n    z int32"), "    pick Pick"), "Av")
}

// ---- flags_append ----

func TestLockFlagsAppendRefuses(t *testing.T) {
	before := rowWith("flags Perks { Shielded, Cloaked }", "    perks Perks")
	t.Run("moved", func(t *testing.T) {
		rowRefuses(t, before, rowWith("flags Perks { Cloaked, Shielded }", "    perks Perks"),
			"flags Perks", "Shielded", "flag moved")
	})
	t.Run("removed", func(t *testing.T) {
		rowRefuses(t, before, rowWith("flags Perks { Shielded }", "    perks Perks"),
			"flags Perks", "Cloaked", "flag removed")
	})
}

func TestLockFlagsAppendAllows(t *testing.T) {
	rowAllows(t, rowWith("flags Perks { Shielded, Cloaked }", "    perks Perks"),
		rowWith("flags Perks { Shielded, Cloaked, Boosted }", "    perks Perks"), "Row")
}

// ---- array_bounded_grow ----

func TestLockArrayBoundedGrowRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    slots [..4]int32"), rowTable("    slots [..2]int32"),
		"fixed table Row", "field slots", "bound narrowed (4 -> 2)")
}

func TestLockArrayBoundedGrowAllows(t *testing.T) {
	rowAllows(t, rowTable("    slots [..4]int32"), rowTable("    slots [..8]int32"), "Row")
}

// ---- array_fixed_grow ----

func TestLockArrayFixedGrowRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    slots [4]int32"), rowTable("    slots [2]int32"),
		"fixed table Row", "field slots", "bound narrowed (4 -> 2)")
}

func TestLockArrayFixedGrowAllows(t *testing.T) {
	rowAllows(t, rowTable("    slots [4]int32"), rowTable("    slots [8]int32"), "Row")
}

// ---- array_shape ----

const fourEnum = "enum Four\n{\n    One\n    Two\n    Three\n    Four\n}"

func TestLockArrayShapeRefuses(t *testing.T) {
	t.Run("fixed to counted", func(t *testing.T) {
		rowRefuses(t, rowTable("    slots [4]int32"), rowTable("    slots [..4]int32"),
			"fixed table Row", "field slots", "shape changed (fixed -> counted)")
	})
	t.Run("fixed to keyed", func(t *testing.T) {
		// the enum is in the closure BEFORE the edit, so the shape change is
		// the only thing the lock has to say
		rowRefuses(t, rowWith(fourEnum, "    slots [4]int32\n    tier Four"),
			rowWith(fourEnum, "    slots [Four]int32\n    tier Four"),
			"fixed table Row", "field slots", "shape changed (fixed -> keyed)")
	})
}

// ---- array_elem_widen ----

func TestLockArrayElemWidenRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    slots [..4]int16"), rowTable("    slots [..4]int8"),
		"fixed table Row", "field slots", "element narrowed (int16 -> int8)")
}

func TestLockArrayElemWidenAllows(t *testing.T) {
	rowAllows(t, rowTable("    slots [..4]int16"), rowTable("    slots [..4]int32"), "Row")
}

// ---- keyed_array_enum_append ----

func TestLockKeyedArrayEnumAppendRefuses(t *testing.T) {
	rowRefuses(t, rowWith(tierEnum, "    per_tier [Tier]int32"),
		rowWith("enum Tier\n{\n    Bronze\n    Gold\n    Silver\n}", "    per_tier [Tier]int32"),
		"enum Tier", "Silver", "variant reordered")
}

func TestLockKeyedArrayEnumAppendAllows(t *testing.T) {
	rowAllows(t, rowWith(tierEnum, "    per_tier [Tier]int32"),
		rowWith("enum Tier\n{\n    Bronze\n    Silver\n    Gold\n    Platinum\n}", "    per_tier [Tier]int32"), "Row")
}

// ---- constant_grow: the lock records the EVALUATED bound ----

func TestLockConstantGrowRefuses(t *testing.T) {
	rowRefuses(t, rowWith("const N = 4", "    slots [..N]int32"),
		rowWith("const N = 2", "    slots [..N]int32"),
		"fixed table Row", "field slots", "bound narrowed (4 -> 2)")
}

func TestLockConstantGrowAllows(t *testing.T) {
	rowAllows(t, rowWith("const N = 4", "    slots [..N]int32"),
		rowWith("const N = 8", "    slots [..N]int32"), "Row")
}

// ---- string_grow / wstring_grow / bytes_grow ----

func TestLockStringGrowRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    name string(8)"), rowTable("    name string(4)"),
		"fixed table Row", "field name", "capacity narrowed (8 -> 4)")
}

func TestLockStringGrowAllows(t *testing.T) {
	rowAllows(t, rowTable("    name string(8)"), rowTable("    name string(16)"), "Row")
}

func TestLockWstringGrowRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    name wstring(8)"), rowTable("    name wstring(4)"),
		"fixed table Row", "field name", "capacity narrowed (8 -> 4)")
}

func TestLockWstringGrowAllows(t *testing.T) {
	rowAllows(t, rowTable("    name wstring(8)"), rowTable("    name wstring(16)"), "Row")
}

func TestLockBytesGrowRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    blob bytes(8)"), rowTable("    blob bytes(4)"),
		"fixed table Row", "field blob", "capacity narrowed (8 -> 4)")
}

func TestLockBytesGrowAllows(t *testing.T) {
	rowAllows(t, rowTable("    blob bytes(8)"), rowTable("    blob bytes(16)"), "Row")
}

// ---- text_kind ----

func TestLockTextKindRefuses(t *testing.T) {
	before := rowTable("    name string(8)")
	t.Run("string to wstring", func(t *testing.T) {
		rowRefuses(t, before, rowTable("    name wstring(8)"),
			"fixed table Row", "field name", "kind changed")
	})
	t.Run("string to bytes", func(t *testing.T) {
		rowRefuses(t, before, rowTable("    name bytes(8)"),
			"fixed table Row", "field name", "kind changed")
	})
}

// ---- int_widen ----

func TestLockIntWidenRefuses(t *testing.T) {
	before := rowTable("    n int16")
	t.Run("narrowed", func(t *testing.T) {
		rowRefuses(t, before, rowTable("    n int8"),
			"fixed table Row", "field n", "narrowed (int16 -> int8)")
	})
	t.Run("signedness", func(t *testing.T) {
		rowRefuses(t, before, rowTable("    n uint16"),
			"fixed table Row", "field n", "signedness changed (int16 -> uint16)")
	})
	t.Run("ladder", func(t *testing.T) {
		rowRefuses(t, before, rowTable("    n float32"),
			"fixed table Row", "field n", "ladder changed (int16 -> float32)")
	})
}

func TestLockIntWidenAllows(t *testing.T) {
	t.Run("to int32", func(t *testing.T) {
		rowAllows(t, rowTable("    n int16"), rowTable("    n int32"), "Row")
	})
	t.Run("to int64", func(t *testing.T) {
		rowAllows(t, rowTable("    n int16"), rowTable("    n int64"), "Row")
	})
}

// ---- uint_widen ----

func TestLockUintWidenRefuses(t *testing.T) {
	before := rowTable("    n uint16")
	t.Run("narrowed", func(t *testing.T) {
		rowRefuses(t, before, rowTable("    n uint8"),
			"fixed table Row", "field n", "narrowed (uint16 -> uint8)")
	})
	t.Run("signedness", func(t *testing.T) {
		rowRefuses(t, before, rowTable("    n int16"),
			"fixed table Row", "field n", "signedness changed (uint16 -> int16)")
	})
}

func TestLockUintWidenAllows(t *testing.T) {
	rowAllows(t, rowTable("    n uint16"), rowTable("    n uint32"), "Row")
}

// ---- float_widen ----

func TestLockFloatWidenRefuses(t *testing.T) {
	t.Run("narrowed", func(t *testing.T) {
		rowRefuses(t, rowTable("    f float64"), rowTable("    f float32"),
			"fixed table Row", "field f", "narrowed (float64 -> float32)")
	})
	t.Run("ladder", func(t *testing.T) {
		rowRefuses(t, rowTable("    f float32"), rowTable("    f int32"),
			"fixed table Row", "field f", "ladder changed (float32 -> int32)")
	})
}

func TestLockFloatWidenAllows(t *testing.T) {
	rowAllows(t, rowTable("    f float32"), rowTable("    f float64"), "Row")
}

// ---- range_widen ----

func TestLockRangeWidenRefuses(t *testing.T) {
	before := rowTable("    hp int32 = 50 | min = 0, max = 100")
	t.Run("max inward", func(t *testing.T) {
		rowRefuses(t, before, rowTable("    hp int32 = 50 | min = 0, max = 50"),
			"fixed table Row", "field hp", "range narrowed", "[0, 100]", "[0, 50]")
	})
	t.Run("min inward", func(t *testing.T) {
		rowRefuses(t, before, rowTable("    hp int32 = 50 | min = 10, max = 100"),
			"fixed table Row", "field hp", "range narrowed", "[0, 100]", "[10, 100]")
	})
}

func TestLockRangeWidenAllows(t *testing.T) {
	before := rowTable("    hp int32 = 50 | min = 0, max = 100")
	t.Run("wider", func(t *testing.T) {
		rowAllows(t, before, rowTable("    hp int32 = 50 | min = 0, max = 200"), "Row")
	})
	t.Run("unranged", func(t *testing.T) {
		rowAllows(t, before, rowTable("    hp int32 = 50"), "Row")
	})
}

// ---- range_added ----

func TestLockRangeAddedRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    hp int32 = 50"), rowTable("    hp int32 = 50 | min = 0, max = 100"),
		"fixed table Row", "field hp", "range added where none was", "[0, 100]")
}

// ---- bits_grow ----

func TestLockBitsGrowRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    mask bits(8)"), rowTable("    mask bits(4)"),
		"fixed table Row", "field mask", "bits narrowed (8 -> 4)")
}

func TestLockBitsGrowAllows(t *testing.T) {
	rowAllows(t, rowTable("    mask bits(8)"), rowTable("    mask bits(12)"), "Row")
}

// ---- fixed_I_grow ----

func TestLockFixedIGrowRefuses(t *testing.T) {
	before := rowTable("    pos fixed(12, 4) | min = -8, max = 7")
	t.Run("F changed", func(t *testing.T) {
		rowRefuses(t, before, rowTable("    pos fixed(8, 8) | min = -8, max = 7"),
			"fixed table Row", "field pos", "F changed (4 -> 8)")
	})
	t.Run("I narrowed", func(t *testing.T) {
		rowRefuses(t, before, rowTable("    pos fixed(4, 4) | min = -8, max = 7"),
			"fixed table Row", "field pos", "I narrowed (12 -> 4)")
	})
	// I AND F INSIDE ONE STORAGE WIDTH, which is the half the row's line did
	// not spell: SPEC §4.6 makes I + F EQUAL a storage width, so with F held a
	// narrowed I IS a narrowed kind (the subtest above), and the only
	// same-storage move left is I against F — a moved SCALE, which takes the F
	// sentence. The two subtests together are the whole of `I(a) <= I(b) and
	// F(a) == F(b)` (docs/FIXED-FORM-ALGORITHM.md §5.1) at the lock.
	t.Run("I narrowed against F, same storage", func(t *testing.T) {
		rowRefuses(t, rowTable("    pos fixed(28, 4) | min = -8, max = 7"),
			rowTable("    pos fixed(24, 8) | min = -8, max = 7"),
			"fixed table Row", "field pos", "F changed (4 -> 8)")
	})
	// AN ARRAY'S ELEMENT, one level in, and the one place the refusal did not
	// hold this file's own law ("BOTH VALUES in the sentence"): it named the
	// element's kind twice, "element I narrowed (fixed -> fixed)", where the
	// lock has the element's I recorded all along.
	t.Run("element I narrowed", func(t *testing.T) {
		rowRefuses(t, rowTable("    pos [4]fixed(28, 4) | min = -8, max = 7"),
			rowTable("    pos [4]fixed(12, 4) | min = -8, max = 7"),
			"fixed table Row", "field pos", "element I narrowed (28 -> 12)")
	})
}

func TestLockFixedIGrowAllows(t *testing.T) {
	rowAllows(t, rowTable("    pos fixed(12, 4) | min = -8, max = 7"),
		rowTable("    pos fixed(28, 4) | min = -8, max = 7"), "Row")
}

// THE POSITIVE HALF OF THE ELEMENT ROW: an element's I widens with F held, so
// the raw scaled value every older writer packed is the same number in a wider
// slot, and `schema lock` takes it.
func TestLockFixedIGrowElementAllows(t *testing.T) {
	rowAllows(t, rowTable("    pos [4]fixed(12, 4) | min = -8, max = 7"),
		rowTable("    pos [4]fixed(28, 4) | min = -8, max = 7"), "Row")
}

// ---- optional_add ----

func TestLockOptionalAddRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    hp ?int32"), rowTable("    hp int32"),
		"fixed table Row", "field hp", "optional removed")
}

func TestLockOptionalAddAllows(t *testing.T) {
	rowAllows(t, rowTable("    hp int32"), rowTable("    hp ?int32"), "Row")
}

// ---- nested_append ----

func vecType(fields string) string { return "type Vec\n{\n" + fields + "\n}" }

func TestLockNestedAppendRefuses(t *testing.T) {
	rowRefuses(t, rowWith(vecType("    x int32\n    y int32\n    z int32"), "    v Vec"),
		rowWith(vecType("    x int32\n    y int32"), "    v Vec"),
		"type Vec", "field z", "field removed")
}

func TestLockNestedAppendAllows(t *testing.T) {
	rowAllows(t, rowWith(vecType("    x int32\n    y int32\n    z int32"), "    v Vec"),
		rowWith(vecType("    x int32\n    y int32\n    z int32\n    w int32"), "    v Vec"), "Vec")
}

// ---- default_change ----

func TestLockDefaultChangeRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    a int32 = 1"), rowTable("    a int32 = 2"),
		"fixed table Row", "field a", "default changed (1 -> 2)")
}

// ---- keyword_change ----

func TestLockKeywordChangeRefuses(t *testing.T) {
	t.Run("fixed removed", func(t *testing.T) {
		rowRefuses(t, rowTable("    a int32"), pkg("table Row\n{\n    a int32\n}\n"),
			"Row", "fixed removed")
	})
	t.Run("fixed added", func(t *testing.T) {
		rowRefusesAmong(t, rowWith(vecType("    x int32"), "    v Vec"),
			rowWith("fixed table Vec\n{\n    x int32\n}", "    v Vec"),
			"type Vec", "fixed added")
	})
}

// ---- rename_without_was ----

func TestLockRenameWithoutWasRefuses(t *testing.T) {
	rowRefuses(t, rowTable("    a int32"), rowTable("    b int32"),
		"fixed table Row", "field a", "renamed without was")
}

func TestLockRenameWithoutWasAllows(t *testing.T) {
	rowUnchanged(t, rowTable("    a int32"), rowTable("    b int32 | was = \"a\""))
}

// ---- retire_strands_older (Rowan's ruling, bill §11.4) ----
//
// THE FLOOR IS AN INDEX CUT, not a set of hashes: a reader built from the lock
// accepts lineage indices at or above one past the highest RETIRED entry
// (docs/FIXED-FORM-ALGORITHM.md §5.2). So retiring entry 3 of 5 would stop
// serving 0, 1 and 2 as well — three layouts retired by arithmetic, with no
// mark and no reason beside them, and nothing in the file saying a person meant
// it. These rows are the refusal that closes that, and the word an operator
// says when the sweep is what they want.

// fiveLayouts locks `Row` five times, each lock a lawful widening (one more
// int32 field), and hands back the five wire hashes OLDEST FIRST plus the
// unit's paths and the lock's path.
func fiveLayouts(t *testing.T) (paths []string, path string, hashes []uint64) {
	t.Helper()
	dir, paths := fixture(t, rowTable("    a int32"))
	fields := "    a int32"
	for i := range 5 {
		if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
			t.Fatalf("lock %d: %v", i, err)
		}
		h, _, _ := wireHashOf(t, paths, "Row")
		hashes = append(hashes, h)
		if i == 4 {
			break
		}
		fields += fmt.Sprintf("\n    f%d int32", i)
		if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(rowTable(fields)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path = filepath.Join(dir, lockfile.FileName)
	if n := len(lineageOf(t, path, "Row")); n != 5 {
		t.Fatalf("the fixture wants five lineage entries, got %d", n)
	}
	return paths, path, hashes
}

// TestLockRetireMiddleStrandsOlderRefuses is the ruling: retiring the MIDDLE
// entry of five is refused, and the refusal names the stranded entries BY INDEX
// AND HASH so the operator can read them off the sentence.
func TestLockRetireMiddleStrandsOlderRefuses(t *testing.T) {
	paths, path, hashes := fiveLayouts(t)
	_, _, err := lockfile.Retire(load(t, paths), paths, fmt.Sprintf("Row@0x%016x", hashes[3]), "the 0.6 client is gone", false)
	if err == nil {
		t.Fatal("retiring entry 3 of 5 strands 0, 1 and 2: it must be refused")
	}
	got := err.Error()
	for _, want := range []string{
		"STRAND",
		"3 unretired older entries",
		fmt.Sprintf("index 0 (0x%016x)", hashes[0]),
		fmt.Sprintf("index 1 (0x%016x)", hashes[1]),
		fmt.Sprintf("index 2 (0x%016x)", hashes[2]),
		"--all-below",
		"§11.4",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal must say %q: %v", want, got)
		}
	}
	// and NOTHING is written: a refused retirement leaves the record alone
	for i, e := range lineageOf(t, path, "Row") {
		if e.Retired {
			t.Errorf("a refused retirement writes nothing: entry %d is marked", i)
		}
	}
}

// TestLockRetireAllBelowRetiresThemToo is the other half: --all-below is the
// word, and it marks every older entry with the SAME REASON, so the floor and
// the file say the same thing.
func TestLockRetireAllBelowRetiresThemToo(t *testing.T) {
	paths, path, hashes := fiveLayouts(t)
	const reason = "every client below 0.7 is gone"
	if _, rewrote, err := lockfile.Retire(load(t, paths), paths, fmt.Sprintf("Row@0x%016x", hashes[3]), reason, true); err != nil || !rewrote {
		t.Fatalf("--all-below takes the sweep: rewrote=%v err=%v", rewrote, err)
	}
	got := lineageOf(t, path, "Row")
	if len(got) != 5 {
		t.Fatalf("a retired entry STAYS in the lineage: %d entries", len(got))
	}
	for i := range 4 {
		if !got[i].Retired {
			t.Errorf("entry %d is retired by --all-below: %+v", i, got[i])
		}
		if got[i].Reason != reason {
			t.Errorf("entry %d carries the same reason, got %q", i, got[i].Reason)
		}
	}
	if got[4].Retired {
		t.Errorf("the CURRENT entry is never retired: %+v", got[4])
	}
	if f := lockfile.Floor(readLock(t, path), "Row"); f != 4 {
		t.Errorf("the floor is one past the highest retired index: %d", f)
	}
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Fatalf("a retirement is not a break: %v", errs)
	}
}

// TestLockRetireOldestAllowed is the case that strands nothing: the oldest
// entry has nothing below it, so no flag is needed — and then the NEXT one up
// is allowed too, because everything below IT is already retired.
func TestLockRetireOldestAllowed(t *testing.T) {
	paths, path, hashes := fiveLayouts(t)
	if _, rewrote, err := lockfile.Retire(load(t, paths), paths, fmt.Sprintf("Row@0x%016x", hashes[0]), "the first client is gone", false); err != nil || !rewrote {
		t.Fatalf("the oldest entry strands nothing: rewrote=%v err=%v", rewrote, err)
	}
	if f := lockfile.Floor(readLock(t, path), "Row"); f != 1 {
		t.Errorf("floor after retiring index 0: %d", f)
	}
	// walking UP one at a time never strands: index 1's only older entry is
	// already retired
	if _, rewrote, err := lockfile.Retire(load(t, paths), paths, fmt.Sprintf("Row@0x%016x", hashes[1]), "and the second", false); err != nil || !rewrote {
		t.Fatalf("index 1 over a retired index 0 strands nothing: rewrote=%v err=%v", rewrote, err)
	}
	if f := lockfile.Floor(readLock(t, path), "Row"); f != 2 {
		t.Errorf("floor after retiring index 1: %d", f)
	}
	got := lineageOf(t, path, "Row")
	if got[0].Reason == got[1].Reason {
		t.Errorf("each retirement keeps its own sentence: %q %q", got[0].Reason, got[1].Reason)
	}
}

// TestLockRetireCurrentStillRefused is the rule the ruling does not touch: the
// current layout is never retired, with or without --all-below, because a
// reader built from this lock reads its own records.
func TestLockRetireCurrentStillRefused(t *testing.T) {
	paths, _, hashes := fiveLayouts(t)
	for _, allBelow := range []bool{false, true} {
		_, _, err := lockfile.Retire(load(t, paths), paths, fmt.Sprintf("Row@0x%016x", hashes[4]), "no", allBelow)
		if err == nil {
			t.Fatalf("the CURRENT layout is never retired (--all-below=%v)", allBelow)
		}
		if !strings.Contains(err.Error(), "CURRENT layout") {
			t.Errorf("the refusal names the current layout: %v", err)
		}
	}
}
