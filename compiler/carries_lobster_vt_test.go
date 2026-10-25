//go:build schema_leg_lobster

package compiler

// The Lobster table surface carries text lists, their total and unbounded
// text (docs/PORT-LOBSTER.md, THE CARD MAP); this extends the non-Go carrier
// map carries_test.go holds without touching that file, keeping the entries
// already there.
func init() {
	nonGoCarriers["lobster"] = append(nonGoCarriers["lobster"], carryTextList, carryTextListTotal, carryUnboundedText)
}
