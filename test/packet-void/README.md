# Payload-free packet union arms

SPEC §4.8 packet wire coverage for #503: one schema, `Void.schema`, and one
driver per leg, `<leg>/`, each answering every case of the shared case file
`testdata/wire/packet-void/cases.txt` (its README says where the pins come
from). `make packet-void-<leg>` runs a leg; python, ruby and lobster also run
it inside their Go tests. The mixed union declares its void arm first.

The C and C++ drivers take `read` or `write` to run one direction. Four named
`tools/sabotage` controls (`make packet-void-{c,cpp}-{read,write}-negative-control`)
add one bit to a void arm on just the write or the read path: the other
direction must answer every case ok, and the altered one must answer
`case mixed-ping FAILED: <direction> has 3 bits, want 2`. Python and Ruby
plant the same extra bit in a copy of their generated code
(`TestPythonPacketUnionsNegativeControl`, `TestRubyPacketVoidNegativeControl`),
and Lobster in `TestLobsterPacketVoidNegativeControls`.
