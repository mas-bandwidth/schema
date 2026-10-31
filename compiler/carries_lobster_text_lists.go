// Text lists, their total and unbounded text, carried by the Lobster table
// surface (docs/SPEC-TABLES.md §2.13, §2.13.1; docs/PORT-LOBSTER.md, THE CARD
// MAP). Every other target keeps refusing them by name in
// compiler/tablesstringarrays.go.
package compiler

var (
	_ = carryLobster(carryTextList)
	_ = carryLobster(carryTextListTotal)
	_ = carryLobster(carryUnboundedText)
)
