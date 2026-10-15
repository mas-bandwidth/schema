package gotable

const tableRefusalSource = `
// TableRefuseReason is the typed error shared by file and accelerator failures.
// A successful call returns nil; a failure returns its first failing clause.
type TableRefuseReason uint8
const (
TableRefuseOk TableRefuseReason = iota
TableRefuseNotACook
TableRefuseForeignOrder
TableRefuseWrongBuildVersion
TableRefuseReservedNotZero
TableRefuseBadAlignment
TableRefuseTruncated
TableRefuseUnalignedBase
TableRefuseBadLayout
TableRefuseUnknownForm
TableRefuseCountOverLength
TableRefuseCountOverExtentCap
TableRefuseBlobOverSizeCap
TableRefuseDataCycle
TableRefuseInvalidValue
TableRefuseAllocationFailed
TableRefuseWireDamaged
TableRefuseTextNotBase64
TableRefuseTextDamaged
)
func(r TableRefuseReason) Error()string{switch r {
case TableRefuseOk:return "ok"
case TableRefuseNotACook:return "not_a_cook"
case TableRefuseForeignOrder:return "foreign_order"
case TableRefuseWrongBuildVersion:return "wrong_build_version"
case TableRefuseReservedNotZero:return "reserved_not_zero"
case TableRefuseBadAlignment:return "bad_alignment"
case TableRefuseTruncated:return "truncated"
case TableRefuseUnalignedBase:return "unaligned_base"
case TableRefuseBadLayout:return "bad_layout"
case TableRefuseUnknownForm:return "unknown_form"
case TableRefuseCountOverLength:return "count_over_length"
case TableRefuseCountOverExtentCap:return "count_over_extent_cap"
case TableRefuseBlobOverSizeCap:return "blob_over_size_cap"
case TableRefuseDataCycle:return "data_cycle"
case TableRefuseInvalidValue:return "invalid_value"
case TableRefuseAllocationFailed:return "allocation_failed"
case TableRefuseWireDamaged:return "wire_damaged"
case TableRefuseTextNotBase64:return "text_not_base64"
case TableRefuseTextDamaged:return "text_damaged"
};return "invalid refusal reason"}
// THE TWO TEXT CARRIERS' OWN NAMES (docs/SPEC-TABLES.md §16.8), added at the
// END of the vocabulary and never renumbered. A text channel refuses a body
// that is not padded standard base64 under text_not_base64, and the JSON
// reader refuses a document its walk will not read under text_damaged. A body
// that DECODES to a wire the binary reader refuses keeps the BINARY reader's
// name — unknown_form, wire_damaged — because the carrier adds no refusal of
// its own there and takes none away.
func tableRefuseError(reason TableRefuseReason)error{if reason==TableRefuseOk{return nil};return reason}
// TableBuildVersionMismatch is the typed error a strict announcement refusal
// reports (docs/SPEC-TABLES.md §3.3): the peer's build version and the
// reader's own, each a 64-bit number.
type TableBuildVersionMismatch struct { Peer, Own uint64 }
func(m TableBuildVersionMismatch) Error() string {const digits="0123456789abcdef";var peer,own [16]byte;for i:=15;i>=0;i-- {peer[i]=digits[m.Peer&0xf];m.Peer>>=4};for i:=15;i>=0;i-- {own[i]=digits[m.Own&0xf];m.Own>>=4};return "build_version_mismatch: peer 0x"+string(peer[:])+" own 0x"+string(own[:])}
// Err is the report's typed error: the strict build-version refusal as a
// TableBuildVersionMismatch (docs/SPEC-TABLES.md §3.3), nil on a clean read.
func(r *TableReport) Err() error {if r!=nil&&r.Reason=="build_version_mismatch"{return TableBuildVersionMismatch{Peer:r.BuildVersionPeer,Own:r.BuildVersionOwn}};return nil}
`
