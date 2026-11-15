package tablewire

import "github.com/mas-bandwidth/schema/v2/ir"

// THE ELEMENT KINDS ARE CLOSED (docs/SPEC-TABLES.md §3): an array body (kind
// 14) opens with one of 1–13 (bool; i8, i16, i32, i64; u8, u16, u32, u64;
// f32, f64; string; table), 15 (union), 17 (pointer), 18–29 (i128, u128 and
// the fixed-point family) or 30 (enum). An enum-keyed body (kind 16) opens
// with 1–11, 13, 18–29 or 30 — the array list without 12, 15 and 17.
//
// Every OTHER element kind is damage — under ANY count, a count of 0 included,
// in a declared field and in an unknown one alike (§3, §4) — because no
// declaration produces it, so no reader can hold a declaration it agrees with.
// The header's element kind byte is judged before any comparison with the
// reader's declared element kind: damage is answered before a kind mismatch.
func arrayElementKindAllowed(kind int) bool {
	switch {
	case kind >= ir.TableKindBool && kind <= ir.TableKindTable:
		return true
	case kind == ir.TableKindUnion, kind == ir.TableKindPointer:
		return true
	case kind >= ir.TableKindI128 && kind <= ir.TableKindUFixed128:
		return true
	case kind == ir.TableKindEnum:
		return true
	}
	return false
}

func keyedElementKindAllowed(kind int) bool {
	switch {
	case kind >= ir.TableKindBool && kind <= ir.TableKindF64:
		return true
	case kind == ir.TableKindTable:
		return true
	case kind >= ir.TableKindI128 && kind <= ir.TableKindUFixed128:
		return true
	case kind == ir.TableKindEnum:
		return true
	}
	return false
}
