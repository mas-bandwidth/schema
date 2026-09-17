package compiler

import "fmt"

// The --codec modes (SPEC §7). A codec mode chooses which CALLS an emitter
// writes, never which BITS: the wire is identical in both modes and the same
// goldens gate them (SPEC §3).
const (
	// CodecBest is each language's ruled fastest correct form — the default.
	// It may be a flat/self-contained codec (Go, Rust, C#, JS, Java, Dart,
	// Elixir today) or the runtime's per-field stream calls where that is
	// already the sanctioned best form (C, C++).
	CodecBest = "best"

	// CodecStream is the PER-FIELD hand-writer idiom: the plain stream calls
	// each serialize library's own docs teach, with no flat folding and no
	// batch. It exists to profile every platform's runtime stream path with
	// uniform, compiler-emitted code — the compiler-value pair the #196
	// excision left unmeasured — and it is expected to be slower than
	// CodecBest on the platforms that carry a flat form.
	CodecStream = "stream"
)

// codecOption is the Options key, named for the CLI flag that sets it.
const codecOption = "codec"

// ParseCodec resolves a generate request's codec mode from the Options map.
// The empty and absent values are CodecBest; an unknown value is refused by
// name with both legal modes, never silently defaulted (the Options contract).
func ParseCodec(opts Options) (string, error) {
	switch v := opts[codecOption]; v {
	case "", CodecBest:
		return CodecBest, nil
	case CodecStream:
		return CodecStream, nil
	default:
		return "", fmt.Errorf("--codec %q is not a mode: the two are %q (the default — each language's ruled fastest correct form) and %q (the per-field hand-writer stream idiom, for profiling the runtime's stream path)", v, CodecBest, CodecStream)
	}
}

// refuseSelfContainedStream refuses CodecStream for a target whose packet
// emitter is self-contained: it has no serialize-family runtime to stream
// through, so honoring the mode is a pinned runtime sibling the tree does not
// carry yet. Refusing by name beats silently emitting best — the mode changes
// the calls, and a target that ignored it would report a stream number that is
// not one.
func refuseSelfContainedStream(opts Options, target string) error {
	if !streamOnly(opts) {
		return nil
	}
	return fmt.Errorf("--codec=%s is not carried by the %s target: its packet codec is self-contained and has no serialize.%s runtime to stream through, so %s is the mode it honors (SPEC §7)", CodecStream, target, target, CodecBest)
}
