package compiler

// lobsterCarried is every construct the lobster target carries
// (docs/PORT-LOBSTER.md, THE CARD MAP). It is registered ONCE, by init below.
// A later card that carries a construct does not edit this file: it adds, in
// its own new file,
//
//	var _ = carryLobster(carryTimestamp)
//
// Package-level variables are initialised before any init() runs, so every
// such line has extended the slice by the time the registration reads it.
var lobsterCarried []string

// carryLobster adds one construct to what the lobster target carries.
func carryLobster(construct string) bool {
	lobsterCarried = append(lobsterCarried, construct)
	return true
}

func init() {
	registerCarries("lobster", lobsterCarried...)
}
