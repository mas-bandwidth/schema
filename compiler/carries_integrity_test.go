package compiler

func init() {
	addCarriesCase(carriesCase{
		construct: carryIntegrity,
		schema:    "package probe\ntable Ledger | integrity = \"crc32c\"\n{\n    x int32\n}\n",
		refusal:   "integrity on table Ledger",
	})
}
