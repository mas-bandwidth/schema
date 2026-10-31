// The INTEGRITY TRAILER on the Squirrel table surface
// (docs/SPEC-TABLES.md §2.14): the target declares that it carries the
// construct, so the untagged registry test's expectation joins it here the way
// compiler/target_squirrel_onesided_test.go does (compiler/carries_test.go is
// read, never edited).
package compiler

func init() {
	nonGoCarriers["squirrel"] = append(nonGoCarriers["squirrel"], carryIntegrity)
}
