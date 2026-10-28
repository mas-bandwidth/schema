// ?T over a variable closure (docs/SPEC-TABLES.md §2.3), carried by the
// Lobster table runtime: its load and save walks gate every edge on presence
// (docs/PORT-LOBSTER.md, THE LOAD).
package compiler

var _ = carryLobster(carryOptionalVariableClosure)
