# Packet wide text

The packet file in `examples-wide/WideText.schema` is staged unchanged away
from that directory's table kind 33 and baseline. C++ and each packet port
generate the same declarations. Source and protocol-id pins live under
`testdata/golden/packet-wide`; the existing table-wide pins are unchanged.

The text harness's `-wide` mode reads all 26 applicable shared wstring
corpus rows at bounds 7 and 4, then adds the six SPEC §4.12 interop cases
with independently packed groups. It checks declared verdicts, code units,
consumed bits and canonical bytes. Its 1,016 single-bit mutations include
503 C++ refusals. Bounds 0 and 1 have no schema declaration, so their two
corpus rows remain outside this fixture.

`make packet-wide-c` runs debug and NDEBUG against the corpus. Its separate
ASan/UBSan contract probe checks writer length/null refusals, read-side
surrogate pairing, terminators, untouched reused tails, an unaligned
conditional, branch zeroing, fixed/counted arrays, born counts and repeated
union tags. `packet-wide-c-negative-control` removes the pairing check,
compiles in release and requires mutation agreement to fail its exact marker.
Both targets ride `test-c`. Table-reachable wide text remains refused by name,
including direct scalar union arms, and unrelated tables remain allowed.

`make packet-wide-rust` runs the same oracle in debug and release. Its native
contracts cover writer bounds/null refusals, reader pairing, untouched tails,
unaligned conditionals and zeroing, fixed/counted arrays, born counts and
repeated union tags. `packet-wide-rust-negative-control` removes pairing and
requires the compiled release reader to fail mutation agreement. Both ride
`test-rust`; storage remains a fixed `[u16; N]` with a used length.

`make packet-wide-go` uses fixed uint16 storage and runs the corpus plus native
composition contracts. The reused reader also measures zero allocations. Its
pairing-removal negative control compiles and must fail mutation agreement.
Both targets ride `test-go`.

`make packet-wide-cs` covers debug/release, ordinary and batched nested codecs,
with preallocated char storage, zero measured read allocations and the same
composition contracts. Its compiled pairing-removal control must fail the
mutation comparison. Both targets ride `test-cs`.
