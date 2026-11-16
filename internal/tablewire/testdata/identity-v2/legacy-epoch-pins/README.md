# Retained main-epoch pins

These 240 original binary pins are retained byte-for-byte from main
`ebf105319` before prospective WireLaw 3 / BuildVersion form 6 regeneration.
Their relative paths mirror the current pin tree. The other 60 changed fixed
wire pins are retained in `../legacy-fixed-wire/`.

Actual native pin writers and the conformance harness regenerate current pins.
Cook headers move only their eight-byte build stamp; fixed files move only
header/record identity keys, preserving all layout and body bytes. Announcements
move only bytes 3 through 10 (the build stamp). The one separate
`announce_unknown_field.bin` consumer witness also moves its six-byte unknown
field before the vocabulary field, as required by the reviewed current C++
test producer; its payload and size are unchanged. No historical key is rewritten
in these retained files. These are evidence of the old epoch, not promises that
an absent or ambiguous historical writer snapshot can be inferred.
