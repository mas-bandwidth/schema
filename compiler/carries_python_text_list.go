package compiler

// The Python table codec carries text lists and unbounded text keys. Totals
// are enforced on both read and write.
var _ = carryPython(carryTextList)
var _ = carryPython(carryUnboundedText)
var _ = carryPython(carryTextListTotal)
