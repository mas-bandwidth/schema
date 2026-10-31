// The Squirrel target carries BOUNDED TEXT LISTS and the ARRAY ROOT
// (docs/SPEC-TABLES.md §2.13, §16.10). The carrier decision lives in the
// shared registry (compiler/carries.go), one registration per target; this
// file is the Squirrel leg's whole share of the rule.
package compiler

func init() {
	registerCarried("squirrel", carryTextList)  // §2.13, `[..N]string(M)` and `[..N]?string(M)`
	registerCarried("squirrel", carryArrayRoot) // §16.10, `| json = "array"`
}
