package compiler

func init() {
	addCarriesCase(carriesCase{
		construct: carryArrayRoot,
		schema:    "package probe\ntable Goal\n{\n    x int32\n}\ntable GoalList | json = \"array\"\n{\n    goals [..4]Goal\n}\n",
		refusal:   `json = "array" on table GoalList`,
	})
}
