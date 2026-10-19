package compiler

func init() {
	// the C++ reference carries the BOUNDED text list (docs/SPEC-TABLES.md
	// §2.13): [..N]string(M) and [..N]?string(M), each element held to M and
	// the count to N as Go holds them. The unbounded element (§2.13.1), the
	// list total and the array root are later cards and stay refused.
	registerCarries("cpp", carryTextList)
}
