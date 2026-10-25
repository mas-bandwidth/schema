// serialize_object.lobster — object: the caller's own serialize, inline
// (STANDARD.md, object; DESIGN.md (a), (f)).

import .serialize_stream

// The object contract: an object is a subclass of this base and carries its
// own serialize, specialized per stream (DESIGN.md (a)). The base method is the
// API-misuse answer for an object that defines none; it never runs for a
// subclass that overrides serialize.
abstract class serialize_object_base:
    def serialize(s) -> bool:
        serialize_abort("serialize: object must define serialize")
        return false

// Invokes the object's own serialize function inline; it contributes no bytes
// of its own (STANDARD.md, object). A latched stream refuses before the object
// runs; a false return latches and leaves the stream terminal (DESIGN.md (a)).
def serialize_object(s:serialize_write_stream, object:serialize_object_base) -> bool:
    if s.failed:
        return false
    let ok = object.serialize(s)
    if not ok:
        return s.fail()
    return s.ok()

def serialize_object(s:serialize_read_stream, object:serialize_object_base) -> bool:
    if s.failed:
        return false
    let ok = object.serialize(s)
    if not ok:
        return s.fail()
    return s.ok()

def serialize_object(s:serialize_measure_stream, object:serialize_object_base) -> bool:
    if s.failed:
        return false
    let ok = object.serialize(s)
    if not ok:
        return s.fail()
    return s.ok()
