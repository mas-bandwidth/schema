package goldens

import "testing"

// The example schemas are teaching files, so their comments say what each
// construct does. Many of their types also exist to pin a regression: the
// golden source and golden wire for that type are what turn red if the
// defect returns. The reasons live here, beside the gate, and
// TestCorpusRegressionPinsPresent fails if a pinning type is removed or
// renamed, so a corpus edit cannot quietly retire one.
//
// examples/Types.schema
//
//   - ExpressionProbe (issues #22, #91). Every symbolic backend renders
//     bounds as source text, so two spellings had rendering regressions no
//     golden caught: an author-parenthesized bound, and a doubled unary minus
//     ("--" is a decrement in the C family and a parse error in Go and Rust,
//     so the target form must parenthesize). Dropping either set of parens
//     changes no wire bit and no folded value: only the source goldens go red.
//
//   - ExtremeProbe, ExtremeRow, FloorLimit, CeilingCount (issue #95). The two
//     values with no direct decimal spelling in the C family: INT64_MIN's
//     literal half overflows long long before the unary minus applies, and a
//     value past INT64_MAX has no signed rung of the decimal ladder. Every fold
//     must reach the compiler guarded (( -9223372036854775807LL - 1 ), the ULL
//     suffix), so these types keep both in every -Werror leg as bounds,
//     constants and defaults. doubled_floor is the doubled minus AT the
//     extreme: -(-9223372036854775808) overflows a 64-bit carrier though the
//     final value fits, so every symbolic backend must fold it (C and C#
//     through carrierEval, Rust through renderArg's i64 gate) or evaluate it
//     in arbitrary precision (Go's untyped constants, JS's BigInt). The unit
//     tests in internal/codegen/{c,cpp,rust} pin the per-target spellings.
//
//   - RigidBody is the serialize library README's own example, field for
//     field: its hand-written `else if ( Stream::IsReading )` zeroing is
//     SPEC §5's zero-on-untaken-branch rule.
//
//   - ShipCreate.ship_type is a plain enum, so None is wire-legal; the
//     hand-written create path wrote [1, MAX]. The index form (enum_index)
//     was designed and cut (SPEC §9 q11): one unused wire value is the
//     recorded v1 cost. ShipCreate.pending keeps the zero-variant enum in the
//     cross-language corpus rather than only in a unit test.
//
//   - Vec3's cpp_native mapping is off inside Types.h, because the hand type
//     derives from that header (#675); Degenerate.schema's Vec3Sole is the
//     corpus's one reference to Vec3 from another file, which is what makes
//     the mapping appear in generated C++ at all.
//
// examples/Wire.schema exists so every emitter codepath is reached by the
// corpus (the constructs the protocol-shaped files do not use).
//
//   - ProbeBits: 33 bits is the low-first boundary; the full-range uint32
//     takes the int64 call family and the full-range uint64 the raw offset
//     bits.
//   - ProbeSample: a compressed float on a plain type's wire, a different
//     codepath from the object-view projection.
//   - ProbeArray: New*/construction must install inner defaults
//     transitively in every target; zero init reaches only fields with no
//     specified default.
//   - Heartbeat: the empty-struct emission corner every target must keep
//     legal.
//   - TestData is serialize.h's own TestData. Differences: a/b/c use
//     declared ranges (the library feeds them from a runtime context, which is
//     not a wire feature); the 17-byte block is a fixed byte array behind an
//     explicit align (the exact-N bytes form died in the §4.7 unification);
//     int_relative (§4.10) and wstring are absent from this unit.
//   - CompressedProbe: the FMA-boundary vectors SPEC §7.2 promises. The
//     values were found by SWEEPING each range for divergence: an on-quantum
//     value like TestData's 2.5 agrees under float32, double and fused
//     multiply-add alike, so it cannot detect the arithmetic.
//     boundary = 0.005 is integer 1 under the float32 two-rounding law (fused
//     and double builds say 0). offset = -4.8585 is 142 under the law (a
//     double build says 141), and its non-zero min exercises the v - min
//     subtraction on write and + min on read. A full-range float32 sweep found
//     no fused divergence on the offset shape: measured, not assumed.
//
// examples/Degenerate.schema (issue #203). Realistic shapes always have
// enough neighbouring fields to mask the arrangements an emitter's run/chunk
// machinery keys on. The Go flat word codec wrote `values [2]float64` TWICE,
// 32 bytes where every other language wrote 16, and every Go-to-Go round
// trip stayed green because nothing in the corpus had that shape.
//
//   - Whole-byte widths: the legs whose emitters write a whole message into a
//     buffer (Dart, Java, Elixir) concatenate per-message buffers, which
//     equals what the stream legs write only while no shape ends mid-byte.
//     Trio sums to one 64-bit word for this reason.
//   - No float-bits helpers: the bare-float-unit defect (an unused `math`
//     import in generated Go) can only manifest in a file with no other math
//     consumer. Types.schema's Vec3 survives it only because that file has
//     other float users. The source goldens pin the import block.
//   - SpanOne: a loop and an unrolled element differ only by the emitter's
//     choice. SpanTail: elements and a field share whatever run holds them.
//   - TrioStraddle: the prefix puts Trio's fields on both sides of the Go
//     emitter's 384-bit run cap (maxRunBits), the arrangement that made that
//     emitter name a nested field on the OUTER type.
//     internal/codegen/golang's TestFlatFallbackReEmitsItemsNotPieces states
//     the property against the constant itself; TrioSole and TrioFirst carry
//     the cap-independent halves.
//
// examples/Clauses.schema: adversarial widths for the Elixir grouped-append
// clauses. The write clause has a 52-bit budget and the read clause a 49-bit
// window, each capped at 4 elements, so a width can make the two sides pick
// DIFFERENT element counts k with both clause boundaries mid-byte, which
// Degenerate.schema's whole-byte property cannot reach.
//
//   - W13: write k = 4 (52 bits), read k = 3 (39 bits).
//   - W17: write k = 3 (51), read k = 2 (34).
//   - W26: write k = 2 (52, exactly the budget), read k = 1.
//   - W1: k = 4 on both sides, the narrowest element.
//   - W52 is exactly the budget as one element; W50 is one bit over the read
//     window.
//   - F13: clause boundaries with no count bits.
//   - ArrTri3: staticBitsScalar reports 3, so writeUnroll picks k = 4 and one
//     append group spans four whole structs. ArrEleven: 44 bits on both sides.
//   - HoldsEmptyUnion, Strs, ArrNested, Sole: zero-bit arms, aligns at a
//     non-zero offset, nested arrays at count 0 and at the bound, and a lone
//     field.
//
// examples/Joins.schema: the same blind spot aimed at the Elixir
// static-offset state machine and its join/barrier handling. ArmsAgree must
// keep the offset static and drop scratch_bits from the tuple; ArmsDisagree
// must publish scratch_bits and go back to maintaining the variable; ArmAlign
// regains staticness on one path only; ArmArray gives it up on one path only;
// HoldsUneven and ArrUneven put unequal union arms at mid-byte offsets;
// RegainAfterAlign gives staticness up, regains it at an align, and follows
// it with a long static run.
//
// examples128/Ludicrous.schema
//
//   - DegenerateProbe: before min == max was legal, an audit found four
//     behaviours: zero bits (C), fraction_bits on the wide path only (Go and
//     C#), an abort (C++) and a panic (Rust). Every degenerate shape rides in
//     front of a tail byte, so a port that emits ANY bits for one shifts the
//     tail and fails the byte-compare. locked_fixed is the fraction_bits shape;
//     locked_wide is the 64/128 boundary shape.
//   - UnsignedProbe.span fills uint64's high half, where any sign-extending
//     route corrupts it; the byte-compare proves the C entry routing and the
//     Go bit-casts. locked is the degenerate ufixed shape.
//   - WideProbe.seed is beyond int64, which C++ renders as composed unsigned
//     halves (no 128-bit literal exists).
var corpusRegressionPins = map[string][]string{
	corpusDir: {
		"ExpressionProbe", "ExtremeProbe", "ExtremeRow", "RigidBody", "ShipCreate",
		"ProbeBits", "ProbeSample", "ProbeArray", "Heartbeat", "TestData", "CompressedProbe",
		"Vec2", "SpanF64", "SpanU64", "SpanI64", "SpanOne", "SpanChunk", "SpanTail", "SpanTwice",
		"TrioSole", "Vec3Sole", "TrioFirst", "TrioStraddle",
		"W13", "W17", "W26", "W1", "W52", "W50", "F13", "ArrTri3", "ArrEleven",
		"HoldsEmptyUnion", "Strs", "ArrNested", "Sole",
		"ArmsAgree", "ArmsDisagree", "ArmEmpty", "ArmsNested", "ArmAlign", "ArmArray",
		"HoldsUneven", "ArrUneven", "RegainAfterAlign",
	},
	corpus128Dir: {"DegenerateProbe", "UnsignedProbe", "WideProbe"},
}

func TestCorpusRegressionPinsPresent(t *testing.T) {
	for dir, names := range corpusRegressionPins {
		u := loadCorpusDir(t, dir)
		for _, name := range names {
			if u.Structs[name] == nil {
				t.Errorf("%s no longer declares %s, which pins a regression: see the "+
					"comment in regressions_test.go before removing or renaming it", dir, name)
			}
		}
	}
}
