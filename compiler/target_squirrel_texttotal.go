// The Squirrel target carries a TEXT LIST TOTAL (docs/SPEC-TABLES.md §2.13):
// the load and the JSON read keep the prefix within the total, and Save,
// Measure, ToJson and the cook writer refuse a value whose sum is past it.
package compiler

func init() {
	registerCarried("squirrel", carryTextListTotal) // §2.13, `| total = N` on a text list
}
