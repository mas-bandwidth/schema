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
TableRefuseMalformed
TableRefuseClamped
TableRefuseKindMismatch
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
case TableRefuseMalformed:return "malformed"
case TableRefuseClamped:return "clamped"
case TableRefuseKindMismatch:return "kind_mismatch"
};return "invalid refusal reason"}
func tableRefuseError(reason TableRefuseReason)error{if reason==TableRefuseOk{return nil};return reason}
// TableBuildVersionMismatch is the typed error a strict announcement refusal
// reports (docs/SPEC-TABLES.md §3.3): the peer's build version and the
// reader's own, each a 64-bit number.
type TableBuildVersionMismatch struct { Peer, Own uint64 }
func(m TableBuildVersionMismatch) Error() string {const digits="0123456789abcdef";var peer,own [16]byte;p,o:=m.Peer,m.Own;for i:=15;i>=0;i-- {peer[i]=digits[p&0xf];p>>=4};for i:=15;i>=0;i-- {own[i]=digits[o&0xf];o>>=4};return "build_version_mismatch: peer 0x"+string(peer[:])+" own 0x"+string(own[:])}
// Err is the report's typed error (docs/SPEC-TABLES.md §6.5, docs/VERSIONING.md
// The read report). A strict read refuses malformed, clamped, kind-mismatched,
// unknown form, wire-damaged and build-version-mismatched, each as one typed
// reason. Unknown, Widened and Retained alone leave it nil, as does a clean read.
func(r *TableReport) Err() error {
if r==nil {return nil}
if r.Reason=="build_version_mismatch" {return TableBuildVersionMismatch{Peer:r.BuildVersionPeer,Own:r.BuildVersionOwn}}
if r.Verdict==TableOpenRefused || r.Reason=="unsupported wire form" || r.Reason=="newer_form" || r.Reason=="message_form_as_file" || r.Reason=="message form requires an announced vocabulary and a message reader" {return TableRefuseUnknownForm}
if r.Verdict==TableOpenDamaged {return TableRefuseWireDamaged}
if r.Malformed {return TableRefuseMalformed}
if r.Clamped>0 {return TableRefuseClamped}
if r.KindMismatch>0 {return TableRefuseKindMismatch}
return nil}

`
