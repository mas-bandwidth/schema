package compiler

func init() {
	addCarriesCase(carriesCase{
		construct: carryTimestamp,
		schema:    timestampUnit,
		refusal:   "timestamp",
	})
}
