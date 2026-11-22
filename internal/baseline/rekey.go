package baseline

import "fmt"

// keyRides judges an ENUM-KEYED array whose KEY enum was swapped for another
// — `[Team]T` respelled `[Axis]T` (docs/SPEC-TABLES.md §2.4, §18.2).
//
// The two wires answer differently, and this file is the table wire's. On the
// TYPE wire `[E]T`, `[E.Max]T` and a constant bound of the same size are one
// layout and one protocol id (SPEC §4.6): the slots are positions, and a rekey
// renames them. On the TABLE wire a slot rides under its variant's NAME id
// (§3.2), so a stored slot reaches the new key enum only where a variant of
// the same name is declared there. Every other stored slot is dropped and
// counted `unknown`, and every slot of the new enum that no stored name
// reaches reads its default. The load is not refused, so it is the baseline
// that refuses the edit, and TestEnumRekeyPredictionAgreesWithTheRead pins
// the sentence against a real read of a saved file.
func keyRides(want, have map[string]bool, was, now string) []Finding {
	if have == nil {
		return []Finding{{Refuse, "", now + " is not described by this baseline"}}
	}
	missing := len(want) - carried(want, have)
	if missing == 0 {
		return nil
	}
	return []Finding{{Refuse, "", fmt.Sprintf(
		"the key vocabulary changed: %d of %s's %d variant names are not declared by %s — a keyed slot rides under its variant's NAME id, so each stored slot keyed by one of them is dropped and counts unknown, and the slots of %s that no stored name reaches read their defaults (docs/SPEC-TABLES.md §3.2)",
		missing, was, len(want), now, now)}}
}
