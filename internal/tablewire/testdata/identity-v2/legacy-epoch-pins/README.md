# Retained main-epoch pins

These 240 original binary pins are retained byte-for-byte from main
`ebf105319` before prospective WireLaw 3 / BuildVersion form 6 regeneration.
Their relative paths mirror the current pin tree. The other 60 changed fixed
wire pins are retained in `../legacy-fixed-wire/`.

Actual native pin writers and the conformance harness regenerate current pins.
Cook headers move only their eight-byte build stamp; fixed files move only
header/record identity keys, preserving all layout and body bytes. Announcements
move only bytes 3 through 10 (the build stamp). The current
`announce_unknown_field.bin` keeps its six-byte unknown field (reference 3)
after the vocabulary field (reference 2), as both its producers write it (the
Go harness and `test/tables/message_form.h`): the id table is in first-use
order and a reader refuses any other (SPEC-TABLES §3). No historical key is rewritten
in these retained files. These are evidence of the old epoch, not promises that
an absent or ambiguous historical writer snapshot can be inferred.
