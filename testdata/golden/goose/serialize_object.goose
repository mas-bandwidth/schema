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
// On a latched stream it refuses before calling, so an empty object (which
// reads nothing) refuses too. On exit it refuses when the caller's function
// returned false OR left the stream latched (a refusal it swallowed), as the
// C++ macro does from serialize 2.0.1 (commit 3f40cb5f). On a read, a
// callback that returns false with nothing latched is the caller's own
// refusal, latched as REJECTED (U2; STANDARD.md 2.1, rejected); a reason
// already latched inside it is kept, through the same first-wins latch
// (DESIGN.md (a), The latched kinds; STANDARD.md, Failure is terminal). On a
// write or a measure nothing latches: the object returns false and records
// nothing, since serialize_fail there records nothing (U6, U9).
recursive fn serialize_object<S, T>(s: S&, object: T&) -> bool {
    guard serialize_error(s) == SERIALIZE_ERROR_NONE else { return false; }
    guard serialize(s, object) && serialize_error(s) == SERIALIZE_ERROR_NONE else {
        return serialize_fail(s, SERIALIZE_ERROR_REJECTED);
    }
    return true;
}
