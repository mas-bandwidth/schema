//go:build schema_leg_goose

package compiler

// THE GOOSE TABLE WIRE's carries (docs/PORT-GOOSE.md, THE CARRIES PATTERN):
// form 1, the 64-bit id-table wire (docs/SPEC-TABLES.md §3), is one
// descriptor-driven walk each way in internal/codegen/goosetable/runtime
// (load.goose, save.goose, retain.goose) over the unit registry tablewire.go
// emits, so every construct that walk reads from a descriptor is carried at
// once: pointers and the flat node table (§3.1), unions with arms of any
// type and arrays of them (§2.6), byte and string buffers (§2.5), optionals
// over a variable closure (§2.3), enum-keyed arrays (§3.2), maps (§2.8),
// lists and unbounded text (§2.9, §2.13), string, bytes and flags defaults
// and was-rows (§5) and the fixed-point and 128-bit kinds (§15). Held by internal/codegen/goosetable's TestGooseTableWire* and
// the conformance driver's wire, report, retain and retain-save surfaces.
var (
	_ = carryGoose(carryTimestamp)
	_ = carryGoose(carryRequired)
	_ = carryGoose(carryUnboundedText)
	_ = carryGoose(carryTextListTotal)
	_ = carryGoose(carryTextList)
	_ = carryGoose(carryOneSidedRange)
	_ = carryGoose(carryOptionalVariableClosure)
	_ = carryGoose(carryOptionalMap)
)

func init() {
	gooseWideTableKinds = true
	registerListCarrier("goose")
	registerMapCarrier("goose")
	registerOptionalArrayCarrier("goose")
	tableArmTargets = append(tableArmTargets, "goose")
	unionArrayTargets = append(unionArrayTargets, "goose")
	blobTargets = append(blobTargets, "goose")
	stringTargets = append(stringTargets, "goose")
	valueDefaultTargets = append(valueDefaultTargets, "goose")
	wasRowTargets = append(wasRowTargets, "goose")
}
