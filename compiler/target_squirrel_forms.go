// The Squirrel target carries the `forms` table declaration
// (docs/SPEC-TABLES.md §2.12). The carrier decision lives in the shared
// registry (compiler/carries.go), one registration per target; this file is
// the Squirrel leg's whole share of the rule.
package compiler

func init() {
	registerCarried("squirrel", carryForms) // §2.12, `| forms = "..."`
}
