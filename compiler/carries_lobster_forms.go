package compiler

// The descriptor runtime gates forms per root; public Cook and block entry
// points are emitted only for declared forms. Integrity and array roots use the same
// checked paths as ordinary wire and JSON reads.
var (
	_ = carryLobster(carryForms)
	_ = carryLobster(carryIntegrity)
	_ = carryLobster(carryArrayRoot)
)
