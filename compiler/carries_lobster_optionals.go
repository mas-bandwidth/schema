// `| required` (docs/SPEC-TABLES.md §2.3) and `?map[K]V` (§2.8), carried by
// the Lobster table runtime: the load counts and names missing_required at
// any depth and the save never elides a required field
// (runtime/load.lobster, runtime/schema_save.lobster); an absent optional map
// elides while a present-empty one writes its kind-14 body with count 0
// (TestLobsterOptionals holds both to the Go engine).
package compiler

var (
	_ = carryLobster(carryRequired)
	_ = carryLobster(carryOptionalMap)
)
