//go:build schema_leg_python

package compiler

// docs/SPEC-TABLES.md §2.8: the Python target carries `map[K]V` as a dict,
// saved in ascending key order and read by refusing a repeated or a descending
// key. A plain map spends no kind of its own and registers no §2 carry
// construct; this file registers the codec's carrier with the cross-target map
// refusal. An OPTIONAL map, `?map[K]V`, is the separate optional-map construct
// and stays refused until its own card.
func init() {
	registerMapCarrier("python")
}
