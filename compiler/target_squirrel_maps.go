package compiler

// The Squirrel leg carries the map codecs (docs/SPEC-TABLES.md §2.8).
func init() {
	registerMapCarrier("squirrel")
}
