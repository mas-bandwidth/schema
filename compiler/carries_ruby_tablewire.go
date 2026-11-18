package compiler

// THE RUBY TABLE WIRE's carries (docs/PORT-RUBY.md, THE CARD MAP: R6-R9):
// form 1 (64-bit id-table wire), form 2 (message form), retain-unknown,
// strict JSON and hand-editable S-expression text, canonical Base64, block
// and cook accelerators, and the unit view.
var (
	_ = carryRuby(carryTimestamp)
	_ = carryRuby(carryRequired)
	_ = carryRuby(carryForms)
	_ = carryRuby(carryIntegrity)
	_ = carryRuby(carryUnboundedText)
	_ = carryRuby(carryTextListTotal)
	_ = carryRuby(carryTextList)
	_ = carryRuby(carryArrayRoot)
	_ = carryRuby(carryOneSidedRange)
	_ = carryRuby(carryOptionalVariableClosure)
	_ = carryRuby(carryOptionalMap)
)

func init() {
	rubyWideTableKinds = true
	registerOptionalArrayCarrier("ruby")
	valueDefaultTargets = append(valueDefaultTargets, "ruby")
	wasRowTargets = append(wasRowTargets, "ruby")
}
