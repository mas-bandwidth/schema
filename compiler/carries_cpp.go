package compiler

func init() {
	// The C++ reference carries the count-bounded text list (§2.13), while
	// unbounded text/count and text-list totals remain separate cards.
	// It also carries the JSON array root (§16.10).
	registerCarries("cpp", carryBoundedTextList, carryArrayRoot)
}
