package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/benchgen"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// BenchTargets lists the languages the bench harness emitter targets.
func BenchTargets() []string { return benchgen.Languages() }

// Bench emits the shape-dependent half of the bench harness for one language
// (issue #191): the pinned instance, the LCG vary mapping, and — for the legs
// with no free memory barrier — the §2.7 full-struct sink fold and the
// decoded-field check. Everything else in a leg (the timed loops, the escape
// barriers, the buffers, the CSV) is shape-independent and stays hand-written.
//
// goldens maps a wire golden's basename without .bin ("bench_mixed") to its
// bytes. A top-level struct whose snake_case name has a golden IS a benchmarked
// shape; the golden is decoded and re-emitted as that shape's pin, so the
// pinned values have one source and no leg transcribes them.
//
// This is a SEPARATE emitter from the language backends: bench work never
// touches the generators under measurement (bench/LOCK).
func Bench(u *ir.Unit, lang string, goldens map[string][]byte) (map[string][]byte, error) {
	return benchgen.Emit(u, lang, goldens)
}
