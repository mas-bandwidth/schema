package ir

import "fmt"

// NO ZERO-WIDTH INTEGER IS EVER VALID (owner decision, Glenn, 2026-10-16). An
// integer a backend PRINTS — a `uint%d_t` cast or storage, a `put%d` / `get%d`
// store or load — is 1, 2, 4 or 8 bytes, and nothing else. A width of 0 is not
// a small integer: it is a field kind the emitter mistook for a scalar (a text,
// bytes or array union arm read as an element was the case that shipped
// `uint0_t` and `table_fixed_put0`), and the only correct answer is to refuse
// it by name before a compiler sees it.
//
// The 128-bit kinds are not on this list on purpose: they have their own
// spellings (`put128_i` / `put128_u`, `serialize_uint128_t`) and never reach a
// `uint%d_t` format.

// PrintedIntegerWidthOK reports whether bytes is a width a backend may print
// as a C integer: 1, 2, 4 or 8.
func PrintedIntegerWidthOK(bytes int64) bool {
	switch bytes {
	case 1, 2, 4, 8:
		return true
	}
	return false
}

// PrintedIntegerWidthRefusal is THE ONE CHECK every printed integer width goes
// through: nil for 1, 2, 4 or 8 bytes, and otherwise a refusal that names the
// table, the field and the width, so the build stops on a sentence instead of a
// `uint0_t` the C compiler has never heard of.
func PrintedIntegerWidthRefusal(table, field string, bytes int64) error {
	if PrintedIntegerWidthOK(bytes) {
		return nil
	}
	if table == "" {
		table = "(unnamed)"
	}
	return fmt.Errorf("table %s, field %s: the C emitter would print a %d-byte integer (uint%d_t); a printed integer is 1, 2, 4 or 8 bytes and no zero-width integer is ever valid — this is an emitter bug, refused by name rather than emitted",
		table, field, bytes, bytes*8)
}
