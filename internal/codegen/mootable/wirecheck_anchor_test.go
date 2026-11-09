package mootable_test

// The emitted read's phase one, the wire-alone walk (table_load.moo
// ReadStrict), and its removal. A control that plants a defect in a check
// phase two (the decode) still owns removes phase one with it, so the
// decode's check is what stands between the input and a value.
const (
	wirePhaseOne    = "local wireFail, wireNonFinite = wireCheck(b, 1, fr.e, fr.ids)"
	wirePhaseOneOff = "local wireFail: string?, wireNonFinite = nil, false"
)
