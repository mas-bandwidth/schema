package compiler

// The Python table codec carries text lists and unbounded text keys. Totals
// remain refused until the separate total-byte contract is implemented.
var _ = carryPython(carryTextList)
var _ = carryPython(carryUnboundedText)
