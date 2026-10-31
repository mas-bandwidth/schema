package compiler

// The Squirrel target carries the `timestamp` kind (docs/SPEC-TABLES.md §2.11,
// §16.2): the table runtime already reads and writes kind 36 as a signed int64
// UTC nanosecond count on the wire (runtime/load.nut readU64, runtime/save.nut)
// and internal/codegen/squirreltable/runtime/json.nut reads and writes the
// strict RFC 3339 §5.6 text that internal/rfc3339's rfc3339Parse defines. The
// central refusal in Generate is lifted here, beside the target's other
// carried constructs (docs/PORT-SQUIRREL.md, THE REGISTER).
func init() {
	registerCarried("squirrel", carryTimestamp) // §2.11, §16.2
}
