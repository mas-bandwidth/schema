# Message integrity regression

`Probe.schema` carries one CRC-32C trailer for the complete three-body batch
in `cases.json`. `batch.hex` was independently reproduced by the native
JavaScript writer, with the checksum also checked against the standard
Castagnoli implementation. It includes the form, count, packed bodies and pad.

`internal/tablewire/messageintegrity_test.go` requires the neutral writer to
match these bytes, verifies the native JavaScript writer, checks damaged and
short trailers before any body is decoded, and checks defaults when a message
destination is reused. The unsealed plain-table message behavior remains
covered by the existing message corpus.
