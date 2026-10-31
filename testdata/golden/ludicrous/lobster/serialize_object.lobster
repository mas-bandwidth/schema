// serialize_object.lobster — object: the caller's own serialize, inline
// (STANDARD.md, object; DESIGN.md (a), (f)).

import .serialize_stream

// The object contract: an object is a subclass of this base and carries its
// own serialize, specialized per stream (DESIGN.md (a)). The base method is the
// API-misuse answer for an object that defines none; it never runs for a
// subclass that overrides serialize.
abstract class serialize_object_base:
    def serialize(s) -> bool:
        // On a read this is caller misuse, refused and never an abort
        // (IMPLEMENTING.md U7).
        if s.is_reading():
            return s.fail(serialize_refusal_value_out_of_range)
        serialize_abort("serialize: object must define serialize")
        return false

// Invokes the object's own serialize function inline; it contributes no bytes
// of its own (STANDARD.md, object). On a read, a latched stream refuses before
// the object runs; a false return latches and leaves the stream terminal
// (DESIGN.md (a)). A refusal inside the object keeps its own reason (the first
// one latched); an object that returns false on its own, with no stream
// refusal, is its caller's refusal and latches rejected (STANDARD.md, Refusal
// reasons; IMPLEMENTING.md U2). On a write or a measure nothing is recorded
// (U6, U9): the object's false is answered with false.
def serialize_object(s:serialize_write_stream, object:serialize_object_base) -> bool:
    return object.serialize(s)

def serialize_object(s:serialize_read_stream, object:serialize_object_base) -> bool:
    if s.failed:
        return false
    let ok = object.serialize(s)
    if not ok:
        return s.fail(serialize_refusal_rejected)
    return s.ok()

def serialize_object(s:serialize_measure_stream, object:serialize_object_base) -> bool:
    return object.serialize(s)
