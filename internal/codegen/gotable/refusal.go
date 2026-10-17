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
TableRefuseBase64Damaged
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
case TableRefuseBase64Damaged:return "base64_damaged"
};return "invalid refusal reason"}
func tableRefuseError(reason TableRefuseReason)error{if reason==TableRefuseOk{return nil};return reason}
// TableBuildVersionMismatch is the typed error a strict announcement refusal
// reports (docs/SPEC-TABLES.md §3.3): the peer's build version and the
// reader's own, each a 64-bit number.
type TableBuildVersionMismatch struct { Peer, Own uint64 }
func(m TableBuildVersionMismatch) Error() string {const digits="0123456789abcdef";var peer,own [16]byte;p,o:=m.Peer,m.Own;for i:=15;i>=0;i-- {peer[i]=digits[p&0xf];p>>=4};for i:=15;i>=0;i-- {own[i]=digits[o&0xf];o>>=4};return "build_version_mismatch: peer 0x"+string(peer[:])+" own 0x"+string(own[:])}
// TableMissingRequired is the typed error a strict read reports for a record
// that lacks a | required field (docs/SPEC-TABLES.md §2.3): Fields names the
// missing fields, comma-separated in declaration order, at whatever depth of
// the record they were missing, and Error() is the report's own reason.
type TableMissingRequired struct { Fields string }
func(m TableMissingRequired) Error() string {return "missing_required: "+m.Fields}
// TableFixedRefusal is the typed error of a fixed-form refusal (docs/SPEC-TABLES.md
// §6.5): Error() is the refusal's own reason — layout_unknown, count_over_bound,
// plan_too_large, and so on — never unknown_form.
type TableFixedRefusal struct { Reason string }
func(r TableFixedRefusal) Error() string {return r.Reason}
// tableFixedReasonNamed reports whether reason is a fixed-form refusal reason (a
// TableFixedReason* constant other than the form-byte ones), not a form-byte or
// message-form one (docs/SPEC-TABLES.md §6.5). batch_too_large is a message
// path's capacity name too, so a message refusal of it answers unknown_form; it
// is left out here and never answers TableFixedRefusal.
func tableFixedReasonNamed(reason string) bool {switch reason {
case TableFixedReasonLayoutMalformed, TableFixedReasonLayoutUnknown, TableFixedReasonLayoutNewer, TableFixedReasonLayoutUnsupported, TableFixedReasonLayoutRecordTooLarge, TableFixedReasonPlanTooLarge, TableFixedReasonNoLayout, TableFixedReasonTextIllFormed, TableFixedReasonTruncated, TableFixedReasonReservedNotZero, TableFixedReasonCountOverBound, TableFixedReasonLengthOverBound:return true};return false}
// Err is the report's typed error (docs/SPEC-TABLES.md §6.5, docs/VERSIONING.md
// The read report). A strict read refuses malformed, clamped, kind-mismatched,
// unknown form, wire-damaged, build-version-mismatched and missing-required
// (§2.3, naming the field), each as one typed reason. A refused report whose
// reason is a fixed reason other than a form-byte one answers TableFixedRefusal;
// a form-byte or message-form refusal answers TableRefuseUnknownForm (so
// errors.Is works there). Unknown, Widened and Retained alone leave it nil, as does a clean read.
func(r *TableReport) Err() error {
if r==nil {return nil}
if r.Reason=="build_version_mismatch" {return TableBuildVersionMismatch{Peer:r.BuildVersionPeer,Own:r.BuildVersionOwn}}
if r.Verdict==TableOpenRefused {
if tableFixedReasonNamed(r.Reason) {return TableFixedRefusal{Reason:r.Reason}}
return TableRefuseUnknownForm}
if r.Verdict==TableOpenDamaged {return TableRefuseWireDamaged}
if r.MissingRequired>0 {const p="missing_required: ";f:=r.Reason;if len(f)>=len(p)&&f[:len(p)]==p{f=f[len(p):]};return TableMissingRequired{Fields:f}}
if r.Malformed {return TableRefuseMalformed}
if r.Clamped>0 {return TableRefuseClamped}
if r.KindMismatch>0 {return TableRefuseKindMismatch}
return nil}

`
