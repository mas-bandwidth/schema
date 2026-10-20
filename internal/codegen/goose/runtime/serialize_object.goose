// serialize_object.goose -- composition (STANDARD.md, object; DESIGN.md (a)).
// It contributes no bytes of its own: the nested operations run inline at
// exactly this position, with no framing, length prefix or alignment.

import .serialize_stream;

// Calls the caller's own `serialize(s, object)` overload inline, as the C++
// macro does (DESIGN.md (a), object). The name resolves at instantiation:
// top-level declarations are program-wide and order-independent, so the
// caller's overload, declared in another module, is in the same global
// namespace this body names (measured on Goose v2026.2).
//
// `recursive` admits the caller's serialize calling serialize_object again
// for a nested object (conformance/object.txt nests one).
//
// On a latched stream it refuses before calling; a false from the caller's
// function latches FAILED unless another kind already latched (DESIGN.md
// (a), The latched kinds; STANDARD.md, Failure is terminal).
recursive fn serialize_object<S, T>(s: S&, object: T&) -> bool {
    guard serialize_error(s) == SERIALIZE_ERROR_NONE else { return false; }
    guard serialize(s, object) else { return serialize_fail(s, SERIALIZE_ERROR_FAILED); }
    return true;
}
